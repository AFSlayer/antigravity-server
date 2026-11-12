package proxy

import (
	"compress/gzip"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/AFSlayer/antigravity-server/internal/httpenc"
)

// Sidecar servers on loopback rarely compress, and the proxy reads their
// bodies uncompressed to rewrite them, so the proxy compresses what it sends
// on to a browser that may be on a slow link.
const sidecarGzipMinBytes = 1024

// compressibleSidecarType reports whether a Content-Type is text that gzip
// shrinks. Event streams are excluded: they are read as they arrive.
func compressibleSidecarType(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch {
	case mt == "text/event-stream":
		return false
	case strings.HasPrefix(mt, "text/"):
		return true
	case strings.HasSuffix(mt, "+json"), strings.HasSuffix(mt, "+xml"):
		return true
	}
	switch mt {
	case "application/javascript", "application/x-javascript", "application/ecmascript",
		"application/json", "application/xml", "image/svg+xml", "application/wasm":
		return true
	}
	return false
}

// compressSidecarResponse gzips resp for clientReq when the browser accepts
// gzip and the body is text that the sidecar sent uncompressed. Partial
// content and responses marked no-transform are left alone.
func compressSidecarResponse(resp *http.Response, clientReq *http.Request) {
	if resp.Body == nil || resp.Body == http.NoBody {
		return
	}
	if resp.Request != nil && resp.Request.Method == http.MethodHead {
		return
	}
	switch {
	case resp.StatusCode < 200, resp.StatusCode == http.StatusNoContent,
		resp.StatusCode == http.StatusPartialContent, resp.StatusCode == http.StatusNotModified:
		return
	}
	if resp.Header.Get("Content-Encoding") != "" || resp.Header.Get("Content-Range") != "" {
		return
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Cache-Control")), "no-transform") {
		return
	}
	if !compressibleSidecarType(resp.Header.Get("Content-Type")) {
		return
	}
	httpenc.AddVary(resp.Header)
	if !httpenc.AcceptsGzip(clientReq.Header.Get("Accept-Encoding")) {
		return
	}
	if resp.ContentLength >= 0 && resp.ContentLength < sidecarGzipMinBytes {
		return
	}
	// The bytes differ from the sidecar's, so a strong validator becomes
	// weak. Conditional requests still match it.
	if etag := resp.Header.Get("ETag"); etag != "" && !strings.HasPrefix(etag, "W/") {
		resp.Header.Set("ETag", "W/"+etag)
	}
	resp.Header.Set("Content-Encoding", "gzip")
	resp.Header.Del("Content-Length")
	// Byte ranges of the gzipped body are not the sidecar's, so a resumed
	// download must not ask for them.
	resp.Header.Del("Accept-Ranges")
	resp.ContentLength = -1
	resp.Body = newGzipStreamBody(resp.Body)
}

// gzipStreamBody compresses src as it is read. Each chunk read from src is
// flushed, so a response the sidecar sends in parts reaches the browser in
// parts.
type gzipStreamBody struct {
	pr  *io.PipeReader
	src io.ReadCloser
}

func newGzipStreamBody(src io.ReadCloser) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		defer src.Close()
		gz := gzip.NewWriter(pw)
		buf := make([]byte, 32<<10)
		for {
			n, rerr := src.Read(buf)
			if n > 0 {
				if _, err := gz.Write(buf[:n]); err != nil {
					pw.CloseWithError(err)
					return
				}
				if err := gz.Flush(); err != nil {
					pw.CloseWithError(err)
					return
				}
			}
			if rerr == io.EOF {
				pw.CloseWithError(gz.Close())
				return
			}
			if rerr != nil {
				pw.CloseWithError(rerr)
				return
			}
		}
	}()
	return &gzipStreamBody{pr: pr, src: src}
}

func (b *gzipStreamBody) Read(p []byte) (int, error) { return b.pr.Read(p) }

// Close stops the copy: closing src ends a read in progress, and closing pr
// fails the next write into the pipe.
func (b *gzipStreamBody) Close() error {
	err := b.pr.Close()
	_ = b.src.Close()
	return err
}

// writeSidecarGenerated writes a body the proxy made itself, gzipped when the
// browser accepts it. Headers other than the encoding and length are set by
// the caller.
func writeSidecarGenerated(w http.ResponseWriter, r *http.Request, body []byte) {
	httpenc.AddVary(w.Header())
	if len(body) < sidecarGzipMinBytes || !httpenc.AcceptsGzip(r.Header.Get("Accept-Encoding")) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
		return
	}
	w.Header().Set("Content-Encoding", "gzip")
	if r.Method == http.MethodHead {
		return
	}
	gz := gzip.NewWriter(w)
	_, _ = gz.Write(body)
	_ = gz.Close()
}
