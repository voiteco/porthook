// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"sync"
	"time"
)

// authAttemptMaxKeys bounds the throttle's memory use. Once exceeded, the
// oldest-seen key is evicted to make room, so a wide spray of source
// addresses cannot grow the map without bound.
const authAttemptMaxKeys = 10000

// authAttemptThrottle applies bounded protection against basic-auth and
// bearer-token credential guessing, keyed per subdomain and client address.
// Only failed credential checks consume budget: public traffic and
// successful authentication never touch it, so it cannot block normal
// tunnel traffic.
type authAttemptThrottle struct {
	mu      sync.Mutex
	entries map[string]*authAttemptEntry
	limit   int
	window  time.Duration
}

type authAttemptEntry struct {
	limiter  *rateLimiter
	lastSeen time.Time
}

func newAuthAttemptThrottle(limit int, window time.Duration) *authAttemptThrottle {
	if limit <= 0 {
		limit = defaultAuthAttemptLimit
	}
	if window <= 0 {
		window = defaultAuthAttemptWindow
	}
	return &authAttemptThrottle{
		entries: make(map[string]*authAttemptEntry),
		limit:   limit,
		window:  window,
	}
}

// blocked reports whether key has exhausted its attempt budget, without
// consuming any budget itself. Requests that were never denied never create
// an entry, so this is always false for keys that have not failed before.
func (t *authAttemptThrottle) blocked(key string, now time.Time) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	entry, ok := t.entries[key]
	t.mu.Unlock()
	if !ok {
		return false
	}
	return !entry.limiter.peek(now)
}

// recordFailure consumes one unit of key's attempt budget after a failed
// basic-auth or bearer-token check.
func (t *authAttemptThrottle) recordFailure(key string, now time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.entries[key]
	if !ok {
		if len(t.entries) >= authAttemptMaxKeys {
			t.evictOldestLocked()
		}
		entry = &authAttemptEntry{limiter: newAttemptLimiter(t.limit, t.window, now)}
		t.entries[key] = entry
	}
	entry.lastSeen = now
	entry.limiter.allow(now)
}

func (t *authAttemptThrottle) evictOldestLocked() {
	var oldestKey string
	var oldestSeen time.Time
	first := true
	for key, entry := range t.entries {
		if first || entry.lastSeen.Before(oldestSeen) {
			oldestKey = key
			oldestSeen = entry.lastSeen
			first = false
		}
	}
	if !first {
		delete(t.entries, oldestKey)
	}
}

func newAttemptLimiter(limit int, window time.Duration, now time.Time) *rateLimiter {
	if limit <= 0 || window <= 0 {
		return nil
	}
	return &rateLimiter{
		rate:     float64(limit) / window.Seconds(),
		capacity: float64(limit),
		tokens:   float64(limit),
		last:     now,
	}
}
