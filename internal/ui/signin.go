package ui

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/signin"
)

// Sign-in routes. They sit under /__agy/ so the authenticator gates them like any
// other application path, and so they cannot collide with an Antigravity route.
const (
	SignInPath         = "/__agy/signin"
	SignInStatusPath   = "/__agy/api/signin/status"
	SignInBeginPath    = "/__agy/api/signin/begin"
	SignInCompletePath = "/__agy/api/signin/complete"
	PingPath           = "/__agy/ping"
)

type authStatusCache struct {
	signedIn  bool
	available bool
	checkedAt time.Time
}

// SignIn serves the pages and endpoints that walk a remote browser through
// Antigravity's Google sign-in.
type SignIn struct {
	coordinator *signin.Coordinator
	mu          sync.Mutex
	cache       *authStatusCache
}

// NewSignIn returns a SignIn backed by coordinator.
func NewSignIn(coordinator *signin.Coordinator) *SignIn {
	return &SignIn{coordinator: coordinator}
}

// Register mounts the sign-in routes on mux.
func (s *SignIn) Register(mux *http.ServeMux) {
	mux.HandleFunc(SignInPath, s.page)
	mux.HandleFunc(SignInStatusPath, s.status)
	mux.HandleFunc(SignInBeginPath, s.begin)
	mux.HandleFunc(SignInCompletePath, s.complete)
	mux.HandleFunc(PingPath, s.ping)
}

// ping provides an ultra-lightweight 0ms healthcheck endpoint that never touches
// the language server or external Google Cloud APIs.
func (s *SignIn) ping(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"alive": true,
	})
}

// status backs the auth banner, caching results for 5 minutes so repeated checks
// never block on Google Cloud CodeAssist round-trips.
func (s *SignIn) status(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.cache != nil && time.Since(s.cache.checkedAt) < 5*time.Minute {
		cached := *s.cache
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"signedIn":  cached.signedIn,
			"available": cached.available,
		})
		return
	}
	s.mu.Unlock()

	ctx, cancel := contextWithTimeout(r, 5*time.Second)
	defer cancel()

	signedIn := s.coordinator.SignedIn(ctx)
	available := s.coordinator.Available()

	s.mu.Lock()
	s.cache = &authStatusCache{
		signedIn:  signedIn,
		available: available,
		checkedAt: time.Now(),
	}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"signedIn":  signedIn,
		"available": available,
	})
}

func (s *SignIn) page(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := contextWithTimeout(r, 5*time.Second)
	defer cancel()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = tmpl.ExecuteTemplate(w, "signin.html", map[string]any{
		"Available": s.coordinator.Available(),
		"SignedIn":  s.coordinator.SignedIn(ctx),
	})
}

func (s *SignIn) begin(w http.ResponseWriter, r *http.Request) {
	if !isPost(w, r) {
		return
	}

	ctx, cancel := contextWithTimeout(r, 40*time.Second)
	defer cancel()

	if s.coordinator.SignedIn(ctx) {
		writeJSON(w, http.StatusOK, map[string]any{"signedIn": true})
		return
	}

	authURL, err := s.coordinator.Begin(ctx)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, signin.ErrUnavailable) {
			status = http.StatusNotImplemented
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"authUrl": authURL})
}

func (s *SignIn) complete(w http.ResponseWriter, r *http.Request) {
	if !isPost(w, r) {
		return
	}

	var req struct {
		Pasted string `json:"pasted"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Could not read the pasted address."})
		return
	}

	ctx, cancel := contextWithTimeout(r, 60*time.Second)
	defer cancel()

	if err := s.coordinator.Complete(ctx, req.Pasted); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, signin.ErrNoSession) {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}

	s.mu.Lock()
	s.cache = nil
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
