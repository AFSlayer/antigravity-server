package patches

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"testing"

	"github.com/AFSlayer/antigravity-server/internal/lsproc"
)

// socketConstructionRe matches how the bundle opens its RPC socket.
var socketConstructionRe = regexp.MustCompile(`new WebSocket\(this\.options\.url\);\s*[a-zA-Z0-9_$]+\.binaryType="arraybuffer";\s*var [a-zA-Z0-9_$]+=!1;\s*[a-zA-Z0-9_$]+\.onopen=`)

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
