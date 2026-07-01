ALTER TABLE custom_domains
	ADD COLUMN IF NOT EXISTS verification_token TEXT;

ALTER TABLE custom_domains
	ADD COLUMN IF NOT EXISTS verified_at TIMESTAMPTZ;

UPDATE custom_domains
SET verification_token = CASE
		WHEN verification_token IS NULL OR verification_token = '' THEN 'legacy-' || id
		ELSE verification_token
	END,
	verified_at = COALESCE(verified_at, updated_at)
WHERE status = 'active';

UPDATE custom_domains
SET verification_token = 'legacy-' || id
WHERE verification_token IS NULL OR verification_token = '';

ALTER TABLE custom_domains
	ALTER COLUMN verification_token SET NOT NULL;

ALTER TABLE custom_domains
	DROP CONSTRAINT IF EXISTS custom_domains_status_check;

ALTER TABLE custom_domains
	ADD CONSTRAINT custom_domains_status_check
	CHECK (status IN ('pending_verification', 'active', 'verification_failed'));

CREATE INDEX IF NOT EXISTS custom_domains_status_idx ON custom_domains (status);
