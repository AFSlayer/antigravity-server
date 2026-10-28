package patches

import (
	"bytes"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// syntheticBundle builds a document that contains every main.js anchor once,
// separated by filler. It exercises the patch engine without shipping any of
// Antigravity's compiled code in this repository; the anchors themselves are
// checked against the real bundle by TestAnchorsMatchLiveBundle and by
// "agy-server doctor".
func syntheticBundle() []byte {
	var buf bytes.Buffer

	buf.WriteString("(function(){var filler=0;\n")
	for _, p := range All() {
		if p.Target != MainJS {
			continue
		}

		buf.WriteString("/* " + p.ID + " */\n")
		switch p.Kind {
		case Literal:
			buf.WriteString(p.Find)
		case Regexp:
			buf.WriteString(regexpFixtures[p.ID])
		}
		buf.WriteString("\nfiller++;\n")
	}
	buf.WriteString("})();\n")

	return buf.Bytes()
}

// regexpFixtures supplies a synthetic match for each regexp patch, since a
// regexp has no literal to reuse.
var regexpFixtures = map[string]string{
	"skip-onboarding":                           `c.hasOnboardingScreens&&e!==2&&RK({to:"/onboarding",replace:!0,throw:!0})`,
	"mobile-enter-newline":                      `registerCommand(FE,k=>{if(!k)return!1;var l=k.shiftKey&&(k.metaKey||k.ctrlKey);if(k.shiftKey&&!l)return!1;k.preventDefault();`,
	"model-effort-submenu":                      ",onClick:()=>{var y=\nv.byEffort.get(w);y&&b(y)}",
	"hide-mic-button":                           `uz.displayName="GutterHoverCommentButton";var vz=(`,
	"hide-user-profile-button":                  `function wmb({className:a=""}={}){return x.createElement("a",{href:"#",onClick:b=>{b.preventDefault()},className:` + "`w-6 h-6 rounded-full overflow-hidden shrink-0 flex items-center justify-center bg-transparent text-muted-foreground ${a}`" + `,"aria-label":"User Profile (Placeholder)"`,
	"sign-in-button":                            `rightElement:x.createElement(tz,{variant:"primary",onClick:()=>` + "\n" + `b.showLoginFlow()},"Sign In")`,
	"auth-service-login-redirect":               `async loginWithRedirect(a){var b=d;`,
	"auth-service-show-login-flow":              `async showLoginFlow(){this._onDidRequestLoginFlow.fire()}`,
	"auth-service-stub-login-redirect":          `loginWithRedirect:async()=>{console.log("[AuthService] loginWithRedirect")},showLoginFlow:async()=>{console.log("[AuthService] showLoginFlow")}`,
	"mobile-skip-notification-prompt":           `var e=!!this.storageService.get("didAskForNotificationPermission");`,
	"mobile-new-convo-view":                     `const tub=()=>{var a=yM(),b=IT();return(0,x.useCallback)((c,e)=>{b(HT.map(f=>({trigger:f,ran:!1})));a(c,{section:e})},[a,b])};` + "\n" + `var uub=()=>{var a=tub(),{q:b}=dM({strict:!1});return x.createElement("div",{className:"w-full h-full flex flex-col min-h-0 animate-fade-in"},x.createElement("div",{className:"flex-1 min-h-0 overflow-y-auto flex flex-col gap-6 pt-3"},x.createElement(sub,{surface:"background"})),`,
	"mobile-new-convo-header":                   `CM=()=>QL({select:a=>a.location.pathname==="/"})`,
	"mobile-back-clears-section":                `x.createElement(gZ,{iconName:"arrow_back",onClick:()=>c(),"aria-label":"Back to home",dataTestId:"mobile-back-to-home"})`,
	"mobile-project-add-button":                 `if(d==="project"||d==="environment"||d==="status"){let za=B?void 0:d==="project"?"New Conversation in Project":d==="environment"?"New Conversation in Workspace":void 0`,
	"mobile-project-header-actions":             `className:Pm("absolute right-1 top-0 flex h-full items-center gap-1",k||t?"opacity-100":"opacity-0 group-hover/header:opacity-100 group-focus-within/header:opacity-100")`,
	"mobile-project-kebab-menu":                 `,z=(0,y.useContext)(DEb),A=lK()&&z!==null,`,
	"mobile-project-add-click-close-sidebar":    `onClick:D=>{D.stopPropagation();Skb(D)||(D.preventDefault(),d(D))}`,
	"mobile-project-sort-order":                 `if(N.length===0)return kZ(ha,D,J,U,ea).map(th);`,
	"mobile-project-settings-export":            `function RNb(){var a=Hm(wX),b=Hm(xX),c=Xm(),e=$t(),{logEvent:f}=aA(),[g,h]=V3("main");return null}`,
	"mobile-settings-projects":                  `var jNb=()=>{var a=Gm(),{settingsFeature:b}=(0,A.useContext)(xk),c=!!Lj().get("settings")?.hideShortcuts,{sharedNavItems:e,globalScreens:f,workspaces:g,showWorkspacesSection:h}=eNb();b=[...e.filter(q=>q!=="Account"),...f,...(c?[]:["Shortcuts"]),...(b.hideProvideFeedback?[]:["Provide Feedback"])];var k=y0a(!0);c=k.path.length;var l=c===0,m=k.path[0]??k.exiting[0],n=m?.startsWith("workspace:");return A.createElement("div",{ref:k.setHost,className:"relative h-full w-full overflow-hidden"},A.createElement(KL,{offset:-c,active:l,depth:0,footer:e.includes("Account")?A.createElement("div",{className:"flex flex-col border-t border-border p-1"},A.createElement(ZJb,{showChevron:!0,className:"mx-1 px-3 py-2.5",onClick:()=>{a(qX("Account"));k.push("Account")}})):void 0},A.createElement(hNb,{text:"Settings"}),b.map(q=>A.createElement(gNb,{key:q,label:q,onClick:()=>{a(qX(q));k.push(q)}})),h&&A.createElement(A.Fragment,null,A.createElement(hNb,{text:"Workspaces"}),g.map(q=>A.createElement(gNb,{key:q.uri,label:q.name,onClick:()=>{var r=q.uri;a(rX({workspaceUri:r}));k.push(` + "`workspace:${r}`" + `)}})))),m&&A.createElement(KL,{offset:1-c,active:c===1,depth:1,exiting:k.exiting.includes(m),onExited:()=>{k.onExited(m)},header:A.createElement(iNb,{onBack:k.pop})},A.createElement(ECb.Provider,{value:!0},n?A.createElement(gMb,null):fNb(m))))};function kNb(){return["Settings"]};var mNb=()=>{var a=Lj(),b=!!a.get("settings")?.hideProjects;a=!!a.get("settings")?.hideShortcuts;var c=Hm(tX),e=Hm(uX),{settingsFeature:f,projectManagementFeature:g}=(0,A.useContext)(xk),{sharedNavItems:h,globalScreens:k,workspaces:l,showWorkspacesSection:m}=eNb(),n=cS(),q=Pj(g?.projectsStateProvider);return null};`,
	"mobile-user-message-actions":               `className:"absolute bottom-0.5 right-0.5 flex flex-row items-center p-1 rounded-full opacity-0 pointer-events-none group-hover/user-input-step:opacity-100 group-hover/user-input-step:pointer-events-auto transition-all bg-card user-input-buttons-shadow user-input-buttons-container"`,
	"mobile-conversation-row-actions":           `className:Pm("absolute top-0 bottom-0 -right-1 pl-6 flex items-center justify-end gap-0.5 z-10",w?"hidden":ua?"opacity-100":"opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 focus-within:opacity-100")`,
	"mobile-titlebar-delete-hook":               `var {handleArchive:v,handleRestore:w,handlePin:y,handleUnpin:z,isArchiveSupported:A,handleShare:B,showShareModal:C,shareUrl:D,handleCloseShareModal:E,onShare:F}=PX(r??"")`,
	"mobile-titlebar-delete-menu":               `r&&(L.push({iconName:"edit",tooltip:"Rename",onClick:J})`,
	"mobile-delete-modal-export":                `var hlb=({isOpen:a,onClose:b,onDelete:c,showLoadingSpinner:d})=>`,
	"mobile-titlebar-delete-modal":              `c=Zkb({cascadeId:r,paneId:c,includeRemoveFromSplit:!1});return g.length>0||c.length>0||d.length>0||a.length>0?G.createElement(G.Fragment,null,`,
	"mobile-kebab-menu-pin-archive":             `const dlb=({cascadeId:a,onDeleteClick:b,onRenameClick:c,onMarkAsReadClick:d,isUnread:f,onViewDebugClick:g})=>G.createElement(HL,{side:"bottom",align:"start",className:"min-w-[180px]",finalFocus:!1},G.createElement(IL,{onClick:c,"data-testid":"conversation-rename-menu-item"},G.createElement(U,{name:"edit",size:16,className:"text-secondary-foreground shrink-0"}),G.createElement("span",null,"Rename")),`,
	"mobile-kebab-wrapper-pin-archive":          `var elb=G.memo(function({cascadeId:a,onDeleteClick:b,onRenameClick:c,onMarkAsReadClick:d,isUnread:f,onOpenChange:g,onViewDebugClick:h}){return G.createElement(FL,{onOpenChange:g},G.createElement(GL,{asChild:!0},G.createElement(Ky,{variant:"ghost",size:"icon","aria-label":"More options","data-testid":"conversation-kebab",onClick:k=>void k.stopPropagation()},G.createElement(U,{name:"more_vert",size:16}))),G.createElement(dlb,{cascadeId:a,onDeleteClick:b,onRenameClick:c,onMarkAsReadClick:d,isUnread:f,` + "\n" + `onViewDebugClick:h}))});`,
	"mobile-kebab-call-pin-archive":             `G.createElement(elb,` + "\n" + `{cascadeId:a,onDeleteClick:()=>{sa(!0)},onRenameClick:hb,onMarkAsReadClick:vb?ja:pa,isUnread:vb,onOpenChange:Ca})`,
	"mobile-hide-aux-sidebar":                   `G.createElement(bZ,{iconName:"dock_to_bottom",onClick:m,"aria-label":"Toggle Auxiliary Pane",dataTestId:"mobile-toggle-aux-sidebar"})`,
	"settings-rules-editor":                     `var WS=({name:a,path:b,onCopyPath:c,description:d,badge:f,disabled:g=!1,isLast:h=!1,onEdit:k,editTitle:l="Edit",onDelete:m,deleteTitle:n="Delete",onToggle:p,toggleChecked:r,toggleDisabled:t=!1,expandableContent:v})=>{var w=k||m||p,[y,z]=(0,G.useState)(!1),`,
	"settings-customizations-show-edit":         `"Copy path")),!A&&n&&z.createElement(z.Fragment,null,Yz.createElement(Yz,{variant:"ghost",size:"icon-sm",onClick:n,"aria-label":`,
	"settings-doc-row-editor":                   `function V2({title:a,displayPath:b,copyPath:c,description:d,content:f,modalChildren:g,badge:h,actions:k,actionsPaddingClass:l,disabled:m=!1,MarkdownRenderer:n}){var [q,r]=(0,A.useState)(!1),t=!!d||!!f||!!g,u=d||b;`,
	"settings-restore-customizations-tab":       `h=k}else h=g;g=l=>{if(!b)return l;l=new Map(l);l.delete("Customizations");` + "\n" + `return l};if(!c.size)return g(h);`,
	"suppress-conversation-unavailable-modal":   `A({tag:"trajectory-not-found",title:"Conversation unavailable",message:"The conversation could not be loaded because its data was not found."})`,
	"force-disable-telemetry":                   `return{telemetryEnabled:f,marketingEmailsEnabled:`,
	"virtualization-disable-contraction":        `contractionSafetyPx:3E3,outerRadiusPx:5E3`,
	"folder-picker-initial-path":                `initialPath:b?b.fsPath:g?"C:/":"/",fetchDirectoryContents:`,
	"composer-upload-menu-item":                 `{icon:ea=>x.createElement(T,{name:"image",size:ea.width?Number(ea.width):14,className:ea.className}),` + "\n" + `label:"Media",onClick:oa}`,
	"file-upload-accept-all":                    `accept:".png,.jpg,.jpeg,.gif,image/png,image/jpeg,image/gif,video/webm,.mp4,video/mp4,.pdf,application/pdf,.txt,text/plain,.csv,text/csv,.json,application/json,.md,text/markdown,.py,text/x-python,.js,.mjs,text/javascript,.ts,.tsx,text/x-typescript,.html,.htm,text/html,.css,text/css",multiple:!0`,
	"file-drop-accept-all":                      `function TC(a,b=""){if(b.startsWith("image/")||b.startsWith("video/")||b.startsWith("audio/")||b==="application/pdf"||KOa(b,a)||AC(b,a))return!0;a=a.slice(a.lastIndexOf(".")+1).toLowerCase();return hPa.has(a)}`,
	"file-upload-input-reset":                   `var IRa=({onFilesSelected:a})=>{var b=(0,x.useRef)(null),c=(0,x.useCallback)(e=>{e=e.target;e.files&&a(e.files)},[a]);return{openFileDialog:(0,x.useCallback)(()=>{b.current?.click()},[]),fileInputRef:b,handleFileChange:c}};`,
	"file-upload-custom-text-types":             `function WEa(a,b){b=b.split(";")[0].trim().toLowerCase();if(UEa.includes(b))return b;a=a.slice(a.lastIndexOf(".")+1).toLowerCase();return VEa[a]}function XEa(a){return WEa("",a)!==void 0}`,
	"file-upload-large-file-streaming-fallback": `if(n)if(k.size>1048576)console.error("Text file size exceeds 1MB limit");`,
	"question-modal-write-in-radio":             `value:"__write_in__",checked:e,onChange:()=>{var D=!e;m(D);D&&(a.isMultiSelect||k())}`,
	"question-modal-write-in-focus":             `onClick:()=>{e||(m(!0),a.isMultiSelect||k())},onChange:D=>{l(D.target.value)}`,
	"question-modal-prevent-radio-focus-steal":  `if(!a.isMultiSelect&&!e&&b.length>0){let D=A.current.get(b[0]);D&&D.focus()}`,
	"autoscroll-distance-fix":                   `return E?(B.current?B.current(E):E.scrollHeight-E.clientHeight-E.scrollTop)<=a:!1`,
	"websocket-transport-default":               `function Khc(){var a=new URLSearchParams(window.location.search),b=a.get("useWebSocket");return b!==null?b==="true":a.get("wsTransport")==="2"}`,
	"websocket-liveness-probe-relax": `function QJ(a,b){var c=a.inboundFrameSeq;a.probeTimer=setTimeout(()=>{if(b.readyState!==WebSocket.OPEN)OJ(a,b);else if(lZa(a,b))if(a.inboundFrameSeq!==c)QJ(a,b);else{b.send(JSON.stringify({streamId:Ke(),type:"ping"}));var e=a.inboundFrameSeq;a.probeTimer=setTimeout(()=>{b.readyState!==WebSocket.OPEN?OJ(a,b):a.inboundFrameSeq!==e?QJ(a,b):(PJ(a),a.options.recordError?.("websocket_liveness_probe_failed",Error("no frames for 4000ms and ping unanswered; closing socket")),b.close(),OJ(a,b))},2E3)}else PJ(a)},
2E3)}`,
	"bootstrap-local-user-info-retry": `async function Fzc(a){var b=d,c=d;try{let e=c(await b(a.lsClient.getLocalUserInfo(p(Fga,{})))),f=e.username;return{homeDirUri:Oe(e.homeDirUri),username:f}}finally{b()}};`,
}

func fullOptions() Options {
	return Options{MobileUX: true, WorkspaceRoot: "/home/ubuntu/workspace", CacheKey: "testkey"}
}

func TestEachAnchorAppearsOnceInSyntheticBundle(t *testing.T) {
	body := syntheticBundle()

	for _, p := range All() {
		if p.Target != MainJS {
			continue
		}

		switch p.Kind {
		case Literal:
			if n := bytes.Count(body, []byte(p.Find)); n != 1 {
				t.Errorf("%s: expected exactly 1 anchor match, got %d", p.ID, n)
			}
		case Regexp:
			if n := len(p.FindRe.FindAll(body, -1)); n != 1 {
				t.Errorf("%s: expected exactly 1 regexp match, got %d", p.ID, n)
			}
		}
	}
}

func TestAllMainJSPatchesApply(t *testing.T) {
	out, report := Apply(MainJS, syntheticBundle(), fullOptions())

	for _, r := range report.Missing() {
		t.Errorf("%s: %s", r.ID, r.Status)
	}

	for _, p := range All() {
		if p.Target != MainJS || p.Kind != Literal {
			continue
		}
		if bytes.Contains(out, []byte(p.Find)) {
			t.Errorf("%s: anchor still present after patching", p.ID)
		}
	}
}

func TestPatchedContentIsCorrect(t *testing.T) {
	out, _ := Apply(MainJS, syntheticBundle(), fullOptions())
	body := string(out)

	want := []string{
		`window.location.origin`,
		`function wmb(){return null};function wmbDisabled(`,
		`initialPath:"/home/ubuntu/workspace",fetchDirectoryContents:`,
		`Upload File`,
		`accept:"*/*",multiple:!0`,
		`function TC(a,b=""){return!0}`,
		`if(t.files&&t.files.length>0)a(t.files);t.value=""`,
		`a==="md"||a==="markdown"?"text/markdown"`,
		`window.__agyUpload([k])`,
		`var e=(window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches))||!!this.storageService`,
		`isMobileNew=Boolean((window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches))&&sec)`,
		`!a.location.search?.section`,
		`onClick:()=>c({clearSection:!0})`,
		`if(true){let za=`,
		`className:"absolute right-1 top-0 flex h-full items-center gap-1 opacity-100"`,
		`,z=(0,y.useContext)(DEb),A=!1,`,
		`sidebar-toggle`,
		`if(J!=="custom"||N.length===0)return kZ(ha,D,J,U,ea).map(th);`,
		`window.__agyProjectSettings=RNb;function RNb(){`,
		`settingsScreen/navigateToProjectScreen`,
		`m?.startsWith("project:")&&window.__agyProjectSettings?A.createElement(window.__agyProjectSettings,null):`,
		`className:"relative self-end ml-auto mt-1 flex flex-row items-center p-1 rounded-full opacity-90 pointer-events-auto transition-all bg-transparent user-input-buttons-container select-none"`,
		`className:Pm("absolute top-0 bottom-0 -right-1 pl-6 flex items-center justify-end gap-0.5 z-10",(w||ua||Boolean(window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches)))?"opacity-100":"opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 focus-within:opacity-100")`,
		`handleDelete:agyDel,showDeleteModal:agyShowDel`,
		`L.push({iconName:"delete",tooltip:"Delete",onClick:()=>{typeof agySetShowDel==="function"&&agySetShowDel(!0)}})`,
		`window.__agyDeleteModal=({isOpen:a,onClose:b,onDelete:c,showLoadingSpinner:d})=>`,
		`window.__agyDeleteModal&&typeof agyShowDel!=="undefined"?`,
		`Save`,
		`/__agy/api/rules/save`,
		`"Copy path")),n&&z.createElement`,
		`g=l=>l;if(!c.size)return g(h);`,
		`contractionSafetyPx:8E3,outerRadiusPx:1.5E4`,
		`value:"__write_in__",checked:e,onChange:()=>{var D=(a.isMultiSelect?!e:!0);m(D);D&&(a.isMultiSelect||k())}`,
		`onClick:()=>{e||(m(!0),a.isMultiSelect||k())},onFocus:()=>{e||(m(!0),a.isMultiSelect||k())},onChange:`,
		`if(!a.isMultiSelect&&!e&&b.length>0&&document.activeElement?.getAttribute?.("data-testid")!=="ask-question-writein"&&!(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches)){let D=A.current.get(b[0]);D&&D.focus()}`,
		`return E?(E.scrollHeight-E.clientHeight-E.scrollTop)<=a:!1`,
		`return b!==null?b==="true":a.get("wsTransport")!=="1"}`,
		`OJ(a,b))},3E4)}else PJ(a)},
2E3)}`,
		`for(let _i=0;_i<3;_i++)try{e=c(await b(a.lsClient.getLocalUserInfo(p(Fga,{}))));break}`,
		`_i=k.isComposing||k.keyCode===229||(window.__agyLastCompEnd&&performance.now()-window.__agyLastCompEnd<80)`,
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("missing expected replacement: %s", w)
		}
	}

	if !strings.Contains(body, `onClick:()=>{window.location.href="/__agy/signin"}`) {
		t.Error("sign-in button was not redirected")
	}

	if !strings.Contains(body, `async loginWithRedirect(a){window.location.href="/__agy/signin";return;`) {
		t.Error("authService class loginWithRedirect was not redirected")
	}

	if !strings.Contains(body, `async showLoginFlow(){window.location.href="/__agy/signin";return;`) {
		t.Error("authService class showLoginFlow was not redirected")
	}

	if !strings.Contains(body, `loginWithRedirect:async()=>{window.location.href="/__agy/signin"},showLoginFlow:async()=>{window.location.href="/__agy/signin"}`) {
		t.Error("authService stub login was not redirected")
	}
}

func TestMobileUXDisabledSkipsMobilePatches(t *testing.T) {
	_, report := Apply(MainJS, []byte(""), Options{MobileUX: false})

	byID := map[string]Result{}
	for _, r := range report {
		byID[r.ID] = r
	}

	mobileIDs := []string{
		"mobile-enter-newline",
		"mobile-skip-notification-prompt",
		"mobile-new-convo-view",
		"mobile-new-convo-header",
		"mobile-back-clears-section",
		"mobile-project-header-actions",
		"mobile-project-kebab-menu",
		"mobile-project-add-click-close-sidebar",
		"mobile-project-sort-order",
		"mobile-project-settings-export",
		"mobile-settings-projects",
		"mobile-user-message-actions",
		"mobile-conversation-row-actions",
		"mobile-titlebar-delete-hook",
		"mobile-titlebar-delete-menu",
		"mobile-delete-modal-export",
		"mobile-titlebar-delete-modal",
	}
	for _, id := range mobileIDs {
		if got := byID[id].Status; got != StatusDisabled {
			t.Errorf("%s: want disabled when MobileUX=false, got %s", id, got)
		}
	}
}

func TestMobileKebabPatchesDisabled(t *testing.T) {
	_, report := Apply(MainJS, []byte(""), fullOptions())

	byID := map[string]Result{}
	for _, r := range report {
		byID[r.ID] = r
	}

	for _, id := range []string{
		"mobile-kebab-menu-pin-archive",
		"mobile-kebab-wrapper-pin-archive",
		"mobile-kebab-call-pin-archive",
	} {
		if got := byID[id].Status; got != StatusDisabled {
			t.Errorf("%s: want disabled, got %s", id, got)
		}
	}
}

func TestDisabledPatches(t *testing.T) {
	_, report := Apply(MainJS, []byte(""), fullOptions())
	byID := map[string]Result{}
	for _, r := range report {
		byID[r.ID] = r
	}

	for _, id := range []string{
		"model-effort-submenu",
		"hide-mic-button",
		"mobile-hide-aux-sidebar",
	} {
		if got := byID[id].Status; got != StatusDisabled {
			t.Errorf("%s: want disabled, got %s", id, got)
		}
	}
}

func TestMissingAnchorIsReported(t *testing.T) {
	_, report := Apply(MainJS, []byte("nothing to see here"), fullOptions())

	if len(report.Missing()) == 0 {
		t.Fatal("expected missing anchors to be reported")
	}
	if len(report.MissingRequired()) != 1 {
		t.Errorf("want 1 missing required patch, got %d", len(report.MissingRequired()))
	}
}

func TestHTMLInjection(t *testing.T) {
	html := []byte(`<!doctype html><html><head><title>x</title>` +
		`<meta name="viewport" content="width=device-width, initial-scale=1.0, viewport-fit=cover, maximum-scale=1.0" />` +
		`</head><body><script src="/main.js"></script></body></html>`)
	out, report := Apply(HTML, html, fullOptions())
	body := string(out)

	for _, r := range report.Missing() {
		t.Errorf("unexpected missing HTML patch %s", r.ID)
	}

	want := []string{
		`id="agy-connection-prewarm"`,
		`init.mode === "no-cors" && init.credentials === "include"`,
		`window.WebSocket = WS`,
		`id="agy-touch-action"`,
		`id="agy-safe-area"`,
		`id="agy-keyboard-detect"`,
		`var DIALOG_CONTAINER_SELECTOR = '[role="dialog"], [role="menu"]`,
		`var returnedFromDialog = (performance.now() - lastDialogBlurAt < 500)`,
		`isPortrait() && !returnedFromDialog`,
		`matchMedia("(pointer:coarse)")`,
		`id="agy-signin-banner"`,
		`/__agy/api/signin/status`,
		`href="/apple-touch-icon.png"`,
		`src="/main.js?agy=testkey"`,
		`div[data-testid="user-input-step"] div.bg-card:has(.user-input-buttons-container)`,
		`div[data-testid="user-input-step"] div.bg-card:has([data-testid="queued-decorators"])`,
		`div[data-testid="conversation-view"]`,
		`max-height: 100% !important;`,
		`overflow-y: hidden !important;`,
		`overscroll-behavior-y: contain !important;`,
		`--agy-top`,
		`agy-kb-p`,
		`function chatScroller()`,
		`function checkNearBottom()`,
		`function scrollChatToBottom()`,
		`var wasNearBottom = true;`,
		`[data-testid="agent-input-box"]`,
		`transition: bottom 0.28s cubic-bezier(0.16, 1, 0.3, 1)`,
		`transition: padding-bottom 0.28s cubic-bezier(0.16, 1, 0.3, 1)`,
		`{ capture: true, passive: true }`,
		`@media (pointer: coarse) and (max-width: 768px)`,
		`@media (pointer: coarse) and (min-width: 769px)`,
		`target < 100`,
		`function isMobileDevice()`,
		`function isTextInput(el)`,
		`div.fixed.inset-0:has(> .aux-drawer-popup)`,
		`var suppressComposerUntil = 0;`,
		`[data-testid="agent-input-box"]`,
		`[data-testid="ask-question-header-text"]`,
		`[data-testid="interaction-continue-button"]`,
		`calc(100vh - var(--agy-bottom, 0px) - 160px)`,
		`html.agy-has-question`,
		`body.agy-has-question`,
		`var hasActiveQuestion = false;`,
		`function updateQuestionState()`,
		`function finishQuestion()`,
		`QUESTION_SELECTOR`,
		`div.group\/pane > div.select-none:first-child div[aria-hidden="true"].shrink-0`,
		`div.group\/pane > div.select-none:first-child > div.flex.items-center.gap-1.min-w-0`,
		`padding-left: 0.75rem !important;`,
		`root.querySelector('[data-testid="autoscroll-viewport"]')`,
		`body.agy-has-question div[data-testid="conversation-view"]`,
		`window.__agyLastCompEnd = performance.now();`,
		`window.dispatchEvent(new MouseEvent('mouseup'));`,
		`agy-line-start-nav`,
		`overflow-anchor: auto !important;`,
		`handleScrollerMutation()`,
		`agy-connection-watchdog`,
		`checkAndDismissLostConnectionBanner`,
		`checkConversationSpinnerStuck`,
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("missing %s in output", w)
		}
	}

	if strings.Contains(body, "min-height: 120px") {
		t.Error("min-height: 120px was found in injected styles; this breaks the empty composer height")
	}

	if strings.Contains(body, "cv.scrollTop = cv.scrollHeight") {
		t.Error("cv.scrollTop = cv.scrollHeight was found in injected scripts; this violently scrolls the parent conversation-view")
	}

	if strings.Index(body, "agy-touch-action") > strings.Index(body, "</head>") {
		t.Error("injected styles must land inside <head>")
	}
}

func TestHTMLWithoutHeadStillInjects(t *testing.T) {
	out, report := Apply(HTML, []byte(`<div id="root"></div>`), fullOptions())
	for _, r := range report.Missing() {
		if r.ID != "cache-bust" {
			t.Errorf("unexpected missing patch %s", r.ID)
		}
	}
	if !strings.Contains(string(out), "agy-touch-action") {
		t.Error("expected injection to fall back to prepending")
	}
}

func TestCacheKeyChangesWithPatchSet(t *testing.T) {
	a := CacheKey("1.0.0", Options{MobileUX: true})
	b := CacheKey("1.0.0", Options{MobileUX: false})
	c := CacheKey("1.0.1", Options{MobileUX: true})

	if a == b {
		t.Error("cache key must change when the enabled patch set changes")
	}
	if a == c {
		t.Error("cache key must change when the version changes")
	}
	if a != CacheKey("1.0.0", Options{MobileUX: true}) {
		t.Error("cache key must be stable for identical inputs")
	}
}

func TestEveryPatchIsWellFormed(t *testing.T) {
	seen := map[string]bool{}

	for _, p := range All() {
		if p.ID == "" {
			t.Error("every patch needs an ID")
			continue
		}
		if seen[p.ID] {
			t.Errorf("%s: duplicate patch ID", p.ID)
		}
		seen[p.ID] = true

		if p.Desc == "" {
			t.Errorf("%s: needs a user-facing Desc", p.ID)
		}

		switch p.Kind {
		case Literal:
			if p.Find == "" {
				t.Errorf("%s: literal patch needs Find", p.ID)
			}
		case Regexp:
			if p.FindRe == nil {
				t.Errorf("%s: regexp patch needs FindRe", p.ID)
			}
			if regexpFixtures[p.ID] == "" {
				t.Errorf("%s: regexp patch needs an entry in regexpFixtures", p.ID)
			}
		case InjectHead:
			if p.Replace == "" && p.ReplaceFn == nil {
				t.Errorf("%s: injection needs content", p.ID)
			}
		}
	}
}

func TestAdaptiveCrossVersionCompatibility(t *testing.T) {
	v210Fixtures := map[string]string{
		"mobile-conversation-row-actions":          `className:Pm("absolute top-0 bottom-0 -right-1 pl-6 flex items-center justify-end gap-0.5 z-10",w?"hidden":ua?"opacity-100":"opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 focus-within:opacity-100")`,
		"mobile-user-message-actions":              `className:"absolute bottom-0.5 right-0.5 flex flex-row items-center p-1 rounded-full opacity-0 pointer-events-none group-hover/user-input-step:opacity-100 group-hover/user-input-step:pointer-events-auto transition-all bg-card user-input-buttons-shadow user-input-buttons-container"`,
		"mobile-kebab-menu-pin-archive":            `const dlb=({cascadeId:a,onDeleteClick:b,onRenameClick:c,onMarkAsReadClick:d,isUnread:f,onViewDebugClick:g})=>G.createElement(HL,{side:"bottom",align:"start",className:"min-w-[180px]",finalFocus:!1},G.createElement(IL,{onClick:c,"data-testid":"conversation-rename-menu-item"},G.createElement(U,{name:"edit",size:16,className:"text-secondary-foreground shrink-0"}),G.createElement("span",null,"Rename")),`,
		"mobile-kebab-wrapper-pin-archive":         `var elb=({cascadeId:a,onDeleteClick:b,onRenameClick:c,onMarkAsReadClick:d,isUnread:f,onOpenChange:g,onViewDebugClick:h})=>G.createElement(FL,{onOpenChange:g},G.createElement(GL,{asChild:!0},G.createElement(Ky,{variant:"ghost",size:"icon","aria-label":"More options","data-testid":"conversation-kebab",onClick:k=>void k.stopPropagation()},G.createElement(U,{name:"more_vert",size:16}))),G.createElement(dlb,{cascadeId:a,onDeleteClick:b,onRenameClick:c,onMarkAsReadClick:d,isUnread:f,onViewDebugClick:h}));`,
		"mobile-kebab-call-pin-archive":            `G.createElement(elb,{cascadeId:a,onDeleteClick:()=>{sa(!0)},onRenameClick:hb,onMarkAsReadClick:vb?ja:pa,isUnread:vb,onOpenChange:Ca})`,
		"question-modal-write-in-radio":            `value:"__write_in__",checked:e,onChange:()=>{var D=!e;m(D);D&&(a.isMultiSelect||k())}`,
		"question-modal-write-in-focus":            `onClick:()=>{e||(m(!0),a.isMultiSelect||k())},onChange:D=>{l(D.target.value)}`,
		"question-modal-prevent-radio-focus-steal": `if(!a.isMultiSelect&&!e&&b.length>0){let D=A.current.get(b[0]);D&&D.focus()}`,
	}

	v211Fixtures := map[string]string{
		"mobile-conversation-row-actions":          `className:$l("absolute top-0 bottom-0 -right-1 pl-6 flex items-center justify-end gap-0.5 z-10",v?"hidden":Ka?"opacity-100":"opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 focus-within:opacity-100")`,
		"mobile-user-message-actions":              `className:"absolute bottom-0.5 right-0.5 flex flex-row items-center p-1 rounded-full opacity-0 pointer-events-none group-hover/user-input-step:opacity-100 group-hover/user-input-step:pointer-events-auto transition-all bg-card user-input-buttons-shadow user-input-buttons-container"`,
		"mobile-kebab-menu-pin-archive":            `const dlb=({cascadeId:a,onDeleteClick:b,onRenameClick:c,onMarkAsReadClick:d,isUnread:f,onViewDebugClick:g})=>G.createElement(HL,{side:"bottom",align:"start",className:"min-w-[180px]",finalFocus:!1},G.createElement(IL,{onClick:c,"data-testid":"conversation-rename-menu-item"},G.createElement(U,{name:"edit",size:16,className:"text-secondary-foreground shrink-0"}),G.createElement("span",null,"Rename")),`,
		"mobile-kebab-wrapper-pin-archive":         `var ynb=y.memo(function({cascadeId:a,onDeleteClick:b,onRenameClick:c,onMarkAsReadClick:e,isUnread:f,onOpenChange:g,onViewDebugClick:h}){return y.createElement(PK,{onOpenChange:g},y.createElement(QK,{asChild:!0},y.createElement(CA,{variant:"ghost",size:"icon","aria-label":"More options","data-testid":"conversation-kebab",onClick:k=>void k.stopPropagation()},y.createElement(T,{name:"more_vert",size:16}))),y.createElement(xnb,{cascadeId:a,onDeleteClick:b,onRenameClick:c,onMarkAsReadClick:e,isUnread:f,` + "\n" + `onViewDebugClick:h}))});`,
		"mobile-kebab-call-pin-archive":            `y.createElement(ynb,` + "\n" + `{cascadeId:a,onDeleteClick:Ia,onRenameClick:va,onMarkAsReadClick:tb?ra:ma,isUnread:tb,onOpenChange:Ja})`,
		"question-modal-write-in-radio":            `value:"__write_in__",checked:e,onChange:()=>{var D=!e;m(D);D&&(a.isMultiSelect||k())}`,
		"question-modal-write-in-focus":            `onClick:()=>{e||(m(!0),a.isMultiSelect||k())},onChange:D=>{l(D.target.value)}`,
		"question-modal-prevent-radio-focus-steal": `if(!a.isMultiSelect&&!e&&b.length>0){let D=A.current.get(b[0]);D&&D.focus()}`,
	}

	v212Fixtures := map[string]string{
		"mobile-user-message-actions":              `className:"absolute bottom-0.5 right-0.5 flex flex-row items-center p-1 rounded-full opacity-0 pointer-events-none group-hover/user-input-step:opacity-100 group-hover/user-input-step:pointer-events-auto transition-all bg-card user-input-buttons-shadow user-input-buttons-container select-none"`,
		"question-modal-write-in-radio":            `value:"__write_in__",checked:e,onChange:()=>{var D=!e;m(D);D&&(a.isMultiSelect||k())}`,
		"question-modal-write-in-focus":            `onClick:()=>{e||(m(!0),a.isMultiSelect||k())},onChange:D=>{l(D.target.value)}`,
		"question-modal-prevent-radio-focus-steal": `if(!a.isMultiSelect&&!e&&b.length>0){let D=A.current.get(b[0]);D&&D.focus()}`,
	}

	v213Fixtures := map[string]string{
		"mobile-titlebar-delete-hook":              `var {handleArchive:a,handleRestore:b,handlePin:c,handleUnpin:e,isArchiveSupported:f,handleShare:g,showShareModal:h,shareUrl:k,handleCloseShareModal:l,onShare:m}=n(p??"")`,
		"mobile-titlebar-delete-modal":             `c=wAb({cascadeId:a,paneId:b,includeRemoveFromSplit:!1});return f.length>0||g.length>0||h.length>0||k.length>0?z.createElement(z.Fragment,null,`,
		"question-modal-prevent-radio-focus-steal": `if(document.hasFocus()&&!a.isMultiSelect&&!e&&b.length>0){let D=A.current.get(b[0]);D&&D.focus()}`,
		"autoscroll-distance-fix":                  `return E?(B.current?B.current(E):E.scrollHeight-E.clientHeight-E.scrollTop)<=a:!1`,
		"mobile-new-convo-view":                    `const KLb=()=>{var a=UL(),b=bU();return(0,z.useCallback)((c,e)=>{b(aU.map(f=>({trigger:f,ran:!1})));a(c,{section:e})},[a,b])};` + "\n" + `var LLb=()=>{var a=KLb(),{q:b}=xL({strict:!1});F1("MOBILE_HOME_VIEW");return z.createElement("div",{className:"w-full h-full flex flex-col min-h-0 animate-fade-in"},z.createElement("div",{className:"flex-1 min-h-0 overflow-y-auto flex flex-col gap-6 pt-3"},z.createElement(JLb,{surface:"background"})),`,
	}

	v214Fixtures := map[string]string{
		"mobile-titlebar-delete-hook":              `var {handleArchive:u,handleRestore:v,handlePin:w,handleUnpin:x,isArchiveSupported:A,handleShare:B,showShareModal:C,` + "\n" + `shareUrl:D,handleCloseShareModal:E,onShare:F}=fW(t??"")`,
		"mobile-titlebar-delete-modal":             `c=isb({cascadeId:t,paneId:c,includeRemoveFromSplit:!1});return h.length>0||c.length>0||g.length>0||b.length>0||f?z.createElement(z.Fragment,null,`,
		"question-modal-prevent-radio-focus-steal": `if(document.hasFocus()&&!a.isMultiSelect&&!e&&b.length>0){let D=A.current.get(b[0]);D&&D.focus()}`,
		"autoscroll-distance-fix":                  `return E?(B.current?B.current(E):E.scrollHeight-E.clientHeight-E.scrollTop)<=a:!1`,
	}

	versions := map[string]map[string]string{
		"2.10.0": v210Fixtures,
		"2.11.0": v211Fixtures,
		"2.12.0": v212Fixtures,
		"2.13.0": v213Fixtures,
		"2.14.0": v214Fixtures,
	}

	patches := All()
	patchMap := make(map[string]Patch)
	for _, p := range patches {
		patchMap[p.ID] = p
	}

	for ver, fixs := range versions {
		for id, snippet := range fixs {
			p, ok := patchMap[id]
			if !ok {
				t.Fatalf("[%s] patch %s not found in All()", ver, id)
			}
			if p.FindRe == nil {
				t.Fatalf("[%s] patch %s is not regexp", ver, id)
			}
			if !p.FindRe.MatchString(snippet) {
				t.Errorf("[%s] patch %s failed to match snippet: %s", ver, id, snippet)
			}
		}
	}
}

func TestAllGoFilesAreGofmtFormatted(t *testing.T) {
	cmd := exec.Command("gofmt", "-l", "../..")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to run gofmt: %v", err)
	}
	unformatted := strings.TrimSpace(string(out))
	if unformatted != "" {
		t.Errorf("The following Go files are not formatted with gofmt:\n%s", unformatted)
	}
}

func TestTextInputFilterLogic(t *testing.T) {
	out, _ := Apply(HTML, []byte("<head></head><body></body>"), fullOptions())
	body := string(out)

	patternRe := regexp.MustCompile(`/(\^\([^/]+\)\$)/`)
	matches := patternRe.FindStringSubmatch(body)
	if len(matches) < 2 {
		t.Fatalf("failed to find text input filter pattern in injected script")
	}

	re, err := regexp.Compile(matches[1])
	if err != nil {
		t.Fatalf("invalid regex pattern %q extracted from script: %v", matches[1], err)
	}

	excluded := []string{"radio", "checkbox", "button", "submit", "reset", "file", "range", "color", "hidden", "image"}
	for _, typ := range excluded {
		if !re.MatchString(typ) {
			t.Errorf("expected %q to be excluded from text input list", typ)
		}
	}

	allowed := []string{"text", "search", "email", "password", "number", "tel", "url"}
	for _, typ := range allowed {
		if re.MatchString(typ) {
			t.Errorf("expected %q to be allowed as text input", typ)
		}
	}
}

func TestConnectionWatchdogScriptIntegrity(t *testing.T) {
	out, _ := Apply(HTML, []byte("<head></head><body></body>"), fullOptions())
	body := string(out)

	requiredGuards := []string{
		"activePingPromise",
		"MAX_RELOAD_ATTEMPTS = 2",
		`sessionStorage.getItem("agy_stuck_reload_count")`,
		`sessionStorage.removeItem("agy_stuck_reload_count")`,
		`sessionStorage.removeItem("agy_stuck_reload")`,
		`window.location.href = "/"`,
		`isConvoRoute`,
		`window.location.pathname.startsWith("/c/")`,
		`document.querySelector('[contenteditable="true"]')`,
		`data.available === false`,
		`now - lastPingSuccess < 3000`,
		`now - lastNetworkActivity < 5000`,
		`now - stuckTimerStart > 30000`,
		`now - lastReload > 30000`,
		`window.__agyFetchActivityTracked`,
		`res.body.getReader`,
		`window.__agyWsActivityTracked`,
		`new Proxy(window.WebSocket, {`,
		`Reflect.construct(target, args, newTarget)`,
		`d.length < 512 && wsControlFrameRe.test(d)`,
		`requestAnimationFrame`,
		`e.isComposing || e.keyCode === 229`,
		`b.style.removeProperty("display")`,
		`checkEmptyRootStuck`,
		`emptyRootTimerStart`,
		`agy_empty_root_reload_count`,
		`now - emptyRootTimerStart > 15000`,
	}

	for _, guard := range requiredGuards {
		if !strings.Contains(body, guard) {
			t.Errorf("missing watchdog safety guard %q in injected HTML", guard)
		}
	}

	// Ensure characterData is omitted from observer options to prevent token streaming jank
	watchdogRe := regexp.MustCompile(`watchdogObserver\.observe\([^,]+,\s*\{([^}]+)\}\)`)
	matches := watchdogRe.FindStringSubmatch(body)
	if len(matches) < 2 {
		t.Fatalf("failed to locate watchdogObserver.observe in injected script")
	}
	if strings.Contains(matches[1], "characterData") {
		t.Errorf("watchdogObserver should not observe characterData: %s", matches[1])
	}

	// Verify that window.location.reload() is strictly wrapped inside pingServer callback
	if !strings.Contains(body, "pingServer(function () {") || !strings.Contains(body, "window.location.reload();") {
		t.Errorf("window.location.reload() must be protected inside pingServer callback")
	}
}

func TestTopSentinelGuardAndFetchInterceptor(t *testing.T) {
	out, _ := Apply(HTML, []byte("<head></head><body></body>"), fullOptions())
	body := string(out)

	requiredGuards := []string{
		"handleScrollerMutation",
		"targetTop = lastScrollTop + delta",
		"sc.scrollTop = targetTop",
		"chatScroller",
	}

	for _, guard := range requiredGuards {
		if !strings.Contains(body, guard) {
			t.Errorf("missing top scroll prepend anchoring element %q in injected HTML", guard)
		}
	}
}

func TestAuthServiceLoginRedirectPatch(t *testing.T) {
	classInput := `async loginWithRedirect(a){var b=d,c=d;try{`
	if !authServiceLoginRedirectRe.MatchString(classInput) {
		t.Fatalf("authServiceLoginRedirectRe failed to match class method")
	}
	classReplaced := authServiceLoginRedirectRe.ReplaceAllString(classInput, `async loginWithRedirect($1){window.location.href="/__agy/signin";return;`)
	if classReplaced != `async loginWithRedirect(a){window.location.href="/__agy/signin";return;var b=d,c=d;try{` {
		t.Fatalf("unexpected class replacement: %s", classReplaced)
	}

	flowInput := `async showLoginFlow(){this._onDidRequestLoginFlow.fire()}`
	if !authServiceShowLoginFlowRe.MatchString(flowInput) {
		t.Fatalf("authServiceShowLoginFlowRe failed to match showLoginFlow method")
	}
	flowReplaced := authServiceShowLoginFlowRe.ReplaceAllString(flowInput, `async showLoginFlow(){window.location.href="/__agy/signin";return;`)
	if flowReplaced != `async showLoginFlow(){window.location.href="/__agy/signin";return;this._onDidRequestLoginFlow.fire()}` {
		t.Fatalf("unexpected showLoginFlow replacement: %s", flowReplaced)
	}

	stubInput := `const auth={loginWithRedirect:async()=>{console.log("[AuthService] loginWithRedirect")},showLoginFlow:async()=>{console.log("[AuthService] showLoginFlow")}};`
	if !authServiceStubLoginRedirectRe.MatchString(stubInput) {
		t.Fatalf("authServiceStubLoginRedirectRe failed to match stub input")
	}
	stubReplaced := authServiceStubLoginRedirectRe.ReplaceAllString(stubInput, `loginWithRedirect:async()=>{window.location.href="/__agy/signin"},showLoginFlow:async()=>{window.location.href="/__agy/signin"}`)
	expected := `const auth={loginWithRedirect:async()=>{window.location.href="/__agy/signin"},showLoginFlow:async()=>{window.location.href="/__agy/signin"}};`
	if stubReplaced != expected {
		t.Fatalf("unexpected stub replacement output: %s", stubReplaced)
	}
}

func TestSettingsRestoreCustomizationsPatch(t *testing.T) {
	settingsInput := `h.length>0&&k.set(l.screen,{sections:h})):k.set(l.screen,h);h=k}else h=g;g=l=>{if(!b)return l;l=new Map(l);l.delete("Customizations");` + "\n" + `return l};if(!c.size)return g(h);`
	if !settingsRestoreCustomizationsRe.MatchString(settingsInput) {
		t.Fatalf("settingsRestoreCustomizationsRe failed to match settings filter input")
	}
	settingsReplaced := settingsRestoreCustomizationsRe.ReplaceAllString(settingsInput, `${1}=${2}=>${2};`)
	if !strings.Contains(settingsReplaced, `h=k}else h=g;g=l=>l;if(!c.size)return g(h);`) {
		t.Fatalf("unexpected settings restore replacement output: %s", settingsReplaced)
	}
}

func TestUploaderScriptDropBehavior(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}

	script := strings.TrimSuffix(strings.TrimPrefix(uploaderScript, "<script>"), "</script>")
	harness := `
const assert = require("assert");
const listeners = {};
let mouseupCount = 0;
let dragleaveNullCount = 0;
let upstreamDropCount = 0;
let uploadedBatches = [];

global.Event = class Event {
  constructor(type) {
    this.type = type;
    this.defaultPrevented = false;
    this._stopped = false;
    this._immediateStopped = false;
  }
  preventDefault() { this.defaultPrevented = true; }
  stopPropagation() { this._stopped = true; }
  stopImmediatePropagation() { this._stopped = true; this._immediateStopped = true; }
};
global.MouseEvent = class MouseEvent extends global.Event {};
global.DragEvent = class DragEvent extends global.Event {
  constructor(type, init) {
    super(type);
    this.relatedTarget = init && "relatedTarget" in init ? init.relatedTarget : undefined;
  }
};

global.window = {
  location: { search: "", pathname: "/c/test-convo" },
  addEventListener(type, fn, capture) {
    if (!listeners[type]) listeners[type] = [];
    listeners[type].push({ fn, capture: Boolean(capture) });
  },
  dispatchEvent(ev) {
    if (ev.type === "mouseup") mouseupCount++;
    if (ev.type === "dragleave" && ev.relatedTarget === null) dragleaveNullCount++;
    const list = (listeners[ev.type] || []).slice();
    for (const entry of list.filter(x => x.capture)) {
      entry.fn(ev);
      if (ev._immediateStopped) return !ev.defaultPrevented;
    }
    if (ev._stopped) return !ev.defaultPrevented;
    for (const entry of list.filter(x => !x.capture)) {
      entry.fn(ev);
      if (ev._immediateStopped) return !ev.defaultPrevented;
    }
    return !ev.defaultPrevented;
  }
};
global.document = {
  getElementById() { return null; },
  createElement() { return { style: {}, appendChild() {}, addEventListener() {} }; },
  body: { appendChild() {} }
};

` + script + `

// Register stand-in upstream bubble drop listener (like m5a) and substitute __agyUpload
window.addEventListener("drop", () => { upstreamDropCount++; }, false);
window.__agyUpload = (files) => { uploadedBatches.push(Array.from(files)); };

function fireDrop(files, items) {
  const ev = new Event("drop");
  ev.dataTransfer = { files, items: items || [] };
  window.dispatchEvent(ev);
  return ev;
}

// 1. Small image (<= 1MB): only upstream drop runs, custom uploader does not run
uploadedBatches = [];
upstreamDropCount = 0;
fireDrop([{ name: "small.png", type: "image/png", size: 200 * 1024 }]);
assert.strictEqual(uploadedBatches.length, 0, "small image should not trigger custom upload");
assert.strictEqual(upstreamDropCount, 1, "small image should reach upstream drop listener");

// 2. Large image (> 1MB): only upstream drop runs (upstream compresses via canvas), custom uploader does not run
uploadedBatches = [];
upstreamDropCount = 0;
fireDrop([
  { name: "large.png", type: "image/png", size: 5 * 1024 * 1024 },
  { name: "photo.jpg", type: "image/jpeg", size: 3 * 1024 * 1024 }
]);
assert.strictEqual(uploadedBatches.length, 0, "large native images must not trigger duplicate custom upload");
assert.strictEqual(upstreamDropCount, 1, "large native images must reach upstream drop listener");

// 3. Files within 2.28.0 native limits (<= 1MB text, <= 10MB PDF/doc, <= 20MB zip/other): upstream drop runs, custom uploader does not intercept
uploadedBatches = [];
upstreamDropCount = 0;
fireDrop([
  { name: "bundle.zip", type: "application/zip", size: 4 * 1024 * 1024 },
  { name: "notes.txt", type: "text/plain", size: 200 * 1024 },
  { name: "report.pdf", type: "application/pdf", size: 5 * 1024 * 1024 }
]);
assert.strictEqual(uploadedBatches.length, 0, "files within native size limits should reach upstream drop handler");
assert.strictEqual(upstreamDropCount, 1, "files within native size limits should reach upstream drop listener");

// 4. Oversized file exceeding native limit (> 20MB zip or > 1MB text): custom uploader runs, propagation to upstream is stopped, overlays are dismissed
uploadedBatches = [];
upstreamDropCount = 0;
mouseupCount = 0;
dragleaveNullCount = 0;
const archiveFile = { name: "huge-bundle.zip", type: "application/zip", size: 25 * 1024 * 1024 };
const dropEv = fireDrop([archiveFile]);
assert.strictEqual(uploadedBatches.length, 1, "oversized file should trigger custom upload");
assert.strictEqual(uploadedBatches[0].length, 1);
assert.strictEqual(uploadedBatches[0][0].name, "huge-bundle.zip");
assert.strictEqual(upstreamDropCount, 0, "stopPropagation must prevent upstream drop listener from running simultaneously");
assert.strictEqual(dropEv.defaultPrevented, true, "drop event must be defaultPrevented");
assert.ok(mouseupCount >= 1, "mouseup must be dispatched to reset context drop overlay");
assert.ok(dragleaveNullCount >= 1, "dragleave with relatedTarget:null must be dispatched to reset media drop overlay");

// 5. Mixed native image + oversized file drop (including empty MIME .log): streams only the oversized file once, marks it in __agyHandledFiles, and lets upstream drop handler run for the native image
uploadedBatches = [];
upstreamDropCount = 0;
const mixedImage = { name: "photo.png", type: "image/png", size: 500 * 1024 };
const mixedHugeLog = { name: "huge.log", type: "", size: 3 * 1024 * 1024 };
fireDrop([mixedImage, mixedHugeLog]);
assert.strictEqual(uploadedBatches.length, 1, "mixed drop should stream oversized file immediately");
assert.strictEqual(uploadedBatches[0].length, 1, "mixed drop must not include native image in custom upload");
assert.strictEqual(uploadedBatches[0][0].name, "huge.log");
assert.strictEqual(Boolean(window.__agyHandledFiles && window.__agyHandledFiles.has(mixedHugeLog)), true, "oversized file must be tracked in __agyHandledFiles to prevent duplicate icb fallback");
assert.strictEqual(upstreamDropCount, 1, "mixed drop must reach upstream drop listener for the native image");

// 6. Directory drop & directory + oversized file mixed drop: custom uploader must ignore directories and let upstream zip handler run
uploadedBatches = [];
upstreamDropCount = 0;
fireDrop(
  [{ name: "my-folder", type: "", size: 0 }],
  [{ kind: "file", webkitGetAsEntry: () => ({ isDirectory: true, name: "my-folder" }) }]
);
assert.strictEqual(uploadedBatches.length, 0, "directory drop must not trigger custom file upload");
assert.strictEqual(upstreamDropCount, 1, "directory drop must reach upstream folder zip listener");

uploadedBatches = [];
upstreamDropCount = 0;
const folderPlusHugeZip = { name: "archive.zip", type: "application/zip", size: 25 * 1024 * 1024 };
fireDrop(
  [{ name: "my-folder", type: "", size: 0 }, folderPlusHugeZip],
  [
    { kind: "file", webkitGetAsEntry: () => ({ isDirectory: true, name: "my-folder" }) },
    { kind: "file", webkitGetAsEntry: () => ({ isDirectory: false, name: "archive.zip" }) }
  ]
);
assert.strictEqual(uploadedBatches.length, 1, "oversized file dropped alongside directory should stream");
assert.strictEqual(uploadedBatches[0][0].name, "archive.zip");
assert.strictEqual(upstreamDropCount, 1, "directory drop alongside oversized file must still reach upstream folder zip listener");

// 7. Multiple files whose individual sizes are <= 20MB but cumulative non-image size exceeds 20MB: excess files stream automatically
uploadedBatches = [];
upstreamDropCount = 0;
const zipA = { name: "part1.zip", type: "application/zip", size: 12 * 1024 * 1024 };
const zipB = { name: "part2.zip", type: "application/zip", size: 12 * 1024 * 1024 };
fireDrop([zipA, zipB]);
assert.strictEqual(uploadedBatches.length, 1, "cumulative >20MB batch should stream excess file");
assert.strictEqual(uploadedBatches[0].length, 1);
assert.strictEqual(uploadedBatches[0][0].name, "part2.zip");
assert.strictEqual(upstreamDropCount, 1, "first file within 20MB budget should still reach upstream drop listener");

// 8. Extensionless Dockerfile > 1MB is recognized as text and streamed
uploadedBatches = [];
upstreamDropCount = 0;
fireDrop([{ name: "Dockerfile", type: "", size: 2 * 1024 * 1024 }]);
assert.strictEqual(uploadedBatches.length, 1, "Dockerfile > 1MB should stream via custom uploader");
assert.strictEqual(uploadedBatches[0][0].name, "Dockerfile");
assert.strictEqual(upstreamDropCount, 0);

// 9. __agyPartitionFiles respects remaining native slot count (maxSlots)
uploadedBatches = [];
const f1 = { name: "a.txt", type: "text/plain", size: 100 };
const f2 = { name: "b.txt", type: "text/plain", size: 100 };
const f3 = { name: "c.txt", type: "text/plain", size: 100 };
const kept = window.__agyPartitionFiles([f1, f2, f3], 2, 0);
assert.strictEqual(kept.length, 2, "only maxSlots files should be kept for native attachment");
assert.strictEqual(uploadedBatches.length, 1, "overflow file beyond maxSlots should be streamed");
assert.strictEqual(uploadedBatches[0][0].name, "c.txt");

// 10. Multi-batch cumulative 20MB tracking across separate drops/selections
uploadedBatches = [];
upstreamDropCount = 0;
window.__agySyncMediaState(0, 5);
fireDrop([{ name: "batch1.zip", type: "application/zip", size: 14 * 1024 * 1024 }]);
assert.strictEqual(uploadedBatches.length, 0, "first 14MB zip should fit native budget");
assert.strictEqual(upstreamDropCount, 1);
window.__agySyncMediaState(1, 5);
fireDrop([{ name: "batch2.zip", type: "application/zip", size: 10 * 1024 * 1024 }]);
assert.strictEqual(uploadedBatches.length, 1, "second 10MB zip exceeding 20MB cumulative budget should stream");
assert.strictEqual(uploadedBatches[0][0].name, "batch2.zip");
assert.strictEqual(upstreamDropCount, 1, "second drop should be stopped from reaching upstream");

// 11. Fingerprint deduplication prevents duplicate upload even if DataTransfer recreates File wrapper instance
uploadedBatches = [];
window.__agySyncMediaState(0, 5);
const origLog = { name: "trace.log", type: "", size: 2 * 1024 * 1024, lastModified: 1700000000 };
const clonedLog = { name: "trace.log", type: "", size: 2 * 1024 * 1024, lastModified: 1700000000 };
const img = { name: "pic.png", type: "image/png", size: 100 * 1024, lastModified: 1700000000 };
fireDrop([img, origLog]);
assert.strictEqual(uploadedBatches.length, 1, "capture drop streams origLog once");
const partitioned = window.__agyPartitionFiles([img, clonedLog], 5, 0);
assert.strictEqual(uploadedBatches.length, 1, "__agyPartitionFiles must not re-upload cloned File with matching fingerprint");
assert.strictEqual(partitioned.length, 1);
assert.strictEqual(partitioned[0].name, "pic.png");
`

	if out, err := exec.Command(node, "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("uploaderScript drop harness failed: %v\n%s", err, out)
	}
}
