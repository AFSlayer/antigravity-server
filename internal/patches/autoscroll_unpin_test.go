package patches

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func applyUnpinPatch(t *testing.T, src string) string {
	t.Helper()
	for _, p := range All() {
		if p.ID == "autoscroll-unpin-needs-input" {
			out := p.FindRe.ReplaceAllStringFunc(src, func(m string) string {
				groups := p.FindRe.FindSubmatch([]byte(m))
				return p.ReplaceMatch(groups, Options{})
			})
			if out == src {
				t.Fatalf("anchor did not match %q", src)
			}
			return out
		}
	}
	t.Fatal("patch is not registered")
	return ""
}

func TestAutoscrollUnpinNeedsInputShapes(t *testing.T) {
	cases := []struct {
		name, src string
		want      []string
	}{
		{
			name: "2.29 keeps the follow guard and the smooth-scroll cleanup",
			src:  `var J=()=>{var L=n.current;if(L){var M=L.scrollTop,N=F(),P=Math.max(0,L.scrollHeight-L.clientHeight),O=P<v.current&&M>=P-1,Q=r.current;M<t.current&&!O?(r.current=!1,tT.delete(L)):N&&!C.current&&(r.current=!0);`,
			want: []string{
				`M<t.current&&!O?(r.current&&!C.current&&window.__agyIsSpontaneousScroll?.(L)?L.scrollTo({top:L.scrollHeight,behavior:"instant"}):(r.current=!1,tT.delete(L))):N&&!C.current&&(r.current=!0)`,
			},
		},
		{
			name: "a line break can land at any token",
			src:  "var J=()=>{var L=\nn.current;if(L){var M=L.scrollTop,N=F(),O=!1,Q=r.current;M<\nt.current&&!O?(r.current=\n!1,\ntT.delete(L)):\nN&&!C.current&&\n(r.current=!0);",
			want: []string{
				`(r.current&&!C.current&&window.__agyIsSpontaneousScroll?.(L)?L.scrollTo({top:L.scrollHeight,behavior:"instant"}):(r.current=!1,tT.delete(L))):`,
			},
		},
		{
			name: "2.23 has neither the follow guard nor the cleanup",
			src:  `var H=()=>{var G=m.current;if(G){var K=G.scrollTop,J=C(),L=G.scrollHeight<u.current,M=p.current;K<t.current&&!L?p.current=!1:J&&(p.current=!0);`,
			want: []string{
				`K<t.current&&!L?(p.current&&window.__agyIsSpontaneousScroll?.(G)?G.scrollTo({top:G.scrollHeight,behavior:"instant"}):(p.current=!1)):J&&(p.current=!0)`,
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := applyUnpinPatch(t, c.src)
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("patched source is missing\n%s\ngot\n%s", w, out)
				}
			}
		})
	}
}

// Runs the patched hook on the same scroll events as the tracker, in the order the page does: the tracker's capturing
// listener first, then the hook, which then asks the tracker about what it just saw.
func TestPatchedHookAndTrackerTogether(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	hook := applyUnpinPatch(t, `var J=()=>{var L=n.current;if(L){var M=L.scrollTop,N=F(),P=Math.max(0,L.scrollHeight-L.clientHeight),O=P<v.current&&M>=P-1,Q=r.current;M<t.current&&!O?(r.current=!1,tT.delete(L)):N&&!C.current&&(r.current=!0);Q!==r.current&&y.current?.(r.current);H(M);t.current=M;v.current=P}};`)
	script := scrollInputTracker
	script = script[strings.Index(script, ">")+1:]
	script = script[:strings.LastIndex(script, "</script>")]
	harness := `
const assert = require("assert");
const fs = require("fs");
const listeners = {};
let clock = 100000;
let observer;
let frames = [];
global.window = global;
global.addEventListener = (name, fn) => { (listeners[name] = listeners[name] || []).push(fn); };
global.document = { querySelector: () => null };
global.MutationObserver = class { constructor(fn) { observer = fn; } observe() {} };
global.requestAnimationFrame = fn => { frames.push(fn); };
global.setTimeout = fn => { frames.push(fn); };
Object.defineProperty(global, "performance", { value: { now: () => clock } });
new Function(fs.readFileSync(process.argv[1], "utf8"))();
const hookSource = fs.readFileSync(process.argv[2], "utf8");

const fire = (name, event) => (listeners[name] || []).forEach(fn => fn(event));
const frame = () => { while (frames.length > 0) { const due = frames; frames = []; due.forEach(fn => fn()); } };
const ROWS = { nodeType: 1, tagName: "DIV", textContent: "x".repeat(14382), matches: () => false, querySelectorAll: () => [], getElementsByTagName: () => ({ length: 143 }) };
const ICON = { nodeType: 1, tagName: "SPAN", textContent: "x".repeat(7), matches: () => false, querySelectorAll: () => [], getElementsByTagName: () => ({ length: 1 }) };

// A conversation followed at the bottom, with the hook's own state.
const conversation = () => {
  clock += 5000;
  const el = {
    scrollTop: 9000, scrollHeight: 10000, clientHeight: 1000,
    matches: () => true, closest: () => el,
    scrollTo(o) { this.scrollTop = Math.min(o.top, this.scrollHeight - this.clientHeight); },
  };
  const n = { current: el }, r = { current: true }, t = { current: 9000 }, v = { current: 9000 };
  const C = { current: false }, y = { current: null }, H = () => {}, tT = new WeakMap();
  const F = () => el.scrollHeight - el.clientHeight - el.scrollTop <= 20;
  const J = new Function("n", "r", "t", "v", "C", "y", "H", "tT", "F", hookSource + "return J;")(n, r, t, v, C, y, H, tT, F);
  const event = () => { fire("scroll", { target: el }); J(); };
  event();
  return { el, r, t, event, remove: node => observer([{ target: el, removedNodes: [node] }]) };
};
const onConversation = c => ({ target: { closest: () => c.el } });

// Rows go and come back before the scroll event: back at the bottom, still following.
{
  const c = conversation();
  c.remove(ROWS);
  c.el.scrollHeight = 1000;
  c.el.scrollTop = 0;
  c.el.scrollHeight = 10000;
  c.event();
  assert.strictEqual(c.el.scrollTop, 9000);
  assert.strictEqual(c.r.current, true);
}

// The rows coming back push the position down a little, as Safari was seen to.
{
  const c = conversation();
  c.remove(ROWS);
  c.el.scrollHeight = 1000;
  c.el.scrollTop = 0;
  frame();
  c.el.scrollHeight = 10100;
  c.el.scrollTop = 98;
  c.event();
  assert.strictEqual(c.el.scrollTop, 9100);
  assert.strictEqual(c.r.current, true);
}

// The position comes back to one pixel short of where it was: the hook still sees a fall and asks.
{
  const c = conversation();
  c.remove(ROWS);
  c.el.scrollHeight = 1000;
  c.el.scrollTop = 0;
  frame();
  c.el.scrollHeight = 10000;
  c.el.scrollTop = 8999;
  c.event();
  assert.strictEqual(c.el.scrollTop, 9000);
  assert.strictEqual(c.r.current, true);
}

// The bundle moved its own idea of the last position on ahead while following, and the position comes back past where
// the tracker last saw it but short of that. The hook sees a fall and asks on the same event that ended the collapse.
{
  const c = conversation();
  c.remove(ROWS);
  c.el.scrollHeight = 1000;
  c.el.scrollTop = 0;
  frame();
  c.t.current = 9500;
  c.el.scrollHeight = 10300;
  c.el.scrollTop = 9150;
  c.event();
  assert.strictEqual(c.el.scrollTop, 9300);
  assert.strictEqual(c.r.current, true);
}

// A status icon swapped in the frame of a scroll with no input: the reader stays and following stops.
{
  const c = conversation();
  c.remove(ICON);
  c.el.scrollTop -= 800;
  c.event();
  assert.strictEqual(c.el.scrollTop, 8200);
  assert.strictEqual(c.r.current, false);
}

// A small shrink, followed back to the bottom, and then a scroll with no input.
{
  const c = conversation();
  c.remove(ICON);
  c.el.scrollHeight -= 30;
  c.el.scrollTop -= 30;
  frame();
  c.event();
  assert.strictEqual(c.r.current, true, "the bundle handles the clamp itself");
  c.el.scrollHeight = 10030;
  c.el.scrollTop = 9030;
  c.event();
  c.el.scrollTop -= 100;
  c.event();
  assert.strictEqual(c.el.scrollTop, 8930);
  assert.strictEqual(c.r.current, false);
}

// The same collapse right after a wheel: that was the reader, so it stands.
{
  const c = conversation();
  fire("wheel", onConversation(c));
  c.remove(ROWS);
  c.el.scrollHeight = 1000;
  c.el.scrollTop = 0;
  c.el.scrollHeight = 10000;
  c.event();
  assert.strictEqual(c.el.scrollTop, 0);
  assert.strictEqual(c.r.current, false);
}

// A scroll with nothing removed stops following, as it always did.
{
  const c = conversation();
  c.el.scrollTop -= 800;
  c.event();
  assert.strictEqual(c.el.scrollTop, 8200);
  assert.strictEqual(c.r.current, false);
}
process.exit(0);
`
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "tracker.js")
	hookPath := filepath.Join(dir, "hook.js")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hookPath, []byte(hook), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "-e", harness, "--", scriptPath, hookPath).CombinedOutput(); err != nil {
		t.Fatalf("hook and tracker harness failed: %v\n%s", err, out)
	}
}

// The same with the hook 2.31 ships, which tells a fall from the change in position itself and reads its
// followDisabled ref before following again.
func TestPatchedHook231AndTrackerTogether(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	hook := applyUnpinPatch(t, `var G=()=>{var H=m.current;if(H){var J=H.scrollTop,K=Math.max(0,H.scrollHeight-H.clientHeight),M=K-J<=a,L=K<t.current&&J>=K-1,N=J-q.current,P=N>0&&N<=K-t.current,O=p.current;N<0&&!L?(p.current=!1,dU.delete(H)):!M||y.current||P||(p.current=!0);O!==p.current&&(z(),w.current?.(p.current));C(J);q.current=J;t.current=K}};`)
	script := scrollInputTracker
	script = script[strings.Index(script, ">")+1:]
	script = script[:strings.LastIndex(script, "</script>")]
	harness := `
const assert = require("assert");
const fs = require("fs");
const listeners = {};
let clock = 100000;
let observer;
let frames = [];
global.window = global;
global.addEventListener = (name, fn) => { (listeners[name] = listeners[name] || []).push(fn); };
global.document = { querySelector: () => null };
global.MutationObserver = class { constructor(fn) { observer = fn; } observe() {} };
global.requestAnimationFrame = fn => { frames.push(fn); };
global.setTimeout = fn => { frames.push(fn); };
Object.defineProperty(global, "performance", { value: { now: () => clock } });
new Function(fs.readFileSync(process.argv[1], "utf8"))();
const hookSource = fs.readFileSync(process.argv[2], "utf8");

const fire = (name, event) => (listeners[name] || []).forEach(fn => fn(event));
const frame = () => { while (frames.length > 0) { const due = frames; frames = []; due.forEach(fn => fn()); } };
const ROWS = { nodeType: 1, tagName: "DIV", textContent: "x".repeat(14382), matches: () => false, querySelectorAll: () => [], getElementsByTagName: () => ({ length: 143 }) };
const ICON = { nodeType: 1, tagName: "SPAN", textContent: "x".repeat(7), matches: () => false, querySelectorAll: () => [], getElementsByTagName: () => ({ length: 1 }) };

// A conversation followed at the bottom, with the hook's own state.
const conversation = followDisabled => {
  clock += 5000;
  const el = {
    scrollTop: 9000, scrollHeight: 10000, clientHeight: 1000,
    matches: () => true, closest: () => el,
    scrollTo(o) { this.scrollTop = Math.min(o.top, this.scrollHeight - this.clientHeight); },
  };
  const m = { current: el }, p = { current: true }, t = { current: 9000 }, q = { current: 9000 };
  const y = { current: followDisabled }, w = { current: null }, z = () => {}, C = () => {}, dU = new WeakMap();
  const G = new Function("m", "a", "t", "q", "p", "dU", "y", "z", "w", "C", hookSource + "return G;")(m, 20, t, q, p, dU, y, z, w, C);
  const event = () => { fire("scroll", { target: el }); G(); };
  event();
  return { el, p, event, remove: node => observer([{ target: el, removedNodes: [node] }]) };
};

// Rows go and come back before the scroll event: back at the bottom, still following.
{
  const c = conversation(false);
  c.remove(ROWS);
  c.el.scrollHeight = 1000;
  c.el.scrollTop = 0;
  c.el.scrollHeight = 10000;
  c.event();
  assert.strictEqual(c.el.scrollTop, 9000);
  assert.strictEqual(c.p.current, true);
}

// A status icon swapped in the frame of a scroll with no input: the reader stays and following stops.
{
  const c = conversation(false);
  c.remove(ICON);
  c.el.scrollTop -= 800;
  c.event();
  assert.strictEqual(c.el.scrollTop, 8200);
  assert.strictEqual(c.p.current, false);
}

// With following turned off, the same collapse is left where it is.
{
  const c = conversation(true);
  c.remove(ROWS);
  c.el.scrollHeight = 1000;
  c.el.scrollTop = 0;
  c.el.scrollHeight = 10000;
  c.event();
  assert.strictEqual(c.el.scrollTop, 0);
  assert.strictEqual(c.p.current, false);
}
frame();
`
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "tracker.js")
	hookPath := filepath.Join(dir, "hook.js")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hookPath, []byte(hook), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "-e", harness, "--", scriptPath, hookPath).CombinedOutput(); err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
}
