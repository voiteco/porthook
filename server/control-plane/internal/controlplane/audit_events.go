// SPDX-License-Identifier: AGPL-3.0-only

package controlplane

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type AuditEvent struct {
	ID        int64             `json:"-"`
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
	Events     []AuditEvent              `json:"events"`
	NextCursor string                    `json:"next_cursor,omitempty"`
	Filters    auditEventFiltersResponse `json:"filters"`
}

type AuditEventStore interface {
	Ping(context.Context) error
	Add(context.Context, AuditEvent) error
	List(context.Context, AuditEventListOptions) (AuditEventListPage, error)
}

type MemoryAuditEventStore struct {
	mu      sync.RWMutex
	entries []AuditEvent
	next    int
	full    bool
	nextID  int64
}

type AuditEventListOptions struct {
	Limit     int
	Event     string
	Level     string
	RequestID string
	RemoteIP  string
	Field     string
	Since     time.Time
	Until     time.Time
	Cursor    auditEventCursor
}

type AuditEventListPage struct {
	Events     []AuditEvent
	NextCursor string
}

type auditEventFiltersResponse struct {
	Limit     int    `json:"limit"`
	Event     string `json:"event,omitempty"`
	Level     string `json:"level,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	RemoteIP  string `json:"remote_ip,omitempty"`
	Field     string `json:"field,omitempty"`
	Since     string `json:"since,omitempty"`
	Until     string `json:"until,omitempty"`
	Cursor    string `json:"cursor,omitempty"`
}

type auditEventCursor struct {
	Time time.Time `json:"time"`
	ID   int64     `json:"id"`
}

const maxAuditEventLimit = 1000

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

	if entry.ID == 0 {
		s.nextID++
		entry.ID = s.nextID
	} else if entry.ID > s.nextID {
		s.nextID = entry.ID
	}
	s.entries[s.next] = entry
	s.next = (s.next + 1) % len(s.entries)
	if s.next == 0 {
		s.full = true
	}
	return nil
}

func (s *MemoryAuditEventStore) List(_ context.Context, opts AuditEventListOptions) (AuditEventListPage, error) {
	if s == nil || len(s.entries) == 0 {
		return AuditEventListPage{}, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := normalizedAuditEventLimit(opts.Limit)
	count := s.next
	if s.full {
		count = len(s.entries)
	}
	out := make([]AuditEvent, 0, min(limit+1, count))
	for i := 0; i < count; i++ {
		index := s.next - 1 - i
		if index < 0 {
			index += len(s.entries)
		}
		if !s.full && index >= s.next {
			break
		}
		entry := s.entries[index]
		if !auditEventMatches(entry, opts) {
			continue
		}
		out = append(out, entry)
		if len(out) > limit {
			break
		}
	}
	return auditEventPage(out, limit), nil
}

func auditEventListOptionsFromRequest(r *http.Request, fallback int) (AuditEventListOptions, error) {
	query := r.URL.Query()
	limit := auditEventLimit(r, fallback)
	opts := AuditEventListOptions{
		Limit:     limit,
		Event:     strings.TrimSpace(query.Get("event")),
		Level:     strings.ToUpper(strings.TrimSpace(query.Get("level"))),
		RequestID: strings.TrimSpace(query.Get("request_id")),
		RemoteIP:  strings.TrimSpace(query.Get("remote_ip")),
		Field:     strings.TrimSpace(query.Get("field")),
	}
	if raw := strings.TrimSpace(query.Get("since")); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return AuditEventListOptions{}, fmt.Errorf("since must be an RFC3339 timestamp")
		}
		opts.Since = value
	}
	if raw := strings.TrimSpace(query.Get("until")); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return AuditEventListOptions{}, fmt.Errorf("until must be an RFC3339 timestamp")
		}
		opts.Until = value
	}
	if !opts.Since.IsZero() && !opts.Until.IsZero() && opts.Since.After(opts.Until) {
		return AuditEventListOptions{}, fmt.Errorf("since must be before until")
	}
	if raw := strings.TrimSpace(query.Get("cursor")); raw != "" {
		cursor, err := decodeAuditEventCursor(raw)
		if err != nil {
			return AuditEventListOptions{}, err
		}
		opts.Cursor = cursor
	}
	return opts, nil
}

func auditEventLimit(r *http.Request, fallback int) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return normalizedAuditEventLimit(fallback)
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return normalizedAuditEventLimit(fallback)
	}
	return normalizedAuditEventLimit(limit)
}

func normalizedAuditEventLimit(limit int) int {
	if limit <= 0 {
		return defaultAuditEventLimit
	}
	if limit > maxAuditEventLimit {
		return maxAuditEventLimit
	}
	return limit
}

func auditEventMatches(event AuditEvent, opts AuditEventListOptions) bool {
	if opts.Cursor.ID > 0 && event.ID >= opts.Cursor.ID {
		return false
	}
	if !opts.Since.IsZero() && event.Time.Before(opts.Since) {
		return false
	}
	if !opts.Until.IsZero() && event.Time.After(opts.Until) {
		return false
	}
	if opts.Event != "" && !strings.Contains(strings.ToLower(event.Event), strings.ToLower(opts.Event)) {
		return false
	}
	if opts.Level != "" && strings.ToUpper(event.Level) != opts.Level {
		return false
	}
	if opts.RequestID != "" && !strings.Contains(strings.ToLower(event.RequestID), strings.ToLower(opts.RequestID)) {
		return false
	}
	if opts.RemoteIP != "" && !strings.Contains(strings.ToLower(event.RemoteIP), strings.ToLower(opts.RemoteIP)) {
		return false
	}
	if opts.Field != "" && !strings.Contains(strings.ToLower(auditEventFieldsText(event.Fields)), strings.ToLower(opts.Field)) {
		return false
	}
	return true
}

func auditEventFieldsText(fields map[string]string) string {
	if len(fields) == 0 {
		return ""
	}
	parts := make([]string, 0, len(fields))
	for key, value := range fields {
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, " ")
}

func auditEventPage(events []AuditEvent, limit int) AuditEventListPage {
	limit = normalizedAuditEventLimit(limit)
	if len(events) <= limit {
		return AuditEventListPage{Events: events}
	}
	page := AuditEventListPage{
		Events: events[:limit],
	}
	page.NextCursor = encodeAuditEventCursor(page.Events[len(page.Events)-1])
	return page
}

func encodeAuditEventCursor(event AuditEvent) string {
	if event.ID <= 0 || event.Time.IsZero() {
		return ""
	}
	data, err := json.Marshal(auditEventCursor{Time: event.Time, ID: event.ID})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeAuditEventCursor(raw string) (auditEventCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return auditEventCursor{}, errors.New("cursor is invalid")
	}
	var cursor auditEventCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return auditEventCursor{}, errors.New("cursor is invalid")
	}
	if cursor.ID <= 0 || cursor.Time.IsZero() {
		return auditEventCursor{}, errors.New("cursor is invalid")
	}
	return cursor, nil
}

func auditEventFiltersForResponse(opts AuditEventListOptions, cursor string) auditEventFiltersResponse {
	filters := auditEventFiltersResponse{
		Limit:     normalizedAuditEventLimit(opts.Limit),
		Event:     opts.Event,
		Level:     opts.Level,
		RequestID: opts.RequestID,
		RemoteIP:  opts.RemoteIP,
		Field:     opts.Field,
		Cursor:    cursor,
	}
	if !opts.Since.IsZero() {
		filters.Since = opts.Since.Format(time.RFC3339)
	}
	if !opts.Until.IsZero() {
		filters.Until = opts.Until.Format(time.RFC3339)
	}
	return filters
}
