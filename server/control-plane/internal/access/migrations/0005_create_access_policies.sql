CREATE TABLE IF NOT EXISTS access_policies (
	id TEXT PRIMARY KEY,
	reserved_subdomain_id TEXT NOT NULL UNIQUE REFERENCES reserved_subdomains(id) ON DELETE CASCADE,
	mode TEXT NOT NULL,
	basic_username TEXT NOT NULL DEFAULT '',
	secret_hash TEXT NOT NULL DEFAULT '',
	ip_allowlist_json TEXT NOT NULL DEFAULT '[]',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	CONSTRAINT access_policies_mode_check CHECK (mode IN ('public', 'basic_auth', 'bearer_token', 'ip_allowlist'))
);

CREATE INDEX IF NOT EXISTS access_policies_reserved_subdomain_id_idx ON access_policies (reserved_subdomain_id);
