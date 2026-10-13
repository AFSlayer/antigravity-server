package ui

import (
	"encoding/json"
	"errors"
	"net/http"
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
)

// SignIn serves the pages and endpoints that walk a remote browser through
// Antigravity's Google sign-in.
type SignIn struct {
	coordinator *signin.Coordinator
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
}

// status backs the injected banner, which has no other way to learn whether
// Antigravity is signed in.
func (s *SignIn) status(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 5*time.Second)
	defer cancel()

	writeJSON(w, http.StatusOK, map[string]any{
		"signedIn":    s.coordinator.SignedIn(ctx),
		"needsSignIn": s.coordinator.NeedsSignIn(ctx),
		"available":   s.coordinator.Available(),
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

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
