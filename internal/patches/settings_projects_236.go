package patches

import (
	"bytes"
	"strings"
)

// Since 2.36 the phone's settings view reads the screen asked for from the store and opens it itself, so its names do
// different jobs: the first is the screen's label, the store's dispatch comes second, the screen open in the second
// panel is r rather than m, and the back button calls a function of its own that also clears what was asked for. This
// is that view, for mobile-settings-projects, matched with the hooks the patch borrows from the desktop settings that
// follow it. Each name the patch writes with is a named group; the others are names the view must use the same way,
// which settingsProjects236IsKnown checks.
const settingsProjects236Name = `[a-zA-Z0-9_$]+`

var mobileSettingsProjects236 = func() string {
	n := settingsProjects236Name
	named := func(name string) string { return `(?P<` + name + `>` + n + `)` }
	return `(?P<s236head>var ` + n + `=\(\)=>\{var ` + n + `=` + n + `\(\),` + named("s236dispatch") + `=` + n + `\(\),` + n + `=` + n +
		`\(\)\.settings\.isScreenVisible\("Provide Feedback"\),\{sharedNavItems:` + n + `,globalScreens:` + n + `,workspaces:` + n +
		`,showWorkspacesSection:` + n + `\}=` + n + `\(\);[^;]{1,200};var ` + named("s236stack") + `=` + n + `\(!0\),[\s\S]{1,600}?;var ` +
		named("s236current") + `=` + named("s236stackPath") + `\.path\[0\]\?\?` + n + `\.exiting\[0\],` + n + `=` + n +
		`\?\.startsWith\("workspace:"\),` + named("s236back") + `=\s*\(\)=>\{[^{}]{0,120}\.pop\(\)\};)` +
		`(?P<s236list>return ` + named("s236react") + `\.createElement\("div",\{ref:` + named("s236stackHost") +
		`\.setHost,className:"relative h-full w-full overflow-hidden"\},\s*` + n + `\.createElement\(` + n + `,\s*\{offset:-` + n +
		`,active:` + n + `,depth:0,footer:` + n + `\.includes\("Account"\)\?` + n + `\.createElement\("div",\s*\{className:"flex flex-col border-t border-border p-1"\},\s*` +
		n + `\.createElement\(` + n + `,\{showChevron:!0,className:"mx-1 px-3 py-2\.5",onClick:\(\)=>\{` + named("s236dispatchAccount") + `\(` +
		named("s236navigate") + `\("Account"\)\);` + n + `\.push\("Account"\)\}\}\)\):void 0\},\s*` + n + `\.createElement\(` + named("s236header") +
		`,\{text:"Settings"\}\),` + n + `\.map\(` + n + `=>` + n + `\.createElement\(` + named("s236item") + `,\{key:` + n + `,text:` + n +
		`,label:` + n + `\(` + n + `\),\s*onClick:\(\)=>\{` + n + `\(` + n + `\(` + n + `\)\);` + n + `\.push\(` + n + `\)\}\}\)\),\s*` + n + `&&` + n +
		`\.createElement\(` + n + `\.Fragment,null,` + n + `\.createElement\(` + n + `,\{text:"Workspaces"\}\),` + n + `\.map\(` + n + `=>` + n +
		`\.createElement\(` + n + `,\s*\{key:` + n + `\.uri,text:` + n + `\.name,\s*onClick:\(\)=>\{var ` + n + `=` + n + `\.uri;` + n + `\(` + n +
		`\(\{workspaceUri:` + n + `\}\)\);` + n + `\.push\(` + "`" + `workspace:\$\{` + n + `\}` + "`" + `\)\}\}\)\)\))` +
		`(?P<s236second>\),\s*` + named("s236currentShown") + `&&` + n + `\.createElement\(` + n + `,\{offset:1-` + n + `,active:` + n + `===\s*1,depth:1,exiting:` + n +
		`\.exiting\.includes\(` + named("s236currentExiting") + `\),onExited:\(\)=>\{` + n + `\.onExited\(` + named("s236currentExited") + `\)\},header:` + n + `\.createElement\(` + n + `,\{onBack:` +
		named("s236backUsed") + `\}\)\},` + n + `\.createElement\(` + n + `\.Provider,\{value:!0\},)` +
		`(?P<s236content>` + n + `\?` + n + `\.createElement\(` + n + `,null\):` + n + `\(` + named("s236currentDrawn") + `,\s*` + n + `===1\))` +
		`(?P<s236tail>\)\)\)\};[\s\S]+?var a=` + named("s236useApp") + `\(\),b=!!a\.get\("settings"\)\?\.hideProjects[\s\S]{1,400}?` +
		`(?:\{(?:settingsFeature:` + n + `,)?projectManagementFeature:` + n + `\}=(?P<s236pmContext>\(0,` + n + `\.useContext\)\(` + n + `\))|` + n +
		`=(?P<s236pmHook>` + n + `\(\)),\{sharedNavItems:)[\s\S]{1,400}?,` + n + `=` + named("s236useProjects") + `\(` + n + `\??\.projectsStateProvider\))`
}()

// settingsProjectsParts are what the patch writes with: the names it calls and the pieces of the view it keeps.
type settingsProjectsParts struct {
	head, list, second, content, tail string
	dispatch, current, stack, react   string
	navigate, header, item            string
	useApp, pmInit, useProjects       string
	// back is the back button's handler, h.pop up to 2.35 and a function of the view's own since, which the patch
	// first offers to the project settings.
	back string
}

// renderSettingsProjects is the view with the Projects and Not in Project sections, the address's deep link, and the
// project settings in the second panel.
func renderSettingsProjects(p settingsProjectsParts) string {
	d, c, stk, react := p.dispatch, p.current, p.stack, p.react
	return p.head + p.pmInit + `,_hp=!!` + p.useApp + `().get("settings")?.hideProjects,_ps=` + p.useProjects + `(_pm?.projectsStateProvider),_pl=(_ps||[]).filter(_x=>_x?.project&&!_x.project.isWorkspaceOnly&&!_x.project.archived),_init=(0,` + react + `.useRef)(!1);window.__agyProjectSettingsHost=!!` + c + `?.startsWith("project:");if(!window.__agyProjectSettingsHost)window.__agyProjectSettingsBack=null;(0,` + react + `.useEffect)(()=>()=>{window.__agyProjectSettingsHost=!1;window.__agyProjectSettingsBack=null},[]);(0,` + react + `.useEffect)(()=>{if(_init.current)return;_init.current=!0;try{var _u=new URLSearchParams(window.location.search),_cl=s=>s?s.replace(/^"|"$/g,""):"",_pid=_cl(_u.get("settingsProjectId")),_scr=_cl(_u.get("settingsScreen"));if(_pid){` + d + `({type:"settingsScreen/setContextProjectId",payload:_pid});` + d + `({type:"settingsScreen/navigateToProjectScreen",payload:{projectId:_pid}});if(_scr)` + d + `(` + p.navigate + `(_scr));` + stk + `.push("project:"+_pid)}else if(_scr&&_scr!=="General"){` + d + `(` + p.navigate + `(_scr));` + stk + `.push(_scr)}}catch(_e){}},[]);` + p.list + `,!_hp&&_pm&&_pl.length>0&&` + react + `.createElement(` + react + `.Fragment,null,` + react + `.createElement(` + p.header + `,{text:"Projects"}),_pl.map(_p=>` + react + `.createElement(` + p.item + `,{key:_p.project.id,text:_p.project.name||"Project",label:_p.project.name||"Project",onClick:()=>{` + d + `({type:"settingsScreen/setContextProjectId",payload:_p.project.id});` + d + `({type:"settingsScreen/navigateToProjectScreen",payload:{projectId:_p.project.id}});` + stk + `.push("project:"+_p.project.id)}}))),_pm&&` + react + `.createElement(` + react + `.Fragment,null,` + react + `.createElement(` + p.header + `,{text:"Not in Project"}),` + react + `.createElement(` + p.item + `,{key:"outside-of-project",text:"Conversations",label:"Conversations",onClick:()=>{` + d + `({type:"settingsScreen/setContextProjectId",payload:"outside-of-project"});` + d + `({type:"settingsScreen/navigateToProjectScreen",payload:{projectId:"outside-of-project"}});` + stk + `.push("project:outside-of-project")}}))` +
		strings.Replace(p.second, `{onBack:`+p.back+`}`, `{onBack:()=>{if(`+c+`?.startsWith("project:")&&window.__agyProjectSettingsBack?.())return;`+p.back+`()}}`, 1) +
		c + `?.startsWith("project:")&&window.__agyProjectSettings?` + react + `.createElement(window.__agyProjectSettings,null):` + p.content + p.tail
}

// settingsProjects236 gives a named group of mobileSettingsProjectsRe's 2.36 form, or nil.
func settingsProjects236(groups [][]byte, name string) []byte {
	i := mobileSettingsProjectsRe.SubexpIndex(name)
	if i < 0 || i >= len(groups) {
		return nil
	}
	return groups[i]
}

// settingsProjects236IsKnown says whether a match of the 2.36 form is the view the patch writes for: the stack it
// pushes onto is the one the view is drawn with and reads the open screen from, the screen it shows is the open one,
// the back button is the view's own function, and the store's dispatch is the one the Account row calls.
func settingsProjects236IsKnown(groups [][]byte) bool {
	g := func(name string) []byte { return settingsProjects236(groups, name) }
	if g("s236head") == nil {
		return true
	}
	return bytes.Equal(g("s236stack"), g("s236stackPath")) && bytes.Equal(g("s236stack"), g("s236stackHost")) &&
		bytes.Equal(g("s236current"), g("s236currentShown")) && bytes.Equal(g("s236current"), g("s236currentExiting")) &&
		bytes.Equal(g("s236current"), g("s236currentExited")) && bytes.Equal(g("s236current"), g("s236currentDrawn")) &&
		bytes.Equal(g("s236back"), g("s236backUsed")) &&
		bytes.Equal(g("s236dispatch"), g("s236dispatchAccount"))
}

// mobileSettingsProjects236Replace writes the 2.36 form.
func mobileSettingsProjects236Replace(groups [][]byte) string {
	g := func(name string) string { return string(settingsProjects236(groups, name)) }
	pmInit := `var {projectManagementFeature:_pm}=` + g("s236pmContext")
	if settingsProjects236(groups, "s236pmHook") != nil {
		pmInit = `var _pm=` + g("s236pmHook")
	}
	return renderSettingsProjects(settingsProjectsParts{
		head: g("s236head"), list: g("s236list"), second: g("s236second"), content: g("s236content"), tail: g("s236tail"),
		dispatch: g("s236dispatch"), current: g("s236current"), stack: g("s236stack"), react: g("s236react"),
		navigate: g("s236navigate"), header: g("s236header"), item: g("s236item"),
		useApp: g("s236useApp"), pmInit: pmInit, useProjects: g("s236useProjects"), back: g("s236back"),
	})
}
