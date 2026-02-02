-- Add transformation column to endpoints table
-- Stores JavaScript code for payload transformation before delivery

ALTER TABLE endpoints ADD COLUMN transformation TEXT;

-- Create index for endpoints that have transformations
-- This helps when querying endpoints with transformations for debugging/monitoring
CREATE INDEX idx_endpoints_has_transformation ON endpoints ((transformation IS NOT NULL)) WHERE transformation IS NOT NULL;

COMMENT ON COLUMN endpoints.transformation IS 'JavaScript code to transform webhook payload before delivery';
