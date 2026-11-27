package patches

import (
	"strings"
	"testing"
)

// The Custom Plugins section goes right after the theme customizer of the Appearance screen, in each form the screen
// has had: 2.35, and before it, where the first group of settings takes a filter for the Chat section. A bundle that
// keeps its line breaks matches too.
func TestCustomPluginsSectionAfterThemeCustomizer(t *testing.T) {
	for name, src := range map[string]string{
		"2.35":   `return E.createElement(YU,{title:"Appearance",description:"x"},E.createElement(NV,{screen:"Appearance"}),a?.(),E.createElement(zV,{expanded:b},E.createElement(NV,{screen:"Appearance",advanced:!0})))`,
		"2.34":   `return E.createElement(LU,{title:"Appearance",description:"x"},E.createElement(TV,{screen:"Appearance",sectionFilter:f=>f==="Chat"}),a?.(),E.createElement(xV,{expanded:b},E.createElement(TV,{screen:"Appearance",sectionFilter:f=>f!=="Chat"})))`,
		"2.33":   `return D.createElement(ZS,{title:"Appearance",description:"x"},D.createElement(gU,{screen:"Appearance",sectionFilter:f=>f==="Chat"}),a?.(),D.createElement(LT,{expanded:b}))`,
		"spaced": "return E.createElement(YU, { title: \"Appearance\" },\n  E.createElement(NV, { screen: \"Appearance\", sectionFilter: f => f === \"Chat\" }),\n  a?.(),\n  E.createElement(zV, {}))",
	} {
		if n := len(customPluginsSectionRe.FindAllStringIndex(src, -1)); n != 1 {
			t.Fatalf("%s: %d matches, want 1", name, n)
		}
		out := customPluginsSectionRe.ReplaceAllString(src, customPluginsSection)
		react := "E"
		if name == "2.33" {
			react = "D"
		}
		want := `a?.(),window.__agyCustomPluginsSection?` + react + `.createElement(window.__agyCustomPluginsSection,{React:` + react + `}):null,`
		if !strings.Contains(out, want) {
			t.Fatalf("%s: section not placed after the customizer:\n%s", name, out)
		}
		if strings.Count(out, "__agyCustomPluginsSection?") != 1 {
			t.Fatalf("%s: section placed more than once:\n%s", name, out)
		}
	}
	// The advanced group, which has the same screen name, is left alone.
	if customPluginsSectionRe.MatchString(`E.createElement(NV,{screen:"Appearance",advanced:!0}),a?.(),`) {
		t.Fatal("matched the advanced group")
	}
}

// The Appearance screen being there is what makes a miss of the section's pattern a failure the doctor reports.
func TestCustomPluginsSectionPrecondition(t *testing.T) {
	if !hasAppearanceScreen([]byte(`E.createElement(NV,{screen: "Appearance"})`)) {
		t.Fatal("the Appearance screen was not found")
	}
	if hasAppearanceScreen([]byte(`{title:"Appearance"}`)) {
		t.Fatal("a title alone counted as the Appearance screen")
	}
}

// The loader is the last script injected into the head, so the inline helpers before it never wait for it.
func TestCustomPluginsLoaderIsTheLastHeadInjection(t *testing.T) {
	last := ""
	for _, p := range All() {
		if p.Kind == InjectHead {
			last = p.ID
		}
	}
	if last != "custom-plugins-loader" {
		t.Fatalf("the last head injection is %s, want custom-plugins-loader", last)
	}
}
