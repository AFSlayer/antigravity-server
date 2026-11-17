package patches

import (
	"strings"
	"testing"
)

// The three places the context indicator's patches rewrite, as Antigravity 2.32.1 writes them: the hook that gives a
// conversation's agent state store and the next hook, which reads it with the selector hook; the input taking the
// indicator from the build bindings; and the settings provider reading the setting.
const (
	v232ContextIndicatorHook = `},cA=a=>{var b=(0,B.useContext)(bA);if(!b)throw Error("useAgentStateProvider must be used within a TrajectoriesContextProvider");var c=b.listen,d=b.peek;b=(0,B.useCallback)(g=>
c(a,g),[c,a]);var f=(0,B.useCallback)(()=>d(a)??aA,[d,a]);return(0,B.useSyncExternalStore)(b,f)};const W2a=()=>{},X2a=()=>{};
var dA=a=>{var b=(0,B.useContext)(bA),c=(0,B.useCallback)(f=>b&&a?b.listen(a,f):W2a,[b,a]),d=(0,B.useCallback)(()=>b&&a?b.peek(a)??aA:void 0,[b,a]);return(0,B.useSyncExternalStore)(c,d,X2a)},Y2a=a=>{a=cA(a);return Bk(a,{selector:jea})`
	v232ContextIndicatorSlot    = `te=oc?.contextUsageIndicator?.Indicator,wh=!!te&&(A.get("contextUsageIndicator")??!1),Hc=Jk("contextUsageIndicatorEnabled",wh)===!0`
	v232ContextIndicatorSetting = `case "contextUsageIndicatorEnabled":return c?.get("contextUsageIndicatorEnabled")===
"true";`
)

var contextIndicatorPatches = []string{"context-indicator-slot", "context-indicator-define", "context-indicator-default-on"}

func contextIndicatorBundle(hook, slot, setting string) string {
	return "var x={a:1" + hook + "};function input(){var A=cfg,oc=bindings,d=convo;var " + slot + ";return Hc}function pref(c,k){switch(k){" + setting + "}}"
}

// The indicator is drawn from the conversation's context window usage, with each part the language server counts,
// and the setting is on until it is turned off.
func TestContextIndicatorDrawsTheUsage(t *testing.T) {
	src := contextIndicatorBundle(v232ContextIndicatorHook, v232ContextIndicatorSlot, v232ContextIndicatorSetting)
	out, report := Apply(MainJS, []byte(src), Options{})
	for _, r := range report {
		for _, id := range contextIndicatorPatches {
			if r.ID == id && r.Status != StatusApplied {
				t.Fatalf("%s is %v", id, r.Status)
			}
		}
	}
	runNode(t, `"use strict";
const assert = require("assert");
globalThis.window = globalThis;
let state = {};
const hooks = [];
const B = {
  useContext: () => ({ listen: () => () => {}, peek: () => store }),
  useCallback: f => f, useSyncExternalStore: (s, g) => g(),
  useState: v => { hooks.push("state"); return [v, () => {}]; }, useRef: v => ({ current: v }), useEffect: () => { hooks.push("effect"); },
  createElement: (type, props, ...children) => ({ type, props: props || {}, children: children.flat() }),
};
const store = { getState: () => state };
const Bk = (s, o) => { hooks.push("select"); return o.selector(s.getState()); };
const jea = s => s, aA = {}, bA = {};
const text = el => el == null || el === false ? "" : typeof el !== "object" ? String(el) : el.children.map(text).join("");
const find = (el, id) => !el || typeof el !== "object" ? null : el.props["data-testid"] === id ? el : el.children.map(c => find(c, id)).find(Boolean);
`+string(out)+`
const Indicator = window.__agyContextIndicator;
assert.strictEqual(typeof Indicator, "function");

// No usage yet: nothing, with the hooks called all the same.
state = {};
hooks.length = 0;
assert.strictEqual(Indicator({ conversationId: "c" }), null);
assert.deepStrictEqual(hooks, ["select", "state", "effect"]);

// A Gemini conversation on oci: 159,422 of 256,000.
state = { contextWindowUsage: { estimatedTokensUsed: 159422, maxContextTokens: 256000, estimatedAfterCompaction: false, tokenGroups: [
  { type: 1, numTokens: 12347, name: "System Prompt" }, { type: 2, numTokens: 14494, name: "Tools" }, { type: 7, numTokens: 132581, name: "Chat Messages" }, { type: 0, numTokens: 0, name: "Unspecified" } ] } };
let el = Indicator({ conversationId: "c" });
const button = find(el, "context-usage-indicator");
assert.strictEqual(text(button), "62%");
assert.strictEqual(button.props["aria-label"], "Context window 62% full");
assert.ok(button.props.className.includes("text-muted-foreground"));
assert.strictEqual(find(el, "context-usage-panel"), undefined, "the breakdown is closed until clicked");

// Opened: the tokens used of the window, each part with tokens in it, and what is free.
const open = B.useState;
B.useState = v => [true, () => {}];
el = Indicator({ conversationId: "c" });
B.useState = open;
const panel = text(find(el, "context-usage-panel"));
for (const part of ["Context window62%", "159.4K of 256K tokens used", "System Prompt12.3K", "Tools14.5K", "Chat Messages132.6K", "Free96.6K"]) assert.ok(panel.includes(part), part + " in " + panel);
assert.ok(!panel.includes("Unspecified"), "a part with no tokens is left out");

// Four fifths full, compacted since the last response: a warning colour and an approximate figure.
state = { contextWindowUsage: { estimatedTokensUsed: 900, maxContextTokens: 1000, estimatedAfterCompaction: true, tokenGroups: [] } };
el = Indicator({ conversationId: "c" });
assert.strictEqual(text(find(el, "context-usage-indicator")), "~90%");
assert.ok(find(el, "context-usage-indicator").props.className.includes("text-warning"));
assert.strictEqual(find(el, "context-usage-indicator").props["aria-label"], "Context window about 90% full");
// Over the window: the ring is full and the figure says so.
state = { contextWindowUsage: { estimatedTokensUsed: 3000000, maxContextTokens: 2000000, tokenGroups: [] } };
assert.strictEqual(text(find(Indicator({ conversationId: "c" }), "context-usage-indicator")), "100%");

// The input takes it where the build bindings give none, and reads the setting from the settings store.
let cfg = { get: () => undefined }, bindings = undefined, convo = "c";
let prefs = {};
const Jk = (k, use) => use ? prefs[k] : undefined;
const pr = (stored) => pref({ get: () => stored }, "contextUsageIndicatorEnabled");
// Never set, or set on: on. Set off: off.
assert.strictEqual(pr(undefined), true);
assert.strictEqual(pr("true"), true);
assert.strictEqual(pr("false"), false);
prefs.contextUsageIndicatorEnabled = true;
assert.strictEqual(input(), true);
prefs.contextUsageIndicatorEnabled = false;
assert.strictEqual(input(), false);
`)
	if !strings.Contains(string(out), "??window.__agyContextIndicator,wh=!!te") {
		t.Error("the input does not take the indicator")
	}
}

// Bundles before 2.30 draw the indicator themselves, and are left alone.
func TestContextIndicatorPatchesWaitForTheSlot(t *testing.T) {
	src := "var x={a:1" + v232ContextIndicatorHook + "};"
	out, report := Apply(MainJS, []byte(src), Options{})
	for _, r := range report {
		for _, id := range contextIndicatorPatches {
			if r.ID == id && r.Status != StatusNotNeeded {
				t.Errorf("%s is %v", id, r.Status)
			}
		}
	}
	if string(out) != src {
		t.Error("a bundle without the slot was changed")
	}
}

func TestContextIndicatorPatchesMatchALineBreakAtAnyToken(t *testing.T) {
	compact := func(s string) string { return strings.Join(strings.Fields(s), "") }
	for i, src := range []string{v232ContextIndicatorSlot, v232ContextIndicatorHook, v232ContextIndicatorSetting} {
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
			broken := src[:pos] + "\n" + src[pos:]
			if out := rewrite(broken); compact(out) != compact(base) {
				t.Errorf("%s: a line break at offset %d is not matched: %q", p.ID, pos, src[max(0, pos-30):min(len(src), pos+30)])
				break
			}
		}
	}
}
