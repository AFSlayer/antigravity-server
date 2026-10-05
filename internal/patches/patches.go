// Package patches rewrites Antigravity's web bundle as it passes through the
// proxy.
//
// The bundle is minified and compiled into Google's language_server binary, so
// patches are anchored on exact substrings rather than parsed structure. Every
// anchor is asserted to match exactly once against a fixture of the real bundle
// (see patches_test.go), and Apply reports which patches matched at runtime so a
// bundle change surfaces as a warning instead of silent breakage.
package patches

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// Target identifies which served document a patch applies to.
type Target int

const (
	// MainJS is the compiled application bundle served at /main.js.
	MainJS Target = iota
	// HTML is any text/html response, in practice the SPA shell.
	HTML
)

func (t Target) String() string {
	if t == MainJS {
		return "main.js"
	}
	return "index.html"
}

// Kind selects how a patch transforms its target.
type Kind int

const (
	// Literal replaces the first occurrence of Find.
	Literal Kind = iota
	// Regexp replaces the first match of FindRe.
	Regexp
	// InjectHead appends Replace just before </head>, ignoring Find.
	InjectHead
)

// Options carries the user's configuration into patch selection and rendering.
type Options struct {
	// MobileUX enables the touch-usability patches.
	MobileUX bool
	// WorkspaceRoot, when set, becomes the folder picker's starting directory.
	WorkspaceRoot string
	// CacheKey busts cached bundles when the applied patch set changes.
	CacheKey string
	// Disabled turns off individual patches by ID, for narrowing down which one is
	// responsible when the UI misbehaves.
	Disabled map[string]bool
	// Debug injects the mobile geometry tracer, which reports to the server.
	Debug bool
	// WebSocketTransportMissing is set by the proxy once the bundle has been
	// served without the websocket-transport-default patch matching. The bundle
	// then uses fetch streaming and never claims an early socket.
	WebSocketTransportMissing bool
}

// Patch is a single declarative rewrite of the served bundle.
type Patch struct {
	// ID is the stable, kebab-case name reported by doctor and the control panel.
	ID string
	// Desc is a user-facing explanation of what the patch fixes.
	Desc string

	Target Target
	Kind   Kind

	// Find is the anchor for Literal patches, FindRe for Regexp patches.
	Find   string
	FindRe *regexp.Regexp

	// Replace is the replacement text; ReplaceFn takes precedence when set.
	Replace   string
	ReplaceFn func(Options) string

	// Required marks patches without which remote access cannot work at all.
	Required bool
	// Optional marks patches that may not match on newer upstream builds where Google already fixed/removed the element.
	Optional bool
	// Enabled gates the patch on configuration; nil means always enabled.
	Enabled func(Options) bool
}

func (p Patch) replacement(opts Options) string {
	if p.ReplaceFn != nil {
		return p.ReplaceFn(opts)
	}
	return p.Replace
}

func (p Patch) enabled(opts Options) bool {
	if opts.Disabled[p.ID] {
		return false
	}
	if p.Enabled == nil {
		return true
	}
	return p.Enabled(opts)
}

// Status is the outcome of attempting one patch.
type Status int

const (
	// StatusApplied means the anchor matched and the rewrite happened.
	StatusApplied Status = iota
	// StatusMissing means the anchor was not found, so Antigravity likely changed.
	StatusMissing
	// StatusDisabled means configuration turned the patch off.
	StatusDisabled
	// StatusNotNeeded means the anchor was absent because upstream already addressed it.
	StatusNotNeeded
)

func (s Status) String() string {
	switch s {
	case StatusApplied:
		return "applied"
	case StatusDisabled:
		return "disabled"
	case StatusNotNeeded:
		return "not needed"
	default:
		return "not found"
	}
}

// Result records what happened to one patch during Apply.
type Result struct {
	ID       string
	Desc     string
	Target   Target
	Status   Status
	Required bool
}

// Report is the set of results from one Apply call.
type Report []Result

// MissingRequired returns the essential patches whose anchors were not found.
func (r Report) MissingRequired() []Result {
	var out []Result
	for _, res := range r {
		if res.Status == StatusMissing && res.Required {
			out = append(out, res)
		}
	}
	return out
}

// Missing returns every patch whose anchor was not found.
func (r Report) Missing() []Result {
	var out []Result
	for _, res := range r {
		if res.Status == StatusMissing {
			out = append(out, res)
		}
	}
	return out
}

// Apply rewrites body with every enabled patch for target and reports the
// outcome of each. A patch whose anchor is missing is skipped rather than fatal,
// so one stale anchor never takes the whole proxy down.
func Apply(target Target, body []byte, opts Options) ([]byte, Report) {
	var report Report
	var head []string

	for _, p := range All() {
		if p.Target != target {
			continue
		}

		res := Result{ID: p.ID, Desc: p.Desc, Target: p.Target, Required: p.Required}

		if !p.enabled(opts) {
			res.Status = StatusDisabled
			report = append(report, res)
			continue
		}

		switch p.Kind {
		case InjectHead:
			head = append(head, p.replacement(opts))
			res.Status = StatusApplied

		case Literal:
			if bytes.Contains(body, []byte(p.Find)) {
				body = bytes.Replace(body, []byte(p.Find), []byte(p.replacement(opts)), 1)
				res.Status = StatusApplied
			} else if p.Optional {
				res.Status = StatusNotNeeded
			} else {
				res.Status = StatusMissing
			}

		case Regexp:
			loc := p.FindRe.FindSubmatchIndex(body)
			if loc != nil {
				repl := p.FindRe.Expand(nil, []byte(p.replacement(opts)), body, loc)
				var out []byte
				out = append(out, body[:loc[0]]...)
				out = append(out, repl...)
				out = append(out, body[loc[1]:]...)
				body = out
				res.Status = StatusApplied
			} else if p.Optional {
				res.Status = StatusNotNeeded
			} else {
				res.Status = StatusMissing
			}
		}

		report = append(report, res)
	}

	if len(head) > 0 {
		body = injectHead(body, strings.Join(head, "\n"))
	}

	return body, report
}

// CacheKey derives a short, stable fingerprint of the applied patch set. It is
// appended to the /main.js URL so the bundle URL changes the moment the patches
// or version change. It does not cover language server updates; the proxy's
// ETag revalidation is what keeps browsers from running a stale bundle.
func CacheKey(version string, opts Options) string {
	h := sha256.New()
	if version != "" {
		h.Write([]byte(version))
	} else {
		h.Write([]byte("v0.2.1-fix"))
	}
	h.Write([]byte{0})
	h.Write([]byte(opts.WorkspaceRoot))
	h.Write([]byte{0})

	for _, p := range All() {
		if !p.enabled(opts) {
			continue
		}
		h.Write([]byte(p.ID))
		h.Write([]byte{0})
		h.Write([]byte(p.replacement(opts)))
		h.Write([]byte{0})
	}

	return hex.EncodeToString(h.Sum(nil))[:12]
}

func injectHead(body []byte, payload string) []byte {
	block := []byte("\n" + payload + "\n")

	if i := bytes.LastIndex(body, []byte("</head>")); i >= 0 {
		out := make([]byte, 0, len(body)+len(block))
		out = append(out, body[:i]...)
		out = append(out, block...)
		out = append(out, body[i:]...)
		return out
	}

	return append(block, body...)
}
