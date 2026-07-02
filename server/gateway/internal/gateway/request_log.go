// SPDX-License-Identifier: AGPL-3.0-only

package gateway

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

type requestLogEntry struct {
	ID            int64     `json:"-"`
	Time          time.Time `json:"time"`
	Method        string    `json:"method"`
	Host          string    `json:"host"`
	Path          string    `json:"path"`
	QueryPresent  bool      `json:"query_present"`
	RemoteIP      string    `json:"remote_ip"`
	RequestID     string    `json:"request_id,omitempty"`
	Subdomain     string    `json:"subdomain,omitempty"`
	CustomDomain  string    `json:"custom_domain,omitempty"`
	TunnelID      string    `json:"tunnel_id,omitempty"`
	StreamID      string    `json:"stream_id,omitempty"`
	Status        int       `json:"status"`
	Outcome       string    `json:"outcome"`
	RequestBytes  int64     `json:"request_bytes"`
	ResponseBytes int64     `json:"response_bytes"`
	DurationMS    int64     `json:"duration_ms"`
	Error         string    `json:"error,omitempty"`
}

type requestLogsResponse struct {
	RequestLogs []requestLogEntry         `json:"request_logs"`
	NextCursor  string                    `json:"next_cursor,omitempty"`
	Filters     requestLogFiltersResponse `json:"filters"`
}

type requestLogWriter interface {
	Ping(context.Context) error
	Add(context.Context, requestLogEntry) error
}

type requestLogReader interface {
	List(context.Context, requestLogListOptions) (requestLogListPage, error)
}

type requestLogFilter struct {
	Subdomain string
	Status    int
	Outcome   string
	RequestID string
	Host      string
	Method    string
	Path      string
	TunnelID  string
	Since     time.Time
	Until     time.Time
}

type requestLogListOptions struct {
	Limit  int
	Filter requestLogFilter
	Cursor requestLogCursor
}

type requestLogListPage struct {
	RequestLogs []requestLogEntry
	NextCursor  string
}

type requestLogFiltersResponse struct {
	Limit     int    `json:"limit"`
	Subdomain string `json:"subdomain,omitempty"`
	Method    string `json:"method,omitempty"`
	Host      string `json:"host,omitempty"`
	Path      string `json:"path,omitempty"`
	Status    int    `json:"status,omitempty"`
	Outcome   string `json:"outcome,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	TunnelID  string `json:"tunnel_id,omitempty"`
	Since     string `json:"since,omitempty"`
	Until     string `json:"until,omitempty"`
	Cursor    string `json:"cursor,omitempty"`
}

type requestLogCursor struct {
	Time time.Time `json:"time"`
	ID   int64     `json:"id"`
}

type requestLogBuffer struct {
	mu      sync.RWMutex
	entries []requestLogEntry
	next    int
	full    bool
	nextID  int64
}

const maxRequestLogLimit = 1000

func newRequestLogBuffer(limit int) *requestLogBuffer {
	if limit <= 0 {
		return &requestLogBuffer{}
	}
	return &requestLogBuffer{entries: make([]requestLogEntry, limit)}
}

func (b *requestLogBuffer) add(entry requestLogEntry) {
	if b == nil || len(b.entries) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if entry.ID == 0 {
		b.nextID++
		entry.ID = b.nextID
	} else if entry.ID > b.nextID {
		b.nextID = entry.ID
	}
	b.entries[b.next] = entry
	b.next = (b.next + 1) % len(b.entries)
	if b.next == 0 {
		b.full = true
	}
}

func (b *requestLogBuffer) list(limit int) []requestLogEntry {
	return b.listPage(requestLogListOptions{Limit: limit}).RequestLogs
}

func (b *requestLogBuffer) count() int {
	if b == nil || len(b.entries) == 0 {
		return 0
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.full {
		return len(b.entries)
	}
	return b.next
}

func (b *requestLogBuffer) capacity() int {
	if b == nil {
		return 0
	}
	return len(b.entries)
}

func (b *requestLogBuffer) listFiltered(limit int, filter requestLogFilter) []requestLogEntry {
	return b.listPage(requestLogListOptions{Limit: limit, Filter: filter}).RequestLogs
}

func (b *requestLogBuffer) listPage(opts requestLogListOptions) requestLogListPage {
	if b == nil || len(b.entries) == 0 {
		return requestLogListPage{}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()

	count := b.next
	if b.full {
		count = len(b.entries)
	}
	limit := normalizedRequestLogLimit(opts.Limit)
	if limit <= 0 {
		return requestLogListPage{}
	}
	out := make([]requestLogEntry, 0, min(limit+1, count))
	for i := 0; i < count; i++ {
		index := b.next - 1 - i
		if index < 0 {
			index += len(b.entries)
		}
		if !b.full && index >= b.next {
			break
		}
		entry := b.entries[index]
		if opts.Cursor.ID > 0 && entry.ID >= opts.Cursor.ID {
			continue
		}
		if opts.Filter.matches(entry) {
			out = append(out, entry)
			if len(out) > limit {
				break
			}
		}
	}
	return requestLogPage(out, limit)
}

func requestLogEntryFromPublicRequest(r *http.Request, entry publicRequestLog) requestLogEntry {
	out := requestLogEntry{
		Time:          entry.Started,
		Method:        r.Method,
		Host:          r.Host,
		Path:          r.URL.Path,
		QueryPresent:  r.URL.RawQuery != "",
		RemoteIP:      remoteIP(r.RemoteAddr),
		RequestID:     requestID(r),
		Subdomain:     entry.Subdomain,
		CustomDomain:  entry.CustomDomain,
		TunnelID:      entry.TunnelID,
		StreamID:      entry.StreamID,
		Status:        entry.Status,
		Outcome:       entry.Outcome,
		RequestBytes:  entry.RequestBytes,
		ResponseBytes: entry.ResponseBytes,
		DurationMS:    time.Since(entry.Started).Milliseconds(),
	}
	if entry.Error != nil {
		out.Error = entry.Error.Error()
	}
	return out
}

func requestLogLimit(r *http.Request, fallback int) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return normalizedRequestLogLimit(fallback)
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return normalizedRequestLogLimit(fallback)
	}
	return normalizedRequestLogLimit(limit)
}

func normalizedRequestLogLimit(limit int) int {
	if limit < 0 {
		return defaultRequestLogLimit
	}
	if limit > maxRequestLogLimit {
		return maxRequestLogLimit
	}
	return limit
}

func requestLogListOptionsFromRequest(r *http.Request, fallback int) (requestLogListOptions, error) {
	filter, err := requestLogFilterFromRequest(r)
	if err != nil {
		return requestLogListOptions{}, err
	}
	opts := requestLogListOptions{
		Limit:  requestLogLimit(r, fallback),
		Filter: filter,
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("cursor")); raw != "" {
		cursor, err := decodeRequestLogCursor(raw)
		if err != nil {
			return requestLogListOptions{}, err
		}
		opts.Cursor = cursor
	}
	return opts, nil
}

func requestLogFilterFromRequest(r *http.Request) (requestLogFilter, error) {
	query := r.URL.Query()
	filter := requestLogFilter{
		Subdomain: strings.ToLower(strings.TrimSpace(query.Get("subdomain"))),
		Outcome:   strings.ToLower(strings.TrimSpace(query.Get("outcome"))),
		RequestID: strings.ToLower(strings.TrimSpace(query.Get("request_id"))),
		Host:      strings.ToLower(strings.TrimSpace(query.Get("host"))),
		Method:    strings.ToUpper(strings.TrimSpace(query.Get("method"))),
		Path:      strings.ToLower(strings.TrimSpace(query.Get("path"))),
		TunnelID:  strings.ToLower(strings.TrimSpace(query.Get("tunnel_id"))),
	}

	rawStatus := strings.TrimSpace(query.Get("status"))
	if rawStatus != "" {
		status, err := strconv.Atoi(rawStatus)
		if err != nil || status < 100 || status > 599 {
			return requestLogFilter{}, fmt.Errorf("status must be an HTTP status code between 100 and 599")
		}
		filter.Status = status
	}

	since, err := requestLogTimeFilter(query.Get("since"), "since")
	if err != nil {
		return requestLogFilter{}, err
	}
	filter.Since = since

	until, err := requestLogTimeFilter(query.Get("until"), "until")
	if err != nil {
		return requestLogFilter{}, err
	}
	filter.Until = until
	if !filter.Since.IsZero() && !filter.Until.IsZero() && filter.Since.After(filter.Until) {
		return requestLogFilter{}, fmt.Errorf("since must be before until")
	}

	return filter, nil
}

func requestLogTimeFilter(raw, name string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be an RFC3339 timestamp", name)
	}
	return value, nil
}

func (f requestLogFilter) matches(entry requestLogEntry) bool {
	if f.Subdomain != "" && strings.ToLower(entry.Subdomain) != f.Subdomain {
		return false
	}
	if f.Status != 0 && entry.Status != f.Status {
		return false
	}
	if f.Outcome != "" && !strings.Contains(strings.ToLower(entry.Outcome), f.Outcome) {
		return false
	}
	if f.RequestID != "" && !strings.Contains(strings.ToLower(entry.RequestID), f.RequestID) {
		return false
	}
	if f.Host != "" && !strings.Contains(strings.ToLower(entry.Host), f.Host) {
		return false
	}
	if f.Method != "" && strings.ToUpper(entry.Method) != f.Method {
		return false
	}
	if f.Path != "" && !strings.Contains(strings.ToLower(entry.Path), f.Path) {
		return false
	}
	if f.TunnelID != "" && !strings.Contains(strings.ToLower(entry.TunnelID), f.TunnelID) {
		return false
	}
	if !f.Since.IsZero() && entry.Time.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && entry.Time.After(f.Until) {
		return false
	}
	return true
}

func requestLogPage(entries []requestLogEntry, limit int) requestLogListPage {
	limit = normalizedRequestLogLimit(limit)
	if limit <= 0 || len(entries) == 0 {
		return requestLogListPage{}
	}
	if len(entries) <= limit {
		return requestLogListPage{RequestLogs: entries}
	}
	page := requestLogListPage{RequestLogs: entries[:limit]}
	page.NextCursor = encodeRequestLogCursor(page.RequestLogs[len(page.RequestLogs)-1])
	return page
}

func encodeRequestLogCursor(entry requestLogEntry) string {
	if entry.ID <= 0 || entry.Time.IsZero() {
		return ""
	}
	data, err := json.Marshal(requestLogCursor{Time: entry.Time, ID: entry.ID})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeRequestLogCursor(raw string) (requestLogCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return requestLogCursor{}, errors.New("cursor is invalid")
	}
	var cursor requestLogCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return requestLogCursor{}, errors.New("cursor is invalid")
	}
	if cursor.ID <= 0 || cursor.Time.IsZero() {
		return requestLogCursor{}, errors.New("cursor is invalid")
	}
	return cursor, nil
}

func requestLogFiltersForResponse(opts requestLogListOptions, cursor string) requestLogFiltersResponse {
	filters := requestLogFiltersResponse{
		Limit:     normalizedRequestLogLimit(opts.Limit),
		Subdomain: opts.Filter.Subdomain,
		Method:    opts.Filter.Method,
		Host:      opts.Filter.Host,
		Path:      opts.Filter.Path,
		Status:    opts.Filter.Status,
		Outcome:   opts.Filter.Outcome,
		RequestID: opts.Filter.RequestID,
		TunnelID:  opts.Filter.TunnelID,
		Cursor:    cursor,
	}
	if !opts.Filter.Since.IsZero() {
		filters.Since = opts.Filter.Since.Format(time.RFC3339)
	}
	if !opts.Filter.Until.IsZero() {
		filters.Until = opts.Filter.Until.Format(time.RFC3339)
	}
	return filters
}
