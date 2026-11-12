package auth

import (
	"net"
	"strings"
	"sync"
	"time"
)

const (
	maxLimiterEntries = 4096
	maxPendingEntries = 1024
	maxBackoff        = 30 * time.Minute
)

type limiterEntry struct {
	failures    int
	windowStart time.Time
	lockedUntil time.Time
	strikes     int
	seen        time.Time
}

// Limiter counts failures per key and locks the key out with exponential backoff
// once they exceed max within window.
type Limiter struct {
	mu      sync.Mutex
	entries map[string]*limiterEntry
	max     int
	window  time.Duration
}

// NewLimiter returns a Limiter allowing max failures per window.
func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{
		entries: map[string]*limiterEntry{},
		max:     max,
		window:  window,
	}
}

func normalizeLimiterKey(key string) string {
	trimmed := strings.TrimSpace(key)
	ip := net.ParseIP(trimmed)
	if ip == nil || ip.To4() != nil || ip.IsLoopback() {
		return key
	}
	masked := ip.Mask(net.CIDRMask(64, 128))
	if masked == nil {
		return key
	}
	return masked.String() + "/64"
}

// Allow reports whether key may attempt again, and if not, how long to wait.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	key = normalizeLimiterKey(key)
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	e, ok := l.entries[key]
	if !ok {
		return true, 0
	}
	e.seen = now

	if now.Before(e.lockedUntil) {
		retry := e.lockedUntil.Sub(now).Round(time.Second)
		if retry < time.Second {
			retry = time.Second
		}
		return false, retry
	}
	if now.Sub(e.windowStart) > l.window {
		e.failures = 0
		e.windowStart = now
	}
	return true, 0
}

// Fail records a failed attempt, extending the lockout if the limit is reached.
func (l *Limiter) Fail(key string) {
	key = normalizeLimiterKey(key)
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.pruneExpiredLocked(now)

	e, ok := l.entries[key]
	if !ok {
		l.evictUnlockedIfOverPendingBudgetLocked(now)
		e = &limiterEntry{windowStart: now}
		l.entries[key] = e
	}
	e.seen = now

	if now.Sub(e.windowStart) > l.window {
		e.failures = 0
		e.windowStart = now
	}
	e.failures++

	if e.failures >= l.max {
		e.strikes++
		backoff := time.Duration(1<<uint(min(e.strikes-1, 5))) * time.Minute
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
		e.lockedUntil = now.Add(backoff)
		e.failures = 0
		e.windowStart = now
		l.evictLeastLockedIfOverBudgetLocked(now, key)
	}
}

// Reset clears a key's history after a successful attempt.
func (l *Limiter) Reset(key string) {
	key = normalizeLimiterKey(key)
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
}

func (l *Limiter) pruneExpiredLocked(now time.Time) {
	if len(l.entries) < maxLimiterEntries+maxPendingEntries {
		return
	}
	for key, e := range l.entries {
		if now.Sub(e.seen) > l.window && !now.Before(e.lockedUntil) {
			delete(l.entries, key)
		}
	}
}

func (l *Limiter) evictUnlockedIfOverPendingBudgetLocked(now time.Time) {
	for len(l.entries) >= maxLimiterEntries+maxPendingEntries {
		var victimKey string
		var victim *limiterEntry
		for key, e := range l.entries {
			if now.Before(e.lockedUntil) {
				continue
			}
			if victim == nil ||
				e.strikes < victim.strikes ||
				(e.strikes == victim.strikes && e.seen.Before(victim.seen)) {
				victimKey = key
				victim = e
			}
		}
		if victimKey == "" {
			return
		}
		delete(l.entries, victimKey)
	}
}

func (l *Limiter) evictLeastLockedIfOverBudgetLocked(now time.Time, keepKey string) {
	if len(l.entries) <= maxLimiterEntries {
		return
	}
	for {
		lockedCount := 0
		var victimKey string
		var victim *limiterEntry
		for key, e := range l.entries {
			if !now.Before(e.lockedUntil) {
				continue
			}
			lockedCount++
			if key == keepKey {
				continue
			}
			if victim == nil ||
				e.strikes < victim.strikes ||
				(e.strikes == victim.strikes && e.lockedUntil.Before(victim.lockedUntil)) ||
				(e.strikes == victim.strikes && e.lockedUntil.Equal(victim.lockedUntil) && e.seen.Before(victim.seen)) {
				victimKey = key
				victim = e
			}
		}
		if lockedCount <= maxLimiterEntries || victimKey == "" {
			return
		}
		delete(l.entries, victimKey)
	}
}

// TokenBucket bounds aggregate authentication throughput without imposing
// long global lockouts that would let one attacker lock out legitimate users.
type TokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	burst      float64
	refillRate float64
	lastRefill time.Time
}

// NewTokenBucket returns a TokenBucket refilling at ratePerSec up to burst tokens.
func NewTokenBucket(ratePerSec float64, burst int) *TokenBucket {
	if ratePerSec <= 0 {
		ratePerSec = 1
	}
	if burst < 1 {
		burst = 1
	}
	return &TokenBucket{
		tokens:     float64(burst),
		burst:      float64(burst),
		refillRate: ratePerSec,
		lastRefill: time.Now(),
	}
}

// Allow consumes one token when available, or returns how long to wait for the next token.
func (b *TokenBucket) Allow() (bool, time.Duration) {
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()

	if elapsed := now.Sub(b.lastRefill).Seconds(); elapsed > 0 {
		b.tokens += elapsed * b.refillRate
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
		b.lastRefill = now
	}
	if b.tokens >= 1 {
		b.tokens -= 1
		return true, 0
	}
	waitSec := (1 - b.tokens) / b.refillRate
	wait := time.Duration(waitSec * float64(time.Second)).Round(time.Second)
	if wait < time.Second {
		wait = time.Second
	}
	return false, wait
}
