// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestRequestLogBufferPrunesOldEntries(t *testing.T) {
	buffer := newRequestLogBuffer(5)
	base := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	for _, entry := range []requestLogEntry{
		{Time: base.Add(-2 * time.Hour), Method: http.MethodGet, Path: "/old", RequestID: "req_old"},
		{Time: base.Add(-30 * time.Minute), Method: http.MethodGet, Path: "/recent", RequestID: "req_recent"},
		{Time: base, Method: http.MethodGet, Path: "/new", RequestID: "req_new"},
	} {
		buffer.add(entry)
	}

	pruned, err := buffer.PruneBefore(context.Background(), base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("PruneBefore returned error: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("pruned = %d, want 1", pruned)
	}
	page := buffer.listPage(requestLogListOptions{Limit: 10})
	if len(page.RequestLogs) != 2 {
		t.Fatalf("request logs = %d, want 2", len(page.RequestLogs))
	}
	if page.RequestLogs[0].RequestID != "req_new" || page.RequestLogs[1].RequestID != "req_recent" {
		t.Fatalf("request logs = %+v, want retained newest-first entries", page.RequestLogs)
	}
}

func TestConfigFromEnvSupportsRequestLogPruningSettings(t *testing.T) {
	t.Setenv("PORTHOOK_REQUEST_LOG_RETENTION", "0s")
	t.Setenv("PORTHOOK_REQUEST_LOG_PRUNE_INTERVAL", "15m")

	cfg := ConfigFromEnv()
	if cfg.RequestLogRetention != 0 {
		t.Fatalf("RequestLogRetention = %s, want 0s", cfg.RequestLogRetention)
	}
	if cfg.RequestLogPruneInterval != 15*time.Minute {
		t.Fatalf("RequestLogPruneInterval = %s, want 15m", cfg.RequestLogPruneInterval)
	}
}
