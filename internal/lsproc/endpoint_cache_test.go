package lsproc

import (
	"context"
	"errors"
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

func newFakeCache(f *fakeTree, ttl, missRefresh time.Duration) *TreeEndpointCache {
	c := NewTreeEndpointCache(1234, ttl, missRefresh)
	c.discover = f.discover
	c.now = f.now
	return c
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
	if _, ok := c.Lookup(context.Background(), 4000); !ok || f.walks.Load() != 2 {
		t.Fatalf("lookup after the TTL: walks=%d ok=%v, want 2 and found", f.walks.Load(), ok)
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
	var nilCache *TreeEndpointCache
	if _, ok := nilCache.Lookup(context.Background(), 4000); ok {
		t.Fatal("a nil cache reported a port")
	}
}
