package patches

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func scrollbarDragPatch(t *testing.T) Patch {
	t.Helper()
	for _, p := range All() {
		if p.ID == "scrollbar-drag-follows-paging" {
			return p
		}
	}
	t.Fatal("patch is not registered")
	return Patch{}
}

func TestScrollbarDragPatchMatchesWrappedSource(t *testing.T) {
	p := scrollbarDragPatch(t)
	src := "ua<=0||(pa=z.current.startScrollPos+\n((h?pa.clientX:pa.clientY)-z.current.startCoord)/ua*H.current.maxScrollPos,h?la.scrollLeft=pa:la.scrollTop=pa,C.current=Date.now(),m||U())"

	out := p.FindRe.ReplaceAllString(src, p.Replace)

	want := "ua<=0||(window.__agyDragScrollbar?window.__agyDragScrollbar(z.current,la,h,h?pa.clientX:pa.clientY,ua,H.current.maxScrollPos):(pa=z.current.startScrollPos+((h?pa.clientX:pa.clientY)-z.current.startCoord)/ua*H.current.maxScrollPos,h?la.scrollLeft=pa:la.scrollTop=pa),C.current=Date.now(),m||U())"
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

func TestScrollbarDragPatchMatchesALineBreakAtAnyToken(t *testing.T) {
	p := scrollbarDragPatch(t)
	src := "pa=\nz.current.startScrollPos+((h?pa.clientX:\npa.clientY)-z.current.startCoord)/ua*H.current.maxScrollPos,\nh?la.scrollLeft=\npa:la.scrollTop=\npa,"
	if !p.FindRe.MatchString(src) {
		t.Error("anchor did not match")
	}
}

// Drives the helper the way the bundle does, with a position that clamps and snaps like a real element.
func TestDragScrollbarSurvivesPaging(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	script := scrollInputTracker
	script = script[strings.Index(script, ">")+1:]
	script = script[:strings.LastIndex(script, "</script>")]
	harness := `
const assert = require("assert");
const fs = require("fs");
const noop = () => {};
global.window = global;
global.addEventListener = noop;
global.document = {};
global.MutationObserver = class { observe() {} };
new Function(fs.readFileSync(process.argv[1], "utf8"))();

const element = max => {
  let top = 0;
  return {
    max,
    get scrollTop() { return top; },
    set scrollTop(v) { top = Math.round(Math.min(this.max, Math.max(0, v))); },
  };
};
const THUMB = 800;

// With nothing else moving, it is the bundle's own mapping: exact, and clamped past the end.
{
  const el = element(8000);
  const drag = { startScrollPos: 0, startCoord: 100 };
  for (let y = 101; y <= 500; y++) window.__agyDragScrollbar(drag, el, false, y, THUMB, 8000);
  assert.strictEqual(el.scrollTop, 4000);
  window.__agyDragScrollbar(drag, el, false, 5000, THUMB, 8000);
  assert.strictEqual(el.scrollTop, 8000);
  window.__agyDragScrollbar(drag, el, false, 500, THUMB, 8000);
  assert.strictEqual(el.scrollTop, 4000, "coming back from past the end follows the pointer");
}

// A short overflow moves less than a pixel per pointer pixel, which rounding must not swallow.
{
  const el = element(80);
  const drag = { startScrollPos: 0, startCoord: 0 };
  for (let y = 1; y <= 400; y++) window.__agyDragScrollbar(drag, el, false, y, THUMB, 80);
  assert.strictEqual(el.scrollTop, 40);
}

// The window is cut at the top: the position is corrected and the range shrinks, then the pointer moves one pixel.
{
  const el = element(90000);
  const drag = { startScrollPos: 0, startCoord: 100 };
  window.__agyDragScrollbar(drag, el, false, 200, THUMB, 90000);
  assert.strictEqual(el.scrollTop, 11250);
  el.max = 83000;
  el.scrollTop -= 7000;
  assert.strictEqual(el.scrollTop, 4250);
  window.__agyDragScrollbar(drag, el, false, 201, THUMB, 83000);
  assert.ok(Math.abs(el.scrollTop - 4250) < 110, "stayed near the reader, at " + el.scrollTop);
}

// The window is cut after the press and before the first move, which has no earlier move to compare with.
{
  const el = element(90000);
  el.scrollTop = 50000;
  const drag = { startScrollPos: 50000, startCoord: 100 };
  el.max = 83000;
  el.scrollTop -= 7000;
  window.__agyDragScrollbar(drag, el, false, 101, THUMB, 83000);
  assert.ok(Math.abs(el.scrollTop - 43000) < 110, "stayed near the reader, at " + el.scrollTop);
}

// A small correction is kept as well as a large one.
{
  const el = element(90000);
  const drag = { startScrollPos: 0, startCoord: 100 };
  window.__agyDragScrollbar(drag, el, false, 200, THUMB, 90000);
  el.scrollTop -= 50;
  const corrected = el.scrollTop;
  window.__agyDragScrollbar(drag, el, false, 200, THUMB, 90000);
  assert.strictEqual(el.scrollTop, corrected);
}

// A horizontal drag works on the other axis and leaves this one alone.
{
  let left = 0;
  const el = {
    max: 90000,
    scrollTop: 77,
    get scrollLeft() { return left; },
    set scrollLeft(v) { left = Math.round(Math.min(this.max, Math.max(0, v))); },
  };
  const drag = { startScrollPos: 0, startCoord: 100 };
  window.__agyDragScrollbar(drag, el, true, 200, THUMB, 90000);
  assert.strictEqual(el.scrollLeft, 11250);
  el.max = 83000;
  el.scrollLeft -= 7000;
  window.__agyDragScrollbar(drag, el, true, 201, THUMB, 83000);
  assert.ok(Math.abs(el.scrollLeft - 4250) < 110, "stayed near the reader, at " + el.scrollLeft);
  assert.strictEqual(el.scrollTop, 77);

  // A short overflow keeps its sub-pixel mapping on this axis too.
  left = 0;
  el.max = 80;
  const short = { startScrollPos: 0, startCoord: 0 };
  for (let x = 1; x <= 400; x++) window.__agyDragScrollbar(short, el, true, x, THUMB, 80);
  assert.strictEqual(el.scrollLeft, 40);
}

// Rows added above do the same in the other direction, and so does a range change with no position change.
{
  const el = element(20000);
  const drag = { startScrollPos: 10000, startCoord: 400 };
  window.__agyDragScrollbar(drag, el, false, 399, THUMB, 20000);
  el.max = 33000;
  el.scrollTop += 13000;
  const corrected = el.scrollTop;
  window.__agyDragScrollbar(drag, el, false, 398, THUMB, 33000);
  assert.ok(Math.abs(el.scrollTop - corrected) < 45, "kept the correction, at " + el.scrollTop);

  const grown = el.scrollTop;
  el.max = 90000;
  window.__agyDragScrollbar(drag, el, false, 397, THUMB, 90000);
  assert.ok(Math.abs(el.scrollTop - grown) < 115, "range change alone does not jump, at " + el.scrollTop);
}
process.exit(0);
`
	scriptPath := filepath.Join(t.TempDir(), "tracker.js")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "-e", harness, "--", scriptPath).CombinedOutput(); err != nil {
		t.Fatalf("scrollbar drag harness failed: %v\n%s", err, out)
	}
}
