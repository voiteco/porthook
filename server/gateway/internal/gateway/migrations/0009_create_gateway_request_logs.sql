CREATE TABLE IF NOT EXISTS gateway_request_logs (
	id BIGSERIAL PRIMARY KEY,
	time TIMESTAMPTZ NOT NULL,
	method TEXT NOT NULL,
	host TEXT NOT NULL,
	path TEXT NOT NULL,
	query_present BOOLEAN NOT NULL,
	remote_ip TEXT NOT NULL,
	request_id TEXT,
	subdomain TEXT,
	custom_domain TEXT,
	tunnel_id TEXT,
	stream_id TEXT,
	status INTEGER NOT NULL,
	outcome TEXT NOT NULL,
	request_bytes BIGINT NOT NULL,
	response_bytes BIGINT NOT NULL,
	duration_ms BIGINT NOT NULL,
	error TEXT
);

CREATE INDEX IF NOT EXISTS gateway_request_logs_time_id_idx ON gateway_request_logs (time DESC, id DESC);
CREATE INDEX IF NOT EXISTS gateway_request_logs_subdomain_idx ON gateway_request_logs (subdomain);
CREATE INDEX IF NOT EXISTS gateway_request_logs_tunnel_id_idx ON gateway_request_logs (tunnel_id);
CREATE INDEX IF NOT EXISTS gateway_request_logs_request_id_idx ON gateway_request_logs (request_id);
CREATE INDEX IF NOT EXISTS gateway_request_logs_status_idx ON gateway_request_logs (status);
CREATE INDEX IF NOT EXISTS gateway_request_logs_outcome_idx ON gateway_request_logs (outcome);
