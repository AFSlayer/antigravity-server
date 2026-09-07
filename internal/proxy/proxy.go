// Package proxy forwards requests to the language server and rewrites the web
// bundle on the way back.
//
// Streaming matters here: agent responses arrive as long-lived chunked bodies, so
// the proxy flushes immediately and never buffers. Compression is only declined
// for the two documents that get patched, leaving every other asset to pass
// through untouched.
package proxy

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/patches"
)

// ReportFunc receives the patch outcome the first time each document is served.
type ReportFunc func(target patches.Target, report patches.Report)

// Options configures a Proxy.
type Options struct {
	TargetPort int
	Patch      patches.Options
	OnReport   ReportFunc
}

// Proxy is a patching reverse proxy in front of one language server.
type Proxy struct {
	handler  *httputil.ReverseProxy
	opts     Options
	reported sync.Map
}

// New builds a Proxy targeting the language server on opts.TargetPort.
func New(opts Options) (*Proxy, error) {
	target, err := url.Parse(fmt.Sprintf("https://127.0.0.1:%d", opts.TargetPort))
	if err != nil {
		return nil, err
	}

	p := &Proxy{opts: opts}

	rp := httputil.NewSingleHostReverseProxy(target)
	rp.FlushInterval = -1
	rp.Transport = &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		DisableCompression:  true,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}

	host := target.Host
	base := rp.Director
	rp.Director = func(req *http.Request) {
		base(req)
		req.Host = host
		req.Header.Set("Origin", target.String())

		if wantsPatch(req) {
			req.Header.Del("Accept-Encoding")
		}
	}

	rp.ModifyResponse = p.modifyResponse
	rp.ErrorHandler = errorHandler

	p.handler = rp
	return p, nil
}

// Handler returns the HTTP handler to mount.
func (p *Proxy) Handler() http.Handler { return p.handler }

func wantsPatch(req *http.Request) bool {
	if req.URL.Path == "/main.js" {
		return true
	}
	return strings.Contains(req.Header.Get("Accept"), "text/html")
}

// targetFor decides whether a response should be patched. Encoded bodies are
// skipped rather than mangled, and skipping also suppresses a misleading
// "anchor not found" report.
func targetFor(resp *http.Response) (patches.Target, bool) {
	if resp.Request == nil || resp.StatusCode != http.StatusOK {
		return 0, false
	}
	if resp.Header.Get("Content-Encoding") != "" {
		return 0, false
	}

	if resp.Request.URL.Path == "/main.js" {
		return patches.MainJS, true
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		return patches.HTML, true
	}
	return 0, false
}

func (p *Proxy) modifyResponse(resp *http.Response) error {
	target, ok := targetFor(resp)
	if !ok {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}

	patched, report := patches.Apply(target, body, p.opts.Patch)
	p.report(target, report)

	resp.Body = io.NopCloser(bytes.NewReader(patched))
	resp.ContentLength = int64(len(patched))
	resp.Header.Set("Content-Length", strconv.Itoa(len(patched)))

	if target == patches.HTML {
		resp.Header.Set("Cache-Control", "no-store")
	} else if resp.Request.URL.Query().Get("agy") != "" {
		resp.Header.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		resp.Header.Set("Cache-Control", "no-store")
	}

	return nil
}

func (p *Proxy) report(target patches.Target, report patches.Report) {
	if p.opts.OnReport == nil {
		return
	}
	if _, loaded := p.reported.LoadOrStore(target, true); loaded {
		return
	}
	p.opts.OnReport(target, report)
}

func errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	if r.Context().Err() != nil {
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "context canceled") || strings.Contains(msg, "client disconnected") {
		return
	}

	w.WriteHeader(http.StatusBadGateway)
	_, _ = w.Write([]byte("Antigravity is not reachable. Is the language server still running?"))
}
