package patches

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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

// userMessageButtonsOnTouchRe and autoscrollPhysicalDistanceRe match what 2.31 does itself in place of
// mobile-user-message-actions and autoscroll-distance-fix: user message buttons that only hide where the screen can
// hover, and a hook that measures the distance to the bottom from the position.
var userMessageButtonsOnTouchRe = regexp.MustCompile(`user-input-buttons-container[^"]*"\s*,\s*"can-hover:opacity-0 can-hover:pointer-events-none`)
var autoscrollPhysicalDistanceRe = regexp.MustCompile(`=Math\.max\(0,\s*[a-zA-Z0-9_$]+\.scrollHeight-[a-zA-Z0-9_$]+\.clientHeight\),\s*[a-zA-Z0-9_$]+=[a-zA-Z0-9_$]+-[a-zA-Z0-9_$]+<=[a-zA-Z0-9_$]+,`)

// rowSizedInCqhRe matches the inline height scroll-input-tracker recognizes tall rows by. Since 2.30 the bundle builds
// it in a helper that the rows' minHeight calls.
var rowSizedInCqhRe = regexp.MustCompile(`minHeight:[^{}]{0,80}cqh|return` + "`" + `calc\(\d+cqh`)

// userStatusNoResetOnErrorRe matches builds where UserStatusSync logs a failed getUserStatus call without wiping
// Redux userStatus (2.28 and earlier, or a future build that drops the reset upstream). Anchoring from catch{ ensures a
// reset moved before console.warn does not slip past the guard.
var userStatusNoResetOnErrorRe = regexp.MustCompile(`catch\s*\(\s*[a-zA-Z0-9_$]+\s*\)\s*\{\s*(?:[a-zA-Z0-9_$]+\(\)\s*[;,]\s*)?console\.warn\(\s*"\[UserStatusSync\] Failed to fetch user status:"\s*,\s*[a-zA-Z0-9_$]+\s*\)(?:\s*,\s*!?\s*[a-zA-Z0-9_$]+\(\)\s*(?:\|\||&&)\s*[a-zA-Z0-9_$]+(?:\?\.)?\(\))?\s*\}`)

func regexpFindSubmatch(re *regexp.Regexp, body []byte) [][]byte {
	loc := regexpFindSubmatchIndex(re, body)
	if loc == nil {
		return nil
	}
	return submatches(body, loc)
}

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
			wantOne := !p.Optional || (p.Precondition != nil && p.Precondition(body))
			if n := bytes.Count(body, []byte(p.Find)); n != 1 && (wantOne || n != 0) {
				t.Errorf("%s: anchor matched %d times in the live bundle, want 1", p.ID, n)
			}
		case Regexp:
			wantOne := !p.Optional || (p.Precondition != nil && p.Precondition(body))
			if n := len(regexpFindAllIndex(p.FindRe, body, -1)); n != 1 && (wantOne || n != 0) {
				t.Errorf("%s: regexp matched %d times in the live bundle, want 1", p.ID, n)
			}
		}
	}

	// connection-prewarm hands the bundle a socket it opened early. That only
	// works while the bundle builds the socket this way and assigns onopen
	// straight after, so check the shape it relies on.
	if n := len(regexpFindAllIndex(socketConstructionRe, mainJS, -1)); n != 1 {
		t.Errorf("the bundle builds its RPC socket in an unexpected way (%d matches); connection-prewarm may stop working", n)
	}

	// scroll-input-tracker finds these by name, and nothing else would notice them going away. Its scrollbar selectors
	// are not listed because older bundles have no overlay scrollbar.
	if !regexpMatch(findBarInputRe, mainJS) {
		t.Error("the bundle no longer has the find bar's input; scroll-input-tracker may stop working")
	}
	if !regexpMatch(rowSizedInCqhRe, mainJS) {
		t.Error("the bundle no longer sizes rows with an inline cqh height; scroll-input-tracker may stop following after a collapse")
	}
	if !bytes.Contains(mainJS, []byte("autoscroll-viewport")) {
		t.Error("the bundle no longer has autoscroll-viewport; scroll-input-tracker may stop working")
	}
	if !bytes.Contains(mainJS, []byte("agent-input-box")) {
		t.Error("the bundle no longer has agent-input-box; keyboard-detect may stop working")
	}

	// mobile-user-message-actions and autoscroll-distance-fix are optional because 2.31 does both itself. A bundle with
	// neither the code they patch nor that would lose them without a word.
	if !regexpMatch(mobileUserMessageActionsRe, mainJS) && !regexpMatch(userMessageButtonsOnTouchRe, mainJS) {
		t.Error("mobile-user-message-actions: the bundle neither has the buttons the patch shows nor shows them on touch screens itself")
	}
	if !regexpMatch(autoscrollDistanceFixRe, mainJS) && !regexpMatch(autoscrollPhysicalDistanceRe, mainJS) {
		t.Error("autoscroll-distance-fix: the auto-scroll hook neither has the check the patch fixes nor measures the position itself")
	}

	// paging-trim-waits-for-still is optional for bundles without this paging code, and would leave rows dropping under
	// a moving reader without a word.
	if bytes.Contains(mainJS, []byte("oppositeDirectionCooldownMs")) && !regexpMatch(pagingTrimWaitsRe, mainJS) {
		t.Error("paging-trim-waits-for-still: the paging code is there but its update decision was not found; rows above drop while the reader scrolls")
	}

	// user-status-keep-on-error is optional for 2.28 and earlier, which did not clear userStatus in catch. A newer
	// bundle whose UserStatusSync rewraps without matching would silently wipe loaded models on transient socket drops.
	if !regexpMatch(userStatusKeepOnErrorRe, mainJS) && !regexpMatch(userStatusNoResetOnErrorRe, mainJS) {
		t.Error("user-status-keep-on-error: UserStatusSync neither matches the patch nor leaves userStatus intact on fetch errors")
	}

	// mobile-home-new-conversation-row is optional for older bundles, and would fall back to the row in the list without
	// a word. It tells the mobile home apart by the navigation component's conciergeTestId, named in its signature.
	if bytes.Contains(mainJS, []byte(`showNewConversation:!1`)) {
		if !regexpMatch(mobileHomeNewConvoRowRe, mainJS) {
			t.Error("mobile-home-new-conversation-row: the navigation rows were not found; the mobile home falls back to the row in the list")
		}
		if !bytes.Contains(mainJS, []byte(`conciergeTestId:"mobile-home-concierge"`)) {
			t.Error("mobile-home-new-conversation-row: the mobile home no longer identifies itself as the patch expects")
		}
	}

	// mobile-back-clears-section also tells the titlebar's home hook to treat a selected project as away from home, so
	// the back button shows there. That part is optional for older bundles and would fail without a word, so check
	// that it matches and that the hook it flags is the one mobile-new-convo-header rewrites.
	if header := regexpFindSubmatch(mobileNewConvoHeaderRe, mainJS); header == nil {
		t.Error("mobile-new-convo-header: the home hook was not found")
	} else if back := regexpFindSubmatch(mobileBackClearsSectionRe, mainJS); back == nil || len(back[1]) == 0 {
		t.Error("mobile-back-clears-section: the titlebar around the back button was not found; the back button will not show for a selected project")
	} else if hook := regexp.MustCompile(`b=([a-zA-Z0-9_$]+)\($`).FindSubmatch(back[1]); hook == nil || !bytes.Equal(hook[1], header[1]) {
		t.Errorf("mobile-back-clears-section: the titlebar flags %q, not the home hook %q", hook, header[1])
	}

	// 2.31+ sidecar gateway, loopback preview, and remote OAuth patches are marked optional for older bundles,
	// so assert they match whenever the corresponding feature exists in the live bundle.
	if bytes.Contains(mainJS, []byte("resolveSidecarUrl:")) && !regexpMatch(sidecarResolveURLRe, mainJS) {
		t.Error("sidecar-resolve-url: resolveSidecarUrl exists in bundle but patch regexp did not match")
	}
	if bytes.Contains(mainJS, []byte(".webPort")) && bytes.Contains(mainJS, []byte("http://127.0.0.1:")) && !regexpMatch(sidecarModalEndpointRe, mainJS) {
		t.Error("sidecar-modal-endpoint-url: sidecar modal endpoint exists in bundle but patch regexp did not match")
	}
	if bytes.Contains(mainJS, []byte(`"add-artifact-comment"`)) && !regexpMatch(sidecarCommentOriginRe, mainJS) {
		t.Error("sidecar-artifact-comment-origin: the sidecar panel's comment handler exists in bundle but patch regexp did not match")
	}
	if bytes.Contains(mainJS, []byte(`dataset.testid="sidecar-iframe"`)) && !regexpMatch(sidecarIframeSandboxRe, mainJS) {
		t.Error("sidecar-iframe-sandbox: the sidecar panel's iframe exists in bundle but patch regexp did not match")
	}
	// These run when the registry's precondition holds or the feature shows
	// in the bundle another way, so a precondition gone stale still fails.
	if (preconditionHolds("loopback-preview-feature-gate", mainJS) ||
		bytes.Contains(mainJS, []byte("browserAuxPane:")) && bytes.Contains(mainJS, []byte("openLoopbackInPreviewByDefault"))) &&
		!regexpMatch(loopbackPreviewFeatureGateRe, mainJS) {
		t.Error("loopback-preview-feature-gate: browserAuxPane loopback preview exists in bundle but patch regexp did not match")
	}
	if bytes.Contains(mainJS, []byte("fetchSandboxPort:")) && !regexpMatch(loopbackPreviewFetchFallbackRe, mainJS) {
		t.Error("loopback-preview-fetch-fallback: fetchSandboxPort exists in bundle but patch regexp did not match")
	}
	pluginSignIn := bytes.Contains(mainJS, []byte("startPluginAuthSignIn:")) && bytes.Contains(mainJS, []byte(`get("pluginAuth")`))
	if (pluginSignIn || preconditionHolds("router-navigate-bridge", mainJS)) && !regexpMatch(routerNavigateBridgeRe, mainJS) {
		t.Error("router-navigate-bridge: router useNavigate hook did not match")
	}
	if (pluginSignIn || preconditionHolds("plugin-mention-remote-signin", mainJS)) && !regexpMatch(pluginMentionRemoteSignInRe, mainJS) {
		t.Error("plugin-mention-remote-signin: plugin @-mention sign-in hook did not match")
	}
	if bytes.Contains(mainJS, []byte("completeMcpOAuth:")) && !regexpMatch(mcpOAuthRemoteCallbackRe, mainJS) {
		t.Error("mcp-oauth-remote-callback: completeMcpOAuth exists in bundle but patch regexp did not match")
	}
	if bytes.Contains(mainJS, []byte("signInPluginAuthProvider:")) && !regexpMatch(pluginOAuthRemoteCallbackRe, mainJS) {
		t.Error("plugin-oauth-remote-callback: signInPluginAuthProvider exists in bundle but patch regexp did not match")
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
			for _, hook := range []string{
				"window.__agySyncMediaState",
				"window.__agyShouldStreamSingle",
				"window.__agyPartitionFiles",
			} {
				if !bytes.Contains(patched, []byte(hook)) {
					t.Errorf("patched live MainJS missing %s (file-upload-large-file-streaming-fallback partial match regression)", hook)
				}
			}
			tmp := filepath.Join(t.TempDir(), "debug_patched_bundle.js")
			if err := os.WriteFile(tmp, patched, 0o600); err != nil {
				t.Fatalf("write temporary patched bundle: %v", err)
			}
			cmd := exec.Command("node", "--check", tmp)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("JavaScript Syntax Error in patched bundle: %v\nOutput: %s", err, string(out))
			}
		} else if target.target == HTML {
			for _, helper := range []string{
				"window.__agyResolveSidecarUrl",
				"window.__agyFetchLoopbackPort",
				"window.__agyOpenPluginAuth",
				"window.__agyCompleteRemoteOAuth",
			} {
				if !bytes.Contains(patched, []byte(helper)) {
					t.Errorf("patched live HTML missing %s", helper)
				}
			}
		}
	}
}

// preconditionHolds reports whether the registry's precondition for the patch
// id holds for body.
func preconditionHolds(id string, body []byte) bool {
	for _, p := range All() {
		if p.ID == id {
			return p.Precondition != nil && p.Precondition(body)
		}
	}
	return false
}
