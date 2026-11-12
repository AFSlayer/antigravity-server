package patches

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Runs the full keyboard-detect script alongside the patched New Conversation
// helper to verify when focusin speculatively shrinks --agy-bottom and when it
// waits for visualViewport instead.
func TestKeyboardPredictionNeedsATouchOnTheField(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}

	script := keyboardDetect
	script = script[strings.Index(script, ">")+1:]
	script = script[:strings.LastIndex(script, "</script>")]

	v228 := `var Lb=f==="project"||f==="repo";if(Lb||f==="status"||f==="location"){let Qb,kb;Lb&&(Qb=G?"New Conversation in Workspace":"New Conversation in Project",kb=Pc=>{Pc.stopPropagation();` + "\n" + `Ma(Lc)});var row=A.createElement(Muc,{onAddClick:kb,newChatLabel:Qb,surface:a})}return A.createElement(bB,{delay:300,timeout:200},A.createElement("div",{ref:Ac.sidebarBoundsRef,className:"flex-1 min-h-0 h-full w-full relative"},Za?A.createElement(Ztc,null):A.createElement(Bfc,{items:Ac.displayedFlatItems,variant:"sidebar",surface:a,subtitleContext:W,onToggleCollapseSection:Fa,onToggleSectionHeader:X,onTogglePagination:eb,customHeaderRenderer:nf,getItemProps:Ge})))`
	patchedNewConvo := applyOnly(t, "mobile-new-conversation-button", v228, Options{MobileUX: true})

	dir := t.TempDir()
	kbPath := filepath.Join(dir, "keyboard_detect.js")
	if err := os.WriteFile(kbPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	ncPath := filepath.Join(dir, "new_convo.js")
	ncModule := "module.exports = function mountNewConvo(A, Ma, nf) {\n" +
		"  const bB = 'Tooltip', Ztc = 'Skeleton', Bfc = 'VirtualList', Muc = 'ProjectRow';\n" +
		"  const f = 'project', G = false, Lc = 'proj-1', Za = false, W = {}, Fa = () => {}, X = () => {}, eb = () => {}, Ge = () => {};\n" +
		"  const a = 'background', Ac = { sidebarBoundsRef: null, displayedFlatItems: [{ type: 'header', id: 'header-p1', label: 'Project 1' }] };\n" +
		"  " + patchedNewConvo + "\n" +
		"};\n"
	if err := os.WriteFile(ncPath, []byte(ncModule), 0o600); err != nil {
		t.Fatal(err)
	}

	harness := `
const assert = require("assert");
const fs = require("fs");

let clock = 100000;
let rafSeq = 0;
let rafQueue = new Map();
let timerSeq = 0;
let timerQueue = new Map();
const storage = new Map([["agy-kb-p", "280"]]);

const winListeners = {};
const docListeners = {};
const vvListeners = {};

const addListener = (bag, type, fn) => {
  (bag[type] = bag[type] || []).push(fn);
};
const fire = (bag, type, ev) => {
  for (const fn of (bag[type] || []).slice()) fn(ev);
};

const makeClassList = () => {
  const set = new Set();
  return {
    add: (...cs) => cs.forEach(c => set.add(c)),
    remove: (...cs) => cs.forEach(c => set.delete(c)),
    contains: c => set.has(c),
  };
};

const makeStyle = () => {
  const props = new Map();
  return {
    setProperty: (k, v) => props.set(k, String(v)),
    removeProperty: k => props.delete(k),
    getPropertyValue: k => props.get(k) || "",
  };
};

function matchesToken(node, raw) {
  let s = raw.trim();
  if (!s) return false;
  const nots = [];
  s = s.replace(/:not\(([^)]+)\)/g, (_, inner) => {
    nots.push(inner.trim());
    return "";
  });
  for (const neg of nots) {
    if (matchesToken(node, neg)) return false;
  }
  const classes = [];
  s = s.replace(/\.([a-zA-Z0-9_-]+)/g, (_, cls) => {
    classes.push(cls);
    return "";
  });
  for (const cls of classes) {
    if (!node.classList.contains(cls)) return false;
  }
  const attrs = [];
  s = s.replace(/\[([a-zA-Z0-9_-]+)(?:(\^=|=)"([^"]*)")?\]/g, (_, name, op, val) => {
    attrs.push({ name, op, val });
    return "";
  });
  for (const a of attrs) {
    const got = node.getAttribute(a.name);
    if (got === null) return false;
    if (a.op === "=" && got !== a.val) return false;
    if (a.op === "^=" && !got.startsWith(a.val)) return false;
  }
  if (s && node.tagName.toLowerCase() !== s.toLowerCase()) {
    return false;
  }
  return true;
}

function el(parent, tagName, opts = {}) {
  const attrs = new Map(Object.entries(opts.attrs || {}));
  const classList = makeClassList();
  for (const cls of (opts.className || "").split(/\s+/).filter(Boolean)) {
    classList.add(cls);
  }
  const listeners = {};
  const node = {
    nodeType: 1,
    tagName: tagName.toUpperCase(),
    parentElement: parent || null,
    isContentEditable: Boolean(opts.contentEditable),
    type: opts.type || "",
    name: opts.name || "",
    value: opts.value || "",
    textContent: opts.textContent || "",
    blurred: 0,
    classList,
    getAttribute: k => (attrs.has(k) ? String(attrs.get(k)) : null),
    matches: selector => selector.split(",").some(part => matchesToken(node, part)),
    closest(selector) {
      for (let cur = node; cur; cur = cur.parentElement) {
        if (cur.matches && cur.matches(selector)) return cur;
      }
      return null;
    },
    contains(other) {
      for (let cur = other; cur; cur = cur.parentElement) {
        if (cur === node) return true;
      }
      return false;
    },
    addEventListener: (type, fn) => addListener(listeners, type, fn),
    removeEventListener(type, fn) {
      const arr = listeners[type];
      if (!arr) return;
      const idx = arr.indexOf(fn);
      if (idx >= 0) arr.splice(idx, 1);
    },
    dispatchEvent(ev) {
      ev.target = ev.target || node;
      fire(listeners, ev.type, ev);
      if (ev.bubbles && ev.type === "focusin") {
        fire(winListeners, "focusin", ev);
      }
    },
    focus() {
      document.activeElement = node;
      node.dispatchEvent(new FocusEvent("focusin", { bubbles: true }));
    },
    blur() {
      node.blurred++;
      if (document.activeElement === node) {
        document.activeElement = document.body;
      }
      fire(winListeners, "focusout", { target: node });
    },
  };
  return node;
}

function textNode(parent) {
  return { nodeType: 3, parentElement: parent };
}

const vv = {
  height: 844,
  scale: 1,
  offsetTop: 0,
  addEventListener: (type, fn) => addListener(vvListeners, type, fn),
};

global.FocusEvent = class FocusEvent {
  constructor(type, init) {
    this.type = type;
    this.bubbles = Boolean(init && init.bubbles);
    this.target = null;
  }
};
global.MutationObserver = class { observe() {} };
global.localStorage = {
  getItem: k => (storage.has(k) ? storage.get(k) : null),
  setItem: (k, v) => storage.set(k, String(v)),
};
Object.defineProperty(global, "performance", { value: { now: () => clock } });
global.requestAnimationFrame = fn => {
  const id = ++rafSeq;
  rafQueue.set(id, fn);
  return id;
};
global.cancelAnimationFrame = id => rafQueue.delete(id);
global.setTimeout = fn => {
  const id = ++timerSeq;
  timerQueue.set(id, fn);
  return id;
};
global.clearTimeout = id => timerQueue.delete(id);

const docEl = {
  clientHeight: 844,
  scrollTop: 0,
  style: makeStyle(),
  classList: makeClassList(),
};
const bodyEl = el(null, "body");

let mobileEmptyMounted = false;

global.document = {
  documentElement: docEl,
  body: bodyEl,
  activeElement: bodyEl,
  addEventListener: (type, fn) => addListener(docListeners, type, fn),
  querySelector(sel) {
    if (sel.includes("agent-input-box") && sel.includes("data-lexical-editor")) return editor;
    if (sel === '[data-lexical-editor="true"]') return editor;
    if (sel === '[data-testid="mobile-new-convo-empty"]') return mobileEmptyMounted ? {} : null;
    return null;
  },
};

global.window = global;
global.innerWidth = 390;
global.innerHeight = 844;
global.scrollY = 0;
global.pageYOffset = 0;
global.scrollTo = () => {};
global.location = { pathname: "/" };
global.visualViewport = vv;
global.matchMedia = q => ({ matches: q.includes("pointer:coarse") });
global.addEventListener = (type, fn) => addListener(winListeners, type, fn);

// Build realistic DOM around the composer and surrounding UI.
const box = el(bodyEl, "div", { attrs: { "data-testid": "agent-input-box" } });
const padding = el(box, "div", { attrs: { "data-testid": "composer-padding" } });
const editor = el(box, "div", {
  contentEditable: true,
  attrs: { "data-lexical-editor": "true", role: "textbox", tabindex: "0" },
});
const paragraph = el(editor, "p");
const glyphText = textNode(paragraph);
const scrollbarTrack = el(box, "div", { attrs: { "data-overlay-scrollbar-track": "true" } });
const footer = el(box, "div", { className: "agy-composer-footer" });
const modelBtnWrap = el(footer, "div", { className: "no-focus-agent-input" });
const modelBtn = el(modelBtnWrap, "button", { attrs: { "data-testid": "model-selector-trigger", type: "button" } });
const modelBtnIcon = el(modelBtn, "svg");
const addCtxBtn = el(footer, "button", { attrs: { "aria-label": "Add context", "aria-haspopup": "menu", type: "button" } });
const sendBtn = el(footer, "button", { attrs: { type: "button" } });
const docLink = el(box, "a", { attrs: { href: "#ref" } });
const contextChip = el(box, "div", { attrs: { tabindex: "0" } });
const backdrop = el(bodyEl, "div", { attrs: { "data-testid": "bottom-sheet-backdrop" } });
const search = el(bodyEl, "input", { type: "text", attrs: { "data-testid": "search" } });
const askRadio = el(bodyEl, "input", { type: "radio", name: "ask-question-0", value: "opt-1" });

new Function(fs.readFileSync(process.argv[1], "utf8"))();

let startedSection = null;
const mountNewConvo = require(process.argv[2]);
const vdomTree = mountNewConvo(
  { createElement: (tag, props, ...children) => ({ tag, props, children }) },
  sec => { startedSection = sec; mobileEmptyMounted = true; },
  h => ({ tag: "origHeader", id: h.id }),
);
const newConvoBtnVNode = vdomTree.children[0].children[0].props.customHeaderRenderer({ id: "header-__agy_new__" });
const newConvoBtnDom = el(bodyEl, "button", { attrs: { "data-testid": "new-conversation-button", type: "button" } });

const bottom = () => docEl.style.getPropertyValue("--agy-bottom");
const flushRaf = () => {
  const due = Array.from(rafQueue.values());
  rafQueue.clear();
  for (const fn of due) fn();
};
const resetKeyboard = () => {
  vv.height = 844;
  clock += 2000;
  fire(vvListeners, "resize", {});
  flushRaf();
  document.activeElement = bodyEl;
  assert.strictEqual(bottom(), "", "keyboard state must settle back to closed");
  assert.strictEqual(bodyEl.classList.contains("agy-kb-open"), false);
};
const tap = (target, x = 120, y = 780) => {
  fire(docListeners, "touchstart", { target, touches: [{ clientX: x, clientY: y }] });
  clock += 25;
  fire(docListeners, "touchend", { target, touches: [] });
};
const focusField = field => {
  document.activeElement = field;
  fire(winListeners, "focusin", { target: field });
};

// 1. Focus handed to the editor by script with no user touch must NOT shrink.
focusField(editor);
assert.strictEqual(bottom(), "");
assert.strictEqual(bodyEl.classList.contains("agy-kb-open"), false);

// 2. Tapping inside the editor (including a DOM Text node with no .closest) shrinks immediately.
tap(glyphText);
clock += 40;
focusField(editor);
assert.strictEqual(bottom(), "280px");
assert.strictEqual(bodyEl.classList.contains("agy-kb-open"), true);

// 3. Consumed touch cannot be reused if the keyboard closes and script re-focuses within 1s.
vv.height = 844;
clock += 520; // past holdUntil (500ms), still within 1000ms of the original tap (560ms)
flushRaf();
assert.strictEqual(bottom(), "");
focusField(editor);
assert.strictEqual(bottom(), "", "consumed touch must not shrink a second script-driven focusin");
resetKeyboard();

// 4. Tapping non-interactive padding of agent-input-box shrinks when the editor takes focus.
tap(padding);
clock += 60;
focusField(editor);
assert.strictEqual(bottom(), "280px");
resetKeyboard();

// 5. Closing the model picker's bottom sheet by tapping its backdrop returns focus ~540ms later: must NOT shrink.
tap(backdrop, 200, 200);
clock += 540;
focusField(editor);
assert.strictEqual(bottom(), "");

// 6. Tapping buttons, icons, links, popup triggers, focusable chips, or scrollbar tracks inside
//    agent-input-box must NOT shrink when focus returns to the editor (immediately or ~540ms later).
for (const ctrl of [modelBtn, modelBtnIcon, addCtxBtn, sendBtn, docLink, contextChip, scrollbarTrack]) {
  tap(ctrl);
  clock += 10;
  focusField(editor);
  assert.strictEqual(bottom(), "", "immediate editor focus after tapping composer control must not shrink");

  tap(ctrl);
  clock += 540;
  focusField(editor);
  assert.strictEqual(bottom(), "", "delayed editor focus after tapping composer control must not shrink");
}

// 7. A standalone input outside the composer shrinks only for itself, never for the composer.
tap(search, 180, 80);
clock += 40;
focusField(editor);
assert.strictEqual(bottom(), "");
focusField(search);
assert.strictEqual(bottom(), "280px");
resetKeyboard();

// 8. A stale touch (> 1s ago) does not shrink.
tap(paragraph);
clock += 1500;
focusField(editor);
assert.strictEqual(bottom(), "");

// 9. A stationary long press focuses on lift (touchend) and shrinks.
fire(docListeners, "touchstart", { target: paragraph, touches: [{ clientX: 100, clientY: 780 }] });
clock += 1500;
fire(docListeners, "touchmove", { target: paragraph, touches: [{ clientX: 104, clientY: 783 }] });
fire(docListeners, "touchend", { target: paragraph, touches: [] });
clock += 50;
focusField(editor);
assert.strictEqual(bottom(), "280px");
resetKeyboard();

// 10. A scroll or swipe drag starting on the composer must NOT leave a live tap behind on touchend.
fire(docListeners, "touchstart", { target: paragraph, touches: [{ clientX: 100, clientY: 780 }] });
clock += 40;
fire(docListeners, "touchmove", { target: paragraph, touches: [{ clientX: 100, clientY: 740 }] });
clock += 40;
fire(docListeners, "touchend", { target: paragraph, touches: [] });
clock += 30;
focusField(editor);
assert.strictEqual(bottom(), "", "dragged touch must not trigger speculative shrink");

// 11. Multi-touch and touchcancel clear any pending tap state.
fire(docListeners, "touchstart", { target: paragraph, touches: [{ clientX: 100, clientY: 780 }, { clientX: 200, clientY: 780 }] });
fire(docListeners, "touchend", { target: paragraph, touches: [] });
clock += 20;
focusField(editor);
assert.strictEqual(bottom(), "", "multi-touch pinch must not trigger speculative shrink");

fire(docListeners, "touchstart", { target: paragraph, touches: [{ clientX: 100, clientY: 780 }] });
clock += 20;
fire(docListeners, "touchcancel", { target: paragraph, touches: [] });
clock += 20;
focusField(editor);
assert.strictEqual(bottom(), "", "canceled touch must not trigger speculative shrink");

// 12. Tapping the mobile New Conversation button delegates synchronous focus via __agyFocusComposer and shrinks.
document.activeElement = bodyEl;
mobileEmptyMounted = false;
startedSection = null;
tap(newConvoBtnDom, 120, 160);
clock += 10;
newConvoBtnVNode.props.onClick();
assert.strictEqual(startedSection, "outside-of-project");
assert.strictEqual(document.activeElement, editor);
assert.strictEqual(bottom(), "280px", "New Conversation tap must speculatively shrink for the focused composer");
resetKeyboard();

// Mobile home top row path (__agyStartNewConversation) also shrinks, then consumes its token.
document.activeElement = bodyEl;
mobileEmptyMounted = false;
tap(newConvoBtnDom, 120, 120);
clock += 10;
window.__agyStartNewConversation();
assert.strictEqual(bottom(), "280px");
vv.height = 844;
clock += 520;
flushRaf();
assert.strictEqual(bottom(), "");
document.activeElement = bodyEl;
focusField(editor);
assert.strictEqual(bottom(), "", "consumed __agyFocusComposer token must not shrink a later focusin");
resetKeyboard();

// Touching elsewhere cancels any unconsumed __agyComposerFocusAt token.
document.activeElement = bodyEl;
window.__agyComposerFocusAt = clock;
fire(docListeners, "touchstart", { target: backdrop, touches: [{ clientX: 50, clientY: 50 }] });
fire(docListeners, "touchend", { target: backdrop, touches: [] });
clock += 20;
focusField(editor);
assert.strictEqual(bottom(), "");

// 13. Post-question auto-focus suppression blocks button-triggered focus but allows direct composer and New Conversation taps.
document.activeElement = bodyEl;
fire(docListeners, "change", { target: askRadio });
tap(modelBtn);
clock += 10;
editor.blurred = 0;
focusField(editor);
assert.strictEqual(editor.blurred, 1, "auto-focus after non-editor control tap during suppression window must blur");
assert.strictEqual(bottom(), "");

document.activeElement = bodyEl;
fire(docListeners, "change", { target: askRadio });
tap(padding);
clock += 10;
editor.blurred = 0;
focusField(editor);
assert.strictEqual(editor.blurred, 0, "deliberate user tap on composer padding must not be blurred");
assert.strictEqual(bottom(), "280px");
resetKeyboard();

document.activeElement = bodyEl;
fire(docListeners, "change", { target: askRadio });
tap(newConvoBtnDom, 120, 160);
clock += 10;
editor.blurred = 0;
newConvoBtnVNode.props.onClick();
assert.strictEqual(editor.blurred, 0, "New Conversation tap during suppression window must not be blurred");
assert.strictEqual(bottom(), "280px");
resetKeyboard();

// 14. Android resizes-content mode (both layout clientHeight and visualViewport.height shrink together):
//     must cancel speculative --agy-bottom immediately (no double shrink), keep agy-kb-open active,
//     support swipe-down dismiss, and learn usesLayoutResize for subsequent taps.
tap(paragraph);
clock += 20;
focusField(editor);
assert.strictEqual(bottom(), "280px");

// Simulate Android browser shrinking layout + visual viewports together 80ms later (well within 500ms holdUntil)
clock += 80;
docEl.clientHeight = 520;
global.innerHeight = 520;
vv.height = 520;
fire(vvListeners, "resize", {});
flushRaf();
assert.strictEqual(bottom(), "", "Android layout-resize mode must clear --agy-bottom immediately without waiting for holdUntil");
assert.strictEqual(bodyEl.classList.contains("agy-kb-open"), true, "agy-kb-open must stay active while Android layout viewport is shrunk");

// Swipe-down gesture must still blur the active input in Android layout-resize mode
editor.blurred = 0;
fire(docListeners, "touchstart", { target: bodyEl, touches: [{ clientX: 180, clientY: 200 }] });
fire(winListeners, "touchmove", { target: bodyEl, touches: [{ clientX: 180, clientY: 270 }] });
assert.strictEqual(editor.blurred, 1, "swipe-down must dismiss keyboard in Android layout-resize mode");

// Restore full layout height -> agy-kb-open clears cleanly
docEl.clientHeight = 844;
global.innerHeight = 844;
vv.height = 844;
clock += 200;
fire(vvListeners, "resize", {});
flushRaf();
assert.strictEqual(bodyEl.classList.contains("agy-kb-open"), false);

// Subsequent tap on this Android session skips speculative --agy-bottom jump entirely
tap(paragraph);
clock += 20;
focusField(editor);
assert.strictEqual(bottom(), "", "subsequent focus on layout-resizing device must not speculatively set --agy-bottom");
resetKeyboard();

// 15. Window / toolbar / split-screen shrink while no input is focused must recalibrate maxBaseP downward
//     so focusing afterwards does NOT falsely trigger layoutKbOpen, and an actual visualViewport keyboard
//     (target > 0) restores usesLayoutResize = false.
clock += 600;
docEl.clientHeight = 700;
global.innerHeight = 700;
vv.height = 700;
fire(winListeners, "resize", {});
flushRaf();
assert.strictEqual(bodyEl.classList.contains("agy-kb-open"), false, "non-keyboard window shrink must not mark keyboard open");

tap(paragraph);
clock += 20;
focusField(editor);
flushRaf();
assert.strictEqual(bodyEl.classList.contains("agy-kb-open"), false, "focusing after baseline recalibration must not falsely detect layout-resize keyboard");

// Now simulate overlay keyboard shrinking only visualViewport (target = 700 - 420 = 280) -> clears usesLayoutResize
vv.height = 420;
fire(vvListeners, "resize", {});
flushRaf();
clock += 300;
flushRaf();
assert.strictEqual(bottom(), "280px", "overlay keyboard must apply --agy-bottom");
vv.height = 700;
clock += 2000;
fire(vvListeners, "resize", {});
flushRaf();
document.activeElement = bodyEl;
assert.strictEqual(bottom(), "");
assert.strictEqual(bodyEl.classList.contains("agy-kb-open"), false);

// Because usesLayoutResize was cleared when target > 0, next portrait tap speculatively shrinks again
tap(paragraph);
clock += 20;
focusField(editor);
assert.strictEqual(bottom(), "280px", "speculative shrink must resume once overlay keyboard resets usesLayoutResize");

process.exit(0);
`
	if out, err := exec.Command(node, "-e", harness, kbPath, ncPath).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
