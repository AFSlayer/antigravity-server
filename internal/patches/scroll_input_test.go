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
global.document = { querySelector: selector => (findBarOpen && selector === 'input[aria-label="Find"]' ? {} : null) };
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
const element = (elements, text, media = [], tagName = "DIV") => ({
  nodeType: 1,
  tagName,
  textContent: "x".repeat(text),
  matches: selector => selector.split(",").includes(tagName.toLowerCase()),
  querySelectorAll: selector => media.filter(m => selector.split(",").includes(m.selector || m.tagName.toLowerCase())),
  getElementsByTagName: () => ({ length: elements }),
});
const image = (naturalHeight, complete = true) => ({ tagName: "IMG", naturalHeight, complete });
// The sizes seen in the app: a status icon while a reply streams, and rows when the window changes.
const ICON = element(2, 7);
const ROWS = element(143, 14382);
const remove = (el, ...nodes) => observer([{ target: el, removedNodes: nodes }]);
const collapse = el => { remove(el, ROWS); el.scrollHeight = 1000; el.scrollTop = 300; frame(); el.scrollHeight = 10000; };
const prompt = { isContentEditable: true, closest: () => null };
const body = { closest: () => null };
const settle = () => { now += 2000; };

// A row swapped for one of the same height says nothing, even with rows that size.
{
  settle();
  const el = viewport();
  remove(el, ROWS);
  frame();
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
}

// A scroll later on is not blamed on a removal that a frame has already cleared.
{
  settle();
  const el = viewport();
  remove(el, ROWS);
  frame();
  el.scrollTop = 300;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
}

// A scroll with no input in the same frame as a small swap: the swap cannot explain the fall.
{
  settle();
  const el = viewport();
  remove(el, ICON);
  el.scrollTop -= 800;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
  frame();
}

// Nor can a small shrink explain a large fall in the same frame.
{
  settle();
  const el = viewport();
  remove(el, ICON);
  el.scrollHeight -= 30;
  el.scrollTop -= 800;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
  frame();
}

// Small removals add up, whatever kind of node they are.
{
  settle();
  let el = viewport();
  remove(el, element(10, 0), element(10, 0));
  el.scrollTop -= 1000;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true, "elements add up");
  frame();
  settle();
  el = viewport();
  remove(el, element(10, 0));
  remove(el, element(10, 0));
  el.scrollTop -= 1000;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true, "commits add up");
  frame();
  settle();
  el = viewport();
  remove(el, { nodeType: 3, textContent: "x".repeat(800) });
  el.scrollTop -= 1500;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true, "text counts");
  frame();
  settle();
  el = viewport();
  remove(el, { nodeType: 8, textContent: "x".repeat(5000) });
  el.scrollTop -= 1500;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false, "a comment has no height");
  frame();
}

// What the guess comes to, using the largest removal seen during a reply. A fall within it is still put back, which is
// accepted because it takes a scroll with no input in the same frame.
for (const [fall, expected] of [[630, true], [631, false]]) {
  settle();
  const el = viewport();
  remove(el, element(11, 27));
  el.scrollTop -= fall;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), expected, "fall of " + fall);
  frame();
}

// A shrink explains a fall of the same size, give or take a pixel of rounding.
for (const [fall, expected] of [[1001, true], [1002, false]]) {
  settle();
  const el = viewport();
  remove(el, ICON);
  el.scrollHeight -= 1000;
  el.scrollTop -= fall;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), expected, "fall of " + fall);
  frame();
}

// Media reports its own size once removed, scaled up in case CSS stretched it. A small image explains a small fall only.
for (const [what, node, guess] of [
  ["image inside", element(2, 0, [image(16)]), 3 * 48 + 16 * 4],
  ["image removed directly", Object.assign(element(0, 0, [], "IMG"), { naturalHeight: 16 }), 48 + 16 * 4],
  ["two images", element(2, 0, [image(16), image(100)]), 3 * 48 + 116 * 4],
  ["canvas", element(2, 0, [{ tagName: "CANVAS", height: 400 }]), 3 * 48 + 400 * 4],
  ["canvas removed directly", Object.assign(element(0, 0, [], "CANVAS"), { height: 400 }), 48 + 400 * 4],
  ["video", element(2, 0, [{ tagName: "VIDEO", videoHeight: 720 }]), 3 * 48 + 720 * 4],
]) {
  for (const [fall, expected] of [[guess, true], [guess + 1, false]]) {
    settle();
    const el = viewport();
    remove(el, node);
    el.scrollTop -= fall;
    fire("scroll", { target: el });
    assert.strictEqual(window.__agyIsSpontaneousScroll(el), expected, what + ", fall of " + fall);
    frame();
  }
}

// Anything without a size yet could have been any height, and so could a frame.
for (const [what, node] of [
  ["image not loaded", element(2, 0, [image(0, false)])],
  ["image not loaded, removed directly", Object.assign(element(0, 0, [], "IMG"), { naturalHeight: 0, complete: false })],
  ["video not loaded", element(2, 0, [{ tagName: "VIDEO", videoHeight: 0 }])],
  ["empty canvas", element(2, 0, [{ tagName: "CANVAS", height: 0 }])],
  ["frame", element(2, 0, [{ tagName: "IFRAME", height: "300" }])],
  ["frame removed directly", Object.assign(element(0, 0, [], "IFRAME"), { height: "300" })],
]) {
  settle();
  const el = viewport();
  remove(el, node);
  el.scrollTop -= 8000;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true, what);
  frame();
}

// An image that failed to load adds nothing, so it cannot explain a fall its element does not.
for (const [fall, expected] of [[3 * 48, true], [3 * 48 + 1, false]]) {
  settle();
  const el = viewport();
  remove(el, element(2, 0, [image(0)]));
  el.scrollTop -= fall;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), expected, "fall of " + fall);
  frame();
}

// A row sized in cqh can be tall with almost nothing in it, as the latest turn's is, removed directly or inside something.
for (const node of [
  Object.assign(element(2, 0), { matches: selector => selector.split(",").includes('[style*="cqh"]') }),
  element(2, 0, [{ tagName: "DIV", selector: '[style*="cqh"]' }]),
]) {
  settle();
  const el = viewport();
  remove(el, node);
  el.scrollTop -= 8000;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true);
  frame();
}

// Removing something with no height at all still counts as a removal when the conversation visibly got shorter.
{
  settle();
  const el = viewport();
  remove(el, { nodeType: 8, textContent: "" });
  el.scrollHeight -= 1000;
  el.scrollTop -= 1000;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true);
  frame();
}

// What a scroll event reports is what the next fall is measured from, for the position and the height alike.
{
  settle();
  let el = viewport();
  el.scrollTop = 5000;
  fire("scroll", { target: el });
  remove(el, ROWS);
  frame();
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false, "moved before the removal");
  settle();
  el = viewport();
  el.scrollHeight = 20000;
  fire("scroll", { target: el });
  remove(el, ICON);
  el.scrollHeight = 19000;
  el.scrollTop -= 1000;
  frame();
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true, "grew before the removal");
}

// A later collapse replaces an earlier one that has run out.
{
  settle();
  const el = viewport();
  collapse(el);
  now += 3000;
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
  el.scrollTop = 5000;
  fire("scroll", { target: el });
  collapse(el);
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true);
}

// Falling with nothing removed is left to the bundle, which handles the content being short when the event arrives.
{
  settle();
  const el = viewport();
  el.scrollHeight = 1000;
  el.scrollTop = 300;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
}

// Rows coming back can push the position down the conversation, as Safari was seen to, but a scroll that ends further up
// than the collapse left it came from something else.
for (const [top, expected] of [[298, false], [299, true], [300, true], [398, true], [8000, true]]) {
  settle();
  const el = viewport();
  collapse(el);
  el.scrollTop = top;
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), expected, "at " + top);
}

// Back where it was, the collapse is over, and a scroll with no input afterwards is left alone.
for (const [top, over] of [[8998, false], [8999, false], [9000, true], [9500, true]]) {
  settle();
  const el = viewport();
  collapse(el);
  el.scrollTop = top;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true, "on the event reaching " + top);
  el.scrollTop = 8000;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), !over, "after reaching " + top);
}

// A second collapse before the first has ended replaces it, including where it left the position.
{
  settle();
  const el = viewport();
  collapse(el);
  el.scrollTop = 300;
  fire("scroll", { target: el });
  remove(el, ROWS);
  el.scrollTop = 100;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true);
  frame();
}

// A small shrink while following: the next follow ends it, so a short scroll with no input after that is left alone.
{
  settle();
  const el = viewport();
  remove(el, ICON);
  el.scrollHeight -= 30;
  el.scrollTop -= 30;
  frame();
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true, "the clamp itself");
  el.scrollHeight += 400;
  el.scrollTop = el.scrollHeight - 1000;
  fire("scroll", { target: el });
  el.scrollTop -= 100;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false, "after following again");
}

// The observer leaves the layout alone: reading it there could clamp the position in a gap between two commits.
{
  settle();
  let reads = 0;
  const el = viewport();
  Object.defineProperty(el, "scrollHeight", { get() { reads++; return 10000; } });
  Object.defineProperty(el, "scrollTop", { get() { reads++; return 9000; } });
  remove(el, ROWS);
  remove(el, ICON);
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

// A small removal explains a fall as far as the conversation visibly got shorter, and no further.
{
  settle();
  let el = viewport();
  remove(el, ICON);
  el.scrollHeight = 9000;
  el.scrollTop = 8000;
  frame();
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true, "fell by what it shrank");
  settle();
  el = viewport();
  remove(el, ICON);
  el.scrollHeight = 9000;
  frame();
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false, "shrank without falling");
  settle();
  el = viewport();
  remove(el, ROWS);
  el.scrollTop = 8999;
  frame();
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false, "rounding");
  settle();
  el = viewport();
  remove(el, ROWS);
  el.scrollTop = 8998;
  frame();
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true, "more than rounding");
}

// A conversation that has not scrolled yet has nothing to compare with.
{
  settle();
  const el = { scrollTop: 300, scrollHeight: 1000, matches: () => true, closest: () => el };
  remove(el, ROWS);
  frame();
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
}

// A record from outside a conversation does not hide the ones after it, as the observer gets the whole document's.
{
  settle();
  const el = viewport();
  observer([{ target: { closest: () => null }, removedNodes: [ROWS] }, { target: el, removedNodes: [ROWS] }]);
  el.scrollTop = 300;
  frame();
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true);
}

// What removed text comes to on its own.
for (const [fall, expected] of [[1600, true], [1601, false]]) {
  settle();
  const el = viewport();
  remove(el, { nodeType: 3, textContent: "x".repeat(800) });
  el.scrollTop -= fall;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), expected, "fall of " + fall);
  frame();
}

// Removals outside a conversation are not looked at.
{
  settle();
  const el = viewport();
  observer([{ target: { closest: () => null }, removedNodes: [ROWS] }, { target: {}, removedNodes: [ROWS] }]);
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

// Whole rows explain a fall with the height unchanged: the scroll event can arrive before the frame, with the content
// already back.
{
  settle();
  const el = viewport();
  remove(el, ROWS);
  el.scrollTop = 300;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true);
  frame();
}

// A hidden tab runs no frames, so the comparison waits for the reader to return, however long that takes.
{
  settle();
  const el = viewport();
  remove(el, ROWS);
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

// A reader at the bottom flung far above the top goes back to the bottom, and stays there for the bounce.
{
  settle();
  const el = viewport();
  el.clientHeight = 1000;
  el.scrollTop = -34601;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 9000);
  el.scrollTop = -500;
  now += 100;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 9000, "the bounce keeps pulling toward the top");
  el.scrollHeight = 12000;
  now += 100;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 11000, "the reader keeps their distance from the bottom");
  el.scrollTop = 0;
  now += 800;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 0, "released once the bounce has had time to finish");
}

// A reader further up goes back to the position they had, whatever the height does.
{
  settle();
  const el = viewport();
  el.clientHeight = 500;
  el.scrollTop = -34601;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 9000);
  el.scrollHeight = 12000;
  now += 100;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 9000);
}

// The frame after a removal can be the first to see the fling, and it holds just the same.
{
  settle();
  const el = viewport();
  remove(el, ROWS);
  el.scrollTop = -34601;
  frame();
  assert.strictEqual(el.scrollTop, 9000);
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
}

// A hold that ran out with no event to end it does not hide the next collapse.
{
  settle();
  const el = viewport();
  el.scrollTop = -34601;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 9000);
  now += 5000;
  collapse(el);
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true);
}

// From an overscroll already under way there is no position to go back to.
{
  settle();
  const el = viewport();
  el.scrollTop = -300;
  fire("scroll", { target: el });
  el.scrollTop = -1500;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, -1500);
}

// Rows removed during the bounce are not taken for a collapse, then or once it is over.
{
  settle();
  const el = viewport();
  el.scrollTop = -34601;
  fire("scroll", { target: el });
  remove(el, ROWS);
  el.scrollTop = -20000;
  frame();
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
  now += 1000;
  el.scrollTop = 0;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
}

// The bounce settling at the top is still the bounce.
{
  settle();
  const el = viewport();
  el.scrollTop = -34601;
  fire("scroll", { target: el });
  el.scrollTop = 0;
  now += 100;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 9000);
}

// It holds for the whole second, and only past a fling.
{
  settle();
  const el = viewport();
  el.scrollTop = -34601;
  fire("scroll", { target: el });
  now += 999;
  el.scrollTop = -500;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 9000, "still held at 999 ms");
  settle();
  const near = viewport();
  near.scrollTop = -999;
  fire("scroll", { target: near });
  assert.strictEqual(near.scrollTop, -999, "-999 is left alone");
}

// The re-render that flings the conversation usually grows it too, as in the trace (18,331 to 82,650 px).
{
  settle();
  const el = viewport();
  el.clientHeight = 1000;
  el.scrollHeight = 82650;
  el.scrollTop = -34601;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 81650);
}

// Within 100 px of the bottom counts as the bottom, and further up does not.
for (const [clientHeight, want] of [[900, 11000], [800, 9000]]) {
  settle();
  const el = viewport();
  el.clientHeight = clientHeight;
  el.scrollTop = -34601;
  fire("scroll", { target: el });
  el.scrollHeight = 12000;
  now += 100;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, want, "client height " + clientHeight);
}

// Once input on the conversation ends a hold, the bounce that follows is the reader's, event after event.
{
  settle();
  const el = viewport();
  el.scrollTop = -34601;
  fire("scroll", { target: el });
  now += 100;
  fire("touchstart", { target: { closest: () => el } });
  now += 1;
  for (const top of [-20000, -19000, -5000]) {
    el.scrollTop = top;
    fire("scroll", { target: el });
    assert.strictEqual(el.scrollTop, top);
    now += 16;
  }
}

// Nor is a hold that ran out started again by the bounce still going.
{
  settle();
  const el = viewport();
  el.scrollTop = -34601;
  fire("scroll", { target: el });
  now += 1000;
  for (const top of [-15000, -14000]) {
    el.scrollTop = top;
    fire("scroll", { target: el });
    assert.strictEqual(el.scrollTop, top);
    now += 16;
  }
}

// Something else moving the conversation, such as the jump to the latest message, ends the hold. A fling after
// that is a new one, back to where that left the reader.
{
  settle();
  const el = viewport();
  el.scrollTop = -34601;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 9000);
  now += 100;
  el.scrollTop = 9500;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 9500);
  now += 100;
  el.scrollTop = 9400;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 9400, "no longer held");
  el.scrollTop = -30000;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 9400);
}

// A fling seen while pressed is not held, and is not kept as the position to go back to either.
{
  settle();
  const el = viewport();
  fire("pointerdown", { target: { closest: () => el } });
  el.scrollTop = -34601;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, -34601);
  fire("pointerup", { target: body });
  settle();
  el.scrollTop = -30000;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 9000);
}

// The ordinary overscroll of a finger or a fling stays alone.
{
  settle();
  const el = viewport();
  el.scrollTop = -300;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, -300);
}

// Nor is the first thing a page shows a position to go back to.
{
  settle();
  const el = { scrollTop: -34601, scrollHeight: 10000, matches: () => true, closest: () => el };
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, -34601);
}

// Touching the conversation hands it back, and a held press is never overridden.
{
  settle();
  const el = viewport();
  el.scrollTop = -34601;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 9000);
  now += 100;
  fire("touchstart", { target: { closest: () => el } });
  now += 1;
  el.scrollTop = 200;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 200);

  settle();
  const held = viewport();
  fire("pointerdown", { target: { closest: () => held } });
  held.scrollTop = -34601;
  fire("scroll", { target: held });
  assert.strictEqual(held.scrollTop, -34601);
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
