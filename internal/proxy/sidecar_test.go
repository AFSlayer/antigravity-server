package proxy

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/auth"
	"github.com/AFSlayer/antigravity-server/internal/patches"
	"github.com/coder/websocket"
)

type stubSidecarEntry struct {
	BaseURL string `json:"baseUrl,omitempty"`
	WebPort int    `json:"webPort,omitempty"`
	Status  string `json:"status,omitempty"`
}

func newSidecarUpstreamWithStatus(t *testing.T, handler func() (int, []stubSidecarEntry)) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/exa.language_server_pb.LanguageServerService/GetSidecars", func(w http.ResponseWriter, r *http.Request) {
		code, list := handler()
		if code != http.StatusOK {
			http.Error(w, "GetSidecars failure", code)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sidecars": list,
		})
	})
	mux.HandleFunc("/main.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(stubBundle))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(indexHTML))
	})

	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newSidecarUpstream(t *testing.T, entries func() []stubSidecarEntry) *httptest.Server {
	t.Helper()
	return newSidecarUpstreamWithStatus(t, func() (int, []stubSidecarEntry) {
		return http.StatusOK, entries()
	})
}

func TestSidecarPortAllowlistReservedRefreshAndFailClosed(t *testing.T) {
	activeSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(activeSidecar.Close)
	activePort := serverPort(t, activeSidecar.URL)

	rawSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("raw-web-port-secret"))
	}))
	t.Cleanup(rawSidecar.Close)
	rawPort := serverPort(t, rawSidecar.URL)

	stoppedSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("stopped"))
	}))
	t.Cleanup(stoppedSidecar.Close)
	stoppedPort := serverPort(t, stoppedSidecar.URL)

	unrelatedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("unrelated"))
	}))
	t.Cleanup(unrelatedSrv.Close)
	unrelatedPort := serverPort(t, unrelatedSrv.URL)

	forbiddenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("control-panel"))
	}))
	t.Cleanup(forbiddenSrv.Close)
	forbiddenPort := serverPort(t, forbiddenSrv.URL)

	newlySpawnedPort := 49876
	var includeNewSidecar atomic.Bool
	var failGetSidecars atomic.Bool

	ls := newSidecarUpstreamWithStatus(t, func() (int, []stubSidecarEntry) {
		if failGetSidecars.Load() {
			return http.StatusServiceUnavailable, nil
		}
		list := []stubSidecarEntry{
			{
				BaseURL: fmt.Sprintf("http://localhost:%d", activePort),
				WebPort: rawPort,
				Status:  "SIDECAR_STATUS_STOPPED",
			},
			{
				WebPort: stoppedPort,
				Status:  "SIDECAR_STATUS_STOPPED",
			},
			{
				BaseURL: fmt.Sprintf("http://127.0.0.1:%d", forbiddenPort),
				Status:  "SIDECAR_STATUS_RUNNING",
			},
		}
		if includeNewSidecar.Load() {
			list = append(list, stubSidecarEntry{
				BaseURL: fmt.Sprintf("http://localhost:%d", newlySpawnedPort),
				Status:  "SIDECAR_STATUS_RUNNING",
			})
		}
		return http.StatusOK, list
	})

	p, err := New(Options{
		TargetPort:     upstreamPort(t, ls),
		ForbiddenPorts: []int{forbiddenPort},
		Patch:          patches.Options{CacheKey: "k1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	front := httptest.NewServer(p.Handler())
	t.Cleanup(front.Close)

	// Gateway baseUrl port is allowed even before on-demand sidecar status flips to RUNNING.
	resp, err := http.Get(fmt.Sprintf("%s/__agy/sidecar/%d/api/state", front.URL, activePort))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("active gateway port status = %d, want 200", resp.StatusCode)
	}

	// Raw webPort behind a gateway must NOT be directly reachable via sidecar route,
	// yet MUST be reported by IsSidecarReservedPort so generic loopback fetch blocks it too.
	for _, blocked := range []int{rawPort, stoppedPort, unrelatedPort, forbiddenPort, upstreamPort(t, ls)} {
		r, err := http.Get(fmt.Sprintf("%s/__agy/sidecar/%d/", front.URL, blocked))
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusForbidden {
			t.Errorf("port %d: status = %d, want 403", blocked, r.StatusCode)
		}
	}

	if !p.IsSidecarReservedPort(context.Background(), activePort) {
		t.Errorf("gateway port %d is not reported as reserved", activePort)
	}
	if !p.IsSidecarReservedPort(context.Background(), rawPort) {
		t.Errorf("raw webPort %d is not reported as reserved", rawPort)
	}

	// Unrelated dev server port is NOT reserved while GetSidecars is healthy.
	if p.IsSidecarReservedPort(context.Background(), unrelatedPort) {
		t.Fatalf("unrelated port %d should not be reserved", unrelatedPort)
	}

	// Newly spawned sidecar within the cache TTL window is caught immediately via live refresh.
	includeNewSidecar.Store(true)
	if !p.IsSidecarReservedPort(context.Background(), newlySpawnedPort) {
		t.Fatalf("newly spawned sidecar port %d escaped IsSidecarReservedPort during cache TTL window", newlySpawnedPort)
	}

	// Fail-closed: if GetSidecars fails, IsSidecarReservedPort returns true (blocks loopback preview)
	// and sidecar allowlist returns 403 once cache expires (no error grace window).
	// The free answer for unrelatedPort is remembered for a moment; once it
	// expires the failure is seen.
	failGetSidecars.Store(true)
	p.sidecarMu.Lock()
	p.sidecarFreePorts[unrelatedPort] = time.Now().Add(-sidecarFreePortTTL)
	p.sidecarMu.Unlock()
	if !p.IsSidecarReservedPort(context.Background(), unrelatedPort) {
		t.Fatal("IsSidecarReservedPort must fail closed (true) when GetSidecars fails")
	}
	p.sidecarMu.Lock()
	p.sidecarCachedAt = time.Now().Add(-10 * time.Second)
	p.sidecarMu.Unlock()

	failResp, err := http.Get(fmt.Sprintf("%s/__agy/sidecar/%d/api/state", front.URL, activePort))
	if err != nil {
		t.Fatal(err)
	}
	failResp.Body.Close()
	if failResp.StatusCode != http.StatusForbidden {
		t.Fatalf("sidecar proxy must fail closed (403) when GetSidecars is down, got %d", failResp.StatusCode)
	}
}

func TestSidecarOpaqueOriginSandboxAndCookieVault(t *testing.T) {
	var gotCookieHeader atomic.Value
	var gotCSRFHeader atomic.Value
	var gotOriginHeader atomic.Value
	var gotXFFHeader atomic.Value

	sidecarSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookieHeader.Store(r.Header.Get("Cookie"))
		gotCSRFHeader.Store(r.Header.Get("x-codeium-csrf-token"))
		gotOriginHeader.Store(r.Header.Get("Origin"))
		gotXFFHeader.Store(r.Header.Get("X-Forwarded-For"))

		switch r.URL.Path {
		case "/":
			w.Header().Add("Set-Cookie", "agy_session=evil_override; Path=/; HttpOnly")
			w.Header().Add("Set-Cookie", "swarm_auth=tok123; Domain=localhost; Path=/; HttpOnly")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Clear-Site-Data", `"cookies", "storage"`)
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
			w.Header().Set("Alt-Svc", `h3=":443"`)
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'sha256-xyz123='; frame-ancestors 'none'")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><head>` +
				`<title><a href="/keep-in-title">Title</a></title>` +
				`<link rel="stylesheet" href="styles/app.css" integrity="sha384-localbad">` +
				`<link rel="stylesheet" href="https://cdn.example.org/ext.css" integrity="sha384-keepcdn">` +
				`<style>body{background:url('img/bg.svg')}</style></head>` +
				`<body><script src="/preload.js" integrity="sha256-stripme"></script>` +
				`<script>const tpl = '<a href="/keep-js-literal">x</a>';</script>` +
				`<textarea><a href="/keep-in-textarea">raw</a></textarea>` +
				`<iframe srcdoc="<a href='/keep-srcdoc'>inner</a>"></iframe>` +
				`<img src="/logo.png" srcset="logo-1x.png 1x, /logo-2x.png 2x"></body></html>`))
		case "/sub/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head></head><body><script src="bundle.js"></script><a href="../top">up</a></body></html>`))
		case "/not-found-spa":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`<html><head></head><body><script src="/spa-404.js"></script></body></html>`))
		case "/gzip-page":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			_, _ = gz.Write([]byte(`<html><head></head><body><a href="/inside-gz">gz</a></body></html>`))
			_ = gz.Close()
		case "/api/mutate":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"mutated":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(sidecarSrv.Close)
	scPort := serverPort(t, sidecarSrv.URL)

	otherSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("other-sidecar"))
	}))
	t.Cleanup(otherSidecar.Close)
	otherPort := serverPort(t, otherSidecar.URL)

	ls := newSidecarUpstream(t, func() []stubSidecarEntry {
		return []stubSidecarEntry{
			{BaseURL: fmt.Sprintf("http://localhost:%d", scPort), Status: "SIDECAR_STATUS_RUNNING"},
			{BaseURL: fmt.Sprintf("http://localhost:%d", otherPort), Status: "SIDECAR_STATUS_RUNNING"},
		}
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

	p, err := New(Options{
		TargetPort:      upstreamPort(t, ls),
		TargetCSRFToken: "secret-ls-csrf",
		ValidateSession: sessStore.Validate,
		Patch:           patches.Options{CacheKey: "k1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)

	authn := auth.New(auth.Options{
		Credentials:  creds,
		Sessions:     sessStore,
		AllowSidecar: p.ValidateSidecarCapability,
	})
	front := httptest.NewServer(authn.Middleware(p.Handler()))
	t.Cleanup(front.Close)

	noRedirectClient := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	// 1. Unauthenticated entry without session cookie is rejected by auth middleware.
	unauthResp, err := noRedirectClient.Get(fmt.Sprintf("%s/__agy/sidecar/%d/", front.URL, scPort))
	if err != nil {
		t.Fatal(err)
	}
	unauthResp.Body.Close()
	if unauthResp.StatusCode != http.StatusFound && unauthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated entry status = %d, want 302/401", unauthResp.StatusCode)
	}

	// 2. Cross-site top-level navigation to initial entry is rejected with 403 even with valid session cookie.
	crossNavReq, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/__agy/sidecar/%d/", front.URL, scPort), nil)
	crossNavReq.Header.Set("Cookie", auth.CookieName+"="+sessToken)
	crossNavReq.Header.Set("Sec-Fetch-Site", "cross-site")
	crossNavResp, err := noRedirectClient.Do(crossNavReq)
	if err != nil {
		t.Fatal(err)
	}
	crossNavResp.Body.Close()
	if crossNavResp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site initial entry status = %d, want 403", crossNavResp.StatusCode)
	}

	// 3. Missing trailing slash redirects straight to the capability URL with
	// the query preserved.
	slashReq, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/__agy/sidecar/%d?token=t1", front.URL, scPort), nil)
	slashReq.Header.Set("Cookie", auth.CookieName+"="+sessToken)
	slashReq.Header.Set("Sec-Fetch-Site", "same-origin")
	slashResp, err := noRedirectClient.Do(slashReq)
	if err != nil {
		t.Fatal(err)
	}
	slashResp.Body.Close()
	if slashResp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("slash redirect status = %d, want 307", slashResp.StatusCode)
	}
	if got := slashResp.Header.Get("Location"); !regexp.MustCompile(fmt.Sprintf(`^/__agy/sidecar/%d/~s/[0-9a-f]{32}/\?token=t1$`, scPort)).MatchString(got) {
		t.Fatalf("Location = %q, want the capability URL with a trailing slash and the query", got)
	}

	// 4. Authenticated initial entry isolates sidecar in an opaque-origin CSP sandbox,
	// never forwards browser cookies or X-Forwarded-For upstream, and vaults Set-Cookie server-side.
	// The entry mints a capability and redirects to it, so the document is
	// served from a URL the opaque-origin frame can request again.
	entryReq, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/__agy/sidecar/%d/?token=t1", front.URL, scPort), nil)
	entryReq.Header.Set("Cookie", auth.CookieName+"="+sessToken+"; browser_other=do_not_forward")
	entryReq.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, err := noRedirectClient.Do(entryReq)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	entryLoc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusTemporaryRedirect || !regexp.MustCompile(fmt.Sprintf(`^/__agy/sidecar/%d/~s/[0-9a-f]{32}/\?token=t1$`, scPort)).MatchString(entryLoc) {
		t.Fatalf("entry = %d to %q, want 307 to the capability URL with the query", resp.StatusCode, entryLoc)
	}
	if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("Referrer-Policy") != "no-referrer" || len(resp.Header.Values("Set-Cookie")) != 0 {
		t.Fatalf("entry redirect headers: %v", resp.Header)
	}

	capEntryReq, _ := http.NewRequest(http.MethodGet, front.URL+entryLoc, nil)
	capEntryReq.Header.Set("Cookie", auth.CookieName+"="+sessToken+"; browser_other=do_not_forward")
	capEntryReq.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, err = noRedirectClient.Do(capEntryReq)
	if err != nil {
		t.Fatal(err)
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	html := string(bodyBytes)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("capability entry status = %d, want 200", resp.StatusCode)
	}

	if got, _ := gotCookieHeader.Load().(string); got != "" {
		t.Fatalf("initial entry forwarded browser Cookie upstream: %q, want empty", got)
	}
	if got, _ := gotCSRFHeader.Load().(string); got != "" {
		t.Fatalf("upstream received language_server CSRF token %q", got)
	}
	if got, _ := gotXFFHeader.Load().(string); got != "" {
		t.Fatalf("upstream received X-Forwarded-For %q, want empty", got)
	}
	if got, _ := gotOriginHeader.Load().(string); got != fmt.Sprintf("http://localhost:%d", scPort) {
		t.Fatalf("upstream Origin = %q", got)
	}
	if cookies := resp.Header.Values("Set-Cookie"); len(cookies) != 0 {
		t.Fatalf("browser received Set-Cookie headers %v; all sidecar cookies must stay in server vault", cookies)
	}
	if got := resp.Header.Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want no-referrer", got)
	}
	for _, name := range []string{"Clear-Site-Data", "Strict-Transport-Security", "Alt-Svc"} {
		if v := resp.Header.Get(name); v != "" {
			t.Errorf("origin-wide header %s: %q reached the browser", name, v)
		}
	}
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "sandbox allow-scripts") || strings.Contains(csp, "allow-same-origin") || strings.Contains(csp, "allow-top-navigation") {
		t.Fatalf("unsafe sidecar CSP %q: must enforce opaque-origin sandbox without allow-same-origin or allow-top-navigation", csp)
	}
	if !strings.Contains(csp, "frame-ancestors 'self'") {
		t.Errorf("CSP %q missing frame-ancestors 'self'", csp)
	}

	// Extract capability prefix /__agy/sidecar/<port>/~s/<cap> from injected shim tag.
	capRe := regexp.MustCompile(fmt.Sprintf(`/__agy/sidecar/%d/~s/([0-9a-f]{32})`, scPort))
	m := capRe.FindStringSubmatch(html)
	if len(m) < 2 {
		t.Fatalf("rewritten HTML missing capability prefix:\n%s", html)
	}
	capPrefix := m[0]

	// Verify nonce propagation (because upstream CSP had sha256 hash), relative/root URL rewrites,
	// SRI integrity stripping on rewritten local assets (while preserving external CDN integrity),
	// and untouched <title>, <textarea>, <script>, and srcdoc contents.
	for _, want := range []string{
		fmt.Sprintf(`src="%s/__agy_sidecar_shim.js"`, capPrefix),
		`nonce="`,
		fmt.Sprintf(`href="%s/styles/app.css"`, capPrefix),
		`href="https://cdn.example.org/ext.css" integrity="sha384-keepcdn"`,
		fmt.Sprintf(`url('%s/img/bg.svg')`, capPrefix),
		fmt.Sprintf(`src="%s/preload.js"`, capPrefix),
		fmt.Sprintf(`src="%s/logo.png"`, capPrefix),
		fmt.Sprintf(`srcset="%s/logo-1x.png 1x, %s/logo-2x.png 2x"`, capPrefix, capPrefix),
		`<title><a href="/keep-in-title">Title</a></title>`,
		`<textarea><a href="/keep-in-textarea">raw</a></textarea>`,
		`const tpl = '<a href="/keep-js-literal">x</a>';`,
		`srcdoc="<a href='/keep-srcdoc'>inner</a>"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rewritten HTML missing %q\nGot:\n%s", want, html)
		}
	}
	if strings.Contains(html, "sha384-localbad") || strings.Contains(html, "sha256-stripme") {
		t.Errorf("SRI integrity attribute was not stripped from rewritten tag:\n%s", html)
	}

	// 5. Subdirectory relative URL resolution and non-200 HTML rewrite.
	subReq, _ := http.NewRequest(http.MethodGet, front.URL+capPrefix+"/sub/", nil)
	subReq.Header.Set("Origin", "null")
	subReq.Header.Set("Sec-Fetch-Site", "cross-site")
	subResp, err := noRedirectClient.Do(subReq)
	if err != nil {
		t.Fatal(err)
	}
	subBytes, _ := io.ReadAll(subResp.Body)
	subResp.Body.Close()
	subHTML := string(subBytes)
	for _, want := range []string{
		fmt.Sprintf(`src="%s/sub/bundle.js"`, capPrefix),
		fmt.Sprintf(`href="%s/top"`, capPrefix),
	} {
		if !strings.Contains(subHTML, want) {
			t.Errorf("sub-path HTML missing %q:\n%s", want, subHTML)
		}
	}

	nfReq, _ := http.NewRequest(http.MethodGet, front.URL+capPrefix+"/not-found-spa", nil)
	nfResp, err := noRedirectClient.Do(nfReq)
	if err != nil {
		t.Fatal(err)
	}
	nfBytes, _ := io.ReadAll(nfResp.Body)
	nfResp.Body.Close()
	if nfResp.StatusCode != http.StatusNotFound || !strings.Contains(string(nfBytes), fmt.Sprintf(`src="%s/spa-404.js"`, capPrefix)) {
		t.Fatalf("non-200 HTML response was not rewritten (status=%d): %s", nfResp.StatusCode, string(nfBytes))
	}

	// Forced-gzip upstream HTML is transparently decompressed and rewritten.
	gzResp, err := noRedirectClient.Get(front.URL + capPrefix + "/gzip-page")
	if err != nil {
		t.Fatal(err)
	}
	gzBody, _ := io.ReadAll(gzResp.Body)
	gzResp.Body.Close()
	if !strings.Contains(string(gzBody), fmt.Sprintf(`href="%s/inside-gz"`, capPrefix)) {
		t.Errorf("gzipped HTML was not rewritten: %s", string(gzBody))
	}

	// 6. Sandboxed iframe subrequests (no agy_session cookie, Origin: null, Sec-Fetch-Site: cross-site):
	//    CORS preflight succeeds with 204, POST succeeds through auth.Middleware, and vaulted cookie
	//    (swarm_auth=tok123) is automatically injected upstream!
	optReq, _ := http.NewRequest(http.MethodOptions, front.URL+capPrefix+"/api/mutate", nil)
	optReq.Header.Set("Origin", "null")
	optReq.Header.Set("Sec-Fetch-Site", "cross-site")
	optReq.Header.Set("Access-Control-Request-Method", "POST")
	optReq.Header.Set("Access-Control-Request-Headers", "content-type,x-custom")
	optResp, err := noRedirectClient.Do(optReq)
	if err != nil {
		t.Fatal(err)
	}
	optResp.Body.Close()
	if optResp.StatusCode != http.StatusNoContent || optResp.Header.Get("Access-Control-Allow-Origin") != "null" {
		t.Fatalf("CORS preflight failed: status=%d allow-origin=%q", optResp.StatusCode, optResp.Header.Get("Access-Control-Allow-Origin"))
	}

	postReq, _ := http.NewRequest(http.MethodPost, front.URL+capPrefix+"/api/mutate", strings.NewReader(`{"a":1}`))
	postReq.Header.Set("Origin", "null")
	postReq.Header.Set("Sec-Fetch-Site", "cross-site")
	postReq.Header.Set("Content-Type", "application/json")
	postResp, err := noRedirectClient.Do(postReq)
	if err != nil {
		t.Fatal(err)
	}
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusOK {
		t.Fatalf("sandboxed capability POST status = %d, want 200", postResp.StatusCode)
	}
	if got := postResp.Header.Get("Access-Control-Allow-Origin"); got != "null" {
		t.Fatalf("POST Access-Control-Allow-Origin = %q, want null", got)
	}
	if got, _ := gotCookieHeader.Load().(string); got != "swarm_auth=tok123" {
		t.Fatalf("server cookie vault did not inject swarm_auth=tok123 on capability POST: got %q", got)
	}

	// 7. Capability token cannot be replayed against another sidecar port, parent routes, or foreign Origin.
	wrongPortURL := fmt.Sprintf("%s/__agy/sidecar/%d/~s/%s/api", front.URL, otherPort, m[1])
	wpReq, _ := http.NewRequest(http.MethodGet, wrongPortURL, nil)
	wpReq.Header.Set("Origin", "null")
	wpResp, err := noRedirectClient.Do(wpReq)
	if err != nil {
		t.Fatal(err)
	}
	wpResp.Body.Close()
	if wpResp.StatusCode != http.StatusFound && wpResp.StatusCode != http.StatusUnauthorized && wpResp.StatusCode != http.StatusForbidden {
		t.Fatalf("capability token replay on another port must fail, got %d", wpResp.StatusCode)
	}

	evilOriginReq, _ := http.NewRequest(http.MethodPost, front.URL+capPrefix+"/api/mutate", strings.NewReader("{}"))
	evilOriginReq.Header.Set("Origin", "https://evil.example")
	evilResp, err := noRedirectClient.Do(evilOriginReq)
	if err != nil {
		t.Fatal(err)
	}
	evilResp.Body.Close()
	if evilResp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign Origin with capability token must be rejected (403), got %d", evilResp.StatusCode)
	}

	// 8. Revoking the parent agy_session immediately invalidates the sidecar capability token.
	if err := sessStore.Revoke(sessToken); err != nil {
		t.Fatal(err)
	}
	revokedReq, _ := http.NewRequest(http.MethodGet, front.URL+capPrefix+"/sub/", nil)
	revokedReq.Header.Set("Origin", "null")
	revokedResp, err := noRedirectClient.Do(revokedReq)
	if err != nil {
		t.Fatal(err)
	}
	revokedResp.Body.Close()
	if revokedResp.StatusCode != http.StatusFound && revokedResp.StatusCode != http.StatusUnauthorized && revokedResp.StatusCode != http.StatusForbidden {
		t.Fatalf("revoked parent session did not invalidate sidecar capability: got %d", revokedResp.StatusCode)
	}
}

func TestSidecarWebSocketHardeningCloseAndIdle(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	scPort := ln.Addr().(*net.TCPAddr).Port

	var wsCookie atomic.Value
	wsConnected := make(chan struct{}, 1)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				req, err := http.ReadRequest(br)
				if err != nil {
					return
				}
				switch req.URL.Path {
				case "/":
					body := "<html><head></head><body>entry</body></html>"
					_, _ = fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nSet-Cookie: ws_vault=ok77\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
					return
				case "/ws-reject":
					body := `<html><head></head><body><a href="/login">Auth Required</a></body></html>`
					_, _ = fmt.Fprintf(c, "HTTP/1.1 401 Unauthorized\r\nContent-Type: text/html\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
					return
				case "/sse":
					_, _ = fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nCache-Control: no-cache\r\n\r\ndata: ping\n\n")
					time.Sleep(300 * time.Millisecond)
					return
				}

				wsCookie.Store(req.Header.Get("Cookie"))
				acceptKey := computeWebSocketAcceptKey(req.Header.Get("Sec-WebSocket-Key"))
				handshakeAndFrame := fmt.Sprintf(
					"HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\nSet-Cookie: agy_session=bad; Path=/\r\nSet-Cookie: sc_ws=1; Path=/\r\n\r\n",
					acceptKey,
				)
				packet := append([]byte(handshakeAndFrame), 0x81, 0x05, 'h', 'e', 'l', 'l', 'o')
				_, _ = c.Write(packet)
				select {
				case wsConnected <- struct{}{}:
				default:
				}
				buf := make([]byte, 64)
				for {
					if _, err := br.Read(buf); err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	ls := newSidecarUpstream(t, func() []stubSidecarEntry {
		return []stubSidecarEntry{{BaseURL: fmt.Sprintf("http://localhost:%d", scPort), Status: "SIDECAR_STATUS_RUNNING"}}
	})

	p, err := New(Options{
		TargetPort: upstreamPort(t, ls),
		Patch:      patches.Options{CacheKey: "k1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(p.Handler())
	t.Cleanup(front.Close)

	// Mint capability token via initial entry (which also stores ws_vault=ok77 in the server cookie vault).
	entryResp, err := http.Get(fmt.Sprintf("%s/__agy/sidecar/%d/", front.URL, scPort))
	if err != nil {
		t.Fatal(err)
	}
	entryBytes, _ := io.ReadAll(entryResp.Body)
	entryResp.Body.Close()
	capRe := regexp.MustCompile(fmt.Sprintf(`/__agy/sidecar/%d/~s/[0-9a-f]{32}`, scPort))
	capPrefix := capRe.FindString(string(entryBytes))
	if capPrefix == "" {
		t.Fatalf("missing capability prefix in %s", string(entryBytes))
	}

	// 1. POST request smuggling with Upgrade: websocket is rejected with 400.
	badMethodReq, _ := http.NewRequest(http.MethodPost, front.URL+capPrefix+"/ws", strings.NewReader("smuggle"))
	badMethodReq.Header.Set("Origin", "null")
	badMethodReq.Header.Set("Connection", "Upgrade")
	badMethodReq.Header.Set("Upgrade", "websocket")
	badMethodReq.Header.Set("Sec-WebSocket-Version", "13")
	badMethodReq.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	badMethodResp, err := http.DefaultClient.Do(badMethodReq)
	if err != nil {
		t.Fatal(err)
	}
	badMethodResp.Body.Close()
	if badMethodResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST websocket upgrade status = %d, want 400", badMethodResp.StatusCode)
	}

	// 2. Non-101 HTML rejection rewrites body cleanly without Content-Length mismatch.
	rejReq, _ := http.NewRequest(http.MethodGet, front.URL+capPrefix+"/ws-reject", nil)
	rejReq.Header.Set("Origin", "null")
	rejReq.Header.Set("Connection", "Upgrade")
	rejReq.Header.Set("Upgrade", "websocket")
	rejReq.Header.Set("Sec-WebSocket-Version", "13")
	rejReq.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	rejResp, err := http.DefaultClient.Do(rejReq)
	if err != nil {
		t.Fatal(err)
	}
	rejBody, rErr := io.ReadAll(rejResp.Body)
	rejResp.Body.Close()
	if rErr != nil {
		t.Fatalf("failed reading rewritten non-101 WS response (Content-Length mismatch): %v", rErr)
	}
	if rejResp.StatusCode != http.StatusUnauthorized || !strings.Contains(string(rejBody), capPrefix+"/login") {
		t.Fatalf("unexpected non-101 WS response (%d): %s", rejResp.StatusCode, string(rejBody))
	}

	// 3. Long-lived SSE stream releases activeHTTPReqs once headers arrive so idle shutdown is not pinned forever.
	sseCtx, sseCancel := context.WithCancel(context.Background())
	defer sseCancel()
	sseReq, _ := http.NewRequestWithContext(sseCtx, http.MethodGet, front.URL+capPrefix+"/sse", nil)
	sseReq.Header.Set("Origin", "null")
	sseResp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatal(err)
	}
	defer sseResp.Body.Close()

	// 4. Capability-scoped Origin: null WebSocket upgrade succeeds, injects vaulted cookie,
	// strips 101 Set-Cookie, preserves coalesced initial frame, does not pin IsIdle, and closes promptly on p.Close().
	wsURL := "ws" + strings.TrimPrefix(front.URL, "http") + capPrefix + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, hsResp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Origin": []string{"null"},
			"Cookie": []string{auth.CookieName + "=master_secret"},
		},
	})
	if err != nil {
		t.Fatalf("websocket.Dial failed: %v", err)
	}
	defer c.CloseNow()

	<-wsConnected
	if got, _ := wsCookie.Load().(string); got != "ws_vault=ok77" {
		t.Fatalf("upstream WS Cookie = %q, want vaulted ws_vault=ok77 (never master_secret)", got)
	}
	if hsResp != nil && len(hsResp.Header.Values("Set-Cookie")) != 0 {
		t.Fatalf("WS 101 leaked Set-Cookie headers to browser: %v", hsResp.Header.Values("Set-Cookie"))
	}

	_, msg, err := c.Read(ctx)
	if err != nil || string(msg) != "hello" {
		t.Fatalf("initial buffered WS frame = %q, err = %v", string(msg), err)
	}

	// Wait briefly past idle window; open idle sidecar WS + SSE must not block IsIdle.
	time.Sleep(40 * time.Millisecond)
	if !p.IsIdle(20 * time.Millisecond) {
		t.Fatal("open idle sidecar WS/SSE pinned Proxy.IsIdle to false")
	}

	// Closing the proxy immediately tears down hijacked sidecar WS sockets.
	closeDone := make(chan error, 1)
	go func() {
		_, _, err := c.Read(context.Background())
		closeDone <- err
	}()
	p.Close()
	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("p.Close() did not tear down hijacked sidecar WebSocket connection")
	}
}

func TestSidecarReverseProxyPublicURLAndMultipleCSPs(t *testing.T) {
	sidecarSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			// Comma-separated policies in one header + a second stricter header restricting img-src.
			w.Header().Add("Content-Security-Policy", "default-src 'self', script-src 'self' 'nonce-up1'")
			w.Header().Add("Content-Security-Policy", "script-src 'self' 'sha256-xyz123='; img-src 'none'")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><head></head><body><script nonce="up1">window.ok=1;</script></body></html>`))
		case "/strict-none":
			w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head></head><body>static</body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(sidecarSrv.Close)
	scPort := serverPort(t, sidecarSrv.URL)

	ls := newSidecarUpstream(t, func() []stubSidecarEntry {
		return []stubSidecarEntry{{BaseURL: fmt.Sprintf("http://127.0.0.1:%d", scPort), Status: "SIDECAR_STATUS_RUNNING"}}
	})

	p, err := New(Options{
		TargetPort: upstreamPort(t, ls),
		PublicURL:  "https://mybox.ts.net",
		Patch:      patches.Options{CacheKey: "k1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	front := httptest.NewServer(p.Handler())
	t.Cleanup(front.Close)

	// Simulate a local reverse proxy (such as Tailscale Serve) that forwards to 127.0.0.1:<port>
	// without X-Forwarded-Host / TrustedProxies while the browser accesses https://mybox.ts.net.
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/__agy/sidecar/%d/", front.URL, scPort), nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Referer", "https://mybox.ts.net/c/abc")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reverse-proxied initial entry status = %d, want 200", resp.StatusCode)
	}

	csps := resp.Header.Values("Content-Security-Policy")
	if len(csps) != 3 {
		t.Fatalf("expected 3 separate CSP headers preserved from comma+multi-header upstream, got %d: %v", len(csps), csps)
	}

	wantHTTPPrefix := fmt.Sprintf("https://mybox.ts.net/__agy/sidecar/%d/", scPort)
	wantWSPrefix := fmt.Sprintf("wss://mybox.ts.net/__agy/sidecar/%d/", scPort)
	if !strings.Contains(csps[0], wantHTTPPrefix) || !strings.Contains(csps[0], wantWSPrefix) {
		t.Fatalf("policy[0] missing external PublicURL prefixes (%s / %s): %q", wantHTTPPrefix, wantWSPrefix, csps[0])
	}
	if strings.Contains(csps[0], "'self',") {
		t.Fatalf("comma-separated CSP was not split cleanly: %q", csps[0])
	}

	html := string(bodyBytes)
	nonceRe := regexp.MustCompile(`<script nonce="([0-9a-f]{24})" src="(/__agy/sidecar/\d+/~s/[0-9a-f]{32})/__agy_sidecar_shim\.js"></script>`)
	nm := nonceRe.FindStringSubmatch(html)
	if len(nm) < 3 {
		t.Fatalf("injected shim script missing common hex nonce:\n%s", html)
	}
	shimNonce := nm[1]
	capPrefix := nm[2]

	if !strings.Contains(csps[1], "'nonce-up1'") || !strings.Contains(csps[1], "'nonce-"+shimNonce+"'") {
		t.Fatalf("policy[1] must keep upstream nonce-up1 AND add common shim nonce %s: %q", shimNonce, csps[1])
	}
	if !strings.Contains(csps[2], "'sha256-xyz123='") || !strings.Contains(csps[2], "'nonce-"+shimNonce+"'") || !strings.Contains(csps[2], "img-src 'none'") {
		t.Fatalf("policy[2] must keep sha256, add shim nonce, and preserve img-src 'none': %q", csps[2])
	}

	// Verify default-src 'none' is NOT widened to allow arbitrary connect/img/object sources.
	noneReq, _ := http.NewRequest(http.MethodGet, front.URL+capPrefix+"/strict-none", nil)
	noneReq.Header.Set("Origin", "null")
	noneResp, err := http.DefaultClient.Do(noneReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(noneResp.Body)
	noneResp.Body.Close()
	noneCSP := noneResp.Header.Get("Content-Security-Policy")
	if !strings.Contains(noneCSP, "default-src 'none'") || !strings.Contains(noneCSP, "script-src 'nonce-") {
		t.Fatalf("default-src 'none' policy was weakened instead of adding minimal script-src nonce: %q", noneCSP)
	}
}

func TestSidecarLoopbackAddressFamilyIsolationAndCapRenewal(t *testing.T) {
	ln4, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln4.Close() })
	sharedPort := ln4.Addr().(*net.TCPAddr).Port

	ln6, err := net.Listen("tcp6", fmt.Sprintf("[::1]:%d", sharedPort))
	if err != nil {
		t.Skipf("IPv6 loopback on shared port %d unavailable: %v", sharedPort, err)
	}
	t.Cleanup(func() { _ = ln6.Close() })

	var v4Hits atomic.Int64
	var v6Hits atomic.Int64

	srv4 := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v4Hits.Add(1)
		http.Error(w, "wrong-v4-service", http.StatusTeapot)
	})}
	go func() { _ = srv4.Serve(ln4) }()
	t.Cleanup(func() { _ = srv4.Close() })

	srv6 := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v6Hits.Add(1)
		if isWebSocketUpgrade(r) {
			c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
			if err != nil {
				return
			}
			defer c.CloseNow()
			_ = c.Write(r.Context(), websocket.MessageText, []byte("v6-ws-ok:"+r.Header.Get("Cookie")))
			return
		}
		if r.URL.Path == "/" {
			w.Header().Add("Set-Cookie", "v6_vault=preserved99; Path=/")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<html><head></head><body>v6-ok</body></html>"))
			return
		}
		_, _ = w.Write([]byte("v6-api:" + r.Header.Get("Cookie")))
	})}
	go func() { _ = srv6.Serve(ln6) }()
	t.Cleanup(func() { _ = srv6.Close() })

	var useAmbiguousLocalhost atomic.Bool
	ls := newSidecarUpstream(t, func() []stubSidecarEntry {
		if useAmbiguousLocalhost.Load() {
			return []stubSidecarEntry{{BaseURL: fmt.Sprintf("http://localhost:%d", sharedPort), Status: "SIDECAR_STATUS_RUNNING"}}
		}
		return []stubSidecarEntry{{BaseURL: fmt.Sprintf("http://[::1]:%d", sharedPort), Status: "SIDECAR_STATUS_RUNNING"}}
	})

	sessStore, err := auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sessToken, err := sessStore.Create("agent", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	p, err := New(Options{
		TargetPort:      upstreamPort(t, ls),
		ValidateSession: sessStore.Validate,
		ResolveSidecarHost: func(_ context.Context, port int, _ string) (string, bool) {
			if port == sharedPort {
				return "::1", true
			}
			return "", false
		},
		Patch: patches.Options{CacheKey: "k1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	front := httptest.NewServer(p.Handler())
	t.Cleanup(front.Close)

	// 1. Explicit http://[::1]:port BaseURL dials [::1] exclusively (both HTTP and WebSocket), never 127.0.0.1.
	req1, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/__agy/sidecar/%d/", front.URL, sharedPort), nil)
	req1.Header.Set("Cookie", auth.CookieName+"="+sessToken)
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK || !strings.Contains(string(b1), "v6-ok") {
		t.Fatalf("IPv6 sidecar HTTP failed (%d): %s", resp1.StatusCode, string(b1))
	}

	capRe := regexp.MustCompile(fmt.Sprintf(`/__agy/sidecar/%d/~s/([0-9a-f]{32})`, sharedPort))
	m1 := capRe.FindStringSubmatch(string(b1))
	if len(m1) < 2 {
		t.Fatalf("missing capability token in %s", string(b1))
	}
	capPrefix1 := m1[0]
	capTok1 := m1[1]

	// Ambiguous localhost BaseURL resolved via ResolveSidecarHost also dials [::1] for WebSockets without touching 127.0.0.1.
	useAmbiguousLocalhost.Store(true)
	p.sidecarMu.Lock()
	p.sidecarCachedAt = time.Time{}
	p.sidecarMu.Unlock()

	wsURL := "ws" + strings.TrimPrefix(front.URL, "http") + capPrefix1 + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsConn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"null"}},
	})
	if err != nil {
		t.Fatalf("IPv6 sidecar WebSocket dial failed: %v", err)
	}
	_, wsMsg, err := wsConn.Read(ctx)
	wsConn.CloseNow()
	if err != nil || string(wsMsg) != "v6-ws-ok:v6_vault=preserved99" {
		t.Fatalf("IPv6 sidecar WebSocket message = %q, err = %v", string(wsMsg), err)
	}
	if v4Hits.Load() != 0 || v6Hits.Load() < 2 {
		t.Fatalf("address-family isolation violated: v4Hits=%d (want 0), v6Hits=%d", v4Hits.Load(), v6Hits.Load())
	}

	// 2. Near-expiry capability (<30m remaining of 12h max lifetime) mints a fresh capability token on reload
	// while inheriting the server-side cookie vault from the expiring capability.
	p.sidecarCapMu.Lock()
	p.sidecarCaps[capTok1].createdAt = time.Now().Add(-(sidecarCapMaxLifetime - 10*time.Minute))
	p.sidecarCapMu.Unlock()

	req2, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/__agy/sidecar/%d/check", front.URL, sharedPort), nil)
	req2.Header.Set("Cookie", auth.CookieName+"="+sessToken)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if string(b2) != "v6-api:v6_vault=preserved99" {
		t.Fatalf("renewed capability did not inherit vaulted cookies: got %q", string(b2))
	}
	p.sidecarCapMu.Lock()
	var rotatedTok string
	for k := range p.sidecarCaps {
		if k != capTok1 {
			rotatedTok = k
		}
	}
	p.sidecarCapMu.Unlock()
	if rotatedTok == "" {
		t.Fatal("expected a fresh capability token to be minted when remaining lifetime < 30m")
	}
}

func TestSidecarShimJSRuntimeRewrites(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}

	capPrefix := "/__agy/sidecar/39715/~s/0123456789abcdef0123456789abcdef"
	shim := sidecarShimJS(39715, capPrefix)
	script := `
const assert = require("assert");
const calls = [];
global.location = {
  protocol: "https:",
  host: "agy.example.com",
  origin: "null",
  href: "https://agy.example.com/__agy/sidecar/39715/?token=abc",
  pathname: "/__agy/sidecar/39715/"
};
global.navigator = {
  sendBeacon: function(url) { calls.push(["beacon", String(url)]); return true; }
};
global.window = {
  origin: "null",
  location: global.location,
  fetch: function(input) {
    const u = (input && typeof input === "object" && "url" in input) ? input.url : String(input);
    calls.push(["fetch", u]);
    return Promise.resolve({});
  },
  WebSocket: function(url, protocols) { this.url = url; this.protocols = protocols; calls.push(["ws", url]); },
  EventSource: function(url) { this.url = url; calls.push(["es", String(url)]); },
  history: {
    pushState: function(s, t, u) {
      if (String(u).startsWith("/")) { throw new Error("SecurityError in opaque origin"); }
      calls.push(["pushState", u]);
    },
    replaceState: function(s, t, u) { calls.push(["replaceState", u]); }
  }
};
// Simulate opaque-origin SecurityError on window.localStorage / sessionStorage getters.
Object.defineProperty(global.window, "localStorage", {
  configurable: true,
  get() { throw new Error("SecurityError: Storage disabled inside sandboxed iframe"); }
});
global.Request = function Request(input, init) {
  this.url = typeof input === "string" ? input : input.url;
  this.init = init;
};
global.window.Request = global.Request;
global.XMLHttpRequest = function() {};
global.XMLHttpRequest.prototype.open = function(m, u) { calls.push(["xhr", String(u)]); };

function makeDOMCtor(props) {
  function C() { this._attrs = {}; this._props = {}; }
  props.forEach(p => {
    Object.defineProperty(C.prototype, p, {
      configurable: true,
      enumerable: true,
      get() { return this._props[p]; },
      set(v) { this._props[p] = v; }
    });
  });
  return C;
}
global.Element = function Element(tag) { this.nodeType = 1; this.tagName = tag; this._attrs = {}; this._html = ""; };
global.Element.prototype.setAttribute = function(k, v) { this._attrs[k] = v; };
Object.defineProperty(global.Element.prototype, "innerHTML", {
  configurable: true,
  get() { return this._html; },
  set(v) { this._html = v; }
});
global.window.Element = global.Element;

global.Node = function Node() {};
Object.defineProperty(global.Node.prototype, "textContent", {
  configurable: true,
  get() { return this._text; },
  set(v) { this._text = v; }
});
global.Node.prototype.appendChild = function(ch) { this._child = ch; return ch; };
global.window.Node = global.Node;

global.window.HTMLImageElement = makeDOMCtor(["src", "srcset"]);
global.window.HTMLVideoElement = makeDOMCtor(["poster"]);
global.window.HTMLButtonElement = makeDOMCtor(["formAction"]);
global.window.HTMLObjectElement = makeDOMCtor(["data"]);
global.window.CSSStyleDeclaration = makeDOMCtor(["cssText", "backgroundImage"]);
global.window.CSSStyleDeclaration.prototype.setProperty = function(k, v) { this._setProp = [k, v]; };
global.window.CSSStyleSheet = function() {};
global.window.CSSStyleSheet.prototype.insertRule = function(r) { this._rule = r; return 0; };
global.window.CSSStyleSheet.prototype.replaceSync = function(t) { this._replaced = t; };

` + shim + `

// In-memory storage polyfill prevents crashes under Origin: null sandbox.
window.localStorage.setItem("theme", "dark");
assert.strictEqual(window.localStorage.getItem("theme"), "dark");

const P = "` + capPrefix + `";
window.fetch("/api/board?x=1");
window.fetch("api/relative?y=2");
window.fetch(new window.Request("/api/from-req"));
window.fetch("http://localhost:39715/state");
window.fetch("https://cdn.example.org/lib.js");
navigator.sendBeacon("/api/telemetry");
new window.WebSocket("/ws");
new window.WebSocket("ws://agy.example.com/ws");
new window.WebSocket("ws://agy.example.com/__agy/sidecar/39715/ws");
new window.WebSocket("ws://127.0.0.1:39715/live?tok=1");
new window.EventSource("/events");
new XMLHttpRequest().open("GET", "/poll");
// Must not throw even when opaque-origin history.pushState throws SecurityError.
window.history.pushState(null, "", "/board/2");

// Dynamic DOM attribute, property, srcset (with data: comma), style, CSSOM, and <style> rewrites.
const el = new global.Element("IMG");
el.setAttribute("srcset", "data:image/svg+xml,<svg viewBox='0,0,4,4'/> 1x, /assets/hi.png 2x");
el.setAttribute("formaction", "/submit");
el.setAttribute("style", "background: url('/img/card.png'); content: 'url(/keep-literal)'");
assert.strictEqual(el._attrs.srcset, "data:image/svg+xml,<svg viewBox='0,0,4,4'/> 1x, " + P + "/assets/hi.png 2x");
assert.strictEqual(el._attrs.formaction, P + "/submit");
assert.strictEqual(el._attrs.style, "background: url('" + P + "/img/card.png'); content: 'url(/keep-literal)'");

const img = new window.HTMLImageElement();
img.srcset = "/a.png, /b.png 2x";
assert.strictEqual(img.srcset, P + "/a.png, " + P + "/b.png 2x");

const vid = new window.HTMLVideoElement();
vid.poster = "/thumb.jpg";
assert.strictEqual(vid.poster, P + "/thumb.jpg");

const btn = new window.HTMLButtonElement();
btn.formAction = "/act";
assert.strictEqual(btn.formAction, P + "/act");

const obj = new window.HTMLObjectElement();
obj.data = "/embed.svg";
assert.strictEqual(obj.data, P + "/embed.svg");

const decl = new window.CSSStyleDeclaration();
decl.setProperty("--hero", "url(/hero.webp)");
decl.backgroundImage = "url(\"/bg2.webp\")";
assert.deepStrictEqual(decl._setProp, ["--hero", "url(" + P + "/hero.webp)"]);
assert.strictEqual(decl.backgroundImage, "url(\"" + P + "/bg2.webp\")");

const sheet = new window.CSSStyleSheet();
sheet.insertRule("@import '/base.css';");
sheet.replaceSync(".x { mask-image: url(/m.svg); }");
assert.strictEqual(sheet._rule, "@import '" + P + "/base.css';");
assert.strictEqual(sheet._replaced, ".x { mask-image: url(" + P + "/m.svg); }");

const styleNode = Object.assign(new global.Node(), { nodeType: 1, tagName: "STYLE" });
styleNode.textContent = ".a{background:url(/s1.png)}";
assert.strictEqual(styleNode.textContent, ".a{background:url(" + P + "/s1.png)}");
const txt = { nodeType: 3, nodeValue: ".b{background:url(/s2.png)}" };
styleNode.appendChild(txt);
assert.strictEqual(txt.nodeValue, ".b{background:url(" + P + "/s2.png)}");

assert.deepStrictEqual(calls, [
  ["fetch", P + "/api/board?x=1"],
  ["fetch", "https://agy.example.com" + P + "/api/relative?y=2"],
  ["fetch", P + "/api/from-req"],
  ["fetch", "https://agy.example.com" + P + "/state"],
  ["fetch", "https://cdn.example.org/lib.js"],
  ["beacon", P + "/api/telemetry"],
  ["ws", "wss://agy.example.com" + P + "/ws"],
  ["ws", "wss://agy.example.com" + P + "/ws"],
  ["ws", "wss://agy.example.com" + P + "/ws"],
  ["ws", "wss://agy.example.com" + P + "/live?tok=1"],
  ["es", P + "/events"],
  ["xhr", P + "/poll"]
]);
`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("shim runtime test failed: %v\n%s", err, out)
	}
}

func serverPort(t *testing.T, rawURL string) int {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func computeWebSocketAcceptKey(challenge string) string {
	const magic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	sum := sha1.Sum([]byte(challenge + magic))
	return base64.StdEncoding.EncodeToString(sum[:])
}
