// SPDX-License-Identifier: AGPL-3.0-only

package tokens

func PostgresSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS api_tokens (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			scopes_json TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL,
			last_used_at TIMESTAMPTZ,
			revoked_at TIMESTAMPTZ
		)`,
		`ALTER TABLE api_tokens ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ`,
		`CREATE INDEX IF NOT EXISTS api_tokens_token_hash_idx ON api_tokens (token_hash)`,
	}
}
