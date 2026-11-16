package patches

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	want := `componentDidUpdate(a,b,c){a=this.props.viewportRef.current;c=c?.find(({element:d})=>d.isConnected);window.__agyKeepRows?(window.__agyRowsDrawn?.(a,this.props.getAnchorCandidates),a&&c&&window.__agyKeepRows(a,c.element,c.offset)):a&&c&&(c=c.element.getBoundingClientRect().top-a.getBoundingClientRect().top-c.offset,Math.abs(c)>=.5&&(a.scrollTop+=c))}render(){`
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
// be told by, and with it how the component finds the rows it chooses among.
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
const rowsOf = () => [];
kept.props = { viewportRef: { current: viewport }, getAnchorCandidates: rowsOf };
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
window.__agyRowsDrawn = (el, rows) => drawn.push([el, calls.length, rows]);
kept.componentDidUpdate({}, null, null);
kept.componentDidUpdate({}, null, [{ element: row, offset: 60 }]);
assert.deepStrictEqual(drawn, [[viewport, 1, rowsOf], [viewport, 1, rowsOf]]);
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

  // Without room a row handed over a second time is put where the bundle measured it that time.
  {
    const { el, row } = conversation();
    row.y += 1000;
    window.__agyKeepRows(el, row, 60);
    await microtasks();
    frame();
    assert.strictEqual(el.scrollTop, 1300);
    row.y += 30;
    row.y += 500;
    window.__agyKeepRows(el, row, 90);
    await microtasks();
    assert.strictEqual(el.scrollTop, 1800, "by what the second rows added, and no more");
    drain();
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

  // The bundle's navigator jumps to a step in the same commit, after the rows were handed over. The position is the
  // navigator's from then on: putting the row back once more would undo the jump.
  {
    events.length = 0;
    const { el, row } = conversation();
    row.y += 4401;
    window.__agyKeepRows(el, row, 60);
    assert.strictEqual(el.scrollTop, 4701);
    el.scrollTop = 1200;
    await microtasks();
    assert.strictEqual(el.scrollTop, 1200, "a jump made in the same commit is left where it went");
    assert.strictEqual(frames.length, 0, "and the row is not looked at again");
    assert.deepStrictEqual(kept(), [{ moved: 4401, again: "taken" }]);
  }

  // Near the end the second drawing can leave the position past the end, where the browser cuts it back. That is not
  // another script's jump, and the row is put back where it was.
  {
    events.length = 0;
    const { el, row } = conversation();
    row.y += 1000;
    window.__agyKeepRows(el, row, 60);
    assert.strictEqual(el.scrollTop, 1300);
    row.y -= 350;
    el.scrollTop -= 140;
    el.scrollHeight = el.scrollTop + el.clientHeight;
    await microtasks();
    assert.strictEqual(el.scrollTop, 950, "the row is put back, though the position was cut back to the end");
    assert.deepStrictEqual(kept(), [{ moved: 1000, again: -210 }]);
    drain();
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
// The browser anchors rows itself where its anchoring is left on, unless the case is for one that does not.
const anchors = !process.env.NO_ANCHORING;
if (anchors) global.CSS = { supports: property => property === "overflow-anchor" };
new Function(fs.readFileSync(process.argv[1], "utf8"))();
const fire = (name, event) => (listeners[name] || []).forEach(fn => fn(event));
// Every conversation is laid out at the start of a frame, and the page hears of a position that the browser moved then
// or since the last one.
const pages = [];
const frame = () => { pages.forEach(layout => layout(true)); const due = frames; frames = []; due.forEach(fn => fn()); };
// Time passes a frame at a time, as it does for the page.
const pass = ms => { for (const end = now + ms; now < end;) { now += 16; frame(); const due = timers.filter(x => x.at <= now); timers = timers.filter(x => x.at > now); due.forEach(x => x.fn()); } };
const microtasks = () => new Promise(resolve => setImmediate(resolve));
const named = type => events.filter(e => e.type === type).map(e => e.detail);
const ROOM = 200000;

// A viewport 100 px down the page and 666 px tall. Its rows are 9000 px tall and start below the room, and the row the
// bundle keeps in place is row.y down them. A case can have something that is not a row laid out under them.
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
  const end = () => Math.max(0, el.laid + el.rows + el.below - el.screen);
  const layout = tell => {
    if (el.top > end()) { el.top = end(); cut = true; }
    if (tell && cut) { cut = false; fire("scroll", { target: el }); }
  };
  pages.push(layout);
  const el = {
    top: 300, rows: 9000, below: 0, screen: 666, hidden: false, isConnected: true, older, writes: 0,
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
    get scrollHeight() { return el.hidden ? 0 : Math.max(el.screen, el.laid + el.rows + el.below); },
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
      if (el.hidden || !row.laidOut) return { top: 0, bottom: 0, width: 0, height: 0 };
      layout(false);
      const top = 100 + el.laid + row.y - el.top;
      return { top, bottom: top + 80, width: 400, height: 80 };
    },
  };
  // Where the row is on the screen, which is what the reader sees.
  const seen = () => row.getBoundingClientRect().top - 100;
  const onEl = touches => ({ target: { closest: () => el }, touches });
  // The reader scrolls: the position changes and the page hears of it. Nothing here counts as a write.
  const scrollBy = d => { el.top += d; fire("scroll", { target: el }); };
  const flick = () => { fire("touchstart", onEl([{}])); fire("touchmove", onEl([{}])); fire("touchend", onEl([])); };
  const rowsArrive = (height, offset) => { el.rows += height; row.y += height; window.__agyKeepRows(el, row, offset); };
  // Something above the reader's row changes height with nothing handed over: an image loads, a table is drawn again.
  // A browser whose anchoring is on moves the position by as much, and the page hears of it a frame later.
  const grow = by => {
    el.rows += by;
    row.y += by;
    if (anchors && el.anchoring === "auto") { el.top += by; cut = true; }
  };

  // The rows the bundle chooses among, for the cases that have them: one after the other from the top of the rows,
  // so one that changes height moves every row after it. el.looks counts how often the page asks for them.
  const list = [];
  el.looks = 0;
  const rowsOf = () => { el.looks++; return list.slice(); };
  const onPage = r => r.laidOut && r.isConnected;
  const place = () => { let y = 0; for (const r of list) { r.y = y; if (onPage(r)) y += r.h; } el.rows = y; };
  const makeRow = h => {
    const r = { h, y: 0, isConnected: true, laidOut: true,
      getBoundingClientRect: () => {
        if (el.hidden || !onPage(r)) return { top: 0, bottom: 0, width: 0, height: 0 };
        layout(false);
        const top = 100 + el.laid + r.y - el.top;
        return { top, bottom: top + r.h, width: 400, height: r.h };
      } };
    return r;
  };
  const withRows = (n, h) => { for (let k = 0; k < n; k++) list.push(makeRow(h)); place(); };
  // The first row to reach below the top of the screen, which is what the bundle keeps and what a browser anchors to.
  const atTop = () => { layout(false); return list.find(r => onPage(r) && el.laid + r.y + r.h > el.top) || null; };
  const seenOf = r => r.getBoundingClientRect().top - 100;
  // A row changes height, or goes from the page. A browser whose anchoring is on keeps the row at the top of the
  // screen where it is, which moves the position when the change is above that row.
  const change = (r, how) => {
    const anchor = atTop();
    const before = anchor ? seenOf(anchor) : null;
    how(r);
    place();
    if (anchors && el.anchoring === "auto" && anchor && onPage(anchor) && seenOf(anchor) !== before) { el.top += seenOf(anchor) - before; cut = true; }
  };
  const resize = (r, by) => change(r, x => { x.h += by; });
  // Older rows arrive above, and the bundle hands over the row it kept, as its patched component does.
  const prepend = (n, h) => {
    const kept = atTop();
    const offset = seenOf(kept);
    const added = [];
    for (let k = 0; k < n; k++) added.push(makeRow(h));
    list.unshift(...added);
    place();
    window.__agyRowsDrawn(el, rowsOf);
    window.__agyKeepRows(el, kept, offset);
    return added;
  };
  return { el, row, seen, onEl, scrollBy, flick, rowsArrive, grow, list, rowsOf, withRows, atTop, seenOf, change, resize, prepend, place };
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
// The same with the bundle's rows known to the page: thirty of 300 px, and the reader 1,610 px down them, a little
// way down the sixth.
const openRows = () => {
  const c = conversation();
  c.withRows(30, 300);
  c.el.top = 1610;
  window.__agyRowsDrawn(c.el, c.rowsOf);
  c.scrollBy(0);
  pass(600);
  events.length = 0;
  c.el.writes = 0;
  c.el.looks = 0;
  return c;
};
// The same as it has just appeared: drawn and scrolled to where it opens a frame ago, with no finger on it yet and
// no rest.
const freshRows = () => {
  const c = conversation();
  c.withRows(30, 300);
  c.el.top = 1610;
  window.__agyRowsDrawn(c.el, c.rowsOf);
  c.scrollBy(0);
  now += 16; frame();
  events.length = 0;
  c.el.writes = 0;
  c.el.looks = 0;
  return c;
};
// The timers that are due, with no frame drawn first.
const timersDue = () => { const due = timers.filter(x => x.at <= now); timers = timers.filter(x => x.at > now); due.forEach(x => x.fn()); };
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
	// An iPhone whose browser does not anchor rows itself, which is every one before scroll anchoring reached iOS.
	const noAnchoring = "an iPhone that does not anchor"
	for _, c := range []struct{ name, ua, js string }{
		// Rows are followed after they arrive for as long as the browser's anchoring is off, and not once it is back on:
		// from then the browser moves the position for whatever shifts them, and a row still followed would have the
		// same shift taken out of the room as well.
		{"a late change with the anchoring back on is the browser's", "", `
  const { el, seen, scrollBy, rowsArrive, grow } = open();
  now += 5000;
  scrollBy(el.room + 2200 - el.top);
  pass(1000);
  // The reader is still, 2,200 px from the first row, and the rows the bundle asks for are let in.
  assert.strictEqual(window.__agyDeferGrow(), false);
  pass(304);
  const at = seen();
  rowsArrive(3000, at);
  await microtasks();
  assert.strictEqual(el.room, ROOM - 3000);
  // The conversation comes to rest a few frames later, with the rows in.
  pass(160);
  assert.strictEqual(el.anchoring, "auto");
  grow(300);
  for (let k = 0; k < 30; k++) frame();
  assert.strictEqual(el.room, ROOM - 3000, "what the browser moved the position for is not taken out of the room too");
  assert.strictEqual(seen(), at);
  assert.strictEqual(frames.length, 0, "and the rows are no longer followed");`},

		{"a late change with the anchoring off is made up for however late", "", `
  const { el, seen, scrollBy, flick, rowsArrive, grow } = open();
  flick();
  scrollBy(-40);
  assert.strictEqual(window.__agyDeferGrow(), false);
  let at = seen();
  rowsArrive(3000, at);
  await microtasks();
  // The fling goes on for a second, and an image among the new rows loads 40 frames after they arrived.
  for (let k = 0; k < 40; k++) { now += 16; scrollBy(-5); frame(); }
  assert.strictEqual(el.anchoring, "none");
  at = seen();
  grow(300);
  frame();
  frame();
  assert.strictEqual(seen(), at, "the room makes up for it while nothing else would");
  assert.strictEqual(el.room, ROOM - 3300);
  assert.strictEqual(el.writes, 0);
  // Rows handed over after these take the following over, and the check for the earlier ones ends.
  rowsArrive(1000, seen());
  await microtasks();
  for (let k = 0; k < 40; k++) { now += 16; scrollBy(-5); frame(); }
  for (let k = 0; k < 3; k++) frame();
  assert.strictEqual(frames.length, 1, "one check goes on, for the rows handed over last");
  pass(600);
  assert.strictEqual(el.anchoring, "auto");
  assert.strictEqual(frames.length, 0, "and it ends when the browser's anchoring is back on");`},

		// What is followed is the row at the top of the screen, chosen again as the reader moves, and not the row that
		// was handed over: that one stands for what is above the screen only while it is at the top of it.
		{"a change below the top of the screen, above the rows handed over", "", `
  const { el, list, seenOf, atTop, scrollBy, flick, prepend, resize } = openRows();
  flick();
  scrollBy(-40);
  assert.strictEqual(window.__agyDeferGrow(), false);
  const kept = atTop();
  assert.strictEqual(kept, list[5]);
  const added = prepend(10, 300);
  await microtasks();
  assert.strictEqual(seenOf(kept), -70, "the row handed over is where it was");
  assert.strictEqual(el.room, ROOM - 3000);
  // The reader goes on up, 1,500 px into the rows that arrived.
  for (let k = 0; k < 25; k++) { now += 16; scrollBy(-60); frame(); }
  assert.strictEqual(el.anchoring, "none");
  const top = atTop();
  assert.strictEqual(top, list[10]);
  const at = seenOf(top);
  // A row under the top of the screen changes: what is above it stays where it is, as with a browser's anchoring.
  resize(list[12], 200);
  now += 16; frame();
  assert.strictEqual(seenOf(top), at, "the rows at the top of the screen have not moved");
  assert.strictEqual(el.room, ROOM - 3000, "and nothing was taken from the room for it");
  assert.strictEqual(seenOf(kept), -70 + 1500 + 200, "the row handed over has, and is not what is followed");
  // The row at the top edge itself: its top stays.
  resize(top, 60);
  now += 16; frame();
  assert.strictEqual(seenOf(top), at);
  assert.strictEqual(el.room, ROOM - 3000);
  // A row above the screen: that is made up for.
  resize(list[3], -90);
  now += 16; frame();
  assert.strictEqual(seenOf(top), at, "a change above the screen is made up for");
  assert.strictEqual(el.room, ROOM - 3000 + 90);
  assert.strictEqual(el.writes, 0, "with no position written");
  assert.strictEqual(added.length, 10);`},

		{"a reader who goes far below the rows handed over", "", `
  const { el, list, seenOf, atTop, scrollBy, flick, prepend, resize } = openRows();
  flick();
  scrollBy(-40);
  assert.strictEqual(window.__agyDeferGrow(), false);
  const kept = atTop();
  prepend(10, 300);
  await microtasks();
  // Down 3,000 px in the first frames after the rows arrived.
  for (let k = 0; k < 6; k++) { now += 16; scrollBy(500); frame(); }
  assert.strictEqual(el.anchoring, "none");
  assert.strictEqual(seenOf(kept), -70 - 3000);
  const top = atTop();
  assert.strictEqual(top, list[25]);
  const at = seenOf(top);
  // Something between the rows handed over and the screen changes, which those rows do not show.
  resize(list[20], 143);
  assert.strictEqual(seenOf(kept), -70 - 3000, "the rows handed over do not show it");
  assert.strictEqual(seenOf(top), at + 143);
  now += 16; frame();
  assert.strictEqual(seenOf(top), at, "a change between the rows handed over and the screen is made up for");
  assert.strictEqual(el.room, ROOM - 3000 - 143);
  // And while the reader is moving.
  resize(list[18], -80);
  now += 16; scrollBy(60); frame();
  assert.strictEqual(seenOf(top), at - 60, "and so is one while the reader moves");
  assert.strictEqual(el.room, ROOM - 3000 - 143 + 80);
  assert.strictEqual(el.writes, 0);`},

		// From when the anchoring is switched off there is nothing but the tracker to keep the rows where they are, so
		// it follows the row at the top of the screen from then, before any rows have been handed over.
		{"a change while rows that were let in are still to come", "", `
  const { el, list, seenOf, atTop, scrollBy, flick, prepend, resize } = openRows();
  flick();
  scrollBy(-40);
  now += 16; frame();
  assert.strictEqual(el.looks, 0, "nothing is followed while the browser anchors");
  assert.strictEqual(window.__agyDeferGrow(), false);
  assert.strictEqual(el.anchoring, "none");
  assert.strictEqual(el.looks, 1, "the row at the top of the screen is, from when it does not");
  const top = atTop();
  const at = seenOf(top);
  now += 16; frame();
  resize(list[2], 100);
  now += 16; frame();
  assert.strictEqual(seenOf(top), at, "a change above the screen is made up for before the rows arrive");
  assert.strictEqual(el.room, ROOM - 100);
  // The rows arrive and are taken from the room as they always are.
  now += 200;
  prepend(10, 300);
  await microtasks();
  assert.strictEqual(seenOf(top), at);
  assert.strictEqual(el.room, ROOM - 100 - 3000);
  // At rest the browser has the conversation back, and nothing is followed any more.
  pass(600);
  assert.strictEqual(el.anchoring, "auto");
  const room = el.room;
  resize(list[2], 50);
  now += 16; frame();
  assert.strictEqual(seenOf(top), at, "the browser's doing");
  assert.strictEqual(el.room, room, "with nothing more taken from the room");
  // The next rows let in switch it off again, and the row at the top is followed again.
  pass(600);
  assert.strictEqual(window.__agyDeferGrow(), false);
  assert.strictEqual(el.anchoring, "none");
  now += 16; frame();
  resize(list[2], -50);
  now += 16; frame();
  assert.strictEqual(seenOf(top), at, "followed again the next time the anchoring goes off");
  assert.strictEqual(el.room, room + 50);`},

		// The rows that have just arrived are among the rows known from the frame they arrive in.
		{"a reader among rows that have just arrived", "", `
  const { el, list, seenOf, atTop, scrollBy, flick, prepend, resize } = openRows();
  flick();
  scrollBy(-40);
  assert.strictEqual(window.__agyDeferGrow(), false);
  now += 16;
  prepend(10, 300);
  await microtasks();
  // Up 2,400 px in eight frames, to where only rows that have just arrived are on the screen.
  for (let k = 0; k < 8; k++) { now += 16; scrollBy(-300); frame(); }
  const top = atTop();
  assert.strictEqual(top, list[7]);
  const at = seenOf(top);
  // One of them changes, under the top of the screen and above the first row there was before.
  resize(list[9], 150);
  now += 16; frame();
  assert.strictEqual(seenOf(top), at, "the rows at the top of the screen have not moved");
  assert.strictEqual(el.room, ROOM - 3000);`},

		// Rows can come without being handed over: at the end as a reply is written, and above for a reader who
		// follows the end. A reader who gets past either end of the rows that are known has them looked for again.
		{"a reader past the ends of the rows that are known", "", `
  const { el, list, seenOf, atTop, scrollBy, flick, withRows, resize, change, place } = openRows();
  flick();
  scrollBy(-40);
  assert.strictEqual(window.__agyDeferGrow(), false);
  assert.strictEqual(el.looks, 1);
  // Five rows are drawn above with nothing handed over. The row at the top of the screen is followed all the same.
  const top = atTop();
  const at = seenOf(top);
  const older = [];
  change(null, () => { for (let k = 0; k < 5; k++) { withRows(1, 300); older.push(list.pop()); } list.unshift(...older); });
  now += 16; frame();
  assert.strictEqual(seenOf(top), at, "rows drawn above are taken from the room without being handed over");
  assert.strictEqual(el.room, ROOM - 1500);
  // The reader goes up among them, which takes longer than the quarter of a second between looks.
  for (let k = 0; k < 25; k++) { now += 16; scrollBy(-90); frame(); }
  assert.strictEqual(window.__agyDeferGrow(), false);
  const among = atTop();
  assert.strictEqual(list.indexOf(among), 2, "a row that was not known when the rows were looked for");
  const from = seenOf(among);
  assert.strictEqual(el.looks >= 2, true, "the rows were looked for again above the first one known");
  resize(list[4], 110);
  now += 16; frame();
  assert.strictEqual(seenOf(among), from, "a change under the top of the screen moves nothing there");
  assert.strictEqual(el.room, ROOM - 1500);
  // Six rows are written at the end, and the reader goes down among those.
  withRows(6, 300);
  const end = list.length - 6;
  for (let k = 0; k < 50; k++) { now += 16; scrollBy(Math.min(400, el.laid + list[end + 2].y + 40 - el.top)); frame(); }
  assert.strictEqual(list.indexOf(atTop()), end + 2);
  assert.strictEqual(el.anchoring, "none");
  const low = seenOf(list[end + 2]);
  const room = el.room;
  resize(list[end + 1], 90);
  now += 16; frame();
  assert.strictEqual(seenOf(list[end + 2]), low, "a change among rows that were written since is made up for");
  assert.strictEqual(el.room, room - 90);`},

		{"a row handed over that loses its place before the commit is over", "", `
  const { el, list, seenOf, atTop, scrollBy, flick, prepend, resize, change } = openRows();
  flick();
  scrollBy(-40);
  assert.strictEqual(window.__agyDeferGrow(), false);
  const kept = atTop();
  prepend(10, 300);
  change(kept, x => { x.laidOut = false; });
  await microtasks();
  assert.deepStrictEqual(named("agy-rows-kept").pop(), { moved: 3000, again: "gone" });
  const top = atTop();
  const at = seenOf(top);
  now += 16; frame();
  resize(list[3], 80);
  now += 16; frame();
  assert.strictEqual(seenOf(top), at, "the row at the top of the screen is followed from there");
  assert.strictEqual(el.writes, 0);`},

		{"a reader stopped at the first row is followed from there", "", `
  const { el, list, seenOf, atTop, scrollBy, flick, resize } = openRows();
  flick();
  scrollBy(-1700);
  assert.strictEqual(el.room, el.top, "the room is given up as far as the reader");
  assert.strictEqual(el.anchoring, "none");
  const top = atTop();
  assert.strictEqual(top, list[0]);
  now += 16; frame();
  // Nothing is above the first row, and a change to it or below it leaves it where it is.
  resize(list[0], 70);
  now += 16; frame();
  assert.strictEqual(seenOf(top), 0);
  assert.strictEqual(el.room, el.top);`},

		// The anchoring comes back on at rest. A change that landed since the last frame is still the tracker's: it
		// would stop following because the anchoring is on, and the browser only answers for what comes after.
		{"a change in the frame the anchoring comes back on", "", `
  const { el, list, seenOf, atTop, scrollBy, flick, prepend, resize } = openRows();
  flick();
  scrollBy(-40);
  assert.strictEqual(window.__agyDeferGrow(), false);
  prepend(10, 300);
  await microtasks();
  const top = atTop();
  const at = seenOf(top);
  for (let k = 0; k < 10; k++) { now += 16; frame(); }
  assert.strictEqual(el.anchoring, "none");
  // A row above changes, and the rest comes due before the next frame is drawn.
  resize(list[4], 90);
  now += 600;
  timersDue();
  assert.strictEqual(el.anchoring, "auto", "the conversation is at rest, and the browser has it back");
  assert.strictEqual(seenOf(top), at, "what changed before that is made up for");
  frame();
  assert.strictEqual(seenOf(top), at);
  resize(list[4], 30);
  now += 16; frame();
  assert.strictEqual(seenOf(top), at, "and what changes after is the browser's, once");`},

		// Finding the bundle's rows goes through the whole conversation, so it is done once for each batch and not in
		// every frame, and not at all for a reader who stays within the row that was handed over.
		{"the bundle's rows are looked for once for each batch", "", `
  const { el, list, seenOf, atTop, scrollBy, flick, prepend, resize, change } = openRows();
  flick();
  scrollBy(-40);
  assert.strictEqual(window.__agyDeferGrow(), false);
  assert.strictEqual(el.looks, 1, "the rows are looked for when the anchoring goes off");
  for (let k = 0; k < 5; k++) { now += 16; scrollBy(10); frame(); }
  assert.strictEqual(el.looks, 1, "and not again for a reader who stays within the row at the top");
  now += 100;
  prepend(10, 300);
  await microtasks();
  for (let k = 0; k < 5; k++) { now += 16; scrollBy(10); frame(); }
  assert.strictEqual(el.looks, 1, "nor for the rows handed over while the reader is still within their row");
  for (let k = 0; k < 40; k++) { now += 16; scrollBy(-60); frame(); }
  assert.strictEqual(el.looks, 2, "one look for a batch, for a reader who crosses many rows");
  assert.strictEqual(el.anchoring, "none");
  // A row that has gone from the page is not in what the page is told next, and the page asks.
  const top = atTop();
  change(top, x => { x.isConnected = false; });
  list.splice(list.indexOf(top), 1);
  now += 300; frame();
  assert.strictEqual(el.looks, 3, "the rows are looked for again when the one followed has gone");
  const next = atTop();
  const from = seenOf(next);
  resize(list[1], 40);
  now += 16; frame();
  assert.strictEqual(seenOf(next), from, "and the row now at the top is followed");
  // A reader who stays past the last row, with something that is not a row filling the screen: the rows are looked
  // for again in case there are more, but not in every frame.
  el.below = 2000;
  const looked = el.looks;
  for (let k = 0; k < 75; k++) { now += 16; scrollBy(400); frame(); }
  assert.strictEqual(atTop(), null, "no row reaches the screen");
  assert.strictEqual(el.anchoring, "none");
  assert.strictEqual(el.looks > looked, true, "looked for again past the last row known");
  assert.strictEqual(el.looks <= looked + 6, true, "but not more often than every quarter of a second: " + (el.looks - looked));`},

		{"a conversation that is hidden is not looked through", "", `
  const { el, list, seenOf, atTop, scrollBy, flick, prepend } = openRows();
  flick();
  scrollBy(-40);
  assert.strictEqual(window.__agyDeferGrow(), false);
  prepend(10, 300);
  await microtasks();
  for (let k = 0; k < 10; k++) { now += 16; scrollBy(-60); frame(); }
  const looked = el.looks;
  const room = el.room;
  el.hidden = true;
  // The bundle goes on asking about rows for it, every tenth of a second.
  for (let k = 0; k < 20; k++) { now += 100; window.__agyDeferGrow(); frame(); }
  assert.strictEqual(el.looks, looked, "its rows are not looked for");
  assert.strictEqual(el.room, room, "and nothing is made of what they read as");`},

		// What is followed is forgotten when it is given up, so that the next time the anchoring goes off the row at the
		// top of the screen is followed again.
		{"a row handed over that has no place, before the bundle's rows are known", "", `
  for (const when of ["as it is handed over", "before the commit is over"]) {
    const c = conversation();
    c.withRows(30, 300);
    c.el.top = 1610;
    window.__agyRowsDrawn(c.el);
    c.scrollBy(0);
    pass(600);
    const { el, list, seenOf, atTop, scrollBy, flick, resize, rowsOf } = c;
    assert.strictEqual(el.room, ROOM);
    const lost = { isConnected: when !== "as it is handed over", laidOut: true, getBoundingClientRect: () => (lost.isConnected ? { top: 150, bottom: 450, width: 400, height: 300 } : { top: 0, bottom: 0, width: 0, height: 0 }) };
    flick();
    scrollBy(-40);
    window.__agyKeepRows(el, lost, 50);
    lost.isConnected = false;
    await microtasks();
    for (let k = 0; k < 3; k++) { now += 16; frame(); }
    // The conversation comes to rest, the bundle says where its rows are, and rows are let in again.
    pass(900);
    assert.strictEqual(el.anchoring, "auto", when);
    window.__agyRowsDrawn(el, rowsOf);
    now += 6000;
    assert.strictEqual(window.__agyDeferGrow(), false, when);
    assert.strictEqual(el.anchoring, "none", when);
    const top = atTop();
    const at = seenOf(top);
    const room = el.room;
    now += 16; frame();
    resize(list[2], 64);
    now += 16; frame();
    assert.strictEqual(seenOf(top), at, "the row at the top of the screen is followed, " + when);
    assert.strictEqual(el.room, room - 64, when);
    pass(6000);
  }`},

		{"a row that loses its place as the conversation comes to rest", "", `
  const { el, list, seenOf, atTop, scrollBy, flick, prepend, change } = openRows();
  flick();
  scrollBy(-40);
  assert.strictEqual(window.__agyDeferGrow(), false);
  const kept = atTop();
  prepend(10, 300);
  await microtasks();
  for (let k = 0; k < 10; k++) { now += 16; frame(); }
  const room = el.room;
  const top = el.top;
  // The row that is followed stops being laid out, and the rest comes due before the next frame is drawn.
  kept.laidOut = false;
  now += 600;
  timersDue();
  assert.strictEqual(el.anchoring, "auto");
  assert.strictEqual(el.room, room, "a row that reads as nothing is not measured in the last look");
  assert.strictEqual(el.top, top);
  assert.strictEqual(el.writes, 0);`},

		{"rows that have no place are passed over", "", `
  const { el, list, seenOf, atTop, scrollBy, flick, prepend, resize, change } = openRows();
  flick();
  scrollBy(-40);
  assert.strictEqual(window.__agyDeferGrow(), false);
  prepend(10, 300);
  await microtasks();
  // Two rows above the reader are not laid out, as a collapsed part of a turn is not.
  change(list[13], x => { x.laidOut = false; });
  change(list[12], x => { x.laidOut = false; });
  now += 16; frame();
  const room = el.room;
  assert.strictEqual(room, ROOM - 3000 + 600, "their height is given back to the room");
  for (let k = 0; k < 20; k++) { now += 16; scrollBy(-60); frame(); }
  const top = atTop();
  assert.strictEqual(top, list[9], "the reader is past them");
  const at = seenOf(top);
  resize(list[5], 77);
  now += 16; frame();
  assert.strictEqual(seenOf(top), at);
  assert.strictEqual(el.room, room - 77);
  // The row at the top is the one followed, and not the last one before the rows that have no place.
  resize(list[11], 55);
  now += 16; frame();
  assert.strictEqual(seenOf(top), at, "a change under the top of the screen moves nothing there");
  assert.strictEqual(el.room, room - 77);
  // The row that is followed stops being laid out: another is, from where it stands, and nothing is made of it.
  change(top, x => { x.laidOut = false; });
  now += 16; frame();
  const next = atTop();
  const from = seenOf(next);
  for (let k = 0; k < 5; k++) { now += 16; frame(); }
  assert.strictEqual(seenOf(next), from, "no drift from a row that could not be measured");
  resize(list[5], -30);
  now += 16; frame();
  assert.strictEqual(seenOf(next), from, "and the next row is followed");`},

		{"a row handed over that has no place, with the bundle's rows known", "", `
  const { el, list, seenOf, atTop, scrollBy, flick, resize, rowsOf } = openRows();
  flick();
  scrollBy(-40);
  assert.strictEqual(window.__agyDeferGrow(), false);
  now += 100;
  const gone = { isConnected: false, getBoundingClientRect: () => ({ top: 0, bottom: 0, width: 0, height: 0 }) };
  window.__agyRowsDrawn(el, rowsOf);
  window.__agyKeepRows(el, gone, 0);
  await microtasks();
  const top = atTop();
  const at = seenOf(top);
  now += 16; frame();
  resize(list[1], 120);
  now += 16; frame();
  assert.strictEqual(seenOf(top), at, "the row at the top of the screen is followed in its stead");
  assert.strictEqual(el.room, ROOM - 120);`},

		{"where the browser does not anchor rows the first frames are followed at rest too", noAnchoring, `
  const { el, seen, scrollBy, rowsArrive, grow } = open();
  now += 5000;
  scrollBy(el.room + 2200 - el.top);
  pass(1000);
  assert.strictEqual(window.__agyDeferGrow(), false);
  pass(304);
  const at = seen();
  rowsArrive(3000, at);
  await microtasks();
  pass(160);
  assert.strictEqual(el.anchoring, "auto");
  grow(300);
  frame();
  frame();
  assert.strictEqual(seen(), at, "a shift in the frames after the rows arrived is made up for, as nothing else would");
  assert.strictEqual(el.room, ROOM - 3300);`},

		// The room given up to a reader who got into it moves the rows that are being followed, and is not taken back
		// for them.
		{"rows followed while the reader reaches the room", "", `
  const { el, seen, scrollBy, flick, rowsArrive } = open();
  flick();
  scrollBy(el.room + 100 - el.top);
  assert.strictEqual(window.__agyDeferGrow(), false);
  rowsArrive(500, seen());
  await microtasks();
  now += 600;
  scrollBy(-700);
  assert.strictEqual(el.room, el.top, "the reader is 100 px into the room, and it is given up to them");
  frame();
  frame();
  assert.strictEqual(el.room, el.top, "which the rows being followed do not take back");`},

		// With no room left a shift during a fling has nowhere to come from, and the position is not written for it under
		// a reader who is moving.
		{"a shift with the room used up and the reader moving", "", `
  const { el, row, seen, scrollBy, flick, rowsArrive } = open();
  flick();
  scrollBy(-40);
  rowsArrive(ROOM, seen());
  await microtasks();
  assert.strictEqual(el.room, 0);
  assert.strictEqual(el.writes, 0);
  scrollBy(-50);
  frame();
  el.rows += 300;
  row.y += 300;
  scrollBy(-50);
  frame();
  assert.strictEqual(el.writes, 0, "nothing is written while the reader scrolls");`},

		// Rows the bundle is told to hold back leave the browser's anchoring on, which keeps the reader's rows in place
		// if something else above them changes meanwhile. It goes off when they are let through.
		{"rows held back leave the browser's anchoring on", "", `
  const c = open();
  fire("touchstart", c.onEl([{}]));
  fire("touchmove", c.onEl([{}]));
  c.scrollBy(-40);
  assert.strictEqual(window.__agyDeferTrim(), true, "rows are not dropped under a finger");
  assert.strictEqual(c.el.anchoring, "auto", "and the anchoring stays on while they wait");
  fire("touchend", c.onEl([]));
  pass(1000);
  assert.strictEqual(window.__agyDeferTrim(), false);
  assert.strictEqual(c.el.anchoring, "none", "it goes off when they are let go");
  // The same for rows to be added where there is no room for them yet.
  const g = conversation();
  g.scrollBy(0);
  fire("touchstart", g.onEl([{}]));
  fire("touchmove", g.onEl([{}]));
  g.scrollBy(-40);
  assert.strictEqual(window.__agyDeferGrow(), true);
  assert.strictEqual(g.el.anchoring, "auto");
  fire("touchend", g.onEl([]));`},

		// The write that makes the room waits for a conversation that is really at rest: no finger on it, however long
		// it has been still, no pointer held, and no fling being ended.
		{"the room's write waits for a conversation at rest", "", `
  const c = conversation();
  window.__agyRowsDrawn(c.el);
  fire("touchstart", c.onEl([{}]));
  c.scrollBy(0);
  pass(12000);
  assert.strictEqual(c.el.room, 0, "nothing is written under a finger, however long it has been still");
  assert.strictEqual(c.el.writes, 0);
  fire("touchend", c.onEl([]));
  pass(600);
  assert.strictEqual(c.el.room, ROOM, "the room is made once the finger is up");
  // A finger that has just come down is no rest either: the browser's anchoring stays off under it.
  c.flick();
  c.scrollBy(-40);
  c.rowsArrive(1000, c.seen());
  await microtasks();
  fire("touchstart", c.onEl([{}]));
  pass(2000);
  assert.strictEqual(c.el.anchoring, "none", "nothing is taken for rest with a finger down");
  fire("touchend", c.onEl([]));
  pass(600);
  assert.strictEqual(c.el.anchoring, "auto");

  const p = conversation();
  p.scrollBy(0);
  fire("pointerdown", p.onEl());
  pass(2000);
  assert.strictEqual(p.el.room, 0, "nor while a pointer is held on the conversation");
  fire("pointerup", {});
  pass(600);
  assert.strictEqual(p.el.room, ROOM);

  // A jump nobody asked for is found when rows are taken out, with no scroll event to say the conversation moved.
  // The reader is put back and the fling ended, which takes the overflow away for two frames. A finger was on this
  // conversation a while ago, so its room waits for rest.
  const s = conversation();
  s.flick();
  now += 20000;
  s.scrollBy(0);
  pass(368);
  events.length = 0;
  s.el.top += 6000;
  rowsRemoved(s.el, 10);
  pass(32);
  assert.strictEqual(named("agy-fling-hold").length, 1);
  assert.strictEqual(s.el.top, 300);
  assert.strictEqual(s.el.room, 0, "nor while a fling is being ended");
  pass(600);
  assert.strictEqual(s.el.room, ROOM);
  assert.strictEqual(s.el.top, ROOM + 300);
  // The hold is still on, and goes on from where the reader's rows now are.
  s.el.top = ROOM - 4000;
  fire("scroll", { target: s.el });
  assert.strictEqual(s.el.top, ROOM + 300, "a hold made before the room is kept at the same rows after it");
  assert.strictEqual(s.el.room, ROOM);`},

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

  // One that hands it over and has not asked how far ahead to load rows says so the first time it draws the
  // conversation again. The scroll that opened the conversation is over by then, and the room is made without
  // waiting for another.
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
  assert.strictEqual(el.anchoring, "auto", "the browser's anchoring is left on for a conversation that only moves");
  assert.strictEqual(window.__agyDeferGrow(), false, "rows do not wait for the fling with room for them");
  assert.strictEqual(el.anchoring, "none", "and it is off from when rows are let in");
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
  assert.strictEqual(el.anchoring, "none", "with the browser's anchoring off for the room's own change");
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
  assert.strictEqual(el.anchoring, "auto", "rows that never come are given up");
  // Rows for a reader who follows the end come in without being handed over, like rows that never came. The next
  // ones that are let in are waited for all the same.
  pass(3000);
  flick();
  scrollBy(-100);
  assert.strictEqual(window.__agyDeferGrow(), false);
  pass(1500);
  assert.strictEqual(el.anchoring, "none", "a wait that ran out starts again with the next rows");`},

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
  // A scroll event is told a frame after the move, and the conversation can be hidden by then. Nothing is learnt
  // from it: WebKit shows the conversation again at the very top, and the reader is put back where they were.
  // Here a script moves the conversation 40 px, which a hidden one seems to have got to: it reads as at its top, and
  // so does the furthest it can go.
  window.__agyScrolledByScript(h.el, h.el.top - 40, false);
  h.el.hidden = true;
  fire("scroll", { target: h.el });
  h.el.hidden = false;
  assert.strictEqual(h.el.room, ROOM, "also when a scroll event arrives after it was hidden");
  h.el.top = 0;
  fire("scroll", { target: h.el });
  assert.strictEqual(h.el.top, ROOM + 260, "and the reader is where they were when it shows again");
  assert.strictEqual(h.el.room, ROOM);
  pass(2000);
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

		// Rows that turn out shorter can leave the position past the end of the conversation, and the browser cuts it
		// back. That is not the reader scrolling, and the row is put back where it was all the same.
		{"rows redrawn shorter near the end", "", `
  const c = conversation();
  c.el.rows = 2000;
  c.el.top = c.el.rows - c.el.screen - 200;
  c.row.y = c.el.top + 60;
  window.__agyRowsDrawn(c.el);
  c.scrollBy(0);
  pass(600);
  assert.strictEqual(c.el.room, ROOM);
  c.el.writes = 0;
  c.flick();
  c.scrollBy(-10);
  assert.strictEqual(window.__agyDeferGrow(), false);
  const at = c.seen();
  c.rowsArrive(3000, at);
  assert.strictEqual(c.seen(), at);
  // Their tables are drawn again 350 px shorter, with the reader 210 px from the end.
  c.el.rows -= 350;
  c.row.y -= 350;
  await microtasks();
  assert.strictEqual(c.seen(), at, "the row is where it was, though the position was cut back by 140 px");
  assert.strictEqual(c.el.room, ROOM - 3000 + 210);
  assert.strictEqual(c.el.writes, 0);
  for (let k = 0; k < 30; k++) frame();
  assert.strictEqual(c.seen(), at);`},

		// What gets shorter under the top of the screen brings the rows above it down when the position is cut back, in
		// the commit as at any other time. Room taken for that would shorten the conversation by as much again.
		{"rows redrawn shorter under the top of the screen near the end", "", `
  const { el, list, seenOf, atTop, scrollBy, flick, prepend, resize } = openRows();
  const end = () => el.laid + el.rows + el.below - el.screen;
  flick();
  for (let k = 0; k < 200 && el.top < end() - 200; k++) { now += 16; scrollBy(Math.min(400, end() - 200 - el.top)); frame(); }
  pass(1000);
  assert.strictEqual(window.__agyDeferGrow(), false);
  flick();
  scrollBy(-10);
  const kept = atTop();
  const at = seenOf(kept);
  prepend(10, 300);
  assert.strictEqual(seenOf(kept), at);
  assert.strictEqual(el.room, ROOM - 3000);
  // A row on the screen is drawn again 350 px shorter before the commit is over, with the reader 210 px from the end.
  resize(list[list.indexOf(kept) + 1], -290);
  resize(list[list.indexOf(kept) + 2], -60);
  await microtasks();
  assert.strictEqual(end() - el.top, 0, "the reader is at the end");
  assert.strictEqual(seenOf(kept), at + 140, "the rows above came down by what the position was cut back");
  assert.strictEqual(el.room, ROOM - 3000, "and no room is taken for that");
  for (let k = 0; k < 30; k++) { now += 16; frame(); }
  assert.strictEqual(el.room, ROOM - 3000);
  assert.strictEqual(el.writes, 0);`},

		// At the end of the conversation the reader is where the end puts them. Whatever gets shorter there, above the
		// screen or under it, the browser cuts the position back, and nothing the room does can undo that: room given
		// takes the reader off the end, and room taken shortens the conversation again by as much.
		{"a reader who has gone down to the end, and rows above drawn again shorter", "", `
  const { el, list, seenOf, atTop, scrollBy, flick, prepend, resize } = openRows();
  const end = () => el.laid + el.rows + el.below - el.screen;
  flick();
  scrollBy(-40);
  assert.strictEqual(window.__agyDeferGrow(), false);
  prepend(10, 300);
  await microtasks();
  // The reader goes down to the end within the same run of scrolling, so the conversation never rests.
  for (let k = 0; k < 200 && el.top < end(); k++) { now += 16; scrollBy(Math.min(400, end() - el.top)); frame(); }
  assert.strictEqual(el.top, end(), "at the end");
  assert.strictEqual(el.anchoring, "none", "still within one run of scrolling");
  const top = atTop();
  const at = seenOf(top);
  const room = el.room;
  // A row above the screen is drawn again 137 px shorter. The browser cuts the position back to the new end.
  resize(list[3], -137);
  for (let k = 0; k < 4; k++) { now += 16; frame(); }
  assert.strictEqual(seenOf(top), at, "the rows on the screen have not moved");
  assert.strictEqual(el.room, room, "no room is given for a position the browser cut back");
  assert.strictEqual(end() - el.top, 0, "and the reader is still at the end");`},

		{"a reader who follows the end while rows above are dropped", "", `
  const { el, list, seenOf, atTop, scrollBy, flick, withRows, change } = openRows();
  const end = () => el.laid + el.rows + el.below - el.screen;
  // The reader follows the end of a reply that is being written: no finger, and the bundle keeps them at the end.
  flick();
  for (let k = 0; k < 200 && el.top < end(); k++) { now += 16; scrollBy(Math.min(400, end() - el.top)); frame(); }
  now += 20000;
  pass(1000);
  assert.strictEqual(el.top, end());
  assert.strictEqual(el.anchoring, "auto");
  el.writes = 0;
  const stream = n => { for (let k = 0; k < n; k++) { now += 16; withRows(1, 180); scrollBy(end() - el.top); frame(); now += 16; frame(); } };
  stream(5);
  // The bundle asks whether rows far above may be dropped, and drops them. For a reader who follows the end it hands
  // nothing over. It does so twice, with the reply going on between and never 400 ms from one row to the next.
  for (const drop of ["first", "second"]) {
    assert.strictEqual(window.__agyDeferTrim(), false);
    assert.strictEqual(el.anchoring, "none");
    const top = atTop();
    const at = seenOf(top);
    const room = el.room;
    change(null, () => { list.splice(0, 5).forEach(r => { r.isConnected = false; }); });
    for (let k = 0; k < 2; k++) { now += 16; frame(); }
    assert.strictEqual(seenOf(top), at, "the rows on the screen have not moved, " + drop);
    assert.strictEqual(el.room, room, "no room is given, " + drop);
    assert.strictEqual(end() - el.top, 0, "the reader is at the end, " + drop);
    stream(12);
    assert.strictEqual(el.anchoring, "none", "the conversation has not come to rest");
    assert.strictEqual(end() - el.top, 0);
  }
  pass(1000);
  assert.strictEqual(el.anchoring, "auto");
  assert.strictEqual(end() - el.top, 0, "and is still there at rest");
  assert.strictEqual(el.writes, 0);`},

		{"a reader brought to the end by what gets shorter under the screen", "", `
  const { el, list, seenOf, atTop, scrollBy, flick, resize } = openRows();
  const end = () => el.laid + el.rows + el.below - el.screen;
  // The reader is 100 px above the end, and the bundle is told it may bring rows in: the anchoring goes off.
  now += 20000;
  flick();
  for (let k = 0; k < 200 && el.top < end() - 100; k++) { now += 16; scrollBy(Math.min(400, end() - 100 - el.top)); frame(); }
  pass(1000);
  assert.strictEqual(el.anchoring, "auto");
  assert.strictEqual(window.__agyDeferGrow(), false);
  flick();
  assert.strictEqual(el.anchoring, "none");
  now += 16; frame();
  // The reader goes up 300 px.
  for (let k = 0; k < 6; k++) { now += 16; scrollBy(-50); frame(); }
  const top = atTop();
  const at = seenOf(top);
  const room = el.room;
  el.writes = 0;
  // Something under the screen is drawn again 500 px shorter, as a block that folds away, and the browser cuts the
  // position back to the new end: the rows on the screen come down 100 px, as they do without the tracker.
  resize(list[list.length - 1], -250);
  resize(list[list.length - 2], -250);
  for (let k = 0; k < 60; k++) {
    now += 16; frame();
    assert.strictEqual(el.room, room, "no room is taken, frame " + k);
    assert.strictEqual(seenOf(top), at + 100);
  }
  assert.strictEqual(end() - el.top, 0);
  assert.strictEqual(el.writes, 0, "and no position is written");
  // The rows that were let in never come, and once they are no longer waited for the conversation is at rest.
  pass(6000);
  assert.strictEqual(el.anchoring, "auto", "the conversation comes to rest");
  assert.strictEqual(el.room, room);`},

		{"a reader at the end when the last row gets shorter", "", `
  const { el, list, seenOf, atTop, scrollBy, flick, resize } = openRows();
  const end = () => el.laid + el.rows + el.below - el.screen;
  flick();
  for (let k = 0; k < 200 && el.top < end(); k++) { now += 16; scrollBy(Math.min(400, end() - el.top)); frame(); }
  now += 20000;
  pass(1000);
  assert.strictEqual(el.top, end());
  assert.strictEqual(el.anchoring, "auto");
  assert.strictEqual(el.room, ROOM);
  // The bundle asks whether it may bring rows in above, and is told it may: the anchoring goes off.
  assert.strictEqual(window.__agyDeferGrow(), false);
  assert.strictEqual(el.anchoring, "none");
  now += 16; frame();
  const top = atTop();
  const at = seenOf(top);
  el.writes = 0;
  // The last row is drawn again 100 px shorter, as when a progress line goes away. The browser keeps the reader at
  // the end, which lowers every row on the screen by 100 px. Nothing can put them back: there is no more below.
  resize(list[list.length - 1], -100);
  for (let k = 0; k < 120; k++) {
    now += 16; frame();
    assert.strictEqual(el.room, ROOM, "the room is as it was, frame " + k);
  }
  assert.strictEqual(seenOf(top), at + 100);
  assert.strictEqual(el.top, end());
  assert.strictEqual(el.writes, 0, "and no position is written");
  // The rows are followed from where the end left them: a reader who then goes up and stops finds nothing made up
  // for late.
  flick();
  for (let k = 0; k < 5; k++) { now += 16; scrollBy(-40); frame(); }
  for (let k = 0; k < 5; k++) { now += 16; frame(); }
  assert.strictEqual(seenOf(top), at + 100 + 200, "the rows move by the reader's own scrolling and no more");
  assert.strictEqual(el.room, ROOM);`},

		// Rows dropped above a reader who is near the end and not following it are handed over. What they took leaves
		// the position past the end, the browser cuts it back, and the bundle's move puts the rows where they were.
		{"rows dropped above a reader near the end", "", `
  const { el, list, seenOf, atTop, scrollBy, flick, place, rowsOf } = openRows();
  const end = () => el.laid + el.rows + el.below - el.screen;
  flick();
  for (let k = 0; k < 200 && el.top < end() - 50; k++) { now += 16; scrollBy(Math.min(400, end() - 50 - el.top)); frame(); }
  now += 20000;
  pass(1000);
  assert.strictEqual(end() - el.top, 50);
  assert.strictEqual(window.__agyDeferTrim(), false);
  const kept = atTop();
  const at = seenOf(kept);
  list.splice(0, 5).forEach(r => { r.isConnected = false; });
  place();
  window.__agyRowsDrawn(el, rowsOf);
  window.__agyKeepRows(el, kept, at);
  await microtasks();
  assert.strictEqual(seenOf(kept), at, "the row is where it was");
  assert.strictEqual(end() - el.top, 50, "and the reader as far from the end as before");
  assert.strictEqual(el.room, ROOM + 50, "by the 50 px the cut left the rows higher");
  assert.strictEqual(el.writes, 0);`},

		// A conversation that has just appeared has no fling going, so its room is made in the frame after it is drawn
		// and is there for a reader who flicks at once. It was made at the first rest, which such a reader never gave it.
		{"a conversation has its room as it is drawn", "", `
  const { el, seen, scrollBy, flick, rowsArrive } = conversation();
  window.__agyRowsDrawn(el);
  assert.strictEqual(el.room, 0);
  const at = seen();
  now += 16; frame();
  assert.strictEqual(el.room, ROOM, "in the frame after it is drawn");
  assert.strictEqual(seen(), at, "with the rows where they were");
  assert.deepStrictEqual(named("agy-room"), [{ what: "top", by: ROOM }]);
  const asked = frames.length;
  window.__agyRowsDrawn(el);
  assert.strictEqual(frames.length, asked, "and no frame is asked for at a drawing once it has it");
  // The reader flicks up at once and goes on without a rest. Rows are let in and take their height from the room.
  events.length = 0;
  el.writes = 0;
  flick();
  for (let k = 0; k < 6; k++) { now += 16; scrollBy(-40); frame(); }
  assert.strictEqual(window.__agyDeferGrow(), false, "rows are let in while the reader moves");
  const kept = seen();
  rowsArrive(3000, kept);
  await microtasks();
  assert.strictEqual(seen(), kept);
  assert.strictEqual(el.room, ROOM - 3000);
  assert.strictEqual(el.writes, 0, "no position is written");
  assert.deepStrictEqual(named("agy-grow-wait"), []);
  assert.deepStrictEqual(named("agy-fling-stop"), []);`},

		// The bundle tells of a conversation when it draws it again, which is some time after it appears. The app
		// scrolls a conversation to its end as it opens, and that is heard sooner.
		{"a conversation is heard of when it is scrolled as it opens", "", `
  // The page has handed rows over before, in another conversation.
  open();
  const { el, seen, scrollBy } = conversation();
  const at = seen();
  scrollBy(0);
  assert.strictEqual(el.room, 0);
  now += 16; frame();
  assert.strictEqual(el.room, ROOM, "its room is made in the frame after");
  assert.strictEqual(seen(), at);
  // One that a finger is already on when it is first heard of waits for rest, drawn or not.
  const touched = conversation();
  touched.flick();
  touched.scrollBy(-20);
  for (let k = 0; k < 3; k++) { now += 16; frame(); }
  assert.strictEqual(touched.el.room, 0);
  const asked = frames.length;
  window.__agyRowsDrawn(touched.el);
  assert.strictEqual(frames.length, asked, "no frame is asked for at its drawing");
  for (let k = 0; k < 3; k++) { now += 16; frame(); }
  assert.strictEqual(touched.el.room, 0, "nor is it given any");
  pass(1000);
  assert.strictEqual(touched.el.room, ROOM, "at rest");`},

		// The first conversation of a page is drawn a second time some tenths of a second after it appears, which was
		// when the page first knew the bundle hands its rows over. Such a bundle asks how far ahead to load rows as it
		// loads, and that is taken as its word.
		{"the first conversation of a page has its room as it opens", "", `
  assert.strictEqual(window.__agyGrowRadius, 2400, "the bundle asks, and rows are asked for from further away");
  const { el, seen, scrollBy, flick, rowsArrive } = conversation();
  const at = seen();
  scrollBy(0);
  assert.strictEqual(el.room, 0);
  now += 16; frame();
  assert.strictEqual(el.room, ROOM, "made in the frame after it is scrolled to its end, with nothing drawn a second time");
  assert.strictEqual(seen(), at);
  assert.deepStrictEqual(named("agy-room"), [{ what: "top", by: ROOM }]);
  // The reader flicks at once, and the rows that arrive come out of the room.
  el.writes = 0;
  flick();
  for (let k = 0; k < 6; k++) { now += 16; scrollBy(-40); frame(); }
  assert.strictEqual(window.__agyDeferGrow(), false);
  const kept = seen();
  rowsArrive(3000, kept);
  await microtasks();
  assert.strictEqual(seen(), kept);
  assert.strictEqual(el.room, ROOM - 3000);
  assert.strictEqual(el.writes, 0, "no position is written");
  assert.deepStrictEqual(["agy-grow-wait", "agy-fling-stop"].map(type => named(type).length), [0, 0]);`},

		// Rows dropped above a conversation as it opens give their height to the room, which is then a little of what
		// it should be. It is filled in the frame after, as one with none is given it.
		{"a conversation left a little room by rows dropped as it opens", "", `
  const { el, seen, rowsArrive } = conversation();
  const at = seen();
  window.__agyRowsDrawn(el);
  rowsArrive(-2000, at);
  await microtasks();
  assert.strictEqual(el.room, 2000);
  assert.strictEqual(seen(), at);
  el.writes = 0;
  now += 16; frame();
  assert.strictEqual(el.room, ROOM, "filled in the frame after");
  assert.strictEqual(seen(), at, "with the rows where they were");
  assert.strictEqual(el.writes, 1);
  // Once it has enough, what rows take from it is filled back at rest and not as it is drawn.
  rowsArrive(3000, at);
  await microtasks();
  assert.strictEqual(el.room, ROOM - 3000);
  el.writes = 0;
  window.__agyRowsDrawn(el);
  for (let k = 0; k < 3; k++) { now += 16; frame(); }
  assert.strictEqual(el.room, ROOM - 3000);
  assert.strictEqual(el.writes, 0);
  // One whose frame came with a finger down somewhere else is filled when it is next drawn.
  const b = conversation();
  const there = b.seen();
  window.__agyRowsDrawn(b.el);
  b.rowsArrive(-2000, there);
  await microtasks();
  fire("touchstart", { target: { closest: () => null }, touches: [{}] });
  now += 16; frame();
  assert.strictEqual(b.el.room, 2000);
  fire("touchend", { target: { closest: () => null }, touches: [] });
  window.__agyRowsDrawn(b.el);
  now += 16; frame();
  assert.strictEqual(b.el.room, ROOM);
  assert.strictEqual(b.seen(), there);`},

		{"a conversation drawn before it has rows to fill the screen", "", `
  const { el, seen } = conversation();
  el.rows = 400;
  el.top = 0;
  window.__agyRowsDrawn(el);
  for (let k = 0; k < 3; k++) { now += 16; frame(); }
  assert.strictEqual(el.room, 0, "none while its rows do not fill the screen");
  // The rest of its rows are drawn.
  el.rows = 9000;
  window.__agyRowsDrawn(el);
  now += 16; frame();
  assert.strictEqual(el.room, ROOM, "made when it is drawn with enough of them");
  // One with no older rows to load gets none however it is drawn.
  const last = conversation();
  last.el.older.disabled = true;
  window.__agyRowsDrawn(last.el);
  for (let k = 0; k < 3; k++) { now += 16; frame(); }
  assert.strictEqual(last.el.room, 0);
  // Nor does one that has no button to load them with, as it is drawn or at rest.
  const bare = conversation();
  bare.el.querySelector = () => null;
  window.__agyRowsDrawn(bare.el);
  bare.scrollBy(0);
  for (let k = 0; k < 3; k++) { now += 16; frame(); }
  assert.strictEqual(bare.el.room, 0);
  pass(1000);
  assert.strictEqual(bare.el.room, 0);`},

		// A position put back after the room was made as the conversation was drawn says that something moves the
		// conversation after all. The room is taken off, as it is at rest, and is tried again at rest.
		{"the room made as a conversation is drawn is taken back", "", `
  const { el, seen } = conversation();
  const at = seen();
  window.__agyRowsDrawn(el);
  now += 16; frame();
  assert.strictEqual(el.room, ROOM);
  // The position it had is put back.
  el.top = 300;
  fire("scroll", { target: el });
  assert.strictEqual(el.room, 0, "the room goes with it");
  assert.strictEqual(seen(), at);
  assert.deepStrictEqual(named("agy-room").map(e => e.what), ["top", "back"]);
  window.__agyRowsDrawn(el);
  for (let k = 0; k < 3; k++) { now += 16; frame(); }
  assert.strictEqual(el.room, 0, "and is not made again as the conversation is drawn");
  // Time passes until the conversation has its room, which is at its next rest.
  const untilRoom = () => { for (let k = 0; k < 100 && !el.room; k++) pass(16); };
  untilRoom();
  assert.strictEqual(el.room, ROOM, "it is at rest");
  // That first one was not counted against the conversation. It is left alone after two at rest, as it was before
  // it was given room any sooner.
  el.top = 300;
  frame();
  assert.strictEqual(el.room, 0);
  fire("scroll", { target: el });
  untilRoom();
  assert.strictEqual(el.room, ROOM, "after one taken back at rest it is tried again");
  el.top = 300;
  frame();
  assert.strictEqual(el.room, 0);
  fire("scroll", { target: el });
  pass(2000);
  assert.strictEqual(el.room, 0, "and after two it is left alone");
  assert.deepStrictEqual(named("agy-room").map(e => e.what), ["top", "back", "top", "back", "top", "back"]);`},

		// Once a finger or a wheel has been on a conversation there may be a fling, and the room waits for rest.
		{"a conversation that a finger was on before it could be given room", "", `
  const { el, scrollBy, flick, onEl } = conversation();
  el.rows = 400;
  el.top = 0;
  window.__agyRowsDrawn(el);
  now += 16; frame();
  flick();
  el.rows = 9000;
  el.top = 300;
  window.__agyRowsDrawn(el);
  for (let k = 0; k < 5; k++) { now += 16; scrollBy(-10); frame(); }
  assert.strictEqual(el.room, 0, "not while it may be flung");
  pass(1000);
  assert.strictEqual(el.room, ROOM, "at rest, as before");
  // One that is drawn with a finger down waits for the finger.
  const held = conversation();
  fire("touchstart", { target: { closest: () => null }, touches: [{}] });
  window.__agyRowsDrawn(held.el);
  for (let k = 0; k < 3; k++) { now += 16; frame(); }
  assert.strictEqual(held.el.room, 0, "none with a finger down");
  fire("touchend", { target: { closest: () => null }, touches: [] });
  window.__agyRowsDrawn(held.el);
  now += 16; frame();
  assert.strictEqual(held.el.room, ROOM, "the finger was not on a conversation, and is up");
  // Where nothing draws or scrolls it again after the finger is up, it has its room at rest.
  const quiet = conversation();
  fire("touchstart", { target: { closest: () => null }, touches: [{}] });
  window.__agyRowsDrawn(quiet.el);
  now += 16; frame();
  fire("touchend", { target: { closest: () => null }, touches: [] });
  for (let k = 0; k < 3; k++) { now += 16; frame(); }
  assert.strictEqual(quiet.el.room, 0);
  pass(1000);
  assert.strictEqual(quiet.el.room, ROOM);`},

		// Its first rest ends that as well. From then on something other than a finger may be moving a conversation,
		// a key or the app's own smooth scroll, and only rest says that nothing is.
		{"a conversation that has come to rest is given room at rest", "", `
  const { el, scrollBy } = conversation();
  el.older.disabled = true;
  window.__agyRowsDrawn(el);
  scrollBy(0);
  pass(1000);
  assert.strictEqual(el.room, 0, "none with no older rows to load");
  // Rows above the reader are dropped as a reply grows, and there are older ones to load now.
  el.older.disabled = false;
  window.__agyRowsDrawn(el);
  scrollBy(0);
  for (let k = 0; k < 3; k++) { now += 16; frame(); }
  assert.strictEqual(el.room, 0, "not as it is drawn or scrolled");
  pass(1000);
  assert.strictEqual(el.room, ROOM, "at its next rest");`},

		// What counts is whether a finger has been on a conversation since this one appeared, not how long ago: a
		// reader who leaves a conversation they were flicking and opens another has room in the new one at once.
		{"a conversation opened straight after another was flicked", "", `
  const first = open();
  first.flick();
  for (let k = 0; k < 4; k++) { now += 16; first.scrollBy(-60); frame(); }
  first.el.isConnected = false;
  now += 300;
  events.length = 0;
  const { el, seen, scrollBy } = conversation();
  // A browser tells the page of a position that script sets, as it is set.
  const own = Object.getOwnPropertyDescriptor(el, "scrollTop");
  Object.defineProperty(el, "scrollTop", { get: own.get, set(v) { window.__agyScrolledByScript(el, v); own.set.call(el, v); } });
  const at = seen();
  // The app scrolls it to its end as it opens, and with the flick of a moment ago that reads as a conversation
  // still moving.
  scrollBy(0);
  now += 16; frame();
  assert.strictEqual(el.room, ROOM);
  assert.strictEqual(seen(), at);
  // The room's write is no move of the app's to end a fling for. There is none on this conversation, and with its
  // overflow taken away for two frames a finger that lands in them scrolls nothing.
  assert.deepStrictEqual(named("agy-fling-stop"), []);
  assert.strictEqual(el.style.overflowY, "");
  // A move of the app's that far still ends one.
  now += 16;
  scrollBy(-10);
  el.scrollTop = el.top - 5000;
  assert.strictEqual(named("agy-fling-stop").length, 1);`},

		// The scroll event that tells of the room's write is not the conversation moving. Rows the bundle drops as it
		// opens a conversation wait for one that moves, and were held back for this until the reader had started.
		{"the room's write is not taken for the conversation moving", "", `
  const first = open();
  first.flick();
  for (let k = 0; k < 4; k++) { now += 16; first.scrollBy(-60); frame(); }
  first.el.isConnected = false;
  now += 300;
  const opened = () => {
    const c = conversation();
    c.scrollBy(0);
    now += 16; frame();
    assert.strictEqual(c.el.room, ROOM);
    return c;
  };
  const a = opened();
  // The page is busy drawing the conversation, and the event for the room's write comes when it is done.
  now += 400;
  for (let k = 0; k < 3; k++) { now += 16; frame(); }
  fire("scroll", { target: a.el });
  assert.strictEqual(window.__agyDeferTrim(), false, "rows are dropped as they would be with no room made");
  // An event after that one is the conversation moving, wherever it finds the position.
  now += 400;
  fire("scroll", { target: a.el });
  assert.strictEqual(window.__agyDeferTrim(), true);
  // So is the first, where the position is not what the room wrote: something else has moved it since.
  const b = opened();
  now += 400;
  for (let k = 0; k < 3; k++) { now += 16; frame(); }
  b.el.top -= 40;
  fire("scroll", { target: b.el });
  assert.strictEqual(window.__agyDeferTrim(), true);`},

		{"a conversation that is hidden or gone when its frame comes", "", `
  const a = conversation();
  window.__agyRowsDrawn(a.el);
  a.el.hidden = true;
  now += 16; frame();
  assert.strictEqual(a.el.room, 0, "a hidden one is left as it is");
  a.el.hidden = false;
  window.__agyRowsDrawn(a.el);
  now += 16; frame();
  assert.strictEqual(a.el.room, ROOM, "and given its room when it is drawn again");
  const b = conversation();
  const asked = frames.length;
  window.__agyRowsDrawn(b.el);
  window.__agyRowsDrawn(b.el);
  assert.strictEqual(frames.length, asked + 1, "one frame is asked for however often it is drawn");
  b.el.isConnected = false;
  now += 16; frame();
  assert.strictEqual(b.el.room, 0);`},

		// A finger can land as the room is filled, and iOS then puts the old position back. The room goes back to what it
		// was, so the rows are where that position has them. After the second time it is left alone.
		{"the room's own write is taken back", "", `
  const d = conversation();
  window.__agyRowsDrawn(d.el);
  // A finger has been on the conversation since it appeared, so its room waits for rest.
  d.flick();
  d.scrollBy(0);
  frame();
  assert.strictEqual(d.el.room, 0);
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
  assert.deepStrictEqual(named("agy-room").map(e => e.what), ["top", "back", "top", "back"]);
  // That is this conversation's count. The next one opened on the page has its own.
  const next = conversation();
  next.scrollBy(0);
  pass(1000);
  assert.strictEqual(next.el.room, ROOM, "another conversation still gets its room");`},

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

		// A conversation whose markup does not grow by the padding the room is: its room is given up for good, and
		// another conversation on the page is tried on its own.
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
  pass(1000);
  assert.strictEqual(e.el.room, 0, "nor is it tried again for that conversation");
  assert.strictEqual(named("agy-room").filter(x => x.what === "none").length, 1);
  const later = conversation();
  later.scrollBy(0);
  pass(1000);
  assert.strictEqual(later.el.room, ROOM, "another conversation, which does lay it out, has its room");`},

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

		// After a touch a fling may still be going, and a page held up for half a second sees all of that fling's way at
		// once. That is the reader's too. A move as long in a single frame is faster than any fling, and is a throw.
		{"a fling seen late and a throw seen at once", "", `
  const start = () => { const c = conversation(); c.el.rows = 100000; c.el.top = 3000; window.__agyRowsDrawn(c.el); c.scrollBy(0); pass(1200); assert.strictEqual(c.el.room, ROOM); now += 20000; events.length = 0; return c; };
  const f = start();
  f.flick();
  now += 16;
  f.scrollBy(-200);
  now += 500;
  f.scrollBy(-6000);
  assert.strictEqual(f.el.room, f.el.top, "6,000 px in half a second is a fling, stopped at the first row");
  assert.deepStrictEqual(named("agy-fling-hold"), []);
  const t = start();
  t.flick();
  now += 16;
  t.scrollBy(-200);
  now += 16;
  t.scrollBy(-6000);
  assert.strictEqual(t.el.top, ROOM + 2800, "6,000 px in one frame is not, and the reader is put back");
  assert.strictEqual(t.el.room, ROOM);
  assert.strictEqual(named("agy-fling-hold").length, 1);`},

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
  assert.strictEqual(window.__agyIsSpontaneousScroll(c.el), false, "and so is a drop after the write has settled");
  // A room with little left in it is given up the same way.
  const small = conversation();
  small.el.top = small.el.rows - small.el.screen - 40;
  small.scrollBy(0);
  pass(1000);
  assert.strictEqual(small.el.room, ROOM);
  small.flick();
  small.scrollBy(-10);
  small.rowsArrive(ROOM - 1500, small.seen());
  await microtasks();
  assert.strictEqual(small.el.room, 1500);
  small.el.older.disabled = true;
  for (let k = 0; k < 100 && small.el.room > 0; k++) pass(16);
  assert.strictEqual(small.el.room, 0);
  assert.strictEqual(window.__agyIsSpontaneousScroll(small.el), true, "however little room it was");`},
	} {
		t.Run(c.name, func(t *testing.T) {
			harness := roomPage + "(async () => {" + c.js + "\n})().catch(e => { console.error(e); process.exit(1); });\n"
			args := []string{"-e", harness, scriptPath}
			cmd := exec.Command(node, args...)
			switch c.ua {
			case "":
			case noAnchoring:
				cmd.Env = append(os.Environ(), "NO_ANCHORING=1")
			default:
				cmd = exec.Command(node, append(args, c.ua)...)
			}
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}

// roomWalk walks the page at random, a seed for each walk. The reader scrolls, goes to the end and follows it while a
// reply is written, rows are let in and handed over with some drawn again shorter before the commit is over, rows are
// dropped above with and without being handed over, rows change height or stop being laid out above the screen, on
// it and under it, and the conversation rests. After every step the row that was at the top of the screen is where
// a browser that anchored to it would have left it, with what the end of the conversation changes about that: see
// expected. Four idle frames then have to leave the room and the position as they were.
const roomWalk = `
  const seed = Number(process.env.WALK_SEED);
  const steps = Number(process.env.WALK_STEPS);
  let s = seed * 2654435761 % 4294967296;
  const rnd = () => { s = (s * 1664525 + 1013904223) % 4294967296; return s / 4294967296; };
  const pick = (a, b) => Math.floor(a + rnd() * (b - a + 1));
  const { el, list, seenOf, atTop, scrollBy, flick, withRows, prepend, change, place, rowsOf } = process.env.WALK_FRESH ? freshRows() : openRows();
  const among = () => el.top - el.laid;
  const last = () => Math.max(0, el.rows + el.below - el.screen);
  const frames = n => { for (let k = 0; k < n; k++) { now += 16; frame(); } };
  // The reader goes somewhere among the rows, a finger down first and no further than a fling in each frame.
  const go = to => { flick(); for (let k = 0; k < 600 && Math.abs(among() - to) >= 1; k++) { now += 16; scrollBy(Math.max(-400, Math.min(400, to - among()))); frame(); } };
  const onPage = r => r.laidOut && r.isConnected;
  const trail = [];
  // Where the row is expected among the rows. A browser that anchors to it moves the position by what changed above
  // it, as far as the conversation goes. With the anchoring off the tracker does the same out of the room, except at
  // the end: a position the change left at or past the end stays where the browser cut it to, unless the change is
  // one the bundle handed over.
  const expected = (kind, off, a0, moved) => {
    if (kind === "change" && off && a0 >= last() - 1) return Math.min(a0, last());
    return Math.max(0, Math.min(last(), a0 + moved));
  };
  for (let i = 0; i < steps; i++) {
    const r = rnd();
    const top = atTop();
    const y0 = top.y;
    const a0 = among();
    const off = el.anchoring === "none";
    const atEnd = a0 >= last() - 1;
    let kind = "";
    let what = "";
    if (r < 0.22) {
      const d = rnd() < 0.15 ? pick(1500, 6000) : pick(-450, 300);
      go(Math.max(260, Math.min(last(), a0 + d)));
      what = "scroll " + d;
    } else if (r < 0.30) {
      go(last());
      what = "to the end";
    } else if (r < 0.48) {
      if (a0 > 2400) { frames(2); what = "rows not wanted"; kind = "idle"; }
      else if (window.__agyDeferGrow()) { frames(6); what = "rows held"; kind = "idle"; }
      else {
        frames(pick(0, 12));
        const added = prepend(pick(3, 20), pick(60, 240));
        if (rnd() < 0.6) { added.slice(0, pick(1, 3)).forEach(row => { row.h = pick(20, 50); }); place(); }
        await microtasks();
        what = "rows in " + added.length;
        kind = "commit";
      }
    } else if (r < 0.58) {
      let n = 0, tall = 0;
      while (n < list.length && tall + list[n].h < a0 - 3000) { tall += list[n].h; n++; }
      if (!n) { frames(2); what = "nothing to drop"; kind = "idle"; }
      else if (window.__agyDeferTrim()) { frames(16); what = "drop held"; kind = "idle"; }
      else if (atEnd) {
        // A reader who follows the end has nothing handed over.
        frames(pick(0, 3));
        change(null, () => { list.splice(0, n).forEach(row => { row.isConnected = false; }); });
        what = "dropped " + n + " under a reader who follows the end";
        kind = "change";
      } else {
        frames(pick(0, 3));
        const at = seenOf(top);
        list.splice(0, n).forEach(row => { row.isConnected = false; });
        place();
        window.__agyRowsDrawn(el, rowsOf);
        window.__agyKeepRows(el, top, at);
        await microtasks();
        what = "dropped " + n;
        kind = "commit";
      }
    } else if (r < 0.78) {
      const near = list.filter(row => row.y + row.h > a0 - 6000 && row.y < a0 + el.screen + 2500);
      const row = near[pick(0, near.length - 1)];
      const where = !onPage(row) ? "that was not laid out" : row.y + row.h <= a0 ? "above the screen" : row.y >= a0 + el.screen ? "under the screen" : row === top ? "at the top edge" : "on the screen";
      if (!onPage(row)) change(row, x => { x.laidOut = true; });
      else if (rnd() < 0.2 && row !== top) change(row, x => { x.laidOut = false; });
      else change(row, x => { x.h = pick(40, 400); });
      what = "a row " + where + " changes";
      kind = "change";
      // The conversation can come to rest before a frame has seen the change.
      if (rnd() < 0.25) { now += 600; timersDue(); what += ", and the rest comes due before a frame"; }
    } else if (r < 0.86) {
      if (!atEnd) { frames(2); what = "no reply"; kind = "idle"; }
      else {
        // A reply is written at the end, and the bundle keeps the reader there.
        const n = pick(1, 8);
        for (let k = 0; k < n; k++) { now += 16; withRows(1, pick(40, 200)); scrollBy(last() - among()); frame(); }
        what = "a reply of " + n + " rows";
      }
    } else if (r < 0.94) {
      frames(pick(1, 30));
      what = "frames";
      kind = "idle";
    } else {
      pass(pick(450, 1500));
      what = "rest";
      kind = "idle";
    }
    trail.push(what + (off ? " (anchoring off)" : ""));
    const told = " | seed " + seed + ", step " + i + ": " + trail.slice(-6).join(" > ");
    if (kind) {
      frames(3);
      if (onPage(top)) {
        const want = top.y - expected(kind, off, a0, top.y - y0);
        assert.ok(Math.abs(seenOf(top) - want) <= 2, "the row at the top of the screen is at " + seenOf(top) + ", and a browser would have left it at " + want + told);
      }
    }
    const room = el.room;
    const at = el.top;
    frames(4);
    assert.strictEqual(el.room, room, "the room goes on changing with nothing happening" + told);
    assert.strictEqual(el.top, at, "the position goes on changing with nothing happening" + told);
    assert.ok(el.room >= 0 && Number.isFinite(el.room), "the room is " + el.room + told);
  }`

// On a page of its own for each seed, as an iPhone and as an Android phone, which has no room and leaves everything
// to the browser. An iPhone's walk starts from a conversation that has come to rest, or from one that has just
// appeared and has its room from the frame before.
func TestRandomWalksLeaveTheRowAtTheTopWhereABrowserWould(t *testing.T) {
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
	harness := roomPage + "(async () => {" + roomWalk + "\n})().catch(e => { console.error(e); process.exit(1); });\n"
	const android = "Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 Chrome/131.0 Mobile Safari/537.36"
	for _, c := range []struct {
		name, ua string
		fresh    bool
		walks    int
	}{{"iPhone", "", false, 120}, {"iPhone, as the conversation appears", "", true, 60}, {"Android", android, false, 30}} {
		for seed := 1; seed <= c.walks; seed++ {
			args := []string{"-e", harness, scriptPath}
			if c.ua != "" {
				args = append(args, c.ua)
			}
			fresh := ""
			if c.fresh {
				fresh = "1"
			}
			cmd := exec.Command(node, args...)
			cmd.Env = append(os.Environ(), "WALK_SEED="+strconv.Itoa(seed), "WALK_STEPS=80", "WALK_FRESH="+fresh)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%s, walk %d: %v\n%s", c.name, seed, err, out)
			}
		}
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
