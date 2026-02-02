-- Remove transformation column from endpoints table

DROP INDEX IF EXISTS idx_endpoints_has_transformation;
ALTER TABLE endpoints DROP COLUMN IF EXISTS transformation;
