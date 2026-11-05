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
global.window = global;
global.addEventListener = (name, fn) => { (listeners[name] = listeners[name] || []).push(fn); };
global.document = { querySelector: () => (findBarOpen ? {} : null) };
global.MutationObserver = class { constructor(fn) { observer = fn; } observe() {} };
Object.defineProperty(global, "performance", { value: { now: () => now } });
new Function(fs.readFileSync(process.argv[1], "utf8"))();

const fire = (name, event) => (listeners[name] || []).forEach(fn => fn(event));
const viewport = () => {
  const el = {
    scrollTop: 9000,
    scrollHeight: 10000,
    getAttribute: () => "autoscroll-viewport",
    matches: () => true,
    closest: () => el,
  };
  fire("scroll", { target: el });
  return el;
};
const removeRows = el => observer([{ target: el, removedNodes: [{}] }]);
const collapse = el => { el.scrollHeight = 1000; el.scrollTop = 300; removeRows(el); el.scrollHeight = 10000; };
const prompt = { isContentEditable: true, closest: () => null };
const body = { closest: () => null };
const settle = () => { now += 2000; };

// A row swapped for one of the same height says nothing.
{
  settle();
  const el = viewport();
  removeRows(el);
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
}

// A collapse with no input is spontaneous, for that conversation and only for a moment.
{
  settle();
  const el = viewport();
  collapse(el);
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true);
  assert.strictEqual(window.__agyIsSpontaneousScroll(viewport()), false);
  now += 600;
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), false);
}

// The position moving is enough: the content can already be back by the time anything looks.
{
  settle();
  const el = viewport();
  el.scrollTop = 300;
  removeRows(el);
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true);
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
  fire(end, { target: body });
  assert.strictEqual(window.__agyIsSpontaneousScroll(el), true, "after " + end);
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
