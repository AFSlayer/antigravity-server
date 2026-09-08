package main

import (
	"os"
	"strings"
	"testing"

	"github.com/AFSlayer/antigravity-server/internal/config"
	"github.com/AFSlayer/antigravity-server/internal/lsproc"
)

func TestCSRFTokenPersistence(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("AGY_HOME", tempDir)

	cfg := config.Default()
	r := &runner{cfg: cfg}

	// 1. Initial creation: token file does not exist
	token1 := r.loadOrCreateCSRFToken()
	if token1 == "" {
		t.Fatal("expected non-empty CSRF token")
	}

	tokenPath := cfg.Path("csrf-token.txt")
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("expected csrf-token.txt to exist: %v", err)
	}
	if strings.TrimSpace(string(data)) != token1 {
		t.Errorf("persisted token %q does not match returned token %q", string(data), token1)
	}

	// 2. Reuse: subsequent calls should return the exact same token
	token2 := r.loadOrCreateCSRFToken()
	if token2 != token1 {
		t.Errorf("expected reused token %q, got %q", token1, token2)
	}

	// 3. Pre-existing file with extra whitespace
	customDir := t.TempDir()
	t.Setenv("AGY_HOME", customDir)
	customCfg := config.Default()
	customRunner := &runner{cfg: customCfg}

	customPath := customCfg.Path("csrf-token.txt")
	expectedCustomToken := "my-custom-persistent-token-12345"
	if err := os.WriteFile(customPath, []byte("  "+expectedCustomToken+"  \n"), 0o600); err != nil {
		t.Fatal(err)
	}

	loadedToken := customRunner.loadOrCreateCSRFToken()
	if loadedToken != expectedCustomToken {
		t.Errorf("expected %q, got %q", expectedCustomToken, loadedToken)
	}
}

func TestSyncInstanceCSRFToken(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("AGY_HOME", tempDir)
	cfg := config.Default()
	r := &runner{cfg: cfg}
	tokenPath := cfg.Path("csrf-token.txt")

	// Case 1: nil instance should not panic
	r.syncInstanceCSRFToken(nil)

	// Case 2: Running instance has a token, disk file is missing -> writes token
	inst1 := &lsproc.Instance{CSRFToken: "token-inst-1"}
	r.syncInstanceCSRFToken(inst1)
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("failed to read token: %v", err)
	}
	if strings.TrimSpace(string(data)) != "token-inst-1" {
		t.Errorf("want %q, got %q", "token-inst-1", string(data))
	}

	// Case 3: Running instance has a different token -> overwrites disk file
	inst2 := &lsproc.Instance{CSRFToken: "token-inst-2-updated"}
	r.syncInstanceCSRFToken(inst2)
	data, err = os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("failed to read token: %v", err)
	}
	if strings.TrimSpace(string(data)) != "token-inst-2-updated" {
		t.Errorf("want %q, got %q", "token-inst-2-updated", string(data))
	}

	// Case 4: Running instance has empty token, disk has token -> fills instance token
	inst3 := &lsproc.Instance{CSRFToken: ""}
	r.syncInstanceCSRFToken(inst3)
	if inst3.CSRFToken != "token-inst-2-updated" {
		t.Errorf("want %q, got %q", "token-inst-2-updated", inst3.CSRFToken)
	}
}
