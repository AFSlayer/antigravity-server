package lsproc

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	DefaultIDEVersion        = "2.24.0"
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
const (
	BrowserShimName       = "xdg-open"
	browserShimTargetFile = ".agy-shim-target"
)

// WriteBrowserShim installs a stand-in for xdg-open (and open.exe on Windows) in
// dir that appends whatever URL it is asked to open to urlFile, one per line.
// On Windows it installs native executable shims rather than .cmd batch files so
// unquoted '&' and '=' query parameters in OAuth URLs are never truncated or
// interpreted by cmd.exe.
func WriteBrowserShim(dir, urlFile string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, browserShimTargetFile), []byte(urlFile+"\n"), 0o600); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		_ = os.Remove(filepath.Join(dir, "xdg-open.cmd"))
		_ = os.Remove(filepath.Join(dir, "open.cmd"))
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("locate executable for browser shim: %w", err)
		}
		for _, name := range []string{"xdg-open.exe", "open.exe"} {
			if err := installBinaryShim(exePath, filepath.Join(dir, name)); err != nil {
				return err
			}
		}
		return nil
	}

	quotedTarget := shellQuote(urlFile)
	script := "#!/bin/sh\n" +
		"for arg in \"$@\"; do\n" +
		"  case \"$arg\" in\n" +
		"    http://*|https://*)\n" +
		"      printf '%s\\n' \"$arg\" >> " + quotedTarget + "\n" +
		"      exit 0\n" +
		"      ;;\n" +
		"  esac\n" +
		"done\n" +
		"[ \"$#\" -gt 0 ] && printf '%s\\n' \"$1\" >> " + quotedTarget + "\n"
	return os.WriteFile(filepath.Join(dir, BrowserShimName), []byte(script), 0o700)
}

func installBinaryShim(srcPath, dstPath string) error {
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return err
	}
	if dstInfo, statErr := os.Stat(dstPath); statErr == nil && !dstInfo.IsDir() {
		if os.SameFile(srcInfo, dstInfo) || (dstInfo.Size() == srcInfo.Size() && dstInfo.ModTime().Equal(srcInfo.ModTime())) {
			return nil
		}
	}

	dir := filepath.Dir(dstPath)
	if linkProbe, probeErr := os.CreateTemp(dir, ".shim-link-*.tmp"); probeErr == nil {
		tmpLink := linkProbe.Name()
		_ = linkProbe.Close()
		_ = os.Remove(tmpLink)
		if err := os.Link(srcPath, tmpLink); err == nil {
			_ = os.Remove(dstPath)
			if renErr := os.Rename(tmpLink, dstPath); renErr == nil {
				return nil
			}
			_ = os.Remove(tmpLink)
		}
	}

	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()

	tmpFile, err := os.CreateTemp(dir, ".shim-copy-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := io.Copy(tmpFile, in); err != nil {
		_ = tmpFile.Close()
		return err
	}
	_ = tmpFile.Chmod(0o700)
	if err := tmpFile.Close(); err != nil {
		return err
	}
	_ = os.Remove(dstPath)
	if err := os.Rename(tmpName, dstPath); err != nil {
		if existing, statErr := os.Stat(dstPath); statErr == nil && !existing.IsDir() && existing.Size() > 0 {
			return nil
		}
		return err
	}
	_ = os.Chtimes(dstPath, srcInfo.ModTime(), srcInfo.ModTime())
	return nil
}

// CaptureBrowserShimArgs extracts the target URL from argv and appends it as a
// single sanitized line to urlFile.
func CaptureBrowserShimArgs(urlFile string, args []string) error {
	urlFile = strings.TrimSpace(urlFile)
	if urlFile == "" {
		return fmt.Errorf("empty browser shim target file")
	}

	chosen := ""
	fallback := ""
	for _, raw := range args {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
			chosen = trimmed
			break
		}
		if fallback == "" && !strings.HasPrefix(trimmed, "-") {
			fallback = trimmed
		}
	}
	if chosen == "" {
		chosen = fallback
	}
	if chosen == "" && len(args) > 0 {
		chosen = strings.TrimSpace(args[0])
	}
	chosen = strings.NewReplacer("\r", "", "\n", "").Replace(chosen)
	if chosen == "" {
		return nil
	}

	f, err := os.OpenFile(urlFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.WriteString(chosen + "\n")
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

// RunBrowserShimIfInvoked checks whether the current process was launched as an
// xdg-open / open shim inside a directory prepared by WriteBrowserShim, records
// the OAuth URL argument directly from os.Args without any shell parsing, and
// reports true when handled so main() can exit immediately.
func RunBrowserShimIfInvoked() bool {
	if len(os.Args) == 0 {
		return false
	}
	base := strings.ToLower(filepath.Base(os.Args[0]))
	base = strings.TrimSuffix(base, ".exe")
	if base != "xdg-open" && base != "open" {
		return false
	}

	var candidateDirs []string
	if exePath, err := os.Executable(); err == nil && exePath != "" {
		candidateDirs = append(candidateDirs, filepath.Dir(exePath))
	}
	if argDir := filepath.Dir(os.Args[0]); argDir != "" && argDir != "." {
		candidateDirs = append(candidateDirs, argDir)
	}

	for _, dir := range candidateDirs {
		data, err := os.ReadFile(filepath.Join(dir, browserShimTargetFile))
		if err != nil {
			continue
		}
		target := strings.TrimSpace(string(data))
		if target == "" {
			continue
		}
		_ = CaptureBrowserShimArgs(target, os.Args[1:])
		return true
	}
	return false
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
	case "windows":
		candidates = append(candidates,
			filepath.Join(exeDir, "language_server.exe"),
			filepath.Join(home, ".agy-remote", "language_server.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Antigravity", "resources", "bin", "language_server.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Antigravity", "resources", "bin", "language_server.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Antigravity", "resources", "bin", "language_server.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Antigravity", "resources", "bin", "language_server.exe"),
		)
	}

	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}
