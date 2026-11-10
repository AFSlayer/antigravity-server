package patches

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"testing"

	"github.com/AFSlayer/antigravity-server/internal/lsproc"
)

// socketConstructionRe matches how the bundle opens its RPC socket and assigns
// onopen in the same synchronous run, which the connection prewarm relies on.
// Up to 2.22 onopen is assigned in the statement right after a flag; 2.23
// declares a dial timer first, so the declaration list may run up to the
// statement that assigns onopen.
var socketConstructionRe = regexp.MustCompile(`new WebSocket\(this\.options\.url\);\s*[a-zA-Z0-9_$]+\.binaryType="arraybuffer";\s*var [a-zA-Z0-9_$]+=(?:!1|Date\.now\(\),[\s\S]{0,1000}?);[a-zA-Z0-9_$]+\.onopen=`)

// findBarInputRe matches the input scroll-input-tracker looks for. The bundle can wrap lines between any two tokens.
var findBarInputRe = regexp.MustCompile(`"input",\s*\{[^{}]*"aria-label":\s*"Find"`)

// rowSizedInCqhRe matches the inline height scroll-input-tracker recognizes tall rows by. Since 2.30 the bundle builds
// it in a helper that the rows' minHeight calls.
var rowSizedInCqhRe = regexp.MustCompile(`minHeight:[^{}]{0,80}cqh|return` + "`" + `calc\(\d+cqh`)

// TestAnchorsMatchLiveBundle is the check that actually proves the patches still
// work. It runs only when a standalone Antigravity language server is reachable,
// so CI skips it and no part of Antigravity's bundle needs to live in this
// repository. Run it locally with the desktop app open before cutting a release.
func TestAnchorsMatchLiveBundle(t *testing.T) {
	instance, err := lsproc.Find()
	if err != nil {
		t.Skip("no standalone Antigravity language server running; start the desktop app to run this test")
	}

	mainJS, _, err := instance.Fetch("/main.js")
	if err != nil {
		t.Fatalf("fetch /main.js: %v", err)
	}
	indexHTML, _, err := instance.Fetch("/")
	if err != nil {
		t.Fatalf("fetch /: %v", err)
	}

	for _, p := range All() {
		body := mainJS
		if p.Target == HTML {
			body = indexHTML
		}

		switch p.Kind {
		case Literal:
			if n := bytes.Count(body, []byte(p.Find)); n != 1 && (!p.Optional || n != 0) {
				t.Errorf("%s: anchor matched %d times in the live bundle, want 1", p.ID, n)
			}
		case Regexp:
			if n := len(p.FindRe.FindAll(body, -1)); n != 1 && (!p.Optional || n != 0) {
				t.Errorf("%s: regexp matched %d times in the live bundle, want 1", p.ID, n)
			}
		}
	}

	// connection-prewarm hands the bundle a socket it opened early. That only
	// works while the bundle builds the socket this way and assigns onopen
	// straight after, so check the shape it relies on.
	if n := len(socketConstructionRe.FindAll(mainJS, -1)); n != 1 {
		t.Errorf("the bundle builds its RPC socket in an unexpected way (%d matches); connection-prewarm may stop working", n)
	}

	// scroll-input-tracker finds these by name, and nothing else would notice them going away. Its scrollbar selectors
	// are not listed because older bundles have no overlay scrollbar.
	if !findBarInputRe.Match(mainJS) {
		t.Error("the bundle no longer has the find bar's input; scroll-input-tracker may stop working")
	}
	if !rowSizedInCqhRe.Match(mainJS) {
		t.Error("the bundle no longer sizes rows with an inline cqh height; scroll-input-tracker may stop following after a collapse")
	}
	if !bytes.Contains(mainJS, []byte("autoscroll-viewport")) {
		t.Error("the bundle no longer has autoscroll-viewport; scroll-input-tracker may stop working")
	}

	// paging-trim-waits-for-still is optional for bundles without this paging code, and would leave rows dropping under
	// a moving reader without a word.
	if bytes.Contains(mainJS, []byte("oppositeDirectionCooldownMs")) && !pagingTrimWaitsRe.Match(mainJS) {
		t.Error("paging-trim-waits-for-still: the paging code is there but its update decision was not found; rows above drop while the reader scrolls")
	}

	// mobile-home-new-conversation-row is optional for older bundles, and would fall back to the row in the list without
	// a word. It also tells the mobile home apart by the navigation component's third parameter.
	if bytes.Contains(mainJS, []byte(`showNewConversation:!1`)) {
		if !mobileHomeNewConvoRowRe.Match(mainJS) {
			t.Error("mobile-home-new-conversation-row: the navigation rows were not found; the mobile home falls back to the row in the list")
		}
		if !bytes.Contains(mainJS, []byte(`conciergeTestId:"mobile-home-concierge"`)) {
			t.Error("mobile-home-new-conversation-row: the mobile home no longer identifies itself as the patch expects")
		}
	}

	// mobile-back-clears-section also tells the titlebar's home hook to treat a selected project as away from home, so
	// the back button shows there. That part is optional for older bundles and would fail without a word, so check
	// that it matches and that the hook it flags is the one mobile-new-convo-header rewrites.
	if header := mobileNewConvoHeaderRe.FindSubmatch(mainJS); header == nil {
		t.Error("mobile-new-convo-header: the home hook was not found")
	} else if back := mobileBackClearsSectionRe.FindSubmatch(mainJS); back == nil || len(back[1]) == 0 {
		t.Error("mobile-back-clears-section: the titlebar around the back button was not found; the back button will not show for a selected project")
	} else if hook := regexp.MustCompile(`b=([a-zA-Z0-9_$]+)\($`).FindSubmatch(back[1]); hook == nil || !bytes.Equal(hook[1], header[1]) {
		t.Errorf("mobile-back-clears-section: the titlebar flags %q, not the home hook %q", hook, header[1])
	}

	opts := Options{MobileUX: true, WorkspaceRoot: "/tmp/workspace"}
	opts.CacheKey = CacheKey("test", opts)

	for _, target := range []struct {
		body   []byte
		target Target
	}{{mainJS, MainJS}, {indexHTML, HTML}} {
		patched, report := Apply(target.target, target.body, opts)
		if len(report.Missing()) > 0 {
			for _, r := range report.Missing() {
				t.Errorf("%s did not apply to the live %s", r.ID, target.target)
			}
		}
		if target.target == MainJS {
			tmp := "/tmp/debug_patched_bundle.js"
			_ = os.WriteFile(tmp, patched, 0644)
			cmd := exec.Command("node", "--check", tmp)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("JavaScript Syntax Error in patched bundle: %v\nOutput: %s", err, string(out))
			}
		}
	}
}
