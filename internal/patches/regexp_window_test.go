package patches

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestWindowedRegexpFallbackOverMaxAnchorOccurrences(t *testing.T) {
	re := regexp.MustCompile(`([a-zA-Z0-9_$]+)\.frequentToken\([0-9]+\)`)
	w := getWindowedRegexp(re)
	if len(w.anchors) == 0 {
		t.Fatal("expected literal anchor to be extracted")
	}

	var buf bytes.Buffer
	buf.Write(bytes.Repeat([]byte("function _pad(){return 1;}\n"), 2000))
	for i := 0; i < maxAnchorOccurrences+5; i++ {
		buf.WriteString(`noop.frequentToken(0);` + "\n")
	}
	buf.WriteString(`realCaller.frequentToken(42);` + "\n")
	buf.Write(bytes.Repeat([]byte("function _tail(){return 2;}\n"), 2000))
	body := buf.Bytes()

	spans, fallback := w.candidateSpans(body)
	if !fallback || spans != nil {
		t.Fatalf("expected fallback=true when anchor occurrences exceed %d, got fallback=%v spans=%d", maxAnchorOccurrences, fallback, len(spans))
	}

	got := regexpFindAllIndex(re, body, -1)
	want := re.FindAllIndex(body, -1)
	if len(got) != len(want) {
		t.Fatalf("fallback FindAllIndex count = %d, want %d", len(got), len(want))
	}
}

func TestWindowedRegexpMergesAdjacentSpansAndDeduplicatesFindAll(t *testing.T) {
	re := regexp.MustCompile(`([a-zA-Z0-9_$]+)\.mergedAnchorCall\(([0-9]+)\)`)
	w := getWindowedRegexp(re)

	var buf bytes.Buffer
	buf.Write(bytes.Repeat([]byte("var _p=0;\n"), 7000)) // 70 KB prefix
	buf.WriteString("firstObj.mergedAnchorCall(1);\n")
	buf.Write(bytes.Repeat([]byte("var _m=1;\n"), 100)) // 1 KB gap (< 2*regexpWindowRadius) -> must merge into 1 span
	buf.WriteString("secondObj.mergedAnchorCall(2);\n")
	buf.Write(bytes.Repeat([]byte("var _s=2;\n"), 6000)) // 60 KB gap (> 2*regexpWindowRadius) -> separate 2nd span
	buf.WriteString("thirdObj.mergedAnchorCall(3);\n")
	buf.Write(bytes.Repeat([]byte("var _t=3;\n"), 7000)) // 70 KB suffix
	body := buf.Bytes()

	spans, fallback := w.candidateSpans(body)
	if fallback {
		t.Fatal("expected windowed scan without fallback")
	}
	if len(spans) != 2 {
		t.Fatalf("expected 2 merged spans (2 close + 1 distant), got %d: %+v", len(spans), spans)
	}

	gotAll := regexpFindAllIndex(re, body, -1)
	wantAll := re.FindAllIndex(body, -1)
	if len(gotAll) != 3 || len(gotAll) != len(wantAll) {
		t.Fatalf("FindAllIndex count = %d, want %d", len(gotAll), len(wantAll))
	}
	for i := range wantAll {
		if gotAll[i][0] != wantAll[i][0] || gotAll[i][1] != wantAll[i][1] {
			t.Fatalf("match[%d] = %v, want %v", i, gotAll[i], wantAll[i])
		}
	}

	limited := regexpFindAllIndex(re, body, 2)
	if len(limited) != 2 || limited[1][0] != wantAll[1][0] {
		t.Fatalf("FindAllIndex(n=2) = %v, want first 2 of %v", limited, wantAll)
	}
}

func TestWindowedRegexpAlternateMissingLiteralFallsBackSafely(t *testing.T) {
	// Second branch has no 4+ byte literal, so extracting only "alphaLiteralBranch" would miss numeric matches.
	re := regexp.MustCompile(`alphaLiteralBranch|[0-9]{6}`)
	w := getWindowedRegexp(re)
	if len(w.anchors) != 0 {
		t.Fatalf("expected no anchors when an OpAlternate branch lacks a required literal, got %q", w.anchors)
	}

	var buf bytes.Buffer
	buf.Write(bytes.Repeat([]byte("var x=true;\n"), 4000))
	buf.WriteString("var code=987654;\n")
	buf.Write(bytes.Repeat([]byte("var y=false;\n"), 4000))
	body := buf.Bytes()

	if !regexpMatch(re, body) {
		t.Fatal("expected unanchored alternate branch to match via full-scan fallback")
	}
}

func TestWindowedRegexpNegatedClassAndLargeRepeatFallbackAcrossWideGap(t *testing.T) {
	negatedRe := regexp.MustCompile(`openMarker:"([^"]*)"\s*,\s*closeMarker:"targetEnd"`)
	greedyEdgeRe := regexp.MustCompile(`edgeAnchor:"([^"]*)"`)
	chainedRepeatRe := regexp.MustCompile(`chainStart` + strings.Repeat(`[\s\S]{0,1000}?`, 10) + `chainEndLiteral`)

	if w := getWindowedRegexp(negatedRe); !w.hasUnboundedRepeat {
		t.Fatal("expected hasUnboundedRepeat=true for negated character class repetition")
	}
	if w := getWindowedRegexp(chainedRepeatRe); len(w.anchors) != 0 {
		t.Fatalf("expected chained repeats exceeding radius/2 to disable fixed window, got %q", w.anchors)
	}

	var buf bytes.Buffer
	buf.Write(bytes.Repeat([]byte("function padA(){}\n"), 3000))
	buf.WriteString(`openMarker:"`)
	buf.Write(bytes.Repeat([]byte("x"), 20000)) // > regexpWindowRadius (16 KB)
	buf.WriteString(`",closeMarker:"targetEnd";`)
	buf.WriteString(`edgeAnchor:"`)
	buf.Write(bytes.Repeat([]byte("z"), 20000)) // extends past right window boundary
	buf.WriteString(`";`)
	buf.WriteString(`chainStart`)
	buf.Write(bytes.Repeat([]byte("가"), 8000)) // 8,000 runes = 24 KB (> 16 KB)
	buf.WriteString(`chainEndLiteral;`)
	buf.Write(bytes.Repeat([]byte("function padB(){}\n"), 3000))
	body := buf.Bytes()

	if loc := regexpFindSubmatchIndex(negatedRe, body); loc == nil {
		t.Fatal("negated character class spanning >16 KB failed to match")
	} else if want := negatedRe.FindSubmatchIndex(body); loc[0] != want[0] || loc[1] != want[1] {
		t.Fatalf("negatedRe loc = %v, want %v", loc, want)
	}

	if loc := regexpFindSubmatchIndex(greedyEdgeRe, body); loc == nil {
		t.Fatal("greedy negated character class crossing right window boundary failed to match")
	} else if want := greedyEdgeRe.FindSubmatchIndex(body); loc[0] != want[0] || loc[1] != want[1] {
		t.Fatalf("greedyEdgeRe loc = %v, want %v", loc, want)
	}

	if loc := regexpFindSubmatchIndex(chainedRepeatRe, body); loc == nil {
		t.Fatal("chained OpRepeat spanning 24 KB failed to match")
	}
}

func TestWindowedRegexpAlignsUTF8CodepointBoundaries(t *testing.T) {
	re := regexp.MustCompile(`([a-zA-Z0-9_$]+)\.utf8AnchorTarget\(\)`)
	w := getWindowedRegexp(re)
	if len(w.anchors) == 0 {
		t.Fatal("expected anchor for utf8 boundary test")
	}

	// Repeat 3-byte ("가") and 4-byte ("🚀") UTF-8 sequences so raw +/-16 KB offsets land mid-rune.
	multibyteChunk := []byte("/*가나다라마바사🚀🎉🔥*/")
	repeats := (regexpWindowRadius*3)/len(multibyteChunk) + 64

	for shift := 0; shift < 7; shift++ {
		var buf bytes.Buffer
		buf.Write(bytes.Repeat([]byte("a"), shift))
		buf.Write(bytes.Repeat(multibyteChunk, repeats))
		buf.WriteString(" boundaryCaller.utf8AnchorTarget() ")
		buf.Write(bytes.Repeat(multibyteChunk, repeats))
		body := buf.Bytes()

		if !utf8.Valid(body) {
			t.Fatal("test fixture must be valid UTF-8")
		}
		spans, fallback := w.candidateSpans(body)
		if fallback || len(spans) != 1 {
			t.Fatalf("shift=%d: expected 1 candidate span, fallback=%v spans=%d", shift, fallback, len(spans))
		}
		slice := body[spans[0].start:spans[0].end]
		if !utf8.Valid(slice) {
			t.Fatalf("shift=%d: candidate window [%d:%d] sliced through a multi-byte UTF-8 rune", shift, spans[0].start, spans[0].end)
		}
		if loc := regexpFindSubmatchIndex(re, body); loc == nil {
			t.Fatalf("shift=%d: expected match inside UTF-8 aligned window", shift)
		}
	}
}

func TestWindowedRegexpNarrowClassBoundaryAndMultiSpanOrder(t *testing.T) {
	// 1. Narrow character class (<= 256 runes) crossing the 16 KB window boundary must not be truncated.
	narrowEdgeRe := regexp.MustCompile(`narrowBase64:"([A-Za-z0-9+/=]+)"`)
	// 2. Multi-span where 1st anchor requires >16 KB expansion and 2nd anchor fits in 16 KB must return 1st match.
	multiSpanRe := regexp.MustCompile(`multiSpanKey:"([A-Za-z0-9_]+)",endTag:true`)

	var buf bytes.Buffer
	buf.Write(bytes.Repeat([]byte("function pre(){}\n"), 3000))
	buf.WriteString(`narrowBase64:"`)
	buf.Write(bytes.Repeat([]byte("A"), 24000)) // > 16 KB
	buf.WriteString(`";`)

	buf.WriteString(`multiSpanKey:"`)
	buf.Write(bytes.Repeat([]byte("B"), 28000)) // 1st span > 16 KB, <= 128 KB
	buf.WriteString(`",endTag:true;`)
	buf.Write(bytes.Repeat([]byte("function mid(){}\n"), 4000)) // separate 2nd span
	buf.WriteString(`multiSpanKey:"shortSecond",endTag:true;`)
	buf.Write(bytes.Repeat([]byte("function post(){}\n"), 3000))
	body := buf.Bytes()

	gotNarrow := regexpFindSubmatchIndex(narrowEdgeRe, body)
	wantNarrow := narrowEdgeRe.FindSubmatchIndex(body)
	if len(gotNarrow) != len(wantNarrow) || gotNarrow[0] != wantNarrow[0] || gotNarrow[1] != wantNarrow[1] {
		t.Fatalf("narrowEdgeRe = %v, want %v", gotNarrow, wantNarrow)
	}

	gotMulti := regexpFindSubmatchIndex(multiSpanRe, body)
	wantMulti := multiSpanRe.FindSubmatchIndex(body)
	if len(gotMulti) != len(wantMulti) || gotMulti[0] != wantMulti[0] || gotMulti[1] != wantMulti[1] {
		t.Fatalf("multiSpanRe returned wrong span %v, want first match %v", gotMulti, wantMulti)
	}

	gotAll := regexpFindAllIndex(multiSpanRe, body, -1)
	wantAll := multiSpanRe.FindAllIndex(body, -1)
	if len(gotAll) != 2 || len(gotAll) != len(wantAll) || gotAll[0][0] != wantAll[0][0] || gotAll[1][0] != wantAll[1][0] {
		t.Fatalf("multiSpanRe FindAllIndex = %v, want %v", gotAll, wantAll)
	}
}

func TestWindowedRegexpEdgeCasesExpandedOverlapOptionalSidesAndNestedRepeats(t *testing.T) {
	// 1. FindAllIndex(n=2) where span 2 expands backward across span 1's already-collected match:
	// overlapping matches from span 1 must not consume rem and drop span 2's match.
	overlapRe := regexp.MustCompile(`overlapAnchor:"([^"]*)",done:1`)
	var overlapBuf bytes.Buffer
	overlapBuf.Write(bytes.Repeat([]byte("var p=0;\n"), 6000)) // ~54 KB prefix
	overlapBuf.WriteString(`overlapAnchor:"firstShort",done:1;`)
	overlapBuf.Write(bytes.Repeat([]byte("var g=1;\n"), 5000)) // ~45 KB gap (>32 KB so disjoint 16 KB spans, <128 KB so expanded span 2 overlaps span 1!)
	overlapBuf.WriteString(`overlapAnchor:"`)
	overlapBuf.Write(bytes.Repeat([]byte("X"), 22000)) // >16 KB so span 2 misses initial window and calls expandSpan
	overlapBuf.WriteString(`",done:1;`)
	overlapBuf.Write(bytes.Repeat([]byte("var t=2;\n"), 6000))
	overlapBody := overlapBuf.Bytes()

	gotLimited := regexpFindAllIndex(overlapRe, overlapBody, 2)
	wantLimited := overlapRe.FindAllIndex(overlapBody, 2)
	if len(gotLimited) != 2 || len(wantLimited) != 2 || gotLimited[1][0] != wantLimited[1][0] || gotLimited[1][1] != wantLimited[1][1] {
		t.Fatalf("FindAllIndex(n=2) across expanded overlapping span = %v, want %v", gotLimited, wantLimited)
	}

	// 2. Nested narrow-class OpRepeat exceeding radius/2 disables fixed window and matches wide span.
	nestedNarrowRe := regexp.MustCompile(`nestedAnchor` + strings.Repeat(`(?:a|[a-z]{0,300}){0,3}`, 10) + `nestedEnd`)
	if w := getWindowedRegexp(nestedNarrowRe); len(w.anchors) != 0 {
		t.Fatalf("expected nested narrow OpRepeat > radius/2 to disable fixed window, got anchors %q", w.anchors)
	}

	// 3. Multibyte bounded OpRepeat (5,000 runes < 8,192, but 15 KB..20 KB UTF-8 > 16 KB) must still match.
	multibyteBoundedRe := regexp.MustCompile(`mbStart` + strings.Repeat(`[\s\S]{0,1000}?`, 5) + `mbFinish`)
	var mbBuf bytes.Buffer
	mbBuf.Write(bytes.Repeat([]byte("function pad(){}\n"), 3000))
	mbBuf.WriteString("mbStart")
	mbBuf.Write(bytes.Repeat([]byte("🚀"), 4500)) // 4,500 runes = 18,000 bytes (> 16 KB)
	mbBuf.WriteString("mbFinish")
	mbBuf.Write(bytes.Repeat([]byte("function end(){}\n"), 3000))
	mbBody := mbBuf.Bytes()
	if got := regexpFindSubmatchIndex(multibyteBoundedRe, mbBody); got == nil {
		t.Fatal("multibyte bounded OpRepeat exceeding 16 KB byte radius failed to match")
	}

	// 4. Optional prefix and suffix crossing the 16 KB window boundary must not be truncated.
	optSidesRe := regexp.MustCompile(`(optHead:[a-z0-9 ]+;)?coreAnchorToken(;optTail:[a-z0-9 ]+)?`)
	var optBuf bytes.Buffer
	optBuf.Write(bytes.Repeat([]byte("var a=1;\n"), 4000))
	optBuf.WriteString("optHead:")
	optBuf.Write(bytes.Repeat([]byte("ab "), 6500)) // ~19.5 KB (> 16 KB before coreAnchorToken)
	optBuf.WriteString(";coreAnchorToken;optTail:")
	optBuf.Write(bytes.Repeat([]byte("cd "), 6500)) // ~19.5 KB (> 16 KB after coreAnchorToken)
	optBuf.WriteString(";")
	optBuf.Write(bytes.Repeat([]byte("var b=2;\n"), 4000))
	optBody := optBuf.Bytes()

	gotOpt := regexpFindSubmatchIndex(optSidesRe, optBody)
	wantOpt := optSidesRe.FindSubmatchIndex(optBody)
	if len(gotOpt) != len(wantOpt) || gotOpt[0] != wantOpt[0] || gotOpt[1] != wantOpt[1] {
		t.Fatalf("optional prefix/suffix crossing 16 KB window was truncated: got %v, want %v", gotOpt, wantOpt)
	}

	// 5. Greedy match touching the expanded 128 KB window boundary falls back to full buffer cleanly.
	ultraWideRe := regexp.MustCompile(`ultraOpen:"([^"]*)"`)
	var ultraBuf bytes.Buffer
	ultraBuf.Write(bytes.Repeat([]byte("var u=0;\n"), 4000))
	ultraBuf.WriteString(`ultraOpen:"`)
	ultraBuf.Write(bytes.Repeat([]byte("W"), 150000)) // 150 KB (> 128 KB expanded radius)
	ultraBuf.WriteString(`";`)
	ultraBuf.Write(bytes.Repeat([]byte("var v=1;\n"), 4000))
	ultraBody := ultraBuf.Bytes()

	gotUltra := regexpFindSubmatchIndex(ultraWideRe, ultraBody)
	wantUltra := ultraWideRe.FindSubmatchIndex(ultraBody)
	if len(gotUltra) != len(wantUltra) || gotUltra[0] != wantUltra[0] || gotUltra[1] != wantUltra[1] {
		t.Fatalf("regexpFindSubmatchIndex on >128 KB greedy span = %v, want %v", gotUltra, wantUltra)
	}

	// 6. Short leading/trailing literal (<4 bytes) paired with an unbounded repeat before/after the anchor
	// must expand past an inner short match to find the true outer leftmost/longest match.
	shortLeadRe := regexp.MustCompile(`if\([^x]*UNIQUE_ANCHOR_LEAD[^y]*\)`)
	var leadBuf bytes.Buffer
	leadBuf.Write(bytes.Repeat([]byte("var x=0;\n"), 4000))
	leadBuf.WriteString("if(outerStart_")
	leadBuf.Write(bytes.Repeat([]byte("a"), 22000)) // >16 KB before anchor, no 'x'
	leadBuf.WriteString("if(innerShort_UNIQUE_ANCHOR_LEAD_innerEnd)")
	leadBuf.Write(bytes.Repeat([]byte("b"), 22000)) // >16 KB after anchor, no 'y'
	leadBuf.WriteString("_outerEnd);y=1;")
	leadBuf.Write(bytes.Repeat([]byte("var y=1;\n"), 4000))
	leadBody := leadBuf.Bytes()

	gotLead := regexpFindSubmatchIndex(shortLeadRe, leadBody)
	wantLead := shortLeadRe.FindSubmatchIndex(leadBody)
	if len(gotLead) != len(wantLead) || gotLead[0] != wantLead[0] || gotLead[1] != wantLead[1] {
		t.Fatalf("short literal edge with unbounded repeat = %v, want %v", gotLead, wantLead)
	}
}
