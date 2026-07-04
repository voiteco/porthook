// SPDX-License-Identifier: AGPL-3.0-only

package admintokens

import (
	"strings"
	"testing"
)

func TestNewPostgresStoreRejectsNilDB(t *testing.T) {
	if _, err := NewPostgresStore(nil); err == nil {
		t.Fatal("NewPostgresStore returned nil error")
	}
}

func TestPostgresSchemaStatements(t *testing.T) {
	statements := PostgresSchemaStatements()
	if len(statements) == 0 {
		t.Fatal("PostgresSchemaStatements returned no statements")
	}
	if statements[0] == "" {
		t.Fatal("first schema statement is empty")
	}
}

func TestPostgresMigrationsAreVersioned(t *testing.T) {
	migrations := PostgresMigrations()
	if len(migrations) != 1 {
		t.Fatalf("migrations = %d, want 1", len(migrations))
	}
	migration := migrations[0]
	if migration.Version != 10 {
		t.Fatalf("migration version = %d, want 10", migration.Version)
	}
	if migration.Name != "create_admin_tokens" {
		t.Fatalf("migration name = %q, want create_admin_tokens", migration.Name)
	}
	if !strings.Contains(migration.SQL, "CREATE TABLE IF NOT EXISTS admin_tokens") {
		t.Fatalf("migration SQL = %q, want admin_tokens table", migration.SQL)
	}
}

func TestParsePostgresMigrationName(t *testing.T) {
	version, name, err := parsePostgresMigrationName("0010_create_admin_tokens.sql")
	if err != nil {
		t.Fatalf("parsePostgresMigrationName returned error: %v", err)
	}
	if version != 10 || name != "create_admin_tokens" {
		t.Fatalf("version/name = %d/%q, want 10/create_admin_tokens", version, name)
	}
}
