package proxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/auth"
	"github.com/AFSlayer/antigravity-server/internal/patches"
)

// Behind the login, a capability request is authorized once: the session and
// the sidecar list are not checked again by the proxy. A capability that has
// expired gets a 403 in the frame, not the login page.
func TestSidecarCapabilityBehindLogin(t *testing.T) {
	sidecarSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		htmlPage(w, "", `<html><head></head><body>board</body></html>`)
	}))
	t.Cleanup(sidecarSrv.Close)
	scPort := serverPort(t, sidecarSrv.URL)
	ls := newSidecarUpstream(t, func() []stubSidecarEntry {
		return []stubSidecarEntry{{BaseURL: fmt.Sprintf("http://127.0.0.1:%d", scPort), Status: "SIDECAR_STATUS_RUNNING"}}
	})

	sessStore, err := auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	creds, _, err := auth.LoadOrCreateCredentials(filepath.Join(t.TempDir(), "creds.json"), "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	sessToken, err := sessStore.Create("test-agent", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	var validations atomic.Int32
	p, err := New(Options{
		TargetPort: upstreamPort(t, ls),
		ValidateSession: func(token string) bool {
			validations.Add(1)
			return sessStore.Validate(token)
		},
		Patch: patches.Options{CacheKey: "k1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	authn := auth.New(auth.Options{Credentials: creds, Sessions: sessStore, AllowSidecar: p.ValidateSidecarCapability})
	front := httptest.NewServer(authn.Middleware(p.Handler()))
	t.Cleanup(front.Close)

	entry, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/__agy/sidecar/%d/", front.URL, scPort), nil)
	entry.Header.Set("Cookie", auth.CookieName+"="+sessToken)
	entry.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, err := http.DefaultClient.Do(entry)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	capPrefix := regexp.MustCompile(`^/__agy/sidecar/\d+/~s/[0-9a-f]{32}`).FindString(resp.Request.URL.Path)
	if resp.StatusCode != http.StatusOK || capPrefix == "" {
		t.Fatalf("entry: status %d at %s", resp.StatusCode, resp.Request.URL.Path)
	}

	// A request from the frame carries no cookie.
	validations.Store(0)
	sub, _ := http.NewRequest(http.MethodGet, front.URL+capPrefix+"/", nil)
	sub.Header.Set("Origin", "null")
	resp, err = http.DefaultClient.Do(sub)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("capability request: status %d", resp.StatusCode)
	}
	if n := validations.Load(); n != 1 {
		t.Fatalf("one capability request validated the session %d times, want 1", n)
	}

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	expired, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/__agy/sidecar/%d/~s/%032x/", front.URL, scPort, 1), nil)
	expired.Header.Set("Accept", "text/html")
	resp, err = noRedirect.Do(expired)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || resp.Header.Get("Location") != "" {
		t.Fatalf("expired capability: status %d Location %q, want 403 without a redirect", resp.StatusCode, resp.Header.Get("Location"))
	}
}
