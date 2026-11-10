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
	// ErrExpired means the language server gave up on the attempt, so the code
	// or address can no longer be used.
	ErrExpired = errors.New("this sign-in expired, so start it again")
	// ErrNoCode means the pasted address carried no authorization code.
	ErrNoCode = errors.New("that address has no authorization code in it")

	// errNoAuthCode means the language server did not switch to the auth-code
	// flow, so the loopback flow Login started is still the one to use.
	errNoAuthCode = errors.New("no auth-code sign-in")
)

type session struct {
	mode        Mode
	authURL     string
	callbackURL string
	cancel      context.CancelFunc
	// done is closed when the Login RPC returns, which ends the attempt.
	done chan struct{}
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

	// beginMu serialises Begin, so two tabs opening the page share one attempt
	// instead of the second Login replacing the first.
	beginMu sync.Mutex
	mu      sync.Mutex
	pending *session

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
	return &Coordinator{instance: instance, urlFile: urlFile}
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

	c.beginMu.Lock()
	defer c.beginMu.Unlock()
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
		// Login blocks until the attempt succeeds, fails or expires. Its result
		// is ignored: SignedIn is the source of truth afterwards.
		_, _ = c.instance.Call(loginCtx, "Login", map[string]any{})
		close(sess.done)
	}()

	loopbackURL, err := c.waitForURL(ctx, sess.done)
	if err != nil {
		cancel()
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
		// is unknown. Drop it rather than guess.
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
		return "", "", fmt.Errorf("the language server ended the sign-in before it could be used")
	}

	c.mu.Lock()
	c.pending = sess
	c.mu.Unlock()

	return sess.authURL, sess.mode, nil
}

// switchToAuthCode asks the language server to move the attempt in flight to
// the auth-code flow and returns the consent URL for it. Language servers older
// than 2.31 do not have the RPC; that and any answer that leaves the attempt in
// loopback mode come back as errNoAuthCode.
func (c *Coordinator) switchToAuthCode(ctx context.Context) (string, error) {
	data, err := c.instance.Call(ctx, "SwitchLoginToAuthCode", map[string]any{"openBrowser": false})
	if err != nil {
		return "", fmt.Errorf("%w: %v", errNoAuthCode, err)
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
			return "", fmt.Errorf("the language server ended the sign-in before producing a URL")
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
	if !pending.live() {
		c.finish(pending)
		return ErrExpired
	}

	if pending.mode == ModeAuthCode {
		if err := c.submitAuthCode(ctx, pending, pasted); err != nil {
			return err
		}
	} else {
		query, err := queryFrom(pasted)
		if err != nil {
			return err
		}
		if _, err := fetch(ctx, pending.callbackURL+"?"+query); err != nil {
			c.finish(pending)
			return fmt.Errorf("could not hand the code to Antigravity: %w", err)
		}
	}

	// The Login RPC needs a moment to exchange the code for a token.
	for i := 0; i < 20; i++ {
		if c.instance.SignedIn(ctx) {
			c.authMu.Lock()
			c.cached = authSignedIn
			c.cachedAt = time.Now()
			c.authMu.Unlock()
			c.finish(pending)
			return nil
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
	if !pending.live() {
		c.finish(pending)
		return ErrExpired
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

	client := &http.Client{Timeout: 30 * time.Second}

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
