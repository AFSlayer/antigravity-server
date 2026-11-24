package patches

import "regexp"

// The function that picks the RPC transport, in each form it has had, sharing its head so that a search starts from
// "function". Up to 2.22 it returns whether to use the WebSocket; 2.23 to 2.34 return "off", "explicit" or "hub"; 2.35
// reads the parameter inline and falls back to "default". Each form is written token for token, so a release that
// breaks its lines elsewhere is still matched.
var websocketTransport = func() (s struct {
	re                                     *regexp.Regexp
	names                                  map[string][]int
	name, body, explicit, hub, legacy, now int
}) {
	var b shapeBuilder
	b.raw(`function\s+`)
	s.name = b.open()
	b.raw(`[a-zA-Z0-9_$]+`)
	b.close()
	b.raw(`\s*`)
	b.code(`(){`)
	b.raw(`\s*(?:`)
	s.body = b.open()
	b.raw(`(?:var|let|const)\s+`)
	b.code(`«params»=new URLSearchParams(`)
	b.raw(`\s*(?:window\s*\.\s*)?`)
	b.code(`location.search),«flag»=«params».`)
	// Written whole, as the minifier never breaks it, so that the search starts from its one place in a bundle.
	b.raw(`get\("useWebSocket"\)`)
	b.raw(`\s*`)
	b.code(`;return «flag»!==null?«flag»==="true"`)
	b.close()
	b.raw(`\s*(?:`)
	s.explicit = b.open()
	b.code(`?"explicit":"off"`)
	b.close()
	b.raw(`\s*`)
	s.hub = b.open()
	b.code(`:«hubParams».get("wsTransport")`)
	b.close()
	b.raw(`\s*`)
	b.code(`==="3"?"hub":"off"`)
	b.raw(`|`)
	s.legacy = b.open()
	b.code(`:«legacyParams».get("wsTransport")`)
	b.close()
	b.raw(`\s*`)
	b.code(`==="2"`)
	b.raw(`)|`)
	s.now = b.open()
	b.raw(`(?:var|let|const)\s+`)
	b.code(`«nowFlag»=`)
	b.raw(`\s*\(?\s*`)
	b.code(`new URLSearchParams(`)
	b.raw(`\s*(?:window\s*\.\s*)?`)
	b.code(`location.search)`)
	b.raw(`\s*\)?\s*\.get\("useWebSocket"\)\s*`)
	b.code(`;return «nowFlag»!==null?«nowFlag»==="true"?"explicit":"off":`)
	b.close()
	b.raw(`\s*`)
	b.code(`"default"`)
	b.raw(`)\s*`)
	b.code(`}`)
	s.re, s.names = regexp.MustCompile(b.String()), b.names
	return s
}()

// websocketTransportIsKnown says whether a match reads the wsTransport parameter from the same URLSearchParams it read
// useWebSocket from, and the one name it read that into in every place.
func websocketTransportIsKnown(g [][]byte) bool {
	if !sameNames(g, websocketTransport.names) {
		return false
	}
	at := func(role string) []byte { return g[websocketTransport.names[role][0]] }
	for _, role := range []string{"hubParams", "legacyParams"} {
		if at(role) != nil && string(at(role)) != string(at("params")) {
			return false
		}
	}
	return true
}

func renderWebsocketTransport(g [][]byte) string {
	head := `function ` + string(g[websocketTransport.name]) + `(){`
	switch {
	case g[websocketTransport.now] != nil:
		return head + string(g[websocketTransport.now]) + `new URLSearchParams(window.location.search).get("wsTransport")==="1"?"off":"explicit"}`
	case g[websocketTransport.legacy] != nil:
		return head + string(g[websocketTransport.body]) + string(g[websocketTransport.legacy]) + `!=="1"}`
	}
	return head + string(g[websocketTransport.body]) + string(g[websocketTransport.explicit]) + string(g[websocketTransport.hub]) + `==="1"?"off":"explicit"}`
}

// The row of a skill or a rule in Customizations, up to the flags it reads its description into. Groups 1 to 6 are what
// settings-doc-row-editor's replacement has always gone by: the head of the row with its open state, the description,
// the open state and its setter, React, and the flags; the names in the flags come after.
var docRow = func() (s struct {
	re    *regexp.Regexp
	names map[string][]int
}) {
	var b shapeBuilder
	b.open()
	b.raw(`function\s+[a-zA-Z0-9_$]+\s*`)
	b.code(`({title:a,displayPath:b,copyPath:c,description:«description»,content:f,modalChildren:g,badge:h,actions:k,actionsPaddingClass:l,disabled:m=!1,MarkdownRenderer:n}){var[«open»,«setOpen»]=(0,«React».useState)(!1),`)
	b.close()
	b.raw(`\s*`)
	b.open()
	b.code(`«hasDetail»=!!«description»||!!f||!!g,«shown»=«description»||b;`)
	b.close()
	s.re, s.names = regexp.MustCompile(b.String()), b.names
	return s
}()
