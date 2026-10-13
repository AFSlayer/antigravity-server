// Package ui renders the two pages this tool owns: the login prompt served to
// the network, and the control panel served only on loopback.
package ui

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/AFSlayer/antigravity-server/internal/assets"
	"github.com/AFSlayer/antigravity-server/internal/auth"
	"github.com/AFSlayer/antigravity-server/internal/patches"
)

//go:embed templates
var templateFS embed.FS

var tmpl = template.Must(template.ParseFS(templateFS, "templates/*.html"))

// Endpoint is one address the control panel offers to copy.
type Endpoint struct {
	Label string
	URL   string
}

// PatchRow is one patch's status as displayed in the control panel.
type PatchRow struct {
	ID    string
	Desc  string
	Mark  string
	Class string
}

func patchRows(report patches.Report) []PatchRow {
	rows := make([]PatchRow, 0, len(report))
	for _, res := range report {
		row := PatchRow{ID: res.ID, Desc: res.Desc}
		switch res.Status {
		case patches.StatusApplied, patches.StatusNotNeeded:
			row.Mark, row.Class = "✓", "ok"
		case patches.StatusDisabled:
			row.Mark, row.Class = "–", "off"
		default:
			row.Mark, row.Class = "✕", "bad"
		}
		rows = append(rows, row)
	}
	return rows
}

func patchWarning(report patches.Report) string {
	missing := report.Missing()
	if len(missing) == 0 {
		return ""
	}

	required := len(report.MissingRequired())
	if required > 0 {
		return fmt.Sprintf(
			"%d patch(es) could not be applied, %d of them essential. This usually means Antigravity changed its bundle. Please open an issue so it can be updated.",
			len(missing), required)
	}
	return fmt.Sprintf(
		"%d optional patch(es) could not be applied. Antigravity may have changed its bundle; some mobile fixes are inactive.",
		len(missing))
}

// LoginPage renders the password prompt.
func LoginPage() auth.LoginPageFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = tmpl.ExecuteTemplate(w, "login.html", map[string]any{
			"LoginAPIPath": auth.LoginAPIPath,
		})
	}
}

// LocalOptions wires the control panel to the running server. The function
// fields are read at request time so the page always reflects current state.
type LocalOptions struct {
	Version       string
	Mode          string
	NetworkNote   string
	Port          int
	Auth          *auth.Authenticator
	Tracker       *patches.Tracker
	Endpoints     func() []Endpoint
	LoginBaseURL  func() string
	KnownPassword func() string
	ResetPassword func() (string, error)
	Shutdown      func()
}

// Local is the loopback-only control panel: QR code, addresses, password,
// signed-in devices, patch status and shutdown.
type Local struct {
	opts LocalOptions
}

// NewLocal builds the control panel.
func NewLocal(opts LocalOptions) *Local {
	return &Local{opts: opts}
}

// Handler returns the control panel routes. Mount it on a listener bound to
// 127.0.0.1 only; these endpoints are unauthenticated by design.
func (l *Local) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, path := range assets.Paths() {
		mux.Handle(path, assets.Handler())
	}
	mux.HandleFunc("/", l.page)
	mux.HandleFunc("/api/qr", l.newQR)
	mux.HandleFunc("/api/password/reveal", l.revealPassword)
	mux.HandleFunc("/api/password/reset", l.resetPassword)
	mux.HandleFunc("/api/sessions/revoke", l.revokeSessions)
	mux.HandleFunc("/api/shutdown", l.shutdown)
	return mux
}

func (l *Local) enrollURL() (string, error) {
	tok, err := l.opts.Auth.NewEnrollmentToken()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s?t=%s", l.opts.LoginBaseURL(), auth.LoginPath, tok), nil
}

func qrPNG(url string) (string, error) {
	png, err := qrcode.Encode(url, qrcode.Medium, 512)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(png), nil
}

func (l *Local) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	url, err := l.enrollURL()
	if err != nil {
		http.Error(w, "could not create login code", http.StatusInternalServerError)
		return
	}
	qr, err := qrPNG(url)
	if err != nil {
		http.Error(w, "could not render QR code", http.StatusInternalServerError)
		return
	}

	report := l.opts.Tracker.Results()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = tmpl.ExecuteTemplate(w, "local.html", map[string]any{
		"Version":       l.opts.Version,
		"Mode":          l.opts.Mode,
		"NetworkNote":   l.opts.NetworkNote,
		"Port":          l.opts.Port,
		"QR":            qr,
		"EnrollMinutes": int(auth.EnrollmentTTL.Minutes()),
		"Endpoints":     l.opts.Endpoints(),
		"Sessions":      l.opts.Auth.Sessions().Count(),
		"Patches":       patchRows(report),
		"PatchWarning":  patchWarning(report),
	})
}

func (l *Local) newQR(w http.ResponseWriter, r *http.Request) {
	if !isPost(w, r) {
		return
	}

	url, err := l.enrollURL()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	qr, err := qrPNG(url)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": url, "qr": qr})
}

func (l *Local) revealPassword(w http.ResponseWriter, r *http.Request) {
	if !isPost(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"password": l.opts.KnownPassword()})
}

func (l *Local) resetPassword(w http.ResponseWriter, r *http.Request) {
	if !isPost(w, r) {
		return
	}

	password, err := l.opts.ResetPassword()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"password": password})
}

func (l *Local) revokeSessions(w http.ResponseWriter, r *http.Request) {
	if !isPost(w, r) {
		return
	}

	n, err := l.opts.Auth.Sessions().RevokeAll()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": n})
}

func (l *Local) shutdown(w http.ResponseWriter, r *http.Request) {
	if !isPost(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
	go l.opts.Shutdown()
}

// contextWithTimeout bounds a handler's work without outliving the request.
func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

func isPost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	if sec := r.Header.Get("Sec-Fetch-Site"); sec != "" && sec != "same-origin" && sec != "none" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
