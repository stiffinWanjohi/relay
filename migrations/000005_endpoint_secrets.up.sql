-- Add per-endpoint signing secrets with rotation support
ALTER TABLE endpoints
ADD COLUMN signing_secret VARCHAR(64),
ADD COLUMN previous_secret VARCHAR(64),
ADD COLUMN secret_rotated_at TIMESTAMPTZ;

-- Index for cleanup of rotated secrets (find endpoints with old previous secrets)
CREATE INDEX idx_endpoints_secret_rotation ON endpoints(secret_rotated_at)
WHERE previous_secret IS NOT NULL;

-- Add comment explaining the rotation mechanism
COMMENT ON COLUMN endpoints.signing_secret IS 'Current signing secret for webhook signatures. If NULL, use global secret.';
COMMENT ON COLUMN endpoints.previous_secret IS 'Previous signing secret, valid during rotation grace period.';
COMMENT ON COLUMN endpoints.secret_rotated_at IS 'Timestamp when the secret was last rotated.';
