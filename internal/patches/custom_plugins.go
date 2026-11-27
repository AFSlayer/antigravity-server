package patches

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/AFSlayer/antigravity-server/internal/customplugins"
)

// The user's custom plugins (internal/customplugins): the page loads the loader, which loads the enabled plugins and
// defines window.__agyCustomPluginsSection, and Settings > Appearance draws that section after the theme settings.
// The Appearance screen renders its first group of settings and then the theme customizer, `a?.()`; the section goes
// right after the customizer. Before 2.35 the first group took a filter for the Chat section.
//
// The screen's code is matched token for token, so a release that breaks its lines is still matched. The screen name
// and the brace before it are written whole, as the minifier never breaks them, so that the search starts from their
// few places in a bundle and not from the settings that name the screen too. Names and the space between tokens are
// bounded: with no unbounded repeat, a window around one of those places that has no match is not widened to a
// quarter of a megabyte, and the search takes about 15 ms instead of 55.
const (
	customPluginsName  = `[A-Za-z_$][\w$]{0,63}`
	customPluginsSpace = `\s{0,16}`
)

// customPluginsTokens joins tokens with the space a release may put between them.
func customPluginsTokens(tokens ...string) string { return strings.Join(tokens, customPluginsSpace) }

// Group 1 is everything up to the customizer's comma, group 2 the React the screen draws with, and groups 3 and 4 the
// Chat filter's argument and the name it compares, which have to be one name.
var customPluginsSectionRe = regexp.MustCompile(`(` +
	customPluginsTokens(`(`+customPluginsName+`)`, `\.`, `createElement`, `\(`, customPluginsName, `,`, `\{screen:"Appearance"`) +
	`(?:` + customPluginsSpace +
	customPluginsTokens(`,`, `sectionFilter`, `:`, `(`+customPluginsName+`)`, `=>`, `(`+customPluginsName+`)`, `===`, `"Chat"`) +
	`)?` + customPluginsSpace +
	customPluginsTokens(`\}`, `\)`, `,`, customPluginsName, `\?\.`, `\(`, `\)`, `,`) + `)`)

const customPluginsSection = `${1}window.__agyCustomPluginsSection?${2}.createElement(window.__agyCustomPluginsSection,{React:${2}}):null,`

// customPluginsFilterIsOneName turns down a Chat filter that compares a name other than its argument.
func customPluginsFilterIsOneName(g [][]byte) bool { return bytes.Equal(g[3], g[4]) }

// hasAppearanceScreen says whether the bundle has the Appearance screen the section goes in. While it does and the
// section's pattern does not match, the patch fails and the doctor says so, instead of the section going missing
// without a word.
func hasAppearanceScreen(b []byte) bool { return bytes.Contains(b, []byte(`screen:"Appearance"`)) }

// customPluginsLoaderScript loads the loader, which is the same for every page and kept by the browser, so it costs no
// round trip after the first visit; the loader then asks for the enabled plugins without holding up the page. It is the
// last of the head injections, so the inline helpers before it run without waiting for it.
var customPluginsLoaderScript = `<script id="agy-custom-plugins-loader" src="` + customplugins.LoaderURL + `"></script>`
