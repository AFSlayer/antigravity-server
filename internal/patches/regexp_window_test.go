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
