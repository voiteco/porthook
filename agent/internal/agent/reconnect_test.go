// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"errors"
	"testing"
	"time"
)

func TestPermanentErrorClassification(t *testing.T) {
	base := errors.New("terminal")
	err := permanent(base)

	if !isPermanent(err) {
		t.Fatal("isPermanent returned false")
	}
	if !errors.Is(err, base) {
		t.Fatal("permanent error does not unwrap base error")
	}
	if isPermanent(errors.New("transient")) {
		t.Fatal("isPermanent returned true for transient error")
	}
}

func TestReconnectDelayExponentialWithJitter(t *testing.T) {
	jitter := func(limit time.Duration) time.Duration {
		if limit != 250*time.Millisecond {
			t.Fatalf("jitter limit = %s, want 250ms", limit)
		}
		return 100 * time.Millisecond
	}

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: 600 * time.Millisecond},
		{attempt: 1, want: 1100 * time.Millisecond},
		{attempt: 2, want: 2100 * time.Millisecond},
		{attempt: 3, want: 4100 * time.Millisecond},
		{attempt: 4, want: 5 * time.Second},
	}

	for _, tt := range tests {
		got := reconnectDelay(tt.attempt, 500*time.Millisecond, 5*time.Second, 250*time.Millisecond, jitter)
		if got != tt.want {
			t.Fatalf("attempt %d delay = %s, want %s", tt.attempt, got, tt.want)
		}
	}
}

func TestReconnectDelayNormalizesInvalidInputs(t *testing.T) {
	got := reconnectDelay(-1, 0, 0, 0, nil)
	if got != defaultReconnectInitialDelay {
		t.Fatalf("delay = %s, want %s", got, defaultReconnectInitialDelay)
	}
}
