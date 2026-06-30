// SPDX-License-Identifier: AGPL-3.0-only

package access

import (
	"strings"
	"testing"
)

func TestNewPostgresStoreRejectsNilDB(t *testing.T) {
	if _, err := NewPostgresStore(nil); err == nil {
		t.Fatal("NewPostgresStore returned nil error")
	}
}

func TestPostgresMigrationsAreVersioned(t *testing.T) {
	migrations := PostgresMigrations()
	if len(migrations) != 1 {
		t.Fatalf("migrations = %d, want 1", len(migrations))
	}
	if migrations[0].Version != 5 {
		t.Fatalf("migration version = %d, want 5", migrations[0].Version)
	}
	if migrations[0].Name != "create_access_policies" {
		t.Fatalf("migration name = %q, want create_access_policies", migrations[0].Name)
	}
	if !strings.Contains(migrations[0].SQL, "access_policies") {
		t.Fatalf("migration SQL = %q, want access_policies", migrations[0].SQL)
	}
}

func TestParsePostgresMigrationName(t *testing.T) {
	version, name, err := parsePostgresMigrationName("0042_add_example.sql")
	if err != nil {
		t.Fatalf("parsePostgresMigrationName returned error: %v", err)
	}
	if version != 42 || name != "add_example" {
		t.Fatalf("version/name = %d/%q, want 42/add_example", version, name)
	}
}
