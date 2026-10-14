package patches

import (
	"os/exec"
	"strings"
	"testing"
)

// applyOnly runs the whole patch set over body and fails when the named patch
// is reported missing, so a bundle change that moves one anchor is caught by
// name instead of by a vague output mismatch.
func applyOnly(t *testing.T, id string, body string, opts Options) string {
	t.Helper()
	out, report := Apply(MainJS, []byte(body), opts)
	for _, r := range report.Missing() {
		if r.ID == id {
			t.Fatalf("%s did not apply to:\n%s", id, body)
		}
	}
	return string(out)
}

// 2.23.0 added a search argument to the onboarding redirect.
func TestSkipOnboardingAcceptsSearchArgument(t *testing.T) {
	body := `c.hasOnboardingScreens&&e!==2&&aQ({to:"/onboarding",search:wR,replace:!0,throw:!0})}}))(J5,"manager-landing-lay`
	got := applyOnly(t, "skip-onboarding", body, Options{})
	if strings.Contains(got, `to:"/onboarding"`) {
		t.Errorf("the onboarding redirect survived:\n%s", got)
	}
}

// 2.23.0 prefixes the hidden state with can-hover:, which the patch has to
// carry through unchanged.
func TestConversationRowActionsKeepsCanHoverPrefix(t *testing.T) {
	const tail = `can-hover:opacity-0 group-hover:opacity-100 group-focus-visible:opacity-100 group-has-[:focus-visible]:opacity-100 has-[:focus-visible]:opacity-100`
	body := `className:Xi("absolute top-0 bottom-0 -right-1 pl-6 flex items-center justify-end gap-0.5 z-10",x?"hidden":Wa?"opacity-100":"` + tail + `")`
	got := applyOnly(t, "mobile-conversation-row-actions", body, Options{MobileUX: true})
	if !strings.Contains(got, `Boolean(window.innerWidth<=768`) || !strings.Contains(got, `?"opacity-100":"`+tail+`")`) {
		t.Errorf("row actions output is wrong:\n%s", got)
	}
}

// 2.23.0 renamed the result variable and put a line break after the call.
func TestTitlebarDeleteModalAllowsLineBreakAndNewVariable(t *testing.T) {
	body := "J=f&&!!a;b=SDb({cascadeId:a,paneId:b,includeRemoveFromSplit:!1});\nreturn H.length>0||b.length>0||G.length>0||g.length>0||e?A.createElement(A.Fragment,null,A.createElement(NL,null)):null"
	got := applyOnly(t, "mobile-titlebar-delete-modal", body, Options{MobileUX: true})
	for _, want := range []string{
		`e?A.createElement(A.Fragment,null,window.__agyDeleteModal?A.createElement(window.__agyDeleteModal,`,
		`typeof agyDel==="function"&&a`,
		`showLoadingSpinner:agyDelSpin}):null,A.createElement(NL,null)):null`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("titlebar output is missing %q:\n%s", want, got)
		}
	}
}

// 2.23.0 moved the accept list into a shared constant.
func TestFileUploadAcceptAllHandlesConstantReference(t *testing.T) {
	body := `A.createElement(n5a,{ref:wa,onChange:za,accept:PYb,multiple:!0}),A.createElement`
	got := applyOnly(t, "file-upload-accept-all", body, Options{})
	if want := `{ref:wa,onChange:za,accept:"*/*",multiple:!0}`; !strings.Contains(got, want) {
		t.Errorf("want %q in\n%s", want, got)
	}
}

// The constant form is only recognised next to onChange, so an unrelated file
// input that happens to look similar is left alone.
func TestFileUploadAcceptAllIgnoresUnrelatedConstantInput(t *testing.T) {
	body := `A.createElement("input",{type:"file",accept:PYb,multiple:!0})`
	if fileUploadAcceptAllRe.MatchString(body) {
		t.Errorf("the pattern matched an input without the onChange context")
	}
}

// 2.23.0 breaks the line after the comma in the 1MB size error.
func TestFileUploadStreamingFallbackAllowsLineBreak(t *testing.T) {
	body := `else{var u=vNa(m.name,m.type);if(u)if(m.size>1048576)e?.("Text file size exceeds 1MB limit"),g("validation_check_failed",` + "\n" + `Error("Text file size exceeds 1MB limit"));else try{a([q(await n(SN(m,u)))])}catch(x){q(),g("val`
	got := applyOnly(t, "file-upload-large-file-streaming-fallback", body, Options{})
	if strings.Contains(got, `Text file size exceeds 1MB limit`) {
		t.Errorf("the size error survived:\n%s", got)
	}
}

// The transport selector returns a boolean up to 2.22 and a mode string from
// 2.23. The patch has to pick the replacement that fits the shape it found.
func TestWebsocketTransportDefaultBothShapes(t *testing.T) {
	old := `function wrc(){var a=new URLSearchParams(window.location.search),b=a.get("useWebSocket");return b!==null?b==="true":a.get("wsTransport")==="2"}`
	cur := `function wrc(){var a=new URLSearchParams(window.location.search),b=a.get("useWebSocket");return b!==null?b==="true"?"explicit":"off":a.get("wsTransport")==="3"?"hub":"off"}` + "\nvar xrc=class{}"

	got := applyOnly(t, "websocket-transport-default", old, Options{})
	if want := `return b!==null?b==="true":a.get("wsTransport")!=="1"}`; !strings.Contains(got, want) {
		t.Errorf("2.22 shape: want %q in\n%s", want, got)
	}

	got = applyOnly(t, "websocket-transport-default", cur, Options{})
	want := `return b!==null?b==="true"?"explicit":"off":a.get("wsTransport")==="1"?"off":"explicit"}`
	if !strings.Contains(got, want) {
		t.Errorf("2.23 shape: want %q in\n%s", want, got)
	}
	if !strings.Contains(got, "\nvar xrc=class{}") {
		t.Errorf("the code after the function was lost:\n%s", got)
	}
}

// A selector whose two halves belong to different spellings is not something
// the patch knows how to rewrite, so it must not match.
func TestWebsocketTransportDefaultRejectsMixedShapes(t *testing.T) {
	head := `function wrc(){var a=new URLSearchParams(window.location.search),b=a.get("useWebSocket");return b!==null?b==="true"`
	for name, body := range map[string]string{
		"boolean head with hub tail": head + `:a.get("wsTransport")==="3"?"hub":"off"}`,
		"mode head with old tail":    head + `?"explicit":"off":a.get("wsTransport")==="2"}`,
	} {
		if websocketTransportDefaultRe.MatchString(body) {
			t.Errorf("%s: the pattern matched a mixed spelling", name)
		}
	}
}

// The composer's Enter handler gained a shift-key check before preventDefault.
// The patch has to keep that check and still insert the touch and IME guard
// ahead of preventDefault, for both the older and the newer handler.
func TestMobileEnterNewlineBothHandlerShapes(t *testing.T) {
	shift := `var l=k.shiftKey&&(k.metaKey||k.ctrlKey);if(k.shiftKey&&!l)return!1;`
	for name, body := range map[string]string{
		"with shift check":    `b.registerCommand(eE,k=>{if(!k)return!1;` + shift + `k.preventDefault();a(k,e);return!0},1)`,
		"without shift check": `b.registerCommand(eE,k=>{if(!k)return!1;k.preventDefault();a(k,e);return!0},1)`,
	} {
		got := applyOnly(t, "mobile-enter-newline", body, Options{MobileUX: true})
		if !strings.Contains(got, `registerCommand(eE,k=>{if(!k)return!1;`) ||
			!strings.Contains(got, `_i=k.isComposing||k.keyCode===229||`) ||
			!strings.Contains(got, `if(_i||(_t&&!k.metaKey&&!k.ctrlKey))return!1;k.preventDefault();a(k,e);return!0}`) {
			t.Errorf("%s: unexpected output:\n%s", name, got)
		}
		if strings.Contains(body, "shiftKey") != strings.Contains(got, "k.shiftKey&&!l)return!1;var _t") {
			t.Errorf("%s: the shift check was not kept in front of the guard:\n%s", name, got)
		}
	}
}

// Antigravity 2.23.0 populates localStorage["projectsOrder"] on mount with
// projectsFolderSortBy defaulted to "custom", which locks in alphabetical order
// when projects load before conversation summaries arrive. Ensure the patch
// dynamically sorts whenever projectsSortBy is not "custom" across 2.21, 2.22,
// and 2.23 bundle shapes while keeping any preceding folder-sort check intact.
func TestMobileProjectSortOrderShapes(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "2.21 four-arg helper",
			body: `var za=(0,A.useMemo)(()=>{if(I.length===0)return Fgb(sa,A,G,va).map(wj);var ha=sa.filter(ka=>!I.includes(ka));return ha},[y,I,G])`,
			want: `if(G!=="custom"||I.length===0)return Fgb(sa,A,G,va).map(wj);`,
		},
		{
			name: "2.22 five-arg helper",
			body: `var za=(0,A.useMemo)(()=>{if(P.length===0)return qJb(Ba,F,L,R,X).map(uh);var ua=P.filter(ha=>Ba.includes(ha));return ua},[a,P,L])`,
			want: `if(L!=="custom"||P.length===0)return qJb(Ba,F,L,R,X).map(uh);`,
		},
		{
			name: "2.23 with folder sort branch",
			body: `var da=(0,A.useMemo)(()=>{if(P==="last_user_prompt")return kZ(ha,D,"last_user_prompt",U).map(th);if(N.length===0)return kZ(ha,D,J,U,ea).map(th);var na=new Set(N);return na},[P,ha,N,J])`,
			want: `if(P==="last_user_prompt")return kZ(ha,D,"last_user_prompt",U).map(th);if(J!=="custom"||N.length===0)return kZ(ha,D,J,U,ea).map(th);`,
		},
	}

	for _, tc := range cases {
		got := applyOnly(t, "mobile-project-sort-order", tc.body, Options{MobileUX: true})
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: want %q in\n%s", tc.name, tc.want, got)
		}
	}
}

func TestMobileSettingsProjectsAndDeepLinks(t *testing.T) {
	gotExport := applyOnly(t, "mobile-project-settings-export", regexpFixtures["mobile-project-settings-export"], Options{MobileUX: true})
	if !strings.Contains(gotExport, `window.__agyProjectSettings=RNb;function RNb(){`) {
		t.Errorf("export patch output wrong:\n%s", gotExport)
	}

	gotSettings := applyOnly(t, "mobile-settings-projects", regexpFixtures["mobile-settings-projects"], Options{MobileUX: true})
	for _, want := range []string{
		`{text:"Projects"}`,
		`{text:"Not in Project"}`,
		`settingsScreen/navigateToProjectScreen`,
		`m?.startsWith("project:")&&window.__agyProjectSettings?A.createElement(window.__agyProjectSettings,null):n?A.createElement(gMb,null):fNb(m)`,
	} {
		if !strings.Contains(gotSettings, want) {
			t.Errorf("want %q in\n%s", want, gotSettings)
		}
	}
}

func TestBootstrapLocalUserInfoRetryShapes(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "2.21 F5b",
			body: `async function F5b(a){var b=d,c=d;try{let e=c(await b(a.lsClient.getLocalUserInfo(q(Oea,{})))),f=e.username;return{homeDirUri:vg(e.homeDirUri),username:f}}finally{b()}};`,
			want: `async function F5b(a){var b=d,c=d;try{let e;for(let _i=0;_i<3;_i++)try{e=c(await b(a.lsClient.getLocalUserInfo(q(Oea,{}))));break}catch(_e){c();if(_i===2)return{homeDirUri:vg("file:///"),username:""};await new Promise(_r=>setTimeout(_r,250*(_i+1)))}let f=e.username;return{homeDirUri:vg(e.homeDirUri),username:f}}finally{b()}};`,
		},
		{
			name: "2.22 Qtc",
			body: `async function Qtc(a){var b=d,c=d;try{let e=c(await b(a.lsClient.getLocalUserInfo(r(Kga,{})))),f=e.username;return{homeDirUri:Qe(e.homeDirUri),username:f}}finally{b()}};`,
			want: `async function Qtc(a){var b=d,c=d;try{let e;for(let _i=0;_i<3;_i++)try{e=c(await b(a.lsClient.getLocalUserInfo(r(Kga,{}))));break}catch(_e){c();if(_i===2)return{homeDirUri:Qe("file:///"),username:""};await new Promise(_r=>setTimeout(_r,250*(_i+1)))}let f=e.username;return{homeDirUri:Qe(e.homeDirUri),username:f}}finally{b()}};`,
		},
		{
			name: "2.23 Fzc",
			body: `async function Fzc(a){var b=d,c=d;try{let e=c(await b(a.lsClient.getLocalUserInfo(p(Fga,{})))),f=e.username;return{homeDirUri:Oe(e.homeDirUri),username:f}}finally{b()}};`,
			want: `async function Fzc(a){var b=d,c=d;try{let e;for(let _i=0;_i<3;_i++)try{e=c(await b(a.lsClient.getLocalUserInfo(p(Fga,{}))));break}catch(_e){c();if(_i===2)return{homeDirUri:Oe("file:///"),username:""};await new Promise(_r=>setTimeout(_r,250*(_i+1)))}let f=e.username;return{homeDirUri:Oe(e.homeDirUri),username:f}}finally{b()}};`,
		},
	}

	for _, tc := range cases {
		got := applyOnly(t, "bootstrap-local-user-info-retry", tc.body, Options{})
		if got != tc.want {
			t.Errorf("%s:\ngot:  %s\nwant: %s", tc.name, got, tc.want)
		}
	}

	node, err := exec.LookPath("node")
	if err != nil {
		return
	}
	patched := applyOnly(t, "bootstrap-local-user-info-retry", cases[2].body, Options{})
	script := `
const d = (x) => x;
const p = (x, y) => ({});
const Fga = {};
const Oe = (u) => "URI(" + u + ")";
const origSetTimeout = global.setTimeout;
global.setTimeout = (fn) => origSetTimeout(fn, 0);
` + patched + `
(async () => {
  let calls = 0;
  // 1) Transient failure on first attempt, success on second attempt
  let res = await Fzc({
    lsClient: {
      getLocalUserInfo: async () => {
        calls++;
        if (calls === 1) throw new Error("socket closed");
        return { homeDirUri: "file:///home/ubuntu", username: "ubuntu" };
      }
    }
  });
  if (calls !== 2 || res.homeDirUri !== "URI(file:///home/ubuntu)" || res.username !== "ubuntu") {
    throw new Error("retry case failed: " + JSON.stringify({ calls, res }));
  }

  // 2) All 3 attempts fail -> fallback returned without throwing
  calls = 0;
  res = await Fzc({
    lsClient: {
      getLocalUserInfo: async () => {
        calls++;
        throw new Error("still closed");
      }
    }
  });
  if (calls !== 3 || res.homeDirUri !== "URI(file:///)" || res.username !== "") {
    throw new Error("fallback case failed: " + JSON.stringify({ calls, res }));
  }
})();
`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("patched Fzc runtime test failed: %v\n%s", err, out)
	}
}
