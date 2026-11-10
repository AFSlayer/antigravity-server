package patches

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
let coarse = false;
global.matchMedia = () => ({ matches: coarse });
global.navigator = { userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X)", platform: "iPhone", maxTouchPoints: 5 };
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

// A bounce that keeps pulling up from the position just set is still the bounce.
{
  settle();
  const el = viewport();
  el.clientHeight = 500;
  el.scrollTop = -34601;
  fire("scroll", { target: el });
  for (const top of [6000, 2000, 20]) {
    now += 16;
    el.scrollTop = top;
    fire("scroll", { target: el });
    assert.strictEqual(el.scrollTop, 9000, "pulled up to " + top);
  }
}

// A fling right after a collapse goes back to where the reader was before it, not to where it left them.
{
  settle();
  const el = viewport();
  el.clientHeight = 600;
  el.scrollTop = 9400;
  fire("scroll", { target: el });
  remove(el, ROWS);
  el.scrollHeight = 2000;
  el.scrollTop = 0;
  frame();
  el.scrollHeight = 82650;
  el.scrollTop = -34601;
  now += 50;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 82050);
}

// From a trace: scrolled up in a long conversation, the window dropped rows above the reader and the position fell by
// exactly what went, so nothing moved on screen. A frame later iOS put back the position from before, in a conversation
// that was now far shorter. Going back there threw the reader 18,258 px down, and the window slid after them.
{
  coarse = true;
  now += 5000;
  const el = viewport();
  el.clientHeight = 666;
  el.scrollHeight = 48921;
  el.scrollTop = 26327;
  fire("scroll", { target: el });
  now += 950;
  observer([{ target: el, removedNodes: [ROWS] }]);
  el.scrollHeight = 30663;
  el.scrollTop = 8069;
  frame();
  now += 34;
  el.scrollTop = 26292;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 8069, "the rows above went for good, so the position they left is the reader's");
  now += 5000;
  coarse = false;
}

// The same with the reader still moving, so the position falls by a little more than the rows took.
{
  coarse = true;
  now += 5000;
  const el = viewport();
  el.clientHeight = 666;
  el.scrollHeight = 33021;
  el.scrollTop = 26039;
  fire("scroll", { target: el });
  now += 500;
  observer([{ target: el, removedNodes: [ROWS] }]);
  el.scrollHeight = 22594;
  el.scrollTop = 15500;
  frame();
  now += 12;
  el.scrollTop = 25641;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 15500);
  now += 5000;
  coarse = false;
}

// A hold still running when the window slides moves with it, rather than going back to where the rows used to be.
{
  coarse = true;
  now += 5000;
  const el = viewport();
  el.clientHeight = 666;
  el.scrollHeight = 48921;
  el.scrollTop = 26327;
  fire("scroll", { target: el });
  now += 50;
  el.scrollTop = -58;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 26327);
  now += 300;
  el.scrollHeight = 30663;
  el.scrollTop = 8069;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 8069);
  now += 50;
  el.scrollTop = -30;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 8069, "the hold keeps the position the slide left");
  now += 5000;
  coarse = false;
}

// A collapse long past says nothing about where a later fling came from.
{
  coarse = true;
  now += 5000;
  const el = viewport();
  el.clientHeight = 1000;
  observer([{ target: el, removedNodes: [ROWS] }]);
  el.scrollHeight = 1000;
  el.scrollTop = 0;
  frame();
  now += 3000;
  el.scrollHeight = 30000;
  el.scrollTop = 5000;
  fire("scroll", { target: el });
  now += 20;
  el.scrollTop = 20000;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 5000);
  now += 5000;
  coarse = false;
}

// From a phone trace: rows came above the reader at the top, then rows went while the reader flung up, and each time the
// app moved the position to keep the same rows on screen. iOS kept the fling going in the old coordinates, so the
// fling is ended where the app put the position, once the finger is off and only on iOS.
{
  coarse = true;
  now += 5000;
  const el = viewport();
  el.clientHeight = 666;
  el.style = { overflowY: "" };
  const onEl = touches => ({ target: { closest: () => el }, touches });
  el.scrollHeight = 31965;
  el.scrollTop = 0;
  fire("scroll", { target: el });
  fire("touchstart", onEl([{}]));
  fire("touchmove", onEl([{}]));
  fire("touchend", onEl([]));
  now += 400;
  el.scrollHeight = 62646;
  el.scrollTop = 30681;
  fire("scroll", { target: el });
  assert.strictEqual(el.style.overflowY, "hidden", "rows added above during a fling end it");
  frame();
  assert.strictEqual(el.style.overflowY, "", "for one frame");

  now += 300;
  el.scrollTop = 29606;
  fire("scroll", { target: el });
  assert.strictEqual(el.style.overflowY, "", "an ordinary scroll leaves the fling alone");
  el.scrollHeight = 41217;
  el.scrollTop = 8105;
  fire("scroll", { target: el });
  assert.strictEqual(el.style.overflowY, "hidden", "rows taken from above during a fling end it");
  frame();

  // A finger still down is dragging, and nothing ends that.
  fire("touchstart", onEl([{}]));
  fire("touchmove", onEl([{}]));
  el.scrollHeight = 71217;
  el.scrollTop = 38105;
  fire("scroll", { target: el });
  assert.strictEqual(el.style.overflowY, "");
  fire("touchend", onEl([]));

  // Long after the last input there is no fling to end.
  now += 10000;
  el.scrollHeight = 41217;
  el.scrollTop = 8105;
  fire("scroll", { target: el });
  assert.strictEqual(el.style.overflowY, "");

  now += 5000;
  coarse = false;
}

// From a phone trace: a long fling ended in a bounce at the top 3.6 s after the finger lifted, rows came above, and the
// bounce painted a frame in its old coordinates before the scroll event could end it. The fling is ended when the app
// asks to move the position that far, before it moves.
{
  coarse = true;
  now += 5000;
  const el = viewport();
  el.clientHeight = 666;
  el.style = { overflowY: "" };
  const onEl = touches => ({ target: { closest: () => el }, touches });
  el.scrollHeight = 43193;
  el.scrollTop = -57;
  fire("touchstart", onEl([{}]));
  fire("touchmove", onEl([{}]));
  fire("touchend", onEl([]));
  now += 3564;
  window.__agyScrolledByScript(el, 29592);
  assert.strictEqual(el.style.overflowY, "hidden", "ended before the move");
  frame();
  assert.strictEqual(el.style.overflowY, "");

  // A small correction is not rows coming or going.
  window.__agyScrolledByScript(el, 900);
  assert.strictEqual(el.style.overflowY, "");

  // Nor is a finger still down ended.
  fire("touchstart", onEl([{}]));
  window.__agyScrolledByScript(el, 29592);
  assert.strictEqual(el.style.overflowY, "");
  fire("touchend", onEl([]));
  now += 10000;
  coarse = false;
}

// From a phone trace: a second after older rows loaded, the app dropped the rows far above the reader while the reader
// was still scrolling up, and the position then ran 300 to 1000 px back down. The rows wait while a finger is down or
// the conversation is still moving after input, and go once it has been still for a moment.
{
  now += 5000;
  const el = viewport();
  el.clientHeight = 666;
  el.style = { overflowY: "" };
  const onEl = touches => ({ target: { closest: () => el }, touches });
  let waits = 0;
  global.CustomEvent = class { constructor(type) { this.type = type; } };
  global.dispatchEvent = e => { if (e.type === "agy-trim-wait") waits++; };
  el.scrollHeight = 71349;
  el.scrollTop = 31911;
  fire("scroll", { target: el });
  now += 450;
  fire("touchstart", onEl([{}]));
  assert.strictEqual(window.__agyDeferTrim(), true, "a finger down");
  fire("touchmove", onEl([{}]));
  now += 50;
  el.scrollTop -= 40;
  fire("scroll", { target: el });
  fire("touchend", onEl([]));
  assert.strictEqual(window.__agyDeferTrim(), true, "a fling still going");
  now += 200;
  el.scrollTop -= 30;
  fire("scroll", { target: el });
  now += 299;
  assert.strictEqual(window.__agyDeferTrim(), true);
  now += 2;
  assert.strictEqual(window.__agyDeferTrim(), false, "still for 300 ms");
  assert.strictEqual(waits, 1, "the trace marks the wait once");

  // The app moving the position long after any input, as it does while following a reply, does not hold the rows.
  now += 10000;
  el.scrollTop += 500;
  fire("scroll", { target: el });
  assert.strictEqual(window.__agyDeferTrim(), false);
  delete global.CustomEvent;
  delete global.dispatchEvent;
  now += 5000;
}

// A reply streaming in below changes the height by a line at a time. While the reader flings up the middle of the
// conversation, a step of the fling can land within the slack of that change, which is not rows moving above.
{
  coarse = true;
  now += 5000;
  const el = viewport();
  el.clientHeight = 666;
  el.scrollTop = 20000;
  el.scrollHeight = 40000;
  el.style = { overflowY: "" };
  const onEl = touches => ({ target: { closest: () => el }, touches });
  fire("scroll", { target: el });
  fire("touchstart", onEl([{}]));
  fire("touchmove", onEl([{}]));
  fire("touchend", onEl([]));
  for (let k = 0; k < 30; k++) {
    now += 16;
    el.scrollTop -= 120;
    if (k % 5 === 2) el.scrollHeight += 24;
    fire("scroll", { target: el });
    assert.strictEqual(el.style.overflowY, "", "a fling past a streaming reply goes on, step " + k);
    frame();
  }

  // Nor does a held position move down with the reply.
  now += 5000;
  el.scrollTop = 20000;
  fire("scroll", { target: el });
  now += 30;
  el.scrollTop = -5000;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 20000, "held");
  for (let k = 0; k < 20; k++) {
    now += 16;
    el.scrollHeight += 30;
    fire("scroll", { target: el });
  }
  assert.strictEqual(el.scrollTop, 20000, "the hold stays where the reader was");
  now += 5000;
  frame();
  coarse = false;
}

// A touch ends on the element it started on, and the app can remove that element while the finger is down. Its end
// then never reaches the window, and the rows above would wait for the next touch.
{
  now += 5000;
  const el = viewport();
  const row = { closest: () => el, own: {}, addEventListener(name, fn) { this.own[name] = fn; } };
  fire("touchstart", { target: row, touches: [{}] });
  assert.strictEqual(window.__agyDeferTrim(), true);
  row.own.touchend({ target: row, touches: [] });
  now += 400;
  assert.strictEqual(window.__agyDeferTrim(), false, "the removed row's own end lifts the finger");
  now += 5000;
}

// A hold that ran out with no event after it leaves its own position as the last one seen. Otherwise a re-render that
// shortened the conversation looks, at the next small removal, like a collapse of that size.
{
  settle();
  const el = viewport();
  el.clientHeight = 1000;
  el.scrollHeight = 12000;
  el.scrollTop = 11000;
  fire("scroll", { target: el });
  el.scrollHeight = 10000;
  el.scrollTop = -34601;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 9000);
  now += 5000;
  remove(el, ICON);
  frame();
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
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

// On a touch screen, a bounce at the bottom that ends tens of thousands of pixels up, where the bottom was before the
// conversation grew, goes back to the bottom, also when the bounce lands there again.
{
  settle();
  coarse = true;
  const tall = () => {
    const el = viewport();
    el.clientHeight = 666;
    el.scrollHeight = 103856;
    el.scrollTop = 103190;
    fire("scroll", { target: el });
    now += 16;
    return el;
  };
  let el = tall();
  el.scrollTop = 39242;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 103190);
  now += 100;
  el.scrollTop = 39242;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 103190, "again within the second");

  // Script asking for that spot, or for any spot by bringing a row into view, is the app's own doing.
  settle();
  el = tall();
  window.__agyScrolledByScript(el, 39242);
  el.scrollTop = 39242;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 39242, "asked for that spot");
  settle();
  el = tall();
  window.__agyScrolledByScript(null, null);
  el.scrollTop = 39242;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 39242, "a row brought into view");

  // A pin to the bottom in the same frame does not cover a jump somewhere else, and a request ages out.
  settle();
  el = tall();
  window.__agyScrolledByScript(el, 1e9);
  el.scrollTop = 39242;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 103190, "asked for the bottom");
  settle();
  el = tall();
  window.__agyScrolledByScript(el, 39242);
  now += 151;
  el.scrollTop = 39242;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 103190, "asked too long ago");

  // The conversation getting that much shorter explains the move, and smaller moves are left alone.
  settle();
  el = tall();
  el.scrollHeight = 39908;
  el.scrollTop = 39242;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 39242, "shorter by as much");
  for (const [to, want] of [[103190 - 31000, 103190 - 31000], [103190 - 32000, 103190]]) {
    settle();
    el = tall();
    el.scrollTop = to;
    fire("scroll", { target: el });
    assert.strictEqual(el.scrollTop, want, "moved " + (103190 - to));
  }
  settle();
  el = viewport();
  el.scrollTop = 4900;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 4900, "under 5000 px");

  // Away from touch screens the browser moves the conversation by itself.
  coarse = false;
  settle();
  el = tall();
  el.scrollTop = 39242;
  fire("scroll", { target: el });
  assert.strictEqual(el.scrollTop, 39242, "not a touch screen");
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

// Traced on a long conversation: rows were added above the reader, and the retries that reassert the position ran four
// seconds late because rendering held them back. The app had dropped other rows and lowered the position by then, so
// they put the reader 24,000 px down, and the window followed.
// The fling stop and the wait for still rows are for iOS. Other phones see the same moves and must keep their fling.
func TestFlingStopAndTrimWaitAreIOSOnly(t *testing.T) {
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
let frames = [];
global.window = global;
global.addEventListener = (name, fn) => { (listeners[name] = listeners[name] || []).push(fn); };
global.document = { querySelector: () => null };
global.MutationObserver = class { observe() {} };
global.requestAnimationFrame = fn => { frames.push(fn); };
global.setTimeout = fn => { frames.push(fn); };
Object.defineProperty(global, "performance", { value: { now: () => now } });
global.matchMedia = () => ({ matches: true });
global.navigator = { userAgent: "Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 Chrome/131.0 Mobile Safari/537.36", platform: "Linux armv8l", maxTouchPoints: 5 };
new Function(fs.readFileSync(process.argv[1], "utf8"))();
const fire = (name, event) => (listeners[name] || []).forEach(fn => fn(event));

const el = { scrollTop: 0, scrollHeight: 31965, clientHeight: 666, matches: () => true, closest: () => el, style: { overflowY: "" } };
const onEl = touches => ({ target: { closest: () => el }, touches });
fire("scroll", { target: el });
fire("touchstart", onEl([{}]));
assert.strictEqual(window.__agyDeferTrim(), false, "a finger down holds nothing back");
fire("touchmove", onEl([{}]));
fire("touchend", onEl([]));
now += 100;
el.scrollHeight = 62646;
el.scrollTop = 30681;
fire("scroll", { target: el });
assert.strictEqual(el.style.overflowY, "", "rows added above leave the fling alone");
assert.strictEqual(window.__agyDeferTrim(), false, "a fling holds nothing back");
window.__agyScrolledByScript(el, 8000);
assert.strictEqual(el.style.overflowY, "", "a large move by the app leaves the fling alone");
`
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "tracker.js")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "-e", harness, scriptPath).CombinedOutput(); err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
}

func TestLatePrependRetryLeavesThePositionAlone(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	start := strings.Index(keyboardDetect, "function handleScrollerMutation()")
	end := strings.Index(keyboardDetect, "function attachScrollerGuard()")
	if start < 0 || end < start {
		t.Fatal("the prepend handler was not found")
	}
	limit := regexp.MustCompile(`var PREPEND_REAPPLY_MS = \d+;`).FindString(keyboardDetect)
	if limit == "" {
		t.Fatal("the retry limit was not found")
	}
	harness := `
const assert = require("assert");
let clock = 100000;
let timers = [];
const performance = { now: () => clock };
const window = { location: { pathname: "/c/x" } };
const requestAnimationFrame = fn => fn();
const setTimeout = fn => { timers.push(fn); };
` + limit + `
let lastScrollHeight, lastScrollTop, sc;
const currentConvoUrl = "/c/x", convoNavigatedAt = 0, userScrolledNearTop = true;
const chatScroller = () => sc;
` + keyboardDetect[start:end] + `
// Rows arrive while the reader is at the top, and the position goes to where the old top now is.
const prepend = () => {
  timers = [];
  sc = { scrollTop: 0, scrollHeight: 458560 };
  lastScrollHeight = 458560;
  lastScrollTop = 0;
  sc.scrollHeight = 490841;
  handleScrollerMutation();
  assert.strictEqual(sc.scrollTop, 32281);
};
const runRetries = () => timers.forEach(fn => fn());

// A re-render that puts the position back at the top is still corrected.
prepend();
clock += 50;
sc.scrollTop = 0;
runRetries();
assert.strictEqual(sc.scrollTop, 32281);

// So is one that comes with more rows mounted.
prepend();
clock += 150;
sc.scrollTop = 0;
sc.scrollHeight = 491000;
runRetries();
assert.strictEqual(sc.scrollTop, 32281);

// Rows dropped above the reader shorten the conversation, and the lower position is right, however soon it happens.
prepend();
clock += 150;
sc.scrollHeight = 467045;
sc.scrollTop = 8346;
runRetries();
assert.strictEqual(sc.scrollTop, 8346);

// A retry held back for seconds does nothing, even if the height happens to be the same.
prepend();
clock += 4600;
sc.scrollTop = 8299;
runRetries();
assert.strictEqual(sc.scrollTop, 8299);
process.exit(0);
`
	if out, err := exec.Command(node, "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
}
