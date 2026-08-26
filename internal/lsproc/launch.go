package lsproc

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	DefaultIDEVersion        = "2.10.0"
	DefaultAPIServerURL      = "https://generativelanguage.googleapis.com"
	DefaultCloudCodeEndpoint = "https://daily-cloudcode-pa.googleapis.com"
)

// HeadlessOptions configures a language server started by us rather than by the
// desktop app.
type HeadlessOptions struct {
	BinaryPath        string
	CSRFToken         string
	IDEVersion        string
	APIServerURL      string
	CloudCodeEndpoint string
	LogWriter         *os.File

	// BrowserShimDir is prepended to the child's PATH. Sign-in needs it: the
	// language server opens the Google consent page with xdg-open, which does not
	// exist on a server, so a shim there captures the URL instead of losing it.
	BrowserShimDir string
}

// BrowserShimName is the command the language server runs to open a URL.
const BrowserShimName = "xdg-open"

// WriteBrowserShim installs a stand-in for xdg-open in dir that appends whatever
// URL it is asked to open to urlFile, one per line.
func WriteBrowserShim(dir, urlFile string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		cmdScript := fmt.Sprintf("@echo off\r\necho %%~1 >> %s\r\n", urlFile)
		_ = os.WriteFile(filepath.Join(dir, "xdg-open.cmd"), []byte(cmdScript), 0o700)
		_ = os.WriteFile(filepath.Join(dir, "open.cmd"), []byte(cmdScript), 0o700)
	}

	script := "#!/bin/sh\nprintf '%s\\n' \"$1\" >> " + shellQuote(urlFile) + "\n"
	return os.WriteFile(filepath.Join(dir, BrowserShimName), []byte(script), 0o700)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// NewCSRFToken returns a UUID to pass as --csrf_token. The desktop app always
// supplies one; without it the served bundle reports an empty token and every
// state-changing request is rejected.
func NewCSRFToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "agy-remote-csrf-token"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// LaunchHeadless starts a standalone language server directly, for servers with
// no desktop environment. The flags mirror what the desktop app passes.
func LaunchHeadless(opts HeadlessOptions) (*exec.Cmd, error) {
	if opts.BinaryPath == "" {
		return nil, fmt.Errorf("no language_server binary path configured")
	}
	if _, err := os.Stat(opts.BinaryPath); err != nil {
		return nil, fmt.Errorf("language_server not found at %s", opts.BinaryPath)
	}

	if opts.IDEVersion == "" {
		opts.IDEVersion = DefaultIDEVersion
	}
	if opts.APIServerURL == "" {
		opts.APIServerURL = DefaultAPIServerURL
	}
	if opts.CloudCodeEndpoint == "" {
		opts.CloudCodeEndpoint = DefaultCloudCodeEndpoint
	}

	args := []string{
		"--standalone",
		"--override_ide_name", "antigravity",
		"--subclient_type", "hub",
		"--override_ide_version", opts.IDEVersion,
		"--override_user_agent_name", "antigravity",
		"--https_server_port", "0",
		"--app_data_dir", "antigravity",
		"--api_server_url", opts.APIServerURL,
		"--cloud_code_endpoint", opts.CloudCodeEndpoint,
		"--enable_sidecars",
		"--disable_telemetry",
	}
	if opts.CSRFToken != "" {
		args = append(args, "--csrf_token", opts.CSRFToken)
	}

	cmd := exec.Command(opts.BinaryPath, args...)
	cmd.Dir = filepath.Dir(opts.BinaryPath)
	if opts.LogWriter != nil {
		cmd.Stdout = opts.LogWriter
		cmd.Stderr = opts.LogWriter
	}
	if opts.BrowserShimDir != "" {
		cmd.Env = append(os.Environ(), "PATH="+opts.BrowserShimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

// LaunchDesktopApp opens the Antigravity desktop app, which starts its own
// language server.
func LaunchDesktopApp() error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-a", "Antigravity").Run()

	case "windows":
		for _, path := range desktopAppCandidates() {
			if _, err := os.Stat(path); err == nil {
				return exec.Command("cmd", "/C", "start", "", path).Start()
			}
		}
		if err := exec.Command("cmd", "/C", "start", "antigravity").Run(); err == nil {
			return nil
		}
		return fmt.Errorf("Antigravity.exe not found")

	default:
		for _, path := range desktopAppCandidates() {
			if _, err := os.Stat(path); err == nil {
				return exec.Command(path).Start()
			}
		}
		if err := exec.Command("antigravity").Start(); err == nil {
			return nil
		}
		return fmt.Errorf("antigravity executable not found")
	}
}

func desktopAppCandidates() []string {
	home, _ := os.UserHomeDir()

	switch runtime.GOOS {
	case "windows":
		return []string{
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Antigravity", "Antigravity.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Antigravity", "Antigravity.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Antigravity", "Antigravity.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Antigravity", "Antigravity.exe"),
		}
	case "darwin":
		return []string{
			"/Applications/Antigravity.app/Contents/MacOS/Antigravity",
			filepath.Join(home, "Applications", "Antigravity.app", "Contents", "MacOS", "Antigravity"),
		}
	default:
		return []string{
			"/opt/Antigravity/antigravity",
			"/usr/share/antigravity/antigravity",
			"/usr/local/bin/antigravity",
			filepath.Join(home, ".local", "share", "Antigravity", "antigravity"),
		}
	}
}

// FindLanguageServer locates a language_server binary, preferring the configured
// path and falling back to known install locations. It returns "" when none is
// found.
func FindLanguageServer(configured string) string {
	if configured != "" {
		if _, err := os.Stat(configured); err == nil {
			return configured
		}
	}

	home, _ := os.UserHomeDir()
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)

	candidates := []string{
		filepath.Join(exeDir, "language_server"),
		filepath.Join(home, ".agy-remote", "language_server"),
		"/opt/agy-server/language_server",
		// Where the installer put it before the rename.
		"/opt/agy-remote/language_server",
	}

	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates,
			"/Applications/Antigravity.app/Contents/Resources/bin/language_server",
			filepath.Join(home, "Applications", "Antigravity.app", "Contents", "Resources", "bin", "language_server"),
		)
	case "linux":
		candidates = append(candidates,
			"/opt/Antigravity/resources/bin/language_server",
			"/usr/share/antigravity/resources/bin/language_server",
			filepath.Join(home, "antigravity", "language_server"),
		)
	}

	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}
