// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"strings"
	"testing"
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
