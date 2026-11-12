package patches

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Antigravity 2.31.0 moved, rewrapped or extended what these patches match. Each case is the text the patch matches in
// the 2.31.0 bundle, so a pattern that stops matching it fails here as well as in the live bundle test.
func TestAntigravity231PatchShapes(t *testing.T) {
	// The composer's Enter command first lets an open IME or suggestion handle the key.
	t.Run("mobile-enter-newline", func(t *testing.T) {
		v231 := `registerCommand(XI,l=>{if(!l)return!1;if(rO(l,d,f.current))return!0;var m=l.shiftKey&&(l.metaKey||l.ctrlKey);if(l.shiftKey&&!m)return!1;l.preventDefault();`
		got := applyOnly(t, "mobile-enter-newline", v231, Options{MobileUX: true})
		for _, want := range []string{
			`if(!l)return!1;if(rO(l,d,f.current))return!0;var m=l.shiftKey&&(l.metaKey||l.ctrlKey);if(l.shiftKey&&!m)return!1;var _t=`,
			`if(_i||(_t&&!l.metaKey&&!l.ctrlKey))return!1;l.preventDefault();`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("2.31 shape missing %q in:\n%s", want, got)
			}
		}
	})
	// A line break between the two labels.
	t.Run("mobile-new-conversation-button", func(t *testing.T) {
		v231 := `"New Conversation in Workspace":` + "\n" + `"New Conversation in Project",ec=Pc=>{Pc.stopPropagation();bb(pb)});hb={item:hb,brandedProjectId:pb,projectId:Xb?pb:void 0,onAddClick:ec,newChatLabel:fc,onClick:()=>{$c||kb(pb)},onPointerDown:!V&&h==="project"&&Xb?Pc=>{Pc.button===0&&Pd(pb,Pc)}:void 0,isHovered:Cb(pb),surface:a};return Xb?B.createElement(nZc,{...hb,projectId:pb,archiving:N,isOpen:ha===pb,onOpenChange:Pc=>{qa(Pc?pb:null)}}):B.createElement(lZc,{...hb})}},[h,kb,Pd,$c,Ya,Sa,pa,ma,ha,bb,N,ua,a,V,R,T,J,bd,U,oa,qb]),Eb=ok(hb=>hb.newProject?.isOpen??` + "\n" + `!1);return B.createElement(Ox,{delay:300,timeout:200},B.createElement("div",{ref:Bb.sidebarBoundsRef,className:Ai("w-full relative",!b&&"flex-1 min-h-0 h-full")},Va?B.createElement(nYc,null):B.createElement(NHc,{items:Bb.displayedFlatItems,variant:"sidebar",surface:a,scrollElementRef:b,scrollMargin:c,subtitleContext:aa,onToggleCollapseSection:kb,onToggleSectionHeader:oa,onTogglePagination:vc,customHeaderRenderer:Ka`
		got := applyOnly(t, "mobile-new-conversation-button", v231, Options{MobileUX: true})
		for _, want := range []string{`id:"header-__agy_new__"`, `window.__agyStartNewConversation=()=>{window.__agyFocusComposer&&window.__agyFocusComposer();bb("outside-of-project")}`} {
			if !strings.Contains(got, want) {
				t.Errorf("2.31 shape missing %q in:\n%s", want, got)
			}
		}
	})
	// A line break inside the comparison.
	t.Run("mobile-project-sort-order", func(t *testing.T) {
		v231 := `if(R.length===` + "\n" + `0)return W6(X,L,N,V,ba).map(oi);`
		got := applyOnly(t, "mobile-project-sort-order", v231, Options{MobileUX: true})
		if want := `if(N!=="custom"||R.length===0)return W6(X,L,N,V,ba).map(oi);`; !strings.Contains(got, want) {
			t.Errorf("2.31 shape missing %q in:\n%s", want, got)
		}
	})
	// The titlebar now also takes handleRename from the hook, which has to stay.
	t.Run("mobile-titlebar-delete-hook", func(t *testing.T) {
		v231 := `{handleArchive:x,handleRestore:y,handlePin:z,handleUnpin:A,isArchiveSupported:D,` + "\n" + `handleShare:C,showShareModal:F,shareUrl:G,handleCloseShareModal:H,onShare:J,handleRename:K}=a1(a??"")`
		got := applyOnly(t, "mobile-titlebar-delete-hook", v231, Options{MobileUX: true})
		for _, want := range []string{`handleDelete:agyDel,showDeleteModal:agyShowDel`, `onShare:J,handleRename:K}=a1(a??"")`} {
			if !strings.Contains(got, want) {
				t.Errorf("2.31 shape missing %q in:\n%s", want, got)
			}
		}
	})
	// The hook now stops following on any fall it cannot put down to the content shrinking, and reads its
	// followDisabled ref before turning following back on.
	t.Run("autoscroll-unpin-needs-input", func(t *testing.T) {
		v231 := `var H=m.current;if(H){var J=H.scrollTop,K=Math.max(0,H.scrollHeight-H.clientHeight),M=K-J<=a,L=K<t.current&&J>=K-1,N=J-q.current,P=N>0&&N<=K-t.current,O=p.current;N<0&&!L?(p.current=!1,dU.delete(H)):!M||y.current||`
		got := applyOnly(t, "autoscroll-unpin-needs-input", v231, Options{})
		want := `N<0&&!L?(p.current&&!y.current&&window.__agyIsSpontaneousScroll?.(H)?H.scrollTo({top:H.scrollHeight,behavior:"instant"}):(p.current=!1,dU.delete(H))):!M||y.current||`
		if !strings.Contains(got, want) {
			t.Errorf("2.31 shape missing %q in:\n%s", want, got)
		}
	})
	// 2.31.0 passes a sixth callback to RPb that checks whether the server still holds a credential; keep that call
	// while dropping the empty UserStatusSchema reset on errors.
	t.Run("user-status-keep-on-error", func(t *testing.T) {
		v231 := `async function RPb(a,b,c,d,f,g){var h=e,k=e;try{try{if(typeof a.getUserStatus!=="function")b(Xq(Wb(E.UserStatusSchema)));else{var {userStatus:l}=k(await h(a.getUserStatus()));f()||(l&&(b(Xq(Opa(l,c))),l.email&&d?.(!l.disableTelemetry&&!l.userDataCollectionForceDisabled)),l?.email&&!l.cascadeModelConfigData?.errorMessage||g?.())}}catch(m){k(),console.warn("[UserStatusSync] Failed to fetch user status:",m),f()||(b(Xq(Wb(E.UserStatusSchema))),g?.())}}finally{h()}}` + "\n" + `function SPb(){var a=nk(),b=mk(),c=(0,B.useContext)(Pt),d=ok(ik),f=fd()?.core.authService,g=ol(),h=gj(),k=h.get("models")?.geminiModelsOnly===!0,l=h.get("settings")?.onTelemetrySettingChanged;(0,B.useEffect)(()=>{if(c)if(d?.state==="signedOut")a(Xq(Wb(E.UserStatusSchema)));else{var p=!1,q=()=>p,t=()=>{ik(b.getState())?.state==="signedIn"&&f?.signOutIfServerHasNoCredential?.()};RPb(c,a,k,l,q,t);var u=setInterval(()=>{RPb(c,a,k,l,q,t)},6E5);return()=>{p=!0;clearInterval(u)}}},[a,c,d,f,b,k,l]);return null}`
		if userStatusNoResetOnErrorRe.MatchString(v231) || !userStatusKeepOnErrorRe.MatchString(v231) {
			t.Error("2.31 UserStatusSync guard patterns did not recognize the 2.31 shape")
		}
		got := applyOnly(t, "user-status-keep-on-error", v231, Options{})
		for _, want := range []string{
			`catch(m){return k(),console.warn("[UserStatusSync] Failed to fetch user status:",m),f()||g?.(),!1}`,
			`try{_ok=(await RPb(c,a,k,l,q,t))!==!1}catch{_ok=!1}`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("2.31 shape missing %q in:\n%s", want, got)
			}
		}
	})
	// Two patches are optional since 2.31.0 does what they did, and the live bundle test checks that it still does.
	t.Run("done by the bundle itself", func(t *testing.T) {
		buttons := `className:Ai("flex flex-row items-center justify-end gap-0.5 select-none",` + "\n" + `f?"px-1 transition-opacity":"px-1 can-hover:absolute can-hover:bottom-1 can-hover:right-1 can-hover:p-1 can-hover:rounded-full can-hover:bg-card can-hover:border can-hover:border-card-border can-hover:shadow-sm user-input-buttons-container transition-all","can-hover:opacity-0 can-hover:pointer-events-none group-hover/user-input-step:opacity-100 group-hover/user-input-step:pointer-events-auto group-has-[:focus-visible]/user-input-step:opacity-100 group-has-[:focus-visible]/user-input-step:pointer-events-auto")`
		if mobileUserMessageActionsRe.MatchString(buttons) || !userMessageButtonsOnTouchRe.MatchString(buttons) {
			t.Error("2.31 user message buttons are not recognized as shown on touch screens")
		}
		hook := `var H=m.current;if(H){var J=H.scrollTop,K=Math.max(0,H.scrollHeight-H.clientHeight),M=K-J<=a,L=K<t.current&&J>=K-1,N=J-q.current,P=N>0&&N<=K-t.current,O=p.current;N<0&&!L?(p.current=!1,dU.delete(H)):!M||y.current||`
		if autoscrollDistanceFixRe.MatchString(hook) || !autoscrollPhysicalDistanceRe.MatchString(hook) {
			t.Error("2.31 auto-scroll hook is not recognized as measuring the distance itself")
		}
	})
	// 2.31.0 passes a sixth policy-map parameter to bAb and wraps each attachment branch in an policy guard (!k(p, ...)).
	t.Run("file-upload-large-file-streaming-fallback", func(t *testing.T) {
		v231 := `function test(){var bAb=(a,b,c,d,f=5,g)=>{var h=St(),k=(0,B.useCallback)((p,q)=>{if(!g||g.get(q)===!0)return!1;p=RYa(p.name);d?.(p);h(rg.VALIDATION_CHECK_FAILED,Error(p));return!0},[g,d,h]),l=(0,B.useCallback)(async p=>{var q=e,t=e;try{let z=c?GYa:[];if(b>=f)d?.("Maximum number of attachments reached");else{var u=MYa(p.name,p.type);if(u){if(!k(p,u))if(p.size>10485760)d?.("PDF file size exceeds 10MB limit"),h(rg.VALIDATION_CHECK_FAILED,Error("PDF file size exceeds 10MB limit"));else try{a()}catch(A){}}else{var v=VYa(p.name,p.type);if(v){if(!k(p,v))if(p.size>10485760)d?.("Document file size exceeds 10MB limit"),h(rg.VALIDATION_CHECK_FAILED,Error("Document file size exceeds 10MB limit"));else try{a()}catch(A){}}else{var w=aZa(p.name,p.type);if(w){if(!k(p,w))if(p.size>20971520)d?.("Zip file size exceeds 20MB limit"),h(rg.VALIDATION_CHECK_FAILED,Error("Zip file size exceeds 20MB limit"));else try{a()}catch(A){}}else{var x=TYa(p.name,p.type);if(x){if(!k(p,x))if(p.size>1048576)d?.("Text file size exceeds 1MB limit"),h(rg.VALIDATION_CHECK_FAILED,Error("Text file size exceeds 1MB limit"));else try{a()}catch(A){}}else if(z.includes(p.type.toLowerCase())){if(!k(p,p.type.toLowerCase()))try{a()}catch(A){}}else{var y=UYa(p.name,p.type);if(y){if(!k(p,y))if(p.size>20971520)d?.("Audio file size exceeds 20MB limit"),h(rg.VALIDATION_CHECK_FAILED,Error("Audio file size exceeds 20MB limit"));else try{a()}catch(A){}}else if(!["image/png","image/jpeg","image/gif","image/svg+xml","image/webp"].includes(p.type.toLowerCase())){let A=p.type.toLowerCase();if(!(g===void 0||g.get(A)!==!0&&SYa(p))||!k(p,A||"application/octet-stream"))if(p.size>20971520)d?.("File size exceeds 20MB limit"),h(rg.VALIDATION_CHECK_FAILED,Error("File size exceeds 20MB limit"));else try{a()}catch(D){}}else if(!k(p,p.type.toLowerCase()))try{let {inlineData:A,mimeType:D}=t(await q(Bkb(p,1048576,1024))),{inlineData:C}=t(await q(Bkb(p,10240,320))),F=Wb(E.MediaSchema,{mimeType:D,description:p.name,payload:{case:"inlineData",value:A},thumbnail:C});a([F])}catch(A){t(),d?.("Failed to process image"),h(rg.VALIDATION_CHECK_FAILED,A)}}}}}}}finally{q()}},[a,b,c,d,h,f,k,g]),m=(0,B.useCallback)(async p=>{var q=e,t=e;try{if(p.length!==0){d?.(null);var u=f-b;a()}}finally{q()}},[b,l,d,f]);return{m}}}`
		got := applyOnly(t, "file-upload-large-file-streaming-fallback", v231, Options{})
		if count := strings.Count(got, `window.__agyUpload([p])`); count != 8 {
			t.Errorf("expected all 8 streaming branches in 2.31 shape, got %d in:\n%s", count, got)
		}
		for _, want := range []string{
			`window.__agySyncMediaState&&window.__agySyncMediaState(b,f);`,
			`window.__agyShouldStreamSingle&&window.__agyShouldStreamSingle(p,b,f)`,
			`p=window.__agyPartitionFiles?window.__agyPartitionFiles(p,f-b,b):`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("2.31 shape missing %q in:\n%s", want, got)
			}
		}
		if node, err := exec.LookPath("node"); err == nil {
			if out, err := exec.Command(node, "-e", got).CombinedOutput(); err != nil {
				t.Fatalf("invalid JS syntax in 2.31 file-upload-large-file-streaming-fallback: %v\n%s", err, out)
			}
		}
	})
}

// 2.31.0 gives the navigation column a compact mode for the mobile home, which moves conciergeTestId to the fourth
// parameter. The mobile home is still told apart by it, and the PC sidebar keeps its own row.
func TestMobileHomeNewConversationRow231(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	nav := `function nav({showNewConversation:a=!0,compact:b=!1,className:c,conciergeTestId:d}){var z=deps.z,y=deps.y,G=deps.G,I=[],B=!1,x=()=>"/",t={},p=()=>{},J=!1,r=()=>{},f=true,C=null;return a||!z?.hideConversationHistory||y||G||I.length>0||B?A.createElement("div",{className:dj("flex flex-col",b?"gap-0.5":"gap-1.5",c),"data-testid":"sidebar-navigation-items"},a&&A.createElement(B5,{variant:"secondary",icon:A.createElement(lt,{name:"add"}),` + "\n" + `text:"New Conversation",href:x(t),"data-testid":"new-conversation-button",onActivate:()=>{p();J&&r()},selected:f,tooltipText:"New Conversation",tooltipKeybinding:C?.keybindings}),"rest"):null}`
	got := applyOnly(t, "mobile-home-new-conversation-row", nav, Options{MobileUX: true})
	if !strings.Contains(got, `var _agyHome=d==="mobile-home-concierge"`) {
		t.Fatalf("the patch did not read the fourth parameter:\n%s", got)
	}
	harness := `
const assert = require("assert");
global.window = {};
const A = { createElement(tag, props, ...children) { return { tag, props, children }; } };
const dj = (...c) => c.join(" "), B5 = "SideBarButton", lt = "Icon";
const deps = { z: { hideConversationHistory: true }, y: false, G: false };
` + got + `
const home = nav({ showNewConversation: false, compact: true, className: "px-2", conciergeTestId: "mobile-home-concierge" });
assert.strictEqual(home.props.className, "flex flex-col gap-0.5 px-2");
const row = home.children[0];
assert.strictEqual(row.props.variant, "ghost");
assert.strictEqual(row.props.href, undefined);
assert.strictEqual(row.props.selected, false);
assert.strictEqual(window.__agyNewRowOnTop, true);
window.__agyNewRowOnTop = undefined;
const desk = nav({}).children[0];
assert.strictEqual(desk.props.variant, "secondary");
assert.strictEqual(desk.props.href, "/");
assert.strictEqual(window.__agyNewRowOnTop, undefined);
`
	if out, err := exec.Command(node, "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
}

func TestRemoteSidecarPreviewAndOAuthPatches2312(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}

	bridgeJS := strings.TrimPrefix(remoteRuntimeBridgeScript, `<script id="agy-remote-runtime-bridge">`)
	bridgeJS = strings.TrimSuffix(bridgeJS, `</script>`)

	sidecarJS := applyOnly(t, "sidecar-resolve-url",
		`const u={};const sidecarHelper={resolveSidecarUrl:(C,F)=>{a:{var G=u;F=F.startsWith("/")?F:`+"`/${F}`"+`;try{let J=new URL(C),K=new URL(F,"http://dummy.local");J.pathname=K.pathname;for(let [L,N]of K.searchParams.entries())J.searchParams.set(L,N);let M=G[C];var H=M?`+"`plugin://${M}${J.pathname}${J.search}`"+`:J.toString();break a}catch(J){H=(H=G[C])?`+"`plugin://${H}${F}`"+`:`+"`${C}${F}`"+`;break a}H=void 0}return H}};`,
		Options{},
	)
	sidecarCatchlessJS := applyOnly(t, "sidecar-resolve-url",
		`const u2={};const sidecarCatchless={resolveSidecarUrl:(C,F)=>{a:{var G=u2;F=F.startsWith("/")?F:`+"`/${F}`"+`;try{let J=new URL(C),K=new URL(F,"http://dummy.local");J.pathname=K.pathname;for(let [L,N]of K.searchParams.entries())J.searchParams.set(L,N);let M=G[C];var H=M?`+"`plugin://${M}${J.pathname}${J.search}`"+`:J.toString();break a}catch{H=(H=G[C])?`+"`plugin://${H}${F}`"+`:`+"`${C}${F}`"+`;break a}H=void 0}return H}};`,
		Options{},
	)
	frameJS := applyOnly(t, "sidecar-iframe-sandbox",
		`let m=document.createElement("iframe");m.setAttribute("allow","clipboard-read; clipboard-write");try{asa(m,2,b)}catch(n){}m.dataset.testid="sidecar-iframe";`,
		Options{},
	)
	if want := `let m=document.createElement("iframe");m.setAttribute("sandbox","` + SidecarFrameSandbox + `");m.setAttribute("allow"`; !strings.Contains(frameJS, want) {
		t.Fatalf("sidecar-iframe-sandbox: the sandbox is not set before the frame loads:\n%s", frameJS)
	}
	modalJS := applyOnly(t, "sidecar-modal-endpoint-url",
		`function endpointFor(c){var A=c.baseUrl||(c.webPort?`+"`http://127.0.0.1:${c.webPort}`"+`:"\u2014");return A}`,
		Options{},
	)
	gateJS := applyOnly(t, "loopback-preview-feature-gate",
		`const ti={Web:{Desktop:1,Mobile:2},Electron:3},qBb={name:"basic"},rBb={name:"loopback",openLoopbackInPreviewByDefault:!0},sBb={name:"webview"};const featureDefs={browserAuxPane:{feature:!1,applyFirst:[{runtimeGating:{host:[ti.Web.Desktop,ti.Web.Mobile],userCohort:["google"]},feature:qBb}],applyAll:[{runtimeGating:{host:[ti.Electron],customFlag:"enable-hub-builtin-browser"},feature:sBb}]},remoteConversationBrowserPane:{feature:!1,applyAll:[{runtimeGating:{host:[ti.Web.Desktop],userCohort:["google"],customFlag:"enable-remote-conversation-preview"},feature:rBb}]}};`,
		Options{},
	)
	fetchFallbackJS := applyOnly(t, "loopback-preview-fetch-fallback",
		`const f=fn=>fn(upstreamClient);const lsSvc={streamTerminalOutput:(...u)=>f(v=>v.streamTerminalOutput(...u)),fetchSandboxPort:(...u)=>f(async v=>v.fetchSandboxPort(...u)),listBackgroundTasks:(...u)=>f(async v=>v.listBackgroundTasks(...u))};`,
		Options{},
	)
	routerJS := applyOnly(t, "router-navigate-bridge",
		`function RD(){var a=SC();return B.useCallback(b=>a.navigate({...b,from:b.from??void 0}),[void 0,a])}`,
		Options{},
	)
	mentionJS := applyOnly(t, "plugin-mention-remote-signin",
		`function nCc(a){var b=gj(),{startPluginAuthSignIn:c}=Qt(),d=St(),f=!!b.get("pluginAuth"),g=f&&CT(b.runtimeEnvironment)===1;b=(0,B.useCallback)((k,l)=>{if(f)return mCc(l??a,k)},[a,f]);var h=(0,B.useCallback)(k=>{g&&c&&c({plugin:k.plugin,provider:k.provider,accountId:k.accountId,mode:1}).then(l=>{dm(window,l.authUrl,"_blank","noopener,noreferrer")})},[g,d,c]);return{canSignIn:g,authViewFor:b,startSignIn:h}}`,
		Options{},
	)
	mcpOAuthJS := applyOnly(t, "mcp-oauth-remote-callback",
		`const e=x=>typeof x==="undefined"?void 0:x;const mcpSvc={completeMcpOAuth:async(u,v)=>f(async w=>{var x=e,y=e;try{y(await x(w.completeMcpOAuth(Wb(Dia,{serverName:u,authorizationCode:v}))))}finally{x()}})};`,
		Options{},
	)
	pluginOAuthJS := applyOnly(t, "plugin-oauth-remote-callback",
		`const plugSvc={signInPluginAuthProvider:async u=>f(v=>v.signInPluginAuthProvider(Wb(Eja,u)))};`,
		Options{},
	)

	harness := `"use strict";
const assert = require("assert");
global.location = { origin: "https://agy.example.com", protocol: "https:", host: "agy.example.com" };
global.window = { location: global.location };

let mockFetchHandler = async () => { throw new Error("unexpected fetch"); };
global.fetch = (url, opts) => mockFetchHandler(url, opts);

` + bridgeJS + "\n" + sidecarJS + "\n" + sidecarCatchlessJS + "\n" + modalJS + "\n" + gateJS + `

let upstreamClient = {};
` + fetchFallbackJS + `

const navCalls = [];
const popupCalls = [];
const desktopSignInCalls = [];
let runtimeMode = 2; // 2 = web, 1 = desktop
const SC = () => ({ navigate(arg) { navCalls.push(arg); } });
const B = { useCallback: fn => fn };
const gj = () => ({ get: k => k === "pluginAuth", runtimeEnvironment: { marker: "valid-env" } });
const CT = env => {
  assert.strictEqual(env && env.marker, "valid-env", "CT must receive original runtimeEnvironment, not reassigned authViewFor callback");
  return runtimeMode;
};
const Qt = () => ({
  startPluginAuthSignIn: req => {
    if (runtimeMode !== 1) {
      return Promise.reject(new Error("should not call desktop mode=1 in web runtime"));
    }
    desktopSignInCalls.push(req);
    return Promise.resolve({ authUrl: "https://auth.example.com/desktop-start" });
  }
});
const St = () => () => {};
const mCc = () => null;
const dm = (_win, url, target, features) => { popupCalls.push({ url, target, features }); };
const Wb = (_s, msg) => msg;
const Dia = "CompleteMcpOAuth", Eja = "SignInPluginAuth";

` + routerJS + "\n" + mentionJS + "\n" + mcpOAuthJS + "\n" + pluginOAuthJS + `

(async () => {
  // 1. Sidecar URL resolution (both catch(e) and catch{} minifier forms) + modal endpoint
  assert.strictEqual(
    sidecarHelper.resolveSidecarUrl("http://localhost:39715", "/board?token=t1"),
    "https://agy.example.com/__agy/sidecar/39715/board?token=t1"
  );
  assert.strictEqual(
    sidecarCatchless.resolveSidecarUrl("http://127.0.0.1:39716", "sub"),
    "https://agy.example.com/__agy/sidecar/39716/sub"
  );
  assert.strictEqual(
    endpointFor({ baseUrl: "http://127.0.0.1:41234" }),
    "https://agy.example.com/__agy/sidecar/41234/"
  );
  assert.strictEqual(endpointFor({}), "\u2014");

  // 2. Loopback preview feature gate removes google cohort restriction and binds rBb
  const webRule = featureDefs.browserAuxPane.applyFirst[0];
  assert.strictEqual(webRule.runtimeGating.userCohort, undefined);
  assert.strictEqual(webRule.feature.openLoopbackInPreviewByDefault, true);

  // 3. fetchSandboxPort falls back on Unimplemented (code 12) and translates noListener -> ConnectError code 5
  upstreamClient.fetchSandboxPort = async () => {
    const err = new Error("Unimplemented");
    err.code = 12;
    throw err;
  };
  mockFetchHandler = async (url, opts) => {
    assert.strictEqual(url, "/__agy/api/loopback-fetch");
    const body = JSON.parse(opts.body);
    if (body.port === 9999) {
      return { ok: true, status: 200, json: async () => ({ noListener: true }) };
    }
    return {
      ok: true,
      status: 200,
      json: async () => ({
        statusCode: 200,
        headers: [{ name: "content-type", value: "text/html" }],
        bodyBase64: Buffer.from("<h1>ok</h1>").toString("base64")
      })
    };
  };
  const okRes = await lsSvc.fetchSandboxPort({ conversationId: "c1", port: 3000, method: "GET", path: "/" });
  assert.strictEqual(okRes.statusCode, 200);
  assert.strictEqual(Buffer.from(okRes.body).toString("utf8"), "<h1>ok</h1>");

  let caughtNoListener = null;
  try {
    await lsSvc.fetchSandboxPort({ conversationId: "c1", port: 9999, method: "GET", path: "/" });
  } catch (e) {
    caughtNoListener = e;
  }
  assert.ok(caughtNoListener && caughtNoListener.name === "ConnectError" && caughtNoListener.code === 5);

  // 4a. Plugin @-mention sign-in on web routes to plugin Accounts tab via captured router
  RD();
  const mention = nCc([]);
  assert.strictEqual(mention.canSignIn, true);
  mention.startSignIn({ plugin: "/home/ubuntu/.gemini/config/plugins/figma", provider: "oauth", accountId: "" });
  mention.startSignIn({ plugin: "C:\\Users\\alice\\.gemini\\config\\plugins\\figma\\", provider: "oauth", accountId: "" });
  mention.startSignIn({ plugin: "file:///C:/Users/alice/.gemini/config/plugins/linear?rev=1#frag", provider: "oauth", accountId: "" });
  assert.strictEqual(navCalls.length, 3);
  assert.deepStrictEqual(navCalls[0], {
    to: "/customizations/plugins/$pluginId",
    params: { pluginId: "figma" },
    search: { pluginTab: "accounts" }
  });
  assert.deepStrictEqual(navCalls[1], {
    to: "/customizations/plugins/$pluginId",
    params: { pluginId: "figma" },
    search: { pluginTab: "accounts" }
  });
  assert.deepStrictEqual(navCalls[2], {
    to: "/customizations/plugins/$pluginId",
    params: { pluginId: "linear" },
    search: { pluginTab: "accounts" }
  });

  // 4b. Desktop mode (CT === 1) preserves native mode=1 popup flow even after b was reassigned to authViewFor
  runtimeMode = 1;
  const deskMention = nCc([]);
  assert.strictEqual(deskMention.canSignIn, true);
  deskMention.startSignIn({ plugin: "/plugins/figma", provider: "oauth", accountId: "acc1" });
  await new Promise(r => setImmediate(r));
  assert.strictEqual(navCalls.length, 3, "desktop mode must not trigger remote web router navigation");
  assert.strictEqual(desktopSignInCalls.length, 1);
  assert.strictEqual(desktopSignInCalls[0].mode, 1);
  assert.strictEqual(popupCalls.length, 1);
  assert.strictEqual(popupCalls[0].url, "https://auth.example.com/desktop-start");

  // 5. Remote OAuth: loopback callback delivered short-circuits single-use auth code; full HTTPS callback URL falls back to extracted code
  let mcpSubmitted = [];
  upstreamClient.completeMcpOAuth = async (msg) => { mcpSubmitted.push(msg); return {}; };
  mockFetchHandler = async (url, opts) => {
    assert.strictEqual(url, "/__agy/api/oauth-callback");
    const b = JSON.parse(opts.body);
    assert.strictEqual(b.url, "http://127.0.0.1:45111/callback?code=once_tok&state=s1");
    return { ok: true, status: 200, json: async () => ({ delivered: true, statusCode: 200 }) };
  };
  await mcpSvc.completeMcpOAuth("my-mcp", "http://127.0.0.1:45111/callback?code=once_tok&state=s1");
  assert.strictEqual(mcpSubmitted.length, 0, "delivered loopback callback must not double-submit single-use code");

  let pluginAttempts = [];
  upstreamClient.signInPluginAuthProvider = async (msg) => {
    pluginAttempts.push(msg.secret);
    if (msg.secret.startsWith("https://")) {
      throw new Error("raw URL rejected by provider");
    }
    return {};
  };
  await plugSvc.signInPluginAuthProvider({
    plugin: "/plugins/figma",
    provider: "figma",
    secret: "https://antigravity.google/oauth-callback?code=extracted_777&state=abc"
  });
  assert.deepStrictEqual(pluginAttempts, [
    "https://antigravity.google/oauth-callback?code=extracted_777&state=abc",
    "extracted_777"
  ]);
})().catch(err => { console.error(err); process.exit(1); });
`
	if out, err := exec.Command(node, "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("2.31.2 remote patches harness failed: %v\n%s", err, out)
	}
}

// The sign-in patch ends the hook's var statement and reassigns the flag. If
// the minifier ever continues that declaration, a later name would become an
// undeclared assignment, which throws in strict mode, so the patch must
// report itself missing instead of applying.
func TestPluginMentionSignInNeedsEndOfDeclaration(t *testing.T) {
	body := `function nCc(a){var b=gj(),{startPluginAuthSignIn:c}=Qt(),d=St(),f=!!b.get("pluginAuth"),g=f&&CT(b.runtimeEnvironment)===1,b2=(0,B.useCallback)((k,l)=>{if(f)return mCc(l??a,k)},[a,f]),h=(0,B.useCallback)(k=>{g&&c&&c({plugin:k.plugin,provider:k.provider,accountId:k.accountId,mode:1}).then(l=>{dm(window,l.authUrl,"_blank","noopener,noreferrer")})},[g,d,c]);return{canSignIn:g,authViewFor:b2,startSignIn:h}}`
	out, report := Apply(MainJS, []byte(body), Options{})
	var status Status = -1
	for _, r := range report {
		if r.ID == "plugin-mention-remote-signin" {
			status = r.Status
		}
	}
	if status != StatusMissing || string(out) != body {
		t.Fatalf("plugin-mention-remote-signin on a continued declaration: status %v, changed %v", status, string(out) != body)
	}
}

// A sandboxed sidecar frame posts with origin "null". The panel takes its
// add-artifact-comment only with the gateway token of the iframe's src, and
// still takes a message from the frame's real origin as before.
func TestSidecarArtifactCommentOrigin(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	bridgeJS := strings.TrimPrefix(remoteRuntimeBridgeScript, `<script id="agy-remote-runtime-bridge">`)
	bridgeJS = strings.TrimSuffix(bridgeJS, `</script>`)
	handlerJS := applyOnly(t, "sidecar-artifact-comment-origin",
		`function quc(a){var b=nk(),c=FE();(0,B.useEffect)(()=>{var d=f=>{var g=a.current,h;if(h=g?.contentWindow!=null&&f.source===g.contentWindow){h=f.origin;a:{try{var {origin:k}=new URL(g.src);var l=k==="null"?void 0:k;break a}catch{}l=void 0}h=h===l}if(h&&f.data?.type==="add-artifact-comment"){b(f.data.payload)}};window.addEventListener("message",d);return()=>{window.removeEventListener("message",d)}},[b,c,a])}`,
		Options{},
	)
	harness := `"use strict";
const assert = require("assert");
global.location = { origin: "https://agy.example.com", protocol: "https:", host: "agy.example.com", href: "https://agy.example.com/c/1" };
const listeners = [];
global.window = global;
window.addEventListener = (type, fn) => { if (type === "message") listeners.push(fn); };
window.removeEventListener = () => {};
global.document = { addEventListener() {}, createElement() { return {}; }, head: { appendChild() {} } };
` + bridgeJS + `
const added = [];
global.nk = () => (p) => added.push(p);
global.FE = () => ({});
global.B = { useEffect: (fn) => fn() };
` + handlerJS + `
const frameWin = {};
const frame = { contentWindow: frameWin, src: "https://agy.example.com/__agy/sidecar/38489/?token=gw123" };
quc({ current: frame });
const send = (origin, data, source) => listeners.forEach((fn) => fn({ origin, data, source: source || frameWin }));
send("null", { type: "add-artifact-comment", payload: "ok-token", agySidecarToken: "gw123" });
send("null", { type: "add-artifact-comment", payload: "no-token" });
send("null", { type: "add-artifact-comment", payload: "wrong-token", agySidecarToken: "other" });
send("null", { type: "add-artifact-comment", payload: "other-window", agySidecarToken: "gw123" }, {});
send("https://agy.example.com", { type: "add-artifact-comment", payload: "same-origin" });
frame.src = "https://agy.example.com/__agy/sidecar/38489/";
send("null", { type: "add-artifact-comment", payload: "src-without-token", agySidecarToken: "" });
assert.deepStrictEqual(added, ["ok-token", "same-origin"]);
`
	dir := t.TempDir()
	path := filepath.Join(dir, "harness.js")
	if err := os.WriteFile(path, []byte(harness), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, path).CombinedOutput(); err != nil {
		t.Fatalf("node harness failed: %v\n%s", err, out)
	}
}
