-- Remove per-endpoint signing secrets
DROP INDEX IF EXISTS idx_endpoints_secret_rotation;

ALTER TABLE endpoints
DROP COLUMN IF EXISTS signing_secret,
DROP COLUMN IF EXISTS previous_secret,
DROP COLUMN IF EXISTS secret_rotated_at;
