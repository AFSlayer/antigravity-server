package patches

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The text each patch matches in Antigravity 2.36.0, as the bundle writes it. The settings view is followed in the
// bundle by the desktop settings the patch borrows its hooks from, about 110 KB further on; v236SettingsTail is that part.
const (
	v236NewConvoView     = "const kBd=()=>{var a=RR(),b=Z_();return(0,F.useCallback)((c,d)=>{b(Y_.map(f=>({trigger:f,ran:!1})));a(c,{project:d})},[a,b])};\nvar lBd=()=>{var a=c1(),b=Al(),c=Bl(cFa),d=kBd(),f=uk(),{q:g}=go({strict:!1}),h=(0,F.useRef)(null),[k,l]=(0,F.useState)(null),m=(0,F.useRef)(null),n=(0,F.useRef)(null),[p,q]=(0,F.useState)(0),r=(0,F.useCallback)(v=>{h.current=v;l(v)},[]),t=(0,F.useCallback)(v=>{b(aFa(v))},[b]);gXc({container:k,storedScrollPosition:c,saveScroll:t});(0,F.useEffect)(()=>{var v=()=>{var x=n.current?.offsetTop??0;q(y=>y!==x?x:y)};v();if(typeof ResizeObserver!==\"undefined\"&&m.current){var w=new ResizeObserver(v);w.observe(m.current,\n{box:\"border-box\"});return()=>{w.disconnect()}}},[]);U5(\"MOBILE_HOME_VIEW\");return F.createElement(\"div\",{\"data-media-drop-scope\":\"\",className:\"w-full h-full flex flex-col min-h-0 animate-fade-in\"},F.createElement(\"div\",{ref:r,[\"data-sidebar-scroll-container\"]:\"true\",className:`flex-1 min-h-0 overflow-y-auto flex flex-col relative ${f?\"pb-1\":\"pb-2\"}`},F.createElement(\"div\",{ref:m,className:`shrink-0 flex flex-col empty:hidden ${f?\"gap-0.5 pt-1 pb-1\":\"gap-2 pt-2 pb-3\"}`},F.createElement(iBd,{showNewConversation:!1,\ncompact:!0,className:\"px-2\",conciergeTestId:\"mobile-home-concierge\",surface:\"background\"}),!a&&F.createElement(Hyd,{surface:\"background\"}),F.createElement(gBd,{surface:\"background\"})),F.createElement(\"div\",{ref:n,className:\"shrink-0\"},F.createElement(Ned,null,F.createElement(XAd,{surface:\"background\",scrollElementRef:h,scrollMargin:p})))),"
	v236AgentScreen      = "uyd=({hasHomeWallpaper:a,isHomeWallpaperLoading:b})=>{var c=Al(),d=(0,F.useRef)(null),f=tyd(),{q:g,ws:h,customSubagent:k}=go({strict:!1}),l=lk(),m=pO();m=uk(m);var n=l.get(\"tipCards\"),p=l.get(\"inputComponent\");l=(0,F.useRef)(null);syd();(0,F.useEffect)(()=>{h&&d.current!==h&&(d.current=h,c(gt(!1)))},[h,c]);var {agentScriptsEnabled:q,selectedAgentScriptItem:r,setSelectedAgentScriptItem:t,\nglobalScriptsDir:v,workspaceScriptsDir:w}=byc(),{data:x,refetch:y,isLoading:z,error:A}=ayc(),B=(0,F.useMemo)(()=>x?.agentCustomizations??[],[x]),C=(0,F.useMemo)(()=>B.filter(D=>!D.agent?.disabled),[B]);dyd({isLoadingAgentScripts:z,agentScriptsError:A,selectedAgentScriptItem:r,enabledCustomizations:C,setSelectedAgentScriptItem:t});Wxd({initialCustomSubagent:k,agentCustomizations:B,setSelectedAgentScriptItem:t});p=cyd({agentScriptsEnabled:q,isLoadingAgentScripts:z,agentScriptsError:A,hideAgentSelectorIfEmpty:p?.hideAgentSelectorIfEmpty,\nagentCustomizationsCount:B.length,selectedAgentScriptItem:r})?F.createElement(gyd,{agentScriptsEnabled:q,selectedAgentScriptItem:r,setSelectedAgentScriptItem:t,globalScriptsDir:v,workspaceScriptsDir:w,agentCustomizations:B,refreshAgentScripts:y,isLoadingAgentScripts:z,agentScriptsError:A}):void 0;return F.createElement(F.Fragment"
	v236ConversationList = "\"New Conversation in Workspace\":\"New Conversation in Project\",onAddClick:Bf=>{Bf.stopPropagation();ab(Cc)},onClick:()=>{Zj||tb(Cc)},onPointerDown:!za&&me.length>1?Bf=>{Bf.button===0&&el(Cc,Bf)}:void 0,isHovered:Rd(Cc),surface:a,archiving:ca,isOpen:Ma===Cc,onOpenChange:Bf=>{Va(Bf?Cc:null)},isPinned:!0,onTogglePin:$b?Vb:void 0});var Fe=h===\"project\"||h===\"repo\",fn=h===\"status\"&&!aa;if(Fe||fn||h===\"location\"){Fe=\nFe&&cb.has(Cc);let Bf,Vc;Fe&&(Bf=U?\"New Conversation in Workspace\":\"New Conversation in Project\",Vc=ne=>{ne.stopPropagation();ab(Cc)});Rd={item:Kb,brandedProjectId:Cc,projectId:Fe?Cc:void 0,onAddClick:Vc,newChatLabel:Bf,onClick:()=>{Qo||tb(Cc)},onPointerDown:!za&&h===\"project\"&&Fe?ne=>{ne.button===0&&Gj(Cc,ne)}:void 0,isHovered:Rd(Cc),surface:a};return Fe?F.createElement(UAd,{...Rd,projectId:Cc,archiving:ca,isOpen:Ma===Cc,onOpenChange:ne=>{Va(ne?Cc:null)},isPinned:id.has(Cc),onTogglePin:$b?Vb:void 0}):\nF.createElement(SAd,{...Rd})}}},[h,aa,tb,Gj,Qo,rr,Ro,Ga,Pa,Ma,Yn,ab,ca,cb,a,za,oa,va,U,Ng,ja,ua,Ia,Jb,Dd,en,fl,Bb,Rb,ec,id,$b,me,Zj,el,Vb,Yp,ed,$i,Zd,ze,qe]),Hc=Bl(Kb=>Kb.newProject?.isOpen??!1);return F.createElement(eCc.Provider,{value:$b&&Z===\"project\"},F.createElement(Qed.Provider,{value:xe},F.createElement(jK,{delay:300,timeout:200},F.createElement(\"div\",{ref:Ud.sidebarBoundsRef,className:wj(\"w-full relative\",!b&&\"flex-1 min-h-0 h-full\")},Ec?F.createElement(Pyd,null):F.createElement(Uzd.Provider,\n{value:a===\"sidebar\"&&aa?Yn:null},F.createElement(rfd,{items:Ud.displayedFlatItems,surface:a,emptyMessage:Qc.hideWhenEmpty?\"\":void 0,scrollElementRef:b,scrollMargin:c,subtitleContext:ma,onToggleCollapseSection:tb,onToggleSectionHeader:ua,onTogglePagination:Jc,customHeaderRenderer:Cg"
	v236HomeRows         = "{showNewConversation:a=!0,compact:b=!1,className:c,conciergeTestId:d,surface:f=\"sidebar\"}){var g=c1(),h=pO(),k=SPb(),l=itd(),m=VPb(),n=WPb(),{config:p,navigate:q,isOpen:r}=HDc(),t=TR(),v=WR(),w=iQb(),x=$md(\"new_conversation_button\"),{toggleSidebar:y}=C0(),z=Ql(),A=aS({to:\"/\"}),B=aS({to:\"/customizations\"}),C=b1(),D=lk(),E=jM()?.conciergeCos?.SidebarButton,H=(D.get(\"conciergeAgent\")?.enabled??!1)&&!!E,J=QP(\"openNewConversation\"),I=VU(\"openNewConversation\"),L=QP(\"openScheduledTasks\"),M=\nVU(\"openScheduledTasks\"),N=D.get(\"plugins\")??!1,O=D.get(\"sidebarItemsFeature\"),P=(0,F.useMemo)(()=>(O?.items??[]).filter(aa=>!Moa(aa)),[O?.items]),Q=B0(),S=D.get(\"extensibility\");D=El(S?.enabled)??!1;S=El(S?.cloudTriggers?.triggers)??[];S=Jod(S.filter(aa=>aa.optionId));var R=mN(),U=Fyd().length>0,V=(aa,Z)=>aa&&g&&!h&&f===\"sidebar\"?F.createElement(\"kbd\",{\"aria-hidden\":\"true\",className:wj(\"agy-sidebar-shortcut-hint agy-shortcut-keycap font-sans ml-auto -mr-0.5 shrink-0 items-center justify-center\",\n\"hidden group-hover/sidebar-button:inline-flex group-focus-visible/sidebar-button:inline-flex\"),\"data-compact-glyphs\":aa.includes(\"+\")?void 0:\"true\",\"data-testid\":Z},aa):void 0;U=a||l||D||N||P.length>0||H||U;var X=(0,F.useRef)(null);return U?F.createElement(\"div\",{ref:X,className:wj(\"flex flex-col\",b?\"gap-0.5\":g?\"gap-1\":\"gap-1.5\",c),\"data-testid\":\"sidebar-navigation-items\"},a&&F.createElement(q6,{icon:F.createElement(iz,{name:\"add\"}),variant:g?\"outline\":\"secondary\",text:\"New Conversation\",badge:V(I,\n\"new-conversation-shortcut\"),href:C(A),\"data-testid\":\"new-conversation-button\",onActivate:()=>{x();Q&&y()},selected:!g&&k,"
	v236SettingsView     = "var xec=()=>{var a=mec(),b=Al(),c=lk().settings.isScreenVisible(\"Provide Feedback\"),{sharedNavItems:d,globalScreens:f,workspaces:g,showWorkspacesSection:h}=pec();c=[...d.filter(w=>w!==\"Account\"),...f,...(c?[\"Provide Feedback\"]:[])];var k=TFb(!0),l=Bl(qG),m=Bl(vG),[n,p]=(0,F.useState)(void 0),q=c.includes(l)||d.includes(l);m===void 0&&n!==void 0?p(void 0):m!==void 0&&m!==n&&q&&(p(m),k.path[0]!==l&&k.replace([l]));l=k.path.length;q=l===0;var r=k.path[0]??k.exiting[0],t=r?.startsWith(\"workspace:\"),v=\n()=>{m!==void 0&&b(oG(void 0));k.pop()};return F.createElement(\"div\",{ref:k.setHost,className:\"relative h-full w-full overflow-hidden\"},F.createElement(VFb,{offset:-l,active:q,depth:0,footer:d.includes(\"Account\")?F.createElement(\"div\",{className:\"flex flex-col border-t border-border p-1\"},F.createElement(N2b,{showChevron:!0,className:\"mx-1 px-3 py-2.5\",onClick:()=>{b(mG(\"Account\"));k.push(\"Account\")}})):void 0},F.createElement(vec,{text:\"Settings\"}),c.map(w=>F.createElement(uec,{key:w,text:w,label:a(w),\nonClick:()=>{b(mG(w));k.push(w)}})),h&&F.createElement(F.Fragment,null,F.createElement(vec,{text:\"Workspaces\"}),g.map(w=>F.createElement(uec,{key:w.uri,text:w.name,onClick:()=>{var x=w.uri;b(fbb({workspaceUri:x}));k.push(`workspace:${x}`)}})))),r&&F.createElement(VFb,{offset:1-l,active:l===1,depth:1,exiting:k.exiting.includes(r),onExited:()=>{k.onExited(r)},header:F.createElement(wec,{onBack:v})},F.createElement(O2b.Provider,{value:!0},t?F.createElement(Ybc,null):tec(r,l===1)"
	v236SettingsTail     = "var a=lk(),b=!!a.get(\"settings\")?.hideProjects,c=a.get(\"settingsSearch\")?.enabled??!1,d=mec(),f=Bl(qG),g=Bl(rG),h=po(),{sharedNavItems:k,globalScreens:l,workspaces:m,showWorkspacesSection:n}=pec(),p=Bl(ap),q=El(h.projectsStateProvider)"
	v236TitlebarActions  = "b=oed({cascadeId:a,paneId:b,includeRemoveFromSplit:!1});var ca=(0,F.useContext)(Dtd);ca=a?[]:ca;var ea=c.length>0||b.length>0||X.length>0||A.length>\n0||B.length>0||T||d;return ca.length>0||ea?F.createElement(F.Fragment,null,"
	v236DocRow           = "function $4({title:a,displayPath:b,copyPath:c,description:d,content:f,modalChildren:g,badge:h,actions:k,actionsPaddingClass:l,disabled:m=!1,MarkdownRenderer:n}){var [p,q]=(0,F.useState)(!1),r=XU(p?\"v127\":null,[\"impression\"]),t=!!d||!!f||!!g,v=d||b;"
	v236PlusMenu         = "{icon:Va=>F.createElement(iz,{name:\"attach_file\",size:Va.width?Number(Va.width):14,className:Va.className}),label:\"Files\",onClick:()=>{fa()}}"
	v236WriteIn          = "\"data-testid\":\"ask-question-writein\",value:d,onClick:()=>{O()},onChange:"
	v236UserStatus       = "catch(q){if(l(),console.warn(\"[UserStatusSync] Failed to fetch user status:\",q),!f()){var n=q instanceof Eh&&q.code===7?V3a(q.rawMessage||q.message):void 0;(a=n)?p(Z3a(a)):(h===\"clearStatus\"&&p(MD(ic(G.UserStatusSchema))),g?.())}}}finally{k()}}\nfunction Ghc(){var a=zl(),b=wd()?.core.authService;return(0,F.useCallback)(()=>{vl(a.getState())?.state===\"signedIn\"&&b?.signOutIfServerHasNoCredential?.()},[a,b])}\nfunction Hhc(){var a=zl(),b=(0,F.useContext)(bC),c=lk(),d=c.get(\"models\")?.geminiModelsOnly===!0,f=c.get(\"settings\")?.onTelemetrySettingChanged,g=Ghc();return(0,F.useCallback)(async()=>{var h=e,k=e;try{if(b){var l=vl(a.getState());k(await h(Fhc({agentService:b,store:a,geminiModelsOnly:d,onTelemetrySettingChanged:f,isDisposed:()=>vl(a.getState())!==l,onStatusUnavailable:g,ifFetchFails:\"keepStatus\"})))}}finally{h()}},[a,b,d,f,g])}\nfunction Ihc(){var a=Al(),b=zl(),c=(0,F.useContext)(bC),d=Bl(vl),f=wd()?.core.authService,g=fp(),h=lk(),k=h.get(\"models\")?.geminiModelsOnly===!0,l=h.get(\"settings\")?.onTelemetrySettingChanged,m=Ghc();(0,F.useEffect)(()=>{if(c)if(d?.state===\"signedOut\")a(MD(ic(G.UserStatusSchema))),a(GF.util.resetApiState());else{var q=!1,r=()=>q;Fhc({agentService:c,store:b,geminiModelsOnly:k,onTelemetrySettingChanged:l,isDisposed:r,onStatusUnavailable:m,ifFetchFails:\"clearStatus\"});var t=setInterval(()=>{Fhc({agentService:c,\nstore:b,geminiModelsOnly:k,onTelemetrySettingChanged:l,isDisposed:r,onStatusUnavailable:m,ifFetchFails:\"keepStatus\"})},6E5);return()=>{q=!0;clearInterval(t)}"
	v236SidePanelLegacy  = "Y&&a.kind!==\"customView\"&&H.some(T=>T.id===\"terminal\")&&F.createElement(Gad,{stripRef:N,activeTabId:X}),F.createElement(\"div\",{className:\"flex-grow overflow-hidden\",\"data-side-panel-body\":!0},V&&F.createElement(\"div\",{className:\"h-full w-full\"},F.createElement(Fad,{key:V.id,panelScope:a,tabId:V.id,tab:aa}))"
)

// 2.36 rewrote or extended what these patches match.
func TestAntigravity236PatchShapes(t *testing.T) {
	for _, c := range []struct {
		id, src string
		want    []string
	}{
		// Five hooks before the address is read, and more of the view's own before it draws.
		{"mobile-new-convo-view", v236NewConvoView, []string{`a(c,{project:d,replace:!0})`, `var _s=go({strict:!1}),sec=_s?.section||_s?.project`, `isMobileNew?F.createElement("div",{"data-testid":"mobile-new-convo-empty"`}},
		// The list is told what to say when it is empty.
		{"mobile-new-conversation-button", v236ConversationList, []string{`id:"header-__agy_new__"`, `emptyMessage:Qc.hideWhenEmpty?"":void 0,scrollElementRef:b`}},
		// Whether the rows are drawn is worked out into a name before the return.
		{"mobile-home-new-conversation-row", v236HomeRows, []string{`var _agyHome=d==="mobile-home-concierge";_agyHome&&(window.__agyNewRowOnTop=!0);U=a||_agyHome||l||D||N||P.length>0||H||U;`, `return U?F.createElement("div",{ref:X,`, `(a||_agyHome)&&F.createElement(q6,{icon:F.createElement(iz,{name:"add"}),variant:_agyHome?"ghost":(g?"outline":"secondary")`}},
		// The view opens the screen asked for itself: the dispatch is b, the open screen r, and back is a function.
		{"mobile-settings-projects", v236SettingsView + ")))};function zz(){" + v236SettingsTail, []string{
			`var _pm=po(),_hp=!!lk().get("settings")?.hideProjects,_ps=El(_pm?.projectsStateProvider)`,
			`window.__agyProjectSettingsHost=!!r?.startsWith("project:")`,
			`if(_pid){b({type:"settingsScreen/setContextProjectId",payload:_pid});b({type:"settingsScreen/navigateToProjectScreen",payload:{projectId:_pid}});if(_scr)b(mG(_scr));k.push("project:"+_pid)}`,
			`F.createElement(vec,{text:"Projects"})`,
			`{onBack:()=>{if(r?.startsWith("project:")&&window.__agyProjectSettingsBack?.())return;v()}}`,
			`r?.startsWith("project:")&&window.__agyProjectSettings?F.createElement(window.__agyProjectSettings,null):t?F.createElement(Ybc,null):tec(r,l===1)`,
		}},
		// Three statements between the hook and the return.
		{"mobile-titlebar-delete-modal", v236TitlebarActions, []string{`return ca.length>0||ea?F.createElement(F.Fragment,null,window.__agyDeleteModal&&typeof agyShowDel!=="undefined"?F.createElement(window.__agyDeleteModal,`}},
		// An impression is logged while the row is open.
		{"settings-doc-row-editor", v236DocRow, []string{`var [p,q]=(0,F.useState)(!1),r=XU(p?"v127":null,["impression"]),_R=F,`, `var t=!!d||!!f||!!g,v=d||b;`}},
		// The Files item calls its handler from an arrow function.
		{"composer-upload-menu-item", v236PlusMenu, []string{`label:"Upload File",onClick:()=>window.__agyTriggerUpload&&window.__agyTriggerUpload()}`, `label:"Files",onClick:()=>{fa()}}`}},
		{"question-modal-write-in-focus", v236WriteIn, []string{`onClick:()=>{O()},onFocus:()=>{O()},onChange:`}},
		// Signing out also resets the API cache.
		{"user-status-keep-on-error", v236UserStatus, []string{`a(MD(ic(G.UserStatusSchema))),a(GF.util.resetApiState());else{`}},
		// The terminal hint is not drawn in a custom view.
		{"side-panel-closed-file-viewer-legacy", v236SidePanelLegacy, []string{`Y&&a.kind!=="customView"&&H.some(`, `window.__agyPaneTab?F.createElement(window.__agyPaneTab,{key:V.id,R:F,treeId:a,tab:V,open:Y,idle:null}`}},
	} {
		got, status := applyWithStatus(t, c.id, c.src, Options{MobileUX: true})
		if status != StatusApplied {
			t.Errorf("%s: %v", c.id, status)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: no %q in:\n%s", c.id, w, got)
			}
		}
	}

	// The new conversation screen is handed the wallpaper's state.
	loc := agentScreen.re.FindStringSubmatchIndex(v236AgentScreen)
	if loc == nil || !agentScreenIsKnown(submatches([]byte(v236AgentScreen), loc)) {
		t.Fatal("mobile-agent-picker-hook does not take the 2.36 screen")
	}
	got := string(agentScreen.re.ExpandString(nil, mobileAgentPickerHookReplace, v236AgentScreen, loc))
	if want := `__agyPhoneAgentPicker=window.__agyPhoneAgentPicker=function(){var{customSubagent:k}=go({strict:!1}),p=lk().get("inputComponent");var {agentScriptsEnabled:q,`; !strings.HasPrefix(got, want) {
		t.Errorf("mobile-agent-picker-hook gives:\n%s\nwant it to start with %q", got, want)
	}
	if !strings.Contains(got, `},uyd=({hasHomeWallpaper:a,isHomeWallpaperLoading:b})=>{var c=Al(),`) {
		t.Errorf("the screen's parameters are not kept:\n%s", got)
	}
	// A wallpaper prop the run reads is the screen's, and the run is not taken.
	clash := strings.Replace(v236AgentScreen, `hasHomeWallpaper:a,`, `hasHomeWallpaper:go,`, 1)
	if l := agentScreen.re.FindStringSubmatchIndex(clash); l != nil && agentScreenIsKnown(submatches([]byte(clash), l)) {
		t.Error("a wallpaper prop named like a hook the run reads was taken")
	}
}

// The 2.36 settings view is taken only where its names are the view's own: the stack it draws and reads, the open
// screen it shows, its back function, and the dispatch the Account row calls.
func TestSettingsProjects236Names(t *testing.T) {
	src := v236SettingsView + ")))};function zz(){" + v236SettingsTail
	got, status := applyWithStatus(t, "mobile-settings-projects", src, Options{MobileUX: true})
	if status != StatusApplied {
		t.Fatalf("status %v", status)
	}
	// The view is written once, starting where it started, and is still JavaScript.
	if !strings.HasPrefix(got, "var xec=()=>{") || strings.Contains(got, "var var ") {
		t.Fatalf("the view starts %.40q", got)
	}
	checkSyntax(t, "mobile-settings-projects", strings.TrimSuffix(got, "function zz(){"+v236SettingsTail)+"function zz(){}")
	for name, from := range map[string]string{
		"stack":    `ref:k.setHost`,
		"open":     `exiting.includes(r)`,
		"back":     `{onBack:v}`,
		"dispatch": `onClick:()=>{b(mG("Account"))`,
	} {
		to := map[string]string{
			"stack": `ref:z.setHost`, "open": `exiting.includes(z)`, "back": `{onBack:z}`, "dispatch": `onClick:()=>{z(mG("Account"))`,
		}[name]
		other := strings.Replace(src, from, to, 1)
		if other == src {
			t.Fatalf("%s: nothing was changed", name)
		}
		if _, status := applyWithStatus(t, "mobile-settings-projects", other, Options{MobileUX: true}); status == StatusApplied {
			t.Errorf("another %s was taken", name)
		}
	}
}

// The home's rows are added to the name the condition is stored in only where the return reads that name.
func TestHomeRows236ReadTheirName(t *testing.T) {
	other := strings.Replace(v236HomeRows, `return U?F.createElement("div"`, `return Y?F.createElement("div"`, 1)
	if other == v236HomeRows {
		t.Fatal("nothing was changed")
	}
	if _, status := applyWithStatus(t, "mobile-home-new-conversation-row", other, Options{MobileUX: true}); status == StatusApplied {
		t.Error("a return of another name was taken")
	}
}

// checkSyntax runs code through node's parser, where node is installed.
func checkSyntax(t *testing.T, id, code string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		return
	}
	f := filepath.Join(t.TempDir(), "code.js")
	if err := os.WriteFile(f, []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "--check", f).CombinedOutput(); err != nil {
		t.Errorf("%s: not JavaScript: %v\n%s", id, err, out)
	}
}
