package patches

import (
	"strings"
	"testing"
)

// The places the context indicator's patches rewrite, as Antigravity 2.32.1 writes them: the hook that gives a
// conversation's agent state store and the next hook, which reads it with the selector hook; the input taking the
// indicator from the build bindings; the settings provider reading the setting; and the three ways the web build
// hides the setting.
const (
	v232ContextIndicatorHook = `},cA=a=>{var b=(0,B.useContext)(bA);if(!b)throw Error("useAgentStateProvider must be used within a TrajectoriesContextProvider");var c=b.listen,d=b.peek;b=(0,B.useCallback)(g=>
c(a,g),[c,a]);var f=(0,B.useCallback)(()=>d(a)??aA,[d,a]);return(0,B.useSyncExternalStore)(b,f)};const W2a=()=>{},X2a=()=>{};
var dA=a=>{var b=(0,B.useContext)(bA),c=(0,B.useCallback)(f=>b&&a?b.listen(a,f):W2a,[b,a]),d=(0,B.useCallback)(()=>b&&a?b.peek(a)??aA:void 0,[b,a]);return(0,B.useSyncExternalStore)(c,d,X2a)},Y2a=a=>{a=cA(a);return Bk(a,{selector:jea})`
	v232ContextIndicatorSlot    = `te=oc?.contextUsageIndicator?.Indicator,wh=!!te&&(A.get("contextUsageIndicator")??!1),Hc=Jk("contextUsageIndicatorEnabled",wh)===!0`
	v232ContextIndicatorSetting = `case "contextUsageIndicatorEnabled":return c?.get("contextUsageIndicatorEnabled")===
"true";`
	v232ContextIndicatorAllowed = `g||l.add("contextUsageIndicatorEnabled");`
	v232ContextIndicatorVisible = `contextUsageIndicatorEnabled:{feature:{visible:!1},applyAll:[{runtimeGating:{customFlag:"enable-context-usage-indicator"},
feature:{visible:!0}}],important:[W$]}`
	v232ContextIndicatorSections = `{title:"Chat",settings:["verboseConversationSteps"]},{title:"Chat Layout",settings:["themeMode","conversationWidth","artifactWidth","contextUsageIndicatorEnabled"]}`
)

var contextIndicatorPatches = []string{"context-indicator-slot", "context-indicator-define", "context-indicator-default-on", "context-indicator-setting-allowed", "context-indicator-setting-visible", "context-indicator-setting-in-chat"}

// contextIndicatorBundle puts the places together as functions a node page can call.
func contextIndicatorBundle(hook, slot string) string {
	return "var x={a:1" + hook + "};function input(){var A=cfg,oc=bindings,d=convo;var " + slot + ";return Hc}" +
		"function pref(c,k){switch(k){" + v232ContextIndicatorSetting + "}}" +
		"function denied(g,h){var l=new Set;" + v232ContextIndicatorAllowed + "return l}" +
		"const W$={cli:1},visibility={" + v232ContextIndicatorVisible + "},appearance=[" + v232ContextIndicatorSections + "];"
}

func contextIndicatorStatuses(t *testing.T, src string) (string, map[string]Status) {
	t.Helper()
	out, report := Apply(MainJS, []byte(src), Options{})
	got := map[string]Status{}
	for _, r := range report {
		got[r.ID] = r.Status
	}
	return string(out), got
}

// A page with the bundle's hooks reduced to what the indicator uses. The selector hook notes each query it is given,
// since a new one would subscribe again.
func contextIndicatorPage(react, selectHook, bundle string) string {
	return `"use strict";
const assert = require("assert");
globalThis.window = globalThis;
globalThis.innerWidth = 412;
let state = {}, opened = null;
const hooks = [], queries = new Set();
const ` + react + ` = {
  useContext: () => ({ listen: () => () => {}, peek: () => store }),
  useCallback: f => f, useSyncExternalStore: (s, g) => g(),
  useState: v => { hooks.push("state"); return [opened ?? v, () => {}]; }, useRef: v => ({ current: v }), useEffect: () => { hooks.push("effect"); },
  createElement: (type, props, ...children) => ({ type, props: props || {}, children: children.flat() }),
};
const store = { getState: () => state };
const ` + selectHook + ` = (s, q) => { hooks.push("select"); queries.add(q.selector); queries.add(q.equalityFn); return q.selector(s.getState()); };
const jea = s => s, aA = {}, bA = {};
const text = el => el == null || el === false ? "" : typeof el !== "object" ? String(el) : el.children.map(text).join("");
const find = (el, id) => !el || typeof el !== "object" ? null : el.props["data-testid"] === id ? el : el.children.map(c => find(c, id)).find(Boolean);
` + bundle + `
const Indicator = window.__agyContextIndicator;
assert.strictEqual(typeof Indicator, "function");
const draw = () => Indicator({ conversationId: "c" });
`
}

// The indicator is drawn from the conversation's context window usage, with each part the language server counts.
func TestContextIndicatorDrawsTheUsage(t *testing.T) {
	src := contextIndicatorBundle(v232ContextIndicatorHook, v232ContextIndicatorSlot)
	out, got := contextIndicatorStatuses(t, src)
	for _, id := range contextIndicatorPatches {
		if got[id] != StatusApplied {
			t.Fatalf("%s is %v", id, got[id])
		}
	}
	runNode(t, contextIndicatorPage("B", "Bk", out)+`
// No usage yet: nothing, with the hooks called all the same.
assert.strictEqual(draw(), null);
assert.deepStrictEqual(hooks, ["select", "state", "effect"]);

// A Gemini conversation on a test server: 159,422 of 256,000.
const gemini = { estimatedTokensUsed: 159422, maxContextTokens: 256000, lastStepIndex: 40, tokenGroups: [
  { type: 1, numTokens: 12347, name: "System Prompt" }, { type: 2, numTokens: 14494, name: "Tools" }, { type: 7, numTokens: 132581, name: "Chat Messages" }, { type: 0, numTokens: 0, name: "Unspecified" } ] };
state = { contextWindowUsage: gemini };
let el = draw();
const button = find(el, "context-usage-indicator");
assert.strictEqual(text(button), "62%");
assert.strictEqual(button.props["aria-label"], "Context window 62% full");
assert.strictEqual(button.props["aria-expanded"], false);
assert.ok(button.props.className.includes("text-muted-foreground"));
assert.strictEqual(find(el, "context-usage-panel"), undefined, "the breakdown is closed until tapped");

// Opened: the tokens used of the window, each part with tokens in it, and what is free, inside the screen.
opened = { left: -200, width: 280 };
el = draw();
opened = null;
const panel = find(el, "context-usage-panel");
for (const part of ["Context window62%", "159.4K of 256K tokens used", "System Prompt12.3K", "Tools14.5K", "Chat Messages132.6K", "Free96.6K"]) assert.ok(text(panel).includes(part), part + " in " + text(panel));
assert.ok(!text(panel).includes("Unspecified"), "a part with no tokens is left out");
assert.deepStrictEqual([panel.props.style.left, panel.props.style.width], [-200, 280]);

// Tapped: the breakdown opens above the indicator, inside the screen however near its edge the indicator is.
{
  const saved = B.useState, savedRef = B.useRef;
  let set, ref;
  B.useState = v => [null, s => { set = s; }];
  B.useRef = () => ref;
  const tap = (left, right) => { ref = { current: { getBoundingClientRect: () => ({ left, right }) } }; find(draw(), "context-usage-indicator").props.onClick(); return set; };
  // Near the right edge: right-aligned with the indicator.
  assert.deepStrictEqual(tap(300, 354), { left: -226, width: 280 });
  // Near the left edge: kept 12 px from it.
  assert.deepStrictEqual(tap(20, 74), { left: -8, width: 280 });
  // A 320 px screen: the same, and no wider than the screen allows.
  globalThis.innerWidth = 320;
  assert.deepStrictEqual(tap(20, 74), { left: -8, width: 280 });
  globalThis.innerWidth = 260;
  assert.deepStrictEqual(tap(150, 204), { left: -138, width: 236 });
  globalThis.innerWidth = 412;
  B.useState = saved; B.useRef = savedRef;
}

// Four fifths full: the warning colour.
state = { contextWindowUsage: { ...gemini, estimatedTokensUsed: 900, maxContextTokens: 1000 } };
assert.ok(find(draw(), "context-usage-indicator").props.className.includes("text-warning"));
// Over the window: a full ring.
state = { contextWindowUsage: { ...gemini, estimatedTokensUsed: 3000000, maxContextTokens: 2000000 } };
assert.strictEqual(text(find(draw(), "context-usage-indicator")), "100%");

// Compacted since the usage was counted, as the 2.29 indicator tells it: a done checkpoint step after that step.
const step = (index, type, status, intentOnly) => ({ metadata: { sourceTrajectoryStepInfo: { stepIndex: index } }, type, status, step: { case: type === 23 ? "checkpoint" : "other", value: { intentOnly } } });
const compacted = steps => { state = { contextWindowUsage: gemini, trajectorySlice: { stepsInSlice: steps } }; return text(find(draw(), "context-usage-indicator")).startsWith("~"); };
assert.strictEqual(compacted([step(39, 5, 3), step(41, 23, 3, false)]), true);
assert.strictEqual(compacted([step(41, 23, 3, true)]), false, "a checkpoint that only states an intent");
assert.strictEqual(compacted([step(41, 23, 2, false)]), false, "a checkpoint still running");
assert.strictEqual(compacted([step(41, 5, 3)]), false, "another step after the usage");
assert.strictEqual(compacted([step(38, 23, 3, false), step(39, 5, 3)]), false, "a checkpoint before the usage was counted");
assert.strictEqual(compacted([step(41, 23, 3, false), step(39, 5, 3)]), false, "the walk stops at the first step before the usage");
state = { contextWindowUsage: gemini, trajectorySlice: { stepsInSlice: [step(41, 23, 3, false)] } };
const about = find(draw(), "context-usage-indicator");
assert.strictEqual(about.props["aria-label"], "Context window about 62% full");
opened = { left: 0, width: 280 };
assert.ok(text(find(draw(), "context-usage-panel")).includes("Compacted since the last response"));
opened = null;

// Every drawing reads the store with the same query, so the selector hook subscribes once.
for (let i = 0; i < 5; i++) draw();
assert.strictEqual(queries.size, 2, "one selector and one equality function");

// The input takes it where the build bindings give none, and reads the setting from the settings store.
let cfg = { get: () => undefined }, bindings = undefined, convo = "c";
let prefs = {};
const Jk = (k, use) => use ? prefs[k] : undefined;
const pr = stored => pref({ get: () => stored }, "contextUsageIndicatorEnabled");
assert.strictEqual(pr(undefined), true, "never set: on");
assert.strictEqual(pr("true"), true);
assert.strictEqual(pr("false"), false, "turned off: off");
prefs.contextUsageIndicatorEnabled = true;
assert.strictEqual(input(), true);
prefs.contextUsageIndicatorEnabled = false;
assert.strictEqual(input(), false);

// The setting: kept in the settings screens, shown without the Google flag, and at the top of Appearance.
assert.ok(!denied(false, false).has("contextUsageIndicatorEnabled"));
assert.deepStrictEqual(visibility.contextUsageIndicatorEnabled, { feature: { visible: true }, important: [W$] });
assert.deepStrictEqual(appearance[0], { title: "Chat", settings: ["verboseConversationSteps", "contextUsageIndicatorEnabled"] });
assert.ok(!appearance[1].settings.includes("contextUsageIndicatorEnabled"), "and not twice");
`)
}

// The indicator declares no name of its own that could hide one of the bundle's it uses, whatever the minifier calls
// them.
func TestContextIndicatorKeepsTheBundlesNames(t *testing.T) {
	for _, c := range []struct{ react, selectHook string }{{"R", "Bk"}, {"E", "Bk"}, {"L", "Bk"}, {"B", "u"}, {"B", "s"}, {"p", "q"}} {
		hook := strings.ReplaceAll(v232ContextIndicatorHook, "B.", c.react+".")
		hook = strings.Replace(hook, "return Bk(a,{selector:", "return "+c.selectHook+"(a,{selector:", 1)
		out, got := contextIndicatorStatuses(t, contextIndicatorBundle(hook, v232ContextIndicatorSlot))
		if got["context-indicator-define"] != StatusApplied {
			t.Fatalf("React as %s and the selector hook as %s: %v", c.react, c.selectHook, got["context-indicator-define"])
		}
		runNode(t, contextIndicatorPage(c.react, c.selectHook, out)+`
state = { contextWindowUsage: { estimatedTokensUsed: 500, maxContextTokens: 1000, tokenGroups: [] } };
assert.strictEqual(text(find(draw(), "context-usage-indicator")), "50%");
`)
	}
}

// Each needs the one before it. Where the indicator cannot be defined the input is handed nothing, and draws what it
// did; where the hooks are in another form that is reported.
func TestContextIndicatorPatchesBuildOnEachOther(t *testing.T) {
	notNeeded, missing, applied := StatusNotNeeded, StatusMissing, StatusApplied
	for _, c := range []struct {
		name string
		src  string
		want []Status
	}{
		{"2.32.1", contextIndicatorBundle(v232ContextIndicatorHook, v232ContextIndicatorSlot), []Status{applied, applied, applied, applied, applied, applied}},
		{"a bundle before 2.30", "var x={a:1" + v232ContextIndicatorHook + "};", []Status{notNeeded, notNeeded, notNeeded, notNeeded, notNeeded, notNeeded}},
		{"hooks in another form", contextIndicatorBundle(strings.Replace(v232ContextIndicatorHook, "return Bk(a,{selector:", "return Bk(a,{pick:", 1), v232ContextIndicatorSlot), []Status{applied, missing, notNeeded, notNeeded, notNeeded, notNeeded}},
		{"a next hook reading another store", contextIndicatorBundle(strings.Replace(v232ContextIndicatorHook, "a=cA(a);return Bk", "a=dA(a);return Bk", 1), v232ContextIndicatorSlot), []Status{applied, missing, notNeeded, notNeeded, notNeeded, notNeeded}},
		{"a slot whose names do not agree", contextIndicatorBundle(v232ContextIndicatorHook, strings.Replace(v232ContextIndicatorSlot, "wh=!!te&&", "wh=!!Qg&&", 1)), []Status{missing, notNeeded, notNeeded, notNeeded, notNeeded, notNeeded}},
	} {
		out, got := contextIndicatorStatuses(t, c.src)
		for i, id := range contextIndicatorPatches {
			if got[id] != c.want[i] {
				t.Errorf("%s: %s is %v, want %v", c.name, id, got[id], c.want[i])
			}
		}
		if c.name == "hooks in another form" {
			// The input looks for a function that is not there, and has no indicator, as before.
			runNode(t, `"use strict";
const assert = require("assert");
globalThis.window = globalThis;
const B = {}, Bk = () => {}, jea = 0, aA = {}, bA = {};
let cfg = { get: () => undefined }, bindings = undefined, convo = "c";
const Jk = (k, use) => use ? true : undefined;
`+out+`
assert.strictEqual(window.__agyContextIndicator, undefined);
assert.strictEqual(input(), false, "no indicator: the input reads the setting as off, as it did");
`)
		}
	}
}

func TestContextIndicatorPatchesMatchALineBreakAtAnyToken(t *testing.T) {
	compact := func(s string) string { return strings.Join(strings.Fields(s), "") }
	for i, src := range []string{v232ContextIndicatorSlot, v232ContextIndicatorHook, v232ContextIndicatorSetting, v232ContextIndicatorAllowed, v232ContextIndicatorVisible, v232ContextIndicatorSections} {
		var p Patch
		for _, each := range All() {
			if each.ID == contextIndicatorPatches[i] {
				p = each
			}
		}
		rewrite := func(src string) string {
			loc := p.FindRe.FindStringSubmatchIndex(src)
			if loc == nil || (p.Accept != nil && !p.Accept(submatches([]byte(src), loc))) {
				return src
			}
			return src[:loc[0]] + string(p.FindRe.ExpandString(nil, p.Replace, src, loc)) + src[loc[1]:]
		}
		base := rewrite(src)
		if base == src {
			t.Fatalf("%s does not match its place", p.ID)
		}
		for _, pos := range lineBreakPositions(src) {
			if out := rewrite(src[:pos] + "\n" + src[pos:]); compact(out) != compact(base) {
				t.Errorf("%s: a line break at offset %d is not matched: %q", p.ID, pos, src[max(0, pos-30):min(len(src), pos+30)])
				break
			}
		}
	}
}
