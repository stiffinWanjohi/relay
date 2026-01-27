-- Drop client-related indexes from events
DROP INDEX IF EXISTS idx_events_failed_recent;
DROP INDEX IF EXISTS idx_events_client_created;
DROP INDEX IF EXISTS idx_events_client_status;
DROP INDEX IF EXISTS idx_events_client_id;

-- Remove client_id from events
ALTER TABLE events DROP COLUMN IF EXISTS client_id;

-- Drop api_keys foreign key and table
ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS fk_api_keys_client;
DROP TABLE IF EXISTS api_keys;

-- Drop clients table
DROP TABLE IF EXISTS clients;
