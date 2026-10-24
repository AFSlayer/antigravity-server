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
		`e?A.createElement(A.Fragment,null,window.__agyDeleteModal&&typeof agyShowDel!=="undefined"?A.createElement(window.__agyDeleteModal,`,
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
		`else if(_scr&&_scr!=="General"){a(qX(_scr));k.push(_scr)}`,
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

func TestAntigravity224PatchShapes(t *testing.T) {
	t.Run("mobile-new-convo-view", func(t *testing.T) {
		v223 := `var g1b=()=>{var a=e1b(),b=f1b();return(0,A.useCallback)((c,e)=>{a(e.map(f=>({trigger:f,ran:!1})));b(c,{section:e})},[a,b])};var i1b=()=>{var a=h1b(),{q:b}=oR({strict:!1});r4("MOBILE_HOME_VIEW");return A.createElement("div",{className:"w-full h-full flex flex-col min-h-0 animate-fade-in"},A.createElement("div",{className:"flex-1 min-h-0 overflow-y-auto flex flex-col gap-6 pt-3"},A.createElement(g1b,{surface:"background"})),A.createElement("div",null))}`
		v224 := `var j8b=()=>{var a=g8b(),b=h8b();return(0,A.useCallback)((c,e)=>{a(e.map(f=>({trigger:f,ran:!1})));b(c,{section:e})},[a,b])};var k8b=()=>{var a=j8b(),{q:b}=FS({strict:!1}),c=fk().get("conciergeAgent")?.enabled??!1;s4("MOBILE_HOME_VIEW");return A.createElement("div",{className:"w-full h-full flex flex-col min-h-0 animate-fade-in"},A.createElement("div",{className:"flex-1 min-h-0 overflow-y-auto flex flex-col gap-3 pt-3"},c&&A.createElement("div",{className:"shrink-0 px-2","data-testid":"mobile-home-concierge"},A.createElement(b7b,null)),A.createElement(i8b,{surface:"background"})),A.createElement("div",null))}`

		got223 := applyOnly(t, "mobile-new-convo-view", v223, Options{MobileUX: true})
		if !strings.Contains(got223, `{section:e,replace:!0}`) ||
			!strings.Contains(got223, `var {section:sec}=oR({strict:!1}),isMobileNew=`) ||
			!strings.Contains(got223, `,"Start a new conversation")):A.createElement("div",{className:"flex-1 min-h-0 overflow-y-auto flex flex-col gap-6 pt-3"},A.createElement(g1b,{surface:"background"})),`) {
			t.Errorf("2.23 shape output wrong:\n%s", got223)
		}

		got224 := applyOnly(t, "mobile-new-convo-view", v224, Options{MobileUX: true})
		if !strings.Contains(got224, `{section:e,replace:!0}`) ||
			!strings.Contains(got224, `var {section:sec}=FS({strict:!1}),isMobileNew=`) ||
			!strings.Contains(got224, `,"Start a new conversation")):A.createElement("div",{className:"flex-1 min-h-0 overflow-y-auto flex flex-col gap-3 pt-3"},c&&A.createElement("div",{className:"shrink-0 px-2","data-testid":"mobile-home-concierge"},A.createElement(b7b,null)),A.createElement(i8b,{surface:"background"})),`) {
			t.Errorf("2.24 shape output wrong:\n%s", got224)
		}
		if node, err := exec.LookPath("node"); err == nil {
			for _, snippet := range []string{got223, got224} {
				if out, err := exec.Command(node, "-e", "function test(){"+snippet+"}").CombinedOutput(); err != nil {
					t.Fatalf("invalid JS syntax in patched mobile-new-convo-view: %v\n%s", err, out)
				}
			}
		}
	})

	t.Run("mobile-project-kebab-menu", func(t *testing.T) {
		v223 := `var F=!1,H=(0,A.useContext)(z0b),J=rK()&&H!==null,K=!0;`
		v224 := `function D7b(a){var {isOpen:b,onOpenChange:c,ariaLabel:e,sortMode:f,onSortModeChange:g,size:h="icon"}=a;a=B7b(a);return xL()||a.length===0&&!g?null:A.createElement(TM,{open:b,onOpenChange:c})}`

		got223 := applyOnly(t, "mobile-project-kebab-menu", v223, Options{MobileUX: true})
		if want := `,H=(0,A.useContext)(z0b),J=!1,`; !strings.Contains(got223, want) {
			t.Errorf("2.23 shape: want %q in\n%s", want, got223)
		}

		got224 := applyOnly(t, "mobile-project-kebab-menu", v224, Options{MobileUX: true})
		if want := `a=B7b(a);return a.length===0&&!g?null:A.createElement(TM,`; !strings.Contains(got224, want) {
			t.Errorf("2.24 shape: want %q in\n%s", want, got224)
		}
		if node, err := exec.LookPath("node"); err == nil {
			for _, snippet := range []string{got223, got224} {
				if out, err := exec.Command(node, "-e", "function test(){"+snippet+"}").CombinedOutput(); err != nil {
					t.Fatalf("invalid JS syntax in patched mobile-project-kebab-menu: %v\n%s", err, out)
				}
			}
		}
	})

	t.Run("file-upload-large-file-streaming-fallback", func(t *testing.T) {
		v224 := `if(true){}else{var u=qNa(m.name,m.type);if(u)if(m.size>1048576)e?.("Text file size exceeds 1MB limit"),g(Mf.VALIDATION_CHECK_FAILED,` + "\n" + `Error("Text file size exceeds 1MB limit"));else try{}catch(err){}}`
		got224 := applyOnly(t, "file-upload-large-file-streaming-fallback", v224, Options{})
		if want := `if(u)if(m.size>1048576){if(window.__agyUpload){window.__agyUpload([m]);return;}}else try{`; !strings.Contains(got224, want) {
			t.Errorf("2.24 shape: want %q in\n%s", want, got224)
		}
		if node, err := exec.LookPath("node"); err == nil {
			wrapped := "function test(){" + got224 + "}"
			if out, err := exec.Command(node, "-e", wrapped).CombinedOutput(); err != nil {
				t.Fatalf("invalid JS syntax in patched file-upload-large-file-streaming-fallback: %v\n%s", err, out)
			}
		}
	})
}

func TestAntigravity226PatchShapes(t *testing.T) {
	checkSyntax := func(t *testing.T, name, code string) {
		t.Helper()
		node, err := exec.LookPath("node")
		if err != nil {
			return
		}
		if out, err := exec.Command(node, "-e", "function test(){"+code+"}").CombinedOutput(); err != nil {
			t.Fatalf("invalid JS syntax in %s: %v\n%s", name, err, out)
		}
	}

	t.Run("skip-onboarding", func(t *testing.T) {
		v226 := `function x(){c.hasOnboardingScreens&&e!==2&&(Hac(),LQ({to:"/onboarding",search:Meb,replace:!0,throw:!0}))}`
		got := applyOnly(t, "skip-onboarding", v226, Options{})
		if strings.Contains(got, `to:"/onboarding"`) {
			t.Errorf("the onboarding redirect survived:\n%s", got)
		}
		checkSyntax(t, "skip-onboarding", got)
	})

	t.Run("mobile-titlebar-delete-hook", func(t *testing.T) {
		v226 := "var r=PQb(a),t=JQb(a),{handleArchive:u,handleRestore:v,handlePin:w,handleUnpin:x,isArchiveSupported:y,handleShare:B,showShareModal:D,shareUrl:C,handleCloseShareModal:E,\nonShare:G}=tZ(a??\"\");"
		got := applyOnly(t, "mobile-titlebar-delete-hook", v226, Options{MobileUX: true})
		if !strings.Contains(got, `var r=PQb(a),t=JQb(a),{handleArchive:u,handleRestore:v,handlePin:w,handleUnpin:x,isArchiveSupported:y,handleDelete:agyDel,showDeleteModal:agyShowDel,setShowDeleteModal:agySetShowDel,showLoadingSpinner:agyDelSpin,handleCloseDeleteModal:agyCloseDel,handleShare:B,showShareModal:D,shareUrl:C,handleCloseShareModal:E,onShare:G}=tZ(a??"");`) {
			t.Errorf("2.26 shape output wrong:\n%s", got)
		}
		checkSyntax(t, "mobile-titlebar-delete-hook", got)
	})

	t.Run("mobile-settings-projects", func(t *testing.T) {
		v226 := `var X_b=()=>{var a=Nk(),b=gj(),c=b.get("settingsFeature")??Pk.settingsFeature;b=!!b.get("settings")?.hideShortcuts;var {sharedNavItems:e,globalScreens:f,workspaces:g,showWorkspacesSection:h}=S_b();c=[...e.filter(p=>p!=="Account"),...f,...(b?[]:["Shortcuts"]),...(c.hideProvideFeedback?[]:["Provide Feedback"])];var k=S2a(!0);b=k.path.length;var l=b===0,m=k.path[0]??k.exiting[0],n=m?.startsWith("workspace:");return A.createElement("div",{ref:k.setHost,className:"relative h-full w-full overflow-hidden"},` + "\n" + `A.createElement(SM,{offset:-b,active:l,depth:0,footer:e.includes("Account")?A.createElement("div",{className:"flex flex-col border-t border-border p-1"},A.createElement(UWb,{showChevron:!0,className:"mx-1 px-3 py-2.5",onClick:()=>{a(Vu("Account"));k.push("Account")}})):void 0},A.createElement(V_b,{text:"Settings"}),c.map(p=>A.createElement(U_b,{key:p,text:p,label:eT(p),onClick:()=>{a(Vu(p));k.push(p)}})),h&&A.createElement(A.Fragment,null,A.createElement(V_b,{text:"Workspaces"}),g.map(p=>A.createElement(U_b,` + "\n" + `{key:p.uri,text:p.name,onClick:()=>{var r=p.uri;a(Wu({workspaceUri:r}));k.push(` + "`workspace:${r}`" + `)}})))),m&&A.createElement(SM,{offset:1-b,active:b===1,depth:1,exiting:k.exiting.includes(m),onExited:()=>{k.onExited(m)},header:A.createElement(W_b,{onBack:k.pop})},A.createElement(VWb.Provider,{value:!0},n?A.createElement(VZb,null):T_b(m))))};function Y_b(){return["Settings"]};var e0b=A.memo(function(){var a=gj(),b=!!a.get("settings")?.hideProjects,c=!!a.get("settings")?.hideShortcuts,e=Ok(Yu),f=Ok(Zu),{projectManagementFeature:g}=(0,A.useContext)(Qk),h=a.get("settingsFeature")??Pk.settingsFeature,{sharedNavItems:k,globalScreens:l,workspaces:m,showWorkspacesSection:n}=S_b(),p=Ok(wm),r=zl(g?.projectsStateProvider);return null});`
		got := applyOnly(t, "mobile-settings-projects", v226, Options{MobileUX: true})
		for _, want := range []string{
			`var {projectManagementFeature:_pm}=(0,A.useContext)(Qk),_hp=!!gj().get("settings")?.hideProjects,_ps=zl(_pm?.projectsStateProvider)`,
			`{text:"Projects"}`,
			`{text:"Not in Project"}`,
			`else if(_scr&&_scr!=="General"){a(Vu(_scr));k.push(_scr)}`,
			`m?.startsWith("project:")&&window.__agyProjectSettings?A.createElement(window.__agyProjectSettings,null):n?A.createElement(VZb,null):T_b(m)`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("2.26 shape missing %q in:\n%s", want, got)
			}
		}
		checkSyntax(t, "mobile-settings-projects", got)
	})

	t.Run("composer-upload-menu-item", func(t *testing.T) {
		v226 := `var items=[{icon:Aa=>A.createElement(Z,{name:"attach_file",size:Aa.width?Number(Aa.width):14,className:Aa.className}),label:"Files",onClick:aa}];`
		got := applyOnly(t, "composer-upload-menu-item", v226, Options{})
		if !strings.Contains(got, `label:"Upload File",onClick:()=>window.__agyTriggerUpload&&window.__agyTriggerUpload()`) {
			t.Errorf("2.26 shape output wrong:\n%s", got)
		}
		checkSyntax(t, "composer-upload-menu-item", got)
	})

	t.Run("file-upload-input-reset", func(t *testing.T) {
		v226 := `var Z7a=({onFilesSelected:a})=>{var b=(0,A.useRef)(null),c=(0,A.useCallback)(e=>{e=e.target;if(e.files&&e.files.length>0){var f=e.files;if(typeof DataTransfer!=="undefined"&&typeof FileList!=="undefined"&&e.files instanceof FileList){f=new DataTransfer;for(let g of Array.from(e.files))f.items.add(g);f=f.files}a(f);e.value=""}},[a]);return{openFileDialog:(0,A.useCallback)(()=>{b.current?.click()},[]),fileInputRef:b,handleFileChange:c}};`
		got := applyOnly(t, "file-upload-input-reset", v226, Options{})
		if !strings.Contains(got, `if(b.current)b.current.value="";b.current?.click()`) {
			t.Errorf("2.26 shape output wrong:\n%s", got)
		}
		checkSyntax(t, "file-upload-input-reset", got)
	})

	t.Run("question-modal-write-in-focus", func(t *testing.T) {
		v226 := "var props={onClick:()=>{e||\n(m(!0),a.isMultiSelect||k())},onChange:D=>{l(D.target.value)}};"
		got := applyOnly(t, "question-modal-write-in-focus", v226, Options{})
		if !strings.Contains(got, `,onFocus:()=>{e||(m(!0),a.isMultiSelect||k())},onChange:`) {
			t.Errorf("2.26 shape output wrong:\n%s", got)
		}
		checkSyntax(t, "question-modal-write-in-focus", got)
	})

	t.Run("question-modal-prevent-radio-focus-steal", func(t *testing.T) {
		v226 := `e||J===void 0||a.isMultiSelect||!document.hasFocus()||vGb(K)&&K!==E.current||G.current.get(J)?.focus();`
		got := applyOnly(t, "question-modal-prevent-radio-focus-steal", v226, Options{})
		want := `e||J===void 0||a.isMultiSelect||!document.hasFocus()||vGb(K)&&K!==E.current||document.activeElement?.getAttribute?.("data-testid")==="ask-question-writein"||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches)||G.current.get(J)?.focus();`
		if got != want {
			t.Errorf("2.26 shape:\ngot:  %s\nwant: %s", got, want)
		}
		checkSyntax(t, "question-modal-prevent-radio-focus-steal", got)
	})
}
