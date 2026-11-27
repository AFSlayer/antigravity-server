package patches

import (
	"regexp"

	"github.com/AFSlayer/antigravity-server/internal/customplugins"
)

// customPluginIdent is a minified name, bounded so the search does not run through long words in the bundle.
const customPluginIdent = `[A-Za-z_$][\w$]{0,63}`

// The user's custom plugins (internal/customplugins): the page loads the loader, which loads the enabled plugins and
// defines window.__agyCustomPluginsSection, and Settings > Appearance draws that section after the theme settings.
// The Appearance screen renders its first group of settings and then the theme customizer, `a?.()`; the section goes
// right after the customizer. Before 2.35 the first group took a filter for the Chat section. The pattern allows
// space between any two tokens, so a bundle that keeps line breaks still matches.
var customPluginsSectionRe = regexp.MustCompile(
	`((` + customPluginIdent + `)\s*\.\s*createElement\s*\(\s*` + customPluginIdent + `\s*,\s*\{\s*screen\s*:\s*"Appearance"\s*` +
		`(?:,\s*sectionFilter\s*:\s*` + customPluginIdent + `\s*=>\s*` + customPluginIdent + `\s*===\s*"Chat"\s*)?` +
		`\}\s*\)\s*,\s*` + customPluginIdent + `\s*\?\.\s*\(\s*\)\s*,)`)

const customPluginsSection = `${1}window.__agyCustomPluginsSection?${2}.createElement(window.__agyCustomPluginsSection,{React:${2}}):null,`

// appearanceScreenRe finds the Appearance screen the section goes in. While it is there and the section's pattern does
// not match, the patch fails and the doctor says so, instead of the section going missing without a word.
var appearanceScreenRe = regexp.MustCompile(`screen\s*:\s*"Appearance"`)

func hasAppearanceScreen(b []byte) bool { return appearanceScreenRe.Match(b) }

// customPluginsLoaderScript loads the loader, which is the same for every page and kept by the browser, so it costs no
// round trip after the first visit; the loader then asks for the enabled plugins without holding up the page. It is the
// last of the head injections, so the inline helpers before it run without waiting for it.
var customPluginsLoaderScript = `<script id="agy-custom-plugins-loader" src="` + customplugins.LoaderURL + `"></script>`
