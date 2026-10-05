package proxy

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/patches"
	"github.com/coder/websocket"
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
	return newTestProxyWith(t, server, nil)
}

// newTestProxyWith lets a test adjust the proxy options before it is built.
func newTestProxyWith(t *testing.T, server *httptest.Server, adjust func(*Options)) (*httptest.Server, map[patches.Target]patches.Report) {
	t.Helper()

	reports := map[patches.Target]patches.Report{}

	opts := Options{
		TargetPort: upstreamPort(t, server),
		Patch:      patches.Options{MobileUX: true, CacheKey: "k1"},
		OnReport: func(target patches.Target, report patches.Report) {
			reports[target] = report
		},
	}
	if adjust != nil {
		adjust(&opts)
	}

	p, err := New(opts)
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

func getWithHeader(t *testing.T, base, path, key, value string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(key, value)

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

func TestProxyRevalidatesBundleWithETagAndNeverCachesHTML(t *testing.T) {
	front, _ := newTestProxy(t, upstream(t))

	first := get(t, front.URL, "/main.js?agy=k1", "")
	tag := first.Header.Get("ETag")
	if tag == "" {
		t.Fatal("bundle response must carry an ETag")
	}
	if got := first.Header.Get("Cache-Control"); got != "private, no-cache" {
		t.Errorf("bundle must be revalidated and kept out of shared caches, got %q", got)
	}

	// The second request is served from the in-memory copy and must keep the
	// same tag and policy.
	again := get(t, front.URL, "/main.js?agy=k1", "")
	if again.Header.Get("ETag") != tag || again.Header.Get("Cache-Control") != "private, no-cache" {
		t.Errorf("cached bundle changed validators: etag %q, cache-control %q", again.Header.Get("ETag"), again.Header.Get("Cache-Control"))
	}

	for _, inm := range []string{tag, "W/" + tag, `"other", ` + tag} {
		resp := getWithHeader(t, front.URL, "/main.js?agy=k1", "If-None-Match", inm)
		if resp.StatusCode != http.StatusNotModified {
			t.Errorf("If-None-Match %s: want 304, got %d", inm, resp.StatusCode)
		}
		if n := len(body(t, resp)); n != 0 {
			t.Errorf("If-None-Match %s: 304 must not carry a body, got %d bytes", inm, n)
		}
	}

	stale := getWithHeader(t, front.URL, "/main.js?agy=k1", "If-None-Match", `"stale"`)
	if stale.StatusCode != http.StatusOK || len(body(t, stale)) == 0 {
		t.Errorf("stale ETag must get the full patched bundle, got %d", stale.StatusCode)
	}

	html := get(t, front.URL, "/", "text/html")
	if html.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("HTML must not be cached, got %q", html.Header.Get("Cache-Control"))
	}
}

// Browsers send the bundle's ETag back, but upstream never saw those bytes.
// Forwarding the validator could earn a 304 with nothing to patch.
func TestProxyStripsValidatorsBeforeFetchingBundle(t *testing.T) {
	var sawINM, sawIMS string
	mux := http.NewServeMux()
	mux.HandleFunc("/main.js", func(w http.ResponseWriter, r *http.Request) {
		sawINM = r.Header.Get("If-None-Match")
		sawIMS = r.Header.Get("If-Modified-Since")
		w.Header().Set("Content-Type", "text/javascript")
		w.Header().Set("ETag", `"upstream"`)
		w.Header().Set("Last-Modified", "Mon, 01 Jan 2024 00:00:00 GMT")
		_, _ = w.Write([]byte(stubBundle))
	})
	ls := httptest.NewTLSServer(mux)
	t.Cleanup(ls.Close)
	front, _ := newTestProxy(t, ls)

	req, _ := http.NewRequest(http.MethodGet, front.URL+"/main.js?agy=k1", nil)
	req.Header.Set("If-None-Match", `"from-browser"`)
	req.Header.Set("If-Modified-Since", "Mon, 01 Jan 2024 00:00:00 GMT")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if sawINM != "" || sawIMS != "" {
		t.Errorf("validators reached upstream: If-None-Match %q, If-Modified-Since %q", sawINM, sawIMS)
	}
	if resp.Header.Get("ETag") == `"upstream"` || resp.Header.Get("Last-Modified") != "" {
		t.Errorf("upstream validators leaked onto the patched bundle: etag %q, last-modified %q", resp.Header.Get("ETag"), resp.Header.Get("Last-Modified"))
	}
}

// wsUpstream mimics the language server's /connect-websocket endpoint: it
// refuses the upgrade unless Origin is its own loopback address, never
// negotiates compression, and answers each message with an echo. A message
// reading "big" is answered with a large repetitive one instead.
func wsUpstream(t *testing.T) *httptest.Server {
	t.Helper()

	ls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/connect-websocket" || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.NotFound(w, r)
			return
		}
		origin, err := url.Parse(r.Header.Get("Origin"))
		if err != nil || origin.Hostname() != "127.0.0.1" {
			http.Error(w, "bad origin", http.StatusForbidden)
			return
		}
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
			CompressionMode:    websocket.CompressionDisabled,
			Subprotocols:       []string{"agy.v1"},
		})
		if err != nil {
			return
		}
		defer c.CloseNow()
		c.SetReadLimit(1 << 20)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		switch string(data) {
		case "big":
			_ = c.Write(ctx, websocket.MessageText, bigMessage())
			return
		case "headers":
			seen, _ := json.Marshal(map[string]string{
				"csrf":      r.Header.Get("x-codeium-csrf-token"),
				"cookie":    r.Header.Get("Cookie"),
				"origin":    r.Header.Get("X-Original-Origin"),
				"subproto":  c.Subprotocol(),
				"forwarded": r.Header.Get("X-Test-Forward"),
			})
			_ = c.Write(ctx, websocket.MessageText, seen)
			return
		case "close":
			_ = c.Close(websocket.StatusCode(4001), "language server says bye")
			return
		}
		_ = c.Write(ctx, typ, append([]byte("echo:"), data...))
	}))
	t.Cleanup(ls.Close)
	return ls
}

func bigMessage() []byte {
	return []byte(`{"type":"data","payload":"` + strings.Repeat("conversation summary ", 20000) + `"}`)
}

// wsHandshake sends a raw upgrade request so the test controls Host and
// Origin exactly as a browser behind a reverse proxy would send them.
func wsHandshake(t *testing.T, front *httptest.Server, host, origin string, extra ...string) (*http.Response, net.Conn, *bufio.Reader) {
	t.Helper()

	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(front.URL, "http://"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	req := "GET /connect-websocket HTTP/1.1\r\n" +
		"Host: " + host + "\r\n" +
		"Origin: " + origin + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n"
	for _, h := range extra {
		req += h + "\r\n"
	}
	_, _ = io.WriteString(conn, req+"\r\n")

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	return resp, conn, br
}

// dialRelay opens a WebSocket to the proxy the way a browser behind a reverse
// proxy would, with the public name carried in X-Forwarded-Host and Origin.
func dialRelay(t *testing.T, front *httptest.Server, opts *websocket.DialOptions) (*websocket.Conn, *http.Response) {
	t.Helper()

	if opts == nil {
		opts = &websocket.DialOptions{}
	}
	if opts.HTTPHeader == nil {
		opts.HTTPHeader = http.Header{}
	}
	opts.HTTPHeader.Set("Origin", "https://agy.example.com")
	opts.HTTPHeader.Set("X-Forwarded-Host", "agy.example.com")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(front.URL, "http")+"/connect-websocket", opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.CloseNow() })
	c.SetReadLimit(1 << 20)
	return c, resp
}

func readMessage(t *testing.T, c *websocket.Conn) []byte {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// The bundle's WebSocket RPC transport connects to /connect-websocket, and the
// language server rejects the upgrade unless Origin is its own loopback
// address. The proxy must relay messages with Origin rewritten.
func TestProxyRelaysWebSocketMessagesWithLoopbackOrigin(t *testing.T) {
	front, _ := newTestProxy(t, wsUpstream(t))

	c, _ := dialRelay(t, front, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	if got := string(readMessage(t, c)); got != "echo:ping" {
		t.Errorf("relay did not carry the message, got %q", got)
	}
}

// The language server never compresses, so the proxy has to: a browser that
// offers permessage-deflate must get it, and large messages must arrive intact.
func TestProxyCompressesWebSocketTowardBrowser(t *testing.T) {
	front, _ := newTestProxy(t, wsUpstream(t))

	c, resp := dialRelay(t, front, &websocket.DialOptions{CompressionMode: websocket.CompressionNoContextTakeover})
	if ext := resp.Header.Get("Sec-WebSocket-Extensions"); !strings.Contains(ext, "permessage-deflate") {
		t.Fatalf("permessage-deflate was not negotiated, got %q", ext)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, []byte("big")); err != nil {
		t.Fatal(err)
	}
	if got := readMessage(t, c); string(got) != string(bigMessage()) {
		t.Errorf("large message was altered by the relay, got %d bytes want %d", len(got), len(bigMessage()))
	}
}

func TestProxyWebSocketCompressionCanBeDisabled(t *testing.T) {
	front, _ := newTestProxyWith(t, wsUpstream(t), func(o *Options) { o.DisableWSRelay = true })

	c, resp := dialRelay(t, front, &websocket.DialOptions{CompressionMode: websocket.CompressionNoContextTakeover})
	if ext := resp.Header.Get("Sec-WebSocket-Extensions"); ext != "" {
		t.Errorf("pass-through must leave negotiation to the language server, got %q", ext)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	if got := string(readMessage(t, c)); got != "echo:ping" {
		t.Errorf("pass-through did not carry the message, got %q", got)
	}
}

func TestProxyWebSocketRelayReportsUnavailableUpstream(t *testing.T) {
	ls := wsUpstream(t)
	front, _ := newTestProxy(t, ls)
	ls.Close()

	resp, _, _ := wsHandshake(t, front, "agy.example.com", "https://agy.example.com")
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("want 502 when the language server is down, got %d", resp.StatusCode)
	}
}

func TestProxyWebSocketOriginCheck(t *testing.T) {
	front, _ := newTestProxy(t, wsUpstream(t))

	cases := []struct {
		name   string
		host   string
		origin string
		extra  []string
		want   int
	}{
		{"same host", "agy.example.com", "https://agy.example.com", nil, http.StatusSwitchingProtocols},
		{"same host on another port when host omits port", "agy.example.com", "https://agy.example.com:8443", nil, http.StatusSwitchingProtocols},
		{"same host with matching port", "192.168.1.50:8765", "http://192.168.1.50:8765", nil, http.StatusSwitchingProtocols},
		{"same host with mismatching port", "192.168.1.50:8765", "http://192.168.1.50:9000", nil, http.StatusForbidden},
		{"host with port vs origin on default port", "agy.example.com:8443", "https://agy.example.com", nil, http.StatusForbidden},
		{"forwarded host", "127.0.0.1:8765", "https://agy.example.com", []string{"X-Forwarded-Host: agy.example.com"}, http.StatusSwitchingProtocols},
		{"forwarded host with matching port", "127.0.0.1:8765", "http://192.168.1.50:8765", []string{"X-Forwarded-Host: 192.168.1.50:8765"}, http.StatusSwitchingProtocols},
		{"forwarded host with mismatching port", "127.0.0.1:8765", "http://192.168.1.50:9000", []string{"X-Forwarded-Host: 192.168.1.50:8765"}, http.StatusForbidden},
		{"proxy forwards no host", "127.0.0.1:8765", "https://agy.example.com", nil, http.StatusSwitchingProtocols},
		{"sibling subdomain", "agy.example.com", "https://evil.example.com", nil, http.StatusForbidden},
		{"sibling behind proxy", "127.0.0.1:8765", "https://evil.example.com", []string{"X-Forwarded-Host: agy.example.com"}, http.StatusForbidden},
		{"opaque origin", "agy.example.com", "null", nil, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, _, _ := wsHandshake(t, front, tc.host, tc.origin, tc.extra...)
			if resp.StatusCode != tc.want {
				t.Errorf("want %d, got %d", tc.want, resp.StatusCode)
			}
		})
	}
}

func TestProxyWebSocketOriginCheckWithPublicURL(t *testing.T) {
	ls := wsUpstream(t)
	p, err := New(Options{
		TargetPort: upstreamPort(t, ls),
		PublicURL:  "https://custom-public-domain.com",
		Patch:      patches.Options{MobileUX: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(p.Handler())
	t.Cleanup(front.Close)

	// Even if reverse proxy forwarded an internal host, Origin matching PublicURL is accepted
	resp, _, _ := wsHandshake(t, front, "internal-docker:8765", "https://custom-public-domain.com")
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("want 101 Switching Protocols with matching PublicURL, got %d", resp.StatusCode)
	}

	// Mismatched Origin is still rejected
	resp, _, _ = wsHandshake(t, front, "internal-docker:8765", "https://evil.com")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("want 403 Forbidden with mismatched Origin, got %d", resp.StatusCode)
	}
}

func TestProxyHeadDoesNotPolluteMainJSCache(t *testing.T) {
	front, _ := newTestProxy(t, upstream(t))

	// Send a HEAD request first
	req, _ := http.NewRequest(http.MethodHead, front.URL+"/main.js?agy=k1", nil)
	headResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	headResp.Body.Close()

	// Subsequent GET request must return the full, valid patched bundle
	getResp := get(t, front.URL, "/main.js?agy=k1", "")
	data := body(t, getResp)
	if len(data) == 0 {
		t.Fatal("GET returned empty body after HEAD request")
	}
	if !strings.Contains(data, "window.location") {
		t.Error("GET did not return expected patched bundle content")
	}
	// Send a second HEAD request; it should be served from memory fast-path with identical headers and 0-byte body
	reqFast, _ := http.NewRequest(http.MethodHead, front.URL+"/main.js?agy=k1", nil)
	fastResp, err := http.DefaultClient.Do(reqFast)
	if err != nil {
		t.Fatal(err)
	}
	defer fastResp.Body.Close()
	if fastResp.StatusCode != http.StatusOK {
		t.Errorf("fast-path HEAD want 200, got %d", fastResp.StatusCode)
	}
	if fastResp.Header.Get("ETag") != getResp.Header.Get("ETag") {
		t.Errorf("fast-path HEAD ETag mismatch: got %q, want %q", fastResp.Header.Get("ETag"), getResp.Header.Get("ETag"))
	}
	if b, _ := io.ReadAll(fastResp.Body); len(b) != 0 {
		t.Errorf("fast-path HEAD must have 0-byte body, got %d bytes", len(b))
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

func TestProxyIdleTracking(t *testing.T) {
	holdUpstream := make(chan struct{})
	upstreamEntered := make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("/blocking", func(w http.ResponseWriter, r *http.Request) {
		close(upstreamEntered)
		<-holdUpstream
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("done"))
	})
	server := httptest.NewTLSServer(mux)
	defer server.Close()

	p, err := New(Options{
		TargetPort: upstreamPort(t, server),
	})
	if err != nil {
		t.Fatal(err)
	}

	front := httptest.NewServer(p.Handler())
	defer front.Close()

	if got := p.ActiveConnections(); got != 0 {
		t.Errorf("initial active conns: want 0, got %d", got)
	}
	if p.LastActivity().IsZero() {
		t.Errorf("expected non-zero initial last activity")
	}

	// Launch in-flight request
	reqDone := make(chan struct{})
	go func() {
		defer close(reqDone)
		resp, err := http.Get(front.URL + "/blocking")
		if err != nil {
			t.Errorf("http.Get failed: %v", err)
			return
		}
		_ = resp.Body.Close()
	}()

	<-upstreamEntered

	// During in-flight request, activeConns should be 1 and IsIdle should be false even with threshold 0
	if got := p.ActiveConnections(); got != 1 {
		t.Errorf("in-flight active conns: want 1, got %d", got)
	}
	if p.IsIdle(0) {
		t.Errorf("expected IsIdle(0) to be false during in-flight request")
	}

	// Release upstream and wait for completion
	close(holdUpstream)
	<-reqDone

	// Wait briefly for server-side handler defer to finish activeConns decrement
	for i := 0; i < 50; i++ {
		if p.ActiveConnections() == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := p.ActiveConnections(); got != 0 {
		t.Errorf("after completion active conns: want 0, got %d", got)
	}

	// Check idle with long threshold vs short threshold
	if p.IsIdle(10 * time.Second) {
		t.Errorf("expected IsIdle(10s) to be false right after request completion")
	}

	// Sleep small interval to verify threshold expiration
	time.Sleep(30 * time.Millisecond)
	if !p.IsIdle(20 * time.Millisecond) {
		t.Errorf("expected IsIdle(20ms) to be true after 30ms elapsed")
	}
}

// The first visitor after a restart must not pay for fetching and patching the
// bundle: Prewarm does that ahead of time, after which the bundle is served from
// memory without another upstream round trip.
func TestPrewarmServesFirstRequestFromMemory(t *testing.T) {
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/main.js", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write([]byte(stubBundle))
	})
	ls := httptest.NewTLSServer(mux)
	t.Cleanup(ls.Close)

	p, err := New(Options{
		TargetPort: upstreamPort(t, ls),
		Patch:      patches.Options{MobileUX: true, CacheKey: "k1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(p.Handler())
	t.Cleanup(front.Close)

	if err := p.Prewarm(); err != nil {
		t.Fatal(err)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("prewarm should fetch the bundle once, got %d fetches", got)
	}

	resp := get(t, front.URL, "/main.js?agy=k1", "*/*")
	got := body(t, resp)
	if hits.Load() != 1 {
		t.Errorf("the first request after prewarm went back to the language server")
	}
	if !strings.Contains(got, "window.location.origin") {
		t.Errorf("prewarmed bundle was not patched: %q", got)
	}
	if resp.Header.Get("ETag") == "" {
		t.Error("prewarmed bundle is missing its validator")
	}
}

func TestPrewarmReportsLanguageServerFailure(t *testing.T) {
	ls := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(ls.Close)

	p, err := New(Options{TargetPort: upstreamPort(t, ls)})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Prewarm(); err == nil {
		t.Error("a 404 from the language server should surface as an error")
	}
}

func TestProxyRelayKeepsBinaryMessagesBinary(t *testing.T) {
	front, _ := newTestProxy(t, wsUpstream(t))
	c, _ := dialRelay(t, front, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageBinary, []byte{0, 1, 2}); err != nil {
		t.Fatal(err)
	}
	typ, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if typ != websocket.MessageBinary || string(data) != "echo:\x00\x01\x02" {
		t.Errorf("binary message came back as type %v %q", typ, data)
	}
}

func TestProxyRelayForwardsHeadersAndSubprotocol(t *testing.T) {
	front, _ := newTestProxyWith(t, wsUpstream(t), func(o *Options) { o.TargetCSRFToken = "csrf-123" })
	c, resp := dialRelay(t, front, &websocket.DialOptions{
		Subprotocols: []string{"agy.v1", "other"},
		HTTPHeader:   http.Header{"Cookie": {"agy_session=abc"}, "X-Test-Forward": {"yes"}},
	})
	if got := c.Subprotocol(); got != "agy.v1" {
		t.Errorf("sub-protocol was not negotiated end to end, got %q", got)
	}
	_ = resp

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, []byte("headers")); err != nil {
		t.Fatal(err)
	}
	var seen map[string]string
	if err := json.Unmarshal(readMessage(t, c), &seen); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"csrf":      "csrf-123",
		"cookie":    "agy_session=abc",
		"origin":    "https://agy.example.com",
		"subproto":  "agy.v1",
		"forwarded": "yes",
	}
	for k, v := range want {
		if seen[k] != v {
			t.Errorf("language server saw %s=%q, want %q", k, seen[k], v)
		}
	}
}

func TestProxyRelayPassesCloseCodeToBrowser(t *testing.T) {
	front, _ := newTestProxy(t, wsUpstream(t))
	c, _ := dialRelay(t, front, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, []byte("close")); err != nil {
		t.Fatal(err)
	}
	_, _, err := c.Read(ctx)
	if got := websocket.CloseStatus(err); got != 4001 {
		t.Fatalf("browser saw close status %d (%v), want 4001", got, err)
	}
	var ce websocket.CloseError
	if !errors.As(err, &ce) || ce.Reason != "language server says bye" {
		t.Errorf("close reason was not passed on: %v", err)
	}
}

func TestProxyRelayRejectsOversizedBrowserMessage(t *testing.T) {
	front, _ := newTestProxy(t, wsUpstream(t))
	c, _ := dialRelay(t, front, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_ = c.Write(ctx, websocket.MessageBinary, make([]byte, clientReadLimit+1))
	_, _, err := c.Read(ctx)
	if got := websocket.CloseStatus(err); got != websocket.StatusMessageTooBig {
		t.Errorf("oversized message ended with status %d (%v), want %d", got, err, websocket.StatusMessageTooBig)
	}
}

func TestCloseStatusFor(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantCode   websocket.StatusCode
		wantReason string
	}{
		{"application code is passed on", websocket.CloseError{Code: 4001, Reason: "bye"}, 4001, "bye"},
		{"going away is passed on", websocket.CloseError{Code: websocket.StatusGoingAway}, websocket.StatusGoingAway, ""},
		{"no status received becomes a normal closure", websocket.CloseError{Code: websocket.StatusNoStatusRcvd}, websocket.StatusNormalClosure, ""},
		{"abnormal closure cannot be sent", websocket.CloseError{Code: websocket.StatusAbnormalClosure}, websocket.StatusNormalClosure, ""},
		{"end of stream is going away", io.EOF, websocket.StatusGoingAway, ""},
		{"canceled is going away", context.Canceled, websocket.StatusGoingAway, ""},
		{"any other failure is an internal error", errors.New("boom"), websocket.StatusInternalError, "relay error"},
		{"long reasons are cut on a character boundary", websocket.CloseError{Code: 4000, Reason: strings.Repeat("\u00e9", 100)}, 4000, strings.Repeat("\u00e9", 61)},
	}
	for _, tc := range cases {
		code, reason := closeStatusFor(tc.err)
		if code != tc.wantCode || reason != tc.wantReason {
			t.Errorf("%s: got %d %q, want %d %q", tc.name, code, reason, tc.wantCode, tc.wantReason)
		}
	}
}

// recordingUpstream accepts one socket and reports how it ended.
func recordingUpstream(t *testing.T) (*httptest.Server, <-chan error) {
	t.Helper()

	ended := make(chan error, 1)
	ls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, err = c.Read(ctx)
		ended <- err
	}))
	t.Cleanup(ls.Close)
	return ls, ended
}

func TestProxyRelayPassesBrowserCloseCodeToLanguageServer(t *testing.T) {
	ls, ended := recordingUpstream(t)
	front, _ := newTestProxy(t, ls)
	c, _ := dialRelay(t, front, nil)

	if err := c.Close(websocket.StatusCode(4002), "browser says bye"); err != nil {
		t.Logf("close handshake: %v", err)
	}
	select {
	case err := <-ended:
		var ce websocket.CloseError
		if !errors.As(err, &ce) || ce.Code != 4002 || ce.Reason != "browser says bye" {
			t.Errorf("language server saw %v, want close 4002 with the browser's reason", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("language server never learned that the browser closed")
	}
}

// A browser that stops answering pings, such as a phone that went to sleep,
// must not hold the relay and the idle accounting forever.
func TestProxyRelayDropsBrowserThatStopsAnsweringPings(t *testing.T) {
	oldInterval, oldTimeout := pingInterval.Load(), pingTimeout.Load()
	pingInterval.Store(int64(50 * time.Millisecond))
	pingTimeout.Store(int64(100 * time.Millisecond))
	t.Cleanup(func() {
		pingInterval.Store(oldInterval)
		pingTimeout.Store(oldTimeout)
	})

	ls, ended := recordingUpstream(t)
	front, _ := newTestProxy(t, ls)

	// The client never reads, so it never answers the ping.
	dialRelay(t, front, nil)

	select {
	case err := <-ended:
		if got := websocket.CloseStatus(err); got != websocket.StatusInternalError {
			t.Errorf("language server saw %v, want an internal error close", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the relay kept an unresponsive browser alive")
	}
}

// readingUpstream accepts one socket and continuously reads messages until it closes.
func readingUpstream(t *testing.T) (*httptest.Server, <-chan error) {
	t.Helper()

	ended := make(chan error, 1)
	ls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for {
			_, _, err := c.Read(ctx)
			if err != nil {
				ended <- err
				return
			}
		}
	}))
	t.Cleanup(ls.Close)
	return ls, ended
}

// A connection actively transferring messages must not be dropped by keepalive pings
// even if ping timeouts occur while the transfer is in progress.
func TestProxyRelayKeepsActiveConnectionAliveDuringTransfer(t *testing.T) {
	oldInterval, oldTimeout := pingInterval.Load(), pingTimeout.Load()
	pingInterval.Store(int64(40 * time.Millisecond))
	pingTimeout.Store(int64(40 * time.Millisecond))
	t.Cleanup(func() {
		pingInterval.Store(oldInterval)
		pingTimeout.Store(oldTimeout)
	})

	ls, ended := readingUpstream(t)
	front, _ := newTestProxy(t, ls)

	client, _ := dialRelay(t, front, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Keep sending messages so activity is continuously refreshed beyond pingInterval.
	// Transfer lasts 250ms, while pingInterval+pingTimeout is only 80ms.
	// Without activity tracking, this would drop after 80ms.
	for i := 0; i < 10; i++ {
		time.Sleep(25 * time.Millisecond)
		select {
		case err := <-ended:
			t.Fatalf("connection was prematurely dropped during active transfer at iteration %d: %v", i, err)
		default:
		}
		if err := client.Write(ctx, websocket.MessageText, []byte("active-payload")); err != nil {
			t.Fatalf("failed to write message %d: %v", i, err)
		}
	}

	_ = client.Close(websocket.StatusNormalClosure, "done")
	select {
	case err := <-ended:
		if got := websocket.CloseStatus(err); got != websocket.StatusNormalClosure {
			t.Errorf("upstream ended with %v, want StatusNormalClosure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("upstream never saw clean close")
	}
}

// A connection where the language server is streaming messages downstream to the browser
// must not be dropped by keepalive pings even under long streaming durations.
func TestProxyRelayKeepsActiveConnectionAliveDuringDownstreamStreaming(t *testing.T) {
	oldInterval, oldTimeout := pingInterval.Load(), pingTimeout.Load()
	pingInterval.Store(int64(40 * time.Millisecond))
	pingTimeout.Store(int64(40 * time.Millisecond))
	t.Cleanup(func() {
		pingInterval.Store(oldInterval)
		pingTimeout.Store(oldTimeout)
	})

	streamDone := make(chan struct{})
	ended := make(chan error, 1)

	ls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Stream downstream messages every 25ms for 250ms (exceeding 80ms keepalive window)
		for i := 0; i < 10; i++ {
			time.Sleep(25 * time.Millisecond)
			if err := c.Write(ctx, websocket.MessageText, []byte("downstream-chunk")); err != nil {
				ended <- err
				return
			}
		}
		close(streamDone)

		// Wait for client to close cleanly after receiving all chunks
		_, _, err = c.Read(ctx)
		ended <- err
	}))
	t.Cleanup(ls.Close)

	front, _ := newTestProxy(t, ls)
	client, _ := dialRelay(t, front, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	received := 0
	for {
		_, _, err := client.Read(ctx)
		if err != nil {
			t.Fatalf("client read failed after %d messages: %v", received, err)
		}
		received++
		if received == 10 {
			break
		}
	}

	<-streamDone
	_ = client.Close(websocket.StatusNormalClosure, "done")

	select {
	case err := <-ended:
		if got := websocket.CloseStatus(err); got != websocket.StatusNormalClosure {
			t.Errorf("upstream ended with %v, want StatusNormalClosure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("upstream never saw clean close")
	}
}

// wsPair returns the two ends of one WebSocket connection.
func wsPair(t *testing.T) (client, server *websocket.Conn) {
	t.Helper()

	accepted := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		accepted <- c
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.CloseNow() })
	server = <-accepted
	t.Cleanup(func() { server.CloseNow() })
	return client, server
}

func TestPumpTellsWhichSideFailed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// The destination is gone, so the failure is on the write side.
	srcClient, srcServer := wsPair(t)
	dstClient, dstServer := wsPair(t)
	dstServer.CloseNow()
	dstClient.CloseNow()
	if err := srcClient.Write(ctx, websocket.MessageText, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if writeFailed, err := pump(ctx, dstClient, srcServer, nil); !writeFailed || err == nil {
		t.Errorf("a dead destination should be a write failure, got writeFailed=%v err=%v", writeFailed, err)
	}

	// The source closes, so the failure is on the read side.
	srcClient2, srcServer2 := wsPair(t)
	dstClient2, _ := wsPair(t)
	go func() { _ = srcClient2.Close(websocket.StatusNormalClosure, "") }()
	if writeFailed, err := pump(ctx, dstClient2, srcServer2, nil); writeFailed || err == nil {
		t.Errorf("a closing source should be a read failure, got writeFailed=%v err=%v", writeFailed, err)
	}
}

func TestCloseStatusForTreatsDisconnectsAsRoutine(t *testing.T) {
	for _, err := range []error{
		io.ErrUnexpectedEOF,
		net.ErrClosed,
		syscall.ECONNRESET,
		syscall.EPIPE,
		&net.OpError{Op: "read", Err: syscall.ECONNRESET},
	} {
		if code, _ := closeStatusFor(err); code != websocket.StatusGoingAway {
			t.Errorf("%v became %d, want going away so no log line is written", err, code)
		}
	}
}

const transportAnchor = `function Xa(){var a=new URLSearchParams(location.search),b=a.get("useWebSocket");return b!==null?b==="true":a.get("wsTransport")==="2"}`

func upstreamServing(t *testing.T, bundle string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/main.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write([]byte(bundle))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(indexHTML))
	})
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	return server
}

// The early socket only helps when the bundle will use the WebSocket transport.
// If that patch stopped matching, the proxy must stop injecting the early socket.
func TestProxyDropsPrewarmWhenTransportPatchDoesNotMatch(t *testing.T) {
	cases := []struct {
		name   string
		bundle string
		want   bool
	}{
		{"transport patch matched", transportAnchor, true},
		{"transport patch missed", stubBundle, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			front, _ := newTestProxy(t, upstreamServing(t, tc.bundle))
			get(t, front.URL, "/main.js", "*/*")
			page := body(t, get(t, front.URL, "/", "text/html"))
			if got := strings.Contains(page, `id="agy-connection-prewarm"`); got != tc.want {
				t.Errorf("prewarm injected = %v, want %v", got, tc.want)
			}
		})
	}
}
