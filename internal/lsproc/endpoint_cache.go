package lsproc

import (
	"context"
	"sync"
	"time"
)

// TreeEndpointCache memoizes ProcessTreeListeningEndpoints for one root
// process. A walk reads every process and socket, and on macOS and Windows it
// starts ps, lsof or PowerShell, so the dozens of requests a sidecar page makes
// at once must not each run one. Callers that arrive during a walk share it.
type TreeEndpointCache struct {
	rootPID     int
	ttl         time.Duration
	missRefresh time.Duration
	discover    func(ctx context.Context, rootPID int) (map[int]LoopbackListenMask, error)
	now         func() time.Time

	mu        sync.Mutex
	endpoints map[int]LoopbackListenMask
	err       error
	walkedAt  time.Time
	inflight  chan struct{}
}

// NewTreeEndpointCache returns a cache for rootPID's process tree. A result
// stays fresh for ttl. A port missing from a fresh result, or a failed walk,
// leads to a new walk at most once per missRefresh, since a sidecar may have
// started after the last one.
func NewTreeEndpointCache(rootPID int, ttl, missRefresh time.Duration) *TreeEndpointCache {
	if missRefresh > ttl {
		missRefresh = ttl
	}
	return &TreeEndpointCache{
		rootPID:     rootPID,
		ttl:         ttl,
		missRefresh: missRefresh,
		discover:    ProcessTreeListeningEndpoints,
		now:         time.Now,
	}
}

// Lookup reports the loopback families port listens on in the process tree.
// It answers false when the port is not in the tree or the walk failed.
func (c *TreeEndpointCache) Lookup(ctx context.Context, port int) (LoopbackListenMask, bool) {
	if c == nil || c.rootPID <= 0 {
		return 0, false
	}
	if ctx == nil {
		ctx = context.Background()
	}

	c.mu.Lock()
	if !c.walkedAt.IsZero() {
		age := c.now().Sub(c.walkedAt)
		if age < c.ttl && c.err == nil {
			if mask, ok := c.endpoints[port]; ok {
				c.mu.Unlock()
				return mask, true
			}
		}
		if age < c.missRefresh {
			c.mu.Unlock()
			return 0, false
		}
	}
	done := c.inflight
	if done == nil {
		done = make(chan struct{})
		c.inflight = done
		// The walk is not tied to this caller's context, so one caller giving
		// up does not fail the others waiting on it.
		go c.walk(done)
	}
	c.mu.Unlock()

	select {
	case <-done:
	case <-ctx.Done():
		return 0, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return 0, false
	}
	mask, ok := c.endpoints[port]
	return mask, ok
}

func (c *TreeEndpointCache) walk(done chan struct{}) {
	endpoints, err := c.discover(context.Background(), c.rootPID)

	c.mu.Lock()
	if err == nil {
		c.endpoints = endpoints
	}
	c.err = err
	c.walkedAt = c.now()
	c.inflight = nil
	c.mu.Unlock()
	close(done)
}
