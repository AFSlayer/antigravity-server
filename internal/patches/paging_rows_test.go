package patches

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The component 2.32 restores the reader's rows with, as its bundle has it.
const v232RowsKept = `var n_c=class extends B.Component{getSnapshotBeforeUpdate(a){var b=this.props,c=b.disabled,d=b.getAnchorCandidates,f=b.shouldYield,g=b.viewportRef.current;if(a.anchorKey===b.anchorKey||c||!g||f?.())var h=null;else{c=d();g=g.getBoundingClientRect().top;a=[];let k;b=!0;for(let l of c){let {top:m,bottom:n,width:p,height:q}=l.getBoundingClientRect();if(p===0&&q===0)continue;c={element:l,offset:m-g};d=b;b=!1;if(n<=g)k=c;else if(d)h=c;else if(a.push(c),a.length===3)break}h&&a.push(h);a.length===0&&k&&a.push(k);h=a}return h}componentDidUpdate(a,b,c){a=this.props.viewportRef.current;c=c?.find(({element:d})=>d.isConnected);a&&c&&(c=c.element.getBoundingClientRect().top-a.getBoundingClientRect().top-c.offset,Math.abs(c)>=.5&&(a.scrollTop+=c))}render(){return this.props.children}};`

func rowsKeptPatch(t *testing.T) Patch {
	t.Helper()
	for _, p := range All() {
		if p.ID == "paging-keeps-rows-after-render" {
			return p
		}
	}
	t.Fatal("patch is not registered")
	return Patch{}
}

func TestRowsKeptPatchHandsTheMoveToTheTracker(t *testing.T) {
	got := applyOnly(t, "paging-keeps-rows-after-render", v232RowsKept, Options{})
	want := `componentDidUpdate(a,b,c){a=this.props.viewportRef.current;c=c?.find(({element:d})=>d.isConnected);window.__agyKeepRows?(window.__agyRowsDrawn?.(a),a&&c&&window.__agyKeepRows(a,c.element,c.offset)):a&&c&&(c=c.element.getBoundingClientRect().top-a.getBoundingClientRect().top-c.offset,Math.abs(c)>=.5&&(a.scrollTop+=c))}render(){`
	if !strings.Contains(got, want) {
		t.Errorf("got\n%s\nwant it to contain\n%s", got, want)
	}
	if !strings.HasPrefix(got, v232RowsKept[:strings.Index(v232RowsKept, "componentDidUpdate")]) {
		t.Error("the snapshot before the update was changed")
	}
}

// The minifier wraps lines after commas, semicolons, closing braces and operators.
func TestRowsKeptPatchMatchesALineBreakAtAnyToken(t *testing.T) {
	p := rowsKeptPatch(t)
	src := v232RowsKept[strings.Index(v232RowsKept, "componentDidUpdate"):strings.Index(v232RowsKept, "render(){")]
	compact := func(s string) string { return strings.Join(strings.Fields(s), "") }
	base := p.FindRe.ReplaceAllString(src, p.Replace)
	if base == src {
		t.Fatal("the patch did not apply")
	}
	positions := lineBreakPositions(src)
	if len(positions) < 10 {
		t.Fatalf("only %d break positions", len(positions))
	}
	for _, pos := range positions {
		broken := src[:pos] + "\n" + src[pos:]
		if out := p.FindRe.ReplaceAllString(broken, p.Replace); compact(out) != compact(base) {
			t.Errorf("a line break at offset %d is not matched: %q", pos, src[max(0, pos-30):min(len(src), pos+30)])
		}
	}
}

// A bundle that restores the rows some other way is reported, and one from before 2.32 needs nothing.
func TestRowsKeptPatchReportsAnUnknownForm(t *testing.T) {
	changed := strings.Replace(v232RowsKept, "Math.abs(c)>=.5&&(a.scrollTop+=c)", "a.scrollBy(0,c)", 1)
	if _, status := applyWithStatus(t, "paging-keeps-rows-after-render", changed, Options{}); status != StatusMissing {
		t.Errorf("a changed restore: status %v, want %v", status, StatusMissing)
	}
	older := `var lVc=a=>{var b=a.viewportRef;return{snapshot:a,tryRestoreSnapshot:b}};`
	if _, status := applyWithStatus(t, "paging-keeps-rows-after-render", older, Options{}); status != StatusNotNeeded {
		t.Errorf("a bundle without the component: status %v, want %v", status, StatusNotNeeded)
	}
}

// Runs the patched method. With the tracker it hands over the row and where it was, without it the bundle's own move
// is made, and an update that kept no row does neither. The tracker is told of every drawing, where it has a hook to
// be told by.
func TestRowsKeptPatchRuns(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	patched := applyOnly(t, "paging-keeps-rows-after-render", v232RowsKept, Options{})
	harness := `
const assert = require("assert");
const B = { Component: class {} };
globalThis.window = {};
const Kept = new Function("B", process.argv[1] + "return n_c;")(B);
const rect = top => ({ getBoundingClientRect: () => ({ top }) });
const viewport = Object.assign(rect(100), { scrollTop: 500 });
const kept = new Kept();
kept.props = { viewportRef: { current: viewport } };
const gone = Object.assign(rect(0), { isConnected: false });
const row = Object.assign(rect(2460), { isConnected: true });

// Without the tracker the row is put back by the bundle's own arithmetic.
kept.componentDidUpdate({}, null, [{ element: gone, offset: 0 }, { element: row, offset: 60 }]);
assert.strictEqual(viewport.scrollTop, 2800);
kept.componentDidUpdate({}, null, null);
assert.strictEqual(viewport.scrollTop, 2800, "an update that kept no row moves nothing");

// With it, the tracker gets the first row still in the document and makes the move itself.
const calls = [];
window.__agyKeepRows = (...args) => calls.push(args);
kept.componentDidUpdate({}, null, [{ element: gone, offset: 0 }, { element: row, offset: 60 }]);
assert.strictEqual(calls.length, 1);
assert.strictEqual(calls[0][0], viewport);
assert.strictEqual(calls[0][1], row);
assert.strictEqual(calls[0][2], 60);
assert.strictEqual(viewport.scrollTop, 2800, "the bundle's own move is not made as well");
kept.componentDidUpdate({}, null, null);
kept.componentDidUpdate({}, null, [{ element: gone, offset: 0 }]);
assert.strictEqual(calls.length, 1, "nothing to hand over without a row");

// Every drawing is told to a tracker that asks to be, before the row is handed over and when there is none to hand.
const drawn = [];
window.__agyRowsDrawn = el => drawn.push([el, calls.length]);
kept.componentDidUpdate({}, null, null);
kept.componentDidUpdate({}, null, [{ element: row, offset: 60 }]);
assert.deepStrictEqual(drawn, [[viewport, 1], [viewport, 1]]);
assert.strictEqual(calls.length, 2);
`
	if out, err := exec.Command(node, "-e", harness, patched).CombinedOutput(); err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
}

// Drives the tracker's side with a conversation whose rows shift the way the app's do: older rows are added above, the
// bundle's move is made, and the tables in them are drawn again shorter before the commit is over.
func TestKeepRowsPutsTheRowBackAfterTheSecondDrawing(t *testing.T) {
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
const events = [];
global.window = global;
global.addEventListener = (name, fn) => { (listeners[name] = listeners[name] || []).push(fn); };
global.dispatchEvent = e => { events.push(e); };
global.CustomEvent = class { constructor(type, init) { this.type = type; this.detail = init && init.detail; } };
global.document = { querySelector: () => null };
global.MutationObserver = class { observe() {} };
global.requestAnimationFrame = fn => { frames.push(fn); };
global.setTimeout = fn => { frames.push(fn); };
Object.defineProperty(global, "performance", { value: { now: () => now } });
global.matchMedia = () => ({ matches: true });
global.navigator = { userAgent: process.argv[2] || "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X)", platform: "iPhone", maxTouchPoints: 5 };
new Function(fs.readFileSync(process.argv[1], "utf8"))();
const fire = (name, event) => (listeners[name] || []).forEach(fn => fn(event));
const frame = () => { const due = frames; frames = []; due.forEach(fn => fn()); };
const drain = () => { for (let k = 0; k < 60 && frames.length > 0; k++) frame(); };
const microtasks = () => new Promise(resolve => setImmediate(resolve));

// A viewport 100 px down the page, and a row whose place in the conversation the test moves.
const conversation = () => {
  const el = { scrollTop: 300, scrollHeight: 9000, clientHeight: 666, style: { overflowY: "" }, matches: () => true, closest: () => el,
    getBoundingClientRect: () => ({ top: 100 }) };
  // A row that is not laid out has no size, and reads as at the top of the page.
  const row = { isConnected: true, y: 360, laidOut: true, getBoundingClientRect: () => (row.laidOut ? { top: 100 + row.y - el.scrollTop, width: 400, height: 80 } : { top: 0, width: 0, height: 0 }) };
  return { el, row };
};
const kept = () => events.filter(e => e.type === "agy-rows-kept").map(e => e.detail);

(async () => {
  // Rows 4401 px tall arrive above a reader whose row was 60 px down the viewport.
  {
    const { el, row } = conversation();
    row.y += 4401;
    window.__agyKeepRows(el, row, 60);
    assert.strictEqual(el.scrollTop, 4701, "the bundle's move");
    // Their tables are drawn again 350 px shorter before the commit is over.
    row.y -= 350;
    await microtasks();
    assert.strictEqual(el.scrollTop, 4351, "made again after the second drawing");
    assert.deepStrictEqual(kept(), [{ moved: 4401, again: -350 }], "and the trace says how far each went");
    // An image above loads two frames later, with the reader still where the move left them.
    frame();
    row.y += 120;
    frame();
    assert.strictEqual(el.scrollTop, 4471, "a later shift is followed too");
    // The reader scrolls, and from then on the row is theirs to move.
    el.scrollTop -= 200;
    row.y += 80;
    frame();
    frame();
    assert.strictEqual(el.scrollTop, 4271, "nothing is put back under a reader who moved");
    assert.strictEqual(frames.length, 0, "and nothing goes on checking");
  }

  // A browser that anchors the rows itself has done all of it already.
  {
    events.length = 0;
    const { el, row } = conversation();
    row.y += 4401;
    el.scrollTop += 4401;
    window.__agyKeepRows(el, row, 60);
    row.y -= 350;
    el.scrollTop -= 350;
    await microtasks();
    assert.strictEqual(el.scrollTop, 4351);
    assert.deepStrictEqual(kept(), [], "nothing moved, nothing to report");
    for (let k = 0; k < 40; k++) frame();
    assert.strictEqual(frames.length, 0, "the checks end after a few frames");
  }

  // WebKit keeps a position to whole pixels, so a row left a fraction of one off is not asked for again and again.
  {
    const { el, row } = conversation();
    let top = el.scrollTop, writes = 0;
    Object.defineProperty(el, "scrollTop", { get: () => top, set: v => { writes++; top = Math.floor(v); } });
    row.y += 1000.75;
    window.__agyKeepRows(el, row, 60);
    assert.strictEqual(el.scrollTop, 1300);
    await microtasks();
    for (let k = 0; k < 40; k++) frame();
    assert.strictEqual(writes, 1, "one move, and none for the fraction left over");
    assert.deepStrictEqual(kept().pop(), { moved: 1001, again: 0 });
    assert.strictEqual(frames.length, 0);
  }

  // The row leaves the document before the commit is over, or a frame later.
  {
    const { el, row } = conversation();
    row.y += 1000;
    window.__agyKeepRows(el, row, 60);
    row.isConnected = false;
    row.y -= 500;
    await microtasks();
    assert.strictEqual(el.scrollTop, 1300, "a row that is gone is not measured");
    assert.strictEqual(frames.length, 0);
  }

  // A row that stops being laid out in the frames after, or a conversation that is hidden, reads as nothing. Moving
  // the position by what that seems to say, frame after frame, took the reader thousands of pixels away.
  {
    const { el, row } = conversation();
    row.y += 1000;
    window.__agyKeepRows(el, row, 60);
    await microtasks();
    assert.strictEqual(el.scrollTop, 1300);
    frame();
    row.laidOut = false;
    for (let k = 0; k < 40; k++) frame();
    assert.strictEqual(el.scrollTop, 1300, "a row that is not laid out is not measured");
    assert.strictEqual(frames.length, 0, "and is not looked at again");
    row.laidOut = true;
    row.y += 1000;
    window.__agyKeepRows(el, row, 60);
    await microtasks();
    assert.strictEqual(el.scrollTop, 2300);
    frame();
    el.clientHeight = 0;
    row.y += 5000;
    for (let k = 0; k < 40; k++) frame();
    assert.strictEqual(el.scrollTop, 2300, "nor is anything in a conversation that is hidden");
    el.clientHeight = 666;
    // One that is not laid out when the bundle hands it over is no place to put anything back to.
    row.laidOut = false;
    window.__agyKeepRows(el, row, 60);
    await microtasks();
    drain();
    assert.strictEqual(el.scrollTop, 2300);
    row.laidOut = true;
  }

  // A fling still going when the rows arrive would carry on from where it was, so it is ended before the move, as
  // long as the move is more than a few rows.
  {
    const { el, row } = conversation();
    const onEl = touches => ({ target: { closest: () => el }, touches });
    fire("scroll", { target: el });
    drain();
    fire("touchstart", onEl([{}]));
    fire("touchmove", onEl([{}]));
    fire("touchend", onEl([]));
    now += 50;
    el.scrollTop -= 40;
    fire("scroll", { target: el });
    row.y += 149;
    window.__agyKeepRows(el, row, 100);
    assert.strictEqual(el.style.overflowY, "", "a move of a few rows leaves the fling alone");
    await microtasks();
    drain();
    row.y += 151;
    window.__agyKeepRows(el, row, 100);
    assert.strictEqual(el.style.overflowY, process.argv[2] ? "" : "hidden", "a longer one ends it");
    await microtasks();
    frame();
    frame();
    assert.strictEqual(el.style.overflowY, "", "for a frame");
    drain();

    // A finger still down is dragging, and is left to.
    now += 5000;
    fire("touchstart", onEl([{}]));
    fire("touchmove", onEl([{}]));
    el.scrollTop -= 40;
    fire("scroll", { target: el });
    row.y += 3000;
    window.__agyKeepRows(el, row, 100);
    assert.strictEqual(el.style.overflowY, "", "a finger down is not a fling");
    fire("touchend", onEl([]));
    await microtasks();
    drain();

    // Nor is there anything to end in a conversation that is still.
    now += 5000;
    row.y += 3000;
    window.__agyKeepRows(el, row, 100);
    assert.strictEqual(el.style.overflowY, "", "nothing to end in a still conversation");
    await microtasks();
    drain();
  }
})().catch(e => { console.error(e); process.exit(1); });
`
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "tracker.js")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "-e", harness, scriptPath).CombinedOutput(); err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
	// Another phone keeps its fling: it moves with the position there.
	android := "Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 Chrome/131.0 Mobile Safari/537.36"
	if out, err := exec.Command(node, "-e", harness, scriptPath, android).CombinedOutput(); err != nil {
		t.Fatalf("harness failed on Android: %v\n%s", err, out)
	}
}

// Rows added above the reader wait for the conversation to stop moving, as rows dropped there do, except that a fling
// at the top of the conversation has nowhere left to go. That is for a bundle that hands the tracker the move that
// keeps the rows in place: one that makes it itself is not held back.
func TestGrowWaitsForStillOrTheTop(t *testing.T) {
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
let waits = [];
global.window = global;
global.addEventListener = (name, fn) => { (listeners[name] = listeners[name] || []).push(fn); };
global.dispatchEvent = e => { waits.push(e.type); };
global.CustomEvent = class { constructor(type) { this.type = type; } };
global.document = { querySelector: () => null };
global.MutationObserver = class { observe() {} };
global.requestAnimationFrame = fn => { frames.push(fn); };
global.setTimeout = fn => { frames.push(fn); };
Object.defineProperty(global, "performance", { value: { now: () => now } });
global.matchMedia = () => ({ matches: true });
global.navigator = { userAgent: process.argv[2] || "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X)", platform: "iPhone", maxTouchPoints: 5 };
new Function(fs.readFileSync(process.argv[1], "utf8"))();
const fire = (name, event) => (listeners[name] || []).forEach(fn => fn(event));
const frame = () => { while (frames.length > 0) { const due = frames; frames = []; due.forEach(fn => fn()); } };
const el = { scrollTop: 3000, scrollHeight: 9000, clientHeight: 666, isConnected: true, style: { overflowY: "" }, matches: () => true, closest: () => el };
const onEl = touches => ({ target: { closest: () => el }, touches });
const ios = !process.argv[2];

// A bundle that moves the position for its rows itself adds them when it always did, finger down or not.
fire("touchstart", onEl([{}]));
fire("touchmove", onEl([{}]));
el.scrollTop = 2900;
fire("scroll", { target: el });
assert.strictEqual(window.__agyDeferGrow(), false, "nothing is held back for a bundle that does not hand its move over");
fire("touchend", onEl([]));
assert.deepStrictEqual(waits, []);
now += 20000;
el.scrollTop = 3000;

// One that hands the move over says so each time it draws the conversation, and is answered from then on.
window.__agyRowsDrawn(el);
fire("scroll", { target: el });
now += 5000;
assert.strictEqual(window.__agyDeferGrow(), false, "a still conversation takes its rows at once");

// A finger dragging toward the top, 600 px from it.
fire("touchstart", onEl([{}]));
fire("touchmove", onEl([{}]));
el.scrollTop = 600;
fire("scroll", { target: el });
assert.strictEqual(window.__agyDeferGrow(), ios, "a finger down");
// It lifts and the fling goes on.
fire("touchend", onEl([]));
now += 250;
el.scrollTop = 350;
fire("scroll", { target: el });
assert.strictEqual(window.__agyDeferGrow(), ios, "a fling still going");
// The fling reaches the top and bounces there.
now += 250;
el.scrollTop = -30;
fire("scroll", { target: el });
assert.strictEqual(window.__agyDeferGrow(), false, "a fling at the top has nowhere left to go");
// A finger back down at the top holds the rows again.
fire("touchstart", onEl([{}]));
assert.strictEqual(window.__agyDeferGrow(), ios, "a finger down at the top");
fire("touchend", onEl([]));
assert.deepStrictEqual(waits, ios ? ["agy-grow-wait", "agy-grow-wait"] : [], "the trace marks each wait once");

// A fling that stops short of the top: the rows come once it has been still for a moment.
now += 20000;
waits = [];
fire("touchstart", onEl([{}]));
fire("touchmove", onEl([{}]));
fire("touchend", onEl([]));
el.scrollTop = 400;
fire("scroll", { target: el });
assert.strictEqual(window.__agyDeferGrow(), ios);
frame();
now += 299;
assert.strictEqual(window.__agyDeferGrow(), ios);
now += 2;
assert.strictEqual(window.__agyDeferGrow(), false, "still for 300 ms");

// Flings run together hold the rows for 3 s and no longer, and rows being dropped keep their own count.
now += 20000;
fire("touchstart", onEl([{}]));
fire("touchmove", onEl([{}]));
fire("touchend", onEl([]));
el.scrollTop = 5000;
const grow = [], trim = [];
for (let k = 0; k < 16; k++) {
  now += 250;
  el.scrollTop -= 100;
  fire("scroll", { target: el });
  grow.push(window.__agyDeferGrow());
  if (k >= 8) trim.push(window.__agyDeferTrim());
  if (k % 6 === 5) { fire("touchstart", onEl([{}])); fire("touchmove", onEl([{}])); fire("touchend", onEl([])); }
}
assert.strictEqual(grow.indexOf(false), ios ? 12 : 0, "waits for 3 s, then lets the rows in");
assert.ok(trim.every(answer => answer === ios), "a drop asked about meanwhile has waited less");
`
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "tracker.js")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "-e", harness, scriptPath).CombinedOutput(); err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
	android := "Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 Chrome/131.0 Mobile Safari/537.36"
	if out, err := exec.Command(node, "-e", harness, scriptPath, android).CombinedOutput(); err != nil {
		t.Fatalf("harness failed on Android: %v\n%s", err, out)
	}
}

// The phone trace starts an episode for each of these, named so a jump can be matched to what the page did.
func TestTraceRecordsRowsKeptAndGrowWaits(t *testing.T) {
	for _, want := range []string{`"agy-grow-wait"`, `"agy-rows-kept"`, `"agy-trim-wait"`, `"agy-room"`} {
		if !strings.Contains(mobileDebug, want) {
			t.Errorf("the trace does not listen for %s", want)
		}
		if !strings.Contains(scrollInputTracker, want) {
			t.Errorf("the tracker does not send %s", want)
		}
	}
}

// The page the scroll tracker's room is tried on. Its conversations are laid out as the page's are: the room, which is
// padding on the one element inside the viewport, then the rows, with a position past the end cut back to the end. The
// test moves the clock, and what the tracker sends is kept in events.
const roomPage = `
const assert = require("assert");
const fs = require("fs");
const listeners = {};
let now = 100000;
let frames = [];
let timers = [];
const events = [];
const styles = [];
global.window = global;
global.addEventListener = (name, fn) => { (listeners[name] = listeners[name] || []).push(fn); };
global.dispatchEvent = e => { events.push(e); };
global.CustomEvent = class { constructor(type, init) { this.type = type; this.detail = init && init.detail; } };
global.document = { querySelector: () => null, createElement: () => ({}), head: { appendChild: el => styles.push(el) } };
const observers = [];
global.MutationObserver = class { constructor(fn) { observers.push(fn); } observe() {} };
global.requestAnimationFrame = fn => { frames.push(fn); };
global.setTimeout = (fn, ms) => { timers.push({ at: now + (ms || 0), fn }); return timers.length; };
Object.defineProperty(global, "performance", { value: { now: () => now } });
global.matchMedia = () => ({ matches: true });
const ios = !process.argv[2];
global.navigator = { userAgent: process.argv[2] || "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X)", platform: ios ? "iPhone" : "Linux", maxTouchPoints: 5 };
new Function(fs.readFileSync(process.argv[1], "utf8"))();
const fire = (name, event) => (listeners[name] || []).forEach(fn => fn(event));
// Every conversation is laid out at the start of a frame, and the page hears of a position that was cut back then or
// since the last one.
const pages = [];
const frame = () => { pages.forEach(layout => layout(true)); const due = frames; frames = []; due.forEach(fn => fn()); };
// Time passes a frame at a time, as it does for the page.
const pass = ms => { for (const end = now + ms; now < end;) { now += 16; frame(); const due = timers.filter(x => x.at <= now); timers = timers.filter(x => x.at > now); due.forEach(x => x.fn()); } };
const microtasks = () => new Promise(resolve => setImmediate(resolve));
const named = type => events.filter(e => e.type === type).map(e => e.detail);
const ROOM = 200000;

// A viewport 100 px down the page and 666 px tall. Its rows are 9000 px tall and start below the room, and the row the
// bundle keeps in place is row.y down them.
const conversation = () => {
  const older = { disabled: false, getAttribute: () => String(older.disabled) };
  const names = { "padding-top": "paddingTop", "overflow-anchor": "overflowAnchor" };
  const box = {
    style: { paddingTop: "", overflowAnchor: "", removeProperty: name => { box.style[names[name]] = ""; } },
    get offsetHeight() { return el.hidden ? 0 : el.laid + el.rows; },
  };
  // A position past the end of the conversation is cut back to it, as a browser cuts it when it lays the page out,
  // and the page hears of one that ends up somewhere it was not. A write that lands where it was asked is not told
  // to the page here.
  let cut = false;
  const end = () => Math.max(0, el.laid + el.rows - el.screen);
  const layout = tell => {
    if (el.top > end()) { el.top = end(); cut = true; }
    if (tell && cut) { cut = false; fire("scroll", { target: el }); }
  };
  pages.push(layout);
  const el = {
    top: 300, rows: 9000, screen: 666, hidden: false, isConnected: true, older, writes: 0,
    style: { overflowY: "" },
    matches: () => true, closest: () => el, querySelector: () => older,
    getBoundingClientRect: () => ({ top: 100 }),
    firstElementChild: box,
    get room() { return parseFloat(box.style.paddingTop) || 0; },
    get anchoring() { return box.style.overflowAnchor || "auto"; },
    // What the page lays out of the room: all of it, unless a test has the padding count for nothing.
    laidOut: true,
    get laid() { return el.laidOut ? el.room : 0; },
    // A conversation that is hidden has no size, and reads as at its top.
    get clientHeight() { return el.hidden ? 0 : el.screen; },
    get scrollHeight() { return el.hidden ? 0 : Math.max(el.screen, el.laid + el.rows); },
    get scrollTop() { if (el.hidden) return 0; layout(false); return el.top; },
    set scrollTop(v) {
      layout(false);
      const to = Math.min(Math.max(0, v), end());
      if (to !== v && to !== el.top) cut = true;
      el.writes++;
      el.top = to;
    },
  };
  // A row is 80 px tall where it is laid out, and nothing at all where it is not or its conversation is hidden.
  const row = {
    isConnected: true, y: 360, laidOut: true,
    getBoundingClientRect: () => {
      if (el.hidden || !row.laidOut) return { top: 0, width: 0, height: 0 };
      layout(false);
      return { top: 100 + el.laid + row.y - el.top, width: 400, height: 80 };
    },
  };
  // Where the row is on the screen, which is what the reader sees.
  const seen = () => row.getBoundingClientRect().top - 100;
  const onEl = touches => ({ target: { closest: () => el }, touches });
  // The reader scrolls: the position changes and the page hears of it. Nothing here counts as a write.
  const scrollBy = d => { el.top += d; fire("scroll", { target: el }); };
  const flick = () => { fire("touchstart", onEl([{}])); fire("touchmove", onEl([{}])); fire("touchend", onEl([])); };
  const rowsArrive = (height, offset) => { el.rows += height; row.y += height; window.__agyKeepRows(el, row, offset); };
  return { el, row, seen, onEl, scrollBy, flick, rowsArrive };
};
// The page is told that rows were taken out of a conversation, as many elements as it says.
const rowsRemoved = (el, elements) => observers.forEach(fn => fn([{ target: { closest: () => el }, removedNodes: [{ nodeType: 1, textContent: "", getElementsByTagName: () => ({ length: elements }), matches: () => false, querySelectorAll: () => [] }] }]));
// A conversation of a bundle that hands its rows over, opened and come to rest: on iOS it has its room.
const open = () => {
  const c = conversation();
  window.__agyRowsDrawn(c.el);
  c.scrollBy(0);
  pass(600);
  events.length = 0;
  c.el.writes = 0;
  return c;
};
`

// On iOS the conversation keeps room above its first row, and rows that arrive there take their height out of it, so
// the position is never written while the reader scrolls. Each case is tried on a page of its own.
func TestRoomAboveTheFirstRowTakesRowsWithoutMovingThePosition(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	script := scrollInputTracker
	script = script[strings.Index(script, ">")+1:]
	script = script[:strings.LastIndex(script, "</script>")]
	scriptPath := filepath.Join(t.TempDir(), "tracker.js")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	const android = "Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 Chrome/131.0 Mobile Safari/537.36"
	for _, c := range []struct{ name, ua, js string }{
		// Another phone's fling moves with the position, so it has no room and the position is written as before.
		{"another phone has no room", android, `
  const { el, seen, scrollBy, flick, rowsArrive } = conversation();
  window.__agyRowsDrawn(el);
  scrollBy(0);
  pass(1000);
  assert.strictEqual(el.room, 0, "no room elsewhere");
  assert.strictEqual(window.__agyGrowRadius, 0, "and rows are asked for from where the bundle asks");
  assert.strictEqual(window.__agyTopRoom, undefined, "its scrollbar is told of none");
  flick();
  scrollBy(-40);
  const at = seen();
  rowsArrive(4401, at);
  assert.strictEqual(el.top, 260 + 4401);
  assert.strictEqual(seen(), at);
  assert.strictEqual(el.style.overflowY, "", "and its fling is left to run");
  assert.strictEqual(styles.length, 0);`},

		// A bundle that moves the position for its rows itself, as those before 2.32 do, would move it for rows the room
		// had made up for. It never says it hands that move over, and the page is left as it was for it.
		{"a bundle that keeps its own rows", "", `
  assert.strictEqual(window.__agyGrowRadius, 2400, "rows are asked for from further away");
  const old = conversation();
  old.scrollBy(0);
  pass(1000);
  old.flick();
  old.scrollBy(-40);
  assert.strictEqual(old.el.room, 0, "no room is made");
  assert.strictEqual(old.el.anchoring, "auto", "the browser's anchoring is left on");
  assert.strictEqual(window.__agyDeferGrow(), false, "and its rows are not held back, fling or no fling");
  assert.strictEqual(window.__agyTopRoom(old.el), 0, "its scrollbar is told of no room");
  pass(1000);
  assert.strictEqual(old.el.room, 0);
  assert.strictEqual(old.el.writes, 0);
  assert.deepStrictEqual(["agy-room", "agy-grow-wait", "agy-fling-stop"].map(type => named(type).length), [0, 0, 0]);

  // One that hands it over says so the first time it draws the conversation. The scroll that opened the conversation
  // is over by then, and the room is made without waiting for another.
  window.__agyRowsDrawn(old.el);
  assert.strictEqual(old.el.room, 0);
  pass(416);
  assert.strictEqual(old.el.room, ROOM, "room is made at the next rest");
  assert.strictEqual(old.el.top, ROOM + 260);
  assert.strictEqual(old.el.style.setProperty, undefined, "as padding inside the conversation, with nothing set on the conversation itself");
  assert.strictEqual(styles.length, 0, "and nothing added to the page");`},

		// The conversation opens and comes to rest: the room is made, with the position moved by as much. Rows that arrive
		// during a flick come out of the room: the position is not written and the fling is not ended.
		{"rows arrive in the room", "", `
  const c = conversation();
  const { el, row, seen, scrollBy, flick, rowsArrive } = c;
  window.__agyRowsDrawn(el);
  scrollBy(0);
  assert.strictEqual(el.room, 0);
  let at = seen();
  pass(600);
  assert.strictEqual(el.room, ROOM, "room is made at rest");
  assert.strictEqual(el.top, ROOM + 300);
  assert.strictEqual(seen(), at, "with nothing on screen moving");
  assert.deepStrictEqual(named("agy-room"), [{ what: "top", by: ROOM }]);
  assert.strictEqual(el.anchoring, "auto", "the browser's anchoring is back on at rest");
  pass(200);
  assert.strictEqual(el.room, ROOM, "and stays when the position did");

  el.writes = 0;
  flick();
  scrollBy(-40);
  assert.strictEqual(el.anchoring, "none", "the browser's anchoring is off while the conversation moves");
  assert.strictEqual(window.__agyDeferGrow(), false, "rows do not wait for the fling with room for them");
  at = seen();
  rowsArrive(4401, at);
  assert.strictEqual(el.room, ROOM - 4401);
  assert.strictEqual(el.writes, 0, "no position is written");
  assert.strictEqual(el.style.overflowY, "", "and the fling goes on");
  assert.strictEqual(seen(), at);
  // Their tables are drawn again 350 px shorter before the commit is over, which goes back into the room.
  el.rows -= 350;
  row.y -= 350;
  await microtasks();
  assert.strictEqual(el.room, ROOM - 4401 + 350);
  assert.strictEqual(seen(), at);
  assert.deepStrictEqual(named("agy-rows-kept").pop(), { moved: 4401, again: -350 });
  // The fling carries on while an image above loads: the reader's own movement is theirs, and the image is made up for.
  scrollBy(-200);
  frame();
  el.rows += 120;
  row.y += 120;
  scrollBy(-180);
  frame();
  assert.strictEqual(seen(), at + 380, "the row moved by the reader's scrolling alone");
  assert.strictEqual(el.room, ROOM - 4401 + 350 - 120);
  assert.strictEqual(el.writes, 0);
  pass(600);

  // Rows the bundle drops above the reader give their height back.
  flick();
  scrollBy(300);
  at = seen();
  rowsArrive(-2000, at);
  await microtasks();
  assert.strictEqual(el.room, ROOM - 4401 + 350 - 120 + 2000);
  assert.strictEqual(seen(), at);
  assert.strictEqual(el.writes, 0);
  pass(600);
  assert.strictEqual(el.room, ROOM - 4401 + 350 - 120 + 2000, "a room this full is left alone at rest");`},

		// Rows a long way above the screen can wait: they are let in while the conversation is idle, not in the middle of
		// a flick or of typing, and not on the heels of the last ones. Near the screen they are let in whatever is going on.
		{"rows are let in at a pace", "", `
  const { el, seen, scrollBy, flick, rowsArrive } = open();
  now += 1000;
  flick();
  scrollBy(el.room + 3000 - el.top);
  assert.strictEqual(window.__agyDeferGrow(), true, "far ahead of a fling, rows wait");
  scrollBy(-1200);
  assert.strictEqual(window.__agyDeferGrow(), false, "near it they do not");
  rowsArrive(1500, seen());
  await microtasks();
  pass(320);
  assert.strictEqual(window.__agyDeferGrow(), true, "at rest, but the last rows have only just been drawn");
  pass(100);
  document.activeElement = { tagName: "TEXTAREA" };
  assert.strictEqual(window.__agyDeferGrow(), true, "nor while the reader types");
  document.activeElement = null;
  assert.strictEqual(window.__agyDeferGrow(), false, "idle: rows further off are let in");
  rowsArrive(1500, seen());
  await microtasks();
  assert.deepStrictEqual(named("agy-grow-wait"), [], "none of this is the wait the trace marks");`},

		// A reader who gets ahead of the rows reaches the room. It is given up above them, so the first row is at the top
		// of the screen, and the fling ends there. A finger pulling down into it is left to until it lifts.
		{"a reader ahead of the rows stops at the first one", "", `
  const { el, onEl, scrollBy, flick } = open();
  flick();
  scrollBy(el.room - 80 - el.top);
  assert.strictEqual(el.room, el.top, "the room ends where the reader is");
  assert.strictEqual(el.style.overflowY, "hidden", "and the fling is ended");
  assert.deepStrictEqual(named("agy-room"), [{ what: "stop", by: 80 }]);
  pass(100);
  assert.strictEqual(el.style.overflowY, "");
  now += 1000;
  fire("touchstart", onEl([{}]));
  fire("touchmove", onEl([{}]));
  scrollBy(-50);
  assert.strictEqual(el.room, el.top + 50, "a finger pulling into the room is left to");
  fire("touchend", onEl([]));
  scrollBy(0);
  assert.strictEqual(el.room, el.top, "until it lifts");`},

		// Rows already let in are on their way. A fling that gets into the room ahead of them is left to carry on: they
		// fill the room from where it ends, and the reader is among them when they arrive.
		{"rows on their way are waited for in the room", "", `
  const { el, seen, scrollBy, flick, rowsArrive } = open();
  flick();
  scrollBy(el.room - el.top);
  assert.strictEqual(window.__agyDeferGrow(), false, "at the first row there is nothing to wait for");
  scrollBy(-300);
  assert.strictEqual(el.room, el.top + 300, "the reader is 300 px into the room");
  assert.strictEqual(el.style.overflowY, "", "with the fling still going");
  const at = seen();
  rowsArrive(1000, at);
  await microtasks();
  assert.strictEqual(el.room, el.top - 700, "and 700 px down the rows once they are in");
  assert.strictEqual(seen(), at);
  assert.deepStrictEqual(named("agy-room"), []);
  // Rows that do not come in time leave the reader at the top of what is loaded after all.
  now += 50;
  scrollBy(el.room - el.top);
  assert.strictEqual(window.__agyDeferGrow(), false);
  scrollBy(-200);
  assert.strictEqual(el.room, el.top + 200);
  now += 600;
  scrollBy(-100);
  assert.strictEqual(el.room, el.top, "the wait for them is short");
  assert.strictEqual(el.style.overflowY, "hidden");
  assert.deepStrictEqual(named("agy-room"), [{ what: "stop", by: 300 }]);`},

		// Rows that were let in can take longer to come than the conversation takes to come to rest, and a finger can land
		// as they are drawn. The browser's anchoring would move the position for them, so it stays off until they are in.
		{"rows that are due keep the browser's anchoring off", "", `
  const { el, seen, scrollBy, flick, rowsArrive } = open();
  flick();
  scrollBy(el.room + 1500 - el.top);
  assert.strictEqual(window.__agyDeferGrow(), false);
  assert.strictEqual(el.anchoring, "none");
  pass(1500);
  assert.strictEqual(el.anchoring, "none", "at rest, with rows that were let in still to come");
  const at = seen();
  rowsArrive(3000, at);
  await microtasks();
  assert.strictEqual(el.room, ROOM - 3000, "they come out of the room");
  assert.strictEqual(el.writes, 0);
  assert.strictEqual(seen(), at);
  pass(600);
  assert.strictEqual(el.anchoring, "auto", "and the anchoring is back on once they are in");
  // Rows that never come are not waited for longer than a few seconds.
  flick();
  scrollBy(el.room + 1500 - el.top);
  assert.strictEqual(window.__agyDeferGrow(), false);
  pass(4000);
  assert.strictEqual(el.anchoring, "none");
  pass(1500);
  assert.strictEqual(el.anchoring, "auto", "rows that never come are given up");`},

		// With little room left rows wait for the conversation to stop, as they did before there was any.
		{"the room runs out", "", `
  const d = open();
  d.flick();
  d.scrollBy(-40);
  // Rows arrive through one long run of flicks until 5,000 px of the room is left, and it is only filled at rest.
  for (let k = 0; k < (ROOM - 5000) / 5000; k++) { d.rowsArrive(5000, d.seen()); await microtasks(); d.scrollBy(-100); }
  assert.strictEqual(d.el.room, 5000);
  assert.strictEqual(d.el.writes, 0, "nothing was written for any of them");
  assert.strictEqual(window.__agyDeferGrow(), true, "too little room left: the rows wait");
  // Rows taller than what is left take all of it, and the rest is written with the fling ended first.
  const at = d.seen();
  const top = d.el.top;
  d.rowsArrive(7000, at);
  assert.strictEqual(d.el.room, 0);
  assert.strictEqual(d.el.top, top + 2000);
  assert.strictEqual(d.el.style.overflowY, "hidden");
  assert.strictEqual(d.seen(), at);
  await microtasks();
  // Checks left running from the rows before count that write once between them.
  for (let k = 0; k < 30; k++) frame();
  assert.strictEqual(d.el.top, top + 2000, "one write, however many rows are still being watched");
  assert.strictEqual(d.seen(), at);
  // At rest the room is filled back up.
  pass(600);
  assert.strictEqual(d.el.room, ROOM);
  assert.strictEqual(d.seen(), at);`},

		// Rows that do not fill the screen get no room. The position that goes with it would be past the end of the
		// conversation, and the browser would cut it back to the end, which is inside the room.
		{"rows shorter than the screen get no room", "", `
  const s = conversation();
  s.el.rows = 452;
  s.el.top = 0;
  s.row.y = 60;
  window.__agyRowsDrawn(s.el);
  s.scrollBy(0);
  pass(2000);
  assert.strictEqual(s.el.room, 0, "no room above rows shorter than the screen");
  assert.strictEqual(s.el.top, 0);
  assert.strictEqual(s.el.writes, 0);
  assert.deepStrictEqual(named("agy-room"), []);
  // The bundle goes on loading older rows while the first one is on screen. They are put back by a write, the
  // conversation being still, and once the rows fill the screen the room is made.
  assert.strictEqual(window.__agyDeferGrow(), false);
  s.rowsArrive(3000, s.seen());
  await microtasks();
  pass(100);
  assert.strictEqual(s.el.top, 3452 - 666, "as far down the new rows as the end allows");
  pass(1000);
  assert.strictEqual(s.el.room, ROOM, "room once the rows fill the screen");
  assert.strictEqual(s.el.top, ROOM + 3452 - 666);
  assert.deepStrictEqual(named("agy-room"), [{ what: "top", by: ROOM }]);`},

		// The keyboard closes and the screen is taller than the rows are. The end of the conversation is then inside the
		// room. Given up only as far as the reader, the room would have the end inside it again, and again after that, so
		// all of it goes at once.
		{"a screen that outgrows the rows", "", `
  const k = conversation();
  k.el.rows = 700;
  k.el.top = 34;
  window.__agyRowsDrawn(k.el);
  k.scrollBy(0);
  pass(600);
  assert.strictEqual(k.el.room, ROOM);
  assert.strictEqual(k.el.top, ROOM + 34);
  events.length = 0;
  k.el.writes = 0;
  k.el.screen = 874;
  pass(2000);
  assert.strictEqual(k.el.room, 0, "no room is left above rows the screen has outgrown");
  assert.strictEqual(k.el.top, 0);
  assert.deepStrictEqual(named("agy-room"), [{ what: "stop", by: 174 }], "and it went in one step");
  assert.strictEqual(k.el.writes, 0);`},

		// A conversation that is hidden reads as empty and at its top, which says nothing about its room. One hidden
		// before it came to rest keeps the room it had, for the position the browser gives it back when it shows again,
		// and the page goes on making room for others.
		{"a hidden conversation", "", `
  const h = open();
  assert.strictEqual(h.el.room, ROOM);
  h.flick();
  h.scrollBy(-40);
  h.el.hidden = true;
  pass(1000);
  h.el.hidden = false;
  assert.strictEqual(h.el.room, ROOM, "a hidden conversation keeps its room");
  assert.strictEqual(h.el.top, ROOM + 260);
  assert.strictEqual(h.el.anchoring, "auto");
  const n = conversation();
  n.scrollBy(0);
  n.el.hidden = true;
  pass(1000);
  n.el.hidden = false;
  assert.strictEqual(n.el.room, 0, "and one hidden before it had any is given none while it is");
  const next = conversation();
  next.scrollBy(0);
  pass(416);
  assert.strictEqual(next.el.room, ROOM, "the page still makes room for the next one");
  // Hidden in the frames after its room was filled, it is not taken for one whose position iOS put back.
  next.el.hidden = true;
  for (let k = 0; k < 12; k++) frame();
  next.el.hidden = false;
  assert.strictEqual(next.el.room, ROOM);
  assert.deepStrictEqual(named("agy-room").map(e => e.what), ["top"]);`},

		// Rows are followed for some frames after they arrive. A conversation hidden meanwhile, or a row that is no longer
		// laid out, reads as nothing, and nothing is made of that: before, the room grew by the same wrong amount on every
		// one of those frames.
		{"rows that can no longer be measured", "", `
  const { el, row, seen, scrollBy, flick, rowsArrive } = open();
  flick();
  scrollBy(-40);
  let at = seen();
  rowsArrive(3000, at);
  await microtasks();
  frame();
  el.hidden = true;
  for (let k = 0; k < 30; k++) frame();
  el.hidden = false;
  assert.strictEqual(el.room, ROOM - 3000, "nothing is measured in a hidden conversation");
  assert.strictEqual(seen(), at);
  pass(600);
  flick();
  scrollBy(-40);
  at = seen();
  rowsArrive(1000, at);
  await microtasks();
  frame();
  row.laidOut = false;
  for (let k = 0; k < 30; k++) frame();
  row.laidOut = true;
  assert.strictEqual(el.room, ROOM - 4000, "nor by a row that is not laid out");
  assert.strictEqual(seen(), at);
  assert.strictEqual(el.writes, 0);
  // One that is not laid out when the bundle hands it over is not a place to put anything back to.
  row.laidOut = false;
  rowsArrive(500, at);
  await microtasks();
  for (let k = 0; k < 30; k++) frame();
  assert.strictEqual(el.room, ROOM - 4000);
  assert.strictEqual(el.writes, 0);`},

		// A finger can land as the room is filled, and iOS then puts the old position back. The room goes back to what it
		// was, so the rows are where that position has them. After the second time it is left alone.
		{"the room's own write is taken back", "", `
  const d = conversation();
  window.__agyRowsDrawn(d.el);
  d.scrollBy(0);
  pass(416);
  assert.strictEqual(d.el.room, ROOM);
  // The page hears of the position put back. The room goes back with it at once, and the move is not taken for a
  // jump nobody asked for: nothing writes the position again under the finger.
  d.el.writes = 0;
  d.el.top = 300;
  fire("scroll", { target: d.el });
  assert.strictEqual(d.el.room, 0, "the room is put back with the position");
  assert.strictEqual(d.seen(), 60);
  assert.strictEqual(d.el.writes, 0, "and nothing holds the position against it");
  assert.deepStrictEqual(named("agy-fling-hold"), []);
  assert.strictEqual(d.el.style.overflowY, "");
  // Where no event tells of it, the frames after the write find it.
  pass(416);
  assert.strictEqual(d.el.room, ROOM, "the room is tried once more at the next rest");
  d.el.top = 300;
  frame();
  assert.strictEqual(d.el.room, 0);
  assert.strictEqual(d.seen(), 60);
  d.scrollBy(0);
  pass(1000);
  assert.strictEqual(d.el.room, 0, "left alone after two");
  assert.deepStrictEqual(named("agy-room").map(e => e.what), ["top", "back", "top", "back"]);`},

		// A conversation that leaves the page just after its room was filled reads as at its top from then on, which is
		// not iOS putting the position back.
		{"a conversation that leaves the page", "", `
  const d = conversation();
  window.__agyRowsDrawn(d.el);
  d.scrollBy(0);
  pass(416);
  assert.strictEqual(d.el.room, ROOM);
  d.el.isConnected = false;
  d.el.top = 0;
  for (let k = 0; k < 12; k++) frame();
  assert.strictEqual(d.el.room, ROOM, "a conversation that left the page is not one iOS moved back");
  const next = conversation();
  next.scrollBy(0);
  pass(416);
  assert.strictEqual(next.el.room, ROOM, "and the next one still gets its room");
  assert.deepStrictEqual(named("agy-room").map(e => e.what), ["top", "top"]);`},

		// A bundle whose markup does not grow by the padding the room is: the room is given up for the page.
		{"markup the room does not fit", "", `
  const e = conversation();
  e.el.laidOut = false;
  window.__agyRowsDrawn(e.el);
  e.scrollBy(0);
  pass(416);
  assert.strictEqual(e.el.room, 0, "a room the page does not lay out is given up");
  assert.strictEqual(e.el.top, 300, "with the position left where it was");
  assert.deepStrictEqual(named("agy-room").pop(), { what: "none", by: ROOM });
  // From then on rows are put back by writing the position, with a fling ended first, as before there was room.
  e.flick();
  e.scrollBy(-40);
  const at = e.seen();
  e.rowsArrive(4401, at);
  assert.strictEqual(e.el.top, 260 + 4401);
  assert.strictEqual(e.el.style.overflowY, "hidden");
  assert.strictEqual(e.seen(), at);
  assert.strictEqual(window.__agyTopRoom(e.el), 0, "and the scrollbar is told there is none");
  const later = conversation();
  later.scrollBy(0);
  pass(1000);
  assert.strictEqual(later.el.room, 0, "nor is it tried again on that page");`},

		// An iPhone has been seen moving a conversation a long way with nothing asking it to, and the tracker puts the
		// reader back. What counts as a long way is a share of the rows, which the room is no part of.
		{"a jump nobody asked for is held against", "", `
  const c = conversation();
  c.el.rows = 33000;
  c.el.top = 20000;
  window.__agyRowsDrawn(c.el);
  c.scrollBy(0);
  pass(600);
  assert.strictEqual(c.el.top, ROOM + 20000);
  now += 20000;
  events.length = 0;
  c.el.top -= 12000;
  fire("scroll", { target: c.el });
  assert.strictEqual(c.el.top, ROOM + 20000, "12,000 px of 33,000 is a jump, room or no room");
  assert.strictEqual(named("agy-fling-hold").length, 1);
  // One that would have ended above the top ends in the room instead, and is held against before the room is
  // given up to it.
  const d = conversation();
  d.el.rows = 100000;
  d.el.top = 20000;
  d.scrollBy(0);
  pass(600);
  assert.strictEqual(d.el.top, ROOM + 20000);
  now += 20000;
  events.length = 0;
  d.el.top -= 40000;
  fire("scroll", { target: d.el });
  assert.strictEqual(d.el.top, ROOM + 20000);
  assert.strictEqual(d.el.room, ROOM, "the room is not given up to a jump");
  assert.deepStrictEqual(named("agy-room"), []);
  assert.strictEqual(named("agy-fling-hold").length, 1);`},

		// A conversation thrown above its top used to read as a negative position, whatever the size of the throw. With
		// room it ends in the room, where a reader's own fling gets too, a step at a time. One move that far is still not
		// the reader's.
		{"a throw that ends in the room", "", `
  const c = conversation();
  c.el.rows = 100000;
  c.el.top = 20000;
  window.__agyRowsDrawn(c.el);
  c.scrollBy(0);
  pass(600);
  assert.strictEqual(c.el.room, ROOM);
  now += 20000;
  events.length = 0;
  c.el.top -= 25000;
  fire("scroll", { target: c.el });
  assert.strictEqual(c.el.top, ROOM + 20000, "25,000 px up, a quarter of the rows, is held against as it was above the top");
  assert.strictEqual(c.el.room, ROOM);
  assert.strictEqual(named("agy-fling-hold").length, 1);
  assert.deepStrictEqual(named("agy-room"), []);`},

		// A reader's own fling gets into the room in steps, however fast, and is stopped at the first row.
		{"a fast fling into the room is the reader's", "", `
  const f = conversation();
  f.el.rows = 100000;
  f.el.top = 20000;
  window.__agyRowsDrawn(f.el);
  f.scrollBy(0);
  pass(600);
  assert.strictEqual(f.el.room, ROOM);
  now += 20000;
  events.length = 0;
  f.flick();
  for (let k = 0; k < 6; k++) { now += 16; f.scrollBy(-4000); }
  assert.strictEqual(f.el.room, f.el.top, "a fling that outruns the rows stops at the first one");
  assert.deepStrictEqual(named("agy-room"), [{ what: "stop", by: 4000 }]);
  assert.deepStrictEqual(named("agy-fling-hold"), []);`},

		// WebKit shows a conversation that was hidden at the very top again, which with room is 200,000 px above the first
		// row. That is one move nobody asked for, and the reader is put back where they were. A script that asks for the
		// top is the page's own doing, and gets the first row.
		{"the very top in one step", "", `
  const h = conversation();
  h.el.rows = 100000;
  h.el.top = 20000;
  window.__agyRowsDrawn(h.el);
  h.scrollBy(0);
  pass(600);
  assert.strictEqual(h.el.room, ROOM);
  now += 20000;
  events.length = 0;
  h.el.top = 0;
  fire("scroll", { target: h.el });
  assert.strictEqual(h.el.top, ROOM + 20000, "the reader is put back");
  assert.strictEqual(h.el.room, ROOM, "and the room is kept");
  assert.strictEqual(named("agy-fling-hold").length, 1);
  pass(2000);
  now += 20000;
  events.length = 0;
  window.__agyScrolledByScript(h.el, 0, false);
  h.el.top = 0;
  fire("scroll", { target: h.el });
  assert.strictEqual(h.el.room, 0, "a script that asks for the top gets the first row");
  assert.strictEqual(h.el.top, 0);
  assert.deepStrictEqual(named("agy-fling-hold"), []);`},

		// A script that scrolls to the top over some frames is the page's own doing, and is left to run up to the first
		// row.
		{"a scroll the page animates into the room", "", `
  const s = conversation();
  s.el.rows = 100000;
  s.el.top = 20000;
  window.__agyRowsDrawn(s.el);
  s.scrollBy(0);
  pass(600);
  assert.strictEqual(s.el.room, ROOM);
  now += 20000;
  events.length = 0;
  window.__agyScrolledByScript(s.el, 0, true);
  for (let k = 0; k < 5; k++) { now += 16; s.scrollBy(-8000); }
  assert.strictEqual(s.el.room, s.el.top, "it ends at the first row");
  assert.deepStrictEqual(named("agy-fling-hold"), []);
  assert.strictEqual(s.el.room < ROOM, true);`},

		// Rows being drawn again can leave the conversation shorter than the screen for a moment, which puts its end, and
		// the reader with it, in the room. The room is given up, and what the tracker had noted of the collapse moves with
		// it: the bundle is still told, once the rows are back, that the reader did not leave the end themselves.
		{"a collapse under a reader who follows the end", "", `
  const c = conversation();
  c.el.top = c.el.rows - c.el.screen;
  window.__agyRowsDrawn(c.el);
  c.scrollBy(0);
  pass(600);
  assert.strictEqual(c.el.room, ROOM);
  assert.strictEqual(c.el.top, ROOM + 9000 - 666);
  now += 20000;
  events.length = 0;
  rowsRemoved(c.el, 200);
  c.el.rows = 300;
  pass(100);
  assert.strictEqual(c.el.room, 0, "rows shorter than the screen keep no room");
  assert.strictEqual(c.el.top, 0);
  assert.deepStrictEqual(named("agy-fling-hold"), []);
  c.el.rows = 9000;
  pass(50);
  assert.strictEqual(window.__agyIsSpontaneousScroll(c.el), true, "the drop is still the collapse's, not the reader's");`},

		// With nothing older left to load the room is given up at rest, and the position drops by as much with nothing on
		// screen moving. The bundle takes a drop for the reader leaving the end unless it is told otherwise.
		{"the room given up under a reader who follows the end", "", `
  const c = conversation();
  c.el.top = c.el.rows - c.el.screen - 40;
  window.__agyRowsDrawn(c.el);
  c.scrollBy(0);
  pass(600);
  assert.strictEqual(c.el.room, ROOM);
  c.el.older.disabled = true;
  c.scrollBy(0);
  pass(416);
  assert.strictEqual(c.el.room, 0, "no room is kept with nothing older to load");
  assert.strictEqual(c.el.top, c.el.rows - c.el.screen - 40);
  assert.strictEqual(window.__agyIsSpontaneousScroll(c.el), true, "the drop is not the reader's");
  fire("touchstart", c.onEl([{}]));
  assert.strictEqual(window.__agyIsSpontaneousScroll(c.el), false, "a finger down is");
  fire("touchend", c.onEl([]));
  for (let k = 0; k < 12; k++) frame();
  assert.strictEqual(window.__agyIsSpontaneousScroll(c.el), false, "and so is a drop after the write has settled");`},
	} {
		t.Run(c.name, func(t *testing.T) {
			harness := roomPage + "(async () => {" + c.js + "\n})().catch(e => { console.error(e); process.exit(1); });\n"
			args := []string{"-e", harness, scriptPath}
			if c.ua != "" {
				args = append(args, c.ua)
			}
			if out, err := exec.Command(node, args...).CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}

// A page for the phone tracer to run in: one conversation with room kept above its rows, a clock the test moves, and what
// the tracer sends kept in posted. tick runs the tracer's sampler, which a page runs every 150 ms.
func debugTracerPage(t *testing.T) string {
	t.Helper()
	js := strings.TrimPrefix(mobileDebug, `<script id="agy-debug">`)
	src, err := json.Marshal(strings.TrimSuffix(js, `</script>`))
	if err != nil {
		t.Fatal(err)
	}
	return `"use strict";
const assert = require("assert");
let now = 1000;
let frames = [];
let room = 200000;
const hooks = {};
const posted = [];
const samplers = [];
const viewport = {
  scrollTop: 203000, scrollHeight: 209000, clientHeight: 704, className: "h-full", tagName: "DIV",
  getAttribute: k => (k === "data-testid" ? "autoscroll-viewport" : null),
  getBoundingClientRect: () => ({ top: 0, bottom: 704, left: 0, width: 400, height: 704 }),
  contains: () => false,
};
const root = { clientHeight: 800, scrollHeight: 800, scrollTop: 0, style: { getPropertyValue: () => "" }, appendChild: el => { el.parentNode = root; }, removeChild: () => {} };
const doc = {
  documentElement: root, body: {},
  querySelector: sel => (sel === '[data-testid="autoscroll-viewport"]' ? viewport : null),
  querySelectorAll: sel => (sel === "body *" ? [viewport] : []),
  createElement: () => ({ style: {} }),
  elementFromPoint: () => null,
};
global.getComputedStyle = () => ({ overflowY: "auto", paddingTop: "0px", paddingRight: "0px", paddingBottom: "0px", paddingLeft: "0px" });
global.location = { pathname: "/" };
const win = {
  visualViewport: { width: 400, height: 800, offsetTop: 0, offsetLeft: 0, pageTop: 0, scale: 1, addEventListener: () => {} },
  matchMedia: q => ({ matches: q === "(pointer: coarse)" }),
  addEventListener: (name, fn) => { (hooks[name] = hooks[name] || []).push(fn); },
  innerWidth: 400, innerHeight: 800, scrollY: 0, devicePixelRatio: 3,
  // The scroll tracker says how much room it keeps above the conversation's rows.
  __agyTopRoom: el => (el === viewport ? room : 0),
};
const run = new Function("window", "setInterval", "document", "navigator", "screen", "performance", "fetch", "requestAnimationFrame", ` + string(src) + `);
run(win, fn => { samplers.push(fn); return samplers.length; }, doc, { userAgent: "test", standalone: true }, { width: 400, height: 800 }, { now: () => now }, (url, init) => { posted.push(init.body); }, fn => { frames.push(fn); return frames.length; });
const fire = (name, detail) => hooks[name].forEach(fn => fn({ detail }));
const tick = () => { now += 150; samplers.forEach(fn => fn()); };
`
}

// One long scroll is one episode, and its 120 samples are used up in the first two seconds. What the page did after
// that is kept as events, short ones, until the episode is as large as still goes in one request.
func TestDebugTracerKeepsEventsAfterItsSamples(t *testing.T) {
	runNode(t, debugTracerPage(t)+`
const frame = () => { now += 16; viewport.scrollTop -= 7; const due = frames; frames = []; due.forEach(fn => fn()); };

// Rows arrive every second or so and the reader goes on scrolling: every frame is a sample until there are 120.
fire("agy-rows-kept", { moved: 2267, again: 0 });
for (let k = 1; k <= 150; k++) { frame(); if (k % 50 === 0) fire("agy-rows-kept", { moved: 126, again: 0 }); }
// From there the frames are left out and what happens is kept, in a line with the conversation's own numbers.
fire("agy-room", { what: "stop", by: 310 });
fire("agy-rows-kept", { moved: 4174, again: -176 });
for (let k = 0; k < 40; k++) frame();
// A scroll that goes on for minutes stops being recorded before the request would be too large to send.
for (let k = 0; k < 900; k++) { now += 300; fire("agy-rows-kept", { moved: 100 + k, again: 0 }); frame(); }
now += 5000;
frame();
assert.strictEqual(posted.length, 1, "one long scroll is one episode");
const lines = posted[0].trimEnd().split("\n");
assert.ok(lines[0].startsWith("=== ") && lines[0].includes("trigger=rows-kept:2267,0"));
const full = lines.slice(1).filter(l => l.includes(" vv="));
const brief = lines.slice(1).filter(l => !l.includes(" vv="));
assert.strictEqual(full.length, 120, "the samples stop at 120");
assert.ok(full[5].includes("ev=raf") && full[5].includes("scrollers=[autoscroll-viewport:st" + (203000 - 5 * 7) + "/sh209000/ch704/room200000]"), full[5]);
assert.ok(brief[0].includes("ev=rows-kept:126,0 "), "the rows that arrive after the samples are full: " + brief[0]);
assert.ok(/^  t= *\d+ ev=room-stop:310 +scrollers=\[autoscroll-viewport:st\d+\/sh209000\/ch704\/room200000\]$/.test(brief[1]), brief[1]);
assert.ok(brief[2].includes("ev=rows-kept:4174,-176 "), brief[2]);
assert.ok(brief.every(l => !l.includes("ev=raf")), "no frame is kept after the samples are full");
const longest = Math.max(...brief.map(l => l.length + 1));
assert.ok(brief.length > 100 && brief.length < 903, "events are kept for a good while, and then they stop too: " + brief.length);
assert.ok(posted[0].length >= 56000 && posted[0].length < 56000 + longest, "at a size that still goes in one request: " + posted[0].length);
`)
}

// The tracer samples the conversation for moves no event tells of. Room being filled or given up moves the position
// by as much as the room changed, with the reader where they were among the rows, and is not one of them.
func TestDebugTracerLeavesTheRoomOutOfAJump(t *testing.T) {
	runNode(t, debugTracerPage(t)+`
const frame = () => { now += 16; const due = frames; frames = []; due.forEach(fn => fn()); };
const settle = () => { for (let k = 0; k < 200; k++) frame(); };
const triggers = () => posted.map(body => /trigger=(\S+)/.exec(body)[1].replace(/\+\d+$/, ""));
// Long after any input, with the reader 3,000 px down 9,000 px of rows.
now += 60000;
tick();
// The room is given up at rest, and filled again: the position and the height change by as much as it did.
room = 0; viewport.scrollTop -= 200000; viewport.scrollHeight -= 200000;
tick();
settle();
room = 200000; viewport.scrollTop += 200000; viewport.scrollHeight += 200000;
tick();
settle();
assert.deepStrictEqual(triggers(), [], "neither is a move of the reader's");
// A reader who went up into the room has it given up above them, which leaves the position as it was.
viewport.scrollTop = 199950;
tick();
settle();
assert.deepStrictEqual(triggers(), ["st-drop"], "the reader's own 3,050 px, long after any input");
room = 199950; viewport.scrollHeight -= 50;
tick();
settle();
assert.deepStrictEqual(triggers(), ["st-drop"], "the 50 px of room given up is nothing more");
// The position alone moving is what it was before there was room: a drop, and further than a finger goes, a jump.
viewport.scrollTop -= 200;
tick();
settle();
viewport.scrollTop += 6000;
tick();
settle();
assert.deepStrictEqual(triggers(), ["st-drop", "st-drop", "jump"]);
`)
}

// The distance the bundle asks for older rows at, as 2.32 has it.
const v232PagingConfig = `var O2a={startIndex:-50},R2a={expansion:{minBatchSize:15,multiplier:.5,maxBatchSize:50},timing:{throttleMs:100,retryDelayMs:1E3,oppositeDirectionCooldownMs:1E3},trigger:{innerRadiusPx:600,contractionSafetyPx:8E3,outerRadiusPx:1.5E4}};`

// The scroll tracker's distance is used where it gives one, and the bundle's own where it gives none. Only a bundle
// that hands the tracker the move that keeps its rows in place is changed at all.
func TestRowsAreAskedForFromFurtherAwayOnIOS(t *testing.T) {
	got, status := applyWithStatus(t, "paging-loads-rows-ahead", v232PagingConfig+v232RowsKept, Options{})
	want := `trigger:{innerRadiusPx:(window.__agyGrowRadius||600),contractionSafetyPx:8E3,outerRadiusPx:1.5E4}}`
	if status != StatusApplied || !strings.Contains(got, want) {
		t.Errorf("status %v, got\n%s\nwant it to contain\n%s", status, got, want)
	}
	if !strings.Contains(got, `expansion:{minBatchSize:15,multiplier:.5,maxBatchSize:50}`) {
		t.Error("the rest of the configuration was changed")
	}
	// The minifier wraps lines after commas, colons and braces, and writes a round number its own way.
	wrapped := "trigger:\n{innerRadiusPx:\n6E2,\ncontractionSafetyPx:8E3"
	if out := applyOnly(t, "paging-loads-rows-ahead", wrapped+v232RowsKept, Options{}); !strings.Contains(out, "(window.__agyGrowRadius||6E2)") {
		t.Errorf("a wrapped configuration was not matched: %s", out)
	}
	// A bundle that names the distance some other way is reported, and one without this paging needs nothing.
	renamed := strings.Replace(v232PagingConfig, "trigger:{innerRadiusPx:600,", "trigger:{contractionSafetyPx:8E3,innerRadiusPx:600,", 1)
	if _, status := applyWithStatus(t, "paging-loads-rows-ahead", renamed+v232RowsKept, Options{}); status != StatusMissing {
		t.Errorf("a changed configuration: status %v, want %v", status, StatusMissing)
	}
	if _, status := applyWithStatus(t, "paging-loads-rows-ahead", `var a={startIndex:-50};`, Options{}); status != StatusNotNeeded {
		t.Errorf("a bundle without the configuration: status %v, want %v", status, StatusNotNeeded)
	}
}

// The bundles before 2.32 have the same paging, the same scrollbar and the same button for older messages, and move the
// position for their rows themselves. Rows asked for sooner would reach them in the middle of a fling, and room they
// do not take rows from would only stand above the conversation. So what is built on the hand-over is left out where
// there is none: with a bundle that has no such component, with one whose component this does not match, and when the
// hand-over is switched off.
func TestPatchesBuiltOnTheHandOverNeedIt(t *testing.T) {
	older := v232PagingConfig + v232ScrollbarMeasure + v232ScrollbarClick + ";" + v232ScrollbarTimeline + v232ScrollbarTrack
	changed := strings.Replace(v232RowsKept, "Math.abs(c)>=.5&&(a.scrollTop+=c)", "a.scrollBy(0,c)", 1)
	built := []string{"paging-loads-rows-ahead", "scrollbar-measures-rows", "scrollbar-click-lands-in-rows", "scrollbar-thumb-by-script"}
	for _, c := range []struct {
		name, src string
		opts      Options
		handOver  Status
		want      Status
	}{
		{"a bundle before 2.32", older, Options{}, StatusNotNeeded, StatusNotNeeded},
		{"a component in another form", older + changed, Options{}, StatusMissing, StatusNotNeeded},
		{"the hand-over switched off", older + v232RowsKept, Options{Disabled: map[string]bool{"paging-keeps-rows-after-render": true}}, StatusDisabled, StatusNotNeeded},
		{"2.32", older + v232RowsKept, Options{}, StatusApplied, StatusApplied},
	} {
		out, report := Apply(MainJS, []byte(c.src), c.opts)
		got := map[string]Result{}
		for _, r := range report {
			got[r.ID] = r
		}
		if got["paging-keeps-rows-after-render"].Status != c.handOver {
			t.Errorf("%s: the hand-over is %v, want %v", c.name, got["paging-keeps-rows-after-render"].Status, c.handOver)
		}
		// Doctor says which patch each one went without.
		without := ""
		if c.want == StatusNotNeeded {
			without = "paging-keeps-rows-after-render"
		}
		for _, id := range built {
			if got[id].Status != c.want || got[id].Without != without {
				t.Errorf("%s: %s is %v without %q, want %v without %q", c.name, id, got[id].Status, got[id].Without, c.want, without)
			}
		}
		if untouched := strings.HasPrefix(string(out), older); untouched != (c.want != StatusApplied) {
			t.Errorf("%s: the paging and the scrollbar left as they were: %v", c.name, untouched)
		}
	}
	for _, p := range All() {
		for _, id := range built {
			if p.ID == id && p.Requires != "paging-keeps-rows-after-render" {
				t.Errorf("%s does not say it needs the hand-over", id)
			}
		}
	}
}
