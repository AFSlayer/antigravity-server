// Package lsproc finds, starts and talks to Antigravity's language_server.
//
// The desktop app runs it with --standalone, in which mode it serves the entire
// Antigravity web UI over HTTPS on a random loopback port. There is no discovery
// file to read, so the process is located by scanning the process table and its
// port by probing each socket it listens on.
package lsproc

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var ErrNotRunning = errors.New("no standalone Antigravity language server is running")

// Instance is a running standalone language server.
type Instance struct {
	PID       int
	Port      int
	CSRFToken string
}

// BaseURL is the local HTTPS origin the instance serves. Its certificate is
// self-signed, so callers must skip verification.
func (i *Instance) BaseURL() string {
	return fmt.Sprintf("https://127.0.0.1:%d", i.Port)
}

type process struct {
	pid     int
	cmdline string
}

var csrfRe = regexp.MustCompile(`--csrf_token[= ]+([^\s]+)`)

func csrfFromCmdline(cmdline string) string {
	if m := csrfRe.FindStringSubmatch(cmdline); len(m) > 1 {
		return strings.Trim(m[1], `"'`)
	}
	return ""
}

func isStandaloneLS(cmdline string) bool {
	if !strings.Contains(cmdline, "language_server") {
		return false
	}
	if !strings.Contains(cmdline, "--standalone") {
		return false
	}
	return !strings.Contains(cmdline, "grep ")
}

// Filter narrows which language server to accept. Server mode uses it to attach
// to the instance it started rather than any that happens to be running.
type Filter struct {
	BinaryPath string
	PID        int
}

func (f Filter) matches(p process) bool {
	if f.PID != 0 && p.pid != f.PID {
		return false
	}
	if f.BinaryPath != "" && !strings.Contains(p.cmdline, f.BinaryPath) {
		return false
	}
	return true
}

// Find returns any running standalone language server.
func Find() (*Instance, error) {
	return FindMatching(Filter{})
}

// FindMatching returns the first standalone language server satisfying filter
// that answers with the web UI. A process may listen on several ports, only one
// of which serves the bundle, so each is probed.
func FindMatching(filter Filter) (*Instance, error) {
	procs, err := standaloneProcesses()
	if err != nil {
		return nil, err
	}

	for _, p := range procs {
		if !filter.matches(p) {
			continue
		}

		ports, err := listeningPorts(p.pid)
		if err != nil {
			continue
		}
		for _, port := range ports {
			if servesWebUI(port) {
				return &Instance{PID: p.pid, Port: port, CSRFToken: csrfFromCmdline(p.cmdline)}, nil
			}
		}
	}

	return nil, ErrNotRunning
}

var probeClient = &http.Client{
	Timeout: 2 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

func servesWebUI(port int) bool {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://127.0.0.1:%d/", port), nil)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", "text/html")

	resp, err := probeClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	head, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return false
	}
	return strings.Contains(string(head), "__APP_CONFIG__")
}

// Fetch retrieves a document from the language server. It backs the doctor
// command and the live anchor test.
func (i *Instance) Fetch(path string) (body []byte, contentType string, err error) {
	req, err := http.NewRequest(http.MethodGet, i.BaseURL()+path, nil)
	if err != nil {
		return nil, "", err
	}
	if strings.HasSuffix(path, ".js") {
		req.Header.Set("Accept", "*/*")
	} else {
		req.Header.Set("Accept", "text/html")
	}

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: probeClient.Transport,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s returned %s", path, resp.Status)
	}

	body, err = io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, "", err
	}
	return body, resp.Header.Get("Content-Type"), nil
}

// WaitFor polls until any standalone language server is ready.
// CSRFHeader is the header the web bundle uses to authorise RPCs against the
// language server.
const CSRFHeader = "x-codeium-csrf-token"

// Call invokes a Connect RPC on the language server. method is the bare method
// name, for example "GetAuthStatus". It returns the raw JSON response.
//
// Some methods block for a long time; Login in particular waits for an OAuth
// callback, so pass a context with a deadline that matches the caller's needs.
func (i *Instance) Call(ctx context.Context, method string, body any) ([]byte, error) {
	payload := []byte("{}")
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = encoded
	}

	url := fmt.Sprintf("%s/exa.language_server_pb.LanguageServerService/%s", i.BaseURL(), method)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	if i.CSRFToken != "" {
		req.Header.Set(CSRFHeader, i.CSRFToken)
	}

	client := &http.Client{Transport: probeClient.Transport}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return data, fmt.Errorf("%s returned %s", method, resp.Status)
	}
	return data, nil
}

// SignedIn reports whether the language server currently holds valid Antigravity
// credentials. A failed query counts as not signed in; use AuthStatus to tell
// that apart from an answer.
func (i *Instance) SignedIn(ctx context.Context) bool {
	signedIn, err := i.AuthStatus(ctx)
	return err == nil && signedIn
}

// AuthStatus asks the language server whether it holds valid Antigravity
// credentials. The error is non-nil when no answer was obtained, for example
// when the call timed out. GetAuthStatus can take seconds right after the
// server's own caches expire, so a failure says nothing about the credentials.
func (i *Instance) AuthStatus(ctx context.Context) (bool, error) {
	data, err := i.Call(ctx, "GetAuthStatus", nil)
	if err != nil {
		return false, err
	}

	var out struct {
		AuthResult struct {
			HasValidAuth bool `json:"hasValidAuth"`
		} `json:"authResult"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return false, err
	}
	return out.AuthResult.HasValidAuth, nil
}

func WaitFor(ctx context.Context, timeout time.Duration, onTick func()) (*Instance, error) {
	return WaitForMatching(ctx, timeout, Filter{}, onTick)
}

// WaitForMatching polls until a language server satisfying filter is ready,
// calling onTick once per second so callers can show progress.
func WaitForMatching(ctx context.Context, timeout time.Duration, filter Filter, onTick func()) (*Instance, error) {
	deadline := time.Now().Add(timeout)

	for {
		if inst, err := FindMatching(filter); err == nil {
			return inst, nil
		}

		if time.Now().After(deadline) {
			return nil, ErrNotRunning
		}

		if onTick != nil {
			onTick()
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
