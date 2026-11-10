package signin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
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
	// switchBody, when set, is what SwitchLoginToAuthCode answers instead,
	// with switchStatus if that is set too.
	switchBody   string
	switchStatus int
	// switchHang makes SwitchLoginToAuthCode wait for the caller to give up.
	switchHang bool
	// sessionBody, when set, is what GetLoginSession answers. Otherwise it is
	// not there.
	sessionBody string
	// loginNoURL makes Login return at once without producing a URL.
	loginNoURL bool
	// loginEndsOnSwitch makes Login return, unsigned, as the switch arrives.
	loginEndsOnSwitch bool

	callback *httptest.Server
	server   *httptest.Server
	stop     chan struct{}

	mu            sync.Mutex
	logins        int
	signedIn      bool
	endLogin      chan struct{}
	callbackQuery string
	submitted     []string
	// failReply is what Login answers when it ends without signing in. The
	// default is what 2.31 says when the attempt times out.
	failReply string
	// authLag delays GetAuthStatus seeing a sign-in that Login already
	// reported.
	authLag    time.Duration
	signedInAt time.Time
}

func newFakeLS(t *testing.T, authCode bool) *fakeLS {
	t.Helper()
	f := &fakeLS{
		urlFile:  filepath.Join(t.TempDir(), "urls"),
		authCode: authCode,
		stop:     make(chan struct{}),
	}
	f.callback = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("code") == "reject" {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.callbackQuery = r.URL.RawQuery
		f.mu.Unlock()
		f.end(true)
		// Like the real listener, send the browser on to a page elsewhere. It
		// is unreachable here, so following it would fail the sign-in.
		http.Redirect(w, r, "http://127.0.0.1:1/auth-success", http.StatusFound)
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
	if signedIn && !f.signedIn {
		f.signedInAt = time.Now()
	}
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
		f.mu.Lock()
		reply := `{"authResult":{"uiMessage":"context deadline exceeded","generalError":{}}}`
		if f.signedIn {
			reply = `{"authResult":{"hasValidAuth":true}}`
		} else if f.failReply != "" {
			reply = f.failReply
		}
		f.mu.Unlock()
		_, _ = w.Write([]byte(reply))

	case "SwitchLoginToAuthCode":
		if f.loginEndsOnSwitch {
			f.end(false)
			// Let Login's answer reach the coordinator first.
			time.Sleep(50 * time.Millisecond)
		}
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
			if f.switchStatus != 0 {
				w.WriteHeader(f.switchStatus)
			}
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

	case "GetLoginSession":
		if f.sessionBody == "" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(f.sessionBody))

	case "SubmitLoginAuthCode":
		var req struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Code == "4/drop" {
			// The connection goes away mid-call, as when the server restarts.
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				_ = conn.Close()
			}
			return
		}
		f.mu.Lock()
		f.submitted = append(f.submitted, req.Code)
		f.mu.Unlock()
		switch req.Code {
		case "4/good":
		case "4/verify":
			// Accepted, but the account needs a step only Google can take.
			f.mu.Lock()
			f.failReply = `{"authResult":{"verificationRequired":{"verificationUrl":"https://accounts.google.com/verify"}}}`
			f.mu.Unlock()
			f.end(false)
			_, _ = w.Write([]byte(`{"accepted":true}`))
			return
		case "4/exchange-fails":
			// Accepted, but Login then fails to get a token.
			f.mu.Lock()
			f.failReply = `{"authResult":{"uiMessage":"oauth2: \"invalid_grant\" \"Bad Request\""}}`
			f.mu.Unlock()
			f.end(false)
			_, _ = w.Write([]byte(`{"accepted":true}`))
			return
		case "4/connect-error":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"invalid_argument","message":"The code is malformed."}`))
			return
		case "4/expire":
			// The attempt ends while the code is being checked. Login's answer
			// is sent after this one.
			go func() {
				time.Sleep(50 * time.Millisecond)
				f.end(false)
			}()
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
		signedIn := f.signedIn && time.Since(f.signedInAt) >= f.authLag
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"authResult": map[string]any{"hasValidAuth": signedIn}})

	default:
		http.NotFound(w, r)
	}
}

func (f *fakeLS) submittedCodes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.submitted...)
}

func (f *fakeLS) callbackGot() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callbackQuery
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
	if got := f.submittedCodes(); len(got) != 2 || got[1] != "4/good" {
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
	if got := f.callbackGot(); got != "state=loop&code=abc" {
		t.Errorf("callback got %q", got)
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
		if len(f.submittedCodes()) != 0 || f.callbackGot() != "" {
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

// A language server that answers SwitchLoginToAuthCode without switching keeps
// the loopback flow, whose listener is still waiting. Any answer that may have
// switched, or found no attempt, leaves nothing usable, so Begin fails rather
// than guess.
func TestBeginSwitchAnswers(t *testing.T) {
	for _, tc := range []struct {
		status       int
		body         string
		wantLoopback bool
	}{
		{0, `{"active":true,"authUrl":"https://accounts.google.com/x","mode":"LOGIN_SESSION_MODE_LOOPBACK"}`, true},
		{0, `<html>not an RPC</html>`, true},
		{http.StatusNotFound, `404 page not found`, true},
		{http.StatusNotFound, `{"code":"not_found","message":"no login in progress"}`, false},
		{http.StatusNotImplemented, `{"code":"unimplemented"}`, true},
		{http.StatusBadRequest, `{"code":"invalid_argument"}`, false},
		{http.StatusConflict, `{"code":"aborted"}`, false},
		{0, `{"active":false}`, false},
		{0, `{"active":true,"authUrl":"https://example.com/x","mode":"LOGIN_SESSION_MODE_AUTH_CODE"}`, false},
		{http.StatusInternalServerError, `{"code":"internal"}`, false},
		{http.StatusServiceUnavailable, `{"code":"unavailable"}`, false},
	} {
		f := newFakeLS(t, true)
		f.switchStatus, f.switchBody = tc.status, tc.body
		c := f.coordinator()
		_, mode, err := c.Begin(context.Background())
		if tc.wantLoopback && (err != nil || mode != ModeLoopback) {
			t.Errorf("switch answered %d %s: Begin = %q, %v; want loopback", tc.status, tc.body, mode, err)
		}
		if !tc.wantLoopback && (err == nil || c.pending != nil) {
			t.Errorf("switch answered %d %s: Begin = %q, %v; want an error and no attempt", tc.status, tc.body, mode, err)
		}
	}
}

// Login can record its URL and fail straight after. That URL must not be
// handed out.
func TestBeginDropsAttemptThatEndedAfterItsURL(t *testing.T) {
	f := newFakeLS(t, false)
	f.loginEndsOnSwitch = true
	c := f.coordinator()
	if _, _, err := c.Begin(context.Background()); !errors.Is(err, errEndedEarly) || c.pending != nil {
		t.Fatalf("Begin = %v; want errEndedEarly and no attempt", err)
	}
}

// After the language server gives up, as it does 5 minutes in, the next page
// load must start a new attempt rather than hand out the old URL.
func TestBeginReplacesEndedAttempt(t *testing.T) {
	f := newFakeLS(t, true)
	c := f.coordinator()
	ctx := context.Background()
	if _, _, err := c.Begin(ctx); err != nil {
		t.Fatal(err)
	}
	f.end(false)
	waitEnded(t, c)

	if _, _, err := c.Begin(ctx); err != nil {
		t.Fatal(err)
	}
	if n := f.loginCount(); n != 2 {
		t.Fatalf("Login ran %d times, want a fresh attempt", n)
	}
	if !c.pending.live() {
		t.Fatal("the new attempt must be live")
	}
}

// A code can be accepted and the token exchange still fail, which ends Login.
// That is known within a moment, so Complete must not wait out its polling, and
// it is not an expiry, so the reason must reach the user.
func TestCompleteStopsWaitingWhenLoginEnds(t *testing.T) {
	f := newFakeLS(t, true)
	c := f.coordinator()
	ctx := context.Background()
	if _, _, err := c.Begin(ctx); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	err := c.Complete(ctx, "4/exchange-fails")
	if !errors.Is(err, ErrFailed) || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("Complete = %v, want ErrFailed with the language server's reason", err)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Errorf("Complete took %v after Login ended", elapsed)
	}
	if c.pending != nil {
		t.Error("the ended attempt must be cleared")
	}
}

// Login can report success before GetAuthStatus catches up. That is a
// sign-in, not an ended attempt.
func TestCompleteWaitsForSlowAuthStatusAfterSuccess(t *testing.T) {
	f := newFakeLS(t, true)
	f.authLag = 2 * time.Second
	c := f.coordinator()
	ctx := context.Background()
	if _, _, err := c.Begin(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Complete(ctx, "4/good"); err != nil {
		t.Fatalf("Complete = %v, want success once GetAuthStatus catches up", err)
	}
}

// A failure reported only as a field still has to reach the user as a
// failure; calling it an expiry would send them round the same loop.
func TestCompleteNamesFailureWithoutMessage(t *testing.T) {
	f := newFakeLS(t, true)
	c := f.coordinator()
	ctx := context.Background()
	if _, _, err := c.Begin(ctx); err != nil {
		t.Fatal(err)
	}
	err := c.Complete(ctx, "4/verify")
	if !errors.Is(err, ErrFailed) || !strings.Contains(err.Error(), "https://accounts.google.com/verify") {
		t.Fatalf("Complete = %v, want ErrFailed with the verification link", err)
	}
}

// When the callback listener refuses the address, the attempt is ended, so the
// error has to say the attempt is over.
func TestCompleteLoopbackRefusedEndsAttempt(t *testing.T) {
	f := newFakeLS(t, false)
	c := f.coordinator()
	ctx := context.Background()
	if _, _, err := c.Begin(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Complete(ctx, "http://localhost:1/auth/callback?state=old&code=reject"); !errors.Is(err, ErrFailed) {
		t.Fatalf("Complete = %v, want ErrFailed", err)
	}
	if c.pending != nil {
		t.Error("the refused attempt must be cleared")
	}
}

// A second Begin waiting behind a stuck first one must give up with its own
// deadline.
func TestBeginWaitHonoursContext(t *testing.T) {
	f := newFakeLS(t, true)
	f.switchHang = true
	c := f.coordinator()

	first, cancelFirst := context.WithTimeout(context.Background(), 2*time.Second)
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _, _ = c.Begin(first)
	}()
	t.Cleanup(func() {
		cancelFirst()
		<-firstDone
	})
	for len(c.beginning) == 0 {
		time.Sleep(time.Millisecond)
	}

	second, cancelSecond := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelSecond()
	start := time.Now()
	if _, _, err := c.Begin(second); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second Begin = %v, want its deadline", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("second Begin waited %v", elapsed)
	}
}

// A switch that fails may still have taken effect. GetLoginSession says which
// flow the attempt is in, so Begin need not give up.
func TestBeginAsksAfterAmbiguousSwitch(t *testing.T) {
	for _, tc := range []struct {
		session  string
		wantMode Mode
	}{
		{`{"active":true,"authUrl":"https://accounts.google.com/o/oauth2/auth?state=asked","mode":"LOGIN_SESSION_MODE_AUTH_CODE"}`, ModeAuthCode},
		{`{"active":true,"authUrl":"https://accounts.google.com/x","mode":"LOGIN_SESSION_MODE_LOOPBACK"}`, ModeLoopback},
		{`{"active":true,"authUrl":"https://example.com/x","mode":"LOGIN_SESSION_MODE_AUTH_CODE"}`, ""},
		{`{}`, ""},
	} {
		f := newFakeLS(t, true)
		f.switchStatus, f.switchBody = http.StatusInternalServerError, `{"code":"internal"}`
		f.sessionBody = tc.session
		c := f.coordinator()
		authURL, mode, err := c.Begin(context.Background())
		if tc.wantMode == "" {
			if err == nil || c.pending != nil {
				t.Errorf("session %s: Begin = %q, %v; want an error and no attempt", tc.session, mode, err)
			}
			continue
		}
		if err != nil || mode != tc.wantMode {
			t.Errorf("session %s: Begin = %q, %v; want %q", tc.session, mode, err, tc.wantMode)
		}
		if tc.wantMode == ModeAuthCode && !strings.Contains(authURL, "state=asked") {
			t.Errorf("Begin handed out %q, want the URL GetLoginSession gave", authURL)
		}
	}
}

// Errors from the language server can quote internal addresses and, on the
// loopback path, the pasted code. Neither belongs on the page or, for the
// code, in the log.
func TestCompleteKeepsTransportDetailOffThePage(t *testing.T) {
	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	f := newFakeLS(t, true)
	c := f.coordinator()
	ctx := context.Background()
	if _, _, err := c.Begin(ctx); err != nil {
		t.Fatal(err)
	}
	err := c.Complete(ctx, "4/drop")
	if err == nil || strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("Complete(dropped) = %v; want an error without the address", err)
	}

	f = newFakeLS(t, false)
	c = f.coordinator()
	if _, _, err := c.Begin(ctx); err != nil {
		t.Fatal(err)
	}
	f.callback.Close()
	err = c.Complete(ctx, "http://localhost:1/auth/callback?state=loop&code=4/SECRET")
	if !errors.Is(err, ErrFailed) || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("Complete(listener gone) = %v; want ErrFailed without the code or address", err)
	}
	if strings.Contains(logged.String(), "SECRET") {
		t.Errorf("the log quotes the pasted code: %s", logged.String())
	}
}

// If the switch fails and the language server cannot say what happened, the
// page gets a plain message rather than an internal address.
func TestBeginKeepsSwitchErrorOffThePage(t *testing.T) {
	f := newFakeLS(t, true)
	f.switchStatus, f.switchBody = http.StatusInternalServerError, `{"code":"internal","message":"at 127.0.0.1:9"}`
	_, _, err := f.coordinator().Begin(context.Background())
	if err == nil || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("Begin = %v; want a plain error", err)
	}
}

// If the caller gives up during the switch, the language server may or may not
// have switched, so neither URL can be trusted.
func TestBeginDropsAttemptWhenSwitchIsInterrupted(t *testing.T) {
	f := newFakeLS(t, true)
	f.switchHang = true
	c := f.coordinator()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
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
	if len(f.submittedCodes()) != 0 {
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

func TestLoginOutcome(t *testing.T) {
	for _, tc := range []struct {
		data   string
		err    error
		ok     bool
		reason string
	}{
		{`{"authResult":{"hasValidAuth":true,"grantedScopes":["openid"]}}`, nil, true, ""},
		// What 2.31 answers when an attempt times out.
		{`{"authResult":{"uiMessage":"context deadline exceeded","generalError":{}}}`, nil, false, "context deadline exceeded"},
		// Fields that are not failures say nothing.
		{`{"authResult":{"authMode":"AUTH_MODE_PERSONAL_CONSUMER","isGcpTos":true,"projectId":"p","generalError":{}}}`, nil, false, ""},
		{`{"authResult":{"licenseRequired":{}}}`, nil, false, "the account needs an Antigravity license"},
		{`{"authResult":{"tosViolation":null,"uiMessage":"context deadline exceeded"}}`, nil, false, "context deadline exceeded"},
		{`{"authResult":{"projectRequired":{}}}`, nil, false, "the account needs a Google Cloud project"},
		{`{"authResult":{"verificationRequired":{"verificationUrl":"https://v"}}}`, nil, false, "Google needs to verify the account: https://v"},
		{`{"authResult":{"tosViolation":{"message":"Suspended.","appealUrl":"https://a"},"uiMessage":"x"}}`, nil, false, "the account is blocked under the terms of service: Suspended.: https://a"},
		{`{"authResult":{"generalError":{"reason":"token exchange failed"}}}`, nil, false, "token exchange failed"},
		{`{"code":"internal","message":"token exchange failed"}`, errors.New("Login returned 500"), false, "token exchange failed"},
		{``, errors.New(`Post "https://127.0.0.1:1/Login": EOF`), false, "the language server stopped answering the sign-in"},
		{``, context.DeadlineExceeded, false, ""},
		{`{}`, nil, false, ""},
	} {
		ok, reason := loginOutcome([]byte(tc.data), tc.err)
		if ok != tc.ok || reason != tc.reason {
			t.Errorf("loginOutcome(%s, %v) = %v, %q; want %v, %q", tc.data, tc.err, ok, reason, tc.ok, tc.reason)
		}
	}
}

func TestIsMode(t *testing.T) {
	for raw, want := range map[string]bool{
		`"LOGIN_SESSION_MODE_AUTH_CODE"`:   true,
		`2`:                                true,
		` "LOGIN_SESSION_MODE_AUTH_CODE" `: true,
		`"LOGIN_SESSION_MODE_LOOPBACK"`:    false,
		`1`:                                false,
		``:                                 false,
	} {
		if got := isMode(json.RawMessage(raw), "LOGIN_SESSION_MODE_AUTH_CODE", 2); got != want {
			t.Errorf("isMode(%s, AUTH_CODE) = %v, want %v", raw, got, want)
		}
	}
}
