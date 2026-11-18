package patches

import (
	"strings"
	"testing"
)

// The text each patch matches in Antigravity 2.33.0, with the bundle's own line breaks.
const (
	v233AuthRetry      = `_authState.state==="uninitialized"){a.startupAuth.lastErrorCode=W5a(p);let q=d6a(p),t=h||!k||!k.hasToken||k.isGcpTos&&!k.projectId?void 0:k,u=!!t&&p instanceof gh&&p.code===4;if(b>0&&q&&!u){let v=4-b;d(await c(new Promise(w=>{setTimeout(w,1E3*2**v)})));return a.isLoggingOut||a.sessionTracker.id!==f?void 0:lC(a,b-1)}`
	v233UserStatusSync = `catch(q){if(m(),console.warn("[UserStatusSync] Failed to fetch user status:",q),!f()){var p=q instanceof gh&&q.code===7?SHa(q.rawMessage||
q.message):void 0;p?b(VHa(p)):(h!=="clearStatus"||k?.()||b(Mr(cc(F.UserStatusSchema))),g?.())}}}finally{l()}}function kXb(){var a=Gk(),b=nd()?.core.authService;return(0,D.useCallback)(()=>{xk(a.getState())?.state==="signedIn"&&b?.signOutIfServerHasNoCredential?.()},[a,b])}
function lXb(){var a=Hk(),b=Gk(),c=(0,D.useContext)(Qt),d=vj(),f=d.get("models")?.geminiModelsOnly===!0,g=d.get("settings")?.onTelemetrySettingChanged,h=kXb();return(0,D.useCallback)(async()=>{var k=e,l=e;try{if(c){var m=xk(b.getState());l(await k(SX(c,a,f,g,()=>xk(b.getState())!==m,h,"keepStatus")))}}finally{k()}},[a,b,c,f,g,h])}
function mXb(){var a=Hk(),b=Gk(),c=(0,D.useContext)(Qt),d=Ik(xk),f=nd()?.core.authService,g=Ll(),h=vj(),k=h.get("models")?.geminiModelsOnly===!0,l=h.get("settings")?.onTelemetrySettingChanged,m=kXb();(0,D.useEffect)(()=>{if(c)if(d?.state==="signedOut")a(Mr(cc(F.UserStatusSchema)));else{var q=!1,t=()=>q,u=()=>{var w=b.getState();return!(!w.userStatus||!w.userStatus[Lr])};SX(c,a,k,l,t,m,"clearStatus",u);var v=setInterval(()=>{SX(c,a,k,l,t,m,"clearStatus",u)},6E5);return()=>{q=!0;clearInterval(v)}`
	v233SidePanelBody       = `"data-side-panel-body":!0},ea?D.createElement("div",{className:"h-full w-full"},D.createElement(efd,{key:ea.id,panelScope:a,tabId:ea.id,tab:fa})):D.createElement(Ffd,{panelScope:a,isPaneOpen:aa})`
	v233SidePanelBodyLegacy = `U&&H.some(W=>W.id==="terminal")&&D.createElement(cVc,{stripRef:L,activeTabId:T}),D.createElement("div",{className:"flex-grow overflow-hidden","data-side-panel-body":!0},R&&D.createElement("div",{className:"h-full w-full"},D.createElement(bVc,{key:R.id,panelScope:a,tabId:R.id,tab:X}))`
	v233NewConvoButton      = `"New Conversation in Workspace":
"New Conversation in Project",rc=ad=>{ad.stopPropagation();Va(pb)});Fa={item:Fa,brandedProjectId:pb,projectId:Jb?pb:void 0,onAddClick:rc,newChatLabel:hc,onClick:()=>{Fb||bb(pb)},onPointerDown:!V&&h==="project"&&Jb?ad=>{ad.button===0&&Vc(pb,ad)}:void 0,isHovered:wb(pb),surface:a};return Jb?D.createElement(gdd,{...Fa,projectId:pb,archiving:N,isOpen:ja===pb,onOpenChange:ad=>{pa(ad?pb:null)}}):D.createElement(edd,{...Fa})}},[h,bb,Vc,Fb,Dc,Ma,ta,sa,ja,Va,N,ra,a,V,R,T,I,ec,U,na,hb]),kb=Ik(Fa=>Fa.newProject?.isOpen??
!1);return D.createElement(Sy,{delay:300,timeout:200},D.createElement("div",{ref:ob.sidebarBoundsRef,className:Oi("w-full relative",!b&&"flex-1 min-h-0 h-full")},Na?D.createElement(Ubd,null):D.createElement(ZWc,{items:ob.displayedFlatItems,surface:a,scrollElementRef:b,scrollMargin:c,subtitleContext:S,onToggleCollapseSection:bb,onToggleSectionHeader:na,onTogglePagination:Nb,customHeaderRenderer:Sa`
	v233HomeNewConvoRow = `{showNewConversation:a=!0,compact:b=!1,className:c,conciergeTestId:d}){var f=zJ(),g=qlb(),{config:h,navigate:k,isOpen:l}=x1(),m=wJ(),n=AJ(),p=ylb(),q=v3c("new_conversation_button"),{toggleSidebar:t}=V0(),u=WH(),v=FJ({to:"/"}),w=FJ({to:"/customizations"}),x=q1(),y=vj(),z=fB()?.conciergeCos?.SidebarButton,A=(y.get("conciergeAgent")?.enabled??!1)&&!!z,C=jH("openNewConversation"),B=jH("openScheduledTasks"),E=y.get("plugins")??!1,G=y.get("sidebarItemsFeature"),H=(0,D.useMemo)(()=>(G?.items??
[]).filter(M=>!zna(M)),[G?.items]),I=U0(),K=y.get("extensibility");y=Lk(K?.enabled)??!1;K=Lk(K?.cloudTriggers?.triggers)??[];K=W4c(K.filter(M=>M.optionId));var L=pC();return a||y||E||H.length>0||A?D.createElement("div",{className:Oi("flex flex-col",b?"gap-0.5":"gap-1.5",c),"data-testid":"sidebar-navigation-items"},a&&D.createElement(R6,{variant:"secondary",icon:D.createElement(Nq,{name:"add"}),text:"New Conversation",href:x(v),"data-testid":"new-conversation-button",onActivate:()=>{q();I&&t()},selected:f,`
	v233CustomizationsTab = `if(!c)return f;f=new Map(f);f.delete("Customizations");return f}`
	v233WriteInFocus      = `"data-testid":"ask-question-writein",value:c,onClick:M,onChange:`
	v233RadioFocusSteal   = `d||da===void 0||a.isMultiSelect||!document.hasFocus()||Ipc(Z)&&
Z!==Q.current||T.current.get(da)?.focus()`
	v233NotificationPrompt      = `var d=!!this.storageService.get("didAskForNotificationPermission");this.notificationPermission!==`
	v233ContextIndicatorSetting = `case "contextUsageIndicatorEnabled":return a.rootProvider.getState().userConfig?.userSettings?.contextUsageIndicatorEnabled??c?.get("contextUsageIndicatorEnabled")==="true"`
	v233ContextIndicatorAllowed = `case "contextUsageIndicatorEnabled":return b;`

	// v233NotificationBridge is the desktop bridge's notification service, which 2.33 writes with the same statement
	// before the web one.
	v233NotificationBridge = `if(this.bridge){var b=!!this.storageService.get("didAskForNotificationPermission");this.offerPreferences&&!b&&x()}`
)

// 2.33 rewrote or rewrapped what these patches match.
func TestAntigravity233PatchShapes(t *testing.T) {
	mobileOpts := Options{MobileUX: true}
	for _, c := range []struct {
		id, src string
		opts    Options
		want    []string
		gone    []string
	}{
		// Two more names after the transient check, and no retry for a timeout with a token in hand.
		{"auth-startup-retry", v233AuthRetry, Options{}, []string{`lC(a,b>1?b-1:Date.now()<(a.__agyAuthRetryUntil??=Date.now()+3E5)?1:0)}`, `if(b>0&&q&&!u){`}, nil},
		// The catch answers a permission error before clearing: the answer stays, the clearing goes, and a failure
		// is told to the retry.
		{"user-status-keep-on-error", v233UserStatusSync, Options{}, []string{
			"catch(q){if(m(),console.warn(\"[UserStatusSync] Failed to fetch user status:\",q),!f()){var p=q instanceof gh&&q.code===7?SHa(q.rawMessage||\nq.message):void 0;p?b(VHa(p)):g?.()}return!1}",
			`_ok=(await SX(c,a,k,l,t,m,"clearStatus",u))!==!1`,
			`var v=setInterval(()=>{_agyRun(!1)},6E5);`,
		}, []string{`k?.()||b(`}},
		// The tab's scope is passed as panelScope.
		{"side-panel-closed-file-viewer", v233SidePanelBody, Options{}, []string{`ea?(window.__agyPaneTab?D.createElement(window.__agyPaneTab,{key:ea.id,R:D,treeId:a,tab:ea,open:aa,idle:D.createElement(Ffd,{panelScope:a,isPaneOpen:!1})},D.createElement("div",{className:"h-full w-full"},`}, nil},
		{"side-panel-closed-file-viewer-legacy", v233SidePanelBodyLegacy, Options{}, []string{`R&&(window.__agyPaneTab?D.createElement(window.__agyPaneTab,{key:R.id,R:D,treeId:a,tab:R,open:U,idle:null},D.createElement("div",{className:"h-full w-full"},`}, nil},
		// The list is passed no variant.
		{"mobile-new-conversation-button", v233NewConvoButton, mobileOpts, []string{`"header-__agy_new__"`, `items:a==="background"&&!window.__agyNewRowOnTop?`}, nil},
		// No setting hides the conversation history.
		{"mobile-home-new-conversation-row", v233HomeNewConvoRow, mobileOpts, []string{`var _agyHome=d==="mobile-home-concierge";`, `return a||_agyHome||y||E||H.length>0||A?`}, nil},
		// The tab is dropped in the hook that builds the screens.
		{"settings-restore-customizations-tab", v233CustomizationsTab, Options{}, []string{`return f}`}, []string{`delete("Customizations")`}},
		// The write-in's click handler has a name.
		{"question-modal-write-in-focus", v233WriteInFocus, Options{}, []string{`onClick:M,onFocus:M,onChange:`}, nil},
		// A line break after && and a check before the rest.
		{"question-modal-prevent-radio-focus-steal", v233RadioFocusSteal, Options{}, []string{`Z!==Q.current||document.activeElement?.getAttribute?.("data-testid")==="ask-question-writein"||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches)||T.current.get(da)?.focus()`}, nil},
		// The web notification service, after the desktop bridge's, which has the same statement.
		{"mobile-skip-notification-prompt", v233NotificationBridge + v233NotificationPrompt, mobileOpts, []string{
			v233NotificationBridge,
			`var d=(window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches))||!!this.storageService.get("didAskForNotificationPermission");this.notificationPermission!==`,
		}, nil},
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
}

func TestAntigravity233PatchesTolerateLineBreaks(t *testing.T) {
	mobileOpts := Options{MobileUX: true}
	compact := func(s string) string { return strings.Join(strings.Fields(s), "") }
	for _, c := range []struct {
		id, src string
		opts    Options
	}{
		{"auth-startup-retry", v233AuthRetry, Options{}},
		{"user-status-keep-on-error", v233UserStatusSync, Options{}},
		{"side-panel-closed-file-viewer", v233SidePanelBody, Options{}},
		{"side-panel-closed-file-viewer-legacy", v233SidePanelBodyLegacy, Options{}},
		{"settings-restore-customizations-tab", v233CustomizationsTab, Options{}},
		{"question-modal-write-in-focus", v233WriteInFocus, Options{}},
		{"question-modal-prevent-radio-focus-steal", v233RadioFocusSteal, Options{}},
		{"mobile-skip-notification-prompt", v233NotificationPrompt, mobileOpts},
	} {
		base, status := applyWithStatus(t, c.id, c.src, c.opts)
		if status != StatusApplied {
			t.Errorf("%s: status %v without a line break", c.id, status)
			continue
		}
		positions := lineBreakPositions(c.src)
		if len(positions) < 2 {
			t.Errorf("%s: only %d break positions", c.id, len(positions))
		}
		for _, pos := range positions {
			out, status := applyWithStatus(t, c.id, c.src[:pos]+"\n"+c.src[pos:], c.opts)
			if status != StatusApplied || compact(out) != compact(base) {
				t.Errorf("%s: status %v with a line break at offset %d: %q", c.id, status, pos, c.src[max(0, pos-30):min(len(c.src), pos+30)])
				break
			}
		}
	}
}
