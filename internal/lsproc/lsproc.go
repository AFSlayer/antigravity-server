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
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime"
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
	if f.BinaryPath != "" && !cmdlineContainsBinary(p.cmdline, f.BinaryPath) {
		return false
	}
	return true
}

func isCmdlineArgBoundary(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '"', '\'', 0:
		return true
	default:
		return false
	}
}

func cmdlineContainsBinary(cmdline, binaryPath string) bool {
	if binaryPath == "" {
		return true
	}
	haystack := cmdline
	needle := binaryPath
	if runtime.GOOS == "windows" {
		haystack = strings.ToLower(strings.ReplaceAll(haystack, "/", `\`))
		needle = strings.ToLower(strings.ReplaceAll(needle, "/", `\`))
	}
	offset := 0
	for offset <= len(haystack)-len(needle) {
		idx := strings.Index(haystack[offset:], needle)
		if idx < 0 {
			return false
		}
		pos := offset + idx
		end := pos + len(needle)
		leftOK := pos == 0 || isCmdlineArgBoundary(haystack[pos-1])
		rightOK := end == len(haystack) || isCmdlineArgBoundary(haystack[end])
		if leftOK && rightOK {
			return true
		}
		offset = pos + 1
	}
	return false
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

// StatusError is what Call returns when the language server answers with a
// status other than 200. Call returns the body alongside it.
type StatusError struct {
	Method string
	Code   int
	Status string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s returned %s", e.Method, e.Status)
}

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
		return data, &StatusError{Method: method, Code: resp.StatusCode, Status: resp.Status}
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

// TerminateProcess gracefully stops a language server process and waits until it
// exits, falling back to a forced kill if grace expires. When done is nil (an
// externally started process reused by serve mode), liveness is polled by PID.
func TerminateProcess(proc *os.Process, pid int, done <-chan struct{}, grace time.Duration) {
	if proc == nil && pid > 0 {
		if found, err := os.FindProcess(pid); err == nil {
			proc = found
		}
	}
	if proc == nil {
		return
	}
	if pid <= 0 {
		pid = proc.Pid
	}

	_ = sendTerminate(proc)
	if done != nil {
		select {
		case <-done:
			return
		case <-time.After(grace):
			_ = proc.Kill()
			select {
			case <-done:
			case <-time.After(time.Second):
			}
		}
		return
	}

	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !isProcessAlive(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = proc.Kill()
	killDeadline := time.Now().Add(time.Second)
	for time.Now().Before(killDeadline) {
		if !isProcessAlive(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// CanControlPID reports whether pid is alive and owned by a principal this
// process is permitted to signal or terminate.
func CanControlPID(pid int) bool {
	return canControlProcess(pid)
}

// LoopbackListenMask records which loopback address families a TCP listener accepts.
type LoopbackListenMask uint8

const (
	// ListenIPv4 indicates 127.0.0.0/8 or 0.0.0.0.
	ListenIPv4 LoopbackListenMask = 1 << iota
	// ListenIPv6 indicates ::1.
	ListenIPv6
	// ListenIPv6Wildcard indicates :: (which accepts ::1 and, on dual-stack systems, IPv4).
	ListenIPv6Wildcard
)

// ResolveDialHost returns the exact loopback IP to dial for urlHost ("localhost",
// "127.0.0.1", or "::1") so OAuth callbacks never cross address families onto an
// unrelated local process occupying the same port on another loopback address.
func (m LoopbackListenMask) ResolveDialHost(urlHost string) (string, bool) {
	if m == 0 {
		return "", false
	}
	clean := strings.Trim(strings.ToLower(strings.TrimSpace(urlHost)), "[]")
	switch clean {
	case "127.0.0.1":
		if m&ListenIPv4 != 0 {
			return "127.0.0.1", true
		}
		if m&ListenIPv6Wildcard != 0 {
			return "::1", true
		}
		return "", false
	case "::1":
		if m&(ListenIPv6|ListenIPv6Wildcard) != 0 {
			return "::1", true
		}
		return "", false
	case "localhost":
		if m&ListenIPv4 != 0 {
			return "127.0.0.1", true
		}
		if m&(ListenIPv6|ListenIPv6Wildcard) != 0 {
			return "::1", true
		}
		return "", false
	default:
		return "", false
	}
}

func parseListenHostMask(rawHost string, ipv6Hint bool) LoopbackListenMask {
	h := strings.TrimSpace(rawHost)
	if h == "" || h == "*" {
		if ipv6Hint {
			return ListenIPv6Wildcard
		}
		return ListenIPv4
	}
	h = strings.Trim(h, "[]")
	if zone := strings.IndexByte(h, '%'); zone >= 0 {
		h = h[:zone]
	}
	if h == "*" {
		if ipv6Hint {
			return ListenIPv6Wildcard
		}
		return ListenIPv4
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return 0
	}
	return listenIPMask(ip)
}

// listenIPMask reports the loopback families a listener bound to ip accepts
// when dialed at 127.0.0.1 or ::1. A listener on another 127.x address is not
// counted: dialing 127.0.0.1 would reach whatever listens there instead.
func listenIPMask(ip net.IP) LoopbackListenMask {
	if ip4 := ip.To4(); ip4 != nil {
		if ip4.IsUnspecified() || ip4.Equal(net.IPv4(127, 0, 0, 1)) {
			return ListenIPv4
		}
		return 0
	}
	if ip.IsUnspecified() {
		return ListenIPv6Wildcard
	}
	if ip.Equal(net.IPv6loopback) {
		return ListenIPv6
	}
	return 0
}

const defaultDiscoveryTimeout = 4 * time.Second

func discoveryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defaultDiscoveryTimeout)
}

// ListeningPorts returns the TCP ports currently in LISTEN state owned by pid.
func ListeningPorts(pid int) ([]int, error) {
	return ListeningPortsContext(context.Background(), pid)
}

// ListeningPortsContext returns the TCP ports currently in LISTEN state owned by pid.
func ListeningPortsContext(ctx context.Context, pid int) ([]int, error) {
	if pid <= 0 {
		return nil, errors.New("invalid pid")
	}
	cctx, cancel := discoveryContext(ctx)
	defer cancel()
	return listeningPortsContext(cctx, pid)
}

// ProcessTreeListeningEndpoints returns the loopback-accessible TCP ports in
// LISTEN state owned by rootPID or any of its descendants, paired with the
// loopback address families each port is bound on.
func ProcessTreeListeningEndpoints(ctx context.Context, rootPID int) (map[int]LoopbackListenMask, error) {
	if rootPID <= 0 {
		return nil, errors.New("invalid root pid")
	}
	cctx, cancel := discoveryContext(ctx)
	defer cancel()
	return processTreeListeningEndpoints(cctx, rootPID)
}

// WatchPID polls an externally started process by PID and closes the returned
// channel if the process exits before ctx is cancelled. When ctx ends first,
// the goroutine exits without closing the channel.
func WatchPID(ctx context.Context, pid int, pollInterval time.Duration) <-chan struct{} {
	if pid <= 0 {
		return nil
	}
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}
	done := make(chan struct{})
	go func() {
		if !isProcessAlive(pid) {
			close(done)
			return
		}
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !isProcessAlive(pid) {
					close(done)
					return
				}
			}
		}
	}()
	return done
}
