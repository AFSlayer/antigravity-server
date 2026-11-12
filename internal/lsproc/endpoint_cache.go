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

	mu        sync.Mutex
	endpoints map[int]LoopbackListenMask
	err       error
	walkedAt  time.Time
	inflight  chan struct{}
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
// It records no address families, so Lookup reports a zero mask. A port
// missing from a result is not looked up again until the result is ttl old.
func NewProcessPortCache(pid int, ttl time.Duration) *EndpointCache {
	return newEndpointCache(pid, ttl, ttl, func(ctx context.Context, pid int) (map[int]LoopbackListenMask, error) {
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
	if !c.walkedAt.IsZero() {
		age := c.now().Sub(c.walkedAt)
		if c.err == nil {
			// A listener seen within two TTLs is answered at once. Once the
			// result has expired it is refreshed in the background, so a slow
			// walk does not stall the requests of a page in use.
			if mask, ok := c.endpoints[port]; ok && age < 2*c.ttl {
				if age >= c.ttl {
					c.startWalkLocked()
				}
				c.mu.Unlock()
				return mask, true, nil
			}
			if age < c.missRefresh {
				c.mu.Unlock()
				return 0, false, nil
			}
		} else if age < c.missRefresh {
			err := c.err
			c.mu.Unlock()
			return 0, false, err
		}
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
	if c.err != nil {
		return 0, false, c.err
	}
	mask, ok := c.endpoints[port]
	return mask, ok, nil
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
	if err == nil {
		c.endpoints = endpoints
	}
	c.err = err
	c.walkedAt = c.now()
	c.inflight = nil
	c.mu.Unlock()
	close(done)
}
