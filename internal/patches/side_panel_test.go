package patches

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The 2.31.2 text the side panel patches match, with the line break the
// minifier put in the navigation code.
const (
	sidePanelBodyV231       = `B.createElement("div",{className:"flex-grow overflow-hidden","data-side-panel-body":!0},fa?B.createElement("div",{className:"h-full w-full"},B.createElement(i0c,{key:fa.id,treeId:a,tabId:fa.id,tab:X})):B.createElement(H0c,{treeId:a,isPaneOpen:ea}))`
	sidePanelBodyLegacyV231 = `R&&C.some(S=>S.id==="terminal")&&B.createElement(dGc,{stripRef:H,activeTabId:O}),B.createElement("div",{className:"flex-grow overflow-hidden","data-side-panel-body":!0},P&&B.createElement("div",{className:"h-full w-full"},B.createElement(cGc,{key:P.id,treeId:a,tabId:P.id,tab:Q})))`
	conversationNavTabV231  = "var q=l?.focusedId??m,t=!f||k.sidePanel&&\nqp(k,q)?d?k.sidePanelV2.conversationPanes[q]?.activeTabId:k.sidePanel.conversationPanes[q]?.activeTabId:void 0;"
)

func runNode(t *testing.T, harness string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	path := filepath.Join(t.TempDir(), "harness.js")
	if err := os.WriteFile(path, []byte(harness), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, path).CombinedOutput(); err != nil {
		t.Fatalf("node harness failed: %v\n%s", err, out)
	}
}

func sidePanelTabJS() string {
	js := strings.TrimPrefix(sidePanelTabScript, `<script id="agy-side-panel-tab">`)
	return strings.TrimSuffix(js, `</script>`)
}

// The patched side panel wraps its active tab in __agyPaneTab with the panel's
// open state, and renders as before when the helper is missing. Opening a
// conversation takes the tab for the URL only from an open panel.
func TestSidePanelPatchesRender(t *testing.T) {
	body := applyOnly(t, "side-panel-closed-file-viewer", sidePanelBodyV231, Options{})
	legacy := applyOnly(t, "side-panel-closed-file-viewer-legacy", sidePanelBodyLegacyV231, Options{})
	nav := applyOnly(t, "conversation-nav-tab-open-pane", conversationNavTabV231, Options{})
	if strings.Contains(nav, "!f||") {
		t.Fatalf("the desktop layout still skips the open check:\n%s", nav)
	}

	runNode(t, `"use strict";
const assert = require("assert");
global.window = global;
const B = { createElement: (type, props, ...children) => ({ type, props, children }) };
const i0c = "i0c", H0c = "H0c", cGc = "cGc", dGc = "dGc";
const a = { kind: "cascadeId", cascadeId: "A" };
const fa = { id: "file__x.pdf", content: { type: "fileView" } }, X = fa;
function body(fa, ea) { return `+body+`; }
function legacy(R, P) { var C = [], H = null, O = "o", Q = P; return [`+legacy+`]; }

for (const helper of [undefined, function Helper() {}]) {
  window.__agyPaneTab = helper;
  const panel = body(fa, false).children[0];
  if (!helper) {
    assert.strictEqual(panel.props.className, "h-full w-full");
    assert.strictEqual(panel.children[0].type, i0c);
  } else {
    assert.strictEqual(panel.type, helper);
    assert.strictEqual(panel.props.key, fa.id);
    assert.strictEqual(panel.props.R, B);
    assert.strictEqual(panel.props.treeId, a);
    assert.strictEqual(panel.props.tab, fa);
    assert.strictEqual(panel.props.open, false);
    assert.deepStrictEqual(panel.props.idle, { type: H0c, props: { treeId: a, isPaneOpen: false }, children: [] });
    assert.strictEqual(panel.children[0].children[0].type, i0c);
  }
  assert.deepStrictEqual(body(undefined, true).children[0], { type: H0c, props: { treeId: a, isPaneOpen: true }, children: [] });

  const old = legacy(true, fa)[1].children[0];
  if (!helper) {
    assert.strictEqual(old.children[0].type, cGc);
  } else {
    assert.strictEqual(old.type, helper);
    assert.strictEqual(old.props.open, true);
    assert.strictEqual(old.props.idle, null);
    assert.strictEqual(old.children[0].children[0].type, cGc);
  }
  assert.strictEqual(legacy(false, undefined)[1].children[0], undefined);
}

function qp(k, q) { return !!k.sidePanel.conversationPanes[q]?.isPaneOpen; }
function navTab(k, l, m, d, f) { `+nav+` return t; }
const panes = (open) => ({ conversationPanes: { A: { isPaneOpen: open, activeTabId: "old" } } });
const store = (open) => ({ sidePanel: panes(open), sidePanelV2: { conversationPanes: { A: { activeTabId: "file__x.pdf" } } } });
for (const mobile of [false, true]) {
  assert.strictEqual(navTab(store(false), undefined, "A", true, mobile), undefined);
  assert.strictEqual(navTab(store(true), undefined, "A", true, mobile), "file__x.pdf");
  assert.strictEqual(navTab(store(true), undefined, "A", false, mobile), "old");
}
`)
}

// __agyPaneTab holds back a file or artifact viewer in a closed panel until
// the panel opens in that conversation, keeps it while the panel closes, and
// takes the tab out of the URL once the panel has closed.
func TestSidePanelTabHelper(t *testing.T) {
	runNode(t, `"use strict";
const assert = require("assert");
global.window = global;
// Timers fire only when the test says so.
const timers = new Map();
let timerSeq = 0;
global.setTimeout = (fn, ms) => { timers.set(++timerSeq, { fn, ms }); return timerSeq; };
global.clearTimeout = (id) => { timers.delete(id); };
function flushTimers() {
  for (const [id, t] of [...timers]) { timers.delete(id); assert.strictEqual(t.ms, 700); t.fn(); }
}
`+sidePanelTabJS()+`
// A component with hooks, rendered again until its effects stop setting state.
function host(Comp) {
  const slots = [], cleanups = [];
  let cursor = 0, pending = [], dirty = false;
  const R = {
    useState(init) {
      const i = cursor++;
      if (!(i in slots)) slots[i] = { v: init };
      const s = slots[i];
      return [s.v, (v) => { if (s.v !== v) { s.v = v; dirty = true; } }];
    },
    useRef(init) {
      const i = cursor++;
      if (!(i in slots)) slots[i] = { current: init };
      return slots[i];
    },
    useEffect(fn, deps) {
      const i = cursor++;
      const prev = slots[i];
      if (!prev || deps.some((d, j) => !Object.is(d, prev[j]))) { slots[i] = deps; pending.push([i, fn]); }
    },
  };
  const render = (props) => {
    for (let n = 0; n < 10; n++) {
      cursor = 0; pending = []; dirty = false;
      const out = Comp(Object.assign({ R }, props));
      pending.forEach(([i, fn]) => { if (cleanups[i]) cleanups[i](); cleanups[i] = fn(); });
      if (!dirty) return out;
    }
    throw new Error("the component keeps setting state");
  };
  render.unmount = () => cleanups.forEach((c) => c && c());
  return render;
}

const navs = [];
window.__agyRouter = {
  state: { location: { pathname: "/c/A", search: { project: "p", tab: "file__x.pdf" } } },
  navigate(o) {
    navs.push(o);
    assert.strictEqual(o.replace, true);
    this.state.location.search = o.search(this.state.location.search);
  },
};
const A = { kind: "cascadeId", cascadeId: "A" }, Bc = { kind: "cascadeId", cascadeId: "B" };
const pdf = { id: "file__x.pdf", content: { type: "fileView" } };
const plan = { id: "artifact__plan.md", content: { type: "artifactView" } };
const term = { id: "terminal", content: { type: "terminal" } };
const IDLE = { idle: true }, VIEW = { view: true };
const props = (treeId, tab, open, idle) => ({ treeId, tab, open, idle: idle === undefined ? IDLE : idle, children: VIEW });

let r = host(window.__agyPaneTab);
assert.strictEqual(r(props(A, pdf, false)), IDLE, "a closed panel loaded the file");
assert.strictEqual(r(props(A, pdf, true)), VIEW, "an open panel did not show the file");
assert.strictEqual(r(props(A, pdf, false)), VIEW, "the file vanished as the panel closed");
assert.strictEqual(navs.length, 0, "the URL changed while the panel was closing");
flushTimers();
assert.strictEqual(navs.length, 1);
assert.deepStrictEqual(window.__agyRouter.state.location.search, { project: "p" });
assert.strictEqual(r(props(Bc, pdf, false)), IDLE, "another conversation's closed panel loaded the same file");
assert.strictEqual(r(props(A, pdf, false)), IDLE, "returning to the conversation loaded the file");
assert.strictEqual(navs.length, 1, "a conversation change cleared the URL");

// Mounted in an open panel, then switched to another conversation's open panel.
r = host(window.__agyPaneTab);
window.__agyRouter.state.location.search = { tab: "file__x.pdf" };
assert.strictEqual(r(props(A, pdf, true)), VIEW);
assert.strictEqual(r(props(Bc, pdf, true)), VIEW);
assert.strictEqual(navs.length, 1);
assert.strictEqual(r(props(Bc, pdf, false)), VIEW);
flushTimers();
assert.strictEqual(navs.length, 2);
assert.deepStrictEqual(window.__agyRouter.state.location.search, {});

// Closing when the URL has no tab leaves the URL alone.
r = host(window.__agyPaneTab);
r(props(A, pdf, true));
r(props(A, pdf, false));
flushTimers();
assert.strictEqual(navs.length, 2);

// A panel that opens again, a conversation change, the panel going away or a
// new page before the delay keep the URL.
const closeThen = (after) => {
  window.__agyRouter.state.location = { pathname: "/c/A", search: { tab: pdf.id } };
  const h = host(window.__agyPaneTab);
  h(props(A, pdf, true));
  h(props(A, pdf, false));
  after(h);
  flushTimers();
  assert.strictEqual(navs.length, 2);
  assert.deepStrictEqual(window.__agyRouter.state.location.search, { tab: pdf.id });
};
closeThen((h) => h(props(A, pdf, true)));
closeThen((h) => h(props(Bc, pdf, false)));
closeThen((h) => h.unmount());
closeThen(() => { window.__agyRouter.state.location.pathname = "/c/B"; });
window.__agyRouter.state.location = { pathname: "/c/A", search: {} };

// Artifacts are held back too, and the legacy panel renders nothing.
assert.strictEqual(host(window.__agyPaneTab)(props(A, plan, false, null)), null);
r = host(window.__agyPaneTab);
window.__agyRouter.state.location.search = { tab: plan.id };
r(props(A, plan, true));
assert.strictEqual(r(props(A, plan, false)), VIEW);
flushTimers();
assert.strictEqual(navs.length, 3);
assert.deepStrictEqual(window.__agyRouter.state.location.search, {});

// Other tabs keep their state, so they render as before. The selected
// terminal lives only in the URL, so closing the panel on it keeps the tab.
assert.strictEqual(host(window.__agyPaneTab)(props(A, term, false)), VIEW);
assert.strictEqual(host(window.__agyPaneTab)(props(A, undefined, false)), VIEW);
r = host(window.__agyPaneTab);
window.__agyRouter.state.location.search = { tab: term.id };
assert.strictEqual(r(props(A, term, true)), VIEW);
assert.strictEqual(r(props(A, term, false)), VIEW);
flushTimers();
assert.strictEqual(navs.length, 3, "closing on a terminal cleared the URL");
assert.deepStrictEqual(window.__agyRouter.state.location.search, { tab: term.id });

// Without the router the panel still closes.
delete window.__agyRouter;
r = host(window.__agyPaneTab);
r(props(A, pdf, true));
assert.strictEqual(r(props(A, pdf, false)), VIEW);
flushTimers();
`)
}

// Before 2.30 the bundle never asks whether the panel is open, so there is
// nothing to rewrite and the patch must not report itself missing.
func TestConversationNavTabNotNeededWithoutOpenCheck(t *testing.T) {
	v229 := `else{g=f?.focusedId??g;var l=c?h.sidePanelV2.conversationPanes[g]?.activeTabId:h.sidePanel.conversationPanes[g]?.activeTabId;a({to:"/c/$cascadeId"`
	out, report := Apply(MainJS, []byte(v229), Options{})
	for _, r := range report {
		if r.ID == "conversation-nav-tab-open-pane" && r.Status != StatusNotNeeded {
			t.Fatalf("conversation-nav-tab-open-pane on a bundle without the open check: status %v", r.Status)
		}
	}
	if string(out) != v229 {
		t.Fatalf("the navigation changed:\n%s", out)
	}
}

// The geometry tracer records touch screens only. On a desktop it would read
// the layout of every element each frame after a focus change.
func TestDebugTracerSkipsDesktop(t *testing.T) {
	js := strings.TrimPrefix(mobileDebug, `<script id="agy-debug">`)
	src, err := json.Marshal(strings.TrimSuffix(js, `</script>`))
	if err != nil {
		t.Fatal(err)
	}
	runNode(t, `"use strict";
const assert = require("assert");
for (const coarse of [false, true]) {
  const hooks = [];
  const win = {
    visualViewport: { addEventListener: (name) => hooks.push("vv:" + name) },
    matchMedia: (q) => ({ matches: q === "(pointer: coarse)" ? coarse : false }),
    addEventListener: (name) => hooks.push(name),
  };
  const run = new Function("window", "setInterval", "document", "navigator", "screen", "performance", "fetch", "requestAnimationFrame", `+string(src)+`);
  run(win, () => hooks.push("interval"), {}, {}, {}, { now: () => 0 }, () => {}, () => 0);
  if (coarse) assert.ok(hooks.includes("vv:resize") && hooks.includes("focusin") && hooks.includes("interval"), "a touch screen is not traced: " + hooks);
  else assert.deepStrictEqual(hooks, [], "a desktop is traced");
}
`)
}
