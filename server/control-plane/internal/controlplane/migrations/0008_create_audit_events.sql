CREATE TABLE IF NOT EXISTS audit_events (
	id BIGSERIAL PRIMARY KEY,
	time TIMESTAMPTZ NOT NULL,
	level TEXT NOT NULL,
	message TEXT NOT NULL,
	event TEXT NOT NULL,
	method TEXT NOT NULL,
	path TEXT NOT NULL,
	remote_ip TEXT NOT NULL,
	request_id TEXT,
	fields_json JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS audit_events_time_id_idx ON audit_events (time DESC, id DESC);
CREATE INDEX IF NOT EXISTS audit_events_event_idx ON audit_events (event);
CREATE INDEX IF NOT EXISTS audit_events_request_id_idx ON audit_events (request_id);
