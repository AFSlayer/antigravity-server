// Package auth guards the public listener with a password, server-side sessions
// and rate limiting.
//
// Reaching Antigravity means being able to read files and run commands on the
// host, so the defaults are deliberately strict: passwords are only ever stored
// hashed, session tokens are stored as hashes so a leaked file grants nothing,
// failed logins lock out with exponential backoff, and forwarded-header trust is
// opt-in. Administrative endpoints are not defined here at all — they live on a
// separate loopback-only listener so they cannot be reached from the network.
package auth

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Endpoints live under /__agy/ so they cannot collide with an Antigravity route
// now or after a future update.
const (
	CookieName    = "agy_session"
	LoginPath     = "/__agy/login"
	LoginAPIPath  = "/__agy/api/login"
	LogoutAPIPath = "/__agy/api/logout"

	loginWindow         = 5 * time.Minute
	loginMaxFailures    = 5
	enrollMaxFailures   = 10
	maxEnrollmentTokens = 64
	globalVerifyRate    = 8.0
	globalVerifyBurst   = 24
	globalEnrollRate    = 10.0
	globalEnrollBurst   = 30

	// EnrollmentTTL bounds how long a QR code stays usable.
	EnrollmentTTL = 10 * time.Minute
)

// LoginPageFunc renders the password prompt.
type LoginPageFunc func(w http.ResponseWriter, r *http.Request)

// Options configures an Authenticator.
type Options struct {
	Credentials *Credentials
	Sessions    *SessionStore
	// Trusted lists proxy networks whose X-Forwarded-* headers may be believed.
	// Empty means trust nothing, which is the safe default.
	Trusted   []*net.IPNet
	PublicURL string
	LoginPage LoginPageFunc
	// IsPublic marks paths served without authentication, such as icons.
	IsPublic func(path string) bool
}

// Authenticator wraps a handler with password authentication.
type Authenticator struct {
	creds       *Credentials
	sessions    *SessionStore
	trusted     []*net.IPNet
	publicURL   string
	perIP       *Limiter
	enrollPerIP *Limiter
	verifyGate  *TokenBucket
	enrollGate  *TokenBucket
	login       LoginPageFunc
	isPublic    func(string) bool

	mu     sync.Mutex
	enroll map[string]time.Time
}

// New builds an Authenticator from opts.
func New(opts Options) *Authenticator {
	return &Authenticator{
		creds:       opts.Credentials,
		sessions:    opts.Sessions,
		trusted:     opts.Trusted,
		publicURL:   opts.PublicURL,
		perIP:       NewLimiter(loginMaxFailures, loginWindow),
		enrollPerIP: NewLimiter(enrollMaxFailures, loginWindow),
		verifyGate:  NewTokenBucket(globalVerifyRate, globalVerifyBurst),
		enrollGate:  NewTokenBucket(globalEnrollRate, globalEnrollBurst),
		login:       opts.LoginPage,
		isPublic:    opts.IsPublic,
		enroll:      map[string]time.Time{},
	}
}

// Sessions exposes the session store for the control panel and CLI.
func (a *Authenticator) Sessions() *SessionStore { return a.sessions }

// Credentials exposes the stored password hash for rotation.
func (a *Authenticator) Credentials() *Credentials { return a.creds }

// NewEnrollmentToken mints a single-use token that signs a device in without
// typing the password. It is what the QR code encodes, and it expires after
// EnrollmentTTL.
func (a *Authenticator) NewEnrollmentToken() (string, error) {
	tok, err := randomToken()
	if err != nil {
		return "", err
	}

	now := time.Now()
	a.mu.Lock()
	for t, exp := range a.enroll {
		if now.After(exp) {
			delete(a.enroll, t)
		}
	}
	for len(a.enroll) >= maxEnrollmentTokens {
		var oldestTok string
		var oldestExp time.Time
		for t, exp := range a.enroll {
			if oldestTok == "" || exp.Before(oldestExp) {
				oldestTok = t
				oldestExp = exp
			}
		}
		if oldestTok == "" {
			break
		}
		delete(a.enroll, oldestTok)
	}
	a.enroll[hashToken(tok)] = now.Add(EnrollmentTTL)
	a.mu.Unlock()

	return tok, nil
}

func (a *Authenticator) peekEnrollment(tok string) bool {
	if tok == "" {
		return false
	}
	key := hashToken(tok)
	now := time.Now()

	a.mu.Lock()
	defer a.mu.Unlock()

	exp, ok := a.enroll[key]
	if !ok {
		return false
	}
	if !now.Before(exp) {
		delete(a.enroll, key)
		return false
	}
	return true
}

func (a *Authenticator) consumeEnrollment(tok string) bool {
	if tok == "" {
		return false
	}
	key := hashToken(tok)

	a.mu.Lock()
	defer a.mu.Unlock()

	exp, ok := a.enroll[key]
	if !ok {
		return false
	}
	delete(a.enroll, key)
	return time.Now().Before(exp)
}

// Authenticated reports whether the request carries a valid session cookie.
func (a *Authenticator) Authenticated(r *http.Request) bool {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return false
	}
	return a.sessions.Validate(cookie.Value)
}

// Middleware serves the login endpoints and gates everything else behind a
// valid session.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if a.isPublic != nil && a.isPublic(path) {
			next.ServeHTTP(w, r)
			return
		}

		if isStateChangingMethod(r.Method) && !a.sameOriginRequest(r) {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}

		switch {
		case path == LoginPath:
			a.handleLoginPage(w, r)
			return
		case path == LoginAPIPath:
			a.handleLoginAPI(w, r)
			return
		case path == LogoutAPIPath:
			a.handleLogout(w, r)
			return
		}

		if a.Authenticated(r) {
			next.ServeHTTP(w, r)
			return
		}

		a.reject(w, r)
	})
}

func isStateChangingMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

func (a *Authenticator) sameOriginRequest(r *http.Request) bool {
	ok, unverified := CheckSameOrigin(r, a.trusted, a.publicURL)
	if ok && unverified {
		WarnUnverifiedOriginFallback()
	}
	return ok
}

func setEnrollmentSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
}

func renderEnrollmentConfirmPage(w http.ResponseWriter, tok string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	setEnrollmentSecurityHeaders(w)
	w.WriteHeader(http.StatusOK)
	escaped := html.EscapeString(tok)
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Sign in — Antigravity</title><style>body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;background:#0b0d10;color:#e6edf3;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif}.card{max-width:360px;width:calc(100%% - 32px);padding:28px 24px;border-radius:16px;background:#14181f;border:1px solid #262c36;text-align:center}h1{margin:0 0 8px;font-size:18px;font-weight:600}p{margin:0 0 20px;font-size:14px;color:#9aa4b2;line-height:1.5}button{width:100%%;padding:12px 16px;border:0;border-radius:10px;background:#3b82f6;color:#fff;font-size:15px;font-weight:600;cursor:pointer}button:active{opacity:.9}</style></head><body><div class="card"><h1>Sign in to Antigravity</h1><p>Confirm to finish signing in on this device.</p><form method="POST" action="%s"><input type="hidden" name="t" value="%s"><button type="submit">Continue</button></form></div></body></html>`, LoginPath, escaped)
}

func (a *Authenticator) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		tok := strings.TrimSpace(r.FormValue("t"))
		if tok == "" {
			http.Redirect(w, r, LoginPath, http.StatusFound)
			return
		}
		ip := ClientIP(r, a.trusted)
		if ok, retry := a.enrollPerIP.Allow(ip); !ok {
			a.tooMany(w, retry)
			return
		}
		if ok, retry := a.enrollGate.Allow(); !ok {
			a.tooMany(w, retry)
			return
		}
		if a.consumeEnrollment(tok) {
			a.enrollPerIP.Reset(ip)
			if err := a.startSession(w, r); err != nil {
				http.Error(w, "could not start session", http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		a.enrollPerIP.Fail(ip)
		http.Redirect(w, r, LoginPath, http.StatusFound)
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if tok := r.URL.Query().Get("t"); tok != "" {
		ip := ClientIP(r, a.trusted)

		if ok, retry := a.enrollPerIP.Allow(ip); !ok {
			a.tooMany(w, retry)
			return
		}
		if ok, retry := a.enrollGate.Allow(); !ok {
			a.tooMany(w, retry)
			return
		}

		if a.peekEnrollment(tok) {
			if r.Method == http.MethodHead {
				setEnrollmentSecurityHeaders(w)
				w.WriteHeader(http.StatusOK)
				return
			}
			renderEnrollmentConfirmPage(w, tok)
			return
		}

		a.enrollPerIP.Fail(ip)
		http.Redirect(w, r, LoginPath, http.StatusFound)
		return
	}

	if a.Authenticated(r) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	a.login(w, r)
}

func (a *Authenticator) handleLoginAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "Method not allowed."})
		return
	}
	if r.Context().Err() != nil {
		return
	}

	ip := ClientIP(r, a.trusted)
	if ok, retry := a.allow(ip); !ok {
		a.tooMany(w, retry)
		return
	}
	if ok, retry := a.verifyGate.Allow(); !ok {
		a.tooMany(w, retry)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Request body too large or unreadable."})
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	ct := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if ct == "application/x-www-form-urlencoded" {
		if vals, perr := url.ParseQuery(string(body)); perr == nil {
			req.Password = vals.Get("password")
		}
	} else if err := json.Unmarshal(body, &req); err != nil {
		if vals, perr := url.ParseQuery(string(body)); perr == nil {
			req.Password = vals.Get("password")
		}
	}

	if req.Password == "" {
		a.perIP.Fail(ip)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"success": false, "error": "Incorrect password."})
		return
	}
	if !a.creds.VerifyContext(r.Context(), req.Password) {
		if r.Context().Err() != nil {
			return
		}
		a.perIP.Fail(ip)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"success": false, "error": "Incorrect password."})
		return
	}

	a.perIP.Reset(ip)

	if err := a.startSession(w, r); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": "Could not start session."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (a *Authenticator) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false})
		return
	}

	if cookie, err := r.Cookie(CookieName); err == nil {
		_ = a.sessions.Revoke(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.cookieShouldBeSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (a *Authenticator) startSession(w http.ResponseWriter, r *http.Request) error {
	raw, err := a.sessions.Create(r.UserAgent(), ClientIP(r, a.trusted))
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    raw,
		Path:     "/",
		MaxAge:   int(a.sessions.ttl.Seconds()),
		HttpOnly: true,
		Secure:   a.cookieShouldBeSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (a *Authenticator) cookieShouldBeSecure(r *http.Request) bool {
	if RequestIsSecure(r, a.trusted) {
		return true
	}
	if r == nil || a.publicURL == "" {
		return false
	}
	pu, err := url.Parse(a.publicURL)
	if err != nil || !strings.EqualFold(pu.Scheme, "https") {
		return false
	}
	pubHost := pu.Hostname()
	if pubHost == "" || IsLoopbackHost(pubHost) {
		return false
	}
	if h := SanitizeHostPort(r.Host); h != "" {
		reqHost, _ := SplitHostOptionalPort(h)
		if strings.EqualFold(reqHost, pubHost) {
			return true
		}
	}
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" && originsExplicitlyEqual(origin, a.publicURL) {
		return true
	}
	return false
}

func (a *Authenticator) allow(ip string) (bool, time.Duration) {
	return a.perIP.Allow(ip)
}

func (a *Authenticator) tooMany(w http.ResponseWriter, retry time.Duration) {
	w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retry.Seconds())))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{
		"success": false,
		"error":   fmt.Sprintf("Too many attempts. Try again in %s.", retry),
	})
}

// reject answers an unauthenticated request in the shape its caller expects: a
// redirect for page loads, a bare status for API and websocket calls.
func (a *Authenticator) reject(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != http.MethodGet || !strings.Contains(r.Header.Get("Accept"), "text/html") {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}

	http.Redirect(w, r, LoginPath, http.StatusFound)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
