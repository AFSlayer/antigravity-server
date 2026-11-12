package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/config"
	"github.com/AFSlayer/antigravity-server/internal/lsproc"
)

const updaterStateFile = "updater-state.json"

var (
	cfgPersistMu     sync.Mutex
	pendingUpdatesMu sync.Mutex
	pendingUpdates   = map[string]string{}
)

func pendingUpdateKey(cfg *config.Config, targetPath string) string {
	dir := ""
	if cfg != nil {
		dir = filepath.Clean(cfg.Dir())
	}
	return dir + "|" + filepath.Clean(targetPath)
}

func getPendingUpdate(cfg *config.Config, targetPath string) string {
	pendingUpdatesMu.Lock()
	defer pendingUpdatesMu.Unlock()
	return pendingUpdates[pendingUpdateKey(cfg, targetPath)]
}

func setPendingUpdate(cfg *config.Config, targetPath, ver string) {
	pendingUpdatesMu.Lock()
	defer pendingUpdatesMu.Unlock()
	key := pendingUpdateKey(cfg, targetPath)
	if ver == "" {
		delete(pendingUpdates, key)
		return
	}
	pendingUpdates[key] = ver
}

type persistedUpdaterState struct {
	IDEVersion     string `json:"ide_version"`
	LanguageServer string `json:"language_server"`
	StagedVersion  string `json:"staged_version"`
	BrokenVersion  string `json:"broken_version"`
}

func readUpdaterState(cfg *config.Config) persistedUpdaterState {
	if cfg == nil {
		return persistedUpdaterState{}
	}
	cfgPersistMu.Lock()
	defer cfgPersistMu.Unlock()
	data, err := os.ReadFile(cfg.Path(updaterStateFile))
	if err != nil {
		return persistedUpdaterState{}
	}
	var st persistedUpdaterState
	if err := json.Unmarshal(data, &st); err != nil {
		return persistedUpdaterState{}
	}
	return st
}

// EffectiveLanguageServer returns cfg's configured language_server path or falls
// back to the path recorded in updater-state.json when running without config.json.
func EffectiveLanguageServer(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	if p := cfg.RuntimeLanguageServer(); p != "" {
		return p
	}
	return readUpdaterState(cfg).LanguageServer
}

// EffectiveIDEVersion resolves the installed or staged IDE version for CLI reporting
// (such as 'agy-server update'), honoring AGY_IDE_VERSION when explicitly set.
func EffectiveIDEVersion(cfg *config.Config, targetPath string) string {
	if cfg != nil && cfg.IDEVersionFromEnv {
		if v := cfg.RuntimeIDEVersion(); v != "" {
			return v
		}
	}
	if targetPath != "" {
		if diskVer := DetectBinaryVersion(targetPath); diskVer != "" {
			return diskVer
		}
	}
	st := readUpdaterState(cfg)
	if st.StagedVersion != "" {
		return st.StagedVersion
	}
	if cfg != nil {
		if v := cfg.RuntimeIDEVersion(); v != "" {
			return v
		}
	}
	return st.IDEVersion
}

// StagedVersion returns the downloaded candidate version awaiting startup verification.
func StagedVersion(cfg *config.Config) string {
	return readUpdaterState(cfg).StagedVersion
}

// HasUnverifiedStagedUpdate reports whether targetPath currently holds a staged
// update that has not yet passed headless startup verification, so existing .bak
// backups of the last known-good binary must be preserved.
func HasUnverifiedStagedUpdate(cfg *config.Config, targetPath string) bool {
	if StagedVersion(cfg) != "" {
		return true
	}
	if targetPath != "" && getPendingUpdate(cfg, targetPath) != "" {
		return true
	}
	return false
}

// LaunchIDEVersion selects the --override_ide_version flag for a headless child
// launch without committing unverified candidate versions to config.json.
func LaunchIDEVersion(cfg *config.Config, detectedBinaryVersion string) string {
	if cfg != nil && cfg.IDEVersionFromEnv {
		if v := cfg.RuntimeIDEVersion(); v != "" {
			return v
		}
	}
	if detectedBinaryVersion != "" {
		return detectedBinaryVersion
	}
	st := readUpdaterState(cfg)
	if st.StagedVersion != "" {
		return st.StagedVersion
	}
	if cfg != nil {
		if v := cfg.RuntimeIDEVersion(); v != "" {
			return v
		}
	}
	return st.IDEVersion
}

// RecordStagedUpdate records a newly downloaded binary version as staged in
// updater-state.json without overwriting the verified ide_version until startup succeeds.
func RecordStagedUpdate(cfg *config.Config, stagedVersion, targetPath string, clearBroken bool) error {
	if cfg == nil {
		return nil
	}
	cfgPersistMu.Lock()
	defer cfgPersistMu.Unlock()

	if cfg.RuntimeLanguageServer() == "" && targetPath != "" {
		cfg.SetRuntimeLanguageServer(targetPath)
	}

	stateRaw := map[string]any{}
	if existingState, err := os.ReadFile(cfg.Path(updaterStateFile)); err == nil && len(existingState) > 0 {
		_ = json.Unmarshal(existingState, &stateRaw)
	}
	if stagedVersion != "" && !cfg.IDEVersionFromEnv {
		stateRaw["staged_version"] = stagedVersion
	}
	if targetPath != "" {
		stateRaw["language_server"] = targetPath
	}
	if clearBroken {
		delete(stateRaw, "broken_version")
	}
	return writeJSONFileAtomic(cfg.Path(updaterStateFile), stateRaw)
}

// ClearStagedVersion discards any unverified staged_version entry after a rollback.
func ClearStagedVersion(cfg *config.Config, targetPath string) {
	if targetPath != "" {
		setPendingUpdate(cfg, targetPath, "")
	}
	if cfg == nil {
		return
	}
	cfgPersistMu.Lock()
	defer cfgPersistMu.Unlock()

	stateRaw := map[string]any{}
	if existingState, err := os.ReadFile(cfg.Path(updaterStateFile)); err == nil && len(existingState) > 0 {
		_ = json.Unmarshal(existingState, &stateRaw)
	}
	if _, ok := stateRaw["staged_version"]; ok {
		delete(stateRaw, "staged_version")
		_ = writeJSONFileAtomic(cfg.Path(updaterStateFile), stateRaw)
	}
}

// ConfirmInstalledVersion commits the running binary's version to runtime state,
// updater-state.json, and existing config.json ONLY after the language server has
// verified healthy startup (or been matched as an already-running instance).
func ConfirmInstalledVersion(cfg *config.Config, binaryPath string) {
	if cfg == nil || binaryPath == "" {
		return
	}
	setPendingUpdate(cfg, binaryPath, "")
	if cfg.IDEVersionFromEnv {
		ClearStagedVersion(cfg, binaryPath)
		return
	}

	st := readUpdaterState(cfg)
	confirmed := DetectBinaryVersion(binaryPath)
	if confirmed == "" {
		confirmed = st.StagedVersion
	}
	ClearStagedVersion(cfg, binaryPath)

	if confirmed == "" {
		if st.IDEVersion != "" && cfg.RuntimeIDEVersion() != st.IDEVersion {
			cfg.SetRuntimeIDEVersion(st.IDEVersion)
		}
		return
	}
	clearBroken := st.BrokenVersion != "" && (st.BrokenVersion == confirmed || isNewerVersion(confirmed, st.BrokenVersion))
	if cfg.RuntimeIDEVersion() != confirmed || st.IDEVersion != confirmed || clearBroken {
		_ = persistUpdaterMetadata(cfg, confirmed, binaryPath)
	}
}

// SyncInstalledVersion is an alias for ConfirmInstalledVersion kept for callers.
func SyncInstalledVersion(cfg *config.Config, binaryPath string) {
	ConfirmInstalledVersion(cfg, binaryPath)
}

func loadUpdaterStateVersion(cfg *config.Config) string {
	return readUpdaterState(cfg).IDEVersion
}

func loadBrokenVersion(cfg *config.Config) string {
	return readUpdaterState(cfg).BrokenVersion
}

// RecordBrokenVersion marks version in updater-state.json so the background
// auto-updater does not repeatedly re-download a release that failed to start
// and triggered a .bak rollback.
func RecordBrokenVersion(cfg *config.Config, version string) {
	if cfg == nil || version == "" {
		return
	}
	cfgPersistMu.Lock()
	defer cfgPersistMu.Unlock()

	stateRaw := map[string]any{}
	if existingState, err := os.ReadFile(cfg.Path(updaterStateFile)); err == nil && len(existingState) > 0 {
		_ = json.Unmarshal(existingState, &stateRaw)
	}
	stateRaw["broken_version"] = version
	_ = writeJSONFileAtomic(cfg.Path(updaterStateFile), stateRaw)
}

func writeJSONFileAtomic(targetPath string, raw map[string]any) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".updater-meta-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, targetPath)
}

// RecordMetadata updates runtime version/path fields on cfg, writes updater-state.json,
// and updates existing config.json without materializing transient CLI/env flags.
func RecordMetadata(cfg *config.Config, ideVersion, targetPath string) error {
	if cfg != nil {
		cfgPersistMu.Lock()
		stateRaw := map[string]any{}
		if existingState, err := os.ReadFile(cfg.Path(updaterStateFile)); err == nil && len(existingState) > 0 {
			_ = json.Unmarshal(existingState, &stateRaw)
		}
		if _, hasBroken := stateRaw["broken_version"]; hasBroken {
			delete(stateRaw, "broken_version")
			_ = writeJSONFileAtomic(cfg.Path(updaterStateFile), stateRaw)
		}
		cfgPersistMu.Unlock()
	}
	return persistUpdaterMetadata(cfg, ideVersion, targetPath)
}

func persistUpdaterMetadata(cfg *config.Config, ideVersion, targetPath string) error {
	if cfg == nil {
		return nil
	}
	cfgPersistMu.Lock()
	defer cfgPersistMu.Unlock()

	if ideVersion != "" && !cfg.IDEVersionFromEnv {
		cfg.SetRuntimeIDEVersion(ideVersion)
	}
	if cfg.RuntimeLanguageServer() == "" && targetPath != "" {
		cfg.SetRuntimeLanguageServer(targetPath)
	}

	// Always record updater-owned metadata in updater-state.json so runs without a
	// user-authored config.json never materialize CLI/env flags into config.json.
	stateRaw := map[string]any{}
	if existingState, err := os.ReadFile(cfg.Path(updaterStateFile)); err == nil && len(existingState) > 0 {
		_ = json.Unmarshal(existingState, &stateRaw)
	}
	if ideVersion != "" && !cfg.IDEVersionFromEnv {
		stateRaw["ide_version"] = ideVersion
		delete(stateRaw, "staged_version")
		if broken, ok := stateRaw["broken_version"].(string); ok && broken != "" {
			if broken == ideVersion || isNewerVersion(ideVersion, broken) {
				delete(stateRaw, "broken_version")
			}
		}
	}
	if targetPath != "" {
		stateRaw["language_server"] = targetPath
	}
	stateErr := writeJSONFileAtomic(cfg.Path(updaterStateFile), stateRaw)

	cfgPath := cfg.Path("config.json")
	existing, err := os.ReadFile(cfgPath)
	if os.IsNotExist(err) {
		return stateErr
	}
	if err != nil {
		return err
	}

	raw := map[string]any{}
	if err := json.Unmarshal(existing, &raw); err != nil {
		return fmt.Errorf("parse %s: %w", cfgPath, err)
	}
	if ideVersion != "" && !cfg.IDEVersionFromEnv {
		raw["ide_version"] = ideVersion
	}
	if existingLS, _ := raw["language_server"].(string); existingLS == "" && targetPath != "" {
		raw["language_server"] = targetPath
	}
	return writeJSONFileAtomic(cfgPath, raw)
}

// CheckUpdateFunc resolves update status given the currently installed IDE version.
type CheckUpdateFunc func(currentVersion string) (*UpdateInfo, error)

// AutoUpdaterOptions configures the auto-updater and daily maintenance background service.
type AutoUpdaterOptions struct {
	CheckInterval     time.Duration // defaults to 24 hours
	IdleRetryInterval time.Duration // defaults to 10 minutes
	MaxIdleRetries    int           // defaults to 6 (max 1 hour deferral)
	InitialDelay      time.Duration // defaults to 5 minutes
	TargetPath        string
	ReloadLS          func() bool
	IsIdle            func() bool
	CheckUpdate       CheckUpdateFunc // defaults to updater.CheckUpdate
}

// StartAutoUpdater runs a background loop in serve mode that checks once a day for official Antigravity updates
// and performs daily language server maintenance restarts when the server is idle.
func StartAutoUpdater(ctx context.Context, cfg *config.Config, reloadLS func() bool, isIdle ...func() bool) {
	go RunAutoUpdater(ctx, cfg, reloadLS, isIdle...)
}

// RunAutoUpdater blocks until ctx ends, checking once a day for official Antigravity updates
// and performing daily language server maintenance restarts when the server is idle.
func RunAutoUpdater(ctx context.Context, cfg *config.Config, reloadLS func() bool, isIdle ...func() bool) {
	var idleFn func() bool
	if len(isIdle) > 0 {
		idleFn = isIdle[0]
	}

	RunAutoUpdaterWithOptions(ctx, cfg, AutoUpdaterOptions{
		CheckInterval:     24 * time.Hour,
		IdleRetryInterval: 10 * time.Minute,
		MaxIdleRetries:    6,
		InitialDelay:      5 * time.Minute,
		ReloadLS:          reloadLS,
		IsIdle:            idleFn,
	})
}

// StartAutoUpdaterWithOptions starts the auto-updater and maintenance loop with custom options.
func StartAutoUpdaterWithOptions(ctx context.Context, cfg *config.Config, opts AutoUpdaterOptions) {
	go RunAutoUpdaterWithOptions(ctx, cfg, opts)
}

// RunAutoUpdaterWithOptions runs the auto-updater and maintenance loop synchronously until ctx ends.
func RunAutoUpdaterWithOptions(ctx context.Context, cfg *config.Config, opts AutoUpdaterOptions) {
	targetPath := opts.TargetPath
	if targetPath == "" {
		targetPath = EffectiveLanguageServer(cfg)
	}
	if targetPath == "" {
		targetPath = lsproc.FindLanguageServer("")
	}
	if targetPath == "" && runtime.GOOS == "linux" {
		targetPath = "/opt/agy-server/language_server"
	}

	checkInterval := opts.CheckInterval
	if checkInterval <= 0 {
		checkInterval = 24 * time.Hour
	}
	retryInterval := opts.IdleRetryInterval
	if retryInterval <= 0 {
		retryInterval = 10 * time.Minute
	}
	maxRetries := opts.MaxIdleRetries
	if maxRetries <= 0 {
		maxRetries = 6
	}

	checkUpdateFn := opts.CheckUpdate
	if checkUpdateFn == nil {
		checkUpdateFn = CheckUpdate
	}

	// Wait initial delay after startup before the first check so initial traffic is undisturbed.
	if opts.InitialDelay > 0 {
		initTimer := time.NewTimer(opts.InitialDelay)
		select {
		case <-ctx.Done():
			initTimer.Stop()
			return
		case <-initTimer.C:
			initTimer.Stop()
		}
	}

	// Initial check: check for updates only; do not restart for maintenance immediately after boot
	checkAndApply(ctx, cfg, targetPath, opts.ReloadLS, opts.IsIdle, retryInterval, maxRetries, checkUpdateFn, false)

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	pendingPoll := retryInterval
	if checkInterval < pendingPoll {
		pendingPoll = checkInterval
	}
	pendingTicker := time.NewTicker(pendingPoll)
	defer pendingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-pendingTicker.C:
			if pendingVer := getPendingUpdate(cfg, targetPath); pendingVer != "" {
				if opts.IsIdle == nil || opts.IsIdle() {
					log.Printf("[auto-updater] server became idle, restarting language_server to apply pending update %s...", pendingVer)
					if err := RecordStagedUpdate(cfg, pendingVer, targetPath, false); err != nil {
						log.Printf("[auto-updater] warning: could not save updater state: %v", err)
					}
					if opts.ReloadLS != nil && opts.ReloadLS() {
						setPendingUpdate(cfg, targetPath, "")
					}
				}
			}
		case <-ticker.C:
			checkAndApply(ctx, cfg, targetPath, opts.ReloadLS, opts.IsIdle, retryInterval, maxRetries, checkUpdateFn, true)
		}
	}
}

func checkAndApply(ctx context.Context, cfg *config.Config, targetPath string, reloadLS func() bool, isIdle func() bool, retryInterval time.Duration, maxRetries int, checkUpdate CheckUpdateFunc, allowMaintenanceRestart bool) bool {
	if checkUpdate == nil {
		checkUpdate = CheckUpdate
	}

	commitPendingReload := func(pendingVer string) func() bool {
		return func() bool {
			if saveErr := RecordStagedUpdate(cfg, pendingVer, targetPath, false); saveErr != nil {
				log.Printf("[auto-updater] warning: could not save updater state: %v", saveErr)
			}
			if reloadLS == nil || !reloadLS() {
				return false
			}
			setPendingUpdate(cfg, targetPath, "")
			return true
		}
	}

	pendingBefore := getPendingUpdate(cfg, targetPath)
	stagedBefore := StagedVersion(cfg)

	currentVersion := cfg.RuntimeIDEVersion()
	if currentVersion == "" && !cfg.IDEVersionFromEnv {
		if saved := loadUpdaterStateVersion(cfg); saved != "" {
			currentVersion = saved
			cfg.SetRuntimeIDEVersion(saved)
		} else if !HasUnverifiedStagedUpdate(cfg, targetPath) {
			if detected := DetectBinaryVersion(targetPath); detected != "" {
				currentVersion = detected
				_ = persistUpdaterMetadata(cfg, detected, targetPath)
			} else {
				currentVersion = "unknown"
			}
		} else {
			currentVersion = "unknown"
		}
	} else if currentVersion == "" {
		currentVersion = "unknown"
	}

	queryVersion := currentVersion
	if queryVersion == "unknown" {
		if pendingBefore != "" {
			queryVersion = pendingBefore
		} else if stagedBefore != "" {
			queryVersion = stagedBefore
		}
	}

	info, err := checkUpdate(queryVersion)
	if err != nil {
		log.Printf("[auto-updater] update check failed: %v", err)
		if pendingVer := getPendingUpdate(cfg, targetPath); pendingVer != "" {
			return waitUntilIdleAndReload(ctx, commitPendingReload(pendingVer), isIdle, retryInterval, maxRetries, "pending update", false)
		}
		if stagedBefore != "" {
			return waitUntilIdleAndReload(ctx, commitPendingReload(stagedBefore), isIdle, retryInterval, maxRetries, "pending update", false)
		}
		return false
	}

	// Self-healing when ide_version was missing and an existing binary is already present on disk.
	if currentVersion == "unknown" && !HasUnverifiedStagedUpdate(cfg, targetPath) && !info.UpdateAvailable && info.LatestVersion != "" && info.LatestVersion != "unknown" {
		if st, statErr := os.Stat(targetPath); statErr == nil && st.Size() > 10*1024*1024 {
			if saveErr := persistUpdaterMetadata(cfg, info.LatestVersion, targetPath); saveErr != nil {
				log.Printf("[auto-updater] warning: could not save config.json: %v", saveErr)
			}
			log.Printf("[auto-updater] recorded missing ide_version as %s from existing binary at %s", info.LatestVersion, targetPath)
			return false
		}
		if info.DownloadURL != "" {
			info.UpdateAvailable = true
		}
	}

	if !info.UpdateAvailable {
		waitVer := getPendingUpdate(cfg, targetPath)
		if waitVer == "" {
			waitVer = StagedVersion(cfg)
		}
		if waitVer != "" {
			return waitUntilIdleAndReload(ctx, commitPendingReload(waitVer), isIdle, retryInterval, maxRetries, "pending update", false)
		}
		log.Printf("[auto-updater] Antigravity is up to date (%s)", currentVersion)
		if !allowMaintenanceRestart {
			return false
		}
		return waitUntilIdleAndReload(ctx, reloadLS, isIdle, retryInterval, maxRetries, "daily memory maintenance", false)
	}

	if broken := loadBrokenVersion(cfg); broken != "" && info.LatestVersion == broken {
		log.Printf("[auto-updater] skipping update %s because it previously failed to start and was rolled back", broken)
		if !allowMaintenanceRestart {
			return false
		}
		return waitUntilIdleAndReload(ctx, reloadLS, isIdle, retryInterval, maxRetries, "daily memory maintenance", false)
	}

	if targetPath == "" {
		if !allowMaintenanceRestart {
			return false
		}
		return waitUntilIdleAndReload(ctx, reloadLS, isIdle, retryInterval, maxRetries, "daily memory maintenance", false)
	}

	pendingVer := getPendingUpdate(cfg, targetPath)
	stagedVer := StagedVersion(cfg)
	alreadyOnDisk := false
	if st, statErr := os.Stat(targetPath); statErr == nil && !st.IsDir() {
		if pendingVer == info.LatestVersion || stagedVer == info.LatestVersion || DetectBinaryVersion(targetPath) == info.LatestVersion {
			alreadyOnDisk = true
		}
	}

	if !alreadyOnDisk {
		// Pre-flight permission check: abort before downloading 170MB if the target directory is not writable
		if err := CheckWritable(targetPath); err != nil {
			log.Printf("[auto-updater] cannot update to %s: %v. Please run 'agy-server update' to update.", info.LatestVersion, err)
			return false
		}

		log.Printf("[auto-updater] new Antigravity %s available (current: %s). Downloading...", info.LatestVersion, currentVersion)

		preserveBak := HasUnverifiedStagedUpdate(cfg, targetPath)
		err = DownloadAndInstallWithBackupPolicy(ctx, info.DownloadURL, targetPath, preserveBak, nil)
		if err != nil {
			log.Printf("[auto-updater] update failed safely (binary unmodified): %v", err)
			return false
		}
		log.Printf("[auto-updater] successfully updated Antigravity to %s at %s", info.LatestVersion, targetPath)
	} else {
		log.Printf("[auto-updater] Antigravity %s is already staged on disk at %s; awaiting idle window to restart...", info.LatestVersion, targetPath)
	}

	if err := RecordStagedUpdate(cfg, info.LatestVersion, targetPath, false); err != nil {
		log.Printf("[auto-updater] warning: could not save staged updater state: %v", err)
	}
	setPendingUpdate(cfg, targetPath, info.LatestVersion)

	return waitUntilIdleAndReload(ctx, commitPendingReload(info.LatestVersion), isIdle, retryInterval, maxRetries, "update", false)
}

func waitUntilIdleAndReload(ctx context.Context, reloadLS func() bool, isIdle func() bool, retryInterval time.Duration, maxRetries int, reason string, forceAfterMaxRetries bool) bool {
	if isIdle == nil || isIdle() {
		log.Printf("[auto-updater] server is idle, restarting language_server for %s...", reason)
		return reloadLS != nil && reloadLS()
	}

	log.Printf("[auto-updater] server is busy with active traffic, deferring %s restart by %v (max %d retries)...", reason, retryInterval, maxRetries)
	retryTicker := time.NewTicker(retryInterval)
	defer retryTicker.Stop()

	retries := 0
	for {
		select {
		case <-ctx.Done():
			return false
		case <-retryTicker.C:
			if isIdle() {
				log.Printf("[auto-updater] server became idle, restarting language_server for %s...", reason)
				return reloadLS != nil && reloadLS()
			}
			retries++
			if retries >= maxRetries {
				if forceAfterMaxRetries {
					log.Printf("[auto-updater] server remained busy after %d deferred attempts; restarting language_server to apply %s...", retries, reason)
					return reloadLS != nil && reloadLS()
				}
				log.Printf("[auto-updater] server remained busy after %d deferred attempts; skipping %s restart for today", retries, reason)
				return false
			}
			log.Printf("[auto-updater] server still busy (attempt %d/%d), deferring %s restart by %v...", retries, maxRetries, reason, retryInterval)
		}
	}
}
