package ui

import (
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
)

// DebugLogPath collects the traces the mobile-debug patch records on the phone.
// A phone has no console and the server runs detached, so a file is the only
// reliable way to read what the viewport did during a keyboard animation.
const DebugLogPath = "/__agy/api/debug/log"

const (
	debugMaxBody  = 256 << 10
	debugMaxFile  = 8 << 20
	debugMaxLines = 2000
)

// Debug appends client traces to a log file. It is only constructed when
// AGY_DEBUG is set, so the route does not exist in a normal run.
type Debug struct {
	path      string
	trusted   []*net.IPNet
	publicURL string

	mu      sync.Mutex
	file    *os.File
	written int64
	closed  bool
}

// NewDebug opens path for appending.
func NewDebug(path string) (*Debug, error) {
	d := &Debug{path: path}
	if err := d.open(false); err != nil {
		return nil, err
	}
	return d, nil
}

// WithSameOrigin configures reverse-proxy origin verification to match Authenticator.
func (d *Debug) WithSameOrigin(trusted []*net.IPNet, publicURL string) *Debug {
	d.trusted = trusted
	d.publicURL = publicURL
	return d
}

// Register mounts the collector on mux.
func (d *Debug) Register(mux *http.ServeMux) {
	mux.HandleFunc(DebugLogPath, d.collect)
}

// Path is where traces land, so the startup banner can point at it.
func (d *Debug) Path() string { return d.path }

// Close releases the underlying debug log file handle.
func (d *Debug) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	if d.file == nil {
		return nil
	}
	err := d.file.Close()
	d.file = nil
	return err
}

func (d *Debug) open(truncate bool) error {
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if truncate {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	file, err := os.OpenFile(d.path, flags, 0o600)
	if err != nil {
		return err
	}
	if d.file != nil {
		_ = d.file.Close()
	}
	d.file = file

	d.written = 0
	if info, err := file.Stat(); err == nil {
		d.written = info.Size()
	}
	return nil
}

func (d *Debug) collect(w http.ResponseWriter, r *http.Request) {
	if !isPublicPost(w, r, d.trusted, d.publicURL) {
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, debugMaxBody))
	if err != nil {
		http.Error(w, "too much", http.StatusRequestEntityTooLarge)
		return
	}

	text := sanitizeTrace(string(body))
	if text == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed || d.file == nil {
		http.Error(w, "closed", http.StatusServiceUnavailable)
		return
	}

	// The traces are verbose by design; recycling the file keeps a long session
	// from filling the disk.
	if d.written > debugMaxFile {
		if err := d.open(true); err != nil {
			http.Error(w, "cannot write", http.StatusInternalServerError)
			return
		}
	}

	n, err := d.file.WriteString(text)
	d.written += int64(n)
	if err != nil {
		http.Error(w, "cannot write", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// sanitizeTrace keeps the body from injecting control characters or unbounded
// content into the log. The client is authenticated, but it is still input.
func sanitizeTrace(body string) string {
	lines := strings.Split(body, "\n")
	if len(lines) > debugMaxLines {
		lines = lines[:debugMaxLines]
	}

	var b strings.Builder
	for _, line := range lines {
		line = strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, line)
		if line = strings.TrimRight(line, " "); line == "" {
			continue
		}
		if runes := []rune(line); len(runes) > 1000 {
			line = string(runes[:1000])
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
