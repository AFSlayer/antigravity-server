package patches

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The four places the agent picker's patches rewrite, as Antigravity 2.32.1 writes them: the desktop layout's new
// conversation screen up to where it draws its input, the input's choice of who draws the picker, cut down to that
// choice and its two uses, the input of the phone layout's new conversation screen, and the input's footer, which
// draws the chip for the agent that is chosen.
const (
	v232AgentHome = `o2c=()=>{var a=zk(),b=(0,B.useRef)(null),c=n2c(),{q:d,ws:f,customSubagent:g}=dI({strict:!1}),h=oj(),k=h.get("tipCards"),l=h.get("inputComponent");h=(0,B.useRef)(null);m2c();(0,B.useEffect)(()=>{f&&b.current!==f&&(b.current=f,a(Kp(!1)))},[f,a]);var {agentScriptsEnabled:m,selectedAgentScriptItem:n,setSelectedAgentScriptItem:p,getAgentScripts:q,globalScriptsDir:t,workspaceScriptsDir:u}=
j5b(),{data:v,refetch:w,isLoading:x,error:y}=U1c({fire:q}),z=(0,B.useMemo)(()=>v?.agentCustomizations??[],[v]),A=(0,B.useMemo)(()=>z.filter(C=>!C.agent?.disabled),[z]);c2c({isLoadingAgentScripts:x,agentScriptsError:y,selectedAgentScriptItem:n,enabledCustomizations:A,setSelectedAgentScriptItem:p});V1c({initialCustomSubagent:g,agentCustomizations:z,setSelectedAgentScriptItem:p});var {registerOpenSelector:D}=A3a();(0,B.useEffect)(()=>{var C=D(()=>{w()});return()=>C.dispose()},[D,w]);l=b2c({agentScriptsEnabled:m,
isLoadingAgentScripts:x,agentScriptsError:y,hideAgentSelectorIfEmpty:l?.hideAgentSelectorIfEmpty,agentCustomizationsCount:z.length,selectedAgentScriptItem:n})?B.createElement(f2c,{agentScriptsEnabled:m,selectedAgentScriptItem:n,setSelectedAgentScriptItem:p,globalScriptsDir:t,workspaceScriptsDir:u,agentCustomizations:z,refreshAgentScripts:w,isLoadingAgentScripts:x,agentScriptsError:y}):void 0;return B.createElement(B.Fragment,null,B.createElement(STc,{conversationId:void 0,isLoading:!1,dropdownPlacement:"top-start",openConversationOptimistically:c,agentSelector:l,showBottomToolbar:!0,toolbarPlacement:"top",initialQuery:d}))}`
	v232AgentRouting = `$w=n==="top"&&!q,qA=D&&C&&F&&n==="top"&&fb.connectors,Fq=(0,B.useMemo)(()=>q?void 0:B.createElement($Qc,{placement:b,conversationId:fa,bestOfNConfig:t,
footerExtra:$w?f:void 0}),[q,t,b,fa,$w,f]),OTc=B.createElement(PTc,{modelSelector:Fq,environmentSelector:R,agentSelector:$w?void 0:f,connectorsMenu:ds})`
	v232PhoneHome = `B.createElement(STc,{conversationId:void 0,isLoading:!1,dropdownPlacement:"top-start",openConversationOptimistically:a,showBottomToolbar:!0,aboveContent:B.createElement(jNc,
null),initialQuery:b})`
	v232AgentChip = `B.createElement(HSc,{cancelInvocation:c,isRunning:h,canQueue:k,isConvoInputEmpty:Qc,canSendWithoutText:l,isConvoLoading:m,handleSubmit:le,
modelSelector:n,environmentSelector:p,currentModelConfig:z,handleImageUpload:hd,onAttachFolder:yb,conversationCapabilities:ra,lexicalRef:Kc,dropdownPlacement:C,captureScreenRecording:H,...ac,hasMicrophone:ac.hasMicrophone&&!Eb?.isOn&&(Bd!=="listening"||Ia),hasAttachments:lb.length>0,isStreamingTranscriptionEnabled:Jd,isUploading:jb||Rc.length>0,isFetchingMcpPrompts:Za,sendBlock:xd,inExistingConversation:hb,conversationAgentScript:O,isSubagent:na,selectedEnvironment:W,battleMode:fa,autonomousModeConfig:da,
setSelectedEnvironment:y?void 0:S,isCreatingEnvironment:Z,isGitRepo:T,isCitcWorkspace:aa,isPendingNewWorkspace:V,slashCommandItems:I,sideQuestionControl:A,permissionsSelector:u,contextUsageIndicator:D,connectorsMenu:v,onOpenDrivePlugin:G,selectedAgent:$c&&!hb?L?.selectedAgentScriptItem:void 0})`
)

// The same four as 2.29.0 writes them, where React goes by another name and the desktop screen reads one more
// setting before it gets to the picker.
const (
	v229AgentHome = `nFc=()=>{var a=Pl(),b=(0,A.useRef)(null),c=mFc(),{q:d,ws:f,customSubagent:g}=cG({strict:!1}),h=hk(),k=h.get("tipCards"),l=h.get("inputComponent");h=h.get("projectV5Migration")?.regroupCard;var m=(0,A.useRef)(null);lFc();(0,A.useEffect)(()=>{f&&b.current!==f&&(b.current=f,a(zs(!1)))},[f,a]);var {agentScriptsEnabled:n,selectedAgentScriptItem:p,setSelectedAgentScriptItem:r,getAgentScripts:u,
globalScriptsDir:t,workspaceScriptsDir:v}=iMb(),{data:w,refetch:x,isLoading:y,error:z}=ZEc({fire:u}),B=(0,A.useMemo)(()=>w?.agentCustomizations??[],[w]),C=(0,A.useMemo)(()=>B.filter(F=>!F.agent?.disabled),[B]);hFc({isLoadingAgentScripts:y,agentScriptsError:z,selectedAgentScriptItem:p,enabledCustomizations:C,setSelectedAgentScriptItem:r});$Ec({initialCustomSubagent:g,agentCustomizations:B,setSelectedAgentScriptItem:r});var {registerOpenSelector:E}=xWa();(0,A.useEffect)(()=>{var F=E(()=>{x()});return()=>
F.dispose()},[E,x]);l=gFc({agentScriptsEnabled:n,isLoadingAgentScripts:y,agentScriptsError:z,hideAgentSelectorIfEmpty:l?.hideAgentSelectorIfEmpty,agentCustomizationsCount:B.length,selectedAgentScriptItem:p})?A.createElement(kFc,{agentScriptsEnabled:n,selectedAgentScriptItem:p,setSelectedAgentScriptItem:r,globalScriptsDir:t,workspaceScriptsDir:v,agentCustomizations:B,refreshAgentScripts:x,isLoadingAgentScripts:y,agentScriptsError:z}):void 0;return A.createElement(A.Fragment,null,A.createElement(Y4,{conversationId:void 0,isLoading:!1,dropdownPlacement:"top-start",openConversationOptimistically:c,agentSelector:l,showBottomToolbar:!0,toolbarPlacement:"top",initialQuery:d}))}`
	v229AgentRouting = `aG=n==="top"&&!r,VN=C&&E&&n==="top"&&Na.connectors,o_=(0,A.useMemo)(()=>r?void 0:A.createElement(iKc,{placement:b,conversationId:d,bestOfNConfig:u,footerExtra:aG?f:void 0}),[r,u,b,d,aG,f]),Jqc=A.createElement(Kqc,{modelSelector:o_,environmentSelector:Q,agentSelector:aG?void 0:f,connectorsMenu:n_})`
	v229PhoneHome    = `A.createElement(Y4,{conversationId:void 0,isLoading:!1,dropdownPlacement:"top-start",openConversationOptimistically:a,showBottomToolbar:!0,aboveContent:A.createElement(Ppc,null),initialQuery:b})`
	v229AgentChip    = `A.createElement(dvc,{cancelInvocation:c,isRunning:h,isConvoInputEmpty:Uh,canSendWithoutText:k,isConvoLoading:l,handleSubmit:xd,modelSelector:m,environmentSelector:n,currentModelConfig:y,handleImageUpload:ie,onAttachFolder:Gg,conversationCapabilities:la,lexicalRef:qb,dropdownPlacement:C,captureScreenRecording:G,...ld,hasMicrophone:ld.hasMicrophone&&!hd?.isOn&&Oe!=="listening",hasAttachments:db.length>0,isStreamingTranscriptionEnabled:Ob,
isUploading:rb||ni.length>0,isFetchingMcpPrompts:Wa,sendBlock:Fc,inExistingConversation:Fa,conversationAgentScript:N,isSubagent:xa,selectedEnvironment:R,battleMode:aa,autonomousModeConfig:V,setSelectedEnvironment:x?void 0:S,isCreatingEnvironment:W,isGitRepo:T,isCitcWorkspace:U,isPendingNewWorkspace:Z,slashCommandItems:H,sideQuestionControl:z,permissionsSelector:u,contextUsageIndicator:B,connectorsMenu:t,onOpenDrivePlugin:F,selectedAgent:Fb&&!Fa?L?.selectedAgentScriptItem:void 0})`
)

var agentPickerPatches = []string{"mobile-agent-picker-in-model-menu", "mobile-agent-picker-hook", "mobile-agent-picker", "mobile-agent-picker-chip"}

// agentBundle puts the four places together the way a bundle has them: the picker's row in the model selector's
// menu, the input's footer and the input, then the two screens.
func agentBundle(chip, routing, home, phone, search string) string {
	return `var rowOf={"data-testid":"agent-selector-submenu"},footerOf=()=>` + chip + ";var " + routing + ";const n2c=()=>0," + home + ";var M3c=()=>{var a=L3c(),{q:b}=" + search + `({strict:!1});P6("MOBILE_HOME_VIEW");return ` + phone + "};"
}

var (
	v232AgentBundle = agentBundle(v232AgentChip, v232AgentRouting, v232AgentHome, v232PhoneHome, "dI")
	v229AgentBundle = agentBundle(v229AgentChip, v229AgentRouting, v229AgentHome, v229PhoneHome, "cG")
)

func agentStatuses(t *testing.T, src string, opts Options) (string, map[string]Result) {
	t.Helper()
	out, report := Apply(MainJS, []byte(src), opts)
	got := map[string]Result{}
	for _, r := range report {
		got[r.ID] = r
	}
	return string(out), got
}

// The call the phone screen's input is given the picker by.
const phonePickerCall = `agentSelector:(()=>{try{return window.__agyPhoneAgentPicker()}catch(e){if(e&&typeof e.then==="function")throw e;window.__agyPhoneAgentPickerFailed||(window.__agyPhoneAgentPickerFailed=!0,console.error("[agy] the agent picker could not be built for this screen:",e))}})()`

// The phone layout's new conversation screen is given the picker the desktop one builds, in every bundle since the
// picker moved into the model selector's menu.
func TestAgentPickerPatchesFitTheBundlesSince229(t *testing.T) {
	node, _ := exec.LookPath("node")
	for _, c := range []struct {
		name, src, home string
		want            []string
	}{
		{"2.32.1", v232AgentBundle, v232AgentHome, []string{
			`var $w=!q,qA=D&&C&&F&&n==="top"&&fb.connectors,`,
			"footerExtra:$w?f:void 0}),[q,t,b,fa,$w,f])",
			"agentSelector:$w?void 0:f,",
			`const n2c=()=>0,__agyPhoneAgentPicker=window.__agyPhoneAgentPicker=function(){var{customSubagent:g}=dI({strict:!1}),l=oj().get("inputComponent");var {agentScriptsEnabled:m,`,
			"agentScriptsError:y}):void 0;return l},o2c=()=>{var a=zk(),",
			"initialQuery:b," + phonePickerCall + "})",
			"onOpenDrivePlugin:G,selectedAgent:($c||n?.props?.footerExtra)&&!hb?L?.selectedAgentScriptItem:void 0})",
		}},
		{"2.29.0", v229AgentBundle, v229AgentHome, []string{
			`var aG=!r,VN=C&&E&&n==="top"&&Na.connectors,`,
			`const n2c=()=>0,__agyPhoneAgentPicker=window.__agyPhoneAgentPicker=function(){var{customSubagent:g}=cG({strict:!1}),l=hk().get("inputComponent");var {agentScriptsEnabled:n,`,
			"agentScriptsError:z}):void 0;return l},nFc=()=>{var a=Pl(),",
			"initialQuery:b," + phonePickerCall + "})",
			"onOpenDrivePlugin:F,selectedAgent:(Fb||m?.props?.footerExtra)&&!Fa?L?.selectedAgentScriptItem:void 0})",
		}},
	} {
		out, got := agentStatuses(t, c.src, Options{MobileUX: true})
		for _, id := range agentPickerPatches {
			if got[id].Status != StatusApplied {
				t.Errorf("%s: %s is %v", c.name, id, got[id].Status)
			}
		}
		for _, want := range c.want {
			if !strings.Contains(out, want) {
				t.Errorf("%s: the patched bundle has no %q in:\n%s", c.name, want, out)
			}
		}
		// The screen the picker was copied from is as it was, to the letter.
		if !strings.Contains(out, c.home) {
			t.Errorf("%s: the desktop screen was changed", c.name)
		}
		if node == "" {
			continue
		}
		path := filepath.Join(t.TempDir(), "bundle.js")
		if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
			t.Fatal(err)
		}
		if msg, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
			t.Errorf("%s: the patched bundle is not valid JavaScript: %v\n%s", c.name, err, msg)
		}
	}
	// A phone that is served the desktop's patches only has nothing changed.
	out, got := agentStatuses(t, v232AgentBundle, Options{})
	for _, id := range agentPickerPatches {
		if got[id].Status != StatusDisabled {
			t.Errorf("without the phone patches %s is %v", id, got[id].Status)
		}
	}
	if out != v232AgentBundle {
		t.Error("without the phone patches the bundle was changed")
	}
}

// agentPage is a page for the patched 2.32.1 bundle to run on, in strict mode as the bundle is: React's hooks reduced
// to what the two screens use of them, and the bundle's own pieces replaced by ones that note how they were called.
const agentPage = `"use strict";
const assert = require("assert");
globalThis.window = globalThis;
let calls = [];
let search = { q: "", ws: undefined, customSubagent: undefined };
let settings = { tipCards: null, inputComponent: { hideAgentSelectorIfEmpty: true } };
let agents = [];
let chosen;
const B = {
  Fragment: "Fragment",
  useRef: v => ({ current: v }), useEffect: () => {}, useMemo: f => f(), useCallback: f => f,
  createElement: (type, props, ...children) => ({ type, props: props || {}, children }),
};
const zk = () => () => {}, m2c = () => {}, Kp = () => ({}), P6 = () => {}, L3c = () => () => {};
const dI = () => { calls.push("address"); return search; };
const oj = () => ({ get: name => settings[name] });
const j5b = () => { calls.push("agents"); return { agentScriptsEnabled: true, selectedAgentScriptItem: chosen, setSelectedAgentScriptItem: () => {}, getAgentScripts: () => {}, globalScriptsDir: "/home/.gemini/config/agents", workspaceScriptsDir: "/work/.agents/agents" }; };
const U1c = () => { calls.push("read"); return { data: { agentCustomizations: agents }, refetch: () => {}, isLoading: false, error: null }; };
const c2c = a => { calls.push("in step with " + a.enabledCustomizations.length); };
const V1c = a => { calls.push("asked for " + a.initialCustomSubagent); };
const A3a = () => ({ registerOpenSelector: () => ({ dispose() {} }) });
// The bundle's own rule for showing the picker.
function b2c({agentScriptsEnabled:a,isLoadingAgentScripts:b,agentScriptsError:c,hideAgentSelectorIfEmpty:d,agentCustomizationsCount:f,selectedAgentScriptItem:g}){return a?!(!b&&!c&&d===!0&&f===0&&!g):!1}
const f2c = "AgentPicker", STc = "Input", jNc = "Selectors", $Qc = "ModelSelector", PTc = "InputBox";
// What a screen hands its input as the picker, and what its building of it called on the way.
const inputOf = tree => tree.type === "Input" ? tree : (tree.children || []).map(inputOf).find(Boolean);
const drawn = screen => { calls = []; const picker = inputOf(screen()).props.agentSelector; return { picker, calls: calls.filter(c => c !== "address") }; };
// A picker's element without the functions it is given, which are made anew at every drawing.
const plain = el => el && { type: el.type, props: Object.fromEntries(Object.entries(el.props).filter(([, v]) => typeof v !== "function")) };
`

// The picker the phone screen gets is the one the desktop screen builds: the same element from the same hooks, and
// nothing where the desktop screen has nothing.
func TestAgentPickerOnAPhoneIsTheDesktopOne(t *testing.T) {
	out, got := agentStatuses(t, v232AgentBundle, Options{MobileUX: true})
	for _, id := range agentPickerPatches {
		if got[id].Status != StatusApplied {
			t.Fatalf("%s is %v", id, got[id].Status)
		}
	}
	runNode(t, agentPage+`
var n, q, f, D, C, F, fb = {}, t, b, fa, R, ds;
`+out+`
const both = () => { const desktop = drawn(o2c), phone = drawn(M3c); return { desktop, phone }; };

// Two agents in the project: both screens hand their input the picker, with the same agents in it.
agents = [{ agent: { name: "squad" }, path: "/work/.agents/agents/squad.md" }, { agent: { name: "svelte-file-editor" }, path: "/work/.agents/agents/svelte-file-editor.md" }];
let s = both();
assert.strictEqual(s.desktop.picker.type, "AgentPicker");
assert.strictEqual(s.phone.picker.type, "AgentPicker", "the phone screen hands its input a picker");
assert.deepStrictEqual(plain(s.phone.picker), plain(s.desktop.picker), "the same one as the desktop screen's");
assert.strictEqual(s.phone.picker.props.agentCustomizations.length, 2);
assert.deepStrictEqual(s.phone.calls, ["agents", "read", "in step with 2", "asked for undefined"]);
assert.deepStrictEqual(s.phone.calls, s.desktop.calls, "built by the same hooks in the same order");

// One of them switched off is not offered, on either.
agents[1].agent.disabled = true;
s = both();
assert.deepStrictEqual(s.phone.calls, ["agents", "read", "in step with 1", "asked for undefined"]);
assert.deepStrictEqual(s.phone.calls, s.desktop.calls);
agents[1].agent.disabled = false;

// No agents, and the app set to hide the picker then: neither screen has one, so the model selector's menu ends
// where it did.
agents = [];
s = both();
assert.strictEqual(s.desktop.picker, undefined);
assert.strictEqual(s.phone.picker, undefined, "nothing for the phone's input either");

// With no such setting the picker is shown with nothing but the main agent in it, on both.
settings.inputComponent = undefined;
s = both();
assert.strictEqual(s.desktop.picker.type, "AgentPicker");
assert.deepStrictEqual(plain(s.phone.picker), plain(s.desktop.picker));
settings.inputComponent = { hideAgentSelectorIfEmpty: true };

// An agent that is chosen is shown even with none to list, as on the desktop.
chosen = { name: "squad" };
s = both();
assert.strictEqual(s.phone.picker.props.selectedAgentScriptItem, chosen);
assert.deepStrictEqual(plain(s.phone.picker), plain(s.desktop.picker));
chosen = undefined;

// An agent asked for in the address is passed on by both.
agents = [{ agent: { name: "squad" }, path: "/work/.agents/agents/squad.md" }];
search = { q: "", ws: undefined, customSubagent: "squad" };
s = both();
assert.deepStrictEqual(s.phone.calls, ["agents", "read", "in step with 1", "asked for squad"]);
assert.deepStrictEqual(s.phone.calls, s.desktop.calls);

// The phone screen builds its picker at every drawing, which is what hooks ask for.
calls = [];
M3c(); M3c();
assert.strictEqual(calls.filter(c => c === "read").length, 2);

// A function that throws at the same step at every drawing costs the screen its picker and nothing else, and says so
// once.
const built = window.__agyPhoneAgentPicker;
const said = [];
console.error = (...a) => said.push(a.join(" "));
window.__agyPhoneAgentPicker = () => { throw new ReferenceError("a is not defined"); };
for (let k = 0; k < 3; k++) {
  const input = inputOf(M3c());
  assert.strictEqual(input.props.agentSelector, undefined, "the input is drawn, with no picker");
  assert.strictEqual(input.props.initialQuery, "");
}
assert.strictEqual(said.length, 1, "the error is written once and not at every drawing");
assert.ok(said[0].includes("a is not defined"), said[0]);
// A promise thrown to wait for something is React's to catch.
const waiting = Promise.resolve();
window.__agyPhoneAgentPicker = () => { throw waiting; };
assert.throws(() => M3c(), e => e === waiting);
assert.strictEqual(said.length, 1);
// With the function back the picker is too.
window.__agyPhoneAgentPicker = built;
assert.strictEqual(inputOf(M3c()).props.agentSelector.type, "AgentPicker");
`)
}

// The input hands the picker to the model selector wherever it has one, toolbar on top or not, and keeps it for
// its own toolbar only where the model selector is hidden, as it did.
func TestAgentPickerGoesToTheModelSelectorWithoutATopToolbar(t *testing.T) {
	src := `var rowOf={"data-testid":"agent-selector-submenu"};var ` + v232AgentRouting + ";"
	out, got := agentStatuses(t, src, Options{MobileUX: true})
	if got["mobile-agent-picker-in-model-menu"].Status != StatusApplied {
		t.Fatalf("the routing is %v", got["mobile-agent-picker-in-model-menu"].Status)
	}
	inputOf := func(body string) string {
		return "(n, q, f) => { var D, C, F, fb = {}, t, b, fa, R, ds; " + body + " return { footer: Fq && Fq.props.footerExtra, modelSelector: Boolean(Fq), own: OTc.props.agentSelector }; }"
	}
	runNode(t, agentPage+`
const before = `+inputOf(src)+`;
const after = `+inputOf(out)+`;
const picker = { type: "AgentPicker" };
// The desktop screen: toolbar on top. The model selector draws the picker, before and after.
for (const input of [before, after]) assert.deepStrictEqual(input("top", false, picker), { footer: picker, modelSelector: true, own: undefined });
// The phone screen: toolbar at the bottom. The input kept the picker for its own toolbar, where a submenu's parts
// have no menu around them. Now the model selector draws it.
assert.deepStrictEqual(before(undefined, false, picker), { footer: undefined, modelSelector: true, own: picker });
assert.deepStrictEqual(after(undefined, false, picker), { footer: picker, modelSelector: true, own: undefined });
// No model selector to draw it: the input keeps it, as it did.
for (const input of [before, after]) for (const toolbar of ["top", undefined]) assert.deepStrictEqual(input(toolbar, true, picker), { footer: undefined, modelSelector: false, own: picker });
// No picker: nothing anywhere, as it was.
for (const input of [before, after]) for (const toolbar of ["top", undefined]) assert.deepStrictEqual(input(toolbar, false, undefined), { footer: undefined, modelSelector: true, own: undefined });
`)
}

// The chip for the agent that is chosen is drawn wherever the input's model selector has the picker in its menu,
// toolbar on top or not, and nowhere it was not drawn before but there.
func TestAgentChipShowsWhereTheModelMenuHasThePicker(t *testing.T) {
	out, got := agentStatuses(t, v232AgentBundle, Options{MobileUX: true})
	if got["mobile-agent-picker-chip"].Status != StatusApplied {
		t.Fatalf("the chip is %v", got["mobile-agent-picker-chip"].Status)
	}
	patched := out[strings.Index(out, "B.createElement(HSc,"):]
	patched = patched[:strings.Index(patched, ";var ")]
	// The footer's other inputs are whatever they are: only the four the chip is decided by are given.
	footerOf := func(body string) string {
		return `($c, hb, L, n) => { var c, h, k, Qc, l, m, le, p, z, hd, yb, ra, Kc, C, H, ac = {}, Eb, Bd, Ia, lb = [], Jd, jb, Rc = [], Za, xd, O, na, W, fa, da, y, S, Z, T, aa, V, I, A, u, D, v, G; return (` + body + `).props.selectedAgent; }`
	}
	runNode(t, `"use strict";
const assert = require("assert");
const B = { createElement: (type, props) => ({ type, props }) };
const HSc = "Footer";
const before = `+footerOf(v232AgentChip)+`;
const after = `+footerOf(patched)+`;
const squad = { name: "squad" };
const chosen = { selectedAgentScriptItem: squad };
const picker = { type: "AgentPicker" };
const withPicker = { type: "ModelSelector", props: { footerExtra: picker } };
const without = { type: "ModelSelector", props: { footerExtra: undefined } };

// The desktop screen: toolbar on top, a new conversation. The chip shows the agent, before and after.
for (const footer of [before, after]) assert.strictEqual(footer(true, false, chosen, withPicker), squad);
// The phone screen: toolbar at the bottom, and the picker in the model selector's menu. No chip before, the chip now.
assert.strictEqual(before(false, false, chosen, withPicker), undefined);
assert.strictEqual(after(false, false, chosen, withPicker), squad, "the phone screen shows which agent is chosen");
// An input whose model selector has no picker in its menu, or that has no model selector: no chip, as before.
for (const footer of [before, after]) for (const model of [without, undefined]) assert.strictEqual(footer(false, false, chosen, model), undefined);
// An existing conversation shows its own agent another way, and never the chip.
for (const footer of [before, after]) for (const top of [true, false]) assert.strictEqual(footer(top, true, chosen, withPicker), undefined);
// No agent chosen, or nothing known of agents: nothing to show.
for (const footer of [before, after]) for (const top of [true, false]) {
  assert.strictEqual(footer(top, false, { selectedAgentScriptItem: undefined }, withPicker), undefined);
  assert.strictEqual(footer(top, false, undefined, withPicker), undefined);
}
`)
}

// Each of the four builds on the one before it, so a bundle that one of them does not fit is left alone by those
// after it, and doctor says which one each went without. A bundle that has the picker in the model selector's menu
// and does not fit one of them is told of, whatever it has renamed.
func TestAgentPickerPatchesBuildOnEachOther(t *testing.T) {
	routing, hook, phone := agentPickerPatches[0], agentPickerPatches[1], agentPickerPatches[2]
	change := func(old, new string) string {
		t.Helper()
		if !strings.Contains(v232AgentBundle, old) {
			t.Fatalf("the bundle has no %q", old)
		}
		return strings.Replace(v232AgentBundle, old, new, 1)
	}
	applied, notNeeded, missing, disabled := StatusApplied, StatusNotNeeded, StatusMissing, StatusDisabled
	afterRouting := [4]Status{missing, notNeeded, notNeeded, notNeeded}
	afterHook := [4]Status{applied, missing, notNeeded, notNeeded}
	for _, c := range []struct {
		name, src string
		opts      Options
		want      [4]Status
	}{
		{"2.32.1", v232AgentBundle, Options{MobileUX: true}, [4]Status{applied, applied, applied, applied}},
		// Before 2.29 the picker stood in the input's toolbar: no menu to draw it in, and no chip.
		{"a bundle before 2.29", "var x=1;const n2c=()=>0," + v232AgentHome + `;var M3c=()=>{P6("MOBILE_HOME_VIEW");return ` + v232PhoneHome + "};", Options{MobileUX: true}, [4]Status{notNeeded, notNeeded, notNeeded, notNeeded}},

		{"the input's choice in another form", change(`$w=n==="top"&&!q,`, `$w=!q&&n==="top",`), Options{MobileUX: true}, afterRouting},
		{"the model selector's footer by another name", strings.ReplaceAll(v232AgentBundle, "footerExtra", "menuFooter"), Options{MobileUX: true}, afterRouting},
		// In the bundles the toolbar's use is some 2,500 characters after the footer's, where the fixture cuts them out.
		{"the toolbar's use as far from the footer as in the bundles", change("environmentSelector:R,agentSelector:", "environmentSelector:R,"+strings.Repeat("ka:b,", 520)+"agentSelector:"), Options{MobileUX: true}, [4]Status{applied, applied, applied, applied}},
		// The decision is changed only where both uses go by it and hand on the same picker.
		{"the toolbar's use decided on its own", change("agentSelector:$w?void 0:f", `agentSelector:n==="top"?void 0:f`), Options{MobileUX: true}, afterRouting},
		{"the footer on another decision", change("footerExtra:$w?f:void 0", "footerExtra:qA?f:void 0"), Options{MobileUX: true}, afterRouting},
		{"the toolbar's use on another decision", change("agentSelector:$w?void 0:f", "agentSelector:qA?void 0:f"), Options{MobileUX: true}, afterRouting},
		{"another decision of that form before it", change(`$w=n==="top"&&!q,`, `$v=n==="top"&&!D,$w=n==="top"&&!q,`), Options{MobileUX: true}, afterRouting},
		{"the footer with another picker", change("footerExtra:$w?f:void 0", "footerExtra:$w?t:void 0"), Options{MobileUX: true}, afterRouting},
		{"the toolbar's use with another picker", change("agentSelector:$w?void 0:f", "agentSelector:$w?void 0:t"), Options{MobileUX: true}, afterRouting},
		{"no toolbar use", change("agentSelector:$w?void 0:f,", ""), Options{MobileUX: true}, afterRouting},

		// The run of hooks is copied only where it is the run this was written for, and reads only what the function
		// that it is copied into gives it.
		{"a run with one more step", change("var {registerOpenSelector:D}=A3a();", "var {registerOpenSelector:D}=A3a();Y1c(z);"), Options{MobileUX: true}, afterHook},
		{"a run that reads another of the screen's names", change("V1c({initialCustomSubagent:g,", "V1c({dispatch:a,initialCustomSubagent:g,"), Options{MobileUX: true}, afterHook},
		{"a run that passes another of the screen's names", change("V1c({initialCustomSubagent:g,", "V1c({initialCustomSubagent:d,"), Options{MobileUX: true}, afterHook},
		{"a run whose names do not agree", change("U1c({fire:q})", "U1c({fire:p})"), Options{MobileUX: true}, afterHook},
		{"a run that calls something of the screen's", change("=\nj5b(),{data:v,", "=\nk(),{data:v,"), Options{MobileUX: true}, afterHook},
		{"a run that ends in another name", change("l=b2c({agentScriptsEnabled:m,", "k=b2c({agentScriptsEnabled:m,"), Options{MobileUX: true}, afterHook},
		{"a setting hidden by another name", change("hideAgentSelectorIfEmpty:l?.hideAgentSelectorIfEmpty,", "hideIfEmpty:l?.hideAgentSelectorIfEmpty,"), Options{MobileUX: true}, afterHook},
		{"a run that does not end the screen's hooks", change("agentScriptsError:y}):void 0;return B.createElement(B.Fragment", "agentScriptsError:y}):void 0;Y1c(l);return B.createElement(B.Fragment"), Options{MobileUX: true}, afterHook},

		// What the function reads for the run is what the screen has in those names when it gets to the run.
		{"a screen that reads the input's settings from elsewhere", change(`l=h.get("inputComponent");`, `l=k.get("inputComponent");`), Options{MobileUX: true}, afterHook},
		{"a screen that reads the tips into the name of the app's settings", change(`k=h.get("tipCards")`, `h=h.get("tipCards")`), Options{MobileUX: true}, afterHook},
		{"a screen that reads the tips into the name of the agent asked for", change(`k=h.get("tipCards")`, `g=h.get("tipCards")`), Options{MobileUX: true}, afterHook},
		{"a screen that changes the input's settings before the run", change("m2c();", "m2c();l=l||{};"), Options{MobileUX: true}, afterHook},
		{"a screen that changes the agent asked for before the run", change("m2c();", "m2c();g&&=g.trim();"), Options{MobileUX: true}, afterHook},
		{"a screen that takes the input's settings apart before the run", change("m2c();", "m2c();var{current:C,...l}=b;"), Options{MobileUX: true}, afterHook},
		{"a screen that so much as reads the agent asked for before the run", change("m2c();", "m2c();g&&m2c();"), Options{MobileUX: true}, afterHook},

		// What the function calls is the bundle's own and nothing of the screen's.
		{"a screen that reads the address with a hook of its own", change("customSubagent:g}=dI({strict:!1})", "customSubagent:g}=a({strict:!1})"), Options{MobileUX: true}, afterHook},
		{"a screen that reads the app's settings with a hook of its own", change("h=oj(),", "h=c(),"), Options{MobileUX: true}, afterHook},
		{"a screen that takes React from a name of its own", strings.ReplaceAll(change("zk(),b=(0,B.useRef)", "zk(),b=(0,a.useRef)"), "B.", "a."), Options{MobileUX: true}, afterHook},
		{"a screen with React by two names", change("zk(),b=(0,B.useRef)", "zk(),b=(0,a.useRef)"), Options{MobileUX: true}, afterHook},
		{"a screen that declares one of the run's hooks before the run", change("m2c();", "m2c();function U1c(){}"), Options{MobileUX: true}, afterHook},
		{"a screen that declares one of its own hooks before the run", change("m2c();", "m2c();function dI(){}"), Options{MobileUX: true}, afterHook},

		// A property by one of those names is not that name.
		{"a screen that reads properties named like them before the run", change("m2c();", "m2c();b.current?.l;b.g;b.U1c;"), Options{MobileUX: true}, [4]Status{applied, applied, applied, applied}},

		{"the phone screen's input in another form", change("showBottomToolbar:!0,aboveContent:", "showBottomToolbar:!0,belowContent:"), Options{MobileUX: true}, [4]Status{applied, applied, missing, notNeeded}},
		{"the chip decided another way", change("selectedAgent:$c&&!hb?L?.selectedAgentScriptItem:void 0", "selectedAgent:hb||!$c?void 0:L?.selectedAgentScriptItem"), Options{MobileUX: true}, [4]Status{applied, applied, applied, missing}},

		{"the first switched off", v232AgentBundle, Options{MobileUX: true, Disabled: map[string]bool{routing: true}}, [4]Status{disabled, notNeeded, notNeeded, notNeeded}},
		{"the second switched off", v232AgentBundle, Options{MobileUX: true, Disabled: map[string]bool{hook: true}}, [4]Status{applied, disabled, notNeeded, notNeeded}},
		{"the third switched off", v232AgentBundle, Options{MobileUX: true, Disabled: map[string]bool{phone: true}}, [4]Status{applied, applied, disabled, notNeeded}},
	} {
		out, got := agentStatuses(t, c.src, c.opts)
		for i, id := range agentPickerPatches {
			// One that was not tried names the one before it, which it went without.
			without := ""
			if i > 0 && c.want[i] == notNeeded {
				without = agentPickerPatches[i-1]
			}
			if got[id].Status != c.want[i] || got[id].Without != without {
				t.Errorf("%s: %s is %v without %q, want %v without %q", c.name, id, got[id].Status, got[id].Without, c.want[i], without)
			}
		}
		// One that is not applied leaves nothing of its own in the bundle, so the phone screen is only handed the
		// function where the function is there to call, and the chip is only drawn for a picker that screen is handed.
		for i, its := range []string{`var $w=!q,`, "__agyPhoneAgentPicker=function()", "window.__agyPhoneAgentPicker()", "?.props?.footerExtra)"} {
			if has := strings.Contains(out, its); has != (c.want[i] == applied) {
				t.Errorf("%s: %s is %v and the bundle has %q: %v", c.name, agentPickerPatches[i], c.want[i], its, has)
			}
		}
	}
}

// A match that the patch turns down is no match at all: nothing is rewritten, and what is reported goes by the same
// rule as for a pattern that found nothing.
func TestAgentPickerMatchTurnedDownIsNoMatch(t *testing.T) {
	hook, phone, chip := agentPickerPatches[1], agentPickerPatches[2], agentPickerPatches[3]
	// The screen as the pattern knows it, with one of the run's names written as another in one place.
	turnedDown := strings.Replace(v232AgentBundle, "U1c({fire:q})", "U1c({fire:p})", 1)
	if turnedDown == v232AgentBundle || !agentScreen.re.MatchString(turnedDown) {
		t.Fatal("the pattern does not match the screen with one name changed")
	}
	// In a bundle with the picker's row in the model selector's menu that is a patch gone missing.
	out, got := agentStatuses(t, turnedDown, Options{MobileUX: true})
	if got[hook].Status != StatusMissing || got[hook].Without != "" {
		t.Errorf("with the row the patch is %v without %q", got[hook].Status, got[hook].Without)
	}
	if strings.Contains(out, "__agyPhoneAgentPicker") {
		t.Error("with the row the bundle was given the function")
	}
	// In one without the row, nothing says the bundle is one the patch is for.
	out, got = agentStatuses(t, strings.Replace(turnedDown, `"agent-selector-submenu"`, `"agent-selector"`, 1), Options{MobileUX: true})
	if got[hook].Status != StatusNotNeeded || got[hook].Without != "" {
		t.Errorf("without the row the patch is %v without %q", got[hook].Status, got[hook].Without)
	}
	if got[phone].Status != StatusNotNeeded || got[phone].Without != hook || got[chip].Status != StatusNotNeeded || got[chip].Without != phone {
		t.Errorf("without the row the two after it are %v without %q and %v without %q", got[phone].Status, got[phone].Without, got[chip].Status, got[chip].Without)
	}
	if strings.Contains(out, "__agyPhoneAgentPicker") {
		t.Error("without the row the bundle was given the function")
	}
}

// The function the run is copied into calls nothing that is the screen's own: a bundle in which one of the names it
// reads from outside is a name of the screen's start, or is declared on the screen's way to the run, is not taken.
func TestAgentPickerFunctionCallsNothingOfTheScreens(t *testing.T) {
	// 2.32.1's names for what the screen's start declares and for what the function reads from outside.
	start := []string{"a", "b", "c", "d", "f", "g", "h", "k", "l"}
	outside := []string{"dI", "oj", "B", "j5b", "U1c", "c2c", "V1c", "A3a", "b2c", "f2c"}
	groups := agentScreen.re.FindStringSubmatch(v232AgentBundle)
	if groups == nil {
		t.Fatal("the pattern does not match the screen")
	}
	for _, c := range []struct {
		roles []string
		want  []string
	}{{agentScreenDeclares, start}, {agentPickerOutside, outside}} {
		var got []string
		for _, role := range c.roles {
			// 2.32.1's start does not read whether the keyboard is up.
			if name := groups[agentScreen.names[role][0]]; name != "" {
				got = append(got, name)
			}
		}
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Fatalf("%v are %v in 2.32.1, want %v", c.roles, got, c.want)
		}
	}
	hookOf := func(home string) Status {
		t.Helper()
		if home == v232AgentHome {
			t.Fatal("nothing was changed")
		}
		_, got := agentStatuses(t, agentBundle(v232AgentChip, v232AgentRouting, home, v232PhoneHome, "dI"), Options{MobileUX: true})
		return got["mobile-agent-picker-hook"].Status
	}
	for _, name := range start {
		// The run calling a name of the start's.
		if got := hookOf(strings.Replace(v232AgentHome, "=A3a();", "="+name+"();", 1)); got != StatusMissing {
			t.Errorf("a run that calls the screen's %s is %v", name, got)
		}
		// React by a name of the start's, in every place the screen reads it.
		if got := hookOf(strings.ReplaceAll(v232AgentHome, "B.", name+".")); got != StatusMissing {
			t.Errorf("a screen with React in its own %s is %v", name, got)
		}
	}
	for _, name := range outside {
		// React is read by the screen's start, where a declaration further down would be in the way of that.
		if name == "B" {
			continue
		}
		if got := hookOf(strings.Replace(v232AgentHome, "m2c();", "m2c();function "+name+"(){}", 1)); got != StatusMissing {
			t.Errorf("a screen that declares %s on its way to the run is %v", name, got)
		}
	}
	// The two that the function reads for the run, declared again on the way to it.
	for _, name := range []string{"g", "l"} {
		if got := hookOf(strings.Replace(v232AgentHome, "m2c();", "m2c();var["+name+"]=b.current;", 1)); got != StatusMissing {
			t.Errorf("a screen that declares %s again on its way to the run is %v", name, got)
		}
	}
}

// A release breaks the bundle's long lines in different places. Each of the four matches, and rewrites the same way,
// with a line break wherever the minifier could put one.
func TestAgentPickerPatchesMatchALineBreakAtAnyToken(t *testing.T) {
	compact := func(s string) string { return strings.Join(strings.Fields(s), "") }
	for i, src := range []string{v232AgentRouting, v232AgentHome, v232PhoneHome, v232AgentChip} {
		var p Patch
		for _, each := range All() {
			if each.ID == agentPickerPatches[i] {
				p = each
			}
		}
		// The patch's own rewrite of its own place, as Apply makes it.
		rewrite := func(src string) (string, bool) {
			loc := p.FindRe.FindStringSubmatchIndex(src)
			if loc == nil || (p.Accept != nil && !p.Accept(submatches([]byte(src), loc))) {
				return "", false
			}
			return src[:loc[0]] + string(p.FindRe.ExpandString(nil, p.Replace, src, loc)) + src[loc[1]:], true
		}
		base, ok := rewrite(src)
		positions := lineBreakPositions(src)
		if !ok || len(positions) < 5 {
			t.Fatalf("%s: matched %v, with %d break positions", p.ID, ok, len(positions))
		}
		for _, pos := range positions {
			if out, ok := rewrite(src[:pos] + "\n" + src[pos:]); !ok || compact(out) != compact(base) {
				t.Errorf("%s: a line break at offset %d is not matched: %q", p.ID, pos, src[max(0, pos-30):min(len(src), pos+30)])
				break
			}
		}
	}
}
