package patches

import (
	"strings"
	"testing"
)

// The Plugins section goes right after the theme customizer of the Appearance screen, in each form the screen has had:
// 2.35, and before it, where the first group of settings takes a filter for the Chat section.
func TestPluginsSectionAfterThemeCustomizer(t *testing.T) {
	for name, src := range map[string]string{
		"2.35": `return E.createElement(YU,{title:"Appearance",description:"x"},E.createElement(NV,{screen:"Appearance"}),a?.(),E.createElement(zV,{expanded:b},E.createElement(NV,{screen:"Appearance",advanced:!0})))`,
		"2.34": `return E.createElement(LU,{title:"Appearance",description:"x"},E.createElement(TV,{screen:"Appearance",sectionFilter:f=>f==="Chat"}),a?.(),E.createElement(xV,{expanded:b},E.createElement(TV,{screen:"Appearance",sectionFilter:f=>f!=="Chat"})))`,
		"2.33": `return D.createElement(ZS,{title:"Appearance",description:"x"},D.createElement(gU,{screen:"Appearance",sectionFilter:f=>f==="Chat"}),a?.(),D.createElement(LT,{expanded:b}))`,
	} {
		if n := len(pluginsSettingsSectionRe.FindAllStringIndex(src, -1)); n != 1 {
			t.Fatalf("%s: %d matches, want 1", name, n)
		}
		out := pluginsSettingsSectionRe.ReplaceAllString(src, pluginsSettingsSection)
		react := "E"
		if name == "2.33" {
			react = "D"
		}
		want := `a?.(),window.__agyPluginsSection?` + react + `.createElement(window.__agyPluginsSection,{React:` + react + `}):null,`
		if !strings.Contains(out, want) {
			t.Fatalf("%s: section not placed after the customizer:\n%s", name, out)
		}
		if strings.Count(out, "__agyPluginsSection?") != 1 {
			t.Fatalf("%s: section placed more than once:\n%s", name, out)
		}
	}
	// The advanced group, which has the same screen name, is left alone.
	if pluginsSettingsSectionRe.MatchString(`E.createElement(NV,{screen:"Appearance",advanced:!0}),a?.(),`) {
		t.Fatal("matched the advanced group")
	}
}
