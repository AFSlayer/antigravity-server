package lsproc

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCaptureBrowserShimArgsPreservesComplexOAuthURL(t *testing.T) {
	tmpDir := t.TempDir()
	urlFile := filepath.Join(tmpDir, "dir%USERPROFILE%", "auth-url.txt")
	if err := os.MkdirAll(filepath.Dir(urlFile), 0o700); err != nil {
		t.Fatal(err)
	}

	oauthURL := `https://accounts.google.com/o/oauth2/v2/auth?client_id=123.apps.googleusercontent.com&redirect_uri=http%3A%2F%2F127.0.0.1%3A43210%2Foauth-callback&response_type=code&scope=openid+email&state=abc%25def"quote&access_type=offline`
	if err := CaptureBrowserShimArgs(urlFile, []string{"--new-window", oauthURL + "\r\ninjected"}); err != nil {
		t.Fatalf("CaptureBrowserShimArgs: %v", err)
	}

	data, err := os.ReadFile(urlFile)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 captured line (newline injection stripped), got %d: %q", len(lines), string(data))
	}
	want := oauthURL + "injected"
	if lines[0] != want {
		t.Fatalf("captured URL = %q, want %q", lines[0], want)
	}
}

func TestWriteBrowserShimAndInvocation(t *testing.T) {
	tmpDir := t.TempDir()
	shimDir := filepath.Join(tmpDir, "shim")
	urlFile := filepath.Join(tmpDir, "captured-auth.txt")

	if err := WriteBrowserShim(shimDir, urlFile); err != nil {
		t.Fatalf("WriteBrowserShim: %v", err)
	}

	targetMeta, err := os.ReadFile(filepath.Join(shimDir, browserShimTargetFile))
	if err != nil {
		t.Fatalf("missing %s: %v", browserShimTargetFile, err)
	}
	if strings.TrimSpace(string(targetMeta)) != urlFile {
		t.Fatalf("target file = %q, want %q", strings.TrimSpace(string(targetMeta)), urlFile)
	}

	oauthURL := "https://accounts.google.com/o/oauth2/v2/auth?client_id=abc&redirect_uri=http://127.0.0.1:51234/callback&state=x=y&prompt=consent"

	if runtime.GOOS != "windows" {
		cmd := exec.Command(filepath.Join(shimDir, BrowserShimName), oauthURL)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("unix shim failed: %v (%s)", err, string(out))
		}
		got, err := os.ReadFile(urlFile)
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(got)) != oauthURL {
			t.Fatalf("unix shim captured %q, want %q", strings.TrimSpace(string(got)), oauthURL)
		}
	}

	// Also verify RunBrowserShimIfInvoked directly using os.Args simulation
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{filepath.Join(shimDir, "xdg-open.exe"), oauthURL}
	if !RunBrowserShimIfInvoked() {
		t.Fatal("expected RunBrowserShimIfInvoked() = true when invoked from shim directory as xdg-open.exe")
	}

	// Must NOT trigger when invoked outside a prepared shim directory
	otherDir := t.TempDir()
	os.Args = []string{filepath.Join(otherDir, "open.exe"), oauthURL}
	if RunBrowserShimIfInvoked() {
		t.Fatal("expected RunBrowserShimIfInvoked() = false when .agy-shim-target is absent")
	}
}

func TestFilterMatchesBinaryTokenBoundaries(t *testing.T) {
	f := Filter{BinaryPath: "/opt/agy-server/language_server"}

	cases := []struct {
		cmdline string
		want    bool
	}{
		{"/opt/agy-server/language_server --standalone", true},
		{`"/opt/agy-server/language_server" --standalone`, true},
		{"env FOO=1 /opt/agy-server/language_server", true},
		{"/opt/agy-server/language_server_canary --standalone", false},
		{"/opt/agy-server/language_server.bak --standalone", false},
		{"/tmp/prefix/opt/agy-server/language_server --standalone", false},
	}

	for _, tc := range cases {
		got := f.matches(process{pid: 100, cmdline: tc.cmdline})
		if got != tc.want {
			t.Errorf("Filter.matches(%q) = %v, want %v", tc.cmdline, got, tc.want)
		}
	}
}

func TestWatchPIDAndCanControlPID(t *testing.T) {
	if !CanControlPID(os.Getpid()) {
		t.Fatal("expected CanControlPID(os.Getpid()) == true")
	}
	if CanControlPID(-1) {
		t.Fatal("expected CanControlPID(-1) == false")
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", "ping -n 2 127.0.0.1 >NUL")
	} else {
		cmd = exec.Command("sh", "-c", "sleep 0.1")
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	go func() { _ = cmd.Wait() }()

	done := WatchPID(t.Context(), pid, 15*1e6) // 15ms
	<-done
}
