package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
)

// relayPath is the language server's WebSocket RPC endpoint, the only upgrade
// the proxy terminates itself.
const relayPath = "/connect-websocket"

// Read limits per direction. The language server sends a whole conversation as
// one JSON message, which reaches a couple of megabytes for a long one, and the
// library's default limit of 32 KiB would cut it off. Requests from the browser
// are small RPC calls, so that direction gets a much lower ceiling.
const (
	upstreamReadLimit = 64 << 20
	clientReadLimit   = 16 << 20
)

// relayWriteTimeout bounds one message write. A phone that stops acknowledging
// would otherwise block its pump until the other direction failed.
const relayWriteTimeout = 2 * time.Minute

// relayWebSocket terminates the browser's WebSocket and opens a second one to the
// language server, copying messages in both directions.
//
// The language server does not negotiate permessage-deflate, so everything it
// sends crosses the network as uncompressed JSON. That is several times larger
// than the same data over fetch, which nginx compresses, and on a slow phone link
// the transfer outlasts the bundle's liveness probe. Terminating the socket here
// lets the proxy compress toward the browser while keeping the loopback leg plain.
//
// Cookies and the other request headers are forwarded to the language server, as
// the plain pass-through did, with Origin rewritten to the loopback address.
// Sub-protocols are negotiated end to end.
//
// The caller must have passed sameOriginUpgrade first. Accept below skips the
// library's own Origin comparison because that check has already run.
func (p *Proxy) relayWebSocket(w http.ResponseWriter, r *http.Request) {
	started := time.Now()

	upstream, err := p.dialUpstreamWebSocket(r)
	if err != nil {
		log.Printf("[proxy] websocket upstream dial failed: %v", err)
		http.Error(w, "language server unavailable", http.StatusBadGateway)
		return
	}
	defer upstream.CloseNow()

	var subprotocols []string
	if chosen := upstream.Subprotocol(); chosen != "" {
		subprotocols = []string{chosen}
	}

	client, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// sameOriginUpgrade has already vetted Origin against Host,
		// X-Forwarded-Host and the public URL, which is stricter about reverse
		// proxies than the library's Host-only comparison.
		InsecureSkipVerify: true,
		Subprotocols:       subprotocols,
		// Each message is compressed on its own. Context takeover would shrink
		// the repeated keys further but holds a sliding window per connection.
		CompressionMode: websocket.CompressionNoContextTakeover,
	})
	if err != nil {
		// Accept has already written the failure response.
		return
	}
	defer client.CloseNow()

	upstream.SetReadLimit(upstreamReadLimit)
	client.SetReadLimit(clientReadLimit)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var lastActivity atomic.Int64
	lastActivity.Store(time.Now().UnixNano())
	recordActivity := func() {
		lastActivity.Store(time.Now().UnixNano())
	}

	// result names the end that outlived the failure, so it can be told how the
	// connection ended.
	type result struct {
		err  error
		peer *websocket.Conn
	}
	done := make(chan result, 3)
	go func() {
		writeFailed, err := pump(ctx, upstream, client, recordActivity)
		if writeFailed {
			done <- result{err, client}
			return
		}
		done <- result{err, upstream}
	}()
	go func() {
		writeFailed, err := pump(ctx, client, upstream, recordActivity)
		if writeFailed {
			done <- result{err, upstream}
			return
		}
		done <- result{err, client}
	}()
	go func() {
		if err := keepAlive(ctx, client, &lastActivity); err != nil {
			done <- result{err, upstream}
		}
	}()

	res := <-done

	// Hand the end of the connection to the other side with the same close code
	// and reason, so the browser sees what the language server said and the
	// language server sees what the browser said. Canceling first would drop the
	// connection without a close frame.
	code, reason := closeStatusFor(res.err)
	if code == websocket.StatusInternalError {
		log.Printf("[proxy] websocket relay closed after %s: %v", time.Since(started).Round(time.Millisecond), res.err)
	}
	_ = res.peer.Close(code, reason)
}

// pingInterval and pingTimeout bound how long a browser that vanished without a
// close frame, such as a phone that went to sleep, keeps the relay and the idle
// accounting alive.
var (
	pingInterval atomic.Int64
	pingTimeout  atomic.Int64
)

func init() {
	pingInterval.Store(int64(30 * time.Second))
	pingTimeout.Store(int64(15 * time.Second))
}

// keepAlive pings the browser when the connection has been idle for pingInterval.
// If any message was read or written recently, the ping is skipped because the
// connection is actively in use. A phone that went to sleep or a link that
// died silently produces no activity, so it gets probed and dropped when unresponsive.
func keepAlive(ctx context.Context, c *websocket.Conn, lastActivity *atomic.Int64) error {
	interval := time.Duration(pingInterval.Load())
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		interval = time.Duration(pingInterval.Load())
		if lastActivity != nil {
			idle := time.Since(time.Unix(0, lastActivity.Load()))
			if idle < interval {
				continue
			}
		}
		timeout := time.Duration(pingTimeout.Load())
		pctx, cancel := context.WithTimeout(ctx, timeout)
		err := c.Ping(pctx)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			// If activity occurred while waiting for ping (e.g. concurrent read/write completed), ignore the timeout.
			if lastActivity != nil && time.Since(time.Unix(0, lastActivity.Load())) < interval {
				continue
			}
			return err
		}
		if lastActivity != nil {
			lastActivity.Store(time.Now().UnixNano())
		}
	}
}

// closeStatusFor maps the error that ended one side to the close frame the other
// side should receive. Codes that must not be sent on the wire, such as 1005 and
// 1006, become a normal closure, and a failure with no close frame becomes an
// internal error so the browser reconnects.
func closeStatusFor(err error) (websocket.StatusCode, string) {
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		if sendableStatus(ce.Code) {
			return ce.Code, truncateReason(ce.Reason)
		}
		return websocket.StatusNormalClosure, ""
	}
	if err == nil || isDisconnect(err) {
		return websocket.StatusGoingAway, ""
	}
	return websocket.StatusInternalError, "relay error"
}

// isDisconnect reports whether err only says that the other end went away, which
// is routine on a phone and not worth a log line.
func isDisconnect(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE)
}

// sendableStatus reports whether a close code may appear in a close frame.
func sendableStatus(c websocket.StatusCode) bool {
	switch {
	case c >= 3000 && c <= 4999:
		return true
	case c >= 1000 && c <= 1014:
		return c != 1004 && c != 1005 && c != 1006
	}
	return false
}

// truncateReason keeps a close reason within the 123 bytes a control frame can
// carry without splitting a UTF-8 sequence.
func truncateReason(s string) string {
	const limit = 123
	if len(s) <= limit {
		return s
	}
	s = s[:limit]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// pump copies whole messages from src to dst, preserving their text or binary
// type, until either side fails or ctx is canceled. writeFailed tells whether
// the failure was on dst, in which case src is still healthy.
// onActivity callbacks, if provided, are invoked whenever a message is transferred.
func pump(ctx context.Context, dst, src *websocket.Conn, onActivity ...func()) (writeFailed bool, err error) {
	for {
		typ, data, err := src.Read(ctx)
		if err != nil {
			return false, err
		}
		for _, fn := range onActivity {
			if fn != nil {
				fn()
			}
		}
		wctx, cancel := context.WithTimeout(ctx, relayWriteTimeout)
		err = dst.Write(wctx, typ, data)
		cancel()
		if err != nil {
			return true, err
		}
		for _, fn := range onActivity {
			if fn != nil {
				fn()
			}
		}
	}
}

// hopHeaders are not forwarded to the language server. The WebSocket handshake
// headers are generated again for that connection.
var hopHeaders = map[string]bool{
	"Connection":               true,
	"Upgrade":                  true,
	"Host":                     true,
	"Origin":                   true,
	"Keep-Alive":               true,
	"Proxy-Connection":         true,
	"Te":                       true,
	"Trailer":                  true,
	"Transfer-Encoding":        true,
	"Content-Length":           true,
	"Sec-Websocket-Key":        true,
	"Sec-Websocket-Version":    true,
	"Sec-Websocket-Extensions": true,
	"Sec-Websocket-Protocol":   true,
}

// dialUpstreamWebSocket opens the language server leg with the same headers the
// reverse proxy would have sent, so the server's Origin and CSRF checks see an
// ordinary loopback client.
func (p *Proxy) dialUpstreamWebSocket(r *http.Request) (*websocket.Conn, error) {
	target := fmt.Sprintf("127.0.0.1:%d", p.opts.TargetPort)

	header := http.Header{}
	for name, values := range r.Header {
		if hopHeaders[http.CanonicalHeaderKey(name)] {
			continue
		}
		header[name] = append([]string(nil), values...)
	}
	header.Set("Origin", "https://"+target)
	if orig := r.Header.Get("Origin"); orig != "" {
		header.Set("X-Original-Origin", orig)
	}
	if clientHost := r.Header.Get("X-Forwarded-Host"); clientHost != "" {
		header.Set("X-Client-Host", clientHost)
	} else if r.Host != "" {
		header.Set("X-Client-Host", r.Host)
	}
	if p.opts.TargetCSRFToken != "" {
		header.Set("x-codeium-csrf-token", p.opts.TargetCSRFToken)
	}

	var subprotocols []string
	for _, line := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, sp := range strings.Split(line, ",") {
			if sp = strings.TrimSpace(sp); sp != "" {
				subprotocols = append(subprotocols, sp)
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "wss://"+target+r.URL.RequestURI(), &websocket.DialOptions{
		HTTPClient:      &http.Client{Transport: p.transport},
		HTTPHeader:      header,
		Subprotocols:    subprotocols,
		CompressionMode: websocket.CompressionDisabled,
	})
	return conn, err
}
