package patches

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestConnectionPrewarmScriptBehavior runs the head script in Node against a
// stand-in browser, which is the only way to check the claim, replay and
// opt-out logic. It is skipped where Node is not installed.
func TestConnectionPrewarmScriptBehavior(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}

	script := strings.TrimSuffix(strings.TrimPrefix(connectionPrewarmScript, `<script id="agy-connection-prewarm">`), "</script>")
	path := filepath.Join(t.TempDir(), "prewarm.js")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command(node, filepath.Join("testdata", "prewarm_harness.js"), path).CombinedOutput()
	if err != nil {
		t.Fatalf("prewarm script behaved wrongly: %v\n%s", err, out)
	}
}

// TestConnectionPrewarmFollowsTransportPatch keeps the early socket from being
// opened when the bundle will not use it.
func TestConnectionPrewarmFollowsTransportPatch(t *testing.T) {
	html := []byte("<html><head></head><body></body></html>")

	on, _ := Apply(HTML, html, Options{MobileUX: true})
	if !strings.Contains(string(on), `id="agy-connection-prewarm"`) {
		t.Error("prewarm should be injected when the WebSocket transport patch is enabled")
	}

	off, _ := Apply(HTML, html, Options{MobileUX: true, Disabled: map[string]bool{"websocket-transport-default": true}})
	if strings.Contains(string(off), `id="agy-connection-prewarm"`) {
		t.Error("prewarm must not be injected when the WebSocket transport patch is disabled")
	}
}

// TestLivenessProbePatchToleratesLineBreaks guards against the bundle breaking
// its long lines at a different place after an upstream update.
func TestLivenessProbePatchToleratesLineBreaks(t *testing.T) {
	base := regexpFixtures["websocket-liveness-probe-relax"]
	if base == "" {
		t.Fatal("missing fixture")
	}

	breaks := 0
	inString := false
	for i := 0; i < len(base); i++ {
		if base[i] == '"' {
			inString = !inString
		}
		// A line break cannot fall inside a string literal, between the
		// characters of an operator, or inside an identifier or number.
		if inString || !strings.ContainsRune(",;{}()=>", rune(base[i])) {
			continue
		}
		if i+1 < len(base) && strings.ContainsRune("=!<>", rune(base[i])) && strings.ContainsRune("=>", rune(base[i+1])) {
			continue
		}
		body := base[:i+1] + "\n" + base[i+1:]
		out, _ := Apply(MainJS, []byte(body), Options{})
		compact := strings.Join(strings.Fields(string(out)), "")
		if !strings.Contains(compact, "OJ(a,b))},1E4)") {
			t.Fatalf("patch did not apply with a line break after offset %d: %q", i, base[max(0, i-20):min(len(base), i+20)])
		}
		breaks++
	}
	if breaks < 40 {
		t.Fatalf("only %d break positions were exercised", breaks)
	}
}

// TestConnectionPrewarmFollowsWatchdog pins the injection order. The watchdog
// wraps WebSocket first, and the early socket must be created through that
// wrapper.
func TestConnectionPrewarmFollowsWatchdog(t *testing.T) {
	out, _ := Apply(HTML, []byte("<html><head></head><body></body></html>"), Options{MobileUX: true})
	html := string(out)
	wd := strings.Index(html, `id="agy-connection-watchdog"`)
	pw := strings.Index(html, `id="agy-connection-prewarm"`)
	if wd < 0 || pw < 0 {
		t.Fatalf("both scripts must be injected (watchdog at %d, prewarm at %d)", wd, pw)
	}
	if pw < wd {
		t.Error("connection-prewarm must be injected after connection-watchdog")
	}
}

// TestConnectionPrewarmSkippedWhenTransportPatchMissed covers the proxy having
// seen a bundle on which the transport patch did not match.
func TestConnectionPrewarmSkippedWhenTransportPatchMissed(t *testing.T) {
	out, _ := Apply(HTML, []byte("<html><head></head><body></body></html>"), Options{MobileUX: true, WebSocketTransportMissing: true})
	if strings.Contains(string(out), `id="agy-connection-prewarm"`) {
		t.Error("prewarm must not open a socket the bundle will not claim")
	}
}
