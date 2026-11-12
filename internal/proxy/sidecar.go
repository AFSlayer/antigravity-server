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

	sidecarShimName            = "__agy_sidecar_shim.js"
	sidecarCapSegment          = "~s"
	sidecarCapHexLen           = 32
	sidecarCapIdleTTL          = 2 * time.Hour
	sidecarCapMaxLifetime      = 12 * time.Hour
	sidecarCapMaxEntries       = 256
	sidecarVaultMaxCookies     = 64
	sidecarVaultMaxCookieBytes = 8192

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

	cookieMu sync.Mutex
	cookies  map[string]string
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
		DialContext:           dialLoopbackOnly,
	}
}

func dialLoopbackOnly(ctx context.Context, network, addr string) (net.Conn, error) {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return nil, fmt.Errorf("invalid loopback port %q", portStr)
	}

	dialer := &net.Dialer{Timeout: sidecarDialTimeout}
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

func (p *Proxy) isAllowedSidecarPort(ctx context.Context, port int) bool {
	if p.isForbiddenPort(port) {
		return false
	}
	now := time.Now()
	p.sidecarMu.Lock()
	if !p.sidecarCachedAt.IsZero() && now.Sub(p.sidecarCachedAt) < sidecarCacheTTL && p.sidecarLastErr == nil {
		if _, ok := p.sidecarAllowed[port]; ok {
			p.sidecarMu.Unlock()
			return true
		}
		if !p.sidecarLastProbe.IsZero() && now.Sub(p.sidecarLastProbe) < sidecarMissRefreshMin {
			p.sidecarMu.Unlock()
			return false
		}
	}
	p.sidecarMu.Unlock()

	allowed, _, err := p.refreshSidecarPorts(ctx, true, now)
	if err != nil {
		return false
	}
	_, ok := allowed[port]
	return ok
}

func (p *Proxy) refreshSidecarPorts(ctx context.Context, force bool, minStart time.Time) (allowed, reserved map[int]struct{}, err error) {
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

func (p *Proxy) fetchSidecarPorts(parent context.Context) (allowed, reserved map[int]struct{}, err error) {
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

	allowed = make(map[int]struct{})
	reserved = make(map[int]struct{})

	for _, sc := range payload.Sidecars {
		webPort := parseJSONPort(sc.WebPort)
		if webPort > 0 && webPort <= 65535 {
			reserved[webPort] = struct{}{}
		}
		if gwPort, ok := loopbackPortFromURL(sc.BaseURL); ok && !p.isForbiddenPort(gwPort) {
			reserved[gwPort] = struct{}{}
			allowed[gwPort] = struct{}{}
			continue
		}
		if sc.BaseURL == "" && !p.isForbiddenPort(webPort) && sidecarStatusRunning(sc.Status) {
			allowed[webPort] = struct{}{}
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

func loopbackPortFromURL(raw string) (int, bool) {
	if raw == "" {
		return 0, false
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "http") {
		return 0, false
	}
	host := strings.ToLower(u.Hostname())
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return 0, false
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 || port > 65535 {
		return 0, false
	}
	return port, true
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

	for _, existing := range p.sidecarCaps {
		if existing.port == port && existing.parentSession == parentSession {
			existing.lastUsedAt = now
			return existing, nil
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
	sess := &sidecarCapSession{
		token:         tok,
		port:          port,
		parentSession: parentSession,
		createdAt:     now,
		lastUsedAt:    now,
		cookies:       make(map[string]string),
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
	return p.authorizeSidecarCapRequest(r, port, capToken) != nil
}

func (p *Proxy) authorizeSidecarCapRequest(r *http.Request, port int, capToken string) *sidecarCapSession {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if isWebSocketUpgrade(r) {
		if origin != "null" && !p.sameOriginUpgrade(r) {
			return nil
		}
	} else {
		if origin != "" && origin != "null" && !p.sameOriginRequest(r) {
			return nil
		}
	}

	capSess := p.lookupSidecarCap(capToken, port)
	if capSess == nil {
		return nil
	}
	if p.opts.ValidateSession != nil {
		if capSess.parentSession == "" || !p.opts.ValidateSession(capSess.parentSession) {
			p.deleteSidecarCap(capToken)
			return nil
		}
	}
	if !p.isAllowedSidecarPort(r.Context(), port) {
		return nil
	}
	return capSess
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

	var capSess *sidecarCapSession
	if capToken == "" {
		if isWebSocketUpgrade(r) || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			http.Error(w, "sidecar capability required", http.StatusForbidden)
			return
		}
		if sfs := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))); sfs == "cross-site" || sfs == "same-site" {
			http.Error(w, "cross-site sidecar navigation rejected", http.StatusForbidden)
			return
		}
		if orig := strings.TrimSpace(r.Header.Get("Origin")); orig != "" && !p.sameOriginRequest(r) {
			http.Error(w, "cross-origin sidecar request rejected", http.StatusForbidden)
			return
		}
		if ref := strings.TrimSpace(r.Header.Get("Referer")); ref != "" {
			refURL, err := url.Parse(ref)
			if err != nil || (refURL.Host != "" && !p.sameRefererHost(r, refURL.Host)) {
				http.Error(w, "cross-origin sidecar referer rejected", http.StatusForbidden)
				return
			}
		}

		parentSession := ""
		if c, err := r.Cookie(auth.CookieName); err == nil {
			parentSession = c.Value
		}
		if p.opts.ValidateSession != nil && (parentSession == "" || !p.opts.ValidateSession(parentSession)) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !p.isAllowedSidecarPort(r.Context(), port) {
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
	} else {
		capSess = p.authorizeSidecarCapRequest(r, port, capToken)
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
		p.proxySidecarWebSocket(w, r, port, capPrefix, capSess, decodedRest, restEscaped)
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
			req.URL.Host = fmt.Sprintf("127.0.0.1:%d", port)
			req.URL.Path = decodedRest
			req.URL.RawPath = restEscaped
			req.Host = upstreamHost

			prepareSidecarUpstreamHeaders(req.Header, port, capPrefix, upstreamOrigin, vaultCookie)
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

func (p *Proxy) redirectSidecarOrphanRequest(w http.ResponseWriter, r *http.Request) bool {
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Dest")), "document") {
		return false
	}
	path := r.URL.Path
	if path == "/" ||
		strings.HasPrefix(path, "/__agy/") ||
		strings.HasPrefix(path, "/exa.") ||
		path == "/connect-websocket" ||
		path == "/main.js" ||
		path == "/favicon.ico" ||
		path == "/apple-touch-icon.png" ||
		isParentSPARoute(path) {
		return false
	}

	ref := strings.TrimSpace(r.Header.Get("Referer"))
	if ref == "" {
		return false
	}
	refURL, err := url.Parse(ref)
	if err != nil {
		return false
	}
	if refURL.Host != "" && !p.sameRefererHostWithScheme(r, refURL.Host, refURL.Scheme) {
		return false
	}
	port, capToken, _, _, invalidCap, ok := parseSidecarRoute(refURL.EscapedPath())
	if !ok || invalidCap {
		return false
	}

	var targetPrefix string
	if capToken != "" {
		if p.authorizeSidecarCapRequest(r, port, capToken) == nil {
			return false
		}
		targetPrefix = sidecarCapPrefix(port, capToken)
	} else {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			return false
		}
		if !p.isAllowedSidecarPort(r.Context(), port) {
			return false
		}
		targetPrefix = fmt.Sprintf("%s%d", SidecarPrefix, port)
	}

	target := targetPrefix + r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	applySidecarCORSHeaders(w.Header(), r)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	return true
}

func isParentSPARoute(p string) bool {
	for _, prefix := range []string{"/onboarding", "/c", "/customizations", "/settings", "/login", "/signin", "/workspace", "/projects"} {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

func (p *Proxy) sameRefererHost(r *http.Request, refHost string) bool {
	scheme := "http"
	if auth.RequestIsSecure(r, p.opts.TrustedProxies) {
		scheme = "https"
	}
	return p.sameRefererHostWithScheme(r, refHost, scheme)
}

func (p *Proxy) sameRefererHostWithScheme(r *http.Request, refHost, refScheme string) bool {
	reqScheme := "http"
	if auth.RequestIsSecure(r, p.opts.TrustedProxies) {
		reqScheme = "https"
	}
	if refScheme == "" {
		refScheme = reqScheme
	}
	refNorm := normalizeAuthorityHostPort(refHost, refScheme)
	if refNorm == "" {
		return false
	}
	if fwd := p.trustedForwardedHost(r); fwd != "" && refNorm == normalizeAuthorityHostPort(fwd, reqScheme) {
		return true
	}
	if reqHost := auth.SanitizeHostPort(r.Host); reqHost != "" && refNorm == normalizeAuthorityHostPort(reqHost, reqScheme) {
		return true
	}
	if p.opts.PublicURL != "" {
		if pub, err := url.Parse(p.opts.PublicURL); err == nil {
			if pubNorm := normalizeAuthorityHostPort(pub.Host, pub.Scheme); pubNorm != "" && refNorm == pubNorm {
				return true
			}
		}
	}
	return false
}

func normalizeAuthorityHostPort(rawHost, scheme string) string {
	clean := strings.ToLower(auth.SanitizeHostPort(rawHost))
	if clean == "" {
		return ""
	}
	h, port, err := net.SplitHostPort(clean)
	if err != nil {
		h = strings.Trim(clean, "[]")
		port = ""
	}
	if (strings.EqualFold(scheme, "http") && port == "80") || (strings.EqualFold(scheme, "https") && port == "443") {
		port = ""
	}
	if port == "" {
		if strings.Contains(h, ":") {
			return "[" + h + "]"
		}
		return h
	}
	return net.JoinHostPort(h, port)
}

func prepareSidecarUpstreamHeaders(h http.Header, port int, capPrefix string, upstreamOrigin string, vaultCookie string) {
	// Never forward browser cookies to sidecar servers; inject only cookies
	// stored in the server-side capability vault for this sidecar session.
	h.Del("Cookie")
	if vaultCookie != "" {
		h.Set("Cookie", vaultCookie)
	}

	h.Set("Origin", upstreamOrigin)
	h.Set("Sec-Fetch-Site", "same-origin")

	if ref := strings.TrimSpace(h.Get("Referer")); ref != "" {
		if refURL, err := url.Parse(ref); err == nil {
			basePrefix := fmt.Sprintf("%s%d", SidecarPrefix, port)
			esc := refURL.EscapedPath()
			switch {
			case capPrefix != "" && (esc == capPrefix || strings.HasPrefix(esc, capPrefix+"/")):
				sub := strings.TrimPrefix(esc, capPrefix)
				if sub == "" {
					sub = "/"
				}
				rewritten := upstreamOrigin + sub
				if refURL.RawQuery != "" {
					rewritten += "?" + refURL.RawQuery
				}
				h.Set("Referer", rewritten)
			case esc == basePrefix || strings.HasPrefix(esc, basePrefix+"/"):
				sub := strings.TrimPrefix(esc, basePrefix)
				if sub == "" {
					sub = "/"
				}
				rewritten := upstreamOrigin + sub
				if refURL.RawQuery != "" {
					rewritten += "?" + refURL.RawQuery
				}
				h.Set("Referer", rewritten)
			default:
				h.Del("Referer")
			}
		} else {
			h.Del("Referer")
		}
	}

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
	shimNonce := p.enforceSidecarSecurityHeaders(resp.Header, clientReq, port)

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

func (p *Proxy) enforceSidecarSecurityHeaders(h http.Header, clientReq *http.Request, port int) (shimNonce string) {
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Del("Content-Security-Policy-Report-Only")

	upstreamCSPs := h.Values("Content-Security-Policy")
	h.Del("Content-Security-Policy")
	if len(upstreamCSPs) == 0 {
		h.Set("Content-Security-Policy", sidecarDefaultCSP)
		return ""
	}

	httpPrefix, wsPrefix := p.externalSidecarPrefixURL(clientReq, port)
	merged, nonce := rewriteUpstreamSidecarCSP(strings.Join(upstreamCSPs, "; "), httpPrefix, wsPrefix)
	h.Set("Content-Security-Policy", merged)
	return nonce
}

func (p *Proxy) externalSidecarPrefixURL(r *http.Request, port int) (httpPrefix, wsPrefix string) {
	proto := "http"
	if r != nil && auth.RequestIsSecure(r, p.opts.TrustedProxies) {
		proto = "https"
	}
	host := ""
	if r != nil {
		if fwd := p.trustedForwardedHost(r); fwd != "" {
			host = fwd
		} else if safe := auth.SanitizeHostPort(r.Host); safe != "" {
			host = safe
		}
	}
	if host == "" && p.opts.PublicURL != "" {
		if pub, err := url.Parse(p.opts.PublicURL); err == nil && pub.Host != "" {
			host = auth.SanitizeHostPort(pub.Host)
			if strings.EqualFold(pub.Scheme, "https") {
				proto = "https"
			}
		}
	}
	if host == "" {
		return "", ""
	}
	wsProto := "ws"
	if proto == "https" {
		wsProto = "wss"
	}
	suffix := fmt.Sprintf("%s%d/", SidecarPrefix, port)
	return proto + "://" + host + suffix, wsProto + "://" + host + suffix
}

func rewriteUpstreamSidecarCSP(csp, httpPrefix, wsPrefix string) (rewritten string, shimNonce string) {
	rawDirs := strings.Split(csp, ";")
	type parsedDir struct {
		name   string
		tokens []string
	}
	var dirs []parsedDir
	scriptElemIdx := -1
	scriptIdx := -1
	defaultIdx := -1

	for _, raw := range rawDirs {
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
		dirs = append(dirs, parsedDir{name: name, tokens: fields[1:]})
	}

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

	effectiveIdx := scriptElemIdx
	if effectiveIdx < 0 {
		effectiveIdx = scriptIdx
	}
	if effectiveIdx < 0 {
		effectiveIdx = defaultIdx
	}

	if effectiveIdx >= 0 {
		toks := dirs[effectiveIdx].tokens
		existingNonce := ""
		hasHash := false
		hasNone := false
		for _, tok := range toks {
			low := strings.ToLower(tok)
			if low == "'none'" {
				hasNone = true
			}
			if strings.HasPrefix(low, "'nonce-") && strings.HasSuffix(tok, "'") && len(tok) > 8 {
				existingNonce = tok[7 : len(tok)-1]
			}
			if strings.HasPrefix(low, "'sha256-") || strings.HasPrefix(low, "'sha384-") || strings.HasPrefix(low, "'sha512-") {
				hasHash = true
			}
		}
		if hasNone && httpPrefix != "" {
			filtered := make([]string, 0, len(toks)+1)
			for _, tok := range toks {
				if !strings.EqualFold(tok, "'none'") {
					filtered = append(filtered, tok)
				}
			}
			toks = appendUniqueCSPToken(filtered, httpPrefix)
		} else if httpPrefix != "" {
			toks = appendUniqueCSPToken(toks, httpPrefix)
		}

		if existingNonce != "" && isSafeCSPNonce(existingNonce) {
			shimNonce = existingNonce
		} else if hasHash {
			var nb [12]byte
			if _, err := rand.Read(nb[:]); err == nil {
				shimNonce = hex.EncodeToString(nb[:])
				toks = append(toks, "'nonce-"+shimNonce+"'")
			}
		}
		dirs[effectiveIdx].tokens = toks
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
	return strings.Join(out, "; "), shimNonce
}

func appendUniqueCSPToken(tokens []string, candidate string) []string {
	for _, t := range tokens {
		if strings.EqualFold(t, candidate) {
			return tokens
		}
	}
	return append(tokens, candidate)
}

func isSafeCSPNonce(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		isAlphaNum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !isAlphaNum && c != '+' && c != '/' && c != '=' && c != '-' && c != '_' {
			return false
		}
	}
	return true
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
	case "src", "href", "action", "poster":
		return rewriteSidecarRelativeOrLoopbackURL(rawVal, port, capPrefix, baseDir)
	case "srcset":
		return rewriteSrcsetValue(rawVal, port, capPrefix, baseDir)
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

func rewriteSrcsetValue(val string, port int, capPrefix string, baseDir string) string {
	parts := strings.Split(val, ",")
	for idx, p := range parts {
		trimmed := strings.TrimLeft(p, " \t\r\n")
		lead := p[:len(p)-len(trimmed)]
		space := strings.IndexAny(trimmed, " \t\r\n")
		if space < 0 {
			parts[idx] = lead + rewriteSidecarRelativeOrLoopbackURL(trimmed, port, capPrefix, baseDir)
		} else {
			parts[idx] = lead + rewriteSidecarRelativeOrLoopbackURL(trimmed[:space], port, capPrefix, baseDir) + trimmed[space:]
		}
	}
	return strings.Join(parts, ",")
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

func (p *Proxy) proxySidecarWebSocket(w http.ResponseWriter, r *http.Request, port int, capPrefix string, capSess *sidecarCapSession, decodedPath, rawPath string) {
	if !isValidWebSocketHandshake(r) {
		http.Error(w, "invalid websocket upgrade request", http.StatusBadRequest)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "websocket hijack unsupported", http.StatusInternalServerError)
		return
	}

	upConn, err := dialLoopbackOnly(r.Context(), "tcp", fmt.Sprintf("127.0.0.1:%d", port))
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
	prepareSidecarUpstreamHeaders(outReq.Header, port, capPrefix, upstreamOrigin, capSess.cookieHeader())
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

  if (typeof Element !== "undefined" && Element.prototype && Element.prototype.setAttribute) {
    var origSetAttr = Element.prototype.setAttribute;
    Element.prototype.setAttribute = function(name, value) {
      if (typeof name === "string" && typeof value === "string") {
        var low = name.toLowerCase();
        if (low === "src" || low === "href" || low === "action" || low === "poster") {
          value = rewriteStr(value, false);
        }
      }
      return origSetAttr.call(this, name, value);
    };
  }

  function wrapProp(Ctor, prop) {
    if (!Ctor || !Ctor.prototype) return;
    var desc = Object.getOwnPropertyDescriptor(Ctor.prototype, prop);
    if (!desc || typeof desc.set !== "function") return;
    Object.defineProperty(Ctor.prototype, prop, {
      configurable: true,
      enumerable: desc.enumerable,
      get: desc.get,
      set: function(v) {
        return desc.set.call(this, typeof v === "string" ? rewriteStr(v, false) : v);
      }
    });
  }
  try {
    wrapProp(window.HTMLImageElement, "src");
    wrapProp(window.HTMLScriptElement, "src");
    wrapProp(window.HTMLLinkElement, "href");
    wrapProp(window.HTMLAnchorElement, "href");
    wrapProp(window.HTMLFormElement, "action");
    wrapProp(window.HTMLIFrameElement, "src");
    wrapProp(window.HTMLMediaElement, "src");
    wrapProp(window.HTMLSourceElement, "src");
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
