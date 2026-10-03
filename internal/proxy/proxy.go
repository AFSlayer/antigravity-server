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

	"github.com/AFSlayer/antigravity-server/internal/patches"
)

// ReportFunc receives the patch outcome the first time each document is served.
type ReportFunc func(target patches.Target, report patches.Report)

// Options configures a Proxy.
type Options struct {
	TargetPort      int
	TargetCSRFToken string
	PublicURL       string
	Patch           patches.Options
	OnReport        ReportFunc
}

// Proxy is a patching reverse proxy in front of one language server.
type Proxy struct {
	handler        *httputil.ReverseProxy
	opts           Options
	reported       sync.Map
	activeConns    atomic.Int64
	lastActivityNs atomic.Int64
	mainJSMu       sync.RWMutex
	cachedMainJS   []byte
	cachedMainTag  string
}

// New builds a Proxy targeting the language server on opts.TargetPort.
func New(opts Options) (*Proxy, error) {
	target, err := url.Parse(fmt.Sprintf("https://127.0.0.1:%d", opts.TargetPort))
	if err != nil {
		return nil, err
	}

	p := &Proxy{opts: opts}
	p.touchActivity()

	rp := httputil.NewSingleHostReverseProxy(target)
	rp.FlushInterval = -1
	rp.Transport = &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		DisableCompression:  true,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}

	host := target.Host
	base := rp.Director
	rp.Director = func(req *http.Request) {
		if orig := req.Header.Get("Origin"); orig != "" {
			req.Header.Set("X-Original-Origin", orig)
		}
		if clientHost := req.Header.Get("X-Forwarded-Host"); clientHost != "" {
			req.Header.Set("X-Client-Host", clientHost)
		} else if req.Host != "" {
			req.Header.Set("X-Client-Host", req.Host)
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
		p.activeConns.Add(1)
		p.touchActivity()
		defer func() {
			p.activeConns.Add(-1)
			p.touchActivity()
		}()

		if isWebSocketUpgrade(r) && !p.sameOriginUpgrade(r) {
			http.Error(w, "cross-origin WebSocket upgrade rejected", http.StatusForbidden)
			return
		}

		// Fast-path: Serve pre-warmed / cached main.js immediately from memory (0ms latency)
		if r.URL.Path == "/main.js" && r.Method == http.MethodGet {
			cached, tag := p.cachedBundle()
			if cached != nil {
				setBundleValidators(w.Header(), tag)
				if etagMatches(r.Header.Get("If-None-Match"), tag) {
					w.WriteHeader(http.StatusNotModified)
					return
				}
				w.Header().Set("Content-Type", "application/javascript")
				w.Header().Set("Content-Length", strconv.Itoa(len(cached)))
				_, _ = w.Write(cached)
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

// IsIdle returns true if there are zero active connections and no request activity has occurred
// for at least the specified threshold duration.
func (p *Proxy) IsIdle(threshold time.Duration) bool {
	if p.activeConns.Load() != 0 {
		return false
	}
	last := p.LastActivity()
	if last.IsZero() {
		return true
	}
	return time.Since(last) >= threshold
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
				proto := resp.Request.Header.Get("X-Forwarded-Proto")
				if proto == "" {
					proto = "https"
				}
				host := resp.Request.Header.Get("X-Forwarded-Host")
				if host == "" {
					host = resp.Request.Header.Get("X-Client-Host")
				}
				if host == "" {
					host = resp.Request.Host
				}
				orig = proto + "://" + host
			}
			resp.Header.Set("Access-Control-Allow-Origin", orig)
		}
	}

	target, ok := targetFor(resp)
	if !ok {
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

	patched, report := patches.Apply(target, body, p.opts.Patch)
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

func (p *Proxy) cachedBundle() ([]byte, string) {
	p.mainJSMu.RLock()
	defer p.mainJSMu.RUnlock()
	return p.cachedMainJS, p.cachedMainTag
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

// sameOriginUpgrade rejects cross-origin WebSocket handshakes. The Director
// rewrites Origin to the loopback target, which disables the language
// server's own Origin check, and browsers apply no CORS to WebSockets, so a
// page on another subdomain or another port of the same host could otherwise
// open an authenticated RPC socket with the session cookie.
//
// When the reverse proxy forwards neither Host nor X-Forwarded-Host, only
// loopback names are left to compare against; the check is skipped with a
// warning rather than breaking every handshake.
func (p *Proxy) sameOriginUpgrade(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		log.Printf("[proxy] rejecting WebSocket upgrade: malformed Origin %q", origin)
		return false
	}
	originHost := u.Hostname()
	originPort := u.Port()
	if originPort == "" {
		if strings.EqualFold(u.Scheme, "https") || strings.EqualFold(u.Scheme, "wss") {
			originPort = "443"
		} else if strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "ws") {
			originPort = "80"
		}
	}

	candidates := []string{
		r.Host,
		firstValue(r.Header.Get("X-Forwarded-Host")),
	}
	if p.opts.PublicURL != "" {
		if pu, err := url.Parse(p.opts.PublicURL); err == nil && pu.Host != "" {
			candidates = append(candidates, pu.Host)
		}
	}

	verifiable := false
	for _, h := range candidates {
		candHost, candPort := splitHostMaybePort(h)
		if candHost == "" {
			continue
		}
		if strings.EqualFold(candHost, originHost) {
			if candPort == "" || candPort == originPort {
				return true
			}
		}
		if !isLoopbackName(candHost) {
			verifiable = true
		}
	}

	if !verifiable {
		log.Printf("[proxy] warning: WebSocket upgrade from Origin %q accepted without Host verification (reverse proxy forwarded no external Host header)", origin)
		return true
	}

	log.Printf("[proxy] rejected cross-origin WebSocket upgrade: Origin=%q does not match Host=%q or X-Forwarded-Host=%q. Ensure your reverse proxy forwards Host (e.g. proxy_set_header Host $host;)", origin, r.Host, r.Header.Get("X-Forwarded-Host"))
	return false
}

func splitHostMaybePort(hostport string) (string, string) {
	if h, port, err := net.SplitHostPort(hostport); err == nil {
		return h, port
	}
	return strings.Trim(hostport, "[]"), ""
}

func firstValue(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

func hostname(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return strings.Trim(hostport, "[]")
}

func isLoopbackName(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
