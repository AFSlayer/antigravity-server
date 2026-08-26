package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/assets"
	"github.com/AFSlayer/antigravity-server/internal/auth"
	"github.com/AFSlayer/antigravity-server/internal/config"
	"github.com/AFSlayer/antigravity-server/internal/lsproc"
	"github.com/AFSlayer/antigravity-server/internal/netinfo"
	"github.com/AFSlayer/antigravity-server/internal/patches"
	"github.com/AFSlayer/antigravity-server/internal/proxy"
	"github.com/AFSlayer/antigravity-server/internal/rules"
	"github.com/AFSlayer/antigravity-server/internal/signin"
	"github.com/AFSlayer/antigravity-server/internal/ui"
	"github.com/AFSlayer/antigravity-server/internal/updater"
	"github.com/AFSlayer/antigravity-server/internal/upload"
)

type runMode int

const (
	modeLocal runMode = iota
	modeServe
)

func (m runMode) String() string {
	if m == modeServe {
		return "Server mode"
	}
	return "Local mode"
}

type runner struct {
	mode runMode
	cfg  *config.Config

	firstRun bool

	// shimURLFile is set only when we started the language server ourselves, which
	// is what lets the sign-in page drive its OAuth flow.
	shimURLFile string

	mu                sync.Mutex
	generatedPassword string
}

func run(mode runMode, cfg *config.Config) error {
	r := &runner{mode: mode, cfg: cfg}
	return r.start()
}

func (r *runner) start() error {
	if err := r.cfg.EnsureDir(); err != nil {
		return errWithHints(
			fmt.Sprintf("Could not create %s: %v", r.cfg.Dir(), err),
			"Check that your home directory is writable, or set AGY_HOME to another location.")
	}

	credentialsPath := r.cfg.Path("credentials.json")
	if _, err := os.Stat(credentialsPath); err != nil {
		r.firstRun = true
	}

	creds, generated, err := auth.LoadOrCreateCredentials(credentialsPath, os.Getenv("AGY_PASSWORD"))
	if err != nil {
		return err
	}
	r.setGeneratedPassword(generated)

	sessions, err := auth.NewSessionStore(r.cfg.Path("sessions.json"), r.cfg.SessionTTL())
	if err != nil {
		return err
	}

	trusted, err := r.cfg.TrustedProxyNets()
	if err != nil {
		return err
	}

	banner(version)

	r.syncRemoteControlSetting()

	instance, err := r.resolveLanguageServer()
	if err != nil {
		return err
	}
	step("Connected to Antigravity language server on port %d", instance.Port)

	r.syncRemoteControlSetting()

	patchOpts := patches.Options{
		MobileUX:      r.cfg.MobileUX,
		WorkspaceRoot: r.cfg.WorkspaceRoot,
		Disabled:      r.cfg.DisabledPatchSet(),
		Debug:         r.cfg.Debug,
	}
	patchOpts.CacheKey = patches.CacheKey(version, patchOpts)

	tracker := patches.NewTracker()

	p, err := proxy.New(proxy.Options{
		TargetPort: instance.Port,
		Patch:      patchOpts,
		OnReport: func(target patches.Target, report patches.Report) {
			tracker.Record(target, report)
			reportPatches(target, report)
		},
	})
	if err != nil {
		return err
	}

	publicListener, err := listen(r.cfg.BindAddr, r.cfg.Port)
	if err != nil {
		return errWithHints(
			fmt.Sprintf("Could not open a listening port: %v", err),
			"Another program may be using the port. Try: agy-remote --port 8888")
	}
	publicPort := publicListener.Addr().(*net.TCPAddr).Port

	localListener, err := listen("127.0.0.1", 0)
	if err != nil {
		return err
	}
	localPort := localListener.Addr().(*net.TCPAddr).Port

	authenticator := auth.New(auth.Options{
		Credentials: creds,
		Sessions:    sessions,
		Trusted:     trusted,
		LoginPage:   ui.LoginPage(),
		IsPublic:    assets.IsPublicPath,
	})

	publicMux := http.NewServeMux()
	for _, path := range assets.Paths() {
		publicMux.Handle(path, assets.Handler())
	}
	uploaderCtx, uploaderCancel := context.WithCancel(context.Background())
	defer uploaderCancel()

	uploader := upload.New(upload.Options{
		WorkspaceRoot: r.cfg.WorkspaceRoot,
		TTL:           upload.DefaultTTL,
	})
	uploader.Register(publicMux)
	uploader.StartCleaner(uploaderCtx, time.Hour)

	rulesMgr := rules.New(rules.Options{
		WorkspaceRoot: r.cfg.WorkspaceRoot,
	})
	rulesMgr.Register(publicMux)

	var (
		shutdownReason string
		shutdownMu     sync.Mutex
	)
	shutdown := make(chan struct{})
	stop := sync.OnceFunc(func() { close(shutdown) })
	stopWithReason := func(reason string) {
		shutdownMu.Lock()
		if shutdownReason == "" {
			shutdownReason = reason
		}
		shutdownMu.Unlock()
		stop()
	}

	if r.mode == modeServe {
		reloadLS := func() {
			if instance != nil && instance.PID > 0 {
				if proc, err := os.FindProcess(instance.PID); err == nil {
					_ = proc.Signal(syscall.SIGTERM)
				}
			}
			stopWithReason("Restarting server to apply Antigravity update…")
		}
		updater.StartAutoUpdater(uploaderCtx, r.cfg, reloadLS)
	}

	ui.NewSignIn(signin.New(instance, r.shimURLFile)).Register(publicMux)
	if r.cfg.Debug {
		if debug, err := ui.NewDebug(r.cfg.Path("mobile-debug.log")); err != nil {
			warn("could not open the mobile debug log: %v", err)
		} else {
			debug.Register(publicMux)
			info("Mobile debug tracing on, writing to %s", dim(debug.Path()))
		}
	}
	publicMux.Handle("/", p.Handler())

	localUI := ui.NewLocal(ui.LocalOptions{
		Version:       version,
		Mode:          r.mode.String(),
		NetworkNote:   r.networkNote(),
		Port:          publicPort,
		Auth:          authenticator,
		Tracker:       tracker,
		Endpoints:     func() []ui.Endpoint { return r.endpoints(publicPort) },
		LoginBaseURL:  func() string { return r.loginBaseURL(publicPort) },
		KnownPassword: r.knownPassword,
		ResetPassword: func() (string, error) { return r.resetPassword(creds) },
		Shutdown:      stop,
	})

	publicServer := &http.Server{Handler: authenticator.Middleware(publicMux)}
	localServer := &http.Server{Handler: localUI.Handler()}

	go func() { _ = publicServer.Serve(publicListener) }()
	go func() { _ = localServer.Serve(localListener) }()

	controlURL := fmt.Sprintf("http://127.0.0.1:%d/", localPort)

	statusCtx, statusCancel := context.WithTimeout(context.Background(), 5*time.Second)
	signedIn := instance.SignedIn(statusCtx)
	statusCancel()

	r.printReady(publicPort, controlURL, generated, signedIn)

	if r.mode == modeLocal {
		go openBrowser(controlURL)
	}

	flusher := time.NewTicker(time.Minute)
	defer flusher.Stop()
	go func() {
		for range flusher.C {
			_ = sessions.Flush()
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-signals:
		fmt.Println()
		info("Received %s, shutting down…", sig)
	case <-shutdown:
		fmt.Println()
		shutdownMu.Lock()
		reason := shutdownReason
		shutdownMu.Unlock()
		if reason != "" {
			info("%s", reason)
		} else {
			info("Shutdown requested from the control panel…")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_ = publicServer.Shutdown(ctx)
	_ = localServer.Shutdown(ctx)
	_ = sessions.Flush()

	step("Stopped")
	return nil
}

func listen(host string, port int) (net.Listener, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err == nil || port == 0 {
		return listener, err
	}
	return net.Listen("tcp", net.JoinHostPort(host, "0"))
}

func (r *runner) syncRemoteControlSetting() {
	changed, err := lsproc.EnableRemoteControl(config.GeminiDir())

	switch {
	case err == nil && changed:
		step("Enabled remote control in Antigravity settings")
	case errors.Is(err, lsproc.ErrConfigMissing):
		return
	case err != nil:
		warn("%v", err)
	}
}

func (r *runner) resolveLanguageServer() (*lsproc.Instance, error) {
	ctx := context.Background()

	if r.mode == modeServe {
		return r.resolveHeadless(ctx)
	}

	if instance, err := lsproc.Find(); err == nil {
		return instance, nil
	}

	info("Antigravity is not running yet, starting the app…")

	if err := lsproc.LaunchDesktopApp(); err != nil {
		return nil, errWithHints(
			"Could not find the Antigravity desktop app.",
			"Install it from https://antigravity.google/download (the desktop app, not the IDE).",
			"If it is already installed, start it manually and run agy-remote again.",
			"On a server with no desktop, use 'agy-remote serve' instead.")
	}

	return r.waitForServer(ctx, 60*time.Second, lsproc.Filter{}, "")
}

func (r *runner) resolveHeadless(ctx context.Context) (*lsproc.Instance, error) {
	binary := lsproc.FindLanguageServer(r.cfg.LanguageServer)
	if binary == "" {
		return nil, errWithHints(
			"Could not find the Antigravity language_server binary.",
			"Run the installer: curl -fsSL https://raw.githubusercontent.com/AFSlayer/antigravity-server/main/scripts/install.sh | bash",
			"Or point at an existing binary: agy-remote serve --language-server /path/to/language_server")
	}

	filter := lsproc.Filter{BinaryPath: binary}

	if instance, err := lsproc.FindMatching(filter); err == nil {
		info("Reusing the language server already running from %s", dim(binary))
		return instance, nil
	}

	info("Starting language server from %s", dim(binary))

	logPath := r.cfg.Path("language-server.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}

	shimDir := r.cfg.Path("browser-shim")
	urlFile := r.cfg.Path("auth-url.txt")
	if err := lsproc.WriteBrowserShim(shimDir, urlFile); err != nil {
		warn("could not install the sign-in helper: %v", err)
		shimDir = ""
	}

	cmd, err := lsproc.LaunchHeadless(lsproc.HeadlessOptions{
		BinaryPath:     binary,
		CSRFToken:      lsproc.NewCSRFToken(),
		IDEVersion:     r.cfg.IDEVersion,
		LogWriter:      logFile,
		BrowserShimDir: shimDir,
	})
	if err != nil {
		return nil, err
	}
	if shimDir != "" {
		r.shimURLFile = urlFile
	}

	return r.waitForServer(ctx, 120*time.Second, lsproc.Filter{PID: cmd.Process.Pid}, logPath)
}

func (r *runner) waitForServer(ctx context.Context, timeout time.Duration, filter lsproc.Filter, logPath string) (*lsproc.Instance, error) {
	fmt.Print("  " + dim("Waiting for the language server"))

	instance, err := lsproc.WaitForMatching(ctx, timeout, filter, func() { fmt.Print(dim(".")) })
	fmt.Println()

	if err != nil {
		hints := []string{
			"Make sure the Antigravity desktop app finished loading, then run agy-remote again.",
		}
		if logPath != "" {
			hints = []string{fmt.Sprintf("Check the language server log: %s", logPath)}
		}
		return nil, errWithHints("The Antigravity language server did not come up in time.", hints...)
	}
	return instance, nil
}

func (r *runner) endpoints(port int) []ui.Endpoint {
	if r.cfg.PublicURL != "" {
		return []ui.Endpoint{{Label: "Public", URL: strings.TrimRight(r.cfg.PublicURL, "/")}}
	}

	local := netinfo.Local()
	var out []ui.Endpoint

	if local.LAN != "" {
		out = append(out, ui.Endpoint{Label: "Same network", URL: fmt.Sprintf("http://%s:%d", local.LAN, port)})
	}
	if local.Tailscale != "" {
		out = append(out, ui.Endpoint{Label: "Tailscale", URL: fmt.Sprintf("http://%s:%d", local.Tailscale, port)})
	}
	if len(out) == 0 {
		out = append(out, ui.Endpoint{Label: "This machine", URL: fmt.Sprintf("http://127.0.0.1:%d", port)})
	}
	return out
}

func (r *runner) networkNote() string {
	if r.cfg.PublicURL != "" {
		return "Reachable from the internet. Keep the password strong."
	}
	return "Only devices on your network can reach this address."
}

func (r *runner) loginBaseURL(port int) string {
	if r.cfg.PublicURL != "" {
		return strings.TrimRight(r.cfg.PublicURL, "/")
	}
	return fmt.Sprintf("http://%s:%d", netinfo.Local().Primary(), port)
}

func (r *runner) setGeneratedPassword(password string) {
	if password == "" {
		return
	}
	r.mu.Lock()
	r.generatedPassword = password
	r.mu.Unlock()
}

func (r *runner) knownPassword() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.generatedPassword
}

func (r *runner) resetPassword(creds *auth.Credentials) (string, error) {
	password := auth.GeneratePassword()
	if err := creds.Set(password); err != nil {
		return "", err
	}
	r.setGeneratedPassword(password)
	return password, nil
}

func (r *runner) printReady(publicPort int, controlURL, generated string, signedIn bool) {
	fmt.Println()
	step("%s ready", bold(r.mode.String()))
	fmt.Println()

	for _, endpoint := range r.endpoints(publicPort) {
		info("%-14s %s", endpoint.Label, cyan(endpoint.URL))
	}

	fmt.Println()
	switch {
	case generated != "":
		info("%-14s %s", "Password", bold(generated))
		info("%-14s %s", "", dim("saved to "+r.cfg.Path("credentials.json")))
	case r.firstRun:
		info("%-14s %s", "Password", dim("set from AGY_PASSWORD"))
	default:
		info("%-14s %s", "Password", dim("unchanged — run 'agy-remote passwd' to set a new one"))
	}

	fmt.Println()
	info("Control panel  %s", cyan(controlURL))
	if r.mode == modeLocal {
		info("%s", dim("Scan the QR code there to sign in on your phone without typing."))
	}

	if !signedIn {
		fmt.Println()
		warn("Antigravity is not signed in to Google yet.")
		info("%s Open %s and follow the three steps.", dim("→"),
			cyan(r.loginBaseURL(publicPort)+ui.SignInPath))
		if r.shimURLFile == "" {
			info("%s Sign-in cannot be driven here, so use the Antigravity desktop app.", dim("→"))
		}
	}

	fmt.Println()
	rule()
	info("%s", dim("Press Ctrl+C to stop."))
	fmt.Println()
}

func reportPatches(target patches.Target, report patches.Report) {
	missing := report.Missing()
	if len(missing) == 0 {
		return
	}

	fmt.Println()
	for _, res := range missing {
		if res.Required {
			fail("patch %s did not apply (%s) — remote access may not work", res.ID, target)
		} else {
			warn("patch %s did not apply (%s) — %s", res.ID, target, res.Desc)
		}
	}
	warn("%s", dim("Antigravity probably changed its bundle. Please report it: https://github.com/AFSlayer/antigravity-server/issues"))
	fmt.Println()
}

func openBrowser(url string) {
	time.Sleep(400 * time.Millisecond)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
