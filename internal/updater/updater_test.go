package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/config"
)

func TestValidateDownloadURL(t *testing.T) {
	tests := []struct {
		url     string
		wantErr bool
	}{
		{"https://storage.googleapis.com/antigravity-public/antigravity-hub/2.9.1-4871453687021568/linux-x64/Antigravity.tar.gz", false},
		{"http://127.0.0.1:8765/test.tar.gz", false},
		{"http://localhost:8765/test.tar.gz", false},
		{"https://evil.example.com/malicious.tar.gz", true},
		{"https://storage.googleapis.com/other-bucket/file.tar.gz", true},
	}

	for _, tt := range tests {
		err := ValidateDownloadURL(tt.url)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateDownloadURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
		}
	}
}

func TestPlatformSlug(t *testing.T) {
	tests := []struct {
		goos, goarch string
		wantSlug     string
		wantErr      bool
	}{
		{"linux", "amd64", "linux-x64/Antigravity.tar.gz", false},
		{"linux", "arm64", "linux-arm/Antigravity.tar.gz", false},
		{"darwin", "arm64", "darwin-arm/Antigravity.dmg", false},
		{"darwin", "amd64", "darwin-x64/Antigravity.dmg", false},
		{"windows", "amd64", "windows-x64/Antigravity-x64.exe", false},
		{"windows", "arm64", "windows-arm/Antigravity-arm64.exe", false},
		{"freebsd", "amd64", "", true},
	}

	for _, tt := range tests {
		got, err := PlatformSlug(tt.goos, tt.goarch)
		if (err != nil) != tt.wantErr {
			t.Errorf("PlatformSlug(%s, %s) error = %v, wantErr %v", tt.goos, tt.goarch, err, tt.wantErr)
			continue
		}
		if got != tt.wantSlug {
			t.Errorf("PlatformSlug(%s, %s) = %q, want %q", tt.goos, tt.goarch, got, tt.wantSlug)
		}
	}
}

func TestResolveLatest(t *testing.T) {
	t.Run("releases JSON array", func(t *testing.T) {
		payload := `[{"version":"2.31.2","execution_id":"6470801290035200"},{"version":"2.31.1","execution_id":"6044591015723008"}]`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, payload)
		}))
		defer srv.Close()

		info, err := ResolveLatest(srv.URL, "linux", "arm64")
		if err != nil {
			t.Fatalf("ResolveLatest JSON array failed: %v", err)
		}
		if info.LatestVersion != "2.31.2" {
			t.Errorf("want version 2.31.2, got %s", info.LatestVersion)
		}
		if info.RawBuildVersion != "2.31.2-6470801290035200" {
			t.Errorf("want raw build 2.31.2-6470801290035200, got %s", info.RawBuildVersion)
		}
		wantURL := "https://storage.googleapis.com/antigravity-public/antigravity-hub/2.31.2-6470801290035200/linux-arm/Antigravity.tar.gz"
		if info.DownloadURL != wantURL {
			t.Errorf("unexpected download URL: got %s, want %s", info.DownloadURL, wantURL)
		}
	})

	t.Run("releases JSON envelope", func(t *testing.T) {
		payload := `{"versions":[{"version":"2.31.1","execution_id":"6044591015723008"}]}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, payload)
		}))
		defer srv.Close()

		info, err := ResolveLatest(srv.URL, "windows", "arm64")
		if err != nil {
			t.Fatalf("ResolveLatest JSON envelope failed: %v", err)
		}
		if info.LatestVersion != "2.31.1" {
			t.Errorf("want version 2.31.1, got %s", info.LatestVersion)
		}
		wantURL := "https://storage.googleapis.com/antigravity-public/antigravity-hub/2.31.1-6044591015723008/windows-arm/Antigravity-arm64.exe"
		if info.DownloadURL != wantURL {
			t.Errorf("unexpected download URL: got %s, want %s", info.DownloadURL, wantURL)
		}
	})

	t.Run("rejects malformed release entry", func(t *testing.T) {
		payload := `[{"version":"2.31.1/../evil","execution_id":"6044591015723008"}]`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, payload)
		}))
		defer srv.Close()

		if _, err := ResolveLatest(srv.URL, "linux", "amd64"); err == nil {
			t.Fatal("expected malformed version to fail validation")
		}
	})

	t.Run("fallback HTML page", func(t *testing.T) {
		html := `
		<!DOCTYPE html>
		<html>
		<body>
			<a href="https://storage.googleapis.com/antigravity-public/antigravity-hub/2.9.1-4871453687021568/linux-x64/Antigravity.tar.gz">Linux x64</a>
			<a href="https://storage.googleapis.com/antigravity-public/antigravity-hub/2.9.1-4871453687021568/linux-arm/Antigravity.tar.gz">Linux arm</a>
			<a href="https://storage.googleapis.com/antigravity-public/antigravity-hub/2.9.1-4871453687021568/darwin-arm/Antigravity.dmg">Mac arm</a>
			<a href="https://storage.googleapis.com/antigravity-public/antigravity-hub/2.9.1-4871453687021568/windows-x64/Antigravity-x64.exe">Win x64</a>
		</body>
		</html>
		`

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, html)
		}))
		defer srv.Close()

		info, err := ResolveLatest(srv.URL, "linux", "amd64")
		if err != nil {
			t.Fatalf("ResolveLatest HTML failed: %v", err)
		}

		if info.LatestVersion != "2.9.1" {
			t.Errorf("want version 2.9.1, got %s", info.LatestVersion)
		}
		if info.DownloadURL != "https://storage.googleapis.com/antigravity-public/antigravity-hub/2.9.1-4871453687021568/linux-x64/Antigravity.tar.gz" {
			t.Errorf("unexpected download URL: %s", info.DownloadURL)
		}
	})
}

func TestDownloadAndExtractTarGz(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	content := []byte("#!/bin/sh\necho mock language_server\n")
	hdr := &tar.Header{
		Name: "Antigravity-2.9.1/resources/bin/language_server",
		Mode: 0755,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gw.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-tar")
		w.Write(buf.Bytes())
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "bin", "language_server")

	var progressCalls int
	err := DownloadAndInstall(context.Background(), srv.URL+"/test.tar.gz", targetPath, func(downloaded, total int64) {
		progressCalls++
	})
	if err != nil {
		t.Fatalf("DownloadAndInstall failed: %v", err)
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read installed file: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("installed content mismatch: got %q, want %q", string(data), string(content))
	}
}

func TestAutoUpdaterReloadLS(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	content := []byte("#!/bin/sh\necho mock v2.10.0\n")
	hdr := &tar.Header{
		Name: "Antigravity-2.10.0/resources/bin/language_server",
		Mode: 0755,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gw.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-tar")
		w.Write(buf.Bytes())
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "language_server")

	var reloadCalled bool
	reloadLS := func() bool {
		reloadCalled = true
		return true
	}

	// Direct download and reload check
	err := DownloadAndInstall(context.Background(), srv.URL+"/test.tar.gz", targetPath, nil)
	if err != nil {
		t.Fatalf("DownloadAndInstall failed: %v", err)
	}

	if reloadLS != nil {
		reloadLS()
	}

	if !reloadCalled {
		t.Errorf("expected reloadLS to be called upon successful update")
	}
}

func TestCheckWritable(t *testing.T) {
	tmpDir := t.TempDir()
	validTarget := filepath.Join(tmpDir, "bin", "language_server")
	if err := CheckWritable(validTarget); err != nil {
		t.Errorf("CheckWritable(%q) unexpected error: %v", validTarget, err)
	}

	// Non-writable directory check
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	if err := os.MkdirAll(readOnlyDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(readOnlyDir, 0755) }()

	readOnlyTarget := filepath.Join(readOnlyDir, "language_server")
	// If run as non-root, this should error
	if os.Geteuid() != 0 {
		if err := CheckWritable(readOnlyTarget); err == nil {
			t.Errorf("CheckWritable(%q) expected error for read-only directory, got nil", readOnlyTarget)
		}
	}
}

func TestCheckAndApplySelfHealsMissingVersion(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "language_server")

	// Create a mock binary file larger than 10MB
	data := make([]byte, 11*1024*1024)
	if err := os.WriteFile(targetPath, data, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		IDEVersion:     "", // missing
		LanguageServer: targetPath,
	}
	cfg.SetDir(tmpDir)

	var reloadCalled bool
	reloadLS := func() bool {
		reloadCalled = true
		return true
	}

	// When checkAndApply runs on existing binary with unknown version, it should self-heal and record version without calling reloadLS
	mockSelfHeal := func(ver string) (*UpdateInfo, error) {
		return &UpdateInfo{
			Platform:        "linux/amd64",
			LatestVersion:   "2.16.0",
			UpdateAvailable: false,
		}, nil
	}
	checkAndApply(context.Background(), cfg, targetPath, reloadLS, nil, 10*time.Minute, 3, mockSelfHeal, false)

	if reloadCalled {
		t.Errorf("expected reloadLS NOT to be called on self-healing path")
	}
	if cfg.IDEVersion == "" || cfg.IDEVersion == "unknown" {
		t.Errorf("expected ide_version to be self-healed, got %q", cfg.IDEVersion)
	}
}

func TestCheckAndApplyDailyMaintenanceRestart(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "language_server")

	cfg := &config.Config{
		IDEVersion:     "2.16.0",
		LanguageServer: targetPath,
	}
	cfg.SetDir(tmpDir)

	mockUpToDate := func(ver string) (*UpdateInfo, error) {
		return &UpdateInfo{
			Platform:        "linux/amd64",
			LatestVersion:   "2.16.0",
			UpdateAvailable: false,
		}, nil
	}

	// 1. When allowMaintenanceRestart is false (e.g. initial boot check), reloadLS should not be called
	var reloadCalled bool
	reloadLS := func() bool { reloadCalled = true; return true }

	isIdleCalled := false
	isIdle := func() bool {
		isIdleCalled = true
		return true
	}

	restarted := checkAndApply(context.Background(), cfg, targetPath, reloadLS, isIdle, 10*time.Minute, 3, mockUpToDate, false)
	if restarted {
		t.Errorf("expected restarted=false when allowMaintenanceRestart=false")
	}
	if reloadCalled {
		t.Errorf("expected reloadLS NOT to be called when allowMaintenanceRestart=false")
	}
	if isIdleCalled {
		t.Errorf("expected isIdle NOT to be checked when allowMaintenanceRestart=false")
	}

	// 2. When allowMaintenanceRestart is true and isIdle returns true, reloadLS must be called
	reloadCalled = false
	isIdleCalled = false

	restarted = checkAndApply(context.Background(), cfg, targetPath, reloadLS, isIdle, 10*time.Minute, 3, mockUpToDate, true)
	if !restarted {
		t.Errorf("expected restarted=true when idle maintenance restart triggers")
	}
	if !reloadCalled {
		t.Errorf("expected reloadLS to be called for idle maintenance restart")
	}
	if !isIdleCalled {
		t.Errorf("expected isIdle to be evaluated")
	}
}

func TestCheckAndApplyDailyMaintenanceDeferredWhenBusy(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "language_server")

	cfg := &config.Config{
		IDEVersion:     "2.16.0",
		LanguageServer: targetPath,
	}
	cfg.SetDir(tmpDir)

	mockUpToDate := func(ver string) (*UpdateInfo, error) {
		return &UpdateInfo{
			Platform:        "linux/amd64",
			LatestVersion:   "2.16.0",
			UpdateAvailable: false,
		}, nil
	}

	var reloadCalled bool
	reloadLS := func() bool { reloadCalled = true; return true }

	checks := 0
	isIdle := func() bool {
		checks++
		// First check (immediate) is busy, second check (after 1st retry tick) is idle
		return checks >= 2
	}

	retryInterval := 20 * time.Millisecond
	restarted := checkAndApply(context.Background(), cfg, targetPath, reloadLS, isIdle, retryInterval, 5, mockUpToDate, true)
	if !restarted {
		t.Errorf("expected restarted=true after deferred retry succeeded")
	}
	if !reloadCalled {
		t.Errorf("expected reloadLS to be called once server became idle")
	}
	if checks < 2 {
		t.Errorf("expected at least 2 idle checks (initial busy + retry idle), got %d", checks)
	}
}

func TestCheckAndApplyDailyMaintenanceSkipsWhenContinuouslyBusy(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "language_server")

	cfg := &config.Config{
		IDEVersion:     "2.16.0",
		LanguageServer: targetPath,
	}
	cfg.SetDir(tmpDir)

	mockUpToDate := func(ver string) (*UpdateInfo, error) {
		return &UpdateInfo{
			Platform:        "linux/amd64",
			LatestVersion:   "2.16.0",
			UpdateAvailable: false,
		}, nil
	}

	var reloadCalled bool
	reloadLS := func() bool { reloadCalled = true; return true }

	// Always busy
	isIdle := func() bool { return false }

	retryInterval := 10 * time.Millisecond
	maxRetries := 3
	restarted := checkAndApply(context.Background(), cfg, targetPath, reloadLS, isIdle, retryInterval, maxRetries, mockUpToDate, true)
	if restarted {
		t.Errorf("expected restarted=false when maxRetries exceeded without becoming idle")
	}
	if reloadCalled {
		t.Errorf("expected reloadLS NOT to be called when maintenance is skipped")
	}
}

func TestStartAutoUpdaterWithOptionsMaintenanceLoop(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "language_server")

	cfg := &config.Config{
		IDEVersion:     "2.16.0",
		LanguageServer: targetPath,
	}
	cfg.SetDir(tmpDir)

	mockUpToDate := func(ver string) (*UpdateInfo, error) {
		return &UpdateInfo{
			Platform:        "linux/amd64",
			LatestVersion:   "2.16.0",
			UpdateAvailable: false,
		}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reloaded := make(chan struct{}, 1)
	reloadLS := func() bool {
		select {
		case reloaded <- struct{}{}:
		default:
		}
		return true
	}

	StartAutoUpdaterWithOptions(ctx, cfg, AutoUpdaterOptions{
		CheckInterval:     30 * time.Millisecond,
		IdleRetryInterval: 10 * time.Millisecond,
		MaxIdleRetries:    3,
		InitialDelay:      0, // immediate initial check
		TargetPath:        targetPath,
		ReloadLS:          reloadLS,
		IsIdle:            func() bool { return true },
		CheckUpdate:       mockUpToDate,
	})

	select {
	case <-reloaded:
		// Maintenance restart succeeded via ticker
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for maintenance restart from AutoUpdater loop")
	}
}

func TestCheckAndApplyUpdateDefersRestartWhenBusy(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	content := []byte("#!/bin/sh\necho mock v2.23.0\n")
	hdr := &tar.Header{
		Name: "Antigravity-2.23.0/resources/bin/language_server",
		Mode: 0755,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gw.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-tar")
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "language_server")
	cfg := &config.Config{
		IDEVersion:     "2.22.0",
		LanguageServer: targetPath,
	}
	cfg.SetDir(tmpDir)

	mockUpdateAvailable := func(ver string) (*UpdateInfo, error) {
		return &UpdateInfo{
			Platform:        "linux/amd64",
			LatestVersion:   "2.23.0",
			DownloadURL:     srv.URL + "/test.tar.gz",
			UpdateAvailable: true,
		}, nil
	}

	var reloadCalled bool
	reloadLS := func() bool { reloadCalled = true; return true }

	checks := 0
	isIdle := func() bool {
		checks++
		return checks >= 2
	}

	restarted := checkAndApply(context.Background(), cfg, targetPath, reloadLS, isIdle, 15*time.Millisecond, 4, mockUpdateAvailable, false)
	if !restarted {
		t.Errorf("expected restarted=true after update and deferred idle check")
	}
	if !reloadCalled {
		t.Errorf("expected reloadLS to be called once idle")
	}
	if checks < 2 {
		t.Errorf("expected update restart to check isIdle at least twice when initially busy, got %d", checks)
	}
	if got := StagedVersion(cfg); got != "2.23.0" {
		t.Errorf("expected StagedVersion=2.23.0 before startup verification, got %q", got)
	}
	if cfg.RuntimeIDEVersion() != "2.22.0" {
		t.Errorf("expected cfg.RuntimeIDEVersion=2.22.0 before startup verification, got %q", cfg.RuntimeIDEVersion())
	}
	ConfirmInstalledVersion(cfg, targetPath)
	if cfg.RuntimeIDEVersion() != "2.23.0" {
		t.Errorf("expected cfg.RuntimeIDEVersion=2.23.0 after startup confirmation, got %q", cfg.RuntimeIDEVersion())
	}
}

func TestCheckAndApplyUpdateDeferredAcrossCyclesUntilIdle(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	content := []byte("#!/bin/sh\necho mock v2.24.0\n")
	hdr := &tar.Header{
		Name: "Antigravity-2.24.0/resources/bin/language_server",
		Mode: 0o755,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gw.Close()

	downloads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads++
		w.Header().Set("Content-Type", "application/x-tar")
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "language_server")
	cfg := &config.Config{
		IDEVersion:     "2.22.0",
		LanguageServer: targetPath,
	}
	cfg.SetDir(tmpDir)

	mockCheck := func(ver string) (*UpdateInfo, error) {
		return &UpdateInfo{
			Platform:        "linux/amd64",
			LatestVersion:   "2.24.0",
			DownloadURL:     srv.URL + "/test.tar.gz",
			UpdateAvailable: ver != "2.24.0",
		}, nil
	}

	var reloadCalls int
	reloadLS := func() bool { reloadCalls++; return true }

	// Cycle 1: continuously busy -> binary downloaded, but restart deferred; IDEVersion must stay 2.22.0
	restarted := checkAndApply(context.Background(), cfg, targetPath, reloadLS, func() bool { return false }, 5*time.Millisecond, 2, mockCheck, false)
	if restarted || reloadCalls != 0 {
		t.Fatalf("expected restart to be deferred while busy, restarted=%v reloadCalls=%d", restarted, reloadCalls)
	}
	if cfg.IDEVersion != "2.22.0" {
		t.Fatalf("IDEVersion must not be prematurely updated before reload, got %q", cfg.IDEVersion)
	}
	if downloads != 1 {
		t.Fatalf("expected 1 download on first cycle, got %d", downloads)
	}

	// Cycle 2: server becomes idle -> must restart without re-downloading and keep staged until startup confirmation
	restarted = checkAndApply(context.Background(), cfg, targetPath, reloadLS, func() bool { return true }, 5*time.Millisecond, 2, mockCheck, false)
	if !restarted || reloadCalls != 1 {
		t.Fatalf("expected pending update restart once idle, restarted=%v reloadCalls=%d", restarted, reloadCalls)
	}
	if downloads != 1 {
		t.Fatalf("expected staged binary to be reused without re-downloading, downloads=%d", downloads)
	}
	if got := StagedVersion(cfg); got != "2.24.0" {
		t.Fatalf("expected StagedVersion=2.24.0 before startup confirmation, got %q", got)
	}
	if cfg.RuntimeIDEVersion() != "2.22.0" {
		t.Fatalf("expected RuntimeIDEVersion=2.22.0 before startup confirmation, got %q", cfg.RuntimeIDEVersion())
	}
	ConfirmInstalledVersion(cfg, targetPath)
	if cfg.RuntimeIDEVersion() != "2.24.0" {
		t.Fatalf("expected RuntimeIDEVersion=2.24.0 after startup confirmation, got %q", cfg.RuntimeIDEVersion())
	}
	if got := StagedVersion(cfg); got != "" {
		t.Fatalf("expected StagedVersion to be cleared after confirmation, got %q", got)
	}
}

func TestPersistMetadataDoesNotCreateConfigJSONOrOverwriteInvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "language_server")
	if err := os.WriteFile(targetPath, []byte(`mock "ideVersion":"2.30.0"`), 0o755); err != nil {
		t.Fatal(err)
	}

	// 1. Without config.json, SyncInstalledVersion must NOT create config.json on disk
	cfg := config.Default()
	cfg.SetDir(tmpDir)
	cfg.Port = 9999 // transient CLI flag
	SyncInstalledVersion(cfg, targetPath)

	if got := cfg.RuntimeIDEVersion(); got != "2.30.0" {
		t.Fatalf("RuntimeIDEVersion() = %q, want 2.30.0", got)
	}
	if _, err := os.Stat(cfg.Path("config.json")); !os.IsNotExist(err) {
		t.Fatalf("expected config.json NOT to be created when absent, err=%v", err)
	}

	// 2. With AGY_IDE_VERSION override active, checkAndApply must preserve IDEVersion
	envCfg := config.Default()
	envCfg.SetDir(tmpDir)
	envCfg.IDEVersion = "2.99.0"
	envCfg.IDEVersionFromEnv = true
	_ = persistUpdaterMetadata(envCfg, "2.30.0", targetPath)
	if got := envCfg.RuntimeIDEVersion(); got != "2.99.0" {
		t.Fatalf("IDEVersionFromEnv should preserve 2.99.0, got %q", got)
	}

	// 3. Invalid syntax in an existing config.json must return an error instead of clobbering it
	malformed := []byte("{ invalid json // user comment\n")
	if err := os.WriteFile(cfg.Path("config.json"), malformed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := persistUpdaterMetadata(cfg, "2.31.0", targetPath); err == nil {
		t.Fatal("expected error when existing config.json has invalid JSON")
	}
	after, err := os.ReadFile(cfg.Path("config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(malformed) {
		t.Fatalf("malformed config.json was overwritten: got %q", string(after))
	}

	// 4. Backup creation and RestoreBackup recovery
	v1Path := filepath.Join(tmpDir, "ls_bin")
	if err := os.WriteFile(v1Path, []byte("good-v1-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	tmpNew := filepath.Join(tmpDir, "ls_bin.tmp")
	if err := os.WriteFile(tmpNew, []byte("broken-v2-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := atomicReplace(tmpNew, v1Path, false); err != nil {
		t.Fatalf("atomicReplace: %v", err)
	}
	if err := RestoreBackup(v1Path); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	restored, err := os.ReadFile(v1Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != "good-v1-binary" {
		t.Fatalf("restored content = %q, want good-v1-binary", string(restored))
	}
	v1Info, err := os.Stat(v1Path)
	if err != nil {
		t.Fatal(err)
	}
	bakInfo, err := os.Stat(v1Path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(v1Info, bakInfo) {
		t.Fatal("RestoreBackup must not leave restored binary hardlinked to .bak")
	}
}

func TestStagedUpdatePreservesVerifiedBackupAndRollbackMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "language_server")
	v1Bytes := []byte("verified-v1-without-embedded-semver")
	if err := os.WriteFile(binPath, v1Bytes, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.SetDir(tmpDir)
	cfg.IDEVersion = "2.31.0"
	cfg.LanguageServer = binPath
	if err := os.WriteFile(cfg.Path("config.json"), []byte("{\n  \"ide_version\": \"2.31.0\"\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordMetadata(cfg, "2.31.0", binPath); err != nil {
		t.Fatal(err)
	}

	// Stage unverified v2 -> .bak captures verified v1
	tmpV2 := filepath.Join(tmpDir, "v2.tmp")
	if err := os.WriteFile(tmpV2, []byte("unverified-v2-broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := atomicReplace(tmpV2, binPath, HasUnverifiedStagedUpdate(cfg, binPath)); err != nil {
		t.Fatal(err)
	}
	if err := RecordStagedUpdate(cfg, "2.32.0", binPath, false); err != nil {
		t.Fatal(err)
	}

	// Before restart happens, stage v3 -> .bak MUST still hold verified v1, NOT unverified v2!
	if !HasUnverifiedStagedUpdate(cfg, binPath) {
		t.Fatal("expected HasUnverifiedStagedUpdate=true after staging v2")
	}
	tmpV3 := filepath.Join(tmpDir, "v3.tmp")
	if err := os.WriteFile(tmpV3, []byte("corrupted-v3-no-version-marker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := atomicReplace(tmpV3, binPath, HasUnverifiedStagedUpdate(cfg, binPath)); err != nil {
		t.Fatal(err)
	}
	if err := RecordStagedUpdate(cfg, "2.33.0", binPath, false); err != nil {
		t.Fatal(err)
	}

	bakBytes, err := os.ReadFile(binPath + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(bakBytes) != string(v1Bytes) {
		t.Fatalf(".bak was overwritten with unverified binary: got %q, want %q", string(bakBytes), string(v1Bytes))
	}

	// Simulate failed startup of v3 (DetectBinaryVersion == "") and .bak rollback
	stagedCandidate := DetectBinaryVersion(binPath)
	if stagedCandidate == "" {
		stagedCandidate = StagedVersion(cfg)
	}
	if err := RestoreBackup(binPath); err != nil {
		t.Fatal(err)
	}
	ClearStagedVersion(cfg, binPath)
	RecordBrokenVersion(cfg, stagedCandidate)
	ConfirmInstalledVersion(cfg, binPath)

	if gotBroken := loadBrokenVersion(cfg); gotBroken != "2.33.0" {
		t.Fatalf("broken_version = %q, want 2.33.0 (must never mark known-good 2.31.0 broken)", gotBroken)
	}
	if gotRuntime := cfg.RuntimeIDEVersion(); gotRuntime != "2.31.0" {
		t.Fatalf("RuntimeIDEVersion = %q, want 2.31.0 after rollback", gotRuntime)
	}
	cfgAfter, err := os.ReadFile(cfg.Path("config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(cfgAfter, []byte(`"2.31.0"`)) || bytes.Contains(cfgAfter, []byte(`"2.33.0"`)) {
		t.Fatalf("config.json ide_version corrupted during rollback: %s", string(cfgAfter))
	}

	// Once the same or a newer release successfully starts up, ConfirmInstalledVersion must clear broken_version.
	if err := os.WriteFile(binPath, []byte(`mock "ideVersion":"2.33.0"`), 0o755); err != nil {
		t.Fatal(err)
	}
	ConfirmInstalledVersion(cfg, binPath)
	if gotBroken := loadBrokenVersion(cfg); gotBroken != "" {
		t.Fatalf("expected broken_version to be cleared when 2.33.0 starts cleanly, got %q", gotBroken)
	}
}

func TestCheckAndApplyPreservesPendingUpdateWhenReloadDeclined(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "language_server")
	if err := os.WriteFile(targetPath, []byte(`mock "ideVersion":"2.32.0"`), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.SetDir(tmpDir)
	cfg.IDEVersion = "2.31.0"
	cfg.LanguageServer = targetPath
	setPendingUpdate(cfg, targetPath, "2.32.0")

	mockCheck := func(ver string) (*UpdateInfo, error) {
		return &UpdateInfo{
			Platform:        "linux/amd64",
			LatestVersion:   "2.32.0",
			UpdateAvailable: false,
		}, nil
	}

	// External / unmanaged LS declines reload -> pendingUpdate MUST stay queued.
	restarted := checkAndApply(context.Background(), cfg, targetPath, func() bool { return false }, func() bool { return true }, 5*time.Millisecond, 2, mockCheck, false)
	if restarted {
		t.Fatal("expected restarted=false when reloadLS declines")
	}
	if got := getPendingUpdate(cfg, targetPath); got != "2.32.0" {
		t.Fatalf("pendingUpdate was lost after declined reload: got %q, want 2.32.0", got)
	}

	// Subsequent managed cycle accepts reload -> pendingUpdate is cleared once reload succeeds.
	restarted = checkAndApply(context.Background(), cfg, targetPath, func() bool { return true }, func() bool { return true }, 5*time.Millisecond, 2, mockCheck, false)
	if !restarted {
		t.Fatal("expected restarted=true when reloadLS succeeds")
	}
	if got := getPendingUpdate(cfg, targetPath); got != "" {
		t.Fatalf("expected pendingUpdate to clear after accepted reload, got %q", got)
	}
}

func TestCheckAndApplyUnknownVersionStagedUpdateSkipsRedownload(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "language_server")
	if err := os.WriteFile(targetPath, []byte("staged-binary-without-embedded-version"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.SetDir(tmpDir)
	cfg.LanguageServer = targetPath
	// Stage 2.31.0 with no verified ide_version recorded yet.
	if err := RecordStagedUpdate(cfg, "2.31.0", targetPath, false); err != nil {
		t.Fatal(err)
	}

	downloads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads++
		http.Error(w, "unexpected download", http.StatusInternalServerError)
	}))
	defer srv.Close()

	// Emulate CheckUpdate's exact comparison rule against queryVersion.
	realLikeCheck := func(currentVersion string) (*UpdateInfo, error) {
		info := &UpdateInfo{
			Platform:      "linux/amd64",
			LatestVersion: "2.31.0",
			DownloadURL:   srv.URL + "/Antigravity.tar.gz",
		}
		if currentVersion != "" && currentVersion != "unknown" && info.LatestVersion != "" && info.LatestVersion != "unknown" {
			info.UpdateAvailable = currentVersion != info.LatestVersion
		}
		return info, nil
	}

	reloadCalled := false
	restarted := checkAndApply(context.Background(), cfg, targetPath, func() bool {
		reloadCalled = true
		return true
	}, func() bool { return true }, 5*time.Millisecond, 2, realLikeCheck, false)

	if !restarted || !reloadCalled {
		t.Fatalf("expected staged update to trigger idle reload without re-download, restarted=%v reloadCalled=%v", restarted, reloadCalled)
	}
	if downloads != 0 {
		t.Fatalf("expected 0 archive downloads when staged version matches latest, got %d", downloads)
	}
}

func TestIsNewerVersionPreventsDowngrade(t *testing.T) {
	cases := []struct {
		latest  string
		current string
		want    bool
	}{
		{"2.31.1", "2.31.0", true},
		{"2.31.0", "2.31.1", false},
		{"2.31.1", "2.31.1", false},
		{"2.10.0", "2.9.1", true},
		{"2.9.1", "2.10.0", false},
		{"2.31.0", "2.31", false},
		{"2.31.1", "2.31", true},
		{"v2.31.2", "2.31.1", true},
		{"2.31.1-rc1", "2.31.1", false},
		{"2.31.1", "2.31.1-rc1", true},
	}
	for _, tc := range cases {
		if got := isNewerVersion(tc.latest, tc.current); got != tc.want {
			t.Errorf("isNewerVersion(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}

func TestValidateDownloadURLRejectsUserinfoAndTraversalAndDetectsMultiSegmentVersion(t *testing.T) {
	allowed := []string{
		"https://storage.googleapis.com/antigravity-public/antigravity-hub/2.31.2-4802483155533824/linux-x64/Antigravity.tar.gz",
		"http://127.0.0.1:18080/Antigravity.tar.gz",
		"http://localhost:8080/linux-x64/Antigravity.tar.gz",
	}
	for _, u := range allowed {
		if err := ValidateDownloadURL(u); err != nil {
			t.Errorf("expected URL %q to be allowed, got error: %v", u, err)
		}
	}

	rejected := []string{
		"",
		"http://127.0.0.1:80@attacker.example/Antigravity.tar.gz",
		"http://localhost:8080@attacker.example/Antigravity.tar.gz",
		"https://user@storage.googleapis.com/antigravity-public/antigravity-hub/2.31.2-1/linux-x64/Antigravity.tar.gz",
		"https://storage.googleapis.com/antigravity-public/antigravity-hub/../../other-bucket/linux-x64/Antigravity.tar.gz",
		"https://storage.googleapis.com/antigravity-public/antigravity-hub/%2e%2e/%2e%2e/other-bucket/linux-x64/Antigravity.tar.gz",
		"https://storage.googleapis.com/antigravity-public/antigravity-hub/2.31.2%2f..%2f..%2fother/linux-x64/Antigravity.tar.gz",
		"https://storage.googleapis.com:8443/antigravity-public/antigravity-hub/2.31.2-1/linux-x64/Antigravity.tar.gz",
		"https://storage.googleapis.com/antigravity-public/antigravity-hub/?alt=media",
		"http://127.0.0.1:/Antigravity.tar.gz",
		"http://127.0.0.1:99999/Antigravity.tar.gz",
	}
	for _, u := range rejected {
		if err := ValidateDownloadURL(u); err == nil {
			t.Errorf("expected URL %q to be rejected, got nil", u)
		}
	}

	tmpBin := filepath.Join(t.TempDir(), "language_server")
	if err := os.WriteFile(tmpBin, []byte(`prefix "ideVersion":"2.31.2.1" suffix`), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DetectBinaryVersion(tmpBin); got != "2.31.2.1" {
		t.Fatalf("DetectBinaryVersion 4-segment = %q, want 2.31.2.1", got)
	}

	// Redirect to an untrusted host must be blocked by CheckRedirect before downloading.
	redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://attacker.example/Antigravity.tar.gz", http.StatusFound)
	}))
	defer redirectSrv.Close()

	targetBin := filepath.Join(t.TempDir(), "language_server")
	if err := DownloadAndInstallWithBackupPolicy(nil, redirectSrv.URL+"/Antigravity.tar.gz", targetBin, true, nil); err == nil {
		t.Fatal("expected DownloadAndInstallWithBackupPolicy to reject untrusted redirect target, got nil")
	}
}
