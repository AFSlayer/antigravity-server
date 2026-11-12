// Package loopback provides authenticated same-origin endpoints for previewing
// user-started localhost development servers and relaying loopback OAuth
// redirects back to local Antigravity / MCP callback listeners.
package loopback

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/auth"
)

const (
	// FetchAPIPath backs remote localhost preview fetches when the standalone
	// language server leaves FetchSandboxPort unimplemented.
	FetchAPIPath = "/__agy/api/loopback-fetch"
	// OAuthCallbackAPIPath delivers pasted http://localhost:<port>/callback?code=...
	// URLs to ephemeral OAuth listeners owned by language_server or its child MCP servers.
	OAuthCallbackAPIPath = "/__agy/api/oauth-callback"

	maxDecodedBodyBytes  = 16 << 20
	maxFetchRequestBytes = 24 << 20
	maxResponseBodyBytes = 16 << 20
	maxOAuthRequestBytes = 16 << 10
	maxConcurrentFetches = 12

	dialTimeout  = 5 * time.Second
	fetchTimeout = 30 * time.Second
	oauthTimeout = 15 * time.Second
)

// Options configures a Handler.
type Options struct {
	Trusted   []*net.IPNet
	PublicURL string
	// IsReservedPort reports whether port belongs to agy-server, the language
	// server, or an active sidecar and must never be reachable via FetchAPIPath.
	IsReservedPort func(ctx context.Context, port int) bool
	// ResolveFetchTarget verifies that port is an unreserved listener of the
	// language server's process tree, such as a dev server the agent started,
	// and returns the loopback IP literal ("127.0.0.1" or "::1") it listens on.
	// FetchAPIPath serves only such ports: a preview page's scripts can ask for
	// any port, and must not reach other services on the host. Any other port
	// is reported as having no listener without being dialed. Without a
	// resolver, no port is served.
	ResolveFetchTarget func(ctx context.Context, port int) (dialHost string, allowed bool)
	// ResolveOAuthTarget verifies that port is an unreserved listener owned by
	// the language server process tree and returns the exact loopback IP literal
	// ("127.0.0.1" or "::1") to dial for urlHost without cross-family fallback.
	ResolveOAuthTarget func(ctx context.Context, port int, urlHost string) (dialHost string, allowed bool)
}

// Handler serves FetchAPIPath and OAuthCallbackAPIPath.
type Handler struct {
	trusted            []*net.IPNet
	publicURL          string
	isReservedPort     func(ctx context.Context, port int) bool
	resolveFetchTarget func(ctx context.Context, port int) (string, bool)
	resolveOAuthTarget func(ctx context.Context, port int, urlHost string) (string, bool)
	transport          *http.Transport
	oauthTransport     *http.Transport
	client             *http.Client
	oauthClient        *http.Client
	sem                chan struct{}

	activeReqs     atomic.Int64
	lastActivityNs atomic.Int64
}

// New constructs a loopback Handler.
func New(opts Options) *Handler {
	noRedirect := func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	tr := &http.Transport{
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       60 * time.Second,
		ResponseHeaderTimeout: fetchTimeout,
		DialContext:           dialLoopbackExact,
	}
	oauthTr := &http.Transport{
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: oauthTimeout,
		DialContext:           dialLoopbackExact,
	}
	h := &Handler{
		trusted:            opts.Trusted,
		publicURL:          opts.PublicURL,
		isReservedPort:     opts.IsReservedPort,
		resolveFetchTarget: opts.ResolveFetchTarget,
		resolveOAuthTarget: opts.ResolveOAuthTarget,
		transport:          tr,
		oauthTransport:     oauthTr,
		client: &http.Client{
			Transport:     tr,
			CheckRedirect: noRedirect,
		},
		oauthClient: &http.Client{
			Transport:     oauthTr,
			CheckRedirect: noRedirect,
		},
		sem: make(chan struct{}, maxConcurrentFetches),
	}
	h.touchActivity()
	return h
}

// WithSameOrigin updates reverse-proxy origin verification settings.
func (h *Handler) WithSameOrigin(trusted []*net.IPNet, publicURL string) *Handler {
	h.trusted = trusted
	h.publicURL = publicURL
	return h
}

// Register mounts the loopback endpoints on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc(FetchAPIPath, h.handleFetch)
	mux.HandleFunc(OAuthCallbackAPIPath, h.handleOAuthCallback)
}

// Close releases idle upstream connections.
func (h *Handler) Close() {
	if h == nil {
		return
	}
	if h.transport != nil {
		h.transport.CloseIdleConnections()
	}
	if h.oauthTransport != nil {
		h.oauthTransport.CloseIdleConnections()
	}
}

func (h *Handler) touchActivity() {
	h.lastActivityNs.Store(time.Now().UnixNano())
}

// IsIdle reports whether no loopback requests are in flight and none have
// completed within threshold.
func (h *Handler) IsIdle(threshold time.Duration) bool {
	if h == nil {
		return true
	}
	if h.activeReqs.Load() != 0 {
		return false
	}
	ns := h.lastActivityNs.Load()
	if ns == 0 {
		return true
	}
	return time.Since(time.Unix(0, ns)) >= threshold
}

type headerEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type fetchRequest struct {
	Port       int           `json:"port"`
	Method     string        `json:"method"`
	Path       string        `json:"path"`
	Headers    []headerEntry `json:"headers"`
	BodyBase64 string        `json:"bodyBase64"`
}

type fetchResponse struct {
	StatusCode int           `json:"statusCode,omitempty"`
	Headers    []headerEntry `json:"headers,omitempty"`
	BodyBase64 string        `json:"bodyBase64,omitempty"`
	NoListener bool          `json:"noListener,omitempty"`
}

type oauthCallbackRequest struct {
	URL string `json:"url"`
}

type oauthCallbackResponse struct {
	Delivered  bool `json:"delivered"`
	StatusCode int  `json:"statusCode,omitempty"`
}

func (h *Handler) checkSameOrigin(r *http.Request) bool {
	ok, unverified := auth.CheckSameOrigin(r, h.trusted, h.publicURL)
	if ok && unverified {
		auth.WarnUnverifiedOriginFallback()
	}
	return ok
}

func (h *Handler) handleFetch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.checkSameOrigin(r) {
		http.Error(w, "cross-origin request forbidden", http.StatusForbidden)
		return
	}

	h.activeReqs.Add(1)
	h.touchActivity()
	defer func() {
		h.activeReqs.Add(-1)
		h.touchActivity()
	}()

	ctx, cancel := context.WithTimeout(r.Context(), fetchTimeout)
	defer cancel()

	// Acquire the concurrency semaphore before decoding up to 24 MiB of JSON and
	// 16 MiB of base64 payload so queued requests cannot pre-allocate memory unboundedly.
	select {
	case h.sem <- struct{}{}:
		defer func() { <-h.sem }()
	case <-ctx.Done():
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxFetchRequestBytes)
	var req fetchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request body exceeds maximum size", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid json request", http.StatusBadRequest)
		return
	}

	if req.Port <= 0 || req.Port > 65535 {
		http.Error(w, "invalid port", http.StatusBadRequest)
		return
	}
	if isBlockedLoopbackPort(req.Port) {
		http.Error(w, "port is blocked", http.StatusForbidden)
		return
	}
	if h.isReservedPort != nil && h.isReservedPort(r.Context(), req.Port) {
		http.Error(w, "port is reserved by Antigravity", http.StatusForbidden)
		return
	}
	dialHost, served := "", false
	if h.resolveFetchTarget != nil {
		dialHost, served = h.resolveFetchTarget(ctx, req.Port)
	}
	if !served || (dialHost != "127.0.0.1" && dialHost != "::1") {
		// Answer as if nothing listened, which lets the preview offer to
		// start the server and retry. Nothing is dialed, so the answer is the
		// same whether or not another service holds the port, and a preview
		// page cannot use it to map the host's open ports.
		writeJSON(w, http.StatusOK, fetchResponse{NoListener: true})
		return
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	if !isAllowedFetchMethod(method) {
		http.Error(w, "unsupported http method", http.StatusBadRequest)
		return
	}

	reqPath := req.Path
	if reqPath == "" {
		reqPath = "/"
	}
	if !strings.HasPrefix(reqPath, "/") || strings.HasPrefix(reqPath, "//") || hasControlByte(reqPath) {
		http.Error(w, "invalid request path", http.StatusBadRequest)
		return
	}
	parsedURI, err := url.ParseRequestURI(reqPath)
	if err != nil || parsedURI.Scheme != "" || parsedURI.Host != "" {
		http.Error(w, "invalid request path", http.StatusBadRequest)
		return
	}

	var reqBody []byte
	if req.BodyBase64 != "" {
		decoded, err := decodeBase64(req.BodyBase64)
		if err != nil {
			http.Error(w, "invalid base64 body", http.StatusBadRequest)
			return
		}
		if len(decoded) > maxDecodedBodyBytes {
			http.Error(w, "decoded body exceeds maximum size", http.StatusRequestEntityTooLarge)
			return
		}
		reqBody = decoded
	}

	var bodyReader io.Reader
	if len(reqBody) > 0 {
		bodyReader = bytes.NewReader(reqBody)
	}

	targetURL := fmt.Sprintf("http://%s%s", net.JoinHostPort(dialHost, strconv.Itoa(req.Port)), reqPath)
	outReq, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader)
	if err != nil {
		http.Error(w, "invalid upstream request", http.StatusBadRequest)
		return
	}
	outReq.Host = fmt.Sprintf("localhost:%d", req.Port)
	outReq.ContentLength = int64(len(reqBody))
	sanitizeFetchHeaders(outReq.Header, req.Headers, req.Port)

	resp, err := h.client.Do(outReq)
	if err != nil {
		if isConnRefused(err) {
			writeJSON(w, http.StatusOK, fetchResponse{NoListener: true})
			return
		}
		if r.Context().Err() != nil {
			return
		}
		http.Error(w, "loopback request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes+1))
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		http.Error(w, "failed reading loopback response", http.StatusBadGateway)
		return
	}
	if int64(len(respBody)) > maxResponseBodyBytes {
		http.Error(w, "loopback response exceeds maximum size", http.StatusBadGateway)
		return
	}

	outHeaders := collectResponseHeaders(resp.Header)
	writeJSON(w, http.StatusOK, fetchResponse{
		StatusCode: resp.StatusCode,
		Headers:    outHeaders,
		BodyBase64: base64.StdEncoding.EncodeToString(respBody),
	})
}

func (h *Handler) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.checkSameOrigin(r) {
		http.Error(w, "cross-origin request forbidden", http.StatusForbidden)
		return
	}

	h.activeReqs.Add(1)
	h.touchActivity()
	defer func() {
		h.activeReqs.Add(-1)
		h.touchActivity()
	}()

	r.Body = http.MaxBytesReader(w, r.Body, maxOAuthRequestBytes)
	var req oauthCallbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request body exceeds maximum size", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid json request", http.StatusBadRequest)
		return
	}

	rawURL := strings.TrimSpace(req.URL)
	if rawURL == "" || hasControlByte(rawURL) {
		http.Error(w, "invalid callback url", http.StatusBadRequest)
		return
	}

	u, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(u.Scheme, "http") || u.User != nil {
		writeJSON(w, http.StatusOK, oauthCallbackResponse{Delivered: false})
		return
	}
	host := strings.ToLower(u.Hostname())
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		writeJSON(w, http.StatusOK, oauthCallbackResponse{Delivered: false})
		return
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 || port > 65535 || isBlockedLoopbackPort(port) {
		writeJSON(w, http.StatusOK, oauthCallbackResponse{Delivered: false})
		return
	}
	if strings.TrimSpace(u.Query().Get("code")) == "" {
		writeJSON(w, http.StatusOK, oauthCallbackResponse{Delivered: false})
		return
	}

	escapedPath := u.EscapedPath()
	if escapedPath == "" {
		escapedPath = "/"
	}
	if strings.HasPrefix(escapedPath, "//") {
		writeJSON(w, http.StatusOK, oauthCallbackResponse{Delivered: false})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), oauthTimeout)
	defer cancel()

	if h.resolveOAuthTarget == nil {
		writeJSON(w, http.StatusOK, oauthCallbackResponse{Delivered: false})
		return
	}
	dialHost, ok := h.resolveOAuthTarget(ctx, port, host)
	if !ok || (dialHost != "127.0.0.1" && dialHost != "::1") {
		writeJSON(w, http.StatusOK, oauthCallbackResponse{Delivered: false})
		return
	}

	targetURL := fmt.Sprintf("http://%s%s", net.JoinHostPort(dialHost, strconv.Itoa(port)), escapedPath)
	if u.RawQuery != "" {
		targetURL += "?" + u.RawQuery
	}

	outReq, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, oauthCallbackResponse{Delivered: false})
		return
	}
	outReq.Host = net.JoinHostPort(host, strconv.Itoa(port))
	outReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.9,*/*;q=0.8")

	resp, err := h.oauthClient.Do(outReq)
	if err != nil {
		writeJSON(w, http.StatusOK, oauthCallbackResponse{Delivered: false})
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()

	writeJSON(w, http.StatusOK, oauthCallbackResponse{
		Delivered:  true,
		StatusCode: resp.StatusCode,
	})
}

func dialLoopbackExact(ctx context.Context, network, addr string) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if host != "127.0.0.1" && host != "::1" {
		return nil, fmt.Errorf("disallowed loopback host %q", host)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return nil, fmt.Errorf("invalid loopback port %q", portStr)
	}
	dialer := &net.Dialer{Timeout: dialTimeout}
	return dialer.DialContext(ctx, network, net.JoinHostPort(host, strconv.Itoa(port)))
}

func isConnRefused(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.Errno(10061)) {
		return true
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "connection refused") || strings.Contains(lower, "actively refused")
}

func isAllowedFetchMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return true
	default:
		return false
	}
}

func hasControlByte(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

func decodeBase64(s string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

func sanitizeFetchHeaders(dst http.Header, entries []headerEntry, port int) {
	for _, e := range entries {
		name := strings.TrimSpace(e.Name)
		if !isValidHeaderName(name) || hasInvalidHeaderValue(e.Value) {
			continue
		}
		lower := strings.ToLower(name)
		if isForbiddenRequestHeader(lower) {
			continue
		}
		switch lower {
		case "cookie":
			if cleaned := stripProtectedCookies(e.Value); cleaned != "" {
				dst.Add("Cookie", cleaned)
			}
		case "origin":
			// Forward Origin only when it is the target's own loopback origin,
			// which is what a page served from that port sends. Any other
			// origin, even another loopback port, would let a preview page
			// pass the Origin-based CSRF checks of a different local service.
			if isLoopbackOriginForPort(e.Value, port) {
				dst.Set("Origin", strings.TrimSpace(e.Value))
			}
		case "referer":
			if isLoopbackOriginForPort(e.Value, port) {
				dst.Set("Referer", strings.TrimSpace(e.Value))
			}
		default:
			dst.Add(name, e.Value)
		}
	}
}

func isForbiddenRequestHeader(lower string) bool {
	switch lower {
	case "host",
		"connection",
		"keep-alive",
		"proxy-authenticate",
		"proxy-authorization",
		"te",
		"trailer",
		"transfer-encoding",
		"upgrade",
		"content-length",
		"accept-encoding",
		"x-forwarded-for",
		"x-forwarded-host",
		"x-forwarded-proto",
		"forwarded",
		"x-real-ip",
		"x-original-origin",
		"x-client-host",
		"x-codeium-csrf-token":
		return true
	default:
		return strings.HasPrefix(lower, "x-forwarded-")
	}
}

func isValidHeaderName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		isAlphaNum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !isAlphaNum && !strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)) {
			return false
		}
	}
	return true
}

func hasInvalidHeaderValue(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\r' || c == '\n' || c == 0 {
			return true
		}
	}
	return false
}

// isLoopbackOriginForPort reports whether raw is a loopback URL on port.
func isLoopbackOriginForPort(raw string, port int) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if h != "localhost" && h != "127.0.0.1" && h != "::1" {
		return false
	}
	urlPort := u.Port()
	if urlPort == "" {
		urlPort = "80"
		if strings.EqualFold(u.Scheme, "https") {
			urlPort = "443"
		}
	}
	return urlPort == strconv.Itoa(port)
}

func stripProtectedCookies(raw string) string {
	var kept []string
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name := part
		if eq := strings.IndexByte(part, '='); eq >= 0 {
			name = strings.TrimSpace(part[:eq])
		}
		lower := strings.ToLower(name)
		if lower == strings.ToLower(auth.CookieName) || strings.Contains(lower, "agy_session") {
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, "; ")
}

func collectResponseHeaders(h http.Header) []headerEntry {
	var out []headerEntry
	for name, vals := range h {
		lower := strings.ToLower(name)
		switch lower {
		case "connection", "keep-alive", "transfer-encoding", "upgrade", "proxy-authenticate", "proxy-authorization", "te", "trailer":
			continue
		}
		for _, v := range vals {
			if lower == "set-cookie" {
				first := v
				if semi := strings.IndexByte(v, ';'); semi >= 0 {
					first = v[:semi]
				}
				if eq := strings.IndexByte(first, '='); eq >= 0 {
					cName := strings.ToLower(strings.TrimSpace(first[:eq]))
					if cName == strings.ToLower(auth.CookieName) || strings.Contains(cName, "agy_session") {
						continue
					}
				}
			}
			out = append(out, headerEntry{Name: name, Value: v})
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// blockedLoopbackPorts combines WHATWG Fetch restricted ports with common local
// non-HTTP daemons and privileged local control APIs (Docker, etcd, ADB,
// Kubernetes, Consul, Chrome DevTools / Node Inspector, WebDriver, Ollama,
// Redis, Memcached, databases) so preview/callback relays cannot reach them.
var blockedLoopbackPorts = map[int]struct{}{
	1: {}, 7: {}, 9: {}, 11: {}, 13: {}, 15: {}, 17: {}, 19: {}, 20: {}, 21: {},
	22: {}, 23: {}, 25: {}, 37: {}, 42: {}, 43: {}, 53: {}, 69: {}, 77: {}, 79: {},
	87: {}, 95: {}, 101: {}, 102: {}, 103: {}, 104: {}, 109: {}, 110: {}, 111: {},
	113: {}, 115: {}, 117: {}, 119: {}, 123: {}, 135: {}, 137: {}, 139: {}, 143: {},
	161: {}, 179: {}, 389: {}, 427: {}, 465: {}, 512: {}, 513: {}, 514: {}, 515: {},
	526: {}, 530: {}, 531: {}, 532: {}, 540: {}, 548: {}, 554: {}, 556: {}, 563: {},
	587: {}, 601: {}, 636: {}, 989: {}, 990: {}, 993: {}, 995: {}, 1719: {}, 1720: {},
	1723: {}, 2049: {}, 2375: {}, 2376: {}, 2379: {}, 2380: {}, 3306: {}, 3659: {},
	4045: {}, 4444: {}, 5037: {}, 5060: {}, 5061: {}, 5432: {}, 6000: {}, 6379: {},
	6443: {}, 6566: {}, 6665: {}, 6666: {}, 6667: {}, 6668: {}, 6669: {}, 6697: {},
	8001: {}, 8500: {}, 9222: {}, 9229: {}, 9515: {}, 10080: {}, 10250: {}, 10255: {},
	11211: {}, 11434: {}, 27017: {},
}

func isBlockedLoopbackPort(port int) bool {
	_, blocked := blockedLoopbackPorts[port]
	return blocked
}
