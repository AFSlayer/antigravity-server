package patches

import (
	"regexp"
	"strconv"
	"strings"
)

// Up to 2.29 the web bundle could draw a context indicator beside the send button: how full the conversation's
// context window is, with a breakdown on click. It was behind the "Context and Quota Indicator" setting, which was off
// by default and itself behind a Google feature flag. Since 2.30 the indicator's component comes from the app's build
// bindings, and the web build has none, so nothing is drawn over remote access. The language server still sends each
// conversation's context window usage.

// hasContextIndicatorSlot says whether a bundle takes the indicator from the build bindings, as 2.30 and later do,
// with a line break wherever the minifier could put one.
func hasContextIndicatorSlot(b []byte) bool { return regexpMatch(contextIndicatorSlotMarkRe, b) }

var contextIndicatorSlotMarkRe = regexp.MustCompile(`\?\.\s*contextUsageIndicator\s*\?\.\s*Indicator\b`)

// The hook that gives a conversation's agent state store, and the hook after it, which reads the store with the
// selector hook. Each name a release changes is a «role»: see shapeBuilder.
const (
	contextIndicatorHookStart = `«hook»=«a»=>{var «b»=(0,«React».useContext)(«ctx»);if(!«b2»)throw Error("useAgentStateProvider must be used within a TrajectoriesContextProvider");`
	contextIndicatorHookNext  = `«next»=«a2»=>{«a3»=«hook»(«a4»);return «select»(«a5»,{selector:`
)

// contextIndicatorDefine is the pattern for the hooks with its replacement, which declares the indicator, and the
// query it reads the store with, as two more names in the list the hook is declared in, before it.
var contextIndicatorDefine = func() (s struct {
	re      *regexp.Regexp
	names   map[string][]int
	replace string
}) {
	var b shapeBuilder
	b.code(`},`)
	b.raw(`\s*`)
	head := b.open()
	b.code(contextIndicatorHookStart)
	b.raw(`[\s\S]{0,700}?`)
	b.code(contextIndicatorHookNext)
	b.close()
	s.re, s.names = regexp.MustCompile(b.String()), b.names
	name := func(role string) string { return "${" + strconv.Itoa(s.names[role][0]) + "}" }
	component := strings.NewReplacer("«hook»", name("hook"), "«React»", name("React"), "«select»", name("select")).Replace(contextIndicatorComponent)
	s.replace = "},__agyContextUsageQuery=" + contextIndicatorQuery + ",__agyContextIndicator=window.__agyContextIndicator=" + component + ",${" + strconv.Itoa(head) + "}"
	return s
}()

// contextIndicatorHooksAreKnown takes a match only where the hook after it reads the store with the same hook.
func contextIndicatorHooksAreKnown(groups [][]byte) bool {
	return sameNames(groups, contextIndicatorDefine.names)
}

// The input taking the indicator from the build bindings, and the setting's default from the app's configuration.
var contextIndicatorSlot = func() (s struct {
	re      *regexp.Regexp
	names   map[string][]int
	replace string
}) {
	var b shapeBuilder
	b.code(`«indicator»=«bindings»?.contextUsageIndicator?.Indicator,«byDefault»=!!«indicator»&&(«config».get("contextUsageIndicator")??!1)`)
	s.re, s.names = regexp.MustCompile(b.String()), b.names
	name := func(role string) string { return "${" + strconv.Itoa(s.names[role][0]) + "}" }
	s.replace = name("indicator") + "=" + name("bindings") + "?.contextUsageIndicator?.Indicator??window.__agyContextIndicator," + name("byDefault") + "=!!" + name("indicator")
	return s
}()

func contextIndicatorSlotIsKnown(groups [][]byte) bool {
	return sameNames(groups, contextIndicatorSlot.names)
}

// The settings provider reading the setting as on only where it was stored as "true". Group 1 is the read.
var contextIndicatorDefaultRe = func() *regexp.Regexp {
	var b shapeBuilder
	b.open()
	b.code(`case "contextUsageIndicatorEnabled":return «settings»?.get("contextUsageIndicatorEnabled")`)
	b.close()
	b.raw(`\s*`)
	b.code(`==="true"`)
	return regexp.MustCompile(b.String())
}()

// The web build hides the setting three ways: the settings screens leave it out where the build bindings have no
// indicator, the feature manager shows it only behind a Google flag, and the Appearance screen keeps the section it is
// in, "Chat Layout", collapsed. These are the three, as 2.30 to 2.32.1 write them.
var (
	// It is a statement of its own, and is matched from the end of the one before it, which is kept: group 1.
	contextIndicatorSettingAllowedRe = func() *regexp.Regexp {
		var b shapeBuilder
		b.open()
		b.raw(`[;{}]`)
		b.close()
		b.raw(`\s*`)
		b.code(`«withIndicator»||«hidden».add("contextUsageIndicatorEnabled");`)
		return regexp.MustCompile(b.String())
	}()
	contextIndicatorSettingVisible = func() (s struct {
		re      *regexp.Regexp
		replace string
	}) {
		var b shapeBuilder
		b.code(`contextUsageIndicatorEnabled:{feature:{visible:!1},applyAll:[{runtimeGating:{customFlag:"enable-context-usage-indicator"},feature:{visible:!0}}],important:[«important»]}`)
		s.re = regexp.MustCompile(b.String())
		s.replace = "contextUsageIndicatorEnabled:{feature:{visible:!0},important:[${" + strconv.Itoa(b.names["important"][0]) + "}]}"
		return s
	}()
	contextIndicatorSettingInChatRe = func() *regexp.Regexp {
		var b shapeBuilder
		b.code(`{title:"Chat",settings:["verboseConversationSteps"]},{title:"Chat Layout",settings:["themeMode","conversationWidth","artifactWidth","contextUsageIndicatorEnabled"]}`)
		return regexp.MustCompile(b.String())
	}()
)

// The setting's description promises the model quota, which this indicator does not show.
var contextIndicatorSettingDescription = func() (s struct {
	re      *regexp.Regexp
	replace string
}) {
	var b shapeBuilder
	b.code(`description:"Show how full the context window is next to the send button. Click it for a breakdown and to see your model quota."`)
	s.re = regexp.MustCompile(b.String())
	s.replace = `description:"Show how full the context window is next to the send button. Tap it for a breakdown."`
	return s
}()

const contextIndicatorSettingInChat = `{title:"Chat",settings:["verboseConversationSteps","contextUsageIndicatorEnabled"]},{title:"Chat Layout",settings:["themeMode","conversationWidth","artifactWidth"]}`

// contextIndicatorQuery picks the conversation's usage out of its state, and says, as the 2.29 indicator did, whether
// the conversation was compacted since the usage was counted: a checkpoint step (type 23) that is done (status 3)
// after the step the usage was counted at, and not one that only states an intent. It is declared once, so the
// selector hook keeps one subscription for the indicator's life.
const contextIndicatorQuery = `{selector:__agyX=>{var __agyW=__agyX?.contextWindowUsage;if(!__agyW)return null;` +
	`var __agySteps=__agyX.trajectorySlice?.stepsInSlice??[],__agyC=!1;` +
	`for(let __agyI=__agySteps.length-1;__agyI>=0;__agyI--){let __agyF=__agySteps[__agyI],__agyN=__agyF?.metadata?.sourceTrajectoryStepInfo?.stepIndex??-1;` +
	`if(__agyN>=0&&__agyN<__agyW.lastStepIndex)break;` +
	`if(__agyN>=0&&__agyF.type===23&&__agyF.status===3&&(__agyF.step?.case!=="checkpoint"||!__agyF.step.value?.intentOnly)){__agyC=!0;break}}` +
	`return{usage:__agyW,compacted:__agyC}},` +
	`equalityFn:(__agyA,__agyB)=>__agyA===__agyB||!!__agyA&&!!__agyB&&__agyA.usage===__agyB.usage&&__agyA.compacted===__agyB.compacted}`

// contextIndicatorComponent is the indicator, after the 2.29 one: a ring and the percentage, and on a tap the tokens
// used of the window, each part the language server counts them in, and what is free. It reads the conversation's
// store with the bundle's own two hooks, «hook» and «select», and draws with its React, «React»; contextIndicatorDefine
// puts in the names they have. Every name it declares starts with __agy, so none of those three is hidden by one of
// its own. It has no template literal and no $, since the replacement is expanded.
const contextIndicatorComponent = `function(__agyP){` +
	`var __agyR=«React»,__agyE=__agyR.createElement,__agyS=«hook»(__agyP.conversationId),__agyQ=«select»(__agyS,__agyContextUsageQuery),` +
	`[__agyO,__agySetO]=(0,__agyR.useState)(null),__agyRef=(0,__agyR.useRef)(null);` +
	`(0,__agyR.useEffect)(()=>{if(!__agyO)return;var __agyH=__agyV=>{(__agyV.type==="keydown"?__agyV.key==="Escape":__agyRef.current&&!__agyRef.current.contains(__agyV.target))&&__agySetO(null)};` +
	`document.addEventListener("pointerdown",__agyH,!0);document.addEventListener("keydown",__agyH,!0);` +
	`return()=>{document.removeEventListener("pointerdown",__agyH,!0);document.removeEventListener("keydown",__agyH,!0)}},[__agyO]);` +
	`var __agyU=__agyQ&&__agyQ.usage,__agyT=Number(__agyU?.estimatedTokensUsed??0),__agyM=Number(__agyU?.maxContextTokens??0);if(!(__agyT>0&&__agyM>0))return null;` +
	`var __agyF=Math.min(1,__agyT/__agyM),__agyPc=Math.round(__agyF*100),__agyA=!!__agyQ.compacted,__agyL=2*Math.PI*6,` +
	`__agyZ=__agyV=>__agyV.endsWith(".0")?__agyV.slice(0,-2):__agyV,__agyN=__agyK=>__agyK<1E3?String(__agyK):__agyK<1E6?__agyZ((__agyK/1E3).toFixed(1))+"K":__agyZ((__agyK/1E6).toFixed(1))+"M",` +
	`__agyRows=[];` +
	`(__agyU.tokenGroups||[]).filter(__agyX=>Number(__agyX.numTokens)>0).forEach((__agyX,__agyI)=>{__agyRows.push(__agyE("dt",{key:"t"+__agyI},__agyX.name||"Other"),__agyE("dd",{key:"d"+__agyI,style:{textAlign:"right"}},__agyN(Number(__agyX.numTokens))))});` +
	`__agyRows.push(__agyE("dt",{key:"tf"},"Free"),__agyE("dd",{key:"df",style:{textAlign:"right"}},__agyN(Math.max(0,__agyM-__agyT))));` +
	`return __agyE("span",{ref:__agyRef,style:{position:"relative",display:"inline-flex"}},` +
	`__agyE("button",{type:"button","data-testid":"context-usage-indicator","aria-label":"Context window "+(__agyA?"about ":"")+__agyPc+"% full","aria-expanded":__agyO!==null,` +
	// The breakdown opens above the indicator, as wide as the screen allows and inside it.
	`onClick:()=>{if(__agyO!==null){__agySetO(null);return}var __agyB=__agyRef.current.getBoundingClientRect(),__agyWd=Math.min(280,innerWidth-24),__agyLeft=Math.min(Math.max(12,__agyB.right-__agyWd),innerWidth-12-__agyWd);__agySetO({left:__agyLeft-__agyB.left,width:__agyWd})},` +
	`className:"inline-flex items-center gap-1 h-6 px-1.5 rounded-md text-xs tabular-nums hover:bg-muted "+(__agyF>=.8?"text-warning":"text-muted-foreground")},` +
	`__agyE("svg",{"aria-hidden":"true",viewBox:"0 0 16 16",width:14,height:14,style:{transform:"rotate(-90deg)"}},` +
	`__agyE("circle",{cx:"8",cy:"8",r:6,fill:"none",stroke:"currentColor",strokeWidth:"2",opacity:.25}),` +
	`__agyE("circle",{cx:"8",cy:"8",r:6,fill:"none",stroke:"currentColor",strokeWidth:"2",strokeLinecap:"round",strokeDasharray:__agyL,strokeDashoffset:__agyL*(1-__agyF)})),` +
	`__agyE("span",null,__agyA?"~":"",__agyPc,"%")),` +
	`__agyO&&__agyE("div",{"data-testid":"context-usage-panel",role:"dialog","aria-label":"Context window",className:"agy-overlay surface-glass bg-card text-foreground border border-border text-xs",` +
	`style:{position:"absolute",bottom:"100%",left:__agyO.left,width:__agyO.width,marginBottom:8,zIndex:60,borderRadius:10,padding:12,display:"flex",flexDirection:"column",gap:8}},` +
	`__agyE("div",{style:{display:"flex",justifyContent:"space-between"}},__agyE("span",{className:"font-medium text-muted-foreground"},"Context window"),__agyE("span",{className:"font-medium tabular-nums"},__agyPc,"%")),` +
	`__agyE("div",{className:"tabular-nums"},__agyN(__agyT)," of ",__agyN(__agyM)," tokens used"),` +
	`__agyE("dl",{className:"tabular-nums",style:{display:"grid",gridTemplateColumns:"minmax(0,1fr) auto",columnGap:8,rowGap:4,margin:0}},__agyRows),` +
	`__agyE("p",{className:"text-muted-foreground",style:{margin:0}},__agyA?"Compacted since the last response; counts update after the next one.":"When it fills up, older turns are summarized to make room.")))}`
