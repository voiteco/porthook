// SPDX-License-Identifier: AGPL-3.0-only

package tokens

import "testing"

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
