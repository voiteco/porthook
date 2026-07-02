// SPDX-License-Identifier: AGPL-3.0-only

package controlplane

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type AuditEvent struct {
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
	Events []AuditEvent `json:"events"`
}

type AuditEventStore interface {
	Ping(context.Context) error
	Add(context.Context, AuditEvent) error
	List(context.Context, int) ([]AuditEvent, error)
}

type MemoryAuditEventStore struct {
	mu      sync.RWMutex
	entries []AuditEvent
	next    int
	full    bool
}

func NewMemoryAuditEventStore(limit int) *MemoryAuditEventStore {
	if limit <= 0 {
		return &MemoryAuditEventStore{}
	}
	return &MemoryAuditEventStore{entries: make([]AuditEvent, limit)}
}

func (s *MemoryAuditEventStore) Ping(context.Context) error {
	return nil
}

func (s *MemoryAuditEventStore) Add(_ context.Context, entry AuditEvent) error {
	if s == nil || len(s.entries) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries[s.next] = entry
	s.next = (s.next + 1) % len(s.entries)
	if s.next == 0 {
		s.full = true
	}
	return nil
}

func (s *MemoryAuditEventStore) List(_ context.Context, limit int) ([]AuditEvent, error) {
	if s == nil || len(s.entries) == 0 {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := s.next
	if s.full {
		count = len(s.entries)
	}
	if limit <= 0 || limit > count {
		limit = count
	}
	out := make([]AuditEvent, 0, limit)
	for i := 0; i < limit; i++ {
		index := s.next - 1 - i
		if index < 0 {
			index += len(s.entries)
		}
		if !s.full && index >= s.next {
			break
		}
		out = append(out, s.entries[index])
	}
	return out, nil
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
