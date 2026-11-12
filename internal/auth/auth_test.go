package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testAuth(t *testing.T, password string) *Authenticator {
	t.Helper()

	dir := t.TempDir()
	creds, _, err := LoadOrCreateCredentials(filepath.Join(dir, "credentials.json"), password)
	if err != nil {
		t.Fatalf("credentials: %v", err)
	}
	sessions, err := NewSessionStore(filepath.Join(dir, "sessions.json"), time.Hour)
	if err != nil {
		t.Fatalf("sessions: %v", err)
	}

	return New(Options{
		Credentials: creds,
		Sessions:    sessions,
		LoginPage: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("login page"))
		},
		IsPublic: func(path string) bool { return path == "/favicon.ico" },
	})
}

func protected() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("secret"))
	})
}

func TestPasswordVerify(t *testing.T) {
	dir := t.TempDir()
	creds, generated, err := LoadOrCreateCredentials(filepath.Join(dir, "c.json"), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(generated) < 12 {
		t.Fatalf("generated password too short: %q", generated)
	}
	if !creds.Verify(generated) {
		t.Error("generated password should verify")
	}
	if creds.Verify(generated + "x") {
		t.Error("wrong password must not verify")
	}
	if strings.Contains(creds.Hash, generated) {
		t.Error("stored hash must not contain the plaintext")
	}
}

func TestCredentialsPersistAndRotate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")

	creds, _, err := LoadOrCreateCredentials(path, "first-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := creds.Set("second-password"); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadCredentials(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Verify("second-password") {
		t.Error("rotated password should verify after reload")
	}
	if reloaded.Verify("first-password") {
		t.Error("old password must stop working")
	}
}

func TestShortPasswordRejected(t *testing.T) {
	if _, _, err := LoadOrCreateCredentials(filepath.Join(t.TempDir(), "c.json"), "short"); err == nil {
		t.Error("expected short password to be rejected")
	}
}

func TestSessionLifecycle(t *testing.T) {
	store, err := NewSessionStore(filepath.Join(t.TempDir(), "s.json"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	raw, err := store.Create("test-agent", "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if !store.Validate(raw) {
		t.Fatal("fresh session should validate")
	}
	if store.Validate(raw + "x") {
		t.Error("tampered token must not validate")
	}

	for _, s := range store.List() {
		if s.Key == raw {
			t.Error("raw token must not be stored on disk")
		}
	}

	if err := store.Revoke(raw); err != nil {
		t.Fatal(err)
	}
	if store.Validate(raw) {
		t.Error("revoked session must not validate")
	}
}

func TestSessionExpiry(t *testing.T) {
	store, err := NewSessionStore(filepath.Join(t.TempDir(), "s.json"), -time.Second)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := store.Create("", "")
	if err != nil {
		t.Fatal(err)
	}
	if store.Validate(raw) {
		t.Error("expired session must not validate")
	}
}

func TestRevokeAllInvalidatesEveryDevice(t *testing.T) {
	store, _ := NewSessionStore(filepath.Join(t.TempDir(), "s.json"), time.Hour)

	a, _ := store.Create("phone", "1.1.1.1")
	b, _ := store.Create("laptop", "2.2.2.2")

	n, err := store.RevokeAll()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("want 2 revoked, got %d", n)
	}
	if store.Validate(a) || store.Validate(b) {
		t.Error("all sessions must be invalid after RevokeAll")
	}
}

func TestSessionsSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")

	store, _ := NewSessionStore(path, time.Hour)
	raw, _ := store.Create("phone", "1.1.1.1")

	reopened, err := NewSessionStore(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.Validate(raw) {
		t.Error("sessions should survive a restart")
	}
}

func TestLimiterLocksOutAfterMaxFailures(t *testing.T) {
	l := NewLimiter(3, time.Minute)

	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("ip"); !ok {
			t.Fatalf("attempt %d should be allowed", i)
		}
		l.Fail("ip")
	}

	ok, retry := l.Allow("ip")
	if ok {
		t.Fatal("expected lockout after reaching max failures")
	}
	if retry <= 0 {
		t.Error("lockout should report a retry-after duration")
	}
}

func TestLimiterBackoffGrows(t *testing.T) {
	l := NewLimiter(1, time.Minute)

	l.Fail("ip")
	_, first := l.Allow("ip")

	l.mu.Lock()
	l.entries["ip"].lockedUntil = time.Now().Add(-time.Second)
	l.mu.Unlock()

	l.Fail("ip")
	_, second := l.Allow("ip")

	if second <= first {
		t.Errorf("backoff should grow: first=%s second=%s", first, second)
	}
}

func TestLimiterResetClearsLockout(t *testing.T) {
	l := NewLimiter(1, time.Minute)
	l.Fail("ip")
	l.Reset("ip")

	if ok, _ := l.Allow("ip"); !ok {
		t.Error("reset should clear the lockout")
	}
}

func mustCIDR(t *testing.T, s string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestClientIPIgnoresForwardedHeaderFromUntrustedPeer(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:1234"
	r.Header.Set("X-Forwarded-For", "1.1.1.1")

	if got := ClientIP(r, nil); got != "203.0.113.9" {
		t.Errorf("want direct peer when no proxies are trusted, got %s", got)
	}
}

func TestClientIPUsesForwardedHeaderFromTrustedProxy(t *testing.T) {
	trusted := []*net.IPNet{mustCIDR(t, "127.0.0.1/32")}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 127.0.0.1")

	if got := ClientIP(r, trusted); got != "203.0.113.7" {
		t.Errorf("want the client behind the proxy, got %s", got)
	}
}

func TestClientIPRejectsSpoofedChain(t *testing.T) {
	trusted := []*net.IPNet{mustCIDR(t, "127.0.0.1/32")}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "10.0.0.1, 203.0.113.7")

	if got := ClientIP(r, trusted); got != "203.0.113.7" {
		t.Errorf("want the rightmost untrusted hop, got %s", got)
	}
}

func TestRequestIsSecureOnlyTrustsProxyHeader(t *testing.T) {
	trusted := []*net.IPNet{mustCIDR(t, "127.0.0.1/32")}

	spoofed := httptest.NewRequest(http.MethodGet, "/", nil)
	spoofed.RemoteAddr = "203.0.113.9:1"
	spoofed.Header.Set("X-Forwarded-Proto", "https")
	if RequestIsSecure(spoofed, trusted) {
		t.Error("must not trust X-Forwarded-Proto from an untrusted peer")
	}

	viaProxy := httptest.NewRequest(http.MethodGet, "/", nil)
	viaProxy.RemoteAddr = "127.0.0.1:1"
	viaProxy.Header.Set("X-Forwarded-Proto", "https")
	if !RequestIsSecure(viaProxy, trusted) {
		t.Error("should trust X-Forwarded-Proto from a trusted proxy")
	}
}

func TestMiddlewareBlocksUnauthenticated(t *testing.T) {
	a := testAuth(t, "correct-horse")
	h := a.Middleware(protected())

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Errorf("want redirect for html navigation, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != LoginPath {
		t.Errorf("want redirect to %s, got %s", LoginPath, loc)
	}
}

func TestMiddlewareReturnsJSONForAPICalls(t *testing.T) {
	a := testAuth(t, "correct-horse")
	h := a.Middleware(protected())

	r := httptest.NewRequest(http.MethodPost, "/exa.language_server_pb.Service/Something", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401 for API call, got %d", w.Code)
	}
}

func TestMiddlewareBlocksUnauthenticatedWebsocket(t *testing.T) {
	a := testAuth(t, "correct-horse")
	h := a.Middleware(protected())

	r := httptest.NewRequest(http.MethodGet, "/connect-websocket", nil)
	r.Header.Set("Upgrade", "websocket")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401 for websocket upgrade, got %d", w.Code)
	}
}

func TestPublicPathsBypassAuth(t *testing.T) {
	a := testAuth(t, "correct-horse")
	h := a.Middleware(protected())

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))

	if w.Code != http.StatusOK {
		t.Errorf("public assets should bypass auth, got %d", w.Code)
	}
}

func TestLoginFlowSetsHardenedCookie(t *testing.T) {
	a := testAuth(t, "correct-horse")
	h := a.Middleware(protected())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, LoginAPIPath, strings.NewReader(`{"password":"correct-horse"}`))
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("login should succeed, got %d: %s", w.Code, w.Body.String())
	}

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("want one cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Name != CookieName || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie not hardened: %+v", c)
	}

	authed := httptest.NewRequest(http.MethodGet, "/", nil)
	authed.AddCookie(c)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, authed)

	if w2.Body.String() != "secret" {
		t.Errorf("authenticated request should reach the app, got %q", w2.Body.String())
	}
}

func TestLoginRateLimited(t *testing.T) {
	a := testAuth(t, "correct-horse")
	h := a.Middleware(protected())

	var last int
	for i := 0; i < loginMaxFailures+1; i++ {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, LoginAPIPath, strings.NewReader(`{"password":"nope"}`))
		r.RemoteAddr = "198.51.100.5:1000"
		h.ServeHTTP(w, r)
		last = w.Code
	}

	if last != http.StatusTooManyRequests {
		t.Errorf("want 429 after %d failures, got %d", loginMaxFailures, last)
	}
}

func TestCorrectPasswordRejectedWhileLockedOut(t *testing.T) {
	a := testAuth(t, "correct-horse")
	h := a.Middleware(protected())

	for i := 0; i < loginMaxFailures; i++ {
		r := httptest.NewRequest(http.MethodPost, LoginAPIPath, strings.NewReader(`{"password":"nope"}`))
		r.RemoteAddr = "198.51.100.6:1000"
		h.ServeHTTP(httptest.NewRecorder(), r)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, LoginAPIPath, strings.NewReader(`{"password":"correct-horse"}`))
	r.RemoteAddr = "198.51.100.6:1000"
	h.ServeHTTP(w, r)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("lockout must apply even to the correct password, got %d", w.Code)
	}
}

func TestEnrollmentTokenIsSingleUse(t *testing.T) {
	a := testAuth(t, "correct-horse")
	h := a.Middleware(protected())

	tok, err := a.NewEnrollmentToken()
	if err != nil {
		t.Fatal(err)
	}

	// 1. Messenger link preview bots (HEAD and GET) must NOT consume the token or grant a cookie
	headRec := httptest.NewRecorder()
	h.ServeHTTP(headRec, httptest.NewRequest(http.MethodHead, LoginPath+"?t="+tok, nil))
	if headRec.Code != http.StatusOK || len(headRec.Result().Cookies()) != 0 {
		t.Fatalf("HEAD preview should return 200 without cookie, got %d", headRec.Code)
	}

	previewRec := httptest.NewRecorder()
	h.ServeHTTP(previewRec, httptest.NewRequest(http.MethodGet, LoginPath+"?t="+tok, nil))
	if previewRec.Code != http.StatusOK || len(previewRec.Result().Cookies()) != 0 {
		t.Fatalf("GET confirmation page should return 200 without consuming token, got %d", previewRec.Code)
	}
	if !strings.Contains(previewRec.Body.String(), `method="POST"`) {
		t.Fatalf("expected confirmation form in GET response")
	}

	// 2. Submitting the confirmation form via POST consumes the token and starts a session
	postReq := httptest.NewRequest(http.MethodPost, LoginPath, strings.NewReader("t="+tok))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, postReq)

	if w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
		t.Fatalf("enrollment POST should log in and redirect home, got %d %s", w.Code, w.Header().Get("Location"))
	}
	if len(w.Result().Cookies()) != 1 {
		t.Fatal("enrollment should set a session cookie")
	}

	// 3. Reusing the consumed token via GET or POST must fail
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, LoginPath+"?t="+tok, nil))
	if w2.Header().Get("Location") != LoginPath || len(w2.Result().Cookies()) != 0 {
		t.Error("reused enrollment token on GET must redirect to login without cookie")
	}

	postReuse := httptest.NewRequest(http.MethodPost, LoginPath, strings.NewReader("t="+tok))
	postReuse.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, postReuse)
	if w3.Header().Get("Location") != LoginPath || len(w3.Result().Cookies()) != 0 {
		t.Error("reused enrollment token on POST must redirect to login without cookie")
	}
}

func TestMiddlewareBlocksCrossOriginStateChangeAndMultipleXFF(t *testing.T) {
	a := testAuth(t, "correct-horse")
	h := a.Middleware(protected())

	// Cross-origin login CSRF attempt must be rejected with 403
	csrfLogin := httptest.NewRequest(http.MethodPost, LoginAPIPath, strings.NewReader(`{"password":"correct-horse"}`))
	csrfLogin.Host = "agy.example.com"
	csrfLogin.Header.Set("Origin", "https://evil.example")
	csrfLogin.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, csrfLogin)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-origin POST login CSRF, got %d", rec.Code)
	}

	// Multiple X-Forwarded-For header lines must inspect rightmost untrusted hop
	trusted := []*net.IPNet{mustCIDR(t, "127.0.0.1/32")}
	xffReq := httptest.NewRequest(http.MethodGet, "/", nil)
	xffReq.RemoteAddr = "127.0.0.1:5555"
	xffReq.Header.Add("X-Forwarded-For", "1.1.1.1")
	xffReq.Header.Add("X-Forwarded-For", "198.51.100.42")
	if got := ClientIP(xffReq, trusted); got != "198.51.100.42" {
		t.Errorf("ClientIP with multiple X-Forwarded-For headers = %s, want 198.51.100.42", got)
	}

	// Sibling subdomain (Sec-Fetch-Site: same-site) must be rejected even when reverse proxy forwards no external Host
	sibling := httptest.NewRequest(http.MethodPost, LoginAPIPath, strings.NewReader(`{"password":"correct-horse"}`))
	sibling.Host = "127.0.0.1:8765"
	sibling.RemoteAddr = "127.0.0.1:5555"
	sibling.Header.Set("Origin", "https://other.example.com")
	sibling.Header.Set("Sec-Fetch-Site", "same-site")
	sibRec := httptest.NewRecorder()
	h.ServeHTTP(sibRec, sibling)
	if sibRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for Sec-Fetch-Site: same-site behind non-forwarding proxy, got %d", sibRec.Code)
	}

	// External Origin without Sec-Fetch-Site must STILL be rejected even when Host is loopback
	noSFS := httptest.NewRequest(http.MethodPost, LoginAPIPath, strings.NewReader(`{"password":"correct-horse"}`))
	noSFS.Host = "127.0.0.1:8765"
	noSFS.RemoteAddr = "127.0.0.1:5555"
	noSFS.Header.Set("Origin", "https://attacker.example")
	noSFSRec := httptest.NewRecorder()
	h.ServeHTTP(noSFSRec, noSFS)
	if noSFSRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-origin POST without Sec-Fetch-Site on loopback Host, got %d", noSFSRec.Code)
	}

	// Genuine same-origin behind non-forwarding loopback proxy (Sec-Fetch-Site: same-origin) must succeed
	legitProxy := httptest.NewRequest(http.MethodPost, LoginAPIPath, strings.NewReader(`{"password":"correct-horse"}`))
	legitProxy.Host = "127.0.0.1:8765"
	legitProxy.RemoteAddr = "127.0.0.1:5555"
	legitProxy.Header.Set("Origin", "https://agy.example.com")
	legitProxy.Header.Set("Sec-Fetch-Site", "same-origin")
	legitRec := httptest.NewRecorder()
	h.ServeHTTP(legitRec, legitProxy)
	if legitRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for genuine Sec-Fetch-Site: same-origin behind loopback proxy, got %d", legitRec.Code)
	}

	// Untrusted loopback peer must not spoof X-Forwarded-Host when Trusted is empty
	spoofXFH := httptest.NewRequest(http.MethodPost, LoginAPIPath, strings.NewReader(`{"password":"correct-horse"}`))
	spoofXFH.Host = "agy.example.com"
	spoofXFH.RemoteAddr = "127.0.0.1:5555"
	spoofXFH.Header.Set("Origin", "https://evil.example.com")
	spoofXFH.Header.Set("X-Forwarded-Host", "evil.example.com")
	spoofRec := httptest.NewRecorder()
	h.ServeHTTP(spoofRec, spoofXFH)
	if spoofRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when X-Forwarded-Host is sent without TrustedProxies, got %d", spoofRec.Code)
	}
}

func TestLimiterBucketsIPv6SubnetsAndProtectsLockedEntries(t *testing.T) {
	l := NewLimiter(2, 5*time.Minute)

	// Two distinct addresses in the same IPv6 /64 must share one bucket
	l.Fail("2001:db8:abcd:1234::1")
	l.Fail("2001:db8:abcd:1234:ffff:eeee:dddd:2")
	if ok, _ := l.Allow("2001:db8:abcd:1234::9999"); ok {
		t.Fatal("expected IPv6 /64 bucket to lock out all addresses in the same /64")
	}

	// Churning thousands of unlocked entries must NOT evict a locked entry or block new clients
	for i := 0; i < maxLimiterEntries+200; i++ {
		l.Fail(fmt.Sprintf("10.0.%d.%d", (i>>8)&0xff, i&0xff))
	}
	if ok, _ := l.Allow("2001:db8:abcd:1234::1"); ok {
		t.Fatal("locked entry must never be evicted by unlocked key churn")
	}
	if ok, _ := l.Allow("203.0.113.250"); !ok {
		t.Fatal("unlocked churn must still allow unseen client IPs")
	}

	// Saturating all 4,096 lockout slots must NEVER block legitimate unseen client IPs
	// (preventing distributed lockout DoS), must protect repeat-offender multi-strike locks,
	// must ignore single-shot noise probes against active locks, and must lock newly failing attackers.
	saturated := NewLimiter(2, 5*time.Minute)
	repeatOffender := "198.18.0.0"
	saturated.Fail(repeatOffender)
	saturated.Fail(repeatOffender) // strike 1
	saturated.mu.Lock()
	saturated.entries[repeatOffender].lockedUntil = time.Now().Add(-time.Second)
	saturated.mu.Unlock()
	saturated.Fail(repeatOffender)
	saturated.Fail(repeatOffender) // strike 2 (higher backoff)

	for i := 1; i < maxLimiterEntries; i++ {
		ip := fmt.Sprintf("198.18.%d.%d", (i>>8)&0xff, i&0xff)
		saturated.Fail(ip)
		saturated.Fail(ip)
	}

	// Single-failure probes from thousands of spoofed IPs must not evict any active lockout
	for i := 0; i < maxPendingEntries+256; i++ {
		probe := fmt.Sprintf("198.19.%d.%d", (i>>8)&0xff, i&0xff)
		saturated.Fail(probe)
	}
	if ok, _ := saturated.Allow(repeatOffender); ok {
		t.Fatal("multi-strike repeat offender must never be evicted under saturation")
	}
	if ok, _ := saturated.Allow("198.18.15.255"); ok {
		t.Fatal("single-failure probe churn must not evict active locked entries")
	}

	// Unseen legitimate client IP must STILL be allowed so distributed attackers cannot DoS sign-in
	if ok, _ := saturated.Allow("203.0.113.251"); !ok {
		t.Fatal("unseen legitimate client IP must remain allowed even when all lockout slots are full")
	}

	// A 4,097th attacker reaching max failures must be locked while preserving higher-strike offenders
	newAttacker := "203.0.113.252"
	saturated.Fail(newAttacker)
	saturated.Fail(newAttacker)
	if ok, _ := saturated.Allow(newAttacker); ok {
		t.Fatal("newly failing attacker must be locked even when lockout table was saturated")
	}
	if ok, _ := saturated.Allow(repeatOffender); ok {
		t.Fatal("higher-strike repeat offender must remain locked when evicting a 1-strike entry")
	}

	// TokenBucket enforces burst cap without long lockout and refills promptly
	tb := NewTokenBucket(20, 2)
	if ok, _ := tb.Allow(); !ok {
		t.Fatal("expected first token to succeed")
	}
	if ok, _ := tb.Allow(); !ok {
		t.Fatal("expected second token to succeed")
	}
	if ok, retry := tb.Allow(); ok || retry <= 0 {
		t.Fatalf("expected third immediate token to be throttled, ok=%v retry=%v", ok, retry)
	}
	time.Sleep(60 * time.Millisecond)
	if ok, _ := tb.Allow(); !ok {
		t.Fatal("expected token bucket to refill after 60ms at 20/sec")
	}
}

func TestEnrollmentHeadersAndSeparateRateLimiter(t *testing.T) {
	a := testAuth(t, "correct-horse")
	h := a.Middleware(protected())

	tok, err := a.NewEnrollmentToken()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, LoginPath+"?t="+tok, nil))
	if rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("missing security headers on enrollment page: %+v", rec.Header())
	}

	// Repeated invalid GETs on expired QR links must not lock out password login for that IP
	for i := 0; i < enrollMaxFailures+2; i++ {
		badQR := httptest.NewRequest(http.MethodGet, LoginPath+"?t=expired-token", nil)
		badQR.RemoteAddr = "198.51.100.77:1234"
		h.ServeHTTP(httptest.NewRecorder(), badQR)
	}
	pwLogin := httptest.NewRequest(http.MethodPost, LoginAPIPath, strings.NewReader(`{"password":"correct-horse"}`))
	pwLogin.RemoteAddr = "198.51.100.77:1234"
	pwRec := httptest.NewRecorder()
	h.ServeHTTP(pwRec, pwLogin)
	if pwRec.Code != http.StatusOK {
		t.Fatalf("password login must remain available after expired QR link previews, got %d", pwRec.Code)
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	a := testAuth(t, "correct-horse")
	h := a.Middleware(protected())

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, LoginAPIPath, strings.NewReader(`{"password":"correct-horse"}`)))
	cookie := w.Result().Cookies()[0]

	out := httptest.NewRecorder()
	logout := httptest.NewRequest(http.MethodPost, LogoutAPIPath, nil)
	logout.AddCookie(cookie)
	h.ServeHTTP(out, logout)

	if out.Code != http.StatusOK {
		t.Fatalf("logout should succeed, got %d", out.Code)
	}

	after := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(cookie)
	r.Header.Set("Accept", "text/html")
	h.ServeHTTP(after, r)

	if after.Code != http.StatusFound {
		t.Error("session should be invalid after logout")
	}
}

func TestLoginErrorDoesNotLeakWhetherPasswordExists(t *testing.T) {
	a := testAuth(t, "correct-horse")
	h := a.Middleware(protected())

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, LoginAPIPath, strings.NewReader(`{"password":""}`)))

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "Incorrect password." {
		t.Errorf("unexpected error message: %v", body["error"])
	}
}

func TestLoginAPISupportsFormBodyAndVerifyContextCancellation(t *testing.T) {
	a := testAuth(t, "correct-horse")
	h := a.Middleware(protected())

	// 1. application/x-www-form-urlencoded POST body must succeed
	formReq := httptest.NewRequest(http.MethodPost, LoginAPIPath, strings.NewReader("password=correct-horse"))
	formReq.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	formRec := httptest.NewRecorder()
	h.ServeHTTP(formRec, formReq)
	if formRec.Code != http.StatusOK || len(formRec.Result().Cookies()) != 1 {
		t.Fatalf("expected 200 and session cookie for form-encoded login, got %d: %s", formRec.Code, formRec.Body.String())
	}

	// 2. VerifyContext must abort immediately when all PBKDF2 worker slots are occupied and ctx is canceled
	for i := 0; i < maxVerifyWorkers; i++ {
		verifySem <- struct{}{}
	}
	defer func() {
		for i := 0; i < maxVerifyWorkers; i++ {
			<-verifySem
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if a.Credentials().VerifyContext(ctx, "correct-horse") {
		t.Fatal("expected VerifyContext to return false when context is canceled")
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("VerifyContext blocked despite canceled context (%s)", elapsed)
	}

	// Canceled request on handleLoginAPI must not count as an incorrect password strike against the client IP
	for i := 0; i < loginMaxFailures+2; i++ {
		cancelReq := httptest.NewRequest(http.MethodPost, LoginAPIPath, strings.NewReader(`{"password":"correct-horse"}`)).WithContext(ctx)
		cancelReq.RemoteAddr = "198.51.100.88:4321"
		h.ServeHTTP(httptest.NewRecorder(), cancelReq)
	}
	if ok, _ := a.perIP.Allow("198.51.100.88"); !ok {
		t.Fatal("canceled login requests must not lock out client IP")
	}
}

func TestPublicURLHTTPSPreservesPlaintextLocalAndPrivateAccess(t *testing.T) {
	dir := t.TempDir()
	creds, _, err := LoadOrCreateCredentials(filepath.Join(dir, "creds.json"), "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := NewSessionStore(filepath.Join(dir, "sessions.json"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	a := New(Options{
		Credentials: creds,
		Sessions:    sessions,
		PublicURL:   "https://agy.example.com",
		LoginPage:   func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) },
	})
	h := a.Middleware(protected())

	cases := []struct {
		name   string
		host   string
		origin string
		want   int
	}{
		{"public https origin allowed", "127.0.0.1:8765", "https://agy.example.com", http.StatusOK},
		{"public http scheme downgrade blocked", "agy.example.com", "http://agy.example.com", http.StatusForbidden},
		{"loopback plaintext direct access allowed", "127.0.0.1:8765", "http://127.0.0.1:8765", http.StatusOK},
		{"localhost plaintext direct access allowed", "localhost:8765", "http://localhost:8765", http.StatusOK},
		{"tailscale plaintext direct access allowed", "100.64.0.1:8765", "http://100.64.0.1:8765", http.StatusOK},
		{"lan plaintext direct access allowed", "192.168.1.25:8765", "http://192.168.1.25:8765", http.StatusOK},
		{"loopback cross-port rejected", "127.0.0.1:8765", "http://127.0.0.1:3000", http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, LoginAPIPath, strings.NewReader(`{"password":"correct-horse"}`))
			req.Host = tc.host
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("got status %d, want %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
