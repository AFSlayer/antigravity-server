package patches

import (
	"bytes"
	"regexp"
	"regexp/syntax"
	"sort"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	// regexpWindowRadius bounds how far a single bounded patch match can extend on
	// either side of its required literal anchor. Every bounded bundle patch spans
	// well under 5 KB, so 16 KB on each side (32 KB window) leaves >3x safety margin.
	regexpWindowRadius = 16 << 10
	// regexpExpandedWindowRadius is used when an unbounded repetition misses inside
	// the initial 16 KB radius, catching wide single-span matches up to 128 KB from
	// the anchor without scanning the entire multi-megabyte bundle.
	regexpExpandedWindowRadius = 128 << 10
	// maxAnchorOccurrences falls back to a full-buffer regexp scan when a candidate
	// literal appears too frequently in the bundle to narrow the search space.
	maxAnchorOccurrences = 64
	minRequiredLitLen    = 4
	maxRequiredAlts      = 8
)

type windowedRegexp struct {
	re                 *regexp.Regexp
	anchors            [][]byte
	hasUnboundedRepeat bool
}

type byteSpan struct {
	start int
	end   int
}

var windowedRegexpCache sync.Map // map[*regexp.Regexp]*windowedRegexp

func getWindowedRegexp(re *regexp.Regexp) *windowedRegexp {
	if cached, ok := windowedRegexpCache.Load(re); ok {
		return cached.(*windowedRegexp)
	}
	w := &windowedRegexp{re: re}
	if parsed, err := syntax.Parse(re.String(), syntax.Perl); err == nil {
		if !hasUnboundedDotOrAllChar(parsed) &&
			wildcardRepeatMaxSum(parsed) < regexpWindowRadius/2 &&
			!hasTextOrLineAnchor(parsed) {
			if lits, ok := extractRequiredLiterals(parsed); ok {
				anchors := make([][]byte, 0, len(lits))
				for _, s := range lits {
					anchors = append(anchors, []byte(s))
				}
				w.anchors = anchors
				w.hasUnboundedRepeat = hasAnyUnboundedRepeat(parsed)
			}
		}
	}
	actual, _ := windowedRegexpCache.LoadOrStore(re, w)
	return actual.(*windowedRegexp)
}

func charClassRuneCount(ranges []rune) int {
	total := 0
	for i := 0; i+1 < len(ranges); i += 2 {
		span := int(ranges[i+1]-ranges[i]) + 1
		if span > 0 {
			total += span
			if total > unicode.MaxRune {
				return unicode.MaxRune
			}
		}
	}
	return total
}

func isAllCharWildcardAtom(r *syntax.Regexp) bool {
	if r == nil {
		return false
	}
	switch r.Op {
	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return true
	case syntax.OpCharClass:
		return len(r.Rune) == 2 && r.Rune[0] == 0 && r.Rune[1] == unicode.MaxRune
	case syntax.OpCapture:
		return len(r.Sub) == 1 && isAllCharWildcardAtom(r.Sub[0])
	default:
		return false
	}
}

func isBroadCharClassAtom(r *syntax.Regexp) bool {
	if r == nil {
		return false
	}
	if isAllCharWildcardAtom(r) {
		return true
	}
	switch r.Op {
	case syntax.OpCharClass:
		return charClassRuneCount(r.Rune) > 256
	case syntax.OpCapture:
		return len(r.Sub) == 1 && isBroadCharClassAtom(r.Sub[0])
	default:
		return false
	}
}

func containsAtomAnywhere(r *syntax.Regexp, pred func(*syntax.Regexp) bool) bool {
	if r == nil {
		return false
	}
	if pred(r) {
		return true
	}
	for _, sub := range r.Sub {
		if containsAtomAnywhere(sub, pred) {
			return true
		}
	}
	return false
}

// hasUnboundedDotOrAllChar reports whether r contains an unbounded repetition (*, +,
// or {n,}) over . or [\s\S], which commonly bridges distant sections across the bundle.
func hasUnboundedDotOrAllChar(r *syntax.Regexp) bool {
	if r == nil {
		return false
	}
	switch r.Op {
	case syntax.OpStar, syntax.OpPlus:
		if len(r.Sub) > 0 && containsAtomAnywhere(r.Sub[0], isAllCharWildcardAtom) {
			return true
		}
	case syntax.OpRepeat:
		if r.Max < 0 && len(r.Sub) > 0 && containsAtomAnywhere(r.Sub[0], isAllCharWildcardAtom) {
			return true
		}
	}
	for _, sub := range r.Sub {
		if hasUnboundedDotOrAllChar(sub) {
			return true
		}
	}
	return false
}

// hasAnyUnboundedRepeat reports whether r contains any unbounded repetition
// (*, +, or {n,}) so a missed 16 KB window can be re-evaluated in an expanded
// window before moving on to subsequent anchor spans.
func hasAnyUnboundedRepeat(r *syntax.Regexp) bool {
	if r == nil {
		return false
	}
	switch r.Op {
	case syntax.OpStar, syntax.OpPlus:
		return true
	case syntax.OpRepeat:
		if r.Max < 0 {
			return true
		}
	}
	for _, sub := range r.Sub {
		if hasAnyUnboundedRepeat(sub) {
			return true
		}
	}
	return false
}

func wildcardRepeatMaxSum(r *syntax.Regexp) int {
	if r == nil {
		return 0
	}
	switch r.Op {
	case syntax.OpRepeat:
		if r.Max > 0 && len(r.Sub) > 0 {
			if containsAtomAnywhere(r.Sub[0], isBroadCharClassAtom) {
				return r.Max
			}
			return r.Max * wildcardRepeatMaxSum(r.Sub[0])
		}
	case syntax.OpAlternate:
		best := 0
		for _, sub := range r.Sub {
			if v := wildcardRepeatMaxSum(sub); v > best {
				best = v
			}
		}
		return best
	}
	sum := 0
	for _, sub := range r.Sub {
		sum += wildcardRepeatMaxSum(sub)
	}
	return sum
}

// hasTextOrLineAnchor reports whether r uses ^, $, \A, or \z anchors that would
// falsely match an interior slice boundary if evaluated on a sub-window.
func hasTextOrLineAnchor(r *syntax.Regexp) bool {
	if r == nil {
		return false
	}
	switch r.Op {
	case syntax.OpBeginText, syntax.OpEndText, syntax.OpBeginLine, syntax.OpEndLine:
		return true
	}
	for _, sub := range r.Sub {
		if hasTextOrLineAnchor(sub) {
			return true
		}
	}
	return false
}

// extractRequiredLiterals derives a small disjunctive set of literal substrings
// from a parsed regexp AST such that EVERY possible match of r is guaranteed to
// contain at least one element of the returned slice.
func extractRequiredLiterals(r *syntax.Regexp) ([]string, bool) {
	if r == nil {
		return nil, false
	}
	switch r.Op {
	case syntax.OpLiteral:
		if r.Flags&syntax.FoldCase != 0 || len(r.Rune) == 0 {
			return nil, false
		}
		s := string(r.Rune)
		if len(s) < minRequiredLitLen {
			return nil, false
		}
		return []string{s}, true

	case syntax.OpCapture, syntax.OpPlus:
		if len(r.Sub) == 0 {
			return nil, false
		}
		return extractRequiredLiterals(r.Sub[0])

	case syntax.OpRepeat:
		if r.Min < 1 || len(r.Sub) == 0 {
			return nil, false
		}
		return extractRequiredLiterals(r.Sub[0])

	case syntax.OpConcat:
		var best []string
		bestScore := -1
		var litBuf []rune
		flushLit := func() {
			if len(litBuf) == 0 {
				return
			}
			s := string(litBuf)
			litBuf = litBuf[:0]
			if len(s) >= minRequiredLitLen && len(s) > bestScore {
				best = []string{s}
				bestScore = len(s)
			}
		}
		for _, sub := range r.Sub {
			if sub.Op == syntax.OpLiteral && sub.Flags&syntax.FoldCase == 0 {
				litBuf = append(litBuf, sub.Rune...)
				continue
			}
			flushLit()
			cands, ok := extractRequiredLiterals(sub)
			if !ok || len(cands) == 0 || len(cands) > maxRequiredAlts {
				continue
			}
			score := minStringLen(cands)
			if score >= minRequiredLitLen && (score > bestScore || (score == bestScore && len(cands) < len(best))) {
				best = cands
				bestScore = score
			}
		}
		flushLit()
		if bestScore < minRequiredLitLen {
			return nil, false
		}
		return best, true

	case syntax.OpAlternate:
		if len(r.Sub) == 0 {
			return nil, false
		}
		seen := make(map[string]struct{})
		var union []string
		for _, sub := range r.Sub {
			cands, ok := extractRequiredLiterals(sub)
			if !ok || len(cands) == 0 {
				return nil, false
			}
			for _, c := range cands {
				if len(c) < minRequiredLitLen {
					return nil, false
				}
				if _, exists := seen[c]; !exists {
					seen[c] = struct{}{}
					union = append(union, c)
					if len(union) > maxRequiredAlts {
						return nil, false
					}
				}
			}
		}
		if len(union) == 0 {
			return nil, false
		}
		return union, true

	default:
		return nil, false
	}
}

func minStringLen(ss []string) int {
	if len(ss) == 0 {
		return 0
	}
	m := len(ss[0])
	for _, s := range ss[1:] {
		if len(s) < m {
			m = len(s)
		}
	}
	return m
}

func isJSIdentByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '$'
}

// candidateSpans returns sorted, disjoint byte windows around all occurrences of
// w.anchors in body. fallback=true tells the caller to run w.re over the full body.
func (w *windowedRegexp) candidateSpans(body []byte) (spans []byteSpan, fallback bool) {
	if len(w.anchors) == 0 || len(body) <= 2*regexpWindowRadius {
		return nil, true
	}

	var raw []byteSpan
	for _, anchor := range w.anchors {
		off := 0
		for off <= len(body)-len(anchor) {
			idx := bytes.Index(body[off:], anchor)
			if idx < 0 {
				break
			}
			pos := off + idx
			if len(raw) >= maxAnchorOccurrences {
				return nil, true
			}
			wStart := pos - regexpWindowRadius
			if wStart < 0 {
				wStart = 0
			} else {
				for wStart > 0 && isJSIdentByte(body[wStart-1]) {
					wStart--
				}
				for wStart > 0 && !utf8.RuneStart(body[wStart]) {
					wStart--
				}
			}
			wEnd := pos + len(anchor) + regexpWindowRadius
			if wEnd >= len(body) {
				wEnd = len(body)
			} else {
				for wEnd < len(body) && isJSIdentByte(body[wEnd]) {
					wEnd++
				}
				for wEnd < len(body) && !utf8.RuneStart(body[wEnd]) {
					wEnd++
				}
			}
			raw = append(raw, byteSpan{start: wStart, end: wEnd})
			off = pos + 1
		}
	}

	if len(raw) == 0 {
		return nil, false
	}

	sort.Slice(raw, func(i, j int) bool {
		if raw[i].start != raw[j].start {
			return raw[i].start < raw[j].start
		}
		return raw[i].end < raw[j].end
	})

	merged := raw[:1]
	totalBytes := 0
	for _, sp := range raw[1:] {
		last := &merged[len(merged)-1]
		if sp.start <= last.end {
			if sp.end > last.end {
				last.end = sp.end
			}
		} else {
			totalBytes += last.end - last.start
			merged = append(merged, sp)
		}
	}
	totalBytes += merged[len(merged)-1].end - merged[len(merged)-1].start
	if totalBytes > len(body)/2 {
		return nil, true
	}
	return merged, false
}

func expandSpan(body []byte, sp byteSpan) byteSpan {
	extra := regexpExpandedWindowRadius - regexpWindowRadius
	wStart := sp.start - extra
	if wStart <= 0 {
		wStart = 0
	} else {
		for wStart > 0 && isJSIdentByte(body[wStart-1]) {
			wStart--
		}
		for wStart > 0 && !utf8.RuneStart(body[wStart]) {
			wStart--
		}
	}
	wEnd := sp.end + extra
	if wEnd >= len(body) {
		wEnd = len(body)
	} else {
		for wEnd < len(body) && isJSIdentByte(body[wEnd]) {
			wEnd++
		}
		for wEnd < len(body) && !utf8.RuneStart(body[wEnd]) {
			wEnd++
		}
	}
	return byteSpan{start: wStart, end: wEnd}
}

func touchesWindowBoundary(sp byteSpan, loc []int, bodyLen int) bool {
	if len(loc) < 2 {
		return false
	}
	if sp.start > 0 && loc[0] == 0 {
		return true
	}
	if sp.end < bodyLen && loc[1] == sp.end-sp.start {
		return true
	}
	return false
}

func regexpFindSubmatchIndex(re *regexp.Regexp, body []byte) []int {
	w := getWindowedRegexp(re)
	spans, fallback := w.candidateSpans(body)
	if fallback {
		return re.FindSubmatchIndex(body)
	}
	if len(spans) == 0 {
		return nil
	}
	for _, sp := range spans {
		loc := re.FindSubmatchIndex(body[sp.start:sp.end])
		if loc == nil && w.hasUnboundedRepeat && (sp.start > 0 || sp.end < len(body)) {
			sp = expandSpan(body, sp)
			loc = re.FindSubmatchIndex(body[sp.start:sp.end])
		}
		if loc != nil {
			if touchesWindowBoundary(sp, loc, len(body)) {
				return re.FindSubmatchIndex(body)
			}
			for i := range loc {
				if loc[i] >= 0 {
					loc[i] += sp.start
				}
			}
			return loc
		}
	}
	return nil
}

func regexpMatch(re *regexp.Regexp, body []byte) bool {
	w := getWindowedRegexp(re)
	spans, fallback := w.candidateSpans(body)
	if fallback {
		return re.Match(body)
	}
	if len(spans) == 0 {
		return false
	}
	for _, sp := range spans {
		loc := re.FindIndex(body[sp.start:sp.end])
		if loc == nil && w.hasUnboundedRepeat && (sp.start > 0 || sp.end < len(body)) {
			sp = expandSpan(body, sp)
			loc = re.FindIndex(body[sp.start:sp.end])
		}
		if loc != nil {
			if touchesWindowBoundary(sp, loc, len(body)) {
				return re.Match(body)
			}
			return true
		}
	}
	return false
}

func regexpFindAllIndex(re *regexp.Regexp, body []byte, n int) [][]int {
	if n == 0 {
		return nil
	}
	w := getWindowedRegexp(re)
	spans, fallback := w.candidateSpans(body)
	if fallback {
		return re.FindAllIndex(body, n)
	}
	if len(spans) == 0 {
		return nil
	}
	var out [][]int
	lastEnd := -1
	for _, sp := range spans {
		rem := -1
		if n > 0 {
			rem = n - len(out)
			if rem <= 0 {
				break
			}
		}
		matches := re.FindAllIndex(body[sp.start:sp.end], rem)
		if len(matches) == 0 && w.hasUnboundedRepeat && (sp.start > 0 || sp.end < len(body)) {
			sp = expandSpan(body, sp)
			matches = re.FindAllIndex(body[sp.start:sp.end], rem)
		}
		for _, m := range matches {
			if touchesWindowBoundary(sp, m, len(body)) {
				return re.FindAllIndex(body, n)
			}
			absStart := m[0] + sp.start
			absEnd := m[1] + sp.start
			if absStart < lastEnd {
				continue
			}
			out = append(out, []int{absStart, absEnd})
			lastEnd = absEnd
			if n > 0 && len(out) >= n {
				return out
			}
		}
	}
	return out
}
