// SPDX-License-Identifier: AGPL-3.0-only

package controlplane

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

type auditEvent struct {
	Time      time.Time         `json:"time"`
	Level     string            `json:"level"`
	Message   string            `json:"message"`
	Event     string            `json:"event"`
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	RemoteIP  string            `json:"remote_ip"`
	RequestID string            `json:"request_id,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
}

type auditEventsResponse struct {
	Events []auditEvent `json:"events"`
}

type auditEventBuffer struct {
	mu      sync.RWMutex
	entries []auditEvent
	next    int
	full    bool
}

func newAuditEventBuffer(limit int) *auditEventBuffer {
	if limit <= 0 {
		return &auditEventBuffer{}
	}
	return &auditEventBuffer{entries: make([]auditEvent, limit)}
}

func (b *auditEventBuffer) add(entry auditEvent) {
	if b == nil || len(b.entries) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	b.entries[b.next] = entry
	b.next = (b.next + 1) % len(b.entries)
	if b.next == 0 {
		b.full = true
	}
}

func (b *auditEventBuffer) list(limit int) []auditEvent {
	if b == nil || len(b.entries) == 0 {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()

	count := b.next
	if b.full {
		count = len(b.entries)
	}
	if limit <= 0 || limit > count {
		limit = count
	}
	out := make([]auditEvent, 0, limit)
	for i := 0; i < limit; i++ {
		index := b.next - 1 - i
		if index < 0 {
			index += len(b.entries)
		}
		if !b.full && index >= b.next {
			break
		}
		out = append(out, b.entries[index])
	}
	return out
}

func auditEventLimit(r *http.Request, fallback int) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return fallback
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return fallback
	}
	return limit
}
