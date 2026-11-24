package patches

import (
	"strings"
	"testing"
)

// The text each patch matches in Antigravity 2.35.0, with the bundle's own line breaks.
const (
	v235WebSocketTransport = `function ISd(){var a=(new URLSearchParams(window.location.search)).get("useWebSocket");return a!==null?a==="true"?"explicit":"off":"default"}`
	v235AgentScreen        = `Tqd=()=>{var a=ul(),b=(0,E.useRef)(null),c=Sqd(),{q:d,ws:f,customSubagent:g}=SR({strict:!1}),h=Zj(),k=qT();k=lk(k);var l=h.get("tipCards"),m=h.get("inputComponent");h=(0,E.useRef)(null);Rqd();(0,E.useEffect)(()=>{f&&b.current!==f&&(b.current=f,a(rr(!1)))},[f,a]);var {agentScriptsEnabled:n,selectedAgentScriptItem:p,setSelectedAgentScriptItem:q,globalScriptsDir:r,workspaceScriptsDir:u}=
Bjc(),{data:v,refetch:w,isLoading:x,error:y}=Ajc(),z=(0,E.useMemo)(()=>v?.agentCustomizations??[],[v]),A=(0,E.useMemo)(()=>z.filter(B=>!B.agent?.disabled),[z]);Cqd({isLoadingAgentScripts:x,agentScriptsError:y,selectedAgentScriptItem:p,enabledCustomizations:A,setSelectedAgentScriptItem:q});uqd({initialCustomSubagent:g,agentCustomizations:z,setSelectedAgentScriptItem:q});m=Bqd({agentScriptsEnabled:n,isLoadingAgentScripts:x,agentScriptsError:y,hideAgentSelectorIfEmpty:m?.hideAgentSelectorIfEmpty,agentCustomizationsCount:z.length,
selectedAgentScriptItem:p})?E.createElement(Fqd,{agentScriptsEnabled:n,selectedAgentScriptItem:p,setSelectedAgentScriptItem:q,globalScriptsDir:r,workspaceScriptsDir:u,agentCustomizations:z,refreshAgentScripts:w,isLoadingAgentScripts:x,agentScriptsError:y}):void 0;return E.createElement(E.Fragment`
	v235IndicatorSetting = `{title:"Chat",settings:["verboseConversationSteps"]},{title:"Chat Layout",advanced:!0,settings:["conversationWidth","artifactWidth","contextUsageIndicatorEnabled"]}`
	v235DocRow           = `function c5({title:a,displayPath:b,copyPath:c,description:d,content:f,modalChildren:g,badge:h,actions:k,actionsPaddingClass:l,disabled:m=!1,MarkdownRenderer:n}){var [p,q]=(0,E.useState)(!1),r=!!d||!!f||!!g,u=d||b;`
	v235PagingDecision   = `var je=fc.startIndex,yb=je<Ac.startIndex;if(kb(tb,fc,Wb))Ra(!yb,!md),(yb||md)&&Ta(g.timing.oppositeDirectionCooldownMs||g.timing.retryDelayMs);else{`
)

// 2.35 rewrote or rewrapped what these patches match.
func TestAntigravity235PatchShapes(t *testing.T) {
	for _, c := range []struct {
		id, src string
		opts    Options
		want    []string
		gone    []string
	}{
		// The parameter is read inline, and the fallback is the bundle's own default mode.
		{"websocket-transport-default", v235WebSocketTransport, Options{}, []string{`return a!==null?a==="true"?"explicit":"off":new URLSearchParams(window.location.search).get("wsTransport")==="1"?"off":"explicit"}`}, []string{`"default"`}},
		// The first flag has another name.
		{"settings-doc-row-editor", v235DocRow, Options{}, []string{`var [p,q]=(0,E.useState)(!1),_R=E,`, `var r=!!d||!!f||!!g,u=d||b;`}, nil},
		// A window it waits for first clears what is shown of loads at the ends it does not grow.
		{"paging-trim-waits-for-still", v232PagingConfig + v235PagingDecision, Options{}, []string{"if(kb(tb,fc,Wb))Ra(!yb,!md),(yb||md)&&Ta(g.timing.oppositeDirectionCooldownMs||g.timing.retryDelayMs);else if(!(yb||md)&&fc.startIndex>Ac.startIndex&&window.__agyDeferTrim?.())Ta(250);else if(fc.startIndex<Ac.startIndex&&window.__agyDeferGrow?.())Ta(100);else{"}, nil},
	} {
		got, status := applyWithStatus(t, c.id, c.src, c.opts)
		if status != StatusApplied {
			t.Errorf("%s: %v", c.id, status)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: no %q in:\n%s", c.id, w, got)
			}
		}
		for _, g := range c.gone {
			if strings.Contains(got, g) {
				t.Errorf("%s: %q is still in:\n%s", c.id, g, got)
			}
		}
	}

	// The clearing negates the names the retry goes by, or it is not this decision.
	other := strings.Replace(v235PagingDecision, `Ra(!yb,!md)`, `Ra(!md,!yb)`, 1)
	if _, status := applyWithStatus(t, "paging-trim-waits-for-still", v232PagingConfig+other, Options{}); status != StatusMissing {
		t.Errorf("a clearing of other ends: status %v, want %v", status, StatusMissing)
	}

	// Chat Layout is marked advanced, which is kept where the setting is moved out of it.
	got := contextIndicatorSettingInChatRe.ReplaceAllString(v235IndicatorSetting, contextIndicatorSettingInChat)
	if want := `{title:"Chat",settings:["verboseConversationSteps","contextUsageIndicatorEnabled"]},{title:"Chat Layout",advanced:!0,settings:["conversationWidth","artifactWidth"]}`; got != want {
		t.Errorf("context-indicator-setting-in-chat gives %q, want %q", got, want)
	}

	// The keyboard check without the comparison, the agents read without the function that fetches them, and no
	// refetch when the selector opens.
	loc := agentScreen.re.FindStringSubmatchIndex(v235AgentScreen)
	if loc == nil || !agentScreenIsKnown(submatches([]byte(v235AgentScreen), loc)) {
		t.Fatal("mobile-agent-picker-hook does not take the 2.35 screen")
	}
	got = string(agentScreen.re.ExpandString(nil, mobileAgentPickerHookReplace, v235AgentScreen, loc))
	if want := `__agyPhoneAgentPicker=window.__agyPhoneAgentPicker=function(){var{customSubagent:g}=SR({strict:!1}),m=Zj().get("inputComponent");var {agentScriptsEnabled:n,`; !strings.HasPrefix(got, want) {
		t.Errorf("mobile-agent-picker-hook gives:\n%s\nwant it to start with %q", got, want)
	}
	if !strings.Contains(got, `k=qT();k=lk(k);var l=h.get("tipCards")`) {
		t.Errorf("the screen's keyboard check is not kept:\n%s", got)
	}
	// The agents' fetch with the function on one side and not the other is not this run.
	half := strings.Replace(v235AgentScreen, `selectedAgentScriptItem:p,setSelectedAgentScriptItem:q,globalScriptsDir`, `selectedAgentScriptItem:p,setSelectedAgentScriptItem:q,getAgentScripts:t,globalScriptsDir`, 1)
	if half == v235AgentScreen {
		t.Fatal("nothing was changed")
	}
	if loc := agentScreen.re.FindStringSubmatchIndex(half); loc != nil && agentScreenIsKnown(submatches([]byte(half), loc)) {
		t.Error("a run that reads the fetch function and does not pass it is taken")
	}
}

func TestAntigravity235PatchesTolerateLineBreaks(t *testing.T) {
	compact := func(s string) string { return strings.Join(strings.Fields(s), "") }
	for _, c := range []struct{ id, src string }{
		{"paging-trim-waits-for-still", v235PagingDecision},
	} {
		base, status := applyWithStatus(t, c.id, v232PagingConfig+c.src, Options{})
		if status != StatusApplied {
			t.Errorf("%s: status %v without a line break", c.id, status)
			continue
		}
		for _, pos := range lineBreakPositions(c.src) {
			out, status := applyWithStatus(t, c.id, v232PagingConfig+c.src[:pos]+"\n"+c.src[pos:], Options{})
			if status != StatusApplied || compact(out) != compact(base) {
				t.Errorf("%s: status %v with a line break at offset %d: %q", c.id, status, pos, c.src[max(0, pos-30):min(len(c.src), pos+30)])
				break
			}
		}
	}
}

// The refresh on opening the selector, which 2.34 has and 2.35 dropped, is taken only where it reads the screen's
// React and refetches through the run's own refetch.
func TestAgentScreenRefreshNames(t *testing.T) {
	known := func(src string) bool {
		loc := agentScreen.re.FindStringSubmatchIndex(src)
		return loc != nil && agentScreenIsKnown(submatches([]byte(src), loc))
	}
	if !known(v234AgentScreen) {
		t.Fatal("the 2.34 screen is not taken")
	}
	refresh := `(0,E.useEffect)(()=>{var D=B(()=>{x()});`
	for name, other := range map[string]string{
		"another React":   `(0,F.useEffect)(()=>{var D=B(()=>{x()});`,
		"another refetch": `(0,E.useEffect)(()=>{var D=B(()=>{y()});`,
	} {
		changed := strings.Replace(v234AgentScreen, refresh, other, 1)
		if changed == v234AgentScreen {
			t.Fatalf("%s: nothing was changed", name)
		}
		if known(changed) {
			t.Errorf("a refresh with %s is taken", name)
		}
	}
}
