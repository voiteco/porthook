// SPDX-License-Identifier: AGPL-3.0-only

package tokens

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
	if len(migrations) != 3 {
		t.Fatalf("migrations = %d, want 3", len(migrations))
	}
	for i, migration := range migrations {
		wantVersion := i + 1
		if migration.Version != wantVersion {
			t.Fatalf("migration[%d].Version = %d, want %d", i, migration.Version, wantVersion)
		}
		if migration.Name == "" {
			t.Fatalf("migration[%d].Name is empty", i)
		}
		if strings.TrimSpace(migration.SQL) == "" {
			t.Fatalf("migration[%d].SQL is empty", i)
		}
	}
	if migrations[1].Name != "add_token_last_used_at" || !strings.Contains(migrations[1].SQL, "last_used_at") {
		t.Fatalf("migration[1] = %+v, want last_used_at migration", migrations[1])
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
