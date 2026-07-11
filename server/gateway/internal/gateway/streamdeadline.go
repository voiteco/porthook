// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	// ErrStreamRequestTimeout means the local service did not send a first
	// response byte within the configured request timeout.
	ErrStreamRequestTimeout = errors.New("gateway timed out waiting for the first response byte")
	// ErrStreamIdleTimeout means an active stream produced no activity
	// (request or response bytes) within the configured idle timeout.
	ErrStreamIdleTimeout = errors.New("gateway stream idle timeout exceeded")
	// ErrStreamMaxLifetimeExceeded means a stream ran longer than the
	// configured absolute maximum lifetime, regardless of activity.
	ErrStreamMaxLifetimeExceeded = errors.New("gateway stream exceeded its maximum lifetime")
)

// streamDeadlinePolicy configures the three timers a streamDeadline
// enforces. It replaces a single absolute deadline so long-lived but active
// streams (SSE, long polling, WebSocket tunnels) are not killed on a fixed
// schedule, while idle or runaway streams are still bounded.
type streamDeadlinePolicy struct {
	// RequestTimeout bounds the time from stream start until the first
	// response activity (MarkResponseStarted). Zero disables this phase.
	RequestTimeout time.Duration
	// IdleTimeout bounds the gap between successive Touch calls once the
	// stream is active. Zero disables idle detection.
	IdleTimeout time.Duration
	// MaxLifetime is an absolute cap on total stream duration from start,
	// independent of activity. It is always enforced.
	MaxLifetime time.Duration
}

// streamDeadline derives a cancellable context from a parent context and
// cancels it with a specific cause when the request, idle, or max-lifetime
// timer fires. Callers call MarkResponseStarted once response activity
// begins and Touch on every subsequent unit of activity.
type streamDeadline struct {
	ctx    context.Context
	cancel context.CancelCauseFunc

	mu      sync.Mutex
	policy  streamDeadlinePolicy
	phase   *time.Timer
	max     *time.Timer
	stopped bool
}

func newStreamDeadline(parent context.Context, policy streamDeadlinePolicy) *streamDeadline {
	ctx, cancel := context.WithCancelCause(parent)
	d := &streamDeadline{
		ctx:    ctx,
		cancel: cancel,
		policy: policy,
	}
	if policy.MaxLifetime > 0 {
		d.max = time.AfterFunc(policy.MaxLifetime, func() {
			cancel(ErrStreamMaxLifetimeExceeded)
		})
	}
	if policy.RequestTimeout > 0 {
		d.phase = time.AfterFunc(policy.RequestTimeout, func() {
			cancel(ErrStreamRequestTimeout)
		})
	}
	return d
}

func (d *streamDeadline) Context() context.Context {
	return d.ctx
}

// MarkResponseStarted stops the request-timeout timer and, if configured,
// starts the idle timer. Call it once, when the first response byte (or
// equivalent activity) occurs.
func (d *streamDeadline) MarkResponseStarted() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return
	}
	if d.phase != nil {
		d.phase.Stop()
		d.phase = nil
	}
	if d.policy.IdleTimeout > 0 {
		d.phase = time.AfterFunc(d.policy.IdleTimeout, func() {
			d.cancel(ErrStreamIdleTimeout)
		})
	}
}

// Touch resets the idle timer. It is a no-op before MarkResponseStarted or
// once the deadline has been stopped, and safe to call concurrently.
func (d *streamDeadline) Touch() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped || d.phase == nil {
		return
	}
	d.phase.Reset(d.policy.IdleTimeout)
}

// Stop releases the deadline's timers and cancels its context if it has not
// already been cancelled. Callers must defer Stop to avoid leaking timers.
func (d *streamDeadline) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return
	}
	d.stopped = true
	if d.phase != nil {
		d.phase.Stop()
	}
	if d.max != nil {
		d.max.Stop()
	}
	d.cancel(nil)
}
