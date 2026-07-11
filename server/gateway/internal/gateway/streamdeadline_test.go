// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStreamDeadlineFiresRequestTimeoutBeforeResponseStarts(t *testing.T) {
	d := newStreamDeadline(context.Background(), streamDeadlinePolicy{
		RequestTimeout: 20 * time.Millisecond,
		IdleTimeout:    time.Hour,
		MaxLifetime:    time.Hour,
	})
	defer d.Stop()

	select {
	case <-d.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("context was not cancelled after the request timeout elapsed")
	}
	if cause := context.Cause(d.Context()); !errors.Is(cause, ErrStreamRequestTimeout) {
		t.Fatalf("cause = %v, want ErrStreamRequestTimeout", cause)
	}
}

func TestStreamDeadlineMarkResponseStartedStopsRequestTimer(t *testing.T) {
	d := newStreamDeadline(context.Background(), streamDeadlinePolicy{
		RequestTimeout: 20 * time.Millisecond,
		IdleTimeout:    0,
		MaxLifetime:    time.Hour,
	})
	defer d.Stop()

	d.MarkResponseStarted()

	select {
	case <-d.Context().Done():
		t.Fatal("context was cancelled after MarkResponseStarted despite the request timeout having elapsed")
	case <-time.After(60 * time.Millisecond):
	}
}

func TestStreamDeadlineIdleTimeoutFiresWithoutActivity(t *testing.T) {
	d := newStreamDeadline(context.Background(), streamDeadlinePolicy{
		RequestTimeout: time.Hour,
		IdleTimeout:    20 * time.Millisecond,
		MaxLifetime:    time.Hour,
	})
	defer d.Stop()

	d.MarkResponseStarted()

	select {
	case <-d.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("context was not cancelled after the idle timeout elapsed")
	}
	if cause := context.Cause(d.Context()); !errors.Is(cause, ErrStreamIdleTimeout) {
		t.Fatalf("cause = %v, want ErrStreamIdleTimeout", cause)
	}
}

func TestStreamDeadlineTouchResetsIdleTimeout(t *testing.T) {
	d := newStreamDeadline(context.Background(), streamDeadlinePolicy{
		RequestTimeout: time.Hour,
		IdleTimeout:    40 * time.Millisecond,
		MaxLifetime:    time.Hour,
	})
	defer d.Stop()

	d.MarkResponseStarted()

	// Touch faster than the idle timeout for several rounds; the deadline
	// must survive well past what a single idle window would allow.
	for i := 0; i < 5; i++ {
		time.Sleep(20 * time.Millisecond)
		d.Touch()
		if d.Context().Err() != nil {
			t.Fatalf("context was cancelled despite repeated activity (round %d)", i)
		}
	}
}

func TestStreamDeadlineMaxLifetimeFiresDespiteActivity(t *testing.T) {
	d := newStreamDeadline(context.Background(), streamDeadlinePolicy{
		RequestTimeout: time.Hour,
		IdleTimeout:    10 * time.Millisecond,
		MaxLifetime:    60 * time.Millisecond,
	})
	defer d.Stop()

	d.MarkResponseStarted()

	stop := time.After(200 * time.Millisecond)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
loop:
	for {
		select {
		case <-d.Context().Done():
			break loop
		case <-ticker.C:
			d.Touch()
		case <-stop:
			t.Fatal("max lifetime did not cancel the context despite continuous activity")
		}
	}

	if cause := context.Cause(d.Context()); !errors.Is(cause, ErrStreamMaxLifetimeExceeded) {
		t.Fatalf("cause = %v, want ErrStreamMaxLifetimeExceeded", cause)
	}
}

func TestStreamDeadlineStopIsIdempotentAndReleasesTimers(t *testing.T) {
	d := newStreamDeadline(context.Background(), streamDeadlinePolicy{
		RequestTimeout: time.Hour,
		IdleTimeout:    time.Hour,
		MaxLifetime:    time.Hour,
	})
	d.MarkResponseStarted()
	d.Stop()
	d.Stop() // must not panic or double-close

	if d.Context().Err() == nil {
		t.Fatal("context was not cancelled after Stop")
	}
	// Touch after Stop must be a safe no-op.
	d.Touch()
}

func TestStreamDeadlineRespectsParentCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	d := newStreamDeadline(parent, streamDeadlinePolicy{
		RequestTimeout: time.Hour,
		IdleTimeout:    time.Hour,
		MaxLifetime:    time.Hour,
	})
	defer d.Stop()

	cancel()

	select {
	case <-d.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("child context was not cancelled when the parent was cancelled")
	}
	if cause := context.Cause(d.Context()); !errors.Is(cause, context.Canceled) {
		t.Fatalf("cause = %v, want context.Canceled", cause)
	}
}
