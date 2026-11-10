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
