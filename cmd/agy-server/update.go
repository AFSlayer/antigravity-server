package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/AFSlayer/antigravity-server/internal/config"
	"github.com/AFSlayer/antigravity-server/internal/lsproc"
	"github.com/AFSlayer/antigravity-server/internal/updater"
)

func updateCommand(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	checkOnly := fs.Bool("check", false, "Check for updates without downloading")
	force := fs.Bool("force", false, "Re-download even if already on the latest version")
	autoYes := fs.Bool("yes", false, "Skip confirmation prompts")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	targetPath := cfg.LanguageServer
	if targetPath == "" {
		targetPath = lsproc.FindLanguageServer("")
	}
	if targetPath == "" {
		targetPath = "/opt/agy-server/language_server"
	}

	currentVersion := cfg.IDEVersion
	if currentVersion == "" {
		currentVersion = "unknown"
	}

	fmt.Printf("\n  %s\n\n", bold("Antigravity Language Server Updater"))
	fmt.Printf("  Installed version : %s (%s)\n", cyan(currentVersion), targetPath)
	fmt.Printf("  Checking official download channel...\n")

	info, err := updater.CheckUpdate(currentVersion)
	if err != nil {
		return fmt.Errorf("check updates: %w", err)
	}

	fmt.Printf("  Latest version    : %s for %s\n\n", green(info.LatestVersion), info.Platform)

	if !info.UpdateAvailable && !*force {
		fmt.Printf("  %s\n\n", green("✓ Antigravity is already up to date."))
		return nil
	}

	if *checkOnly {
		if info.UpdateAvailable {
			fmt.Printf("  %s\n\n", yellow(fmt.Sprintf("! A new version %s is available (run 'agy-server update' to install)", info.LatestVersion)))
		}
		return nil
	}

	if !*autoYes {
		fmt.Printf("  Download and install Antigravity %s to %s? [y/N] ", info.LatestVersion, targetPath)
		var answer string
		fmt.Scanln(&answer)
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Printf("  Update cancelled.\n\n")
			return nil
		}
	}

	fmt.Printf("\n  Downloading official Antigravity build...\n")
	var lastPct int = -1
	err = updater.DownloadAndInstall(context.Background(), info.DownloadURL, targetPath, func(downloaded, total int64) {
		if total > 0 {
			pct := int(float64(downloaded) / float64(total) * 100)
			if pct != lastPct {
				lastPct = pct
				fmt.Printf("\r  Progress: %d%% (%d / %d MB)", pct, downloaded/(1024*1024), total/(1024*1024))
			}
		} else {
			fmt.Printf("\r  Downloaded: %d MB", downloaded/(1024*1024))
		}
	})
	if err != nil {
		fmt.Println()
		return fmt.Errorf("install update: %w", err)
	}
	fmt.Printf("\n  %s\n\n", green(fmt.Sprintf("✓ Successfully installed Antigravity %s to %s", info.LatestVersion, targetPath)))

	// Save new IDE version to config
	cfg.IDEVersion = info.LatestVersion
	if cfg.LanguageServer == "" {
		cfg.LanguageServer = targetPath
	}
	_ = cfg.Save()

	fmt.Printf("  %s\n", dim("Next steps:"))
	fmt.Printf("    • Restart the service: %s (or restart agy-server serve)\n", cyan("sudo systemctl restart agy-server"))
	fmt.Printf("    • Verify installation: %s\n\n", cyan("agy-server doctor"))

	return nil
}
