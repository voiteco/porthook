// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"fmt"
	"testing"
	"time"
)

func TestAuthAttemptThrottleAllowsUntouchedKeys(t *testing.T) {
	throttle := newAuthAttemptThrottle(3, time.Minute)
	now := time.Now()

	if throttle.blocked("demo|203.0.113.1", now) {
		t.Fatal("blocked = true for a key that never failed, want false")
	}
}

func TestAuthAttemptThrottleBlocksAfterLimit(t *testing.T) {
	throttle := newAuthAttemptThrottle(3, time.Minute)
	now := time.Now()
	key := "demo|203.0.113.1"

	for i := 0; i < 3; i++ {
		if throttle.blocked(key, now) {
			t.Fatalf("blocked = true before exhausting the limit (attempt %d)", i)
		}
		throttle.recordFailure(key, now)
	}

	if !throttle.blocked(key, now) {
		t.Fatal("blocked = false after exhausting the attempt limit, want true")
	}
}

func TestAuthAttemptThrottleRefillsOverTime(t *testing.T) {
	throttle := newAuthAttemptThrottle(1, time.Minute)
	now := time.Now()
	key := "demo|203.0.113.1"

	throttle.recordFailure(key, now)
	if !throttle.blocked(key, now) {
		t.Fatal("blocked = false immediately after exhausting a single-attempt budget")
	}

	later := now.Add(time.Minute + time.Second)
	if throttle.blocked(key, later) {
		t.Fatal("blocked = true after the window elapsed, want refilled budget")
	}
}

func TestAuthAttemptThrottleKeysAreIndependent(t *testing.T) {
	throttle := newAuthAttemptThrottle(1, time.Minute)
	now := time.Now()

	throttle.recordFailure("demo|203.0.113.1", now)
	if throttle.blocked("demo|203.0.113.2", now) {
		t.Fatal("blocked = true for an unrelated client address")
	}
	if throttle.blocked("other|203.0.113.1", now) {
		t.Fatal("blocked = true for an unrelated subdomain")
	}
}

func TestAuthAttemptThrottlePeekDoesNotConsumeBudget(t *testing.T) {
	throttle := newAuthAttemptThrottle(2, time.Minute)
	now := time.Now()
	key := "demo|203.0.113.1"

	throttle.recordFailure(key, now)
	for i := 0; i < 5; i++ {
		if throttle.blocked(key, now) {
			t.Fatalf("blocked = true on peek %d before exhausting the limit", i)
		}
	}
	throttle.recordFailure(key, now)
	if !throttle.blocked(key, now) {
		t.Fatal("blocked = false after two failures against a two-attempt limit")
	}
}

func TestAuthAttemptThrottleEvictsOldestBeyondMaxKeys(t *testing.T) {
	throttle := newAuthAttemptThrottle(1, time.Minute)
	base := time.Now()

	for i := 0; i < authAttemptMaxKeys; i++ {
		throttle.recordFailure(keyForIndex(i), base.Add(time.Duration(i)*time.Millisecond))
	}
	if len(throttle.entries) != authAttemptMaxKeys {
		t.Fatalf("entries = %d, want %d before eviction", len(throttle.entries), authAttemptMaxKeys)
	}

	newest := base.Add(time.Duration(authAttemptMaxKeys) * time.Millisecond)
	throttle.recordFailure("demo|overflow", newest)

	if len(throttle.entries) != authAttemptMaxKeys {
		t.Fatalf("entries = %d, want bounded at %d after eviction", len(throttle.entries), authAttemptMaxKeys)
	}
	if _, ok := throttle.entries[keyForIndex(0)]; ok {
		t.Fatal("oldest entry was not evicted after exceeding the bound")
	}
	if _, ok := throttle.entries["demo|overflow"]; !ok {
		t.Fatal("newest entry is missing after eviction")
	}
}

func keyForIndex(i int) string {
	return fmt.Sprintf("demo|k%d", i)
}

func TestNilAuthAttemptThrottleAllowsEverything(t *testing.T) {
	var throttle *authAttemptThrottle
	now := time.Now()

	if throttle.blocked("demo|203.0.113.1", now) {
		t.Fatal("nil throttle reported blocked, want always allowed")
	}
	throttle.recordFailure("demo|203.0.113.1", now)
}
