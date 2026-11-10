// Package signin drives Antigravity's Google sign-in from a browser that is not
// on the same machine as the language server.
//
// Antigravity cannot sign in through its own web UI: the standalone build wires
// a stub auth service that only logs to the console, and its OAuth client is an
// installed-app client, so Google rejects any redirect_uri that is not a
// loopback address. What does work is driving the language server's Login RPC
// directly. It builds a Google consent URL, tries to open it with xdg-open, then
// waits for the callback on 127.0.0.1.
//
// This package captures that URL with a shim standing in for xdg-open. From
// 2.31 the language server can then switch the attempt to an auth-code flow,
// where Google redirects to a page on antigravity.google that shows a one-time
// code. When it can, Begin switches and hands that URL to the browser, and
// Complete submits the code the user pastes back. Older language servers lack
// the switch, so Begin falls back to the loopback URL and Complete forwards the
// address the browser was redirected to afterwards to the waiting callback
// listener. Either way sign-in completes with no SSH tunnel.
package signin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/lsproc"
)

const (
	// urlTimeout bounds how long we wait for the language server to hand its
	// consent URL to the shim.
	urlTimeout = 20 * time.Second
	// loginTimeout bounds the Login RPC, which blocks until the attempt ends.
	// The language server ends it on its own after 5 minutes.
	loginTimeout = 15 * time.Minute
)

// Mode says what the user pastes back once Google is done with them.
type Mode string

const (
	// ModeAuthCode means Google ends on a page that shows a one-time code.
	ModeAuthCode Mode = "authCode"
	// ModeLoopback means Google redirects to a localhost address that fails to
	// load, and the user pastes that address.
	ModeLoopback Mode = "loopback"
)

var (
	// ErrUnavailable means sign-in cannot be driven here, usually because the
	// language server was started by the desktop app rather than by us.
	ErrUnavailable = errors.New("sign-in cannot be started on this instance")
	// ErrNoSession means Complete was called before Begin.
	ErrNoSession = errors.New("no sign-in is in progress")
	// ErrExpired means the language server gave up on the attempt after
	// waiting too long, so the code or address can no longer be used.
	ErrExpired = errors.New("this sign-in expired, so start it again")
	// ErrFailed means the attempt ended without signing in for some other
	// reason. Errors that wrap it carry the language server's own account.
	ErrFailed = errors.New("Google sign-in did not complete")
	// ErrNoCode means the pasted address carried no authorization code.
	ErrNoCode = errors.New("that address has no authorization code in it")

	// errNoAuthCode means the language server did not switch to the auth-code
	// flow, so the loopback flow Login started is still the one to use.
	errNoAuthCode = errors.New("no auth-code sign-in")
	// errEndedEarly means Login returned before its URL could be handed out.
	errEndedEarly = errors.New("the language server ended the sign-in before it could be used")
)

type session struct {
	mode        Mode
	authURL     string
	callbackURL string
	cancel      context.CancelFunc
	// done is closed when the Login RPC returns, which ends the attempt.
	done chan struct{}
	// loginOK and endReason say how Login returned: signed in, or why not,
	// empty when it gave no reason. They are set before done is closed and
	// read only after.
	loginOK   bool
	endReason string
}

// failed reports whether Login has returned without signing in.
func (s *session) failed() bool {
	return !s.live() && !s.loginOK
}

// live reports whether the language server is still waiting on this attempt.
func (s *session) live() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

// Coordinator owns at most one in-flight sign-in attempt.
type Coordinator struct {
	instance *lsproc.Instance
	urlFile  string

	// beginning serialises Begin, so two tabs opening the page share one
	// attempt instead of the second Login replacing the first. It is a channel
	// so a caller can stop waiting for it.
	beginning chan struct{}
	mu        sync.Mutex
	pending   *session

	authMu     sync.Mutex
	cached     authState
	cachedAt   time.Time
	refreshing bool
}

// New returns a Coordinator. urlFile is where the browser shim records URLs; it
// must be the same path passed to lsproc.WriteBrowserShim. An empty urlFile
// disables sign-in, which is the right behaviour when the desktop app owns the
// language server.
func New(instance *lsproc.Instance, urlFile string) *Coordinator {
	return &Coordinator{instance: instance, urlFile: urlFile, beginning: make(chan struct{}, 1)}
}

// Available reports whether sign-in can be driven from here.
func (c *Coordinator) Available() bool {
	return c.instance != nil && c.urlFile != ""
}

// authState is what the language server last said about the credentials.
type authState int

const (
	authUnknown authState = iota
	authSignedIn
	authSignedOut
)

// SignedIn reports whether Antigravity already holds valid credentials,
// caching positive results for 30 seconds (with background revalidation once
// stale) and negative results for 3 seconds to avoid querying the language
// server on every HTTP request. A failed query counts as not signed in.
func (c *Coordinator) SignedIn(ctx context.Context) bool {
	return c.authStatus(ctx) == authSignedIn
}

// SignedInFresh queries the language server directly, bypassing the in-memory
// cache, and updates the cached state with the fresh answer. Use it on explicit
// sign-in routes so logging out in the web app and clicking Sign In never
// serves a stale "Already signed in" page.
func (c *Coordinator) SignedInFresh(ctx context.Context) bool {
	return c.queryAuthStatus(ctx) == authSignedIn
}

// NeedsSignIn reports whether the language server answered that it holds no
// valid credentials. Unlike !SignedIn it is false when the question went
// unanswered, so a slow or failed query never sends a signed-in user to the
// sign-in page.
func (c *Coordinator) NeedsSignIn(ctx context.Context) bool {
	return c.authStatus(ctx) == authSignedOut
}

// NeedsSignInFresh queries the language server directly, bypassing the
// in-memory cache, and reports whether it answered that it holds no valid
// credentials.
func (c *Coordinator) NeedsSignInFresh(ctx context.Context) bool {
	return c.queryAuthStatus(ctx) == authSignedOut
}

func (c *Coordinator) authStatus(ctx context.Context) authState {
	if c.instance == nil {
		return authUnknown
	}

	c.authMu.Lock()
	age := time.Since(c.cachedAt)
	if !c.cachedAt.IsZero() {
		if c.cached == authSignedIn {
			if age >= 30*time.Second && !c.refreshing {
				c.refreshing = true
				prevCachedAt := c.cachedAt
				go c.refreshSignedIn(prevCachedAt)
			}
			c.authMu.Unlock()
			return authSignedIn
		}
		if age < 3*time.Second {
			state := c.cached
			c.authMu.Unlock()
			return state
		}
	}
	c.authMu.Unlock()

	return c.queryAuthStatus(ctx)
}

func (c *Coordinator) queryAuthStatus(ctx context.Context) authState {
	if c.instance == nil {
		return authUnknown
	}

	state := authSignedOut
	signedIn, err := c.instance.AuthStatus(ctx)
	switch {
	case err != nil:
		state = authUnknown
	case signedIn:
		state = authSignedIn
	}

	c.authMu.Lock()
	c.cached = state
	c.cachedAt = time.Now()
	c.authMu.Unlock()

	return state
}

// refreshSignedIn revalidates a previously confirmed signed-in state in the
// background so page navigations never block on a synchronous GetAuthStatus RPC
// once the server is signed in.
func (c *Coordinator) refreshSignedIn(prevCachedAt time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	signedIn, err := c.instance.AuthStatus(ctx)

	c.authMu.Lock()
	defer c.authMu.Unlock()
	c.refreshing = false
	if c.cachedAt.IsZero() || !c.cachedAt.Equal(prevCachedAt) {
		return
	}
	if err == nil {
		if signedIn {
			c.cached = authSignedIn
		} else {
			c.cached = authSignedOut
		}
	}
	c.cachedAt = time.Now()
}

// Begin starts a sign-in attempt and returns the Google consent URL to open,
// along with what the user will paste back. While an attempt is still in flight
// its URL is returned instead, so reloading the page, as iOS does when the user
// comes back from Google, does not throw away the code they just copied.
func (c *Coordinator) Begin(ctx context.Context) (string, Mode, error) {
	if !c.Available() {
		return "", "", ErrUnavailable
	}

	select {
	case c.beginning <- struct{}{}:
		defer func() { <-c.beginning }()
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}

	c.mu.Lock()
	if p := c.pending; p != nil && p.live() {
		c.mu.Unlock()
		return p.authURL, p.mode, nil
	}
	if c.pending != nil {
		c.pending.cancel()
		c.pending = nil
	}
	c.mu.Unlock()

	if err := os.Remove(c.urlFile); err != nil && !os.IsNotExist(err) {
		return "", "", err
	}

	loginCtx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	sess := &session{cancel: cancel, done: make(chan struct{})}
	go func() {
		// Login blocks until the attempt succeeds, fails or expires. SignedIn
		// is the source of truth for success; what Login says is kept to
		// explain a failure.
		data, err := c.instance.Call(loginCtx, "Login", map[string]any{})
		sess.loginOK, sess.endReason = loginOutcome(data, err)
		close(sess.done)
		cancel()
	}()

	loopbackURL, err := c.waitForURL(ctx, sess.done)
	if err != nil {
		cancel()
		if errors.Is(err, errEndedEarly) {
			return "", "", endedEarly(sess)
		}
		return "", "", err
	}

	authURL, err := c.switchToAuthCode(ctx)
	switch {
	case err == nil:
		sess.mode, sess.authURL = ModeAuthCode, authURL
	case errors.Is(err, errNoAuthCode) && ctx.Err() == nil:
		log.Printf("[signin] using the loopback sign-in: %v", err)
		callbackURL, err := callbackFrom(loopbackURL)
		if err != nil {
			cancel()
			return "", "", err
		}
		sess.mode, sess.authURL, sess.callbackURL = ModeLoopback, loopbackURL, callbackURL
	default:
		// The switch may have taken effect, so which URL the attempt expects
		// is unknown. Drop it rather than guess; the next Login replaces it on
		// the language server.
		cancel()
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		return "", "", err
	}

	// Login can fail just after recording its URL. Handing that URL out would
	// send the user through Google for nothing.
	if !sess.live() {
		cancel()
		return "", "", endedEarly(sess)
	}

	c.mu.Lock()
	c.pending = sess
	c.mu.Unlock()

	return sess.authURL, sess.mode, nil
}

// switchToAuthCode asks the language server to move the attempt in flight to
// the auth-code flow and returns the consent URL for it. Language servers older
// than 2.31 do not have the RPC. They answer 404, or serve the web UI with 200;
// those and any answer that leaves the attempt in loopback mode come back as
// errNoAuthCode. Anything else, such as a dropped connection or another error
// status, may have come after the switch, so it comes back as a plain error.
func (c *Coordinator) switchToAuthCode(ctx context.Context) (string, error) {
	data, err := c.instance.Call(ctx, "SwitchLoginToAuthCode", map[string]any{"openBrowser": false})
	if err != nil {
		// 404 is an unknown method and 501 is Connect's unimplemented; neither
		// can have switched.
		var status *lsproc.StatusError
		if errors.As(err, &status) && (status.Code == http.StatusNotFound || status.Code == http.StatusNotImplemented) {
			return "", fmt.Errorf("%w: %v", errNoAuthCode, err)
		}
		return "", err
	}

	var out struct {
		Active  bool            `json:"active"`
		AuthURL string          `json:"authUrl"`
		Mode    json.RawMessage `json:"mode"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("%w: SwitchLoginToAuthCode answered with something other than JSON", errNoAuthCode)
	}
	if !out.Active {
		return "", fmt.Errorf("the language server has no sign-in in flight")
	}
	if !isAuthCodeMode(out.Mode) {
		return "", fmt.Errorf("%w: SwitchLoginToAuthCode left the mode at %s", errNoAuthCode, out.Mode)
	}
	if !strings.HasPrefix(out.AuthURL, "https://accounts.google.com/") {
		return "", fmt.Errorf("SwitchLoginToAuthCode switched but returned no Google URL")
	}
	return out.AuthURL, nil
}

// isAuthCodeMode accepts LoginSessionMode as protojson writes it, by name, and
// as a number in case a server writes enums that way.
func isAuthCodeMode(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return string(raw) == `"LOGIN_SESSION_MODE_AUTH_CODE"` || string(raw) == "2"
}

// waitForURL waits for the shim to record the consent URL. done is closed if
// the Login RPC returns first, in which case no URL is coming.
func (c *Coordinator) waitForURL(ctx context.Context, done <-chan struct{}) (string, error) {
	deadline := time.Now().Add(urlTimeout)

	for {
		if data, err := os.ReadFile(c.urlFile); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "https://accounts.google.com/") {
					return line, nil
				}
			}
		}

		if time.Now().After(deadline) {
			return "", fmt.Errorf("the language server did not produce a sign-in URL")
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-done:
			return "", errEndedEarly
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// callbackFrom derives the loopback address the language server is listening on
// from the consent URL's redirect_uri.
func callbackFrom(authURL string) (string, error) {
	parsed, err := url.Parse(authURL)
	if err != nil {
		return "", err
	}

	redirect := parsed.Query().Get("redirect_uri")
	if redirect == "" {
		return "", fmt.Errorf("sign-in URL has no redirect_uri")
	}

	target, err := url.Parse(redirect)
	if err != nil {
		return "", err
	}
	if target.Port() == "" {
		return "", fmt.Errorf("redirect_uri %q has no port", redirect)
	}

	// The language server binds the callback to loopback, and "localhost" may
	// resolve to IPv6 first, so address it explicitly.
	target.Host = "127.0.0.1:" + target.Port()
	return target.String(), nil
}

// Complete hands what the user pasted to the language server and reports
// whether Antigravity ended up signed in. In auth-code mode that is the code
// Google showed, or the address of the page that showed it. In loopback mode it
// is the address the browser was redirected to.
func (c *Coordinator) Complete(ctx context.Context, pasted string) error {
	c.mu.Lock()
	pending := c.pending
	c.mu.Unlock()

	if pending == nil {
		return ErrNoSession
	}
	if pending.failed() {
		return c.ended(pending)
	}

	// An attempt whose Login has already signed in only needs confirming.
	if pending.live() && pending.mode == ModeAuthCode {
		if err := c.submitAuthCode(ctx, pending, pasted); err != nil {
			return err
		}
	} else if pending.live() {
		query, err := queryFrom(pasted)
		if err != nil {
			return err
		}
		if _, err := fetch(ctx, pending.callbackURL+"?"+query); err != nil {
			c.finish(pending)
			return fmt.Errorf("%w: Antigravity could not take that address: %v", ErrFailed, err)
		}
	}

	// The Login RPC needs a moment to exchange the code for a token. If it
	// returns without signing in, the exchange failed and the attempt is over,
	// so there is no point waiting the full time. A few checks after it
	// returns cover GetAuthStatus catching up.
	checksAfterLogin := 3
	for i := 0; i < 20; i++ {
		if c.instance.SignedIn(ctx) {
			c.authMu.Lock()
			c.cached = authSignedIn
			c.cachedAt = time.Now()
			c.authMu.Unlock()
			// Let Login finish on its own rather than cancel it mid-reply.
			select {
			case <-pending.done:
			case <-ctx.Done():
			case <-time.After(3 * time.Second):
			}
			c.finish(pending)
			return nil
		}
		if pending.failed() {
			if checksAfterLogin == 0 {
				return c.ended(pending)
			}
			checksAfterLogin--
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	return fmt.Errorf("Antigravity accepted the code but is still not signed in")
}

// submitAuthCode passes the pasted text to SubmitLoginAuthCode as is. The
// language server accepts a bare code or the whole callback page address and
// explains what is wrong with anything else, so it is left to judge.
func (c *Coordinator) submitAuthCode(ctx context.Context, pending *session, pasted string) error {
	code := strings.TrimSpace(pasted)
	if code == "" {
		return errors.New("paste the code Google showed you")
	}

	data, err := c.instance.Call(ctx, "SubmitLoginAuthCode", map[string]any{"code": code})

	var out struct {
		Accepted   bool   `json:"accepted"`
		RetryError string `json:"retryError"`
		// Message is set instead when the RPC fails as a Connect error.
		Message string `json:"message"`
	}
	parseErr := json.Unmarshal(data, &out)
	if err == nil && parseErr == nil && out.Accepted {
		return nil
	}

	// A rejected code leaves the attempt open for another try unless the
	// language server has given up on it. When it gives up as it answers,
	// Login's answer can land just after this one, so allow it a moment.
	select {
	case <-pending.done:
	case <-ctx.Done():
	case <-time.After(300 * time.Millisecond):
	}
	if pending.failed() {
		return c.ended(pending)
	}
	switch {
	case out.RetryError != "":
		return errors.New(out.RetryError)
	case out.Message != "":
		return errors.New(out.Message)
	case err != nil:
		return fmt.Errorf("could not hand the code to Antigravity: %w", err)
	case parseErr != nil:
		return fmt.Errorf("could not read Antigravity's answer: %w", parseErr)
	}
	return errors.New("Antigravity did not accept that code")
}

// ended clears sess, whose Login has returned without signing in, and says
// why it ended.
func (c *Coordinator) ended(sess *session) error {
	c.finish(sess)
	reason := sess.endReason
	if reason == "" || strings.Contains(reason, "deadline exceeded") {
		return ErrExpired
	}
	log.Printf("[signin] sign-in failed: %s", reason)
	return fmt.Errorf("%w: %s", ErrFailed, reason)
}

// endedEarly is errEndedEarly with the language server's reason, if it gave
// one. sess must have ended.
func endedEarly(sess *session) error {
	if sess.endReason == "" {
		return errEndedEarly
	}
	return fmt.Errorf("%w: %s", errEndedEarly, sess.endReason)
}

// loginOutcome reads what Login answered: whether it signed in and, if not,
// the language server's account of why, empty when it gave none.
func loginOutcome(data []byte, err error) (bool, string) {
	var out struct {
		AuthResult map[string]json.RawMessage `json:"authResult"`
		// Message is set instead when the RPC fails as a Connect error.
		Message string `json:"message"`
	}
	_ = json.Unmarshal(data, &out)
	if string(out.AuthResult["hasValidAuth"]) == "true" {
		return true, ""
	}

	if failure := authFailure(out.AuthResult); failure != "" {
		return false, failure
	}
	var message string
	if json.Unmarshal(out.AuthResult["uiMessage"], &message) == nil && message != "" {
		return false, message
	}
	if out.Message != "" {
		return false, out.Message
	}

	// Running out of time, ours or the language server's, is an expiry. Any
	// other transport error names an internal address, so keep it to the log.
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		log.Printf("[signin] Login failed: %v", err)
		return false, "the language server stopped answering the sign-in"
	}
	return false, ""
}

// authFailures are AuthResult's failure fields with what each means. Each is a
// message whose presence is the signal, even when it is empty, as
// licenseRequired always is. generalError is left out: it also comes along
// empty when the attempt timed out, so only its reason counts.
var authFailures = []struct{ field, meaning string }{
	{"tosViolation", "the account is blocked under the terms of service"},
	{"ineligible", "the account is not eligible"},
	{"verificationRequired", "Google needs to verify the account"},
	{"licenseRequired", "the account needs an Antigravity license"},
	{"projectRequired", "the account needs a Google Cloud project"},
	{"headlessAuthRequired", "the language server asked for a different sign-in"},
}

// authFailure describes the failure an AuthResult reports, with any detail
// and link it carries, or returns "" when it reports none.
func authFailure(result map[string]json.RawMessage) string {
	var detail struct {
		Reason          string `json:"reason"`
		Message         string `json:"message"`
		VerificationURL string `json:"verificationUrl"`
		AppealURL       string `json:"appealUrl"`
		AuthURL         string `json:"authUrl"`
	}

	meaning := ""
	for _, f := range authFailures {
		if raw, ok := result[f.field]; ok && json.Unmarshal(raw, &detail) == nil {
			meaning = f.meaning
			break
		}
	}
	if meaning == "" {
		if raw, ok := result["generalError"]; !ok || json.Unmarshal(raw, &detail) != nil || detail.Reason == "" {
			return ""
		}
	}

	var parts []string
	for _, part := range []string{meaning, detail.Message, detail.Reason, detail.VerificationURL, detail.AppealURL, detail.AuthURL} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, ": ")
}

// finish ends sess. It leaves alone an attempt that has since replaced it.
func (c *Coordinator) finish(sess *session) {
	sess.cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending != sess {
		return
	}
	c.pending = nil
	_ = os.Remove(c.urlFile)
}

// fetch performs a plain GET against the loopback callback listener.
func fetch(ctx context.Context, target string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		// The callback answers with a redirect to a page on antigravity.google
		// meant for a browser. Its own status is what matters.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return body, fmt.Errorf("callback returned %s", resp.Status)
	}
	return body, nil
}

// queryFrom accepts either the whole redirected address or just its query string
// and returns the query, having checked that an authorization code is present.
func queryFrom(pasted string) (string, error) {
	pasted = strings.TrimSpace(pasted)
	if pasted == "" {
		return "", ErrNoCode
	}

	raw := pasted
	if i := strings.Index(pasted, "?"); i >= 0 {
		raw = pasted[i+1:]
	}
	raw = strings.TrimPrefix(raw, "?")

	values, err := url.ParseQuery(raw)
	if err != nil {
		return "", ErrNoCode
	}
	if values.Get("code") == "" {
		if desc := values.Get("error"); desc != "" {
			return "", fmt.Errorf("Google reported an error: %s", desc)
		}
		return "", ErrNoCode
	}
	return raw, nil
}
