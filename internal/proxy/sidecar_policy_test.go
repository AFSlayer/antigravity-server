package proxy

import (
	"bufio"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/patches"
)

// sidecarPolicyFixture runs a sidecar behind the proxy and opens it once to
// get the capability prefix, the way the app's frame does.
type sidecarPolicyFixture struct {
	front      *httptest.Server
	port       int
	capPrefix  string
	httpPrefix string
	wsPrefix   string
}

func newSidecarPolicyFixture(t *testing.T, handler http.HandlerFunc) *sidecarPolicyFixture {
	t.Helper()
	sidecarSrv := httptest.NewServer(handler)
	t.Cleanup(sidecarSrv.Close)
	port := serverPort(t, sidecarSrv.URL)

	ls := newSidecarUpstream(t, func() []stubSidecarEntry {
		return []stubSidecarEntry{{BaseURL: fmt.Sprintf("http://127.0.0.1:%d", port), Status: "SIDECAR_STATUS_RUNNING"}}
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

	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/__agy/sidecar/%d/", front.URL, port), nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Referer", "https://mybox.ts.net/c/abc")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	m := regexp.MustCompile(`src="(/__agy/sidecar/\d+/~s/[0-9a-f]{32})/__agy_sidecar_shim\.js"`).FindStringSubmatch(string(body))
	if m == nil {
		t.Fatalf("entry page has no shim (status %d):\n%s", resp.StatusCode, body)
	}
	return &sidecarPolicyFixture{
		front:      front,
		port:       port,
		capPrefix:  m[1],
		httpPrefix: fmt.Sprintf("https://mybox.ts.net/__agy/sidecar/%d/", port),
		wsPrefix:   fmt.Sprintf("wss://mybox.ts.net/__agy/sidecar/%d/", port),
	}
}

func (f *sidecarPolicyFixture) get(t *testing.T, subPath string, header map[string]string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, f.front.URL+f.capPrefix+subPath, nil)
	req.Header.Set("Origin", "null")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(body)
}

func htmlPage(w http.ResponseWriter, csp, body string) {
	if csp != "" {
		w.Header().Set("Content-Security-Policy", csp)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func cspDirective(policy, name string) string {
	for _, d := range strings.Split(policy, ";") {
		d = strings.TrimSpace(d)
		if d == name || strings.HasPrefix(d, name+" ") {
			return d
		}
	}
	return ""
}

// A nonce next to 'unsafe-inline' makes the browser ignore 'unsafe-inline', so
// the page's own inline scripts would stop running. The shim is let through by
// the sidecar's URL prefix instead, and a nonce is used only where one cannot
// change anything else.
func TestSidecarShimKeepsUnsafeInline(t *testing.T) {
	f := newSidecarPolicyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		page := `<html><head></head><body><script>window.inline=1</script></body></html>`
		switch r.URL.Path {
		case "/inline":
			htmlPage(w, "script-src 'unsafe-inline'", page)
		case "/cdn":
			htmlPage(w, "script-src https://cdn.example.com 'unsafe-inline'; style-src 'self'", page)
		case "/dynamic":
			htmlPage(w, "script-src 'strict-dynamic' 'nonce-up1' 'unsafe-inline'", page)
		case "/self":
			htmlPage(w, "script-src 'self' 'unsafe-inline'", page)
		default:
			htmlPage(w, "", page)
		}
	})

	for _, path := range []string{"/inline", "/cdn", "/self"} {
		resp, body := f.get(t, path, nil)
		csp := resp.Header.Get("Content-Security-Policy")
		script := cspDirective(csp, "script-src")
		if strings.Contains(csp, "'nonce-") || strings.Contains(body, "nonce=") {
			t.Errorf("%s: a nonce was added next to 'unsafe-inline':\n%s\n%s", path, csp, body)
		}
		if !strings.Contains(script, "'unsafe-inline'") || !strings.Contains(script, f.httpPrefix) {
			t.Errorf("%s: script-src must keep 'unsafe-inline' and allow the shim by prefix: %q", path, script)
		}
	}

	resp, body := f.get(t, "/cdn", nil)
	if !strings.Contains(cspDirective(resp.Header.Get("Content-Security-Policy"), "script-src"), "https://cdn.example.com") {
		t.Errorf("the page's own host source was lost: %q", resp.Header.Get("Content-Security-Policy"))
	}
	_ = body

	// 'strict-dynamic' ignores host sources, so only a nonce lets the shim in.
	resp, body = f.get(t, "/dynamic", nil)
	nonce := regexp.MustCompile(`<script nonce="([0-9a-f]+)" src="[^"]+__agy_sidecar_shim\.js">`).FindStringSubmatch(body)
	if nonce == nil {
		t.Fatalf("/dynamic: shim has no nonce:\n%s", body)
	}
	if script := cspDirective(resp.Header.Get("Content-Security-Policy"), "script-src"); !strings.Contains(script, "'nonce-"+nonce[1]+"'") || !strings.Contains(script, "'nonce-up1'") {
		t.Errorf("/dynamic: script-src must keep the page's nonce and add the shim's: %q", script)
	}
}

// The frame never gets allow-same-origin or top navigation. A sidecar that
// sandboxes itself more tightly keeps its own restrictions.
func TestSidecarSandboxIntersection(t *testing.T) {
	f := newSidecarPolicyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		page := `<html><head></head><body>ok</body></html>`
		switch r.URL.Path {
		case "/tight":
			htmlPage(w, "default-src 'self'; sandbox allow-forms", page)
		case "/wide":
			htmlPage(w, "default-src 'self'; sandbox allow-scripts allow-same-origin allow-top-navigation allow-popups", page)
		case "/bare":
			htmlPage(w, "sandbox; default-src 'self'", page)
		default:
			htmlPage(w, "default-src 'self'", page)
		}
	})
	cases := map[string]string{
		"/tight": "sandbox allow-forms",
		"/wide":  "sandbox allow-scripts allow-popups",
		"/bare":  "sandbox",
		"/plain": sidecarSandboxDirective,
	}
	for path, want := range cases {
		resp, _ := f.get(t, path, nil)
		csp := resp.Header.Get("Content-Security-Policy")
		if got := cspDirective(csp, "sandbox"); got != want {
			t.Errorf("%s: sandbox = %q, want %q (policy %q)", path, got, want, csp)
		}
		if strings.Contains(csp, "allow-same-origin") || strings.Contains(csp, "allow-top-navigation") {
			t.Errorf("%s: the frame was granted same-origin or top navigation: %q", path, csp)
		}
		sandboxes := 0
		for _, d := range strings.Split(csp, ";") {
			if fields := strings.Fields(d); len(fields) > 0 && fields[0] == "sandbox" {
				sandboxes++
			}
		}
		if sandboxes != 1 {
			t.Errorf("%s: want exactly one sandbox directive: %q", path, csp)
		}
	}
}

// A policy in a <meta> tag is rewritten like a header one, minus the
// directives a meta policy cannot carry.
func TestSidecarMetaCSPRewritten(t *testing.T) {
	f := newSidecarPolicyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		htmlPage(w, "", `<html><head><meta content="default-src 'self'; connect-src 'self'" http-equiv="Content-Security-Policy">`+
			`<meta name="viewport" content="width=device-width"></head><body></body></html>`)
	})
	_, body := f.get(t, "/page", nil)
	m := regexp.MustCompile(`<meta content="([^"]*)" http-equiv="Content-Security-Policy">`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("meta policy missing:\n%s", body)
	}
	policy := html.UnescapeString(m[1])
	if !strings.Contains(cspDirective(policy, "default-src"), f.httpPrefix) || !strings.Contains(cspDirective(policy, "connect-src"), f.wsPrefix) {
		t.Errorf("meta policy did not get the sidecar prefixes: %q", policy)
	}
	if strings.Contains(policy, "sandbox") || strings.Contains(policy, "frame-ancestors") {
		t.Errorf("meta policy got directives it cannot carry: %q", policy)
	}
	if !strings.Contains(body, `<meta name="viewport" content="width=device-width">`) {
		t.Errorf("an unrelated meta tag was changed:\n%s", body)
	}
}

// After <base href>, relative URLs resolve against it, as in a browser. A base
// pointing away from the sidecar leaves later relative URLs alone.
func TestSidecarBaseHref(t *testing.T) {
	var port int
	var portMu sync.Mutex
	f := newSidecarPolicyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		portMu.Lock()
		p := port
		portMu.Unlock()
		switch r.URL.Path {
		case "/app/page":
			htmlPage(w, "", `<html><head><base href="../assets/"><link rel="stylesheet" href="site.css"></head>`+
				`<body><img src="x.png"><a href="/root">r</a><a href=".">dot</a><style>b{background:url(bg.png)}</style></body></html>`)
		case "/app/ext":
			htmlPage(w, "", fmt.Sprintf(`<html><head><base href="https://cdn.example.org/lib/"></head>`+
				`<body><img src="x.png"><img src="/y.png"><img src="http://localhost:%d/z.png"></body></html>`, p))
		case "/app/dotbase/index.html":
			htmlPage(w, "", `<html><head><base href="."></head><body><img src="a.png"></body></html>`)
		default:
			htmlPage(w, "", `<html><head></head><body></body></html>`)
		}
	})
	portMu.Lock()
	port = f.port
	portMu.Unlock()

	_, body := f.get(t, "/app/page", nil)
	for _, want := range []string{
		`<base href="` + f.capPrefix + `/assets/">`,
		`href="` + f.capPrefix + `/assets/site.css"`,
		`src="` + f.capPrefix + `/assets/x.png"`,
		`href="` + f.capPrefix + `/root"`,
		`<a href="` + f.capPrefix + `/assets/">dot</a>`,
		`url(` + f.capPrefix + `/assets/bg.png)`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/app/page: missing %s in\n%s", want, body)
		}
	}

	_, body = f.get(t, "/app/ext", nil)
	for _, want := range []string{
		`<img src="x.png">`,
		`<img src="/y.png">`,
		`<img src="` + f.capPrefix + `/z.png">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/app/ext: missing %s in\n%s", want, body)
		}
	}

	_, body = f.get(t, "/app/dotbase/index.html", nil)
	if !strings.Contains(body, `src="`+f.capPrefix+`/app/dotbase/a.png"`) {
		t.Errorf(`<base href="."> must keep the document's directory:%s`, body)
	}
}

func TestDropProtocolSwitch(t *testing.T) {
	h := http.Header{}
	h.Add("Connection", "keep-alive, Upgrade")
	h.Add("Connection", "X-Hop")
	h.Set("Upgrade", "h2c")
	dropProtocolSwitch(h)
	if h.Get("Upgrade") != "" {
		t.Errorf("Upgrade survived: %q", h.Get("Upgrade"))
	}
	if got := h.Values("Connection"); len(got) != 1 || got[0] != "keep-alive, X-Hop" {
		t.Errorf("Connection = %q, want the other hop-by-hop names kept", got)
	}

	only := http.Header{}
	only.Set("Connection", "upgrade")
	dropProtocolSwitch(only)
	if _, ok := only["Connection"]; ok {
		t.Errorf("an empty Connection header was left: %q", only.Values("Connection"))
	}
}

// rawRequest sends a request as written, since net/http would refuse or
// rewrite some of these headers, and returns the status line.
func rawRequest(t *testing.T, server *httptest.Server, request string) string {
	t.Helper()
	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, request); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(line)
}

// Only a WebSocket upgrade may switch protocols, and only through the paths
// that check its origin and track the connection. Upgrade is a token list, so
// websocket listed beside another protocol still counts as WebSocket.
func TestProtocolSwitchOnlyForWebSocket(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]string{}
	f := newSidecarPolicyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Path] = r.Header.Get("Upgrade") + "|" + r.Header.Get("Connection")
		mu.Unlock()
		if r.Header.Get("Upgrade") != "" {
			w.Header().Set("Upgrade", r.Header.Get("Upgrade"))
			w.Header().Set("Connection", "Upgrade")
			w.WriteHeader(http.StatusSwitchingProtocols)
			return
		}
		htmlPage(w, "", `<html><head></head><body>plain</body></html>`)
	})
	host := strings.TrimPrefix(f.front.URL, "http://")

	// A non-WebSocket switch on the sidecar's HTTP path is served as plain HTTP.
	status := rawRequest(t, f.front, "GET "+f.capPrefix+"/h2c HTTP/1.1\r\nHost: "+host+"\r\nOrigin: null\r\nConnection: Upgrade, HTTP2-Settings\r\nUpgrade: h2c\r\nHTTP2-Settings: AAMAAABkAARAAAAAAAIAAAAA\r\n\r\n")
	if !strings.Contains(status, " 200 ") {
		t.Errorf("h2c upgrade on the sidecar path: %q, want a plain 200", status)
	}
	mu.Lock()
	if got := seen["/h2c"]; strings.Contains(strings.ToLower(got), "h2c") || strings.Contains(strings.ToLower(got), "upgrade") {
		t.Errorf("the sidecar received a protocol switch: %q", got)
	}
	mu.Unlock()

	// websocket among other tokens is a WebSocket upgrade: without a
	// capability it is refused instead of being served as a page.
	status = rawRequest(t, f.front, fmt.Sprintf("GET /__agy/sidecar/%d/ HTTP/1.1\r\nHost: %s\r\nSec-Fetch-Site: same-origin\r\nConnection: Upgrade\r\nUpgrade: websocket, foo\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n", f.port, host))
	if !strings.Contains(status, " 403 ") {
		t.Errorf("multi-token WebSocket upgrade without a capability: %q, want 403", status)
	}

	// With a capability but no Origin it is refused, like any WebSocket upgrade.
	status = rawRequest(t, f.front, "GET "+f.capPrefix+"/ws HTTP/1.1\r\nHost: "+host+"\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nUpgrade: foo\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n")
	if !strings.Contains(status, " 403 ") {
		t.Errorf("WebSocket upgrade without Origin split over two Upgrade headers: %q, want 403", status)
	}
}

// The language server's own paths get the same treatment: no protocol switch
// except a WebSocket upgrade.
func TestProtocolSwitchOnMainProxy(t *testing.T) {
	var mu sync.Mutex
	var gotUpgrade, gotConnection string
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotUpgrade, gotConnection = r.Header.Get("Upgrade"), r.Header.Get("Connection")
		mu.Unlock()
		if r.Header.Get("Upgrade") != "" {
			w.Header().Set("Upgrade", r.Header.Get("Upgrade"))
			w.Header().Set("Connection", "Upgrade")
			w.WriteHeader(http.StatusSwitchingProtocols)
			return
		}
		_, _ = w.Write([]byte("plain"))
	}))
	t.Cleanup(upstream.Close)

	p, err := New(Options{TargetPort: upstreamPort(t, upstream), Patch: patches.Options{CacheKey: "k1"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	front := httptest.NewServer(p.Handler())
	t.Cleanup(front.Close)
	host := strings.TrimPrefix(front.URL, "http://")

	status := rawRequest(t, front, "GET /probe HTTP/1.1\r\nHost: "+host+"\r\nConnection: Upgrade\r\nUpgrade: h2c\r\n\r\n")
	if !strings.Contains(status, " 200 ") {
		t.Errorf("h2c upgrade on the main proxy: %q, want a plain 200", status)
	}
	mu.Lock()
	if gotUpgrade != "" || strings.Contains(strings.ToLower(gotConnection), "upgrade") {
		t.Errorf("the language server received a protocol switch: Upgrade=%q Connection=%q", gotUpgrade, gotConnection)
	}
	mu.Unlock()

	status = rawRequest(t, front, "GET /probe HTTP/1.1\r\nHost: "+host+"\r\nConnection: Upgrade\r\nUpgrade: websocket, foo\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n")
	if !strings.Contains(status, " 403 ") {
		t.Errorf("multi-token WebSocket upgrade without Origin on the main proxy: %q, want 403", status)
	}
}

func TestShimAccessFor(t *testing.T) {
	const httpsPrefix = "https://box.example/__agy/sidecar/4000/"
	const httpPrefix = "http://192.168.0.2:8765/__agy/sidecar/4000/"
	cases := []struct {
		tokens []string
		prefix string
		none   bool
		want   shimAccess
	}{
		{[]string{"'unsafe-inline'"}, httpsPrefix, false, shimByPrefix},
		{[]string{"https://cdn.example.com", "'unsafe-inline'"}, httpsPrefix, false, shimByPrefix},
		{[]string{"'self'", "'unsafe-inline'"}, httpsPrefix, false, shimAllowed},
		{[]string{"*"}, httpsPrefix, false, shimAllowed},
		{[]string{"https:", "'unsafe-inline'"}, httpsPrefix, false, shimAllowed},
		{[]string{"https:", "'unsafe-inline'"}, httpPrefix, false, shimByPrefix},
		{[]string{"'strict-dynamic'", "'nonce-abc'", "https:"}, httpsPrefix, false, shimByNonce},
		{[]string{"'self'", "'sha256-abc='"}, httpsPrefix, false, shimByNonce},
		{[]string{"'none'"}, httpsPrefix, true, shimByNonce},
		{[]string{"'unsafe-inline'"}, "", false, shimAllowed},
		{[]string{"https://cdn.example.com"}, "", false, shimByNonce},
	}
	for _, tc := range cases {
		none, got := shimAccessFor(tc.tokens, tc.prefix)
		if none != tc.none || got != tc.want {
			t.Errorf("shimAccessFor(%q, %q) = %v, %v; want %v, %v", tc.tokens, tc.prefix, none, got, tc.none, tc.want)
		}
	}
}

// The entry URL mints a capability and redirects, so the document lives at a
// URL its opaque-origin frame can request again: a reload, a form without an
// action, or fetch("") all go to the capability URL and are served.
func TestSidecarEntryRedirectsToCapability(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	f := newSidecarPolicyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		htmlPage(w, "", `<html><head></head><body><form method="post"><button>go</button></form></body></html>`)
	})
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		req, _ := http.NewRequest(method, fmt.Sprintf("%s/__agy/sidecar/%d/app/?x=1", f.front.URL, f.port), nil)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		resp, err := noRedirect.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		loc := resp.Header.Get("Location")
		if resp.StatusCode != http.StatusTemporaryRedirect || !strings.HasPrefix(loc, fmt.Sprintf("/__agy/sidecar/%d/~s/", f.port)) || !strings.HasSuffix(loc, "/app/?x=1") {
			t.Fatalf("%s entry = %d to %q, want 307 to the capability URL keeping path and query", method, resp.StatusCode, loc)
		}
	}

	// What a sandboxed document sends when it reloads or posts to itself.
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req, _ := http.NewRequest(method, f.front.URL+f.capPrefix+"/", strings.NewReader("a=1"))
		req.Header.Set("Origin", "null")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		resp, err := noRedirect.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s from the frame to its own capability URL = %d, want 200", method, resp.StatusCode)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(methods) == 0 || methods[len(methods)-1] != "POST /" {
		t.Errorf("the frame's POST did not reach the sidecar: %v", methods)
	}
}

// A relative Location resolves against the upstream request's directory and
// cannot climb out of the capability prefix.
func TestSidecarRelativeLocation(t *testing.T) {
	f := newSidecarPolicyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		// Set Location directly: http.Redirect would make it absolute.
		redirect := func(loc string) {
			w.Header().Set("Location", loc)
			w.WriteHeader(http.StatusFound)
		}
		switch r.URL.Path {
		case "/admin/":
			redirect("login?next=1")
		case "/admin/deep/page":
			redirect("../../../../x")
		case "/admin/abs":
			redirect("/y")
		default:
			htmlPage(w, "", `<html><head></head><body></body></html>`)
		}
	})
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for path, want := range map[string]string{
		"/admin/":          f.capPrefix + "/admin/login?next=1",
		"/admin/deep/page": f.capPrefix + "/x",
		"/admin/abs":       f.capPrefix + "/y",
	} {
		req, _ := http.NewRequest(http.MethodGet, f.front.URL+f.capPrefix+path, nil)
		req.Header.Set("Origin", "null")
		resp, err := noRedirect.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if got := resp.Header.Get("Location"); got != want {
			t.Errorf("%s: Location = %q, want %q", path, got, want)
		}
	}
}

// The idle limit applies to the connection: a sidecar that keeps pushing to a
// page that never sends stays connected, and the connection closes once both
// sides go quiet.
func TestSidecarWebSocketIdleIsPerConnection(t *testing.T) {
	saved := sidecarWSIdleTimeout
	sidecarWSIdleTimeout = 300 * time.Millisecond
	t.Cleanup(func() { sidecarWSIdleTimeout = saved })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	scPort := ln.Addr().(*net.TCPAddr).Port
	const pushes = 12
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				req, err := http.ReadRequest(bufio.NewReader(c))
				if err != nil {
					return
				}
				if req.Header.Get("Upgrade") == "" {
					body := "<html><head></head><body>entry</body></html>"
					_, _ = fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
					return
				}
				_, _ = fmt.Fprintf(c, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", computeWebSocketAcceptKey(req.Header.Get("Sec-WebSocket-Key")))
				for i := 0; i < pushes; i++ {
					if _, err := c.Write([]byte{0x81, 0x01, 'p'}); err != nil {
						return
					}
					time.Sleep(100 * time.Millisecond)
				}
				// Then go quiet without closing.
				time.Sleep(10 * time.Second)
			}(conn)
		}
	}()

	ls := newSidecarUpstream(t, func() []stubSidecarEntry {
		return []stubSidecarEntry{{BaseURL: fmt.Sprintf("http://127.0.0.1:%d", scPort), Status: "SIDECAR_STATUS_RUNNING"}}
	})
	p, err := New(Options{TargetPort: upstreamPort(t, ls), Patch: patches.Options{CacheKey: "k1"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	front := httptest.NewServer(p.Handler())
	t.Cleanup(front.Close)

	entry, err := http.Get(fmt.Sprintf("%s/__agy/sidecar/%d/", front.URL, scPort))
	if err != nil {
		t.Fatal(err)
	}
	entryBody, _ := io.ReadAll(entry.Body)
	entry.Body.Close()
	capPrefix := regexp.MustCompile(fmt.Sprintf(`/__agy/sidecar/%d/~s/[0-9a-f]{32}`, scPort)).FindString(string(entryBody))
	if capPrefix == "" {
		t.Fatalf("no capability in %s", entryBody)
	}

	conn, err := net.Dial("tcp", strings.TrimPrefix(front.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	host := strings.TrimPrefix(front.URL, "http://")
	_, _ = fmt.Fprintf(conn, "GET %s/ws HTTP/1.1\r\nHost: %s\r\nOrigin: null\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n", capPrefix, host)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("handshake: %v %v", resp, err)
	}

	start := time.Now()
	frames := 0
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		b, err := br.ReadByte()
		if err != nil {
			break
		}
		if b == 'p' {
			frames++
		}
	}
	elapsed := time.Since(start)
	if frames != pushes {
		t.Fatalf("received %d of %d pushes; the quiet client direction closed the connection", frames, pushes)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("connection stayed open %v after both sides went quiet, want about the idle limit", elapsed)
	}
}

func TestSidecarWebSocketCloseGraceIsNotExtended(t *testing.T) {
	savedGrace := sidecarWSCloseGrace
	sidecarWSCloseGrace = 300 * time.Millisecond
	t.Cleanup(func() { sidecarWSCloseGrace = savedGrace })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	scPort := ln.Addr().(*net.TCPAddr).Port
	upstreamClosed := make(chan time.Time, 1)
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
				if req.Header.Get("Upgrade") == "" {
					body := "<html><head></head><body>entry</body></html>"
					_, _ = fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
					return
				}
				_, _ = fmt.Fprintf(c, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", computeWebSocketAcceptKey(req.Header.Get("Sec-WebSocket-Key")))
				// The sidecar finishes its half, then waits for the proxy to
				// close the connection.
				_ = c.(*net.TCPConn).CloseWrite()
				_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
				_, _ = io.Copy(io.Discard, br)
				upstreamClosed <- time.Now()
			}(conn)
		}
	}()

	ls := newSidecarUpstream(t, func() []stubSidecarEntry {
		return []stubSidecarEntry{{BaseURL: fmt.Sprintf("http://127.0.0.1:%d", scPort), Status: "SIDECAR_STATUS_RUNNING"}}
	})
	p, err := New(Options{TargetPort: upstreamPort(t, ls), Patch: patches.Options{CacheKey: "k1"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	front := httptest.NewServer(p.Handler())
	t.Cleanup(front.Close)

	entry, err := http.Get(fmt.Sprintf("%s/__agy/sidecar/%d/", front.URL, scPort))
	if err != nil {
		t.Fatal(err)
	}
	entryBody, _ := io.ReadAll(entry.Body)
	entry.Body.Close()
	capPrefix := regexp.MustCompile(fmt.Sprintf(`/__agy/sidecar/%d/~s/[0-9a-f]{32}`, scPort)).FindString(string(entryBody))
	if capPrefix == "" {
		t.Fatalf("no capability in %s", entryBody)
	}

	host := strings.TrimPrefix(front.URL, "http://")
	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = fmt.Fprintf(conn, "GET %s/ws HTTP/1.1\r\nHost: %s\r\nOrigin: null\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n", capPrefix, host)
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("handshake: %v %v", resp, err)
	}

	// The page keeps sending after the sidecar has finished. That must not
	// keep the connection open past the close grace.
	start := time.Now()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
			}
			if _, err := conn.Write([]byte{0x81, 0x01, 'c'}); err != nil {
				return
			}
		}
	}()
	select {
	case at := <-upstreamClosed:
		if d := at.Sub(start); d > 2*time.Second {
			t.Fatalf("connection closed %v after the sidecar finished, want about the close grace", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a page that keeps sending held the connection open past the close grace")
	}
}
