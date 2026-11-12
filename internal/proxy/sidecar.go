package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/auth"
)

const (
	// SidecarPrefix is the same-origin path prefix under which active sidecar
	// HTTP/WebSocket gateways are reverse-proxied.
	SidecarPrefix = "/__agy/sidecar/"

	sidecarShimName             = "__agy_sidecar_shim.js"
	sidecarCapSegment           = "~s"
	sidecarCapHexLen            = 32
	sidecarCapIdleTTL           = 2 * time.Hour
	sidecarCapMaxLifetime       = 12 * time.Hour
	sidecarCapReuseMinRemaining = 30 * time.Minute
	sidecarCapMaxEntries        = 256
	sidecarVaultMaxCookies      = 64
	sidecarVaultMaxCookieBytes  = 8192

	sidecarCacheTTL        = 2 * time.Second
	sidecarMissRefreshMin  = 200 * time.Millisecond
	sidecarMaxRewriteBytes = 16 << 20
	sidecarMaxWSRejectBody = 4 << 20
	sidecarDialTimeout     = 5 * time.Second
	sidecarHeaderTimeout   = 30 * time.Second
	sidecarRPCTimeout      = 3 * time.Second
	sidecarWSIdleTimeout   = 10 * time.Minute
	sidecarWSWriteTimeout  = 30 * time.Second
	sidecarWSCloseGrace    = 5 * time.Second

	sidecarSandboxDirective = "sandbox allow-scripts allow-forms allow-popups allow-popups-to-escape-sandbox allow-modals allow-downloads"
	sidecarFrameAncestors   = "frame-ancestors 'self'"
	sidecarDefaultCSP       = sidecarSandboxDirective + "; " + sidecarFrameAncestors
)

type sidecarCapSession struct {
	token         string
	port          int
	parentSession string
	createdAt     time.Time
	lastUsedAt    time.Time

	cookieMu      sync.Mutex
	cookies       map[string]string
	extHTTPPrefix string
	extWSPrefix   string
}

func (s *sidecarCapSession) setExternalPrefixes(httpPrefix, wsPrefix string) {
	if s == nil {
		return
	}
	s.cookieMu.Lock()
	s.extHTTPPrefix = httpPrefix
	s.extWSPrefix = wsPrefix
	s.cookieMu.Unlock()
}

func (s *sidecarCapSession) externalPrefixes() (string, string) {
	if s == nil {
		return "", ""
	}
	s.cookieMu.Lock()
	defer s.cookieMu.Unlock()
	return s.extHTTPPrefix, s.extWSPrefix
}

func (s *sidecarCapSession) cookieHeader() string {
	if s == nil {
		return ""
	}
	s.cookieMu.Lock()
	defer s.cookieMu.Unlock()
	if len(s.cookies) == 0 {
		return ""
	}
	names := make([]string, 0, len(s.cookies))
	for name := range s.cookies {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		if isProtectedCookieName(name) {
			continue
		}
		parts = append(parts, name+"="+s.cookies[name])
	}
	return strings.Join(parts, "; ")
}

func (s *sidecarCapSession) snapshotCookies() map[string]string {
	if s == nil {
		return make(map[string]string)
	}
	s.cookieMu.Lock()
	defer s.cookieMu.Unlock()
	out := make(map[string]string, len(s.cookies))
	for k, v := range s.cookies {
		out[k] = v
	}
	return out
}

func (s *sidecarCapSession) storeSetCookies(rawHeaders []string) {
	if s == nil || len(rawHeaders) == 0 {
		return
	}
	now := time.Now()
	s.cookieMu.Lock()
	defer s.cookieMu.Unlock()
	if s.cookies == nil {
		s.cookies = make(map[string]string)
	}

	for _, raw := range rawHeaders {
		parts := strings.Split(raw, ";")
		if len(parts) == 0 {
			continue
		}
		first := strings.TrimSpace(parts[0])
		eq := strings.IndexByte(first, '=')
		if eq <= 0 {
			continue
		}
		name := strings.TrimSpace(first[:eq])
		val := strings.TrimSpace(first[eq+1:])
		if name == "" || !isValidCookieToken(name) || isProtectedCookieName(name) {
			continue
		}
		if len(name)+len(val) > sidecarVaultMaxCookieBytes || hasCookieControlByte(val) {
			continue
		}

		expired := false
		for _, attr := range parts[1:] {
			attr = strings.TrimSpace(attr)
			if attr == "" {
				continue
			}
			k := attr
			v := ""
			if idx := strings.IndexByte(attr, '='); idx >= 0 {
				k = strings.TrimSpace(attr[:idx])
				v = strings.TrimSpace(attr[idx+1:])
			}
			switch strings.ToLower(k) {
			case "max-age":
				if maxAge, err := strconv.Atoi(v); err == nil && maxAge <= 0 {
					expired = true
				}
			case "expires":
				if exp, err := http.ParseTime(v); err == nil && !exp.After(now) {
					expired = true
				}
			}
		}

		if expired {
			delete(s.cookies, name)
			continue
		}
		if _, exists := s.cookies[name]; !exists && len(s.cookies) >= sidecarVaultMaxCookies {
			continue
		}
		s.cookies[name] = val
	}
}

func isProtectedCookieName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return lower == strings.ToLower(auth.CookieName) || strings.Contains(lower, "agy_session")
}

func isValidCookieToken(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		isAlphaNum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !isAlphaNum && !strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)) {
			return false
		}
	}
	return len(s) > 0
}

func hasCookieControlByte(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f || s[i] == ';' {
			return true
		}
	}
	return false
}

func newSidecarTransport() *http.Transport {
	return &http.Transport{
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       60 * time.Second,
		ResponseHeaderTimeout: sidecarHeaderTimeout,
		DialContext:           dialSidecarAddress,
	}
}

// dialSidecarAddress dials a loopback sidecar endpoint while honoring explicit
// address families ("127.0.0.1" or "::1") without cross-family fallback so an
// unrelated process bound to the other loopback family on the same port cannot
// be reached. Only ambiguous "localhost" targets fall back from IPv4 to IPv6 on
// ECONNREFUSED.
func dialSidecarAddress(ctx context.Context, network, addr string) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return nil, fmt.Errorf("invalid loopback port %q", portStr)
	}

	dialer := &net.Dialer{Timeout: sidecarDialTimeout}
	cleanHost := strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
	switch cleanHost {
	case "127.0.0.1":
		return dialer.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	case "::1":
		return dialer.DialContext(ctx, network, net.JoinHostPort("::1", strconv.Itoa(port)))
	default:
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			return conn, nil
		}
		if isConnRefused(err) {
			if conn6, err6 := dialer.DialContext(ctx, network, net.JoinHostPort("::1", strconv.Itoa(port))); err6 == nil {
				return conn6, nil
			}
		}
		return nil, err
	}
}

func (p *Proxy) resolveSidecarDialAddr(ctx context.Context, dialMode string, port int) string {
	switch dialMode {
	case "127.0.0.1":
		return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	case "::1":
		return net.JoinHostPort("::1", strconv.Itoa(port))
	default:
		if p.opts.ResolveSidecarHost != nil {
			if exact, ok := p.opts.ResolveSidecarHost(ctx, port, "localhost"); ok && (exact == "127.0.0.1" || exact == "::1") {
				return net.JoinHostPort(exact, strconv.Itoa(port))
			}
		}
		return net.JoinHostPort("localhost", strconv.Itoa(port))
	}
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

// IsSidecarReservedPort reports whether port is forbidden or currently owned by
// any sidecar (raw webPort or auth gateway). Uncached ports trigger a
// singleflight-coalesced live GetSidecars check, and RPC errors fail closed so
// a newly spawned or transiently unreachable sidecar port can never be exposed
// through generic loopback preview fetches.
func (p *Proxy) IsSidecarReservedPort(ctx context.Context, port int) bool {
	if p.isForbiddenPort(port) {
		return true
	}
	callStart := time.Now()
	p.sidecarMu.Lock()
	_, cachedReserved := p.sidecarReserved[port]
	freshCache := !p.sidecarCachedAt.IsZero() && callStart.Sub(p.sidecarCachedAt) < sidecarCacheTTL && p.sidecarLastErr == nil
	p.sidecarMu.Unlock()
	if cachedReserved && freshCache {
		return true
	}

	_, reserved, err := p.refreshSidecarPorts(ctx, true, callStart)
	if err != nil {
		return true
	}
	_, isReserved := reserved[port]
	return isReserved
}

// ReservedSidecarPorts returns a snapshot of all ports currently owned by
// sidecars (both raw webPorts and auth gateway ports).
func (p *Proxy) ReservedSidecarPorts(ctx context.Context) (map[int]struct{}, error) {
	_, reserved, err := p.refreshSidecarPorts(ctx, false, time.Time{})
	if err != nil {
		return nil, err
	}
	out := make(map[int]struct{}, len(reserved))
	for port := range reserved {
		out[port] = struct{}{}
	}
	return out, nil
}

func (p *Proxy) isForbiddenPort(port int) bool {
	if port <= 0 || port > 65535 || port == p.opts.TargetPort {
		return true
	}
	for _, fp := range p.opts.ForbiddenPorts {
		if fp > 0 && port == fp {
			return true
		}
	}
	return false
}

func (p *Proxy) allowedSidecarDialMode(ctx context.Context, port int) (string, bool) {
	if p.isForbiddenPort(port) {
		return "", false
	}
	now := time.Now()
	p.sidecarMu.Lock()
	if !p.sidecarCachedAt.IsZero() && now.Sub(p.sidecarCachedAt) < sidecarCacheTTL && p.sidecarLastErr == nil {
		if mode, ok := p.sidecarAllowed[port]; ok {
			p.sidecarMu.Unlock()
			return mode, true
		}
		if !p.sidecarLastProbe.IsZero() && now.Sub(p.sidecarLastProbe) < sidecarMissRefreshMin {
			p.sidecarMu.Unlock()
			return "", false
		}
	}
	p.sidecarMu.Unlock()

	allowed, _, err := p.refreshSidecarPorts(ctx, true, now)
	if err != nil {
		return "", false
	}
	mode, ok := allowed[port]
	return mode, ok
}

func (p *Proxy) refreshSidecarPorts(ctx context.Context, force bool, minStart time.Time) (allowed map[int]string, reserved map[int]struct{}, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for attempt := 0; attempt < 2; attempt++ {
		p.sidecarMu.Lock()
		if !force && !p.sidecarCachedAt.IsZero() && time.Since(p.sidecarCachedAt) < sidecarCacheTTL && p.sidecarLastErr == nil {
			a, r := p.sidecarAllowed, p.sidecarReserved
			p.sidecarMu.Unlock()
			return a, r, nil
		}
		if p.sidecarInflight != nil {
			waitCh := p.sidecarInflight
			started := p.sidecarInflightStarted
			p.sidecarMu.Unlock()

			select {
			case <-waitCh:
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			}

			p.sidecarMu.Lock()
			a, r, lastErr := p.sidecarAllowed, p.sidecarReserved, p.sidecarLastErr
			p.sidecarMu.Unlock()
			if !force || !started.Before(minStart) || attempt == 1 {
				if lastErr != nil {
					return nil, nil, lastErr
				}
				return a, r, nil
			}
			continue
		}

		ch := make(chan struct{})
		now := time.Now()
		p.sidecarInflight = ch
		p.sidecarInflightStarted = now
		p.sidecarLastProbe = now
		p.sidecarMu.Unlock()

		freshAllowed, freshReserved, fetchErr := p.fetchSidecarPorts(context.Background())

		p.sidecarMu.Lock()
		if fetchErr == nil {
			p.sidecarAllowed = freshAllowed
			p.sidecarReserved = freshReserved
			p.sidecarCachedAt = time.Now()
			p.sidecarLastErr = nil
		} else {
			p.sidecarLastErr = fetchErr
		}
		p.sidecarInflight = nil
		close(ch)
		p.sidecarMu.Unlock()

		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		if fetchErr != nil {
			return nil, nil, fetchErr
		}
		return freshAllowed, freshReserved, nil
	}
	return nil, nil, errors.New("sidecar port refresh aborted")
}

func (p *Proxy) fetchSidecarPorts(parent context.Context) (allowed map[int]string, reserved map[int]struct{}, err error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, sidecarRPCTimeout)
	defer cancel()

	rpcURL := p.target.String() + "/exa.language_server_pb.LanguageServerService/GetSidecars"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	if p.opts.TargetCSRFToken != "" {
		req.Header.Set("x-codeium-csrf-token", p.opts.TargetCSRFToken)
	}

	resp, err := p.transport.RoundTrip(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("GetSidecars returned %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, nil, err
	}

	var payload struct {
		Sidecars []struct {
			BaseURL string          `json:"baseUrl"`
			WebPort json.RawMessage `json:"webPort"`
			Status  json.RawMessage `json:"status"`
		} `json:"sidecars"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, nil, err
	}

	allowed = make(map[int]string)
	reserved = make(map[int]struct{})

	for _, sc := range payload.Sidecars {
		webPort := parseJSONPort(sc.WebPort)
		if webPort > 0 && webPort <= 65535 {
			reserved[webPort] = struct{}{}
		}
		if gwHost, gwPort, ok := loopbackEndpointFromURL(sc.BaseURL); ok && !p.isForbiddenPort(gwPort) {
			reserved[gwPort] = struct{}{}
			allowed[gwPort] = gwHost
			continue
		}
		if sc.BaseURL == "" && !p.isForbiddenPort(webPort) && sidecarStatusRunning(sc.Status) {
			allowed[webPort] = "localhost"
		}
	}
	return allowed, reserved, nil
}

func parseJSONPort(raw json.RawMessage) int {
	s := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if s == "" || s == "null" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 || n > 65535 {
		return 0
	}
	return n
}

func sidecarStatusRunning(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == `"SIDECAR_STATUS_RUNNING"` || s == "2"
}

func loopbackEndpointFromURL(raw string) (hostMode string, port int, ok bool) {
	if raw == "" {
		return "", 0, false
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "http") {
		return "", 0, false
	}
	host := strings.ToLower(u.Hostname())
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return "", 0, false
	}
	port, err = strconv.Atoi(u.Port())
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, false
	}
	return host, port, true
}

func loopbackPortFromURL(raw string) (int, bool) {
	_, port, ok := loopbackEndpointFromURL(raw)
	return port, ok
}

func isValidSidecarCapHex(s string) bool {
	if len(s) != sidecarCapHexLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func parseSidecarRoute(escapedPath string) (port int, capToken string, restEscaped string, needsSlash bool, invalidCap bool, ok bool) {
	if !strings.HasPrefix(escapedPath, SidecarPrefix) {
		return 0, "", "", false, false, false
	}
	tail := strings.TrimPrefix(escapedPath, SidecarPrefix)
	if tail == "" {
		return 0, "", "", false, false, false
	}
	slash := strings.IndexByte(tail, '/')
	var portPart string
	if slash < 0 {
		portPart = tail
		needsSlash = true
	} else {
		portPart = tail[:slash]
		restEscaped = tail[slash:]
	}
	p, err := strconv.Atoi(portPart)
	if err != nil || p <= 0 || p > 65535 {
		return 0, "", "", false, false, false
	}
	if needsSlash {
		return p, "", "", true, false, true
	}

	capPrefix := "/" + sidecarCapSegment
	if restEscaped == capPrefix || strings.HasPrefix(restEscaped, capPrefix+"/") {
		if restEscaped == capPrefix || restEscaped == capPrefix+"/" {
			return p, "", "", false, true, true
		}
		after := strings.TrimPrefix(restEscaped, capPrefix+"/")
		nextSlash := strings.IndexByte(after, '/')
		var tok string
		if nextSlash < 0 {
			tok = after
			if !isValidSidecarCapHex(tok) {
				return p, "", "", false, true, true
			}
			return p, tok, "", true, false, true
		}
		tok = after[:nextSlash]
		if !isValidSidecarCapHex(tok) {
			return p, "", "", false, true, true
		}
		return p, tok, after[nextSlash:], false, false, true
	}

	return p, "", restEscaped, false, false, true
}

func sidecarCapPrefix(port int, capToken string) string {
	return fmt.Sprintf("%s%d/%s/%s", SidecarPrefix, port, sidecarCapSegment, capToken)
}

func (p *Proxy) issueOrReuseSidecarCap(parentSession string, port int) (*sidecarCapSession, error) {
	now := time.Now()

	p.sidecarCapMu.Lock()
	defer p.sidecarCapMu.Unlock()
	if p.sidecarCaps == nil {
		p.sidecarCaps = make(map[string]*sidecarCapSession)
	}

	p.pruneExpiredSidecarCapsLocked(now)

	var inheritedCookies map[string]string
	for _, existing := range p.sidecarCaps {
		if existing.port == port && existing.parentSession == parentSession {
			if sidecarCapMaxLifetime-now.Sub(existing.createdAt) >= sidecarCapReuseMinRemaining {
				existing.lastUsedAt = now
				return existing, nil
			}
			inheritedCookies = existing.snapshotCookies()
		}
	}

	for len(p.sidecarCaps) >= sidecarCapMaxEntries {
		var oldestKey string
		var oldestTime time.Time
		for k, sess := range p.sidecarCaps {
			if oldestKey == "" || sess.lastUsedAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = sess.lastUsedAt
			}
		}
		if oldestKey == "" {
			break
		}
		delete(p.sidecarCaps, oldestKey)
	}

	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, err
	}
	tok := hex.EncodeToString(raw[:])
	if inheritedCookies == nil {
		inheritedCookies = make(map[string]string)
	}
	sess := &sidecarCapSession{
		token:         tok,
		port:          port,
		parentSession: parentSession,
		createdAt:     now,
		lastUsedAt:    now,
		cookies:       inheritedCookies,
	}
	p.sidecarCaps[tok] = sess
	return sess, nil
}

func (p *Proxy) lookupSidecarCap(capToken string, port int) *sidecarCapSession {
	if !isValidSidecarCapHex(capToken) {
		return nil
	}
	now := time.Now()

	p.sidecarCapMu.Lock()
	defer p.sidecarCapMu.Unlock()
	sess, ok := p.sidecarCaps[capToken]
	if !ok {
		return nil
	}
	if now.Sub(sess.lastUsedAt) > sidecarCapIdleTTL || now.Sub(sess.createdAt) > sidecarCapMaxLifetime {
		delete(p.sidecarCaps, capToken)
		return nil
	}
	if sess.port != port {
		return nil
	}
	sess.lastUsedAt = now
	return sess
}

func (p *Proxy) deleteSidecarCap(capToken string) {
	p.sidecarCapMu.Lock()
	delete(p.sidecarCaps, capToken)
	p.sidecarCapMu.Unlock()
}

func (p *Proxy) pruneExpiredSidecarCapsLocked(now time.Time) {
	for k, sess := range p.sidecarCaps {
		if now.Sub(sess.lastUsedAt) > sidecarCapIdleTTL || now.Sub(sess.createdAt) > sidecarCapMaxLifetime {
			delete(p.sidecarCaps, k)
		}
	}
}

// ValidateSidecarCapability authorizes capability-scoped sidecar requests
// (/.../~s/<cap>/...) sent from opaque-origin (Origin: null) sandboxed frames
// without relying on ambient browser cookies.
func (p *Proxy) ValidateSidecarCapability(r *http.Request) bool {
	if p == nil || r == nil || r.URL == nil {
		return false
	}
	port, capToken, _, _, invalidCap, ok := parseSidecarRoute(r.URL.EscapedPath())
	if !ok || invalidCap || capToken == "" {
		return false
	}
	sess, _ := p.authorizeSidecarCapRequest(r, port, capToken)
	return sess != nil
}

func (p *Proxy) authorizeSidecarCapRequest(r *http.Request, port int, capToken string) (*sidecarCapSession, string) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if isWebSocketUpgrade(r) {
		if origin != "null" && !p.sameOriginUpgrade(r) {
			return nil, ""
		}
	} else {
		if origin != "" && origin != "null" && !p.sameOriginRequest(r) {
			return nil, ""
		}
	}

	capSess := p.lookupSidecarCap(capToken, port)
	if capSess == nil {
		return nil, ""
	}
	if p.opts.ValidateSession != nil {
		if capSess.parentSession == "" || !p.opts.ValidateSession(capSess.parentSession) {
			p.deleteSidecarCap(capToken)
			return nil, ""
		}
	}
	dialMode, ok := p.allowedSidecarDialMode(r.Context(), port)
	if !ok {
		return nil, ""
	}
	return capSess, dialMode
}

func (p *Proxy) serveSidecar(w http.ResponseWriter, r *http.Request, releaseHTTPReq func()) {
	escaped := r.URL.EscapedPath()
	port, capToken, restEscaped, needsSlash, invalidCap, ok := parseSidecarRoute(escaped)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if invalidCap {
		http.Error(w, "invalid sidecar capability", http.StatusForbidden)
		return
	}

	var (
		capSess  *sidecarCapSession
		dialMode string
	)
	if capToken == "" {
		if isWebSocketUpgrade(r) || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			http.Error(w, "sidecar capability required", http.StatusForbidden)
			return
		}
		if !p.sameOriginRequest(r) {
			http.Error(w, "cross-origin sidecar request rejected", http.StatusForbidden)
			return
		}

		parentSession := ""
		if c, err := r.Cookie(auth.CookieName); err == nil {
			parentSession = c.Value
		}
		if p.opts.ValidateSession != nil && (parentSession == "" || !p.opts.ValidateSession(parentSession)) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var allowed bool
		dialMode, allowed = p.allowedSidecarDialMode(r.Context(), port)
		if !allowed {
			http.Error(w, "sidecar port is not active", http.StatusForbidden)
			return
		}
		if needsSlash {
			target := fmt.Sprintf("%s%d/", SidecarPrefix, port)
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			http.Redirect(w, r, target, http.StatusTemporaryRedirect)
			return
		}

		var err error
		capSess, err = p.issueOrReuseSidecarCap(parentSession, port)
		if err != nil {
			http.Error(w, "could not initialize sidecar sandbox", http.StatusInternalServerError)
			return
		}
		httpPfx, wsPfx := p.externalSidecarPrefixURL(r, port, nil)
		capSess.setExternalPrefixes(httpPfx, wsPfx)
	} else {
		capSess, dialMode = p.authorizeSidecarCapRequest(r, port, capToken)
		if capSess == nil {
			http.Error(w, "invalid or expired sidecar capability", http.StatusForbidden)
			return
		}
		if needsSlash {
			target := sidecarCapPrefix(port, capToken) + "/"
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			applySidecarCORSHeaders(w.Header(), r)
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			http.Redirect(w, r, target, http.StatusTemporaryRedirect)
			return
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			applySidecarCORSHeaders(w.Header(), r)
			w.Header().Set("Content-Security-Policy", sidecarDefaultCSP)
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	capPrefix := sidecarCapPrefix(port, capSess.token)
	dialAddr := p.resolveSidecarDialAddr(r.Context(), dialMode, port)

	decodedRest, err := url.PathUnescape(restEscaped)
	if err != nil {
		decodedRest = restEscaped
	}
	if decodedRest == "/"+sidecarShimName {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		shim := []byte(sidecarShimJS(port, capPrefix))
		applySidecarCORSHeaders(w.Header(), r)
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Content-Security-Policy", sidecarDefaultCSP)
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(shim)))
		if r.Method == http.MethodGet {
			_, _ = w.Write(shim)
		}
		return
	}

	if isWebSocketUpgrade(r) {
		p.proxySidecarWebSocket(w, r, port, dialAddr, capPrefix, capSess, decodedRest, restEscaped)
		return
	}

	upstreamHost := fmt.Sprintf("localhost:%d", port)
	upstreamOrigin := "http://" + upstreamHost
	vaultCookie := capSess.cookieHeader()

	rp := &httputil.ReverseProxy{
		Transport:     p.sidecarTransport,
		FlushInterval: -1,
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = dialAddr
			req.URL.Path = decodedRest
			req.URL.RawPath = restEscaped
			req.Host = upstreamHost

			prepareSidecarUpstreamHeaders(req.Header, upstreamOrigin, vaultCookie)
			req.Header["X-Forwarded-For"] = nil
			req.Header.Del("Accept-Encoding")
			req.Header.Del("If-None-Match")
			req.Header.Del("If-Modified-Since")
		},
		ModifyResponse: func(resp *http.Response) error {
			return p.modifySidecarResponse(resp, r, port, capPrefix, capSess, releaseHTTPReq)
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, proxyErr error) {
			if req.Context().Err() != nil {
				return
			}
			applySidecarCORSHeaders(rw.Header(), r)
			rw.Header().Set("Content-Security-Policy", sidecarDefaultCSP)
			rw.Header().Set("Referrer-Policy", "no-referrer")
			rw.WriteHeader(http.StatusBadGateway)
			_, _ = rw.Write([]byte("Sidecar UI is not reachable."))
		},
	}
	rp.ServeHTTP(w, r)
}

func applySidecarCORSHeaders(h http.Header, r *http.Request) {
	if r == nil {
		return
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return
	}
	h.Set("Access-Control-Allow-Origin", origin)
	h.Set("Access-Control-Allow-Credentials", "true")
	h.Add("Vary", "Origin")
	if reqMethod := strings.TrimSpace(r.Header.Get("Access-Control-Request-Method")); reqMethod != "" {
		h.Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS")
		if reqHeaders := strings.TrimSpace(r.Header.Get("Access-Control-Request-Headers")); reqHeaders != "" {
			h.Set("Access-Control-Allow-Headers", reqHeaders)
			h.Add("Vary", "Access-Control-Request-Headers")
		}
		h.Set("Access-Control-Max-Age", "600")
	}
}

func prepareSidecarUpstreamHeaders(h http.Header, upstreamOrigin string, vaultCookie string) {
	// Never forward browser cookies to sidecar servers; inject only cookies
	// stored in the server-side capability vault for this sidecar session.
	h.Del("Cookie")
	if vaultCookie != "" {
		h.Set("Cookie", vaultCookie)
	}

	h.Set("Origin", upstreamOrigin)
	h.Set("Sec-Fetch-Site", "same-origin")
	h.Del("Referer")

	h.Del("X-Original-Origin")
	h.Del("X-Client-Host")
	h.Del("X-Forwarded-For")
	h.Del("X-Forwarded-Host")
	h.Del("X-Forwarded-Proto")
	h.Del("Forwarded")
	h.Del("X-Real-IP")
	h.Del("x-codeium-csrf-token")
}

type activityReadCloser struct {
	rc    io.ReadCloser
	touch func()
}

func (a *activityReadCloser) Read(p []byte) (int, error) {
	n, err := a.rc.Read(p)
	if n > 0 && a.touch != nil {
		a.touch()
	}
	return n, err
}

func (a *activityReadCloser) Close() error {
	return a.rc.Close()
}

func (p *Proxy) modifySidecarResponse(resp *http.Response, clientReq *http.Request, port int, capPrefix string, capSess *sidecarCapSession, releaseHTTPReq func()) error {
	if rawCookies := resp.Header.Values("Set-Cookie"); len(rawCookies) > 0 {
		capSess.storeSetCookies(rawCookies)
		resp.Header.Del("Set-Cookie")
	}

	if loc := resp.Header.Get("Location"); loc != "" {
		resp.Header.Set("Location", rewriteSidecarLocation(loc, port, capPrefix))
	}

	applySidecarCORSHeaders(resp.Header, clientReq)
	shimNonce := p.enforceSidecarSecurityHeaders(resp.Header, clientReq, port, capSess)

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "text/event-stream") {
		if releaseHTTPReq != nil {
			releaseHTTPReq()
		}
		if resp.Body != nil {
			resp.Body = &activityReadCloser{rc: resp.Body, touch: p.touchActivity}
		}
		return nil
	}

	if resp.Request != nil && resp.Request.Method == http.MethodHead {
		resp.Header.Del("Content-Length")
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified || resp.Header.Get("Content-Encoding") != "" {
		return nil
	}

	reqPath := "/"
	if resp.Request != nil && resp.Request.URL != nil && resp.Request.URL.Path != "" {
		reqPath = resp.Request.URL.Path
	}
	isHTML := strings.Contains(ct, "text/html")
	isCSS := strings.Contains(ct, "text/css")
	isPreloadJS := reqPath == "/preload.js" && (strings.Contains(ct, "javascript") || strings.Contains(ct, "ecmascript") || ct == "")
	if !isHTML && !isCSS && !isPreloadJS {
		return nil
	}

	limited := io.LimitReader(resp.Body, sidecarMaxRewriteBytes+1)
	buf, err := io.ReadAll(limited)
	if err != nil {
		_ = resp.Body.Close()
		return err
	}
	if int64(len(buf)) > sidecarMaxRewriteBytes {
		origBody := resp.Body
		resp.Body = struct {
			io.Reader
			io.Closer
		}{
			Reader: io.MultiReader(bytes.NewReader(buf), origBody),
			Closer: origBody,
		}
		return nil
	}
	_ = resp.Body.Close()

	baseDir := sidecarBaseDir(reqPath)
	var rewritten []byte
	switch {
	case isHTML:
		rewritten = rewriteSidecarHTML(buf, port, capPrefix, baseDir, shimNonce)
	case isCSS:
		rewritten = rewriteSidecarCSS(buf, port, capPrefix, baseDir)
	case isPreloadJS:
		shim := sidecarShimJS(port, capPrefix) + "\n"
		rewritten = make([]byte, 0, len(shim)+len(buf))
		rewritten = append(rewritten, shim...)
		rewritten = append(rewritten, buf...)
	}

	resp.Body = io.NopCloser(bytes.NewReader(rewritten))
	resp.ContentLength = int64(len(rewritten))
	resp.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
	resp.Header.Set("Cache-Control", "no-store")
	resp.Header.Del("ETag")
	resp.Header.Del("Last-Modified")
	return nil
}

func (p *Proxy) enforceSidecarSecurityHeaders(h http.Header, clientReq *http.Request, port int, capSess *sidecarCapSession) (shimNonce string) {
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Del("Content-Security-Policy-Report-Only")

	policies := splitCSPPolicies(h.Values("Content-Security-Policy"))
	h.Del("Content-Security-Policy")
	if len(policies) == 0 {
		h.Set("Content-Security-Policy", sidecarDefaultCSP)
		return ""
	}

	httpPrefix, wsPrefix := p.externalSidecarPrefixURL(clientReq, port, capSess)

	var candidateNonce string
	var nonceUsed bool
	rewrittenPolicies := make([]string, 0, len(policies))
	for _, pol := range policies {
		if candidateNonce == "" && cspPolicyNeedsShimNonce(pol, httpPrefix) {
			var nb [12]byte
			if _, err := rand.Read(nb[:]); err == nil {
				candidateNonce = hex.EncodeToString(nb[:])
			}
		}
		rewritten, used := rewriteSingleSidecarCSP(pol, httpPrefix, wsPrefix, candidateNonce)
		if used {
			nonceUsed = true
		}
		rewrittenPolicies = append(rewrittenPolicies, rewritten)
	}

	for _, pol := range rewrittenPolicies {
		h.Add("Content-Security-Policy", pol)
	}
	if nonceUsed {
		return candidateNonce
	}
	return ""
}

func splitCSPPolicies(headerValues []string) []string {
	var out []string
	for _, hv := range headerValues {
		for _, part := range strings.Split(hv, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	return out
}

func (p *Proxy) externalSidecarPrefixURL(r *http.Request, port int, capSess *sidecarCapSession) (httpPrefix, wsPrefix string) {
	if capSess != nil {
		if savedHTTP, savedWS := capSess.externalPrefixes(); savedHTTP != "" {
			return savedHTTP, savedWS
		}
	}

	var (
		pubHostPort string
		pubHost     string
		pubIsHTTPS  bool
	)
	if p.opts.PublicURL != "" {
		if pub, err := url.Parse(p.opts.PublicURL); err == nil && pub.Host != "" {
			pubHostPort = auth.SanitizeHostPort(pub.Host)
			pubHost, _ = auth.SplitHostOptionalPort(pubHostPort)
			pubIsHTTPS = strings.EqualFold(pub.Scheme, "https")
		}
	}

	var (
		reqHostPort string
		reqHost     string
		reqIsHTTPS  bool
		sourceHost  string
	)
	if r != nil {
		reqIsHTTPS = auth.RequestIsSecure(r, p.opts.TrustedProxies)
		if fwd := p.trustedForwardedHost(r); fwd != "" {
			reqHostPort = fwd
		} else if safe := auth.SanitizeHostPort(r.Host); safe != "" {
			reqHostPort = safe
		}
		reqHost, _ = auth.SplitHostOptionalPort(reqHostPort)

		src := strings.TrimSpace(r.Header.Get("Origin"))
		if src == "" || src == "null" {
			src = strings.TrimSpace(r.Header.Get("Referer"))
		}
		if src != "" && src != "null" {
			if su, err := url.Parse(src); err == nil {
				sourceHost = su.Hostname()
			}
		}
	}

	directLoopback := sourceHost != "" && auth.IsLoopbackHost(sourceHost) && strings.EqualFold(sourceHost, reqHost)

	var (
		host  string
		https bool
	)
	switch {
	case directLoopback && reqHostPort != "":
		host = reqHostPort
		https = reqIsHTTPS
	case reqHostPort == "" || (auth.IsLoopbackHost(reqHost) && pubHost != "" && !auth.IsLoopbackHost(pubHost)):
		host = pubHostPort
		https = pubIsHTTPS
	default:
		host = reqHostPort
		https = reqIsHTTPS || (pubHost != "" && strings.EqualFold(reqHost, pubHost) && pubIsHTTPS)
	}
	if host == "" {
		return "", ""
	}

	proto := "http"
	wsProto := "ws"
	if https {
		proto = "https"
		wsProto = "wss"
	}
	suffix := fmt.Sprintf("%s%d/", SidecarPrefix, port)
	return proto + "://" + host + suffix, wsProto + "://" + host + suffix
}

type parsedCSPDirective struct {
	name   string
	tokens []string
}

func parseCSPDirectives(csp string) (dirs []parsedCSPDirective, scriptElemIdx, scriptIdx, defaultIdx int) {
	scriptElemIdx = -1
	scriptIdx = -1
	defaultIdx = -1
	for _, raw := range strings.Split(csp, ";") {
		fields := strings.Fields(strings.TrimSpace(raw))
		if len(fields) == 0 {
			continue
		}
		name := strings.ToLower(fields[0])
		if name == "sandbox" || name == "frame-ancestors" {
			continue
		}
		idx := len(dirs)
		switch name {
		case "script-src-elem":
			if scriptElemIdx < 0 {
				scriptElemIdx = idx
			}
		case "script-src":
			if scriptIdx < 0 {
				scriptIdx = idx
			}
		case "default-src":
			if defaultIdx < 0 {
				defaultIdx = idx
			}
		}
		dirs = append(dirs, parsedCSPDirective{name: name, tokens: fields[1:]})
	}
	return dirs, scriptElemIdx, scriptIdx, defaultIdx
}

func effectiveScriptDirectiveIndex(scriptElemIdx, scriptIdx, defaultIdx int) int {
	if scriptElemIdx >= 0 {
		return scriptElemIdx
	}
	if scriptIdx >= 0 {
		return scriptIdx
	}
	return defaultIdx
}

func cspTokensRequireNonce(tokens []string, httpPrefix string) (hasNone bool, needsNonce bool) {
	hasSelf := false
	hasWildcard := false
	hasCrypto := false
	for _, tok := range tokens {
		low := strings.ToLower(tok)
		switch {
		case low == "'none'":
			hasNone = true
		case low == "'self'":
			hasSelf = true
		case low == "*" || low == "http:" || low == "https:":
			hasWildcard = true
		case low == "'strict-dynamic'":
			hasCrypto = true
		case strings.HasPrefix(low, "'nonce-") && strings.HasSuffix(tok, "'") && len(tok) > 8:
			hasCrypto = true
		case strings.HasPrefix(low, "'sha256-") || strings.HasPrefix(low, "'sha384-") || strings.HasPrefix(low, "'sha512-"):
			hasCrypto = true
		}
	}
	if hasNone || hasCrypto {
		return hasNone, true
	}
	if !hasWildcard && (!hasSelf || httpPrefix == "") {
		return false, true
	}
	return false, false
}

func cspPolicyNeedsShimNonce(csp, httpPrefix string) bool {
	dirs, scriptElemIdx, scriptIdx, defaultIdx := parseCSPDirectives(csp)
	effIdx := effectiveScriptDirectiveIndex(scriptElemIdx, scriptIdx, defaultIdx)
	if effIdx < 0 {
		return false
	}
	_, needs := cspTokensRequireNonce(dirs[effIdx].tokens, httpPrefix)
	return needs
}

func rewriteSingleSidecarCSP(csp, httpPrefix, wsPrefix, shimNonce string) (rewritten string, usedNonce bool) {
	dirs, scriptElemIdx, scriptIdx, defaultIdx := parseCSPDirectives(csp)

	for i := range dirs {
		hasSelf := false
		for _, tok := range dirs[i].tokens {
			if strings.EqualFold(tok, "'self'") {
				hasSelf = true
				break
			}
		}
		if hasSelf && httpPrefix != "" {
			dirs[i].tokens = appendUniqueCSPToken(dirs[i].tokens, httpPrefix)
			if (dirs[i].name == "default-src" || dirs[i].name == "connect-src") && wsPrefix != "" {
				dirs[i].tokens = appendUniqueCSPToken(dirs[i].tokens, wsPrefix)
			}
		}
	}

	effIdx := effectiveScriptDirectiveIndex(scriptElemIdx, scriptIdx, defaultIdx)
	if effIdx >= 0 {
		toks := dirs[effIdx].tokens
		hasNone, needsNonce := cspTokensRequireNonce(toks, httpPrefix)
		if hasNone {
			if dirs[effIdx].name == "default-src" {
				// Preserve default-src 'none' for non-script directives while permitting
				// only the injected capability shim via an explicit script-src nonce.
				if shimNonce != "" {
					dirs = append(dirs, parsedCSPDirective{
						name:   "script-src",
						tokens: []string{"'nonce-" + shimNonce + "'"},
					})
					usedNonce = true
				} else if httpPrefix != "" {
					dirs = append(dirs, parsedCSPDirective{
						name:   "script-src",
						tokens: []string{httpPrefix},
					})
				}
			} else {
				filtered := make([]string, 0, len(toks)+1)
				for _, tok := range toks {
					if !strings.EqualFold(tok, "'none'") {
						filtered = append(filtered, tok)
					}
				}
				if shimNonce != "" {
					filtered = appendUniqueCSPToken(filtered, "'nonce-"+shimNonce+"'")
					usedNonce = true
				} else if httpPrefix != "" {
					filtered = appendUniqueCSPToken(filtered, httpPrefix)
				}
				dirs[effIdx].tokens = filtered
			}
		} else {
			if needsNonce && shimNonce != "" {
				toks = appendUniqueCSPToken(toks, "'nonce-"+shimNonce+"'")
				usedNonce = true
			}
			dirs[effIdx].tokens = toks
		}
	}

	out := make([]string, 0, len(dirs)+2)
	for _, d := range dirs {
		if len(d.tokens) == 0 {
			out = append(out, d.name)
		} else {
			out = append(out, d.name+" "+strings.Join(d.tokens, " "))
		}
	}
	out = append(out, sidecarSandboxDirective, sidecarFrameAncestors)
	return strings.Join(out, "; "), usedNonce
}

func appendUniqueCSPToken(tokens []string, candidate string) []string {
	for _, t := range tokens {
		if strings.EqualFold(t, candidate) {
			return tokens
		}
	}
	return append(tokens, candidate)
}

func rewriteSidecarLocation(loc string, port int, capPrefix string) string {
	loc = strings.TrimSpace(loc)
	if loc == "" {
		return loc
	}
	basePrefix := fmt.Sprintf("%s%d", SidecarPrefix, port)
	if strings.HasPrefix(loc, "/") && !strings.HasPrefix(loc, "//") {
		if loc == capPrefix || strings.HasPrefix(loc, capPrefix+"/") {
			return loc
		}
		if loc == basePrefix || strings.HasPrefix(loc, basePrefix+"/") {
			sub := strings.TrimPrefix(loc, basePrefix)
			if sub == "" {
				sub = "/"
			}
			if strings.HasPrefix(sub, "/"+sidecarCapSegment+"/") {
				return loc
			}
			return capPrefix + sub
		}
		if strings.HasPrefix(loc, "/__agy/") {
			return loc
		}
		return capPrefix + loc
	}
	u, err := url.Parse(loc)
	if err != nil {
		return loc
	}
	if p, ok := loopbackPortFromURL(loc); ok && p == port {
		ep := u.EscapedPath()
		if ep == "" {
			ep = "/"
		}
		out := capPrefix + ep
		if u.RawQuery != "" {
			out += "?" + u.RawQuery
		}
		if u.Fragment != "" {
			out += "#" + u.Fragment
		}
		return out
	}
	if !strings.Contains(loc, "://") && !hasURIScheme(loc) {
		return rewriteSidecarRelativeOrLoopbackURL(loc, port, capPrefix, "/")
	}
	return loc
}

func sidecarBaseDir(reqPath string) string {
	if reqPath == "" || !strings.HasPrefix(reqPath, "/") {
		return "/"
	}
	if strings.HasSuffix(reqPath, "/") {
		return reqPath
	}
	lastSlash := strings.LastIndexByte(reqPath, '/')
	if lastSlash <= 0 {
		return "/"
	}
	return reqPath[:lastSlash+1]
}

// rewriteSidecarHTML injects the external capability-scoped shim script and
// rewrites relative and root-relative URL attributes while leaving raw text
// elements (<script>, <title>, <textarea>, etc.) and comments untouched.
func rewriteSidecarHTML(body []byte, port int, capPrefix string, baseDir string, shimNonce string) []byte {
	shimTag := fmt.Sprintf(`<script src="%s/%s"></script>`, capPrefix, sidecarShimName)
	if shimNonce != "" {
		shimTag = fmt.Sprintf(`<script nonce="%s" src="%s/%s"></script>`, shimNonce, capPrefix, sidecarShimName)
	}

	var out bytes.Buffer
	out.Grow(len(body) + len(shimTag) + 256)

	injected := false
	n := len(body)
	i := 0

	for i < n {
		if body[i] != '<' {
			next := bytes.IndexByte(body[i:], '<')
			if next < 0 {
				out.Write(body[i:])
				break
			}
			out.Write(body[i : i+next])
			i += next
			continue
		}

		if i+4 <= n && bytes.Equal(body[i:i+4], []byte("<!--")) {
			end := bytes.Index(body[i+4:], []byte("-->"))
			if end < 0 {
				out.Write(body[i:])
				break
			}
			out.Write(body[i : i+4+end+3])
			i += 4 + end + 3
			continue
		}

		tagEnd := findHTMLTagEnd(body, i)
		if tagEnd < 0 {
			out.Write(body[i:])
			break
		}

		tagBytes := body[i : tagEnd+1]
		tagName, isClosing := parseHTMLTagName(tagBytes)

		if !isClosing && tagName != "" && !strings.HasPrefix(tagName, "!") && !strings.HasPrefix(tagName, "?") {
			out.Write(rewriteHTMLOpenTag(tagBytes, port, capPrefix, baseDir))
		} else {
			out.Write(tagBytes)
		}
		i = tagEnd + 1

		if !injected && !isClosing && tagName == "head" {
			out.WriteString(shimTag)
			injected = true
		}

		if !isClosing && isRawTextHTMLElement(tagName) && !strings.HasSuffix(strings.TrimSpace(string(tagBytes[:len(tagBytes)-1])), "/") {
			closeIdx := findRawTextCloseTag(body, i, tagName)
			if closeIdx < 0 {
				rawRest := body[i:]
				if tagName == "style" {
					out.Write(rewriteSidecarCSS(rawRest, port, capPrefix, baseDir))
				} else {
					out.Write(rawRest)
				}
				break
			}
			inner := body[i:closeIdx]
			if tagName == "style" {
				out.Write(rewriteSidecarCSS(inner, port, capPrefix, baseDir))
			} else {
				out.Write(inner)
			}
			i = closeIdx
		}
	}

	if !injected {
		rendered := out.Bytes()
		if insertAt := afterDoctypeOffset(rendered); insertAt > 0 {
			var combined bytes.Buffer
			combined.Grow(len(rendered) + len(shimTag))
			combined.Write(rendered[:insertAt])
			combined.WriteString(shimTag)
			combined.Write(rendered[insertAt:])
			return combined.Bytes()
		}
		var combined bytes.Buffer
		combined.Grow(len(rendered) + len(shimTag))
		combined.WriteString(shimTag)
		combined.Write(rendered)
		return combined.Bytes()
	}
	return out.Bytes()
}

func isRawTextHTMLElement(tagName string) bool {
	switch tagName {
	case "script", "style", "title", "textarea", "xmp", "noembed", "noframes":
		return true
	default:
		return false
	}
}

func afterDoctypeOffset(b []byte) int {
	i := 0
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\r' || b[i] == '\n') {
		i++
	}
	if i+9 <= len(b) && strings.EqualFold(string(b[i:i+9]), "<!doctype") {
		end := findHTMLTagEnd(b, i)
		if end >= 0 {
			return end + 1
		}
	}
	return 0
}

func findHTMLTagEnd(b []byte, start int) int {
	var quote byte
	for i := start + 1; i < len(b); i++ {
		c := b[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == '>' {
			return i
		}
	}
	return -1
}

func parseHTMLTagName(tag []byte) (name string, closing bool) {
	if len(tag) < 2 || tag[0] != '<' {
		return "", false
	}
	j := 1
	for j < len(tag) && (tag[j] == ' ' || tag[j] == '\t' || tag[j] == '\r' || tag[j] == '\n') {
		j++
	}
	if j < len(tag) && tag[j] == '/' {
		closing = true
		j++
	}
	start := j
	for j < len(tag) {
		c := tag[j]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '>' || c == '/' {
			break
		}
		j++
	}
	return strings.ToLower(string(tag[start:j])), closing
}

func findRawTextCloseTag(b []byte, from int, tagName string) int {
	needle := "</" + tagName
	nLen := len(needle)
	for i := from; i+nLen < len(b); i++ {
		if b[i] == '<' && b[i+1] == '/' && strings.EqualFold(string(b[i:i+nLen]), needle) {
			after := b[i+nLen]
			if after == '>' || after == ' ' || after == '\t' || after == '\r' || after == '\n' || after == '/' {
				return i
			}
		}
	}
	return -1
}

type htmlAttrToken struct {
	leadWS   []byte
	rawName  []byte
	nameLow  string
	hasValue bool
	eqAndWS  []byte
	quote    byte
	value    string
	closedQ  bool
}

func rewriteHTMLOpenTag(tag []byte, port int, capPrefix string, baseDir string) []byte {
	n := len(tag)
	i := 1
	for i < n && tag[i] != ' ' && tag[i] != '\t' && tag[i] != '\r' && tag[i] != '\n' && tag[i] != '>' && tag[i] != '/' {
		i++
	}
	header := tag[:i]

	var attrs []htmlAttrToken
	var tail []byte
	rewroteURL := false

	for i < n {
		wsStart := i
		for i < n && (tag[i] == ' ' || tag[i] == '\t' || tag[i] == '\r' || tag[i] == '\n') {
			i++
		}
		leadWS := tag[wsStart:i]
		if i >= n {
			tail = leadWS
			break
		}
		if tag[i] == '>' || (tag[i] == '/' && i+1 < n && tag[i+1] == '>') {
			tail = append(tail, leadWS...)
			tail = append(tail, tag[i:]...)
			break
		}

		attrStart := i
		for i < n && tag[i] != '=' && tag[i] != ' ' && tag[i] != '\t' && tag[i] != '\r' && tag[i] != '\n' && tag[i] != '>' && tag[i] != '/' {
			i++
		}
		rawName := tag[attrStart:i]
		nameLow := strings.ToLower(string(rawName))

		eqStart := i
		for i < n && (tag[i] == ' ' || tag[i] == '\t' || tag[i] == '\r' || tag[i] == '\n') {
			i++
		}
		if i >= n || tag[i] != '=' {
			attrs = append(attrs, htmlAttrToken{
				leadWS:  leadWS,
				rawName: rawName,
				nameLow: nameLow,
			})
			i = eqStart
			continue
		}
		i++ // '='
		for i < n && (tag[i] == ' ' || tag[i] == '\t' || tag[i] == '\r' || tag[i] == '\n') {
			i++
		}
		eqAndWS := tag[eqStart:i]
		if i >= n {
			attrs = append(attrs, htmlAttrToken{
				leadWS:   leadWS,
				rawName:  rawName,
				nameLow:  nameLow,
				hasValue: true,
				eqAndWS:  eqAndWS,
			})
			break
		}

		var quote byte
		closedQ := false
		valStart := i
		if tag[i] == '"' || tag[i] == '\'' {
			quote = tag[i]
			i++
			valStart = i
			for i < n && tag[i] != quote {
				i++
			}
			rawVal := string(tag[valStart:i])
			if i < n && tag[i] == quote {
				closedQ = true
				i++
			}
			newVal := rewriteTagAttrValue(nameLow, rawVal, port, capPrefix, baseDir)
			if newVal != rawVal || strings.HasPrefix(strings.TrimSpace(newVal), capPrefix+"/") {
				rewroteURL = true
			}
			attrs = append(attrs, htmlAttrToken{
				leadWS:   leadWS,
				rawName:  rawName,
				nameLow:  nameLow,
				hasValue: true,
				eqAndWS:  eqAndWS,
				quote:    quote,
				value:    newVal,
				closedQ:  closedQ,
			})
		} else {
			for i < n && tag[i] != ' ' && tag[i] != '\t' && tag[i] != '\r' && tag[i] != '\n' && tag[i] != '>' {
				i++
			}
			rawVal := string(tag[valStart:i])
			newVal := rewriteTagAttrValue(nameLow, rawVal, port, capPrefix, baseDir)
			if newVal != rawVal || strings.HasPrefix(strings.TrimSpace(newVal), capPrefix+"/") {
				rewroteURL = true
			}
			attrs = append(attrs, htmlAttrToken{
				leadWS:   leadWS,
				rawName:  rawName,
				nameLow:  nameLow,
				hasValue: true,
				eqAndWS:  eqAndWS,
				value:    newVal,
			})
		}
	}

	var out bytes.Buffer
	out.Grow(len(tag) + 64)
	out.Write(header)
	for _, a := range attrs {
		if rewroteURL && a.nameLow == "integrity" {
			continue
		}
		out.Write(a.leadWS)
		out.Write(a.rawName)
		if !a.hasValue {
			continue
		}
		out.Write(a.eqAndWS)
		if a.quote != 0 {
			out.WriteByte(a.quote)
			out.WriteString(a.value)
			if a.closedQ {
				out.WriteByte(a.quote)
			}
		} else {
			out.WriteString(a.value)
		}
	}
	out.Write(tail)
	return out.Bytes()
}

func rewriteTagAttrValue(attrName, rawVal string, port int, capPrefix string, baseDir string) string {
	switch attrName {
	case "src", "href", "action", "poster", "formaction", "data":
		return rewriteSidecarRelativeOrLoopbackURL(rawVal, port, capPrefix, baseDir)
	case "srcset", "imagesrcset":
		return rewriteSrcsetValue(rawVal, port, capPrefix, baseDir)
	case "style":
		return string(rewriteSidecarCSS([]byte(rawVal), port, capPrefix, baseDir))
	default:
		return rawVal
	}
}

func hasURIScheme(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ':' {
			return i > 0
		}
		if c == '/' || c == '?' || c == '#' {
			return false
		}
		if i == 0 {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
				return false
			}
		} else {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.') {
				return false
			}
		}
	}
	return false
}

func rewriteSidecarRelativeOrLoopbackURL(val string, port int, capPrefix string, baseDir string) string {
	trimmed := strings.TrimSpace(val)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
		return val
	}
	leadLen := len(val) - len(strings.TrimLeft(val, " \t\r\n"))
	trailLen := len(val) - len(strings.TrimRight(val, " \t\r\n"))
	lead := val[:leadLen]
	trail := ""
	if trailLen > 0 {
		trail = val[len(val)-trailLen:]
	}

	if hasURIScheme(trimmed) {
		if p, ok := loopbackPortFromURL(trimmed); ok && p == port {
			if u, err := url.Parse(trimmed); err == nil {
				ep := u.EscapedPath()
				if ep == "" {
					ep = "/"
				}
				out := capPrefix + ep
				if u.RawQuery != "" {
					out += "?" + u.RawQuery
				}
				if u.Fragment != "" {
					out += "#" + u.Fragment
				}
				return lead + out + trail
			}
		}
		return val
	}

	basePrefix := fmt.Sprintf("%s%d", SidecarPrefix, port)
	if strings.HasPrefix(trimmed, "/") {
		if trimmed == capPrefix || strings.HasPrefix(trimmed, capPrefix+"/") {
			return val
		}
		if trimmed == basePrefix || strings.HasPrefix(trimmed, basePrefix+"/") {
			sub := strings.TrimPrefix(trimmed, basePrefix)
			if sub == "" {
				sub = "/"
			}
			if strings.HasPrefix(sub, "/"+sidecarCapSegment+"/") {
				return val
			}
			return lead + capPrefix + sub + trail
		}
		if strings.HasPrefix(trimmed, "/__agy/") {
			return val
		}
		return lead + capPrefix + trimmed + trail
	}

	if baseDir == "" {
		baseDir = "/"
	}
	if strings.HasPrefix(trimmed, "?") {
		return lead + capPrefix + baseDir + trimmed + trail
	}

	pathPart := trimmed
	suffix := ""
	if cut := strings.IndexAny(trimmed, "?#"); cut >= 0 {
		pathPart = trimmed[:cut]
		suffix = trimmed[cut:]
	}
	trailingSlash := strings.HasSuffix(pathPart, "/")
	joined := path.Clean(path.Join(baseDir, pathPart))
	if !strings.HasPrefix(joined, "/") {
		joined = "/" + joined
	}
	if trailingSlash && joined != "/" {
		joined += "/"
	}
	return lead + capPrefix + joined + suffix + trail
}

func isSrcsetSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f'
}

func rewriteSrcsetValue(val string, port int, capPrefix string, baseDir string) string {
	var out strings.Builder
	out.Grow(len(val) + 64)
	n := len(val)
	i := 0
	for i < n {
		sepStart := i
		for i < n && (isSrcsetSpace(val[i]) || val[i] == ',') {
			i++
		}
		out.WriteString(val[sepStart:i])
		if i >= n {
			break
		}

		if i+5 <= n && strings.EqualFold(val[i:i+5], "data:") {
			dataStart := i
			commaIdx := strings.IndexByte(val[i:], ',')
			if commaIdx < 0 {
				out.WriteString(val[dataStart:])
				break
			}
			i += commaIdx + 1
			var quote byte
			parenDepth := 0
			inAngle := false
			for i < n {
				c := val[i]
				if quote != 0 {
					if c == quote {
						quote = 0
					}
					i++
					continue
				}
				if c == '"' || c == '\'' {
					quote = c
					i++
					continue
				}
				if c == '<' {
					inAngle = true
				} else if c == '>' {
					inAngle = false
				} else if c == '(' {
					parenDepth++
				} else if c == ')' && parenDepth > 0 {
					parenDepth--
				} else if c == ',' && parenDepth == 0 && !inAngle {
					break
				}
				i++
			}
			out.WriteString(val[dataStart:i])
			continue
		}

		urlStart := i
		for i < n && !isSrcsetSpace(val[i]) {
			i++
		}
		rawURL := val[urlStart:i]
		trailingCommas := 0
		for len(rawURL) > 0 && rawURL[len(rawURL)-1] == ',' {
			rawURL = rawURL[:len(rawURL)-1]
			trailingCommas++
		}
		out.WriteString(rewriteSidecarRelativeOrLoopbackURL(rawURL, port, capPrefix, baseDir))
		if trailingCommas > 0 {
			for k := 0; k < trailingCommas; k++ {
				out.WriteByte(',')
			}
			continue
		}

		descStart := i
		parenDepth := 0
		for i < n {
			c := val[i]
			if c == '(' {
				parenDepth++
			} else if c == ')' && parenDepth > 0 {
				parenDepth--
			} else if c == ',' && parenDepth == 0 {
				break
			}
			i++
		}
		out.WriteString(val[descStart:i])
	}
	return out.String()
}

func rewriteSidecarCSS(body []byte, port int, capPrefix string, baseDir string) []byte {
	var out bytes.Buffer
	out.Grow(len(body) + 128)

	n := len(body)
	i := 0
	for i < n {
		if i+1 < n && body[i] == '/' && body[i+1] == '*' {
			end := bytes.Index(body[i+2:], []byte("*/"))
			if end < 0 {
				out.Write(body[i:])
				break
			}
			out.Write(body[i : i+2+end+2])
			i += 2 + end + 2
			continue
		}

		if i+4 <= n && strings.EqualFold(string(body[i:i+4]), "url(") && (i == 0 || !isCSSIdentChar(body[i-1])) {
			out.Write(body[i : i+4])
			i += 4
			for i < n && (body[i] == ' ' || body[i] == '\t' || body[i] == '\r' || body[i] == '\n') {
				out.WriteByte(body[i])
				i++
			}
			if i >= n {
				break
			}
			if body[i] == '"' || body[i] == '\'' {
				q := body[i]
				out.WriteByte(q)
				i++
				start := i
				for i < n && body[i] != q {
					if body[i] == '\\' && i+1 < n {
						i += 2
						continue
					}
					i++
				}
				out.WriteString(rewriteSidecarRelativeOrLoopbackURL(string(body[start:i]), port, capPrefix, baseDir))
				if i < n && body[i] == q {
					out.WriteByte(q)
					i++
				}
			} else {
				start := i
				for i < n && body[i] != ')' && body[i] != ' ' && body[i] != '\t' && body[i] != '\r' && body[i] != '\n' {
					i++
				}
				out.WriteString(rewriteSidecarRelativeOrLoopbackURL(string(body[start:i]), port, capPrefix, baseDir))
			}
			continue
		}

		if i+7 <= n && strings.EqualFold(string(body[i:i+7]), "@import") {
			out.Write(body[i : i+7])
			i += 7
			wsStart := i
			for i < n && (body[i] == ' ' || body[i] == '\t' || body[i] == '\r' || body[i] == '\n') {
				i++
			}
			out.Write(body[wsStart:i])
			if i < n && (body[i] == '"' || body[i] == '\'') {
				q := body[i]
				out.WriteByte(q)
				i++
				start := i
				for i < n && body[i] != q {
					if body[i] == '\\' && i+1 < n {
						i += 2
						continue
					}
					i++
				}
				out.WriteString(rewriteSidecarRelativeOrLoopbackURL(string(body[start:i]), port, capPrefix, baseDir))
				if i < n && body[i] == q {
					out.WriteByte(q)
					i++
				}
			}
			continue
		}

		if body[i] == '"' || body[i] == '\'' {
			q := body[i]
			start := i
			i++
			for i < n && body[i] != q {
				if body[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				i++
			}
			if i < n && body[i] == q {
				i++
			}
			out.Write(body[start:i])
			continue
		}

		out.WriteByte(body[i])
		i++
	}
	return out.Bytes()
}

func isCSSIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
}

func isValidWebSocketHandshake(r *http.Request) bool {
	if r == nil || r.Method != http.MethodGet {
		return false
	}
	if r.ContentLength > 0 || len(r.TransferEncoding) > 0 {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return false
	}
	if !headerContainsToken(r.Header, "Connection", "upgrade") {
		return false
	}
	if strings.TrimSpace(r.Header.Get("Sec-WebSocket-Version")) != "13" {
		return false
	}
	if strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key")) == "" {
		return false
	}
	return true
}

func headerContainsToken(h http.Header, headerName, wantToken string) bool {
	for _, line := range h.Values(headerName) {
		for _, tok := range strings.Split(line, ",") {
			if strings.EqualFold(strings.TrimSpace(tok), wantToken) {
				return true
			}
		}
	}
	return false
}

func stripHopByHopHeaders(h http.Header) {
	for _, k := range []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"TE",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	} {
		h.Del(k)
	}
}

func setTCPKeepAlive(conn net.Conn, period time.Duration) {
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(period)
	}
}

type halfCloseConn interface {
	CloseWrite() error
}

func (p *Proxy) proxySidecarWebSocket(w http.ResponseWriter, r *http.Request, port int, dialAddr string, capPrefix string, capSess *sidecarCapSession, decodedPath, rawPath string) {
	if !isValidWebSocketHandshake(r) {
		http.Error(w, "invalid websocket upgrade request", http.StatusBadRequest)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "websocket hijack unsupported", http.StatusInternalServerError)
		return
	}

	upConn, err := dialSidecarAddress(r.Context(), "tcp", dialAddr)
	if err != nil {
		http.Error(w, "sidecar websocket unreachable", http.StatusBadGateway)
		return
	}

	upstreamHost := fmt.Sprintf("localhost:%d", port)
	upstreamOrigin := "http://" + upstreamHost

	outReq := r.Clone(r.Context())
	outReq.Method = http.MethodGet
	outReq.Body = nil
	outReq.GetBody = nil
	outReq.ContentLength = 0
	outReq.TransferEncoding = nil
	outReq.URL.Scheme = "http"
	outReq.URL.Host = upstreamHost
	outReq.URL.Path = decodedPath
	outReq.URL.RawPath = rawPath
	outReq.Host = upstreamHost
	outReq.RequestURI = ""
	prepareSidecarUpstreamHeaders(outReq.Header, upstreamOrigin, capSess.cookieHeader())
	outReq.Header["X-Forwarded-For"] = nil
	outReq.Header.Set("Connection", "Upgrade")
	outReq.Header.Set("Upgrade", "websocket")

	_ = upConn.SetWriteDeadline(time.Now().Add(sidecarDialTimeout))
	if err := outReq.Write(upConn); err != nil {
		_ = upConn.Close()
		http.Error(w, "sidecar websocket handshake write failed", http.StatusBadGateway)
		return
	}
	_ = upConn.SetWriteDeadline(time.Time{})

	upReader := bufio.NewReader(upConn)
	_ = upConn.SetReadDeadline(time.Now().Add(15 * time.Second))
	resp, err := http.ReadResponse(upReader, outReq)
	_ = upConn.SetReadDeadline(time.Time{})
	if err != nil {
		_ = upConn.Close()
		http.Error(w, "sidecar websocket handshake read failed", http.StatusBadGateway)
		return
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		defer upConn.Close()
		defer resp.Body.Close()
		stripHopByHopHeaders(resp.Header)
		if err := p.modifySidecarResponse(resp, r, port, capPrefix, capSess, nil); err != nil {
			http.Error(w, "sidecar websocket handshake rejected", http.StatusBadGateway)
			return
		}
		bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, sidecarMaxWSRejectBody+1))
		if err != nil || int64(len(bodyBytes)) > sidecarMaxWSRejectBody {
			http.Error(w, "sidecar websocket handshake response too large", http.StatusBadGateway)
			return
		}
		for k, vv := range resp.Header {
			if strings.EqualFold(k, "Content-Length") {
				continue
			}
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(bodyBytes)))
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(bodyBytes)
		return
	}

	if rawCookies := resp.Header.Values("Set-Cookie"); len(rawCookies) > 0 {
		capSess.storeSetCookies(rawCookies)
		resp.Header.Del("Set-Cookie")
	}

	clientConn, clientBuf, err := hj.Hijack()
	if err != nil {
		_ = upConn.Close()
		return
	}
	defer clientConn.Close()
	defer upConn.Close()

	setTCPKeepAlive(clientConn, 30*time.Second)
	setTCPKeepAlive(upConn, 30*time.Second)

	stopWatch := make(chan struct{})
	defer close(stopWatch)
	if p.closed != nil {
		go func() {
			select {
			case <-p.closed:
				_ = clientConn.Close()
				_ = upConn.Close()
			case <-stopWatch:
			}
		}()
	}

	var hs bytes.Buffer
	fmt.Fprintf(&hs, "HTTP/1.1 %s\r\n", resp.Status)
	_ = resp.Header.Write(&hs)
	hs.WriteString("\r\n")
	_ = clientConn.SetWriteDeadline(time.Now().Add(sidecarWSWriteTimeout))
	if _, err := clientConn.Write(hs.Bytes()); err != nil {
		return
	}

	if upBuffered := upReader.Buffered(); upBuffered > 0 {
		if _, err := io.CopyN(clientConn, upReader, int64(upBuffered)); err != nil {
			return
		}
	}
	if clientBuf != nil && clientBuf.Reader.Buffered() > 0 {
		_ = upConn.SetWriteDeadline(time.Now().Add(sidecarWSWriteTimeout))
		if _, err := io.CopyN(upConn, clientBuf.Reader, int64(clientBuf.Reader.Buffered())); err != nil {
			return
		}
	}
	_ = clientConn.SetWriteDeadline(time.Time{})
	_ = upConn.SetWriteDeadline(time.Time{})

	var wg sync.WaitGroup
	wg.Add(2)
	pipeConn := func(dst, src net.Conn) {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			_ = src.SetReadDeadline(time.Now().Add(sidecarWSIdleTimeout))
			n, rErr := src.Read(buf)
			if n > 0 {
				p.touchActivity()
				_ = dst.SetWriteDeadline(time.Now().Add(sidecarWSWriteTimeout))
				if _, wErr := dst.Write(buf[:n]); wErr != nil {
					_ = src.Close()
					_ = dst.Close()
					return
				}
			}
			if rErr != nil {
				if hc, ok := dst.(halfCloseConn); ok && errors.Is(rErr, io.EOF) {
					_ = hc.CloseWrite()
					_ = dst.SetReadDeadline(time.Now().Add(sidecarWSCloseGrace))
				} else {
					_ = dst.Close()
				}
				return
			}
		}
	}

	go pipeConn(upConn, clientConn)
	go pipeConn(clientConn, upConn)
	wg.Wait()
}

func sidecarShimJS(port int, capPrefix string) string {
	return `(function(){
  var PORT = ` + strconv.Itoa(port) + `;
  var BASE_PREFIX = "` + SidecarPrefix + `" + PORT;
  var PREFIX = ` + strconv.Quote(capPrefix) + `;
  if (window.__agySidecarShimPrefix === PREFIX) return;
  window.__agySidecarShimPrefix = PREFIX;

  function makeMemoryStorage() {
    var store = Object.create(null);
    return {
      get length() { return Object.keys(store).length; },
      key: function(i) { var k = Object.keys(store); return i >= 0 && i < k.length ? k[i] : null; },
      getItem: function(k) { k = String(k); return Object.prototype.hasOwnProperty.call(store, k) ? store[k] : null; },
      setItem: function(k, v) { store[String(k)] = String(v); },
      removeItem: function(k) { delete store[String(k)]; },
      clear: function() { store = Object.create(null); }
    };
  }
  ["localStorage", "sessionStorage"].forEach(function(prop) {
    try { void window[prop]; } catch (e) {
      try { Object.defineProperty(window, prop, { value: makeMemoryStorage(), configurable: true }); } catch (e2) {}
    }
  });

  function isNonNetworkScheme(s) {
    return /^(?:data|blob|javascript|mailto|tel|about):/i.test(s);
  }

  function mapSidecarPath(p) {
    if (!p || p.charAt(0) !== "/") p = "/" + (p || "");
    if (p === PREFIX || p.indexOf(PREFIX + "/") === 0) return p;
    if (p === BASE_PREFIX || p.indexOf(BASE_PREFIX + "/") === 0) {
      var sub = p.slice(BASE_PREFIX.length) || "/";
      if (sub.indexOf("/` + sidecarCapSegment + `/") === 0) return p;
      return PREFIX + sub;
    }
    if (p.indexOf("/__agy/") === 0) return p;
    return PREFIX + p;
  }

  function rewriteStr(u, forWS) {
    if (typeof u !== "string" || !u) return u;
    var trimmed = u.trim();
    if (!trimmed || trimmed.charAt(0) === "#" || isNonNetworkScheme(trimmed)) return u;

    var wsOrigin = (location.protocol === "https:" ? "wss://" : "ws://") + location.host;
    var httpOrigin = location.protocol + "//" + location.host;

    if (trimmed.charAt(0) === "/" && trimmed.charAt(1) !== "/") {
      var mappedRoot = mapSidecarPath(trimmed);
      if (mappedRoot.indexOf("/__agy/") === 0 && mappedRoot !== PREFIX && mappedRoot.indexOf(PREFIX + "/") !== 0) {
        return forWS ? (wsOrigin + mappedRoot) : u;
      }
      return forWS ? (wsOrigin + mappedRoot) : mappedRoot;
    }

    try {
      var parsed = new URL(trimmed, location.href);
      var protoLow = parsed.protocol.toLowerCase();
      if (protoLow !== "http:" && protoLow !== "https:" && protoLow !== "ws:" && protoLow !== "wss:") {
        return u;
      }
      var h = parsed.hostname.toLowerCase();
      var isLoop = (h === "localhost" || h === "127.0.0.1" || h === "[::1]" || h === "::1") && parsed.port === String(PORT);
      var isSameSidecar = parsed.host === location.host && (parsed.pathname === BASE_PREFIX || parsed.pathname.indexOf(BASE_PREFIX + "/") === 0);
      var isSameRoot = parsed.host === location.host && parsed.pathname.indexOf("/__agy/") !== 0;
      if (isLoop || isSameSidecar || isSameRoot) {
        var nextPath = mapSidecarPath(parsed.pathname || "/");
        var outOrigin = forWS ? wsOrigin : httpOrigin;
        return outOrigin + nextPath + parsed.search + parsed.hash;
      }
    } catch (e) {}
    return u;
  }

  function rewriteAny(u) {
    if (typeof u === "string") return rewriteStr(u, false);
    if (typeof URL !== "undefined" && u instanceof URL) {
      var s = rewriteStr(u.href, false);
      if (s !== u.href) { try { return new URL(s, location.href); } catch (e) {} }
    }
    return u;
  }

  if (typeof window.Request === "function") {
    var OrigRequest = window.Request;
    var PatchedRequest = function(input, init) {
      if (typeof input === "string" || (typeof URL !== "undefined" && input instanceof URL)) {
        input = rewriteAny(input);
      } else if (input instanceof OrigRequest) {
        var next = rewriteStr(input.url, false);
        if (next !== input.url) input = new OrigRequest(next, input);
      }
      return arguments.length > 1 ? new OrigRequest(input, init) : new OrigRequest(input);
    };
    PatchedRequest.prototype = OrigRequest.prototype;
    try { Object.setPrototypeOf(PatchedRequest, OrigRequest); } catch (e) {}
    window.Request = PatchedRequest;
  }

  if (typeof window.fetch === "function") {
    var origFetch = window.fetch;
    window.fetch = function(input, init) {
      try {
        if (typeof input === "string" || (typeof URL !== "undefined" && input instanceof URL)) {
          input = rewriteAny(input);
        } else if (typeof Request !== "undefined" && input instanceof Request) {
          var next = rewriteStr(input.url, false);
          if (next !== input.url) input = new Request(next, input);
        }
      } catch (e) {}
      return origFetch.call(this, input, init);
    };
  }

  if (typeof navigator !== "undefined" && typeof navigator.sendBeacon === "function") {
    var origBeacon = navigator.sendBeacon;
    navigator.sendBeacon = function(url, data) {
      var next = rewriteAny(url);
      return arguments.length > 1 ? origBeacon.call(this, next, data) : origBeacon.call(this, next);
    };
  }

  if (typeof XMLHttpRequest !== "undefined" && XMLHttpRequest.prototype && XMLHttpRequest.prototype.open) {
    var origOpen = XMLHttpRequest.prototype.open;
    XMLHttpRequest.prototype.open = function(method, url) {
      var args = Array.prototype.slice.call(arguments);
      if (args.length > 1) args[1] = rewriteAny(args[1]);
      return origOpen.apply(this, args);
    };
  }

  if (typeof window.EventSource === "function") {
    var OrigES = window.EventSource;
    var PatchedES = function(url, cfg) {
      return arguments.length > 1 ? new OrigES(rewriteAny(url), cfg) : new OrigES(rewriteAny(url));
    };
    PatchedES.prototype = OrigES.prototype;
    PatchedES.CONNECTING = OrigES.CONNECTING;
    PatchedES.OPEN = OrigES.OPEN;
    PatchedES.CLOSED = OrigES.CLOSED;
    window.EventSource = PatchedES;
  }

  if (typeof window.WebSocket === "function") {
    var OrigWS = window.WebSocket;
    var PatchedWS = function(url, protocols) {
      var next = typeof url === "string" ? rewriteStr(url, true) : (url && url.href ? rewriteStr(url.href, true) : url);
      return protocols === undefined ? new OrigWS(next) : new OrigWS(next, protocols);
    };
    PatchedWS.prototype = OrigWS.prototype;
    PatchedWS.CONNECTING = 0;
    PatchedWS.OPEN = 1;
    PatchedWS.CLOSING = 2;
    PatchedWS.CLOSED = 3;
    window.WebSocket = PatchedWS;
  }

  function isSrcsetWS(c) {
    return c === " " || c === "\t" || c === "\r" || c === "\n" || c === "\f";
  }

  function rewriteSrcset(val) {
    if (typeof val !== "string" || !val) return val;
    var out = "";
    var n = val.length;
    var i = 0;
    while (i < n) {
      var sepStart = i;
      while (i < n && (isSrcsetWS(val.charAt(i)) || val.charAt(i) === ",")) i++;
      out += val.slice(sepStart, i);
      if (i >= n) break;
      if (val.slice(i, i + 5).toLowerCase() === "data:") {
        var dataStart = i;
        var commaIdx = val.indexOf(",", i);
        if (commaIdx < 0) {
          out += val.slice(dataStart);
          break;
        }
        i = commaIdx + 1;
        var quote = "";
        var pDepth = 0;
        var inAngle = false;
        while (i < n) {
          var dc = val.charAt(i);
          if (quote) {
            if (dc === quote) quote = "";
            i++;
            continue;
          }
          if (dc === "\"" || dc === "'") {
            quote = dc;
            i++;
            continue;
          }
          if (dc === "<") inAngle = true;
          else if (dc === ">") inAngle = false;
          else if (dc === "(") pDepth++;
          else if (dc === ")" && pDepth > 0) pDepth--;
          else if (dc === "," && pDepth === 0 && !inAngle) break;
          i++;
        }
        out += val.slice(dataStart, i);
        continue;
      }
      var urlStart = i;
      while (i < n && !isSrcsetWS(val.charAt(i))) i++;
      var rawUrl = val.slice(urlStart, i);
      var commas = "";
      while (rawUrl.length > 0 && rawUrl.charAt(rawUrl.length - 1) === ",") {
        rawUrl = rawUrl.slice(0, -1);
        commas += ",";
      }
      out += rewriteStr(rawUrl, false);
      if (commas) {
        out += commas;
        continue;
      }
      var descStart = i;
      var depth = 0;
      while (i < n) {
        var ch = val.charAt(i);
        if (ch === "(") depth++;
        else if (ch === ")" && depth > 0) depth--;
        else if (ch === "," && depth === 0) break;
        i++;
      }
      out += val.slice(descStart, i);
    }
    return out;
  }

  function isCSSIdent(c) {
    return (c >= "a" && c <= "z") || (c >= "A" && c <= "Z") || (c >= "0" && c <= "9") || c === "-" || c === "_";
  }

  function rewriteCSSText(css) {
    if (typeof css !== "string" || !css || !/url\(|@import/i.test(css)) return css;
    var out = "";
    var n = css.length;
    var i = 0;
    while (i < n) {
      if (i + 1 < n && css.charAt(i) === "/" && css.charAt(i + 1) === "*") {
        var cEnd = css.indexOf("*/", i + 2);
        if (cEnd < 0) { out += css.slice(i); break; }
        out += css.slice(i, cEnd + 2);
        i = cEnd + 2;
        continue;
      }
      if (i + 4 <= n && css.slice(i, i + 4).toLowerCase() === "url(" && (i === 0 || !isCSSIdent(css.charAt(i - 1)))) {
        out += css.slice(i, i + 4);
        i += 4;
        while (i < n && isSrcsetWS(css.charAt(i))) { out += css.charAt(i); i++; }
        if (i >= n) break;
        var q = css.charAt(i);
        if (q === "\"" || q === "'") {
          out += q;
          i++;
          var qs = i;
          while (i < n && css.charAt(i) !== q) {
            if (css.charAt(i) === "\\" && i + 1 < n) { i += 2; continue; }
            i++;
          }
          out += rewriteStr(css.slice(qs, i), false);
          if (i < n && css.charAt(i) === q) { out += q; i++; }
        } else {
          var us = i;
          while (i < n && css.charAt(i) !== ")" && !isSrcsetWS(css.charAt(i))) i++;
          out += rewriteStr(css.slice(us, i), false);
        }
        continue;
      }
      if (i + 7 <= n && css.slice(i, i + 7).toLowerCase() === "@import") {
        out += css.slice(i, i + 7);
        i += 7;
        while (i < n && isSrcsetWS(css.charAt(i))) { out += css.charAt(i); i++; }
        var iq = i < n ? css.charAt(i) : "";
        if (iq === "\"" || iq === "'") {
          out += iq;
          i++;
          var iqs = i;
          while (i < n && css.charAt(i) !== iq) {
            if (css.charAt(i) === "\\" && i + 1 < n) { i += 2; continue; }
            i++;
          }
          out += rewriteStr(css.slice(iqs, i), false);
          if (i < n && css.charAt(i) === iq) { out += iq; i++; }
        }
        continue;
      }
      var ch = css.charAt(i);
      if (ch === "\"" || ch === "'") {
        var litStart = i;
        i++;
        while (i < n && css.charAt(i) !== ch) {
          if (css.charAt(i) === "\\" && i + 1 < n) { i += 2; continue; }
          i++;
        }
        if (i < n && css.charAt(i) === ch) i++;
        out += css.slice(litStart, i);
        continue;
      }
      out += ch;
      i++;
    }
    return out;
  }

  if (typeof Element !== "undefined" && Element.prototype && Element.prototype.setAttribute) {
    var origSetAttr = Element.prototype.setAttribute;
    Element.prototype.setAttribute = function(name, value) {
      if (typeof name === "string" && typeof value === "string") {
        var low = name.toLowerCase();
        if (low === "src" || low === "href" || low === "action" || low === "poster" || low === "formaction" || low === "data") {
          value = rewriteStr(value, false);
        } else if (low === "srcset" || low === "imagesrcset") {
          value = rewriteSrcset(value);
        } else if (low === "style") {
          value = rewriteCSSText(value);
        }
      }
      return origSetAttr.call(this, name, value);
    };
  }

  function wrapProp(Ctor, prop, transform) {
    if (!Ctor || !Ctor.prototype) return;
    var desc = Object.getOwnPropertyDescriptor(Ctor.prototype, prop);
    if (!desc || typeof desc.set !== "function") return;
    var fn = transform || function(v) { return rewriteStr(v, false); };
    Object.defineProperty(Ctor.prototype, prop, {
      configurable: true,
      enumerable: desc.enumerable,
      get: desc.get,
      set: function(v) {
        return desc.set.call(this, typeof v === "string" ? fn(v) : v);
      }
    });
  }
  try {
    wrapProp(window.HTMLImageElement, "src");
    wrapProp(window.HTMLImageElement, "srcset", rewriteSrcset);
    wrapProp(window.HTMLSourceElement, "src");
    wrapProp(window.HTMLSourceElement, "srcset", rewriteSrcset);
    wrapProp(window.HTMLScriptElement, "src");
    wrapProp(window.HTMLLinkElement, "href");
    wrapProp(window.HTMLLinkElement, "imageSrcset", rewriteSrcset);
    wrapProp(window.HTMLAnchorElement, "href");
    wrapProp(window.HTMLFormElement, "action");
    wrapProp(window.HTMLButtonElement, "formAction");
    wrapProp(window.HTMLInputElement, "src");
    wrapProp(window.HTMLInputElement, "formAction");
    wrapProp(window.HTMLIFrameElement, "src");
    wrapProp(window.HTMLMediaElement, "src");
    wrapProp(window.HTMLVideoElement, "poster");
    wrapProp(window.HTMLObjectElement, "data");
  } catch (e) {}

  try {
    if (window.CSSStyleDeclaration && window.CSSStyleDeclaration.prototype) {
      var styleProto = window.CSSStyleDeclaration.prototype;
      if (typeof styleProto.setProperty === "function") {
        var origSetProp = styleProto.setProperty;
        styleProto.setProperty = function(prop, val, prio) {
          if (typeof val === "string") val = rewriteCSSText(val);
          return arguments.length > 2 ? origSetProp.call(this, prop, val, prio) : origSetProp.call(this, prop, val);
        };
      }
      wrapProp(window.CSSStyleDeclaration, "cssText", rewriteCSSText);
      ["background", "backgroundImage", "mask", "maskImage", "webkitMask", "webkitMaskImage", "listStyleImage", "borderImage", "borderImageSource", "cursor"].forEach(function(p) {
        wrapProp(window.CSSStyleDeclaration, p, rewriteCSSText);
      });
    }
    if (window.CSSStyleSheet && window.CSSStyleSheet.prototype) {
      var sheetProto = window.CSSStyleSheet.prototype;
      if (typeof sheetProto.insertRule === "function") {
        var origInsertRule = sheetProto.insertRule;
        sheetProto.insertRule = function(rule, index) {
          if (typeof rule === "string") rule = rewriteCSSText(rule);
          return arguments.length > 1 ? origInsertRule.call(this, rule, index) : origInsertRule.call(this, rule);
        };
      }
      ["replace", "replaceSync"].forEach(function(m) {
        if (typeof sheetProto[m] !== "function") return;
        var origReplace = sheetProto[m];
        sheetProto[m] = function(text) {
          if (typeof text === "string") text = rewriteCSSText(text);
          return origReplace.call(this, text);
        };
      });
    }
    if (window.Node && window.Node.prototype) {
      var tcDesc = Object.getOwnPropertyDescriptor(window.Node.prototype, "textContent");
      if (tcDesc && typeof tcDesc.set === "function") {
        Object.defineProperty(window.Node.prototype, "textContent", {
          configurable: true,
          enumerable: tcDesc.enumerable,
          get: tcDesc.get,
          set: function(v) {
            if (this && this.nodeType === 1 && this.tagName === "STYLE" && typeof v === "string") {
              v = rewriteCSSText(v);
            }
            return tcDesc.set.call(this, v);
          }
        });
      }
      ["appendChild", "insertBefore"].forEach(function(fn) {
        var origNodeFn = window.Node.prototype[fn];
        if (typeof origNodeFn !== "function") return;
        window.Node.prototype[fn] = function(child) {
          if (this && this.nodeType === 1 && this.tagName === "STYLE" && child && child.nodeType === 3 && typeof child.nodeValue === "string") {
            child.nodeValue = rewriteCSSText(child.nodeValue);
          }
          return origNodeFn.apply(this, arguments);
        };
      });
    }
    if (window.Element && window.Element.prototype) {
      var ihDesc = Object.getOwnPropertyDescriptor(window.Element.prototype, "innerHTML");
      if (ihDesc && typeof ihDesc.set === "function") {
        Object.defineProperty(window.Element.prototype, "innerHTML", {
          configurable: true,
          enumerable: ihDesc.enumerable,
          get: ihDesc.get,
          set: function(v) {
            if (this && this.tagName === "STYLE" && typeof v === "string") {
              v = rewriteCSSText(v);
            }
            return ihDesc.set.call(this, v);
          }
        });
      }
    }
  } catch (e) {}

  if (window.history) {
    ["pushState", "replaceState"].forEach(function(fn) {
      var orig = window.history[fn];
      if (typeof orig !== "function") return;
      window.history[fn] = function(state, title, url) {
        var next = url;
        if (typeof url === "string") {
          next = rewriteStr(url, false);
        } else if (typeof URL !== "undefined" && url instanceof URL) {
          next = rewriteAny(url);
        }
        try {
          return orig.call(this, state, title, next);
        } catch (e) {
          try { return orig.call(this, state, title, url); } catch (e2) {}
        }
      };
    });
  }
})();`
}
