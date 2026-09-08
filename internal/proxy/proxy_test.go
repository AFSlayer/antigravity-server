package proxy

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/AFSlayer/antigravity-server/internal/patches"
)

// These tests cover the proxy plumbing — reading, rewriting and re-framing
// upstream responses — not the patch anchors themselves, which belong to the
// patches package. The stub bundle therefore carries only the one anchor needed
// to prove a rewrite happened.
const stubBundle = "var a=1;" +
	"get baseUrl(){return`https://127.0.0.1:${this.port}`}" +
	"var b=2;"

const indexHTML = `<!doctype html><html><head><title>Jetski Web</title>` +
	`<meta name="viewport" content="width=device-width, initial-scale=1.0, viewport-fit=cover, maximum-scale=1.0" />` +
	`</head><body><div id="root"></div><script src="/main.js"></script></body></html>`

func upstream(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/main.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write([]byte(stubBundle))
	})
	mux.HandleFunc("/prism_bundle.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write([]byte("window.Prism={};"))
	})
	mux.HandleFunc("/gzipped.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		_, _ = gz.Write([]byte(indexHTML))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(indexHTML))
	})

	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	return server
}

func upstreamPort(t *testing.T, server *httptest.Server) int {
	t.Helper()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func newTestProxy(t *testing.T, server *httptest.Server) (*httptest.Server, map[patches.Target]patches.Report) {
	t.Helper()

	reports := map[patches.Target]patches.Report{}

	p, err := New(Options{
		TargetPort: upstreamPort(t, server),
		Patch:      patches.Options{MobileUX: true, CacheKey: "k1"},
		OnReport: func(target patches.Target, report patches.Report) {
			reports[target] = report
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	front := httptest.NewServer(p.Handler())
	t.Cleanup(front.Close)
	return front, reports
}

func get(t *testing.T, base, path, accept string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func body(t *testing.T, resp *http.Response) string {
	t.Helper()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func statusOf(report patches.Report, id string) patches.Status {
	for _, r := range report {
		if r.ID == id {
			return r.Status
		}
	}
	return patches.StatusMissing
}

func TestProxyRewritesMainJS(t *testing.T) {
	front, reports := newTestProxy(t, upstream(t))

	resp := get(t, front.URL, "/main.js?agy=k1", "")
	out := body(t, resp)

	if !strings.Contains(out, "window.location.origin") {
		t.Error("main.js was not patched")
	}
	if strings.Contains(out, "get baseUrl(){return`https://127.0.0.1:${this.port}`}") {
		t.Error("original baseUrl getter still present")
	}
	if got := resp.Header.Get("Content-Length"); got != strconv.Itoa(len(out)) {
		t.Errorf("Content-Length %s does not match body length %d", got, len(out))
	}
	if got := statusOf(reports[patches.MainJS], "base-url-origin"); got != patches.StatusApplied {
		t.Errorf("base-url-origin: want applied, got %s", got)
	}
}

func TestProxyInjectsIntoHTML(t *testing.T) {
	front, reports := newTestProxy(t, upstream(t))

	out := body(t, get(t, front.URL, "/", "text/html"))

	for _, want := range []string{"agy-touch-action", "agy-safe-area", "agy-keyboard-detect", "agy-signin-banner", `src="/main.js?agy=k1"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in patched HTML", want)
		}
	}
	if _, reported := reports[patches.HTML]; !reported {
		t.Error("expected an HTML patch report")
	}
}

func TestProxyLeavesOtherContentAlone(t *testing.T) {
	front, _ := newTestProxy(t, upstream(t))

	resp := get(t, front.URL, "/prism_bundle.js", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}
	if strings.Contains(body(t, resp), "agy-touch-action") {
		t.Error("non-HTML asset must not be patched")
	}
}

func TestProxySkipsEncodedBodies(t *testing.T) {
	front, reports := newTestProxy(t, upstream(t))

	resp := get(t, front.URL, "/gzipped.html", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}
	if _, reported := reports[patches.HTML]; reported {
		t.Error("compressed responses must not be patched or reported")
	}
}

func TestProxyCachesKeyedBundleAndNotHTML(t *testing.T) {
	front, _ := newTestProxy(t, upstream(t))

	keyed := get(t, front.URL, "/main.js?agy=k1", "")
	if !strings.Contains(keyed.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("keyed bundle should be cacheable, got %q", keyed.Header.Get("Cache-Control"))
	}

	unkeyed := get(t, front.URL, "/main.js", "")
	if unkeyed.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("unkeyed bundle must not be cached, got %q", unkeyed.Header.Get("Cache-Control"))
	}

	html := get(t, front.URL, "/", "text/html")
	if html.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("HTML must not be cached, got %q", html.Header.Get("Cache-Control"))
	}
}

func TestProxyReportsOncePerTarget(t *testing.T) {
	calls := 0
	p, err := New(Options{
		TargetPort: upstreamPort(t, upstream(t)),
		Patch:      patches.Options{MobileUX: true},
		OnReport:   func(patches.Target, patches.Report) { calls++ },
	})
	if err != nil {
		t.Fatal(err)
	}

	front := httptest.NewServer(p.Handler())
	t.Cleanup(front.Close)

	get(t, front.URL, "/main.js", "")
	get(t, front.URL, "/main.js", "")

	if calls != 1 {
		t.Errorf("want a single report per target, got %d", calls)
	}
}

func TestProxyReportsBadGatewayWhenUpstreamIsGone(t *testing.T) {
	server := upstream(t)
	port := upstreamPort(t, server)
	server.Close()

	p, err := New(Options{TargetPort: port, Patch: patches.Options{}})
	if err != nil {
		t.Fatal(err)
	}

	front := httptest.NewServer(p.Handler())
	t.Cleanup(front.Close)

	resp := get(t, front.URL, "/", "text/html")
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("want 502 when upstream is down, got %d", resp.StatusCode)
	}
}

func TestProxyInjectsTargetCSRFToken(t *testing.T) {
	var receivedToken string
	mux := http.NewServeMux()
	mux.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("x-codeium-csrf-token")
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewTLSServer(mux)
	defer server.Close()

	port := upstreamPort(t, server)
	canonicalToken := "canonical-test-token-42"

	p, err := New(Options{
		TargetPort:      port,
		TargetCSRFToken: canonicalToken,
		Patch:           patches.Options{},
	})
	if err != nil {
		t.Fatal(err)
	}

	front := httptest.NewServer(p.Handler())
	defer front.Close()

	// Case 1: Client sends no CSRF token header -> Proxy should inject TargetCSRFToken
	req1, err := http.NewRequest(http.MethodPost, front.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatal(err)
	}
	resp1.Body.Close()
	if receivedToken != canonicalToken {
		t.Errorf("expected injected token %q, got %q", canonicalToken, receivedToken)
	}

	// Case 2: Client sends a stale CSRF token -> Proxy should overwrite with TargetCSRFToken
	req2, err := http.NewRequest(http.MethodPost, front.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	req2.Header.Set("x-codeium-csrf-token", "stale-browser-token")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if receivedToken != canonicalToken {
		t.Errorf("expected overwritten token %q, got %q", canonicalToken, receivedToken)
	}
}

func TestProxyErrorHandlerGRPCWeb(t *testing.T) {
	server := upstream(t)
	port := upstreamPort(t, server)
	server.Close() // Simulate upstream language server down

	p, err := New(Options{TargetPort: port, Patch: patches.Options{}})
	if err != nil {
		t.Fatal(err)
	}

	front := httptest.NewServer(p.Handler())
	defer front.Close()

	// 1. Request with x-grpc-web header
	req1, err := http.NewRequest(http.MethodPost, front.URL+"/grpc-call", nil)
	if err != nil {
		t.Fatal(err)
	}
	req1.Header.Set("x-grpc-web", "1")

	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatal(err)
	}
	resp1.Body.Close()

	if resp1.StatusCode != http.StatusOK {
		t.Errorf("grpc-web: want 200 OK, got %d", resp1.StatusCode)
	}
	if got := resp1.Header.Get("grpc-status"); got != "14" {
		t.Errorf("grpc-web: want grpc-status 14, got %q", got)
	}
	if got := resp1.Header.Get("grpc-message"); !strings.Contains(got, "restarting") {
		t.Errorf("grpc-web: want restarting message, got %q", got)
	}
	if got := resp1.Header.Get("Content-Type"); got != "application/grpc-web+json" {
		t.Errorf("grpc-web: want application/grpc-web+json, got %q", got)
	}

	// 2. Request with Content-Type: application/grpc-web
	req2, err := http.NewRequest(http.MethodPost, front.URL+"/grpc-call", nil)
	if err != nil {
		t.Fatal(err)
	}
	req2.Header.Set("Content-Type", "application/grpc-web")

	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("grpc-web content-type: want 200 OK, got %d", resp2.StatusCode)
	}
	if got := resp2.Header.Get("grpc-status"); got != "14" {
		t.Errorf("grpc-web content-type: want grpc-status 14, got %q", got)
	}

	// 3. Regular non-gRPC request should still return 502
	req3, err := http.NewRequest(http.MethodGet, front.URL+"/regular", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatal(err)
	}
	resp3.Body.Close()

	if resp3.StatusCode != http.StatusBadGateway {
		t.Errorf("regular request: want 502 Bad Gateway, got %d", resp3.StatusCode)
	}
}
