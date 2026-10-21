package signin

import (
	"context"
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
	c.cachedAt = time.Time{}
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

// A GetAuthStatus that does not answer in time says nothing about the
// credentials. It must not read as "sign in again", which is what sent
// signed-in users to the Google sign-in page after the server's caches expired.
func TestCoordinatorNeedsSignInOnlyOnExplicitNo(t *testing.T) {
	var delay atomic.Int64
	var hasValidAuth atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Duration(delay.Load()))
		_ = json.NewEncoder(w).Encode(map[string]any{"authResult": map[string]any{"hasValidAuth": hasValidAuth.Load()}})
	}))
	defer server.Close()

	port := server.Listener.Addr().(*net.TCPAddr).Port
	newCoordinator := func() *Coordinator {
		return New(&lsproc.Instance{Port: port, CSRFToken: "t"}, "/tmp/dummy-url")
	}

	// Slow answer past the caller's deadline: unknown, neither signed in nor out.
	delay.Store(int64(300 * time.Millisecond))
	hasValidAuth.Store(true)
	c := newCoordinator()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	if c.NeedsSignIn(ctx) {
		t.Error("a timed out query must not ask for a sign-in")
	}
	cancel()
	if c.SignedIn(context.Background()) {
		t.Error("the unknown result is cached briefly and must not read as signed in")
	}

	// The same slow server answering "no" in time is an explicit sign-out.
	delay.Store(0)
	hasValidAuth.Store(false)
	c = newCoordinator()
	if !c.NeedsSignIn(context.Background()) {
		t.Error("an explicit hasValidAuth=false must ask for a sign-in")
	}

	// And "yes" never does.
	hasValidAuth.Store(true)
	c = newCoordinator()
	if c.NeedsSignIn(context.Background()) {
		t.Error("a signed-in user must not be asked to sign in")
	}

	// A server that is not there at all is unknown too.
	c = New(&lsproc.Instance{Port: 1, CSRFToken: "t"}, "/tmp/dummy-url")
	if c.NeedsSignIn(context.Background()) {
		t.Error("an unreachable server must not ask for a sign-in")
	}
}

// Once a server is signed in, an expired 30s cache entry must return the cached
// signed-in state immediately and revalidate in the background so root page
// loads never block on a slow GetAuthStatus RPC.
func TestCoordinatorSignedInStaleWhileRevalidate(t *testing.T) {
	var delay atomic.Int64
	var hasValidAuth atomic.Bool
	revalidated := make(chan struct{}, 4)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if d := time.Duration(delay.Load()); d > 0 {
			time.Sleep(d)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"authResult": map[string]any{"hasValidAuth": hasValidAuth.Load()}})
		select {
		case revalidated <- struct{}{}:
		default:
		}
	}))
	defer server.Close()

	port := server.Listener.Addr().(*net.TCPAddr).Port
	c := New(&lsproc.Instance{Port: port, CSRFToken: "t"}, "/tmp/dummy-url")

	hasValidAuth.Store(true)
	if !c.SignedIn(context.Background()) {
		t.Fatal("initial SignedIn check failed")
	}
	<-revalidated

	// Age the positive cache past 30s and make upstream slow (200ms).
	c.authMu.Lock()
	c.cachedAt = time.Now().Add(-35 * time.Second)
	c.authMu.Unlock()
	delay.Store(int64(200 * time.Millisecond))
	hasValidAuth.Store(false)

	start := time.Now()
	if c.NeedsSignIn(context.Background()) {
		t.Fatal("stale positive cache must return immediately as signed in while revalidating")
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Errorf("stale-while-revalidate blocked for %v, want immediate return", elapsed)
	}

	// Wait for the background refresh to finish and flip state to signed out.
	select {
	case <-revalidated:
	case <-time.After(2 * time.Second):
		t.Fatal("background revalidation did not complete")
	}
	for i := 0; i < 50; i++ {
		c.authMu.Lock()
		busy := c.refreshing
		c.authMu.Unlock()
		if !busy {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !c.NeedsSignIn(context.Background()) {
		t.Error("expected NeedsSignIn=true after background revalidation observed sign-out")
	}
}

func TestCoordinatorSignedInFreshInvalidatesStaleCache(t *testing.T) {
	var hasValidAuth atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authResult": map[string]any{
				"hasValidAuth": hasValidAuth.Load(),
			},
		})
	}))
	defer server.Close()

	port := server.Listener.Addr().(*net.TCPAddr).Port
	c := New(&lsproc.Instance{Port: port, CSRFToken: "t"}, "/tmp/dummy-url")
	ctx := context.Background()

	// Warm the 30s positive cache.
	hasValidAuth.Store(true)
	if !c.SignedIn(ctx) {
		t.Fatal("expected initial SignedIn=true")
	}

	// Simulate user signing out inside the web app while the 30s cache is still fresh.
	hasValidAuth.Store(false)
	if !c.SignedIn(ctx) {
		t.Fatal("expected cached SignedIn=true before fresh check")
	}

	// SignedInFresh must bypass the positive cache, observe sign-out immediately,
	// and update the cache so subsequent SignedIn / NeedsSignIn calls also see it.
	if c.SignedInFresh(ctx) {
		t.Fatal("SignedInFresh must return false immediately after sign-out")
	}
	if c.SignedIn(ctx) {
		t.Fatal("SignedIn cache must be updated to false after SignedInFresh")
	}
	if !c.NeedsSignIn(ctx) {
		t.Fatal("NeedsSignIn cache must be updated to true after SignedInFresh")
	}
}
