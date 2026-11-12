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

	"github.com/AFSlayer/antigravity-server/internal/auth"
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

	h := New(Options{
		IsReservedPort: func(_ context.Context, port int) bool {
			return port == reservedControlPort || port == reservedLSPort
		},
	})
	t.Cleanup(h.Close)

	mux := http.NewServeMux()
	h.Register(mux)
	front := httptest.NewServer(mux)
	t.Cleanup(front.Close)

	// 1. Valid POST to dev server strips agy_session & internal CSRF, rewrites non-loopback Origin, and decodes body.
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
	if got, _ := gotOrigin.Load().(string); got != fmt.Sprintf("http://localhost:%d", devPort) {
		t.Fatalf("upstream Origin = %q", got)
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

	// 4. Reserved Antigravity ports and sensitive daemon ports (SSH 22, Redis 6379, Docker 2375) return 403.
	for _, blocked := range []int{reservedControlPort, reservedLSPort, 22, 2375, 3306, 5432, 6379, 11211, 27017} {
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

	// 6. Unbound local port returns HTTP 200 with {"noListener": true}.
	closedLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := closedLn.Addr().(*net.TCPAddr).Port
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
		AllowOAuthPort: func(_ context.Context, port int) bool {
			return port == allowedPort
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
