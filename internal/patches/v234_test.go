package patches

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The text each patch matches in Antigravity 2.34.0, with the bundle's own line breaks; the Customizations screen's
// filter with the statement before it and the return after it; and the whole of the user status sync: the function,
// the hook that checks the credential, the one that refreshes on request, and the effect that polls.
const (
	v234ContextIndicatorSetting = `{title:"Chat",settings:["verboseConversationSteps"]},{title:"Chat Layout",settings:["conversationWidth","artifactWidth","contextUsageIndicatorEnabled"]}`
	v234CustomizationsTab       = `if(!a)return qMb;var b=new Map(qMb);b.delete("Customizations");return b}`
	v234CustomizationsScreen    = `d||(l=l.filter(m=>m!=="Jetski Chat"));a.get("plugins")&&(l=l.filter(m=>m!=="Customizations"));return l}`
	v234HeaderActions           = `className:Yi("absolute right-1 top-0 flex h-full items-center gap-1",
k||u?"opacity-100":"opacity-0 group-hover/header:opacity-100 group-focus-within/header:opacity-100")`
	v234KebabMenu       = `function gkd(a){var {isOpen:b,onOpenChange:c,ariaLabel:d,sortMode:f,onSortModeChange:g,size:h="icon"}=a;a=ekd(a);var k=yS(),l=j1();return k||a.length===0&&!g?null:`
	v234RowActions      = `className:Yi("agy-row-actions absolute top-0 bottom-0 -right-1 pl-6 flex items-center justify-end gap-0.5 z-10",Nc&&"agy-shortcut-masked-actions",!h&&"[&>*]:pointer-events-auto",x?"hidden":Ia?"opacity-100":"can-hover:opacity-0 group-hover:opacity-100 group-focus-visible:opacity-100 group-has-[:focus-visible]:opacity-100 has-[:focus-visible]:opacity-100")`
	v234AutoscrollUnpin = `var H=m.current;if(H){var I=H.scrollTop,J=Math.max(0,H.scrollHeight-H.clientHeight),L=J-I<=a,M=J<t.current&&I>=J-1,N=I-q.current,R=EYb(H),O=J-t.current;O=R||N>0&&O>0&&N<=O+1;var P=p.current;N<0&&!M&&!R?(p.current=!1,oY.delete(H)):N>0&&L&&!y.current&&`
	v234LoopbackPreview = `browserAuxPane:{feature:!1,applyAll:[{runtimeGating:{host:[Qi.Web.Desktop,Qi.Web.Mobile],userCohort:["google"]},feature:gMb},...(a.buildBindings?.liveBrowser?[{runtimeGating:{host:[Qi.Web.Desktop],userCohort:["google"],customFlag:a.buildBindings.liveBrowser.mendelCustomFlag},feature:{engine:"extension",newTabLabel:"New Browser",paneTitle:"Browser",openLinkMenuItem:{id:"open-in-preview",
label:"Open in Side Panel",testId:"link-context-menu-open-in-preview"},content:a.buildBindings.liveBrowser.content}}]:[]),{runtimeGating:{host:[Qi.Electron],customFlag:"enable-hub-builtin-browser"},feature:x?iMb:!1}]},remoteConversationBrowserPane:{feature:!1,applyAll:[{runtimeGating:{host:[Qi.Web.Desktop],userCohort:["google"],customFlag:"enable-remote-conversation-preview"},feature:hMb`
	v234HomeNewConvoRow = `{showNewConversation:a=!0,compact:b=!1,className:c,conciergeTestId:d,surface:f="sidebar"}){var g=j1(),h=yS(),k=NR(),l=BAb(),{config:m,navigate:n,isOpen:p}=Mjc(),q=KR(),t=OR(),u=JAb(),v=R$c("new_conversation_button"),{toggleSidebar:w}=N0(),x=gQ(),y=UR({to:"/"}),z=UR({to:"/customizations"}),A=i1(),C=Nj(),B=CJ()?.conciergeCos?.SidebarButton,D=(C.get("conciergeAgent")?.enabled??!1)&&!!B,G=xP("openNewConversation"),H=yP("openNewConversation"),I=xP("openScheduledTasks"),J=yP("openScheduledTasks"),
L=C.get("plugins")??!1,M=C.get("sidebarItemsFeature"),N=(0,E.useMemo)(()=>(M?.items??[]).filter(X=>!Cna(X)),[M?.items]),R=M0(),O=C.get("extensibility");C=hl(O?.enabled)??!1;O=hl(O?.cloudTriggers?.triggers)??[];O=rbd(O.filter(X=>X.optionId));var P=GK(),Q=qjd().length>0,T=(X,W)=>X&&g&&!h&&f==="sidebar"?E.createElement("kbd",{"aria-hidden":"true",className:Yi("agy-sidebar-shortcut-hint agy-shortcut-keycap font-sans ml-auto -mr-0.5 shrink-0 items-center justify-center","hidden group-hover/sidebar-button:inline-flex group-focus-visible/sidebar-button:inline-flex"),
"data-compact-glyphs":X.includes("+")?void 0:"true","data-testid":W},X):void 0;return a||C||L||N.length>0||D||Q?E.createElement("div",{className:Yi("flex flex-col",b?"gap-0.5":g?"gap-1":"gap-1.5",c),"data-testid":"sidebar-navigation-items"},a&&E.createElement(N6,{icon:E.createElement(Ew,{name:"add"}),variant:g?"outline":"secondary",className:g?"mb-1":void 0,text:"New Conversation",badge:T(H,"new-conversation-shortcut"),href:A(y),"data-testid":"new-conversation-button",onActivate:()=>{v();R&&w()},
selected:!g&&k,`
	v234NewConvoButton = `"New Conversation in Workspace":"New Conversation in Project",onAddClick:wg=>{wg.stopPropagation();ib(dd)},onClick:()=>{ke||rb(dd)},onPointerDown:!ba&&Xe.length>1?wg=>{wg.button===0&&Od(dd,wg)}:void 0,isHovered:Ie(dd),surface:a,archiving:X,isOpen:va===dd,onOpenChange:wg=>{Aa(wg?dd:null)},isPinned:!0,onTogglePin:xc?ad:void 0});var rf=h==="project"||h==="repo",xk=h==="status"&&
!O;if(rf||xk||h==="location"){rf=rf&&Sa.has(dd);let wg,gn;rf&&(wg=M?"New Conversation in Workspace":"New Conversation in Project",gn=Yh=>{Yh.stopPropagation();ib(dd)});Ub={item:Ub,brandedProjectId:dd,projectId:rf?dd:void 0,onAddClick:gn,newChatLabel:wg,onClick:()=>{Hc||rb(dd)},onPointerDown:!ba&&h==="project"&&rf?Yh=>{Yh.button===0&&lc(dd,Yh)}:void 0,isHovered:Ie(dd),surface:a};return rf?E.createElement(tld,{...Ub,projectId:dd,archiving:X,isOpen:va===dd,onOpenChange:Yh=>{Aa(Yh?dd:null)},isPinned:od.has(dd),
onTogglePin:xc?ad:void 0}):E.createElement(rld,{...Ub})}},[h,O,rb,lc,Hc,Jc,Oc,ua,ea,va,uj,ib,X,Sa,a,ba,V,U,M,Mc,aa,ta,ye,od,xc,Xe,ke,Od,ad,Df,aj]),Gl=el(Ub=>Ub.newProject?.isOpen??!1);return E.createElement(vic.Provider,{value:xc},E.createElement(MH,{delay:300,timeout:200},E.createElement("div",{ref:De.sidebarBoundsRef,className:Yi("w-full relative",!b&&"flex-1 min-h-0 h-full")},le?E.createElement(Ajd,null):E.createElement(wkd.Provider,{value:a==="sidebar"&&O?uj:null},E.createElement(n3c,{items:wk,
surface:a,scrollElementRef:b,scrollMargin:c,subtitleContext:qa,onToggleCollapseSection:rb,onToggleSectionHeader:ta,onTogglePagination:Gc,customHeaderRenderer:Tj`
	v234NewConvoView = `const Hld=()=>{var a=IR(),b=g0();return(0,E.useCallback)((c,d)=>{b(f0.map(f=>({trigger:f,ran:!1})));a(c,{project:d})},[a,b])};
var Ild=()=>{var a=j1(),b=Hld(),c=Wj()!==void 0,{q:d}=fR({strict:!1}),f=(0,E.useRef)(null),g=(0,E.useRef)(null),h=(0,E.useRef)(null),[k,l]=(0,E.useState)(0);(0,E.useEffect)(()=>{var m=()=>{var p=h.current?.offsetTop??0;l(q=>q!==p?p:q)};m();if(typeof ResizeObserver!=="undefined"&&g.current){var n=new ResizeObserver(m);n.observe(g.current,{box:"border-box"});return()=>{n.disconnect()}}},[]);H6("MOBILE_HOME_VIEW");return E.createElement("div",{"data-media-drop-scope":"",className:"w-full h-full flex flex-col min-h-0 animate-fade-in"},
E.createElement("div",{ref:f,["data-sidebar-scroll-container"]:"true",className:` + "`" + `flex-1 min-h-0 overflow-y-auto flex flex-col relative ${c?"pb-1":"pb-2"}` + "`" + `},E.createElement("div",{ref:g,className:` + "`" + `shrink-0 flex flex-col empty:hidden ${c?"gap-0.5 pt-1 pb-1":"gap-2 pt-2 pb-3"}` + "`" + `},E.createElement(Fld,{showNewConversation:!1,compact:!0,className:"px-2",conciergeTestId:"mobile-home-concierge",surface:"background"}),!a&&E.createElement(sjd,{surface:"background"}),E.createElement(Dld,{surface:"background"})),
E.createElement("div",{ref:h,className:"shrink-0"},E.createElement(V2c,null,E.createElement(xld,{surface:"background",scrollElementRef:f,scrollMargin:k})))),`
	v234AgentScreen = `fjd=()=>{var a=dl(),b=(0,E.useRef)(null),c=ejd(),{q:d,ws:f,customSubagent:g}=fR({strict:!1}),h=Nj(),k=yS();k=Wj(k)!==void 0;var l=h.get("tipCards"),m=h.get("inputComponent");h=(0,E.useRef)(null);djd();(0,E.useEffect)(()=>{f&&b.current!==f&&(b.current=f,a(rq(!1)))},[f,a]);var {agentScriptsEnabled:n,selectedAgentScriptItem:p,setSelectedAgentScriptItem:q,getAgentScripts:t,globalScriptsDir:u,
workspaceScriptsDir:v}=xec(),{data:w,refetch:x,isLoading:y,error:z}=Gid({fire:t}),A=(0,E.useMemo)(()=>w?.agentCustomizations??[],[w]),C=(0,E.useMemo)(()=>A.filter(D=>!D.agent?.disabled),[A]);Pid({isLoadingAgentScripts:y,agentScriptsError:z,selectedAgentScriptItem:p,enabledCustomizations:C,setSelectedAgentScriptItem:q});Hid({initialCustomSubagent:g,agentCustomizations:A,setSelectedAgentScriptItem:q});var {registerOpenSelector:B}=Fhb();(0,E.useEffect)(()=>{var D=B(()=>{x()});return()=>D.dispose()},
[B,x]);m=Oid({agentScriptsEnabled:n,isLoadingAgentScripts:y,agentScriptsError:z,hideAgentSelectorIfEmpty:m?.hideAgentSelectorIfEmpty,agentCustomizationsCount:A.length,selectedAgentScriptItem:p})?E.createElement(Sid,{agentScriptsEnabled:n,selectedAgentScriptItem:p,setSelectedAgentScriptItem:q,globalScriptsDir:u,workspaceScriptsDir:v,agentCustomizations:A,refreshAgentScripts:x,isLoadingAgentScripts:y,agentScriptsError:z}):void 0;return E.createElement(E.Fragment`
	v234RowsKept = `var FYb=class extends E.Component{constructor(){super(...arguments);this.clampedDelta=0;this.listeningViewport=this.clampedAtScrollTop=null;this.handleViewportScroll=()=>{var a=this.listeningViewport;(!a||this.clampedAtScrollTop===null||Math.abs(a.scrollTop-this.clampedAtScrollTop)>1)&&nY(this)}}componentWillUnmount(){var a=this.props.viewportRef.current;a&&mY.delete(a);nY(this)}getSnapshotBeforeUpdate(a){var b=this.props,c=b.anchorKey,d=b.getAnchorCandidates,f=b.getAnchorId,g=b.shouldYield,h=b.viewportRef.current;
if(a.anchorKey===c)return null;if(b.disabled||!h||g?.())return nY(this),null;(c>a.anchorKey||this.clampedAtScrollTop!==null&&Math.abs(h.scrollTop-this.clampedAtScrollTop)>1)&&nY(this);c=d();h=h.getBoundingClientRect().top;a=[];b=!0;for(let m of c){let {top:n,bottom:p,width:q,height:t}=m.getBoundingClientRect();if(q===0&&t===0)continue;c={element:m,id:f?.(m)??null,offset:n-h};d=b;b=!1;if(p<=h)var k=c;else if(d)var l=c;else if(a.push(c),a.length===3)break}l&&a.push(l);a.length===0&&k&&a.push(k);return a}componentDidUpdate(a,
b,c){if((a=this.props.viewportRef.current)&&c&&c.length!==0){a:{b=this.props.getAnchorCandidates;var d=this.props.getAnchorId;for(var f of c){if(f.element.isConnected){var g={anchor:f,element:f.element};break a}if(f.id!==null&&d){if(!g){g=new Map;for(var h of b()){c=d(h);if(c===null||g.has(c))continue;let {width:k,height:l}=h.getBoundingClientRect();k===0&&l===0||g.set(c,h)}}if(c=g.get(f.id)){g={anchor:f,element:c};break a}}}g=null}g?(f=g.element.getBoundingClientRect().top-a.getBoundingClientRect().top-
g.anchor.offset,g=a.scrollTop,f=g+f+this.clampedDelta,h=Math.max(0,f),Math.abs(h-g)>=.5&&(a.scrollTop=h),h=a.scrollTop,h!==g&&mY.set(a,{targetTop:h,markedAt:performance.now()}),f<-.5?(this.clampedDelta=f-h,this.clampedAtScrollTop=h,this.listeningViewport!==a&&(this.listeningViewport&&this.listeningViewport.removeEventListener("scroll",this.handleViewportScroll),a.addEventListener("scroll",this.handleViewportScroll,{passive:!0}),this.listeningViewport=a)):nY(this)):nY(this)}}render(){return this.props.children}};`
	v234PagingDecision = `var Va=Ja.startIndex,db=Va<Aa.startIndex;if(U(ta,Ja,ua))(db||Qa)&&V(g.timing.oppositeDirectionCooldownMs||
g.timing.retryDelayMs);else{`
	v234UserStatusSyncWhole = `async function I_b({agentService:a,store:b,geminiModelsOnly:c,onTelemetrySettingChanged:d,isDisposed:f,onStatusUnavailable:g,ifFetchFails:h}){var k=e,l=e;try{let p=b.dispatch;try{if(typeof a.getUserStatus!=="function")h==="clearStatus"&&p(rB(ec(F.UserStatusSchema)));else{var {userStatus:m}=l(await k(a.getUserStatus()));if(!f()){if(m){let q=b.getState().userStatus,t=h==="keepStatus"&&H_b(q)?c_a(m):void 0;if(t)p(f_a(t));else if(h==="clearStatus"||!H_b(q)||H_b(m))p(rB(xqa(m,c))),m.email&&d?.(!m.disableTelemetry&&
!m.userDataCollectionForceDisabled)}m?.email&&!m.cascadeModelConfigData?.errorMessage||g?.()}}}catch(q){if(l(),console.warn("[UserStatusSync] Failed to fetch user status:",q),!f()){var n=q instanceof qh&&q.code===7?b_a(q.rawMessage||q.message):void 0;(a=n)?p(f_a(a)):(h==="clearStatus"&&p(rB(ec(F.UserStatusSchema))),g?.())}}}finally{k()}}
function J_b(){var a=cl(),b=ud()?.core.authService;return(0,E.useCallback)(()=>{Xk(a.getState())?.state==="signedIn"&&b?.signOutIfServerHasNoCredential?.()},[a,b])}
function K_b(){var a=cl(),b=(0,E.useContext)(Mz),c=Nj(),d=c.get("models")?.geminiModelsOnly===!0,f=c.get("settings")?.onTelemetrySettingChanged,g=J_b();return(0,E.useCallback)(async()=>{var h=e,k=e;try{if(b){var l=Xk(a.getState());k(await h(I_b({agentService:b,store:a,geminiModelsOnly:d,onTelemetrySettingChanged:f,isDisposed:()=>Xk(a.getState())!==l,onStatusUnavailable:g,ifFetchFails:"keepStatus"})))}}finally{h()}},[a,b,d,f,g])}
function L_b(){var a=dl(),b=cl(),c=(0,E.useContext)(Mz),d=el(Xk),f=ud()?.core.authService,g=em(),h=Nj(),k=h.get("models")?.geminiModelsOnly===!0,l=h.get("settings")?.onTelemetrySettingChanged,m=J_b();(0,E.useEffect)(()=>{if(c)if(d?.state==="signedOut")a(rB(ec(F.UserStatusSchema)));else{var q=!1,t=()=>q;I_b({agentService:c,store:b,geminiModelsOnly:k,onTelemetrySettingChanged:l,isDisposed:t,onStatusUnavailable:m,ifFetchFails:"clearStatus"});var u=setInterval(()=>{I_b({agentService:c,store:b,geminiModelsOnly:k,
onTelemetrySettingChanged:l,isDisposed:t,onStatusUnavailable:m,ifFetchFails:"keepStatus"})},6E5);return()=>{q=!0;clearInterval(u)}}},[a,b,c,d,k,l,m]);}`
)

// 2.34 rewrote or rewrapped what these patches match.
func TestAntigravity234PatchShapes(t *testing.T) {
	mobileOpts := Options{MobileUX: true}
	touch := `Boolean(window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches))`
	for _, c := range []struct {
		id, src string
		opts    Options
		want    []string
		gone    []string
	}{
		// The tab is dropped from a copy of the list under another name.
		{"settings-restore-customizations-tab", v234CustomizationsTab, Options{}, []string{`return qMb}`}, []string{`delete("Customizations")`, `return b}`}},
		// The list of screens is filtered once more.
		{"settings-restore-customizations-screen", v234CustomizationsScreen, Options{}, []string{`d||(l=l.filter(m=>m!=="Jetski Chat"));return l}`}, []string{`"Customizations"`}},
		// A line break after the class string.
		{"mobile-project-header-actions", v234HeaderActions, mobileOpts, []string{`items-center gap-1",(k||u||` + touch + `)?"opacity-100":`}, nil},
		// The touch check is read into a name before the return.
		{"mobile-project-kebab-menu", v234KebabMenu, mobileOpts, []string{`a=ekd(a);var k=yS(),l=j1();return a.length===0&&!g?null:`}, []string{`return k||`}},
		// One more conditional class before the pointer events.
		{"mobile-conversation-row-actions", v234RowActions, mobileOpts, []string{`"agy-shortcut-masked-actions",!h&&"[&>*]:pointer-events-auto",(x||Ia||` + touch + `)?"opacity-100":`}, nil},
		// The hook leaves its own moves alone, and checks the direction before following again.
		{"autoscroll-unpin-needs-input", v234AutoscrollUnpin, Options{}, []string{`N<0&&!M&&!R?(p.current&&!y.current&&window.__agyIsSpontaneousScroll?.(H)?H.scrollTo({top:H.scrollHeight,behavior:"instant"}):(p.current=!1,oY.delete(H))):N>0&&L&&!y.current&&`}, nil},
		// The web hosts are listed in applyAll, with the live browser after them.
		{"loopback-preview-feature-gate", v234LoopbackPreview, Options{}, []string{`browserAuxPane:{feature:!1,applyAll:[{runtimeGating:{host:[Qi.Web.Desktop,Qi.Web.Mobile]},feature:hMb},...(a.buildBindings?.liveBrowser?`}, []string{`feature:gMb}`}},
		// The icon comes first, the variant and the selection depend on the layout, and there is a badge.
		{"mobile-home-new-conversation-row", v234HomeNewConvoRow, mobileOpts, []string{
			`var _agyHome=d==="mobile-home-concierge";_agyHome&&(window.__agyNewRowOnTop=!0);return a||_agyHome||C||L||N.length>0||D||Q?`,
			`(a||_agyHome)&&E.createElement(N6,{icon:E.createElement(Ew,{name:"add"}),variant:_agyHome?"ghost":(g?"outline":"secondary"),className:g?"mb-1":void 0,text:"New Conversation",badge:T(H,"new-conversation-shortcut"),href:_agyHome?void 0:A(y),onClick:_agyHome?`,
			`selected:!_agyHome&&(!g&&k),`,
		}, nil},
		// A line break before the surface.
		{"mobile-new-conversation-button", v234NewConvoButton, mobileOpts, []string{`"header-__agy_new__"`, `items:a==="background"&&!window.__agyNewRowOnTop?`}, nil},
		// More declarations before the address, and the scroll classes in a template.
		{"mobile-new-convo-view", v234NewConvoView, mobileOpts, []string{`{project:d,replace:!0})`, `var _s=fR({strict:!1}),sec=_s?.section||_s?.project,`, `"mobile-new-convo-empty"`}, nil},
		// The function takes an object, the permission error is tested by assigning it, and the poll keeps the status.
		{"user-status-keep-on-error", v234UserStatusSyncWhole, Options{}, []string{
			`var n=q instanceof qh&&q.code===7?b_a(q.rawMessage||q.message):void 0;return (a=n)?p(f_a(a)):(g?.(),!1)}return!1}`,
			`_ok=(await (_init?I_b({agentService:c,store:b,geminiModelsOnly:k,onTelemetrySettingChanged:l,isDisposed:t,onStatusUnavailable:m,ifFetchFails:"clearStatus"}):I_b({agentService:c,`,
			`ifFetchFails:"keepStatus"})))!==!1`,
			`var u=setInterval(()=>{_agyRun(!1)},6E5);`,
		}, []string{`h==="clearStatus"&&p(rB(ec(F.UserStatusSchema))),g?.()`}},
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

// These two build on patches whose code is elsewhere in the bundle, so their patterns are run on their own.
func TestAntigravity234PatternsOnTheirOwn(t *testing.T) {
	// The theme is no longer in Chat Layout.
	got := contextIndicatorSettingInChatRe.ReplaceAllString(v234ContextIndicatorSetting, contextIndicatorSettingInChat)
	if want := `{title:"Chat",settings:["verboseConversationSteps","contextUsageIndicatorEnabled"]},{title:"Chat Layout",settings:["conversationWidth","artifactWidth"]}`; got != want {
		t.Errorf("context-indicator-setting-in-chat gives %q, want %q", got, want)
	}
	// The screen reads whether the keyboard is up before the input's settings.
	loc := agentScreen.re.FindStringSubmatchIndex(v234AgentScreen)
	if loc == nil || !agentScreenIsKnown(submatches([]byte(v234AgentScreen), loc)) {
		t.Fatal("mobile-agent-picker-hook does not take the 2.34 screen")
	}
	got = string(agentScreen.re.ExpandString(nil, mobileAgentPickerHookReplace, v234AgentScreen, loc))
	if want := `__agyPhoneAgentPicker=window.__agyPhoneAgentPicker=function(){var{customSubagent:g}=fR({strict:!1}),m=Nj().get("inputComponent");var {agentScriptsEnabled:n,`; !strings.HasPrefix(got, want) {
		t.Errorf("mobile-agent-picker-hook gives:\n%s\nwant it to start with %q", got, want)
	}
	if !strings.Contains(got, `k=yS();k=Wj(k)!==void 0;var l=h.get("tipCards")`) {
		t.Errorf("the screen's keyboard check is not kept:\n%s", got)
	}
	// The keyboard check is one name, as the start declares it.
	other := strings.Replace(v234AgentScreen, `k=Wj(k)`, `k=Wj(l)`, 1)
	if loc := agentScreen.re.FindStringSubmatchIndex(other); loc != nil && agentScreenIsKnown(submatches([]byte(other), loc)) {
		t.Error("a keyboard check by two names is taken")
	}
}

func TestAntigravity234PatchesTolerateLineBreaks(t *testing.T) {
	mobileOpts := Options{MobileUX: true}
	compact := func(s string) string { return strings.Join(strings.Fields(s), "") }
	for _, c := range []struct {
		id, src string
		opts    Options
	}{
		{"settings-restore-customizations-tab", v234CustomizationsTab, Options{}},
		{"settings-restore-customizations-screen", v234CustomizationsScreen, Options{}},
		{"mobile-project-header-actions", v234HeaderActions, mobileOpts},
		{"mobile-conversation-row-actions", v234RowActions, mobileOpts},
		{"loopback-preview-feature-gate", v234LoopbackPreview, Options{}},
		{"user-status-keep-on-error", v234UserStatusSyncWhole, Options{}},
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

// The Customizations screen's filter is removed only as a statement of its own, so that what it is removed from still
// reads, and only where it filters the list it is assigned to. A bundle that filters in another form is reported.
func TestCustomizationsScreen234TakesOnlyAWholeStatement(t *testing.T) {
	filter := `a.get("plugins")&&(l=l.filter(m=>m!=="Customizations"));`
	for _, other := range []string{
		strings.Replace(v234CustomizationsScreen, filter, `!b&&`+filter, 1),
		strings.Replace(v234CustomizationsScreen, filter, `a.settings.`+filter[2:], 1),
		strings.Replace(v234CustomizationsScreen, filter, `if(b)`+filter, 1),
		strings.Replace(v234CustomizationsScreen, filter, strings.Replace(filter, `(l=l.filter(`, `(l=k.filter(`, 1), 1),
	} {
		if other == v234CustomizationsScreen {
			t.Fatal("nothing was changed")
		}
		if _, status := applyWithStatus(t, "settings-restore-customizations-screen", other, Options{}); status != StatusMissing {
			t.Errorf("status %v, want %v, for %q", status, StatusMissing, other)
		}
	}
}

// The kebab menu's return is taken only where it goes by the touch check read just before it.
func TestKebabMenu234TakesItsOwnCheck(t *testing.T) {
	other := strings.Replace(v234KebabMenu, `return k||`, `return l||`, 1)
	if _, status := applyWithStatus(t, "mobile-project-kebab-menu", other, Options{MobileUX: true}); status == StatusApplied {
		t.Errorf("a return by another name was taken")
	}
}

// The 2.34 rewrite run in node, with the bundle's helpers reduced to what the sync uses: a dropped socket leaves the
// status as it is and is retried soon with the poll's call, which keeps a loaded status, a permission error is shown
// and not retried, and an answer is kept.
func TestUserStatusKeepOnErrorRuntime234(t *testing.T) {
	patched, status := applyWithStatus(t, "user-status-keep-on-error", v234UserStatusSyncWhole, Options{})
	if status != StatusApplied {
		t.Fatalf("status %v", status)
	}
	harness := func(src string) string {
		return `"use strict";
const assert = require("assert");
const e = x => x;
const F = { UserStatusSchema: "schema" }, ec = (schema, init) => ({ ...init }), rB = status => ({ type: "set", status }), xqa = m => m;
class qh extends Error { constructor(code, message) { super(message); this.code = code; this.rawMessage = message; } }
const b_a = m => m, f_a = m => ({ type: "blocked", message: m }), c_a = () => undefined, H_b = s => !!(s && s.loaded);
let answer, current, dispatched = [], timeouts = [], intervals = [], cleanup;
const E = { useEffect: f => { cleanup = f(); }, useCallback: f => f, useContext: () => client };
const client = { getUserStatus: async () => answer() };
const store = { dispatch: a => dispatched.push(a), getState: () => ({ auth: { state: "signedIn" }, userStatus: current }) };
const cl = () => store, dl = () => store.dispatch, Mz = {}, el = sel => sel(store.getState()), Xk = s => s.auth;
const ud = () => ({ core: { authService: { signOutIfServerHasNoCredential: () => {} } } }), em = () => null, Nj = () => ({ get: () => undefined });
// Each copy has timers and a page of its own.
const setTimeout = (f, ms) => (timeouts.push([ms, f]), timeouts.length), clearTimeout = () => {};
const setInterval = (f, ms) => (intervals.push(ms), intervals.length), clearInterval = () => {};
const document = { visibilityState: "visible", addEventListener() {}, removeEventListener() {} };
const window = { addEventListener() {}, removeEventListener() {} };
console.warn = () => {};
` + src + `
const settle = () => new Promise(r => setImmediate(r));
module.exports = async (answerWith, retryWith) => {
  answer = answerWith; current = undefined; dispatched = []; timeouts = []; intervals = [];
  L_b(); await settle(); await settle();
  const first = { dispatched: [...dispatched], timeouts: timeouts.map(([ms]) => ms), intervals: [...intervals] };
  if (retryWith) {
    // The status loads some other way, and the retry is answered with one that is not loaded.
    current = { loaded: true }; dispatched = []; answer = retryWith;
    timeouts[0][1](); await settle(); await settle();
    first.retried = [...dispatched];
  }
  cleanup && cleanup();
  return first;
};
`
	}
	dir := t.TempDir()
	paths := map[string]string{"patched": filepath.Join(dir, "patched.js"), "upstream": filepath.Join(dir, "upstream.js")}
	for name, src := range map[string]string{"patched": harness(patched), "upstream": harness(v234UserStatusSyncWhole)} {
		if err := os.WriteFile(paths[name], []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runNode(t, `"use strict";
const assert = require("assert");
const patched = require(`+strconv.Quote(paths["patched"])+`);
const upstream = require(`+strconv.Quote(paths["upstream"])+`);
(async () => {
  const dropped = () => { throw new Error("socket closed"); };
  // 2.34 as it is: a dropped socket at the first call clears the status.
  let r = await upstream(dropped);
  assert.deepStrictEqual(r.dispatched, [{ type: "set", status: {} }]);
  // Patched: it stays, and the sync is tried again in a second, keeping the status that loaded meanwhile.
  r = await patched(dropped, () => ({ userStatus: { email: "a@b" } }));
  assert.deepStrictEqual(r.dispatched, []);
  assert.deepStrictEqual(r.timeouts, [1000]);
  assert.deepStrictEqual(r.intervals, [6E5]);
  assert.deepStrictEqual(r.retried, [], "the retry clears a loaded status");
  // An answer is kept, and nothing is retried.
  r = await patched(() => ({ userStatus: { email: "a@b" } }));
  assert.deepStrictEqual(r.dispatched, [{ type: "set", status: { email: "a@b" } }]);
  assert.deepStrictEqual(r.timeouts, []);
})().catch(e => { console.error(e); process.exit(1); });
`)
	// A permission error is the server's answer: shown as 2.34 shows it, and not retried.
	runNode(t, harness(patched)+`
(async () => {
  const r = await module.exports(() => { throw new qh(7, "PERMISSION_DENIED: blocked"); });
  assert.deepStrictEqual(r.dispatched, [{ type: "blocked", message: "PERMISSION_DENIED: blocked" }]);
  assert.deepStrictEqual(r.timeouts, [], "no retry for a permission error");
})().catch(e => { console.error(e); process.exit(1); });
`)
}

// 2.34 finds the row to keep by its id where the element was drawn again, and moves the position itself, keeping what
// it could not move above the top. The row it finds is handed to the tracker, and what it would have kept is cleared.
func TestRowsKept234HandsTheMoveToTheTracker(t *testing.T) {
	got := applyOnly(t, "paging-keeps-rows-after-render", v234RowsKept, Options{})
	for _, want := range []string{
		"componentDidUpdate(a,\nb,c){window.__agyKeepRows&&window.__agyRowsDrawn?.(this.props.viewportRef.current,this.props.getAnchorCandidates);if((a=this.props.viewportRef.current)&&c&&c.length!==0){",
		`g=null}window.__agyKeepRows?(g&&window.__agyKeepRows(a,g.element,g.anchor.offset),nY(this)):g?(f=g.element.getBoundingClientRect().top-`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	// A move made some other way is not this code.
	changed := strings.Replace(v234RowsKept, `f=g+f+this.clampedDelta`, `f=g+f`, 1)
	if _, status := applyWithStatus(t, "paging-keeps-rows-after-render", changed, Options{}); status != StatusMissing {
		t.Errorf("a changed move: status %v, want %v", status, StatusMissing)
	}

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	harness := `
const assert = require("assert");
const E = { Component: class {} };
globalThis.window = {};
const mY = new WeakMap();
const Kept = new Function("E", "mY", "nY", process.argv[1] + "return FYb;")(E, mY, k => { k.clampedDelta = 0; k.clampedAtScrollTop = null; k.reset = (k.reset || 0) + 1; });
const rect = (top, h = 40) => ({ getBoundingClientRect: () => ({ top, width: 100, height: h }) });
const viewport = Object.assign(rect(100), { scrollTop: 500, addEventListener() {}, removeEventListener() {} });
const fresh = Object.assign(rect(2460), { isConnected: true, id: "row-7" });
const kept = new Kept();
const rowsOf = () => [fresh];
kept.props = { viewportRef: { current: viewport }, getAnchorCandidates: rowsOf, getAnchorId: el => el.id ?? null };
kept.clampedDelta = 0;
const gone = Object.assign(rect(0), { isConnected: false });

// Without the tracker the bundle finds the row drawn again by its id and moves the position itself.
kept.componentDidUpdate({}, null, [{ element: gone, id: "row-7", offset: 60 }]);
assert.strictEqual(viewport.scrollTop, 2800);

// With it, that row is handed over, the bundle does not move the position, and what it kept for later is cleared.
const calls = [], drawn = [];
window.__agyKeepRows = (...args) => calls.push(args);
window.__agyRowsDrawn = (el, rows) => drawn.push([el, rows]);
kept.clampedDelta = -30;
kept.componentDidUpdate({}, null, [{ element: gone, id: "row-7", offset: 60 }]);
assert.deepStrictEqual(calls, [[viewport, fresh, 60]]);
assert.strictEqual(viewport.scrollTop, 2800, "the bundle's own move is not made as well");
assert.strictEqual(kept.clampedDelta, 0);
// Every drawing is told, with a row to hand over or not.
kept.componentDidUpdate({}, null, null);
assert.deepStrictEqual(drawn, [[viewport, rowsOf], [viewport, rowsOf]]);
assert.strictEqual(calls.length, 1, "nothing to hand over without a snapshot");
`
	if out, err := exec.Command(node, "-e", harness, got).CombinedOutput(); err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
}

// The paging's decision whether to wait holds the trim and the growth above as before.
func TestPagingTrimWaits234(t *testing.T) {
	got := applyOnly(t, "paging-trim-waits-for-still", v234PagingDecision, Options{})
	want := `(db||Qa)&&V(g.timing.oppositeDirectionCooldownMs||` + "\n" + `g.timing.retryDelayMs);else if(!(db||Qa)&&Ja.startIndex>Aa.startIndex&&window.__agyDeferTrim?.())V(250);else if(Ja.startIndex<Aa.startIndex&&window.__agyDeferGrow?.())V(100);else{`
	if !strings.Contains(got, want) {
		t.Errorf("no %q in:\n%s", want, got)
	}
	// The window it would ask for is the one the decision is about, and a bundle with this paging code that the pattern
	// does not fit is reported, not taken as one without it.
	other := strings.Replace(v234PagingDecision, `if(U(ta,Ja,ua))`, `if(U(ta,Xa,ua))`, 1)
	if _, status := applyWithStatus(t, "paging-trim-waits-for-still", v232PagingConfig+other, Options{}); status != StatusMissing {
		t.Errorf("a decision about another window: status %v, want %v", status, StatusMissing)
	}
}
