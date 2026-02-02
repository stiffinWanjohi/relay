-- Add filter column to endpoints table for content-based routing
-- The filter is stored as JSONB containing filter rules with JSONPath expressions

ALTER TABLE endpoints ADD COLUMN filter JSONB;

-- Add an index for endpoints that have filters configured
-- This helps with queries that need to find endpoints with specific filter configurations
CREATE INDEX idx_endpoints_has_filter ON endpoints ((filter IS NOT NULL)) WHERE filter IS NOT NULL;

COMMENT ON COLUMN endpoints.filter IS 'Content-based routing filter as JSONB. Events are only delivered if they match the filter conditions.';
