-- Add FIFO (ordered delivery) configuration to endpoints
ALTER TABLE endpoints ADD COLUMN fifo BOOLEAN DEFAULT FALSE;
ALTER TABLE endpoints ADD COLUMN fifo_partition_key VARCHAR(255);

-- Index for efficient querying of FIFO endpoints
CREATE INDEX idx_endpoints_fifo ON endpoints (fifo) WHERE fifo = TRUE;

-- Comment for documentation
COMMENT ON COLUMN endpoints.fifo IS 'When true, events are delivered sequentially (one at a time)';
COMMENT ON COLUMN endpoints.fifo_partition_key IS 'JSONPath expression to extract partition key from payload for parallel ordered streams';
