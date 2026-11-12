package lsproc

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeTree stands in for the process tree walk and lets a test move the clock.
type fakeTree struct {
	mu        sync.Mutex
	clock     time.Time
	endpoints map[int]LoopbackListenMask
	err       error
	walks     atomic.Int32
	gate      chan struct{}
}

func (f *fakeTree) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clock
}

func (f *fakeTree) advance(d time.Duration) {
	f.mu.Lock()
	f.clock = f.clock.Add(d)
	f.mu.Unlock()
}

func (f *fakeTree) set(endpoints map[int]LoopbackListenMask, err error) {
	f.mu.Lock()
	f.endpoints, f.err = endpoints, err
	f.mu.Unlock()
}

func (f *fakeTree) discover(context.Context, int) (map[int]LoopbackListenMask, error) {
	f.walks.Add(1)
	if f.gate != nil {
		<-f.gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[int]LoopbackListenMask, len(f.endpoints))
	for k, v := range f.endpoints {
		out[k] = v
	}
	return out, f.err
}

func newFakeCache(f *fakeTree, ttl, missRefresh time.Duration) *EndpointCache {
	c := newEndpointCache(1234, ttl, missRefresh, f.discover)
	c.now = f.now
	return c
}

// waitWalks waits for a background walk to be counted.
func waitWalks(t *testing.T, f *fakeTree, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for f.walks.Load() < want {
		if time.Now().After(deadline) {
			t.Fatalf("walks = %d, want %d", f.walks.Load(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

// A sidecar page loads dozens of assets at once. They must share one walk of
// the process tree, which on macOS and Windows starts subprocesses.
func TestTreeEndpointCacheSharesOneWalk(t *testing.T) {
	f := &fakeTree{clock: time.Unix(1000, 0), gate: make(chan struct{})}
	f.set(map[int]LoopbackListenMask{4000: ListenIPv4}, nil)
	c := newFakeCache(f, 2*time.Second, time.Second)

	var wg sync.WaitGroup
	var found atomic.Int32
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if mask, ok := c.Lookup(context.Background(), 4000); ok && mask == ListenIPv4 {
				found.Add(1)
			}
		}()
	}
	for f.walks.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(f.gate)
	wg.Wait()

	if n := f.walks.Load(); n != 1 {
		t.Fatalf("walks = %d, want 1 for concurrent lookups", n)
	}
	if n := found.Load(); n != 40 {
		t.Fatalf("%d of 40 lookups found the port", n)
	}

	// Within the TTL a known port is answered from the cache.
	f.advance(1500 * time.Millisecond)
	if _, ok := c.Lookup(context.Background(), 4000); !ok || f.walks.Load() != 1 {
		t.Fatalf("lookup inside the TTL walked again: walks=%d ok=%v", f.walks.Load(), ok)
	}
	// After it, the tree is walked again.
	f.advance(time.Second)
	if _, ok := c.Lookup(context.Background(), 4000); !ok {
		t.Fatal("lookup after the TTL did not find the port")
	}
	waitWalks(t, f, 2)
}

// An expired result still answers a known port at once and is refreshed in
// the background, so a slow walk does not stall a page's requests. A result
// older than two TTLs is not used.
func TestEndpointCacheRefreshesExpiredHitsInBackground(t *testing.T) {
	f := &fakeTree{clock: time.Unix(1000, 0)}
	f.set(map[int]LoopbackListenMask{4000: ListenIPv4}, nil)
	c := newFakeCache(f, 2*time.Second, time.Second)
	if _, ok := c.Lookup(context.Background(), 4000); !ok {
		t.Fatal("known port not found")
	}

	f.gate = make(chan struct{})
	f.advance(2500 * time.Millisecond)
	answered := make(chan bool, 1)
	go func() {
		_, ok := c.Lookup(context.Background(), 4000)
		answered <- ok
	}()
	select {
	case ok := <-answered:
		if !ok {
			t.Fatal("an expired hit was not answered")
		}
	case <-time.After(time.Second):
		t.Fatal("an expired hit waited for the walk")
	}
	waitWalks(t, f, 2)
	// Further hits while that walk runs do not start another.
	if _, ok := c.Lookup(context.Background(), 4000); !ok || f.walks.Load() != 2 {
		t.Fatalf("hit during the refresh: walks=%d ok=%v", f.walks.Load(), ok)
	}

	// The port has closed. Past two TTLs the old result is not trusted.
	f.set(map[int]LoopbackListenMask{}, nil)
	f.advance(2 * time.Second)
	go func() {
		_, ok := c.Lookup(context.Background(), 4000)
		answered <- ok
	}()
	select {
	case <-answered:
		t.Fatal("a result two TTLs old was answered without waiting for the walk")
	case <-time.After(50 * time.Millisecond):
	}
	close(f.gate)
	if ok := <-answered; ok {
		t.Fatal("a closed port was reported after the walk")
	}
}

// Holds fails closed: a failed walk or a caller giving up counts as held.
func TestEndpointCacheHoldsFailsClosed(t *testing.T) {
	f := &fakeTree{clock: time.Unix(1000, 0)}
	f.set(map[int]LoopbackListenMask{4000: 0}, nil)
	c := newFakeCache(f, 5*time.Second, 5*time.Second)
	if !c.Holds(context.Background(), 4000) {
		t.Fatal("a listening port is not held")
	}
	if c.Holds(context.Background(), 4001) {
		t.Fatal("a port that is not listening is held")
	}

	f.set(nil, errors.New("walk failed"))
	f.advance(6 * time.Second)
	if !c.Holds(context.Background(), 4001) {
		t.Fatal("a failed walk did not fail closed")
	}
	if _, ok := c.Lookup(context.Background(), 4000); ok {
		t.Fatal("Lookup reported a port after a failed walk")
	}

	f.set(map[int]LoopbackListenMask{}, nil)
	f.gate = make(chan struct{})
	defer close(f.gate)
	f.advance(6 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if !c.Holds(ctx, 4001) {
		t.Fatal("a caller that gave up did not fail closed")
	}
	if NewProcessPortCache(0, time.Second).Holds(context.Background(), 4001) {
		t.Fatal("a cache without a process holds a port")
	}
}

// NewProcessPortCache reads the ports of the process itself.
func TestProcessPortCacheOwnPorts(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	freePort := free.Addr().(*net.TCPAddr).Port
	_ = free.Close()

	c := NewProcessPortCache(os.Getpid(), time.Second)
	if !c.Holds(context.Background(), port) {
		t.Fatalf("own listening port %d is not held", port)
	}
	if c.Holds(context.Background(), freePort) {
		t.Fatalf("closed port %d is held", freePort)
	}
}

// A port missing from a fresh walk may belong to a sidecar that started since.
// It leads to another walk, but at most once per miss interval.
func TestTreeEndpointCacheMissRefresh(t *testing.T) {
	f := &fakeTree{clock: time.Unix(1000, 0)}
	f.set(map[int]LoopbackListenMask{4000: ListenIPv4}, nil)
	c := newFakeCache(f, 5*time.Second, time.Second)

	if _, ok := c.Lookup(context.Background(), 4000); !ok {
		t.Fatal("known port not found")
	}
	f.set(map[int]LoopbackListenMask{4000: ListenIPv4, 5000: ListenIPv6}, nil)

	if _, ok := c.Lookup(context.Background(), 5000); ok || f.walks.Load() != 1 {
		t.Fatalf("a miss right after a walk walked again: walks=%d ok=%v", f.walks.Load(), ok)
	}
	f.advance(1100 * time.Millisecond)
	mask, ok := c.Lookup(context.Background(), 5000)
	if !ok || mask != ListenIPv6 || f.walks.Load() != 2 {
		t.Fatalf("a miss after the interval: walks=%d mask=%v ok=%v, want a walk that finds it", f.walks.Load(), mask, ok)
	}
}

// A failed walk fails closed and is retried at most once per miss interval,
// so a broken PowerShell does not start one per request.
func TestTreeEndpointCacheFailedWalk(t *testing.T) {
	f := &fakeTree{clock: time.Unix(1000, 0)}
	f.set(nil, errors.New("walk failed"))
	c := newFakeCache(f, 5*time.Second, time.Second)

	for i := 0; i < 5; i++ {
		if _, ok := c.Lookup(context.Background(), 4000); ok {
			t.Fatal("a failed walk must not report a port")
		}
	}
	if n := f.walks.Load(); n != 1 {
		t.Fatalf("walks = %d after repeated lookups on a failure, want 1", n)
	}

	f.set(map[int]LoopbackListenMask{4000: ListenIPv4}, nil)
	f.advance(1100 * time.Millisecond)
	if _, ok := c.Lookup(context.Background(), 4000); !ok || f.walks.Load() != 2 {
		t.Fatalf("retry after the interval: walks=%d ok=%v", f.walks.Load(), ok)
	}
}

// A caller that gives up stops waiting, but the walk finishes for the others.
func TestTreeEndpointCacheCallerCancel(t *testing.T) {
	f := &fakeTree{clock: time.Unix(1000, 0), gate: make(chan struct{})}
	f.set(map[int]LoopbackListenMask{4000: ListenIPv4}, nil)
	c := newFakeCache(f, 5*time.Second, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan bool, 1)
	go func() {
		_, ok := c.Lookup(ctx, 4000)
		result <- ok
	}()
	for f.walks.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case ok := <-result:
		if ok {
			t.Fatal("a cancelled lookup reported a port")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled lookup kept waiting")
	}

	waiter := make(chan bool, 1)
	go func() {
		_, ok := c.Lookup(context.Background(), 4000)
		waiter <- ok
	}()
	time.Sleep(20 * time.Millisecond)
	close(f.gate)
	if ok := <-waiter; !ok {
		t.Fatal("the walk did not finish for a caller still waiting")
	}
	if n := f.walks.Load(); n != 1 {
		t.Fatalf("walks = %d, want the cancelled caller's walk to be shared", n)
	}
}

func TestTreeEndpointCacheNoRoot(t *testing.T) {
	c := NewTreeEndpointCache(0, time.Second, time.Second)
	if _, ok := c.Lookup(context.Background(), 4000); ok {
		t.Fatal("a cache without a root process reported a port")
	}
	var nilCache *EndpointCache
	if _, ok := nilCache.Lookup(context.Background(), 4000); ok {
		t.Fatal("a nil cache reported a port")
	}
}

// The language server's own ports are asked about for every preview request,
// almost always for a port it does not hold. Those answers come from an
// expired result too, so a request does not wait for lsof or PowerShell.
func TestProcessPortCacheAnswersExpiredMissesInBackground(t *testing.T) {
	f := &fakeTree{clock: time.Unix(1000, 0)}
	f.set(map[int]LoopbackListenMask{4000: 0}, nil)
	c := newFakeCache(f, 2*time.Second, 2*time.Second)
	c.staleMisses = true
	if c.Holds(context.Background(), 5173) {
		t.Fatal("a port the process does not hold is held")
	}

	f.gate = make(chan struct{})
	f.advance(2500 * time.Millisecond)
	answered := make(chan bool, 1)
	go func() { answered <- c.Holds(context.Background(), 5173) }()
	select {
	case held := <-answered:
		if held {
			t.Fatal("an expired miss was answered as held")
		}
	case <-time.After(time.Second):
		t.Fatal("an expired miss waited for the walk")
	}
	waitWalks(t, f, 2)

	// Past two TTLs the old result is not used.
	f.advance(2 * time.Second)
	go func() { answered <- c.Holds(context.Background(), 5173) }()
	select {
	case <-answered:
		t.Fatal("a result two TTLs old was used without waiting for the walk")
	case <-time.After(50 * time.Millisecond):
	}
	close(f.gate)
	<-answered
}
