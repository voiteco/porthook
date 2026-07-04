CREATE TABLE IF NOT EXISTS admin_tokens (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	token_hash TEXT NOT NULL UNIQUE,
	scopes_json TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	last_used_at TIMESTAMPTZ,
	revoked_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS admin_tokens_token_hash_idx ON admin_tokens (token_hash);
CREATE INDEX IF NOT EXISTS admin_tokens_revoked_at_idx ON admin_tokens (revoked_at);
