CREATE TABLE IF NOT EXISTS custom_domains (
	id TEXT PRIMARY KEY,
	hostname TEXT NOT NULL UNIQUE,
	reserved_subdomain_id TEXT NOT NULL REFERENCES reserved_subdomains(id) ON DELETE CASCADE,
	status TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	CONSTRAINT custom_domains_status_check CHECK (status IN ('active'))
);

CREATE INDEX IF NOT EXISTS custom_domains_reserved_subdomain_id_idx ON custom_domains (reserved_subdomain_id);
