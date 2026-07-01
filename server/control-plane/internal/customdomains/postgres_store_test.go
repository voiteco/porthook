// SPDX-License-Identifier: AGPL-3.0-only

package customdomains

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
	if len(migrations) != 2 {
		t.Fatalf("migrations = %d, want 2", len(migrations))
	}
	if migrations[0].Version != 6 {
		t.Fatalf("migration version = %d, want 6", migrations[0].Version)
	}
	if migrations[0].Name != "create_custom_domains" {
		t.Fatalf("migration name = %q, want create_custom_domains", migrations[0].Name)
	}
	if !strings.Contains(migrations[0].SQL, "custom_domains") {
		t.Fatalf("migration SQL = %q, want custom_domains", migrations[0].SQL)
	}
	if migrations[1].Version != 7 {
		t.Fatalf("migration version = %d, want 7", migrations[1].Version)
	}
	if migrations[1].Name != "add_custom_domain_verification" {
		t.Fatalf("migration name = %q, want add_custom_domain_verification", migrations[1].Name)
	}
	if !strings.Contains(migrations[1].SQL, "verification_token") {
		t.Fatalf("migration SQL = %q, want verification_token", migrations[1].SQL)
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
