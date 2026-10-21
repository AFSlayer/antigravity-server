package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/AFSlayer/antigravity-server/internal/config"
	"github.com/AFSlayer/antigravity-server/internal/lsproc"
	"github.com/AFSlayer/antigravity-server/internal/signin"
	"github.com/AFSlayer/antigravity-server/internal/ui"
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

func TestWrapRootWithSignInRedirect(t *testing.T) {
	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fallback-body"))
	})

	// Case 1: Coordinator is nil -> fallback is returned as is
	h1 := wrapRootWithSignInRedirect(fallback, nil)
	rec1 := httptest.NewRecorder()
	h1.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec1.Code != http.StatusOK || rec1.Body.String() != "fallback-body" {
		t.Errorf("expected fallback when coordinator is nil, got code %d body %s", rec1.Code, rec1.Body.String())
	}

	// Case 2: Coordinator is unavailable (empty urlFile) -> fallback is returned
	cUnavailable := signin.New(&lsproc.Instance{Port: 1234}, "")
	h2 := wrapRootWithSignInRedirect(fallback, cUnavailable)
	rec2 := httptest.NewRecorder()
	h2.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec2.Code != http.StatusOK || rec2.Body.String() != "fallback-body" {
		t.Errorf("expected fallback when coordinator is unavailable, got code %d body %s", rec2.Code, rec2.Body.String())
	}

	// Case 3: Coordinator is available but not signed in -> redirect GET / and GET /onboarding
	authServer := func(hasValidAuth bool) *signin.Coordinator {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"authResult": map[string]any{"hasValidAuth": hasValidAuth}})
		}))
		t.Cleanup(srv.Close)
		port := srv.Listener.Addr().(*net.TCPAddr).Port
		return signin.New(&lsproc.Instance{Port: port, CSRFToken: "t"}, "/tmp/urlfile")
	}
	cUnsigned := authServer(false)
	h3 := wrapRootWithSignInRedirect(fallback, cUnsigned)

	// 3a. GET / -> 302 redirect to /__agy/signin
	rec3a := httptest.NewRecorder()
	h3.ServeHTTP(rec3a, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec3a.Code != http.StatusFound {
		t.Errorf("expected 302 redirect for GET /, got %d", rec3a.Code)
	}
	if loc := rec3a.Header().Get("Location"); loc != ui.SignInPath {
		t.Errorf("expected redirect location %q, got %q", ui.SignInPath, loc)
	}

	// 3b. GET /onboarding -> 302 redirect to /__agy/signin
	rec3b := httptest.NewRecorder()
	h3.ServeHTTP(rec3b, httptest.NewRequest(http.MethodGet, "/onboarding", nil))
	if rec3b.Code != http.StatusFound {
		t.Errorf("expected 302 redirect for GET /onboarding, got %d", rec3b.Code)
	}
	if loc := rec3b.Header().Get("Location"); loc != ui.SignInPath {
		t.Errorf("expected redirect location %q, got %q", ui.SignInPath, loc)
	}

	// 3c. GET /onboarding/ (trailing slash) -> 302 redirect to /__agy/signin
	rec3cSlash := httptest.NewRecorder()
	h3.ServeHTTP(rec3cSlash, httptest.NewRequest(http.MethodGet, "/onboarding/", nil))
	if rec3cSlash.Code != http.StatusFound {
		t.Errorf("expected 302 redirect for GET /onboarding/, got %d", rec3cSlash.Code)
	}
	if loc := rec3cSlash.Header().Get("Location"); loc != ui.SignInPath {
		t.Errorf("expected redirect location %q, got %q", ui.SignInPath, loc)
	}

	// 3d. GET /main.js -> not redirected, serves fallback
	rec3c := httptest.NewRecorder()
	h3.ServeHTTP(rec3c, httptest.NewRequest(http.MethodGet, "/main.js", nil))
	if rec3c.Code != http.StatusOK || rec3c.Body.String() != "fallback-body" {
		t.Errorf("expected fallback for /main.js, got code %d", rec3c.Code)
	}

	// 3d. POST / -> not redirected, serves fallback
	rec3d := httptest.NewRecorder()
	h3.ServeHTTP(rec3d, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec3d.Code != http.StatusOK || rec3d.Body.String() != "fallback-body" {
		t.Errorf("expected fallback for POST /, got code %d", rec3d.Code)
	}

	// Case 4: the language server cannot be asked -> no redirect, because the
	// credentials may well be fine and a wrongly sent user has to sign in again.
	cUnreachable := signin.New(&lsproc.Instance{Port: 1}, "/tmp/urlfile")
	rec4 := httptest.NewRecorder()
	wrapRootWithSignInRedirect(fallback, cUnreachable).ServeHTTP(rec4, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec4.Code != http.StatusOK || rec4.Body.String() != "fallback-body" {
		t.Errorf("an unanswered auth query must not redirect, got code %d", rec4.Code)
	}

	// Case 5: signed in -> no redirect.
	rec5 := httptest.NewRecorder()
	wrapRootWithSignInRedirect(fallback, authServer(true)).ServeHTTP(rec5, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec5.Code != http.StatusOK || rec5.Body.String() != "fallback-body" {
		t.Errorf("a signed-in user must not be redirected, got code %d", rec5.Code)
	}

	// Case 6: positive cache is warm, then user signs out and hits GET /onboarding -> immediate redirect.
	signedInDynamic := true
	dynSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"authResult": map[string]any{"hasValidAuth": signedInDynamic}})
	}))
	t.Cleanup(dynSrv.Close)
	dynPort := dynSrv.Listener.Addr().(*net.TCPAddr).Port
	cDynamic := signin.New(&lsproc.Instance{Port: dynPort, CSRFToken: "t"}, "/tmp/urlfile")
	h6 := wrapRootWithSignInRedirect(fallback, cDynamic)

	rec6Warm := httptest.NewRecorder()
	h6.ServeHTTP(rec6Warm, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec6Warm.Code != http.StatusOK {
		t.Fatalf("expected 200 on initial signed-in GET /, got %d", rec6Warm.Code)
	}

	signedInDynamic = false
	rec6Onboarding := httptest.NewRecorder()
	h6.ServeHTTP(rec6Onboarding, httptest.NewRequest(http.MethodGet, "/onboarding?login=true", nil))
	if rec6Onboarding.Code != http.StatusFound || rec6Onboarding.Header().Get("Location") != ui.SignInPath {
		t.Errorf("expected GET /onboarding after logout to bypass stale cache and redirect to %q, got code %d loc %q",
			ui.SignInPath, rec6Onboarding.Code, rec6Onboarding.Header().Get("Location"))
	}
}

func TestListenReturnsErrorWhenPortInUse(t *testing.T) {
	ln1, err := listen("127.0.0.1", 0)
	if err != nil {
		t.Fatalf("first listen failed: %v", err)
	}
	defer ln1.Close()

	port := ln1.Addr().(*net.TCPAddr).Port
	ln2, err := listen("127.0.0.1", port)
	if err == nil {
		ln2.Close()
		t.Fatalf("expected error when binding occupied port %d, got nil", port)
	}
}
