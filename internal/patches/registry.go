package patches

import (
	"encoding/json"
	"regexp"
)

// Adaptive regular expressions matching across Antigravity 2.8.x, 2.9.x and future minified builds.
var (
	modelEffortRe = regexp.MustCompile(`,?onClick:(?:[a-zA-Z0-9_$]+\?void 0:)?\(\)=>\{var [a-zA-Z0-9_$]+=[\r\n\s]*[a-zA-Z0-9_$]+\.byEffort\.get\([a-zA-Z0-9_$]+\);[a-zA-Z0-9_$]+&&[a-zA-Z0-9_$]+\([a-zA-Z0-9_$]+\)\}`)

	signInButtonRe = regexp.MustCompile(`onClick:\(\)=>[\r\n\s]*(?:\{[\r\n\s]*(?:\w+\(\);[\r\n\s]*)?\w+\.showLoginFlow\(\)[\r\n\s]*\}|\w+\.showLoginFlow\(\))`)

	skipOnboardingRe = regexp.MustCompile(`c\.hasOnboardingScreens&&[a-zA-Z0-9_$]+!==2&&[a-zA-Z0-9_$]+\(\{to:"/onboarding",replace:!0,throw:!0\}\)`)

	mobileEnterNewlineRe                = regexp.MustCompile(`registerCommand\(([a-zA-Z0-9_$]+),k=>\{if\(!k\)return!1;k\.preventDefault\(\);`)
	mobileProjectAddButtonRe            = regexp.MustCompile(`if\((\w+)==="project"\|\|(\w+)==="environment"\|\|(\w+)==="status"\)\{let\s+([a-zA-Z0-9_$]+)=([a-zA-Z0-9_$]+)\?void 0:([a-zA-Z0-9_$]+)==="project"\?"New Conversation in Project":([a-zA-Z0-9_$]+)==="environment"\?"New Conversation in Workspace":[\r\n\s]*void 0`)
	mobileProjectHeaderActionsRe        = regexp.MustCompile(`className:[a-zA-Z0-9_$]+\("absolute right-1 top-0 flex h-full items-center gap-1",([a-zA-Z0-9_$]+)\|\|([a-zA-Z0-9_$]+)\?"opacity-100":"opacity-0 group-hover\/header:opacity-100 group-focus-within\/header:opacity-100"\)`)
	mobileProjectAddClickCloseSidebarRe = regexp.MustCompile(`onClick:([a-zA-Z0-9_$]+)=>[\r\n\s]*\{([a-zA-Z0-9_$]+)\.stopPropagation\(\);([a-zA-Z0-9_$]+)\([a-zA-Z0-9_$]+\)\|\|\([a-zA-Z0-9_$]+\.preventDefault\(\),([a-zA-Z0-9_$]+)\([a-zA-Z0-9_$]+\)\)\}`)
	mobileUserMessageActionsRe          = regexp.MustCompile(`(className:)"absolute bottom-0\.5 right-0\.5 (flex flex-row items-center p-1 rounded-full) opacity-0 pointer-events-none group-hover/user-input-step:opacity-100 group-hover/user-input-step:pointer-events-auto transition-all bg-card user-input-buttons-shadow (user-input-buttons-container)"`)

	mobileTitlebarDeleteHookRe  = regexp.MustCompile(`var\s+\{handleArchive:([a-zA-Z0-9_$]+),handleRestore:([a-zA-Z0-9_$]+),handlePin:([a-zA-Z0-9_$]+),handleUnpin:([a-zA-Z0-9_$]+),[\r\n\s]*isArchiveSupported:([a-zA-Z0-9_$]+),handleShare:([a-zA-Z0-9_$]+),showShareModal:([a-zA-Z0-9_$]+),shareUrl:([a-zA-Z0-9_$]+),handleCloseShareModal:([a-zA-Z0-9_$]+),onShare:([a-zA-Z0-9_$]+)\}=([a-zA-Z0-9_$]+)\(([a-zA-Z0-9_$]+)\?\?""\)`)
	mobileTitlebarDeleteMenuRe  = regexp.MustCompile(`r\&\&\(L\.push\(\{iconName:"edit",tooltip:"Rename",onClick:([a-zA-Z0-9_$]+)\}\)`)
	mobileTitlebarDeleteModalRe = regexp.MustCompile(`(c=[a-zA-Z0-9_$]+\(\{cascadeId:([a-zA-Z0-9_$]+),paneId:([a-zA-Z0-9_$]+),includeRemoveFromSplit:!1\}\);return\s+([a-zA-Z0-9_$]+)\.length>0\|\|([a-zA-Z0-9_$]+)\.length>0\|\|([a-zA-Z0-9_$]+)\.length>0\|\|([a-zA-Z0-9_$]+)\.length>0\?([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+)\.Fragment,null,)`)
	mobileDeleteModalExportRe   = regexp.MustCompile(`(?:var|const)\s+([a-zA-Z0-9_$]+)=(\(\{[^}]*isOpen:a,onClose:b,onDelete:c,showLoadingSpinner:[a-zA-Z0-9_$]+\}\)=>)`)

	mobileKebabMenuPinArchiveRe    = regexp.MustCompile(`(?:const|var)\s+([a-zA-Z0-9_$]+)=\(\{cascadeId:([a-zA-Z0-9_$]+),onDeleteClick:([a-zA-Z0-9_$]+),onRenameClick:([a-zA-Z0-9_$]+),onMarkAsReadClick:([a-zA-Z0-9_$]+),isUnread:([a-zA-Z0-9_$]+),onViewDebugClick:([a-zA-Z0-9_$]+)\}\)=>([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{side:"bottom",align:"start",className:"min-w-\[180px\]",finalFocus:!1\},([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{onClick:([a-zA-Z0-9_$]+),"data-testid":"conversation-rename-menu-item"\},([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{name:"edit",size:16,className:"text-secondary-foreground shrink-0"\}\),([a-zA-Z0-9_$]+)\.createElement\("span",null,"Rename"\)\),`)
	mobileKebabWrapperPinArchiveRe = regexp.MustCompile(`(?:const|var)\s+([a-zA-Z0-9_$]+)=\(\{cascadeId:([a-zA-Z0-9_$]+),onDeleteClick:([a-zA-Z0-9_$]+),onRenameClick:([a-zA-Z0-9_$]+),onMarkAsReadClick:([a-zA-Z0-9_$]+),isUnread:([a-zA-Z0-9_$]+),onOpenChange:([a-zA-Z0-9_$]+),onViewDebugClick:([a-zA-Z0-9_$]+)\}\)=>([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{onOpenChange:([a-zA-Z0-9_$]+)\},([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{asChild:!0\},([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{variant:"ghost",size:"icon","aria-label":"More options","data-testid":"conversation-kebab",onClick:([a-zA-Z0-9_$]+)=>void ([a-zA-Z0-9_$]+)\.stopPropagation\(\)\},([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{name:"more_vert",size:16\}\)\)\),([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{cascadeId:([a-zA-Z0-9_$]+),onDeleteClick:([a-zA-Z0-9_$]+),onRenameClick:([a-zA-Z0-9_$]+),onMarkAsReadClick:([a-zA-Z0-9_$]+),isUnread:([a-zA-Z0-9_$]+),onViewDebugClick:([a-zA-Z0-9_$]+)\}\)\);`)
	mobileKebabCallPinArchiveRe    = regexp.MustCompile(`([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{cascadeId:([a-zA-Z0-9_$]+),onDeleteClick:\(\)=>\{([a-zA-Z0-9_$]+)\(!0\)\},onRenameClick:([a-zA-Z0-9_$]+),onMarkAsReadClick:([^\}]+?),isUnread:([a-zA-Z0-9_$]+(?:\.[a-zA-Z0-9_$]+)?),onOpenChange:([a-zA-Z0-9_$]+)(?:,onViewDebugClick:([a-zA-Z0-9_$]+))?\}\)`)
	mobileHideAuxSidebarRe         = regexp.MustCompile(`([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{iconName:"dock_to_bottom",onClick:[a-zA-Z0-9_$]+,"aria-label":"Toggle Auxiliary Pane",dataTestId:"mobile-toggle-aux-sidebar"\}\)`)
	settingsRulesEditorRe          = regexp.MustCompile(`((?:var|const)\s+([a-zA-Z0-9_$]+)=\(\{name:a,path:b,onCopyPath:c[^\}]*?onEdit:([a-zA-Z0-9_$]+),editTitle:([a-zA-Z0-9_$]+)="Edit",onDelete:([a-zA-Z0-9_$]+),deleteTitle:([a-zA-Z0-9_$]+)="Delete",onToggle:([a-zA-Z0-9_$]+),toggleChecked:([a-zA-Z0-9_$]+),toggleDisabled:([a-zA-Z0-9_$]+)=!1,expandableContent:([a-zA-Z0-9_$]+)\}\)=>\{)(var\s+[a-zA-Z0-9_$]+=[a-zA-Z0-9_$]+\|\|[a-zA-Z0-9_$]+\|\|[a-zA-Z0-9_$]+,\[[a-zA-Z0-9_$]+,[a-zA-Z0-9_$]+\]=\(0,([a-zA-Z0-9_$]+)\.useState\)\(!1\),)`)

	hideMicButtonRe = regexp.MustCompile(`([a-zA-Z0-9_$]+\.displayName="GutterHoverCommentButton";var )([a-zA-Z0-9_$]+)=\(`)

	hideUserProfileRe = regexp.MustCompile(`function [a-zA-Z0-9_$]+\(\{className:a=""\}={}\)\{return [a-zA-Z0-9_$]+\.createElement\("a",\{href:"#",onClick:b=>\{b\.preventDefault\(\)\},className:` + "`w-6 h-6 rounded-full overflow-hidden shrink-0 flex items-center justify-center bg-transparent text-muted-foreground \\${a}`" + `,"aria-label":"User Profile \(Placeholder\)"`)

	mobileSkipNotificationRe = regexp.MustCompile(`var ([a-zA-Z0-9_$]+)=!!this\.storageService\.get\("didAskForNotificationPermission"\);`)

	mobileNewConvoViewRe = regexp.MustCompile(`(?:var|const)\s+([a-zA-Z0-9_$]+)=\(\)=>\{var a=([a-zA-Z0-9_$]+)\(\),b=([a-zA-Z0-9_$]+)\(\);return\(0,([a-zA-Z0-9_$]+)\.useCallback\)\(\(c,([a-zA-Z0-9_$]+)\)=>\{b\(([a-zA-Z0-9_$]+)\.map\(f=>\(\{trigger:f,ran:!1\}\)\)\);a\(c,\{section:([a-zA-Z0-9_$]+)\}\)\},\[a,b\]\)\};[\r\n\s]*(?:var|const)\s+([a-zA-Z0-9_$]+)=\(\)=>\{var a=[a-zA-Z0-9_$]+\(\),\{q:b\}=([a-zA-Z0-9_$]+)\(\{strict:!1\}\);(?:[a-zA-Z0-9_$]+\("MOBILE_HOME_VIEW"\);)?return ([a-zA-Z0-9_$]+)\.createElement\("div",\{className:"w-full h-full flex flex-col min-h-0 animate-fade-in"\},([a-zA-Z0-9_$]+)\.createElement\("div",\{className:"flex-1 min-h-0 overflow-y-auto flex flex-col gap-6 pt-3"\},([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{surface:"background"\}\)\),([a-zA-Z0-9_$]+)\.createElement\("div",\{className:"shrink-0 p-2"\},([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{cascadeId:void 0\},([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{conversationId:void 0,isLoading:!1,dropdownPlacement:"top-start",openConversationOptimistically:a,[\r\n\s]*showBottomToolbar:!0,[\r\n\s]*aboveContent:([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),null\),initialQuery:b\}\)\)\)\)\};`)

	mobileNewConvoHeaderRe = regexp.MustCompile(`([a-zA-Z0-9_$]+)=\(\)=>([a-zA-Z0-9_$]+)\(\{select:a=>a\.location\.pathname==="\/"\}\)`)

	mobileBackClearsSectionRe         = regexp.MustCompile(`([a-zA-Z0-9_$]+\.createElement\([a-zA-Z0-9_$]+,\{iconName:"arrow_back",onClick:\(\)=>)([a-zA-Z0-9_$]+)\(\)(,"aria-label":"Back to home",dataTestId:"mobile-back-to-home"\}\))`)
	suppressConversationUnavailableRe = regexp.MustCompile(`[a-zA-Z0-9_$]+\(\{tag:"trajectory-not-found",title:"Conversation unavailable",message:"The conversation could not be loaded because its data was not found\."\}\)`)
	disableTelemetryRe                = regexp.MustCompile(`return\{telemetryEnabled:[a-zA-Z0-9_$]+,marketingEmailsEnabled:`)

	folderPickerInitialPathRe = regexp.MustCompile(`initialPath:[a-zA-Z0-9_$]+\?[a-zA-Z0-9_$]+\.fsPath:[a-zA-Z0-9_$]+\?"C:/":"/",fetchDirectoryContents:`)

	composerUploadMenuRe = regexp.MustCompile(`\{icon:([a-zA-Z0-9_$]+)=>([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{name:"image",size:[a-zA-Z0-9_$]+\.width\?Number\([a-zA-Z0-9_$]+\.width\):14,className:[a-zA-Z0-9_$]+\.className\}\),[\r\n\s]*label:"Media",onClick:([a-zA-Z0-9_$]+)\}`)

	fileUploadAcceptAllRe = regexp.MustCompile(`accept:"\.png,[^"]+",multiple:!0`)

	fileUploadInputResetRe = regexp.MustCompile(`var\s+([a-zA-Z0-9_$]+)=\(\{onFilesSelected:([a-zA-Z0-9_$]+)\}\)=>\{var\s+([a-zA-Z0-9_$]+)=\(0,([a-zA-Z0-9_$]+)\.useRef\)\(null\),([a-zA-Z0-9_$]+)=\(0,[a-zA-Z0-9_$]+\.useCallback\)\(([a-zA-Z0-9_$]+)=>\{[a-zA-Z0-9_$]+=[a-zA-Z0-9_$]+\.target;[a-zA-Z0-9_$]+\.files\&\&[a-zA-Z0-9_$]+\([a-zA-Z0-9_$]+\.files\)\},\[[a-zA-Z0-9_$]+\]\);return\{openFileDialog:\(0,[a-zA-Z0-9_$]+\.useCallback\)\(\(\)=>\{[a-zA-Z0-9_$]+\.current\?\.click\(\)\},\[\]\),fileInputRef:[a-zA-Z0-9_$]+,handleFileChange:[a-zA-Z0-9_$]+\}\};`)

	fileUploadCustomTextTypesRe = regexp.MustCompile(`function ([a-zA-Z0-9_$]+)\(a,b\)\{b=b\.split\(";"\)\[0\]\.trim\(\)\.toLowerCase\(\);if\(([a-zA-Z0-9_$]+)\.includes\(b\)\)return b;a=a\.slice\(a\.lastIndexOf\("\."\)\+1\)\.toLowerCase\(\);return ([a-zA-Z0-9_$]+)\[a\]\}`)

	fileUploadLargeFileStreamingRe = regexp.MustCompile(`if\(([a-zA-Z0-9_$]+)\)if\(([a-zA-Z0-9_$]+)\.size>1048576\)(?:console\.error\("Text file size exceeds 1MB limit"\);|[a-zA-Z0-9_$]+\?\.\("Text file size exceeds 1MB limit"\),[a-zA-Z0-9_$]+\("validation_check_failed",Error\("Text file size exceeds 1MB limit"\)\);)`)
)

func mobile(o Options) bool { return o.MobileUX }

// All returns every patch in a stable order. Adding a patch here is the only
// step needed: the tests, the doctor report, the control panel and the cache key
// all derive from this list.
func All() []Patch {
	return []Patch{
		// Without this the phone's browser would call https://127.0.0.1:<port>,
		// which resolves to the phone itself. Nothing works until it is fixed.
		{
			ID:       "base-url-origin",
			Desc:     "Point the web app at the browser origin instead of https://127.0.0.1",
			Target:   MainJS,
			Kind:     Literal,
			Required: true,
			Find:     "get baseUrl(){return`https://127.0.0.1:${this.port}`}",
			Replace:  "get baseUrl(){return typeof window!==\"undefined\"?window.location.origin:`https://127.0.0.1:${this.port}`}",
		},
		{
			ID:       "skip-onboarding",
			Desc:     "Skip the desktop onboarding redirect on remote clients",
			Target:   MainJS,
			Kind:     Regexp,
			Required: true,
			FindRe:   skipOnboardingRe,
			Replace:  `return null`,
		},
		// Returning false from the Lexical ENTER command handler lets the editor
		// insert its default newline. There are three registerCommand(FE, ...)
		// call sites; only this one is the message composer.
		{
			ID:      "mobile-enter-newline",
			Desc:    "Enter inserts a newline on touch devices; Cmd/Ctrl+Enter sends",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileEnterNewlineRe,
			Replace: `registerCommand($1,k=>{if(!k)return!1;if((window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches))&&!k.metaKey&&!k.ctrlKey)return!1;k.preventDefault();`,
		},
		// On a desktop the effort submenu opens on hover, so the row's onClick is
		// a convenience that picks the default effort. A tap fires both, closing
		// the popup before the submenu can be used. Removing the handler leaves
		// the submenu reachable.
		{
			ID:      "model-effort-submenu",
			Desc:    "Tapping a model opens its reasoning-effort submenu instead of picking medium",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  modelEffortRe,
			Replace: "",
		},
		// Replacing the component with a function rather than an arrow keeps the
		// anchor "var vz=(" out of the replacement, so the rewrite cannot match
		// its own output.
		{
			ID:      "hide-mic-button",
			Desc:    "Hide the voice-recording button (transcription is unavailable in standalone mode)",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  hideMicButtonRe,
			Replace: `${1}${2}=function(){return null};var ${2}Disabled=(`,
		},
		// The titlebar user profile icon is a dead placeholder in standalone mode (2.8.x; removed upstream in 2.9.x).
		// Replacing the component with a function returning null hides it cleanly.
		{
			ID:       "hide-user-profile-button",
			Desc:     "Hide the non-functional user profile placeholder button on mobile",
			Target:   MainJS,
			Kind:     Regexp,
			Enabled:  mobile,
			Optional: true,
			FindRe:   hideUserProfileRe,
			Replace:  `function wmb(){return null};function wmbDisabled({className:a=""}={}){return x.createElement("a",{href:"#",onClick:b=>{b.preventDefault()},className:` + "`w-6 h-6 rounded-full overflow-hidden shrink-0 flex items-center justify-center bg-transparent text-muted-foreground ${a}`" + `,"aria-label":"User Profile (Placeholder)"`,
		},
		// Google's standalone build cannot sign in from a browser: its auth service
		// is a stub, and its OAuth client only accepts loopback redirect URIs. Point
		// the button at a page that can actually complete the flow instead of
		// leaving it dead.
		{
			ID:      "sign-in-button",
			Desc:    "Make the Settings > Account sign-in button work over the network",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  signInButtonRe,
			Replace: `onClick:()=>{window.location.href="/__agy/signin"}`,
		},

		// The prompt is an in-app banner shown once when notificationPermission is
		// still "default". Making the "have we asked yet" flag read as true on
		// touch devices skips it without touching the granted path, so a desktop
		// browser can still turn notifications on.
		{
			ID:      "mobile-skip-notification-prompt",
			Desc:    "Skip the Enable Notifications banner on touch devices",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileSkipNotificationRe,
			Replace: `var $1=(window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches))||!!this.storageService.get("didAskForNotificationPermission");`,
		},

		// When a project or standalone section is selected on touch devices (e.g. via + button),
		// show the empty conversation view and composer directly rather than the full list.
		// Uses replace:true for the optimistic conversation creation so history back returns to the list.
		{
			ID:      "mobile-new-convo-view",
			Desc:    "Show the empty conversation composer view on touch devices when a project is selected",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileNewConvoViewRe,
			Replace: `var ${1}=()=>{var a=${2}(),b=${3}();return(0,${4}.useCallback)((c,${5})=>{b(${6}.map(f=>({trigger:f,ran:!1})));a(c,{section:${5},replace:!0})},[a,b])};var ${8}=()=>{var a=${1}(),{q:b,section:sec}=${9}({strict:!1}),isMobileNew=Boolean((window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches))&&sec);return ${10}.createElement("div",{className:"w-full h-full flex flex-col min-h-0 animate-fade-in"},isMobileNew?${10}.createElement("div",{className:"flex-1 min-h-0 flex flex-col items-center justify-center gap-3 select-none"},${10}.createElement("span",{className:"text-xs text-muted-foreground/60"},"Start a new conversation")):${10}.createElement("div",{className:"flex-1 min-h-0 overflow-y-auto flex flex-col gap-6 pt-3"},${10}.createElement(${13},{surface:"background"})),${10}.createElement("div",{className:"shrink-0 p-2"},${10}.createElement(${16},{cascadeId:void 0},${10}.createElement(${18},{conversationId:void 0,isLoading:!1,dropdownPlacement:"top-start",openConversationOptimistically:a,showBottomToolbar:!0,aboveContent:${10}.createElement(${20},null),initialQuery:b}))))};`,
		},

		// On mobile, show the back button in the main titlebar when a project is selected
		// and ensure back navigation always clears the active section.
		{
			ID:      "mobile-new-convo-header",
			Desc:    "Show the back button in the titlebar when in new conversation mode and clear section on back",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileNewConvoHeaderRe,
			Replace: `$1=()=>$2({select:a=>a.location.pathname==="/"&&!a.location.search?.section})`,
		},
		{
			ID:      "mobile-back-clears-section",
			Desc:    "Ensure the mobile back button always clears the selected section to return to the root conversation list",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileBackClearsSectionRe,
			Replace: `${1}${2}({clearSection:!0})${3}`,
		},
		{
			ID:      "mobile-project-add-button",
			Desc:    "Always show project New Conversation button on mobile touch devices without hovering",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileProjectAddButtonRe,
			Replace: `if(true){let $4=$6==="project"?"New Conversation in Project":$7==="environment"?"New Conversation in Workspace":void 0`,
		},
		{
			ID:      "mobile-project-header-actions",
			Desc:    "Always show project header actions (add conversation button and menu) on touch devices",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileProjectHeaderActionsRe,
			Replace: `className:"absolute right-1 top-0 flex h-full items-center gap-1 opacity-100"`,
		},
		{
			ID:      "mobile-project-add-click-close-sidebar",
			Desc:    "Automatically collapse mobile sidebar drawer when clicking + new conversation on mobile",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileProjectAddClickCloseSidebarRe,
			Replace: `onClick:${1}=>{${2}.stopPropagation();${3}(${2})||(function(){try{if(window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches)){var _sb=document.querySelector('[data-testid="sidebar-toggle"], [data-testid="mobile-toggle-sidebar"]');if(_sb&&window.getComputedStyle(_sb).display!=="none"){_sb.click();}}}catch(e){}}(),${2}.preventDefault(),${4}(${2}))}`,
		},
		{
			ID:      "mobile-user-message-actions",
			Desc:    "Make user message action buttons (Undo icon and copy) visible on touch devices",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileUserMessageActionsRe,
			Replace: `${1}"relative self-end ml-auto mt-1 ${2} opacity-90 pointer-events-auto transition-all bg-transparent ${3}"`,
		},
		{
			ID:      "mobile-titlebar-delete-hook",
			Desc:    "Expose conversation deletion handlers in the titlebar more-actions menu",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileTitlebarDeleteHookRe,
			Replace: `var {handleArchive:$1,handleRestore:$2,handlePin:$3,handleUnpin:$4,isArchiveSupported:$5,handleDelete:agyDel,showDeleteModal:agyShowDel,setShowDeleteModal:agySetShowDel,showLoadingSpinner:agyDelSpin,handleCloseDeleteModal:agyCloseDel,handleShare:$6,showShareModal:$7,shareUrl:$8,handleCloseShareModal:$9,onShare:$10}=$11($12??"")`,
		},
		{
			ID:      "mobile-titlebar-delete-menu",
			Desc:    "Add Delete option to the titlebar more-actions dropdown menu",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileTitlebarDeleteMenuRe,
			Replace: `r&&(L.push({iconName:"edit",tooltip:"Rename",onClick:$1}),L.push({iconName:"delete",tooltip:"Delete",onClick:()=>{agySetShowDel(!0)}})`,
		},
		{
			ID:      "mobile-delete-modal-export",
			Desc:    "Export conversation delete modal component to global window for adaptive titlebar rendering",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileDeleteModalExportRe,
			Replace: `var $1=window.__agyDeleteModal=$2`,
		},
		{
			ID:      "mobile-titlebar-delete-modal",
			Desc:    "Render conversation delete confirmation modal in titlebar component",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileTitlebarDeleteModalRe,
			Replace: `${1}window.__agyDeleteModal?${8}.createElement(window.__agyDeleteModal,{isOpen:agyShowDel,onClose:agyCloseDel,onDelete:function(){agySetShowDel(!1);try{if(r)agyDel()}catch(e){}var _b=document.querySelector('[data-testid="mobile-back-to-home"]');if(_b){_b.click()}else{try{window.history.replaceState(null,"","/");window.dispatchEvent(new PopStateEvent("popstate"))}catch(e){window.location.replace("/")}}},showLoadingSpinner:agyDelSpin}):null,`,
		},
		{
			ID:      "mobile-kebab-menu-pin-archive",
			Desc:    "Add Pin and Archive actions into conversation kebab dropdown menu",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileKebabMenuPinArchiveRe,
			Replace: `const $1=({cascadeId:$2,onDeleteClick:$3,onRenameClick:$4,onMarkAsReadClick:$5,isUnread:$6,onViewDebugClick:$7,onPinClick:agyPin,isPinned:agyIsPinned,onArchiveClick:agyArchive})=>$8.createElement($9,{side:"bottom",align:"start",className:"min-w-[180px]",finalFocus:!1},$10.createElement($11,{onClick:$12,"data-testid":"conversation-rename-menu-item"},$13.createElement($14,{name:"edit",size:16,className:"text-secondary-foreground shrink-0"}),$15.createElement("span",null,"Rename")),agyPin&&$8.createElement($11,{onClick:agyPin,"data-testid":"conversation-pin-menu-item"},$13.createElement($14,{name:agyIsPinned?"keep_off":"keep",size:16,className:"text-secondary-foreground shrink-0"}),$15.createElement("span",null,agyIsPinned?"Unpin":"Pin")),agyArchive&&$8.createElement($11,{onClick:agyArchive,"data-testid":"conversation-archive-menu-item"},$13.createElement($14,{name:"archive",size:16,className:"text-secondary-foreground shrink-0"}),$15.createElement("span",null,"Archive")),`,
		},
		{
			ID:      "mobile-kebab-wrapper-pin-archive",
			Desc:    "Pass pin and archive props through conversation kebab wrapper component",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileKebabWrapperPinArchiveRe,
			Replace: `var $1=({cascadeId:$2,onDeleteClick:$3,onRenameClick:$4,onMarkAsReadClick:$5,isUnread:$6,onOpenChange:$7,onViewDebugClick:$8,onPinClick:agyPin,isPinned:agyIsPinned,onArchiveClick:agyArchive})=>$9.createElement($10,{onOpenChange:$11},$12.createElement($13,{asChild:!0},$14.createElement($15,{variant:"ghost",size:"icon","aria-label":"More options","data-testid":"conversation-kebab",onClick:$16=>void $17.stopPropagation()},$18.createElement($19,{name:"more_vert",size:16}))),$20.createElement($21,{cascadeId:$22,onDeleteClick:$23,onRenameClick:$24,onMarkAsReadClick:$25,isUnread:$26,onViewDebugClick:$27,onPinClick:agyPin,isPinned:agyIsPinned,onArchiveClick:agyArchive}));`,
		},
		{
			ID:      "mobile-kebab-call-pin-archive",
			Desc:    "Supply pin and archive handlers to conversation kebab button call in history list",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileKebabCallPinArchiveRe,
			Replace: `$1.createElement($2,{cascadeId:$3,onDeleteClick:()=>{$4(!0)},onRenameClick:$5,onMarkAsReadClick:$6,isUnread:$7,onOpenChange:$8,onPinClick:()=>b.handlePin?.(a),isPinned:b.isPinned,onArchiveClick:()=>b.handleArchive?.(a)})`,
		},
		{
			ID:      "mobile-hide-aux-sidebar",
			Desc:    "Hide unclickable auxiliary sidebar toggle icon on mobile navigation bar",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileHideAuxSidebarRe,
			Replace: `null`,
		},
		{
			ID:      "settings-rules-editor",
			Desc:    "Enable inline editor and save button for rules in Settings Customizations view",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: func(Options) bool { return true },
			FindRe:  settingsRulesEditorRe,
			Replace: `${1}var _R=${12},[agyEdit,agySetEdit]=_R.useState(!1),[agyTxt,agySetTxt]=_R.useState(""),[agySave,agySetSave]=_R.useState(!1),[agyDone,agySetDone]=_R.useState(!1),[agySavedDesc,agySetSavedDesc]=_R.useState(null);if(agySavedDesc!==null)e=agySavedDesc;var agyDoEdit=${3}||(b?async()=>{if(agyEdit){agySetEdit(!1);return;}try{let res=await fetch("/__agy/api/rules/read?path="+encodeURIComponent(b));if(res.ok){let json=await res.json();agySetTxt(json.content||"");agySetEdit(!0);}}catch(e){}}:void 0);var agyDoSave=async()=>{agySetSave(!0);try{let res=await fetch("/__agy/api/rules/save",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({path:b,content:agyTxt})});if(res.ok){agySetDone(!0);var _desc=agyTxt.length>120?agyTxt.slice(0,120)+"\u2026":agyTxt;agySetSavedDesc(_desc);setTimeout(()=>{agySetDone(!1);agySetEdit(!1)},1000);}}catch(e){}finally{agySetSave(!1)}};${3}=agyDoEdit;var agyEditorNode=agyEdit?_R.createElement("div",{className:"w-full mt-2 pt-2 border-t border-border flex flex-col gap-2"},_R.createElement("textarea",{value:agyTxt,onChange:e=>agySetTxt(e.target.value),placeholder:"Write rule markdown instructions...",className:"w-full font-mono text-xs p-2.5 rounded-lg border border-border bg-muted/40 focus:outline-none focus:ring-1 focus:ring-primary min-h-[220px] max-h-[500px] resize-y text-foreground leading-relaxed",spellCheck:!1}),_R.createElement("div",{className:"flex items-center justify-end gap-2"},_R.createElement("button",{type:"button",onClick:()=>agySetEdit(!1),disabled:agySave,className:"text-xs h-7 px-3 rounded border border-border bg-muted/40 hover:bg-muted text-muted-foreground"},"Cancel"),_R.createElement("button",{type:"button",onClick:agyDoSave,disabled:agySave,className:"text-xs h-7 px-3 rounded flex items-center gap-1 font-medium bg-primary text-primary-foreground hover:bg-primary/90"},agySave?"Saving...":(agyDone?"Saved ✓":"Save Rule 💾")))):null;var agyExp=${10}?_R.createElement(_R.Fragment,null,${10},agyEditorNode):agyEditorNode;${10}=agyExp;${11}`,
		},
		{
			ID:      "suppress-conversation-unavailable-modal",
			Desc:    "Suppress annoying Conversation unavailable popup when navigating away from deleted conversations",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: func(Options) bool { return true },
			FindRe:  suppressConversationUnavailableRe,
			Replace: `void 0`,
		},
		{
			ID:      "force-disable-telemetry",
			Desc:    "Force telemetry setting to be disabled by default in Settings and user settings state",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: func(Options) bool { return true },
			FindRe:  disableTelemetryRe,
			Replace: `return{telemetryEnabled:!1,marketingEmailsEnabled:`,
		},

		// Always start the folder picker at the configured workspace root instead of
		// falling back to homeDirUri (b).
		{
			ID:     "folder-picker-initial-path",
			Desc:   "Start the folder picker at the configured workspace root",
			Target: MainJS,
			Kind:   Regexp,
			Enabled: func(o Options) bool {
				return o.WorkspaceRoot != ""
			},
			FindRe: folderPickerInitialPathRe,
			ReplaceFn: func(o Options) string {
				return `initialPath:` + jsString(o.WorkspaceRoot) + `,fetchDirectoryContents:`
			},
		},
		{
			ID:      "workspace-file-uploader",
			Desc:    "Inject client-side asynchronous streaming file uploader and progress UI",
			Target:  HTML,
			Kind:    InjectHead,
			Replace: uploaderScript,
		},
		{
			ID:      "composer-upload-menu-item",
			Desc:    "Add Upload File menu item to the composer plus menu",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  composerUploadMenuRe,
			Replace: `{icon:$1=>$2.createElement($3,{name:"attach_file",size:$1.width?Number($1.width):14,className:$1.className}),label:"Upload File",onClick:()=>window.__agyTriggerUpload&&window.__agyTriggerUpload()},{icon:$1=>$2.createElement($3,{name:"image",size:$1.width?Number($1.width):14,className:$1.className}),label:"Media",onClick:$4}`,
		},
		{
			ID:      "file-upload-accept-all",
			Desc:    "Allow selecting any file type in the composer attachment dialog",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  fileUploadAcceptAllRe,
			Replace: `accept:"*/*",multiple:!0`,
		},
		{
			ID:      "file-upload-input-reset",
			Desc:    "Ensure file input is reset after selection so selecting the same file triggers onChange",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  fileUploadInputResetRe,
			Replace: `var $1=({onFilesSelected:$2})=>{var $3=(0,$4.useRef)(null),$5=(0,$4.useCallback)($6=>{var t=$6.target;if(t.files&&t.files.length>0)$2(t.files);t.value=""},[$2]);return{openFileDialog:(0,$4.useCallback)(()=>{if($3.current)$3.current.value="";$3.current?.click()},[]),fileInputRef:$3,handleFileChange:$5}};`,
		},
		{
			ID:      "file-upload-custom-text-types",
			Desc:    "Allow non-standard text and data files like .har to be attached as text/plain or application/json",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  fileUploadCustomTextTypesRe,
			Replace: `function $1(a,b){b=b.split(";")[0].trim().toLowerCase();if($2.includes(b))return b;a=a.slice(a.lastIndexOf(".")+1).toLowerCase();return $3[a]||(b.startsWith("image/")||b.startsWith("video/")||b==="application/pdf"?void 0:a==="har"||a==="jsonl"?"application/json":"text/plain")}`,
		},
		{
			ID:      "file-upload-large-file-streaming-fallback",
			Desc:    "Stream large files exceeding 1MB to the workspace asynchronously with progress UI",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  fileUploadLargeFileStreamingRe,
			Replace: `if($1)if($2.size>1048576){if(window.__agyUpload){window.__agyUpload([$2]);return;}}`,
		},

		{
			ID:      "app-icons",
			Desc:    "Serve the official Antigravity favicon and home-screen icon",
			Target:  HTML,
			Kind:    InjectHead,
			Replace: appIcons,
		},
		{
			ID:      "touch-action",
			Desc:    "Remove the 300ms tap delay and tap highlight on controls",
			Target:  HTML,
			Kind:    InjectHead,
			Replace: touchAction,
		},
		{
			ID:      "safe-area-insets",
			Desc:    "Keep the composer and toasts clear of the iOS home bar",
			Target:  HTML,
			Kind:    InjectHead,
			Enabled: mobile,
			Replace: safeArea,
		},
		{
			ID:      "keyboard-detect",
			Desc:    "Collapse the safe-area gap while the on-screen keyboard is open",
			Target:  HTML,
			Kind:    InjectHead,
			Enabled: mobile,
			Replace: keyboardDetect,
		},
		// A phone has no console, and the shell's geometry during the keyboard
		// animation is the only thing that explains the remaining layout bugs.
		// Off unless AGY_DEBUG is set.
		{
			ID:      "mobile-debug",
			Desc:    "Record the viewport and shell geometry around every keyboard event",
			Target:  HTML,
			Kind:    InjectHead,
			Enabled: func(o Options) bool { return o.Debug },
			Replace: mobileDebug,
		},
		{
			ID:      "mobile-signin-banner",
			Desc:    "Show a sign-in prompt on touch devices, which Antigravity omits there",
			Target:  HTML,
			Kind:    InjectHead,
			Enabled: mobile,
			Replace: signInBanner,
		},
		{
			ID:     "cache-bust",
			Desc:   "Invalidate cached bundles when the applied patch set changes",
			Target: HTML,
			Kind:   Literal,
			Find:   `src="/main.js"`,
			ReplaceFn: func(o Options) string {
				if o.CacheKey == "" {
					return `src="/main.js"`
				}
				return `src="/main.js?agy=` + o.CacheKey + `"`
			},
		},
	}
}

func jsString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

const appIcons = `<link rel="icon" type="image/x-icon" href="/favicon.ico">
<link rel="apple-touch-icon" href="/apple-touch-icon.png">`

const touchAction = `<style id="agy-touch-action">
button,input,textarea,select{touch-action:manipulation;-webkit-tap-highlight-color:transparent}
input,textarea,select{font-size:16px !important}
</style>`

const safeArea = `<style id="agy-safe-area">
html,
body {
  position: fixed !important;
  inset: 0 !important;
  width: 100% !important;
  height: 100% !important;
  overflow: hidden !important;
  overscroll-behavior: none !important;
  margin: 0 !important;
  padding: 0 !important;
}

@supports (padding-bottom: env(safe-area-inset-bottom)) {
  .relative.w-screen.h-\[100dvh\] {
    position: fixed !important;
    inset: 0 !important;
    width: 100vw !important;
    height: 100% !important;
    max-height: 100% !important;
    padding: 0 !important;
    overflow: hidden !important;
  }
  div.h-\[100dvh\].w-screen.flex.flex-col {
    position: absolute !important;
    top: 0 !important;
    left: 0 !important;
    right: 0 !important;
    bottom: var(--agy-bottom, 0px) !important;
    height: auto !important;
    max-height: none !important;
    padding-top: 0 !important;
    padding-bottom: 0 !important;
    box-sizing: border-box !important;
  }
  div.shrink-0.p-2 {
    padding: 0.25rem 0.5rem 0 0.5rem !important;
  }
  /* When keyboard is active, collapse the safe-area inset on the input box so it hugs the keyboard tightly */
  body.agy-kb-open [data-testid="agent-input-box"],
  html[style*="--agy-bottom"] [data-testid="agent-input-box"],
  body.agy-kb-open .shrink-0.p-2,
  html[style*="--agy-bottom"] .shrink-0.p-2 {
    padding-bottom: 0px !important;
  }
  /* Flow user message action buttons (Undo, Copy, Timestamp) naturally without overlapping message text */
  div[data-testid="user-input-step"] div.bg-card,
  .group\/user-input-step div.bg-card {
    display: flex !important;
    flex-direction: column !important;
    align-items: stretch !important;
    overflow: visible !important;
    position: relative !important;
  }
  div[data-testid="user-input-step"] .user-input-buttons-container,
  .group\/user-input-step .user-input-buttons-container,
  .user-input-buttons-container {
    position: relative !important;
    top: auto !important;
    bottom: auto !important;
    left: auto !important;
    right: auto !important;
    margin-left: auto !important;
    margin-top: 0.25rem !important;
    align-self: flex-end !important;
    flex-shrink: 0 !important;
    opacity: 0.85 !important;
    pointer-events: auto !important;
    background: transparent !important;
    box-shadow: none !important;
    padding: 0 !important;
    display: inline-flex !important;
    flex-direction: row !important;
    align-items: center !important;
    gap: 0.25rem !important;
  }
  @media (pointer: coarse), (max-width: 768px) {
    /* Clean mobile conversation row: align kebab menu and timestamp side by side without overlapping */
    div[data-testid^="conversation-row-"] div.absolute.top-0 {
      position: relative !important;
      top: auto !important;
      bottom: auto !important;
      right: auto !important;
      padding-left: 0 !important;
      opacity: 1 !important;
      background: transparent !important;
    }
    div[data-testid^="conversation-row-"] [data-testid="conversation-pin-button"],
    div[data-testid^="conversation-row-"] [data-testid="conversation-archive-button"],
    div[data-testid^="conversation-row-"] [data-testid="conversation-restore-button"],
    div[data-testid^="conversation-row-"] [data-testid="conversation-delete-button"] {
      display: none !important;
    }
  }
  .aux-drawer-popup {
    padding-bottom: var(--agy-bottom, env(safe-area-inset-bottom, 0px)) !important;
  }
  .fixed.bottom-3 {
    bottom: calc(0.75rem + var(--agy-bottom, 0px)) !important;
  }
}
</style>`

const keyboardDetect = `<script id="agy-keyboard-detect">
(function () {
  var vv = window.visualViewport;
  if (!vv) return;

  // The messages live in a scroller nested inside the conversation view, which is
  // itself never taller than its content. Searching the subtree keeps the home
  // screen's history list out of it -- scrolling that one threw its virtualised
  // sticky headers off -- without depending on which element is the scroller.
  function chatScroller() {
    var root = document.querySelector('[data-testid="conversation-view"]');
    if (!root) return null;
    if (root.scrollHeight > root.clientHeight + 20) return root;

    var nodes = root.querySelectorAll("*");
    for (var i = 0; i < nodes.length && i < 400; i++) {
      var el = nodes[i];
      if (el.scrollHeight > el.clientHeight + 20 &&
          /auto|scroll/.test(getComputedStyle(el).overflowY)) {
        return el;
      }
    }
    return null;
  }

  function scrollChatToBottom() {
    var el = chatScroller();
    if (el) el.scrollTop = el.scrollHeight;
  }

  // html is position:fixed, so clientHeight is the layout viewport and does not
  // move with Safari's toolbar the way innerHeight can.
  function base() {
    return document.documentElement.clientHeight || window.innerHeight;
  }

  // Safari reveals the focused composer by panning the layout viewport, and it
  // reports that pan as a document scroll even here, where the document is fixed
  // and has nothing to scroll. Fixed elements move with it, so the whole shell
  // slides off the top of the screen until the offset is put back. Undoing it
  // once, mid-animation, is what made the shell lurch; doing it every frame keeps
  // the offset from ever being on screen for longer than one frame.
  function unpan() {
    var de = document.documentElement;
    if (window.scrollY === 0 && de.scrollTop === 0) return;
    window.scrollTo(0, 0);
    if (de.scrollTop !== 0) de.scrollTop = 0;
  }

  // The keyboard slides up over roughly this long, while visualViewport reports
  // its final height in a single step at the start.
  var OPEN_MS = 250;

  // Safari decides whether to pan about 40-80ms after focusin, before it reports
  // the new viewport height. Shrinking the shell to the height the keyboard had
  // last time gets the composer out of the way first, so there is no pan to
  // undo -- undoing one races Safari's own animation, which is what made the
  // shell lurch. The measurement is kept across page loads because the first
  // focus of a session is the one with nothing to go on.
  var predicted = 0;
  try {
    predicted = parseInt(localStorage.getItem("agy-kb"), 10) || 0;
    if (predicted < 100) predicted = 0;
  } catch (e) {}
  var holdUntil = 0;

  var applied = 0;
  var goal = 0;
  var from = 0;
  var moveAt = 0;
  var settled = true;
  var raf = 0;
  var deadline = 0;

  function write(kb) {
    if (Math.abs(kb - applied) < 1) return;

    var opening = applied === 0 && kb > 0;
    applied = kb;
    if (kb > 0) {
      document.documentElement.style.setProperty("--agy-bottom", kb + "px");
      document.body.classList.add("agy-kb-open");
    } else {
      document.documentElement.style.removeProperty("--agy-bottom");
      document.body.classList.remove("agy-kb-open");
    }
    if (opening) scrollChatToBottom();
  }

  function frame() {
    unpan();

    var target = Math.max(0, Math.round(base() - vv.height));
    if (target < 100) target = 0;

    if (target > 0) {
      holdUntil = 0;
      if (target !== predicted) {
        predicted = target;
        try {
          localStorage.setItem("agy-kb", String(target));
        } catch (e) {}
      }
    } else if (performance.now() < holdUntil) {
      // Hold the predicted shrink until Safari reports the keyboard. If it never
      // does -- a hardware keyboard, say -- the hold expires and the shell
      // springs back on its own.
      target = predicted;
    }

    if (target !== goal) {
      goal = target;
      from = applied;
      moveAt = performance.now();
      settled = false;
    }

    if (goal <= from) {
      // Closing: the keyboard is already on its way out, and following it
      // immediately is what the shell did smoothly before.
      write(goal);
    } else {
      var p = Math.min(1, (performance.now() - moveAt) / OPEN_MS);
      write(Math.round(from + (goal - from) * (1 - Math.pow(1 - p, 3))));
    }

    // The chat has to be pulled to the bottom once the shell has stopped moving:
    // doing it only while the shell shrinks leaves it short of the last message,
    // because the scrollable distance is still growing.
    if (!settled && applied === goal) {
      settled = true;
      if (goal > 0) scrollChatToBottom();
    }

    if (performance.now() < deadline) {
      raf = requestAnimationFrame(frame);
      return;
    }
    raf = 0;
    write(goal);
    if (applied > 0) scrollChatToBottom();
  }

  function track(ms) {
    unpan();
    var until = performance.now() + ms;
    if (until > deadline) deadline = until;
    if (!raf) raf = requestAnimationFrame(frame);
  }

  vv.addEventListener("resize", function () { track(700); });
  vv.addEventListener("scroll", function () { track(400); });

  window.addEventListener("focusin", function (e) {
    var t = e.target;
    if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable)) {
      if (predicted > 20 && applied === 0) {
        holdUntil = performance.now() + 500;
        goal = from = predicted;
        moveAt = performance.now();
        write(predicted);
      }
      track(900);
    }
  });

  window.addEventListener("focusout", function () {
    track(500);
  });
})();
</script>`

const mobileDebug = `<script id="agy-debug">
(function () {
  var vv = window.visualViewport;
  if (!vv) return;

  var ENDPOINT = "/__agy/api/debug/log";

  // The two selectors the safe-area patch relies on. Their match count is logged
  // on every sample because a selector that matches nothing, or several nested
  // shells, still reports as "applied" in the patch report.
  var OUTER = ".relative.w-screen.h-\\[100dvh\\]";
  var INNER = "div.h-\\[100dvh\\].w-screen.flex.flex-col";

  var session = Math.random().toString(36).slice(2, 8);
  var episodes = 0;

  function n(v) { return Math.round(v); }
  function pad(v, w) { v = String(v); while (v.length < w) v = " " + v; return v; }
  function padR(v, w) { v = String(v); while (v.length < w) v += " "; return v; }

  function all(sel) {
    try { return document.querySelectorAll(sel); } catch (e) { return []; }
  }
  function one(sel) {
    try { return document.querySelector(sel); } catch (e) { return null; }
  }

  function box(el) {
    if (!el) return "-";
    var b = el.getBoundingClientRect();
    return n(b.top) + ".." + n(b.bottom) + "/h" + n(b.height);
  }

  function insets() {
    var probe = document.createElement("div");
    probe.style.cssText =
      "position:fixed;left:0;top:0;width:0;height:0;visibility:hidden;padding:" +
      "env(safe-area-inset-top) env(safe-area-inset-right) " +
      "env(safe-area-inset-bottom) env(safe-area-inset-left)";
    document.documentElement.appendChild(probe);
    var s = getComputedStyle(probe);
    var out = [s.paddingTop, s.paddingRight, s.paddingBottom, s.paddingLeft].join("/");
    probe.parentNode.removeChild(probe);
    return out.replace(/px/g, "");
  }

  // In a conversation the composer's scroller is the conversation view; on the
  // home screen the history list is virtualised and its scroller is an ancestor.
  function scroller() {
    var el = one('[data-testid="conversation-view"]');
    if (el) return el;
    el = one('[data-testid^="conversation-list-"]');
    while (el && el !== document.body) {
      if (/auto|scroll/.test(getComputedStyle(el).overflowY)) return el;
      el = el.parentElement;
    }
    return null;
  }

  // Focusing an input makes the browser reveal it by scrolling ancestors, and an
  // overflow:hidden box still scrolls programmatically. An offset left behind
  // there moves the whole shell without touching the document scroll, so name
  // every ancestor of the composer that is not at zero.
  function scrolled() {
    var out = [];
    var el = one('[contenteditable="true"]');
    for (var i = 0; el && i < 15 && el !== document.documentElement; i++) {
      if (el.scrollTop || el.scrollLeft) {
        out.push(
          el.tagName.toLowerCase() +
          (el.getAttribute("data-testid") ? "[" + el.getAttribute("data-testid") + "]" : "") +
          "." + String(el.className || "").split(/\s+/).slice(0, 2).join(".") +
          "=" + n(el.scrollTop) + "," + n(el.scrollLeft));
      }
      el = el.parentElement;
    }
    return out.length ? out.join(" ") : "-";
  }

  function heads() {
    var nodes = all('[data-testid="section-header"]');
    var out = [];
    for (var i = 0; i < nodes.length; i++) {
      var wrap = nodes[i].closest("[data-index]") || nodes[i];
      var s = getComputedStyle(wrap);
      out.push(
        (nodes[i].getAttribute("data-title") || "?") +
        " " + s.position + " top:" + s.top + " z:" + s.zIndex + " " + box(wrap));
    }
    return out.length ? out.join(" | ") : "-";
  }

  // Which element actually holds the messages is not obvious from the class names,
  // and picking the wrong one is why a chat can stay scrolled away from its last
  // message. List every scroller that has something to scroll.
  function scrollers() {
    var nodes = document.querySelectorAll("body *");
    var out = [];
    for (var i = 0; i < nodes.length && out.length < 5; i++) {
      var el = nodes[i];
      if (el.scrollHeight <= el.clientHeight + 4) continue;
      if (!/auto|scroll/.test(getComputedStyle(el).overflowY)) continue;
      out.push(
        (el.getAttribute("data-testid") || el.tagName.toLowerCase() +
          "." + String(el.className || "").split(/\s+/)[0]) +
        ":st" + n(el.scrollTop) + "/sh" + el.scrollHeight + "/ch" + el.clientHeight);
    }
    return out.length ? out.join(" ") : "-";
  }

  function stickyOffset() {
    var el = one('[data-testid="history-search-input"]');
    el = el && el.closest(".sticky");
    return el ? n(el.getBoundingClientRect().height) : "-";
  }

  function state() {
    var de = document.documentElement;
    var sc = scroller();
    return [
      "vv=" + n(vv.width) + "x" + n(vv.height) + "+" + n(vv.offsetTop) + "," + n(vv.offsetLeft),
      "pageTop=" + n(vv.pageTop),
      "scale=" + vv.scale,
      "win=" + window.innerWidth + "x" + window.innerHeight,
      "dch=" + de.clientHeight,
      "dsh=" + de.scrollHeight,
      "dst=" + de.scrollTop,
      "sy=" + n(window.scrollY),
      "kb=" + (de.style.getPropertyValue("--agy-bottom") || "-"),
      "outer=" + all(OUTER).length + ":" + box(one(OUTER)),
      "inner=" + all(INNER).length + ":" + box(one(INNER)),
      "nav=" + box(one('[data-testid="mobile-open-settings"]')),
      "comp=" + box(one('[contenteditable="true"]')),
      "scr=" + (sc ? "st" + n(sc.scrollTop) + "/sh" + sc.scrollHeight + "/ch" + sc.clientHeight : "-"),
      "panned=" + scrolled(),
      "scrollers=[" + scrollers() + "]",
      "stickyTop=" + stickyOffset(),
      "heads=[" + heads() + "]"
    ].join(" ");
  }

  var lines = null;
  var startedAt = 0;
  var deadline = 0;
  var raf = 0;
  var last = "";

  function push(ev) {
    var s = state();
    if (ev === "raf" && s === last) return;
    last = s;
    // Safari drops a keepalive body over 64KB, so stay well inside one request.
    if (lines.length > 120) return;
    lines.push("  t=" + pad(n(performance.now() - startedAt), 5) + " ev=" + padR(ev, 9) + " " + s);
  }

  function flush() {
    var body = lines.join("\n") + "\n";
    lines = null;
    try {
      fetch(ENDPOINT, {
        method: "POST",
        headers: { "Content-Type": "text/plain" },
        credentials: "same-origin",
        body: body
      });
    } catch (e) {}
  }

  function loop() {
    push("raf");
    if (performance.now() < deadline) {
      raf = requestAnimationFrame(loop);
      return;
    }
    raf = 0;
    flush();
  }

  function begin(ev, ms) {
    if (!lines) {
      lines = [];
      startedAt = performance.now();
      last = "";
      episodes++;
      lines.push(
        "=== " + new Date().toISOString() +
        " session=" + session + " ep=" + episodes + " trigger=" + ev +
        " standalone=" + (navigator.standalone === true) +
        "/" + window.matchMedia("(display-mode:standalone)").matches +
        " screen=" + screen.width + "x" + screen.height +
        " dpr=" + window.devicePixelRatio +
        " env(t/r/b/l)=" + insets() +
        " ua=" + navigator.userAgent);
    }
    push(ev);
    var until = performance.now() + ms;
    if (until > deadline) deadline = until;
    if (!raf) raf = requestAnimationFrame(loop);
  }

  function editable(t) {
    return !!t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable);
  }

  vv.addEventListener("resize", function () { begin("vv-resize", 700); });
  vv.addEventListener("scroll", function () { begin("vv-scroll", 400); });
  window.addEventListener("focusin", function (e) {
    if (editable(e.target)) begin("focusin", 1500);
  });
  window.addEventListener("focusout", function (e) {
    if (editable(e.target)) begin("focusout", 1200);
  });
})();
</script>`

const signInBanner = `<style id="agy-signin-banner-style">
#agy-signin-banner-el {
  position: fixed;
  z-index: 40;
  display: none;
  text-decoration: none;
  animation: agy-banner-in 0.18s ease-out;
}
@keyframes agy-banner-in {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: none; }
}
</style>
<script id="agy-signin-banner">
(function () {
  if (!(window.matchMedia && window.matchMedia("(pointer:coarse)").matches)) return;
  if (location.pathname.indexOf("/__agy/") === 0) return;

  // Antigravity's own auth banner, reproduced with its classes and icon so it themes
  // with the app. Its mobile layout omits the real one, which on desktop sits above
  // the composer card.
  //
  // The banner lives on document.body and is positioned over that spot rather than
  // inserted next to the card: the card is inside React's tree, so anything put
  // there is removed on the next render, and re-adding it in a loop thrashes the
  // layout badly enough to break the app's own keyboard handling.
  var ICON =
    '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 -960 960 960"' +
    ' fill="currentColor" class="h-4 w-4 shrink-0 text-yellow-500" aria-hidden="true">' +
    '<path d="M74.62-140L480-840L885.38-140H74.62ZM178-200H782L480-720L178-200Zm324.92-57.08q9.38-9.38 ' +
    '9.38-22.92t-9.38-22.92T480-312.31t-22.92,9.38T447.69-280t9.38,22.92T480-247.69t22.92-9.38ZM450-352.31h60v-200H450v200ZM480-460Z"/>' +
    "</svg>";

  var banner = document.createElement("a");
  banner.id = "agy-signin-banner-el";
  banner.href = "/__agy/signin";
  banner.className =
    "bg-muted px-3 min-h-[30px] py-1.5 flex items-center gap-2 text-sm border rounded-lg";
  banner.innerHTML =
    ICON +
    '<span class="text-foreground"><span>To use the agent, please login </span>' +
    '<span class="text-current underline">here</span></span>';

  // The composer is the only editable region on screen. Its card is the outermost
  // ancestor that still has a visible margin on both sides, since the wrappers above
  // it span the full width. Requiring an actual inset rather than merely "not quite
  // full width" is what keeps the banner from stretching edge to edge. Matching on
  // width rather than height keeps it detectable while the keyboard is open, which
  // shrinks innerHeight and broke an earlier ratio-based rule.
  function composerCard() {
    var editable = document.querySelector('[contenteditable="true"]');
    if (!editable) return null;

    var card = null;
    var node = editable;
    var viewport = window.innerWidth;

    for (var i = 0; i < 10 && node.parentElement && node.parentElement !== document.body; i++) {
      node = node.parentElement;
      var box = node.getBoundingClientRect();
      if (box.left >= 6 && box.right <= viewport - 6 && box.width >= viewport * 0.5 && box.height > 24) {
        card = node;
      }
    }
    return card;
  }

  // Antigravity does render its own banner inside a chat, just not on the project
  // list, so showing ours unconditionally puts two of them on screen. Detect the real
  // one by the copy it shares with the desktop layout and stand down when it is
  // there. If Google restyles it this stops matching and the duplicate comes back,
  // which is the mild failure mode of the two.
  function nativeBanner() {
    var nodes = document.querySelectorAll('[class*="bg-muted"]');
    for (var i = 0; i < nodes.length; i++) {
      if (nodes[i] !== banner && nodes[i].textContent.indexOf("please login") >= 0) {
        return true;
      }
    }
    return false;
  }

  var lastKey = "";

  function sync() {
    var card = nativeBanner() ? null : composerCard();
    if (!card) {
      if (banner.style.display !== "none") banner.style.display = "none";
      lastKey = "";
      return;
    }

    var box = card.getBoundingClientRect();
    if (box.width <= 0) return;

    if (banner.style.display === "none") banner.style.display = "flex";

    // Both getBoundingClientRect and position:fixed resolve against the layout
    // viewport, so tracking the card needs no adjustment for Safari's pan: the two
    // move together. Subtracting the pan here pushed the banner off-screen instead.
    var height = banner.offsetHeight || 32;
    var top = Math.max(4, box.top - height - 8);
    var key = box.left + ":" + box.width + ":" + top;
    if (key === lastKey) return;
    lastKey = key;

    banner.style.left = box.left + "px";
    banner.style.width = box.width + "px";
    banner.style.top = top + "px";
  }

  function start() {
    document.body.appendChild(banner);
    sync();

    window.addEventListener("resize", sync, { passive: true });
    window.addEventListener("scroll", sync, { passive: true });
    if (window.visualViewport) {
      window.visualViewport.addEventListener("resize", sync, { passive: true });
      window.visualViewport.addEventListener("scroll", sync, { passive: true });
    }
    setInterval(sync, 1000);
  }

  function check() {
    fetch("/__agy/api/signin/status", { credentials: "same-origin" })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (d) { if (d && !d.signedIn) start(); })
      .catch(function () {});
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", check);
  } else {
    check();
  }
})();
</script>`

const uploaderScript = `<script>
(function() {
  function formatBytes(bytes) {
    if (bytes === 0) return '0 B';
    var k = 1024;
    var sizes = ['B', 'KB', 'MB', 'GB'];
    var i = Math.floor(Math.log(bytes) / Math.log(k));
    return (bytes / Math.pow(k, i)).toFixed(1) + ' ' + sizes[i];
  }

  function getActiveConversationId() {
    var params = new URLSearchParams(window.location.search);
    var id = params.get('conversationId') || params.get('c');
    if (id) return id;
    var path = window.location.pathname;
    if (path.startsWith('/c/')) {
      return path.slice(3).split('/')[0];
    }
    return 'temp_' + Date.now().toString(36);
  }

  function getActiveProjectPath() {
    var params = new URLSearchParams(window.location.search);
    return params.get('project') || params.get('workspace') || '';
  }

  function insertPromptPath(path) {
    var el = document.querySelector('[contenteditable="true"]');
    if (el) {
      el.focus();
      var textToInsert = path + ' ';
      try {
        if (!document.execCommand('insertText', false, textToInsert)) {
          el.innerText = (el.innerText ? el.innerText + ' ' : '') + textToInsert;
          el.dispatchEvent(new Event('input', { bubbles: true }));
        }
      } catch (e) {
        el.innerText = (el.innerText ? el.innerText + ' ' : '') + textToInsert;
        el.dispatchEvent(new Event('input', { bubbles: true }));
      }
    }
  }

  window.__agyUpload = function(files) {
    if (!files || files.length === 0) return;
    var file = files[0];

    var convoId = getActiveConversationId();
    var projectPath = getActiveProjectPath();

    var container = document.getElementById('agy-upload-container');
    if (!container) {
      container = document.createElement('div');
      container.id = 'agy-upload-container';
      container.style.cssText = 'position:fixed;bottom:84px;right:16px;z-index:99999;display:flex;flex-direction:column;gap:8px;max-width:340px;width:calc(100vw - 32px);pointer-events:none;';
      document.body.appendChild(container);
    }

    var card = document.createElement('div');
    card.style.cssText = 'background:#18181b;border:1px solid #27272a;border-radius:8px;padding:10px 12px;box-shadow:0 8px 24px rgba(0,0,0,0.5);color:#f4f4f5;font-family:-apple-system,BlinkMacSystemFont,Segoe UI,Roboto,sans-serif;font-size:12px;pointer-events:auto;transition:all 0.2s ease-out;';

    var header = document.createElement('div');
    header.style.cssText = 'display:flex;justify-content:space-between;align-items:center;margin-bottom:6px;gap:8px;';
    
    var nameSpan = document.createElement('span');
    nameSpan.style.cssText = 'overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-weight:500;color:#e4e4e7;';
    nameSpan.textContent = file.name;
    
    var pctSpan = document.createElement('span');
    pctSpan.style.cssText = 'color:#a1a1aa;font-size:11px;font-variant-numeric:tabular-nums;flex-shrink:0;';
    pctSpan.textContent = '0%';

    header.appendChild(nameSpan);
    header.appendChild(pctSpan);

    var barBg = document.createElement('div');
    barBg.style.cssText = 'width:100%;height:3px;background:#27272a;border-radius:99px;overflow:hidden;margin-bottom:6px;';
    var bar = document.createElement('div');
    bar.style.cssText = 'width:0%;height:100%;background:#3b82f6;border-radius:99px;transition:width 0.1s linear;';
    barBg.appendChild(bar);

    var footer = document.createElement('div');
    footer.style.cssText = 'display:flex;justify-content:space-between;font-size:10px;color:#71717a;font-variant-numeric:tabular-nums;';
    
    var bytesSpan = document.createElement('span');
    bytesSpan.textContent = '0 / ' + formatBytes(file.size);
    
    var speedSpan = document.createElement('span');
    speedSpan.textContent = 'Uploading...';

    footer.appendChild(bytesSpan);
    footer.appendChild(speedSpan);

    card.appendChild(header);
    card.appendChild(barBg);
    card.appendChild(footer);
    container.appendChild(card);

    var formData = new FormData();
    formData.append('conversationId', convoId);
    formData.append('projectPath', projectPath);
    formData.append('file', file);

    var xhr = new XMLHttpRequest();
    var startTime = Date.now();

    xhr.upload.onprogress = function(e) {
      if (e.lengthComputable) {
        var pct = Math.round((e.loaded / e.total) * 100);
        bar.style.width = pct + '%';
        pctSpan.textContent = pct + '%';
        bytesSpan.textContent = formatBytes(e.loaded) + ' / ' + formatBytes(e.total);
        var elapsed = (Date.now() - startTime) / 1000;
        if (elapsed > 0.4) {
          var speed = e.loaded / elapsed;
          speedSpan.textContent = formatBytes(speed) + '/s';
        }
      }
    };

    xhr.onload = function() {
      if (xhr.status === 200) {
        try {
          var res = JSON.parse(xhr.responseText);
          bar.style.background = '#22c55e';
          pctSpan.textContent = 'Completed';
          pctSpan.style.color = '#22c55e';
          speedSpan.textContent = res.relativePath;
          
          insertPromptPath(res.relativePath);

          setTimeout(function() {
            card.style.opacity = '0';
            card.style.transform = 'translateY(6px)';
            setTimeout(function() { card.remove(); }, 200);
          }, 2500);
        } catch (err) {
          showError('Invalid server response');
        }
      } else {
        showError('Upload failed (' + xhr.status + ')');
      }
    };

    xhr.onerror = function() {
      showError('Network error');
    };

    xhr.ontimeout = function() {
      showError('Request timed out');
    };

    function showError(msg) {
      bar.style.background = '#ef4444';
      pctSpan.textContent = 'Failed';
      pctSpan.style.color = '#ef4444';
      speedSpan.textContent = msg;
      setTimeout(function() { card.remove(); }, 5000);
    }

    xhr.timeout = 120000;
    xhr.open('POST', '/__agy/api/upload');
    xhr.send(formData);
  };

  var hiddenFileInput = null;
  window.__agyTriggerUpload = function() {
    if (!hiddenFileInput) {
      hiddenFileInput = document.createElement('input');
      hiddenFileInput.type = 'file';
      hiddenFileInput.style.display = 'none';
      document.body.appendChild(hiddenFileInput);
      hiddenFileInput.addEventListener('change', function(e) {
        if (e.target.files && e.target.files.length > 0) {
          window.__agyUpload(e.target.files);
        }
        e.target.value = '';
      });
    }
    hiddenFileInput.value = '';
    hiddenFileInput.click();
  };
})();
</script>`
