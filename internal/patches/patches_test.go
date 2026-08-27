package patches

import (
	"bytes"
	"os/exec"
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
	"mobile-enter-newline":                      `registerCommand(FE,k=>{if(!k)return!1;k.preventDefault();`,
	"model-effort-submenu":                      ",onClick:()=>{var y=\nv.byEffort.get(w);y&&b(y)}",
	"hide-mic-button":                           `uz.displayName="GutterHoverCommentButton";var vz=(`,
	"hide-user-profile-button":                  `function wmb({className:a=""}={}){return x.createElement("a",{href:"#",onClick:b=>{b.preventDefault()},className:` + "`w-6 h-6 rounded-full overflow-hidden shrink-0 flex items-center justify-center bg-transparent text-muted-foreground ${a}`" + `,"aria-label":"User Profile (Placeholder)"`,
	"sign-in-button":                            `rightElement:x.createElement(tz,{variant:"primary",onClick:()=>` + "\n" + `b.showLoginFlow()},"Sign In")`,
	"mobile-skip-notification-prompt":           `var e=!!this.storageService.get("didAskForNotificationPermission");`,
	"mobile-new-convo-view":                     `const tub=()=>{var a=yM(),b=IT();return(0,x.useCallback)((c,e)=>{b(HT.map(f=>({trigger:f,ran:!1})));a(c,{section:e})},[a,b])};` + "\n" + `var uub=()=>{var a=tub(),{q:b}=dM({strict:!1});return x.createElement("div",{className:"w-full h-full flex flex-col min-h-0 animate-fade-in"},x.createElement("div",{className:"flex-1 min-h-0 overflow-y-auto flex flex-col gap-6 pt-3"},x.createElement(sub,{surface:"background"})),x.createElement("div",{className:"shrink-0 p-2"},x.createElement(e_,{cascadeId:void 0},x.createElement(b_,{conversationId:void 0,isLoading:!1,dropdownPlacement:"top-start",openConversationOptimistically:a,showBottomToolbar:!0,` + "\n" + `aboveContent:x.createElement(s_,null),initialQuery:b}))))};`,
	"mobile-new-convo-header":                   `CM=()=>QL({select:a=>a.location.pathname==="/"})`,
	"mobile-back-clears-section":                `x.createElement(gZ,{iconName:"arrow_back",onClick:()=>c(),"aria-label":"Back to home",dataTestId:"mobile-back-to-home"})`,
	"mobile-project-add-button":                 `if(d==="project"||d==="environment"||d==="status"){let za=B?void 0:d==="project"?"New Conversation in Project":d==="environment"?"New Conversation in Workspace":void 0`,
	"mobile-project-header-actions":             `className:Pm("absolute right-1 top-0 flex h-full items-center gap-1",k||t?"opacity-100":"opacity-0 group-hover/header:opacity-100 group-focus-within/header:opacity-100")`,
	"mobile-project-add-click-close-sidebar":    `onClick:D=>{D.stopPropagation();Skb(D)||(D.preventDefault(),d(D))}`,
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
	"suppress-conversation-unavailable-modal":   `A({tag:"trajectory-not-found",title:"Conversation unavailable",message:"The conversation could not be loaded because its data was not found."})`,
	"force-disable-telemetry":                   `return{telemetryEnabled:f,marketingEmailsEnabled:`,
	"folder-picker-initial-path":                `initialPath:b?b.fsPath:g?"C:/":"/",fetchDirectoryContents:`,
	"composer-upload-menu-item":                 `{icon:ea=>x.createElement(T,{name:"image",size:ea.width?Number(ea.width):14,className:ea.className}),` + "\n" + `label:"Media",onClick:oa}`,
	"file-upload-accept-all":                    `accept:".png,.jpg,.jpeg,.gif,image/png,image/jpeg,image/gif,video/webm,.mp4,video/mp4,.pdf,application/pdf,.txt,text/plain,.csv,text/csv,.json,application/json,.md,text/markdown,.py,text/x-python,.js,.mjs,text/javascript,.ts,.tsx,text/x-typescript,.html,.htm,text/html,.css,text/css",multiple:!0`,
	"file-upload-input-reset":                   `var IRa=({onFilesSelected:a})=>{var b=(0,x.useRef)(null),c=(0,x.useCallback)(e=>{e=e.target;e.files&&a(e.files)},[a]);return{openFileDialog:(0,x.useCallback)(()=>{b.current?.click()},[]),fileInputRef:b,handleFileChange:c}};`,
	"file-upload-custom-text-types":             `function WEa(a,b){b=b.split(";")[0].trim().toLowerCase();if(UEa.includes(b))return b;a=a.slice(a.lastIndexOf(".")+1).toLowerCase();return VEa[a]}`,
	"file-upload-large-file-streaming-fallback": `if(n)if(k.size>1048576)console.error("Text file size exceeds 1MB limit");`,
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
		`window.matchMedia("(pointer:coarse)")`,
		`var vz=function(){return null};var vzDisabled=(`,
		`function wmb(){return null};function wmbDisabled(`,
		`initialPath:"/home/ubuntu/workspace",fetchDirectoryContents:`,
		`Upload File`,
		`accept:"*/*",multiple:!0`,
		`if(t.files&&t.files.length>0)a(t.files);t.value=""`,
		`a==="har"||a==="jsonl"?"application/json":"text/plain"`,
		`window.__agyUpload([k])`,
		`var e=(window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches))||!!this.storageService`,
		`isMobileNew=Boolean((window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches))&&sec)`,
		`!a.location.search?.section`,
		`onClick:()=>c({clearSection:!0})`,
		`if(true){let za=`,
		`className:"absolute right-1 top-0 flex h-full items-center gap-1 opacity-100"`,
		`sidebar-toggle`,
		`className:"relative self-end ml-auto mt-1 flex flex-row items-center p-1 rounded-full opacity-90 pointer-events-auto transition-all bg-transparent user-input-buttons-container"`,
		`className:Pm("absolute top-0 bottom-0 -right-1 pl-6 flex items-center justify-end gap-0.5 z-10",(w||ua||Boolean(window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches)))?"opacity-100":"opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 focus-within:opacity-100")`,
		`handleDelete:agyDel,showDeleteModal:agyShowDel`,
		`L.push({iconName:"delete",tooltip:"Delete",onClick:()=>{agySetShowDel(!0)}})`,
		`window.__agyDeleteModal=({isOpen:a,onClose:b,onDelete:c,showLoadingSpinner:d})=>`,
		`window.__agyDeleteModal?`,
		`data-testid":"conversation-pin-menu-item"`,
		`data-testid":"conversation-archive-menu-item"`,
		`onPinClick:()=>b.handlePin?.(a),isPinned:b.isPinned,onArchiveClick:()=>b.handleArchive?.(a)`,
		`Save Rule 💾`,
		`/__agy/api/rules/save`,
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("missing expected replacement: %s", w)
		}
	}

	if strings.Contains(body, `v.byEffort.get(w);y&&b(y)}`) {
		t.Error("model-effort onClick handler was not removed")
	}
	if !strings.Contains(body, `onClick:()=>{window.location.href="/__agy/signin"}`) {
		t.Error("sign-in button was not redirected")
	}
	if strings.Contains(body, "showLoginFlow()") {
		t.Error("stub showLoginFlow call still wired to the button")
	}
	if strings.Contains(body, `RK({to:"/onboarding"`) {
		t.Error("onboarding redirect was not removed")
	}
}

func TestMobileUXDisabledSkipsMobilePatches(t *testing.T) {
	_, report := Apply(MainJS, syntheticBundle(), Options{MobileUX: false})

	byID := map[string]Result{}
	for _, r := range report {
		byID[r.ID] = r
	}

	for _, id := range []string{
		"mobile-enter-newline", "hide-mic-button", "model-effort-submenu",
		"mobile-skip-notification-prompt",
	} {
		if got := byID[id].Status; got != StatusDisabled {
			t.Errorf("%s: want disabled, got %s", id, got)
		}
	}
	if got := byID["base-url-origin"].Status; got != StatusApplied {
		t.Errorf("base-url-origin: want applied, got %s", got)
	}
	if got := byID["folder-picker-initial-path"].Status; got != StatusDisabled {
		t.Errorf("folder-picker-initial-path: want disabled without workspace root, got %s", got)
	}
}

func TestMissingAnchorIsReported(t *testing.T) {
	_, report := Apply(MainJS, []byte("nothing to see here"), fullOptions())

	if len(report.Missing()) == 0 {
		t.Fatal("expected missing anchors to be reported")
	}
	if len(report.MissingRequired()) != 2 {
		t.Errorf("want 2 missing required patches, got %d", len(report.MissingRequired()))
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
		`id="agy-touch-action"`,
		`id="agy-safe-area"`,
		`id="agy-keyboard-detect"`,
		`id="agy-signin-banner"`,
		`/__agy/api/signin/status`,
		`href="/apple-touch-icon.png"`,
		`src="/main.js?agy=testkey"`,
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("missing %s in output", w)
		}
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
		"mobile-conversation-row-actions":  `className:Pm("absolute top-0 bottom-0 -right-1 pl-6 flex items-center justify-end gap-0.5 z-10",w?"hidden":ua?"opacity-100":"opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 focus-within:opacity-100")`,
		"mobile-kebab-menu-pin-archive":    `const dlb=({cascadeId:a,onDeleteClick:b,onRenameClick:c,onMarkAsReadClick:d,isUnread:f,onViewDebugClick:g})=>G.createElement(HL,{side:"bottom",align:"start",className:"min-w-[180px]",finalFocus:!1},G.createElement(IL,{onClick:c,"data-testid":"conversation-rename-menu-item"},G.createElement(U,{name:"edit",size:16,className:"text-secondary-foreground shrink-0"}),G.createElement("span",null,"Rename")),`,
		"mobile-kebab-wrapper-pin-archive": `var elb=({cascadeId:a,onDeleteClick:b,onRenameClick:c,onMarkAsReadClick:d,isUnread:f,onOpenChange:g,onViewDebugClick:h})=>G.createElement(FL,{onOpenChange:g},G.createElement(GL,{asChild:!0},G.createElement(Ky,{variant:"ghost",size:"icon","aria-label":"More options","data-testid":"conversation-kebab",onClick:k=>void k.stopPropagation()},G.createElement(U,{name:"more_vert",size:16}))),G.createElement(dlb,{cascadeId:a,onDeleteClick:b,onRenameClick:c,onMarkAsReadClick:d,isUnread:f,onViewDebugClick:h}));`,
		"mobile-kebab-call-pin-archive":    `G.createElement(elb,{cascadeId:a,onDeleteClick:()=>{sa(!0)},onRenameClick:hb,onMarkAsReadClick:vb?ja:pa,isUnread:vb,onOpenChange:Ca})`,
	}

	v211Fixtures := map[string]string{
		"mobile-conversation-row-actions":  `className:$l("absolute top-0 bottom-0 -right-1 pl-6 flex items-center justify-end gap-0.5 z-10",v?"hidden":Ka?"opacity-100":"opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 focus-within:opacity-100")`,
		"mobile-kebab-menu-pin-archive":    `const dlb=({cascadeId:a,onDeleteClick:b,onRenameClick:c,onMarkAsReadClick:d,isUnread:f,onViewDebugClick:g})=>G.createElement(HL,{side:"bottom",align:"start",className:"min-w-[180px]",finalFocus:!1},G.createElement(IL,{onClick:c,"data-testid":"conversation-rename-menu-item"},G.createElement(U,{name:"edit",size:16,className:"text-secondary-foreground shrink-0"}),G.createElement("span",null,"Rename")),`,
		"mobile-kebab-wrapper-pin-archive": `var ynb=y.memo(function({cascadeId:a,onDeleteClick:b,onRenameClick:c,onMarkAsReadClick:e,isUnread:f,onOpenChange:g,onViewDebugClick:h}){return y.createElement(PK,{onOpenChange:g},y.createElement(QK,{asChild:!0},y.createElement(CA,{variant:"ghost",size:"icon","aria-label":"More options","data-testid":"conversation-kebab",onClick:k=>void k.stopPropagation()},y.createElement(T,{name:"more_vert",size:16}))),y.createElement(xnb,{cascadeId:a,onDeleteClick:b,onRenameClick:c,onMarkAsReadClick:e,isUnread:f,` + "\n" + `onViewDebugClick:h}))});`,
		"mobile-kebab-call-pin-archive":    `y.createElement(ynb,` + "\n" + `{cascadeId:a,onDeleteClick:Ia,onRenameClick:va,onMarkAsReadClick:tb?ra:ma,isUnread:tb,onOpenChange:Ja})`,
	}

	versions := map[string]map[string]string{
		"2.10.0": v210Fixtures,
		"2.11.0": v211Fixtures,
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
