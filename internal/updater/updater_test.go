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
		t.Fatalf("ResolveLatest failed: %v", err)
	}

	if info.LatestVersion != "2.9.1" {
		t.Errorf("want version 2.9.1, got %s", info.LatestVersion)
	}
	if info.DownloadURL != "https://storage.googleapis.com/antigravity-public/antigravity-hub/2.9.1-4871453687021568/linux-x64/Antigravity.tar.gz" {
		t.Errorf("unexpected download URL: %s", info.DownloadURL)
	}
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
	reloadLS := func() {
		reloadCalled = true
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
	reloadLS := func() {
		reloadCalled = true
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
	reloadLS := func() { reloadCalled = true }

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
	reloadLS := func() { reloadCalled = true }

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
	reloadLS := func() { reloadCalled = true }

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
	reloadLS := func() {
		select {
		case reloaded <- struct{}{}:
		default:
		}
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
