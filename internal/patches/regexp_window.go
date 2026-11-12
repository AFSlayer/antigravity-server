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
	flexPrefix         bool
	prefixUnbounded    bool
	prefixMaxBytes     int
	flexSuffix         bool
	suffixUnbounded    bool
	suffixMaxBytes     int
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
			boundedRepeatMaxRunes(parsed) < regexpWindowRadius/2 &&
			!hasTextOrLineAnchor(parsed) {
			if lits, ok := extractRequiredLiterals(parsed); ok {
				anchors := make([][]byte, 0, len(lits))
				for _, s := range lits {
					anchors = append(anchors, []byte(s))
				}
				w.anchors = anchors
				w.hasUnboundedRepeat = hasAnyUnboundedRepeat(parsed) || boundedRepeatMaxBytes(parsed) >= regexpWindowRadius/2
				analyzeEdgeMargins(parsed, lits, w)
			}
		}
	}
	actual, _ := windowedRegexpCache.LoadOrStore(re, w)
	return actual.(*windowedRegexp)
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

func capAddSpan(a, b int) int {
	if a >= regexpExpandedWindowRadius || b >= regexpExpandedWindowRadius || a+b >= regexpExpandedWindowRadius {
		return regexpExpandedWindowRadius
	}
	return a + b
}

func capMulSpan(a, b int) int {
	if a <= 0 || b <= 0 {
		return 0
	}
	if a >= regexpExpandedWindowRadius || b >= regexpExpandedWindowRadius || a > regexpExpandedWindowRadius/b {
		return regexpExpandedWindowRadius
	}
	return a * b
}

func unitMaxRunes(r *syntax.Regexp) int {
	if r == nil {
		return 0
	}
	switch r.Op {
	case syntax.OpLiteral:
		return len(r.Rune)
	case syntax.OpCharClass, syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return 1
	case syntax.OpCapture, syntax.OpQuest, syntax.OpStar, syntax.OpPlus:
		if len(r.Sub) == 0 {
			return 0
		}
		return unitMaxRunes(r.Sub[0])
	case syntax.OpRepeat:
		if len(r.Sub) == 0 {
			return 0
		}
		if r.Max > 0 {
			return capMulSpan(r.Max, unitMaxRunes(r.Sub[0]))
		}
		if r.Max == 0 {
			return 0
		}
		n := r.Min
		if n < 1 {
			n = 1
		}
		return capMulSpan(n, unitMaxRunes(r.Sub[0]))
	case syntax.OpAlternate:
		best := 0
		for _, sub := range r.Sub {
			if v := unitMaxRunes(sub); v > best {
				best = v
			}
		}
		return best
	default:
		sum := 0
		for _, sub := range r.Sub {
			sum = capAddSpan(sum, unitMaxRunes(sub))
		}
		return sum
	}
}

func boundedRepeatMaxRunes(r *syntax.Regexp) int {
	if r == nil {
		return 0
	}
	switch r.Op {
	case syntax.OpRepeat:
		if len(r.Sub) == 0 {
			return 0
		}
		if r.Max > 0 {
			return capMulSpan(r.Max, unitMaxRunes(r.Sub[0]))
		}
		if r.Max < 0 {
			n := r.Min
			if n < 1 {
				n = 1
			}
			return capMulSpan(n, boundedRepeatMaxRunes(r.Sub[0]))
		}
		return 0
	case syntax.OpAlternate:
		best := 0
		for _, sub := range r.Sub {
			if v := boundedRepeatMaxRunes(sub); v > best {
				best = v
			}
		}
		return best
	default:
		sum := 0
		for _, sub := range r.Sub {
			sum = capAddSpan(sum, boundedRepeatMaxRunes(sub))
		}
		return sum
	}
}

func unitMaxBytes(r *syntax.Regexp) int {
	if r == nil {
		return 0
	}
	switch r.Op {
	case syntax.OpLiteral:
		if r.Flags&syntax.FoldCase != 0 {
			return capMulSpan(len(r.Rune), utf8.UTFMax)
		}
		return len(string(r.Rune))
	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return utf8.UTFMax
	case syntax.OpCharClass:
		if len(r.Rune) > 0 && r.Rune[len(r.Rune)-1] < utf8.RuneSelf {
			return 1
		}
		return utf8.UTFMax
	case syntax.OpCapture, syntax.OpQuest, syntax.OpStar, syntax.OpPlus:
		if len(r.Sub) == 0 {
			return 0
		}
		return unitMaxBytes(r.Sub[0])
	case syntax.OpRepeat:
		if len(r.Sub) == 0 {
			return 0
		}
		if r.Max > 0 {
			return capMulSpan(r.Max, unitMaxBytes(r.Sub[0]))
		}
		if r.Max == 0 {
			return 0
		}
		n := r.Min
		if n < 1 {
			n = 1
		}
		return capMulSpan(n, unitMaxBytes(r.Sub[0]))
	case syntax.OpAlternate:
		best := 0
		for _, sub := range r.Sub {
			if v := unitMaxBytes(sub); v > best {
				best = v
			}
		}
		return best
	default:
		sum := 0
		for _, sub := range r.Sub {
			sum = capAddSpan(sum, unitMaxBytes(sub))
		}
		return sum
	}
}

func boundedRepeatMaxBytes(r *syntax.Regexp) int {
	if r == nil {
		return 0
	}
	switch r.Op {
	case syntax.OpRepeat:
		if len(r.Sub) == 0 {
			return 0
		}
		if r.Max > 0 {
			return capMulSpan(r.Max, unitMaxBytes(r.Sub[0]))
		}
		if r.Max < 0 {
			n := r.Min
			if n < 1 {
				n = 1
			}
			return capMulSpan(n, boundedRepeatMaxBytes(r.Sub[0]))
		}
		return 0
	case syntax.OpAlternate:
		best := 0
		for _, sub := range r.Sub {
			if v := boundedRepeatMaxBytes(sub); v > best {
				best = v
			}
		}
		return best
	default:
		sum := 0
		for _, sub := range r.Sub {
			sum = capAddSpan(sum, boundedRepeatMaxBytes(sub))
		}
		return sum
	}
}

func maxMatchBytes(r *syntax.Regexp) (int, bool) {
	if r == nil {
		return 0, true
	}
	switch r.Op {
	case syntax.OpEmptyMatch, syntax.OpBeginLine, syntax.OpEndLine,
		syntax.OpBeginText, syntax.OpEndText, syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return 0, true
	case syntax.OpLiteral:
		if r.Flags&syntax.FoldCase != 0 {
			return capMulSpan(len(r.Rune), utf8.UTFMax), true
		}
		return len(string(r.Rune)), true
	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return utf8.UTFMax, true
	case syntax.OpCharClass:
		if len(r.Rune) > 0 && r.Rune[len(r.Rune)-1] < utf8.RuneSelf {
			return 1, true
		}
		return utf8.UTFMax, true
	case syntax.OpCapture, syntax.OpQuest:
		if len(r.Sub) == 0 {
			return 0, true
		}
		return maxMatchBytes(r.Sub[0])
	case syntax.OpStar, syntax.OpPlus:
		return 0, false
	case syntax.OpRepeat:
		if r.Max < 0 {
			return 0, false
		}
		if r.Max == 0 || len(r.Sub) == 0 {
			return 0, true
		}
		subBytes, ok := maxMatchBytes(r.Sub[0])
		if !ok {
			return 0, false
		}
		return capMulSpan(r.Max, subBytes), true
	case syntax.OpConcat:
		sum := 0
		for _, sub := range r.Sub {
			b, ok := maxMatchBytes(sub)
			if !ok {
				return 0, false
			}
			sum = capAddSpan(sum, b)
		}
		return sum, true
	case syntax.OpAlternate:
		best := 0
		for _, sub := range r.Sub {
			b, ok := maxMatchBytes(sub)
			if !ok {
				return 0, false
			}
			if b > best {
				best = b
			}
		}
		return best, true
	default:
		return 0, false
	}
}

func canMatchEmpty(r *syntax.Regexp) bool {
	if r == nil {
		return true
	}
	switch r.Op {
	case syntax.OpEmptyMatch, syntax.OpQuest, syntax.OpStar,
		syntax.OpBeginLine, syntax.OpEndLine, syntax.OpBeginText, syntax.OpEndText,
		syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return true
	case syntax.OpLiteral:
		return len(r.Rune) == 0
	case syntax.OpCharClass, syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return false
	case syntax.OpCapture, syntax.OpPlus:
		return len(r.Sub) == 0 || canMatchEmpty(r.Sub[0])
	case syntax.OpRepeat:
		return r.Min == 0 || len(r.Sub) == 0 || canMatchEmpty(r.Sub[0])
	case syntax.OpConcat:
		for _, sub := range r.Sub {
			if !canMatchEmpty(sub) {
				return false
			}
		}
		return true
	case syntax.OpAlternate:
		for _, sub := range r.Sub {
			if canMatchEmpty(sub) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

func unwrapCapture(r *syntax.Regexp) *syntax.Regexp {
	for r != nil && r.Op == syntax.OpCapture && len(r.Sub) == 1 {
		r = r.Sub[0]
	}
	return r
}

func isPureJSIdentAtom(r *syntax.Regexp) bool {
	r = unwrapCapture(r)
	if r == nil || r.Op != syntax.OpCharClass || len(r.Rune) == 0 || r.Rune[len(r.Rune)-1] >= utf8.RuneSelf {
		return false
	}
	for i := 0; i+1 < len(r.Rune); i += 2 {
		for c := r.Rune[i]; c <= r.Rune[i+1]; c++ {
			if !isJSIdentByte(byte(c)) {
				return false
			}
		}
	}
	return true
}

func flexibleTokenMargin(r *syntax.Regexp) (maxBytes int, unbounded bool, flexible bool) {
	r = unwrapCapture(r)
	if r == nil {
		return 0, false, false
	}
	switch r.Op {
	case syntax.OpLiteral, syntax.OpCharClass, syntax.OpAnyChar, syntax.OpAnyCharNotNL, syntax.OpEmptyMatch:
		return 0, false, false
	case syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
		if len(r.Sub) == 1 && isPureJSIdentAtom(r.Sub[0]) {
			return 0, false, false
		}
		m, ok := maxMatchBytes(r)
		if !ok {
			return 0, true, true
		}
		return m, false, true
	default:
		m, ok := maxMatchBytes(r)
		if !ok {
			return 0, true, true
		}
		return m, false, true
	}
}

func hasNonJSIdentUnboundedRepeat(r *syntax.Regexp) bool {
	if r == nil {
		return false
	}
	switch r.Op {
	case syntax.OpStar, syntax.OpPlus:
		if !(len(r.Sub) == 1 && isPureJSIdentAtom(r.Sub[0])) {
			return true
		}
	case syntax.OpRepeat:
		if r.Max < 0 && !(len(r.Sub) == 1 && isPureJSIdentAtom(r.Sub[0])) {
			return true
		}
	}
	for _, sub := range r.Sub {
		if hasNonJSIdentUnboundedRepeat(sub) {
			return true
		}
	}
	return false
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func findAnchorSubSpan(subs []*syntax.Regexp, lits []string) (startIdx int, endIdx int, ok bool) {
	if len(lits) == 1 {
		anchorBytes := []byte(lits[0])
		for i := 0; i < len(subs); {
			if subs[i].Op != syntax.OpLiteral || subs[i].Flags&syntax.FoldCase != 0 {
				i++
				continue
			}
			j := i
			var run []rune
			for j < len(subs) && subs[j].Op == syntax.OpLiteral && subs[j].Flags&syntax.FoldCase == 0 {
				run = append(run, subs[j].Rune...)
				j++
			}
			if bytes.Contains([]byte(string(run)), anchorBytes) {
				return i, j - 1, true
			}
			i = j
		}
	}
	for i, sub := range subs {
		if cands, subOK := extractRequiredLiterals(sub); subOK && stringSlicesEqual(cands, lits) {
			return i, i, true
		}
	}
	return 0, 0, false
}

func analyzeEdgeMargins(parsed *syntax.Regexp, lits []string, w *windowedRegexp) {
	root := unwrapCapture(parsed)
	if root == nil {
		return
	}
	if root.Op != syntax.OpConcat {
		m, unb, flex := flexibleTokenMargin(root)
		if !unb && hasNonJSIdentUnboundedRepeat(root) {
			flex, unb, m = true, true, 0
		}
		w.flexPrefix, w.prefixUnbounded, w.prefixMaxBytes = flex, unb, m
		w.flexSuffix, w.suffixUnbounded, w.suffixMaxBytes = flex, unb, m
		return
	}
	subs := root.Sub
	for i := 0; i < len(subs); i++ {
		m, unb, flex := flexibleTokenMargin(subs[i])
		if flex {
			w.flexPrefix = true
			if unb {
				w.prefixUnbounded = true
			} else {
				w.prefixMaxBytes = capAddSpan(w.prefixMaxBytes, m)
			}
		}
		if !canMatchEmpty(subs[i]) {
			break
		}
	}
	for i := len(subs) - 1; i >= 0; i-- {
		m, unb, flex := flexibleTokenMargin(subs[i])
		if flex {
			w.flexSuffix = true
			if unb {
				w.suffixUnbounded = true
			} else {
				w.suffixMaxBytes = capAddSpan(w.suffixMaxBytes, m)
			}
		}
		if !canMatchEmpty(subs[i]) {
			break
		}
	}

	anchorStart, anchorEnd, found := findAnchorSubSpan(subs, lits)
	if !found {
		if hasNonJSIdentUnboundedRepeat(root) {
			w.flexPrefix, w.prefixUnbounded = true, true
			w.flexSuffix, w.suffixUnbounded = true, true
		}
		return
	}
	for i := 0; i <= anchorStart; i++ {
		if i == anchorStart && unwrapCapture(subs[i]).Op == syntax.OpLiteral {
			break
		}
		if hasNonJSIdentUnboundedRepeat(subs[i]) {
			w.flexPrefix, w.prefixUnbounded = true, true
			break
		}
	}
	for i := len(subs) - 1; i >= anchorEnd; i-- {
		if i == anchorEnd && unwrapCapture(subs[i]).Op == syntax.OpLiteral {
			break
		}
		if hasNonJSIdentUnboundedRepeat(subs[i]) {
			w.flexSuffix, w.suffixUnbounded = true, true
			break
		}
	}
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

func touchesPhysicalBoundary(sp byteSpan, loc []int, bodyLen int) bool {
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

func (w *windowedRegexp) needsEdgeExpansion(sp byteSpan, loc []int, bodyLen int) bool {
	if len(loc) < 2 {
		return false
	}
	if sp.start > 0 && w.flexPrefix && (w.prefixUnbounded || loc[0] <= w.prefixMaxBytes) {
		return true
	}
	if sp.end < bodyLen {
		spLen := sp.end - sp.start
		if w.flexSuffix && (w.suffixUnbounded || spLen-loc[1] <= w.suffixMaxBytes) {
			return true
		}
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
		expanded := false
		loc := re.FindSubmatchIndex(body[sp.start:sp.end])
		if loc == nil && w.hasUnboundedRepeat && (sp.start > 0 || sp.end < len(body)) {
			sp = expandSpan(body, sp)
			expanded = true
			loc = re.FindSubmatchIndex(body[sp.start:sp.end])
		} else if loc != nil && !expanded && w.needsEdgeExpansion(sp, loc, len(body)) {
			sp = expandSpan(body, sp)
			expanded = true
			loc = re.FindSubmatchIndex(body[sp.start:sp.end])
		}
		if loc != nil {
			if touchesPhysicalBoundary(sp, loc, len(body)) {
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
			if touchesPhysicalBoundary(sp, loc, len(body)) {
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
			if sp.start < lastEnd {
				rem = -1
			}
		}
		expanded := false
		matches := re.FindAllIndex(body[sp.start:sp.end], rem)
		if len(matches) == 0 && w.hasUnboundedRepeat && (sp.start > 0 || sp.end < len(body)) {
			sp = expandSpan(body, sp)
			expanded = true
			matches = re.FindAllIndex(body[sp.start:sp.end], -1)
		} else if len(matches) > 0 && !expanded && (w.needsEdgeExpansion(sp, matches[0], len(body)) || w.needsEdgeExpansion(sp, matches[len(matches)-1], len(body))) {
			sp = expandSpan(body, sp)
			expanded = true
			matches = re.FindAllIndex(body[sp.start:sp.end], -1)
		}
		for _, m := range matches {
			if touchesPhysicalBoundary(sp, m, len(body)) {
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
