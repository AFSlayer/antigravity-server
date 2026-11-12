package auth

import (
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

var unverifiedOriginWarnOnce sync.Once

// WarnUnverifiedOriginFallback logs a one-time operational warning when a
// reverse proxy strips Host without forwarding X-Forwarded-Host or setting public_url.
func WarnUnverifiedOriginFallback() {
	unverifiedOriginWarnOnce.Do(func() {
		log.Printf("[security] reverse proxy did not forward external Host; relying on browser Sec-Fetch-Site=same-origin. Configure public_url or trusted_proxies + X-Forwarded-Host for full origin verification.")
	})
}

func isTrusted(ip net.IP, trusted []*net.IPNet) bool {
	if ip == nil {
		return false
	}
	for _, n := range trusted {
		if n != nil && n.Contains(ip) {
			return true
		}
	}
	return false
}

func remoteIP(r *http.Request) net.IP {
	if r == nil {
		return nil
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(strings.TrimSpace(host))
}

func lastCSVHeaderValue(values []string) string {
	joined := strings.Join(values, ",")
	if joined == "" {
		return ""
	}
	parts := strings.Split(joined, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		if v := strings.TrimSpace(parts[i]); v != "" {
			return v
		}
	}
	return ""
}

// SanitizeHostPort validates that raw is a syntactically safe host or host:port
// value with no scheme, path separators, userinfo, whitespace, or control bytes.
func SanitizeHostPort(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > 255 {
		return ""
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c <= 0x20 || c >= 0x7f {
			return ""
		}
		switch c {
		case '/', '\\', '@', '?', '#', ',', ';', '"', '\'':
			return ""
		}
	}

	if h, port, err := net.SplitHostPort(s); err == nil {
		if h == "" {
			return ""
		}
		p, perr := strconv.Atoi(port)
		if perr != nil || p < 1 || p > 65535 {
			return ""
		}
		return s
	}

	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		inner := s[1 : len(s)-1]
		if net.ParseIP(inner) == nil {
			return ""
		}
		return s
	}
	if strings.ContainsRune(s, ':') {
		return ""
	}
	return s
}

// SplitHostOptionalPort splits a validated or raw host[:port] into host and port.
func SplitHostOptionalPort(hostport string) (string, string) {
	if h, port, err := net.SplitHostPort(hostport); err == nil {
		return h, port
	}
	return strings.Trim(hostport, "[]"), ""
}

// IsLoopbackHost reports whether host names the local loopback interface.
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// TrustedForwardedHost returns a validated X-Forwarded-Host value only when the
// direct peer belongs to trusted. The rightmost hop appended by the trusted
// reverse proxy is selected so client-supplied prefixes are ignored.
func TrustedForwardedHost(r *http.Request, trusted []*net.IPNet) string {
	if r == nil || len(trusted) == 0 {
		return ""
	}
	direct := remoteIP(r)
	if !isTrusted(direct, trusted) {
		return ""
	}
	return SanitizeHostPort(lastCSVHeaderValue(r.Header.Values("X-Forwarded-Host")))
}

// TrustedForwardedProto returns "https" or "http" from X-Forwarded-Proto only
// when the direct peer belongs to trusted.
func TrustedForwardedProto(r *http.Request, trusted []*net.IPNet) string {
	if r == nil || len(trusted) == 0 {
		return ""
	}
	direct := remoteIP(r)
	if !isTrusted(direct, trusted) {
		return ""
	}
	proto := strings.ToLower(lastCSVHeaderValue(r.Header.Values("X-Forwarded-Proto")))
	if proto == "https" || proto == "http" {
		return proto
	}
	return ""
}

// CheckSameOrigin verifies that r came from the same origin as the server using
// Fetch Metadata (Sec-Fetch-Site) and Origin comparison against Host, trusted
// X-Forwarded-Host, and optional publicURL.
// unverifiedFallback is true when no external host header was available to
// compare against and Sec-Fetch-Site did not contradict same-origin.
func CheckSameOrigin(r *http.Request, trusted []*net.IPNet, publicURL string) (allowed bool, unverifiedFallback bool) {
	if r == nil {
		return false, false
	}

	sec := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	referer := strings.TrimSpace(r.Header.Get("Referer"))

	if sec != "" && sec != "same-origin" && sec != "none" {
		if sec == "cross-site" || origin == "" || origin == "null" || publicURL == "" {
			return false, false
		}
		if originsExplicitlyEqual(origin, publicURL) {
			return true, false
		}
		return false, false
	}

	sourceURL := origin
	if sourceURL == "" {
		sourceURL = referer
	}
	if sourceURL == "" {
		return true, false
	}
	if sourceURL == "null" {
		return false, false
	}

	u, err := url.Parse(sourceURL)
	if err != nil || u.Host == "" {
		return false, false
	}
	normOriginScheme := normalizeHTTPWSScheme(u.Scheme)
	if normOriginScheme != "http" && normOriginScheme != "https" {
		return false, false
	}
	originPort := defaultPortForScheme(u.Scheme, u.Port())
	if originPort == "" {
		return false, false
	}
	originExplicitPort := u.Port()
	originHost := u.Hostname()
	if originHost == "" {
		return false, false
	}

	xfp := TrustedForwardedProto(r, trusted)
	isSecure := RequestIsSecure(r, trusted)
	if isSecure && normOriginScheme != "https" {
		return false, false
	}
	if r.TLS == nil && xfp == "http" && normOriginScheme != "http" {
		return false, false
	}

	verifiable := IsLoopbackHost(originHost)
	if publicURL != "" {
		if originsExplicitlyEqual(sourceURL, publicURL) {
			return true, false
		}
		if pu, perr := url.Parse(publicURL); perr == nil && pu.Host != "" {
			if strings.EqualFold(originHost, pu.Hostname()) && normalizeHTTPWSScheme(pu.Scheme) == "https" && normOriginScheme != "https" {
				return false, false
			}
			if !IsLoopbackHost(pu.Hostname()) {
				verifiable = true
			}
		}
	}

	var candidates []string
	if h := SanitizeHostPort(r.Host); h != "" {
		candidates = append(candidates, h)
	}
	if xfh := TrustedForwardedHost(r, trusted); xfh != "" {
		candidates = append(candidates, xfh)
	}

	for _, c := range candidates {
		candHost, candPort := SplitHostOptionalPort(c)
		if candHost == "" {
			continue
		}
		if !IsLoopbackHost(candHost) {
			verifiable = true
		}
		if !strings.EqualFold(candHost, originHost) {
			continue
		}
		if candPort != "" {
			if candPort == originPort {
				return true, false
			}
			continue
		}
		// Candidate host omitted an explicit port: normalize to default port when
		// transport security is known, and reject non-standard Origin ports unless
		// the browser attested Sec-Fetch-Site: same-origin.
		if isSecure {
			if originPort == "443" || sec == "same-origin" {
				return true, false
			}
			continue
		}
		if r.TLS == nil && xfp == "http" {
			if originPort == "80" || sec == "same-origin" {
				return true, false
			}
			continue
		}
		if originExplicitPort == "" || sec == "same-origin" {
			return true, false
		}
	}
	if !verifiable && sec == "same-origin" {
		return true, true
	}
	return false, false
}

func defaultPortForScheme(scheme, explicitPort string) string {
	if explicitPort != "" {
		return explicitPort
	}
	switch strings.ToLower(scheme) {
	case "https", "wss":
		return "443"
	case "http", "ws":
		return "80"
	default:
		return ""
	}
}

func originsExplicitlyEqual(originA, originB string) bool {
	ua, errA := url.Parse(originA)
	ub, errB := url.Parse(originB)
	if errA != nil || errB != nil || ua.Host == "" || ub.Host == "" {
		return false
	}
	pa := defaultPortForScheme(ua.Scheme, ua.Port())
	pb := defaultPortForScheme(ub.Scheme, ub.Port())
	if pa == "" || pb == "" {
		return false
	}
	return normalizeHTTPWSScheme(ua.Scheme) == normalizeHTTPWSScheme(ub.Scheme) &&
		strings.EqualFold(ua.Hostname(), ub.Hostname()) &&
		pa == pb
}

func normalizeHTTPWSScheme(s string) string {
	switch strings.ToLower(s) {
	case "https", "wss":
		return "https"
	case "http", "ws":
		return "http"
	default:
		return strings.ToLower(s)
	}
}

// ClientIP returns the address to rate-limit against. X-Forwarded-For is only
// consulted when the direct peer is a trusted proxy, and then the rightmost
// untrusted hop wins, so a client cannot spoof its way out of a lockout by
// prepending addresses.
func ClientIP(r *http.Request, trusted []*net.IPNet) string {
	direct := remoteIP(r)
	if direct == nil {
		return "unknown"
	}
	if len(trusted) == 0 || !isTrusted(direct, trusted) {
		return direct.String()
	}

	forwarded := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
	if forwarded == "" {
		return direct.String()
	}

	parts := strings.Split(forwarded, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(parts[i]))
		if ip == nil {
			continue
		}
		if !isTrusted(ip, trusted) {
			return ip.String()
		}
	}
	return direct.String()
}

// RequestIsSecure reports whether the browser's connection was HTTPS, which
// decides if the session cookie may be marked Secure. X-Forwarded-Proto is
// believed only from a trusted proxy.
func RequestIsSecure(r *http.Request, trusted []*net.IPNet) bool {
	if r != nil && r.TLS != nil {
		return true
	}
	return TrustedForwardedProto(r, trusted) == "https"
}
