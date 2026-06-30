CREATE TABLE IF NOT EXISTS reserved_subdomains (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	token_id TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS reserved_subdomains_token_id_idx ON reserved_subdomains (token_id);
