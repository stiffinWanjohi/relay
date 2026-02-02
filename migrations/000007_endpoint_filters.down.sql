-- Remove filter column from endpoints table

DROP INDEX IF EXISTS idx_endpoints_has_filter;
ALTER TABLE endpoints DROP COLUMN IF EXISTS filter;
