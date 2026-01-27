-- Remove indexes first
DROP INDEX IF EXISTS idx_events_endpoint_created;
DROP INDEX IF EXISTS idx_events_endpoint;
DROP INDEX IF EXISTS idx_events_event_type;

-- Remove columns from events table
ALTER TABLE events DROP COLUMN IF EXISTS endpoint_id;
ALTER TABLE events DROP COLUMN IF EXISTS event_type;

-- Remove endpoint indexes
DROP INDEX IF EXISTS idx_endpoints_active;
DROP INDEX IF EXISTS idx_endpoints_event_types;
DROP INDEX IF EXISTS idx_endpoints_client_status;
DROP INDEX IF EXISTS idx_endpoints_client;

-- Drop endpoints table
DROP TABLE IF EXISTS endpoints;
