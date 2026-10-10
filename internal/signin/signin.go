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
// This package captures that URL with a shim standing in for xdg-open, hands it
// to the browser, and accepts the address the browser was redirected to
// afterwards, forwarding it to the waiting callback listener. Sign-in then
// completes with no SSH tunnel and no token copied by hand.
package signin

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	// loginTimeout bounds the Login RPC, which blocks until the callback arrives.
	loginTimeout = 15 * time.Minute
	// sessionTTL is how long a captured URL stays usable before we start over.
	sessionTTL = 2 * time.Minute
)

var (
	// ErrUnavailable means sign-in cannot be driven here, usually because the
	// language server was started by the desktop app rather than by us.
	ErrUnavailable = errors.New("sign-in cannot be started on this instance")
	// ErrNoSession means Complete was called before Begin.
	ErrNoSession = errors.New("no sign-in is in progress")
	// ErrNoCode means the pasted address carried no authorization code.
	ErrNoCode = errors.New("that address has no authorization code in it")
)

type session struct {
	authURL     string
	callbackURL string
	started     time.Time
	cancel      context.CancelFunc
}

// Coordinator owns at most one in-flight sign-in attempt.
type Coordinator struct {
	instance *lsproc.Instance
	urlFile  string

	mu      sync.Mutex
	pending *session
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

// SignedIn reports whether Antigravity already holds valid credentials.
func (c *Coordinator) SignedIn(ctx context.Context) bool {
	if c.instance == nil {
		return false
	}
	return c.instance.SignedIn(ctx)
}

// Begin starts a sign-in attempt and returns the Google consent URL to open. If
// an attempt is already in flight its URL is returned instead, so refreshing the
// page does not strand the previous one.
func (c *Coordinator) Begin(ctx context.Context) (string, error) {
	if !c.Available() {
		return "", ErrUnavailable
	}

	c.mu.Lock()
	if c.pending != nil && time.Since(c.pending.started) < sessionTTL {
		authURL := c.pending.authURL
		c.mu.Unlock()
		return authURL, nil
	}
	if c.pending != nil {
		c.pending.cancel()
		c.pending = nil
	}
	c.mu.Unlock()

	if err := os.Remove(c.urlFile); err != nil && !os.IsNotExist(err) {
		return "", err
	}

	loginCtx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	go func() {
		// Login blocks until the callback arrives or the context expires. Its
		// result is ignored: SignedIn is the source of truth afterwards.
		_, _ = c.instance.Call(loginCtx, "Login", map[string]any{})
	}()

	authURL, err := c.waitForURL(ctx)
	if err != nil {
		cancel()
		return "", err
	}

	callbackURL, err := callbackFrom(authURL)
	if err != nil {
		cancel()
		return "", err
	}

	c.mu.Lock()
	c.pending = &session{authURL: authURL, callbackURL: callbackURL, started: time.Now(), cancel: cancel}
	c.mu.Unlock()

	return authURL, nil
}

func (c *Coordinator) waitForURL(ctx context.Context) (string, error) {
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

// Complete forwards the address the browser landed on to the waiting callback
// listener and reports whether Antigravity ended up signed in.
func (c *Coordinator) Complete(ctx context.Context, pasted string) error {
	c.mu.Lock()
	pending := c.pending
	c.mu.Unlock()

	if pending == nil {
		return ErrNoSession
	}

	query, err := queryFrom(pasted)
	if err != nil {
		return err
	}

	if _, err := fetch(ctx, pending.callbackURL+"?"+query); err != nil {
		c.finish()
		return fmt.Errorf("could not hand the code to Antigravity: %w", err)
	}

	// The Login RPC needs a moment to exchange the code for a token.
	for i := 0; i < 20; i++ {
		if c.instance.SignedIn(ctx) {
			c.finish()
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

func (c *Coordinator) finish() {
	c.mu.Lock()
	if c.pending != nil {
		c.pending.cancel()
		c.pending = nil
	}
	c.mu.Unlock()
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
