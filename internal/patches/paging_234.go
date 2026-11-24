package patches

// Since 2.34 the bundle's paging decides whether rows are added above from a name of its own, and the component that
// keeps the rows on screen in place finds a row drawn again by its id, keeps what it could not move above the top for
// its next move, and marks the moves it makes. These are those forms, as groups after the ones of the forms before, so
// one pattern takes either.

// The paging's update decision: whether to wait before asking for the new window, which the trim and the growth above
// are held at as before. grows is whether rows are added at the top, growsEnd whether they are at the bottom. Since
// 2.35 a window it waits for first clears what is shown of loads at the ends it does not grow, with the two names
// negated; those are written as roles of their own, which pagingTrimWaitsIsKnown holds to grows and growsEnd. The waits
// added here leave that status as it is: they ask again in a moment, and the decision clears it once it no longer
// waits for the tracker, so what is shown of a finished load stays for that moment longer.
var pagingTrimWaits234 = func() (s struct {
	pattern string
	names   map[string][]int
	upTo    int
}) {
	b := shapeBuilder{groups: pagingTrimWaitsGroups}
	s.upTo = b.open()
	b.code(`var «start»=«next».startIndex,«grows»=«start»<«now».startIndex;if(«skip»(«a»,«next»,«b»))`)
	b.raw(`(?:\s*`)
	b.code(`«settle»(!«settleGrows»,!«settleGrowsEnd»),`)
	b.raw(`)?\s*`)
	b.code(`(«grows»||«growsEnd»)&&«retry»(«config»`)
	// Written whole, as the minifier does, so that the search starts from the few places that read it.
	b.raw(`\.timing\.oppositeDirectionCooldownMs\s*`)
	b.code(`||«config».timing.retryDelayMs);`)
	b.close()
	b.raw(`\s*`)
	b.code(`else{`)
	s.pattern, s.names = b.String(), b.names
	return s
}()

// pagingTrimWaitsIsKnown says whether a match of either form is the paging decision: every role one name, and in the
// 2.35 form the call that clears the loads' status negating the same two names the retry goes by.
func pagingTrimWaitsIsKnown(g [][]byte) bool {
	if !sameNames(g, pagingTrimWaits234.names) {
		return false
	}
	at := func(role string) []byte { return g[pagingTrimWaits234.names[role][0]] }
	if at("settle") == nil {
		return true
	}
	return string(at("settleGrows")) == string(at("grows")) && string(at("settleGrowsEnd")) == string(at("growsEnd"))
}

// pagingTrimWaitsGroups is the number of groups in the forms before 2.34.
const pagingTrimWaitsGroups = 5

func renderPagingTrimWaits234(g [][]byte) string {
	name := func(role string) string { return string(g[pagingTrimWaits234.names[role][0]]) }
	grows := "(" + name("grows") + "||" + name("growsEnd") + ")"
	next, now, retry := name("next"), name("now"), name("retry")
	return string(g[pagingTrimWaits234.upTo]) +
		`else if(!` + grows + `&&` + next + `.startIndex>` + now + `.startIndex&&window.__agyDeferTrim?.())` + retry + `(250);` +
		`else if(` + next + `.startIndex<` + now + `.startIndex&&window.__agyDeferGrow?.())` + retry + `(100);else{`
}

// The component's update: it finds the row to keep, by the element or else by its id, and then moves the position.
// The move is handed to the scroll tracker as before, and the component's own state for it is cleared, so what it
// would have kept for a later move is the tracker's to make up.
var pagingRowsKept234 = func() (s struct {
	pattern           string
	names             map[string][]int
	start, find, move int
}) {
	b := shapeBuilder{groups: pagingRowsKeptGroups}
	s.start = b.open()
	b.code(`componentDidUpdate(«a»,«b»,«c»){`)
	b.close()
	s.find = b.open()
	b.code(`if((«a»=this.props.viewportRef.current)&&«c»&&«c».length!==0){«label»:{«b»=this.props.getAnchorCandidates;var «d»=`)
	// Written whole, as the minifier does, so that the search starts from the one place that reads it rather than from
	// the hundreds of getBoundingClientRect calls in a bundle.
	b.raw(`\s*this\.props\.getAnchorId\s*`)
	b.code(`;for(var «f» of «c»){if(«f».element.isConnected){var «g»={anchor:«f»,element:«f».element};break «label»}if(«f».id!==null&&«d»){if(!«g»){«g»=new Map;for(var «h» of «b»()){«c»=«d»(«h»);if(«c»===null||«g».has(«c»))continue;let{width:«w»,height:«ht»}=«h».getBoundingClientRect();«w»===0&&«ht»===0||«g».set(«c»,«h»)}}if(«c»=«g».get(«f».id)){«g»={anchor:«f»,element:«c»};break «label»}}}«g»=null}`)
	b.close()
	s.move = b.open()
	b.code(`«g»?(«f»=«g».element.getBoundingClientRect().top-«a».getBoundingClientRect().top-«g».anchor.offset,«g»=«a».scrollTop,«f»=«g»+«f»+this.clampedDelta,«h»=Math.max(0,«f»),Math.abs(«h»-«g»)>=.5&&(«a».scrollTop=«h»),«h»=«a».scrollTop,«h»!==«g»&&«marks».set(«a»,{targetTop:«h»,markedAt:performance.now()}),«f»<-.5?(this.clampedDelta=«f»-«h»,this.clampedAtScrollTop=«h»,this.listeningViewport!==«a»&&(this.listeningViewport&&this.listeningViewport.removeEventListener("scroll",this.handleViewportScroll),«a».addEventListener("scroll",this.handleViewportScroll,{passive:!0}),this.listeningViewport=«a»)):«reset»(this)):«reset»(this)`)
	b.close()
	b.code(`}}`)
	s.pattern, s.names = b.String(), b.names
	return s
}()

// pagingRowsKeptGroups is the number of groups in the forms before 2.34.
const pagingRowsKeptGroups = 4

func renderPagingRowsKept234(g [][]byte) string {
	name := func(role string) string { return string(g[pagingRowsKept234.names[role][0]]) }
	a, row, reset := name("a"), name("g"), name("reset")
	return string(g[pagingRowsKept234.start]) +
		`window.__agyKeepRows&&window.__agyRowsDrawn?.(this.props.viewportRef.current,this.props.getAnchorCandidates);` +
		string(g[pagingRowsKept234.find]) +
		`window.__agyKeepRows?(` + row + `&&window.__agyKeepRows(` + a + `,` + row + `.element,` + row + `.anchor.offset),` + reset + `(this)):` +
		string(g[pagingRowsKept234.move]) + `}}`
}
