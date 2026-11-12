package proxy

import (
	"strings"
	"testing"
	"time"
)

const fuzzCapPrefix = "/__agy/sidecar/4000/~s/0123456789abcdef0123456789abcdef"

// rewriteWithin rewrites body and fails if it does not return within limit:
// a sidecar's page must never hang the proxy.
func rewriteWithin(t *testing.T, body string, limit time.Duration) string {
	t.Helper()
	done := make(chan string, 1)
	go func() {
		done <- string(rewriteSidecarHTML([]byte(body), 4000, fuzzCapPrefix, "/", sidecarPageCSP{}))
	}()
	select {
	case out := <-done:
		return out
	case <-time.After(limit):
		t.Fatalf("rewriting %q did not return", body)
		return ""
	}
}

// A "/" inside a tag that does not close it, and a "<" in text, once made
// the rewriter loop without end, growing memory until the server died.
func TestSidecarHTMLStrayCharactersTerminate(t *testing.T) {
	shim := `<script src="` + fuzzCapPrefix + `/` + sidecarShimName + `"></script>`
	for in, want := range map[string]string{
		`<br/ >`:                                `<br/ >`,
		`<img src="a.png" / >`:                  `<img src="` + fuzzCapPrefix + `/a.png" / >`,
		`<a b/c href="/x">`:                     `<a b/c href="` + fuzzCapPrefix + `/x">`,
		`<p>1 < 2</p>`:                          `<p>1 < 2</p>`,
		`<p>a <b> c</p><a href="/y">`:           `<p>a <b> c</p><a href="` + fuzzCapPrefix + `/y">`,
		`<p>it's 3 <4 and "q"</p><a href="/z">`: `<p>it's 3 <4 and "q"</p><a href="` + fuzzCapPrefix + `/z">`,
		// Read as a tag, the text's quote would swallow the link after it.
		`<p>3 <4 "</p><a href="/z">`: `<p>3 <4 "</p><a href="` + fuzzCapPrefix + `/z">`,
		`x </ y> <a href=/w>`:        `x </ y> <a href=` + fuzzCapPrefix + `/w>`,
	} {
		got := strings.TrimPrefix(rewriteWithin(t, in, 2*time.Second), shim)
		if got != want {
			t.Errorf("rewrite(%q) = %q, want %q", in, got, want)
		}
	}
}

func FuzzRewriteSidecarHTML(f *testing.F) {
	for _, seed := range []string{
		`<br/ >`, `<img src="a.png" / >`, `<a b/c>`, `<p>1 < 2</p>`,
		`<html><head><base href="../x/"><meta http-equiv="Content-Security-Policy" content="default-src 'self'"></head>` +
			`<body><a href="/a">a</a><img srcset="/a.png 1x, b.png 2x"><style>body{background:url(/bg.png)}</style>` +
			`<script>var s = "<a href='/no'>";</script><form action="?q=1"></form></body></html>`,
		`<!doctype html><title><a href="/t"></title><textarea></textarea`,
		`<a href="/x` + "\n" + `<b "'=/>`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body string) {
		rewriteWithin(t, body, 5*time.Second)
		done := make(chan struct{})
		go func() {
			_ = rewriteSidecarCSS([]byte(body), 4000, fuzzCapPrefix, "/")
			_ = rewriteSidecarLocation(body, 4000, fuzzCapPrefix, "/")
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("rewriting CSS or a Location from %q did not return", body)
		}
	})
}
