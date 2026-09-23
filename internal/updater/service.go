package updater

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/config"
	"github.com/AFSlayer/antigravity-server/internal/lsproc"
)

// CheckUpdateFunc resolves update status given the currently installed IDE version.
type CheckUpdateFunc func(currentVersion string) (*UpdateInfo, error)

// AutoUpdaterOptions configures the auto-updater and daily maintenance background service.
type AutoUpdaterOptions struct {
	CheckInterval     time.Duration // defaults to 24 hours
	IdleRetryInterval time.Duration // defaults to 10 minutes
	MaxIdleRetries    int           // defaults to 6 (max 1 hour deferral)
	InitialDelay      time.Duration // defaults to 5 minutes
	TargetPath        string
	ReloadLS          func()
	IsIdle            func() bool
	CheckUpdate       CheckUpdateFunc // defaults to updater.CheckUpdate
}

// StartAutoUpdater runs a background loop in serve mode that checks once a day for official Antigravity updates
// and performs daily language server maintenance restarts when the server is idle.
func StartAutoUpdater(ctx context.Context, cfg *config.Config, reloadLS func(), isIdle ...func() bool) {
	var idleFn func() bool
	if len(isIdle) > 0 {
		idleFn = isIdle[0]
	}

	StartAutoUpdaterWithOptions(ctx, cfg, AutoUpdaterOptions{
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
	targetPath := opts.TargetPath
	if targetPath == "" {
		targetPath = cfg.LanguageServer
	}
	if targetPath == "" {
		targetPath = lsproc.FindLanguageServer("")
	}
	if targetPath == "" {
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

	go func() {
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

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				checkAndApply(ctx, cfg, targetPath, opts.ReloadLS, opts.IsIdle, retryInterval, maxRetries, checkUpdateFn, true)
			}
		}
	}()
}

func checkAndApply(ctx context.Context, cfg *config.Config, targetPath string, reloadLS func(), isIdle func() bool, retryInterval time.Duration, maxRetries int, checkUpdate CheckUpdateFunc, allowMaintenanceRestart bool) bool {
	if checkUpdate == nil {
		checkUpdate = CheckUpdate
	}

	currentVersion := cfg.IDEVersion
	if currentVersion == "" {
		currentVersion = "unknown"
	}

	info, err := checkUpdate(currentVersion)
	if err != nil {
		log.Printf("[auto-updater] update check failed: %v", err)
		return false
	}

	// Self-healing: if ide_version was missing or unknown in config, but the binary
	// already exists on disk, record the version without re-downloading ~170MB artifact.
	if currentVersion == "unknown" {
		if st, statErr := os.Stat(targetPath); statErr == nil && st.Size() > 10*1024*1024 {
			cfg.IDEVersion = info.LatestVersion
			if cfg.LanguageServer == "" {
				cfg.LanguageServer = targetPath
			}
			if saveErr := cfg.Save(); saveErr != nil {
				log.Printf("[auto-updater] warning: could not save config.json: %v", saveErr)
			}
			log.Printf("[auto-updater] recorded missing ide_version as %s from existing binary at %s", info.LatestVersion, targetPath)
			return false
		}
	}

	if !info.UpdateAvailable {
		log.Printf("[auto-updater] Antigravity is up to date (%s)", currentVersion)
		if !allowMaintenanceRestart {
			return false
		}

		// If server is idle, perform maintenance restart to reclaim leaked memory and flush caches.
		if isIdle == nil || isIdle() {
			log.Printf("[auto-updater] server is idle, restarting language_server for daily memory maintenance...")
			if reloadLS != nil {
				reloadLS()
			}
			return true
		}

		// If active traffic exists, defer restart by retryInterval until server becomes idle or maxRetries is reached.
		log.Printf("[auto-updater] server is busy with active traffic, deferring daily maintenance restart by %v (max %d retries)...", retryInterval, maxRetries)
		retryTicker := time.NewTicker(retryInterval)
		defer retryTicker.Stop()

		retries := 0
		for {
			select {
			case <-ctx.Done():
				return false
			case <-retryTicker.C:
				if isIdle() {
					log.Printf("[auto-updater] server became idle, restarting language_server for daily memory maintenance...")
					if reloadLS != nil {
						reloadLS()
					}
					return true
				}
				retries++
				if retries >= maxRetries {
					log.Printf("[auto-updater] server remained busy after %d deferred attempts; skipping daily maintenance restart for today", retries)
					return false
				}
				log.Printf("[auto-updater] server still busy (attempt %d/%d), deferring maintenance restart by %v...", retries, maxRetries, retryInterval)
			}
		}
	}

	// Pre-flight permission check: abort before downloading 170MB if the target directory is not writable
	if err := CheckWritable(targetPath); err != nil {
		log.Printf("[auto-updater] cannot update to %s: %v. Please run 'sudo agy-server update' to update.", info.LatestVersion, err)
		return false
	}

	log.Printf("[auto-updater] new Antigravity %s available (current: %s). Downloading...", info.LatestVersion, currentVersion)

	err = DownloadAndInstall(ctx, info.DownloadURL, targetPath, nil)
	if err != nil {
		log.Printf("[auto-updater] update failed safely (binary unmodified): %v", err)
		return false
	}

	cfg.IDEVersion = info.LatestVersion
	if cfg.LanguageServer == "" {
		cfg.LanguageServer = targetPath
	}
	if err := cfg.Save(); err != nil {
		log.Printf("[auto-updater] warning: could not save config.json: %v", err)
	}

	log.Printf("[auto-updater] successfully updated Antigravity to %s at %s", info.LatestVersion, targetPath)

	if reloadLS != nil {
		log.Printf("[auto-updater] restarting language_server to apply update...")
		reloadLS()
	}
	return true
}
