package patches

import (
	"bytes"
	"regexp"
	"strconv"
)

// hasAgentPickerInModelMenu says whether a bundle draws the agent picker as a row of the model selector's menu, which
// it has done since 2.29. The row's test id is in no pattern of the patches that ask this, so a bundle that renames
// what they match is still known as one they are for.
func hasAgentPickerInModelMenu(b []byte) bool {
	return bytes.Contains(b, []byte(`"agent-selector-submenu"`))
}

// The desktop layout's new conversation screen up to where it draws, as every bundle from 2.29.0 to 2.32.1 writes it:
// how it starts, the run of hooks it builds the agent picker with, and the return that follows the run. Between its
// start and the run the screen does other things, which differ from release to release. Each name a release changes is
// written as «role»: see shapeBuilder.
//
// The start reads, among other things, the agent that the address asks for and the input's settings. The run reads the
// agents, keeps the choice in step with them, takes the agent asked for, and ends with the picker's element, or with
// nothing, in the name that held the input's settings.
//
// mobile-agent-picker-hook copies the run into a function of its own, which has nothing of the screen's to read. So
// the run is matched whole and not by its ends: a run with one more step in it, or one that reads something else of
// the screen's, is not this code, and is reported as not found.
const (
	agentScreenStart  = `«screen»=()=>{var «dispatch»=«useDispatch»(),«lastProject»=(0,«React».useRef)(null),«open»=«useOpen»(),{q:«query»,ws:«project»,customSubagent:«asked»}=«useAddress»({strict:!1}),«app»=«useApp»(),«tips»=«app».get("tipCards"),«input»=«app».get("inputComponent");`
	agentPickerRun    = `var{agentScriptsEnabled:«enabled»,selectedAgentScriptItem:«chosen»,setSelectedAgentScriptItem:«choose»,getAgentScripts:«read»,globalScriptsDir:«global»,workspaceScriptsDir:«workspace»}=«agents»(),{data:«data»,refetch:«again»,isLoading:«loading»,error:«error»}=«fetch»({fire:«read»}),«all»=(0,«React».useMemo)(()=>«data»?.agentCustomizations??[],[«data»]),«offered»=(0,«React».useMemo)(()=>«all».filter(«one»=>!«one».agent?.disabled),[«all»]);«inStep»({isLoadingAgentScripts:«loading»,agentScriptsError:«error»,selectedAgentScriptItem:«chosen»,enabledCustomizations:«offered»,setSelectedAgentScriptItem:«choose»});«askedFor»({initialCustomSubagent:«asked»,agentCustomizations:«all»,setSelectedAgentScriptItem:«choose»});var{registerOpenSelector:«register»}=«modal»();(0,«React».useEffect)(()=>{var «handle»=«register»(()=>{«again»()});return()=>«handle».dispose()},[«register»,«again»]);«input»=«shown»({agentScriptsEnabled:«enabled»,isLoadingAgentScripts:«loading»,agentScriptsError:«error»,hideAgentSelectorIfEmpty:«input»?.hideAgentSelectorIfEmpty,agentCustomizationsCount:«all».length,selectedAgentScriptItem:«chosen»})?«React».createElement(«picker»,{agentScriptsEnabled:«enabled»,selectedAgentScriptItem:«chosen»,setSelectedAgentScriptItem:«choose»,globalScriptsDir:«global»,workspaceScriptsDir:«workspace»,agentCustomizations:«all»,refreshAgentScripts:«again»,isLoadingAgentScripts:«loading»,agentScriptsError:«error»}):void 0;`
	agentScreenReturn = `return «React».createElement(«React».Fragment`
)

// The names the screen's start declares.
var agentScreenDeclares = []string{"dispatch", "lastProject", "open", "query", "project", "asked", "app", "tips", "input"}

// The names the function reads from outside itself: the hooks it reads the address and the app's settings with, as the
// screen's start does, and what the run calls. They have to be the bundle's own, from outside any screen.
var agentPickerOutside = []string{"useAddress", "useApp", "React", "agents", "fetch", "inStep", "askedFor", "modal", "shown", "picker"}

// agentScreen is the pattern for the screen, with the groups that are not names: the screen up to the run, what it
// does between its start and the run, the run, and what follows the run.
var agentScreen = func() (s struct {
	re                           *regexp.Regexp
	names                        map[string][]int
	upToRun, between, run, after int
}) {
	var b shapeBuilder
	s.upToRun = b.open()
	b.code(agentScreenStart)
	s.between = b.open()
	b.raw(`[\s\S]{0,700}?`)
	b.close()
	b.close()
	s.run = b.open()
	b.code(agentPickerRun)
	b.close()
	s.after = b.open()
	b.raw(`\s*`)
	b.code(agentScreenReturn)
	b.close()
	s.re, s.names = regexp.MustCompile(b.String()), b.names
	return s
}()

// mobileAgentPickerHookReplace declares the function before the screen, as one more name in the list the screen is
// declared in, and leaves the screen as it is. The function reads the two things the run takes from the screen the
// way the screen's start reads them, and gives back what the run ends with.
var mobileAgentPickerHookReplace = func() string {
	group := func(n int) string { return "${" + strconv.Itoa(n) + "}" }
	name := func(role string) string { return group(agentScreen.names[role][0]) }
	return `__agyPhoneAgentPicker=window.__agyPhoneAgentPicker=function(){var{customSubagent:` + name("asked") + `}=` + name("useAddress") + `({strict:!1}),` +
		name("input") + `=` + name("useApp") + `().get("inputComponent");` + group(agentScreen.run) + `return ` + name("input") + `},` +
		group(agentScreen.upToRun) + group(agentScreen.run) + group(agentScreen.after)
}()

// agentScreenIsKnown says whether a match of agentScreen's pattern is code the function can stand in for. A pattern
// cannot say which name is which, and the function is right only where:
//
//   - every role is one name wherever it is written, so the run reads the agent asked for and the input's settings
//     out of the names the start read them into, and whatever else it reads it has declared itself or is one of the
//     names from outside;
//   - the start declares each of its names once, so those two still hold at its end what it read into them;
//   - the screen does not name those two between its start and the run, so they hold that at the run as well;
//   - nothing the function reads from outside itself is a name of the screen's. The start's names are known. Between
//     the start and the run the screen can declare more, so none of these may be named there either. React is, and
//     need not be looked for: the start has read it by then, which a declaration further down would have broken.
func agentScreenIsKnown(groups [][]byte) bool {
	if !sameNames(groups, agentScreen.names) {
		return false
	}
	name := func(role string) string { return string(groups[agentScreen.names[role][0]]) }
	declared := map[string]bool{}
	for _, role := range agentScreenDeclares {
		if declared[name(role)] {
			return false
		}
		declared[name(role)] = true
	}
	between := groups[agentScreen.between]
	if mentions(between, name("asked")) || mentions(between, name("input")) {
		return false
	}
	for _, role := range agentPickerOutside {
		if declared[name(role)] || role != "React" && mentions(between, name(role)) {
			return false
		}
	}
	return true
}
