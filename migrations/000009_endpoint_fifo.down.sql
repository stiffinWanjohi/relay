-- Remove FIFO configuration from endpoints
DROP INDEX IF EXISTS idx_endpoints_fifo;
ALTER TABLE endpoints DROP COLUMN IF EXISTS fifo_partition_key;
ALTER TABLE endpoints DROP COLUMN IF EXISTS fifo;
