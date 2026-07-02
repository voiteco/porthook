// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"strings"
	"testing"
	"time"
)

func TestRequestLogPostgresMigrationsAreVersioned(t *testing.T) {
	migrations := RequestLogPostgresMigrations()
	if len(migrations) != 1 {
		t.Fatalf("migrations = %d, want 1", len(migrations))
	}
	if migrations[0].Version != 9 {
		t.Fatalf("migration version = %d, want 9", migrations[0].Version)
	}
	if migrations[0].Name != "create_gateway_request_logs" {
		t.Fatalf("migration name = %q, want create_gateway_request_logs", migrations[0].Name)
	}
	for _, want := range []string{"gateway_request_logs", "request_id", "query_present", "duration_ms"} {
		if !strings.Contains(migrations[0].SQL, want) {
			t.Fatalf("migration SQL = %q, want %q", migrations[0].SQL, want)
		}
	}
}

func TestNewPostgresRequestLogStoreRejectsNilDB(t *testing.T) {
	if _, err := NewPostgresRequestLogStore(nil); err == nil {
		t.Fatal("NewPostgresRequestLogStore returned nil error")
	}
}

func TestRequestLogSQLFilters(t *testing.T) {
	since := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	until := since.Add(time.Hour)
	cursorTime := until.Add(time.Minute)
	where, args := requestLogSQLFilters(requestLogListOptions{
		Filter: requestLogFilter{
			Subdomain: "api",
			Method:    "GET",
			Host:      "example",
			Path:      "/hooks",
			Status:    502,
			Outcome:   "tunnel",
			RequestID: "req",
			TunnelID:  "tun",
			Since:     since,
			Until:     until,
		},
		Cursor: requestLogCursor{Time: cursorTime, ID: 42},
	})
	for _, want := range []string{
		"LOWER(subdomain) = $1",
		"status = $2",
		"outcome ILIKE $3",
		"request_id ILIKE $4",
		"host ILIKE $5",
		"method = $6",
		"path ILIKE $7",
		"tunnel_id ILIKE $8",
		"time >= $9",
		"time <= $10",
		"(time < $11 OR (time = $11 AND id < $12))",
	} {
		if !strings.Contains(where, want) {
			t.Fatalf("where = %q, want %q", where, want)
		}
	}
	if len(args) != 12 {
		t.Fatalf("args = %d, want 12", len(args))
	}
}
