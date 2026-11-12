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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/assets"
	"github.com/AFSlayer/antigravity-server/internal/auth"
	"github.com/AFSlayer/antigravity-server/internal/config"
	"github.com/AFSlayer/antigravity-server/internal/loopback"
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

type swapHTTPServer struct {
	mu       sync.Mutex
	listener net.Listener
	server   *http.Server
	serving  bool
	port     int
	handler  atomic.Pointer[http.Handler]
}

func (s *swapHTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h := s.handler.Load(); h != nil && *h != nil {
		(*h).ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Retry-After", "2")
	http.Error(w, "Server is restarting, please retry momentarily.", http.StatusServiceUnavailable)
}

func (s *swapHTTPServer) listen(host string, port int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.port, nil
	}
	ln, err := listen(host, port)
	if err != nil {
		return 0, err
	}
	s.listener = ln
	s.port = ln.Addr().(*net.TCPAddr).Port
	s.server = &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 15 * time.Second,
	}
	return s.port, nil
}

func (s *swapHTTPServer) setHandler(h http.Handler) {
	s.handler.Store(&h)
	s.mu.Lock()
	if !s.serving && s.server != nil && s.listener != nil {
		s.serving = true
		srv := s.server
		ln := s.listener
		go func() { _ = srv.Serve(ln) }()
	}
	s.mu.Unlock()
}

func (s *swapHTTPServer) park() {
	s.handler.Store(nil)
}

func (s *swapHTTPServer) shutdown(ctx context.Context) {
	s.mu.Lock()
	srv := s.server
	ln := s.listener
	wasServing := s.serving
	s.server = nil
	s.listener = nil
	s.serving = false
	s.port = 0
	s.mu.Unlock()

	s.handler.Store(nil)
	if wasServing && srv != nil {
		_ = srv.Shutdown(ctx)
	}
	if ln != nil {
		_ = ln.Close()
	}
}

type sharedAuthState struct {
	creds             *auth.Credentials
	sessions          *auth.SessionStore
	mu                sync.Mutex
	generatedPassword string
}

func (s *sharedAuthState) setGenerated(pw string) {
	if pw == "" {
		return
	}
	s.mu.Lock()
	s.generatedPassword = pw
	s.mu.Unlock()
}

func (s *sharedAuthState) known() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generatedPassword
}

type runner struct {
	mode      runMode
	cfg       *config.Config
	publicSrv *swapHTTPServer
	localSrv  *swapHTTPServer
	authState *sharedAuthState
	uploader  *upload.Uploader

	firstRun          bool
	announceGenerated string

	// shimURLFile is set only when we started the language server ourselves, which
	// is what lets the sign-in page drive its OAuth flow.
	shimURLFile string

	childCmd        *exec.Cmd
	childDone       chan struct{}
	reusedPID       int
	shuttingDown    atomic.Bool
	reloadRequested atomic.Bool
}

func run(mode runMode, cfg *config.Config) error {
	if err := cfg.EnsureDir(); err != nil {
		return errWithHints(
			fmt.Sprintf("Could not create %s: %v", cfg.Dir(), err),
			"Check that your home directory is writable, or set AGY_HOME to another location.")
	}

	credentialsPath := cfg.Path("credentials.json")
	firstRun := false
	if _, err := os.Stat(credentialsPath); err != nil {
		firstRun = true
	}

	creds, initialGenerated, err := auth.LoadOrCreateCredentials(credentialsPath, os.Getenv("AGY_PASSWORD"))
	if err != nil {
		return err
	}

	sessions, err := auth.NewSessionStore(cfg.Path("sessions.json"), cfg.SessionTTL())
	if err != nil {
		return err
	}
	defer func() { _ = sessions.Flush() }()

	authState := &sharedAuthState{
		creds:    creds,
		sessions: sessions,
	}
	authState.setGenerated(initialGenerated)

	flusher := time.NewTicker(time.Minute)
	flusherStop := make(chan struct{})
	defer func() {
		flusher.Stop()
		close(flusherStop)
	}()
	go func() {
		for {
			select {
			case <-flusherStop:
				return
			case <-flusher.C:
				_ = sessions.Flush()
			}
		}
	}()

	runCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	publicSrv := &swapHTTPServer{}
	localSrv := &swapHTTPServer{}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		publicSrv.shutdown(ctx)
		localSrv.shutdown(ctx)
	}()

	sharedUploader := upload.New(upload.Options{
		WorkspaceRoot: cfg.EffectiveWorkspaceRoot(),
		TTL:           upload.DefaultTTL,
	})

	cycle := 0
	for {
		r := &runner{
			mode:      mode,
			cfg:       cfg,
			publicSrv: publicSrv,
			localSrv:  localSrv,
			authState: authState,
			uploader:  sharedUploader,
		}
		if cycle == 0 {
			r.firstRun = firstRun
			r.announceGenerated = initialGenerated
		}
		cycle++

		err := r.start(runCtx)
		if r.reloadRequested.Load() && err == nil && runCtx.Err() == nil {
			continue
		}
		return err
	}
}

func (r *runner) start(runCtx context.Context) error {
	trusted, err := r.cfg.TrustedProxyNets()
	if err != nil {
		return err
	}

	banner(version)

	publicPort, err := r.publicSrv.listen(r.cfg.BindAddr, r.cfg.Port)
	if err != nil {
		return errWithHints(
			fmt.Sprintf("Could not open a listening port: %v", err),
			"Another program may be using the port. Try: agy-server --port 8888")
	}

	var localPort int
	if r.mode == modeLocal {
		localPort, err = r.localSrv.listen("127.0.0.1", 0)
		if err != nil {
			return err
		}
	}

	r.syncRemoteControlSetting()

	instance, err := r.resolveLanguageServer(runCtx)
	if err != nil {
		if runCtx.Err() != nil {
			fmt.Println()
			info("Shutdown signal received during startup, stopping…")
			step("Stopped")
			return nil
		}
		return err
	}
	earlyExit := true
	defer func() {
		if earlyExit && r.mode == modeServe && r.childCmd != nil {
			r.shuttingDown.Store(true)
			r.terminateManagedLanguageServer(instance)
		}
	}()
	step("Connected to Antigravity language server on port %d", instance.Port)

	lifecycleCtx, lifecycleCancel := context.WithCancel(runCtx)
	var bgWG sync.WaitGroup
	stopBackground := sync.OnceFunc(func() {
		lifecycleCancel()
		bgWG.Wait()
	})
	startBackground := func(fn func()) {
		bgWG.Add(1)
		go func() {
			defer bgWG.Done()
			fn()
		}()
	}

	effectiveWS := r.cfg.EffectiveWorkspaceRoot()
	patchWorkspaceRoot := r.cfg.PatchWorkspaceRoot()

	patchOpts := patches.Options{
		MobileUX:      r.cfg.MobileUX,
		WorkspaceRoot: patchWorkspaceRoot,
		Disabled:      r.cfg.DisabledPatchSet(),
		Debug:         r.cfg.Debug,
	}
	patchOpts.CacheKey = patches.CacheKey(version, patchOpts)

	tracker := patches.NewTracker()

	forbiddenPorts := []int{publicPort}
	if localPort > 0 {
		forbiddenPorts = append(forbiddenPorts, localPort)
	}
	// Sidecar and OAuth requests ask which loopback family a port in the
	// language server's process tree listens on. A walk is slow and on macOS
	// and Windows starts subprocesses, so one result serves a few seconds.
	treeEndpointsTTL := 2 * time.Second
	if runtime.GOOS != "linux" {
		treeEndpointsTTL = 5 * time.Second
	}
	treeEndpoints := lsproc.NewTreeEndpointCache(instance.PID, treeEndpointsTTL, time.Second)

	initialLSPorts := make(map[int]struct{})
	if instance.PID > 0 {
		if pts, lerr := lsproc.ListeningPorts(instance.PID); lerr == nil {
			for _, pt := range pts {
				if pt > 0 && pt <= 65535 {
					initialLSPorts[pt] = struct{}{}
					forbiddenPorts = append(forbiddenPorts, pt)
				}
			}
		}
	}

	p, err := proxy.New(proxy.Options{
		TargetPort:      instance.Port,
		TargetCSRFToken: instance.CSRFToken,
		ForbiddenPorts:  forbiddenPorts,
		PublicURL:       r.cfg.PublicURL,
		TrustedProxies:  trusted,
		DisableWSRelay:  r.cfg.DisableWSRelay,
		ValidateSession: r.authState.sessions.Validate,
		ResolveSidecarHost: func(ctx context.Context, port int, urlHost string) (string, bool) {
			mask, ok := treeEndpoints.Lookup(ctx, port)
			if !ok {
				return "", false
			}
			return mask.ResolveDialHost(urlHost)
		},
		Patch: patchOpts,
		OnReport: func(target patches.Target, report patches.Report) {
			tracker.Record(target, report)
			reportPatches(target, report)
		},
	})
	if err != nil {
		stopBackground()
		return err
	}
	defer p.Close()
	defer stopBackground()

	startBackground(func() {
		lsproc.KeepLogRotated(lifecycleCtx, r.cfg.Path("language-server.log"))
	})

	startBackground(func() {
		if err := p.PrewarmContext(lifecycleCtx); err != nil && lifecycleCtx.Err() == nil {
			warn("could not pre-patch the web bundle, the first page load will be slower: %v", err)
		}
	})

	authenticator := auth.New(auth.Options{
		Credentials:  r.authState.creds,
		Sessions:     r.authState.sessions,
		Trusted:      trusted,
		PublicURL:    r.cfg.PublicURL,
		LoginPage:    ui.LoginPage(),
		IsPublic:     assets.IsPublicPath,
		AllowSidecar: p.ValidateSidecarCapability,
	})

	publicMux := http.NewServeMux()
	for _, path := range assets.Paths() {
		publicMux.Handle(path, assets.Handler())
	}

	uploader := r.uploader
	if uploader == nil {
		uploader = upload.New(upload.Options{
			WorkspaceRoot: effectiveWS,
			TTL:           upload.DefaultTTL,
		})
	}
	uploader.SetWorkspaceRoot(effectiveWS).WithSameOrigin(trusted, r.cfg.PublicURL)
	uploader.Register(publicMux)
	startBackground(func() {
		uploader.RunCleaner(lifecycleCtx, time.Hour)
	})

	rulesMgr := rules.New(rules.Options{
		WorkspaceRoot: r.cfg.WorkspaceRoot,
	}).WithSameOrigin(trusted, r.cfg.PublicURL)
	rulesMgr.Register(publicMux)

	var (
		lsPortsMu       sync.Mutex
		lsDirectPorts   map[int]struct{}
		lsPortsCachedAt time.Time
		lsPortsLastErr  error
		lsPortsInflight chan struct{}
	)
	lsPortsTTL := 2 * time.Second
	if runtime.GOOS != "linux" {
		lsPortsTTL = 5 * time.Second
	}
	hasLanguageServerOwnPort := func(ctx context.Context, port int) bool {
		if _, ok := initialLSPorts[port]; ok {
			return true
		}
		if instance.PID <= 0 {
			return false
		}
		lsPortsMu.Lock()
		if !lsPortsCachedAt.IsZero() && time.Since(lsPortsCachedAt) < lsPortsTTL && lsPortsLastErr == nil {
			_, ok := lsDirectPorts[port]
			lsPortsMu.Unlock()
			return ok
		}
		if lsPortsInflight != nil {
			waitCh := lsPortsInflight
			lsPortsMu.Unlock()
			select {
			case <-waitCh:
			case <-ctx.Done():
				return true
			}
			lsPortsMu.Lock()
			portsMap, lastErr := lsDirectPorts, lsPortsLastErr
			lsPortsMu.Unlock()
			if lastErr != nil {
				return true
			}
			_, ok := portsMap[port]
			return ok
		}
		ch := make(chan struct{})
		lsPortsInflight = ch
		lsPortsMu.Unlock()

		ports, perr := lsproc.ListeningPortsContext(context.Background(), instance.PID)
		var next map[int]struct{}
		if perr == nil {
			next = make(map[int]struct{}, len(ports))
			for _, pt := range ports {
				next[pt] = struct{}{}
			}
		}

		lsPortsMu.Lock()
		if perr == nil {
			lsDirectPorts = next
			lsPortsCachedAt = time.Now()
			lsPortsLastErr = nil
		} else {
			lsPortsLastErr = perr
		}
		lsPortsInflight = nil
		close(ch)
		lsPortsMu.Unlock()

		if perr != nil || ctx.Err() != nil {
			return true
		}
		_, ok := next[port]
		return ok
	}
	isCoreReservedPort := func(ctx context.Context, port int) bool {
		if port == publicPort || (localPort > 0 && port == localPort) || port == instance.Port {
			return true
		}
		if _, ok := initialLSPorts[port]; ok {
			return true
		}
		return p.IsSidecarReservedPort(ctx, port)
	}
	loopbackHandler := loopback.New(loopback.Options{
		Trusted:   trusted,
		PublicURL: r.cfg.PublicURL,
		IsReservedPort: func(ctx context.Context, port int) bool {
			return isCoreReservedPort(ctx, port) || hasLanguageServerOwnPort(ctx, port)
		},
		ResolveOAuthTarget: func(ctx context.Context, port int, urlHost string) (string, bool) {
			if isCoreReservedPort(ctx, port) {
				return "", false
			}
			mask, ok := treeEndpoints.Lookup(ctx, port)
			if !ok {
				return "", false
			}
			return mask.ResolveDialHost(urlHost)
		},
	})
	defer loopbackHandler.Close()
	loopbackHandler.Register(publicMux)

	var (
		shutdownReason string
		shutdownMu     sync.Mutex
		childCrashed   atomic.Bool
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

	canReloadLS := r.childCmd != nil || (r.reusedPID > 0 && lsproc.CanControlPID(r.reusedPID))
	if r.mode == modeServe {
		watchDone := (<-chan struct{})(r.childDone)
		if watchDone == nil && r.reusedPID > 0 {
			watchDone = lsproc.WatchPID(lifecycleCtx, r.reusedPID, 2*time.Second)
		}
		if watchDone != nil {
			go func(done <-chan struct{}) {
				<-done
				if !r.shuttingDown.Load() {
					childCrashed.Store(true)
					stopWithReason("Language server exited unexpectedly; stopping so service manager can restart…")
				}
			}(watchDone)
		}
		if runtime.GOOS == "linux" && canReloadLS {
			reloadLS := func() bool {
				if !canReloadLS {
					warn("skipping automatic restart because language_server cannot be signalled by the current user")
					return false
				}
				r.reloadRequested.Store(true)
				r.shuttingDown.Store(true)
				stopWithReason("Restarting server for update or daily maintenance…")
				return true
			}
			isIdle := func() bool {
				return p.IsIdle(15*time.Minute) && uploader.IsIdle(15*time.Minute) && loopbackHandler.IsIdle(15*time.Minute)
			}
			startBackground(func() {
				updater.RunAutoUpdater(lifecycleCtx, r.cfg, reloadLS, isIdle)
			})
		}
	}

	coordinator := signin.New(instance, r.shimURLFile)
	ui.NewSignIn(coordinator).WithSameOrigin(trusted, r.cfg.PublicURL).Register(publicMux)
	if r.cfg.Debug {
		if debug, err := ui.NewDebug(r.cfg.Path("mobile-debug.log")); err != nil {
			warn("could not open the mobile debug log: %v", err)
		} else {
			defer debug.Close()
			debug.WithSameOrigin(trusted, r.cfg.PublicURL).Register(publicMux)
			info("Mobile debug tracing on, writing to %s", dim(debug.Path()))
		}
	}

	var rootHandler http.Handler = wrapRootWithSignInRedirect(p.Handler(), coordinator)
	publicMux.Handle("/", rootHandler)

	r.publicSrv.setHandler(authenticator.Middleware(publicMux))

	var controlURL string
	if r.mode == modeLocal && localPort > 0 {
		localUI := ui.NewLocal(ui.LocalOptions{
			Version:       version,
			Mode:          r.mode.String(),
			NetworkNote:   r.networkNote(),
			Port:          publicPort,
			LocalPort:     localPort,
			Auth:          authenticator,
			Tracker:       tracker,
			Endpoints:     func() []ui.Endpoint { return r.endpoints(publicPort) },
			LoginBaseURL:  func() string { return r.loginBaseURL(publicPort) },
			KnownPassword: r.knownPassword,
			ResetPassword: func() (string, error) { return r.resetPassword(r.authState.creds) },
			Shutdown:      stop,
		})
		r.localSrv.setHandler(localUI.Handler())
		controlURL = fmt.Sprintf("http://127.0.0.1:%d/", localPort)
	}

	statusCtx, statusCancel := context.WithTimeout(runCtx, 5*time.Second)
	signedIn := coordinator.SignedIn(statusCtx)
	statusCancel()

	r.printReady(publicPort, controlURL, r.announceGenerated, signedIn)

	if r.mode == modeLocal && controlURL != "" {
		go openBrowser(controlURL)
	}

	earlyExit = false

	select {
	case <-runCtx.Done():
		r.shuttingDown.Store(true)
		fmt.Println()
		info("Shutdown signal received, stopping…")
	case <-shutdown:
		r.shuttingDown.Store(true)
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

	if r.reloadRequested.Load() && runCtx.Err() == nil {
		r.publicSrv.park()
		r.localSrv.park()
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		r.publicSrv.shutdown(ctx)
		r.localSrv.shutdown(ctx)
	}
	if r.mode == modeServe && (r.childCmd != nil || (r.reloadRequested.Load() && canReloadLS)) {
		r.terminateManagedLanguageServer(instance)
	}
	stopBackground()
	_ = r.authState.sessions.Flush()

	if childCrashed.Load() {
		return errors.New("language server exited unexpectedly")
	}
	if r.reloadRequested.Load() && runCtx.Err() == nil {
		step("Reloading language server")
		return nil
	}

	step("Stopped")
	return nil
}

func listen(host string, port int) (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
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

func (r *runner) resolveLanguageServer(ctx context.Context) (*lsproc.Instance, error) {
	if ctx == nil {
		ctx = context.Background()
	}

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
			"If it is already installed, start it manually and run agy-server again.",
			"On a server with no desktop, use 'agy-server serve' instead.")
	}

	return r.waitForServer(ctx, 60*time.Second, lsproc.Filter{}, "")
}

func (r *runner) resolveHeadless(ctx context.Context) (*lsproc.Instance, error) {
	binary := lsproc.FindLanguageServer(updater.EffectiveLanguageServer(r.cfg))
	if binary == "" {
		return nil, errWithHints(
			"Could not find the Antigravity language_server binary.",
			"Run the installer: curl -fsSL https://raw.githubusercontent.com/AFSlayer/antigravity-server/main/scripts/install.sh | bash",
			"Or point at an existing binary: agy-server serve --language-server /path/to/language_server")
	}
	stagedVer := updater.StagedVersion(r.cfg)
	detectedBefore := updater.DetectBinaryVersion(binary)

	shimDir := r.cfg.Path("browser-shim")
	urlFile := r.cfg.Path("auth-url.txt")
	if err := lsproc.WriteBrowserShim(shimDir, urlFile); err != nil {
		warn("could not install the sign-in helper: %v", err)
		shimDir = ""
	} else {
		r.shimURLFile = urlFile
	}

	filter := lsproc.Filter{BinaryPath: binary}

	if instance, err := lsproc.FindMatching(filter); err == nil {
		info("Reusing the language server already running from %s", dim(binary))
		r.reusedPID = instance.PID
		r.syncInstanceCSRFToken(instance)
		if shouldConfirmReusedInstance(r.cfg, binary, instance.PID) {
			updater.ConfirmInstalledVersion(r.cfg, binary)
		}
		return instance, nil
	}

	launchManaged := func(ideVer string) (*lsproc.Instance, error) {
		info("Starting language server from %s", dim(binary))

		logPath := r.cfg.Path("language-server.log")
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return nil, err
		}

		csrfToken := r.loadOrCreateCSRFToken()

		cmd, err := lsproc.LaunchHeadless(lsproc.HeadlessOptions{
			BinaryPath:     binary,
			CSRFToken:      csrfToken,
			IDEVersion:     ideVer,
			LogWriter:      logFile,
			BrowserShimDir: shimDir,
		})
		_ = logFile.Close()
		if err != nil {
			return nil, err
		}
		done := make(chan struct{})
		r.childCmd = cmd
		r.childDone = done
		go func() {
			_ = cmd.Wait()
			close(done)
		}()

		waitCtx, waitCancel := context.WithCancel(ctx)
		var exitedEarly atomic.Bool
		go func() {
			select {
			case <-done:
				exitedEarly.Store(true)
				waitCancel()
			case <-waitCtx.Done():
			}
		}()

		instance, err := r.waitForServer(waitCtx, 120*time.Second, lsproc.Filter{PID: cmd.Process.Pid}, logPath)
		waitCancel()
		if err != nil {
			lsproc.TerminateProcess(cmd.Process, cmd.Process.Pid, done, 2*time.Second)
			r.childCmd = nil
			r.childDone = nil
			if exitedEarly.Load() && ctx.Err() == nil {
				return nil, errWithHints(
					"The Antigravity language server exited immediately during startup.",
					fmt.Sprintf("Check the language server log: %s", logPath),
				)
			}
			return nil, err
		}
		if instance.CSRFToken == "" {
			instance.CSRFToken = csrfToken
		}
		return instance, nil
	}

	instance, err := launchManaged(updater.LaunchIDEVersion(r.cfg, detectedBefore))
	if err == nil {
		updater.ConfirmInstalledVersion(r.cfg, binary)
		return instance, nil
	}
	if ctx.Err() == nil && updater.HasUnverifiedStagedUpdate(r.cfg, binary) {
		if info, statErr := os.Stat(binary + ".bak"); statErr == nil && !info.IsDir() {
			warn("language server failed to start (%v); rolling back to %s.bak", err, binary)
			brokenCandidate := detectedBefore
			if brokenCandidate == "" {
				brokenCandidate = stagedVer
			}
			if rerr := updater.RestoreBackup(binary); rerr != nil {
				warn("could not restore backup binary: %v", rerr)
				r.shuttingDown.Store(true)
				return nil, err
			}
			updater.ClearStagedVersion(r.cfg, binary)
			updater.RecordBrokenVersion(r.cfg, brokenCandidate)
			restoredVer := updater.LaunchIDEVersion(r.cfg, updater.DetectBinaryVersion(binary))
			instance, err = launchManaged(restoredVer)
			if err == nil {
				updater.ConfirmInstalledVersion(r.cfg, binary)
				return instance, nil
			}
		}
	}
	r.shuttingDown.Store(true)
	return nil, err
}

func shouldConfirmReusedInstance(cfg *config.Config, binary string, pid int) bool {
	if !updater.HasUnverifiedStagedUpdate(cfg, binary) {
		return true
	}
	return procExeMatchesDisk(pid, binary)
}

func procExeMatchesDisk(pid int, diskPath string) bool {
	if runtime.GOOS != "linux" || pid <= 0 || diskPath == "" {
		return false
	}
	diskInfo, err := os.Stat(diskPath)
	if err != nil {
		return false
	}
	procInfo, err := os.Stat(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return false
	}
	return os.SameFile(diskInfo, procInfo)
}

func (r *runner) terminateManagedLanguageServer(instance *lsproc.Instance) {
	var proc *os.Process
	pid := 0
	if r.childCmd != nil && r.childCmd.Process != nil {
		proc = r.childCmd.Process
		pid = proc.Pid
	} else if instance != nil && instance.PID > 0 {
		pid = instance.PID
	}
	lsproc.TerminateProcess(proc, pid, r.childDone, 5*time.Second)
}

func (r *runner) loadOrCreateCSRFToken() string {
	tokenPath := r.cfg.Path("csrf-token.txt")
	if data, err := os.ReadFile(tokenPath); err == nil {
		token := strings.TrimSpace(string(data))
		if token != "" {
			return token
		}
	}
	token := lsproc.NewCSRFToken()
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0o600); err != nil {
		warn("could not persist csrf token: %v", err)
	}
	return token
}

func (r *runner) syncInstanceCSRFToken(instance *lsproc.Instance) {
	if instance == nil {
		return
	}
	tokenPath := r.cfg.Path("csrf-token.txt")
	if instance.CSRFToken != "" {
		if data, err := os.ReadFile(tokenPath); err != nil || strings.TrimSpace(string(data)) != instance.CSRFToken {
			if err := os.WriteFile(tokenPath, []byte(instance.CSRFToken+"\n"), 0o600); err != nil {
				warn("could not update csrf token from running instance: %v", err)
			}
		}
	} else {
		if data, err := os.ReadFile(tokenPath); err == nil {
			instance.CSRFToken = strings.TrimSpace(string(data))
		}
	}
}

func (r *runner) waitForServer(ctx context.Context, timeout time.Duration, filter lsproc.Filter, logPath string) (*lsproc.Instance, error) {
	fmt.Print("  " + dim("Waiting for the language server"))

	instance, err := lsproc.WaitForMatching(ctx, timeout, filter, func() { fmt.Print(dim(".")) })
	fmt.Println()

	if err != nil {
		hints := []string{
			"Make sure the Antigravity desktop app finished loading, then run agy-server again.",
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
	if r.authState != nil {
		r.authState.setGenerated(password)
	}
}

func (r *runner) knownPassword() string {
	if r.authState == nil {
		return ""
	}
	return r.authState.known()
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
		info("%-14s %s", "Password", dim("unchanged — run 'agy-server passwd' to set a new one"))
	}

	if controlURL != "" {
		fmt.Println()
		info("Control panel  %s", cyan(controlURL))
		if r.mode == modeLocal {
			info("%s", dim("Scan the QR code there to sign in on your phone without typing."))
		}
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

func wrapRootWithSignInRedirect(fallback http.Handler, coordinator *signin.Coordinator) http.Handler {
	if coordinator == nil || !coordinator.Available() {
		return fallback
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isOnboarding := r.URL.Path == "/onboarding" || r.URL.Path == "/onboarding/"
		if r.Method == http.MethodGet && (r.URL.Path == "/" || isOnboarding) {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			// Only an explicit "not signed in" redirects. The language server
			// can take longer than this timeout to answer when its own caches
			// have expired, and that must not look like a lost login. On
			// /onboarding routes (reached right after signing out), bypass the
			// 30s positive cache so refreshing redirects immediately.
			needsSignIn := false
			if isOnboarding {
				needsSignIn = coordinator.NeedsSignInFresh(ctx)
			} else {
				needsSignIn = coordinator.NeedsSignIn(ctx)
			}
			if needsSignIn {
				http.Redirect(w, r, ui.SignInPath, http.StatusFound)
				return
			}
		}
		fallback.ServeHTTP(w, r)
	})
}
