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

func newSidecarUpstream(t *testing.T, entries func() []stubSidecarEntry) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/exa.language_server_pb.LanguageServerService/GetSidecars", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sidecars": entries(),
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

func TestSidecarPortAllowlistAndReservedPorts(t *testing.T) {
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

	ls := newSidecarUpstream(t, func() []stubSidecarEntry {
		return []stubSidecarEntry{
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
	// yet MUST appear in ReservedSidecarPorts so generic loopback fetch blocks it too.
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

	reserved := p.ReservedSidecarPorts(context.Background())
	if _, ok := reserved[activePort]; !ok {
		t.Errorf("expected gateway port %d in ReservedSidecarPorts", activePort)
	}
	if _, ok := reserved[rawPort]; !ok {
		t.Errorf("expected raw webPort %d in ReservedSidecarPorts", rawPort)
	}
}

func TestSidecarCookieIsolationAndHTMLRewrite(t *testing.T) {
	var gotCookieHeader atomic.Value
	var gotCSRFHeader atomic.Value
	var gotOriginHeader atomic.Value

	sidecarSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookieHeader.Store(r.Header.Get("Cookie"))
		gotCSRFHeader.Store(r.Header.Get("x-codeium-csrf-token"))
		gotOriginHeader.Store(r.Header.Get("Origin"))

		switch r.URL.Path {
		case "/":
			w.Header().Add("Set-Cookie", "agy_session=evil_override; Path=/; HttpOnly")
			w.Header().Add("Set-Cookie", "swarm_auth=tok123; Domain=localhost; Path=/; HttpOnly")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><head><link rel="stylesheet" href="/app.css"><style>body{background:url('/bg.svg')}</style></head>` +
				`<body><script src="/preload.js"></script><script>const tpl = '<a href="/keep-js-literal">x</a>';</script>` +
				`<iframe srcdoc="<a href='/keep-srcdoc'>inner</a>"></iframe>` +
				`<img src="/logo.png" srcset="/logo-1x.png 1x, /logo-2x.png 2x"></body></html>`))
		case "/gzip-page":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			_, _ = gz.Write([]byte(`<html><head></head><body><a href="/inside-gz">gz</a></body></html>`))
			_ = gz.Close()
		case "/preload.js":
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = w.Write([]byte(`window.__preloadLoaded = true;`))
		case "/assets/chunk.js":
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = w.Write([]byte(`export const ok = 1;`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(sidecarSrv.Close)
	scPort := serverPort(t, sidecarSrv.URL)

	ls := newSidecarUpstream(t, func() []stubSidecarEntry {
		return []stubSidecarEntry{{BaseURL: fmt.Sprintf("http://localhost:%d", scPort), Status: "SIDECAR_STATUS_RUNNING"}}
	})
	front, _ := newTestProxyWith(t, ls, func(o *Options) {
		o.TargetCSRFToken = "secret-ls-csrf"
	})

	noRedirectClient := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	// Missing trailing slash redirects with query preserved.
	slashResp, err := noRedirectClient.Get(fmt.Sprintf("%s/__agy/sidecar/%d?token=t1", front.URL, scPort))
	if err != nil {
		t.Fatal(err)
	}
	slashResp.Body.Close()
	if slashResp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("slash redirect status = %d, want 307", slashResp.StatusCode)
	}
	wantLoc := fmt.Sprintf("/__agy/sidecar/%d/?token=t1", scPort)
	if got := slashResp.Header.Get("Location"); got != wantLoc {
		t.Fatalf("Location = %q, want %q", got, wantLoc)
	}

	// Fetch HTML with agy_session + sidecar cookie.
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/__agy/sidecar/%d/?token=t1", front.URL, scPort), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", auth.CookieName+"=top_secret_master_token; swarm_auth=tok123")
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	html := string(bodyBytes)

	if got, _ := gotCookieHeader.Load().(string); strings.Contains(got, "top_secret_master_token") || got != "swarm_auth=tok123" {
		t.Fatalf("upstream received Cookie = %q, want only swarm_auth=tok123", got)
	}
	if got, _ := gotCSRFHeader.Load().(string); got != "" {
		t.Fatalf("upstream received language_server CSRF token %q", got)
	}
	if got, _ := gotOriginHeader.Load().(string); got != fmt.Sprintf("http://localhost:%d", scPort) {
		t.Fatalf("upstream Origin = %q, want http://localhost:%d", got, scPort)
	}

	// Verify Set-Cookie filtering and Path scoping.
	cookies := resp.Header.Values("Set-Cookie")
	if len(cookies) != 1 {
		t.Fatalf("Set-Cookie count = %d (%v), want 1 (agy_session override dropped)", len(cookies), cookies)
	}
	if strings.Contains(strings.ToLower(cookies[0]), "domain=") {
		t.Errorf("Set-Cookie retained Domain attribute: %q", cookies[0])
	}
	wantPathAttr := fmt.Sprintf("Path=/__agy/sidecar/%d/", scPort)
	if !strings.Contains(cookies[0], wantPathAttr) {
		t.Errorf("Set-Cookie %q missing scoped %q", cookies[0], wantPathAttr)
	}
	if got := resp.Header.Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options = %q, want SAMEORIGIN", got)
	}
	if got := resp.Header.Get("Content-Security-Policy"); !strings.Contains(got, "frame-ancestors 'self'") {
		t.Errorf("CSP = %q, want frame-ancestors 'self'", got)
	}

	prefix := fmt.Sprintf("/__agy/sidecar/%d", scPort)
	for _, want := range []string{
		fmt.Sprintf(`<script src="%s/__agy_sidecar_shim.js"></script>`, prefix),
		fmt.Sprintf(`href="%s/app.css"`, prefix),
		fmt.Sprintf(`url('%s/bg.svg')`, prefix),
		fmt.Sprintf(`src="%s/preload.js"`, prefix),
		fmt.Sprintf(`src="%s/logo.png"`, prefix),
		fmt.Sprintf(`srcset="%s/logo-1x.png 1x, %s/logo-2x.png 2x"`, prefix, prefix),
		`const tpl = '<a href="/keep-js-literal">x</a>';`,
		`srcdoc="<a href='/keep-srcdoc'>inner</a>"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rewritten HTML missing %q\nGot:\n%s", want, html)
		}
	}

	// Forced-gzip upstream HTML is transparently decompressed and rewritten.
	gzResp, err := http.Get(fmt.Sprintf("%s/__agy/sidecar/%d/gzip-page", front.URL, scPort))
	if err != nil {
		t.Fatal(err)
	}
	gzBody, _ := io.ReadAll(gzResp.Body)
	gzResp.Body.Close()
	if !strings.Contains(string(gzBody), fmt.Sprintf(`href="%s/inside-gz"`, prefix)) {
		t.Errorf("gzipped HTML was not rewritten: %s", string(gzBody))
	}

	// Same-origin Referer orphan request redirects into the sidecar prefix, while foreign Referer does not.
	orphanReq, _ := http.NewRequest(http.MethodGet, front.URL+"/assets/chunk.js?v=2", nil)
	orphanReq.Header.Set("Referer", fmt.Sprintf("%s/__agy/sidecar/%d/", front.URL, scPort))
	orphanResp, err := noRedirectClient.Do(orphanReq)
	if err != nil {
		t.Fatal(err)
	}
	orphanResp.Body.Close()
	if orphanResp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("orphan redirect status = %d, want 307", orphanResp.StatusCode)
	}
	if got := orphanResp.Header.Get("Location"); got != fmt.Sprintf("%s/assets/chunk.js?v=2", prefix) {
		t.Fatalf("orphan redirect Location = %q", got)
	}

	foreignReq, _ := http.NewRequest(http.MethodGet, front.URL+"/assets/chunk.js", nil)
	foreignReq.Header.Set("Referer", fmt.Sprintf("https://evil.example/__agy/sidecar/%d/", scPort))
	foreignResp, err := noRedirectClient.Do(foreignReq)
	if err != nil {
		t.Fatal(err)
	}
	foreignResp.Body.Close()
	if foreignResp.StatusCode == http.StatusTemporaryRedirect {
		t.Fatalf("foreign Referer must not trigger sidecar orphan redirect")
	}

	// Cross-origin POST to sidecar must be rejected with 403.
	crossPost, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/__agy/sidecar/%d/api", front.URL, scPort), strings.NewReader("{}"))
	crossPost.Header.Set("Origin", "https://evil.example")
	crossResp, err := noRedirectClient.Do(crossPost)
	if err != nil {
		t.Fatal(err)
	}
	crossResp.Body.Close()
	if crossResp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin POST status = %d, want 403", crossResp.StatusCode)
	}
}

func TestSidecarWebSocketPreservesInitialBufferedFrame(t *testing.T) {
	// Raw TCP listener that writes 101 Switching Protocols AND an unmasked server
	// WebSocket text frame ("hello") in a single Write syscall so bufio.Reader
	// buffers the frame alongside the HTTP response headers.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	scPort := ln.Addr().(*net.TCPAddr).Port

	var wsCookie atomic.Value
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		br := bufio.NewReader(conn)
		req, err := http.ReadRequest(br)
		if err != nil {
			return
		}
		wsCookie.Store(req.Header.Get("Cookie"))
		acceptKey := computeWebSocketAcceptKey(req.Header.Get("Sec-WebSocket-Key"))

		// Text frame (0x81), length 5, payload "hello" coalesced with HTTP 101 headers.
		handshakeAndFrame := fmt.Sprintf(
			"HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\nSet-Cookie: agy_session=bad; Path=/\r\nSet-Cookie: sc_ws=1; Path=/\r\n\r\n",
			acceptKey,
		)
		packet := append([]byte(handshakeAndFrame), 0x81, 0x05, 'h', 'e', 'l', 'l', 'o')
		_, _ = conn.Write(packet)

		// Wait briefly for client close/echo.
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 64)
		_, _ = br.Read(buf)
	}()

	ls := newSidecarUpstream(t, func() []stubSidecarEntry {
		return []stubSidecarEntry{{BaseURL: fmt.Sprintf("http://localhost:%d", scPort), Status: "SIDECAR_STATUS_RUNNING"}}
	})
	front, _ := newTestProxy(t, ls)

	wsURL := "ws" + strings.TrimPrefix(front.URL, "http") + fmt.Sprintf("/__agy/sidecar/%d/ws", scPort)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Cross-origin WS upgrade must fail.
	_, badResp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"https://evil.example"}},
	})
	if err == nil {
		t.Fatal("expected cross-origin WebSocket upgrade to fail")
	}
	if badResp != nil && badResp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin WS status = %d, want 403", badResp.StatusCode)
	}

	// Same-origin WS upgrade succeeds, strips agy_session, and delivers the coalesced initial frame.
	c, hsResp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Origin": []string{front.URL},
			"Cookie": []string{auth.CookieName + "=master_secret; other=ok"},
		},
	})
	if err != nil {
		t.Fatalf("websocket.Dial failed: %v", err)
	}
	defer c.CloseNow()

	if got, _ := wsCookie.Load().(string); strings.Contains(got, "master_secret") || got != "other=ok" {
		t.Fatalf("upstream WS Cookie = %q, want other=ok", got)
	}
	if hsResp != nil {
		for _, sc := range hsResp.Header.Values("Set-Cookie") {
			if strings.Contains(sc, "agy_session") {
				t.Errorf("WS 101 leaked agy_session Set-Cookie: %q", sc)
			}
		}
	}

	_, msg, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("failed reading initial buffered WS frame: %v", err)
	}
	if string(msg) != "hello" {
		t.Fatalf("initial WS message = %q, want %q", string(msg), "hello")
	}
}

func TestSidecarShimJSRuntimeRewrites(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}

	shim := sidecarShimJS(39715)
	script := `
const assert = require("assert");
const calls = [];
global.location = {
  protocol: "https:",
  host: "agy.example.com",
  origin: "https://agy.example.com",
  href: "https://agy.example.com/__agy/sidecar/39715/?token=abc",
  pathname: "/__agy/sidecar/39715/"
};
global.window = {
  location: global.location,
  fetch: function(input) { calls.push(["fetch", String(input)]); return Promise.resolve({}); },
  WebSocket: function(url, protocols) { this.url = url; this.protocols = protocols; calls.push(["ws", url]); },
  EventSource: function(url) { this.url = url; calls.push(["es", String(url)]); },
  history: {
    pushState: function(s, t, u) { calls.push(["pushState", u]); },
    replaceState: function(s, t, u) { calls.push(["replaceState", u]); }
  }
};
global.XMLHttpRequest = function() {};
global.XMLHttpRequest.prototype.open = function(m, u) { calls.push(["xhr", String(u)]); };

` + shim + `

window.fetch("/api/board?x=1");
window.fetch("http://localhost:39715/state");
window.fetch("https://cdn.example.org/lib.js");
new window.WebSocket("/ws");
new window.WebSocket("ws://agy.example.com/ws");
new window.WebSocket("ws://agy.example.com/__agy/sidecar/39715/ws");
new window.WebSocket("ws://127.0.0.1:39715/live?tok=1");
new window.EventSource("/events");
new XMLHttpRequest().open("GET", "/poll");
window.history.pushState(null, "", "/board/2");

assert.deepStrictEqual(calls, [
  ["fetch", "/__agy/sidecar/39715/api/board?x=1"],
  ["fetch", "https://agy.example.com/__agy/sidecar/39715/state"],
  ["fetch", "https://cdn.example.org/lib.js"],
  ["ws", "wss://agy.example.com/__agy/sidecar/39715/ws"],
  ["ws", "wss://agy.example.com/__agy/sidecar/39715/ws"],
  ["ws", "wss://agy.example.com/__agy/sidecar/39715/ws"],
  ["ws", "wss://agy.example.com/__agy/sidecar/39715/live?tok=1"],
  ["es", "/__agy/sidecar/39715/events"],
  ["xhr", "/__agy/sidecar/39715/poll"],
  ["pushState", "/__agy/sidecar/39715/board/2"]
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
