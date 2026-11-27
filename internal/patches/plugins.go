package patches

import (
	"bytes"
	"regexp"

	"github.com/AFSlayer/antigravity-server/internal/plugins"
)

// The user's UI plugins (internal/plugins): the page loads the loader, which loads the enabled plugins and defines
// window.__agyPluginsSection, and Settings > Appearance draws that section after the theme settings. The Appearance screen renders its first group of settings and then the theme customizer, `a?.()`; the
// section goes right after the customizer. Before 2.35 the first group took a filter for the Chat section.
var pluginsSettingsSectionRe = regexp.MustCompile(
	`(([A-Za-z_$][\w$]*)\.createElement\([A-Za-z_$][\w$]*,\{screen:"Appearance"` +
		`(?:,sectionFilter:[A-Za-z_$][\w$]*=>[A-Za-z_$][\w$]*==="Chat")?\}\),\s*[A-Za-z_$][\w$]*\?\.\(\),)`)

const pluginsSettingsSection = `${1}window.__agyPluginsSection?${2}.createElement(window.__agyPluginsSection,{React:${2}}):null,`

// hasAppearanceScreen says whether the bundle has the Appearance screen the section goes in.
func hasAppearanceScreen(b []byte) bool {
	return bytes.Contains(b, []byte(`title:"Appearance",description:`))
}

// pluginsLoaderScript loads the loader, which is the same for every page and kept by the browser, so it costs no
// round trip after the first visit; the loader then asks for the enabled plugins without holding up the page.
var pluginsLoaderScript = `<script id="agy-plugins-loader" src="` + plugins.LoaderURL + `"></script>`
