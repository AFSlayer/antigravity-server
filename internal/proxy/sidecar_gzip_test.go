package proxy

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func gunzip(t *testing.T, body string) string {
	t.Helper()
	zr, err := gzip.NewReader(strings.NewReader(body))
	if err != nil {
		t.Fatalf("body is not gzip: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gzip body is broken: %v", err)
	}
	return string(out)
}

func TestAcceptsGzip(t *testing.T) {
	for in, want := range map[string]bool{
		"gzip, deflate, br": true,
		"br, GZIP;q=0.5":    true,
		"gzip;q=0":          false,
		"gzip; q=0.0":       false,
		"br, deflate":       false,
		"":                  false,
		"x-gzip":            true,
	} {
		if got := acceptsGzip(in); got != want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", in, got, want)
		}
	}
}

// Sidecar servers rarely compress, so the proxy gzips text for a browser
// that accepts it, and leaves everything else as the sidecar sent it.
func TestSidecarResponsesAreGzipped(t *testing.T) {
	js := strings.Repeat("console.log('swarm board');\n", 400)
	var gotINM atomic.Value
	f := newSidecarPolicyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app.js":
			gotINM.Store(r.Header.Get("If-None-Match"))
			if r.Header.Get("If-None-Match") == `W/"v1"` || r.Header.Get("If-None-Match") == `"v1"` {
				w.Header().Set("ETag", `"v1"`)
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("Content-Type", "application/javascript")
			w.Header().Set("ETag", `"v1"`)
			_, _ = io.WriteString(w, js)
		case "/small.js":
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = io.WriteString(w, "1")
		case "/logo.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 1000))
		case "/raw.js":
			w.Header().Set("Content-Type", "application/javascript")
			w.Header().Set("Cache-Control", "no-transform")
			_, _ = io.WriteString(w, js)
		case "/page.html":
			htmlPage(w, "", `<html><head></head><body><a href="/next">next</a>`+strings.Repeat("<p>row</p>", 300)+`</body></html>`)
		default:
			htmlPage(w, "", "<html><head></head><body></body></html>")
		}
	})
	gz := map[string]string{"Accept-Encoding": "gzip, deflate, br"}

	resp, body := f.get(t, "/app.js", gz)
	if resp.Header.Get("Content-Encoding") != "gzip" || gunzip(t, body) != js {
		t.Fatalf("app.js: encoding %q, body not the sidecar's script", resp.Header.Get("Content-Encoding"))
	}
	if !strings.Contains(strings.Join(resp.Header.Values("Vary"), ","), "Accept-Encoding") {
		t.Errorf("gzipped response without Vary: Accept-Encoding: %v", resp.Header)
	}
	if got := resp.Header.Get("ETag"); got != `W/"v1"` {
		t.Errorf("ETag of a gzipped body = %q, want it weakened", got)
	}

	// The browser revalidates with the weak validator and the sidecar's
	// 304 passes through.
	resp, _ = f.get(t, "/app.js", map[string]string{"Accept-Encoding": "gzip", "If-None-Match": `W/"v1"`})
	if resp.StatusCode != http.StatusNotModified || gotINM.Load() != `W/"v1"` {
		t.Fatalf("revalidation: status %d, sidecar saw If-None-Match %q", resp.StatusCode, gotINM.Load())
	}

	resp, body = f.get(t, "/app.js", nil)
	if resp.Header.Get("Content-Encoding") != "" || body != js {
		t.Fatalf("a client without gzip got encoding %q", resp.Header.Get("Content-Encoding"))
	}
	for _, path := range []string{"/small.js", "/logo.png", "/raw.js"} {
		resp, _ = f.get(t, path, gz)
		if resp.Header.Get("Content-Encoding") != "" {
			t.Errorf("%s was gzipped", path)
		}
	}

	// A rewritten page is compressed after the rewrite.
	resp, body = f.get(t, "/page.html", gz)
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("page.html: encoding %q, want gzip", resp.Header.Get("Content-Encoding"))
	}
	if page := gunzip(t, body); !strings.Contains(page, f.capPrefix+"/next") || !strings.Contains(page, sidecarShimName) {
		t.Fatalf("gzipped page lost its rewrite:\n%s", page)
	}

	// The helper script the proxy serves is compressed too.
	resp, body = f.get(t, "/"+sidecarShimName, gz)
	if resp.Header.Get("Content-Encoding") != "gzip" || !strings.Contains(gunzip(t, body), "__agySidecarShimPrefix") {
		t.Fatalf("shim: encoding %q", resp.Header.Get("Content-Encoding"))
	}
}

// A partial response keeps the sidecar's bytes: its Content-Range counts them.
func TestSidecarPartialContentIsNotRewritten(t *testing.T) {
	part := `<a href="/x">` + strings.Repeat("a", 2000)
	contentRange := fmt.Sprintf("bytes 0-%d/9999", len(part)-1)
	f := newSidecarPolicyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/doc.html" {
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Content-Range", contentRange)
			w.WriteHeader(http.StatusPartialContent)
			_, _ = io.WriteString(w, part)
			return
		}
		htmlPage(w, "", "<html><head></head><body></body></html>")
	})
	resp, body := f.get(t, "/doc.html", map[string]string{"Range": "bytes=0-2012", "Accept-Encoding": "gzip"})
	if resp.StatusCode != http.StatusPartialContent || body != part || resp.Header.Get("Content-Range") != contentRange || resp.Header.Get("Content-Encoding") != "" {
		t.Fatalf("206: status %d, encoding %q, Content-Range %q, body changed: %v", resp.StatusCode, resp.Header.Get("Content-Encoding"), resp.Header.Get("Content-Range"), body != part)
	}
}

// A reference that is only a query or a fragment already resolves under the
// capability prefix, against the document rather than its directory.
func TestSidecarQueryOnlyReferences(t *testing.T) {
	const capPrefix = "/__agy/sidecar/4000/~s/0123456789abcdef0123456789abcdef"
	got := string(rewriteSidecarHTML([]byte(`<a href="?page=2">n</a><a href="#top">t</a><form action="?q=1"></form>`), 4000, capPrefix, "/list/", sidecarPageCSP{}))
	for _, want := range []string{`<a href="?page=2">`, `<a href="#top">`, `<form action="?q=1">`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if loc := rewriteSidecarLocation("?page=2", 4000, capPrefix, "/list/"); loc != "?page=2" {
		t.Errorf("Location ?page=2 became %q", loc)
	}
}

// Closing a gzipped body early stops the copy and closes the sidecar's body.
func TestGzipStreamBodyCloseStopsCopy(t *testing.T) {
	pr, pw := io.Pipe()
	var closed atomic.Bool
	src := struct {
		io.Reader
		io.Closer
	}{pr, closerFunc(func() error { closed.Store(true); return pr.Close() })}
	gzBody := newGzipStreamBody(src)
	go func() { _, _ = pw.Write([]byte("hello")) }()
	buf := make([]byte, 64)
	if _, err := gzBody.Read(buf); err != nil {
		t.Fatal(err)
	}
	_ = gzBody.Close()
	if !closed.Load() {
		t.Fatal("closing the gzip body did not close the sidecar's body")
	}
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }
