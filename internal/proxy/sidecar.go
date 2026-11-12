package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/auth"
)

const (
	// SidecarPrefix is the same-origin path prefix under which active sidecar
	// HTTP/WebSocket gateways are reverse-proxied.
	SidecarPrefix = "/__agy/sidecar/"

	sidecarShimName        = "__agy_sidecar_shim.js"
	sidecarCacheTTL        = 5 * time.Second
	sidecarMissRefreshMin  = 500 * time.Millisecond
	sidecarErrorGrace      = 30 * time.Second
	sidecarMaxRewriteBytes = 16 << 20
	sidecarDialTimeout     = 5 * time.Second
	sidecarRPCTimeout      = 3 * time.Second
)

func newSidecarTransport() *http.Transport {
	return &http.Transport{
		MaxIdleConns:        64,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     60 * time.Second,
		DialContext:         dialLoopbackOnly,
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
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "connection refused")
}

// ReservedSidecarPorts returns all ports currently owned by sidecars (both raw
// webPorts and auth gateway ports) so generic loopback preview handlers can
// refuse direct access to them.
func (p *Proxy) ReservedSidecarPorts(ctx context.Context) map[int]struct{} {
	_, reserved := p.sidecarPortSets(ctx, 0)
	out := make(map[int]struct{}, len(reserved))
	for port := range reserved {
		out[port] = struct{}{}
	}
	return out
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
	allowed, _ := p.sidecarPortSets(ctx, port)
	_, ok := allowed[port]
	return ok
}

func (p *Proxy) sidecarPortSets(ctx context.Context, wantPort int) (allowed, reserved map[int]struct{}) {
	now := time.Now()

	p.sidecarMu.Lock()
	if !p.sidecarCachedAt.IsZero() && now.Sub(p.sidecarCachedAt) < sidecarCacheTTL {
		if wantPort <= 0 {
			a, r := p.sidecarAllowed, p.sidecarReserved
			p.sidecarMu.Unlock()
			return a, r
		}
		if _, ok := p.sidecarAllowed[wantPort]; ok {
			a, r := p.sidecarAllowed, p.sidecarReserved
			p.sidecarMu.Unlock()
			return a, r
		}
		if now.Sub(p.sidecarLastProbe) < sidecarMissRefreshMin {
			a, r := p.sidecarAllowed, p.sidecarReserved
			p.sidecarMu.Unlock()
			return a, r
		}
	} else if !p.sidecarLastProbe.IsZero() && now.Sub(p.sidecarLastProbe) < sidecarMissRefreshMin && p.sidecarAllowed != nil {
		a, r := p.sidecarAllowed, p.sidecarReserved
		p.sidecarMu.Unlock()
		return a, r
	}
	p.sidecarLastProbe = now
	p.sidecarMu.Unlock()

	freshAllowed, freshReserved, err := p.fetchSidecarPorts(ctx)

	p.sidecarMu.Lock()
	defer p.sidecarMu.Unlock()
	if err != nil {
		if !p.sidecarCachedAt.IsZero() && time.Since(p.sidecarCachedAt) < sidecarErrorGrace {
			return p.sidecarAllowed, p.sidecarReserved
		}
		return nil, nil
	}
	p.sidecarAllowed = freshAllowed
	p.sidecarReserved = freshReserved
	p.sidecarCachedAt = time.Now()
	return p.sidecarAllowed, p.sidecarReserved
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

func parseSidecarRoute(escapedPath string) (port int, restEscaped string, needsSlash bool, ok bool) {
	if !strings.HasPrefix(escapedPath, SidecarPrefix) {
		return 0, "", false, false
	}
	tail := strings.TrimPrefix(escapedPath, SidecarPrefix)
	if tail == "" {
		return 0, "", false, false
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
		return 0, "", false, false
	}
	return p, restEscaped, needsSlash, true
}

func (p *Proxy) serveSidecar(w http.ResponseWriter, r *http.Request) {
	escaped := r.URL.EscapedPath()
	port, restEscaped, needsSlash, ok := parseSidecarRoute(escaped)
	if !ok {
		http.NotFound(w, r)
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
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
		return
	}

	decodedRest, err := url.PathUnescape(restEscaped)
	if err != nil {
		decodedRest = restEscaped
	}
	if decodedRest == "/"+sidecarShimName {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		shim := []byte(sidecarShimJS(port))
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(shim)))
		if r.Method == http.MethodGet {
			_, _ = w.Write(shim)
		}
		return
	}

	if isWebSocketUpgrade(r) {
		p.proxySidecarWebSocket(w, r, port, decodedRest, restEscaped)
		return
	}

	upstreamHost := fmt.Sprintf("localhost:%d", port)
	upstreamOrigin := "http://" + upstreamHost

	rp := &httputil.ReverseProxy{
		Transport:     p.sidecarTransport,
		FlushInterval: -1,
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = fmt.Sprintf("127.0.0.1:%d", port)
			req.URL.Path = decodedRest
			req.URL.RawPath = restEscaped
			req.Host = upstreamHost

			sanitizeSidecarRequestHeaders(req.Header, port, upstreamOrigin)
			req.Header.Del("Accept-Encoding")
			req.Header.Del("If-None-Match")
			req.Header.Del("If-Modified-Since")
		},
		ModifyResponse: func(resp *http.Response) error {
			return modifySidecarResponse(resp, port)
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, proxyErr error) {
			if req.Context().Err() != nil {
				return
			}
			rw.WriteHeader(http.StatusBadGateway)
			_, _ = rw.Write([]byte("Sidecar UI is not reachable."))
		},
	}
	rp.ServeHTTP(w, r)
}

func (p *Proxy) redirectSidecarOrphanRequest(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if path == "/" ||
		strings.HasPrefix(path, "/__agy/") ||
		strings.HasPrefix(path, "/exa.") ||
		path == "/connect-websocket" ||
		path == "/main.js" ||
		path == "/onboarding" ||
		strings.HasPrefix(path, "/onboarding/") {
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
	if refURL.Host != "" && !p.sameRefererHost(r, refURL.Host) {
		return false
	}
	port, _, _, ok := parseSidecarRoute(refURL.EscapedPath())
	if !ok || !p.isAllowedSidecarPort(r.Context(), port) {
		return false
	}

	target := fmt.Sprintf("%s%d%s", SidecarPrefix, port, r.URL.EscapedPath())
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	return true
}

func (p *Proxy) sameRefererHost(r *http.Request, refHost string) bool {
	refClean := strings.ToLower(auth.SanitizeHostPort(refHost))
	if refClean == "" {
		return false
	}
	if fwd := strings.ToLower(p.trustedForwardedHost(r)); fwd != "" && refClean == fwd {
		return true
	}
	if reqHost := strings.ToLower(auth.SanitizeHostPort(r.Host)); reqHost != "" && refClean == reqHost {
		return true
	}
	if p.opts.PublicURL != "" {
		if pub, err := url.Parse(p.opts.PublicURL); err == nil {
			if pubHost := strings.ToLower(auth.SanitizeHostPort(pub.Host)); pubHost != "" && refClean == pubHost {
				return true
			}
		}
	}
	return false
}

func sanitizeSidecarRequestHeaders(h http.Header, port int, upstreamOrigin string) {
	cleanedCookie := stripSessionCookies(h.Values("Cookie"))
	h.Del("Cookie")
	if cleanedCookie != "" {
		h.Set("Cookie", cleanedCookie)
	}

	h.Set("Origin", upstreamOrigin)
	h.Set("Sec-Fetch-Site", "same-origin")

	if ref := strings.TrimSpace(h.Get("Referer")); ref != "" {
		if refURL, err := url.Parse(ref); err == nil {
			prefix := fmt.Sprintf("%s%d", SidecarPrefix, port)
			if esc := refURL.EscapedPath(); esc == prefix || strings.HasPrefix(esc, prefix+"/") {
				sub := strings.TrimPrefix(esc, prefix)
				if sub == "" {
					sub = "/"
				}
				rewritten := upstreamOrigin + sub
				if refURL.RawQuery != "" {
					rewritten += "?" + refURL.RawQuery
				}
				h.Set("Referer", rewritten)
			} else {
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

// stripSessionCookies removes agy_session (and any cookie whose name references
// agy_session) before forwarding requests to untrusted or semi-trusted loopback
// servers so the parent authentication token can never leak out of the proxy.
func stripSessionCookies(values []string) string {
	if len(values) == 0 {
		return ""
	}
	var kept []string
	for _, line := range values {
		for _, part := range strings.Split(line, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			eq := strings.IndexByte(part, '=')
			name := part
			if eq >= 0 {
				name = strings.TrimSpace(part[:eq])
			}
			lower := strings.ToLower(name)
			if lower == strings.ToLower(auth.CookieName) || strings.Contains(lower, "agy_session") {
				continue
			}
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, "; ")
}

func modifySidecarResponse(resp *http.Response, port int) error {
	prefix := fmt.Sprintf("%s%d", SidecarPrefix, port)

	if rawCookies := resp.Header.Values("Set-Cookie"); len(rawCookies) > 0 {
		resp.Header.Del("Set-Cookie")
		for _, raw := range rawCookies {
			if scoped, ok := rewriteSidecarSetCookie(raw, prefix); ok {
				resp.Header.Add("Set-Cookie", scoped)
			}
		}
	}

	if loc := resp.Header.Get("Location"); loc != "" {
		resp.Header.Set("Location", rewriteSidecarLocation(loc, port, prefix))
	}

	if strings.EqualFold(strings.TrimSpace(resp.Header.Get("X-Frame-Options")), "DENY") {
		resp.Header.Set("X-Frame-Options", "SAMEORIGIN")
	}
	if csp := resp.Header.Get("Content-Security-Policy"); csp != "" {
		resp.Header.Set("Content-Security-Policy", relaxSidecarFrameAncestors(csp))
	}

	if resp.Request != nil && resp.Request.Method == http.MethodHead {
		resp.Header.Del("Content-Length")
		return nil
	}
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Encoding") != "" {
		return nil
	}

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	reqPath := ""
	if resp.Request != nil && resp.Request.URL != nil {
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
		// Too large to buffer safely: stream untouched.
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

	var rewritten []byte
	switch {
	case isHTML:
		rewritten = rewriteSidecarHTML(buf, port)
	case isCSS:
		rewritten = rewriteSidecarCSS(buf, port)
	case isPreloadJS:
		shim := sidecarShimJS(port) + "\n"
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

func rewriteSidecarSetCookie(raw, prefix string) (string, bool) {
	parts := strings.Split(raw, ";")
	if len(parts) == 0 {
		return "", false
	}
	first := strings.TrimSpace(parts[0])
	if first == "" {
		return "", false
	}
	eq := strings.IndexByte(first, '=')
	if eq <= 0 {
		return "", false
	}
	cookieName := strings.ToLower(strings.TrimSpace(first[:eq]))
	if cookieName == strings.ToLower(auth.CookieName) || strings.Contains(cookieName, "agy_session") {
		return "", false
	}

	out := []string{first}
	wrotePath := false
	scopedBase := prefix + "/"
	for _, attr := range parts[1:] {
		attr = strings.TrimSpace(attr)
		if attr == "" {
			continue
		}
		attrName := attr
		attrVal := ""
		if k := strings.IndexByte(attr, '='); k >= 0 {
			attrName = strings.TrimSpace(attr[:k])
			attrVal = strings.TrimSpace(attr[k+1:])
		}
		switch strings.ToLower(attrName) {
		case "domain":
			// Strip Domain so sidecar cookies stay host-only on the proxy origin.
			continue
		case "path":
			wrotePath = true
			if attrVal == "" || attrVal == "/" {
				out = append(out, "Path="+scopedBase)
			} else if strings.HasPrefix(attrVal, prefix+"/") || attrVal == prefix {
				out = append(out, "Path="+attrVal)
			} else if strings.HasPrefix(attrVal, "/") {
				out = append(out, "Path="+prefix+attrVal)
			} else {
				out = append(out, "Path="+scopedBase+attrVal)
			}
		default:
			out = append(out, attr)
		}
	}
	if !wrotePath {
		out = append(out, "Path="+scopedBase)
	}
	return strings.Join(out, "; "), true
}

func rewriteSidecarLocation(loc string, port int, prefix string) string {
	loc = strings.TrimSpace(loc)
	if loc == "" {
		return loc
	}
	if strings.HasPrefix(loc, "/") && !strings.HasPrefix(loc, "//") {
		if loc == prefix || strings.HasPrefix(loc, prefix+"/") || strings.HasPrefix(loc, "/__agy/") {
			return loc
		}
		return prefix + loc
	}
	u, err := url.Parse(loc)
	if err != nil {
		return loc
	}
	if p, ok := loopbackPortFromURL(loc); ok && p == port {
		path := u.EscapedPath()
		if path == "" {
			path = "/"
		}
		out := prefix + path
		if u.RawQuery != "" {
			out += "?" + u.RawQuery
		}
		if u.Fragment != "" {
			out += "#" + u.Fragment
		}
		return out
	}
	return loc
}

func relaxSidecarFrameAncestors(csp string) string {
	directives := strings.Split(csp, ";")
	changed := false
	for i, d := range directives {
		trimmed := strings.TrimSpace(d)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "frame-ancestors") &&
			(len(lower) == len("frame-ancestors") || lower[len("frame-ancestors")] == ' ' || lower[len("frame-ancestors")] == '\t') {
			if !strings.Contains(lower, "'self'") && !strings.Contains(lower, "*") {
				directives[i] = " frame-ancestors 'self'"
				changed = true
			}
		}
	}
	if !changed {
		return csp
	}
	return strings.TrimSpace(strings.Join(directives, ";"))
}

// rewriteSidecarHTML injects the external same-origin shim script and rewrites
// root-relative URL attributes (src, href, action, poster, srcset) on HTML tags
// while leaving <script> bodies, comments, and iframe srcdoc contents untouched.
func rewriteSidecarHTML(body []byte, port int) []byte {
	prefix := fmt.Sprintf("%s%d", SidecarPrefix, port)
	shimTag := fmt.Sprintf(`<script src="%s/%s"></script>`, prefix, sidecarShimName)

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

		// HTML comment <!-- ... -->
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
			out.Write(rewriteHTMLOpenTag(tagBytes, prefix))
		} else {
			out.Write(tagBytes)
		}
		i = tagEnd + 1

		if !injected && !isClosing && tagName == "head" {
			out.WriteString(shimTag)
			injected = true
		}

		// Raw text elements: skip <script> body untouched; rewrite CSS inside <style>.
		if !isClosing && (tagName == "script" || tagName == "style") && !strings.HasSuffix(strings.TrimSpace(string(tagBytes[:len(tagBytes)-1])), "/") {
			closeIdx := findRawTextCloseTag(body, i, tagName)
			if closeIdx < 0 {
				rawRest := body[i:]
				if tagName == "style" {
					out.Write(rewriteSidecarCSS(rawRest, port))
				} else {
					out.Write(rawRest)
				}
				break
			}
			inner := body[i:closeIdx]
			if tagName == "style" {
				out.Write(rewriteSidecarCSS(inner, port))
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

func rewriteHTMLOpenTag(tag []byte, prefix string) []byte {
	var out bytes.Buffer
	out.Grow(len(tag) + 64)

	n := len(tag)
	// Copy '<' and tag name.
	i := 1
	for i < n && tag[i] != ' ' && tag[i] != '\t' && tag[i] != '\r' && tag[i] != '\n' && tag[i] != '>' && tag[i] != '/' {
		i++
	}
	out.Write(tag[:i])

	for i < n {
		// Copy whitespace or closing punctuation.
		wsStart := i
		for i < n && (tag[i] == ' ' || tag[i] == '\t' || tag[i] == '\r' || tag[i] == '\n') {
			i++
		}
		out.Write(tag[wsStart:i])
		if i >= n {
			break
		}
		if tag[i] == '>' || (tag[i] == '/' && i+1 < n && tag[i+1] == '>') {
			out.Write(tag[i:])
			break
		}

		// Read attribute name.
		attrStart := i
		for i < n && tag[i] != '=' && tag[i] != ' ' && tag[i] != '\t' && tag[i] != '\r' && tag[i] != '\n' && tag[i] != '>' && tag[i] != '/' {
			i++
		}
		attrName := strings.ToLower(string(tag[attrStart:i]))
		out.Write(tag[attrStart:i])

		eqWS := i
		for i < n && (tag[i] == ' ' || tag[i] == '\t' || tag[i] == '\r' || tag[i] == '\n') {
			i++
		}
		if i >= n || tag[i] != '=' {
			out.Write(tag[eqWS:i])
			continue
		}
		i++ // '='
		valWS := i
		for i < n && (tag[i] == ' ' || tag[i] == '\t' || tag[i] == '\r' || tag[i] == '\n') {
			i++
		}
		out.Write(tag[eqWS:valWS])
		out.Write(tag[valWS:i])
		if i >= n {
			break
		}

		var quote byte
		valStart := i
		if tag[i] == '"' || tag[i] == '\'' {
			quote = tag[i]
			i++
			valStart = i
			for i < n && tag[i] != quote {
				i++
			}
		} else {
			for i < n && tag[i] != ' ' && tag[i] != '\t' && tag[i] != '\r' && tag[i] != '\n' && tag[i] != '>' {
				i++
			}
		}
		rawVal := string(tag[valStart:i])
		newVal := rawVal
		switch attrName {
		case "src", "href", "action", "poster":
			newVal = rewriteRootRelativeURL(rawVal, prefix)
		case "srcset":
			newVal = rewriteSrcsetValue(rawVal, prefix)
		}

		if quote != 0 {
			out.WriteByte(quote)
			out.WriteString(newVal)
			if i < n && tag[i] == quote {
				out.WriteByte(quote)
				i++
			}
		} else {
			out.WriteString(newVal)
		}
	}

	return out.Bytes()
}

func rewriteRootRelativeURL(val, prefix string) string {
	trimmed := strings.TrimSpace(val)
	if !strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "//") {
		return val
	}
	if trimmed == prefix || strings.HasPrefix(trimmed, prefix+"/") || strings.HasPrefix(trimmed, "/__agy/") {
		return val
	}
	leadLen := len(val) - len(strings.TrimLeft(val, " \t\r\n"))
	return val[:leadLen] + prefix + val[leadLen:]
}

func rewriteSrcsetValue(val, prefix string) string {
	parts := strings.Split(val, ",")
	for idx, p := range parts {
		trimmed := strings.TrimLeft(p, " \t\r\n")
		lead := p[:len(p)-len(trimmed)]
		space := strings.IndexAny(trimmed, " \t\r\n")
		if space < 0 {
			parts[idx] = lead + rewriteRootRelativeURL(trimmed, prefix)
		} else {
			parts[idx] = lead + rewriteRootRelativeURL(trimmed[:space], prefix) + trimmed[space:]
		}
	}
	return strings.Join(parts, ",")
}

func rewriteSidecarCSS(body []byte, port int) []byte {
	prefix := fmt.Sprintf("%s%d", SidecarPrefix, port)
	var out bytes.Buffer
	out.Grow(len(body) + 128)

	n := len(body)
	i := 0
	for i < n {
		// Skip CSS block comments /* ... */
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

		// Match url(...)
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
				out.WriteString(rewriteRootRelativeURL(string(body[start:i]), prefix))
				if i < n && body[i] == q {
					out.WriteByte(q)
					i++
				}
			} else {
				start := i
				for i < n && body[i] != ')' && body[i] != ' ' && body[i] != '\t' && body[i] != '\r' && body[i] != '\n' {
					i++
				}
				out.WriteString(rewriteRootRelativeURL(string(body[start:i]), prefix))
			}
			continue
		}

		// Match @import "..." or @import '...'
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
				out.WriteString(rewriteRootRelativeURL(string(body[start:i]), prefix))
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

func (p *Proxy) proxySidecarWebSocket(w http.ResponseWriter, r *http.Request, port int, decodedPath, rawPath string) {
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
	outReq.URL.Scheme = "http"
	outReq.URL.Host = upstreamHost
	outReq.URL.Path = decodedPath
	outReq.URL.RawPath = rawPath
	outReq.Host = upstreamHost
	outReq.RequestURI = ""
	sanitizeSidecarRequestHeaders(outReq.Header, port, upstreamOrigin)

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
		_ = modifySidecarResponse(resp, port)
		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, io.LimitReader(resp.Body, 1<<20))
		return
	}

	prefix := fmt.Sprintf("%s%d", SidecarPrefix, port)
	if rawCookies := resp.Header.Values("Set-Cookie"); len(rawCookies) > 0 {
		resp.Header.Del("Set-Cookie")
		for _, raw := range rawCookies {
			if scoped, keep := rewriteSidecarSetCookie(raw, prefix); keep {
				resp.Header.Add("Set-Cookie", scoped)
			}
		}
	}

	clientConn, clientBuf, err := hj.Hijack()
	if err != nil {
		_ = upConn.Close()
		return
	}
	defer clientConn.Close()
	defer upConn.Close()

	var hs bytes.Buffer
	fmt.Fprintf(&hs, "HTTP/1.1 %s\r\n", resp.Status)
	_ = resp.Header.Write(&hs)
	hs.WriteString("\r\n")
	if _, err := clientConn.Write(hs.Bytes()); err != nil {
		return
	}

	// Flush any bytes buffered alongside the HTTP upgrade headers before
	// starting bidirectional streaming so initial frames are never lost.
	if upBuffered := upReader.Buffered(); upBuffered > 0 {
		if _, err := io.CopyN(clientConn, upReader, int64(upBuffered)); err != nil {
			return
		}
	}
	if clientBuf != nil && clientBuf.Reader.Buffered() > 0 {
		if _, err := io.CopyN(upConn, clientBuf.Reader, int64(clientBuf.Reader.Buffered())); err != nil {
			return
		}
	}

	done := make(chan struct{}, 2)
	copyStream := func(dst io.Writer, src io.Reader) {
		buf := make([]byte, 32*1024)
		for {
			n, rErr := src.Read(buf)
			if n > 0 {
				p.touchActivity()
				if _, wErr := dst.Write(buf[:n]); wErr != nil {
					break
				}
			}
			if rErr != nil {
				break
			}
		}
		done <- struct{}{}
	}

	go copyStream(upConn, clientConn)
	go copyStream(clientConn, upConn)
	<-done
}

func sidecarShimJS(port int) string {
	return `(function(){
  var PORT = ` + strconv.Itoa(port) + `;
  if (window.__agySidecarShimPort === PORT) return;
  window.__agySidecarShimPort = PORT;
  var PREFIX = "` + SidecarPrefix + `" + PORT;

  function rewriteStr(u, forWS) {
    if (typeof u !== "string" || !u) return u;
    if (u.charAt(0) === "/" && u.charAt(1) !== "/") {
      if (u === PREFIX || u.indexOf(PREFIX + "/") === 0 || u.indexOf("/__agy/") === 0) {
        return forWS ? ((location.protocol === "https:" ? "wss://" : "ws://") + location.host + u) : u;
      }
      var mapped = PREFIX + u;
      return forWS ? ((location.protocol === "https:" ? "wss://" : "ws://") + location.host + mapped) : mapped;
    }
    try {
      var parsed = new URL(u, location.href);
      var h = parsed.hostname.toLowerCase();
      var isLoop = (h === "localhost" || h === "127.0.0.1" || h === "[::1]" || h === "::1") && parsed.port === String(PORT);
      var isSameSidecar = parsed.host === location.host && (parsed.pathname === PREFIX || parsed.pathname.indexOf(PREFIX + "/") === 0);
      var isSameRoot = parsed.host === location.host && parsed.pathname.indexOf("/__agy/") !== 0;
      if (isLoop || (forWS && (isSameSidecar || isSameRoot))) {
        var p = parsed.pathname || "/";
        if (p !== PREFIX && p.indexOf(PREFIX + "/") !== 0) p = PREFIX + p;
        var proto = forWS ? (location.protocol === "https:" ? "wss:" : "ws:") : location.protocol;
        return proto + "//" + location.host + p + parsed.search + parsed.hash;
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
        if (typeof url === "string" && url.charAt(0) === "/" && url.charAt(1) !== "/" && url !== PREFIX && url.indexOf(PREFIX + "/") !== 0 && url.indexOf("/__agy/") !== 0) {
          url = PREFIX + url;
        }
        return orig.call(this, state, title, url);
      };
    });
  }
})();`
}
