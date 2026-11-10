package ui

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	}

	rows := patchRows(report)
	if len(rows) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(rows))
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
			req := httptest.NewRequest(tc.method, "/api/password/reveal", nil)
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
	if rec.Code != http.StatusConflict {
		t.Fatalf("complete after the attempt ended = %d %s, want 409", rec.Code, rec.Body)
	}
}
