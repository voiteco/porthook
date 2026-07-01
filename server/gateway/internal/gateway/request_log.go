// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

type requestLogEntry struct {
	Time          time.Time `json:"time"`
	Method        string    `json:"method"`
	Host          string    `json:"host"`
	Path          string    `json:"path"`
	QueryPresent  bool      `json:"query_present"`
	RemoteIP      string    `json:"remote_ip"`
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
	RequestLogs []requestLogEntry `json:"request_logs"`
}

type requestLogBuffer struct {
	mu      sync.RWMutex
	entries []requestLogEntry
	next    int
	full    bool
}

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

	b.entries[b.next] = entry
	b.next = (b.next + 1) % len(b.entries)
	if b.next == 0 {
		b.full = true
	}
}

func (b *requestLogBuffer) list(limit int) []requestLogEntry {
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
	out := make([]requestLogEntry, 0, limit)
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

func requestLogEntryFromPublicRequest(r *http.Request, entry publicRequestLog) requestLogEntry {
	out := requestLogEntry{
		Time:          entry.Started,
		Method:        r.Method,
		Host:          r.Host,
		Path:          r.URL.Path,
		QueryPresent:  r.URL.RawQuery != "",
		RemoteIP:      remoteIP(r.RemoteAddr),
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
		return fallback
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return fallback
	}
	return limit
}
