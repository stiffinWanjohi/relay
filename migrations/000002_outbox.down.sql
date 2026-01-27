-- Drop outbox indexes
DROP INDEX IF EXISTS idx_outbox_processed;
DROP INDEX IF EXISTS idx_outbox_unprocessed;

-- Drop outbox table
DROP TABLE IF EXISTS outbox;
