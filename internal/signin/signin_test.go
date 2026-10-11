package signin

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/lsproc"
)

func TestCoordinatorNilInstance(t *testing.T) {
	c := New(nil, "/tmp/urlfile")
	if c.Available() {
		t.Error("Coordinator with nil instance should not be available")
	}
	if c.SignedIn(context.Background()) {
		t.Error("Coordinator with nil instance should not report signed in")
	}
}

func TestCoordinatorAvailable(t *testing.T) {
	inst := &lsproc.Instance{Port: 1234}
	c1 := New(inst, "")
	if c1.Available() {
		t.Error("Coordinator with empty urlFile should not be available")
	}

	c2 := New(inst, "/tmp/some-url-file")
	if !c2.Available() {
		t.Error("Coordinator with instance and urlFile should be available")
	}
}

func TestCoordinatorSignedInCaching(t *testing.T) {
	var callCount atomic.Int32
	var hasValidAuth atomic.Bool

	// Set up mock TLS language server
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/exa.language_server_pb.LanguageServerService/GetAuthStatus" {
			callCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"authResult": map[string]any{
					"hasValidAuth": hasValidAuth.Load(),
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Direct loopback client configuration for test
	port := server.Listener.Addr().(*net.TCPAddr).Port
	inst := &lsproc.Instance{
		Port:      port,
		CSRFToken: "test-token",
	}

	// Override default transport for testing self-signed cert if needed, but lsproc.Call
	// uses custom HTTP client skipping verify:
	// http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	c := New(inst, "/tmp/dummy-url")
	ctx := context.Background()

	// Initial negative check
	hasValidAuth.Store(false)
	if c.SignedIn(ctx) {
		t.Fatal("expected signedIn = false")
	}
	if count := callCount.Load(); count != 1 {
		t.Fatalf("expected 1 call, got %d", count)
	}

	// Immediate second check should hit negative cache (3s TTL)
	if c.SignedIn(ctx) {
		t.Fatal("expected cached signedIn = false")
	}
	if count := callCount.Load(); count != 1 {
		t.Fatalf("expected 1 call due to cache, got %d", count)
	}

	// Clear negative cache by advancing time or forcing positive
	c.authMu.Lock()
	c.cachedSignedAt = time.Time{}
	c.authMu.Unlock()

	hasValidAuth.Store(true)
	if !c.SignedIn(ctx) {
		t.Fatal("expected signedIn = true")
	}
	if count := callCount.Load(); count != 2 {
		t.Fatalf("expected 2 calls, got %d", count)
	}

	// Immediate check should hit positive cache (30s TTL)
	hasValidAuth.Store(false) // Backend toggled, but cache should still return true
	if !c.SignedIn(ctx) {
		t.Fatal("expected cached signedIn = true")
	}
	if count := callCount.Load(); count != 2 {
		t.Fatalf("expected 2 calls due to cache, got %d", count)
	}
}
