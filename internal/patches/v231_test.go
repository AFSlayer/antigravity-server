package patches

import (
	"os/exec"
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
