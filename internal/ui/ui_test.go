package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/auth"
	"github.com/AFSlayer/antigravity-server/internal/lsproc"
	"github.com/AFSlayer/antigravity-server/internal/patches"
	"github.com/AFSlayer/antigravity-server/internal/signin"
)

func TestPatchRowsTreatsNotNeededAsOK(t *testing.T) {
	report := patches.Report{
		{ID: "applied", Desc: "applied patch", Status: patches.StatusApplied},
		{ID: "not-needed", Desc: "not needed patch", Status: patches.StatusNotNeeded},
		{ID: "disabled", Desc: "disabled patch", Status: patches.StatusDisabled},
		{ID: "missing", Desc: "missing patch", Status: patches.StatusMissing},
		{ID: "built-on", Desc: "built on another", Status: patches.StatusNotNeeded, Without: "missing"},
	}

	rows := patchRows(report)
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
	// A patch left out because the one it builds on did not apply is not shown as done.
	if rows[4].Mark != "–" || rows[4].Class != "off" || rows[4].Desc != "built on another (not needed without missing)" {
		t.Errorf("row of a patch that went without another = %+v", rows[4])
	}
	if rows[0].Mark != "✓" || rows[0].Class != "ok" {
		t.Errorf("applied row = %+v, want Mark=✓ Class=ok", rows[0])
	}
	if rows[1].Mark != "✓" || rows[1].Class != "ok" {
		t.Errorf("not-needed row = %+v, want Mark=✓ Class=ok", rows[1])
	}
	if rows[2].Mark != "–" || rows[2].Class != "off" {
		t.Errorf("disabled row = %+v, want Mark=– Class=off", rows[2])
	}
	if rows[3].Mark != "✕" || rows[3].Class != "bad" {
		t.Errorf("missing row = %+v, want Mark=✕ Class=bad", rows[3])
	}
}

func TestIsPostRejectsCrossSiteRequests(t *testing.T) {
	local := NewLocal(LocalOptions{
		KnownPassword: func() string { return "secret" },
	})
	handler := local.Handler()

	tests := []struct {
		name         string
		method       string
		secFetchSite string
		wantStatus   int
	}{
		{name: "get rejected", method: http.MethodGet, wantStatus: http.StatusMethodNotAllowed},
		{name: "same-origin allowed", method: http.MethodPost, secFetchSite: "same-origin", wantStatus: http.StatusOK},
		{name: "none allowed", method: http.MethodPost, secFetchSite: "none", wantStatus: http.StatusOK},
		{name: "empty header allowed", method: http.MethodPost, secFetchSite: "", wantStatus: http.StatusOK},
		{name: "cross-site rejected", method: http.MethodPost, secFetchSite: "cross-site", wantStatus: http.StatusForbidden},
		{name: "same-site rejected", method: http.MethodPost, secFetchSite: "same-site", wantStatus: http.StatusForbidden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "http://127.0.0.1/api/password/reveal", nil)
			if tc.secFetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tc.secFetchSite)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}

	// DNS rebinding guard must reject non-loopback Host even when LocalPort is omitted (0)
	rebindDefault := httptest.NewRequest(http.MethodPost, "http://evil-rebind.example.com/api/password/reveal", nil)
	rebindDefaultRec := httptest.NewRecorder()
	handler.ServeHTTP(rebindDefaultRec, rebindDefault)
	if rebindDefaultRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-loopback Host when LocalPort=0, got %d", rebindDefaultRec.Code)
	}

	// DNS rebinding guard when LocalPort is configured
	boundLocal := NewLocal(LocalOptions{
		LocalPort:     54321,
		KnownPassword: func() string { return "secret" },
	}).Handler()

	rebindReq := httptest.NewRequest(http.MethodGet, "/", nil)
	rebindReq.Host = "evil-rebind.example.com:54321"
	rebindRec := httptest.NewRecorder()
	boundLocal.ServeHTTP(rebindRec, rebindReq)
	if rebindRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for DNS rebinding Host header, got %d", rebindRec.Code)
	}

	validReq := httptest.NewRequest(http.MethodPost, "/api/password/reveal", nil)
	validReq.Host = "127.0.0.1:54321"
	validReq.Header.Set("Origin", "http://127.0.0.1:54321")
	validRec := httptest.NewRecorder()
	boundLocal.ServeHTTP(validRec, validReq)
	if validRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid loopback Host+Origin, got %d", validRec.Code)
	}

	crossPortDefault := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/password/reveal", nil)
	crossPortDefault.Header.Set("Origin", "http://127.0.0.1:3000")
	crossPortRec := httptest.NewRecorder()
	handler.ServeHTTP(crossPortRec, crossPortDefault)
	if crossPortRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-port Origin when Host omits port, got %d", crossPortRec.Code)
	}

	httpsSchemeDefault := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/password/reveal", nil)
	httpsSchemeDefault.Header.Set("Origin", "https://127.0.0.1")
	httpsSchemeRec := httptest.NewRecorder()
	handler.ServeHTTP(httpsSchemeRec, httpsSchemeDefault)
	if httpsSchemeRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for https Origin against plaintext control panel, got %d", httpsSchemeRec.Code)
	}
}

func TestSignInStatusIncludesNeedsSignIn(t *testing.T) {
	var failRPC bool
	var signedIn bool

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/exa.language_server_pb.LanguageServerService/GetAuthStatus" {
			if failRPC {
				http.Error(w, "busy", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"authResult": map[string]any{
					"hasValidAuth": signedIn,
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	port := server.Listener.Addr().(*net.TCPAddr).Port
	inst := &lsproc.Instance{Port: port, CSRFToken: "token"}

	// Case 1: RPC error -> signedIn=false, needsSignIn=false
	failRPC = true
	s1 := NewSignIn(signin.New(inst, "/tmp/url"))
	mux1 := http.NewServeMux()
	s1.Register(mux1)

	rec1 := httptest.NewRecorder()
	mux1.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, SignInStatusPath, nil))
	if rec1.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec1.Code)
	}
	var body1 map[string]bool
	if err := json.NewDecoder(rec1.Body).Decode(&body1); err != nil {
		t.Fatal(err)
	}
	if body1["signedIn"] || body1["needsSignIn"] {
		t.Fatalf("on RPC failure expected signedIn=false and needsSignIn=false, got %+v", body1)
	}

	// Case 2: explicitly signed out -> signedIn=false, needsSignIn=true
	failRPC = false
	signedIn = false
	s2 := NewSignIn(signin.New(inst, "/tmp/url"))
	mux2 := http.NewServeMux()
	s2.Register(mux2)

	rec2 := httptest.NewRecorder()
	mux2.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, SignInStatusPath, nil))
	var body2 map[string]bool
	if err := json.NewDecoder(rec2.Body).Decode(&body2); err != nil {
		t.Fatal(err)
	}
	if body2["signedIn"] || !body2["needsSignIn"] {
		t.Fatalf("when signed out expected signedIn=false and needsSignIn=true, got %+v", body2)
	}
}

func TestSignInPageBypassesStaleCacheAfterLogout(t *testing.T) {
	signedIn := true
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/exa.language_server_pb.LanguageServerService/GetAuthStatus" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"authResult": map[string]any{
					"hasValidAuth": signedIn,
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	port := server.Listener.Addr().(*net.TCPAddr).Port
	inst := &lsproc.Instance{Port: port, CSRFToken: "token"}
	s := NewSignIn(signin.New(inst, "/tmp/url"))
	mux := http.NewServeMux()
	s.Register(mux)

	// 1. Warm the 30s positive cache via /__agy/api/signin/status.
	recStatus := httptest.NewRecorder()
	mux.ServeHTTP(recStatus, httptest.NewRequest(http.MethodGet, SignInStatusPath, nil))
	if recStatus.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recStatus.Code)
	}

	// 2. User logs out in the web app and navigates to /__agy/signin within the 30s window.
	signedIn = false
	recPage := httptest.NewRecorder()
	mux.ServeHTTP(recPage, httptest.NewRequest(http.MethodGet, SignInPath, nil))
	if recPage.Code != http.StatusOK {
		t.Fatalf("page status = %d, want 200", recPage.Code)
	}
	if strings.Contains(recPage.Body.String(), "Already signed in") {
		t.Fatalf("GET %s served stale 'Already signed in' page after logout", SignInPath)
	}

	// 3. Subsequent status poll should also reflect the refreshed signed-out state.
	recStatusAfter := httptest.NewRecorder()
	mux.ServeHTTP(recStatusAfter, httptest.NewRequest(http.MethodGet, SignInStatusPath, nil))
	var bodyAfter map[string]bool
	if err := json.NewDecoder(recStatusAfter.Body).Decode(&bodyAfter); err != nil {
		t.Fatal(err)
	}
	if bodyAfter["signedIn"] || !bodyAfter["needsSignIn"] {
		t.Fatalf("expected status after page visit to be signedIn=false, needsSignIn=true, got %+v", bodyAfter)
	}
}

// The page words steps 2 and 3 by mode, so begin has to say which one the
// language server picked. Once the attempt ends, complete answers 409, which is
// what makes the page start a new one.
func TestSignInBeginReportsModeAndCompleteReportsExpiry(t *testing.T) {
	urlFile := filepath.Join(t.TempDir(), "urls")
	stop := make(chan struct{})
	endLogin := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/exa.language_server_pb.LanguageServerService/") {
		case "Login":
			_ = os.WriteFile(urlFile, []byte("https://accounts.google.com/o/oauth2/auth?redirect_uri=http%3A%2F%2Flocalhost%3A1%2Fauth%2Fcallback\n"), 0o600)
			select {
			case <-endLogin:
			case <-r.Context().Done():
			case <-stop:
			}
			_, _ = w.Write([]byte(`{"authResult":{"uiMessage":"context deadline exceeded"}}`))
		case "SwitchLoginToAuthCode":
			_, _ = w.Write([]byte(`{"active":true,"authUrl":"https://accounts.google.com/o/oauth2/auth?state=code","mode":"LOGIN_SESSION_MODE_AUTH_CODE"}`))
		case "SubmitLoginAuthCode":
			_, _ = w.Write([]byte(`{"retryError":"Sign-in session is no longer active. Please start sign-in again."}`))
		case "GetAuthStatus":
			_, _ = w.Write([]byte(`{"authResult":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(stop) })

	port := server.Listener.Addr().(*net.TCPAddr).Port
	s := NewSignIn(signin.New(&lsproc.Instance{Port: port, CSRFToken: "t"}, urlFile))
	mux := http.NewServeMux()
	s.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, SignInBeginPath, strings.NewReader("{}")))
	if rec.Code != http.StatusOK {
		t.Fatalf("begin status = %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		AuthURL string `json:"authUrl"`
		Mode    string `json:"mode"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Mode != "authCode" || body.AuthURL != "https://accounts.google.com/o/oauth2/auth?state=code" {
		t.Fatalf("begin = %+v, want the auth-code URL and mode", body)
	}

	// The language server gives up on the attempt.
	close(endLogin)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, SignInCompletePath, strings.NewReader(`{"pasted":"4/0Acode"}`)))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "expired") {
		t.Fatalf("complete after the attempt ended = %d %s, want 409 saying it expired", rec.Code, rec.Body)
	}
}

func TestCompleteErrorMessages(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		msg    string
	}{
		{signin.ErrNoSession, http.StatusConflict, "That sign-in expired. Open the Google page again."},
		{signin.ErrExpired, http.StatusConflict, "That sign-in expired. Open the Google page again."},
		{
			fmt.Errorf("%w: Invalid authorization code. Please retry.", signin.ErrFailed),
			http.StatusConflict,
			"Google sign-in did not complete: Invalid authorization code. Please retry.",
		},
		{errors.New("Invalid authorization code."), http.StatusBadRequest, "Invalid authorization code."},
	} {
		status, msg := completeError(tc.err)
		if status != tc.status || msg != tc.msg {
			t.Errorf("completeError(%v) = %d %q, want %d %q", tc.err, status, msg, tc.status, tc.msg)
		}
	}
}

func TestSignInBehindReverseProxyAndDebugClose(t *testing.T) {
	_, loopbackNet, err := net.ParseCIDR("127.0.0.1/32")
	if err != nil {
		t.Fatal(err)
	}
	trusted := []*net.IPNet{loopbackNet}

	s := NewSignIn(signin.New(&lsproc.Instance{Port: 1, CSRFToken: "t"}, "")).WithSameOrigin(trusted, "https://agy.example.com")
	mux := http.NewServeMux()
	s.Register(mux)

	// Reverse proxy sends internal loopback Host + X-Forwarded-Host + external Origin -> must pass origin check (not 403)
	proxyReq := httptest.NewRequest(http.MethodPost, SignInCompletePath, strings.NewReader(`{"pasted":"4/0Acode"}`))
	proxyReq.Host = "127.0.0.1:8765"
	proxyReq.RemoteAddr = "127.0.0.1:4444"
	proxyReq.Header.Set("X-Forwarded-Host", "agy.example.com")
	proxyReq.Header.Set("Origin", "https://agy.example.com")
	proxyRec := httptest.NewRecorder()
	mux.ServeHTTP(proxyRec, proxyReq)
	if proxyRec.Code == http.StatusForbidden {
		t.Fatalf("SignIn POST behind trusted reverse proxy was wrongly rejected with 403")
	}

	// Cross-origin POST must still be rejected with 403
	evilReq := httptest.NewRequest(http.MethodPost, SignInCompletePath, strings.NewReader(`{"pasted":"4/0Acode"}`))
	evilReq.Host = "127.0.0.1:8765"
	evilReq.RemoteAddr = "127.0.0.1:4444"
	evilReq.Header.Set("X-Forwarded-Host", "agy.example.com")
	evilReq.Header.Set("Origin", "https://evil.example")
	evilRec := httptest.NewRecorder()
	mux.ServeHTTP(evilRec, evilReq)
	if evilRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-origin SignIn POST, got %d", evilRec.Code)
	}

	// Debug.Close must prevent late collect calls from reopening or writing to the file
	logPath := filepath.Join(t.TempDir(), "mobile-debug.log")
	dbg, err := NewDebug(logPath)
	if err != nil {
		t.Fatal(err)
	}
	dbg.WithSameOrigin(trusted, "https://agy.example.com").Register(mux)
	if err := dbg.Close(); err != nil {
		t.Fatal(err)
	}
	dbgReq := httptest.NewRequest(http.MethodPost, DebugLogPath, strings.NewReader(`{"events":[{"t":1}]}`))
	dbgReq.Host = "agy.example.com"
	dbgReq.Header.Set("Origin", "https://agy.example.com")
	dbgRec := httptest.NewRecorder()
	mux.ServeHTTP(dbgRec, dbgReq)
	if dbgRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 after Debug.Close, got %d", dbgRec.Code)
	}
	if dbg.file != nil {
		t.Fatalf("Debug.file must remain nil after Close()")
	}
}

// A trace file that fills up is kept beside the new one, once: emptying it lost the minutes that were being looked for.
func TestDebugKeepsTheFullFileWhenItStartsANewOne(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "traces")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(folder, "mobile-debug.log")
	dbg, err := NewDebug(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer dbg.Close()
	dbg.maxFile = 20
	mux := http.NewServeMux()
	dbg.WithSameOrigin(nil, "https://agy.example.com").Register(mux)
	try := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, DebugLogPath, strings.NewReader(body))
		req.Host = "agy.example.com"
		req.Header.Set("Origin", "https://agy.example.com")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	post := func(body string) {
		t.Helper()
		if code := try(body); code != http.StatusNoContent {
			t.Fatalf("posting %q: status %d", body, code)
		}
	}
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	post("first episode, long enough to fill it")
	post("second episode, also over the limit")
	if got := read(logPath + ".1"); got != "first episode, long enough to fill it\n" {
		t.Errorf("the full file was not kept: %q", got)
	}
	if got := read(logPath); got != "second episode, also over the limit\n" {
		t.Errorf("the new file holds %q", got)
	}
	post("third")
	if got := read(logPath + ".1"); got != "second episode, also over the limit\n" {
		t.Errorf("only the last full file is kept, got %q", got)
	}
	if got := read(logPath); got != "third\n" {
		t.Errorf("the new file holds %q", got)
	}
	post("fourth")
	if got := read(logPath); got != "third\nfourth\n" {
		t.Errorf("a file under the limit is added to, got %q", got)
	}

	// Something in the way of the kept file: the full one is emptied instead, as it was before any was kept.
	if err := os.Remove(logPath + ".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(logPath+".1", "in-the-way"), 0o755); err != nil {
		t.Fatal(err)
	}
	post("fifth, which takes it over the limit again")
	post("sixth")
	if got := read(logPath); got != "sixth\n" {
		t.Errorf("a full file that cannot be kept is emptied, got %q", got)
	}
	post("seventh")
	if got := read(logPath); got != "sixth\nseventh\n" {
		t.Errorf("and written to again, got %q", got)
	}

	// The folder goes away with the file full, so no file can be started. A trace is refused while that lasts, and
	// taken again once a file opens, without a restart.
	post("eighth, which takes it over the limit once more")
	// Windows does not let a folder go while a file in it is open, so there the file is closed first, which is how a
	// start that failed leaves it anyway.
	if runtime.GOOS == "windows" {
		dbg.mu.Lock()
		_ = dbg.file.Close()
		dbg.file = nil
		dbg.mu.Unlock()
	}
	if err := os.RemoveAll(folder); err != nil {
		t.Fatal(err)
	}
	if code := try("ninth"); code != http.StatusInternalServerError {
		t.Errorf("a trace with nowhere to go: status %d, want %d", code, http.StatusInternalServerError)
	}
	if code := try("tenth"); code != http.StatusInternalServerError {
		t.Errorf("and the next one: status %d, want %d", code, http.StatusInternalServerError)
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	post("eleventh")
	if got := read(logPath); got != "eleventh\n" {
		t.Errorf("a file that opens again is written to, got %q", got)
	}
}

// The control panel opens by itself in local mode, so when the language server was found without its token it says
// why the web UI will stay blank, above the QR code it keeps.
func TestLocalPageExplainsMissingToken(t *testing.T) {
	sessions, err := auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	page := func(missing bool) string {
		h := NewLocal(LocalOptions{
			TokenMissing: missing,
			Auth:         auth.New(auth.Options{Sessions: sessions}),
			Tracker:      patches.NewTracker(),
			Endpoints:    func() []Endpoint { return nil },
			LoginBaseURL: func() string { return "http://127.0.0.1:8765" },
		}).Handler()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("control panel answered %d", rec.Code)
		}
		return rec.Body.String()
	}

	body := page(true)
	if !strings.Contains(body, "No access token was found") || !strings.Contains(body, DesktopUnsupportedURL) {
		t.Fatalf("missing token not explained on the control panel")
	}
	if !strings.Contains(body, `id="qr"`) || strings.Index(body, "No access token was found") > strings.Index(body, `id="qr"`) {
		t.Fatalf("the notice should come before the QR code, which stays")
	}
	if body = page(false); strings.Contains(body, "No access token was found") {
		t.Fatalf("notice shown although the token was found")
	}
}

// The login page suggests the QR code only where one is shown: the control panel of local mode.
func TestLoginPageQRTip(t *testing.T) {
	render := func(qrTip bool) string {
		rec := httptest.NewRecorder()
		LoginPage(qrTip)(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		return rec.Body.String()
	}
	if !strings.Contains(render(true), "scan the QR code") {
		t.Fatal("QR tip missing where a QR code is shown")
	}
	if strings.Contains(render(false), "scan the QR code") {
		t.Fatal("QR tip shown where no QR code is")
	}
}
