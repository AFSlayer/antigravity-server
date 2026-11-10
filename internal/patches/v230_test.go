package patches

import (
	"strings"
	"testing"
)

// Antigravity 2.30.0 moved, renamed or rewrapped what these patches match. Each case is the text the patch matches in
// the 2.30.0 bundle, so a pattern that stops matching it fails here as well as in the live bundle test.
func TestAntigravity230PatchShapes(t *testing.T) {
	// The composer view for a selected project.
	t.Run("mobile-new-convo-view", func(t *testing.T) {
		v230 := `const RVc=()=>{var a=XD(),b=lX();return(0,B.useCallback)((c,d)=>{b(kX.map(f=>({trigger:f,ran:!1})));a(c,{project:d})},[a,b])};` + "\n" + `var SVc=()=>{var a=RVc(),{q:b}=zD({strict:!1}),c=(0,B.useRef)(null),d=(0,B.useRef)(null),f=(0,B.useRef)(null),[g,h]=(0,B.useState)(0);(0,B.useEffect)(()=>{var k=()=>{var m=f.current?.offsetTop??0;h(n=>n!==m?m:n)};k();if(typeof ResizeObserver!=="undefined"&&d.current){var l=new ResizeObserver(k);l.observe(d.current,{box:"border-box"});return()=>{l.disconnect()}}},[]);y5("MOBILE_HOME_VIEW");return B.createElement("div",{"data-media-drop-scope":"",className:"w-full h-full flex flex-col min-h-0 animate-fade-in"},` + "\n" + `B.createElement("div",{ref:c,["data-sidebar-scroll-container"]:"true",className:"flex-1 min-h-0 overflow-y-auto flex flex-col pb-2 relative"},B.createElement("div",{ref:d,className:"shrink-0 flex flex-col gap-3 pt-3 pb-3 empty:hidden"},B.createElement(PVc,{showNewConversation:!1,className:"px-2",conciergeTestId:"mobile-home-concierge"}),B.createElement(HUc,{surface:"background"}),B.createElement(NVc,{surface:"background"})),B.createElement("div",{ref:f,className:"shrink-0"},B.createElement(YDc,null,` + "\n" + `B.createElement(KVc,{surface:"background",scrollElementRef:c,scrollMargin:g})))),`
		got := applyOnly(t, "mobile-new-convo-view", v230, Options{MobileUX: true})
		for _, want := range []string{
			`"data-testid":"mobile-new-convo-empty"`,
			`,replace:!0`,
			`isMobileNew=`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("2.30 shape missing %q in:\n%s", want, got)
			}
		}
	})
	// The titlebar, now a memo, asks the home hook about the project.
	t.Run("mobile-back-clears-section", func(t *testing.T) {
		v230 := `B.memo(function(){var a=Zi(),b=aE(),c=WD(),{product:d}=FH(),f=ZV(),g=a.get("settingsFeature"),h=PB(),k=OB(),l=fk(),m=OX(),n=iE(),p=n5(),q=jE(),u=iRc(),t=GOc(!n||b||p||m?void 0:u);n=n&&!p&&t&&!m;p=B.useCallback(()=>{q&&l(Uo({treeId:q,isOpen:!m}))},[l,q,m]);t=a.get("remoteControlInstanceSelector");var v=a.runtimeEnvironment,w=a.get("environmentFeature"),x=a.get("remoteControlReload");a=a.get("avatarSpacer")??!1;return B.createElement("div",{className:` + "`" + `flex items-center justify-between w-full pl-2 gap-2 border-b border-border ${a?` + "\n" + `"":"pr-4"}` + "`" + `,style:{height:"calc(48px + env(safe-area-inset-top))",paddingTop:"env(safe-area-inset-top)"}},B.createElement("div",{className:"flex items-center min-w-0 flex-1 gap-1"},b?B.createElement(B.Fragment,null,d.isGoogleInternal?B.createElement("span",{className:"text-sm font-medium text-foreground px-1"},"Jetski"):B.createElement(WEa,{className:"!h-[18px] !w-[18px] shrink-0 mx-1"}),t?B.createElement("div",{className:"min-w-0 flex-1 max-w-[220px]"},B.createElement(t)):v.isInIframe&&w&&B.createElement(mRc,` + "\n" + `{environmentFeature:w})):B.createElement(B.Fragment,null,B.createElement(ET,{iconName:"arrow_back",onClick:()=>{c()},"aria-label":"Back to home",dataTestId:"mobile-back-to-home",size:"icon-touch"})`
		got := applyOnly(t, "mobile-back-clears-section", v230, Options{MobileUX: true})
		for _, want := range []string{
			`b=aE(!0)`,
			`({clearSection:!0,clearProject:!0})`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("2.30 shape missing %q in:\n%s", want, got)
			}
		}
	})
	// A line break among the list's props.
	t.Run("mobile-new-conversation-button", func(t *testing.T) {
		v230 := `"New Conversation in Workspace":"New Conversation in Project",ib=wb=>{wb.stopPropagation();Qa(ic)});let Ha=Ya&&qa.has(ic);Vb={item:Vb,brandedProjectId:ic,projectId:Ya?ic:void 0,onAddClick:ib,newChatLabel:Sa,onClick:()=>{ud||Za(ic)},onPointerDown:aa||h!=="project"?void 0:wb=>{wb.button===0&&Jb(ic,wb)},isHovered:Da(ic),surface:a};return Ha?B.createElement(JVc,{...Vb,projectId:ic,archiving:N,isOpen:Z===ic,onOpenChange:wb=>` + "\n" + `{ma(wb?ic:null)}}):B.createElement(HVc,{...Vb})}},[h,Za,Jb,ud,Qd,ae,ra,pa,Z,Qa,N,qa,a,aa,Q,T,J,cc,U,la]),Hc=gk(Vb=>Vb.newProject?.isOpen??!1);return B.createElement(Ax,{delay:300,timeout:200},B.createElement("div",{ref:dd.sidebarBoundsRef,className:si("w-full relative",!b&&"flex-1 min-h-0 h-full")},Hb?B.createElement(LUc,null):B.createElement(kEc,{items:dd.displayedFlatItems,variant:"sidebar",surface:a,scrollElementRef:b,scrollMargin:c,subtitleContext:da,onToggleCollapseSection:Za,onToggleSectionHeader:la,` + "\n" + `onTogglePagination:hb,customHeaderRenderer:bc`
		got := applyOnly(t, "mobile-new-conversation-button", v230, Options{MobileUX: true})
		for _, want := range []string{
			`id:"header-__agy_new__"`,
			`window.__agyStartNewConversation=`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("2.30 shape missing %q in:\n%s", want, got)
			}
		}
	})
	// A line break before the hover classes.
	t.Run("mobile-conversation-row-actions", func(t *testing.T) {
		v230 := `className:si("agy-row-actions absolute top-0 bottom-0 -right-1 pl-6 flex items-center justify-end gap-0.5 z-10",!h&&"[&>*]:pointer-events-auto",v?"hidden":Va?"opacity-100":` + "\n" + `"can-hover:opacity-0 group-hover:opacity-100 group-focus-visible:opacity-100 group-has-[:focus-visible]:opacity-100 has-[:focus-visible]:opacity-100")`
		got := applyOnly(t, "mobile-conversation-row-actions", v230, Options{MobileUX: true})
		for _, want := range []string{
			`matchMedia("(pointer:coarse)")`,
			`?"opacity-100":"can-hover:opacity-0`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("2.30 shape missing %q in:\n%s", want, got)
			}
		}
	})
	// The modal's setter is q rather than r.
	t.Run("settings-doc-row-editor", func(t *testing.T) {
		v230 := `function v3({title:a,displayPath:b,copyPath:c,description:d,content:f,modalChildren:g,badge:h,actions:k,actionsPaddingClass:l,disabled:m=!1,MarkdownRenderer:n}){var [p,q]=(0,B.useState)(!1),u=!!d||!!f||!!g,t=d||b;`
		got := applyOnly(t, "settings-doc-row-editor", v230, Options{MobileUX: true})
		for _, want := range []string{
			`agySetEdit(!0);q(!0);`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("2.30 shape missing %q in:\n%s", want, got)
			}
		}
	})
	// The scroll range comes from a local copy of the ref.
	t.Run("scrollbar-drag-follows-paging", func(t *testing.T) {
		v230 := `pa=z.current.startScrollPos+` + "\n" + `((h?pa.clientX:pa.clientY)-z.current.startCoord)/ma*Z.maxScrollPos,h?oa.scrollLeft=pa:oa.scrollTop=pa,`
		got := applyOnly(t, "scrollbar-drag-follows-paging", v230, Options{MobileUX: true})
		for _, want := range []string{
			`window.__agyDragScrollbar(z.current,oa,h,h?pa.clientX:pa.clientY,ma,Z.maxScrollPos)`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("2.30 shape missing %q in:\n%s", want, got)
			}
		}
	})
	// The session is checked through sessionTracker.
	t.Run("auth-startup-retry", func(t *testing.T) {
		v230 := `_authState.state==="uninitialized"){let n=j6a(m);if(b>0&&n){let p=4-b;d(await c(new Promise(q=>{setTimeout(q,1E3*2**p)})));return a.isLoggingOut||a.sessionTracker.id!==f?void 0:k6a(a,b-1)}`
		got := applyOnly(t, "auth-startup-retry", v230, Options{MobileUX: true})
		for _, want := range []string{
			`__agyAuthRetryUntil`,
			`a.sessionTracker.id!==f?void 0:`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("2.30 shape missing %q in:\n%s", want, got)
			}
		}
	})
}
