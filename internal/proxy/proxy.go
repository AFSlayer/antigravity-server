// Package proxy forwards requests to the language server and rewrites the web
// bundle on the way back.
//
// Streaming matters here: agent responses arrive as long-lived chunked bodies, so
// the proxy flushes immediately and never buffers. Compression is only declined
// for the two documents that get patched, leaving every other asset to pass
// through untouched.
package proxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/auth"
	"github.com/AFSlayer/antigravity-server/internal/patches"
)

// ReportFunc receives the patch outcome the first time each document is served.
type ReportFunc func(target patches.Target, report patches.Report)

// Options configures a Proxy.
type Options struct {
	TargetPort      int
	TargetCSRFToken string
	// ForbiddenPorts lists local listener ports (such as the public HTTP port
	// and loopback control panel port) that sidecar routing must never dial.
	ForbiddenPorts []int
	PublicURL      string
	TrustedProxies []*net.IPNet
	// DisableWSRelay passes the RPC WebSocket through untouched instead of
	// terminating and compressing it in the proxy.
	DisableWSRelay bool
	Patch          patches.Options
	OnReport       ReportFunc
}

// Proxy is a patching reverse proxy in front of one language server.
type Proxy struct {
	handler             *httputil.ReverseProxy
	transport           *http.Transport
	sidecarTransport    *http.Transport
	target              *url.URL
	opts                Options
	reported            sync.Map
	activeConns         atomic.Int64
	activeHTTPReqs      atomic.Int64
	activePassthroughWS atomic.Int64
	lastActivityNs      atomic.Int64
	mainJSMu            sync.RWMutex
	cachedMainJS        []byte
	cachedMainTag       string

	htmlMu              sync.RWMutex
	cachedHTML          []byte
	cachedHTMLMissingWS bool

	sidecarMu        sync.Mutex
	sidecarAllowed   map[int]struct{}
	sidecarReserved  map[int]struct{}
	sidecarCachedAt  time.Time
	sidecarLastProbe time.Time

	// wsTransportMissing remembers the last main.js outcome for the HTML patches.
	wsTransportMissing atomic.Bool
}

// New builds a Proxy targeting the language server on opts.TargetPort.
func New(opts Options) (*Proxy, error) {
	target, err := url.Parse(fmt.Sprintf("https://127.0.0.1:%d", opts.TargetPort))
	if err != nil {
		return nil, err
	}

	p := &Proxy{opts: opts, target: target}
	p.touchActivity()

	rp := httputil.NewSingleHostReverseProxy(target)
	rp.FlushInterval = -1
	p.transport = &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		DisableCompression:  true,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}
	rp.Transport = p.transport
	p.sidecarTransport = newSidecarTransport()

	host := target.Host
	base := rp.Director
	rp.Director = func(req *http.Request) {
		req.Header.Del("X-Original-Origin")
		if orig := strings.TrimSpace(req.Header.Get("Origin")); orig != "" && p.sameOriginRequest(req) {
			req.Header.Set("X-Original-Origin", orig)
		}
		if clientHost := p.trustedForwardedHost(req); clientHost != "" {
			req.Header.Set("X-Client-Host", clientHost)
		} else if safeHost := auth.SanitizeHostPort(req.Host); safeHost != "" {
			req.Header.Set("X-Client-Host", safeHost)
		} else {
			req.Header.Del("X-Client-Host")
		}
		base(req)
		req.Host = host
		req.Header.Set("Origin", target.String())

		if p.opts.TargetCSRFToken != "" {
			req.Header.Set("x-codeium-csrf-token", p.opts.TargetCSRFToken)
		}

		if wantsPatch(req) {
			req.Header.Del("Accept-Encoding")
			// The browser's validators describe the patched bytes, not the
			// upstream ones, so always fetch a full body to patch.
			req.Header.Del("If-None-Match")
			req.Header.Del("If-Modified-Since")
		}
	}

	rp.ModifyResponse = p.modifyResponse
	rp.ErrorHandler = errorHandler

	p.handler = rp
	return p, nil
}

// Handler returns the HTTP handler to mount, tracking in-flight streams and activity for idle detection.
func (p *Proxy) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isWS := isWebSocketUpgrade(r)
		isRelayWS := isWS && r.URL.Path == relayPath && !p.opts.DisableWSRelay
		isPassthroughWS := isWS && !isRelayWS
		p.activeConns.Add(1)
		if !isWS {
			p.activeHTTPReqs.Add(1)
		} else if isPassthroughWS {
			p.activePassthroughWS.Add(1)
		}
		p.touchActivity()
		defer func() {
			if !isWS {
				p.activeHTTPReqs.Add(-1)
				p.touchActivity()
			} else if isPassthroughWS {
				p.activePassthroughWS.Add(-1)
				p.touchActivity()
			}
			p.activeConns.Add(-1)
		}()

		if isWS && !p.sameOriginUpgrade(r) {
			http.Error(w, "cross-origin WebSocket upgrade rejected", http.StatusForbidden)
			return
		}
		if !isWS && isStateChangingHTTP(r.Method) && !p.sameOriginRequest(r) {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}

		if isRelayWS {
			p.relayWebSocket(w, r)
			return
		}

		if strings.HasPrefix(r.URL.Path, SidecarPrefix) {
			p.serveSidecar(w, r)
			return
		}
		if p.redirectSidecarOrphanRequest(w, r) {
			return
		}

		// Fast-path: Serve pre-warmed / cached main.js immediately from memory (0ms latency)
		if r.URL.Path == "/main.js" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			if p.redirectStaleBundle(w, r) {
				return
			}
			cached, tag := p.cachedBundle()
			if cached != nil {
				setBundleValidators(w.Header(), tag)
				if etagMatches(r.Header.Get("If-None-Match"), tag) {
					w.WriteHeader(http.StatusNotModified)
					return
				}
				w.Header().Set("Content-Type", "application/javascript")
				w.Header().Set("Content-Length", strconv.Itoa(len(cached)))
				if r.Method == http.MethodGet {
					_, _ = w.Write(cached)
				}
				return
			}
		}

		// Fast-path: Serve cached patched root HTML immediately from memory (0ms latency)
		if r.URL.Path == "/" && (r.Method == http.MethodGet || r.Method == http.MethodHead) && strings.Contains(r.Header.Get("Accept"), "text/html") {
			if html := p.cachedRootHTML(); html != nil {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Cache-Control", "no-store")
				w.Header().Set("Content-Length", strconv.Itoa(len(html)))
				if r.Method == http.MethodGet {
					_, _ = w.Write(html)
				}
				return
			}
		}

		p.handler.ServeHTTP(w, r)
	})
}

func (p *Proxy) touchActivity() {
	p.lastActivityNs.Store(time.Now().UnixNano())
}

// ActiveConnections returns the number of currently active in-flight requests or streams.
func (p *Proxy) ActiveConnections() int64 {
	return p.activeConns.Load()
}

// LastActivity returns the timestamp of the most recent request initiation or completion.
func (p *Proxy) LastActivity() time.Time {
	ns := p.lastActivityNs.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// IsIdle returns true if there are zero active HTTP requests or pass-through
// WebSockets and no request or WebSocket data frame activity has occurred for
// at least the specified threshold duration.
func (p *Proxy) IsIdle(threshold time.Duration) bool {
	if p.activeHTTPReqs.Load() != 0 || p.activePassthroughWS.Load() != 0 {
		return false
	}
	last := p.LastActivity()
	if last.IsZero() {
		return true
	}
	return time.Since(last) >= threshold
}

func isStateChangingHTTP(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

func wantsPatch(req *http.Request) bool {
	if req.URL.Path == "/main.js" {
		return true
	}
	return strings.Contains(req.Header.Get("Accept"), "text/html")
}

// targetFor decides whether a response should be patched. Encoded bodies are
// skipped rather than mangled, and skipping also suppresses a misleading
// "anchor not found" report.
func targetFor(resp *http.Response) (patches.Target, bool) {
	if resp.Request == nil || resp.StatusCode != http.StatusOK {
		return 0, false
	}
	if resp.Header.Get("Content-Encoding") != "" {
		return 0, false
	}

	if resp.Request.URL.Path == "/main.js" {
		return patches.MainJS, true
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		return patches.HTML, true
	}
	return 0, false
}

func (p *Proxy) modifyResponse(resp *http.Response) error {
	// Restore CORS Access-Control-Allow-Origin header if overwritten by internal upstream loopback target
	if resp.Request != nil {
		if acao := resp.Header.Get("Access-Control-Allow-Origin"); acao != "" {
			orig := resp.Request.Header.Get("X-Original-Origin")
			if orig == "" {
				proto := "http"
				if auth.RequestIsSecure(resp.Request, p.opts.TrustedProxies) {
					proto = "https"
				}
				if host := auth.SanitizeHostPort(resp.Request.Header.Get("X-Client-Host")); host != "" {
					orig = proto + "://" + host
				}
			}
			if orig != "" {
				resp.Header.Set("Access-Control-Allow-Origin", orig)
			}
		}
	}

	target, ok := targetFor(resp)
	if !ok {
		if resp.StatusCode == http.StatusOK && resp.Request != nil && (resp.Request.Method == http.MethodGet || resp.Request.Method == http.MethodHead) && isCacheableStaticAsset(resp.Request.URL.Path) {
			resp.Header.Set("Cache-Control", staticAssetCacheControl)
		}
		return nil
	}

	if target == patches.MainJS {
		cached, tag := p.cachedBundle()
		if cached != nil {
			_ = resp.Body.Close()
			resp.Body = io.NopCloser(bytes.NewReader(cached))
			resp.ContentLength = int64(len(cached))
			resp.Header.Set("Content-Length", strconv.Itoa(len(cached)))
			resp.Header.Set("Content-Type", "application/javascript")
			setBundleValidators(resp.Header, tag)
			return nil
		}
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}

	opts := p.opts.Patch
	if target == patches.HTML {
		opts.WebSocketTransportMissing = p.wsTransportMissing.Load()
	}
	patched, report := patches.Apply(target, body, opts)
	if target == patches.MainJS {
		p.wsTransportMissing.Store(transportPatchMissing(report))
	}
	p.report(target, report)

	var tag string
	if target == patches.MainJS {
		tag = bundleETag(patched)
		if resp.Request != nil && resp.Request.Method == http.MethodGet && len(body) > 0 {
			p.mainJSMu.Lock()
			p.cachedMainJS = patched
			p.cachedMainTag = tag
			p.mainJSMu.Unlock()
		}
	} else if target == patches.HTML && resp.Request != nil && resp.Request.Method == http.MethodGet && resp.Request.URL.Path == "/" && len(body) > 0 {
		p.htmlMu.Lock()
		p.cachedHTML = patched
		p.cachedHTMLMissingWS = opts.WebSocketTransportMissing
		p.htmlMu.Unlock()
	}

	resp.Body = io.NopCloser(bytes.NewReader(patched))
	resp.ContentLength = int64(len(patched))
	resp.Header.Set("Content-Length", strconv.Itoa(len(patched)))

	if target == patches.HTML {
		resp.Header.Set("Cache-Control", "no-store")
		resp.Header.Del("ETag")
		resp.Header.Del("Last-Modified")
	} else {
		setBundleValidators(resp.Header, tag)
	}

	return nil
}

// prewarmTimeout bounds the startup fetch so a language server that accepts the
// connection but never answers cannot hold the goroutine forever.
const prewarmTimeout = 30 * time.Second

// Prewarm fetches and patches the bundle once so the first browser to ask is
// served from memory. Without it that visitor waits for the language server to
// hand over the roughly 9 MB bundle and for the regexp patches to run over it,
// which takes several seconds on top of the download itself.
func (p *Proxy) Prewarm() error {
	return p.PrewarmContext(context.Background())
}

// PrewarmContext is Prewarm bounded by the caller's lifecycle context.
func (p *Proxy) PrewarmContext(parent context.Context) error {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, prewarmTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.target.String()+"/main.js", nil)
	if err != nil {
		return err
	}
	if p.opts.TargetCSRFToken != "" {
		req.Header.Set("x-codeium-csrf-token", p.opts.TargetCSRFToken)
	}

	resp, err := p.transport.RoundTrip(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("language server answered %s for /main.js", resp.Status)
	}
	return p.modifyResponse(resp)
}

// Close releases idle upstream transport connections when a proxy instance is retired.
func (p *Proxy) Close() {
	if p == nil {
		return
	}
	if p.transport != nil {
		p.transport.CloseIdleConnections()
	}
	if p.sidecarTransport != nil {
		p.sidecarTransport.CloseIdleConnections()
	}
}

// transportPatchMissing reports whether the WebSocket transport patch was
// enabled but did not match, so the bundle keeps using fetch streaming.
func transportPatchMissing(report patches.Report) bool {
	for _, res := range report {
		if res.ID == "websocket-transport-default" {
			return res.Status == patches.StatusMissing
		}
	}
	return false
}

// redirectStaleBundle answers a bundle URL from an earlier deploy with a redirect
// to the current one. The HTML only ever names the current key, so an old key comes
// from the browser replaying what it loaded last time. iPhones do this alongside the
// real request after a deploy, and sending the whole bundle to both means two
// multi-megabyte downloads while the page is still blank. The key also changes when
// only the HTML patches do, so a copy whose tag still matches gets its 304 instead,
// rather than a redirect to a URL the browser has nothing cached for.
func (p *Proxy) redirectStaleBundle(w http.ResponseWriter, r *http.Request) bool {
	current := p.opts.Patch.CacheKey
	requested := r.URL.Query().Get("agy")
	if current == "" || requested == "" || requested == current {
		return false
	}
	if _, tag := p.cachedBundle(); etagMatches(r.Header.Get("If-None-Match"), tag) {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/main.js?agy="+current, http.StatusFound)
	return true
}

func (p *Proxy) cachedBundle() ([]byte, string) {
	p.mainJSMu.RLock()
	defer p.mainJSMu.RUnlock()
	return p.cachedMainJS, p.cachedMainTag
}

func (p *Proxy) cachedRootHTML() []byte {
	p.htmlMu.RLock()
	defer p.htmlMu.RUnlock()
	if p.cachedHTML == nil || p.cachedHTMLMissingWS != p.wsTransportMissing.Load() {
		return nil
	}
	return p.cachedHTML
}

// staticAssetCacheControl lets browsers reuse unpatched static assets (render-
// blocking CSS, syntax highlighters, icons, fonts) from disk cache for 1 hour
// and revalidate in the background for a day, avoiding per-navigation round
// trips caused by the language server's default "no-cache" header.
const staticAssetCacheControl = "private, max-age=3600, stale-while-revalidate=86400"

func isCacheableStaticAsset(path string) bool {
	if path == "/main.js" || strings.HasPrefix(path, "/__agy/") {
		return false
	}
	for _, ext := range []string{".css", ".js", ".svg", ".png", ".ico", ".woff2", ".woff", ".ttf", ".mp3", ".wasm"} {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

// bundleCacheControl makes browsers revalidate the patched bundle on every
// load. A matching ETag costs a 304 instead of the multi-megabyte body. Because
// the tag is derived from the patched bytes, when language_server updates or
// patches change, browsers pick up the new bundle on revalidation.
// private keeps shared caches and CDNs from storing a bundle that is only
// served behind authentication and embeds workspace paths.
const bundleCacheControl = "private, no-cache"

func bundleETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}

func setBundleValidators(h http.Header, tag string) {
	h.Set("Cache-Control", bundleCacheControl)
	h.Set("ETag", tag)
	h.Del("Last-Modified")
}

// etagMatches compares If-None-Match against the bundle tag. Weak tags are
// accepted because compressing proxies such as nginx and Cloudflare weaken
// the ETag they pass on.
func etagMatches(header, tag string) bool {
	if header == "" || tag == "" {
		return false
	}
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimPrefix(strings.TrimSpace(part), "W/")
		if part == "*" || part == tag {
			return true
		}
	}
	return false
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func (p *Proxy) sameOriginRequest(r *http.Request) bool {
	ok, unverified := auth.CheckSameOrigin(r, p.opts.TrustedProxies, p.opts.PublicURL)
	if ok && unverified {
		auth.WarnUnverifiedOriginFallback()
	}
	return ok
}

// sameOriginUpgrade rejects cross-origin WebSocket handshakes. The Director
// rewrites Origin to the loopback target, which disables the language
// server's own Origin check, and browsers apply no CORS to WebSockets, so a
// page on another subdomain or another port of the same host could otherwise
// open an authenticated RPC socket with the session cookie.
//
// When the reverse proxy forwards neither Host nor X-Forwarded-Host, only
// loopback names are left to compare against; the check is skipped with a
// warning only when Sec-Fetch-Site does not contradict same-origin.
func (p *Proxy) sameOriginUpgrade(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		log.Printf("[proxy] rejected WebSocket upgrade with missing Origin header: Sec-Fetch-Site=%q Host=%q X-Forwarded-Host=%q", r.Header.Get("Sec-Fetch-Site"), r.Host, r.Header.Get("X-Forwarded-Host"))
		return false
	}
	ok, unverified := auth.CheckSameOrigin(r, p.opts.TrustedProxies, p.opts.PublicURL)
	if ok {
		if unverified {
			log.Printf("[proxy] warning: WebSocket upgrade from Origin %q accepted without Host verification (reverse proxy forwarded no external Host header)", origin)
		}
		return true
	}
	log.Printf("[proxy] rejected cross-origin WebSocket upgrade: Origin=%q Sec-Fetch-Site=%q Host=%q X-Forwarded-Host=%q. Ensure your reverse proxy forwards Host (e.g. proxy_set_header Host $host;)", origin, r.Header.Get("Sec-Fetch-Site"), r.Host, r.Header.Get("X-Forwarded-Host"))
	return false
}

func (p *Proxy) trustedForwardedHost(r *http.Request) string {
	return auth.TrustedForwardedHost(r, p.opts.TrustedProxies)
}

func (p *Proxy) report(target patches.Target, report patches.Report) {
	if p.opts.OnReport == nil {
		return
	}
	if _, loaded := p.reported.LoadOrStore(target, true); loaded {
		return
	}
	p.opts.OnReport(target, report)
}

func errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	if r.Context().Err() != nil {
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "context canceled") || strings.Contains(msg, "client disconnected") {
		return
	}

	if r.Header.Get("x-grpc-web") != "" || strings.Contains(r.Header.Get("Content-Type"), "application/grpc-web") {
		w.Header().Set("Content-Type", "application/grpc-web+json")
		w.Header().Set("grpc-status", "14") // Unavailable
		w.Header().Set("grpc-message", "Antigravity language server is restarting")
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusBadGateway)
	_, _ = w.Write([]byte("Antigravity is not reachable. Is the language server still running?"))
}
