// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"sync"
	"time"
)

type rateLimiter struct {
	mu       sync.Mutex
	rate     float64
	capacity float64
	tokens   float64
	last     time.Time
}

func newRateLimiter(requestsPerSecond, burst int, now time.Time) *rateLimiter {
	if requestsPerSecond <= 0 || burst <= 0 {
		return nil
	}
	return &rateLimiter{
		rate:     float64(requestsPerSecond),
		capacity: float64(burst),
		tokens:   float64(burst),
		last:     now,
	}
}

func (l *rateLimiter) allow(now time.Time) bool {
	if l == nil {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if now.After(l.last) {
		l.tokens += now.Sub(l.last).Seconds() * l.rate
		if l.tokens > l.capacity {
			l.tokens = l.capacity
		}
		l.last = now
	}

	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}

// peek reports whether allow(now) would currently succeed, without
// consuming a token or mutating limiter state.
func (l *rateLimiter) peek(now time.Time) bool {
	if l == nil {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	tokens := l.tokens
	if now.After(l.last) {
		tokens += now.Sub(l.last).Seconds() * l.rate
		if tokens > l.capacity {
			tokens = l.capacity
		}
	}
	return tokens >= 1
}
