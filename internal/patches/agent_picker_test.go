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

// agentBundle puts the four places together the way a bundle has them: the input's footer and the input first, then
// the two screens.
func agentBundle(chip, routing, home, phone, search string) string {
	return `var chipOf={"data-testid":"selected-agent-chip"},footerOf=()=>` + chip + ";var " + routing + ";const n2c=()=>0," + home + ";var M3c=()=>{var a=L3c(),{q:b}=" + search + `({strict:!1});P6("MOBILE_HOME_VIEW");return ` + phone + "};"
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

// The phone layout's new conversation screen is given the picker the desktop one builds, in every bundle since the
// picker moved into the model selector's menu.
func TestAgentPickerPatchesFitTheBundlesSince229(t *testing.T) {
	node, _ := exec.LookPath("node")
	for _, c := range []struct {
		name, src string
		want      []string
	}{
		{"2.32.1", v232AgentBundle, []string{
			`var $w=!q,qA=D&&C&&F&&n==="top"&&fb.connectors,`,
			"footerExtra:$w?f:void 0}),[q,t,b,fa,$w,f])",
			"agentSelector:$w?void 0:f,",
			`const n2c=()=>0,__agyPhoneAgentPicker=window.__agyPhoneAgentPicker=function(){var{customSubagent:g}=dI({strict:!1}),l=oj().get("inputComponent");var {agentScriptsEnabled:m,`,
			"agentScriptsError:y}):void 0;return l},o2c=()=>{var a=zk(),",
			`initialQuery:b,agentSelector:window.__agyPhoneAgentPicker()})`,
			"onOpenDrivePlugin:G,selectedAgent:($c||n?.props?.footerExtra)&&!hb?L?.selectedAgentScriptItem:void 0})",
		}},
		{"2.29.0", v229AgentBundle, []string{
			`var aG=!r,VN=C&&E&&n==="top"&&Na.connectors,`,
			`const n2c=()=>0,__agyPhoneAgentPicker=window.__agyPhoneAgentPicker=function(){var{customSubagent:g}=cG({strict:!1}),l=hk().get("inputComponent");var {agentScriptsEnabled:n,`,
			"agentScriptsError:z}):void 0;return l},nFc=()=>{var a=Pl(),",
			`initialQuery:b,agentSelector:window.__agyPhoneAgentPicker()})`,
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
		home := v232AgentHome
		if c.name == "2.29.0" {
			home = v229AgentHome
		}
		if !strings.Contains(out, home) {
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

// agentPage is a page for the patched 2.32.1 bundle to run on: React's hooks reduced to what the two screens use of
// them, and the bundle's own pieces replaced by ones that note how they were called.
const agentPage = `
const assert = require("assert");
global.window = global;
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
	// The bundle's declarations are a function's own here, so the page can look at the two screens it declares.
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
`)
}

// The input hands the picker to the model selector wherever it has one, toolbar on top or not, and keeps it for
// its own toolbar only where the model selector is hidden, as it did.
func TestAgentPickerGoesToTheModelSelectorWithoutATopToolbar(t *testing.T) {
	out, got := agentStatuses(t, "var "+v232AgentRouting+";", Options{MobileUX: true})
	if got["mobile-agent-picker-in-model-menu"].Status != StatusApplied {
		t.Fatalf("the routing is %v", got["mobile-agent-picker-in-model-menu"].Status)
	}
	inputOf := func(body string) string {
		return "(n, q, f) => { var D, C, F, fb = {}, t, b, fa, R, ds; " + body + " return { footer: Fq && Fq.props.footerExtra, modelSelector: Boolean(Fq), own: OTc.props.agentSelector }; }"
	}
	runNode(t, agentPage+`
const before = `+inputOf("var "+v232AgentRouting+";")+`;
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

// Each of the four builds on the one before it, so a bundle that one of them does not fit is left alone by those
// after it, and doctor says which one each went without.
func TestAgentPickerPatchesBuildOnEachOther(t *testing.T) {
	routing, hook, phone := agentPickerPatches[0], agentPickerPatches[1], agentPickerPatches[2]
	// A bundle from before 2.29, where the picker stood in the input's toolbar: no menu to draw it in, and no chip.
	older := "var x=1;const n2c=()=>0," + v232AgentHome + `;var M3c=()=>{P6("MOBILE_HOME_VIEW");return ` + v232PhoneHome + "};"
	// The desktop screen's picker built another way.
	otherHome := strings.Replace(v232AgentBundle, `hideAgentSelectorIfEmpty:l?.hideAgentSelectorIfEmpty,`, `hideIfEmpty:l?.hideAgentSelectorIfEmpty,`, 1)
	// The input's choice written another way.
	otherRouting := strings.Replace(v232AgentBundle, `$w=n==="top"&&!q,`, `$w=!q&&n==="top",`, 1)
	// The phone screen's input given something else.
	otherPhone := strings.Replace(v232AgentBundle, "showBottomToolbar:!0,aboveContent:", "showBottomToolbar:!0,belowContent:", 1)
	// The chip decided another way.
	otherChip := strings.Replace(v232AgentBundle, "selectedAgent:$c&&!hb?L?.selectedAgentScriptItem:void 0", "selectedAgent:hb||!$c?void 0:L?.selectedAgentScriptItem", 1)
	applied, notNeeded, missing, disabled := StatusApplied, StatusNotNeeded, StatusMissing, StatusDisabled
	for _, c := range []struct {
		name, src string
		opts      Options
		want      [4]Status
		without   [4]string
	}{
		{"2.32.1", v232AgentBundle, Options{MobileUX: true}, [4]Status{applied, applied, applied, applied}, [4]string{}},
		{"a bundle before 2.29", older, Options{MobileUX: true}, [4]Status{notNeeded, notNeeded, notNeeded, notNeeded}, [4]string{"", routing, hook, phone}},
		{"the input's choice in another form", otherRouting, Options{MobileUX: true}, [4]Status{missing, notNeeded, notNeeded, notNeeded}, [4]string{"", routing, hook, phone}},
		{"the desktop screen's picker in another form", otherHome, Options{MobileUX: true}, [4]Status{applied, missing, notNeeded, notNeeded}, [4]string{"", "", hook, phone}},
		{"the phone screen's input in another form", otherPhone, Options{MobileUX: true}, [4]Status{applied, applied, missing, notNeeded}, [4]string{"", "", "", phone}},
		{"the chip decided another way", otherChip, Options{MobileUX: true}, [4]Status{applied, applied, applied, missing}, [4]string{}},
		{"the first switched off", v232AgentBundle, Options{MobileUX: true, Disabled: map[string]bool{routing: true}}, [4]Status{disabled, notNeeded, notNeeded, notNeeded}, [4]string{"", routing, hook, phone}},
		{"the second switched off", v232AgentBundle, Options{MobileUX: true, Disabled: map[string]bool{hook: true}}, [4]Status{applied, disabled, notNeeded, notNeeded}, [4]string{"", "", hook, phone}},
		{"the third switched off", v232AgentBundle, Options{MobileUX: true, Disabled: map[string]bool{phone: true}}, [4]Status{applied, applied, disabled, notNeeded}, [4]string{"", "", "", phone}},
	} {
		out, got := agentStatuses(t, c.src, c.opts)
		for i, id := range agentPickerPatches {
			if got[id].Status != c.want[i] || got[id].Without != c.without[i] {
				t.Errorf("%s: %s is %v without %q, want %v without %q", c.name, id, got[id].Status, got[id].Without, c.want[i], c.without[i])
			}
		}
		// The phone screen is only handed the function where the function is there to call, and the chip is only
		// drawn for a picker the phone screen was handed.
		called, made, chip := strings.Contains(out, "agentSelector:window.__agyPhoneAgentPicker()"), strings.Contains(out, "window.__agyPhoneAgentPicker=function()"), strings.Contains(out, "?.props?.footerExtra)")
		if called && !made {
			t.Errorf("%s: the phone screen calls a function the bundle does not have", c.name)
		}
		if chip && !called {
			t.Errorf("%s: the chip is drawn for a picker the phone screen is not given", c.name)
		}
	}
}

// A release breaks the bundle's long lines in different places. Each of the four applies the same way with a line
// break wherever the minifier could put one.
func TestAgentPickerPatchesMatchALineBreakAtAnyToken(t *testing.T) {
	compact := func(s string) string { return strings.Join(strings.Fields(s), "") }
	base, got := agentStatuses(t, v232AgentBundle, Options{MobileUX: true})
	for _, id := range agentPickerPatches {
		if got[id].Status != StatusApplied {
			t.Fatalf("%s is %v", id, got[id].Status)
		}
	}
	for _, part := range []struct{ id, src string }{
		{agentPickerPatches[0], v232AgentRouting},
		{agentPickerPatches[1], v232AgentHome},
		{agentPickerPatches[2], v232PhoneHome},
		{agentPickerPatches[3], v232AgentChip},
	} {
		at := strings.Index(v232AgentBundle, part.src)
		positions := lineBreakPositions(part.src)
		if at < 0 || len(positions) < 5 {
			t.Fatalf("%s: %d break positions at %d", part.id, len(positions), at)
		}
		for _, pos := range positions {
			broken := v232AgentBundle[:at+pos] + "\n" + v232AgentBundle[at+pos:]
			out, got := agentStatuses(t, broken, Options{MobileUX: true})
			ok := compact(out) == compact(base)
			for _, id := range agentPickerPatches {
				ok = ok && got[id].Status == StatusApplied
			}
			if !ok {
				t.Errorf("%s: a line break at offset %d is not matched: %q", part.id, pos, part.src[max(0, pos-30):min(len(part.src), pos+30)])
				break
			}
		}
	}
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
	runNode(t, `
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
