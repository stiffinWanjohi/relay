-- Remove client_id from events
ALTER TABLE events DROP COLUMN IF EXISTS client_id;

-- Drop api_keys foreign key and table
ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS fk_api_keys_client;
DROP TABLE IF EXISTS api_keys;

-- Drop clients table
DROP TABLE IF EXISTS clients;
