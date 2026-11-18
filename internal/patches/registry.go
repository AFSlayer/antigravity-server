package patches

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// Adaptive regular expressions matching across Antigravity 2.8.x, 2.9.x and future minified builds.
var (
	modelEffortRe = regexp.MustCompile(`,?onClick:(?:[a-zA-Z0-9_$]+\?void 0:)?\(\)=>\{var [a-zA-Z0-9_$]+=[\r\n\s]*[a-zA-Z0-9_$]+\.byEffort\.get\([a-zA-Z0-9_$]+\);[a-zA-Z0-9_$]+&&[a-zA-Z0-9_$]+\([a-zA-Z0-9_$]+\)\}`)

	signInButtonRe = regexp.MustCompile(`onClick:\(\)=>[\r\n\s]*(?:\{[\r\n\s]*(?:\w+\(\);[\r\n\s]*)?\w+\.showLoginFlow\(\)[\r\n\s]*\}|\w+\.showLoginFlow\(\))`)

	authServiceLoginRedirectRe     = regexp.MustCompile(`async loginWithRedirect\(([a-zA-Z0-9_$]+)\)\{`)
	authServiceShowLoginFlowRe     = regexp.MustCompile(`async showLoginFlow\(\)\{`)
	authServiceStubLoginRedirectRe = regexp.MustCompile(`loginWithRedirect:async\(\)=>\{console\.log\("\[AuthService\] loginWithRedirect"\)\},showLoginFlow:async\(\)=>\{console\.log\("\[AuthService\] showLoginFlow"\)\}`)

	skipOnboardingRe = regexp.MustCompile(`c\.hasOnboardingScreens&&[a-zA-Z0-9_$]+!==2&&(?:\((?:[a-zA-Z0-9_$]+\(\),)+[a-zA-Z0-9_$]+\(\{to:"/onboarding",(?:search:[a-zA-Z0-9_$]+,)?replace:!0,throw:!0\}\)\)|[a-zA-Z0-9_$]+\(\{to:"/onboarding",(?:search:[a-zA-Z0-9_$]+,)?replace:!0,throw:!0\}\))`)

	mobileEnterNewlineRe                = regexp.MustCompile(`registerCommand\(([a-zA-Z0-9_$]+),([a-zA-Z0-9_$]+)=>\{if\(![a-zA-Z0-9_$]+\)return!1;((?:if\([a-zA-Z0-9_$]+\([a-zA-Z0-9_$]+,[a-zA-Z0-9_$]+,[a-zA-Z0-9_$]+\.current\)\)return!0;)?)((?:var [a-zA-Z0-9_$]+=[a-zA-Z0-9_$]+\.shiftKey&&\([a-zA-Z0-9_$]+\.metaKey\|\|[a-zA-Z0-9_$]+\.ctrlKey\);if\([a-zA-Z0-9_$]+\.shiftKey&&![a-zA-Z0-9_$]+\)return!1;)?)[a-zA-Z0-9_$]+\.preventDefault\(\);`)
	mobileProjectAddButtonRe            = regexp.MustCompile(`if\((\w+)==="project"\|\|(\w+)==="environment"\|\|(\w+)==="status"\)\{let\s+([a-zA-Z0-9_$]+)=([a-zA-Z0-9_$]+)\?void 0:([a-zA-Z0-9_$]+)==="project"\?"New Conversation in Project":([a-zA-Z0-9_$]+)==="environment"\?"New Conversation in Workspace":[\r\n\s]*void 0`)
	mobileProjectHeaderActionsRe        = regexp.MustCompile(`className:([a-zA-Z0-9_$]+)\("absolute right-1 top-0 flex h-full items-center gap-1",([a-zA-Z0-9_$]+)\|\|([a-zA-Z0-9_$]+)\?"opacity-100":"opacity-0 group-hover\/header:opacity-100 group-focus-within\/header:opacity-100"\)`)
	mobileProjectKebabMenuRe            = regexp.MustCompile(`,([a-zA-Z0-9_$]+)=\(0,([a-zA-Z0-9_$]+)\.useContext\)\(([a-zA-Z0-9_$]+)\),([a-zA-Z0-9_$]+)=[a-zA-Z0-9_$]+\(\)&&[a-zA-Z0-9_$]+!==null,|(function [a-zA-Z0-9_$]+\(a\)\{var \{isOpen:b,onOpenChange:c,ariaLabel:[de],sortMode:f,onSortModeChange:g,size:h="icon"\}=a;a=[a-zA-Z0-9_$]+\(a\);return )[a-zA-Z0-9_$]+\(\)\|\|(a\.length===0&&!g\?null:)`)
	mobileProjectAddClickCloseSidebarRe = regexp.MustCompile(`onClick:([a-zA-Z0-9_$]+)=>[\r\n\s]*\{([a-zA-Z0-9_$]+)\.stopPropagation\(\);([a-zA-Z0-9_$]+)\([a-zA-Z0-9_$]+\)\|\|\([a-zA-Z0-9_$]+\.preventDefault\(\),([a-zA-Z0-9_$]+)\([a-zA-Z0-9_$]+\)\)\}`)
	mobileProjectSortOrderRe            = regexp.MustCompile(`if\(([a-zA-Z0-9_$]+)\.length===\s*0\)return\s+([a-zA-Z0-9_$]+\([a-zA-Z0-9_$]+,[a-zA-Z0-9_$]+,([a-zA-Z0-9_$]+),[a-zA-Z0-9_$]+(?:,[a-zA-Z0-9_$]+)?\)\.map\([a-zA-Z0-9_$]+\));`)
	mobileProjectSettingsExportRe       = regexp.MustCompile(`((?:export\s+)?)function ([a-zA-Z0-9_$]+)\(\)\{(var a=[a-zA-Z0-9_$]+\([a-zA-Z0-9_$]+\),b=[a-zA-Z0-9_$]+\([a-zA-Z0-9_$]+\),c=[a-zA-Z0-9_$]+\(\),[de]=[a-zA-Z0-9_$]+\(\),\{logEvent:f\}=[a-zA-Z0-9_$]+\(\),\[g,h\]=)([a-zA-Z0-9_$]+\("main"\))`)
	mobileSettingsProjectsRe            = regexp.MustCompile(
		`(var [a-zA-Z0-9_$]+=\(\)=>\{var a=[a-zA-Z0-9_$]+\(\),[\s\S]{1,250}?\{sharedNavItems:[ce],globalScreens:[df],workspaces:[fg],showWorkspacesSection:[gh]\}=[a-zA-Z0-9_$]+\(\);` +
			`[bc]=\[\.\.\.[ce]\.filter\([a-zA-Z0-9_$]+=>[a-zA-Z0-9_$]+!=="Account"\),\.\.\.[df],(?:\.\.\.\([bc]\?\[\]:\["Shortcuts"\]\),)?(?:\.\.\.\([bc]\.hideProvideFeedback\?\[\]:\["Provide Feedback"\]\)|\.\.\.\([bc]\?\["Provide Feedback"\]:\[\]\))\];` +
			`var ([hk])=[a-zA-Z0-9_$]+\(!0\)[;,][bck]=[hk]\.path\.length[,;](?:var )?l=[bck]===0,m=[hk]\.path\[0\]\?\?[hk]\.exiting\[0\],n=m\?\.startsWith\("workspace:"\);)` +
			`(return ([a-zA-Z0-9_$]+)\.createElement\("div",\{ref:[hk]\.setHost,className:"relative h-full w-full overflow-hidden"\},[\r\n\s]*[a-zA-Z0-9_$]+\.createElement\([a-zA-Z0-9_$]+,[\r\n\s]*` +
			`\{offset:-[bck],active:l,depth:0,footer:[ce]\.includes\("Account"\)\?[a-zA-Z0-9_$]+\.createElement\("div",[\r\n\s]*\{className:"flex flex-col border-t border-border p-1"\},[\r\n\s]*[a-zA-Z0-9_$]+\.createElement\([a-zA-Z0-9_$]+,\{showChevron:!0,className:"mx-1 px-3 py-2\.5",onClick:\(\)=>\{a\(([a-zA-Z0-9_$]+)\("Account"\)\);[hk]\.push\("Account"\)\}\}\)\):void 0\},` +
			`[a-zA-Z0-9_$]+\.createElement\(([a-zA-Z0-9_$]+),\{text:"Settings"\}\),[bc]\.map\([a-zA-Z0-9_$]+=>[a-zA-Z0-9_$]+\.createElement\(([a-zA-Z0-9_$]+),\{key:[a-zA-Z0-9_$]+,(?:text:[a-zA-Z0-9_$]+,)?label:[^,]+,onClick:\(\)=>\{a\([a-zA-Z0-9_$]+\([a-zA-Z0-9_$]+\)\);[hk]\.push\([a-zA-Z0-9_$]+\)\}\}\)\),` +
			`[gh]&&[a-zA-Z0-9_$]+\.createElement\([a-zA-Z0-9_$]+\.Fragment,null,[a-zA-Z0-9_$]+\.createElement\([a-zA-Z0-9_$]+,\{text:"Workspaces"\}\),[fg]\.map\([a-zA-Z0-9_$]+=>[a-zA-Z0-9_$]+\.createElement\([a-zA-Z0-9_$]+,[\r\n\s]*\{key:[a-zA-Z0-9_$]+\.uri,(?:label|text):[a-zA-Z0-9_$]+\.name,[\r\n\s]*onClick:\(\)=>\{var [a-zA-Z0-9_$]+=[a-zA-Z0-9_$]+\.uri;a\([a-zA-Z0-9_$]+\(\{workspaceUri:[a-zA-Z0-9_$]+\}\)\);[hk]\.push\(` + "`" + `workspace:\$\{[a-zA-Z0-9_$]+\}` + "`" + `\)\}\}\)\)\))` +
			`(\),[\r\n\s]*m&&[a-zA-Z0-9_$]+\.createElement\([a-zA-Z0-9_$]+,\{offset:1-[bck],active:[bck]===[\r\n\s]*1,depth:1,exiting:[hk]\.exiting\.includes\(m\),onExited:\(\)=>\{[hk]\.onExited\(m\)\},header:[a-zA-Z0-9_$]+\.createElement\([a-zA-Z0-9_$]+,\{onBack:[hk]\.pop\}\)\},[a-zA-Z0-9_$]+\.createElement\([a-zA-Z0-9_$]+\.Provider,\{value:!0\},)` +
			`(n\?[a-zA-Z0-9_$]+\.createElement\([a-zA-Z0-9_$]+,null\):[a-zA-Z0-9_$]+\(m\))` +
			`(\)\)\)\};[\s\S]+?var a=([a-zA-Z0-9_$]+)\(\),b=!!a\.get\("settings"\)\?\.hideProjects[\s\S]{1,400}?(?:\{(?:settingsFeature:[a-zA-Z0-9_$]+,)?projectManagementFeature:g\}=(\(0,[a-zA-Z0-9_$]+\.useContext\)\([a-zA-Z0-9_$]+\))|f=([a-zA-Z0-9_$]+\(\)))[\s\S]{1,400}?,[a-zA-Z0-9_$]+=([a-zA-Z0-9_$]+)\([fg]\??\.projectsStateProvider\))`,
	)
	mobileUserMessageActionsRe     = regexp.MustCompile(`(className:)"([^"]*?\buser-input-buttons-container\b[^"]*)"`)
	mobileConversationRowActionsRe = regexp.MustCompile(`className:\s*([a-zA-Z0-9_$]+)\(\s*"((?:agy-row-actions )?absolute top-0 bottom-0 -right-1 pl-6 flex items-center justify-end gap-0\.5 z-10"\s*,(?:\s*![a-zA-Z0-9_$]+\s*&&\s*"\[&>\*\]:pointer-events-auto"\s*,)?)\s*([a-zA-Z0-9_$]+)\s*\?\s*"hidden"\s*:\s*([a-zA-Z0-9_$]+)\s*\?\s*"opacity-100"\s*:\s*"((?:[a-z-]+:)?opacity-0 group-hover:opacity-100 [^"]+)"\s*\)`)

	mobileTitlebarDeleteHookRe  = regexp.MustCompile(`\{handleArchive:([a-zA-Z0-9_$]+),handleRestore:([a-zA-Z0-9_$]+),handlePin:([a-zA-Z0-9_$]+),handleUnpin:([a-zA-Z0-9_$]+),[\r\n\s]*isArchiveSupported:([a-zA-Z0-9_$]+),[\r\n\s]*handleShare:([a-zA-Z0-9_$]+),showShareModal:([a-zA-Z0-9_$]+),[\r\n\s]*shareUrl:([a-zA-Z0-9_$]+),handleCloseShareModal:([a-zA-Z0-9_$]+),[\r\n\s]*onShare:([a-zA-Z0-9_$]+)((?:,\s*handleRename:[a-zA-Z0-9_$]+)?)\}=([a-zA-Z0-9_$]+)\(([a-zA-Z0-9_$]+)\?\?""\)`)
	mobileTitlebarDeleteMenuRe  = regexp.MustCompile(`([a-zA-Z0-9_$]+)\&\&\(([a-zA-Z0-9_$]+)\.push\(\{iconName:"edit",tooltip:"Rename",onClick:([a-zA-Z0-9_$]+)\}\)`)
	mobileTitlebarDeleteModalRe = regexp.MustCompile(`([a-zA-Z0-9_$]+=[a-zA-Z0-9_$]+\(\{cascadeId:([a-zA-Z0-9_$]+),paneId:([a-zA-Z0-9_$]+),includeRemoveFromSplit:!1\}\);[\r\n\s]*return\s+[a-zA-Z0-9_$]+(?:\.length>0)?(?:\|\|[a-zA-Z0-9_$]+(?:\.length>0)?)+\?([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+)\.Fragment,null,)`)
	mobileDeleteModalExportRe   = regexp.MustCompile(`(?:var|const)\s+([a-zA-Z0-9_$]+)=(\(\{[^}]*isOpen:a,onClose:b,onDelete:c,showLoadingSpinner:[a-zA-Z0-9_$]+[^}]*\}\)=>)`)

	mobileKebabMenuPinArchiveRe      = regexp.MustCompile(`(?:const|var)\s+([a-zA-Z0-9_$]+)=\(\{cascadeId:([a-zA-Z0-9_$]+),onDeleteClick:([a-zA-Z0-9_$]+),onRenameClick:([a-zA-Z0-9_$]+),onMarkAsReadClick:([a-zA-Z0-9_$]+),isUnread:([a-zA-Z0-9_$]+),onViewDebugClick:([a-zA-Z0-9_$]+)([^\}]*)\}\)=>([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{side:"bottom",align:"start",className:"min-w-\[180px\]",finalFocus:!1\},([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{onClick:([a-zA-Z0-9_$]+),"data-testid":"conversation-rename-menu-item"\},([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{name:"edit",size:16,className:"text-secondary-foreground shrink-0"\}\),([a-zA-Z0-9_$]+)\.createElement\("span",null,"Rename"\)\),`)
	mobileKebabWrapperPinArchiveRe   = regexp.MustCompile(`(?:const|var)\s+([a-zA-Z0-9_$]+)=(?:(?:[a-zA-Z0-9_$]+)\.memo\()?[\r\n\s]*(?:function)?\(\{cascadeId:([a-zA-Z0-9_$]+),onDeleteClick:([a-zA-Z0-9_$]+),onRenameClick:([a-zA-Z0-9_$]+),onMarkAsReadClick:([a-zA-Z0-9_$]+),isUnread:([a-zA-Z0-9_$]+),onOpenChange:([a-zA-Z0-9_$]+),onViewDebugClick:([a-zA-Z0-9_$]+)[^\}]*\}\)(?:=>|\{return\s+)([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{onOpenChange:([a-zA-Z0-9_$]+)\},([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{asChild:!0\},([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{variant:"ghost",size:"icon","aria-label":"More options","data-testid":"conversation-kebab",onClick:([a-zA-Z0-9_$]+)=>void ([a-zA-Z0-9_$]+)\.stopPropagation\(\)\},([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{name:"more_vert",size:16\}\)\)\),([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{cascadeId:([a-zA-Z0-9_$]+),onDeleteClick:([a-zA-Z0-9_$]+),onRenameClick:([a-zA-Z0-9_$]+),onMarkAsReadClick:([a-zA-Z0-9_$]+),isUnread:([a-zA-Z0-9_$]+),[\r\n\s]*onViewDebugClick:([a-zA-Z0-9_$]+)[^\}]*\}\)(?:\))?(?:\})?(?:\))?;`)
	mobileKebabCallPinArchiveRe      = regexp.MustCompile(`([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),[\r\n\s]*\{cascadeId:([a-zA-Z0-9_$]+),onDeleteClick:(\(\)=>\{?[a-zA-Z0-9_$]+\(!0\)\}?|[a-zA-Z0-9_$]+),onRenameClick:([a-zA-Z0-9_$]+),onMarkAsReadClick:([^\}]+?),isUnread:([a-zA-Z0-9_$]+(?:\.[a-zA-Z0-9_$]+)?),onOpenChange:([a-zA-Z0-9_$]+)(?:,[\r\n\s]*onViewDebugClick:([a-zA-Z0-9_$]+))?(?:,[\r\n\s]*onShareClick:[^\}]+?)?\}\)`)
	mobileHideAuxSidebarRe           = regexp.MustCompile(`([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{iconName:"dock_to_bottom",onClick:[a-zA-Z0-9_$]+,"aria-label":"Toggle Auxiliary Pane",dataTestId:"mobile-toggle-aux-sidebar"\}\)`)
	settingsRulesEditorRe            = regexp.MustCompile(`((?:var|const)\s+([a-zA-Z0-9_$]+)=\(\{name:a,path:b,onCopyPath:c,description:([a-zA-Z0-9_$]+),badge:([a-zA-Z0-9_$]+)[^\}]*?(?:onEdit:([a-zA-Z0-9_$]+),editTitle:([a-zA-Z0-9_$]+)="Edit",)?onDelete:([a-zA-Z0-9_$]+),deleteTitle:([a-zA-Z0-9_$]+)="Delete",onToggle:([a-zA-Z0-9_$]+),toggleChecked:([a-zA-Z0-9_$]+),toggleDisabled:([a-zA-Z0-9_$]+)=!1,expandableContent:([a-zA-Z0-9_$]+)[^\}]*?\}\)=>\{)(var\s+[a-zA-Z0-9_$]+=[a-zA-Z0-9_$]+(?:\|\|[a-zA-Z0-9_$]+)+,\[[a-zA-Z0-9_$]+,[a-zA-Z0-9_$]+\]=\(0,([a-zA-Z0-9_$]+)\.useState\)\(!1\),)`)
	settingsCustomizationsShowEditRe = regexp.MustCompile(`("Copy [pP]ath"\)\),)!([a-zA-Z0-9_$]+)&&([a-zA-Z0-9_$]+)&&([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+)\.Fragment,null,([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{variant:"ghost",size:"icon-sm",onClick:([a-zA-Z0-9_$]+),"aria-label":(?:([a-zA-Z0-9_$]+,className:"[^"]+","data-tooltip-id":[a-zA-Z0-9_$]+\},[a-zA-Z0-9_$]+\.createElement\(([a-zA-Z0-9_$]+),\{name:"(edit_note|delete)"))?`)
	settingsDocRowEditorRe           = regexp.MustCompile(`(function [a-zA-Z0-9_$]+\(\{title:a,displayPath:b,copyPath:c,description:([a-zA-Z0-9_$]+),content:f,modalChildren:g,badge:h,actions:k,actionsPaddingClass:l,disabled:m=!1,MarkdownRenderer:n\}\)\{var \[([pq]),([a-zA-Z0-9_$]+)\]=\(0,([a-zA-Z0-9_$]+)\.useState\)\(!1\),)([tu]=!![a-zA-Z0-9_$]+\|\|!!f\|\|!!g,[ut]=[a-zA-Z0-9_$]+\|\|b;)`)
	settingsRestoreCustomizationsRe  = regexp.MustCompile(`(?:([a-zA-Z0-9_$]+)=([a-zA-Z0-9_$]+)=>\{if\(![a-zA-Z0-9_$]+\)return [a-zA-Z0-9_$]+;[a-zA-Z0-9_$]+=new Map\([a-zA-Z0-9_$]+\);[a-zA-Z0-9_$]+\.delete\("Customizations"\);[\r\n\s]*return [a-zA-Z0-9_$]+\};)|(if\s*\(\s*!\s*[a-zA-Z0-9_$]+\s*\)\s*return\s+([a-zA-Z0-9_$]+)\s*;\s*[a-zA-Z0-9_$]+\s*=\s*new Map\(\s*[a-zA-Z0-9_$]+\s*\)\s*;\s*[a-zA-Z0-9_$]+\.delete\(\s*"Customizations"\s*\)\s*;\s*)(return\s+[a-zA-Z0-9_$]+\s*\})`)

	hideMicButtonRe = regexp.MustCompile(`([a-zA-Z0-9_$]+\.displayName="GutterHoverCommentButton";var )([a-zA-Z0-9_$]+)=\(`)

	hideUserProfileRe = regexp.MustCompile(`function [a-zA-Z0-9_$]+\(\{className:a=""\}={}\)\{return [a-zA-Z0-9_$]+\.createElement\("a",\{href:"#",onClick:b=>\{b\.preventDefault\(\)\},className:` + "`w-6 h-6 rounded-full overflow-hidden shrink-0 flex items-center justify-center bg-transparent text-muted-foreground \\${a}`" + `,"aria-label":"User Profile \(Placeholder\)"`)

	mobileSkipNotificationRe = regexp.MustCompile(`var\s+([a-zA-Z0-9_$]+)\s*=\s*!!this\.storageService\.get\(\s*"didAskForNotificationPermission"\s*\)\s*;(\s*this\.notificationPermission\s*!==)`)

	mobileNewConvoViewRe = regexp.MustCompile(`((?:var|const)\s+[a-zA-Z0-9_$]+=\(\)=>\{var a=[a-zA-Z0-9_$]+\(\),b=[a-zA-Z0-9_$]+\(\);return\(0,[a-zA-Z0-9_$]+\.useCallback\)\(\(c,([a-zA-Z0-9_$]+)\)=>\{[a-zA-Z0-9_$]+\([a-zA-Z0-9_$]+\.map\(f=>\(\{trigger:f,ran:!1\}\)\)\);[a-zA-Z0-9_$]+\(c,\{(?:section|project):)[a-zA-Z0-9_$]+(\}\)\},\[[a-zA-Z0-9_$]+,[a-zA-Z0-9_$]+\]\)\};[\r\n\s]*(?:var|const)\s+[a-zA-Z0-9_$]+=\(\)=>\{var [a-zA-Z0-9_$]+=[a-zA-Z0-9_$]+\(\),\{q:[a-zA-Z0-9_$]+\}=([a-zA-Z0-9_$]+)\(\{strict:!1\}\)(?:,[\s\S]{1,500}?)?;(?:[a-zA-Z0-9_$]+\("MOBILE_HOME_VIEW"\);)?)(return\s+([a-zA-Z0-9_$]+)\.createElement\("div",\{(?:"data-media-drop-scope":"",)?className:"w-full h-full flex flex-col min-h-0 animate-fade-in"\},[\r\n\s]*)([a-zA-Z0-9_$]+\.createElement\("div",\{className:"flex-1 min-h-0 overflow-y-auto flex flex-col gap-(?:6|3) pt-3"\},(?:[a-zA-Z0-9_$]+&&[a-zA-Z0-9_$]+\.createElement\("div",\{className:"shrink-0 px-2","data-testid":"mobile-home-concierge"\},[a-zA-Z0-9_$]+\.createElement\([a-zA-Z0-9_$]+,null\)\),)?[a-zA-Z0-9_$]+\.createElement\([a-zA-Z0-9_$]+,\{surface:"background"\}\)\)|[a-zA-Z0-9_$]+\.createElement\("div",[\r\n\s]*\{ref:[a-zA-Z0-9_$]+,\["data-sidebar-scroll-container"\]:"true",className:"flex-1 min-h-0 overflow-y-auto flex flex-col (?:gap-3 pt-3 )?pb-2 relative"\},[\s\S]{1,600}?surface:"background",scrollElementRef:[a-zA-Z0-9_$]+,scrollMargin:[a-zA-Z0-9_$]+\}\)\)\)\)),`)

	// The input hands the picker to the model selector, which draws it at the end of its menu, where the toolbar is on
	// top and the model selector is shown, and to its own toolbar otherwise. Group 1 is the name that decision goes by,
	// 2 the rest of it up to its second use, 3 and 5 the names of the decisions the two uses go by, and 4 and 6 the
	// pickers they hand on: see agentRoutingIsKnown.
	mobileAgentPickerInModelMenuRe = regexp.MustCompile(`([a-zA-Z0-9_$]+)=\s*[a-zA-Z0-9_$]+===\s*"top"&&\s*(![a-zA-Z0-9_$]+,[\s\S]{1,1000}?[\s\S]{0,400}?footerExtra:\s*([a-zA-Z0-9_$]+)\?\s*([a-zA-Z0-9_$]+):\s*void 0[\s\S]{0,1000}?[\s\S]{0,1000}?[\s\S]{0,1000}?agentSelector:\s*([a-zA-Z0-9_$]+)\?\s*void 0:\s*([a-zA-Z0-9_$]+))`)
	// The input of the phone layout's new conversation screen, which is given no picker.
	mobileAgentPickerRe = regexp.MustCompile(`(\{conversationId:\s*void 0,\s*isLoading:\s*!1,\s*dropdownPlacement:\s*"top-start",\s*openConversationOptimistically:\s*[a-zA-Z0-9_$]+,\s*showBottomToolbar:\s*!0,\s*aboveContent:\s*[a-zA-Z0-9_$]+\.createElement\(\s*[a-zA-Z0-9_$]+,\s*null\),\s*initialQuery:\s*[a-zA-Z0-9_$]+)(\}\))`)

	// The input shows the agent that is chosen as a chip beside the model selector, in a new conversation and only where
	// its toolbar is on top. Group 1 is the input's footer from the model selector it is given, which is group 2, up to
	// that decision, 3 the name the toolbar being on top goes by and 4 the rest of the decision.
	mobileAgentPickerChipRe = regexp.MustCompile(`(modelSelector:\s*([a-zA-Z0-9_$]+),\s*environmentSelector:[\s\S]{1,1000}?[\s\S]{0,400}?selectedAgent:\s*)([a-zA-Z0-9_$]+)(&&\s*![a-zA-Z0-9_$]+\?\s*[a-zA-Z0-9_$]+\?\.selectedAgentScriptItem:\s*void 0)`)

	mobileNewConvoHeaderRe = regexp.MustCompile(`([a-zA-Z0-9_$]+)=\(\)=>([a-zA-Z0-9_$]+)\(\{select:a=>a\.location\.pathname==="\/"\}\)`)

	mobileBackClearsSectionRe         = regexp.MustCompile(`(?:((?:function [a-zA-Z0-9_$]+\(\)|[a-zA-Z0-9_$]+\.memo\(function\(\))\{var a=[a-zA-Z0-9_$]+\(\),b=[a-zA-Z0-9_$]+\()(\),c=[a-zA-Z0-9_$]+\(\),[\s\S]{1,800}?[\s\S]{0,600}?))?([a-zA-Z0-9_$]+\.createElement\([a-zA-Z0-9_$]+,\{iconName:"arrow_back",onClick:\(\)=>)\{?([a-zA-Z0-9_$]+)\(\)\}?(,"aria-label":"Back to home",dataTestId:"mobile-back-to-home"(?:,size:"[^"]+")?\}\))`)
	mobileHomeNewConvoRowRe           = regexp.MustCompile(`(\{showNewConversation:a=!0,\s*(?:compact:[a-zA-Z0-9_$]+=!1,\s*)?className:[a-zA-Z0-9_$]+,\s*conciergeTestId:([a-zA-Z0-9_$]+)\}\)\{[\s\S]{1,1000}?[\s\S]{0,1000}?;\s*)return a\|\|((?:![a-zA-Z0-9_$]+\?\.hideConversationHistory\|\|)?[^?]{1,80}\?\s*[a-zA-Z0-9_$]+\.createElement\("div",\s*\{className:[a-zA-Z0-9_$]+\((?:"flex flex-col gap-1\.5",\s*[a-zA-Z0-9_$]+|"flex flex-col",\s*[a-zA-Z0-9_$]+\?"gap-0\.5":"gap-1\.5",\s*[a-zA-Z0-9_$]+)\),\s*"data-testid":"sidebar-navigation-items"\},\s*)a&&([a-zA-Z0-9_$]+\.createElement\([a-zA-Z0-9_$]+,\s*\{)variant:"secondary",\s*(icon:[a-zA-Z0-9_$]+\.createElement\([a-zA-Z0-9_$]+,\s*\{name:"add"\}\),\s*text:"New Conversation",\s*)href:([a-zA-Z0-9_$]+\([a-zA-Z0-9_$]+\)),\s*("data-testid":"new-conversation-button",\s*onActivate:\(\)=>\{[^}]{1,40}\},\s*)selected:([a-zA-Z0-9_$]+),`)
	mobileNewConvoButtonRe            = regexp.MustCompile(`("New Conversation in Workspace":\s*"New Conversation in Project"[\s\S]{1,600}?[:;][\r\n\s]*)([a-zA-Z0-9_$]+)(\([a-zA-Z0-9_$]+\)\}[\s\S]{1,900}?[\s\S]{0,900}?([a-zA-Z0-9_$]+)\.createElement\([a-zA-Z0-9_$]+,\{items:)([a-zA-Z0-9_$]+(?:\.displayedFlatItems)?)(,(?:variant:"sidebar",)?surface:)([a-zA-Z0-9_$]+)((?:,\s*scrollElementRef:[a-zA-Z0-9_$]+,\s*scrollMargin:[a-zA-Z0-9_$]+)?,\s*subtitleContext:[a-zA-Z0-9_$]+,\s*onToggleCollapseSection:[a-zA-Z0-9_$]+,\s*onToggleSectionHeader:[a-zA-Z0-9_$]+,\s*onTogglePagination:[a-zA-Z0-9_$]+,\s*customHeaderRenderer:)([a-zA-Z0-9_$]+)`)
	suppressConversationUnavailableRe = regexp.MustCompile(`[a-zA-Z0-9_$]+\(\{tag:"trajectory-not-found",title:"Conversation unavailable",message:"The conversation could not be loaded because its data was not found\."\}\)`)
	disableTelemetryRe                = regexp.MustCompile(`return\{telemetryEnabled:[a-zA-Z0-9_$]+(?:&&![a-zA-Z0-9_$]+)?,marketingEmailsEnabled:`)

	// folderPickerInitialPathRe finds the path the interactive folder picker
	// opens at. Up to 2.31 the call computes it inline from the home directory;
	// 2.32 puts it in a variable first and passes that.
	folderPickerInitialPathRe = regexp.MustCompile(
		`(?:initialPath:\s*[a-zA-Z0-9_$]+\s*\?\s*[a-zA-Z0-9_$]+\.fsPath\s*:\s*[a-zA-Z0-9_$]+\s*\?\s*"C:/"\s*:\s*"/"|` +
			`(=\s*[a-zA-Z0-9_$]+\s*\?\s*[a-zA-Z0-9_$]+\(\s*[a-zA-Z0-9_$]+\s*\)\s*:\s*"/"\s*,\s*[a-zA-Z0-9_$]+\s*=\s*[a-zA-Z0-9_$]+\(\s*await\s+[a-zA-Z0-9_$]+\(\s*[a-zA-Z0-9_$]+\(\s*\{\s*)initialPath:\s*[a-zA-Z0-9_$]+)` +
			`(\s*,\s*fetchDirectoryContents:)`,
	)

	composerUploadMenuRe = regexp.MustCompile(`\{icon:([a-zA-Z0-9_$]+)=>([a-zA-Z0-9_$]+)\.createElement\(([a-zA-Z0-9_$]+),\{name:"(image|attach_file)",size:[a-zA-Z0-9_$]+\.width\?Number\([a-zA-Z0-9_$]+\.width\):14,className:[a-zA-Z0-9_$]+\.className\}\),[\r\n\s]*label:"(Media|Files)",onClick:([a-zA-Z0-9_$]+)\}`)

	fileUploadAcceptAllRe = regexp.MustCompile(`(onChange:[a-zA-Z0-9_$]+,)(?:accept:[a-zA-Z0-9_$]+,[\r\n\s]*)?multiple:!0|accept:"\.png,[^"]+",[\r\n\s]*multiple:!0`)
	// fileDropAcceptAllRe finds the test for whether a dropped file is media.
	// 2.32 first answers no for a name that is a text file.
	fileDropAcceptAllRe = regexp.MustCompile(
		`function ([a-zA-Z0-9_$]+)\(\s*a\s*,\s*b\s*=\s*""\s*\)\s*\{\s*` +
			`(?:if\s*\(\s*a\s*&&\s*[a-zA-Z0-9_$]+\(\s*""\s*,\s*a\s*\)\s*\)\s*return\s*!1\s*;\s*)?` +
			`if\s*\(\s*b\.startsWith\(\s*"image\/"\s*\)\s*\|\|\s*b\.startsWith\(\s*"video\/"\s*\)\s*\|\|\s*b\.startsWith\(\s*"audio\/"\s*\)\s*\|\|\s*` +
			`(?:b\s*===\s*"application\/pdf"|[a-zA-Z0-9_$]+\(\s*b\s*,\s*a\s*\))\s*\|\|\s*[a-zA-Z0-9_$]+\(\s*b\s*,\s*a\s*\)\s*\|\|\s*[a-zA-Z0-9_$]+\(\s*b\s*,\s*a\s*\)\s*\)\s*return\s*!0\s*;\s*` +
			`a\s*=\s*` + extensionOfAExpr + `\s*;\s*return\s+[a-zA-Z0-9_$]+\.has\(\s*a\s*\)\s*\}`,
	)

	fileUploadInputResetRe = regexp.MustCompile(`var\s+([a-zA-Z0-9_$]+)=\(\{onFilesSelected:([a-zA-Z0-9_$]+)\}\)=>\{var\s+([a-zA-Z0-9_$]+)=\(0,([a-zA-Z0-9_$]+)\.useRef\)\(null\),([a-zA-Z0-9_$]+)=\(0,[a-zA-Z0-9_$]+\.useCallback\)\(([a-zA-Z0-9_$]+)=>\{[a-zA-Z0-9_$]+=[a-zA-Z0-9_$]+\.target;(?:[a-zA-Z0-9_$]+\.files\&\&[a-zA-Z0-9_$]+\([a-zA-Z0-9_$]+\.files\)|if\([a-zA-Z0-9_$]+\.files\&\&[a-zA-Z0-9_$]+\.files\.length>0\)\{[\s\S]{1,300}?[a-zA-Z0-9_$]+\([a-zA-Z0-9_$]+\);[a-zA-Z0-9_$]+\.value=""\})\},\[[a-zA-Z0-9_$]+\]\);return\{openFileDialog:\(0,[a-zA-Z0-9_$]+\.useCallback\)\(\(\)=>\{[a-zA-Z0-9_$]+\.current\?\.click\(\)\},\[\]\),fileInputRef:[a-zA-Z0-9_$]+,handleFileChange:[a-zA-Z0-9_$]+\}\};`)

	// fileUploadCustomTextTypesRe finds the function that names the text MIME
	// type of a file from its type and its extension. 2.32 answers text/xml
	// for application/xml first, and looks the extension up only for a name
	// with a dot after its first character.
	fileUploadCustomTextTypesRe = regexp.MustCompile(
		`function ([a-zA-Z0-9_$]+)\(\s*a\s*,\s*b\s*\)\s*\{\s*b\s*=\s*b\.split\(\s*";"\s*\)\[0\]\.trim\(\s*\)\.toLowerCase\(\s*\)\s*;\s*` +
			`(if\s*\(\s*b\s*===\s*"application\/xml"\s*\)\s*return\s*"text\/xml"\s*;\s*)?` +
			`if\s*\(\s*([a-zA-Z0-9_$]+)\.includes\(\s*b\s*\)\s*\)\s*return\s+b\s*;\s*` +
			`(?:a\s*=\s*` + extensionOfAExpr + `\s*;\s*return\s+([a-zA-Z0-9_$]+)\[a\]|` +
			`b\s*=\s*a\.lastIndexOf\(\s*"\."\s*\)\s*;\s*if\s*\(\s*!\s*\(\s*b\s*<=\s*0\s*\)\s*\)\s*return\s+([a-zA-Z0-9_$]+)\[a\.slice\(\s*b\s*\+\s*1\s*\)\.toLowerCase\(\s*\)\])\s*\}\s*` +
			`(function [a-zA-Z0-9_$]+\(\s*a\s*\)\s*\{\s*return\s+[a-zA-Z0-9_$]+\(\s*""\s*,\s*a\s*\)\s*!==\s*void 0\s*\}|` +
			`function [a-zA-Z0-9_$]+\(\s*a\s*,\s*b\s*=\s*""\s*\)\s*\{\s*return\s+[a-zA-Z0-9_$]+\(\s*b\s*,\s*a\s*\)\s*!==\s*void 0\s*\}\s*` +
			`function [a-zA-Z0-9_$]+\(\s*a\s*,\s*b\s*\)\s*\{\s*a\s*=\s*` + extensionOfAExpr + `\s*;\s*if\s*\(\s*a\s*!==\s*"webm")`,
	)

	fileUploadLargeFileStreamingRe        = regexp.MustCompile(`var [a-zA-Z0-9_$]+=\([a-zA-Z0-9_$]+,[a-zA-Z0-9_$]+,[a-zA-Z0-9_$]+,[a-zA-Z0-9_$]+,[a-zA-Z0-9_$]+=5(?:,[a-zA-Z0-9_$]+)?\)=>\{[\s\S]{1,450}?if\([a-zA-Z0-9_$]+>=[a-zA-Z0-9_$]+\)[a-zA-Z0-9_$]+\?\.\("Maximum number of attachments reached"\);(?:else if\([a-zA-Z0-9_$]+\.includes\([a-zA-Z0-9_$]+\.type\.toLowerCase\(\)\)\)|else\{var [a-zA-Z0-9_$]+=[a-zA-Z0-9_$]+\([a-zA-Z0-9_$]+\.name,[a-zA-Z0-9_$]+\.type\);if\([a-zA-Z0-9_$]+\)\{?(?:if\(![a-zA-Z0-9_$]+\([a-zA-Z0-9_$]+,[a-zA-Z0-9_$]+\)\))?)if\(([a-zA-Z0-9_$]+)\.size>10485760\)[a-zA-Z0-9_$]+\?\.\("PDF file size exceeds 10MB limit"\)[\s\S]{1,600}?Error\("Document file size exceeds 10MB limit"\)\);[\s\S]{1,600}?Error\("Zip file size exceeds 20MB limit"\)\);[\s\S]{1,600}?Error\("Text file size exceeds 1MB limit"\)\);[\s\S]{1,1000}?[\s\S]{0,400}?Error\("Audio file size exceeds 20MB limit"\)\);(?:[\s\S]{1,1000}?[\s\S]{0,400}?Error\("File size exceeds 20MB limit"\)\);(?:[\s\S]{1,900}?if\([a-zA-Z0-9_$]+\.length!==0\)\{(?:(?:[a-zA-Z0-9_$]+\?\.\(null\);)?var [a-zA-Z0-9_$]+=[a-zA-Z0-9_$]+-[a-zA-Z0-9_$]+;)?)?)?|if\(([a-zA-Z0-9_$]+)\)if\(([a-zA-Z0-9_$]+)\.size>1048576\)(?:console\.error\("Text file size exceeds 1MB limit"\);|[a-zA-Z0-9_$]+\?\.\("Text file size exceeds 1MB limit"\),[\r\n\s]*[a-zA-Z0-9_$]+\((?:[a-zA-Z0-9_$]+\.VALIDATION_CHECK_FAILED|"validation_check_failed"),[\r\n\s]*Error\("Text file size exceeds 1MB limit"\)\);)`)
	fileUploadIcbHeaderRe                 = regexp.MustCompile(`(var [a-zA-Z0-9_$]+=\([a-zA-Z0-9_$]+,([a-zA-Z0-9_$]+),[a-zA-Z0-9_$]+,[a-zA-Z0-9_$]+,([a-zA-Z0-9_$]+)=5(?:,[a-zA-Z0-9_$]+)?\)=>\{)([\s\S]{1,350}?async ([a-zA-Z0-9_$]+)=>\{[\s\S]{1,150}?)if\(([a-zA-Z0-9_$]+)>=([a-zA-Z0-9_$]+)\)([a-zA-Z0-9_$]+\?\.\("Maximum number of attachments reached"\));`)
	fileUploadVideoBranchRe               = regexp.MustCompile(`else if\(([a-zA-Z0-9_$]+)\.includes\(([a-zA-Z0-9_$]+)\.type\.toLowerCase\(\)\)\)(?:(\{if\(![a-zA-Z0-9_$]+\([a-zA-Z0-9_$]+,[a-zA-Z0-9_$]+\.type\.toLowerCase\(\)\)\))try\{|try\{)`)
	fileUploadSizeLimitBranchRe           = regexp.MustCompile(`if\(([a-zA-Z0-9_$]+)\.size>(10485760|20971520|1048576)\)[a-zA-Z0-9_$]+\?\.\("(?:(?:PDF|Document|Zip|Text|Audio) file|File) size exceeds \d+MB limit"\),[\r\n\s]*[a-zA-Z0-9_$]+\((?:[a-zA-Z0-9_$]+\.VALIDATION_CHECK_FAILED|"validation_check_failed"),[\r\n\s]*Error\("(?:(?:PDF|Document|Zip|Text|Audio) file|File) size exceeds \d+MB limit"\)\);`)
	fileUploadBatchEntryRe                = regexp.MustCompile(`if\(([a-zA-Z0-9_$]+)\.length!==0\)\{(([a-zA-Z0-9_$]+\?\.\(null\);)?var ([a-zA-Z0-9_$]+)=([a-zA-Z0-9_$]+)-([a-zA-Z0-9_$]+);)?`)
	virtualizationDisableContractionRe    = regexp.MustCompile(`contractionSafetyPx:3E3,outerRadiusPx:5E3`)
	questionModalWriteInRadioRe           = regexp.MustCompile(`(value:"__write_in__",checked:([a-zA-Z0-9_$]+),onChange:\(\)=>\{(?:var|let|const)\s+([a-zA-Z0-9_$]+)=)[\r\n\s]*!([a-zA-Z0-9_$]+)(;(?:[a-zA-Z0-9_$]+\|\|\([a-zA-Z0-9_$]+\.current=!1,[a-zA-Z0-9_$]+\(\)\);)?[a-zA-Z0-9_$]+\([a-zA-Z0-9_$]+\);[a-zA-Z0-9_$]+&&\(([a-zA-Z0-9_$]+)\.isMultiSelect\|\|)`)
	questionModalWriteInFocusRe           = regexp.MustCompile(`(?:(onClick:\(\)=>\{([a-zA-Z0-9_$]+)\|\|[\r\n\s]*\(([a-zA-Z0-9_$]+)\(!0\),([a-zA-Z0-9_$]+)\.isMultiSelect\|\|([a-zA-Z0-9_$]+)\(\)\)\})(,onChange:))|("data-testid"\s*:\s*"ask-question-writein"\s*,\s*value\s*:\s*[a-zA-Z0-9_$]+\s*,\s*onClick\s*:\s*([a-zA-Z0-9_$]+))(\s*,\s*onChange\s*:)`)
	questionModalPreventRadioFocusStealRe = regexp.MustCompile(`(if\((?:document\.hasFocus\(\)&&)?![a-zA-Z0-9_$]+\.isMultiSelect&&![a-zA-Z0-9_$]+&&[a-zA-Z0-9_$]+\.length>0)(\)\{(?:var|let|const)\s+[a-zA-Z0-9_$]+=[a-zA-Z0-9_$]+\.current\.get\([a-zA-Z0-9_$]+\[0\]\);[a-zA-Z0-9_$]+&&[a-zA-Z0-9_$]+\.focus\(\)\})|([a-zA-Z0-9_$]+\s*\|\|\s*[a-zA-Z0-9_$]+\s*===\s*void 0\s*\|\|\s*[a-zA-Z0-9_$]+\.isMultiSelect\s*\|\|\s*!document\.hasFocus\(\)\s*\|\|\s*[a-zA-Z0-9_$]+\(\s*[a-zA-Z0-9_$]+\s*\)\s*&&\s*[a-zA-Z0-9_$]+\s*!==\s*[a-zA-Z0-9_$]+\.current)(\s*\|\|\s*[a-zA-Z0-9_$]+\.current\.get\(\s*[a-zA-Z0-9_$]+\s*\)\?\.focus\(\))`)
	autoscrollDistanceFixRe               = regexp.MustCompile(`return\s+([a-zA-Z0-9_$]+)\?\(([a-zA-Z0-9_$]+)\.current\?[a-zA-Z0-9_$]+\.current\([a-zA-Z0-9_$]+\):[a-zA-Z0-9_$]+\.scrollHeight-[a-zA-Z0-9_$]+\.clientHeight-[a-zA-Z0-9_$]+\.scrollTop\)<=([a-zA-Z0-9_$]+):!1`)
	autoscrollUnpinInputRe                = regexp.MustCompile(`(var ([a-zA-Z0-9_$]+)=\s*[a-zA-Z0-9_$]+\.current;\s*if\(\s*[a-zA-Z0-9_$]+\)\s*\{\s*var [a-zA-Z0-9_$]+=\s*[a-zA-Z0-9_$]+\.scrollTop,[\s\S]{1,200}?[a-zA-Z0-9_$]+<\s*(?:[a-zA-Z0-9_$]+\.current|0)\s*&&\s*!\s*[a-zA-Z0-9_$]+\s*\?\s*)(?:\(\s*([a-zA-Z0-9_$]+)\.current=\s*!1,\s*([a-zA-Z0-9_$]+\.delete\(\s*[a-zA-Z0-9_$]+\s*\))\s*\)|([a-zA-Z0-9_$]+)\.current=\s*!1)\s*:\s*((?:[a-zA-Z0-9_$]+\s*&&\s*!\s*([a-zA-Z0-9_$]+)\.current\s*&&\s*|!\s*[a-zA-Z0-9_$]+\s*\|\|\s*([a-zA-Z0-9_$]+)\.current\s*\|\|\s*)?)`)
	pagingTrimWaitsRe                     = regexp.MustCompile(`(([a-zA-Z0-9_$]+)\s*=\s*([a-zA-Z0-9_$]+)\.startIndex\s*<\s*([a-zA-Z0-9_$]+)\.startIndex\s*\|\|\s*[a-zA-Z0-9_$]+\.endIndexExclusive\s*>\s*[a-zA-Z0-9_$]+\.endIndexExclusive\s*;\s*if\(\s*[a-zA-Z0-9_$]+\(\s*[a-zA-Z0-9_$]+\s*,\s*[a-zA-Z0-9_$]+\s*,\s*[a-zA-Z0-9_$]+\s*\)\s*\)\s*[a-zA-Z0-9_$]+\s*&&\s*([a-zA-Z0-9_$]+)\(\s*[a-zA-Z0-9_$]+\.timing\.oppositeDirectionCooldownMs\s*\|\|\s*[a-zA-Z0-9_$]+\.timing\.retryDelayMs\s*\)\s*;\s*)else\s*\{`)
	pagingGrowRadiusRe                    = regexp.MustCompile(`(trigger\s*:\s*\{\s*innerRadiusPx\s*:\s*)([0-9][0-9.eE]*)(\s*,\s*contractionSafetyPx\s*:)`)
	pagingRowsKeptRe                      = regexp.MustCompile(`(componentDidUpdate\([a-zA-Z0-9_$]+,\s*[a-zA-Z0-9_$]+,\s*[a-zA-Z0-9_$]+\)\{([a-zA-Z0-9_$]+)=\s*this\.props\.viewportRef\.current;\s*([a-zA-Z0-9_$]+)=\s*[a-zA-Z0-9_$]+\?\.find\(\(\{element:\s*[a-zA-Z0-9_$]+\}\s*\)=>\s*[a-zA-Z0-9_$]+\.isConnected\);\s*)([a-zA-Z0-9_$]+&&\s*[a-zA-Z0-9_$]+&&\s*\([a-zA-Z0-9_$]+=\s*[a-zA-Z0-9_$]+\.element\.getBoundingClientRect\(\)\.top-\s*[a-zA-Z0-9_$]+\.getBoundingClientRect\(\)\.top-\s*[a-zA-Z0-9_$]+\.offset,\s*Math\.abs\([a-zA-Z0-9_$]+\)>=\s*\.5&&\s*\([a-zA-Z0-9_$]+\.scrollTop\+=\s*[a-zA-Z0-9_$]+\)\))\}`)
	scrollbarMeasureRe                    = regexp.MustCompile(`(var\s+([a-zA-Z0-9_$]+)=\s*([a-zA-Z0-9_$]+)\?\s*([a-zA-Z0-9_$]+)\.scrollLeft:\s*[a-zA-Z0-9_$]+\.scrollTop,\s*([a-zA-Z0-9_$]+)=\s*[a-zA-Z0-9_$]+\?\s*[a-zA-Z0-9_$]+\.scrollWidth:\s*[a-zA-Z0-9_$]+\.scrollHeight,\s*[a-zA-Z0-9_$]+=\s*[a-zA-Z0-9_$]+\?\s*[a-zA-Z0-9_$]+\.clientWidth:\s*[a-zA-Z0-9_$]+\.clientHeight;\s*[a-zA-Z0-9_$]+\.pos=\s*[a-zA-Z0-9_$]+;\s*[a-zA-Z0-9_$]+\.scrollLength=\s*[a-zA-Z0-9_$]+;\s*[a-zA-Z0-9_$]+\.clientLength=\s*[a-zA-Z0-9_$]+;)`)
	scrollbarTrackClickRe                 = regexp.MustCompile(`(Math\.min\(\s*[a-zA-Z0-9_$]+,\s*Math\.max\(\s*0,\s*[a-zA-Z0-9_$]+-\s*[a-zA-Z0-9_$]+/2\)\)/\s*[a-zA-Z0-9_$]+\*\s*[a-zA-Z0-9_$]+,\s*[a-zA-Z0-9_$]+\?\s*([a-zA-Z0-9_$]+)\.scrollLeft=\s*[a-zA-Z0-9_$]+:\s*[a-zA-Z0-9_$]+\.scrollTop=\s*)([a-zA-Z0-9_$]+)`)
	scrollbarTimelineRe                   = regexp.MustCompile(`(function\s+[a-zA-Z0-9_$]+\(\)\{var\s+[a-zA-Z0-9_$]+=\s*globalThis\.ScrollTimeline;\s*return\s+typeof\s+[a-zA-Z0-9_$]+===\s*"function")(\?\s*[a-zA-Z0-9_$]+:\s*null\})`)
	scrollbarDragRe                       = regexp.MustCompile(`([a-zA-Z0-9_$]+)=\s*([a-zA-Z0-9_$]+)\.current\.startScrollPos\s*\+\s*\(\s*\(\s*([a-zA-Z0-9_$]+)\s*\?\s*[a-zA-Z0-9_$]+\.clientX\s*:\s*([a-zA-Z0-9_$]+)\.clientY\s*\)\s*-\s*[a-zA-Z0-9_$]+\.current\.startCoord\s*\)\s*/\s*([a-zA-Z0-9_$]+)\s*\*\s*([a-zA-Z0-9_$]+(?:\.current)?)\.maxScrollPos\s*,\s*[a-zA-Z0-9_$]+\s*\?\s*([a-zA-Z0-9_$]+)\.scrollLeft=\s*[a-zA-Z0-9_$]+\s*:\s*[a-zA-Z0-9_$]+\.scrollTop=\s*[a-zA-Z0-9_$]+\s*,`)
	websocketTransportDefaultRe           = regexp.MustCompile(`(function [a-zA-Z0-9_$]+\(\)\{(?:var|let|const)\s+[a-zA-Z0-9_$]+=new URLSearchParams\((?:window\.)?location\.search\),[a-zA-Z0-9_$]+=[a-zA-Z0-9_$]+\.get\("useWebSocket"\);return [a-zA-Z0-9_$]+!==null\?[a-zA-Z0-9_$]+==="true")(?:(\?"explicit":"off")(:[a-zA-Z0-9_$]+\.get\("wsTransport"\))==="3"\?"hub":"off"|(:[a-zA-Z0-9_$]+\.get\("wsTransport"\))==="2")\}`)
	websocketLivenessProbeRe              = regexp.MustCompile(`(type:\s*"ping"\s*\}\s*\)\s*\)\s*;\s*var\s+[a-zA-Z0-9_$]+\s*=\s*[a-zA-Z0-9_$]+\.inboundFrameSeq\s*;\s*[a-zA-Z0-9_$]+\.probeTimer\s*=\s*setTimeout\(\s*\(\s*\)\s*=>\s*\{[\s\S]{0,400}?no frames for \d+ms and ping unanswered; closing socket[\s\S]{0,200}?\}\s*,\s*)(\d+(?:E\d+)?)(\)\s*\}\s*else\s+[a-zA-Z0-9_$]+\s*\(\s*[a-zA-Z0-9_$]+\s*\)\s*\}\s*,\s*\d+(?:E\d+)?\s*\)\s*\})`)
	bootstrapLocalUserInfoRetryRe         = regexp.MustCompile(`async\s+function\s+([a-zA-Z0-9_$]+)\(a\)\{var\s+b=([de]),c=[de];try\{let\s+([de])=c\(await\s+b\(a\.lsClient\.getLocalUserInfo\(([a-zA-Z0-9_$]+\([a-zA-Z0-9_$]+,\{\}\))\)\)\),f=[de]\.username;return\{homeDirUri:([a-zA-Z0-9_$]+)\([de]\.homeDirUri\),username:f\}\}finally\{b\(\)\}\}`)
	authStartupRetryRe                    = regexp.MustCompile(
		`(_authState\.state\s*===\s*"uninitialized"\s*\)\s*\{\s*(?:[a-zA-Z0-9_$]+(?:\.[a-zA-Z0-9_$]+)*\s*=\s*[a-zA-Z0-9_$]+\(\s*[a-zA-Z0-9_$]+\s*\)\s*;\s*)?` +
			`let\s+[a-zA-Z0-9_$]+\s*=\s*[a-zA-Z0-9_$]+\(\s*[a-zA-Z0-9_$]+\s*\)(?:\s*,[^;{}]{1,400}?)?\s*;\s*if\s*\(\s*([a-zA-Z0-9_$]+)\s*>\s*0\s*&&\s*[a-zA-Z0-9_$]+(?:\s*&&\s*![a-zA-Z0-9_$]+)?\s*\)\s*\{\s*let\s+[a-zA-Z0-9_$]+\s*=\s*4\s*-\s*[a-zA-Z0-9_$]+\s*;\s*` +
			`[a-zA-Z0-9_$]+\(\s*await\s+[a-zA-Z0-9_$]+\(\s*new Promise\(\s*[a-zA-Z0-9_$]+\s*=>\s*\{\s*` + authStartupBackoffExpr + `\s*\}\s*\)\s*\)\s*\)\s*;\s*` +
			`return\s+([a-zA-Z0-9_$]+)\.isLoggingOut\s*\|\|\s*[a-zA-Z0-9_$]+(?:\._currentSessionId|\.sessionTracker\.id)\s*!==\s*[a-zA-Z0-9_$]+\s*\?\s*void 0\s*:\s*[a-zA-Z0-9_$]+\(\s*[a-zA-Z0-9_$]+\s*,\s*)` +
			`[a-zA-Z0-9_$]+\s*-\s*1(\s*\)\s*\})`,
	)
	// userStatusNoResetOnErrorRe matches builds where UserStatusSync logs a
	// failed getUserStatus call without wiping Redux userStatus: 2.28 and
	// earlier, or a future build that drops the reset. Anchoring from catch{
	// ensures a reset moved before console.warn does not slip past.
	userStatusNoResetOnErrorRe = regexp.MustCompile(`catch\s*\(\s*[a-zA-Z0-9_$]+\s*\)\s*\{\s*(?:[a-zA-Z0-9_$]+\(\)\s*[;,]\s*)?console\.warn\(\s*"\[UserStatusSync\] Failed to fetch user status:"\s*,\s*[a-zA-Z0-9_$]+\s*\)(?:\s*,\s*!?\s*[a-zA-Z0-9_$]+\(\)\s*(?:\|\||&&)\s*[a-zA-Z0-9_$]+(?:\?\.)?\(\))?\s*\}`)
	// authStartupBackoffRe finds the wait between tries of the startup sign-in
	// check, in any bundle that retries it.
	authStartupBackoffRe    = regexp.MustCompile(`_authState\.state\s*===\s*"uninitialized"\s*\)\s*\{[\s\S]{0,400}?` + authStartupBackoffExpr)
	userStatusKeepOnErrorRe = regexp.MustCompile(
		`(?:catch\s*\(\s*([a-zA-Z0-9_$]+)\s*\)\s*\{\s*` +
			`([a-zA-Z0-9_$]+\(\)\s*,\s*console\.warn\(\s*"\[UserStatusSync\] Failed to fetch user status:"\s*,\s*[a-zA-Z0-9_$]+\s*\))\s*,\s*` +
			`(!?\s*[a-zA-Z0-9_$]+\(\))\s*(\|\||&&)\s*` +
			`(?:\(\s*` + userStatusClearModeExpr + userStatusSchemaDispatchExpr + `\s*,\s*([a-zA-Z0-9_$]+(?:\?\.)?\(\))\s*\)|` + userStatusClearModeExpr + userStatusSchemaDispatchExpr + `)\s*\}` +
			// 2.33: if(<warn>,!<cancelled>()){var p=<permission error>;p?<show it>:(<mode>!=="clearStatus"||<loaded>?.()||<clear>,<credential check>)}
			`|catch\s*\(\s*([a-zA-Z0-9_$]+)\s*\)\s*\{\s*if\s*\(\s*` +
			`([a-zA-Z0-9_$]+\(\)\s*,\s*console\.warn\(\s*"\[UserStatusSync\] Failed to fetch user status:"\s*,\s*[a-zA-Z0-9_$]+\s*\))\s*,\s*` +
			`(!\s*[a-zA-Z0-9_$]+\(\))\s*\)\s*\{\s*` +
			`((?:var|let)\s+[a-zA-Z0-9_$]+\s*=[^;{}]{1,200};\s*[a-zA-Z0-9_$]+\s*\?\s*[a-zA-Z0-9_$]+\(\s*[a-zA-Z0-9_$]+\(\s*[a-zA-Z0-9_$]+\s*\)\s*\)\s*:\s*)` +
			`\(\s*[a-zA-Z0-9_$]+\s*!==\s*"clearStatus"\s*\|\|\s*[a-zA-Z0-9_$]+\?\.\(\)\s*\|\|\s*` + userStatusSchemaDispatchExpr + `\s*,\s*([a-zA-Z0-9_$]+(?:\?\.)?\(\))\s*\)\s*\}\s*\})` +
			`(\s*\}\s*finally\s*\{\s*[a-zA-Z0-9_$]+\(\)\s*\}\s*\}[\s\S]{1,1000}?[\s\S]{0,1000}?state\s*===\s*"signedOut"\s*\)\s*` + userStatusSchemaDispatchExpr + `\s*;\s*else\s*\{\s*(?:var|let)\s+([a-zA-Z0-9_$]+)\s*=\s*!1\s*,[\s\S]{1,300}?;\s*)` +
			`(` + userStatusSyncCallExpr + `)\s*;\s*(?:var|let)\s+([a-zA-Z0-9_$]+)\s*=\s*setInterval\(\s*\(\s*\)\s*=>\s*(?:\{\s*` + userStatusSyncCallExpr + `\s*\}|(?:void\s+)?` + userStatusSyncCallExpr + `)\s*,\s*6E5\s*\)\s*;\s*` +
			`return\s*\(\s*\)\s*=>\s*\{\s*[a-zA-Z0-9_$]+\s*=\s*!0\s*[;,]\s*clearInterval\(\s*[a-zA-Z0-9_$]+\s*\)\s*\}`,
	)

	sidecarResolveURLRe = regexp.MustCompile(
		`(resolveSidecarUrl:\s*\(\s*[a-zA-Z0-9_$]+\s*,\s*[a-zA-Z0-9_$]+\s*\)\s*=>\s*\{[\s\S]{1,360}?var\s+[a-zA-Z0-9_$]+\s*=\s*[a-zA-Z0-9_$]+\s*\?\s*` +
			"`plugin://\\$\\{[a-zA-Z0-9_$]+\\}\\$\\{[a-zA-Z0-9_$]+\\.pathname\\}\\$\\{[a-zA-Z0-9_$]+\\.search\\}`" +
			`\s*:\s*)` +
			`([a-zA-Z0-9_$]+\.toString\(\))` +
			`(\s*;\s*break\s+[a-zA-Z0-9_$]+\s*\}\s*catch(?:\s*\(\s*[a-zA-Z0-9_$]+\s*\))?\s*\{\s*[a-zA-Z0-9_$]+\s*=\s*\(\s*[a-zA-Z0-9_$]+\s*=\s*[a-zA-Z0-9_$]+\[[a-zA-Z0-9_$]+\]\s*\)\s*\?\s*` +
			"`plugin://\\$\\{[a-zA-Z0-9_$]+\\}\\$\\{[a-zA-Z0-9_$]+\\}`" +
			`\s*:\s*)` +
			`(` + "`\\$\\{[a-zA-Z0-9_$]+\\}\\$\\{[a-zA-Z0-9_$]+\\}`" + `)`,
	)
	// sidecarCommentOriginRe finds the sidecar panel's check that an
	// add-artifact-comment message comes from the iframe's origin.
	sidecarCommentOriginRe = regexp.MustCompile(
		`(new URL\(\s*([a-zA-Z0-9_$]+)\.src\s*\)[^{}]{0,120}\}\s*catch(?:\s*\([a-zA-Z0-9_$]+\))?\s*\{\s*\}\s*[a-zA-Z0-9_$]+\s*=\s*void 0\s*\}\s*)` +
			`([a-zA-Z0-9_$]+)\s*=\s*([a-zA-Z0-9_$]+)\s*===\s*([a-zA-Z0-9_$]+)` +
			`(\s*\}\s*if\s*\(\s*[a-zA-Z0-9_$]+\s*&&\s*([a-zA-Z0-9_$]+)\.data\?\.type\s*===\s*"add-artifact-comment")`,
	)
	// sidecarIframeSandboxRe finds where the sidecar panel creates its iframe,
	// before it sets the frame's src.
	sidecarIframeSandboxRe = regexp.MustCompile(
		`(let\s+([a-zA-Z0-9_$]+)\s*=\s*document\.createElement\("iframe"\)\s*;)` +
			`(\s*[a-zA-Z0-9_$]+\.setAttribute\(\s*"allow"\s*,\s*"clipboard-read;)`,
	)
	sidecarModalEndpointRe = regexp.MustCompile(
		`([a-zA-Z0-9_$]+\s*=\s*)` +
			`([a-zA-Z0-9_$]+\.baseUrl\s*\|\|\s*\(\s*[a-zA-Z0-9_$]+\.webPort\s*\?\s*` +
			"`http://127\\.0\\.0\\.1:\\$\\{[a-zA-Z0-9_$]+\\.webPort\\}`" +
			`\s*:\s*"(?:\\u2014|—)"\s*\))`,
	)
	loopbackPreviewFeatureGateRe = regexp.MustCompile(
		`(browserAuxPane:\s*\{\s*feature:\s*!1\s*,\s*applyFirst:\s*\[\s*\{\s*runtimeGating:\s*\{\s*host:\s*\[[^\]]+\])` +
			`\s*,\s*userCohort:\s*\[\s*"google"\s*\]` +
			`(\s*\}\s*,\s*feature:\s*)[a-zA-Z0-9_$]+` +
			`(\s*\}\s*\]\s*,\s*applyAll:\s*\[[\s\S]{1,260}?\}\s*,\s*remoteConversationBrowserPane:\s*\{\s*feature:\s*!1\s*,\s*applyAll:\s*\[\s*\{\s*runtimeGating:\s*\{[^}]+\}\s*,\s*feature:\s*([a-zA-Z0-9_$]+))`,
	)
	loopbackPreviewFetchFallbackRe = regexp.MustCompile(
		`(,\s*fetchSandboxPort:\s*\(\s*\.\.\.([a-zA-Z0-9_$]+)\s*\)\s*=>\s*([a-zA-Z0-9_$]+)\(\s*async\s+([a-zA-Z0-9_$]+)\s*=>\s*)` +
			`[a-zA-Z0-9_$]+\.fetchSandboxPort\(\s*\.\.\.[a-zA-Z0-9_$]+\s*\)` +
			`(\s*\)\s*,)`,
	)
	routerNavigateBridgeRe = regexp.MustCompile(
		`(function\s+[a-zA-Z0-9_$]+\(\)\s*\{\s*(?:var|let|const)\s+([a-zA-Z0-9_$]+)\s*=\s*[a-zA-Z0-9_$]+\(\)\s*;)` +
			`(\s*return\s+(?:\(0,\s*[a-zA-Z0-9_$]+\.useCallback\)|[a-zA-Z0-9_$]+\.useCallback)\(\s*[a-zA-Z0-9_$]+\s*=>\s*[a-zA-Z0-9_$]+\.navigate\(\s*\{\s*\.\.\.[a-zA-Z0-9_$]+\s*,\s*from:\s*[a-zA-Z0-9_$]+\.from\s*\?\?\s*void 0\s*\}\s*\))`,
	)
	pluginMentionRemoteSignInRe = regexp.MustCompile(
		`(function\s+[a-zA-Z0-9_$]+\([a-zA-Z0-9_$]+\)\s*\{\s*var\s+([a-zA-Z0-9_$]+)\s*=\s*[a-zA-Z0-9_$]+\(\)\s*,\s*\{\s*startPluginAuthSignIn:\s*([a-zA-Z0-9_$]+)\s*\}[\s\S]{1,120}?,\s*([a-zA-Z0-9_$]+)\s*=\s*)` +
			`([a-zA-Z0-9_$]+)\s*&&\s*([a-zA-Z0-9_$]+\([a-zA-Z0-9_$]+\.runtimeEnvironment\)\s*===\s*1)` +
			// The replacement ends the var statement before reassigning the
			// flag, so the declaration must end here too. Otherwise a later
			// name in it would become an undeclared assignment.
			`(\s*;[\s\S]{1,180}?useCallback\)?\(\s*([a-zA-Z0-9_$]+)\s*=>\s*\{\s*)` +
			`[a-zA-Z0-9_$]+\s*&&\s*[a-zA-Z0-9_$]+\s*&&\s*([a-zA-Z0-9_$]+\(\s*\{\s*plugin:\s*[a-zA-Z0-9_$]+\.plugin\s*,\s*provider:\s*[a-zA-Z0-9_$]+\.provider)`,
	)
	mcpOAuthRemoteCallbackRe = regexp.MustCompile(
		`(completeMcpOAuth:\s*async\s*\(\s*([a-zA-Z0-9_$]+)\s*,\s*([a-zA-Z0-9_$]+)\s*\)\s*=>\s*)` +
			`([a-zA-Z0-9_$]+\(\s*async\s+[a-zA-Z0-9_$]+\s*=>\s*\{[\s\S]{1,180}?completeMcpOAuth\(\s*[a-zA-Z0-9_$]+\(\s*[a-zA-Z0-9_$]+\s*,\s*\{\s*serverName:\s*[a-zA-Z0-9_$]+\s*,\s*authorizationCode:\s*)` +
			`[a-zA-Z0-9_$]+` +
			`(\s*\}\s*\)\s*\)\s*\)\s*\)\s*\}\s*finally\s*\{\s*[a-zA-Z0-9_$]+\(\)\s*\}\s*\}\s*\))`,
	)
	pluginOAuthRemoteCallbackRe = regexp.MustCompile(
		`(signInPluginAuthProvider:\s*async\s+([a-zA-Z0-9_$]+)\s*=>\s*)` +
			`([a-zA-Z0-9_$]+\(\s*[a-zA-Z0-9_$]+\s*=>\s*[a-zA-Z0-9_$]+\.signInPluginAuthProvider\(\s*[a-zA-Z0-9_$]+\(\s*[a-zA-Z0-9_$]+\s*,\s*)` +
			`[a-zA-Z0-9_$]+` +
			`(\s*\)\s*\)\s*\))`,
	)

	// sidePanelBodyRe finds where the side panel renders its active tab, or an
	// empty pane when it has none.
	sidePanelBodyRe = regexp.MustCompile(
		`("data-side-panel-body"\s*:\s*!0\s*\}\s*,\s*)([a-zA-Z0-9_$]+)\s*\?\s*` +
			`(([a-zA-Z0-9_$]+)\.createElement\(\s*"div"\s*,\s*\{\s*className\s*:\s*"h-full w-full"\s*\}\s*,\s*` +
			`[a-zA-Z0-9_$]+\.createElement\(\s*[a-zA-Z0-9_$]+\s*,\s*\{\s*key\s*:\s*[a-zA-Z0-9_$]+\.id\s*,\s*(?:treeId|panelScope)\s*:\s*([a-zA-Z0-9_$]+)\s*,\s*` +
			`tabId\s*:\s*[a-zA-Z0-9_$]+\.id\s*,\s*tab\s*:\s*[a-zA-Z0-9_$]+\s*\}\s*\)\s*\))\s*:\s*` +
			`([a-zA-Z0-9_$]+\.createElement\(\s*([a-zA-Z0-9_$]+)\s*,\s*\{\s*(treeId|panelScope)\s*:\s*[a-zA-Z0-9_$]+\s*,\s*isPaneOpen\s*:\s*([a-zA-Z0-9_$]+)\s*\}\s*\))`,
	)
	// sidePanelBodyLegacyRe is the same place in the side panel used when
	// auxPaneComponent.redesignEnabled is off. The terminal hint before it
	// renders only while the panel is open.
	sidePanelBodyLegacyRe = regexp.MustCompile(
		`(([a-zA-Z0-9_$]+)\s*&&\s*[a-zA-Z0-9_$]+\.some\(\s*[a-zA-Z0-9_$]+\s*=>\s*[a-zA-Z0-9_$]+\.id\s*===\s*"terminal"\s*\)\s*&&\s*` +
			`[a-zA-Z0-9_$]+\.createElement\(\s*[a-zA-Z0-9_$]+\s*,\s*\{\s*stripRef\s*:\s*[a-zA-Z0-9_$]+\s*,\s*activeTabId\s*:\s*[a-zA-Z0-9_$]+\s*\}\s*\)\s*,\s*` +
			`[a-zA-Z0-9_$]+\.createElement\(\s*"div"\s*,\s*\{\s*className\s*:\s*"flex-grow overflow-hidden"\s*,\s*"data-side-panel-body"\s*:\s*!0\s*\}\s*,\s*)` +
			`([a-zA-Z0-9_$]+)\s*&&\s*` +
			`(([a-zA-Z0-9_$]+)\.createElement\(\s*"div"\s*,\s*\{\s*className\s*:\s*"h-full w-full"\s*\}\s*,\s*` +
			`[a-zA-Z0-9_$]+\.createElement\(\s*[a-zA-Z0-9_$]+\s*,\s*\{\s*key\s*:\s*[a-zA-Z0-9_$]+\.id\s*,\s*(?:treeId|panelScope)\s*:\s*([a-zA-Z0-9_$]+)\s*,\s*` +
			`tabId\s*:\s*[a-zA-Z0-9_$]+\.id\s*,\s*tab\s*:\s*[a-zA-Z0-9_$]+\s*\}\s*\)\s*\))`,
	)
	// conversationNavTabRe finds where opening a conversation picks the tab
	// for the URL from the conversation's side panel. Only the mobile layout
	// asks whether that panel is open.
	conversationNavTabRe = regexp.MustCompile(
		`([a-zA-Z0-9_$]+\s*=\s*)![a-zA-Z0-9_$]+\s*\|\|\s*` +
			`([a-zA-Z0-9_$]+\.sidePanel\s*&&\s*[a-zA-Z0-9_$]+\(\s*[a-zA-Z0-9_$]+\s*,\s*[a-zA-Z0-9_$]+\s*\)\s*\?\s*[a-zA-Z0-9_$]+\s*\?\s*` +
			`[a-zA-Z0-9_$]+\.sidePanelV2\.conversationPanes\[\s*[a-zA-Z0-9_$]+\s*\]\?\.activeTabId\s*:\s*` +
			`[a-zA-Z0-9_$]+\.sidePanel\.conversationPanes\[\s*[a-zA-Z0-9_$]+\s*\]\?\.activeTabId\s*:\s*void 0)`,
	)
	// conversationNavOpenCheckSkippedRe finds the open check that the desktop
	// layout skips. Bundles before it, such as 2.29, never check the panel,
	// and a bundle that drops the skip has fixed this itself.
	conversationNavOpenCheckSkippedRe = regexp.MustCompile(`![a-zA-Z0-9_$]+\s*\|\|\s*[a-zA-Z0-9_$]+\.sidePanel\s*&&\s*[a-zA-Z0-9_$]+\(\s*[a-zA-Z0-9_$]+\s*,\s*[a-zA-Z0-9_$]+\s*\)\s*\?`)
)

// SidecarFrameSandbox is the sandbox of a sidecar's frame: the iframe's
// attribute, and the proxy's CSP sandbox for every sidecar response.
const SidecarFrameSandbox = "allow-scripts allow-forms allow-popups allow-popups-to-escape-sandbox allow-modals allow-downloads"

const (
	userStatusSchemaDispatchExpr = `[a-zA-Z0-9_$]+\(\s*[a-zA-Z0-9_$]+\(\s*[a-zA-Z0-9_$]+\(\s*[a-zA-Z0-9_$]+\.UserStatusSchema\s*\)\s*\)\s*\)`
	userStatusSyncCallExpr       = `[a-zA-Z0-9_$]+\(\s*[a-zA-Z0-9_$]+(?:\s*,\s*(?:[a-zA-Z0-9_$]+|"[a-zA-Z]+")){3,7}\s*\)`
	// userStatusClearModeExpr is the mode check 2.32 puts before the dispatch
	// that clears the status. The poll runs in that mode, so it still clears.
	userStatusClearModeExpr = `(?:[a-zA-Z0-9_$]+\s*===\s*"clearStatus"\s*&&\s*)?`
	// extensionOfAExpr is how the bundle reads the lower-cased extension of the
	// file name in a.
	extensionOfAExpr       = `a\.slice\(\s*a\.lastIndexOf\(\s*"\."\s*\)\s*\+\s*1\s*\)\.toLowerCase\(\s*\)`
	authStartupBackoffExpr = `setTimeout\(\s*[a-zA-Z0-9_$]+\s*,\s*1E3\s*\*\s*2\s*\*\*\s*[a-zA-Z0-9_$]+\s*\)`
)

func renderUserStatusKeepOnError(errVar, warnExpr, credCheck, bridge, cancelVar, callExpr, timerVar string) string {
	return renderUserStatusRetry(`catch(`+errVar+`){return `+warnExpr+credCheck+`,!1}`, bridge, cancelVar, callExpr, timerVar)
}

// renderUserStatusRetry writes the effect that retries a failed refresh, after the catch that says it failed.
func renderUserStatusRetry(catchBlock, bridge, cancelVar, callExpr, timerVar string) string {
	return catchBlock +
		bridge +
		`let _agyRetry,_agyInFlight=!1,_agyDeferred=!1,_agyStale=!1,_agyQueued=!1,_agyFails=0,_agyLastOk=0,` +
		`_agyHidden=()=>typeof document!=="undefined"&&document.visibilityState==="hidden",` +
		`_agyRun=async _init=>{` +
		`if(` + cancelVar + `||_agyInFlight)return;` +
		`if(!_init&&_agyHidden()){_agyDeferred=!0;return}` +
		`clearTimeout(_agyRetry);_agyRetry=void 0;_agyDeferred=!1;_agyStale=!1;_agyInFlight=!0;` +
		`let _ok=!1;try{_ok=(await ` + callExpr + `)!==!1}catch{_ok=!1}` +
		`_agyInFlight=!1;` +
		`if(` + cancelVar + `)return;` +
		`if(_agyQueued){` +
		`_agyQueued=!1;` +
		`if(_agyHidden()){_agyDeferred=!0;return}` +
		`_agyFails=0;_agyRun(!1);return` +
		`}` +
		`if(!_ok){` +
		`if(_agyHidden()){_agyDeferred=!0;return}` +
		`_agyFails++;` +
		`clearTimeout(_agyRetry);` +
		`_agyRetry=setTimeout(()=>{_agyRetry=void 0;_agyRun(!1)},Math.min(6E4,1E3*(2**Math.min(_agyFails-1,6))))` +
		`}else{_agyDeferred=!1;_agyFails=0;_agyLastOk=Date.now()}` +
		`},` +
		`_agyWake=_ev=>{` +
		`if(` + cancelVar + `)return;` +
		`if(_agyHidden()||_ev?.type==="pagehide"){if(_agyInFlight)_agyStale=!0;return}` +
		`let _now=Date.now();` +
		`if(_agyDeferred||_agyFails>0||_agyStale||_ev?.persisted||_ev?.type==="online"||_now<_agyLastOk||_now-_agyLastOk>=6E4){` +
		`if(_agyInFlight){if(_agyStale||_ev?.type==="online"){_agyFails=0;_agyQueued=!0}}` +
		`else{_agyFails=0;_agyRun(!1)}` +
		`}` +
		`};` +
		`_agyRun(!0);` +
		`var ` + timerVar + `=setInterval(()=>{_agyRun(!1)},6E5);` +
		`typeof document!=="undefined"&&document.addEventListener&&document.addEventListener("visibilitychange",_agyWake);` +
		`typeof window!=="undefined"&&window.addEventListener&&(window.addEventListener("pageshow",_agyWake),window.addEventListener("pagehide",_agyWake),window.addEventListener("online",_agyWake));` +
		`return()=>{` +
		cancelVar + `=!0;clearInterval(` + timerVar + `);clearTimeout(_agyRetry);` +
		`typeof document!=="undefined"&&document.removeEventListener&&document.removeEventListener("visibilitychange",_agyWake);` +
		`typeof window!=="undefined"&&window.removeEventListener&&(window.removeEventListener("pageshow",_agyWake),window.removeEventListener("pagehide",_agyWake),window.removeEventListener("online",_agyWake))` +
		`}`
}

// renderCustomTextTypes writes the function that names a file's text MIME
// type, extended to plain text types the bundle does not list, to names such
// as Dockerfile, and to extensions such as .har and .jsonl. xmlFirst is the
// bundle's own statement for application/xml, when it has one.
func renderCustomTextTypes(name, xmlFirst, mimeList, extTable, tail string) string {
	return `function ` + name + `(a,b){b=b.split(";")[0].trim().toLowerCase();` + xmlFirst + `if(` + mimeList + `.includes(b))return b;` +
		`if(b.startsWith("text/"))return"text/plain";if(!a)return;var _base=a.replace(/\\/g,"/").split("/").pop().toLowerCase();if(_base==="dockerfile"||_base==="makefile")return"text/plain";if(!_base.includes("."))return;a=_base.slice(_base.lastIndexOf(".")+1);return ` + extTable + `[a]||(a==="md"||a==="markdown"?"text/markdown":a==="csv"?"text/csv":a==="json"||a==="har"||a==="jsonl"?"application/json":a==="html"||a==="htm"?"text/html":a==="css"?"text/css":a==="js"||a==="mjs"||a==="cjs"?"text/javascript":/^(txt|log|yaml|yml|toml|xml|sql|sh|bash|zsh|go|rs|c|cpp|cc|h|hpp|java|kt|swift|rb|php|env|ini|conf|cfg|dockerfile|makefile|graphql|proto|vue|svelte|astro)$/.test(a)?"text/plain":void 0)}` + tail
}

func mobile(o Options) bool { return o.MobileUX }

// All returns every patch in a stable order. Adding a patch here is the only
// step needed: the tests, the doctor report, the control panel and the cache key
// all derive from this list.
func All() []Patch {
	return []Patch{
		// Without this the phone's browser would call https://127.0.0.1:<port>,
		// which resolves to the phone itself. Antigravity 2.28.0 fixed this upstream
		// by passing window.location.origin directly.
		{
			ID:       "base-url-origin",
			Desc:     "Point the web app at the browser origin instead of https://127.0.0.1",
			Target:   MainJS,
			Kind:     Literal,
			Optional: true,
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
		// Returning false from the Lexical ENTER command handler lets the browser/editor
		// handle native newline insertion and IME composition commit cleanly.
		// On touch devices without send modifiers (Cmd/Ctrl), or during CJK IME composition,
		// bypass preventDefault() to prevent character duplication and ghost remnants.
		{
			ID:      "mobile-enter-newline",
			Desc:    "Preserve native newline and prevent IME composition corruption on touch devices",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileEnterNewlineRe,
			Replace: `registerCommand($1,$2=>{if(!$2)return!1;${3}${4}var _t=window.innerWidth<=768||((window.matchMedia&&window.matchMedia("(pointer:coarse)").matches)&&Boolean(document.body?.classList?.contains("agy-kb-open"))),_i=$2.isComposing||$2.keyCode===229||(window.__agyLastCompEnd&&performance.now()-window.__agyLastCompEnd<80);if(_i||(_t&&!$2.metaKey&&!$2.ctrlKey))return!1;$2.preventDefault();`,
		},
		// On a desktop the effort submenu opens on hover, so the row's onClick is
		// a convenience that picks the default effort. A tap fires both, closing
		// the popup before the submenu can be used. Removing the handler leaves
		// the submenu reachable.
		{
			ID:       "model-effort-submenu",
			Desc:     "Tapping a model opens its reasoning-effort submenu instead of picking medium",
			Target:   MainJS,
			Kind:     Regexp,
			Optional: true,
			Enabled:  func(Options) bool { return false },
			FindRe:   modelEffortRe,
			Replace:  "",
		},
		// Replacing the component with a function rather than an arrow keeps the
		// anchor "var vz=(" out of the replacement, so the rewrite cannot match
		// its own output.
		{
			ID:       "hide-mic-button",
			Desc:     "Hide the voice-recording button (transcription is unavailable in standalone mode)",
			Target:   MainJS,
			Kind:     Regexp,
			Optional: true,
			Enabled:  func(Options) bool { return false },
			FindRe:   hideMicButtonRe,
			Replace:  `${1}${2}=function(){return null};var ${2}Disabled=(`,
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
		{
			ID:      "auth-service-login-redirect",
			Desc:    "Redirect web auth service login calls to the network sign-in page",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  authServiceLoginRedirectRe,
			Replace: `async loginWithRedirect($1){window.location.href="/__agy/signin";return;`,
		},
		{
			ID:      "auth-service-show-login-flow",
			Desc:    "Redirect web auth service showLoginFlow to the network sign-in page",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  authServiceShowLoginFlowRe,
			Replace: `async showLoginFlow(){window.location.href="/__agy/signin";return;`,
		},
		{
			ID:      "auth-service-stub-login-redirect",
			Desc:    "Redirect stub web auth service calls to the network sign-in page",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  authServiceStubLoginRedirectRe,
			Replace: `loginWithRedirect:async()=>{window.location.href="/__agy/signin"},showLoginFlow:async()=>{window.location.href="/__agy/signin"}`,
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
			Replace: `var ${1}=(window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches))||!!this.storageService.get("didAskForNotificationPermission");${2}`,
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
			Replace: `${1}${2},replace:!0${3}var _s=${4}({strict:!1}),sec=_s?.section||_s?.project,isMobileNew=Boolean((window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches))&&sec);${5}isMobileNew?${6}.createElement("div",{"data-testid":"mobile-new-convo-empty",className:"flex-1 min-h-0 flex flex-col items-center justify-center gap-3 select-none"},${6}.createElement("span",{className:"text-xs text-muted-foreground/60"},"Start a new conversation")):${7},`,
		},

		// Antigravity lets the reader choose an agent for a new conversation at the end of the model selector's menu, in
		// its desktop layout only. Its phone layout, which it uses for a mobile browser under 640 px wide, gives the new
		// conversation screen's input no picker, so over remote access from a phone a project's agents could not be
		// chosen at all. These four give the phone layout the same picker. Each builds on the one before it, so a
		// bundle that one of them does not fit is left as it was by those after it. Since 2.29 the picker is a row of
		// the model selector's menu, and a bundle with that row is one these are for: where one of them does not fit
		// such a bundle, that is reported. Before 2.29 the picker stood in the input's toolbar, and they are not needed.
		//
		// The input hands the picker to the model selector only where its toolbar is on top, which it is on the desktop
		// screen and not on the phone's. Anywhere else it would draw the picker in its own toolbar, where the picker's
		// parts, which are those of a submenu, have no menu around them. No screen has asked for that since 2.29, when
		// the picker moved into the menu, so the picker goes to the model selector wherever there is one.
		{
			ID:           "mobile-agent-picker-in-model-menu",
			Desc:         "Draw the agent picker in the model selector's menu wherever the input is given one",
			Target:       MainJS,
			Kind:         Regexp,
			Enabled:      mobile,
			FindRe:       mobileAgentPickerInModelMenuRe,
			Optional:     true,
			Precondition: hasAgentPickerInModelMenu,
			Accept:       agentRoutingIsKnown,
			Replace:      `${1}=${2}`,
		},
		// The desktop screen builds the picker in a run of hooks that reads the agents, keeps the choice in step with
		// them and ends in the picker's element, or in nothing where there is no agent to pick. That run stays where it
		// is, and a copy of it becomes a function the phone screen can call, with the two things the run takes from the
		// screen around it read the way that screen reads them. The copy has nothing else of the screen's to read, so
		// the run is matched whole, token for token, and a match is taken only where its names say that the run reads
		// nothing else: see agentScreenIsKnown.
		{
			ID:           "mobile-agent-picker-hook",
			Desc:         "Make the new conversation screen's agent picker available to the phone layout",
			Target:       MainJS,
			Kind:         Regexp,
			Enabled:      mobile,
			FindRe:       agentScreen.re,
			Optional:     true,
			Requires:     "mobile-agent-picker-in-model-menu",
			Precondition: hasAgentPickerInModelMenu,
			Accept:       agentScreenIsKnown,
			Replace:      mobileAgentPickerHookReplace,
		},
		// The phone screen calls the function as it draws its input: every time and in the same place, which is what
		// hooks ask for. The new conversation screen is where a phone starts everything, so a function that throws at
		// the same step at every drawing, such as one that reads a name the screen does not have, costs the screen its
		// picker and not the screen: the error is written to the console once. One that throws at some drawings and
		// not at others still takes the screen down, since React counts the hooks a component calls, and so does an
		// error in one of the run's effects. A promise thrown to wait for something is React's to catch, and is passed
		// on.
		{
			ID:           "mobile-agent-picker",
			Desc:         "Offer the agent picker on a phone's new conversation screen",
			Target:       MainJS,
			Kind:         Regexp,
			Enabled:      mobile,
			FindRe:       mobileAgentPickerRe,
			Optional:     true,
			Requires:     "mobile-agent-picker-hook",
			Precondition: hasAgentPickerInModelMenu,
			Replace:      `${1},agentSelector:(()=>{try{return window.__agyPhoneAgentPicker()}catch(e){if(e&&typeof e.then==="function")throw e;window.__agyPhoneAgentPickerFailed||(window.__agyPhoneAgentPickerFailed=!0,console.error("[agy] the agent picker could not be built for this screen:",e))}})()${2}`,
		},

		// On the desktop screen the agent that is chosen shows as a chip beside the model selector, which is the one sign
		// of it outside the menu. The input draws the chip only where its toolbar is on top, so on a phone a reader
		// who had chosen an agent saw nothing change until the first reply came. The chip is drawn wherever the input's
		// model selector has the picker in its menu, which the model selector's own element says.
		{
			ID:           "mobile-agent-picker-chip",
			Desc:         "Show the chosen agent beside the model selector on a phone's new conversation screen",
			Target:       MainJS,
			Kind:         Regexp,
			Enabled:      mobile,
			FindRe:       mobileAgentPickerChipRe,
			Optional:     true,
			Requires:     "mobile-agent-picker",
			Precondition: hasAgentPickerInModelMenu,
			Replace:      `${1}(${3}||${2}?.props?.footerExtra)${4}`,
		},

		// The context indicator beside the send button, which the web bundle has had no component for since 2.30: see
		// context_indicator.go. The first hands the input one where the build bindings give none, the second defines
		// it, and the third shows it unless the setting was turned off. The next three show the setting itself, which
		// the web build hides, at the top of Settings > Appearance; they need the indicator and not the third, so the
		// setting can still turn it on where the third does not fit. The last describes the setting as what it shows.
		{
			ID:           "context-indicator-slot",
			Desc:         "Show how full the context window is beside the send button",
			Target:       MainJS,
			Kind:         Regexp,
			FindRe:       contextIndicatorSlot.re,
			Optional:     true,
			Precondition: hasContextIndicatorSlot,
			Accept:       contextIndicatorSlotIsKnown,
			Replace:      contextIndicatorSlot.replace,
		},
		{
			ID:           "context-indicator-define",
			Desc:         "Define a context window indicator for the web bundle, which has none since Antigravity 2.30",
			Target:       MainJS,
			Kind:         Regexp,
			FindRe:       contextIndicatorDefine.re,
			Optional:     true,
			Requires:     "context-indicator-slot",
			Precondition: hasContextIndicatorSlot,
			Accept:       contextIndicatorHooksAreKnown,
			Replace:      contextIndicatorDefine.replace,
		},
		{
			ID:           "context-indicator-default-on",
			Desc:         "Show the context window indicator unless its setting was turned off",
			Target:       MainJS,
			Kind:         Regexp,
			FindRe:       contextIndicatorDefaultRe,
			Optional:     true,
			Requires:     "context-indicator-define",
			Precondition: hasContextIndicatorSlot,
			Replace:      `${1}!=="false"`,
		},
		{
			ID:           "context-indicator-setting-allowed",
			Desc:         "Keep the context indicator's setting in the settings screens",
			Target:       MainJS,
			Kind:         Regexp,
			FindRe:       contextIndicatorSettingAllowedRe,
			Optional:     true,
			Requires:     "context-indicator-define",
			Precondition: hasContextIndicatorSlot,
			Replace:      `${1}`,
			ReplaceMatch: func(g [][]byte, _ Options) string {
				if g[1] != nil {
					return string(g[1])
				}
				return string(g[4]) + "!0;"
			},
		},
		{
			ID:           "context-indicator-setting-visible",
			Desc:         "Show the context indicator's setting without the Google flag it is behind",
			Target:       MainJS,
			Kind:         Regexp,
			FindRe:       contextIndicatorSettingVisible.re,
			Optional:     true,
			Requires:     "context-indicator-setting-allowed",
			Precondition: hasContextIndicatorSlot,
			Replace:      contextIndicatorSettingVisible.replace,
		},
		{
			ID:           "context-indicator-setting-in-chat",
			Desc:         "Put the context indicator's setting in the Chat section of Settings > Appearance, which is never collapsed",
			Target:       MainJS,
			Kind:         Regexp,
			FindRe:       contextIndicatorSettingInChatRe,
			Optional:     true,
			Requires:     "context-indicator-setting-visible",
			Precondition: hasContextIndicatorSlot,
			Replace:      contextIndicatorSettingInChat,
		},
		{
			ID:           "context-indicator-setting-description",
			Desc:         "Describe the context indicator's setting as what it shows, without the model quota",
			Target:       MainJS,
			Kind:         Regexp,
			FindRe:       contextIndicatorSettingDescription.re,
			Optional:     true,
			Requires:     "context-indicator-setting-visible",
			Precondition: hasContextIndicatorSlot,
			Replace:      contextIndicatorSettingDescription.replace,
		},

		// On mobile, show the back button in the main titlebar when a project is selected
		// and ensure back navigation always clears the active section.
		{
			ID:      "mobile-new-convo-header",
			Desc:    "Let the home hook treat a selected project as away from home when asked to, for the mobile titlebar",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileNewConvoHeaderRe,
			Replace: `$1=(_agyTb)=>$2({select:a=>a.location.pathname==="/"&&(!_agyTb||(!a.location.search?.section&&!a.location.search?.project))})`,
		},
		{
			ID:      "mobile-back-clears-section",
			Desc:    "Show the mobile titlebar's back button for a selected project, and make it clear the project to return to the conversation list",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileBackClearsSectionRe,
			Replace: `${1}!0${2}${3}${4}({clearSection:!0,clearProject:!0})${5}`,
			ReplaceMatch: func(g [][]byte, _ Options) string {
				prefix := ""
				if len(g[1]) > 0 {
					prefix = string(g[1]) + `!0` + string(g[2])
				}
				return prefix + string(g[3]) + string(g[4]) + `({clearSection:!0,clearProject:!0})` + string(g[5])
			},
		},
		{
			ID:      "mobile-new-conversation-button",
			Desc:    "Prepend a native New Conversation row to the mobile home conversation list",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileNewConvoButtonRe,
			Replace: `${1}${2}${3}${7}==="background"&&!window.__agyNewRowOnTop?((_m,_a,_r)=>_m.get(_a)||(_r=[{type:"header",id:"header-__agy_new__",label:"New Conversation"},...(_a.length>0?[{type:"spacer",id:"spacer-__agy_new__",height:6},..._a]:[])],_m.set(_a,_r),_r))(window.__agyBgMap||(window.__agyBgMap=new WeakMap),${5}):${5}${6}${7}${8}(${7}==="background"?(${9}.__agyHdr||(${9}.__agyHdr=_h=>_h.id==="header-__agy_new__"?${4}.createElement("button",{type:"button","data-testid":"new-conversation-button","aria-label":"New Conversation",onClick:()=>{window.__agyFocusComposer&&window.__agyFocusComposer();${2}("outside-of-project")},className:"group/headerbtn flex items-center pl-1 pr-2 rounded-lg select-none [-webkit-touch-callout:none] cursor-pointer w-full border-none outline-none text-left text-secondary-foreground hover:bg-muted hover:text-foreground h-11 bg-background"},${4}.createElement("div",{className:"flex-1 min-w-0 flex justify-start h-full items-center"},${4}.createElement("div",{className:"flex items-center gap-0.5 h-full min-w-0"},${4}.createElement("div",{className:"w-6 h-6 shrink-0 flex items-center justify-center"},${4}.createElement("svg",{xmlns:"http://www.w3.org/2000/svg",width:16,height:16,viewBox:"0 -960 960 960",fill:"currentColor",className:"shrink-0","aria-hidden":"true"},${4}.createElement("path",{d:"M450-450H250q-12.75,0-21.37-8.63T220-480.01t8.62-21.37T250-510H450V-710q0-12.75 8.63-21.37T480.01-740t21.37,8.62T510-710v200H710q12.75,0 21.37,8.63T740-479.99t-8.62,21.37T710-450H510v200q0,12.75-8.63,21.37T479.99-220t-21.37-8.62T450-250V-450Z"}))),${4}.createElement("span",{className:"truncate text-base"},"New Conversation")))):${9}(_h))):${9})`,
			ReplaceMatch: func(g [][]byte, _ Options) string {
				startFn := string(g[2])
				g3 := string(g[3])
				react := string(g[4])
				items := string(g[5])
				surf := string(g[7])
				hdr := string(g[9])
				focusInit := `window.__agyFocusComposer=window.__agyFocusComposer||(()=>{var _af=0,_t1=0,_t2=0,_clr=()=>{cancelAnimationFrame(_af);clearTimeout(_t1);clearTimeout(_t2);_af=_t1=_t2=0;window.__agyComposerFocusAt=-Infinity},_f=()=>{var _e=document.querySelector('[data-testid="agent-input-box"] [data-lexical-editor="true"]')||document.querySelector('[data-lexical-editor="true"]');if(_e&&document.activeElement!==_e){var _ev=!1,_h=()=>{_ev=!0};_e.addEventListener?.("focusin",_h,{once:!0});_e.focus({preventScroll:!0});_e.removeEventListener?.("focusin",_h);if(!_ev)_e.dispatchEvent(new FocusEvent("focusin",{bubbles:!0}))}if(_e&&document.activeElement===_e&&document.querySelector('[data-testid="mobile-new-convo-empty"]'))_clr()};document.addEventListener?.("pointerdown",_e=>{if(!_e.target?.closest?.('[data-testid="agent-input-box"]'))_clr()},!0);return()=>{if(!(window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches)))return;_clr();window.__agyComposerFocusAt=typeof performance!=="undefined"?performance.now():-Infinity;_f();if(document.activeElement?.getAttribute?.("data-lexical-editor")!=="true"||!document.querySelector('[data-testid="mobile-new-convo-empty"]')){_af=requestAnimationFrame(_f);_t1=setTimeout(_f,50);_t2=setTimeout(_f,150)}}})();` + surf + `==="background"&&(window.__agyStartNewConversation=()=>{window.__agyFocusComposer&&window.__agyFocusComposer();` + startFn + `("outside-of-project")});`
				if idx := strings.LastIndex(g3, "return "); idx >= 0 {
					sep := ""
					if idx > 0 {
						prev := g3[idx-1]
						if prev != ';' && prev != '}' && prev != '{' {
							sep = ";"
						}
					}
					g3 = g3[:idx] + sep + focusInit + g3[idx:]
				}
				return string(g[1]) + startFn + g3 + surf + `==="background"&&!window.__agyNewRowOnTop?((_m,_a,_r)=>_m.get(_a)||(_r=[{type:"header",id:"header-__agy_new__",label:"New Conversation"},...(_a.length>0?[{type:"spacer",id:"spacer-__agy_new__",height:6},..._a]:[])],_m.set(_a,_r),_r))(window.__agyBgMap||(window.__agyBgMap=new WeakMap),` + items + `):` + items + string(g[6]) + surf + string(g[8]) + `(` + surf + `==="background"?(` + hdr + `.__agyHdr||(` + hdr + `.__agyHdr=_h=>_h.id==="header-__agy_new__"?` + react + `.createElement("button",{type:"button","data-testid":"new-conversation-button","aria-label":"New Conversation",onClick:()=>{window.__agyFocusComposer&&window.__agyFocusComposer();` + startFn + `("outside-of-project")},className:"group/headerbtn flex items-center pl-1 pr-2 rounded-lg select-none [-webkit-touch-callout:none] cursor-pointer w-full border-none outline-none text-left text-secondary-foreground hover:bg-muted hover:text-foreground h-11 bg-background"},` + react + `.createElement("div",{className:"flex-1 min-w-0 flex justify-start h-full items-center"},` + react + `.createElement("div",{className:"flex items-center gap-0.5 h-full min-w-0"},` + react + `.createElement("div",{className:"w-6 h-6 shrink-0 flex items-center justify-center"},` + react + `.createElement("svg",{xmlns:"http://www.w3.org/2000/svg",width:16,height:16,viewBox:"0 -960 960 960",fill:"currentColor",className:"shrink-0","aria-hidden":"true"},` + react + `.createElement("path",{d:"M450-450H250q-12.75,0-21.37-8.63T220-480.01t8.62-21.37T250-510H450V-710q0-12.75 8.63-21.37T480.01-740t21.37,8.62T510-710v200H710q12.75,0 21.37,8.63T740-479.99t-8.62,21.37T710-450H510v200q0,12.75-8.63,21.37T479.99-220t-21.37-8.62T450-250V-450Z"}))),` + react + `.createElement("span",{className:"truncate text-base"},"New Conversation")))):` + hdr + `(_h))):` + hdr + `)`
			},
		},
		{
			ID:      "mobile-home-new-conversation-row",
			Desc:    "Show New Conversation above Automations on the mobile home, as the same kind of row",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileHomeNewConvoRowRe,
			// The mobile home tells itself apart by the component's conciergeTestId, whatever the parameter is called.
			Replace: `${1}var _agyHome=${2}==="mobile-home-concierge";_agyHome&&(window.__agyNewRowOnTop=!0);return a||_agyHome||${3}(a||_agyHome)&&${4}variant:_agyHome?"ghost":"secondary",${5}href:_agyHome?void 0:${6},onClick:_agyHome?()=>{window.__agyStartNewConversation?window.__agyStartNewConversation():window.location.assign("/?project=outside-of-project")}:void 0,${7}selected:!_agyHome&&${8},`,
			// A bundle this does not match keeps the row mobile-new-conversation-button adds to the conversation list.
			Optional: true,
		},
		{
			ID:       "mobile-project-add-button",
			Desc:     "Always show project New Conversation button on mobile touch devices without hovering",
			Target:   MainJS,
			Kind:     Regexp,
			Optional: true,
			Enabled:  mobile,
			FindRe:   mobileProjectAddButtonRe,
			Replace:  `if(true){let $4=$6==="project"?"New Conversation in Project":$7==="environment"?"New Conversation in Workspace":void 0`,
		},
		{
			ID:      "mobile-project-header-actions",
			Desc:    "Always show project header actions (add conversation button and menu) on touch devices",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileProjectHeaderActionsRe,
			Replace: `className:$1("absolute right-1 top-0 flex h-full items-center gap-1",($2||$3||Boolean(window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches)))?"opacity-100":"opacity-0 group-hover/header:opacity-100 group-focus-within/header:opacity-100")`,
		},
		{
			ID:      "mobile-project-kebab-menu",
			Desc:    "Always show project kebab (...) options button on mobile touch devices",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileProjectKebabMenuRe,
			Replace: `${5}${6}`,
			ReplaceMatch: func(g [][]byte, _ Options) string {
				if g[5] != nil {
					return string(g[5]) + string(g[6])
				}
				return `,` + string(g[1]) + `=(0,` + string(g[2]) + `.useContext)(` + string(g[3]) + `),` + string(g[4]) + `=!1,`
			},
		},
		{
			ID:      "mobile-project-add-click-close-sidebar",
			Desc:    "Automatically collapse mobile sidebar drawer when clicking + new conversation on mobile",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileProjectAddClickCloseSidebarRe,
			Replace: `onClick:${1}=>{${2}.stopPropagation();${3}(${2})||(function(){try{if(window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches)){var _sb=document.querySelector('[data-testid="sidebar-toggle"], [data-testid="mobile-toggle-sidebar"]');if(_sb&&window.getComputedStyle(_sb).display!=="none"){_sb.click();}window.__agyFocusComposer&&window.__agyFocusComposer()}}catch(e){}}(),${2}.preventDefault(),${4}(${2}))}`,
		},
		// Antigravity 2.23.0 snapshots projectsOrder into localStorage on mount with
		// projectsFolderSortBy defaulted to "custom". When projects load before
		// conversation timestamps arrive, that snapshot locks in alphabetical order
		// and ignores subsequent Sort By selections ("last_updated" or
		// "last_user_prompt"). Re-run the dynamic sort whenever projectsSortBy is
		// not "custom".
		{
			ID:      "mobile-project-sort-order",
			Desc:    "Sort grouped project folders by active Sort By selection instead of stale alphabetical order",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileProjectSortOrderRe,
			Replace: `if($3!=="custom"||$1.length===0)return $2;`,
		},
		// Antigravity's mobile settings component (jNb) only renders Settings and
		// Workspaces sections, omitting the Projects and Not in Project sections
		// present in the desktop sidebar (mNb), and ignores settingsProjectId /
		// settingsScreen URL parameters when opening Settings from a project menu.
		{
			ID:      "mobile-project-settings-export",
			Desc:    "Export project settings component to global window for mobile settings navigation",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileProjectSettingsExportRe,
			Replace: `window.__agyProjectSettings=$2;${1}function $2(){$3(_s=>(window.__agyProjectSettingsBack=window.__agyProjectSettingsHost&&_s[0]!=="main"?()=>{_s[1]("main");return!0}:null,_s))($4)`,
		},
		{
			ID:      "mobile-settings-projects",
			Desc:    "Add Projects and Not in Project sections and deep-link navigation to mobile Settings view",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileSettingsProjectsRe,
			Replace: `${1}var {projectManagementFeature:_pm}=${12},_hp=!!${11}().get("settings")?.hideProjects,_ps=${14}(_pm?.projectsStateProvider),_pl=(_ps||[]).filter(_x=>_x?.project&&!_x.project.isWorkspaceOnly&&!_x.project.archived),_init=(0,${4}.useRef)(!1);window.__agyProjectSettingsHost=!!m?.startsWith("project:");if(!window.__agyProjectSettingsHost)window.__agyProjectSettingsBack=null;(0,${4}.useEffect)(()=>()=>{window.__agyProjectSettingsHost=!1;window.__agyProjectSettingsBack=null},[]);(0,${4}.useEffect)(()=>{if(_init.current)return;_init.current=!0;try{var _u=new URLSearchParams(window.location.search),_cl=s=>s?s.replace(/^"|"$/g,""):"",_pid=_cl(_u.get("settingsProjectId")),_scr=_cl(_u.get("settingsScreen"));if(_pid){a({type:"settingsScreen/setContextProjectId",payload:_pid});a({type:"settingsScreen/navigateToProjectScreen",payload:{projectId:_pid}});if(_scr)a(${5}(_scr));${2}.push("project:"+_pid)}else if(_scr&&_scr!=="General"){a(${5}(_scr));${2}.push(_scr)}}catch(_e){}},[]);${3},!_hp&&_pm&&_pl.length>0&&${4}.createElement(${4}.Fragment,null,${4}.createElement(${6},{text:"Projects"}),_pl.map(_p=>${4}.createElement(${7},{key:_p.project.id,text:_p.project.name||"Project",label:_p.project.name||"Project",onClick:()=>{a({type:"settingsScreen/setContextProjectId",payload:_p.project.id});a({type:"settingsScreen/navigateToProjectScreen",payload:{projectId:_p.project.id}});${2}.push("project:"+_p.project.id)}}))),_pm&&${4}.createElement(${4}.Fragment,null,${4}.createElement(${6},{text:"Not in Project"}),${4}.createElement(${7},{key:"outside-of-project",text:"Conversations",label:"Conversations",onClick:()=>{a({type:"settingsScreen/setContextProjectId",payload:"outside-of-project"});a({type:"settingsScreen/navigateToProjectScreen",payload:{projectId:"outside-of-project"}});${2}.push("project:outside-of-project")}}))${8}m?.startsWith("project:")&&window.__agyProjectSettings?${4}.createElement(window.__agyProjectSettings,null):${9}${10}`,
			ReplaceMatch: func(g [][]byte, _ Options) string {
				pmInit := `var {projectManagementFeature:_pm}=` + string(g[12])
				if g[13] != nil {
					pmInit = `var _pm=` + string(g[13])
				}
				stk := string(g[2])
				react := string(g[4])
				navAct := string(g[5])
				hdrComp := string(g[6])
				itemComp := string(g[7])
				g8 := strings.Replace(
					string(g[8]),
					`{onBack:`+stk+`.pop}`,
					`{onBack:()=>{if(m?.startsWith("project:")&&window.__agyProjectSettingsBack?.())return;`+stk+`.pop()}}`,
					1,
				)
				return string(g[1]) + pmInit + `,_hp=!!` + string(g[11]) + `().get("settings")?.hideProjects,_ps=` + string(g[14]) + `(_pm?.projectsStateProvider),_pl=(_ps||[]).filter(_x=>_x?.project&&!_x.project.isWorkspaceOnly&&!_x.project.archived),_init=(0,` + react + `.useRef)(!1);window.__agyProjectSettingsHost=!!m?.startsWith("project:");if(!window.__agyProjectSettingsHost)window.__agyProjectSettingsBack=null;(0,` + react + `.useEffect)(()=>()=>{window.__agyProjectSettingsHost=!1;window.__agyProjectSettingsBack=null},[]);(0,` + react + `.useEffect)(()=>{if(_init.current)return;_init.current=!0;try{var _u=new URLSearchParams(window.location.search),_cl=s=>s?s.replace(/^"|"$/g,""):"",_pid=_cl(_u.get("settingsProjectId")),_scr=_cl(_u.get("settingsScreen"));if(_pid){a({type:"settingsScreen/setContextProjectId",payload:_pid});a({type:"settingsScreen/navigateToProjectScreen",payload:{projectId:_pid}});if(_scr)a(` + navAct + `(_scr));` + stk + `.push("project:"+_pid)}else if(_scr&&_scr!=="General"){a(` + navAct + `(_scr));` + stk + `.push(_scr)}}catch(_e){}},[]);` + string(g[3]) + `,!_hp&&_pm&&_pl.length>0&&` + react + `.createElement(` + react + `.Fragment,null,` + react + `.createElement(` + hdrComp + `,{text:"Projects"}),_pl.map(_p=>` + react + `.createElement(` + itemComp + `,{key:_p.project.id,text:_p.project.name||"Project",label:_p.project.name||"Project",onClick:()=>{a({type:"settingsScreen/setContextProjectId",payload:_p.project.id});a({type:"settingsScreen/navigateToProjectScreen",payload:{projectId:_p.project.id}});` + stk + `.push("project:"+_p.project.id)}}))),_pm&&` + react + `.createElement(` + react + `.Fragment,null,` + react + `.createElement(` + hdrComp + `,{text:"Not in Project"}),` + react + `.createElement(` + itemComp + `,{key:"outside-of-project",text:"Conversations",label:"Conversations",onClick:()=>{a({type:"settingsScreen/setContextProjectId",payload:"outside-of-project"});a({type:"settingsScreen/navigateToProjectScreen",payload:{projectId:"outside-of-project"}});` + stk + `.push("project:outside-of-project")}}))` + g8 + `m?.startsWith("project:")&&window.__agyProjectSettings?` + react + `.createElement(window.__agyProjectSettings,null):` + string(g[9]) + string(g[10])
			},
		},
		// Since 2.31 the bundle shows these buttons on screens without hover itself, through can-hover: classes.
		{
			ID:       "mobile-user-message-actions",
			Desc:     "Make user message action buttons (Undo icon and copy) visible on touch devices",
			Target:   MainJS,
			Kind:     Regexp,
			Enabled:  mobile,
			Optional: true,
			FindRe:   mobileUserMessageActionsRe,
			Replace:  `${1}"relative self-end ml-auto mt-1 flex flex-row items-center p-1 rounded-full opacity-90 pointer-events-auto transition-all bg-transparent user-input-buttons-container select-none"`,
		},
		{
			ID:      "mobile-conversation-row-actions",
			Desc:    "Always show conversation row actions on touch/mobile devices while keeping hover behavior on desktop",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileConversationRowActionsRe,
			Replace: `className:${1}("${2}(${3}||${4}||Boolean(window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches)))?"opacity-100":"${5}")`,
		},
		{
			ID:      "mobile-titlebar-delete-hook",
			Desc:    "Expose conversation deletion handlers in the titlebar more-actions menu",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileTitlebarDeleteHookRe,
			Replace: `{handleArchive:$1,handleRestore:$2,handlePin:$3,handleUnpin:$4,isArchiveSupported:$5,handleDelete:agyDel,showDeleteModal:agyShowDel,setShowDeleteModal:agySetShowDel,showLoadingSpinner:agyDelSpin,handleCloseDeleteModal:agyCloseDel,handleShare:${6},showShareModal:${7},shareUrl:${8},handleCloseShareModal:${9},onShare:${10}${11}}=${12}(${13}??"")`,
		},
		{
			ID:      "mobile-titlebar-delete-menu",
			Desc:    "Add Delete option to the titlebar more-actions dropdown menu",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: mobile,
			FindRe:  mobileTitlebarDeleteMenuRe,
			Replace: `$1&&($2.push({iconName:"edit",tooltip:"Rename",onClick:$3}),$2.push({iconName:"delete",tooltip:"Delete",onClick:()=>{typeof agySetShowDel==="function"&&agySetShowDel(!0)}})`,
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
			Replace: `${1}window.__agyDeleteModal&&typeof agyShowDel!=="undefined"?${4}.createElement(window.__agyDeleteModal,{isOpen:agyShowDel,onClose:agyCloseDel,onDelete:async function(){try{if(typeof agyDel==="function"&&${2})await agyDel()}catch(e){return}agySetShowDel(!1);var _b=document.querySelector('[data-testid="mobile-back-to-home"]');if(_b){_b.click()}else if(window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches)){try{window.history.replaceState(null,"","/");window.dispatchEvent(new PopStateEvent("popstate"))}catch(e){window.location.replace("/")}}},showLoadingSpinner:agyDelSpin}):null,`,
		},
		{
			ID:       "mobile-kebab-menu-pin-archive",
			Desc:     "Add Pin and Archive actions into conversation kebab dropdown menu",
			Target:   MainJS,
			Kind:     Regexp,
			Optional: true,
			Enabled:  func(Options) bool { return false },
			FindRe:   mobileKebabMenuPinArchiveRe,
			Replace:  `const $1=({cascadeId:$2,onDeleteClick:$3,onRenameClick:$4,onMarkAsReadClick:$5,isUnread:$6,onViewDebugClick:$7$8,onPinClick:agyPin,isPinned:agyIsPinned,onArchiveClick:agyArchive})=>$9.createElement($10,{side:"bottom",align:"start",className:"min-w-[180px]",finalFocus:!1},$11.createElement($12,{onClick:$13,"data-testid":"conversation-rename-menu-item"},$14.createElement($15,{name:"edit",size:16,className:"text-secondary-foreground shrink-0"}),$16.createElement("span",null,"Rename")),agyPin&&$9.createElement($12,{onClick:agyPin,"data-testid":"conversation-pin-menu-item"},$14.createElement($15,{name:agyIsPinned?"keep_off":"keep",size:16,className:"text-secondary-foreground shrink-0"}),$16.createElement("span",null,agyIsPinned?"Unpin":"Pin")),agyArchive&&$9.createElement($12,{onClick:agyArchive,"data-testid":"conversation-archive-menu-item"},$14.createElement($15,{name:"archive",size:16,className:"text-secondary-foreground shrink-0"}),$16.createElement("span",null,"Archive")),`,
		},
		{
			ID:       "mobile-kebab-wrapper-pin-archive",
			Desc:     "Pass pin and archive props through conversation kebab wrapper component",
			Target:   MainJS,
			Kind:     Regexp,
			Optional: true,
			Enabled:  func(Options) bool { return false },
			FindRe:   mobileKebabWrapperPinArchiveRe,
			Replace:  `var $1=({cascadeId:$2,onDeleteClick:$3,onRenameClick:$4,onMarkAsReadClick:$5,isUnread:$6,onOpenChange:$7,onViewDebugClick:$8,onPinClick:agyPin,isPinned:agyIsPinned,onArchiveClick:agyArchive})=>$9.createElement($10,{onOpenChange:$11},$12.createElement($13,{asChild:!0},$14.createElement($15,{variant:"ghost",size:"icon","aria-label":"More options","data-testid":"conversation-kebab",onClick:$16=>void $17.stopPropagation()},$18.createElement($19,{name:"more_vert",size:16}))),$20.createElement($21,{cascadeId:$22,onDeleteClick:$23,onRenameClick:$24,onMarkAsReadClick:$25,isUnread:$26,onViewDebugClick:$27,onPinClick:agyPin,isPinned:agyIsPinned,onArchiveClick:agyArchive}));`,
		},
		{
			ID:       "mobile-kebab-call-pin-archive",
			Desc:     "Supply pin and archive handlers to conversation kebab button call in history list",
			Target:   MainJS,
			Kind:     Regexp,
			Optional: true,
			Enabled:  func(Options) bool { return false },
			FindRe:   mobileKebabCallPinArchiveRe,
			Replace:  `$1.createElement($2,{cascadeId:$3,onDeleteClick:($4),onRenameClick:$5,onMarkAsReadClick:$6,isUnread:$7,onOpenChange:$8,onPinClick:()=>b.handlePin?.(a),isPinned:b.isPinned,onArchiveClick:()=>b.handleArchive?.(a)})`,
		},
		{
			ID:       "mobile-hide-aux-sidebar",
			Desc:     "Hide unclickable auxiliary sidebar toggle icon on mobile navigation bar",
			Target:   MainJS,
			Kind:     Regexp,
			Optional: true,
			Enabled:  func(Options) bool { return false },
			FindRe:   mobileHideAuxSidebarRe,
			Replace:  `null`,
		},
		{
			ID:      "settings-rules-editor",
			Desc:    "Enable inline editor and save button for rules & skills in Settings Customizations view",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: func(Options) bool { return true },
			FindRe:  settingsRulesEditorRe,
			Replace: `${1}var _R=${14},_rawP=String(b||"");if(/^file:\/\//i.test(_rawP)){_rawP=_rawP.slice(7);try{_rawP=decodeURIComponent(_rawP)}catch(_){}}var _p=_rawP.replace(/\\/g,"/"),_canEdit=Boolean(_p&&${8}!=="Delete plugin"&&${8}!=="Delete agent"&&(/^(\/|~|[a-zA-Z]:\/)/.test(_p))&&!/(?:^|\/)(?:\.git|\.agy-uploads)(?:\/|$)/i.test(_p)&&!/\/(?:\.gemini|\.agents?)\/(?:(?:config|antigravity)\/)?(?:builtin|plugins|agents)(?:\/|$)/i.test(_p)&&(/\.(?:md|mdc|markdown|rules)$/i.test(_p)||/(?:^|\/)\.(?:cursor|windsurf|cline)rules$/i.test(_p)||/(?:\/|\/[^/.]+)$/.test(_p)));if(/^\/[a-zA-Z]:\//.test(_p))_p=_p.slice(1);var _parseDesc=_s=>{var _fm=_s.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/);if(_fm){var _dm=_fm[1].match(/(?:^|\r?\n)description:[ \t]*([^\r\n]*)(?:\r?\n((?:[ \t]+[^\r\n]+(?:\r?\n|$))+))?/);if(_dm){var _v=_dm[1].trim();if((!_v||/^[>|][-+]?\d*$/.test(_v))&&_dm[2])_v=_dm[2].trim().split(/\r?\n/).map(_l=>_l.trim()).join(" ");_v=_v.replace(/^["']|["']$/g,"");if(_v&&!/^[>|][-+]?\d*$/.test(_v))return _v}}var _b=(_fm?_s.slice(_fm[0].length):_s).trim();return _b.length>120?_b.slice(0,120)+"\u2026":_b},_tmr=_R.useRef(null),[agyEdit,agySetEdit]=_R.useState(!1),[agyTxt,agySetTxt]=_R.useState(""),[agySave,agySetSave]=_R.useState(!1),[agyDone,agySetDone]=_R.useState(!1),[agyErr,agySetErr]=_R.useState(""),[agySavedDesc,agySetSavedDesc]=_R.useState(null);typeof _R.useEffect==="function"&&_R.useEffect(()=>{if(_tmr.current){clearTimeout(_tmr.current);_tmr.current=null;}agySetEdit(!1);agySetTxt("");agySetSave(!1);agySetDone(!1);agySetErr("");agySetSavedDesc(null);return()=>{if(_tmr.current){clearTimeout(_tmr.current);_tmr.current=null;}}},[_p]);if(agySavedDesc!==null)${3}=agySavedDesc;var agyDoEdit=_canEdit?async()=>{if(_tmr.current){clearTimeout(_tmr.current);_tmr.current=null;}agySetDone(!1);agySetErr("");if(agyEdit){agySetEdit(!1);return;}try{let res=await fetch("/__agy/api/rules/read?path="+encodeURIComponent(_p));if(res.ok){let json=await res.json();agySetTxt(json.content??"");agySetEdit(!0);}else{agySetErr("Failed to load file")}}catch(e){agySetErr("Failed to load file")}}:void 0;var agyDoSave=async()=>{agySetSave(!0);agySetErr("");try{let res=await fetch("/__agy/api/rules/save",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({path:_p,content:agyTxt})});if(res.ok){agySetDone(!0);agySetSavedDesc(_parseDesc(agyTxt));if(_tmr.current)clearTimeout(_tmr.current);_tmr.current=setTimeout(()=>{_tmr.current=null;agySetDone(!1);agySetEdit(!1)},1000);}else{agySetErr("Save failed")}}catch(e){agySetErr("Save failed")}finally{agySetSave(!1)}};var agyEditorNode=agyEdit?_R.createElement("div",{className:"w-full mt-2 pt-2 border-t border-border flex flex-col gap-2"},_R.createElement("textarea",{value:agyTxt,onChange:e=>agySetTxt(e.target.value),placeholder:"Write markdown instructions...",className:"agy-rules-editor w-full font-mono text-xs p-2.5 rounded-lg border border-border bg-muted/40 focus:outline-none focus:ring-1 focus:ring-primary resize-y text-foreground leading-normal",style:{lineHeight:"1.45",minHeight:"200px",maxHeight:"500px"},spellCheck:!1}),_R.createElement("div",{className:"flex items-center justify-end gap-2"},agyErr&&_R.createElement("span",{className:"text-xs text-destructive mr-auto"},agyErr),_R.createElement("button",{type:"button",onClick:()=>{if(_tmr.current){clearTimeout(_tmr.current);_tmr.current=null;}agySetDone(!1);agySetErr("");agySetEdit(!1)},disabled:agySave,className:"text-xs h-7 px-3 rounded border border-border bg-muted/40 hover:bg-muted text-muted-foreground"},"Cancel"),_R.createElement("button",{type:"button",onClick:agyDoSave,disabled:agySave,className:"text-xs h-7 px-3 rounded flex items-center gap-1 font-medium bg-primary text-primary-foreground hover:bg-primary/90"},agySave?"Saving...":(agyDone?"Saved ✓":"Save")))):(agyErr?_R.createElement("div",{className:"w-full mt-2 pt-2 border-t border-border text-xs text-destructive"},agyErr):null);var agyExp=${12}?_R.createElement(_R.Fragment,null,${12},agyEditorNode):agyEditorNode;${12}=agyExp;${13}`,
			ReplaceMatch: func(g [][]byte, _ Options) string {
				desc := string(g[3])
				delTitle := string(g[8])
				exp := string(g[12])
				react := string(g[14])
				prefix := string(g[1]) + `var _R=` + react + `,_rawP=String(b||"");if(/^file:\/\//i.test(_rawP)){_rawP=_rawP.slice(7);try{_rawP=decodeURIComponent(_rawP)}catch(_){}}var _p=_rawP.replace(/\\/g,"/"),_canEdit=Boolean(_p&&` + delTitle + `!=="Delete plugin"&&` + delTitle + `!=="Delete agent"&&(/^(\/|~|[a-zA-Z]:\/)/.test(_p))&&!/(?:^|\/)(?:\.git|\.agy-uploads)(?:\/|$)/i.test(_p)&&!/\/(?:\.gemini|\.agents?)\/(?:(?:config|antigravity)\/)?(?:builtin|plugins|agents)(?:\/|$)/i.test(_p)&&(/\.(?:md|mdc|markdown|rules)$/i.test(_p)||/(?:^|\/)\.(?:cursor|windsurf|cline)rules$/i.test(_p)||/(?:\/|\/[^/.]+)$/.test(_p)));if(/^\/[a-zA-Z]:\//.test(_p))_p=_p.slice(1);var _parseDesc=_s=>{var _fm=_s.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/);if(_fm){var _dm=_fm[1].match(/(?:^|\r?\n)description:[ \t]*([^\r\n]*)(?:\r?\n((?:[ \t]+[^\r\n]+(?:\r?\n|$))+))?/);if(_dm){var _v=_dm[1].trim();if((!_v||/^[>|][-+]?\d*$/.test(_v))&&_dm[2])_v=_dm[2].trim().split(/\r?\n/).map(_l=>_l.trim()).join(" ");_v=_v.replace(/^["']|["']$/g,"");if(_v&&!/^[>|][-+]?\d*$/.test(_v))return _v}}var _b=(_fm?_s.slice(_fm[0].length):_s).trim();return _b.length>120?_b.slice(0,120)+"\u2026":_b},_tmr=_R.useRef(null),[agyEdit,agySetEdit]=_R.useState(!1),[agyTxt,agySetTxt]=_R.useState(""),[agySave,agySetSave]=_R.useState(!1),[agyDone,agySetDone]=_R.useState(!1),[agyErr,agySetErr]=_R.useState(""),[agySavedDesc,agySetSavedDesc]=_R.useState(null);typeof _R.useEffect==="function"&&_R.useEffect(()=>{if(_tmr.current){clearTimeout(_tmr.current);_tmr.current=null;}agySetEdit(!1);agySetTxt("");agySetSave(!1);agySetDone(!1);agySetErr("");agySetSavedDesc(null);return()=>{if(_tmr.current){clearTimeout(_tmr.current);_tmr.current=null;}}},[_p]);if(agySavedDesc!==null)` + desc + `=agySavedDesc;`
				editorTail := `var agyDoSave=async()=>{agySetSave(!0);agySetErr("");try{let res=await fetch("/__agy/api/rules/save",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({path:_p,content:agyTxt})});if(res.ok){agySetDone(!0);agySetSavedDesc(_parseDesc(agyTxt));if(_tmr.current)clearTimeout(_tmr.current);_tmr.current=setTimeout(()=>{_tmr.current=null;agySetDone(!1);agySetEdit(!1)},1000);}else{agySetErr("Save failed")}}catch(e){agySetErr("Save failed")}finally{agySetSave(!1)}};`
				editorTailEnd := `var agyEditorNode=agyEdit?_R.createElement("div",{className:"w-full mt-2 pt-2 border-t border-border flex flex-col gap-2"},_R.createElement("textarea",{value:agyTxt,onChange:e=>agySetTxt(e.target.value),placeholder:"Write markdown instructions...",className:"agy-rules-editor w-full font-mono text-xs p-2.5 rounded-lg border border-border bg-muted/40 focus:outline-none focus:ring-1 focus:ring-primary resize-y text-foreground leading-normal",style:{lineHeight:"1.45",minHeight:"200px",maxHeight:"500px"},spellCheck:!1}),_R.createElement("div",{className:"flex items-center justify-end gap-2"},agyErr&&_R.createElement("span",{className:"text-xs text-destructive mr-auto"},agyErr),_R.createElement("button",{type:"button",onClick:()=>{if(_tmr.current){clearTimeout(_tmr.current);_tmr.current=null;}agySetDone(!1);agySetErr("");agySetEdit(!1)},disabled:agySave,className:"text-xs h-7 px-3 rounded border border-border bg-muted/40 hover:bg-muted text-muted-foreground"},"Cancel"),_R.createElement("button",{type:"button",onClick:agyDoSave,disabled:agySave,className:"text-xs h-7 px-3 rounded flex items-center gap-1 font-medium bg-primary text-primary-foreground hover:bg-primary/90"},agySave?"Saving...":(agyDone?"Saved ✓":"Save")))):(agyErr?_R.createElement("div",{className:"w-full mt-2 pt-2 border-t border-border text-xs text-destructive"},agyErr):null);var agyExp=` + exp + `?_R.createElement(_R.Fragment,null,` + exp + `,agyEditorNode):agyEditorNode;` + exp + `=agyExp;` + string(g[13])
				if g[5] != nil {
					onEdit := string(g[5])
					return prefix + `var agyDoEdit=` + onEdit + `||(_canEdit?async()=>{if(_tmr.current){clearTimeout(_tmr.current);_tmr.current=null;}agySetDone(!1);agySetErr("");if(agyEdit){agySetEdit(!1);return;}try{let res=await fetch("/__agy/api/rules/read?path="+encodeURIComponent(_p));if(res.ok){let json=await res.json();agySetTxt(json.content??"");agySetEdit(!0);}else{agySetErr("Failed to load file")}}catch(e){agySetErr("Failed to load file")}}:void 0);` + editorTail + onEdit + `=agyDoEdit;` + editorTailEnd
				}
				return prefix + `var agyDoEdit=_canEdit?async()=>{if(_tmr.current){clearTimeout(_tmr.current);_tmr.current=null;}agySetDone(!1);agySetErr("");if(agyEdit){agySetEdit(!1);return;}try{let res=await fetch("/__agy/api/rules/read?path="+encodeURIComponent(_p));if(res.ok){let json=await res.json();agySetTxt(json.content??"");agySetEdit(!0);}else{agySetErr("Failed to load file")}}catch(e){agySetErr("Failed to load file")}}:void 0;` + editorTail + editorTailEnd
			},
		},
		{
			ID:      "settings-customizations-show-edit",
			Desc:    "Allow edit action button to appear in Settings Customizations rows",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: func(Options) bool { return true },
			FindRe:  settingsCustomizationsShowEditRe,
			Replace: `${1}typeof agyDoEdit==="function"&&agyDoEdit&&$4.createElement($7,{variant:"ghost",size:"icon-sm",onClick:agyDoEdit,"aria-label":"Edit",className:"rounded opacity-60 group-hover:opacity-100 group-focus-within:opacity-100 focus-visible:opacity-100 hover:!text-foreground"},$4.createElement($10,{name:"edit_note",className:"w-4 h-4"})),!$2&&$3&&$4.createElement($5.Fragment,null,$6.createElement($7,{variant:"ghost",size:"icon-sm",onClick:$8,"aria-label":`,
			ReplaceMatch: func(g [][]byte, _ Options) string {
				if string(g[11]) == "delete" {
					react := string(g[4])
					btn := string(g[7])
					icon := string(g[10])
					editBtn := `typeof agyDoEdit==="function"&&agyDoEdit&&` + react + `.createElement(` + btn + `,{variant:"ghost",size:"icon-sm",onClick:agyDoEdit,"aria-label":"Edit",className:"rounded opacity-60 group-hover:opacity-100 group-focus-within:opacity-100 focus-visible:opacity-100 hover:!text-foreground"},` + react + `.createElement(` + icon + `,{name:"edit_note",className:"w-4 h-4"})),`
					return string(g[1]) + editBtn + "!" + string(g[2]) + "&&" + string(g[3]) + "&&" + react + ".createElement(" + string(g[5]) + ".Fragment,null," + string(g[6]) + ".createElement(" + btn + `,{variant:"ghost",size:"icon-sm",onClick:` + string(g[8]) + `,"aria-label":` + string(g[9])
				}
				tail := ""
				if g[9] != nil {
					tail = string(g[9])
				}
				return string(g[1]) + string(g[3]) + "&&" + string(g[4]) + ".createElement(" + string(g[5]) + ".Fragment,null," + string(g[6]) + ".createElement(" + string(g[7]) + `,{variant:"ghost",size:"icon-sm",onClick:` + string(g[8]) + `,"aria-label":` + tail
			},
		},
		{
			ID:      "settings-doc-row-editor",
			Desc:    "Enable inline and modal editing for skills & rules in Customizations Installed view",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: func(Options) bool { return true },
			FindRe:  settingsDocRowEditorRe,
			Replace: `${1}_R=${5},_rawP=String(c??b??"");if(/^file:\/\//i.test(_rawP)){_rawP=_rawP.slice(7);try{_rawP=decodeURIComponent(_rawP)}catch(_){}}var _p=_rawP.replace(/\\/g,"/"),_canEdit=Boolean(_p&&!m&&!g&&!l&&(/^(\/|~|[a-zA-Z]:\/)/.test(_p))&&!/(?:^|\/)(?:\.git|\.agy-uploads)(?:\/|$)/i.test(_p)&&!/\/(?:\.gemini|\.agents?)\/(?:(?:config|antigravity)\/)?(?:builtin|plugins|agents)(?:\/|$)/i.test(_p)&&(/\.(?:md|mdc|markdown|rules)$/i.test(_p)||/(?:^|\/)\.(?:cursor|windsurf|cline)rules$/i.test(_p)||/(?:\/|\/[^/.]+)$/.test(_p)));if(/^\/[a-zA-Z]:\//.test(_p))_p=_p.slice(1);var _stripFm=_s=>{var _m=_s.match(/^---\r?\n[\s\S]*?\r?\n---(?:\r?\n|$)/);return _m?_s.slice(_m[0].length).trim():_s},_hadFm=/^---\r?\n/.test(f||""),_parseDesc=_s=>{var _fm=_s.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/);if(_fm){var _dm=_fm[1].match(/(?:^|\r?\n)description:[ \t]*([^\r\n]*)(?:\r?\n((?:[ \t]+[^\r\n]+(?:\r?\n|$))+))?/);if(_dm){var _v=_dm[1].trim();if((!_v||/^[>|][-+]?\d*$/.test(_v))&&_dm[2])_v=_dm[2].trim().split(/\r?\n/).map(_l=>_l.trim()).join(" ");_v=_v.replace(/^["']|["']$/g,"");if(_v&&!/^[>|][-+]?\d*$/.test(_v))return _v}}var _b=(_fm?_s.slice(_fm[0].length):_s).trim();return _b.length>120?_b.slice(0,120)+"\u2026":_b},_tmr=_R.useRef(null),[agyEdit,agySetEdit]=_R.useState(!1),[agyTxt,agySetTxt]=_R.useState(""),[agySave,agySetSave]=_R.useState(!1),[agyDone,agySetDone]=_R.useState(!1),[agyErr,agySetErr]=_R.useState(""),[agySaved,agySetSaved]=_R.useState(null);typeof _R.useEffect==="function"&&_R.useEffect(()=>{if(_tmr.current){clearTimeout(_tmr.current);_tmr.current=null;}agySetEdit(!1);agySetTxt("");agySetSave(!1);agySetDone(!1);agySetErr("");agySetSaved(null);return()=>{if(_tmr.current){clearTimeout(_tmr.current);_tmr.current=null;}}},[_p]);if(!${3}&&agyEdit){if(_tmr.current){clearTimeout(_tmr.current);_tmr.current=null;}agySetDone(!1);agySetErr("");agySetEdit(!1);}if(agySaved!==null){f=n&&_hadFm?agySaved:_stripFm(agySaved);${2}=_parseDesc(agySaved);}var agyDoEdit=_canEdit?async(ev)=>{if(ev&&ev.stopPropagation)ev.stopPropagation();if(_tmr.current){clearTimeout(_tmr.current);_tmr.current=null;}agySetDone(!1);agySetErr("");if(${3}&&agyEdit){agySetEdit(!1);return;}try{let res=await fetch("/__agy/api/rules/read?path="+encodeURIComponent(_p));if(res.ok){let json=await res.json();agySetTxt(typeof json.content==="string"&&(json.content.length>0||agySaved!==null)?json.content:(agySaved??f??""));agySetEdit(!0);${4}(!0);}else{agySetErr("Failed to load file");${4}(!0);}}catch(e){agySetErr("Failed to load file");${4}(!0);}}:void 0;var agyDoSave=async()=>{agySetSave(!0);agySetErr("");try{let res=await fetch("/__agy/api/rules/save",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({path:_p,content:agyTxt})});if(res.ok){agySetDone(!0);agySetSaved(agyTxt);if(_tmr.current)clearTimeout(_tmr.current);_tmr.current=setTimeout(()=>{_tmr.current=null;agySetDone(!1);agySetEdit(!1)},800);}else{agySetErr("Save failed")}}catch(e){agySetErr("Save failed")}finally{agySetSave(!1)}};if(_canEdit){var agyEditIcon=_R.createElement("svg",{xmlns:"http://www.w3.org/2000/svg",width:16,height:16,viewBox:"0 -960 960 960",fill:"currentColor","aria-hidden":"true"},_R.createElement("path",{d:"M160-400v-80h280v80H160Zm0-160v-80h440v80H160Zm0-160v-80h440v80H160Zm360 560v-123l221-220q9-9 20-13t22-4q12 0 23 4.5t20 13.5l37 37q8 9 12.5 20t4.5 22q0 11-4 22.5T863-380L643-160H520Zm300-263-37-37 37 37ZM580-220h38l121-122-18-19-19-18-122 121v38Zm141-141-19-18 37 37-18-19Z"})),agyEditBtn=_R.createElement("button",{type:"button",onClick:agyDoEdit,"aria-label":"Edit",title:"Edit",className:"p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground cursor-pointer flex items-center justify-center"},agyEditIcon);k=k?_R.createElement(_R.Fragment,null,agyEditBtn,k):agyEditBtn;if(agyEdit){g=_R.createElement("div",{className:"w-full flex flex-col gap-2 pt-1"},_R.createElement("textarea",{value:agyTxt,onChange:ev=>agySetTxt(ev.target.value),placeholder:"Write markdown instructions...",className:"agy-rules-editor w-full font-mono text-xs p-2.5 rounded-lg border border-border bg-muted/40 focus:outline-none focus:ring-1 focus:ring-primary resize-y text-foreground leading-normal",style:{lineHeight:"1.45",minHeight:"260px",maxHeight:"55vh"},spellCheck:!1}),_R.createElement("div",{className:"flex items-center justify-end gap-2"},agyErr&&_R.createElement("span",{className:"text-xs text-destructive mr-auto"},agyErr),_R.createElement("button",{type:"button",onClick:()=>{if(_tmr.current){clearTimeout(_tmr.current);_tmr.current=null;}agySetDone(!1);agySetErr("");agySetEdit(!1)},disabled:agySave,className:"text-xs h-7 px-3 rounded border border-border bg-muted/40 hover:bg-muted text-muted-foreground cursor-pointer"},"Cancel"),_R.createElement("button",{type:"button",onClick:agyDoSave,disabled:agySave,className:"text-xs h-7 px-3 rounded flex items-center gap-1 font-medium bg-primary text-primary-foreground hover:bg-primary/90 cursor-pointer"},agySave?"Saving...":(agyDone?"Saved ✓":"Save"))));}else if(!g){g=_R.createElement("div",{className:"flex flex-col gap-2"},_R.createElement("div",{className:"flex items-center justify-end gap-2"},agyErr&&_R.createElement("span",{className:"text-xs text-destructive mr-auto"},agyErr),_R.createElement("button",{type:"button",onClick:agyDoEdit,"aria-label":"Edit",className:"text-xs h-7 px-2.5 rounded border border-border bg-muted/40 hover:bg-muted text-muted-foreground hover:text-foreground cursor-pointer flex items-center gap-1"},agyEditIcon,"Edit")),f&&(n?_R.createElement(n,{markdown:f,className:"text-sm"}):_R.createElement("pre",{className:"text-sm whitespace-pre-wrap font-mono"},f)));}}var ${6}`,
		},
		{
			ID:      "settings-restore-customizations-tab",
			Desc:    "Preserve Customizations tab in Settings modal so it remains accessible on mobile and desktop",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: func(Options) bool { return true },
			FindRe:  settingsRestoreCustomizationsRe,
			Replace: `${1}=${2}=>${2};${5}`,
			// 2.33 drops the tab in the hook that builds the list of screens, not in a helper of its own.
			ReplaceMatch: func(g [][]byte, _ Options) string {
				if g[1] == nil {
					return string(g[5])
				}
				return string(g[1]) + "=" + string(g[2]) + "=>" + string(g[2]) + ";"
			},
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
		{
			ID:      "virtualization-disable-contraction",
			Desc:    "Prevent scroll height collapse caused by virtualization unmounting upper nodes",
			Target:  MainJS,
			Kind:    Regexp,
			Enabled: func(Options) bool { return true },
			FindRe:  virtualizationDisableContractionRe,
			Replace: `contractionSafetyPx:8E3,outerRadiusPx:1.5E4`,
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
			// The 2.32 form keeps the declaration of the variable it no longer
			// reads. The path is written as it is, so a "$" in it is not taken
			// for a group reference.
			ReplaceMatch: func(g [][]byte, o Options) string {
				return string(g[1]) + `initialPath:` + jsString(o.WorkspaceRoot) + string(g[2])
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
			ID:      "composer-line-start-nav",
			Desc:    "Fix Cmd+ArrowLeft (macOS) and Home jumping to start of text when slash commands or chips exist",
			Target:  HTML,
			Kind:    InjectHead,
			Replace: lineStartNavScript,
		},
		{
			ID:      "connection-watchdog",
			Desc:    "Auto-dismiss stale connection banners once reconnected and recover from stuck loading spinners",
			Target:  HTML,
			Kind:    InjectHead,
			Replace: connectionWatchdogScript,
		},
		{
			ID:      "composer-upload-menu-item",
			Desc:    "Add Upload File menu item to the composer plus menu",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  composerUploadMenuRe,
			Replace: `{icon:$1=>$2.createElement($3,{name:"attach_file",size:$1.width?Number($1.width):14,className:$1.className}),label:"Upload File",onClick:()=>window.__agyTriggerUpload&&window.__agyTriggerUpload()},{icon:$1=>$2.createElement($3,{name:"$4",size:$1.width?Number($1.width):14,className:$1.className}),label:"$5",onClick:$6}`,
		},
		{
			ID:      "file-upload-accept-all",
			Desc:    "Allow selecting any file type in the composer attachment dialog",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  fileUploadAcceptAllRe,
			Replace: `${1}accept:"*/*",multiple:!0`,
		},
		{
			ID:      "file-drop-accept-all",
			Desc:    "Allow dropping any file extension into the composer attachment handler",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  fileDropAcceptAllRe,
			Replace: `function $1(a,b=""){return!0}`,
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
			Desc:    "Allow non-standard text and data files like .har and .jsonl to be attached as text/plain or application/json",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  fileUploadCustomTextTypesRe,
			Replace: renderCustomTextTypes("${1}", "${2}", "${3}", "${5}", "${6}"),
			// 2.32 names its extension table in a different group, and its
			// application/xml answer is kept.
			ReplaceMatch: func(g [][]byte, _ Options) string {
				table := g[4]
				if table == nil {
					table = g[5]
				}
				return renderCustomTextTypes(string(g[1]), string(g[2]), string(g[3]), string(table), string(g[6]))
			},
		},
		{
			ID:      "file-upload-large-file-streaming-fallback",
			Desc:    "Stream files exceeding native size limits to the workspace asynchronously with progress UI",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  fileUploadLargeFileStreamingRe,
			Replace: `if($2)if($3.size>1048576){if(window.__agyUpload){if(!window.__agyHandledFiles||!window.__agyHandledFiles.has($3))window.__agyUpload([$3]);return;}}/*partition-v4*/`,
			ReplaceMatch: func(g [][]byte, _ Options) string {
				if g[2] != nil {
					return `if(` + string(g[2]) + `)if(` + string(g[3]) + `.size>1048576){if(window.__agyUpload){if(!window.__agyHandledFiles||!window.__agyHandledFiles.has(` + string(g[3]) + `))window.__agyUpload([` + string(g[3]) + `]);return;}}`
				}
				out := fileUploadIcbHeaderRe.ReplaceAllString(string(g[0]), `${1}window.__agySyncMediaState&&window.__agySyncMediaState($2,$3);${4}if($6>=$7||(window.__agyShouldStreamSingle&&window.__agyShouldStreamSingle($5,$6,$7))){if(window.__agyUpload){if((!window.__agyHandledFiles||!window.__agyHandledFiles.has($5))&&(!window.__agyIsFileHandled||!window.__agyIsFileHandled($5)))window.__agyUpload([$5]);return;}$8}`)
				out = fileUploadVideoBranchRe.ReplaceAllStringFunc(out, func(m string) string {
					sub := fileUploadVideoBranchRe.FindStringSubmatch(m)
					if len(sub) < 4 {
						return m
					}
					return `else if(` + sub[1] + `.includes(` + sub[2] + `.type.toLowerCase()))` + sub[3] + `if(` + sub[2] + `.size>20971520){if(window.__agyUpload){if((!window.__agyHandledFiles||!window.__agyHandledFiles.has(` + sub[2] + `))&&(!window.__agyIsFileHandled||!window.__agyIsFileHandled(` + sub[2] + `)))window.__agyUpload([` + sub[2] + `]);return;}}else try{`
				})
				out = fileUploadSizeLimitBranchRe.ReplaceAllString(out, `if($1.size>$2){if(window.__agyUpload){if((!window.__agyHandledFiles||!window.__agyHandledFiles.has($1))&&(!window.__agyIsFileHandled||!window.__agyIsFileHandled($1)))window.__agyUpload([$1]);return;}}`)
				return fileUploadBatchEntryRe.ReplaceAllStringFunc(out, func(m string) string {
					sub := fileUploadBatchEntryRe.FindStringSubmatch(m)
					if len(sub) >= 7 && sub[5] != "" && sub[6] != "" {
						return sub[1] + `=window.__agyPartitionFiles?window.__agyPartitionFiles(` + sub[1] + `,` + sub[5] + `-` + sub[6] + `,` + sub[6] + `):Array.from(` + sub[1] + `).filter(_f=>(!window.__agyHandledFiles||!window.__agyHandledFiles.has(_f))&&(!window.__agyIsFileHandled||!window.__agyIsFileHandled(_f)));if(` + sub[1] + `.length!==0){` + sub[2]
					}
					return sub[1] + `=window.__agyPartitionFiles?window.__agyPartitionFiles(` + sub[1] + `):Array.from(` + sub[1] + `).filter(_f=>(!window.__agyHandledFiles||!window.__agyHandledFiles.has(_f))&&(!window.__agyIsFileHandled||!window.__agyIsFileHandled(_f)));if(` + sub[1] + `.length!==0){`
				})
			},
		},
		{
			ID:      "question-modal-write-in-radio",
			Desc:    "Prevent write-in radio button from unchecking on repeated label taps in single-select questions",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  questionModalWriteInRadioRe,
			Replace: `${1}(${6}.isMultiSelect?!${4}:!0)${5}`,
		},
		{
			ID:      "question-modal-write-in-focus",
			Desc:    "Trigger write-in radio selection and keyboard focus when write-in textarea is focused",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  questionModalWriteInFocusRe,
			Replace: `${1},onFocus:()=>{${2}||(${3}(!0),${4}.isMultiSelect||${5}())}${6}`,
			// 2.33 names the click handler, which the write-in's focus gets as well.
			ReplaceMatch: func(g [][]byte, _ Options) string {
				if g[7] != nil {
					return string(g[7]) + ",onFocus:" + string(g[8]) + string(g[9])
				}
				return string(g[1]) + ",onFocus:()=>{" + string(g[2]) + "||(" + string(g[3]) + "(!0)," + string(g[4]) + ".isMultiSelect||" + string(g[5]) + "())}" + string(g[6])
			},
		},
		{
			ID:      "question-modal-prevent-radio-focus-steal",
			Desc:    "Prevent question modal from stealing focus from write-in textarea back to objective radio options",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  questionModalPreventRadioFocusStealRe,
			Replace: `${1}&&document.activeElement?.getAttribute?.("data-testid")!=="ask-question-writein"&&!(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches)${2}`,
			ReplaceMatch: func(g [][]byte, _ Options) string {
				if g[3] != nil {
					return string(g[3]) + `||document.activeElement?.getAttribute?.("data-testid")==="ask-question-writein"||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches)` + string(g[4])
				}
				return string(g[1]) + `&&document.activeElement?.getAttribute?.("data-testid")!=="ask-question-writein"&&!(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches)` + string(g[2])
			},
		},
		// Since 2.31 the hook measures the distance from the position itself.
		{
			ID:       "autoscroll-distance-fix",
			Desc:     "Restore stable physical container scroll distance calculation in auto-scroll hook",
			Target:   MainJS,
			Kind:     Regexp,
			Enabled:  func(Options) bool { return true },
			Optional: true,
			FindRe:   autoscrollDistanceFixRe,
			Replace:  `return ${1}?(${1}.scrollHeight-${1}.clientHeight-${1}.scrollTop)<=${3}:!1`,
		},
		// The hook treats any drop in scrollTop as the user scrolling up and stops following the conversation.
		// Rows remounting can collapse the content for a moment, which clamps scrollTop without anyone touching
		// the screen, and the scroll event only arrives once the content is back. The hook then stays where the
		// clamp left it, far above the bottom. A drop that removed rows can explain, with no input, is put back instead.
		// The app's own upward scrolls found so far come with a key, a focus change or the find bar, and jumping from the
		// navigator stops following explicitly first.
		{
			ID:      "scroll-input-tracker",
			Desc:    "Tell the reader's own scrolling from a position the page or the browser moved, and keep their place while rows above them load or drop",
			Target:  HTML,
			Kind:    InjectHead,
			Replace: scrollInputTracker,
		},
		{
			ID:      "autoscroll-unpin-needs-input",
			Desc:    "Stop following the conversation only when a drop in scroll position came from user input",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  autoscrollUnpinInputRe,
			Replace: `${1}(${3}.current&&!${8}.current&&window.__agyIsSpontaneousScroll?.(${2})?${2}.scrollTo({top:${2}.scrollHeight,behavior:"instant"}):(${3}.current=!1,${4})):${6}`,
			ReplaceMatch: func(g [][]byte, _ Options) string {
				el := string(g[2])
				pinned, unpin := string(g[5]), string(g[5])+".current=!1"
				if g[3] != nil {
					pinned, unpin = string(g[3]), string(g[3])+".current=!1,"+string(g[4])
				}
				followOn := ""
				if g[7] != nil {
					followOn = "&&!" + string(g[7]) + ".current"
				} else if g[8] != nil {
					followOn = "&&!" + string(g[8]) + ".current"
				}
				return string(g[1]) + "(" + pinned + ".current" + followOn + "&&window.__agyIsSpontaneousScroll?.(" + el + ")?" +
					el + `.scrollTo({top:` + el + `.scrollHeight,behavior:"instant"}):(` + unpin + "))" + ":" + string(g[6])
			},
		},
		// A second after older rows load above the reader, the bundle drops the rows far above them and moves the position
		// up by what it dropped, usually while the reader is still scrolling up. On an iPhone the position then runs back
		// down the conversation by 300 to 1000 px, and a finger still down drags on from where it was. An update that only
		// drops rows from the top waits, retried until the conversation is still. One that adds rows above is put to the
		// scroll tracker as well: since 2.32 the bundle asks for them in the middle of a fling, and a position moved for
		// them then is taken back, so the reader lands as far up the new rows as they are tall. The tracker lets them in
		// when it has room above the first row to put them in, and otherwise has them wait the same way. It is asked
		// about those more often, because a reader who has stopped may be waiting for them. It only has an answer for a
		// bundle that hands it the move that keeps the rows in place, which is the next patch: a bundle before 2.32 is
		// asked nothing new by this, and adds its rows when it always did. Optional because the paging code is not known
		// to be in every supported bundle; the live bundle test reports it missing where the paging code is.
		{
			ID:       "paging-trim-waits-for-still",
			Desc:     "On iOS, hold rows the bundle would add or drop above the reader until the scroll tracker lets them through",
			Target:   MainJS,
			Kind:     Regexp,
			FindRe:   pagingTrimWaitsRe,
			Optional: true,
			Replace:  `${1}else if(!${2}&&${3}.startIndex>${4}.startIndex&&window.__agyDeferTrim?.())${5}(250);else if(${3}.startIndex<${4}.startIndex&&window.__agyDeferGrow?.())${5}(100);else{`,
		},
		// When the window of loaded rows moves, the bundle puts the rows on screen back where they were as it commits the
		// change, and relies on the browser's scroll anchoring for anything that shifts them after that. Tables do: each
		// finds its width in a layout effect and is drawn again with shorter cells. WebKit does not anchor after a script
		// moved the position, so on an iPhone the rows ended up to 350 px off. The move is handed to the scroll tracker,
		// which makes it again once that drawing is done. The tracker is also told of every drawing, rows moved or not:
		// being told is how it knows, before the first rows arrive, that the move is its own to make, and only then does
		// it keep room for them on iOS. With that it is handed the component's way of finding the rows it chooses among,
		// from which the tracker picks the row at the top of the screen to follow while the browser's anchoring is off.
		// Bundles before 2.32 have no such component and go on moving the position themselves, and one that has it in
		// a form this does not match is reported.
		{
			ID:       "paging-keeps-rows-after-render",
			Desc:     "Keep the rows on screen in place when older rows load, through the second drawing of their tables",
			Target:   MainJS,
			Kind:     Regexp,
			FindRe:   pagingRowsKeptRe,
			Optional: true,
			Precondition: func(b []byte) bool {
				return bytes.Contains(b, []byte("getAnchorCandidates")) && bytes.Contains(b, []byte("getSnapshotBeforeUpdate"))
			},
			Replace: `${1}window.__agyKeepRows?(window.__agyRowsDrawn?.(${2},this.props.getAnchorCandidates),${2}&&${3}&&window.__agyKeepRows(${2},${3}.element,${3}.offset)):${4}}`,
		},
		// The bundle asks for older rows once the first row is within 600 px of the screen. Drawing them takes a phone
		// 0.3 to 0.5 s, and a flick covers that distance sooner, so the reader arrived at the top of what was loaded
		// before the rows did. On iOS the scroll tracker keeps room for rows to arrive in during a fling, and asks for
		// them from further away. Elsewhere, and without the tracker, the bundle's own distance stays. So it does in a
		// bundle that moves the position for its rows itself: asked for sooner, they would arrive in the middle of a
		// fling more often, with nowhere to go but a position that is taken back. The bundle has one distance for both
		// ends of what is loaded, so rows below the reader are asked for from as far too, where a jump to an older step
		// has left some to load. Those move nothing the reader sees.
		//
		// The tracker takes the bundle's asking for this distance as its word that it hands its rows over, which is
		// what Requires makes true.
		{
			ID:       "paging-loads-rows-ahead",
			Desc:     "On iOS, ask for older rows while the reader is still some way from the first one loaded",
			Target:   MainJS,
			Kind:     Regexp,
			FindRe:   pagingGrowRadiusRe,
			Optional: true,
			Requires: "paging-keeps-rows-after-render",
			Precondition: func(b []byte) bool {
				return bytes.Contains(b, []byte("innerRadiusPx")) && bytes.Contains(b, []byte("contractionSafetyPx"))
			},
			Replace: `${1}(window.__agyGrowRadius||${2})${3}`,
		},
		// A long conversation's window is cut at the top while the reader moves down it, which changes the scroll position
		// and its range under a held scrollbar. The bundle maps a drag from where it began, so the next pointer move undoes
		// the correction and throws the reader thousands of pixels away. Builds before the overlay scrollbar have no drag.
		{
			ID:       "scrollbar-drag-follows-paging",
			Desc:     "Keep a scrollbar drag in place when paging moves the conversation under it",
			Target:   MainJS,
			Kind:     Regexp,
			FindRe:   scrollbarDragRe,
			Optional: true,
			Replace:  `window.__agyDragScrollbar?window.__agyDragScrollbar(${2}.current,${7},${3},${3}?${4}.clientX:${4}.clientY,${5},${6}.maxScrollPos):(${1}=${2}.current.startScrollPos+((${3}?${4}.clientX:${4}.clientY)-${2}.current.startCoord)/${5}*${6}.maxScrollPos,${3}?${7}.scrollLeft=${1}:${7}.scrollTop=${1}),`,
		},
		// On iOS the scroll tracker keeps empty room above the conversation's first row, which is most of the position and
		// of the height. The bundle's scrollbar measured both whole: its thumb sat at the bottom and jumped whenever the
		// room was filled back up. It is told how much room there is and leaves it out, so the thumb is as tall as the
		// rows are, where the reader is among them, and a click on its track lands among them too. Its thumb is moved by
		// a scroll timeline where the browser has one, which runs the whole range and cannot leave any out, so with room
		// to leave out it is moved by script as it is elsewhere. Without the tracker, and off iOS, nothing changes. Nor
		// is any of it done to a bundle that does not hand over the move the room depends on, which gets no room.
		{
			ID:       "scrollbar-measures-rows",
			Desc:     "Leave the room kept above the conversation out of what its scrollbar shows",
			Target:   MainJS,
			Kind:     Regexp,
			FindRe:   scrollbarMeasureRe,
			Optional: true,
			Requires: "paging-keeps-rows-after-render",
			Precondition: func(b []byte) bool {
				return bytes.Contains(b, []byte("data-overlay-scrollbar-track"))
			},
			Replace: `${1}if(!${3}&&window.__agyTopRoom){var _agyRoom=window.__agyTopRoom(${4});${2}-=_agyRoom;${5}-=_agyRoom}`,
		},
		{
			ID:       "scrollbar-click-lands-in-rows",
			Desc:     "Land a click on the conversation's scrollbar track among its rows, below the room kept above them",
			Target:   MainJS,
			Kind:     Regexp,
			FindRe:   scrollbarTrackClickRe,
			Optional: true,
			Requires: "paging-keeps-rows-after-render",
			Precondition: func(b []byte) bool {
				return bytes.Contains(b, []byte("data-overlay-scrollbar-track"))
			},
			Replace: `${1}${3}+(window.__agyTopRoom?.(${2})||0)`,
		},
		{
			ID:       "scrollbar-thumb-by-script",
			Desc:     "Move the scrollbar thumb by script where room is kept above the conversation",
			Target:   MainJS,
			Kind:     Regexp,
			FindRe:   scrollbarTimelineRe,
			Optional: true,
			Requires: "paging-keeps-rows-after-render",
			Precondition: func(b []byte) bool {
				return bytes.Contains(b, []byte("data-overlay-scrollbar-track")) && bytes.Contains(b, []byte("globalThis.ScrollTimeline"))
			},
			Replace: `${1}&&!window.__agyTopRoom${2}`,
		},
		// The bundle ships a WebSocket transport that multiplexes every RPC over
		// /connect-websocket, but only enables it behind ?useWebSocket=true. On
		// iOS Safari the default fetch streaming transport regularly stalls
		// mid-response, which leaves the conversation list empty or the
		// conversation spinner running forever. Make WebSocket the default and
		// keep ?useWebSocket=false as a per-page escape hatch. Operators can turn
		// it off for everyone with --disable-patch websocket-transport-default.
		// Not optional: if the selector disappears the iOS stalls come back, so
		// doctor and the startup report must flag it as missing.
		{
			ID:     "websocket-transport-default",
			Desc:   "Use the bundled WebSocket RPC transport by default instead of fetch streaming",
			Target: MainJS,
			Kind:   Regexp,
			FindRe: websocketTransportDefaultRe,
			// Up to 2.22 the function returned a boolean and the default was
			// the fetch transport. From 2.23 it returns "off", "explicit" or
			// "hub", where explicit is the plain WebSocket this patch has always
			// selected and hub adds a fetch fallback after a 4s dial timeout, which
			// is why hub is not chosen. wsTransport=1 turns the transport off in
			// both spellings, and any other value selects explicit, so the
			// upstream wsTransport=3 route to hub is no longer reachable. Replace is the current spelling. The older one has
			// no second group and is handled by ReplaceMatch. Group 4 only exists
			// in the older spelling.
			Replace: `${1}${2}${3}==="1"?"off":"explicit"}`,
			ReplaceMatch: func(g [][]byte, _ Options) string {
				if g[4] != nil {
					return string(g[1]) + string(g[4]) + `!=="1"}`
				}
				return string(g[1]) + string(g[2]) + string(g[3]) + `==="1"?"off":"explicit"}`
			},
		},

		// The bundle's WebSocket client closes the socket when no frame arrives
		// for 2s and a ping goes unanswered for another 2s. On a slow phone link
		// the large initial frames arrive further apart than that, and the ping
		// reply queues behind them, so the socket is dropped mid-transfer and the
		// page reconnects and downloads everything again, which never finishes.
		// Give the ping reply 30s so a slow but alive link survives. The bundle
		// waits 2s up to 2.31 and 5s in 2.32; a longer wait of its own is kept.
		// Not optional: when the anchor moves the drops come back silently, so
		// doctor and the startup report must flag it as missing.
		{
			ID:      "websocket-liveness-probe-relax",
			Desc:    "Give the WebSocket liveness ping 30s instead of a few seconds so slow links are not dropped mid-transfer",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  websocketLivenessProbeRe,
			Replace: `${1}Math.max(3E4,${2})${3}`,
		},

		// During app bootstrap, getServerConfiguration and getMendelFlags catch
		// RPC errors, whereas getLocalUserInfo has no retry or fallback. If the
		// initial RPC socket closes while a mobile PWA is briefly backgrounded,
		// getLocalUserInfo rejects and aborts createRoot().render(), leaving #root
		// permanently blank. Retry up to 3 times with short backoff so the
		// transport can open a fresh socket, and fall back to a default root URI
		// if all attempts fail so the UI still mounts.
		{
			ID:      "bootstrap-local-user-info-retry",
			Desc:    "Retry getLocalUserInfo during startup so a transient socket drop does not abort mounting #root",
			Target:  MainJS,
			Kind:    Regexp,
			FindRe:  bootstrapLocalUserInfoRetryRe,
			Replace: `async function $1(a){var b=$2,c=$2;try{let $3;for(let _i=0;_i<3;_i++)try{$3=c(await b(a.lsClient.getLocalUserInfo($4)));break}catch(_e){c();if(_i===2)return{homeDirUri:$5("file:///"),username:""};await new Promise(_r=>setTimeout(_r,250*(_i+1)))}let f=$3.username;return{homeDirUri:$5($3.homeDirUri),username:f}}finally{b()}}`,
		},

		// The first sign-in check gives each request 10 seconds and signs out after four retries, about a minute. All
		// requests share one socket, so on a slow link they wait behind the conversation the page is loading, and the check
		// can fail for that long with nothing wrong with the account. The app then shows Google as signed out until it is
		// restarted. Failures the bundle itself counts as transient now keep retrying 8 seconds apart for five more
		// minutes before it signs out. Other failures, and the other callers that never retried, are as they were.
		{
			ID:       "auth-startup-retry",
			Desc:     "Keep retrying the startup sign-in check on a slow link for five more minutes before signing out",
			Target:   MainJS,
			Kind:     Regexp,
			Optional: true,
			// Bundles before the check had a timeout do not retry it. One that
			// retries in a form this does not match must be reported.
			Precondition: authStartupBackoffRe.Match,
			FindRe:       authStartupRetryRe,
			Replace:      `${1}${2}>1?${2}-1:Date.now()<(${3}.__agyAuthRetryUntil??=Date.now()+3E5)?1:0${4}`,
		},

		// Since 2.29 UserStatusSync polls getUserStatus every ten minutes and wipes
		// the Redux userStatus slice to an empty schema whenever that RPC throws.
		// When an iOS PWA returns after ten to fifteen minutes in the background, the
		// overdue timer fires on the closed WebSocket before the transport redials,
		// clearing the model list and the selected model until the next ten-minute
		// tick. Keep the previous userStatus on RPC errors (signedOut still clears it
		// through its own branch and signOutIfServerHasNoCredential still runs when
		// present), retry with exponential backoff, and refresh on foreground wake.
		// Optional because builds up to 2.28 already leave userStatus untouched on
		// errors. 2.32 added a mode in which the function keeps the status, but
		// the poll does not use it.
		{
			ID:       "user-status-keep-on-error",
			Desc:     "Keep loaded models and retry when a user-status refresh fails on a dropped socket",
			Target:   MainJS,
			Kind:     Regexp,
			Optional: true,
			// A bundle that logs the failed fetch and does anything more than
			// the credential check still clears the status, so a form this
			// does not match must be reported.
			Precondition: func(b []byte) bool {
				return bytes.Contains(b, []byte(`"[UserStatusSync] Failed to fetch user status:"`)) && !userStatusNoResetOnErrorRe.Match(b)
			},
			FindRe:  userStatusKeepOnErrorRe,
			Replace: renderUserStatusKeepOnError("${1}", "${2}", ",${3}${4}${5}", "${11}", "${12}", "${13}", "${14}"),
			ReplaceMatch: func(g [][]byte, _ Options) string {
				// Since 2.33 a permission error is still shown, and only the clearing is left out.
				if g[6] != nil {
					return renderUserStatusRetry(
						`catch(`+string(g[6])+`){if(`+string(g[7])+`,`+string(g[8])+`){`+string(g[9])+string(g[10])+`}return!1}`,
						string(g[11]), string(g[12]), string(g[13]), string(g[14]),
					)
				}
				credCheck := ""
				if g[5] != nil {
					credCheck = "," + string(g[3]) + string(g[4]) + string(g[5])
				}
				return renderUserStatusKeepOnError(
					string(g[1]),
					string(g[2]),
					credCheck,
					string(g[11]),
					string(g[12]),
					string(g[13]),
					string(g[14]),
				)
			},
		},

		{
			ID:      "remote-runtime-bridge",
			Desc:    "Provide runtime helpers for proxied sidecar UIs, loopback previews, and remote OAuth callbacks",
			Target:  HTML,
			Kind:    InjectHead,
			Replace: remoteRuntimeBridgeScript,
		},
		{
			ID:           "sidecar-resolve-url",
			Desc:         "Rewrite localhost sidecar gateway URLs (Swarm UI and UI plugins) through the same-origin sidecar proxy",
			Target:       MainJS,
			Kind:         Regexp,
			Optional:     true,
			Precondition: func(b []byte) bool { return bytes.Contains(b, []byte("resolveSidecarUrl:")) },
			FindRe:       sidecarResolveURLRe,
			Replace:      `${1}(window.__agyResolveSidecarUrl?window.__agyResolveSidecarUrl(${2}):${2})${3}(window.__agyResolveSidecarUrl?window.__agyResolveSidecarUrl(${4}):${4})`,
		},
		{
			ID:           "sidecar-iframe-sandbox",
			Desc:         "Sandbox the sidecar panel's iframe, so a page the proxy did not give a sandbox CSP still has an opaque origin",
			Target:       MainJS,
			Kind:         Regexp,
			Optional:     true,
			Precondition: func(b []byte) bool { return bytes.Contains(b, []byte(`dataset.testid="sidecar-iframe"`)) },
			FindRe:       sidecarIframeSandboxRe,
			Replace:      `${1}${2}.setAttribute("sandbox",` + strconv.Quote(SidecarFrameSandbox) + `);${3}`,
		},
		{
			ID:           "sidecar-artifact-comment-origin",
			Desc:         "Accept add-artifact-comment from the sandboxed sidecar frame when it carries the gateway token of the frame's URL",
			Target:       MainJS,
			Kind:         Regexp,
			Optional:     true,
			Precondition: func(b []byte) bool { return bytes.Contains(b, []byte(`"add-artifact-comment"`)) },
			FindRe:       sidecarCommentOriginRe,
			Replace:      `${1}${3}=${4}===${5}||${4}==="null"&&!!window.__agySidecarMessageOK&&window.__agySidecarMessageOK(${2}.src,${7}.data)${6}`,
		},
		{
			ID:       "sidecar-modal-endpoint-url",
			Desc:     "Display proxied sidecar URLs in the Sidecars status modal",
			Target:   MainJS,
			Kind:     Regexp,
			Optional: true,
			Precondition: func(b []byte) bool {
				return bytes.Contains(b, []byte(".webPort")) && bytes.Contains(b, []byte("http://127.0.0.1:"))
			},
			FindRe:  sidecarModalEndpointRe,
			Replace: `${1}(_u=>window.__agyResolveSidecarUrl?window.__agyResolveSidecarUrl(_u):_u)(${2})`,
		},
		{
			ID:       "loopback-preview-feature-gate",
			Desc:     "Enable the isolated srcdoc loopback preview side panel on web for localhost links",
			Target:   MainJS,
			Kind:     Regexp,
			Optional: true,
			Precondition: func(b []byte) bool {
				return bytes.Contains(b, []byte("browserAuxPane:")) && bytes.Contains(b, []byte("remoteConversationBrowserPane:"))
			},
			FindRe:  loopbackPreviewFeatureGateRe,
			Replace: `${1}${2}${4}${3}`,
		},
		{
			ID:           "loopback-preview-fetch-fallback",
			Desc:         "Fall back to /__agy/api/loopback-fetch when FetchSandboxPort is unimplemented on standalone language_server",
			Target:       MainJS,
			Kind:         Regexp,
			Optional:     true,
			Precondition: func(b []byte) bool { return bytes.Contains(b, []byte("fetchSandboxPort:")) },
			FindRe:       loopbackPreviewFetchFallbackRe,
			Replace:      `${1}{if(!${2}[0]?.conversationId&&window.__agyFetchLoopbackPort)return window.__agyFetchLoopbackPort(${2}[0],${2}[1]);try{return await ${4}.fetchSandboxPort(...${2})}catch(_e){if(_e?.code===12&&window.__agyFetchLoopbackPort)return window.__agyFetchLoopbackPort(${2}[0],${2}[1]);throw _e}}${5}`,
		},
		{
			ID:       "router-navigate-bridge",
			Desc:     "Capture the SPA router instance so remote helpers can open settings screens without reloading",
			Target:   MainJS,
			Kind:     Regexp,
			Optional: true,
			Precondition: func(b []byte) bool {
				return bytes.Contains(b, []byte(".navigate({")) && bytes.Contains(b, []byte("startPluginAuthSignIn:"))
			},
			FindRe:  routerNavigateBridgeRe,
			Replace: `${1}window.__agyRouter=${2};${3}`,
		},
		{
			ID:           "plugin-mention-remote-signin",
			Desc:         "Route plugin @-mention sign-in clicks on web to the plugin Accounts settings tab",
			Target:       MainJS,
			Kind:         Regexp,
			Optional:     true,
			Precondition: func(b []byte) bool { return bytes.Contains(b, []byte("startPluginAuthSignIn:")) },
			FindRe:       pluginMentionRemoteSignInRe,
			Replace:      `${1}!1,_agyDesk=Boolean(${5}&&(${6}));${4}=Boolean(${5}&&(_agyDesk||!!window.__agyOpenPluginAuth))${7}!_agyDesk&&window.__agyOpenPluginAuth?window.__agyOpenPluginAuth(${8}.plugin):_agyDesk&&${3}&&${9}`,
		},
		{
			ID:           "mcp-oauth-remote-callback",
			Desc:         "Relay pasted loopback OAuth redirect URLs and extract auth codes in CompleteMcpOAuth",
			Target:       MainJS,
			Kind:         Regexp,
			Optional:     true,
			Precondition: func(b []byte) bool { return bytes.Contains(b, []byte("completeMcpOAuth:")) },
			FindRe:       mcpOAuthRemoteCallbackRe,
			Replace:      `${1}{const _sub=_c=>${4}_c${5};return window.__agyCompleteRemoteOAuth?window.__agyCompleteRemoteOAuth(${3},_sub):_sub(${3})}`,
		},
		{
			ID:           "plugin-oauth-remote-callback",
			Desc:         "Relay pasted loopback OAuth redirect URLs and extract auth codes in SignInPluginAuthProvider",
			Target:       MainJS,
			Kind:         Regexp,
			Optional:     true,
			Precondition: func(b []byte) bool { return bytes.Contains(b, []byte("signInPluginAuthProvider:")) },
			FindRe:       pluginOAuthRemoteCallbackRe,
			Replace:      `${1}{const _sub=_s=>${3}{...${2},secret:_s}${4};return ${2}?.secret&&window.__agyCompleteRemoteOAuth?window.__agyCompleteRemoteOAuth(${2}.secret,_sub):${3}${2}${4}}`,
		},

		// The side panel keeps its active tab mounted while it is closed, and
		// opening a conversation restores the tab its panel showed last. A
		// file or artifact viewer there reads the whole file through the
		// language server on every visit, which over a remote link delays the
		// conversation. These patches mount such a viewer only once the panel
		// opens, and drop its tab from the URL when the panel closes.
		{
			ID:      "side-panel-tab-helper",
			Desc:    "Provide the component that holds back a closed side panel's file viewer and clears the viewer's tab from the URL",
			Target:  HTML,
			Kind:    InjectHead,
			Replace: sidePanelTabScript,
		},
		{
			ID:           "side-panel-closed-file-viewer",
			Desc:         "Don't load a file or artifact into the side panel while it is closed",
			Target:       MainJS,
			Kind:         Regexp,
			Optional:     true,
			Precondition: func(b []byte) bool { return bytes.Contains(b, []byte(`"data-side-panel-body"`)) },
			FindRe:       sidePanelBodyRe,
			Replace:      `${1}${2}?(window.__agyPaneTab?${4}.createElement(window.__agyPaneTab,{key:${2}.id,R:${4},treeId:${5},tab:${2},open:${9},idle:${4}.createElement(${7},{${8}:${5},isPaneOpen:!1})},${3}):${3}):${6}`,
		},
		{
			ID:       "side-panel-closed-file-viewer-legacy",
			Desc:     "Don't load a file or artifact into the side panel while it is closed, when the side panel redesign is off",
			Target:   MainJS,
			Kind:     Regexp,
			Optional: true,
			Precondition: func(b []byte) bool {
				return bytes.Contains(b, []byte(`"data-side-panel-body"`)) && bytes.Contains(b, []byte(`stripRef:`))
			},
			FindRe:  sidePanelBodyLegacyRe,
			Replace: `${1}${3}&&(window.__agyPaneTab?${5}.createElement(window.__agyPaneTab,{key:${3}.id,R:${5},treeId:${6},tab:${3},open:${2},idle:null},${4}):${4})`,
		},
		{
			ID:       "conversation-nav-tab-open-pane",
			Desc:     "Put the side panel's tab in a conversation's URL only while that panel is open, as the mobile layout does",
			Target:   MainJS,
			Kind:     Regexp,
			Optional: true,
			Precondition: func(b []byte) bool {
				return bytes.Contains(b, []byte(`.sidePanelV2.conversationPanes[`)) && conversationNavOpenCheckSkippedRe.Match(b)
			},
			FindRe:  conversationNavTabRe,
			Replace: `${1}${2}`,
		},

		// Opening the RPC socket costs several round trips, and the bundle only
		// starts it after main.js has been fetched, revalidated and run. On a
		// phone that is seconds of waiting before the first byte of data. This
		// starts the same connection from the document head, in parallel with the
		// bundle, and hands it over when the bundle asks for it.
		{
			ID:     "connection-prewarm",
			Desc:   "Open the RPC WebSocket while the bundle loads instead of after it runs",
			Target: HTML,
			Kind:   InjectHead,
			// Without the WebSocket transport the bundle never claims the socket,
			// so opening one would only add load. That covers both the operator
			// turning the transport patch off and the patch failing to match,
			// which the proxy reports through WebSocketTransportMissing once it
			// has seen the bundle. It must also stay after connection-watchdog,
			// which wraps WebSocket first, so the early socket is created
			// through that wrapper; TestConnectionPrewarmFollowsWatchdog pins
			// that order.
			Enabled: func(o Options) bool {
				return !o.Disabled["websocket-transport-default"] && !o.WebSocketTransportMissing
			},
			Replace: connectionPrewarmScript,
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
			Desc:    "Record the viewport and shell geometry around every keyboard event on a touch screen",
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
@media (pointer: coarse) {
  button,input,textarea,select{touch-action:manipulation;-webkit-tap-highlight-color:transparent}
  input,textarea,select{font-size:16px !important}
}
</style>`

const safeArea = `<style id="agy-safe-area">
textarea.agy-rules-editor {
  font-size: 11px !important;
  line-height: 1.45 !important;
  min-height: 200px !important;
  max-height: 500px !important;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace !important;
}
@media (pointer: coarse) {
  textarea.agy-rules-editor {
    font-size: 16px !important;
  }
}

/* Flow user message action buttons (Undo, Copy, Timestamp) naturally without overlapping message text */
div[data-testid="user-input-step"] div.bg-card:has(.user-input-buttons-container):not(.user-input-buttons-container),
.group\/user-input-step div.bg-card:has(.user-input-buttons-container):not(.user-input-buttons-container) {
  display: flex !important;
  flex-direction: column !important;
  align-items: stretch !important;
  overflow: visible !important;
  position: relative !important;
}
/* Keep queued messages (waiting in execution queue) on a single row with the delete icon */
div[data-testid="user-input-step"] div.bg-card:has([data-testid="queued-decorators"]),
.group\/user-input-step div.bg-card:has([data-testid="queued-decorators"]) {
  display: flex !important;
  flex-direction: row !important;
  align-items: flex-end !important;
}
div[data-testid="user-input-step"] div.user-input-buttons-container,
.group\/user-input-step div.user-input-buttons-container,
div.user-input-buttons-container {
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
  display: flex !important;
  flex-direction: row !important;
  flex-wrap: nowrap !important;
  align-items: center !important;
  gap: 0.25rem !important;
  width: auto !important;
  height: auto !important;
}
div.user-input-buttons-container > * {
  display: inline-flex !important;
  align-items: center !important;
  flex-shrink: 0 !important;
}
/* Bubble-style user messages keep their actions (Undo, Copy) in a row of their own that shows only on hover, which a
   touch screen does not have. It keeps its space while hidden, so showing it moves nothing. */
@media (pointer: coarse) {
  [data-testid="user-message-actions"] {
    opacity: 0.85 !important;
    pointer-events: auto !important;
  }
}

/* Tablet (iPad) and desktop (screens > 768px): Ensure sidebar padding to prevent truncation */
@media (min-width: 769px) {
  div[role="navigation"][aria-label="Sidebar"],
  div[role="navigation"].bg-sidebar {
    padding-top: max(0.75rem, env(safe-area-inset-top, 12px)) !important;
    padding-bottom: max(0.75rem, env(safe-area-inset-bottom, 14px)) !important;
    box-sizing: border-box !important;
  }
}

/* Touch devices (phones and tablets/iPad): Single safe-area management and keyboard positioning */
@media (pointer: coarse) {
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

  /* Adaptive Dual Mode: When a question popup or interaction is active,
     restore Google Antigravity original pure mode:
     - Unfreeze html & body position (position: static, overflow-y: auto)
     - Allow native layout viewport panning (window.scrollY > 0)
     - Allow dual scrolling (swipe down entire window + scroll top conversation)
     - Remove keyboard-height constraints on the question card */
  html.agy-has-question,
  body.agy-has-question,
  html:has(body.agy-has-question),
  html:has([data-testid="ask-question-header-text"]),
  html:has([data-testid="interaction-continue-button"]),
  html:has([data-testid="interaction-skip-button"]),
  html:has([data-testid="ask-question-writein"]),
  html:has(input[name^="ask-question-"]),
  html:has([data-testid="declared-permissions-modal"]),
  body:has([data-testid="ask-question-header-text"]),
  body:has([data-testid="interaction-continue-button"]),
  body:has([data-testid="interaction-skip-button"]),
  body:has([data-testid="ask-question-writein"]),
  body:has(input[name^="ask-question-"]),
  body:has([data-testid="declared-permissions-modal"]) {
    position: static !important;
    inset: auto !important;
    height: auto !important;
    min-height: 100% !important;
    overflow-y: auto !important;
    overflow-x: hidden !important;
    overscroll-behavior: auto !important;
  }

  /* Eliminate double safe-area padding from Language Server inline style */
  [data-testid="agent-input-box"] {
    padding-bottom: 0px !important;
  }
  [data-testid="agent-input-box"] div[role="textbox"] {
    min-height: 0 !important;
  }
  /* Eliminate 300ms grid expansion delay on mobile focus so composer height settles immediately */
  [data-testid="agent-input-box"] > div.grid {
    transition: none !important;
    transition-duration: 0s !important;
  }
  body:has([data-testid="mobile-new-convo-empty"]) [data-testid="agent-input-box"] > div.grid {
    grid-template-rows: 1fr !important;
    opacity: 1 !important;
  }

  /* The element around a conversation's prompt is found by its classes. Antigravity 2.29 gave it pb-4 where it had
     pb-2, and with only the old one named here the prompt sat in the home indicator's area and kept 16px of padding
     above the keyboard. Both are named, and the live bundle test says when neither is there. */
  /* Smooth padding-bottom transition to prevent 34px snap/jerk when keyboard dismisses */
  div.relative.w-full.px-4.flex-shrink-0:is(.pb-2, .pb-4),
  div.shrink-0.p-2 {
    transition: padding-bottom 0.28s cubic-bezier(0.16, 1, 0.3, 1);
  }

  /* Collapse composer wrapper padding when keyboard is open */
  body.agy-kb-open div.relative.w-full.px-4.flex-shrink-0:is(.pb-2, .pb-4),
  body.agy-kb-open div.shrink-0.p-2,
  html[style*="--agy-bottom"] div.relative.w-full.px-4.flex-shrink-0:is(.pb-2, .pb-4),
  html[style*="--agy-bottom"] div.shrink-0.p-2 {
    padding-bottom: 0px !important;
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
    div.h-\[100dvh\].w-screen.flex.flex-col,
    div[data-testid="mobile-layout"],
    div.agy-shell {
      position: absolute !important;
      top: 0px !important;
      left: 0 !important;
      right: 0 !important;
      bottom: var(--agy-bottom, 0px) !important;
      align-self: stretch !important;
      height: auto !important;
      max-height: 100% !important;
      padding-top: 0 !important;
      box-sizing: border-box !important;
      will-change: bottom;
    }
    /* Hardware-accelerated smooth transition on keyboard dismiss without JS cubic reflow */
    body:not(.agy-kb-open) div.h-\[100dvh\].w-screen.flex.flex-col,
    body:not(.agy-kb-open) div[data-testid="mobile-layout"],
    body:not(.agy-kb-open) div.agy-shell {
      transition: bottom 0.28s cubic-bezier(0.16, 1, 0.3, 1);
    }
    /* Constrain mobile/tablet conversation container so top navbar stays pinned when keyboard opens */
    div[data-testid="conversation-view"] {
      max-height: 100% !important;
      min-height: 0 !important;
      overflow-y: hidden !important;
      overflow-x: hidden !important;
      overscroll-behavior-y: contain !important;
      -webkit-overflow-scrolling: touch !important;
    }
    div[data-testid="conversation-view"] div.flex-1.min-h-0.overflow-y-auto,
    div[data-testid="conversation-view"] div.h-full.overflow-y-auto,
    div[data-testid="conversation-view"] [data-testid="autoscroll-viewport"] {
      overscroll-behavior-y: contain !important;
      -webkit-overflow-scrolling: touch !important;
      overflow-anchor: auto !important;
    }

    /* Dual Mode Overrides: Restore relative flow and viewport bounds when question is active */
    body.agy-has-question .relative.w-screen.h-\[100dvh\],
    body:has([data-testid="ask-question-header-text"]) .relative.w-screen.h-\[100dvh\],
    body:has([data-testid="interaction-continue-button"]) .relative.w-screen.h-\[100dvh\],
    body:has([data-testid="ask-question-writein"]) .relative.w-screen.h-\[100dvh\],
    body:has([data-testid="declared-permissions-modal"]) .relative.w-screen.h-\[100dvh\] {
      position: relative !important;
      inset: auto !important;
      width: 100vw !important;
      height: auto !important;
      min-height: 100dvh !important;
      max-height: none !important;
      overflow: visible !important;
    }

    body.agy-has-question div.h-\[100dvh\].w-screen.flex.flex-col,
    body.agy-has-question div[data-testid="mobile-layout"],
    body.agy-has-question div.agy-shell,
    body:has([data-testid="ask-question-header-text"]) div.h-\[100dvh\].w-screen.flex.flex-col,
    body:has([data-testid="ask-question-header-text"]) div[data-testid="mobile-layout"],
    body:has([data-testid="ask-question-header-text"]) div.agy-shell,
    body:has([data-testid="interaction-continue-button"]) div.h-\[100dvh\].w-screen.flex.flex-col,
    body:has([data-testid="interaction-continue-button"]) div[data-testid="mobile-layout"],
    body:has([data-testid="interaction-continue-button"]) div.agy-shell,
    body:has([data-testid="ask-question-writein"]) div.h-\[100dvh\].w-screen.flex.flex-col,
    body:has([data-testid="ask-question-writein"]) div[data-testid="mobile-layout"],
    body:has([data-testid="ask-question-writein"]) div.agy-shell,
    body:has([data-testid="declared-permissions-modal"]) div.h-\[100dvh\].w-screen.flex.flex-col,
    body:has([data-testid="declared-permissions-modal"]) div[data-testid="mobile-layout"],
    body:has([data-testid="declared-permissions-modal"]) div.agy-shell {
      position: relative !important;
      top: 0 !important;
      left: auto !important;
      right: auto !important;
      bottom: auto !important;
      height: 100dvh !important;
      max-height: 100dvh !important;
      overflow: visible !important;
      transition: none !important;
    }

    body.agy-has-question div[data-testid="conversation-view"],
    body:has([data-testid="ask-question-header-text"]) div[data-testid="conversation-view"],
    body:has([data-testid="interaction-continue-button"]) div[data-testid="conversation-view"],
    body:has([data-testid="ask-question-writein"]) div[data-testid="conversation-view"],
    body:has([data-testid="declared-permissions-modal"]) div[data-testid="conversation-view"] {
      overflow-y: hidden !important;
      max-height: 100% !important;
      min-height: 0 !important;
      overscroll-behavior-y: contain !important;
      -webkit-overflow-scrolling: touch !important;
    }

    body.agy-has-question div[data-testid="conversation-view"] div.flex-1.min-h-0.overflow-y-auto,
    body.agy-has-question div[data-testid="conversation-view"] div.h-full.overflow-y-auto,
    body.agy-has-question div[data-testid="conversation-view"] [data-testid="autoscroll-viewport"],
    body:has([data-testid="ask-question-header-text"]) div[data-testid="conversation-view"] div.flex-1.min-h-0.overflow-y-auto,
    body:has([data-testid="ask-question-header-text"]) div[data-testid="conversation-view"] div.h-full.overflow-y-auto,
    body:has([data-testid="ask-question-header-text"]) div[data-testid="conversation-view"] [data-testid="autoscroll-viewport"],
    body:has([data-testid="interaction-continue-button"]) div[data-testid="conversation-view"] div.flex-1.min-h-0.overflow-y-auto,
    body:has([data-testid="interaction-continue-button"]) div[data-testid="conversation-view"] div.h-full.overflow-y-auto,
    body:has([data-testid="interaction-continue-button"]) div[data-testid="conversation-view"] [data-testid="autoscroll-viewport"],
    body:has([data-testid="ask-question-writein"]) div[data-testid="conversation-view"] div.flex-1.min-h-0.overflow-y-auto,
    body:has([data-testid="ask-question-writein"]) div[data-testid="conversation-view"] div.h-full.overflow-y-auto,
    body:has([data-testid="ask-question-writein"]) div[data-testid="conversation-view"] [data-testid="autoscroll-viewport"] {
      overflow-y: auto !important;
      overscroll-behavior-y: auto !important;
      -webkit-overflow-scrolling: touch !important;
    }

    /* Question modal / Option list constraints:
       Ensure header and action buttons never shrink */
    div:has(> [data-testid="ask-question-header-text"]),
    div:has(> div > [data-testid="ask-question-header-text"]),
    div:has(> div > div > [data-testid="ask-question-header-text"]),
    [data-testid="ask-question-header-text"] {
      flex-shrink: 0 !important;
    }
    div:has(> [data-testid="interaction-continue-button"]),
    div:has(> * > [data-testid="interaction-continue-button"]) {
      flex-shrink: 0 !important;
    }
    /* Question card / modal container: clamp max-height so action buttons never get pushed off screen */
    div.outline-none.flex.flex-col:has([data-testid="interaction-continue-button"]),
    div.flex.flex-col:has(> * > [data-testid="interaction-continue-button"]) {
      max-height: calc(100dvh - var(--agy-bottom, 0px) - 50px) !important;
      max-height: calc(100vh - var(--agy-bottom, 0px) - 50px) !important;
      min-height: 0 !important;
    }
    /* Question modal options list: scroll options within available height comfortably above keyboard or screen bottom */
    div[role="radiogroup"]:has(input[name^="ask-question-"]),
    div.flex.flex-col:has(> * > label > input[name^="ask-question-"]),
    div.flex.flex-col:has(> div > label > input[name^="ask-question-"]) {
      flex: 1 1 auto !important;
      min-height: 0 !important;
      max-height: calc(100dvh - var(--agy-bottom, 0px) - 160px) !important;
      max-height: calc(100vh - var(--agy-bottom, 0px) - 160px) !important;
      overflow-y: auto !important;
      -webkit-overflow-scrolling: touch !important;
      overscroll-behavior-y: contain !important;
      padding-right: 2px !important;
    }

    /* Question modal / Bottom sheet: Dock container cleanly above virtual keyboard */
    div.fixed.inset-0:has(> .aux-drawer-popup),
    div.fixed.inset-0:has(.aux-drawer-popup) {
      bottom: var(--agy-bottom, 0px) !important;
    }
    body:not(.agy-kb-open) div.fixed.inset-0:has(> .aux-drawer-popup),
    body:not(.agy-kb-open) div.fixed.inset-0:has(.aux-drawer-popup) {
      transition: bottom 0.28s cubic-bezier(0.16, 1, 0.3, 1);
    }
    .aux-drawer-popup {
      padding-bottom: max(0.5rem, env(safe-area-inset-bottom, 0px)) !important;
      max-height: calc(100% - 1rem) !important;
    }
    body.agy-kb-open .aux-drawer-popup,
    html[style*="--agy-bottom"] .aux-drawer-popup {
      padding-bottom: 0.5rem !important;
    }
    div.fixed.inset-0:has(> .aux-drawer-popup) .fixed.bottom-3,
    div.fixed.inset-0:has(.aux-drawer-popup) .fixed.bottom-3 {
      bottom: calc(0.75rem + var(--agy-bottom, 0px)) !important;
    }

    /* Dual Mode: Release drawer popup bounds in question mode */
    body.agy-has-question div.fixed.inset-0:has(> .aux-drawer-popup),
    body.agy-has-question div.fixed.inset-0:has(.aux-drawer-popup),
    body:has([data-testid="ask-question-header-text"]) div.fixed.inset-0:has(> .aux-drawer-popup),
    body:has([data-testid="ask-question-header-text"]) div.fixed.inset-0:has(.aux-drawer-popup) {
      bottom: 0px !important;
    }
    body.agy-has-question div.fixed.inset-0:has(> .aux-drawer-popup) .fixed.bottom-3,
    body.agy-has-question div.fixed.inset-0:has(.aux-drawer-popup) .fixed.bottom-3,
    body:has([data-testid="ask-question-header-text"]) div.fixed.inset-0:has(> .aux-drawer-popup) .fixed.bottom-3,
    body:has([data-testid="ask-question-header-text"]) div.fixed.inset-0:has(.aux-drawer-popup) .fixed.bottom-3 {
      bottom: 0.75rem !important;
    }
  }
}

/* Tablet (iPad) touch devices (> 768px): Ensure composer wrappers have proper safe-area padding above home indicator */
@media (pointer: coarse) and (min-width: 769px) {
  div.relative.w-full.px-4.flex-shrink-0:is(.pb-2, .pb-4),
  div.shrink-0.p-2 {
    padding-bottom: max(1.25rem, env(safe-area-inset-bottom, 16px)) !important;
  }
}

/* Mobile phones only (<= 768px touch devices): Apply compact mobile styles.
   Desktop browsers (pointer: fine) and split-screen desktop windows are completely isolated! */
@media (pointer: coarse) and (max-width: 768px) {
  /* Compact padding for mobile composer wrapper (prevents floating gap on mobile phones) */
  div.relative.w-full.px-4.flex-shrink-0:is(.pb-2, .pb-4),
  div.shrink-0.p-2 {
    padding: 0.25rem 0.5rem 0 0.5rem !important;
    padding-bottom: max(0.25rem, env(safe-area-inset-bottom, 6px)) !important;
  }

  /* Mobile conversation row: render [ Title | ... | Time ] side-by-side without background gradient */
  div[data-testid^="conversation-row-"] div.absolute.top-0 {
    position: relative !important;
    top: auto !important;
    bottom: auto !important;
    right: auto !important;
    padding-left: 0 !important;
    opacity: 1 !important;
    background: transparent !important;
    margin-right: 0.25rem !important;
  }
  div[data-testid^="conversation-row-"] div.absolute.top-0::before {
    display: none !important;
  }
  div[data-testid^="conversation-row-"] [data-testid="conversation-kebab"] {
    display: inline-flex !important;
    opacity: 1 !important;
  }
  div[data-testid^="conversation-row-"] [data-testid="conversation-pin-button"],
  div[data-testid^="conversation-row-"] [data-testid="conversation-archive-button"],
  div[data-testid^="conversation-row-"] [data-testid="conversation-restore-button"],
  div[data-testid^="conversation-row-"] [data-testid="conversation-delete-button"] {
    display: none !important;
  }

  /* Mobile pane titlebar (second header line with breadcrumbs):
     Eliminate desktop window-control dummy spacers and align breadcrumbs seamlessly */
  div.group\/pane > div.select-none:first-child div[aria-hidden="true"].shrink-0,
  div.group\/pane > div.select-none:first-child div.shrink-0:empty {
    display: none !important;
    width: 0 !important;
  }
  div.group\/pane > div.select-none:first-child > div.flex.items-center.justify-end {
    padding-right: 0.5rem !important;
  }
  div.group\/pane > div.select-none:first-child > div.flex.items-center.gap-1.min-w-0 {
    padding-left: 0.75rem !important;
  }
}
</style>`

const keyboardDetect = `<script id="agy-keyboard-detect">
(function () {
  // Only activate on touch devices (pointer: coarse) with virtual keyboards.
  // Desktop browsers with fine pointer (mouse) are completely ignored.
  var isTouch = window.matchMedia && window.matchMedia("(pointer:coarse)").matches;
  if (!isTouch) return;

  var vv = window.visualViewport;
  if (!vv) return;

  var cachedScroller = null;
  var wasNearBottom = true;
  var shouldScrollOnOpen = false;

  var hasActiveQuestion = false;
  var finishingQuestionUntil = 0;

  var QUESTION_SELECTOR = [
    '[data-testid="ask-question-header-text"]',
    '[data-testid="ask-question-writein"]',
    'input[name^="ask-question-"]',
    '[data-testid="interaction-continue-button"]',
    '[data-testid="interaction-skip-button"]',
    '[data-testid="ask-question-dismiss-button"]',
    '[data-testid="declared-permissions-modal"]',
    '[data-testid="declared-permissions-confirm"]'
  ].join(",");

  function checkQuestionActive() {
    if (performance.now() < finishingQuestionUntil) {
      return false;
    }
    return !!document.querySelector(QUESTION_SELECTOR);
  }

  function updateQuestionState() {
    var has = checkQuestionActive();
    if (has === hasActiveQuestion) return;
    hasActiveQuestion = has;
    if (has) {
      checkNearBottom();
      document.body.classList.add("agy-has-question");
      document.documentElement.classList.add("agy-has-question");
      document.documentElement.style.removeProperty("--agy-bottom");
      document.body.classList.remove("agy-kb-open");
      applied = 0;
      goal = 0;
      settled = true;
      holdUntil = 0;
      if (raf) {
        cancelAnimationFrame(raf);
        raf = 0;
      }
      if (wasNearBottom) {
        var fixBottom = function () {
          var sc = chatScroller();
          if (sc) sc.scrollTop = sc.scrollHeight;
        };
        requestAnimationFrame(fixBottom);
        setTimeout(fixBottom, 50);
        setTimeout(fixBottom, 150);
      }
    } else {
      var act = document.activeElement;
      if (act && (isTextInput(act) || (act.closest && act.closest(QUESTION_SELECTOR)))) {
        if (act.blur) act.blur();
      }
      document.body.classList.remove("agy-has-question");
      document.documentElement.classList.remove("agy-has-question");
      window.scrollTo(0, 0);
      unpan();
      track(500);
    }
  }

  function finishQuestion() {
    finishingQuestionUntil = performance.now() + 800;
    var active = document.activeElement;
    if (active && (isTextInput(active) || (active.closest && active.closest(QUESTION_SELECTOR)))) {
      if (active.blur) active.blur();
    }
    suppressComposerUntil = performance.now() + 600;
    if (hasActiveQuestion) {
      hasActiveQuestion = false;
      document.body.classList.remove("agy-has-question");
      document.documentElement.classList.remove("agy-has-question");
      window.scrollTo(0, 0);
      unpan();
      track(500);
    }
  }

  // The messages live in a scroller nested inside the conversation view.
  // Direct selector and reference caching avoid forced layout thrashing (reflow)
  // and prevent false-positive matching of inner scrollable code blocks (<pre><code>).
  function chatScroller() {
    if (cachedScroller && cachedScroller.isConnected && cachedScroller.closest('[data-testid="conversation-view"]')) {
      return cachedScroller;
    }
    var root = document.querySelector('[data-testid="conversation-view"]');
    if (!root) return null;

    // Direct target: the main autoscroll viewport in Antigravity
    var el = root.querySelector('[data-testid="autoscroll-viewport"]') ||
             root.querySelector("div.h-full.overflow-y-auto, div.overflow-y-auto.min-h-0");
    if (el) {
      cachedScroller = el;
      return el;
    }

    // Direct children fallback (avoids deep nested code blocks or pre tags)
    var children = root.children;
    for (var i = 0; i < children.length; i++) {
      var child = children[i];
      if (child.classList.contains("overflow-y-auto")) {
        cachedScroller = child;
        return child;
      }
    }

    return null;
  }

  function checkNearBottom() {
    var el = chatScroller();
    if (!el) {
      wasNearBottom = true;
      return;
    }
    var dist = el.scrollHeight - el.clientHeight - el.scrollTop;
    wasNearBottom = dist <= 80;
  }

  function scrollChatToBottom() {
    if (!wasNearBottom) return;
    var el = chatScroller();
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }

  function isTextInput(el) {
    if (!el) return false;
    if (el.tagName === "TEXTAREA" || el.isContentEditable) return true;
    if (el.tagName === "INPUT") {
      var type = (el.type || "").toLowerCase();
      return !/^(radio|checkbox|button|submit|reset|file|range|color|hidden|image)$/.test(type);
    }
    return false;
  }

  // html is position:fixed, so clientHeight is the layout viewport and does not
  // move with Safari's toolbar the way innerHeight can.
  function base() {
    return document.documentElement.clientHeight || window.innerHeight;
  }

  var lastLayoutWidth = window.innerWidth;
  var portraitMode = window.innerHeight >= window.innerWidth;
  var maxBaseP = 0;
  var maxBaseL = 0;
  var layoutKbOpen = false;
  var usesLayoutResize = false;
  var lastFocusOutAt = -Infinity;

  function syncOrientation() {
    if (Math.abs(window.innerWidth - lastLayoutWidth) > 40) {
      lastLayoutWidth = window.innerWidth;
      portraitMode = window.innerHeight >= window.innerWidth;
    }
  }

  function isPortrait() {
    syncOrientation();
    return portraitMode;
  }

  function recordBase(b) {
    if (b <= 0) return 0;
    if (isPortrait()) {
      if (b > maxBaseP) maxBaseP = b;
      return maxBaseP;
    }
    if (b > maxBaseL) maxBaseL = b;
    return maxBaseL;
  }
  recordBase(base());

  function isMobileDevice() {
    return isTouch && Math.min(window.innerWidth, maxBaseP || window.innerHeight) <= 768;
  }

  function getStorageKey() {
    return isPortrait() ? "agy-kb-p" : "agy-kb-l";
  }

  function loadPredicted() {
    var v = 0;
    try {
      v = parseInt(localStorage.getItem(getStorageKey()), 10) || 0;
    } catch (e) {}
    if (v < 100) {
      if (isMobileDevice() && isPortrait()) {
        return Math.min(340, Math.max(260, Math.round(base() * 0.38)));
      }
      return 0;
    }
    return v;
  }

  function savePredicted(val) {
    try {
      localStorage.setItem(getStorageKey(), String(val));
    } catch (e) {}
  }

  // Safari reveals the focused composer by panning the layout viewport, and it
  // reports that pan as a document scroll even here, where the document is fixed
  // and has nothing to scroll.
  // In Question Mode, unpan is bypassed so native layout panning and swipe-down scrolling
  // work exactly like original Google Antigravity.
  function isZoomed() {
    return vv && (vv.scale || 1) > 1.01;
  }

  function unpan() {
    if (hasActiveQuestion) return;
    var de = document.documentElement;
    var sy = window.scrollY || window.pageYOffset || 0;
    var ot = (vv && !isZoomed()) ? (vv.offsetTop || 0) : 0;
    if (sy !== 0 || ot > 0) window.scrollTo(0, 0);
    if (de && de.scrollTop !== 0) de.scrollTop = 0;
    if (document.body && document.body.scrollTop !== 0) document.body.scrollTop = 0;
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
  var predicted = loadPredicted();
  var holdUntil = 0;

  var applied = 0;
  var goal = 0;
  var from = 0;
  var moveAt = 0;
  var settled = true;
  var raf = 0;
  var deadline = 0;

  function write(kb) {
    if (hasActiveQuestion) return;
    if (Math.abs(kb - applied) < 1) {
      if (kb === 0 && !layoutKbOpen && document.body.classList.contains("agy-kb-open")) {
        document.body.classList.remove("agy-kb-open");
      }
      return;
    }

    var opening = applied === 0 && kb > 0 && !layoutKbOpen;
    var closing = applied > 0 && kb === 0 && !layoutKbOpen;
    applied = kb;
    if (kb > 0) {
      document.documentElement.style.setProperty("--agy-bottom", kb + "px");
      document.body.classList.add("agy-kb-open");
    } else {
      document.documentElement.style.removeProperty("--agy-bottom");
      if (!layoutKbOpen) {
        document.body.classList.remove("agy-kb-open");
      }
    }
    if (opening) {
      shouldScrollOnOpen = wasNearBottom;
      if (wasNearBottom) {
        scrollChatToBottom();
      }
    } else if (closing) {
      shouldScrollOnOpen = false;
    }
  }

  function frame() {
    if (hasActiveQuestion) {
      raf = 0;
      return;
    }
    unpan();

    var curBase = base();
    var activeEl = document.activeElement;
    var activeInput = Boolean(activeEl && isTextInput(activeEl));
    var rawTarget = isZoomed() ? 0 : Math.max(0, Math.round(curBase - vv.height));
    var target = rawTarget;
    // Ignore small changes (< 100px) such as iPad Bluetooth shortcut bar (54px)
    if (target < 100) target = 0;

    // Recalibrate baseline downward when the browser window/split-screen/toolbar shrinks
    // while no text input is active and any prior keyboard close animation has settled.
    if (!activeInput && !layoutKbOpen && applied === 0 && target === 0 && !isZoomed() &&
        curBase > 0 && Math.abs(curBase - vv.height) < 24 && (performance.now() - lastFocusOutAt) > 450) {
      if (isPortrait()) {
        maxBaseP = curBase;
      } else {
        maxBaseL = curBase;
      }
    }

    var refBase = recordBase(curBase);
    var layoutShrink = (!isZoomed() && refBase > 0) ? Math.max(0, Math.round(refBase - curBase)) : 0;
    var isLayoutResized = target === 0 && activeInput && layoutShrink >= 100 && Math.abs(curBase - vv.height) < 24;

    if (isLayoutResized) {
      holdUntil = 0;
      usesLayoutResize = true;
      if (!layoutKbOpen) {
        layoutKbOpen = true;
        document.body.classList.add("agy-kb-open");
        if (wasNearBottom) {
          scrollChatToBottom();
        }
      }
    } else if (layoutKbOpen) {
      layoutKbOpen = false;
      shouldScrollOnOpen = false;
      if (applied === 0) {
        document.body.classList.remove("agy-kb-open");
      }
    }

    if (target > 0) {
      usesLayoutResize = false;
      holdUntil = 0;
      if (target !== predicted && Math.round(vv.offsetTop || 0) === 0) {
        predicted = target;
        savePredicted(target);
      }
    } else if (!isLayoutResized && performance.now() < holdUntil) {
      // Hold the predicted shrink for at most 500ms after focusin while Safari
      // prepares to animate the visual viewport. Hardware keyboards will naturally
      // expire after holdUntil and restore the full viewport height.
      target = predicted;
    }

    if (target !== goal) {
      goal = target;
      from = applied;
      moveAt = performance.now();
      settled = false;
    }

    if (goal === 0) {
      // Closing: delegate to immediate write and CSS hardware-accelerated transition.
      // Eliminates per-frame JS reflow fighting iOS native spring animation.
      write(0);
      settled = true;
    } else if (applied > 0) {
      // While keyboard is already active, follow the user's touch gesture immediately
      // without restarting the cubic animation on every frame.
      write(goal);
    } else {
      var p = Math.min(1, (performance.now() - moveAt) / OPEN_MS);
      var curKb = Math.round(from + (goal - from) * (1 - Math.pow(1 - p, 3)));
      write(curKb);
    }

    if (!settled && applied === goal) {
      settled = true;
      if (shouldScrollOnOpen && goal > 0) {
        scrollChatToBottom();
        shouldScrollOnOpen = false;
      }
    }

    if (performance.now() < deadline) {
      raf = requestAnimationFrame(frame);
      return;
    }
    raf = 0;
    write(goal);
    if (shouldScrollOnOpen && goal > 0) {
      scrollChatToBottom();
      shouldScrollOnOpen = false;
    }
  }

  function track(ms) {
    if (hasActiveQuestion) return;
    unpan();
    var until = performance.now() + ms;
    if (until > deadline) deadline = until;
    if (!raf) raf = requestAnimationFrame(frame);
  }

  vv.addEventListener("resize", function () {
    updateQuestionState();
    if (hasActiveQuestion) return;
    predicted = loadPredicted();
    track(700);
  });
  window.addEventListener("resize", function () {
    updateQuestionState();
    if (hasActiveQuestion) return;
    track(500);
  }, { passive: true });
  vv.addEventListener("scroll", function () {
    unpan();
  }, { passive: true });

  // Use capture phase to intercept scroll events inside the message scroller div
  document.addEventListener("scroll", function (e) {
    unpan();
    var t = e.target;
    if (!t || t === document || (t.closest && t.closest('[data-testid="conversation-view"]'))) {
      checkNearBottom();
    }
  }, { capture: true, passive: true });

  // Track touch coordinates and tap state for swipe-down dismiss and keyboard prediction
  var touchStartY = 0;
  var touchStartX = 0;
  var suppressComposerUntil = 0;
  var lastComposerTouchTime = -Infinity;
  var lastTouchTarget = null;
  var lastTouchAt = -Infinity;
  var touchMoved = false;
  var TAP_SLOP_PX = 10;
  // Buttons, links, popup triggers, focusable chips, and opted-out wrappers
  // inside agent-input-box do not open the keyboard when tapped, even when the
  // bundle restores focus to the editor afterwards.
  var COMPOSER_CONTROL_SELECTOR = 'button, [role="button"], a, input, select, textarea, label, summary, [aria-haspopup], [tabindex]:not([tabindex^="-"]), .no-focus-agent-input, [data-overlay-scrollbar-track]';

  function touchElement(t) {
    if (!t) return null;
    return t.closest ? t : (t.parentElement || null);
  }

  function clearLastTouch() {
    lastTouchTarget = null;
    lastTouchAt = -Infinity;
  }

  function isComposerTapTarget(el) {
    if (!el || !el.closest) return false;
    var box = el.closest('[data-testid="agent-input-box"]');
    if (!box) return false;
    var ctrl = el.closest(COMPOSER_CONTROL_SELECTOR);
    if (!ctrl || !box.contains(ctrl)) return true;
    return isTextInput(ctrl) && (ctrl === el || !!(ctrl.contains && ctrl.contains(el)));
  }

  // justTouched reports whether the user's last tap, begun or lifted within the
  // last second without dragging, landed on the field now taking focus (or on
  // non-control padding of its composer box), or whether __agyFocusComposer
  // just requested focus from a New Conversation tap.
  function justTouched(field) {
    if (!field) return false;
    var now = performance.now();
    var box = field.closest && field.closest('[data-testid="agent-input-box"]');
    var isComposer = Boolean(box || (field.getAttribute && field.getAttribute("data-lexical-editor") === "true"));
    var reqAt = window.__agyComposerFocusAt;
    if (isComposer && typeof reqAt === "number" && now >= reqAt && now - reqAt <= 250) {
      return true;
    }
    if (!lastTouchTarget || now < lastTouchAt || now - lastTouchAt > 1000) return false;
    var area = box || field;
    if (area !== lastTouchTarget && !(area.contains && area.contains(lastTouchTarget))) return false;
    var ctrl = lastTouchTarget.closest && lastTouchTarget.closest(COMPOSER_CONTROL_SELECTOR);
    if (ctrl && (area === ctrl || (area.contains && area.contains(ctrl))) &&
        ctrl !== field && !(ctrl.contains && ctrl.contains(field))) {
      return false;
    }
    return true;
  }

  document.addEventListener("touchstart", function (e) {
    window.__agyComposerFocusAt = -Infinity;
    var el = touchElement(e.target);
    // Cancel pending auto-scroll if user touches the conversation view during open
    if (el && el.closest && el.closest('[data-testid="conversation-view"]')) {
      shouldScrollOnOpen = false;
    }
    if (e.touches && e.touches.length > 1) {
      touchMoved = true;
      clearLastTouch();
      return;
    }
    touchMoved = false;
    if (e.touches && e.touches.length === 1) {
      touchStartY = e.touches[0].clientY;
      touchStartX = e.touches[0].clientX;
    } else {
      touchStartY = 0;
      touchStartX = 0;
    }
    lastTouchTarget = el;
    lastTouchAt = performance.now();
    if (isComposerTapTarget(el)) {
      lastComposerTouchTime = lastTouchAt;
    }
  }, { capture: true, passive: true });

  document.addEventListener("touchmove", function (e) {
    if (!e.touches) return;
    if (e.touches.length !== 1 ||
        Math.abs(e.touches[0].clientX - touchStartX) > TAP_SLOP_PX ||
        Math.abs(e.touches[0].clientY - touchStartY) > TAP_SLOP_PX) {
      touchMoved = true;
      clearLastTouch();
    }
  }, { capture: true, passive: true });

  // A long press focuses the field when it is lifted, not when it began.
  // Drags and canceled touches do not focus anything.
  document.addEventListener("touchend", function (e) {
    if (touchMoved) return;
    if (e.touches && e.touches.length > 0) return;
    var el = touchElement(e.target);
    if (el) {
      lastTouchTarget = el;
      lastTouchAt = performance.now();
      if (isComposerTapTarget(el)) {
        lastComposerTouchTime = lastTouchAt;
      }
    }
  }, { capture: true, passive: true });

  document.addEventListener("touchcancel", function () {
    touchMoved = true;
    clearLastTouch();
    window.__agyComposerFocusAt = -Infinity;
  }, { capture: true, passive: true });

  window.addEventListener("touchmove", function (e) {
    if (hasActiveQuestion) return;
    if (applied > 0) {
      unpan();
    }
    if (!e.touches || e.touches.length !== 1) return;
    var curY = e.touches[0].clientY;
    var curX = e.touches[0].clientX;
    var deltaY = curY - touchStartY;
    var deltaX = curX - touchStartX;

    // Swipe down gesture to dismiss virtual keyboard in regular chat mode
    if ((applied > 0 || document.body.classList.contains("agy-kb-open")) && deltaY > 45 && Math.abs(deltaY) > Math.abs(deltaX) * 1.5) {
      var active = document.activeElement;
      if (active && isTextInput(active) && e.target !== active && (!active.contains || !active.contains(e.target))) {
        active.blur();
      }
    }
  }, { passive: true });

  window.addEventListener("touchend", function () {
    if (hasActiveQuestion) return;
    if (applied > 0) {
      unpan();
    }
  }, { passive: true });

  window.addEventListener("orientationchange", function () {
    updateQuestionState();
    if (hasActiveQuestion) return;
    predicted = loadPredicted();
    track(500);
  });

  // When an objective option (radio) in ask_question is selected on mobile devices:
  // Suppress spurious auto-focus jumping into the main agent composer after modal completion
  document.addEventListener("change", function (e) {
    if (!isMobileDevice()) return;
    var t = e.target;
    if (t && t.type === "radio" && typeof t.name === "string" && t.name.indexOf("ask-question-") === 0) {
      if (t.value !== "__write_in__") {
        suppressComposerUntil = performance.now() + 450;
        if (t.blur) t.blur();
      }
    }
  }, true);

  // If submit / continue / skip button is clicked in ask_question or permission modal:
  document.addEventListener("click", function (e) {
    var t = e.target;
    if (!t) return;
    var btn = t.closest && t.closest(
      '[data-testid="interaction-continue-button"],' +
      '[data-testid="interaction-skip-button"],' +
      '[data-testid="ask-question-dismiss-button"],' +
      '[data-testid="declared-permissions-confirm"]'
    );
    if (btn) {
      var tid = btn.getAttribute("data-testid");
      if (tid === "ask-question-dismiss-button" || tid === "declared-permissions-confirm") {
        finishQuestion();
        return;
      }
      // In multi-step prompts, continue/skip may advance to the next step rather than closing.
      // Arm composer focus suppression immediately and finish only once the modal unmounts,
      // avoiding reliance on localized button labels.
      suppressComposerUntil = performance.now() + 600;
      var checkClosed = function () {
        if (!document.querySelector(QUESTION_SELECTOR)) {
          finishQuestion();
        }
      };
      setTimeout(checkClosed, 0);
      setTimeout(checkClosed, 60);
    }
  }, true);

  window.addEventListener("focusin", function (e) {
    updateQuestionState();
    if (hasActiveQuestion) {
      return;
    }
    var t = e.target;

    if (t && t.tagName === "INPUT" && (t.type === "radio" || t.type === "checkbox")) {
      unpan();
    }

    if (t && isTextInput(t)) {
      // On mobile devices, if the main agent composer is auto-focused immediately after question submission,
      // blur it so the virtual keyboard does not pop up unexpectedly.
      // Deliberate user touch on the composer and inputs outside the composer are never blocked!
      if (isMobileDevice() && performance.now() < suppressComposerUntil) {
        var isMainComposer = t.closest && t.closest('[data-testid="agent-input-box"]');
        if (isMainComposer) {
          var now = performance.now();
          var focusReqAt = window.__agyComposerFocusAt;
          var isDirectUserTap = (now - lastComposerTouchTime) < 500 ||
            (typeof focusReqAt === "number" && now >= focusReqAt && now - focusReqAt <= 250);
          if (!isDirectUserTap) {
            if (t.blur) t.blur();
            return;
          }
        }
      }

      checkNearBottom();
      predicted = loadPredicted();
      // Only a field the user just touched brings the keyboard up. Focus handed
      // back by script does not: the model picker's bottom sheet, for one,
      // returns focus to the composer when its closing animation ends, half a
      // second after the tap that closed it. Shrinking for a keyboard that
      // never arrives makes the chat jump up and settle back. If the keyboard
      // does open, the viewport resize still moves the shell, just without
      // the speculative jump.
      // Only apply speculative shrink on mobile phones in portrait mode.
      // Tablets (iPad) and hardware keyboard users must NOT speculatively shrink before visualViewport reports.
      if (!usesLayoutResize && predicted >= 100 && applied === 0 && isMobileDevice() && isPortrait() && justTouched(t)) {
        clearLastTouch();
        window.__agyComposerFocusAt = -Infinity;
        holdUntil = performance.now() + 500;
        goal = from = predicted;
        moveAt = performance.now();
        write(predicted);
      }
      track(900);
    }
  });

  window.addEventListener("focusout", function () {
    lastFocusOutAt = performance.now();
    if (hasActiveQuestion) return;
    track(500);
  });

  // Track IME composition completion timestamp for WebKit Enter race condition
  document.addEventListener("compositionend", function () {
    window.__agyLastCompEnd = performance.now();
  }, true);

  // Mobile Conversation Top-Scroll Guard & Anchoring
  // Preserves scroll position when prepending older messages to prevent infinite fetch storms.
  var lastScrollHeight = 0;
  var lastScrollTop = 0;
  var currentConvoUrl = window.location.pathname;
  var guardedScroller = null;
  var convoNavigatedAt = performance.now();
  var userScrolledNearTop = false;
  var PREPEND_REAPPLY_MS = 600;

  function handleScrollerScroll() {
    var sc = chatScroller();
    if (!sc) return;
    lastScrollTop = sc.scrollTop;
    lastScrollHeight = sc.scrollHeight;
    // Track if user actively scrolled near top (< 120px) after initial mount
    // Only the reader's own scrolling counts, including momentum for a few seconds after the finger lifts. A position
    // the app or the browser set does not, nor one above the top. Without the scroll tracker there is no input to go
    // by, so any position near the top counts, as before.
    var lastInput = window.__agyLastScrollInputAt;
    if (sc.scrollTop >= 0 && sc.scrollTop <= 120 && performance.now() - convoNavigatedAt >= 2500 &&
        (lastInput === undefined || performance.now() - lastInput < 4000)) {
      userScrolledNearTop = true;
    } else if (sc.scrollTop > 150) {
      userScrolledNearTop = false;
    }
  }

  function handleScrollerMutation() {
    var sc = chatScroller();
    if (!sc) return;

    if (window.location.pathname !== currentConvoUrl) {
      currentConvoUrl = window.location.pathname;
      convoNavigatedAt = performance.now();
      userScrolledNearTop = false;
      lastScrollHeight = sc.scrollHeight;
      lastScrollTop = sc.scrollTop;
      return;
    }

    // Guard: During initial conversation load (first 2.5s), messages are being mounted.
    // Never misinterpret initial message mount as a user-initiated prepend!
    if (performance.now() - convoNavigatedAt < 2500) {
      lastScrollHeight = sc.scrollHeight;
      lastScrollTop = sc.scrollTop;
      return;
    }

    var curHeight = sc.scrollHeight;
    var curTop = sc.scrollTop;

    // Detect prepend: content expanded while user was actively scrolled near top
    if (userScrolledNearTop && lastScrollHeight > 0 && curHeight > lastScrollHeight) {
      var delta = curHeight - lastScrollHeight;
      if (lastScrollTop <= 100 && delta >= 30) {
        // Prepend detected: anchor scroll position by adding delta
        var targetTop = lastScrollTop + delta;
        sc.scrollTop = targetTop;
        var prependedAt = performance.now();
        // Rendering a long conversation can hold these timers back for seconds. By then the app may have dropped rows
        // above the reader and lowered the position itself, or the reader may have moved, so a late one does nothing,
        // and neither does one after the conversation lost more than half of what was added. A late one that would
        // still have been right is given up too, because nothing here can tell it from a wrong one.
        var stale = function () {
          return performance.now() - prependedAt > PREPEND_REAPPLY_MS || sc.scrollHeight < curHeight - delta / 2;
        };
        requestAnimationFrame(function () {
          if (!stale()) sc.scrollTop = targetTop;
        });
        var reapply = function () {
          if (!stale() && sc.scrollTop < delta / 2) sc.scrollTop = targetTop;
        };
        setTimeout(reapply, 50);
        setTimeout(reapply, 150);
        setTimeout(reapply, 300);
      }
    }

    lastScrollHeight = curHeight;
    lastScrollTop = curTop;
  }

  function attachScrollerGuard() {
    var sc = chatScroller();
    if (!sc) return;
    if (guardedScroller !== sc) {
      if (guardedScroller) {
        guardedScroller.removeEventListener("scroll", handleScrollerScroll);
      }
      guardedScroller = sc;
      lastScrollHeight = sc.scrollHeight;
      lastScrollTop = sc.scrollTop;
      sc.addEventListener("scroll", handleScrollerScroll, { passive: true });
    }
  }

  // Observe DOM for question modal or interaction card appearance and scroller updates
  var lastQuestionModalSeen = false;
  var modalObserver = new MutationObserver(function () {
    updateQuestionState();
    attachScrollerGuard();
    handleScrollerMutation();
    var hasModal = hasActiveQuestion;
    if (hasModal && !lastQuestionModalSeen) {
      lastQuestionModalSeen = true;
    } else if (!hasModal && lastQuestionModalSeen) {
      lastQuestionModalSeen = false;
    }
  });

  if (document.body) {
    modalObserver.observe(document.body, { childList: true, subtree: true });
    updateQuestionState();
    attachScrollerGuard();
  } else {
    document.addEventListener("DOMContentLoaded", function () {
      if (document.body) {
        modalObserver.observe(document.body, { childList: true, subtree: true });
        updateQuestionState();
        attachScrollerGuard();
      }
    }, { once: true });
  }
})();
</script>`

const scrollInputTracker = `<script id="agy-scroll-input">
(function () {
  var VIEWPORT = '[data-testid="autoscroll-viewport"]';
  var SCROLL_TARGETS = VIEWPORT + ',[data-overlay-scrollbar-track],[data-testid="overlay-scrollbar"],' +
    '[data-testid="vertical-scrollbar"],[data-testid="vertical-scrollbar-thumb"]';
  var FIND_BAR = 'input[aria-label="Find"]';
  // The conversation takes these wherever focus is, including the prompt.
  var NAV_KEYS = { PageUp: 1, PageDown: 1, Home: 1, End: 1 };
  // In a text box these type or move the caret instead.
  var PLAIN_KEYS = { ArrowUp: 1, ArrowDown: 1, " ": 1 };
  // Tall for reasons other than how much is in them. Rows are given a height relative to the conversation's own in cqh.
  var OWN_HEIGHT = 'img,video,canvas,iframe,[style*="cqh"]';
  var collapses = new WeakMap();
  var lastSeen = new WeakMap();
  var holds = new WeakMap();
  var FLUNG_ABOVE_TOP = -1000;
  var FLING_HOLD_MS = 1000;
  var NEAR_BOTTOM = 100;
  var JUMP_MIN = 5000;
  var JUMP_SHARE = 0.3;
  var SCRIPT_GRACE_MS = 150;
  var COLLAPSE_MS = 2000;
  // A fling keeps going for a while after the finger lifts, and a long one has been traced at 3.6 s with its bounce.
  // Ending one that has already stopped changes nothing, so this errs long.
  var MOMENTUM_MS = 8000;
  // Further than this, a move is rows above coming or going, not a nudge or a step of a fling, even one that several
  // busy frames ran together. A reply streaming in below changes the height by less.
  var ROWS_MOVE_MIN = 2000;
  // How far a fling can carry the position between two looks at it.
  var WINDOW_SLACK = 300;
  // The longest flings hold back rows the bundle drops above the reader, counting only time with no finger down. Rows
  // keep loading above meanwhile, and nothing else bounds how many are mounted.
  var TRIM_WAIT_MAX_MS = 3000;
  var TRIM_ASK_GAP_MS = 5000;
  // A fling sends a scroll event every frame until it has stopped. A main thread busy rendering sends none while the
  // fling goes on, so still also needs frames drawn with no event.
  var STILL_MS = 300;
  var STILL_FRAMES = 2;
  var movedAt = -Infinity;
  var framesSinceMove = STILL_FRAMES;
  var countingFrames = false;
  // A finger that sent nothing for this long is taken as lifted, whatever event about it went missing.
  var TOUCH_STALE_MS = 10000;
  var touchAt = -Infinity;
  var trimWait = { waiting: false, askedAt: -Infinity, askedDown: false, flingMs: 0 };
  var growWait = { waiting: false, askedAt: -Infinity, askedDown: false, flingMs: 0 };
  // The conversation that moved last. The bundle asks about rows without saying for which conversation, and with two
  // on screen this is the one taken: a wrong guess lets rows in sooner or later than it might, and they are still put
  // in the room of the conversation they arrive in. Held weakly, so one that has left the page can go.
  var movedRef = null;
  // Rows the bundle put back in place are checked for this many frames more, and a fling is ended for a move this far.
  var KEEP_ROWS_FRAMES = 20;
  var KEEP_ROWS_STOP_MIN = 150;
  // Room kept above the first row on iOS, which rows arriving there take their height from: see takeRoom. It is filled
  // back to ROOM at rest once under ROOM_LOW, or cut back once rows dropped above the reader have left more than
  // ROOM_HIGH. It has to last from one rest to the next, and a reader flicking over and over is never at rest: one
  // went up 40,000 px in 16 s, which was all the room there was at first. With ROOM_NEED left, rows are let in
  // during a fling: the tallest turn seen was 6,300 px.
  var ROOM = 200000;
  var ROOM_LOW = 120000;
  var ROOM_HIGH = 400000;
  var ROOM_NEED = 8000;
  var REST_MS = 400;
  var ROOM_CHECK_FRAMES = 8;
  var ROOM_FAILURES_MAX = 2;
  // A fling has been seen to cover 343 px in a 13 ms frame. Twice that pace is no fling.
  var THROW_PX_PER_MS = 50;
  // Drawing rows holds the page for 0.1 to 0.5 s at a time, which a reader in the middle of a flick or of typing feels,
  // so rows further than GROW_NEAR above the screen are only let in while the conversation is idle, and no sooner than
  // GROW_PACE_MS after the last ones. Nearer than that a flick is there in a quarter of a second, and they are let in
  // whatever the reader is doing.
  var GROW_NEAR = 2000;
  var GROW_PACE_MS = 350;
  // A reader who gets into the room ahead of rows already on their way is left there this long and this far: the
  // rows fill the room from where it ends, so the reader is among them when they arrive, with the fling still going.
  var GRACE_MS = 500;
  var GRACE_PX = 1500;
  // Rows let in are expected for this long. Until they arrive the browser's anchoring stays off, at rest or not.
  var ROWS_DUE_MS = 5000;
  var keptAt = -Infinity;
  var growLetInAt = -Infinity;
  var rowsDueSince = -Infinity;
  var OLDER_ROWS = 'button[aria-label*="older messages"]';
  var rooms = new WeakMap();
  // How far each conversation has been moved by anything here rather than by its reader, summed, so that every row
  // being put back counts a move made for another one.
  var shifts = new WeakMap();
  var roomFailures = new WeakMap();
  // Conversations whose markup did not lay the room out when it was made: see resizeRoom.
  var roomless = new WeakSet();
  // Conversations that are past being given room as they are drawn: a finger or a wheel has been on them, they have
  // come to rest, or a write for their room was taken back. And the ones about to be given it. See roomAsDrawn.
  var settled = new WeakSet();
  var drawing = new WeakSet();
  // The write that last filled or cut each room, for as long as it is watched for being taken back.
  var roomWrites = new WeakMap();
  // Whether the bundle hands the move that keeps its rows in place to __agyKeepRows. One that makes the move itself,
  // as every bundle before 2.32 does, would move the position for rows the room had already made up for: it gets no
  // room, and its rows are not held back. See rowsHandedOver.
  var keepsRows = false;
  var restTimer = 0;
  var resting = new Set();
  var unanchored = new WeakSet();
  // Whether the browser keeps rows in place itself where its anchoring is left on.
  var ANCHORS = typeof CSS !== "undefined" && Boolean(CSS.supports) && CSS.supports("overflow-anchor", "none");
  // What is followed of each conversation while the browser's anchoring is off, one thing at a time: a row, where it
  // is kept on the screen, and the position when that was settled. See keepUp.
  var followers = new WeakMap();
  // How the bundle finds the rows it may keep in place, which it says at every drawing, and the ones last found.
  var rowsFrom = new WeakMap();
  var rowLists = new WeakMap();
  var ROWS_LOOK_MS = 250;
  var removedHeight = new Map();
  var frameRequested = false;
  var pressed = false;
  var touching = false;
  var stopping = new WeakSet();
  window.__agyLastScrollInputAt = -Infinity;

  function mark() { window.__agyLastScrollInputAt = performance.now(); }
  function onConversation(e) { return e.target && e.target.closest && e.target.closest(SCROLL_TARGETS); }
  var opts = { capture: true, passive: true };
  // A finger or a wheel on a conversation is what a fling comes from, and the conversation it was on is noted: see
  // roomAsDrawn.
  ["wheel", "touchstart", "touchmove"].forEach(function (name) {
    window.addEventListener(name, function (e) {
      if (!onConversation(e)) return;
      mark();
      var on = e.target.closest(VIEWPORT);
      if (on) settled.add(on);
    }, opts);
  });
  // A touch ends on the element it started on. One removed while the finger was down is no longer under the window,
  // so its end is listened for on the element too.
  function touchState(e) {
    touching = Boolean(e.touches && e.touches.length > 0);
    touchAt = performance.now();
  }
  function fingerDown(now) { return touching && now - touchAt < TOUCH_STALE_MS; }
  function touchMoved() { touchAt = performance.now(); }
  window.addEventListener("touchmove", touchMoved, opts);
  window.addEventListener("touchstart", function (e) {
    touchState(e);
    var t = e.target;
    if (t && t.addEventListener) {
      t.addEventListener("touchmove", touchMoved, { passive: true });
      t.addEventListener("touchend", touchState, { once: true, passive: true });
      t.addEventListener("touchcancel", touchState, { once: true, passive: true });
    }
  }, opts);
  ["touchend", "touchcancel"].forEach(function (name) { window.addEventListener(name, touchState, opts); });
  // A page hidden with a finger down may never see that touch end.
  ["pagehide", "visibilitychange"].forEach(function (name) {
    window.addEventListener(name, function () { if (name === "pagehide" || document.hidden) touching = false; }, opts);
  });
  window.addEventListener("pointerdown", function (e) {
    if (onConversation(e)) { pressed = true; mark(); }
  }, opts);
  ["pointerup", "pointercancel", "contextmenu", "dragstart", "blur"].forEach(function (name) {
    // Capturing on the window also sees every element losing focus, which a press often causes.
    window.addEventListener(name, function (e) { if (name !== "blur" || e.target === window) pressed = false; }, opts);
  });
  window.addEventListener("pointermove", function () { if (pressed) mark(); }, opts);
  window.addEventListener("keydown", function (e) {
    var t = e.target;
    var editing = t && (t.isContentEditable || t.tagName === "INPUT" || t.tagName === "TEXTAREA");
    if (NAV_KEYS[e.key] || (PLAIN_KEYS[e.key] && !editing)) mark();
  }, opts);
  // Moving focus scrolls the focused row into view.
  window.addEventListener("focusin", function (e) { if (onConversation(e)) mark(); }, opts);

  // A generous guess at how tall removed content was. A detached node has no layout left to ask, and the guess only
  // has to tell a status icon from whole rows: at most about 630 px for the status changes seen during a reply, and
  // over 20,000 px for rows the window removed.
  function guessHeight(node) {
    if (node.nodeType !== 1) return node.nodeType === 3 ? node.textContent.length * 2 : 0;
    var height = (1 + node.getElementsByTagName("*").length) * 48 + node.textContent.length * 2;
    if (node.matches(OWN_HEIGHT)) height += ownHeight(node);
    var tall = node.querySelectorAll(OWN_HEIGHT);
    for (var i = 0; i < tall.length; i++) height += ownHeight(tall[i]);
    return height;
  }

  // Images, videos and canvases keep their own size once removed, scaled up here in case CSS stretched them. An image
  // that finished without a size failed to load and adds nothing. Anything else without a size yet, and any frame or
  // row sized in cqh, could have been any height.
  function ownHeight(node) {
    var own = node.naturalHeight || node.videoHeight || (node.tagName === "CANVAS" ? node.height : 0);
    if (own) return own * 4;
    return node.complete ? 0 : Infinity;
  }

  // Removed rows can only clamp the position by as much as they shortened the conversation. They may be back by the
  // time anything can look, so what was removed counts as well as the height that can be seen. A fall neither explains
  // is a scroll with no input that happened to land next to a removal, such as the browser's find-in-page.
  function compare(el) {
    // A conversation that is hidden reads as empty and at its top. Taken for where the reader is, that would be what
    // they are held to, or kept from being put back to their place, once it shows again.
    if (el.clientHeight === 0) {
      removedHeight.delete(el);
      return;
    }
    // A fling, held or not, is not a position to learn from. The frame after a removal can be the first to see one.
    if (holdAgainstFling(el) || el.scrollTop < FLUNG_ABOVE_TOP) {
      removedHeight.delete(el);
      return;
    }
    var seen = lastSeen.get(el);
    var removed = removedHeight.get(el);
    var top = el.scrollTop;
    var height = el.scrollHeight;
    removedHeight.delete(el);
    // Back where it was, the collapse is over, so a later scroll has nothing to do with it. The event that brought it
    // back is still part of it: the hook runs next and has its own idea of the last position, which can be further on.
    var collapse = collapses.get(el);
    if (collapse && collapse.over) collapses.delete(el);
    else if (collapse && top >= collapse.from) collapse.over = true;
    if (seen && rowsMovedAbove(el, seen, top, height)) stopFling(el, false);
    if (seen && removed !== undefined) {
      var drop = seen.top - top;
      if (drop > 1 && (drop <= seen.height - height + 1 || drop <= removed) && !tookRowsAbove(el, seen, top, height)) {
        collapses.set(el, { at: performance.now(), top: top, from: seen.top, fromHeight: seen.height });
      }
    }
    lastSeen.set(el, { top: top, height: height });
  }

  // Rows above the reader coming or going, with the position moved by the same amount so the same rows stay on screen.
  // Rows added at the end while the reader follows it move the position as much, but leave the reader at the end.
  function rowsMovedAbove(el, seen, top, height) {
    var grew = height - seen.height;
    return Math.abs(grew) > ROWS_MOVE_MIN && Math.abs(top - seen.top - grew) <= WINDOW_SLACK && height - top - el.clientHeight > NEAR_BOTTOM;
  }

  // A fling on iOS carries on in the coordinates it started in when rows above the reader come or go. The app moves the
  // position to keep the same rows on screen and the fling drags it back, which the reader sees as being thrown down, or
  // held there while the hold below fights it. Taking the overflow away for a frame ends the fling where the app put it.
  // Elsewhere a fling has been seen to move with the position, and a finger still down is dragging, not flinging.
  var IOS = typeof navigator !== "undefined" && (/iP(hone|ad|od)/.test(navigator.userAgent) ||
    (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1));
  function stopFling(el, flung) {
    if (!IOS || !el.style || fingerDown(performance.now()) || pressed || stopping.has(el)) return;
    if (!flung && !moving(performance.now())) return;
    stopping.add(el);
    var overflow = el.style.overflowY;
    el.style.overflowY = "hidden";
    // Put back once a frame has been drawn without the overflow. Scroll events run in the same update as the next
    // frame's callbacks, before its style and drawing, so one callback would put it back before anything saw it.
    requestAnimationFrame(function () {
      requestAnimationFrame(function () {
        el.style.overflowY = overflow;
        stopping.delete(el);
      });
    });
    try { window.dispatchEvent(new CustomEvent("agy-fling-stop")); } catch (e) {}
  }

  // Ending a fling keeps the reader's place when rows arrive above them, but the rows then had to wait for the fling
  // to stop or reach the top, so every flick up ended against the top of what was loaded, bounced there and waited,
  // and a finger landing as the rows were drawn still took the position back. So on iOS the conversation keeps empty
  // room above its first row, and rows that arrive take their height out of it. The conversation is then no taller and
  // the position is the same number: nothing is written, and there is nothing for a fling or a finger to take back.
  // Rows dropped above the reader give their height back the same way.
  //
  // The room is padding above everything in the conversation, set on the one element the bundle puts inside it. Set
  // as a custom property on the conversation for a rule to read, it had every node in the conversation styled again
  // at each change, which is a quarter of a second for fifty thousand nodes and happens several times for each batch
  // of rows.
  //
  // The browser's own anchoring knows nothing of the room and would move the position for rows it has already made up
  // for, so it is off from when rows above may change until the conversation is at rest. The bundle sets the property
  // on the conversation itself as the reader starts and stops following the end, so it is set on what is inside. For
  // as long as it is off the row at the top of the screen is followed here instead: see keepUp.
  //
  // All of it depends on the rows being put back here, where the room can take them. The bundle says it hands that
  // move over by calling after every drawing of the conversation, from the place it hands it over, so this is known
  // before the first rows arrive, and sooner still by asking how far ahead to load them: see __agyGrowRadius. Until
  // then, and for a bundle that does neither, the page is as it was: no room, and no rows held back for want of it.
  function rowsHandedOver(el) {
    if (keepsRows) return;
    keepsRows = true;
    // The scroll that opened this conversation may be over, with no other to come before the reader's own.
    if (hasRoom(el)) waitForRest(el);
  }
  window.__agyRowsDrawn = function (el, rowsOf) {
    rowsHandedOver(el);
    if (el && rowsOf) rowsFrom.set(el, rowsOf);
    roomAsDrawn(el);
  };
  // The bundle's own scrollbar reads the position and the height, and the room is most of both. It asks here how much
  // to leave out, so its thumb shows where the reader is among the rows. It only measures once the reader moves or
  // points at the conversation, so whether it will ask is not known when the room is made: a bundle whose scrollbar
  // no longer asks is found by doctor and by the live bundle test, not here.
  if (IOS) window.__agyTopRoom = function (el) { return roomOf(el); };
  // With room for them, rows are asked for from further away than the bundle's 600 px: a flick at 4,000 px a second
  // covers this in the 0.5 s a phone takes to draw them, with a little to spare.
  //
  // Only a bundle that hands its rows over asks how far, and it asks as it loads, before it has drawn anything. That
  // is the first sign of the rows being handed over, some tenths of a second before the bundle says so by drawing
  // the first conversation of the page a second time: taken here, that conversation has its room as it opens, as
  // every later one does. A bundle that hands its rows over and does not ask is known by its drawing, as before.
  Object.defineProperty(window, "__agyGrowRadius", {
    configurable: true,
    get: function () {
      rowsHandedOver(null);
      return IOS ? 2400 : 0;
    }
  });
  function roomBox(el) {
    var box = el && el.firstElementChild;
    return box && box.style ? box : null;
  }
  function hasRoom(el) { return IOS && keepsRows && Boolean(roomBox(el)) && !roomless.has(el); }
  function roomOf(el) { return rooms.get(el) || 0; }
  function setRoom(el, px) {
    var box = roomBox(el);
    px = box ? Math.max(0, px) : 0;
    rooms.set(el, px);
    if (!box) return;
    // Taken off when there is none, which leaves a padding the bundle may give that element as it was.
    if (px) box.style.paddingTop = px + "px";
    else box.style.removeProperty("padding-top");
  }
  // Whether there are more rows than fill the screen. Fewer cannot hold a position under the room: the furthest the
  // conversation then goes is inside it.
  function rowsFill(el) { return el.scrollHeight - el.clientHeight > roomOf(el); }
  function shiftOf(el) { return shifts.get(el) || 0; }
  function noteShift(el, by) { shifts.set(el, shiftOf(el) + by); }
  // A room that changes by so much moves every position noted for the conversation by as much: the same rows are
  // that much further down it. What was last seen is noted again, and a collapse still being followed, or a hold
  // against a jump, goes on from where its rows now are.
  function roomChanged(el, by) {
    lastSeen.set(el, { top: el.scrollTop, height: el.scrollHeight });
    var collapse = collapses.get(el);
    if (collapse) {
      collapse.top += by;
      collapse.from += by;
      collapse.fromHeight += by;
    }
    var hold = holds.get(el);
    if (hold) {
      hold.top += by;
      hold.last += by;
      hold.height += by;
    }
  }
  function roomEvent(what, by) {
    try { window.dispatchEvent(new CustomEvent("agy-room", { detail: { what: what, by: Math.round(by) } })); } catch (e) {}
  }
  // How much of a shift the room makes up for: all of one that left the rows higher, and as much of one that left
  // them lower as there is room.
  function roomFor(el, off) {
    if (!hasRoom(el)) return 0;
    return off > 0 ? Math.min(off, roomOf(el)) : off;
  }
  function takeRoom(el, off) {
    var used = roomFor(el, off);
    if (used) setRoom(el, roomOf(el) - used);
    return used;
  }
  function holdAnchoring(el) {
    if (!hasRoom(el)) return;
    if (!unanchored.has(el)) {
      unanchored.add(el);
      roomBox(el).style.overflowAnchor = "none";
    }
    waitForRest(el);
    standIn(el);
  }
  function anchorAgain(el) {
    unanchored.delete(el);
    var box = roomBox(el);
    if (box) box.style.removeProperty("overflow-anchor");
  }

  // The room holds no rows, so a reader is not left looking at it. One who gets there ahead of the rows has reached the
  // top of what is loaded: the room above them is given up, which leaves the first row at the top of the screen, and
  // a fling ends there. A finger pulling down into it is left to, as it is with the rubber band at the real top, and
  // so for a moment is a fling that rows are about to catch up with.
  function keepOffRoom(el) {
    var room = roomOf(el);
    var left = Math.max(0, el.scrollTop);
    var now = performance.now();
    if (left >= room || fingerDown(now)) return;
    if (growLetInAt > keptAt && now - growLetInAt < GRACE_MS && room - left < GRACE_PX && moving(now)) return;
    // Rows that do not fill the screen leave the end of the conversation inside the room. Giving the room up as far as
    // the reader then only moves that end, and the reader with it, to be found in the room again. All of it goes.
    var keep = rowsFill(el) ? left : 0;
    holdAnchoring(el);
    setRoom(el, keep);
    noteShift(el, keep - room);
    roomChanged(el, keep - room);
    stopFling(el, false);
    roomEvent("stop", room - left);
  }

  // A conversation gets room where it has older rows to load, which its button says, and rows to fill the screen.
  function wantsRoom(el) {
    var older = el.querySelector(OLDER_ROWS);
    return Boolean(older) && older.getAttribute("aria-disabled") !== "true" && rowsFill(el);
  }
  // A conversation that no finger has been on has no fling for a write to be carried off by. So one that has just
  // appeared is given its room at once, in the frame after it is first heard of, as soon as it has rows to fill the
  // screen and older ones to load: a reader who flicks the moment it appears has the room under them. It is heard
  // of when the bundle draws it, and a little sooner when it is scrolled to its end as it opens. Made at the first
  // rest, as it was, the room was not there for a reader who started at once and never rested: rows waited for
  // them and their flings were ended for as long as they went on, seven seconds in one trace. A little room, left
  // by rows dropped above the conversation as it opened, is filled the same way.
  //
  // That ends with the first finger on the conversation, which may leave a fling, and with its first rest: one
  // that has been there a while may be moving for a key or a smooth scroll of the app's, and only rest says that
  // nothing is. It ends as well where the write was taken back. From then on the room is made and filled at rest,
  // as every change to it is.
  function roomAsDrawn(el) {
    if (!hasRoom(el) || roomOf(el) >= ROOM_LOW || settled.has(el) || drawing.has(el)) return;
    drawing.add(el);
    requestAnimationFrame(function () {
      drawing.delete(el);
      if (settled.has(el) || !el.isConnected) return;
      // With a finger down somewhere, or a press, it is left to rest, which waits for both.
      if (fingerDown(performance.now()) || pressed) {
        waitForRest(el);
        return;
      }
      // One that is hidden reads as having no rows, and wants none.
      if (wantsRoom(el)) resizeRoom(el, ROOM, true);
    });
  }

  // Once a finger has been on a conversation, rest is the one time a position can be written, so that is when the
  // room is filled back up, or given up once the conversation has no older rows left to need it. Rows that do not
  // fill the screen get none: the position written for it would be cut back to the end of the conversation, which
  // is then inside the room.
  function waitForRest(el) {
    resting.add(el);
    if (!restTimer) restTimer = setTimeout(atRest, REST_MS);
  }
  function rowsDue(now) { return growLetInAt > keptAt && now - rowsDueSince < ROWS_DUE_MS; }
  function atRest() {
    restTimer = 0;
    var now = performance.now();
    // Gone through as it stood: a conversation put back to wait again is seen at the next turn, not in this one.
    var waiting = [];
    resting.forEach(function (el) { waiting.push(el); });
    waiting.forEach(function (el) {
      // A conversation that has left the page, or is hidden, reads as empty and at its top, which says nothing about
      // its room. It is left as it is until it moves again.
      if (!el.isConnected || !el.clientHeight) {
        resting.delete(el);
        anchorAgain(el);
        return;
      }
      if (fingerDown(now) || pressed || stopping.has(el) || now - movedAt < REST_MS || framesSinceMove < STILL_FRAMES || el.scrollTop < 0) return;
      // Its first rest is the last of its being given room as it is drawn: see roomAsDrawn.
      settled.add(el);
      // Rows that were let in may arrive after the conversation has come to rest, and a finger may land as they are
      // drawn. With its anchoring back on the browser would move the position for them, which is what a finger takes
      // back, so the anchoring stays off until they are in. The rest of what rest is for is not held up.
      if (!rowsDue(now)) {
        resting.delete(el);
        lastLook(el);
        anchorAgain(el);
      }
      keepOffRoom(el);
      // One that gave its room up for good since it was put down to wait has none to fill.
      if (!hasRoom(el)) return;
      var room = roomOf(el);
      var want = wantsRoom(el) ? ROOM : 0;
      if (want === room || (want && room >= ROOM_LOW && room <= ROOM_HIGH) || (roomFailures.get(el) || 0) >= ROOM_FAILURES_MAX) return;
      // A finger that has rested on the screen for long enough is taken as lifted elsewhere, in case its end went
      // missing. Not for this write: it would be taken back when the finger moved, long after the frames it is
      // watched for. It waits until the finger is known to be up.
      if (touching) {
        resting.add(el);
        return;
      }
      resizeRoom(el, want);
    });
    if (resting.size > 0) restTimer = setTimeout(atRest, REST_MS);
  }

  // The room and the position change by the same amount, so nothing on screen moves. A finger can still land in the
  // instant this is written, and iOS then puts back a position that is now tens of thousands of pixels from the same
  // rows. The frames after are watched for that, and so is the scroll event that tells of it, and the room is put
  // back as it was. Twice for a conversation, and its room is left alone.
  //
  // The room is padding on an element of the bundle's markup. Where that element is not taller by as much for it, the
  // markup is not what this was written for, and that conversation's room is given up for good: rows would take
  // their height from something that is not there.
  function resizeRoom(el, want, asDrawn) {
    var by = want - roomOf(el);
    var was = el.scrollTop;
    // What is inside the conversation, since the conversation itself is never shorter than the screen.
    var inner = el.firstElementChild;
    var tall = inner ? inner.offsetHeight : 0;
    setRoom(el, want);
    if (!inner || Math.abs(inner.offsetHeight - tall - by) > 2) {
      setRoom(el, 0);
      roomless.add(el);
      roomEvent("none", by);
      return;
    }
    // The write is the room's own and moves nothing on the screen, so it is not the app moving the conversation, for
    // which a fling is ended: see __agyScrolledByScript.
    ownWrite = true;
    el.scrollTop = was + by;
    ownWrite = false;
    var set = el.scrollTop;
    noteShift(el, set - was);
    roomChanged(el, by);
    roomEvent("top", by);
    // A change this small is no further than a finger moves the conversation in the same frames, so it is not
    // watched for being taken back. It is still this write's doing, which the bundle may ask about.
    var write = { was: was, set: set, by: by, frames: ROOM_CHECK_FRAMES, watched: Math.abs(by) >= ROWS_MOVE_MIN, asDrawn: Boolean(asDrawn) };
    roomWrites.set(el, write);
    requestAnimationFrame(function check() {
      if (roomWrites.get(el) !== write || roomWriteTakenBack(el)) return;
      if (--write.frames > 0) requestAnimationFrame(check);
      else roomWrites.delete(el);
    });
  }

  // Whether iOS has put back the position that a write for the room replaced. The room then goes back to what it was,
  // and so does what was last seen of the conversation: the move is this write undone, and the hold against a jump
  // nobody asked for is not to take it for one and write the position again under the finger.
  function roomWriteTakenBack(el) {
    var write = roomWrites.get(el);
    // A conversation that has left the page, or is hidden, reads as at its top, which is not the position put back.
    if (!write || !write.watched || !el.isConnected || !el.clientHeight) return false;
    var top = el.scrollTop;
    if (Math.abs(top - write.was) >= Math.abs(top - write.set)) return false;
    roomWrites.delete(el);
    setRoom(el, roomOf(el) - write.by);
    noteShift(el, write.was - write.set);
    roomChanged(el, -write.by);
    // Whatever put the position back, something moves this conversation. A room made as it was drawn is tried again
    // at rest and not as it is drawn, and is not one of the two that a conversation is left alone after: it has
    // both of those left at rest, as it had before it was given room any sooner.
    if (!write.asDrawn) roomFailures.set(el, (roomFailures.get(el) || 0) + 1);
    settled.add(el);
    roomEvent("back", write.by);
    return true;
  }

  // Whether a scroll event tells of the room's write and of nothing else: the first after it, with the position still
  // what was written. Nothing on the screen moved for it, so it is not the conversation moving. Taken for that, the
  // room given to a conversation as it is drawn held back the rows the bundle drops as it opens one, which then went
  // under a reader who had started to scroll.
  function roomWriteHeard(el) {
    var write = roomWrites.get(el);
    if (!write || write.heard) return false;
    write.heard = true;
    return Math.abs(el.scrollTop - write.set) <= 1;
  }

  // Rows taken from above the reader lower the position by what they took, which leaves everything on screen where it
  // was and nothing to go back to. A position clamped by a collapse ends up at the end of the conversation instead.
  function tookRowsAbove(el, seen, top, height) {
    var shrank = seen.height - height;
    return shrank > ROWS_MOVE_MIN && Math.abs(seen.top - top - shrank) <= WINDOW_SLACK && height - top - el.clientHeight > NEAR_BOTTOM;
  }

  // An iPhone has been seen moving a long conversation that had just re-rendered, with nothing asking it to. Once it put
  // the conversation tens of thousands of pixels above its top, and its bounce carried the reader to the top. Once a
  // bounce at the bottom ended 64,000 px up, at what had been the bottom before the conversation grew. A finger moves
  // a few hundred pixels at a time and script cannot set a negative position, so neither is the reader. The position
  // is held until the bounce has had time to finish. A reader at the bottom stays at the bottom, whatever the re-render
  // did to the height. Anyone else goes back to the position they had.
  //
  // The bounce only pulls toward where it was going, so a position further down than the one set here, and further
  // down than the last one set, came from something else, such as the reader or the button that jumps to the latest
  // message, and ends the hold. So does input on the conversation. A hold that ends with the bounce still going above
  // the top leaves no position to go back to, so the rest of that bounce is not held again.
  function holdAgainstFling(el) {
    var now = performance.now();
    var hold = holds.get(el);
    if (hold) {
      // Rows above the reader coming or going move the position by what they added or took, and what is held with it.
      // The app asking for that position says so even when the first event after it still carries the bounce, or
      // the old position iOS put back.
      var grew = el.scrollHeight - hold.height;
      var slid = Math.abs(el.scrollTop - hold.last - grew) <= WINDOW_SLACK ||
        (scripted && scripted.el === el && scripted.top !== null && now - scripted.at < FLING_HOLD_MS &&
          Math.abs(scripted.top - hold.last - grew) <= WINDOW_SLACK);
      if (hold.gap < 0 && Math.abs(grew) > ROWS_MOVE_MIN && slid) hold.top += grew;
      var target = hold.gap >= 0 ? el.scrollHeight - hold.gap : hold.top;
      var movedOn = el.scrollTop > target + 1 && el.scrollTop > hold.last + 1;
      if (movedOn || window.__agyLastScrollInputAt > hold.at || now - hold.at >= FLING_HOLD_MS) {
        holds.delete(el);
        if (el.scrollTop < 0) lastSeen.delete(el);
        return false;
      }
      setHeld(el, hold, target);
      return true;
    }
    var seen = lastSeen.get(el);
    if (pressed || !seen || seen.top < 0) return false;
    if (el.scrollTop >= FLUNG_ABOVE_TOP && !thrownIntoRoom(el, seen, now) && !unaskedJump(el, seen, now)) return false;
    // A collapse still being undone left a clamped position behind, and the reader belongs where it started.
    var collapse = collapses.get(el);
    if (collapse && !collapse.over && now - collapse.at < COLLAPSE_MS) seen = { top: collapse.from, height: collapse.fromHeight };
    var flungTo = el.scrollTop;
    var atBottom = seen.height - seen.top - el.clientHeight <= NEAR_BOTTOM;
    hold = { gap: atBottom ? seen.height - seen.top : -1, top: seen.top, at: now };
    holds.set(el, hold);
    stopFling(el, true);
    setHeld(el, hold, atBottom ? el.scrollHeight - hold.gap : hold.top);
    try { window.dispatchEvent(new CustomEvent("agy-fling-hold", { detail: { top: flungTo, back: atBottom ? "bottom" : seen.top, from: seen.top } })); } catch (e) {}
    return true;
  }

  // A conversation thrown above its top reads as a negative position, which no finger and no script gives it. With
  // room above the first row it ends in the room instead, and that is somewhere a reader's own fling gets to as well:
  // a step at a time, to be stopped at the first row. A throw is one move, further than a fling makes between two
  // looks at it, with no finger down, that no script asked for or is still animating. After a touch, while a fling
  // may still be going, the page can be held up for half a second and the fling's own step is then as long: only a
  // move faster than any fling is a throw. A conversation that was hidden comes back at the very top of its room,
  // and is put back where it was as a jump is.
  function thrownIntoRoom(el, seen, now) {
    var top = el.scrollTop;
    var jump = seen.top - top;
    if (roomOf(el) - top <= -FLUNG_ABOVE_TOP || jump < JUMP_MIN || fingerDown(now)) return false;
    if (now - window.__agyLastScrollInputAt < MOMENTUM_MS && jump <= THROW_PX_PER_MS * Math.max(16, now - movedAt)) return false;
    if (scripted && scripted.smooth && scripted.el === el && now - scripted.at < FLING_HOLD_MS) return false;
    return !askedFor(el, top, now);
  }

  // A jump across a large part of the conversation in one event, which its height does not explain and no script asked
  // for. Only on touch screens: a desktop browser moves the conversation by itself for find-in-page and its keys.
  function unaskedJump(el, seen, now) {
    if (!(window.matchMedia && window.matchMedia("(pointer: coarse)").matches)) return false;
    var top = el.scrollTop;
    var height = el.scrollHeight;
    var jump = Math.abs(top - seen.top);
    // A share of the rows, which the room above them is no part of.
    if (jump < JUMP_MIN || jump < JUMP_SHARE * (Math.max(seen.height, height) - roomOf(el))) return false;
    if (Math.abs(height - seen.height) * 2 >= jump) return false;
    return !askedFor(el, top, now);
  }

  // Script moving the conversation is the app's own doing. Where it asked to go is kept, because a pin to the bottom
  // can land in the same frame as a jump somewhere else. Scrolling a row into view moves whatever contains it.
  var scripted = null;
  // Set while the room writes a position of its own.
  var ownWrite = false;
  function askedFor(el, top, now) {
    if (!scripted || now - scripted.at > SCRIPT_GRACE_MS) return false;
    if (scripted.el === null || scripted.top === null) return true;
    if (scripted.el !== el) return false;
    var wanted = Math.max(0, Math.min(scripted.top, el.scrollHeight - el.clientHeight));
    return Math.abs(wanted - top) <= el.clientHeight;
  }
  window.__agyScrolledByScript = function (el, top, smooth) {
    scripted = { at: performance.now(), el: el, top: typeof top === "number" && isFinite(top) ? top : null, smooth: Boolean(smooth) };
    // Ended before the position moves rather than after, or the fling paints one more frame in its old coordinates. A
    // smooth scroll is a move the app animates on purpose, such as the End key, and is left to run.
    // A position past the end, as a pin to the bottom asks for, only goes as far as the end.
    //
    // Not for the room's own write. At rest nothing counts as moving and nothing was ended for it. A conversation
    // given its room as it is drawn can count as moving, from the scroll that opened it and a finger that was on
    // another conversation a moment before, and has no fling of its own to end: its overflow would be taken away for
    // two frames, which on a page busy drawing it is long enough to meet the reader's finger.
    if (!ownWrite && !smooth && el && scripted.top !== null && el.matches && el.matches(VIEWPORT) &&
        Math.abs(Math.max(0, Math.min(scripted.top, el.scrollHeight - el.clientHeight)) - el.scrollTop) > ROWS_MOVE_MIN) {
      stopFling(el, false);
    }
  };
  if (typeof Element !== "undefined") {
    var topSetter = Object.getOwnPropertyDescriptor(Element.prototype, "scrollTop");
    if (topSetter && topSetter.set && topSetter.configurable) {
      Object.defineProperty(Element.prototype, "scrollTop", {
        configurable: true,
        enumerable: topSetter.enumerable,
        get: topSetter.get,
        set: function (v) {
          window.__agyScrolledByScript(this, Number(v));
          topSetter.set.call(this, v);
        }
      });
    }
    ["scrollTo", "scroll"].forEach(function (name) {
      var native = Element.prototype[name];
      if (typeof native !== "function") return;
      Element.prototype[name] = function (a, b) {
        var options = a && typeof a === "object" ? a : null;
        window.__agyScrolledByScript(this, options ? options.top : b, Boolean(options && options.behavior === "smooth"));
        return native.apply(this, arguments);
      };
    });
    ["scrollBy", "scrollIntoView", "scrollIntoViewIfNeeded"].forEach(function (name) {
      var native = Element.prototype[name];
      if (typeof native !== "function") return;
      Element.prototype[name] = function () {
        window.__agyScrolledByScript(null, null);
        return native.apply(this, arguments);
      };
    });
  }

  // The position a hold sets is the last one seen, so a hold that runs out with no event after it leaves that behind.
  function setHeld(el, hold, top) {
    el.scrollTop = top;
    hold.last = el.scrollTop;
    hold.height = el.scrollHeight;
    lastSeen.set(el, { top: hold.last, height: el.scrollHeight });
  }

  // Runs before the bundle's own listener on the element, so the answer is ready when the hook asks. The time is noted
  // after the comparison, which asks whether the conversation was already moving before this event.
  window.addEventListener("scroll", function (e) {
    var el = e.target;
    if (el && el.matches && el.matches(VIEWPORT)) {
      roomWriteTakenBack(el);
      compare(el);
      if (!movedRef || movedRef.deref() !== el) movedRef = typeof WeakRef === "function" ? new WeakRef(el) : { deref: function () { return el; } };
      if (!roomWriteHeard(el)) {
        movedAt = performance.now();
        framesSinceMove = 0;
        if (!countingFrames) {
          countingFrames = true;
          requestAnimationFrame(countFrame);
        }
      }
      // The browser's anchoring is left on for a conversation that only moves: it keeps the reader's rows in place
      // when something above them changes height, and that move is a scroll event as well. It is switched off where
      // rows above are about to come or go, and for the room's own changes.
      // The event comes a frame after the move, and a conversation hidden in between reads as at its top.
      if (hasRoom(el) && el.clientHeight) {
        keepOffRoom(el);
        waitForRest(el);
        roomAsDrawn(el);
      }
    }
  }, opts);

  function lastMoved() {
    var el = movedRef && movedRef.deref();
    return el && el.isConnected ? el : null;
  }

  function countFrame() {
    framesSinceMove++;
    if (framesSinceMove < STILL_FRAMES) requestAnimationFrame(countFrame);
    else countingFrames = false;
  }

  // Still moving after the reader's input: a fling, its bounce, or the reader's own scroll. Moved by the app alone, or
  // still for a moment, it has nothing to end and nothing to wait for.
  function moving(now) {
    return now - window.__agyLastScrollInputAt < MOMENTUM_MS && (now - movedAt < STILL_MS || framesSinceMove < STILL_FRAMES);
  }

  // Asked by the bundle before it drops rows far above the reader. The position moves up by what it drops, and on iOS
  // the reader's finger or fling does not move with it, so the rows wait for the conversation to stop moving.
  // A finger down is waited for however long it stays, because a drag carries on from where it was and throws the
  // reader thousands of pixels. A fling only nudges the position, so the time between flings adds up to at most
  // TRIM_WAIT_MAX_MS, while the time a finger spends down adds nothing.
  window.__agyDeferTrim = function () {
    var wait = waitForStill(trimWait, "agy-trim-wait", false);
    // The browser's anchoring is switched off for rows that are about to go. While they are held back it is left
    // to keep the reader's rows in place when something else above them changes.
    if (!wait) holdAnchoring(lastMoved());
    return wait;
  };

  // Asked before it adds rows above the reader. With room above the first row they take their height from it and
  // nothing is written, so the conversation's moving is no reason to wait: near the screen they are let in at once,
  // and further off once it is idle, since drawing them holds the page.
  //
  // Without that much room the position has to move down by what they add. Since 2.32 the bundle asks with the fling
  // still at speed, and the fling carries on from where it was: the reader lands that far up the new rows, and a move
  // under ROWS_MOVE_MIN does not even end it. So then they wait as dropped rows do, except that a fling which has
  // reached the first row has nowhere left to go, and the rows that arrive end its bounce.
  //
  // A bundle that puts its rows back itself is asked as well. It adds them when it likes, as it did before it was
  // asked: the rows cannot be made to arrive any better for it by having them wait.
  window.__agyDeferGrow = function () {
    if (!keepsRows) return false;
    var el = lastMoved();
    var now = performance.now();
    if (el && roomOf(el) >= ROOM_NEED) {
      growWait.waiting = false;
      var idle = !fingerDown(now) && !pressed && !moving(now) && !typing() && now - keptAt >= GROW_PACE_MS;
      // The bundle asks again in 100 ms for as long as the first row is near enough for it to want more.
      if (el.scrollTop - roomOf(el) >= GROW_NEAR && !idle) return true;
      // Rows let in for a reader who follows the end arrive without being handed over, so a wait that has run out
      // starts again with the next ones.
      if (!rowsDue(now)) rowsDueSince = now;
      growLetInAt = now;
      holdAnchoring(el);
      return false;
    }
    var atTop = Boolean(el && el.scrollTop <= roomOf(el));
    var wait = waitForStill(growWait, "agy-grow-wait", atTop);
    if (!wait) holdAnchoring(el);
    return wait;
  };
  function typing() {
    var at = typeof document !== "undefined" && document.activeElement;
    return Boolean(at && (at.isContentEditable || at.tagName === "TEXTAREA" || at.tagName === "INPUT"));
  }

  function waitForStill(asked, event, flingOver) {
    var now = performance.now();
    var down = fingerDown(now);
    var wait = IOS && (down || (moving(now) && !flingOver));
    // The bundle asks again every 100 or 250 ms while it waits, rendering can hold that back for seconds, and the
    // bundle may stop asking without a last answer. A gap longer than any render seen is a new wait.
    var continued = asked.waiting && now - asked.askedAt < TRIM_ASK_GAP_MS;
    if (wait && !continued) {
      asked.flingMs = 0;
      try { window.dispatchEvent(new CustomEvent(event)); } catch (e) {}
    } else if (wait && !asked.askedDown) {
      asked.flingMs += now - asked.askedAt;
    }
    if (wait && !down && asked.flingMs >= TRIM_WAIT_MAX_MS) wait = false;
    asked.waiting = wait;
    asked.askedAt = now;
    asked.askedDown = down;
    return wait;
  }

  // With the browser's anchoring off nothing else keeps the reader's rows where they are when something above them
  // changes height, so for that long the tracker does: it follows a row, and takes what shifts it out of the room.
  // The row is the one at the top of the screen, as it is for a browser: what changes above that row moves it, and
  // what changes below it does not. A row further down would be moved by a change the reader is looking at, and one
  // further up would miss a change between it and the screen. The reader moves on, so the row is chosen again every
  // frame, from the rows the bundle itself chooses among when it keeps one in place.
  //
  // Finding those rows goes through the whole conversation, which takes milliseconds. They are found once after rows
  // are handed over, and again only when the one followed has gone or the top of the screen is in none of them, no
  // more often than every ROWS_LOOK_MS.
  function rowsIn(el, again) {
    var known = rowLists.get(el);
    var now = performance.now();
    if (known && (!again || now - known.at < ROWS_LOOK_MS)) return known.rows;
    var find = rowsFrom.get(el);
    var rows = [];
    // Until the bundle has said how, there are none to find, and none are noted as found.
    if (!find) return rows;
    try { rows = Array.prototype.slice.call(find()); } catch (e) {}
    rowLists.set(el, { rows: rows, at: now });
    return rows;
  }
  // The first row to reach below the top of the screen. It is looked for from the row followed so far, since from one
  // frame to the next it is that row or one beside it.
  function rowAtTop(el, from) {
    var rows = rowsIn(el, false);
    if (from && !from.isConnected) rows = rowsIn(el, true);
    var i = from ? rows.indexOf(from) : -1;
    var top = el.getBoundingClientRect().top;
    // Nothing for a row that has left the page or is not laid out: it reads as no size, and has no place to measure.
    function boxOf(k) {
      var box = rows[k].getBoundingClientRect();
      return box.width || box.height ? box : null;
    }
    var at = -1;
    var past = true;
    var k;
    var next;
    var box = i >= 0 ? boxOf(i) : null;
    if (box && box.bottom > top) {
      // Up, for as long as the row before reaches below the top of the screen too.
      at = i;
      for (k = i - 1; k >= 0; k--) {
        next = boxOf(k);
        if (!next) continue;
        if (next.bottom <= top) break;
        at = k;
        box = next;
      }
      past = box.top > top;
    } else {
      // Down, to the first row that does.
      for (k = Math.max(i, 0); k < rows.length && past; k++) {
        next = boxOf(k);
        if (!next) continue;
        at = k;
        past = next.bottom <= top;
      }
    }
    // Where the top of the screen is in none of the rows that are known, past either end of them or between two,
    // there may be a row that is not known. The next frame finds it.
    if (past) rowsIn(el, true);
    return at < 0 ? null : rows[at];
  }
  // A row that has left the page or is not laid out has no place to measure, and neither has any row of a
  // conversation that is hidden: everything about them reads as nothing, which is not where the row is. The bundle
  // leaves such rows out when it chooses the one to keep, and one that becomes so afterwards is given up.
  function lostRow(f) {
    if (!f.row || !f.row.isConnected || !f.el.clientHeight) return true;
    var box = f.row.getBoundingClientRect();
    return !box.width && !box.height;
  }
  function rowsTall(el) { return el.scrollHeight - roomOf(el); }
  // Whether the position has fallen to the end since the last look. Nobody scrolls up and arrives at the end: something
  // got shorter, which left the position past the end, and the browser cut it back.
  function cutBack(f) {
    var el = f.el;
    return el.scrollHeight - el.clientHeight - el.scrollTop <= 1 && el.scrollTop < f.left;
  }
  // How far the row is from where it is kept, as far as room can put it back. With room that leaves out how far the
  // reader has scrolled since, and what room cannot put back is given up: the row is kept where that leaves it.
  //
  // The end of the conversation is where it cannot. A reader there is where the end puts them: something that gets
  // shorter leaves the position past the end, the browser cuts it back, and every row on the screen comes down. Room
  // taken for that shortens the conversation again, the position is cut back again by as much, and the rows are
  // where they were, to be taken for in every frame after. So rows that have come down are put back only as far as
  // the conversation goes on under the screen. Room given at the end takes the reader off it, and for one who
  // follows the end as a reply is written, a cut and the bundle's own move to the end can come between two looks
  // and not be told apart. So at the end nothing is put back at all.
  //
  // Except in the commit that hands rows over, where nothing but the browser can have moved the position since the
  // move was made: no input reaches the page before a microtask. There a position that has fallen to the end was
  // cut back, which is not the reader's doing, and rows that the cut left higher are put back where they were.
  function awayOf(f, inCommit) {
    var el = f.el;
    var off = f.row.getBoundingClientRect().top - el.getBoundingClientRect().top - f.offset;
    if (!f.roomy) return off;
    var top = el.scrollTop;
    // Nothing is under the screen for a position that a bounce has carried past the end, either.
    var below = Math.max(0, el.scrollHeight - el.clientHeight - top);
    var atEnd = below <= 1;
    if (cutBack(f)) noteShift(el, top - f.left);
    var away = off + top - shiftOf(el) - f.from;
    var back = atEnd && !inCommit ? 0 : Math.min(away, below);
    f.offset += away - back;
    return back;
  }
  // The first move is the bundle's own, to half a pixel. A position only goes to whole pixels in WebKit, so asking
  // again for less than one would ask on every frame and move nothing.
  function putRow(f, least, roomOnly, inCommit) {
    var el = f.el;
    var off = awayOf(f, inCommit);
    if (Math.abs(off) < least) off = 0;
    var rest = off - takeRoom(el, off);
    if (rest && !roomOnly) {
      var top = el.scrollTop;
      el.scrollTop = top + rest;
      noteShift(el, el.scrollTop - top);
    }
    // Only the first two are told of: see reportKept.
    if (f.moved.length < 2) f.moved.push(Math.round(off));
    f.left = el.scrollTop;
    f.tall = rowsTall(el);
  }
  // The row at the top of the screen is the one followed from here. It is placed as far from the one followed until
  // now as it stands from it, so that what that one was still away by is not lost with it.
  function turnToTop(f) {
    var el = f.el;
    // Nothing of a conversation that is hidden has a place.
    if (!el.clientHeight) return false;
    var top = el.getBoundingClientRect().top;
    var lost = lostRow(f);
    if (!lost) {
      var box = f.row.getBoundingClientRect();
      if (box.top <= top && box.bottom > top) return true;
    }
    var next = rowAtTop(el, f.row);
    if (!next) return !lost;
    if (next === f.row) return true;
    var place = next.getBoundingClientRect().top;
    if (lost) {
      f.offset = place - top;
      f.from = el.scrollTop - shiftOf(el);
    } else {
      f.offset += place - f.row.getBoundingClientRect().top;
    }
    f.row = next;
    return true;
  }
  // One look at what is followed. A reader who is scrolling is only followed with room, and only when the rows have
  // changed height: a position read in the middle of a fling can be a pixel out, and nothing says it is read in step
  // with where the rows are.
  function look(f) {
    var el = f.el;
    var movedOn = Math.abs(el.scrollTop - f.left) > 1;
    if (movedOn && !f.roomy) return false;
    if (!movedOn || rowsTall(el) !== f.tall) putRow(f, movedOn ? 2 : 1, movedOn);
    else f.left = el.scrollTop;
    return true;
  }
  // Every frame for as long as a conversation is followed, which with room is for as long as the browser's anchoring
  // is off: nothing else makes up for what shifts the rows then, however long the fling. Once it is back on the
  // browser moves the position for a shift, and a row still followed would have the same shift taken out of the room
  // as well. Where the browser does not anchor, and without room, it is for the first frames after rows arrive.
  function keepUp(f) {
    var el = f.el;
    if (f.roomy && ANCHORS && !unanchored.has(el)) return false;
    if (lostRow(f) && !(f.roomy && turnToTop(f))) return false;
    if (!look(f)) return false;
    if (f.roomy) turnToTop(f);
    return f.frames-- > 0 || (f.roomy && unanchored.has(el));
  }
  function followOn(f) {
    // Rows handed over since are followed in its place.
    if (followers.get(f.el) !== f) return;
    if (keepUp(f)) requestAnimationFrame(function () { followOn(f); });
    else followers.delete(f.el);
  }
  // The anchoring has just been switched off. Where no rows handed over are being followed, the row at the top of
  // the screen is from here.
  function standIn(el) {
    if (followers.has(el)) return;
    var f = { el: el, row: null, offset: 0, from: 0, left: 0, tall: 0, moved: [], frames: 0, roomy: true };
    if (!turnToTop(f)) return;
    followers.set(el, f);
    requestAnimationFrame(function () { followOn(f); });
  }
  // The anchoring is about to be switched back on. What has shifted the row since the last frame is still to be made
  // up for here: the browser only answers for what changes once it has the conversation.
  function lastLook(el) {
    var f = followers.get(el);
    if (f && f.roomy && !lostRow(f)) look(f);
  }
  function reportKept(f, after) {
    if (!f.moved[0] && !after) return;
    try { window.dispatchEvent(new CustomEvent("agy-rows-kept", { detail: { moved: f.moved[0], again: after } })); } catch (e) {}
  }

  // The bundle puts the rows on screen back where they were when rows above them come or go, once, as it commits the
  // new rows. A table among them only then finds its width, is drawn again with shorter cells, and nothing puts the
  // rows back a second time: where the browser does not anchor after a script moved the position, as WebKit does not,
  // they were left 350 px off. So the bundle's move is made here, made again once that second drawing is done, and
  // the row is followed after.
  //
  // Where there is room above the first row the move comes out of that and no position is written, so it is made
  // whatever the reader is doing, and how far they have scrolled since is left out of how far the row has shifted.
  // Without room the position is written: a fling still going when the rows arrive is ended first, or it carries the
  // position back, and the frames after stop at the first one in which anything else moved the conversation.
  window.__agyKeepRows = function (el, row, offset) {
    // Whoever calls has handed the move over, having said so first or not.
    rowsHandedOver(el);
    keptAt = performance.now();
    // This row is what is followed of the conversation from here, in place of what was, and the rows found in it
    // before are not the ones it has now.
    var f = { el: el, row: row, offset: offset, from: 0, left: 0, tall: 0, moved: [], frames: KEEP_ROWS_FRAMES, roomy: hasRoom(el) };
    followers.set(el, f);
    rowLists.delete(el);
    holdAnchoring(el);
    f.from = el.scrollTop - shiftOf(el);
    // Where the bundle's row has no place there is nothing of its to put back. With room, a row that has one is
    // followed in its stead.
    if (lostRow(f)) {
      if (f.roomy && turnToTop(f)) requestAnimationFrame(function () { followOn(f); });
      else followers.delete(el);
      return;
    }
    var first = awayOf(f, true);
    if (Math.abs(first - roomFor(el, first)) >= KEEP_ROWS_STOP_MIN) stopFling(el, false);
    putRow(f, 0.5, false, true);
    // No input reaches the page before a microtask, so whatever moved the row since is not the reader.
    Promise.resolve().then(function () {
      var on = true;
      if (lostRow(f)) {
        // A row replaced or hidden before the commit is over has no place left to measure.
        reportKept(f, "gone");
        on = f.roomy && turnToTop(f);
      } else if (!f.roomy && Math.abs(el.scrollTop - f.left) > 1 && !cutBack(f)) {
        // Something else in the same commit has moved the conversation since, as the bundle's navigator does when it
        // jumps to a step: the position is that script's now. With room the row is not put back by the position.
        // A position the browser cut back to the end is nobody's jump, and the row is put back as ever.
        if (f.moved[0]) reportKept(f, "taken");
        on = false;
      } else {
        putRow(f, 1, false, true);
        reportKept(f, f.moved[1]);
      }
      if (on) requestAnimationFrame(function () { followOn(f); });
      else followers.delete(el);
    });
  };

  // Removing rows is what collapses the content and clamps the scroll position, but only if it shortened the
  // conversation. Swapping a row for one of the same height happens all the time while a reply streams and says
  // nothing about the next scroll. The comparison waits for the next frame or scroll event, because reading the
  // layout here could clamp the position in a gap between two commits that would otherwise never have shown, and
  // a hidden tab has neither until the reader returns.
  new MutationObserver(function (records) {
    for (var i = 0; i < records.length; i++) {
      var t = records[i].target;
      var el = records[i].removedNodes.length > 0 && t.closest && t.closest(VIEWPORT);
      if (!el) continue;
      var height = removedHeight.get(el) || 0;
      for (var j = 0; j < records[i].removedNodes.length; j++) height += guessHeight(records[i].removedNodes[j]);
      removedHeight.set(el, height);
    }
    if (removedHeight.size > 0 && !frameRequested) {
      frameRequested = true;
      // After the frame's own layout, so that this forces none.
      requestAnimationFrame(function () {
        setTimeout(function () {
          frameRequested = false;
          removedHeight.forEach(function (_, el) { compare(el); });
        });
      });
    }
  }).observe(document, { childList: true, subtree: true });

  // Continues a scrollbar drag from the current position whenever something other than the drag moved it.
  window.__agyDragScrollbar = function (drag, el, horizontal, coord, maxThumbOffset, maxScrollPos) {
    var pos = horizontal ? el.scrollLeft : el.scrollTop;
    if (drag.lastPos === undefined) {
      drag.lastPos = drag.startScrollPos;
      drag.lastCoord = drag.startCoord;
      drag.lastMax = maxScrollPos;
    }
    if (Math.abs(pos - drag.lastPos) > 1 || maxScrollPos !== drag.lastMax) {
      drag.startScrollPos = pos;
      drag.startCoord = drag.lastCoord;
    }
    var target = drag.startScrollPos + (coord - drag.startCoord) / maxThumbOffset * maxScrollPos;
    // The range the bundle gives is the rows', and the room above them is not somewhere a drag goes.
    if (horizontal) el.scrollLeft = target;
    else el.scrollTop = Math.max(target, Math.ceil(roomOf(el)));
    drag.lastPos = horizontal ? el.scrollLeft : el.scrollTop;
    drag.lastMax = maxScrollPos;
    drag.lastCoord = coord;
  };

  window.__agyIsSpontaneousScroll = function (el) {
    if (!el || pressed || document.querySelector(FIND_BAR)) return false;
    var now = performance.now();
    // The room cut back at rest lowers the position with nothing on screen moving, which is not the reader leaving
    // the end of the conversation.
    var write = roomWrites.get(el);
    if (write && write.by < 0 && !fingerDown(now) && Math.abs(el.scrollTop - write.set) <= 1) return true;
    var collapse = collapses.get(el);
    if (!collapse) return false;
    // The rows can take a while to come back on a slow device, and the scroll event only arrives once they have. Rows
    // coming back can push the position down the conversation, but only something else takes it further up than the
    // collapse left it.
    return now - window.__agyLastScrollInputAt >= 1000 && now - collapse.at < COLLAPSE_MS && el.scrollTop >= collapse.top - 1;
  };
})();
</script>`

const mobileDebug = `<script id="agy-debug">
(function () {
  var vv = window.visualViewport;
  if (!vv) return;
  // The bugs this records are a touch screen's. A sample reads the layout of
  // every element on the page, every frame for a second or more after a focus
  // change, so on a desktop it would only slow closing the side panel and
  // typing in a long conversation.
  if (!window.matchMedia || !window.matchMedia("(pointer: coarse)").matches) return;

  var ENDPOINT = "/__agy/api/debug/log";

  // The two selectors the safe-area patch relies on. Their match count is logged
  // on every sample because a selector that matches nothing, or several nested
  // shells, still reports as "applied" in the patch report.
  var OUTER = ".relative.w-screen.h-\\[100dvh\\]";
  var INNER = "div.h-\\[100dvh\\].w-screen.flex.flex-col, div[data-testid=\"mobile-layout\"], div.agy-shell";

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

  // The room the scroll tracker keeps above a conversation's first row on iOS, which is part of the conversation's
  // position and of its height.
  function topRoom(el) {
    return window.__agyTopRoom ? window.__agyTopRoom(el) || 0 : 0;
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
        ":st" + n(el.scrollTop) + "/sh" + el.scrollHeight + "/ch" + el.clientHeight +
        (topRoom(el) ? "/room" + n(topRoom(el)) : ""));
    }
    return out.length ? out.join(" ") : "-";
  }

  // The conversation alone, in the form scrollers() gives it.
  function conversation() {
    var el = one('[data-testid="autoscroll-viewport"]');
    if (!el) return "-";
    var room = topRoom(el);
    return "autoscroll-viewport:st" + n(el.scrollTop) + "/sh" + el.scrollHeight + "/ch" + el.clientHeight + (room ? "/room" + n(room) : "");
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
  // How many characters the episode has come to, and how many it may come to before its events stop being kept.
  var size = 0;
  var EVENTS_UP_TO = 56000;
  var startedAt = 0;
  var deadline = 0;
  var raf = 0;
  var last = "";

  function keep(line) {
    lines.push(line);
    size += line.length + 1;
  }

  function push(ev) {
    // Safari drops a keepalive body over 64KB, so stay well inside one request. A full episode also skips the
    // state, which reads the whole page's layout. A long scroll fills one in two seconds, and what the page did after
    // that is in its events, so those go on being kept, with the conversation's numbers alone, while the episode is
    // short of EVENTS_UP_TO. Its samples come to about 41,000 characters, which leaves room for some 150 events.
    if (lines.length > 120) {
      if (ev !== "raf" && size < EVENTS_UP_TO) {
        keep("  t=" + pad(n(performance.now() - startedAt), 5) + " ev=" + padR(ev, 13) + " scrollers=[" + conversation() + "]");
      }
      return;
    }
    var s = state();
    if (ev === "raf" && s === last) return;
    last = s;
    keep("  t=" + pad(n(performance.now() - startedAt), 5) + " ev=" + padR(ev, 13) + " " + s);
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
      size = 0;
      startedAt = performance.now();
      last = "";
      episodes++;
      keep(
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
    if (!t) return false;
    if (t.tagName === "TEXTAREA" || t.isContentEditable) return true;
    if (t.tagName === "INPUT") {
      var type = (t.type || "").toLowerCase();
      return !/^(radio|checkbox|button|submit|reset|file|range|color|hidden|image)$/.test(type);
    }
    return false;
  }

  // The conversation can move with no viewport event at all: its scroll position drops, or content is
  // inserted above a position that stays put. Neither has an event to hook, so sample it. Recent input
  // is excluded because that movement is the user's own.
  var lastInputAt = 0;
  ["touchstart", "touchmove", "wheel", "keydown", "pointerdown"].forEach(function (name) {
    window.addEventListener(name, function () { lastInputAt = performance.now(); }, { capture: true, passive: true });
  });
  var lastWheelDir = 0;
  window.addEventListener("wheel", function (e) { lastWheelDir = e.deltaY > 0 ? 1 : -1; }, { capture: true, passive: true });
  var prevEl = null, prevTop = 0, prevHeight = 0, anchor = null, anchorTop = 0;
  setInterval(function () {
    var el = one('[data-testid="autoscroll-viewport"]');
    if (!el) { prevEl = null; anchor = null; return; }
    // Where the reader is among the rows. The room above them is filled and given up with the position moved by as
    // much, which is no move of the reader's.
    var room = topRoom(el);
    var top = el.scrollTop - room, height = el.scrollHeight - room;
    var rect = el.getBoundingClientRect();
    var idleMs = performance.now() - lastInputAt;
    var trigger = "";
    // A row that stayed on screen shows what the reader saw, whatever the scroll position did to keep it there.
    if (anchor && anchor.isConnected && el === prevEl) {
      var moved = anchorTop - anchor.getBoundingClientRect().top;
      // Rows rise past a reader who is following a reply, and that is the point of following it.
      var following = prevHeight - prevTop - el.clientHeight <= 50;
      if (idleMs > 3000 && Math.abs(moved) > 40 && !following) trigger = "visual-shift";
      else if (idleMs <= 3000 && lastWheelDir && moved * lastWheelDir < -40) trigger = "visual-back";
    }
    // Momentum carries a flick for a few seconds after the finger lifts, so only longer quiet counts as no input.
    if (!trigger && el === prevEl && idleMs > 3000) {
      if (prevTop - top > 80) trigger = "st-drop";
      else if (Math.abs(top - prevTop) <= 4 && Math.abs(height - prevHeight) > 80) trigger = "content-shift";
    }
    // Too far for one sample to be a finger on the conversation, so this is recorded even right after a touch, with
    // how long ago that touch was. The position bound is the one the scroll tracker holds against.
    if (!trigger && el === prevEl && (Math.abs(top - prevTop) > 3000 || el.scrollTop < -1000)) trigger = "jump+" + Math.round(idleMs);
    if (trigger) begin(trigger, 1500);
    var hit = document.elementFromPoint(rect.left + rect.width / 2 - 100, rect.top + rect.height / 2);
    anchor = hit && el.contains(hit) ? hit : null;
    anchorTop = anchor ? anchor.getBoundingClientRect().top : 0;
    prevEl = el;
    prevTop = top;
    prevHeight = height;
  }, 150);

  // The scroll tracker puts a fling back before any sample can see it, so it reports one here.
  window.addEventListener("agy-fling-hold", function (e) {
    begin("fling-hold" + (e.detail ? ":" + Math.round(e.detail.top) + ">" + e.detail.back : ""), 1500);
  });
  window.addEventListener("agy-fling-stop", function () { begin("fling-stop", 1500); });
  window.addEventListener("agy-trim-wait", function () { begin("trim-wait", 1500); });
  window.addEventListener("agy-grow-wait", function () { begin("grow-wait", 1500); });
  // How far the bundle's move put the rows back, and how far they had shifted again by the end of the commit.
  window.addEventListener("agy-rows-kept", function (e) {
    begin("rows-kept" + (e.detail ? ":" + e.detail.moved + "," + e.detail.again : ""), 1500);
  });
  // The room above the first row made as the conversation is drawn, or filled back up or given up at rest (top), that
  // undone because the position was taken back (back), a reader who reached the room stopped at the first row (stop),
  // and the room given up for good because the page does not lay it out (none), each with how many pixels.
  window.addEventListener("agy-room", function (e) {
    begin("room-" + (e.detail ? e.detail.what + ":" + e.detail.by : ""), 1500);
  });
  // A finger down or up tells the reader's own movement from the browser's.
  ["touchstart", "touchend", "touchcancel"].forEach(function (name) {
    window.addEventListener(name, function (e) {
      if (lines) push(name + ":" + (e.touches ? e.touches.length : 0));
    }, { capture: true, passive: true });
  });
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
  var started = false;
  var needsLogin = false;

  function sync() {
    var card = (!needsLogin || nativeBanner()) ? null : composerCard();
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
    if (!started) {
      started = true;
      document.body.appendChild(banner);
      window.addEventListener("resize", sync, { passive: true });
      window.addEventListener("scroll", sync, { passive: true });
      if (window.visualViewport) {
        window.visualViewport.addEventListener("resize", sync, { passive: true });
        window.visualViewport.addEventListener("scroll", sync, { passive: true });
      }
      setInterval(sync, 1000);
    }
    sync();
  }

  function check() {
    fetch("/__agy/api/signin/status", { credentials: "same-origin", cache: "no-store" })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (d) {
        if (!d) return;
        needsLogin = Boolean(d.needsSignIn !== undefined ? d.needsSignIn : !d.signedIn);
        if (needsLogin) start();
        else if (started) sync();
      })
      .catch(function () {});
  }

  window.addEventListener("focus", check);
  document.addEventListener("visibilitychange", function () {
    if (document.visibilityState === "visible") check();
  });

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

  var PENDING_PATH_TTL_MS = 30 * 60 * 1000;
  var activeTempConvoId = '';
  var submittingTempConvoId = '';
  var lastComposerSubmitAt = 0;
  var tempToRealConvoId = {};
  var tempToRealOrder = [];

  function recordTempToRealConvo(tempId, realId) {
    if (!tempId || !realId) return;
    if (!Object.prototype.hasOwnProperty.call(tempToRealConvoId, tempId)) {
      tempToRealOrder.push(tempId);
      while (tempToRealOrder.length > 64) {
        var oldest = tempToRealOrder.shift();
        delete tempToRealConvoId[oldest];
      }
    }
    tempToRealConvoId[tempId] = realId;
  }

  function promoteTempConvoIdIfSubmitted(realId) {
    if (submittingTempConvoId && realId && submittingTempConvoId !== realId &&
        (!lastComposerSubmitAt || (Date.now() - lastComposerSubmitAt) <= PENDING_PATH_TTL_MS)) {
      var prevTemp = submittingTempConvoId;
      recordTempToRealConvo(prevTemp, realId);
      for (var i = 0; i < pendingPromptPaths.length; i++) {
        if (pendingPromptPaths[i].convoId === prevTemp) {
          pendingPromptPaths[i].convoId = realId;
        }
      }
    }
    submittingTempConvoId = '';
    activeTempConvoId = '';
  }

  function readExplicitUrlConvoId() {
    var params = new URLSearchParams(window.location.search);
    var id = params.get('conversationId') || params.get('c');
    if (id) return id;
    var path = window.location.pathname || '';
    if (path.startsWith('/c/')) {
      var seg = path.slice(3).split('/')[0];
      if (seg) return seg;
    }
    return '';
  }

  function getActiveConversationId() {
    var explicitId = readExplicitUrlConvoId();
    if (explicitId) {
      promoteTempConvoIdIfSubmitted(explicitId);
      return explicitId;
    }
    if (!activeTempConvoId) {
      activeTempConvoId = 'temp_' + Date.now().toString(36) + '_' + Math.random().toString(36).slice(2, 6);
    }
    return activeTempConvoId;
  }

  function getActiveProjectPath() {
    var params = new URLSearchParams(window.location.search);
    var p = (params.get('project') || params.get('workspace') || '').trim();
    if (!p || p === 'outside-of-project') return '';
    if (/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(p)) return '';
    if (/^file:\/\//i.test(p)) {
      p = p.slice(7).replace(/(?:%[0-9a-fA-F]{2})+/g, function(seq) {
        try { return decodeURIComponent(seq); } catch (_) { return seq; }
      });
      if (/^localhost\//i.test(p)) p = p.slice(9);
      else if (/^localhost$/i.test(p)) p = '/';
      else if (p && p[0] !== '/' && p[0] !== '~' && !/^[a-zA-Z][:|]/.test(p)) p = '//' + p;
      if (/^\/[a-zA-Z][:|]/.test(p)) p = p[1] + ':' + p.slice(3);
      else if (/^[a-zA-Z]\|/.test(p)) p = p[0] + ':' + p.slice(2);
    }
    return p;
  }

  var pendingPromptPaths = [];
  var pendingFlushTimer = null;

  function isComposerSubmitButton(btn) {
    if (!btn || btn.disabled) return false;
    if (typeof btn.getAttribute === 'function') {
      if (btn.getAttribute('aria-haspopup') || btn.getAttribute('aria-expanded') || btn.getAttribute('aria-disabled') === 'true') {
        return false;
      }
    }
    var label = (
      (typeof btn.getAttribute === 'function' ? (btn.getAttribute('aria-label') || btn.getAttribute('data-testid') || '') : '') +
      ' ' + (btn.title || '') + ' ' + (btn.textContent || '')
    ).trim();
    if (label && /attach|upload|add|file|folder|image|media|model|voice|mic|record|dictat|cancel|stop|close|dismiss|view|mode|permission|agent|branch|worktree|env/i.test(label)) {
      return false;
    }
    return true;
  }

  function schedulePostSubmitConvoSync() {
    if (typeof setTimeout === 'function') {
      setTimeout(flushPendingPromptPaths, 120);
      setTimeout(flushPendingPromptPaths, 600);
    }
  }

  if (typeof document.addEventListener === 'function') {
    document.addEventListener('keydown', function(e) {
      if (!activeTempConvoId || !e || e.key !== 'Enter' || e.shiftKey || e.altKey || e.isComposing || e.keyCode === 229) return;
      var t = e.target;
      if (t && t.closest && t.closest('[data-testid="agent-input-box"]')) {
        submittingTempConvoId = activeTempConvoId;
        lastComposerSubmitAt = Date.now();
        schedulePostSubmitConvoSync();
      }
    }, true);
    document.addEventListener('click', function(e) {
      if (!activeTempConvoId || !e) return;
      var t = e.target;
      var box = t && t.closest && t.closest('[data-testid="agent-input-box"]');
      if (!box) {
        submittingTempConvoId = '';
        return;
      }
      var btn = t.closest('[data-testid="agent-input-box"] button');
      if (btn && isComposerSubmitButton(btn)) {
        submittingTempConvoId = activeTempConvoId;
        lastComposerSubmitAt = Date.now();
        schedulePostSubmitConvoSync();
      }
    }, true);
  }

  function findComposerEditor() {
    if (typeof document.querySelector !== 'function') return null;
    return document.querySelector('[data-testid="agent-input-box"] [contenteditable="true"]') ||
           document.querySelector('[data-lexical-editor="true"][contenteditable="true"]');
  }

  function readLexicalRootText(editor) {
    if (!editor) return null;
    try {
      var st = editor._pendingEditorState ||
               (typeof editor.getEditorState === 'function' ? editor.getEditorState() : null);
      var root = st && st._nodeMap && typeof st._nodeMap.get === 'function' ? st._nodeMap.get('root') : null;
      if (root && typeof root.getTextContent === 'function') {
        return root.getTextContent();
      }
    } catch (_) {}
    return null;
  }

  function insertIntoComposerElement(el, path) {
    if (!el || !path) return false;
    var curText = typeof el.innerText === 'string' ? el.innerText : (el.textContent || '');
    var prefix = (curText && !/\s$/.test(curText)) ? ' ' : '';
    var textToInsert = prefix + path + ' ';

    if (el.__lexicalEditor) {
      var inserted = false;
      if (typeof el.__lexicalEditor.update === 'function') {
        try {
          el.__lexicalEditor.update(function() {
            var state = el.__lexicalEditor._pendingEditorState ||
                        (typeof el.__lexicalEditor.getEditorState === 'function' ? el.__lexicalEditor.getEditorState() : null);
            var root = state && state._nodeMap && typeof state._nodeMap.get === 'function' ? state._nodeMap.get('root') : null;
            if (!root) return;
            var rootText = typeof root.getTextContent === 'function' ? root.getTextContent() : curText;
            var lexPrefix = (rootText && !/\s$/.test(rootText)) ? ' ' : '';
            var lexText = lexPrefix + path + ' ';
            var sel = null;
            var node = typeof root.getLastChild === 'function' ? root.getLastChild() : null;
            while (node) {
              if (typeof node.selectEnd === 'function') {
                var candidate = node.selectEnd();
                if (candidate && typeof candidate.insertText === 'function') {
                  sel = candidate;
                  break;
                }
              }
              node = typeof node.getPreviousSibling === 'function' ? node.getPreviousSibling() : null;
            }
            if ((!sel || typeof sel.insertText !== 'function') && typeof root.selectEnd === 'function') {
              var rootSel = root.selectEnd();
              if (rootSel && typeof rootSel.insertText === 'function') {
                sel = rootSel;
              }
            }
            if (!sel || typeof sel.insertText !== 'function') {
              sel = state._selection;
            }
            if (sel && typeof sel.insertText === 'function') {
              sel.insertText(lexText);
              inserted = true;
            }
          }, { discrete: true });
        } catch (_) {}
      }

      if (!inserted) {
        var beforeText = readLexicalRootText(el.__lexicalEditor);
        try {
          if (typeof el.__lexicalEditor.focus === 'function') {
            el.__lexicalEditor.focus();
          }
        } catch (_) {}
        try {
          if (typeof el.focus === 'function') {
            el.focus();
          }
        } catch (_) {}
        if (typeof InputEvent === 'function' && typeof el.dispatchEvent === 'function') {
          try {
            var bi = new InputEvent('beforeinput', {
              bubbles: true,
              cancelable: true,
              inputType: 'insertReplacementText',
              data: textToInsert
            });
            el.dispatchEvent(bi);
            var afterText = readLexicalRootText(el.__lexicalEditor);
            if (bi.defaultPrevented && (afterText === null || afterText !== beforeText)) {
              inserted = true;
            }
          } catch (_) {}
        }
      } else {
        try {
          var alreadyFocused = document.activeElement === el ||
            Boolean(el.contains && document.activeElement && el.contains(document.activeElement));
          if (alreadyFocused && (!document.hasFocus || document.hasFocus()) && typeof el.__lexicalEditor.focus === 'function') {
            el.__lexicalEditor.focus();
          }
        } catch (_) {}
      }

      return Boolean(inserted);
    }

    try {
      if (typeof el.focus === 'function') {
        el.focus();
      }
    } catch (_) {}
    try {
      if (typeof document.execCommand === 'function' && document.execCommand('insertText', false, textToInsert)) {
        return true;
      }
    } catch (_) {}

    el.innerText = (el.innerText ? el.innerText.replace(/\s*$/, ' ') : '') + path + ' ';
    if (typeof el.dispatchEvent === 'function' && typeof Event === 'function') {
      el.dispatchEvent(new Event('input', { bubbles: true }));
    }
    return true;
  }

  function flushPendingPromptPaths() {
    if (activeTempConvoId && readExplicitUrlConvoId()) {
      getActiveConversationId();
    }
    var now = Date.now();
    pendingPromptPaths = pendingPromptPaths.filter(function(item) {
      return !item.createdAt || (now - item.createdAt <= PENDING_PATH_TTL_MS);
    });
    if (!pendingPromptPaths.length) {
      if (pendingFlushTimer) {
        clearInterval(pendingFlushTimer);
        pendingFlushTimer = null;
      }
      return;
    }
    var currentConvo = getActiveConversationId();
    var el = findComposerEditor();
    if (!el) return;

    var remaining = [];
    for (var i = 0; i < pendingPromptPaths.length; i++) {
      var item = pendingPromptPaths[i];
      var sameConvo = !item.convoId || item.convoId === currentConvo;
      if (sameConvo) {
        if (!insertIntoComposerElement(el, item.path)) {
          remaining.push(item);
        }
      } else {
        remaining.push(item);
      }
    }
    pendingPromptPaths = remaining;
    if (!pendingPromptPaths.length && pendingFlushTimer) {
      clearInterval(pendingFlushTimer);
      pendingFlushTimer = null;
    }
  }

  function enqueuePendingPromptPath(convoId, path) {
    pendingPromptPaths.push({ convoId: convoId || '', path: path, createdAt: Date.now() });
    if (!pendingFlushTimer && typeof setInterval === 'function') {
      pendingFlushTimer = setInterval(flushPendingPromptPaths, 500);
    }
  }

  function handleRouteChangeSync() {
    getActiveConversationId();
    flushPendingPromptPaths();
    if (typeof setTimeout === 'function') {
      setTimeout(flushPendingPromptPaths, 60);
    }
  }

  if (typeof window.history === 'object' && window.history && !window.history.__agyRouteHooked) {
    try {
      window.history.__agyRouteHooked = true;
      ['pushState', 'replaceState'].forEach(function(method) {
        var orig = window.history[method];
        if (typeof orig === 'function') {
          window.history[method] = function() {
            var ret = orig.apply(this, arguments);
            try { handleRouteChangeSync(); } catch (_) {}
            return ret;
          };
        }
      });
    } catch (_) {}
  }

  if (typeof window.addEventListener === 'function') {
    window.addEventListener('focus', flushPendingPromptPaths);
    window.addEventListener('popstate', handleRouteChangeSync);
  }
  if (typeof document.addEventListener === 'function') {
    document.addEventListener('visibilitychange', function() {
      if (!document.hidden) flushPendingPromptPaths();
    });
  }

  function insertPromptPath(path, targetConvoId) {
    if (!path) return false;
    var currentConvo = getActiveConversationId();
    if (targetConvoId && tempToRealConvoId[targetConvoId]) {
      targetConvoId = tempToRealConvoId[targetConvoId];
    }
    if (targetConvoId && targetConvoId !== currentConvo) {
      enqueuePendingPromptPath(targetConvoId, path);
      return false;
    }
    var el = findComposerEditor();
    if (el && insertIntoComposerElement(el, path)) {
      return true;
    }
    enqueuePendingPromptPath(targetConvoId || currentConvo, path);
    return false;
  }
  window.__agyInsertPromptPath = insertPromptPath;
  window.__agyFlushPendingPaths = flushPendingPromptPaths;

  var DEFAULT_CHUNK_SIZE = 48 * 1024 * 1024;
  var MAX_CHUNKS = 10000;
  var MAX_CHUNK_RETRIES = 3;
  var CHUNK_TIMEOUT_MS = 180000;

  function uploadSingleFile(file) {
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

    var chunkSize = (typeof window.__agyChunkSize === 'number' && window.__agyChunkSize > 0)
      ? window.__agyChunkSize
      : DEFAULT_CHUNK_SIZE;
    if (file.size > chunkSize * MAX_CHUNKS) {
      chunkSize = Math.ceil(file.size / MAX_CHUNKS);
    }
    var totalChunks = (file.size > chunkSize) ? Math.ceil(file.size / chunkSize) : 1;
    var uploadId = 'up_' + Date.now().toString(36) + '_' + Math.random().toString(36).slice(2, 10);
    var startTime = Date.now();

    function showError(msg) {
      bar.style.background = '#ef4444';
      pctSpan.textContent = 'Failed';
      pctSpan.style.color = '#ef4444';
      speedSpan.textContent = msg;
      setTimeout(function() { if (typeof card.remove === 'function') card.remove(); }, 5000);
    }

    function abortChunkedUpload() {
      if (totalChunks <= 1) return;
      try {
        var abortXhr = new XMLHttpRequest();
        var qs = 'uploadId=' + encodeURIComponent(uploadId) +
                 '&conversationId=' + encodeURIComponent(convoId) +
                 '&projectPath=' + encodeURIComponent(projectPath);
        abortXhr.open('DELETE', '/__agy/api/upload?' + qs);
        abortXhr.send();
      } catch (_) {}
    }

    function failUpload(msg) {
      abortChunkedUpload();
      showError(msg);
    }

    function isRetryableStatus(status) {
      return status === 408 || status === 429 || (status >= 500 && status < 600);
    }

    function scheduleRetry(chunkIndex, nextAttempt) {
      var baseDelay = (typeof window.__agyRetryDelayMs === 'number' && window.__agyRetryDelayMs >= 0)
        ? window.__agyRetryDelayMs
        : 800;
      speedSpan.textContent = 'Retrying (' + nextAttempt + '/' + MAX_CHUNK_RETRIES + ')...';
      setTimeout(function() {
        sendChunk(chunkIndex, nextAttempt);
      }, baseDelay * nextAttempt);
    }

    function sendChunk(chunkIndex, attempt) {
      var chunkOffset = chunkIndex * chunkSize;
      var chunkEnd = Math.min(file.size, chunkOffset + chunkSize);
      var blob = (totalChunks > 1 && typeof file.slice === 'function')
        ? file.slice(chunkOffset, chunkEnd)
        : file;

      var formData = new FormData();
      formData.append('conversationId', convoId);
      formData.append('projectPath', projectPath);
      if (totalChunks > 1) {
        formData.append('uploadId', uploadId);
        formData.append('chunkIndex', String(chunkIndex));
        formData.append('totalChunks', String(totalChunks));
        formData.append('chunkOffset', String(chunkOffset));
        formData.append('fileName', file.name);
      }
      formData.append('file', blob, file.name);

      var xhr = new XMLHttpRequest();
      if (!xhr.upload) xhr.upload = {};

      xhr.upload.onprogress = function(e) {
        if (e && e.lengthComputable) {
          var chunkBytes = chunkEnd - chunkOffset;
          var loadedInChunk = e.total > 0 ? Math.min(chunkBytes, Math.round((e.loaded / e.total) * chunkBytes)) : e.loaded;
          var overallLoaded = Math.min(file.size, chunkOffset + loadedInChunk);
          var pct = file.size > 0 ? Math.min(100, Math.round((overallLoaded / file.size) * 100)) : 100;
          bar.style.width = pct + '%';
          pctSpan.textContent = pct + '%';
          bytesSpan.textContent = formatBytes(overallLoaded) + ' / ' + formatBytes(file.size);
          var elapsed = (Date.now() - startTime) / 1000;
          if (elapsed > 0.4) {
            var speed = overallLoaded / elapsed;
            speedSpan.textContent = formatBytes(speed) + '/s';
          }
        }
      };

      xhr.onload = function() {
        if (xhr.status === 200) {
          try {
            var res = JSON.parse(xhr.responseText);
            if (!res || res.success === false) {
              failUpload((res && res.error) ? res.error : 'Upload failed');
              return;
            }
            if (chunkIndex + 1 < totalChunks) {
              var completedBytes = chunkEnd;
              var pct = file.size > 0 ? Math.min(100, Math.round((completedBytes / file.size) * 100)) : 100;
              bar.style.width = pct + '%';
              pctSpan.textContent = pct + '%';
              bytesSpan.textContent = formatBytes(completedBytes) + ' / ' + formatBytes(file.size);
              sendChunk(chunkIndex + 1, 0);
              return;
            }
            bar.style.width = '100%';
            bar.style.background = '#22c55e';
            pctSpan.textContent = 'Completed';
            pctSpan.style.color = '#22c55e';
            bytesSpan.textContent = formatBytes(file.size) + ' / ' + formatBytes(file.size);
            speedSpan.textContent = res.relativePath;
            
            insertPromptPath(res.relativePath, convoId);

            setTimeout(function() {
              card.style.opacity = '0';
              card.style.transform = 'translateY(6px)';
              setTimeout(function() { if (typeof card.remove === 'function') card.remove(); }, 200);
            }, 2500);
          } catch (err) {
            failUpload('Invalid server response');
          }
        } else if (isRetryableStatus(xhr.status) && attempt < MAX_CHUNK_RETRIES) {
          scheduleRetry(chunkIndex, attempt + 1);
        } else {
          failUpload('Upload failed (' + xhr.status + ')');
        }
      };

      xhr.onerror = function() {
        if (attempt < MAX_CHUNK_RETRIES) {
          scheduleRetry(chunkIndex, attempt + 1);
          return;
        }
        failUpload('Network error');
      };

      xhr.ontimeout = function() {
        if (attempt < MAX_CHUNK_RETRIES) {
          scheduleRetry(chunkIndex, attempt + 1);
          return;
        }
        failUpload('Request timed out');
      };

      var postQs = 'conversationId=' + encodeURIComponent(convoId) +
                   '&projectPath=' + encodeURIComponent(projectPath);
      if (totalChunks > 1) {
        postQs += '&uploadId=' + encodeURIComponent(uploadId) +
                  '&chunkIndex=' + encodeURIComponent(String(chunkIndex)) +
                  '&totalChunks=' + encodeURIComponent(String(totalChunks)) +
                  '&chunkOffset=' + encodeURIComponent(String(chunkOffset)) +
                  '&fileName=' + encodeURIComponent(file.name);
      }

      xhr.timeout = CHUNK_TIMEOUT_MS;
      xhr.open('POST', '/__agy/api/upload?' + postQs);
      xhr.send(formData);
    }

    sendChunk(0, 0);
  }

  var handledFiles = typeof WeakSet !== 'undefined' ? new WeakSet() : null;
  if (handledFiles) {
    window.__agyHandledFiles = handledFiles;
  }
  var handledFingerprints = typeof Map !== 'undefined' ? new Map() : null;
  var reservedFingerprints = typeof Map !== 'undefined' ? new Map() : null;
  var activeMediaCount = 0;
  var maxMediaSlots = 5;
  var cumulativeNativeBytes = 0;
  var inFlightNativeUntil = 0;
  var syncedByIcb = false;

  function fileFingerprint(f) {
    if (!f) return '';
    return String(f.name || '') + '\0' + String(typeof f.size === 'number' ? f.size : 0) + '\0' + String(f.lastModified || 0);
  }

  function markFileHandled(f) {
    if (!f) return;
    if (handledFiles) {
      try { handledFiles.add(f); } catch (_) {}
    }
    if (handledFingerprints) {
      var fp = fileFingerprint(f);
      if (fp) handledFingerprints.set(fp, Date.now());
    }
  }

  function isFileHandled(f) {
    if (!f) return false;
    if (handledFiles && handledFiles.has(f)) return true;
    if (handledFingerprints) {
      var fp = fileFingerprint(f);
      var ts = fp ? handledFingerprints.get(fp) : 0;
      if (ts) {
        if (Date.now() - ts < 3000) return true;
        handledFingerprints.delete(fp);
      }
    }
    return false;
  }
  window.__agyIsFileHandled = isFileHandled;

  window.__agySyncMediaState = function(count, maxSlots, forceReset) {
    syncedByIcb = true;
    if (typeof count === 'number') {
      var prevCount = activeMediaCount;
      activeMediaCount = Math.max(0, count);
      if (activeMediaCount > 0) {
        inFlightNativeUntil = 0;
      } else if (forceReset || prevCount > 0 || Date.now() >= inFlightNativeUntil) {
        cumulativeNativeBytes = 0;
        inFlightNativeUntil = 0;
        if (reservedFingerprints) reservedFingerprints.clear();
      }
    }
    if (typeof maxSlots === 'number' && maxSlots > 0) {
      maxMediaSlots = maxSlots;
    }
  };

  window.__agyUpload = function(files) {
    if (!files || files.length === 0) return;
    for (var i = 0; i < files.length; i++) {
      var f = files[i];
      if (!f) continue;
      markFileHandled(f);
      uploadSingleFile(f);
    }
  };

  var hiddenFileInput = null;
  window.__agyTriggerUpload = function() {
    if (!hiddenFileInput) {
      hiddenFileInput = document.createElement('input');
      hiddenFileInput.type = 'file';
      hiddenFileInput.multiple = true;
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

  var NATIVE_IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/gif', 'image/svg+xml', 'image/webp'];
  var TEXT_MIME_TYPES = ['text/plain', 'text/csv', 'text/markdown', 'text/html', 'text/css', 'text/javascript', 'text/x-typescript', 'text/x-python', 'application/json'];
  var TEXT_EXTS = ['txt', 'csv', 'md', 'markdown', 'html', 'htm', 'css', 'js', 'mjs', 'cjs', 'ts', 'tsx', 'py', 'json', 'har', 'jsonl', 'log', 'yaml', 'yml', 'toml', 'xml', 'sql', 'sh', 'bash', 'zsh', 'go', 'rs', 'c', 'cpp', 'cc', 'h', 'hpp', 'java', 'kt', 'swift', 'rb', 'php', 'env', 'ini', 'conf', 'cfg', 'dockerfile', 'makefile', 'graphql', 'proto', 'vue', 'svelte', 'astro'];
  var DOC_MIME_TYPES = [
    'application/pdf',
    'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    'application/vnd.openxmlformats-officedocument.presentationml.presentation',
    'application/msword',
    'application/vnd.ms-excel',
    'application/vnd.ms-powerpoint'
  ];
  var DOC_EXTS = ['docx', 'xlsx', 'pptx', 'doc', 'xls', 'ppt'];

  function getFileExt(name) {
    if (!name || typeof name !== 'string') return '';
    var base = name.replace(/\\/g, '/').split('/').pop().toLowerCase();
    if (base === 'dockerfile' || base === 'makefile') return base;
    var idx = base.lastIndexOf('.');
    return idx >= 0 ? base.slice(idx + 1) : '';
  }

  function getNormalizedMime(f) {
    if (!f || !f.type) return '';
    return f.type.split(';')[0].trim().toLowerCase();
  }

  function isNativeImageFile(f) {
    if (!f) return false;
    var mime = getNormalizedMime(f);
    return mime !== '' && NATIVE_IMAGE_TYPES.indexOf(mime) !== -1;
  }

  function estimateNativeBytes(f) {
    if (!f) return 0;
    var size = typeof f.size === 'number' ? f.size : 0;
    return isNativeImageFile(f) ? Math.min(size, 1048576) : size;
  }

  function isWithinNativeLimit(f) {
    if (!f) return false;
    if (isNativeImageFile(f)) return true;
    var size = typeof f.size === 'number' ? f.size : 0;
    var mime = getNormalizedMime(f);
    var ext = getFileExt(f.name);
    if (DOC_MIME_TYPES.indexOf(mime) !== -1 || DOC_EXTS.indexOf(ext) !== -1) {
      return size <= 10485760;
    }
    if (TEXT_MIME_TYPES.indexOf(mime) !== -1 || mime.indexOf('text/') === 0 || TEXT_EXTS.indexOf(ext) !== -1) {
      return size <= 1048576;
    }
    return size <= 20971520;
  }

  window.__agyShouldStreamSingle = function(f, currentCount, maxSlots) {
    if (!f) return false;
    if (isFileHandled(f)) return true;
    var now = Date.now();
    if (typeof currentCount === 'number') {
      var prevCount = activeMediaCount;
      activeMediaCount = Math.max(0, currentCount);
      if (activeMediaCount === 0 && (prevCount > 0 || (now >= inFlightNativeUntil && (!reservedFingerprints || reservedFingerprints.size === 0)))) {
        cumulativeNativeBytes = 0;
        inFlightNativeUntil = 0;
      }
    }
    if (typeof maxSlots === 'number' && maxSlots > 0) {
      maxMediaSlots = maxSlots;
    }
    var fp = fileFingerprint(f);
    var rTs = reservedFingerprints && fp ? reservedFingerprints.get(fp) : 0;
    if (rTs) {
      reservedFingerprints.delete(fp);
      if (now - rTs < 2000) {
        inFlightNativeUntil = now + 3000;
        return false;
      }
    }
    var est = estimateNativeBytes(f);
    if (!isWithinNativeLimit(f) || activeMediaCount >= maxMediaSlots || cumulativeNativeBytes + est > 20971520) {
      return true;
    }
    cumulativeNativeBytes += est;
    inFlightNativeUntil = now + 3000;
    return false;
  };

  window.__agyPartitionFiles = function(fileList, maxSlots, currentCount) {
    if (!fileList || fileList.length === 0) return [];
    var now = Date.now();
    if (typeof currentCount === 'number') {
      var prevCount = activeMediaCount;
      activeMediaCount = Math.max(0, currentCount);
      if (activeMediaCount === 0 && (prevCount > 0 || (now >= inFlightNativeUntil && (!reservedFingerprints || reservedFingerprints.size === 0)))) {
        cumulativeNativeBytes = 0;
        inFlightNativeUntil = 0;
      }
    }
    var limitSlots = typeof maxSlots === 'number' ? Math.max(0, maxSlots) : Math.max(0, maxMediaSlots - activeMediaCount);
    var nativeFiles = [];
    var streamingFiles = [];
    for (var i = 0; i < fileList.length; i++) {
      var f = fileList[i];
      if (isFileHandled(f)) continue;
      var fp = fileFingerprint(f);
      var rTs = reservedFingerprints && fp ? reservedFingerprints.get(fp) : 0;
      var wasReserved = Boolean(rTs && now - rTs < 2000);
      if (rTs) reservedFingerprints.delete(fp);
      var est = estimateNativeBytes(f);
      if (wasReserved) {
        if (nativeFiles.length < limitSlots) {
          nativeFiles.push(f);
          inFlightNativeUntil = now + 3000;
          if (reservedFingerprints && fp) {
            reservedFingerprints.set(fp, now);
          }
        } else {
          cumulativeNativeBytes = Math.max(0, cumulativeNativeBytes - est);
          streamingFiles.push(f);
        }
        continue;
      }
      if (isWithinNativeLimit(f) && nativeFiles.length < limitSlots && cumulativeNativeBytes + est <= 20971520) {
        nativeFiles.push(f);
        cumulativeNativeBytes += est;
        inFlightNativeUntil = now + 3000;
        if (reservedFingerprints && fp) {
          reservedFingerprints.set(fp, now);
        }
      } else {
        streamingFiles.push(f);
      }
    }
    if (streamingFiles.length > 0) {
      for (var k = 0; k < streamingFiles.length; k++) {
        markFileHandled(streamingFiles[k]);
      }
      if (window.__agyUpload) {
        window.__agyUpload(streamingFiles);
      }
    }
    return nativeFiles;
  };

  function dismissNativeDragOverlays() {
    window.dispatchEvent(new MouseEvent('mouseup'));
    try {
      window.dispatchEvent(new DragEvent('dragleave', { relatedTarget: null }));
    } catch (_) {
      var leaveEvent = new Event('dragleave');
      try {
        Object.defineProperty(leaveEvent, 'relatedTarget', { value: null });
      } catch (_) {
        leaveEvent.relatedTarget = null;
      }
      window.dispatchEvent(leaveEvent);
    }
  }

  // Drag and drop support using Antigravity native UI
  window.addEventListener('dragover', function(e) {
    if (e.dataTransfer && Array.from(e.dataTransfer.types || []).includes('Files')) {
      e.preventDefault();
      e.dataTransfer.dropEffect = 'copy';
    }
  }, true);

  window.addEventListener('drop', function(e) {
    var files = e.dataTransfer && e.dataTransfer.files;
    if (!files || files.length === 0) return;

    // Immediately reset native drag overlay state for file drops
    window.dispatchEvent(new MouseEvent('mouseup'));

    var dirNames = {};
    var items = e.dataTransfer && e.dataTransfer.items;
    if (items) {
      for (var j = 0; j < items.length; j++) {
        try {
          var item = items[j];
          if (item && item.kind === 'file' && typeof item.webkitGetAsEntry === 'function') {
            var entry = item.webkitGetAsEntry();
            if (entry && entry.isDirectory && entry.name) {
              dirNames[entry.name] = true;
            }
          }
        } catch (_) {}
      }
    }

    var streamingFiles = [];
    var hasNativeFile = false;
    var nativeCount = 0;
    var now = Date.now();
    if (!syncedByIcb || (activeMediaCount === 0 && now >= inFlightNativeUntil)) {
      cumulativeNativeBytes = 0;
    }
    var remainingSlots = Math.max(0, maxMediaSlots - activeMediaCount);
    for (var i = 0; i < files.length; i++) {
      var f = files[i];
      if (dirNames[f.name]) {
        hasNativeFile = true;
        continue;
      }
      var est = estimateNativeBytes(f);
      if (isWithinNativeLimit(f) && nativeCount < remainingSlots && cumulativeNativeBytes + est <= 20971520) {
        hasNativeFile = true;
        nativeCount++;
        cumulativeNativeBytes += est;
        inFlightNativeUntil = now + 3000;
        if (reservedFingerprints) {
          var fp = fileFingerprint(f);
          if (fp) reservedFingerprints.set(fp, now);
        }
      } else {
        streamingFiles.push(f);
      }
    }

    if (streamingFiles.length > 0) {
      for (var k = 0; k < streamingFiles.length; k++) {
        markFileHandled(streamingFiles[k]);
      }
      if (!hasNativeFile) {
        e.preventDefault();
        e.stopPropagation();
        if (typeof e.stopImmediatePropagation === 'function') {
          e.stopImmediatePropagation();
        }
        dismissNativeDragOverlays();
      }
      if (window.__agyUpload) {
        window.__agyUpload(streamingFiles);
      }
    }
  }, true);
})();
</script>`

const lineStartNavScript = `<script id="agy-line-start-nav">
(function() {
  var isMac = typeof navigator !== 'undefined' && 
    Boolean((navigator.userAgentData && navigator.userAgentData.platform === 'macOS') ||
            (navigator.platform && navigator.platform.toUpperCase().indexOf('MAC') >= 0) ||
            (navigator.userAgent && /Macintosh|Mac OS X/.test(navigator.userAgent)));

  window.addEventListener('keydown', function(e) {
    if (e.key !== 'ArrowLeft' && e.key !== 'Home') return;

    var active = document.activeElement;
    if (!active || !active.isContentEditable) return;

    // Only intercept when decorators (slash commands, mentions, chips) are present
    if (!active.querySelector('[data-lexical-decorator="true"]')) return;

    var sel = window.getSelection();
    if (!sel || !sel.rangeCount || typeof sel.modify !== 'function') return;

    var alter = e.shiftKey ? 'extend' : 'move';

    // Case 1: Pure Home (all platforms) -> move to line start
    if (e.key === 'Home' && !e.ctrlKey && !e.metaKey && !e.altKey) {
      try {
        sel.modify(alter, 'backward', 'lineboundary');
        e.preventDefault();
        e.stopPropagation();
      } catch (_) {}
      return;
    }

    // Case 2: Cmd + ArrowLeft on macOS -> move to line start
    if (isMac && e.key === 'ArrowLeft' && e.metaKey && !e.ctrlKey && !e.altKey) {
      try {
        sel.modify(alter, 'backward', 'lineboundary');
        e.preventDefault();
        e.stopPropagation();
      } catch (_) {}
      return;
    }

    // Case 3: Ctrl + ArrowLeft on Non-Mac (Windows/Linux)
    // Upstream Lexical's MOVE_TO_START mistakenly triggers on Ctrl+Left on non-Mac
    // and jumps to position 0 when decorators exist. Restore native word navigation.
    if (!isMac && e.key === 'ArrowLeft' && e.ctrlKey && !e.metaKey && !e.altKey) {
      try {
        sel.modify(alter, 'backward', 'word');
        e.preventDefault();
        e.stopPropagation();
      } catch (_) {}
      return;
    }
  }, true);
})();
</script>`

const connectionWatchdogScript = `<script id="agy-connection-watchdog">
(function () {
  var lastPingSuccess = 0;
  var activePingPromise = null;
  var MAX_RELOAD_ATTEMPTS = 2;
  var lastNetworkActivity = Date.now();

  // Track fetch activity, including chunks read through response.body.getReader()
  // as streaming RPC clients do, so we never interrupt in-progress downloads
  if (window.fetch && !window.__agyFetchActivityTracked) {
    window.__agyFetchActivityTracked = true;
    var _origFetchForWatchdog = window.fetch;
    window.fetch = function () {
      lastNetworkActivity = Date.now();
      var p = _origFetchForWatchdog.apply(this, arguments);
      if (p && p.then) {
        return p.then(function (res) {
          lastNetworkActivity = Date.now();
          if (res && res.body && typeof res.body.getReader === "function") {
            var _origGetReader = res.body.getReader.bind(res.body);
            res.body.getReader = function () {
              var reader = _origGetReader.apply(this, arguments);
              if (reader && typeof reader.read === "function") {
                var _origRead = reader.read.bind(reader);
                reader.read = function () {
                  return _origRead.apply(this, arguments).then(function (chunk) {
                    lastNetworkActivity = Date.now();
                    return chunk;
                  }, function (err) {
                    lastNetworkActivity = Date.now();
                    throw err;
                  });
                };
              }
              return reader;
            };
          }
          return res;
        }, function (err) {
          lastNetworkActivity = Date.now();
          throw err;
        });
      }
      return p;
    };
  }

  // The RPC transport may run over a single WebSocket instead of fetch, so
  // count inbound data frames as network activity too. The transport probes
  // an idle socket with a ping; small liveness frames like the replies are
  // skipped so a healthy socket with a stuck view still lets the watchdog fire.
  if (window.WebSocket && window.Proxy && window.Reflect && !window.__agyWsActivityTracked) {
    window.__agyWsActivityTracked = true;
    var wsControlFrameRe = /"type"\s*:\s*"(?:ping|pong|heartbeat|keepalive)"/;
    var onWsMessage = function (e) {
      var d = e && e.data;
      if (typeof d === "string" && d.length < 512 && wsControlFrameRe.test(d)) return;
      lastNetworkActivity = Date.now();
    };
    var onWsOpen = function () { lastNetworkActivity = Date.now(); };
    window.WebSocket = new Proxy(window.WebSocket, {
      construct: function (target, args, newTarget) {
        var ws = Reflect.construct(target, args, newTarget);
        lastNetworkActivity = Date.now();
        ws.addEventListener("message", onWsMessage);
        ws.addEventListener("open", onWsOpen);
        return ws;
      }
    });
  }

  function pingServer(onSuccess, onError, force) {
    var now = Date.now();
    if (!force && (now - lastPingSuccess < 3000)) {
      if (onSuccess) onSuccess();
      return;
    }

    if (!activePingPromise) {
      activePingPromise = fetch("/__agy/api/signin/status", { credentials: "same-origin", cache: "no-store" })
        .then(function (r) {
          if (!r.ok) throw new Error("HTTP " + r.status);
          return r.json().catch(function () { return {}; });
        })
        .then(function (data) {
          activePingPromise = null;
          if (data && data.available === false) {
            throw new Error("Language server unavailable");
          }
          lastPingSuccess = Date.now();
        })
        .catch(function (err) {
          activePingPromise = null;
          throw err;
        });
    }

    activePingPromise
      .then(function () {
        if (onSuccess) onSuccess();
      })
      .catch(function (err) {
        if (onError) onError(err);
      });
  }

  // 1. Auto-dismiss "Lost connection" banner ONLY when server is verified alive
  function checkAndDismissLostConnectionBanner() {
    var banners = document.querySelectorAll('div[data-testid="feature-banner"]');
    if (!banners || banners.length === 0) return;

    banners.forEach(function (b) {
      var text = (b.textContent || "").toLowerCase();
      if (text.indexOf("lost connection") !== -1 ||
          text.indexOf("reconnecting") !== -1 ||
          text.indexOf("연결이 끊") !== -1 ||
          text.indexOf("연결 끊") !== -1 ||
          text.indexOf("재연결") !== -1 ||
          text.indexOf("연결할 수 없") !== -1) {
        // Probe server actively; dismiss if OK, restore banner if connection actually down
        pingServer(function () {
          if (b.style.display !== "none") {
            b.style.setProperty("display", "none", "important");
          }
        }, function () {
          if (b.style.display === "none") {
            b.style.removeProperty("display");
          }
        });
      }
    });
  }

  // 2. Watchdog: Recover if conversation loading spinner is stuck > 30s AND network is completely idle (>5s)
  var stuckTimerStart = 0;
  var currentPath = window.location.pathname;

  function checkConversationSpinnerStuck() {
    if (window.location.pathname.indexOf("/c/") !== 0) {
      stuckTimerStart = 0;
      return;
    }

    if (window.location.pathname !== currentPath) {
      currentPath = window.location.pathname;
      stuckTimerStart = 0;
    }

    var convoView = document.querySelector('div[data-testid="conversation-view"]');
    if (!convoView) {
      stuckTimerStart = 0;
      return;
    }

    var spinner = convoView.querySelector('.animate-spin, [name="progress_activity"]');
    var hasMessages = convoView.querySelector('[data-turn-content], [data-testid="turn-content"], [data-testid="user-input-step"], .turn-content, [data-testid="autoscroll-viewport"] [role="region"], [data-testid="autoscroll-viewport"] > div > div');

    if (hasMessages) {
      // Conversation loaded successfully; reset reload retry circuit breaker
      sessionStorage.removeItem("agy_stuck_reload_count");
      sessionStorage.removeItem("agy_stuck_reload");
      stuckTimerStart = 0;
      return;
    }

    if (spinner && !hasMessages) {
      var now = Date.now();
      // If network communication is actively ongoing (e.g. streaming large conversation/summaries), defer stuck timer
      if (now - lastNetworkActivity < 5000) {
        stuckTimerStart = now;
        return;
      }

      if (!stuckTimerStart) {
        stuckTimerStart = now;
      } else if (now - stuckTimerStart > 30000) {
        // Guard against losing user draft in composer
        var composer = document.querySelector('[contenteditable="true"]');
        if (composer && (composer.textContent || "").trim().length > 0) {
          return;
        }

        // Circuit breaker: stop reloading if maximum attempts reached
        var reloadCount = parseInt(sessionStorage.getItem("agy_stuck_reload_count") || "0", 10);
        var isConvoRoute = window.location.pathname.startsWith("/c/");

        if (reloadCount >= MAX_RELOAD_ATTEMPTS) {
          console.warn("[agy-watchdog] Conversation spinner stuck > 30s, but max reload attempts reached (circuit breaker triggered)");
          if (isConvoRoute) {
            sessionStorage.removeItem("agy_stuck_reload_count");
            sessionStorage.removeItem("agy_stuck_reload");
            window.location.href = "/";
          }
          return;
        }

        var lastReload = parseInt(sessionStorage.getItem("agy_stuck_reload") || "0", 10);
        if (now - lastReload > 30000) {
          // Verify server is alive before reloading; never reload into a dead server!
          pingServer(function () {
            sessionStorage.setItem("agy_stuck_reload", Date.now().toString());
            sessionStorage.setItem("agy_stuck_reload_count", (reloadCount + 1).toString());
            if (reloadCount >= 1 && isConvoRoute) {
              console.warn("[agy-watchdog] Conversation spinner stuck after reload, redirecting to home view");
              sessionStorage.removeItem("agy_stuck_reload_count");
              sessionStorage.removeItem("agy_stuck_reload");
              window.location.href = "/";
              return;
            }
            console.warn("[agy-watchdog] Conversation spinner stuck > 30s with idle network and live server (attempt " + (reloadCount + 1) + "/" + MAX_RELOAD_ATTEMPTS + "), recovering connection via clean reload");
            window.location.reload();
          });
        }
      }
    } else {
      stuckTimerStart = 0;
    }
  }

  // 3. Watchdog: Recover if #root remains completely empty > 15s while visible and network is idle (>5s)
  var emptyRootTimerStart = 0;

  function checkEmptyRootStuck() {
    if (document.visibilityState === "hidden") {
      emptyRootTimerStart = 0;
      return;
    }

    var root = document.getElementById("root");
    if (!root) return;

    if (root.children && root.children.length > 0) {
      if (emptyRootTimerStart || sessionStorage.getItem("agy_empty_root_reload_count")) {
        sessionStorage.removeItem("agy_empty_root_reload_count");
        sessionStorage.removeItem("agy_empty_root_reload");
      }
      emptyRootTimerStart = 0;
      return;
    }

    var now = Date.now();
    if (now - lastNetworkActivity < 5000) {
      emptyRootTimerStart = now;
      return;
    }

    if (!emptyRootTimerStart) {
      emptyRootTimerStart = now;
      return;
    }

    if (now - emptyRootTimerStart > 15000) {
      var reloadCount = parseInt(sessionStorage.getItem("agy_empty_root_reload_count") || "0", 10);
      if (reloadCount >= MAX_RELOAD_ATTEMPTS) {
        return;
      }
      var lastReload = parseInt(sessionStorage.getItem("agy_empty_root_reload") || "0", 10);
      if (now - lastReload > 15000) {
        pingServer(function () {
          sessionStorage.setItem("agy_empty_root_reload", Date.now().toString());
          sessionStorage.setItem("agy_empty_root_reload_count", (reloadCount + 1).toString());
          console.warn("[agy-watchdog] Empty #root stuck > 15s with idle network and live server, recovering via clean reload");
          window.location.reload();
        });
      }
    }
  }

  // Debounced DOM observer via requestAnimationFrame to eliminate streaming layout churn
  var domCheckTimer = null;
  function scheduleDOMCheck() {
    if (domCheckTimer) return;
    domCheckTimer = requestAnimationFrame(function () {
      domCheckTimer = null;
      checkAndDismissLostConnectionBanner();
      checkConversationSpinnerStuck();
      checkEmptyRootStuck();
    });
  }

  var watchdogObserver = new MutationObserver(scheduleDOMCheck);

  function initWatchdog() {
    if (document.body) {
      // childList and subtree are sufficient; omit characterData to prevent token streaming jank
      watchdogObserver.observe(document.body, { childList: true, subtree: true });
    }

    setInterval(function () {
      checkAndDismissLostConnectionBanner();
      checkConversationSpinnerStuck();
      checkEmptyRootStuck();
    }, 1000);

    document.addEventListener("keydown", function (e) {
      if (e.isComposing || e.keyCode === 229) return;
      if (e.key === "Enter") {
        pingServer(checkAndDismissLostConnectionBanner);
      }
    }, true);

    document.addEventListener("visibilitychange", function () {
      if (document.visibilityState === "visible") {
        stuckTimerStart = 0;
        emptyRootTimerStart = 0;
        pingServer(checkAndDismissLostConnectionBanner);
      }
    });

    window.addEventListener("pageshow", function () {
      stuckTimerStart = 0;
      emptyRootTimerStart = 0;
      pingServer(checkAndDismissLostConnectionBanner);
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", initWatchdog);
  } else {
    initWatchdog();
  }
})();
</script>`

// connectionPrewarmScript starts the RPC socket and its warm-up request from the
// document head and hands both to the bundle when it asks for them.
//
// The bundle opens its socket as: fetch(https URL, no-cors), then new WebSocket.
// Both are replayed here. The warm-up request is shared rather than repeated, and
// the socket is returned from the WebSocket constructor in place of a new one.
// A socket the bundle never claims is closed after 120 seconds, and one that
// receives a frame before it is claimed is dropped so no message can be missed.
// Any failure falls back to the bundle opening its own connection.
//
// The bundle ignores the result of its warm-up request and catches a failure of
// it, so sharing a promise that resolves to undefined on error is safe. The
// replacement WebSocket is a plain function rather than a Proxy. It keeps
// instanceof and the static constants, but a subclass of WebSocket would not
// receive the early socket.
const connectionPrewarmScript = `<script id="agy-connection-prewarm">
(function () {
  try {
    if (window.__agyPrewarm || !window.WebSocket || !window.fetch) return;
    window.__agyPrewarm = 1;

    // The bundle reads the same parameters to pick its transport. When it will
    // use fetch it never asks for the socket, so do not open one.
    // Skip prewarming on onboarding or sign-in routes where RPC sockets are never opened.
    var path = location.pathname || "/";
    if (path === "/onboarding" || path.indexOf("/__agy/") === 0) return;

    var query = new URLSearchParams(location.search);
    var useWS = query.get("useWebSocket");
    if (useWS !== null ? useWS !== "true" : query.get("wsTransport") === "1") return;

    var PATH = "/connect-websocket";
    var secure = location.protocol === "https:";
    var wsUrl = (secure ? "wss:" : "ws:") + "//" + location.host + PATH;
    var NativeWS = window.WebSocket;
    var nativeFetch = window.fetch;
    var warm = null;
    var pre = null;
    var claimed = false;
    var expiry = null;
    var createdAt = Date.now();
    var hidden = Boolean(typeof document !== "undefined" && document.visibilityState === "hidden");

    var drop = function () {
      if (claimed) return;
      warm = null;
      if (!pre) return;
      var s = pre;
      pre = null;
      try { s.close(); } catch (e) {}
    };

    if (!hidden && secure) {
      warm = nativeFetch.call(window, location.origin + PATH, { credentials: "include", mode: "no-cors" }).catch(function () {});
    }

    if (!hidden) {
      try {
        pre = new NativeWS(wsUrl);
        pre.addEventListener("close", function () { if (!claimed) pre = null; });
        pre.addEventListener("error", function () { if (!claimed) pre = null; });
        pre.addEventListener("message", drop);
        expiry = setTimeout(drop, 120000);
      } catch (e) {
        pre = null;
      }
    }

    if (typeof document !== "undefined" && document.addEventListener) {
      document.addEventListener("visibilitychange", function () {
        if (document.visibilityState === "hidden") {
          hidden = true;
          drop();
        }
      });
    }
    if (typeof window !== "undefined" && window.addEventListener) {
      window.addEventListener("pagehide", function () {
        hidden = true;
        drop();
      });
    }

    function isFresh() {
      if (claimed || !pre) return false;
      if (hidden || (typeof document !== "undefined" && document.visibilityState === "hidden")) {
        drop();
        return false;
      }
      if (Date.now() - createdAt >= 120000 || pre.readyState > 1) {
        drop();
        return false;
      }
      return true;
    }

    window.fetch = function (input, init) {
      if (warm && init && init.mode === "no-cors" && init.credentials === "include") {
        var url = typeof input === "string" ? input : (input && input.url) || "";
        if (url.indexOf(location.host + PATH) !== -1) {
          var shared = warm;
          warm = null;
          if (isFresh()) {
            return Promise.resolve();
          }
          if (!hidden && Date.now() - createdAt < 120000) {
            return shared;
          }
        }
      }
      return nativeFetch.apply(this || window, arguments);
    };

    function sameUrl(url) {
      try {
        return new URL(String(url), location.href).href === new URL(wsUrl).href;
      } catch (e) {
        return false;
      }
    }

    // The bundle assigns onopen right after construction. A socket that is
    // already open never fires it again, so replay the event once.
    function adopt(ws) {
      try { ws.removeEventListener("message", drop); } catch (e) {}
      var handler = null;
      var fired = false;
      function fire(ev) {
        if (fired || !handler || ws.readyState !== 1) return;
        fired = true;
        try { handler.call(ws, ev); } catch (e) { setTimeout(function () { throw e; }); }
      }
      ws.addEventListener("open", fire);
      var nativeAdd = ws.addEventListener;
      ws.addEventListener = function (type, listener) {
        nativeAdd.apply(this, arguments);
        var call = typeof listener === "function" ? listener
          : listener && typeof listener.handleEvent === "function" ? function (ev) { listener.handleEvent(ev); }
          : null;
        if (type === "open" && ws.readyState === 1 && call) {
          setTimeout(function () {
            if (ws.readyState !== 1) return;
            try { call.call(ws, new Event("open")); } catch (e) { setTimeout(function () { throw e; }); }
          }, 0);
        }
      };
      Object.defineProperty(ws, "onopen", {
        configurable: true,
        enumerable: true,
        get: function () { return handler; },
        set: function (fn) {
          handler = typeof fn === "function" ? fn : null;
          if (handler && ws.readyState === 1 && !fired) {
            setTimeout(function () { fire(new Event("open")); }, 0);
          }
        }
      });
    }

    function WS(url, protocols) {
      if (protocols === undefined && sameUrl(url) && isFresh()) {
        claimed = true;
        clearTimeout(expiry);
        expiry = null;
        var ws = pre;
        pre = null;
        adopt(ws);
        return ws;
      }
      if (!claimed && pre && pre.readyState > 1) {
        drop();
      }
      return protocols === undefined ? new NativeWS(url) : new NativeWS(url, protocols);
    }
    WS.prototype = NativeWS.prototype;
    WS.CONNECTING = 0;
    WS.OPEN = 1;
    WS.CLOSING = 2;
    WS.CLOSED = 3;
    window.WebSocket = WS;
  } catch (e) {}
})();
</script>`

const remoteRuntimeBridgeScript = `<script id="agy-remote-runtime-bridge">
(function () {
  if (window.__agyRemoteRuntimeBridge) return;
  window.__agyRemoteRuntimeBridge = 1;

  function isLoopbackHostname(h) {
    var low = String(h || "").toLowerCase();
    return low === "localhost" || low === "127.0.0.1" || low === "[::1]" || low === "::1";
  }

  // A sidecar frame has an opaque origin, so the app cannot match an
  // add-artifact-comment message's origin to the frame's URL. The proxy has
  // the sidecar's bridge send the gateway token from that URL with the
  // comment, and a page the frame navigated to does not know it.
  window.__agySidecarMessageOK = function (src, data) {
    if (!data || typeof data !== "object" || data.type !== "add-artifact-comment") return false;
    try {
      var u = new URL(src, location.href);
      var token = u.searchParams.get("token");
      return u.origin === location.origin && u.pathname.indexOf("/__agy/sidecar/") === 0 &&
        !!token && data.agySidecarToken === token;
    } catch (e) {
      return false;
    }
  };

  window.__agyResolveSidecarUrl = function (rawUrl) {
    if (typeof rawUrl !== "string" || !rawUrl) return rawUrl;
    if (rawUrl.indexOf("/__agy/sidecar/") === 0) {
      return location.origin + rawUrl;
    }
    try {
      var u = new URL(rawUrl);
      if (u.protocol !== "http:" || !isLoopbackHostname(u.hostname) || !u.port) {
        return rawUrl;
      }
      var port = Number(u.port);
      if (!Number.isInteger(port) || port < 1 || port > 65535) {
        return rawUrl;
      }
      var path = u.pathname || "/";
      if (path.charAt(0) !== "/") path = "/" + path;
      return location.origin + "/__agy/sidecar/" + port + path + u.search + u.hash;
    } catch (e) {
      return rawUrl;
    }
  };

  function encodeBytesBase64(bytes) {
    if (!bytes) return "";
    var u8 = bytes instanceof Uint8Array
      ? bytes
      : (bytes instanceof ArrayBuffer ? new Uint8Array(bytes) : (ArrayBuffer.isView(bytes) ? new Uint8Array(bytes.buffer, bytes.byteOffset, bytes.byteLength) : null));
    if (!u8 || u8.byteLength === 0) return "";
    var parts = [];
    var chunk = 0x8000;
    for (var i = 0; i < u8.length; i += chunk) {
      parts.push(String.fromCharCode.apply(null, u8.subarray(i, i + chunk)));
    }
    return btoa(parts.join(""));
  }

  function decodeBase64Bytes(b64) {
    if (!b64) return new Uint8Array(0);
    var bin = atob(b64);
    var out = new Uint8Array(bin.length);
    for (var i = 0; i < bin.length; i++) {
      out[i] = bin.charCodeAt(i);
    }
    return out;
  }

  function makeConnectError(code, msg) {
    var err = new Error(msg);
    err.name = "ConnectError";
    err.rawMessage = msg;
    err.code = code;
    err.metadata = typeof Headers === "function" ? new Headers() : {};
    err.details = [];
    return err;
  }

  window.__agyFetchLoopbackPort = async function (req, callOpts) {
    if (!req || typeof req !== "object") {
      throw makeConnectError(3, "Invalid loopback fetch request");
    }
    var payload = {
      port: Number(req.port || 0),
      method: typeof req.method === "string" && req.method ? req.method : "GET",
      path: typeof req.path === "string" && req.path ? req.path : "/",
      headers: Array.isArray(req.headers)
        ? req.headers.map(function (h) { return { name: String((h && h.name) || ""), value: String((h && h.value) || "") }; })
        : [],
      bodyBase64: encodeBytesBase64(req.body)
    };
    var res = await fetch("/__agy/api/loopback-fetch", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
      signal: callOpts && callOpts.signal ? callOpts.signal : undefined
    });
    if (res.status === 403) {
      throw makeConnectError(7, "Port is restricted");
    }
    if (!res.ok) {
      throw makeConnectError(13, "Loopback fetch failed with status " + res.status);
    }
    var data = await res.json();
    if (data && data.noListener) {
      throw makeConnectError(5, "No server listening on port " + payload.port);
    }
    return {
      statusCode: Number((data && data.statusCode) || 200),
      headers: Array.isArray(data && data.headers) ? data.headers : [],
      body: decodeBase64Bytes(data && data.bodyBase64)
    };
  };

  window.__agyOpenPluginAuth = function (pluginPath) {
    if (!pluginPath || typeof pluginPath !== "string") return;
    var clean = pluginPath;
    if (/^file:/i.test(clean)) {
      try {
        clean = new URL(clean).pathname;
      } catch (e) {
        clean = clean.replace(/^file:\/\/\/?/i, "").replace(/[?#].*$/, "");
      }
    }
    clean = clean.replace(/\\/g, "/").replace(/\/+$/, "");
    if (/^\/?[a-zA-Z]:$/.test(clean)) return;
    var seg = clean.split("/").pop() || "";
    var pluginId = seg;
    try { pluginId = decodeURIComponent(seg); } catch (e) {}
    if (!pluginId) return;
    if (window.__agyRouter && typeof window.__agyRouter.navigate === "function") {
      window.__agyRouter.navigate({
        to: "/customizations/plugins/$pluginId",
        params: { pluginId: pluginId },
        search: { pluginTab: "accounts" }
      });
      return;
    }
    var target = "/customizations/plugins/" + encodeURIComponent(pluginId) + "?pluginTab=accounts";
    if (window.history && typeof window.history.pushState === "function") {
      window.history.pushState(null, "", target);
      window.dispatchEvent(new PopStateEvent("popstate"));
    } else {
      window.location.href = target;
    }
  };

  window.__agyCompleteRemoteOAuth = async function (rawInput, submitFn) {
    if (typeof rawInput !== "string") {
      return submitFn(rawInput);
    }
    var trimmed = rawInput.trim();
    if (!/^https?:\/\//i.test(trimmed)) {
      return submitFn(rawInput);
    }
    var parsed = null;
    try { parsed = new URL(trimmed); } catch (e) { parsed = null; }
    if (!parsed) {
      return submitFn(rawInput);
    }
    var code = parsed.searchParams.get("code") || "";
    if (!code) {
      return submitFn(rawInput);
    }
    if (parsed.protocol === "http:" && isLoopbackHostname(parsed.hostname) && parsed.port) {
      try {
        var cbRes = await fetch("/__agy/api/oauth-callback", {
          method: "POST",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ url: trimmed })
        });
        if (cbRes.ok) {
          var cbData = await cbRes.json();
          if (cbData && cbData.delivered && cbData.statusCode >= 200 && cbData.statusCode < 400) {
            return {};
          }
        }
      } catch (e) {}
    }
    // The full URL goes first. A language server that wants only the code
    // rejects the URL without using the code, which the retry below then
    // sends. Sending the code first could spend it with a server that needs
    // the URL's state as well, and the URL retry would then fail.
    try {
      return await submitFn(trimmed);
    } catch (firstErr) {
      if (code && code !== trimmed) {
        return await submitFn(code);
      }
      throw firstErr;
    }
  };
})();
</script>`

// sidePanelTabScript defines __agyPaneTab, the component that
// side-panel-closed-file-viewer wraps the side panel's active tab in. It is
// given React as R, because the bundle does not expose it.
//
// A file or artifact viewer in a closed panel renders idle instead, until the
// panel opens in that conversation. It then stays while the panel closes, so
// it does not vanish as the panel slides shut or load again when it reopens.
// Other tabs, such as terminals and browsers, keep state the user would lose,
// so they render as before.
//
// When the panel closes on a file or artifact, its tab also leaves the URL.
// The store already holds that tab as the panel's active one. A terminal tab
// stays in the URL, because the bundle keeps the selected terminal only
// there. Changing the URL renders the route again, so it waits until the
// panel has finished closing, and is dropped if the panel opens again or the
// page moves on first. Clearing needs the router that router-navigate-bridge
// captures, and without it the URL keeps the tab.
const sidePanelTabScript = `<script id="agy-side-panel-tab">
(function () {
  if (window.__agyPaneTab) return;

  var readsFile = { fileView: true, artifactView: true };

  // The panel's width animates for 500ms as it closes, and the conversation
  // lays itself out again at the new width.
  var CLEAR_URL_AFTER_MS = 700;

  function treeKey(t) {
    if (!t || typeof t !== "object") return "";
    return String(t.kind) + ":" + String(t.cascadeId || t.projectId || t.viewId || "");
  }

  function currentPath() {
    var router = window.__agyRouter;
    var loc = router && router.state && router.state.location;
    return loc ? loc.pathname : null;
  }

  function clearURLTab() {
    var router = window.__agyRouter;
    var loc = router && router.state && router.state.location;
    if (!loc || !loc.search || loc.search.tab === undefined || typeof router.navigate !== "function") return;
    router.navigate({
      search: function (s) {
        var next = Object.assign({}, s);
        delete next.tab;
        return next;
      },
      replace: true,
      unsafeRelative: "path"
    });
  }

  window.__agyPaneTab = function (p) {
    var R = p.R;
    var key = treeKey(p.treeId);
    var open = !!p.open;
    var type = p.tab && p.tab.content && p.tab.content.type;
    var shown = R.useState(open ? key : null);
    var last = R.useRef({ key: key, open: open });
    R.useEffect(function () {
      var was = last.current;
      last.current = { key: key, open: open };
      if (open) {
        shown[1](key);
        return;
      }
      if (was.key !== key) {
        shown[1](null);
        return;
      }
      if (!was.open || !readsFile[type]) return;
      var path = currentPath();
      var timer = setTimeout(function () {
        if (currentPath() === path) clearURLTab();
      }, CLEAR_URL_AFTER_MS);
      return function () {
        clearTimeout(timer);
      };
    }, [key, open, type]);
    if (open || !readsFile[type] || shown[0] === key) return p.children;
    return p.idle || null;
  };
})();
</script>`
