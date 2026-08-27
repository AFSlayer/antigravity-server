package updater

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/config"
	"github.com/AFSlayer/antigravity-server/internal/lsproc"
)

// StartAutoUpdater runs a background loop in serve mode that checks once a day for official Antigravity updates.
func StartAutoUpdater(ctx context.Context, cfg *config.Config, reloadLS func()) {
	targetPath := cfg.LanguageServer
	if targetPath == "" {
		targetPath = lsproc.FindLanguageServer("")
	}
	if targetPath == "" {
		targetPath = "/opt/agy-server/language_server"
	}

	go func() {
		// Wait 5 minutes after startup before the first check so initial traffic is undisturbed.
		initTimer := time.NewTimer(5 * time.Minute)
		select {
		case <-ctx.Done():
			initTimer.Stop()
			return
		case <-initTimer.C:
			initTimer.Stop()
		}

		checkAndApply(ctx, cfg, targetPath, reloadLS)

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				checkAndApply(ctx, cfg, targetPath, reloadLS)
			}
		}
	}()
}

func checkAndApply(ctx context.Context, cfg *config.Config, targetPath string, reloadLS func()) {
	currentVersion := cfg.IDEVersion
	if currentVersion == "" {
		currentVersion = "unknown"
	}

	info, err := CheckUpdate(currentVersion)
	if err != nil {
		log.Printf("[auto-updater] update check failed: %v", err)
		return
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
			return
		}
	}

	if !info.UpdateAvailable {
		log.Printf("[auto-updater] Antigravity is up to date (%s)", currentVersion)
		return
	}

	// Pre-flight permission check: abort before downloading 170MB if the target directory is not writable
	if err := CheckWritable(targetPath); err != nil {
		log.Printf("[auto-updater] cannot update to %s: %v. Please run 'sudo agy-server update' to update.", info.LatestVersion, err)
		return
	}

	log.Printf("[auto-updater] new Antigravity %s available (current: %s). Downloading...", info.LatestVersion, currentVersion)

	err = DownloadAndInstall(ctx, info.DownloadURL, targetPath, nil)
	if err != nil {
		log.Printf("[auto-updater] update failed safely (binary unmodified): %v", err)
		return
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
}
