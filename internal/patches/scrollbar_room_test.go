package patches

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Where 2.32's overlay scrollbar measures what it scrolls, lands a click on its track, and looks for a scroll timeline.
const (
	v232ScrollbarMeasure  = `var na=h?oa.scrollLeft:oa.scrollTop,ta=h?oa.scrollWidth:oa.scrollHeight,ra=h?oa.clientWidth:oa.clientHeight;A.pos=na;A.scrollLength=ta;A.clientLength=ra;if(!(ra>0&&ta>ra+1))return W(),!1;`
	v232ScrollbarClick    = `ea>0&&(ta=Math.min(ea,Math.max(0,ta-ra/2))/ea*pa,h?oa.scrollLeft=ta:oa.scrollTop=ta,A.pos=h?oa.scrollLeft:oa.scrollTop,Z(!0))`
	v232ScrollbarTimeline = `function TQb(){var a=globalThis.ScrollTimeline;return typeof a==="function"?a:null}`
	v232ScrollbarTrack    = `B.createElement("div",{ref:l,"data-testid":"overlay-scrollbar","data-overlay-scrollbar-track":""});`
	// What else of the bundle these patches go by: the scrollbar's track, and the component that hands the tracker the
	// move that keeps the rows in place. Without that there is no room for a scrollbar to leave out.
	v232RoomBundle = v232ScrollbarTrack + v232RowsKept
)

func TestScrollbarPatchesLeaveTheRoomOut(t *testing.T) {
	cases := []struct{ id, src, want string }{
		{"scrollbar-measures-rows", v232ScrollbarMeasure,
			`A.clientLength=ra;if(!h&&window.__agyTopRoom){var _agyRoom=window.__agyTopRoom(oa);na-=_agyRoom;ta-=_agyRoom}if(!(ra>0&&ta>ra+1))return W(),!1;`},
		{"scrollbar-click-lands-in-rows", v232ScrollbarClick,
			`ta=Math.min(ea,Math.max(0,ta-ra/2))/ea*pa,h?oa.scrollLeft=ta:oa.scrollTop=ta+(window.__agyTopRoom?.(oa)||0),A.pos=h?oa.scrollLeft:oa.scrollTop,Z(!0))`},
		{"scrollbar-thumb-by-script", v232ScrollbarTimeline,
			`function TQb(){var a=globalThis.ScrollTimeline;return typeof a==="function"&&!window.__agyTopRoom?a:null}`},
	}
	for _, c := range cases {
		got := applyOnly(t, c.id, c.src+v232RoomBundle, Options{})
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: got\n%s\nwant it to contain\n%s", c.id, got, c.want)
		}
		// A bundle with the scrollbar in some other form is reported, and one without it needs nothing.
		if _, status := applyWithStatus(t, c.id, "var x=1;"+v232RoomBundle+`var y=globalThis.ScrollTimeline;`, Options{}); status != StatusMissing {
			t.Errorf("%s: a changed scrollbar: status %v, want %v", c.id, status, StatusMissing)
		}
		if _, status := applyWithStatus(t, c.id, "var x=1;", Options{}); status != StatusNotNeeded {
			t.Errorf("%s: a bundle without the scrollbar: status %v, want %v", c.id, status, StatusNotNeeded)
		}
	}
}

// A release breaks the bundle's long lines in different places. Each of these applies the same way with a line break
// wherever the minifier could put one.
func TestRoomPatchesMatchALineBreakAtAnyToken(t *testing.T) {
	compact := func(s string) string { return strings.Join(strings.Fields(s), "") }
	for _, c := range []struct{ id, src string }{
		{"paging-loads-rows-ahead", v232PagingConfig},
		{"scrollbar-measures-rows", v232ScrollbarMeasure},
		{"scrollbar-click-lands-in-rows", v232ScrollbarClick + ";"},
		{"scrollbar-thumb-by-script", v232ScrollbarTimeline},
	} {
		base, status := applyWithStatus(t, c.id, c.src+v232RoomBundle, Options{})
		if status != StatusApplied {
			t.Fatalf("%s: the patch did not apply: %v", c.id, status)
		}
		positions := lineBreakPositions(c.src)
		if len(positions) < 5 {
			t.Fatalf("%s: only %d break positions", c.id, len(positions))
		}
		for _, pos := range positions {
			broken := c.src[:pos] + "\n" + c.src[pos:]
			if out, status := applyWithStatus(t, c.id, broken+v232RoomBundle, Options{}); status != StatusApplied || compact(out) != compact(base) {
				t.Errorf("%s: a line break at offset %d is not matched: %q", c.id, pos, c.src[max(0, pos-30):min(len(c.src), pos+30)])
			}
		}
	}
}

// Runs the patched measuring and the patched click as the bundle would, against a conversation with room above its rows.
func TestScrollbarShowsWhereTheReaderIsAmongTheRows(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	measure := applyOnly(t, "scrollbar-measures-rows", v232ScrollbarMeasure+v232RoomBundle, Options{})
	measure = measure[:strings.Index(measure, "if(!(ra>0")]
	click := applyOnly(t, "scrollbar-click-lands-in-rows", v232ScrollbarClick+v232RoomBundle, Options{})
	click = click[:strings.Index(click, ",A.pos=h?")] + ")"
	timeline := applyOnly(t, "scrollbar-thumb-by-script", v232ScrollbarTimeline+v232RoomBundle, Options{})
	timeline = timeline[:strings.Index(timeline, "B.createElement")]
	harness := `
const assert = require("assert");
globalThis.window = globalThis;
const [measureSrc, clickSrc, timelineSrc] = process.argv.slice(1);
// What the bundle goes on to work out from what it measured.
const measured = new Function("h", "oa", "A", measureSrc + "var la=Math.max(0,ra-4),size=Math.min(la,Math.max(28,Math.round(ta>0?ra/ta*la:0))),max=Math.max(1,ta-ra);return {pos:na,scrollLength:ta,thumbSize:size,maxScrollPos:max,thumbOffset:Math.round(Math.min(1,Math.max(0,na/max))*Math.max(0,la-size))};");
const clicked = new Function("h", "oa", "ta", "ea", "ra", "pa", clickSrc + ";return oa;");
const timeline = new Function(timelineSrc + "return TQb();");
globalThis.ScrollTimeline = function () {};

// 9,000 px of rows under 200,000 px of room, in a viewport 704 px tall, with the reader 3,000 px down the rows.
const conversation = () => ({ scrollTop: 203000, scrollHeight: 209000, clientHeight: 704, scrollLeft: 0, scrollWidth: 400, clientWidth: 400 });
const state = {};

// Without the tracker the scrollbar measures everything, as the bundle wrote it.
let seen = measured(false, conversation(), state);
assert.strictEqual(seen.scrollLength, 209000);
assert.strictEqual(seen.thumbSize, 28, "the thumb is as small as it gets");
assert.strictEqual(seen.thumbOffset, 655, "and near the bottom");
assert.strictEqual(typeof timeline(), "function", "and a scroll timeline moves it");
assert.strictEqual(clicked(false, conversation(), 350, 600, 28, 208296).scrollTop, (350 - 14) / 600 * 208296);

// With it the room is left out: the thumb is as tall as the rows are, and a third of the way down.
window.__agyTopRoom = el => 200000;
seen = measured(false, conversation(), state);
assert.strictEqual(seen.pos, 3000);
assert.strictEqual(seen.scrollLength, 9000);
assert.strictEqual(seen.thumbSize, 55);
assert.strictEqual(seen.maxScrollPos, 8296);
assert.strictEqual(seen.thumbOffset, Math.round(3000 / 8296 * (700 - 55)));
assert.deepStrictEqual([state.pos, state.scrollLength], [203000, 209000], "what tells the reader's scrolling from the app's is still the real position");
assert.strictEqual(timeline(), null, "a timeline would run the whole range, so the thumb is moved by script");
// A reader in the room is shown at the top of the rows.
assert.strictEqual(measured(false, Object.assign(conversation(), { scrollTop: 199000 }), state).thumbOffset, 0);
// A click halfway down the track lands halfway down the rows.
assert.strictEqual(clicked(false, conversation(), 350, 645, 55, 8296).scrollTop, (350 - 27.5) / 645 * 8296 + 200000);
// Sideways there is no room to leave out.
seen = measured(true, conversation(), state);
assert.strictEqual(seen.scrollLength, 400);
assert.strictEqual(clicked(true, conversation(), 100, 300, 28, 500).scrollLeft, (100 - 14) / 300 * 500);
`
	if out, err := exec.Command(node, "-e", harness, measure, click, timeline).CombinedOutput(); err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
}

// A drag of the thumb stays among the rows, and elsewhere there is no room to speak of.
func TestScrollbarDragStaysOutOfTheRoom(t *testing.T) {
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
let timers = [];
let frames = [];
global.window = global;
global.addEventListener = (name, fn) => { (listeners[name] = listeners[name] || []).push(fn); };
global.dispatchEvent = () => {};
global.CustomEvent = class {};
global.document = { querySelector: () => null, createElement: () => ({}), head: { appendChild: () => {} } };
global.MutationObserver = class { observe() {} };
global.requestAnimationFrame = fn => { frames.push(fn); };
global.setTimeout = (fn, ms) => { timers.push({ at: now + (ms || 0), fn }); };
Object.defineProperty(global, "performance", { value: { now: () => now } });
global.matchMedia = () => ({ matches: true });
const ios = !process.argv[2];
global.navigator = { userAgent: process.argv[2] || "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X)", platform: ios ? "iPhone" : "Linux", maxTouchPoints: 5 };
new Function(fs.readFileSync(process.argv[1], "utf8"))();
const fire = (name, event) => (listeners[name] || []).forEach(fn => fn(event));
const pass = ms => { for (const end = now + ms; now < end;) { now += 16; const f = frames; frames = []; f.forEach(fn => fn()); const due = timers.filter(x => x.at <= now); timers = timers.filter(x => x.at > now); due.forEach(x => x.fn()); } };

const older = { getAttribute: () => "false" };
const box = { style: { paddingTop: "", overflowAnchor: "", removeProperty: () => {} }, get offsetHeight() { return el.room + el.rows; } };
const el = {
  top: 3000, rows: 9000, clientHeight: 704, isConnected: true,
  style: { overflowY: "" },
  matches: () => true, closest: () => el, querySelector: () => older,
  get room() { return parseFloat(box.style.paddingTop) || 0; },
  firstElementChild: box,
  get scrollHeight() { return el.room + el.rows; },
  get scrollTop() { return el.top; },
  set scrollTop(v) { el.top = Math.round(Math.min(el.scrollHeight - el.clientHeight, Math.max(0, v))); },
};
window.__agyRowsDrawn(el);
fire("scroll", { target: el });
pass(600);
if (!ios) {
  assert.strictEqual(el.room, 0);
  assert.strictEqual(window.__agyTopRoom, undefined, "elsewhere the scrollbar is not told of any room");
  process.exit(0);
}
assert.strictEqual(el.room, 200000);
assert.strictEqual(window.__agyTopRoom(el), 200000, "the scrollbar is told how much room there is");
// The bundle hands over the rows' range: 8,296 px of scrolling for 645 px of track. The reader drags up past the top.
const drag = { startScrollPos: el.top, startCoord: 400 };
window.__agyDragScrollbar(drag, el, false, 300, 645, 8296);
assert.strictEqual(el.top, 203000 - Math.round(100 / 645 * 8296), "a drag moves through the rows");
window.__agyDragScrollbar(drag, el, false, -500, 645, 8296);
assert.strictEqual(el.top, 200000, "and stops at the first of them");
window.__agyDragScrollbar(drag, el, false, 300, 645, 8296);
assert.strictEqual(el.top, 203000 - Math.round(100 / 645 * 8296), "coming back follows the pointer");
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
