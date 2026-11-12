package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/auth"
	"github.com/AFSlayer/antigravity-server/internal/patches"
)

// Headers a reverse proxy or tunnel adds about the user do not reach a
// sidecar. Credentials the sidecar's own scripts set do.
func TestSidecarGetsNoFrontProxyHeaders(t *testing.T) {
	var got atomic.Value
	f := newSidecarPolicyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" {
			got.Store(r.Header.Clone())
			_, _ = io.WriteString(w, "{}")
			return
		}
		htmlPage(w, "", "<html><head></head><body></body></html>")
	})
	front := map[string]string{
		"Cf-Access-Jwt-Assertion":            "eyJ.jwt",
		"Cf-Connecting-Ip":                   "203.0.113.7",
		"Cf-Access-Authenticated-User-Email": "me@example.com",
		"X-Forwarded-Port":                   "443",
		"X-Forwarded-Prefix":                 "/agy",
		"Tailscale-User-Login":               "me@example.com",
		"True-Client-Ip":                     "203.0.113.7",
		"X-Real-Ip":                          "203.0.113.7",
		"Proxy-Authorization":                "Basic cHJveHk6cHc=",
		"Cdn-Loop":                           "cloudflare",
	}
	headers := map[string]string{"Authorization": "Basic dXNlcjpwYXNz"}
	for k, v := range front {
		headers[k] = v
	}
	if resp, _ := f.get(t, "/api", headers); resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	h := got.Load().(http.Header)
	for name := range front {
		if v := h.Get(name); v != "" {
			t.Errorf("sidecar received %s: %q", name, v)
		}
	}
	if v := h.Get("Authorization"); v != "" {
		t.Errorf("sidecar received the browser's Basic credentials: %q", v)
	}

	if _, _ = f.get(t, "/api", map[string]string{"Authorization": "Bearer sidecar-token"}); got.Load().(http.Header).Get("Authorization") != "Bearer sidecar-token" {
		t.Error("a Bearer token the sidecar's scripts set was dropped")
	}
}

// Sidecar responses stay out of shared caches, and a rewritten stylesheet is
// revalidated against its rewrite.
func TestSidecarCaching(t *testing.T) {
	css := atomic.Value{}
	css.Store(`body{background:url("/bg.png")}`)
	f := newSidecarPolicyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plain.js":
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = io.WriteString(w, "1")
		case "/public.js":
			w.Header().Set("Content-Type", "application/javascript")
			w.Header().Set("Cache-Control", "public, max-age=60, s-maxage=600")
			_, _ = io.WriteString(w, "1")
		case "/nostore.js":
			w.Header().Set("Content-Type", "application/javascript")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = io.WriteString(w, "1")
		case "/site.css":
			w.Header().Set("Content-Type", "text/css")
			w.Header().Set("Cache-Control", "public, max-age=3600")
			_, _ = io.WriteString(w, css.Load().(string))
		default:
			htmlPage(w, "", "<html><head></head><body></body></html>")
		}
	})
	for path, want := range map[string]string{
		"/plain.js":   "private",
		"/public.js":  "private, max-age=60",
		"/nostore.js": "no-store",
	} {
		resp, _ := f.get(t, path, nil)
		if got := resp.Header.Get("Cache-Control"); got != want {
			t.Errorf("%s: Cache-Control %q, want %q", path, got, want)
		}
	}

	resp, body := f.get(t, "/site.css", nil)
	etag := resp.Header.Get("ETag")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(etag, `W/"agy-`) || resp.Header.Get("Cache-Control") != "private, no-cache" ||
		!strings.Contains(body, f.capPrefix+"/bg.png") {
		t.Fatalf("site.css: %d ETag %q Cache-Control %q body %q", resp.StatusCode, etag, resp.Header.Get("Cache-Control"), body)
	}
	resp, body = f.get(t, "/site.css", map[string]string{"If-None-Match": etag})
	if resp.StatusCode != http.StatusNotModified || body != "" {
		t.Fatalf("unchanged site.css: status %d body %q, want 304", resp.StatusCode, body)
	}
	css.Store(`body{color:red}`)
	resp, body = f.get(t, "/site.css", map[string]string{"If-None-Match": etag})
	if resp.StatusCode != http.StatusOK || body != `body{color:red}` || resp.Header.Get("ETag") == etag {
		t.Fatalf("changed site.css: status %d body %q ETag %q", resp.StatusCode, body, resp.Header.Get("ETag"))
	}
}

// While GetSidecars fails, at most one call goes out per backoff, and a
// valid capability gets a retryable 503 rather than an expired-access 403.
func TestSidecarListFailureBackoffAnd503(t *testing.T) {
	sidecarSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		htmlPage(w, "", `<html><head></head><body>board</body></html>`)
	}))
	t.Cleanup(sidecarSrv.Close)
	scPort := serverPort(t, sidecarSrv.URL)
	var failing atomic.Bool
	var rpcs atomic.Int32
	ls := newSidecarUpstreamWithStatus(t, func() (int, []stubSidecarEntry) {
		rpcs.Add(1)
		if failing.Load() {
			return http.StatusInternalServerError, nil
		}
		return http.StatusOK, []stubSidecarEntry{{BaseURL: fmt.Sprintf("http://127.0.0.1:%d", scPort), Status: "SIDECAR_STATUS_RUNNING"}}
	})
	sessStore, err := auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	creds, _, err := auth.LoadOrCreateCredentials(filepath.Join(t.TempDir(), "creds.json"), "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	sessToken, _ := sessStore.Create("test-agent", "127.0.0.1")
	p, err := New(Options{TargetPort: upstreamPort(t, ls), ValidateSession: sessStore.Validate, Patch: patches.Options{CacheKey: "k1"}})
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
	resp.Body.Close()
	capPrefix := regexp.MustCompile(`^/__agy/sidecar/\d+/~s/[0-9a-f]{32}`).FindString(resp.Request.URL.Path)
	if capPrefix == "" {
		t.Fatalf("entry ended at %s", resp.Request.URL.Path)
	}

	capRequest := func() int {
		sub, _ := http.NewRequest(http.MethodGet, front.URL+capPrefix+"/", nil)
		sub.Header.Set("Origin", "null")
		resp, err := http.DefaultClient.Do(sub)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	waitBackground := func() {
		deadline := time.Now().Add(5 * time.Second)
		for {
			p.sidecarMu.Lock()
			busy := p.sidecarBackgroundRefresh || p.sidecarInflight != nil
			p.sidecarMu.Unlock()
			if !busy || time.Now().After(deadline) {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}
	setListAge := func(age time.Duration) {
		p.sidecarMu.Lock()
		p.sidecarCachedAt = time.Now().Add(-age)
		p.sidecarMu.Unlock()
	}

	// A failed call does not close a sidecar in use: the last list that
	// succeeded allows it for two cache TTLs.
	failing.Store(true)
	setListAge(sidecarCacheTTL + 500*time.Millisecond)
	if code := capRequest(); code != http.StatusOK {
		t.Fatalf("capability request right after GetSidecars failed: status %d, want 200 from the last list", code)
	}
	waitBackground()

	// While GetSidecars fails, one call goes out per backoff however many
	// checks ask.
	p.sidecarMu.Lock()
	p.sidecarCachedAt = time.Time{}
	p.sidecarLastErr = nil
	p.sidecarLastProbe = time.Time{}
	p.sidecarMu.Unlock()
	rpcs.Store(0)
	for i := 0; i < 20; i++ {
		if !p.IsSidecarReservedPort(context.Background(), 5173+i) {
			t.Fatal("a failing GetSidecars did not fail closed")
		}
	}
	if n := rpcs.Load(); n != 1 {
		t.Fatalf("20 checks while GetSidecars fails sent %d calls, want 1", n)
	}

	// Past two cache TTLs the last list is not trusted.
	setListAge(2*sidecarCacheTTL + 500*time.Millisecond)
	sub, _ := http.NewRequest(http.MethodGet, front.URL+capPrefix+"/", nil)
	sub.Header.Set("Origin", "null")
	resp, err = http.DefaultClient.Do(sub)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("capability request while GetSidecars fails with a list two TTLs old: status %d, want 503 with Retry-After", resp.StatusCode)
	}
}

// A GetSidecars call that hangs until its timeout does not stall a sidecar
// in the last list: it is answered at once while the call runs behind.
func TestSidecarListHangDoesNotStall(t *testing.T) {
	sidecarSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		htmlPage(w, "", `<html><head></head><body>board</body></html>`)
	}))
	t.Cleanup(sidecarSrv.Close)
	scPort := serverPort(t, sidecarSrv.URL)
	var hang atomic.Bool
	ls := newSidecarUpstreamWithStatus(t, func() (int, []stubSidecarEntry) {
		if hang.Load() {
			time.Sleep(time.Second)
		}
		return http.StatusOK, []stubSidecarEntry{{BaseURL: fmt.Sprintf("http://127.0.0.1:%d", scPort), Status: "SIDECAR_STATUS_RUNNING"}}
	})
	p, err := New(Options{TargetPort: upstreamPort(t, ls), Patch: patches.Options{CacheKey: "k1"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	p.sidecarRPCTimeout = 300 * time.Millisecond
	if _, ok := p.allowedSidecarDialMode(context.Background(), scPort); !ok {
		t.Fatal("the sidecar is not allowed")
	}

	hang.Store(true)
	p.sidecarMu.Lock()
	p.sidecarCachedAt = time.Now().Add(-sidecarCacheTTL - 500*time.Millisecond)
	p.sidecarMu.Unlock()
	start := time.Now()
	if _, ok := p.allowedSidecarDialMode(context.Background(), scPort); !ok {
		t.Fatal("a sidecar in the last list was refused while GetSidecars hangs")
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("a sidecar in the last list waited %v for a hanging GetSidecars", d)
	}
	p.sidecarMu.Lock()
	started := p.sidecarBackgroundRefresh
	p.sidecarMu.Unlock()
	if !started {
		t.Fatal("the expired list was not refreshed in the background")
	}

	// Let the background call time out before the test's servers close.
	deadline := time.Now().Add(5 * time.Second)
	for {
		p.sidecarMu.Lock()
		busy := p.sidecarBackgroundRefresh
		p.sidecarMu.Unlock()
		if !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the background GetSidecars call did not end")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
