package lsproc

import (
	"context"
	"sync"
	"time"
)

// EndpointCache memoizes a walk of the listeners of one process, or of a
// process and its descendants. A walk reads every process and socket, and on
// macOS and Windows it starts ps, lsof or PowerShell, so the dozens of
// requests a page makes at once must not each run one. Callers that arrive
// during a walk share it.
type EndpointCache struct {
	pid         int
	ttl         time.Duration
	missRefresh time.Duration
	discover    func(ctx context.Context, pid int) (map[int]LoopbackListenMask, error)
	now         func() time.Time
	// staleMisses answers a port missing from an expired result the way a
	// known port is answered, so it does not wait for a walk either.
	staleMisses bool

	mu sync.Mutex
	// endpoints is the result of the last walk that succeeded, at okAt.
	endpoints map[int]LoopbackListenMask
	okAt      time.Time
	// err is the last walk's error, nil if it succeeded, and walkedAt when
	// it ended.
	err      error
	walkedAt time.Time
	inflight chan struct{}
}

// NewTreeEndpointCache returns a cache of the listeners of rootPID and its
// descendants, with the loopback families each listens on. A result stays
// fresh for ttl. A port missing from a fresh result, or a failed walk, leads
// to a new walk at most once per missRefresh, since a listener may have
// started after the last one.
func NewTreeEndpointCache(rootPID int, ttl, missRefresh time.Duration) *EndpointCache {
	return newEndpointCache(rootPID, ttl, missRefresh, ProcessTreeListeningEndpoints)
}

// NewProcessPortCache returns a cache of the ports pid itself listens on.
// It records no address families, so Lookup reports a zero mask. Most
// lookups are for ports the process does not hold, so those too are answered
// from a result up to two TTLs old while a newer one is fetched: a port the
// process opened since may go unnoticed for that long.
func NewProcessPortCache(pid int, ttl time.Duration) *EndpointCache {
	c := newEndpointCache(pid, ttl, ttl, func(ctx context.Context, pid int) (map[int]LoopbackListenMask, error) {
		ports, err := ListeningPortsContext(ctx, pid)
		if err != nil {
			return nil, err
		}
		out := make(map[int]LoopbackListenMask, len(ports))
		for _, port := range ports {
			out[port] = 0
		}
		return out, nil
	})
	c.staleMisses = true
	return c
}

func newEndpointCache(pid int, ttl, missRefresh time.Duration, discover func(context.Context, int) (map[int]LoopbackListenMask, error)) *EndpointCache {
	if missRefresh > ttl {
		missRefresh = ttl
	}
	return &EndpointCache{
		pid:         pid,
		ttl:         ttl,
		missRefresh: missRefresh,
		discover:    discover,
		now:         time.Now,
	}
}

// Lookup reports the loopback families port listens on. It answers false
// when the port is not listening, the walk failed, or ctx ended first.
func (c *EndpointCache) Lookup(ctx context.Context, port int) (LoopbackListenMask, bool) {
	mask, found, err := c.lookup(ctx, port)
	return mask, found && err == nil
}

// Holds reports whether port is listening. It also answers true when that is
// unknown because the walk failed or ctx ended first, for callers that must
// refuse a port unless it is known not to be the process's.
func (c *EndpointCache) Holds(ctx context.Context, port int) bool {
	_, found, err := c.lookup(ctx, port)
	return found || err != nil
}

func (c *EndpointCache) lookup(ctx context.Context, port int) (LoopbackListenMask, bool, error) {
	if c == nil || c.pid <= 0 {
		return 0, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	c.mu.Lock()
	if mask, ok, answered := c.answerLocked(port); answered {
		c.mu.Unlock()
		return mask, ok, nil
	}
	// A walk that just failed is not retried before missRefresh.
	if c.err != nil && c.now().Sub(c.walkedAt) < c.missRefresh {
		err := c.err
		c.mu.Unlock()
		return 0, false, err
	}
	done := c.startWalkLocked()
	c.mu.Unlock()

	select {
	case <-done:
	case <-ctx.Done():
		return 0, false, ctx.Err()
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if mask, ok, answered := c.answerLocked(port); answered {
		return mask, ok, nil
	}
	if c.err != nil {
		return 0, false, c.err
	}
	mask, ok := c.endpoints[port]
	return mask, ok, nil
}

// answerLocked answers from the last result that succeeded without a walk,
// if it can. A listener in it is answered for two TTLs, even when a walk
// since has failed, so one slow or failed walk does not drop the listeners a
// page is using; with staleMisses so is a port missing from it. Once the
// result has expired, a walk starts in the background, at most once per
// missRefresh after a failure. A port missing from a fresh result is
// answered until missRefresh after the walk that produced it.
func (c *EndpointCache) answerLocked(port int) (LoopbackListenMask, bool, bool) {
	if c.okAt.IsZero() {
		return 0, false, false
	}
	now := c.now()
	sinceOK, sinceWalk := now.Sub(c.okAt), now.Sub(c.walkedAt)
	mask, ok := c.endpoints[port]
	if (ok || c.staleMisses) && sinceOK < 2*c.ttl {
		if sinceOK >= c.ttl && sinceWalk >= c.missRefresh {
			c.startWalkLocked()
		}
		return mask, ok, true
	}
	if !ok && c.err == nil && sinceWalk < c.missRefresh {
		return 0, false, true
	}
	return 0, false, false
}

// startWalkLocked starts a walk unless one is running and returns the channel
// closed when it ends. The walk is not tied to any caller's context, so one
// caller giving up does not fail the others waiting on it.
func (c *EndpointCache) startWalkLocked() chan struct{} {
	if c.inflight == nil {
		c.inflight = make(chan struct{})
		go c.walk(c.inflight)
	}
	return c.inflight
}

func (c *EndpointCache) walk(done chan struct{}) {
	endpoints, err := c.discover(context.Background(), c.pid)

	c.mu.Lock()
	now := c.now()
	if err == nil {
		c.endpoints = endpoints
		c.okAt = now
	}
	c.err = err
	c.walkedAt = now
	c.inflight = nil
	c.mu.Unlock()
	close(done)
}
