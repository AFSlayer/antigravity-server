package patches

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Drives the tracker with fake events, a clock and a conversation whose height the test controls.
func TestSpontaneousScrollNeedsACollapseAndNoInput(t *testing.T) {
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
const listeners = {};
let now = 100000;
let findBarOpen = false;
let observer;
let frames = [];
global.window = global;
global.addEventListener = (name, fn) => { (listeners[name] = listeners[name] || []).push(fn); };
global.document = { querySelector: () => (findBarOpen ? {} : null) };
global.MutationObserver = class { constructor(fn) { observer = fn; } observe() {} };
global.requestAnimationFrame = fn => { frames.push(fn); };
global.setTimeout = fn => { frames.push(fn); };
Object.defineProperty(global, "performance", { value: { now: () => now } });
new Function(fs.readFileSync(process.argv[1], "utf8"))();

const fire = (name, event) => (listeners[name] || []).forEach(fn => fn(event));
const frame = () => { while (frames.length > 0) { const due = frames; frames = []; due.forEach(fn => fn()); } };
const viewport = () => {
  const el = {
    scrollTop: 9000,
    scrollHeight: 10000,
    matches: () => true,
    closest: () => el,
  };
  fire("scroll", { target: el });
  return el;
};
const removeRows = el => observer([{ target: el, removedNodes: [{}] }]);
const collapse = el => { removeRows(el); el.scrollHeight = 1000; el.scrollTop = 300; frame(); el.scrollHeight = 10000; };
const prompt = { isContentEditable: true, closest: () => null };
const body = { closest: () => null };
const settle = () => { now += 2000; };

// A row swapped for one of the same height says nothing.
{
  settle();
  const el = viewport();
  removeRows(el);
  frame();
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
}

// A scroll later on is not blamed on a removal that a frame has already cleared.
{
  settle();
  const el = viewport();
  removeRows(el);
  frame();
  el.scrollTop = 300;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
}

// The observer leaves the layout alone: reading it there could clamp the position in a gap between two commits.
{
  settle();
  let reads = 0;
  const el = viewport();
  Object.defineProperty(el, "scrollHeight", { get() { reads++; return 10000; } });
  Object.defineProperty(el, "scrollTop", { get() { reads++; return 9000; } });
  removeRows(el);
  removeRows(el);
  assert.strictEqual(reads, 0);
  assert.strictEqual(frames.length, 1, "one frame for any number of commits");
  frame();
}

// A collapse with no input is spontaneous, for that conversation and only for a while.
{
  settle();
  const el = viewport();
  collapse(el);
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true);
  assert.strictEqual(window.__agyIsSpontaneousScroll(viewport()), false);
  now += 1999;
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true);
  now += 1;
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
}

// The conversation getting shorter is enough, and so is the position moving, by more than rounding.
{
  settle();
  let el = viewport();
  removeRows(el);
  el.scrollHeight = 9000;
  frame();
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true, "shorter");
  settle();
  el = viewport();
  removeRows(el);
  el.scrollTop = 8500;
  frame();
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true, "moved");
  settle();
  el = viewport();
  removeRows(el);
  el.scrollTop = 8999.5;
  frame();
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false, "rounding");
}

// Removals outside a conversation are not looked at.
{
  settle();
  const el = viewport();
  observer([{ target: { closest: () => null }, removedNodes: [{}] }, { target: {}, removedNodes: [{}] }]);
  assert.strictEqual(frames.length, 0);
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
}

// Input on the conversation or its scrollbars counts, for a second, and the same input elsewhere does not.
for (const part of [
  '[data-testid="autoscroll-viewport"]',
  "[data-overlay-scrollbar-track]",
  '[data-testid="overlay-scrollbar"]',
  '[data-testid="vertical-scrollbar"]',
  '[data-testid="vertical-scrollbar-thumb"]',
]) {
  const target = { closest: selector => (selector.split(",").includes(part) ? {} : null) };
  for (const name of ["wheel", "touchstart", "touchmove", "pointerdown"]) {
    settle();
    let el = viewport();
    fire(name, { target });
    fire("pointerup", { target: body });
    now += 999;
    collapse(el);
    assert.strictEqual(window.__agyIsSpontaneousScroll(el), false, name + " on " + part);
    now += 1;
    assert.strictEqual(window.__agyIsSpontaneousScroll(el), true, name + " a second later");
    settle();
    el = viewport();
    fire(name, { target: body });
    fire("pointerup", { target: body });
    collapse(el);
    assert.strictEqual(window.__agyIsSpontaneousScroll(el), true, name + " elsewhere");
  }
}

// Moving the pointer counts only while pressing.
{
  settle();
  let el = viewport();
  fire("pointermove", { target: body });
  collapse(el);
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true);
  settle();
  el = viewport();
  fire("pointerdown", { target: { closest: () => el } });
  settle();
  fire("pointermove", { target: body });
  fire("pointerup", { target: body });
  collapse(el);
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
}

// The position moving is enough: the scroll event can arrive before the frame, with the content already back.
{
  settle();
  const el = viewport();
  removeRows(el);
  el.scrollTop = 300;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true);
  frame();
}

// A hidden tab runs no frames, so the comparison waits for the reader to return, however long that takes.
{
  settle();
  const el = viewport();
  removeRows(el);
  el.scrollTop = 300;
  now += 600000;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true);
  frame();
}

// A collapse in one pane says nothing about another, and does not hide the other's.
{
  settle();
  const a = viewport();
  const b = viewport();
  collapse(a);
  assert.strictEqual(window.__agyIsSpontaneousScroll(b), false);
  collapse(b);
  assert.strictEqual(window.__agyIsSpontaneousScroll(a), true);
  assert.strictEqual(window.__agyIsSpontaneousScroll(b), true);
}

// The conversation takes these keys with the prompt focused, so they count there too.
for (const key of ["PageUp", "PageDown", "Home", "End"]) {
  settle();
  const el = viewport();
  fire("keydown", { key, target: prompt });
  collapse(el);
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false, key);
}

// These only type or move the caret in the prompt, and scroll anywhere else.
for (const key of ["ArrowUp", "ArrowDown", " "]) {
  settle();
  let el = viewport();
  fire("keydown", { key, target: prompt });
  collapse(el);
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true, "typing " + key);
  settle();
  el = viewport();
  fire("keydown", { key, target: body });
  collapse(el);
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false, "scrolling " + key);
}

// Any other key is typing.
{
  settle();
  const el = viewport();
  fire("keydown", { key: "a", target: prompt });
  collapse(el);
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true);
}

// The find bar scrolls to its matches.
{
  settle();
  const el = viewport();
  findBarOpen = true;
  collapse(el);
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
  findBarOpen = false;
}

// Focus moving into the conversation scrolls it, and elsewhere it does not.
{
  settle();
  let el = viewport();
  fire("focusin", { target: { closest: () => el } });
  collapse(el);
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
  settle();
  el = viewport();
  fire("focusin", { target: prompt });
  collapse(el);
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true);
}

// Input on the conversation counts, and a held press counts until something ends it.
for (const end of ["pointerup", "pointercancel", "contextmenu", "dragstart", "blur"]) {
  settle();
  const el = viewport();
  fire("pointerdown", { target: { closest: () => el } });
  settle();
  fire("pointermove", { target: body });
  settle();
  collapse(el);
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false, "held, before " + end);
  fire(end, { target: end === "blur" ? window : body });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true, "after " + end);
}

// An element losing focus is not the window losing it, and pressing the conversation often causes exactly that.
{
  settle();
  const el = viewport();
  fire("pointerdown", { target: { closest: () => el } });
  fire("blur", { target: prompt });
  settle();
  collapse(el);
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
  fire("pointerup", { target: body });
}
process.exit(0);
`
	scriptPath := filepath.Join(t.TempDir(), "tracker.js")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "-e", harness, "--", scriptPath).CombinedOutput(); err != nil {
		t.Fatalf("scroll input harness failed: %v\n%s", err, out)
	}
}
