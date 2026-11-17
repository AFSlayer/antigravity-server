package patches

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

// Up to 2.29 the web bundle drew a context indicator beside the send button: how full the conversation's context
// window is, with a breakdown on click, shown when the "Context and Quota Indicator" setting is on. Since 2.30 the
// indicator's component comes from the app's build bindings, and the web build has none, so the setting draws nothing
// over remote access. The language server still sends each conversation's context window usage.

// hasContextIndicatorSlot says whether a bundle takes the indicator from the build bindings, as 2.30 and later do.
func hasContextIndicatorSlot(b []byte) bool {
	return bytes.Contains(b, []byte(`?.contextUsageIndicator?.Indicator`))
}

// The hook that gives a conversation's agent state store, and the hook after it, which reads the store with the
// selector hook. Each name a release changes is a «role»: see shapeBuilder.
const (
	contextIndicatorHookStart = `«hook»=«a»=>{var «b»=(0,«React».useContext)(«ctx»);if(!«b2»)throw Error("useAgentStateProvider must be used within a TrajectoriesContextProvider");`
	contextIndicatorHookNext  = `«next»=«a2»=>{«a3»=«hook»(«a4»);return «select»(«a5»,{selector:`
)

// contextIndicatorDefine is the pattern for the hooks with its replacement, which declares the indicator as one more
// name in the list the hook is declared in, before it.
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
	component := strings.NewReplacer("${2}", name("hook"), "${4}", name("React"), "${5}", name("select")).Replace(contextIndicatorComponent)
	s.replace = "},__agyContextIndicator=window.__agyContextIndicator=" + component + ",${" + strconv.Itoa(head) + "}"
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

// contextIndicatorComponent is the indicator, after the 2.29 one: a ring and the percentage, and on a click the
// tokens used of the window and each part the language server counts them in. It reads the conversation's store with
// the bundle's own two hooks, written ${2} and ${5}, and draws with React, written ${4}; contextIndicatorDefine puts
// in the groups they are. It has no template literal and no other $, since the replacement is expanded.
const contextIndicatorComponent = `function(p){` +
	`var R=${4},E=R.createElement,s=${2}(p.conversationId),u=${5}(s,{selector:x=>x?.contextWindowUsage}),[o,so]=(0,R.useState)(!1),r=(0,R.useRef)(null);` +
	`(0,R.useEffect)(()=>{if(!o)return;var h=e=>{r.current&&!r.current.contains(e.target)&&so(!1)};document.addEventListener("pointerdown",h,!0);return()=>document.removeEventListener("pointerdown",h,!0)},[o]);` +
	`var t=Number(u?.estimatedTokensUsed??0),m=Number(u?.maxContextTokens??0);if(!(t>0&&m>0))return null;` +
	`var f=Math.min(1,t/m),c=Math.round(f*100),a=!!u.estimatedAfterCompaction,L=2*Math.PI*6,` +
	`z=v=>v.endsWith(".0")?v.slice(0,-2):v,n=k=>k<1E3?String(k):k<1E6?z((k/1E3).toFixed(1))+"K":z((k/1E6).toFixed(1))+"M",` +
	`g=(u.tokenGroups||[]).filter(x=>Number(x.numTokens)>0),rows=[];` +
	`g.forEach((x,i)=>{rows.push(E("dt",{key:"t"+i},x.name||"Other"),E("dd",{key:"d"+i,style:{textAlign:"right"}},n(Number(x.numTokens))))});` +
	`rows.push(E("dt",{key:"tf"},"Free"),E("dd",{key:"df",style:{textAlign:"right"}},n(Math.max(0,m-t))));` +
	`return E("span",{ref:r,style:{position:"relative",display:"inline-flex"}},` +
	`E("button",{type:"button","data-testid":"context-usage-indicator","aria-label":"Context window "+(a?"about ":"")+c+"% full","aria-expanded":o,onClick:()=>so(!o),` +
	`className:"inline-flex items-center gap-1 h-6 px-1.5 rounded-md text-xs tabular-nums hover:bg-muted "+(f>=.8?"text-warning":"text-muted-foreground")},` +
	`E("svg",{"aria-hidden":"true",viewBox:"0 0 16 16",width:14,height:14,style:{transform:"rotate(-90deg)"}},` +
	`E("circle",{cx:"8",cy:"8",r:6,fill:"none",stroke:"currentColor",strokeWidth:"2",opacity:.25}),` +
	`E("circle",{cx:"8",cy:"8",r:6,fill:"none",stroke:"currentColor",strokeWidth:"2",strokeLinecap:"round",strokeDasharray:L,strokeDashoffset:L*(1-f)})),` +
	`E("span",null,a?"~":"",c,"%")),` +
	`o&&E("div",{"data-testid":"context-usage-panel",className:"agy-overlay surface-glass bg-card text-foreground border border-border text-xs",` +
	`style:{position:"absolute",bottom:"100%",right:0,marginBottom:8,zIndex:60,width:280,maxWidth:"calc(100vw - 24px)",borderRadius:10,padding:12,display:"flex",flexDirection:"column",gap:8}},` +
	`E("div",{style:{display:"flex",justifyContent:"space-between"}},E("span",{className:"font-medium text-muted-foreground"},"Context window"),E("span",{className:"font-medium tabular-nums"},c,"%")),` +
	`E("div",{className:"tabular-nums"},n(t)," of ",n(m)," tokens used"),` +
	`E("dl",{className:"tabular-nums",style:{display:"grid",gridTemplateColumns:"minmax(0,1fr) auto",columnGap:8,rowGap:4,margin:0}},rows),` +
	`E("p",{className:"text-muted-foreground",style:{margin:0}},a?"Compacted since the last response; counts update after the next one.":"When it fills up, older turns are summarized to make room.")))}`
