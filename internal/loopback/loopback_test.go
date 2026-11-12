package loopback

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/auth"
	"github.com/AFSlayer/antigravity-server/internal/lsproc"
)

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

func postJSON(t *testing.T, baseURL, path, origin string, payload any) *http.Response {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestLoopbackFetchProxiesDevServerAndEnforcesIsolation(t *testing.T) {
	var gotCookie atomic.Value
	var gotOrigin atomic.Value
	var gotCSRF atomic.Value
	var gotBody atomic.Value

	devSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie.Store(r.Header.Get("Cookie"))
		gotOrigin.Store(r.Header.Get("Origin"))
		gotCSRF.Store(r.Header.Get("x-codeium-csrf-token"))
		b, _ := io.ReadAll(r.Body)
		gotBody.Store(string(b))

		switch r.URL.Path {
		case "/redirect-out":
			w.Header().Set("Location", "https://evil.example/steal")
			w.WriteHeader(http.StatusFound)
		case "/gzipped":
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			_, _ = gz.Write([]byte("<h1>dev gzip ok</h1>"))
			_ = gz.Close()
		default:
			w.Header().Add("Set-Cookie", "agy_session=spoof; Path=/")
			w.Header().Add("Set-Cookie", "dev_sid=abc; Path=/")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprintf(w, "hello %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(devSrv.Close)
	devPort := serverPort(t, devSrv.URL)

	reservedControlPort := 54321
	reservedLSPort := 54322

	// closedPort is set below: a port the language server's tree owned whose
	// server has since gone away.
	var closedPort int
	h := New(Options{
		IsReservedPort: func(_ context.Context, port int) bool {
			return port == reservedControlPort || port == reservedLSPort
		},
		ResolveFetchTarget: func(_ context.Context, port int) (string, bool) {
			if port == devPort || (closedPort != 0 && port == closedPort) {
				return "127.0.0.1", true
			}
			return "", false
		},
	})
	t.Cleanup(h.Close)

	mux := http.NewServeMux()
	h.Register(mux)
	front := httptest.NewServer(mux)
	t.Cleanup(front.Close)

	// 1. Valid POST to dev server strips agy_session & internal CSRF, drops non-loopback Origin (no forging), and decodes body.
	resp := postJSON(t, front.URL, FetchAPIPath, front.URL, fetchRequest{
		Port:   devPort,
		Method: "POST",
		Path:   "/submit?q=1",
		Headers: []headerEntry{
			{Name: "Cookie", Value: auth.CookieName + "=secret_master; dev_sid=abc"},
			{Name: "Origin", Value: "https://remote.example.com"},
			{Name: "x-codeium-csrf-token", Value: "leak-attempt"},
			{Name: "X-Custom", Value: "ok"},
		},
		BodyBase64: base64.StdEncoding.EncodeToString([]byte("payload=42")),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var parsed fetchResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.NoListener || parsed.StatusCode != http.StatusOK {
		t.Fatalf("unexpected fetchResponse: %+v", parsed)
	}
	decodedBody, err := base64.StdEncoding.DecodeString(parsed.BodyBase64)
	if err != nil {
		t.Fatal(err)
	}
	if string(decodedBody) != "hello POST /submit?q=1" {
		t.Fatalf("decoded body = %q", string(decodedBody))
	}
	if got, _ := gotCookie.Load().(string); strings.Contains(got, "secret_master") || got != "dev_sid=abc" {
		t.Fatalf("upstream Cookie = %q, want dev_sid=abc", got)
	}
	if got, _ := gotOrigin.Load().(string); got != "" {
		t.Fatalf("non-loopback Origin must be dropped rather than forged, got %q", got)
	}
	if got, _ := gotCSRF.Load().(string); got != "" {
		t.Fatalf("upstream received x-codeium-csrf-token = %q", got)
	}
	if got, _ := gotBody.Load().(string); got != "payload=42" {
		t.Fatalf("upstream body = %q", got)
	}
	for _, hdr := range parsed.Headers {
		if strings.EqualFold(hdr.Name, "set-cookie") && strings.Contains(hdr.Value, "agy_session") {
			t.Fatalf("response headers leaked agy_session Set-Cookie: %+v", hdr)
		}
	}

	// Explicit matching loopback Origin header from the preview runtime is preserved.
	loopOrigin := fmt.Sprintf("http://localhost:%d", devPort)
	loopOriginResp := postJSON(t, front.URL, FetchAPIPath, front.URL, fetchRequest{
		Port:   devPort,
		Method: "POST",
		Path:   "/submit-loop",
		Headers: []headerEntry{
			{Name: "Origin", Value: loopOrigin},
		},
	})
	loopOriginResp.Body.Close()
	if got, _ := gotOrigin.Load().(string); got != loopOrigin {
		t.Fatalf("matching loopback Origin = %q, want %q", got, loopOrigin)
	}

	// 2. Gzipped response is automatically decompressed so preview HTML rewriter receives plain UTF-8 bytes.
	gzResp := postJSON(t, front.URL, FetchAPIPath, front.URL, fetchRequest{
		Port: devPort,
		Path: "/gzipped",
	})
	defer gzResp.Body.Close()
	var gzParsed fetchResponse
	_ = json.NewDecoder(gzResp.Body).Decode(&gzParsed)
	gzDecoded, _ := base64.StdEncoding.DecodeString(gzParsed.BodyBase64)
	if string(gzDecoded) != "<h1>dev gzip ok</h1>" {
		t.Fatalf("gzipped body = %q, want plain HTML", string(gzDecoded))
	}

	// 3. External redirect is NOT followed by server (returns 302 untouched).
	redirResp := postJSON(t, front.URL, FetchAPIPath, front.URL, fetchRequest{
		Port: devPort,
		Path: "/redirect-out",
	})
	defer redirResp.Body.Close()
	var redirParsed fetchResponse
	_ = json.NewDecoder(redirResp.Body).Decode(&redirParsed)
	if redirParsed.StatusCode != http.StatusFound {
		t.Fatalf("redirect statusCode = %d, want 302", redirParsed.StatusCode)
	}

	// 4. Reserved Antigravity ports and sensitive daemon / debug / agent ports return 403.
	for _, blocked := range []int{
		reservedControlPort, reservedLSPort,
		22, 2375, 2376, 2379, 2380, 3306, 4444, 5037, 5432, 6379, 6443,
		8001, 8500, 9222, 9229, 9515, 10250, 10255, 11211, 11434, 27017,
	} {
		r := postJSON(t, front.URL, FetchAPIPath, front.URL, fetchRequest{Port: blocked, Path: "/"})
		r.Body.Close()
		if r.StatusCode != http.StatusForbidden {
			t.Errorf("port %d: status = %d, want 403", blocked, r.StatusCode)
		}
	}

	// 5. Scheme-relative path, control bytes, and disallowed HTTP methods return 400.
	for _, bad := range []fetchRequest{
		{Port: devPort, Method: "GET", Path: "//evil.example/x"},
		{Port: devPort, Method: "GET", Path: "relative/no-slash"},
		{Port: devPort, Method: "GET", Path: "/bad\r\nheader"},
		{Port: devPort, Method: "CONNECT", Path: "/"},
		{Port: devPort, Method: "TRACE", Path: "/"},
	} {
		r := postJSON(t, front.URL, FetchAPIPath, front.URL, bad)
		r.Body.Close()
		if r.StatusCode != http.StatusBadRequest {
			t.Errorf("bad request %+v: status = %d, want 400", bad, r.StatusCode)
		}
	}

	// 6. A served port with nothing bound returns HTTP 200 with {"noListener": true}.
	closedLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort = closedLn.Addr().(*net.TCPAddr).Port
	_ = closedLn.Close()

	nlResp := postJSON(t, front.URL, FetchAPIPath, front.URL, fetchRequest{Port: closedPort, Path: "/"})
	defer nlResp.Body.Close()
	if nlResp.StatusCode != http.StatusOK {
		t.Fatalf("no-listener HTTP status = %d, want 200", nlResp.StatusCode)
	}
	var nlParsed fetchResponse
	_ = json.NewDecoder(nlResp.Body).Decode(&nlParsed)
	if !nlParsed.NoListener {
		t.Fatalf("expected noListener=true, got %+v", nlParsed)
	}

	// 7. Cross-origin request is rejected with 403.
	crossResp := postJSON(t, front.URL, FetchAPIPath, "https://attacker.example", fetchRequest{Port: devPort, Path: "/"})
	crossResp.Body.Close()
	if crossResp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d, want 403", crossResp.StatusCode)
	}
}

func TestOAuthCallbackRelaysOnlyToAllowedProcessTreePortAndDiscardsBody(t *testing.T) {
	var hitCount atomic.Int32
	var receivedQuery atomic.Value
	var receivedCookie atomic.Value

	oauthListener := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount.Add(1)
		receivedQuery.Store(r.URL.RawQuery)
		receivedCookie.Store(r.Header.Get("Cookie"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("SENSITIVE_UPSTREAM_RESPONSE_BODY_NEVER_RETURNED"))
	}))
	t.Cleanup(oauthListener.Close)
	allowedPort := serverPort(t, oauthListener.URL)

	unownedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("unowned listener must never be contacted by oauth-callback")
	}))
	t.Cleanup(unownedSrv.Close)
	unownedPort := serverPort(t, unownedSrv.URL)

	h := New(Options{
		ResolveOAuthTarget: func(_ context.Context, port int, _ string) (string, bool) {
			if port == allowedPort {
				return "127.0.0.1", true
			}
			return "", false
		},
	})
	t.Cleanup(h.Close)

	mux := http.NewServeMux()
	h.Register(mux)
	front := httptest.NewServer(mux)
	t.Cleanup(front.Close)

	// Disallowed port, external URL, and URL without code= all return delivered: false without dialing.
	for _, rejectedURL := range []string{
		fmt.Sprintf("http://127.0.0.1:%d/callback?code=abc", unownedPort),
		"https://antigravity.google/oauth-callback?code=abc",
		fmt.Sprintf("http://localhost:%d/callback?state=only", allowedPort),
	} {
		r := postJSON(t, front.URL, OAuthCallbackAPIPath, front.URL, oauthCallbackRequest{URL: rejectedURL})
		raw, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var out oauthCallbackResponse
		_ = json.Unmarshal(raw, &out)
		if out.Delivered {
			t.Fatalf("expected delivered=false for %q, got %s", rejectedURL, string(raw))
		}
	}
	if hitCount.Load() != 0 {
		t.Fatalf("oauthListener hitCount = %d before valid call, want 0", hitCount.Load())
	}

	// Allowed port with code= delivers GET without cookies and never echoes response body.
	reqBody, _ := json.Marshal(oauthCallbackRequest{
		URL: fmt.Sprintf("http://localhost:%d/oauth/callback?code=one_time_123&state=st99", allowedPort),
	})
	req, _ := http.NewRequest(http.MethodPost, front.URL+OAuthCallbackAPIPath, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", front.URL)
	req.Header.Set("Cookie", auth.CookieName+"=do_not_forward")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	rawBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if strings.Contains(string(rawBody), "SENSITIVE_UPSTREAM_RESPONSE_BODY") {
		t.Fatalf("oauth-callback leaked upstream response body: %s", string(rawBody))
	}
	var out oauthCallbackResponse
	if err := json.Unmarshal(rawBody, &out); err != nil {
		t.Fatal(err)
	}
	if !out.Delivered || out.StatusCode != http.StatusOK {
		t.Fatalf("unexpected oauthCallbackResponse: %+v", out)
	}
	if got, _ := receivedQuery.Load().(string); got != "code=one_time_123&state=st99" {
		t.Fatalf("listener query = %q", got)
	}
	if got, _ := receivedCookie.Load().(string); got != "" {
		t.Fatalf("listener received Cookie = %q, want empty", got)
	}
}

func TestOAuthCallbackIsolatesLoopbackAddressFamily(t *testing.T) {
	ln6, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	t.Cleanup(func() { _ = ln6.Close() })
	sharedPort := ln6.Addr().(*net.TCPAddr).Port

	ln4, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", sharedPort))
	if err != nil {
		t.Skipf("could not bind parallel IPv4 listener on port %d: %v", sharedPort, err)
	}
	t.Cleanup(func() { _ = ln4.Close() })

	var v4Hits atomic.Int32
	var v6Hits atomic.Int32
	var v6Code atomic.Value

	srv4 := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v4Hits.Add(1)
		w.WriteHeader(http.StatusOK)
	})}
	go func() { _ = srv4.Serve(ln4) }()
	t.Cleanup(func() { _ = srv4.Close() })

	srv6 := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v6Hits.Add(1)
		v6Code.Store(r.URL.Query().Get("code"))
		w.WriteHeader(http.StatusOK)
	})}
	go func() { _ = srv6.Serve(ln6) }()
	t.Cleanup(func() { _ = srv6.Close() })

	// Only the [::1] listener belongs to the language server process tree; 127.0.0.1
	// on the same port belongs to an unrelated local process.
	h := New(Options{
		ResolveOAuthTarget: func(_ context.Context, port int, urlHost string) (string, bool) {
			if port != sharedPort {
				return "", false
			}
			return lsproc.ListenIPv6.ResolveDialHost(urlHost)
		},
	})
	t.Cleanup(h.Close)

	mux := http.NewServeMux()
	h.Register(mux)
	front := httptest.NewServer(mux)
	t.Cleanup(front.Close)

	// 1. Explicit IPv4 callback URL must NOT dial the unrelated 127.0.0.1 listener or fall back to [::1].
	r4 := postJSON(t, front.URL, OAuthCallbackAPIPath, front.URL, oauthCallbackRequest{
		URL: fmt.Sprintf("http://127.0.0.1:%d/cb?code=leak_attempt", sharedPort),
	})
	var out4 oauthCallbackResponse
	_ = json.NewDecoder(r4.Body).Decode(&out4)
	r4.Body.Close()
	if out4.Delivered {
		t.Fatal("expected 127.0.0.1 callback URL to be rejected when tree owns only [::1]")
	}
	if v4Hits.Load() != 0 || v6Hits.Load() != 0 {
		t.Fatalf("unexpected hits after rejected v4 callback: v4=%d v6=%d", v4Hits.Load(), v6Hits.Load())
	}

	// 2. "localhost" callback URL dials ONLY [::1] (the owned family), never touching 127.0.0.1.
	rLocal := postJSON(t, front.URL, OAuthCallbackAPIPath, front.URL, oauthCallbackRequest{
		URL: fmt.Sprintf("http://localhost:%d/cb?code=safe_v6_code", sharedPort),
	})
	var outLocal oauthCallbackResponse
	_ = json.NewDecoder(rLocal.Body).Decode(&outLocal)
	rLocal.Body.Close()
	if !outLocal.Delivered || outLocal.StatusCode != http.StatusOK {
		t.Fatalf("expected localhost callback to deliver to [::1], got %+v", outLocal)
	}
	if v4Hits.Load() != 0 {
		t.Fatalf("unrelated 127.0.0.1 listener was contacted %d times", v4Hits.Load())
	}
	if v6Hits.Load() != 1 || v6Code.Load() != "safe_v6_code" {
		t.Fatalf("expected 1 delivery to [::1] with safe_v6_code, got hits=%d code=%v", v6Hits.Load(), v6Code.Load())
	}
}

// A preview page's scripts choose the port, so the fetch reaches only listeners
// of the language server's process tree. Any other port answers 403 without
// being dialed, whether something listens there or not, so the answer says
// nothing about the host's other ports. A served port whose server has gone
// away reads as "no listener".
func TestLoopbackFetchOnlyServesProcessTreePorts(t *testing.T) {
	var hits atomic.Int32
	var gotOrigin, gotReferer atomic.Value
	devSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOrigin.Store(r.Header.Get("Origin"))
		gotReferer.Store(r.Header.Get("Referer"))
		_, _ = w.Write([]byte("dev"))
	}))
	t.Cleanup(devSrv.Close)
	devPort := serverPort(t, devSrv.URL)

	otherSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("someone else's service"))
	}))
	t.Cleanup(otherSrv.Close)
	otherPort := serverPort(t, otherSrv.URL)

	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := closed.Addr().(*net.TCPAddr).Port
	_ = closed.Close()

	goneServer := httptest.NewServer(http.NotFoundHandler())
	gonePort := serverPort(t, goneServer.URL)
	goneServer.Close()

	h := New(Options{
		ResolveFetchTarget: func(_ context.Context, port int) (string, bool) {
			if port == devPort || port == gonePort {
				return "127.0.0.1", true
			}
			return "", false
		},
	})
	t.Cleanup(h.Close)
	mux := http.NewServeMux()
	h.Register(mux)
	front := httptest.NewServer(mux)
	t.Cleanup(front.Close)

	// A port outside the tree gets the same answer whether or not something
	// listens on it, and is never contacted.
	for _, port := range []int{otherPort, closedPort} {
		r := postJSON(t, front.URL, FetchAPIPath, front.URL, fetchRequest{Port: port, Path: "/"})
		var out fetchResponse
		_ = json.NewDecoder(r.Body).Decode(&out)
		r.Body.Close()
		if r.StatusCode != http.StatusOK || !out.NoListener || out.StatusCode != 0 || out.BodyBase64 != "" {
			t.Fatalf("port %d outside the process tree: status %d %+v, want noListener only", port, r.StatusCode, out)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("a listener outside the process tree was contacted %d times", hits.Load())
	}

	r := postJSON(t, front.URL, FetchAPIPath, front.URL, fetchRequest{Port: gonePort, Path: "/"})
	var out fetchResponse
	_ = json.NewDecoder(r.Body).Decode(&out)
	r.Body.Close()
	if r.StatusCode != http.StatusOK || !out.NoListener {
		t.Fatalf("a served port whose server is gone: status %d %+v, want noListener", r.StatusCode, out)
	}

	// Origin and Referer pass only when they name the target port itself.
	for _, tc := range []struct {
		origin, referer string
		forwarded       bool
	}{
		{fmt.Sprintf("http://localhost:%d", devPort), fmt.Sprintf("http://localhost:%d/page", devPort), true},
		{fmt.Sprintf("http://localhost:%d", otherPort), fmt.Sprintf("http://127.0.0.1:%d/page", otherPort), false},
		{"http://localhost", "http://localhost/page", false},
	} {
		gotOrigin.Store("")
		gotReferer.Store("")
		r = postJSON(t, front.URL, FetchAPIPath, front.URL, fetchRequest{
			Port: devPort,
			Path: "/",
			Headers: []headerEntry{
				{Name: "Origin", Value: tc.origin},
				{Name: "Referer", Value: tc.referer},
			},
		})
		r.Body.Close()
		o, _ := gotOrigin.Load().(string)
		ref, _ := gotReferer.Load().(string)
		if tc.forwarded && (o != tc.origin || ref != tc.referer) {
			t.Errorf("same-port Origin/Referer dropped: got %q %q", o, ref)
		}
		if !tc.forwarded && (o != "" || ref != "") {
			t.Errorf("Origin %q / Referer %q from another port were forwarded as %q %q", tc.origin, tc.referer, o, ref)
		}
	}
}

// Without a resolver nothing is fetched or delivered.
func TestLoopbackFailsClosedWithoutResolvers(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	t.Cleanup(srv.Close)
	port := serverPort(t, srv.URL)

	h := New(Options{})
	t.Cleanup(h.Close)
	mux := http.NewServeMux()
	h.Register(mux)
	front := httptest.NewServer(mux)
	t.Cleanup(front.Close)

	r := postJSON(t, front.URL, FetchAPIPath, front.URL, fetchRequest{Port: port, Path: "/"})
	var fetched fetchResponse
	_ = json.NewDecoder(r.Body).Decode(&fetched)
	r.Body.Close()
	if r.StatusCode != http.StatusOK || !fetched.NoListener {
		t.Errorf("fetch without a resolver: status %d %+v, want noListener", r.StatusCode, fetched)
	}
	r = postJSON(t, front.URL, OAuthCallbackAPIPath, front.URL, oauthCallbackRequest{URL: fmt.Sprintf("http://localhost:%d/cb?code=x", port)})
	var out oauthCallbackResponse
	_ = json.NewDecoder(r.Body).Decode(&out)
	r.Body.Close()
	if out.Delivered || hits.Load() != 0 {
		t.Errorf("oauth-callback without a resolver delivered=%v hits=%d", out.Delivered, hits.Load())
	}
}

// The body is base64-encoded into the response as it is written. Bodies of
// every padding length, and an empty one, must come back intact.
func TestLoopbackFetchStreamsBody(t *testing.T) {
	var size atomic.Int32
	devSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "yes")
		w.WriteHeader(http.StatusCreated)
		body := make([]byte, size.Load())
		for i := range body {
			body[i] = byte(i*7 + i/251)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(devSrv.Close)
	devPort := serverPort(t, devSrv.URL)

	h := New(Options{
		ResolveFetchTarget: func(_ context.Context, port int) (string, bool) {
			return "127.0.0.1", port == devPort
		},
	})
	t.Cleanup(h.Close)
	mux := http.NewServeMux()
	h.Register(mux)
	front := httptest.NewServer(mux)
	t.Cleanup(front.Close)

	for _, n := range []int{0, 1, 2, 3, 70001} {
		size.Store(int32(n))
		r := postJSON(t, front.URL, FetchAPIPath, front.URL, fetchRequest{Port: devPort, Path: "/"})
		var out fetchResponse
		err := json.NewDecoder(r.Body).Decode(&out)
		r.Body.Close()
		if err != nil {
			t.Fatalf("%d-byte body: response is not JSON: %v", n, err)
		}
		got, err := base64.StdEncoding.DecodeString(out.BodyBase64)
		if err != nil || len(got) != n {
			t.Fatalf("%d-byte body: decoded %d bytes, err %v", n, len(got), err)
		}
		for i := range got {
			if got[i] != byte(i*7+i/251) {
				t.Fatalf("%d-byte body differs at byte %d", n, i)
			}
		}
		if out.StatusCode != http.StatusCreated {
			t.Fatalf("%d-byte body: status %d, want 201", n, out.StatusCode)
		}
		var sawHeader bool
		for _, hdr := range out.Headers {
			if strings.EqualFold(hdr.Name, "X-Test") && hdr.Value == "yes" {
				sawHeader = true
			}
		}
		if !sawHeader {
			t.Fatalf("%d-byte body: headers %v lost X-Test", n, out.Headers)
		}
	}
}

// A fetch that waits too long for one of the slots gets an error status, not
// an empty 200 the page would fail to parse.
func TestLoopbackFetchFullQueue(t *testing.T) {
	saved := fetchTimeout
	fetchTimeout = 100 * time.Millisecond
	t.Cleanup(func() { fetchTimeout = saved })

	h := New(Options{ResolveFetchTarget: func(context.Context, int) (string, bool) { return "127.0.0.1", true }})
	t.Cleanup(h.Close)
	for i := 0; i < maxConcurrentFetches; i++ {
		h.sem <- struct{}{}
	}
	mux := http.NewServeMux()
	h.Register(mux)
	front := httptest.NewServer(mux)
	t.Cleanup(front.Close)

	r := postJSON(t, front.URL, FetchAPIPath, front.URL, fetchRequest{Port: 5173, Path: "/"})
	r.Body.Close()
	if r.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %d with every slot taken, want 503", r.StatusCode)
	}
}

// The JSON is gzipped for a browser that accepts it, and plain otherwise.
func TestLoopbackFetchGzip(t *testing.T) {
	page := strings.Repeat("<p>module text</p>\n", 4000)
	devSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, page)
	}))
	t.Cleanup(devSrv.Close)
	devPort := serverPort(t, devSrv.URL)
	h := New(Options{ResolveFetchTarget: func(_ context.Context, port int) (string, bool) { return "127.0.0.1", port == devPort }})
	t.Cleanup(h.Close)
	mux := http.NewServeMux()
	h.Register(mux)
	front := httptest.NewServer(mux)
	t.Cleanup(front.Close)

	raw := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	for _, acceptEncoding := range []string{"gzip, deflate, br", "identity"} {
		body, _ := json.Marshal(fetchRequest{Port: devPort, Path: "/"})
		req, _ := http.NewRequest(http.MethodPost, front.URL+FetchAPIPath, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", front.URL)
		req.Header.Set("Accept-Encoding", acceptEncoding)
		resp, err := raw.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var rd io.Reader = resp.Body
		gzipped := resp.Header.Get("Content-Encoding") == "gzip"
		if gzipped != (acceptEncoding != "identity") {
			t.Fatalf("Accept-Encoding %q: Content-Encoding %q", acceptEncoding, resp.Header.Get("Content-Encoding"))
		}
		if gzipped {
			zr, err := gzip.NewReader(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			rd = zr
		}
		var out fetchResponse
		err = json.NewDecoder(rd).Decode(&out)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("Accept-Encoding %q: %v", acceptEncoding, err)
		}
		got, _ := base64.StdEncoding.DecodeString(out.BodyBase64)
		if string(got) != page {
			t.Fatalf("Accept-Encoding %q: body changed", acceptEncoding)
		}
	}
}
