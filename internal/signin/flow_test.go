package signin

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/lsproc"
)

const rpcPrefix = "/exa.language_server_pb.LanguageServerService/"

// fakeLS plays the language server's side of sign-in: Login records a loopback
// consent URL the way the xdg-open shim would and blocks until the attempt
// ends, and the callback listener and the auth-code RPCs end it.
type fakeLS struct {
	urlFile string
	// authCode makes SwitchLoginToAuthCode exist, as it does from 2.31.
	authCode bool
	// switchBody, when set, is what SwitchLoginToAuthCode answers instead.
	switchBody string
	// switchHang makes SwitchLoginToAuthCode wait for the caller to give up.
	switchHang bool
	// loginNoURL makes Login return at once without producing a URL.
	loginNoURL bool

	callback *httptest.Server
	server   *httptest.Server
	stop     chan struct{}

	mu            sync.Mutex
	logins        int
	signedIn      bool
	endLogin      chan struct{}
	callbackQuery string
	submitted     []string
}

func newFakeLS(t *testing.T, authCode bool) *fakeLS {
	t.Helper()
	f := &fakeLS{
		urlFile:  filepath.Join(t.TempDir(), "urls"),
		authCode: authCode,
		stop:     make(chan struct{}),
	}
	f.callback = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.callbackQuery = r.URL.RawQuery
		f.mu.Unlock()
		f.end(true)
	}))
	f.server = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.callback.Close)
	t.Cleanup(f.server.Close)
	t.Cleanup(func() { close(f.stop) })
	return f
}

func (f *fakeLS) coordinator() *Coordinator {
	port := f.server.Listener.Addr().(*net.TCPAddr).Port
	return New(&lsproc.Instance{Port: port, CSRFToken: "t"}, f.urlFile)
}

// end finishes the current Login, signed in or not.
func (f *fakeLS) end(signedIn bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.signedIn = f.signedIn || signedIn
	if f.endLogin != nil {
		close(f.endLogin)
		f.endLogin = nil
	}
}

func (f *fakeLS) serve(w http.ResponseWriter, r *http.Request) {
	switch strings.TrimPrefix(r.URL.Path, rpcPrefix) {
	case "Login":
		if f.loginNoURL {
			_, _ = w.Write([]byte(`{"authResult":{"uiMessage":"failed"}}`))
			return
		}
		end := make(chan struct{})
		f.mu.Lock()
		f.logins++
		f.endLogin = end
		f.mu.Unlock()

		redirect := "http://localhost:" + f.callback.URL[strings.LastIndex(f.callback.URL, ":")+1:] + "/auth/callback"
		line := "https://accounts.google.com/o/oauth2/auth?redirect_uri=" + url.QueryEscape(redirect) + "&state=loop\n"
		file, err := os.OpenFile(f.urlFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = file.WriteString(line)
			_ = file.Close()
		}

		select {
		case <-end:
		case <-r.Context().Done():
		case <-f.stop:
		}
		_, _ = w.Write([]byte(`{}`))

	case "SwitchLoginToAuthCode":
		if !f.authCode {
			http.NotFound(w, r)
			return
		}
		var req struct {
			OpenBrowser *bool `json:"openBrowser"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if f.switchHang {
			// The body has been read, so the server notices the caller leaving.
			select {
			case <-r.Context().Done():
			case <-f.stop:
			}
			return
		}
		if f.switchBody != "" {
			_, _ = w.Write([]byte(f.switchBody))
			return
		}
		if req.OpenBrowser == nil || *req.OpenBrowser {
			http.Error(w, "openBrowser must be false", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active":  true,
			"authUrl": "https://accounts.google.com/o/oauth2/auth?redirect_uri=" + url.QueryEscape("https://antigravity.google/oauth-callback") + "&state=code",
			"mode":    "LOGIN_SESSION_MODE_AUTH_CODE",
		})

	case "SubmitLoginAuthCode":
		var req struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.submitted = append(f.submitted, req.Code)
		f.mu.Unlock()
		switch req.Code {
		case "4/good":
		case "4/connect-error":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"invalid_argument","message":"The code is malformed."}`))
			return
		case "4/expire":
			// The attempt ends while the code is being checked.
			f.end(false)
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"failed_precondition","message":"no login in progress"}`))
			return
		default:
			_, _ = w.Write([]byte(`{"retryError":"That code did not work. Copy it again."}`))
			return
		}
		f.end(true)
		_, _ = w.Write([]byte(`{"accepted":true}`))

	case "GetAuthStatus":
		f.mu.Lock()
		signedIn := f.signedIn
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"authResult": map[string]any{"hasValidAuth": signedIn}})

	default:
		http.NotFound(w, r)
	}
}

func (f *fakeLS) loginCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logins
}

func waitEnded(t *testing.T, c *Coordinator) {
	t.Helper()
	c.mu.Lock()
	sess := c.pending
	c.mu.Unlock()
	if sess == nil {
		t.Fatal("no attempt in flight")
	}
	select {
	case <-sess.done:
	case <-time.After(2 * time.Second):
		t.Fatal("the Login RPC did not return")
	}
}

func TestBeginPrefersAuthCode(t *testing.T) {
	f := newFakeLS(t, true)
	c := f.coordinator()
	ctx := context.Background()

	authURL, mode, err := c.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if mode != ModeAuthCode || !strings.Contains(authURL, "state=code") {
		t.Fatalf("Begin = %q, %q; want the auth-code URL", authURL, mode)
	}

	// A wrong code is reported in the language server's words and leaves the
	// attempt open for another try.
	err = c.Complete(ctx, "4/wrong")
	if err == nil || err.Error() != "That code did not work. Copy it again." {
		t.Fatalf("Complete(wrong) = %v", err)
	}
	if c.pending == nil {
		t.Fatal("a rejected code must not end the attempt")
	}

	if err := c.Complete(ctx, "  4/good\n"); err != nil {
		t.Fatalf("Complete(good) = %v", err)
	}
	if !c.SignedIn(ctx) {
		t.Error("expected signed in")
	}
	if c.pending != nil {
		t.Error("a finished attempt must be cleared")
	}
	if got := f.submitted; len(got) != 2 || got[1] != "4/good" {
		t.Errorf("submitted %q, want the trimmed code last", got)
	}
}

func TestBeginFallsBackToLoopbackWithoutAuthCode(t *testing.T) {
	f := newFakeLS(t, false)
	c := f.coordinator()
	ctx := context.Background()

	authURL, mode, err := c.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if mode != ModeLoopback || !strings.Contains(authURL, "state=loop") {
		t.Fatalf("Begin = %q, %q; want the loopback URL", authURL, mode)
	}

	if err := c.Complete(ctx, "http://localhost:1/auth/callback?state=loop&code=abc"); err != nil {
		t.Fatalf("Complete = %v", err)
	}
	if f.callbackQuery != "state=loop&code=abc" {
		t.Errorf("callback got %q", f.callbackQuery)
	}
	if !c.SignedIn(ctx) {
		t.Error("expected signed in")
	}
}

// The language server gives up on an attempt after 5 minutes. Pasting into it
// afterwards must say so and let the next Begin start over, rather than reuse a
// URL whose attempt is gone.
func TestCompleteAfterLoginEnded(t *testing.T) {
	for _, authCode := range []bool{true, false} {
		f := newFakeLS(t, authCode)
		c := f.coordinator()
		ctx := context.Background()

		if _, _, err := c.Begin(ctx); err != nil {
			t.Fatal(err)
		}
		f.end(false)
		waitEnded(t, c)

		if err := c.Complete(ctx, "http://localhost:1/auth/callback?code=abc"); !errors.Is(err, ErrExpired) {
			t.Fatalf("authCode=%v: Complete after expiry = %v, want ErrExpired", authCode, err)
		}
		if len(f.submitted) != 0 || f.callbackQuery != "" {
			t.Errorf("authCode=%v: an expired attempt must not be handed the code", authCode)
		}
		if err := c.Complete(ctx, "4/good"); !errors.Is(err, ErrNoSession) {
			t.Fatalf("authCode=%v: Complete with nothing in flight = %v, want ErrNoSession", authCode, err)
		}

		if _, _, err := c.Begin(ctx); err != nil {
			t.Fatal(err)
		}
		if n := f.loginCount(); n != 2 {
			t.Errorf("authCode=%v: Login ran %d times, want a fresh attempt", authCode, n)
		}
	}
}

func TestBeginReusesAttemptInFlight(t *testing.T) {
	f := newFakeLS(t, true)
	c := f.coordinator()
	ctx := context.Background()

	var wg sync.WaitGroup
	urls := make([]string, 3)
	for i := range urls {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			urls[i], _, _ = c.Begin(ctx)
		}(i)
	}
	wg.Wait()

	if n := f.loginCount(); n != 1 {
		t.Fatalf("Login ran %d times for concurrent Begins, want 1", n)
	}
	for _, u := range urls {
		if u != urls[0] || u == "" {
			t.Fatalf("Begins returned %q, want one shared URL", urls)
		}
	}
}

// A language server that answers SwitchLoginToAuthCode without actually
// switching keeps the loopback flow, whose listener is still waiting.
func TestBeginKeepsLoopbackWhenSwitchDoesNotSwitch(t *testing.T) {
	for _, body := range []string{
		`{"active":false}`,
		`{"active":true,"authUrl":"https://accounts.google.com/x","mode":"LOGIN_SESSION_MODE_LOOPBACK"}`,
		`{"active":true,"authUrl":"https://example.com/x","mode":"LOGIN_SESSION_MODE_AUTH_CODE"}`,
		`<html>not an RPC</html>`,
	} {
		f := newFakeLS(t, true)
		f.switchBody = body
		_, mode, err := f.coordinator().Begin(context.Background())
		if err != nil || mode != ModeLoopback {
			t.Errorf("switch answered %s: Begin = %q, %v; want loopback", body, mode, err)
		}
	}
}

// If the caller gives up during the switch, the language server may or may not
// have switched, so neither URL can be trusted.
func TestBeginDropsAttemptWhenSwitchIsInterrupted(t *testing.T) {
	f := newFakeLS(t, true)
	f.switchHang = true
	c := f.coordinator()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := c.Begin(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Begin = %v, want the deadline", err)
	}
	if c.pending != nil {
		t.Fatal("an interrupted switch must not leave an attempt behind")
	}
}

func TestBeginReportsLoginEndingWithoutURL(t *testing.T) {
	f := newFakeLS(t, true)
	f.loginNoURL = true

	start := time.Now()
	if _, _, err := f.coordinator().Begin(context.Background()); err == nil {
		t.Fatal("Begin succeeded with no URL")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Begin waited %v; it should stop once Login returns", elapsed)
	}
}

func TestSubmitAuthCodeErrors(t *testing.T) {
	f := newFakeLS(t, true)
	c := f.coordinator()
	ctx := context.Background()
	if _, _, err := c.Begin(ctx); err != nil {
		t.Fatal(err)
	}

	if err := c.Complete(ctx, " \n"); err == nil || c.pending == nil {
		t.Fatalf("Complete(blank) = %v; want an error that keeps the attempt", err)
	}
	if len(f.submitted) != 0 {
		t.Error("a blank paste must not reach the language server")
	}

	// A Connect error is shown in the language server's words.
	if err := c.Complete(ctx, "4/connect-error"); err == nil || err.Error() != "The code is malformed." {
		t.Fatalf("Complete(connect error) = %v", err)
	}
	if c.pending == nil {
		t.Fatal("a failed submit must not end a live attempt")
	}

	// A submit that fails because the attempt ended reads as expired, so the
	// page starts over.
	if err := c.Complete(ctx, "4/expire"); !errors.Is(err, ErrExpired) {
		t.Fatalf("Complete after the attempt ended = %v, want ErrExpired", err)
	}
	if c.pending != nil {
		t.Error("an expired attempt must be cleared")
	}
}

// finish runs after RPCs that can take seconds. By then a new Begin may have
// replaced the attempt, and ending that one would strand the user.
func TestFinishLeavesReplacementAlone(t *testing.T) {
	c := New(&lsproc.Instance{Port: 1}, filepath.Join(t.TempDir(), "urls"))
	if err := os.WriteFile(c.urlFile, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	oldCtx, oldCancel := context.WithCancel(context.Background())
	old := &session{cancel: oldCancel, done: make(chan struct{})}
	newCtx, newCancel := context.WithCancel(context.Background())
	defer newCancel()
	replacement := &session{cancel: newCancel, done: make(chan struct{})}
	c.pending = replacement

	c.finish(old)
	if oldCtx.Err() == nil {
		t.Error("finish must cancel the attempt it was given")
	}
	if c.pending != replacement || newCtx.Err() != nil {
		t.Error("finish must not end the attempt that replaced it")
	}
	if _, err := os.Stat(c.urlFile); err != nil {
		t.Error("finish must not remove the replacement's URL file")
	}

	c.finish(replacement)
	if c.pending != nil || newCtx.Err() == nil {
		t.Error("finish must end the current attempt")
	}
	if _, err := os.Stat(c.urlFile); !os.IsNotExist(err) {
		t.Error("finish must remove the URL file of the current attempt")
	}
}

func TestIsAuthCodeMode(t *testing.T) {
	for raw, want := range map[string]bool{
		`"LOGIN_SESSION_MODE_AUTH_CODE"`:   true,
		`2`:                                true,
		` "LOGIN_SESSION_MODE_AUTH_CODE" `: true,
		`"LOGIN_SESSION_MODE_LOOPBACK"`:    false,
		`1`:                                false,
		``:                                 false,
	} {
		if got := isAuthCodeMode(json.RawMessage(raw)); got != want {
			t.Errorf("isAuthCodeMode(%s) = %v, want %v", raw, got, want)
		}
	}
}
