package event

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/relay/internal/domain"
)

// Store provides persistence for events and delivery attempts.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a new event store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Create persists a new event (without outbox - use CreateWithOutbox for reliable publishing).
func (s *Store) Create(ctx context.Context, event domain.Event) (domain.Event, error) {
	headersJSON, err := json.Marshal(event.Headers)
	if err != nil {
		return domain.Event{}, err
	}

	query := `
		INSERT INTO events (id, idempotency_key, client_id, event_type, endpoint_id, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id, idempotency_key, client_id, event_type, endpoint_id, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, delivered_at, created_at, updated_at
	`

	return s.scanEvent(s.pool.QueryRow(ctx, query,
		event.ID,
		event.IdempotencyKey,
		nullString(event.ClientID),
		nullString(event.EventType),
		event.EndpointID,
		event.Destination,
		event.Payload,
		headersJSON,
		event.Status,
		event.Attempts,
		event.MaxAttempts,
		event.NextAttemptAt,
		event.CreatedAt,
		event.UpdatedAt,
	))
}

// nullString returns nil if s is empty, otherwise returns s.
func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// CreateWithOutbox creates an event and outbox entry in a single transaction.
// This ensures the event will eventually be published to the queue even if the process crashes.
func (s *Store) CreateWithOutbox(ctx context.Context, event domain.Event) (domain.Event, error) {
	headersJSON, err := json.Marshal(event.Headers)
	if err != nil {
		return domain.Event{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Event{}, err
	}
	defer tx.Rollback(ctx)

	// Insert event
	eventQuery := `
		INSERT INTO events (id, idempotency_key, client_id, event_type, endpoint_id, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id, idempotency_key, client_id, event_type, endpoint_id, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, delivered_at, created_at, updated_at
	`

	createdEvent, err := s.scanEvent(tx.QueryRow(ctx, eventQuery,
		event.ID,
		event.IdempotencyKey,
		nullString(event.ClientID),
		nullString(event.EventType),
		event.EndpointID,
		event.Destination,
		event.Payload,
		headersJSON,
		event.Status,
		event.Attempts,
		event.MaxAttempts,
		event.NextAttemptAt,
		event.CreatedAt,
		event.UpdatedAt,
	))
	if err != nil {
		return domain.Event{}, err
	}

	// Insert outbox entry
	outboxQuery := `INSERT INTO outbox (id, event_id) VALUES ($1, $2)`
	_, err = tx.Exec(ctx, outboxQuery, uuid.New(), createdEvent.ID)
	if err != nil {
		return domain.Event{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Event{}, err
	}

	return createdEvent, nil
}

// GetByID retrieves an event by ID.
func (s *Store) GetByID(ctx context.Context, id uuid.UUID) (domain.Event, error) {
	query := `
		SELECT id, idempotency_key, client_id, event_type, endpoint_id, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, delivered_at, created_at, updated_at
		FROM events
		WHERE id = $1
	`

	event, err := s.scanEvent(s.pool.QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Event{}, domain.ErrEventNotFound
	}
	return event, err
}

// GetByIdempotencyKey retrieves an event by idempotency key.
func (s *Store) GetByIdempotencyKey(ctx context.Context, key string) (domain.Event, error) {
	query := `
		SELECT id, idempotency_key, client_id, event_type, endpoint_id, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, delivered_at, created_at, updated_at
		FROM events
		WHERE idempotency_key = $1
	`

	event, err := s.scanEvent(s.pool.QueryRow(ctx, query, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Event{}, domain.ErrEventNotFound
	}
	return event, err
}

// Update updates an existing event.
func (s *Store) Update(ctx context.Context, event domain.Event) (domain.Event, error) {
	headersJSON, err := json.Marshal(event.Headers)
	if err != nil {
		return domain.Event{}, err
	}

	query := `
		UPDATE events
		SET destination = $2, payload = $3, headers = $4, status = $5, attempts = $6, max_attempts = $7, next_attempt_at = $8, delivered_at = $9, updated_at = NOW()
		WHERE id = $1
		RETURNING id, idempotency_key, client_id, event_type, endpoint_id, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, delivered_at, created_at, updated_at
	`

	updated, err := s.scanEvent(s.pool.QueryRow(ctx, query,
		event.ID,
		event.Destination,
		event.Payload,
		headersJSON,
		event.Status,
		event.Attempts,
		event.MaxAttempts,
		event.NextAttemptAt,
		event.DeliveredAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Event{}, domain.ErrEventNotFound
	}
	return updated, err
}

// ListByStatus retrieves events by status with pagination.
func (s *Store) ListByStatus(ctx context.Context, status domain.EventStatus, limit, offset int) ([]domain.Event, error) {
	query := `
		SELECT id, idempotency_key, client_id, event_type, endpoint_id, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, delivered_at, created_at, updated_at
		FROM events
		WHERE status = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.pool.Query(ctx, query, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanEvents(rows)
}

// ListReadyForDelivery retrieves events ready for delivery attempt.
func (s *Store) ListReadyForDelivery(ctx context.Context, limit int) ([]domain.Event, error) {
	query := `
		SELECT id, idempotency_key, client_id, event_type, endpoint_id, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, delivered_at, created_at, updated_at
		FROM events
		WHERE status IN ('queued', 'failed')
		AND (next_attempt_at IS NULL OR next_attempt_at <= $1)
		ORDER BY next_attempt_at ASC NULLS FIRST
		LIMIT $2
	`

	rows, err := s.pool.Query(ctx, query, time.Now().UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanEvents(rows)
}

// CreateDeliveryAttempt persists a delivery attempt.
func (s *Store) CreateDeliveryAttempt(ctx context.Context, attempt domain.DeliveryAttempt) (domain.DeliveryAttempt, error) {
	query := `
		INSERT INTO delivery_attempts (id, event_id, status_code, response_body, error, duration_ms, attempt_number, attempted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, event_id, status_code, response_body, error, duration_ms, attempt_number, attempted_at
	`

	row := s.pool.QueryRow(ctx, query,
		attempt.ID,
		attempt.EventID,
		attempt.StatusCode,
		attempt.ResponseBody,
		attempt.Error,
		attempt.DurationMs,
		attempt.AttemptNumber,
		attempt.AttemptedAt,
	)

	return s.scanDeliveryAttempt(row)
}

// GetDeliveryAttempts retrieves all delivery attempts for an event.
func (s *Store) GetDeliveryAttempts(ctx context.Context, eventID uuid.UUID) ([]domain.DeliveryAttempt, error) {
	query := `
		SELECT id, event_id, status_code, response_body, error, duration_ms, attempt_number, attempted_at
		FROM delivery_attempts
		WHERE event_id = $1
		ORDER BY attempted_at ASC
	`

	rows, err := s.pool.Query(ctx, query, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attempts []domain.DeliveryAttempt
	for rows.Next() {
		attempt, err := s.scanDeliveryAttemptFromRows(rows)
		if err != nil {
			return nil, err
		}
		attempts = append(attempts, attempt)
	}

	return attempts, rows.Err()
}

// GetQueueStats retrieves queue statistics.
func (s *Store) GetQueueStats(ctx context.Context) (QueueStats, error) {
	query := `
		SELECT 
			COUNT(*) FILTER (WHERE status = 'queued') as queued,
			COUNT(*) FILTER (WHERE status = 'delivering') as delivering,
			COUNT(*) FILTER (WHERE status = 'delivered') as delivered,
			COUNT(*) FILTER (WHERE status = 'failed') as failed,
			COUNT(*) FILTER (WHERE status = 'dead') as dead
		FROM events
	`

	var stats QueueStats
	err := s.pool.QueryRow(ctx, query).Scan(
		&stats.Queued,
		&stats.Delivering,
		&stats.Delivered,
		&stats.Failed,
		&stats.Dead,
	)
	return stats, err
}

// OutboxEntry represents an entry in the outbox table.
type OutboxEntry struct {
	ID          uuid.UUID
	EventID     uuid.UUID
	CreatedAt   time.Time
	ProcessedAt *time.Time
	Attempts    int
	LastError   *string
}

// ClaimAndGetOutbox atomically claims and retrieves unprocessed outbox entries.
// Uses UPDATE ... RETURNING to ensure atomicity - entries are claimed in the same
// operation that retrieves them, preventing race conditions.
func (s *Store) ClaimAndGetOutbox(ctx context.Context, workerID string, limit int, claimTimeout time.Duration) ([]OutboxEntry, error) {
	// Use a CTE to atomically claim and return entries
	// This prevents race conditions where multiple workers could claim the same entries
	query := `
		WITH claimed AS (
			UPDATE outbox
			SET last_error = $1
			WHERE id IN (
				SELECT id FROM outbox
				WHERE processed_at IS NULL 
				  AND (last_error IS NULL OR last_error NOT LIKE 'claimed:%' OR 
				       created_at < NOW() - $3::interval)
				ORDER BY created_at ASC
				LIMIT $2
				FOR UPDATE SKIP LOCKED
			)
			RETURNING id, event_id, created_at, processed_at, attempts, last_error
		)
		SELECT id, event_id, created_at, processed_at, attempts, last_error FROM claimed
	`

	claimMarker := "claimed:" + workerID
	rows, err := s.pool.Query(ctx, query, claimMarker, limit, claimTimeout.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []OutboxEntry
	for rows.Next() {
		var entry OutboxEntry
		if err := rows.Scan(&entry.ID, &entry.EventID, &entry.CreatedAt, &entry.ProcessedAt, &entry.Attempts, &entry.LastError); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

// GetUnprocessedOutbox retrieves unprocessed outbox entries (legacy, prefer ClaimAndGetOutbox).
func (s *Store) GetUnprocessedOutbox(ctx context.Context, limit int) ([]OutboxEntry, error) {
	query := `
		SELECT id, event_id, created_at, processed_at, attempts, last_error
		FROM outbox
		WHERE processed_at IS NULL
		ORDER BY created_at ASC
		LIMIT $1
	`

	rows, err := s.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []OutboxEntry
	for rows.Next() {
		var entry OutboxEntry
		if err := rows.Scan(&entry.ID, &entry.EventID, &entry.CreatedAt, &entry.ProcessedAt, &entry.Attempts, &entry.LastError); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

// MarkOutboxProcessed marks an outbox entry as processed.
func (s *Store) MarkOutboxProcessed(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE outbox SET processed_at = NOW() WHERE id = $1`
	_, err := s.pool.Exec(ctx, query, id)
	return err
}

// MarkOutboxFailed marks an outbox entry as failed with error.
func (s *Store) MarkOutboxFailed(ctx context.Context, id uuid.UUID, errMsg string) error {
	query := `UPDATE outbox SET attempts = attempts + 1, last_error = $2 WHERE id = $1`
	_, err := s.pool.Exec(ctx, query, id, errMsg)
	return err
}

// CleanupProcessedOutbox removes old processed outbox entries.
func (s *Store) CleanupProcessedOutbox(ctx context.Context, olderThan time.Duration) (int64, error) {
	query := `DELETE FROM outbox WHERE processed_at IS NOT NULL AND processed_at < $1`
	result, err := s.pool.Exec(ctx, query, time.Now().Add(-olderThan))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// QueueStats holds queue statistics.
type QueueStats struct {
	Queued     int64
	Delivering int64
	Delivered  int64
	Failed     int64
	Dead       int64
}

// ============================================================================
// Endpoint CRUD Operations
// ============================================================================

// CreateEndpoint creates a new endpoint.
func (s *Store) CreateEndpoint(ctx context.Context, endpoint domain.Endpoint) (domain.Endpoint, error) {
	headersJSON, err := json.Marshal(endpoint.CustomHeaders)
	if err != nil {
		return domain.Endpoint{}, err
	}

	query := `
		INSERT INTO endpoints (
			id, client_id, url, description, event_types, status,
			max_retries, retry_backoff_ms, retry_backoff_max, retry_backoff_mult,
			timeout_ms, rate_limit_per_sec, circuit_threshold, circuit_reset_ms,
			custom_headers, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING id, client_id, url, description, event_types, status,
			max_retries, retry_backoff_ms, retry_backoff_max, retry_backoff_mult,
			timeout_ms, rate_limit_per_sec, circuit_threshold, circuit_reset_ms,
			custom_headers, created_at, updated_at
	`

	return s.scanEndpoint(s.pool.QueryRow(ctx, query,
		endpoint.ID,
		endpoint.ClientID,
		endpoint.URL,
		endpoint.Description,
		endpoint.EventTypes,
		endpoint.Status,
		endpoint.MaxRetries,
		endpoint.RetryBackoffMs,
		endpoint.RetryBackoffMax,
		endpoint.RetryBackoffMult,
		endpoint.TimeoutMs,
		endpoint.RateLimitPerSec,
		endpoint.CircuitThreshold,
		endpoint.CircuitResetMs,
		headersJSON,
		endpoint.CreatedAt,
		endpoint.UpdatedAt,
	))
}

// UpdateEndpoint updates an existing endpoint.
func (s *Store) UpdateEndpoint(ctx context.Context, endpoint domain.Endpoint) (domain.Endpoint, error) {
	headersJSON, err := json.Marshal(endpoint.CustomHeaders)
	if err != nil {
		return domain.Endpoint{}, err
	}

	query := `
		UPDATE endpoints SET
			url = $2, description = $3, event_types = $4, status = $5,
			max_retries = $6, retry_backoff_ms = $7, retry_backoff_max = $8, retry_backoff_mult = $9,
			timeout_ms = $10, rate_limit_per_sec = $11, circuit_threshold = $12, circuit_reset_ms = $13,
			custom_headers = $14, updated_at = NOW()
		WHERE id = $1
		RETURNING id, client_id, url, description, event_types, status,
			max_retries, retry_backoff_ms, retry_backoff_max, retry_backoff_mult,
			timeout_ms, rate_limit_per_sec, circuit_threshold, circuit_reset_ms,
			custom_headers, created_at, updated_at
	`

	updated, err := s.scanEndpoint(s.pool.QueryRow(ctx, query,
		endpoint.ID,
		endpoint.URL,
		endpoint.Description,
		endpoint.EventTypes,
		endpoint.Status,
		endpoint.MaxRetries,
		endpoint.RetryBackoffMs,
		endpoint.RetryBackoffMax,
		endpoint.RetryBackoffMult,
		endpoint.TimeoutMs,
		endpoint.RateLimitPerSec,
		endpoint.CircuitThreshold,
		endpoint.CircuitResetMs,
		headersJSON,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Endpoint{}, domain.ErrEndpointNotFound
	}
	return updated, err
}

// GetEndpointByID retrieves an endpoint by ID.
func (s *Store) GetEndpointByID(ctx context.Context, id uuid.UUID) (domain.Endpoint, error) {
	query := `
		SELECT id, client_id, url, description, event_types, status,
			max_retries, retry_backoff_ms, retry_backoff_max, retry_backoff_mult,
			timeout_ms, rate_limit_per_sec, circuit_threshold, circuit_reset_ms,
			custom_headers, created_at, updated_at
		FROM endpoints
		WHERE id = $1
	`

	endpoint, err := s.scanEndpoint(s.pool.QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Endpoint{}, domain.ErrEndpointNotFound
	}
	return endpoint, err
}

// ListEndpointsByClient retrieves all endpoints for a client.
func (s *Store) ListEndpointsByClient(ctx context.Context, clientID string, limit, offset int) ([]domain.Endpoint, error) {
	query := `
		SELECT id, client_id, url, description, event_types, status,
			max_retries, retry_backoff_ms, retry_backoff_max, retry_backoff_mult,
			timeout_ms, rate_limit_per_sec, circuit_threshold, circuit_reset_ms,
			custom_headers, created_at, updated_at
		FROM endpoints
		WHERE client_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.pool.Query(ctx, query, clientID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanEndpoints(rows)
}

// FindActiveEndpointsByEventType finds all active endpoints subscribed to an event type.
func (s *Store) FindActiveEndpointsByEventType(ctx context.Context, clientID, eventType string) ([]domain.Endpoint, error) {
	// Match endpoints where:
	// 1. event_types array contains the specific event type, OR
	// 2. event_types array contains '*' (wildcard), OR
	// 3. event_types array is empty (subscribe to all)
	query := `
		SELECT id, client_id, url, description, event_types, status,
			max_retries, retry_backoff_ms, retry_backoff_max, retry_backoff_mult,
			timeout_ms, rate_limit_per_sec, circuit_threshold, circuit_reset_ms,
			custom_headers, created_at, updated_at
		FROM endpoints
		WHERE client_id = $1
		AND status = 'active'
		AND ($2 = ANY(event_types) OR '*' = ANY(event_types) OR array_length(event_types, 1) IS NULL)
		ORDER BY created_at ASC
	`

	rows, err := s.pool.Query(ctx, query, clientID, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanEndpoints(rows)
}

// DeleteEndpoint deletes an endpoint by ID.
func (s *Store) DeleteEndpoint(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM endpoints WHERE id = $1`
	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return domain.ErrEndpointNotFound
	}
	return nil
}

// CountEndpointsByClient returns the total number of endpoints for a client.
func (s *Store) CountEndpointsByClient(ctx context.Context, clientID string) (int64, error) {
	query := `SELECT COUNT(*) FROM endpoints WHERE client_id = $1`
	var count int64
	err := s.pool.QueryRow(ctx, query, clientID).Scan(&count)
	return count, err
}

// ============================================================================
// Event Fanout Logic
// ============================================================================

// CreateEventWithFanout creates events for all endpoints subscribed to the event type.
// Returns the list of created events (one per matching endpoint).
func (s *Store) CreateEventWithFanout(ctx context.Context, clientID, eventType, idempotencyKey string, payload json.RawMessage, headers map[string]string) ([]domain.Event, error) {
	// Find all active endpoints subscribed to this event type
	endpoints, err := s.FindActiveEndpointsByEventType(ctx, clientID, eventType)
	if err != nil {
		return nil, err
	}

	if len(endpoints) == 0 {
		return nil, domain.ErrNoSubscribedEndpoints
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var createdEvents []domain.Event

	for _, endpoint := range endpoints {
		// Create unique idempotency key per endpoint
		endpointIdempotencyKey := idempotencyKey + ":" + endpoint.ID.String()

		// Create event for this endpoint
		event := domain.NewEventForEndpoint(clientID, eventType, endpointIdempotencyKey, endpoint, payload, headers)

		headersJSON, err := json.Marshal(event.Headers)
		if err != nil {
			return nil, err
		}

		// Insert event
		eventQuery := `
			INSERT INTO events (id, idempotency_key, client_id, event_type, endpoint_id, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			RETURNING id, idempotency_key, client_id, event_type, endpoint_id, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, delivered_at, created_at, updated_at
		`

		createdEvent, err := s.scanEvent(tx.QueryRow(ctx, eventQuery,
			event.ID,
			event.IdempotencyKey,
			nullString(event.ClientID),
			nullString(event.EventType),
			event.EndpointID,
			event.Destination,
			event.Payload,
			headersJSON,
			event.Status,
			event.Attempts,
			event.MaxAttempts,
			event.NextAttemptAt,
			event.CreatedAt,
			event.UpdatedAt,
		))
		if err != nil {
			return nil, err
		}

		// Insert outbox entry for reliable delivery
		outboxQuery := `INSERT INTO outbox (id, event_id) VALUES ($1, $2)`
		_, err = tx.Exec(ctx, outboxQuery, uuid.New(), createdEvent.ID)
		if err != nil {
			return nil, err
		}

		createdEvents = append(createdEvents, createdEvent)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return createdEvents, nil
}

// ListEventsByEndpoint retrieves events for a specific endpoint with pagination.
func (s *Store) ListEventsByEndpoint(ctx context.Context, endpointID uuid.UUID, limit, offset int) ([]domain.Event, error) {
	query := `
		SELECT id, idempotency_key, client_id, event_type, endpoint_id, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, delivered_at, created_at, updated_at
		FROM events
		WHERE endpoint_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.pool.Query(ctx, query, endpointID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanEvents(rows)
}

// GetEndpointStats retrieves delivery statistics for an endpoint.
func (s *Store) GetEndpointStats(ctx context.Context, endpointID uuid.UUID) (EndpointStats, error) {
	query := `
		SELECT 
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE status = 'delivered') as delivered,
			COUNT(*) FILTER (WHERE status = 'failed') as failed,
			COUNT(*) FILTER (WHERE status IN ('queued', 'delivering')) as pending,
			COALESCE(AVG(da.duration_ms) FILTER (WHERE e.status = 'delivered'), 0) as avg_latency_ms
		FROM events e
		LEFT JOIN delivery_attempts da ON da.event_id = e.id AND da.attempt_number = e.attempts
		WHERE e.endpoint_id = $1
	`

	var stats EndpointStats
	err := s.pool.QueryRow(ctx, query, endpointID).Scan(
		&stats.TotalEvents,
		&stats.Delivered,
		&stats.Failed,
		&stats.Pending,
		&stats.AvgLatencyMs,
	)
	if err != nil {
		return EndpointStats{}, err
	}

	if stats.TotalEvents > 0 {
		stats.SuccessRate = float64(stats.Delivered) / float64(stats.TotalEvents)
	}

	return stats, nil
}

// EndpointStats holds statistics for an endpoint.
type EndpointStats struct {
	TotalEvents  int64
	Delivered    int64
	Failed       int64
	Pending      int64
	SuccessRate  float64
	AvgLatencyMs float64
}

func (s *Store) scanEvent(row pgx.Row) (domain.Event, error) {
	var event domain.Event
	var headersJSON []byte
	var clientID, eventType *string
	var endpointID *uuid.UUID

	err := row.Scan(
		&event.ID,
		&event.IdempotencyKey,
		&clientID,
		&eventType,
		&endpointID,
		&event.Destination,
		&event.Payload,
		&headersJSON,
		&event.Status,
		&event.Attempts,
		&event.MaxAttempts,
		&event.NextAttemptAt,
		&event.DeliveredAt,
		&event.CreatedAt,
		&event.UpdatedAt,
	)
	if err != nil {
		return domain.Event{}, err
	}

	if clientID != nil {
		event.ClientID = *clientID
	}
	if eventType != nil {
		event.EventType = *eventType
	}
	event.EndpointID = endpointID

	if len(headersJSON) > 0 {
		if err := json.Unmarshal(headersJSON, &event.Headers); err != nil {
			return domain.Event{}, err
		}
	}

	return event, nil
}

func (s *Store) scanEvents(rows pgx.Rows) ([]domain.Event, error) {
	var events []domain.Event
	for rows.Next() {
		var event domain.Event
		var headersJSON []byte
		var clientID, eventType *string
		var endpointID *uuid.UUID

		err := rows.Scan(
			&event.ID,
			&event.IdempotencyKey,
			&clientID,
			&eventType,
			&endpointID,
			&event.Destination,
			&event.Payload,
			&headersJSON,
			&event.Status,
			&event.Attempts,
			&event.MaxAttempts,
			&event.NextAttemptAt,
			&event.DeliveredAt,
			&event.CreatedAt,
			&event.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if clientID != nil {
			event.ClientID = *clientID
		}
		if eventType != nil {
			event.EventType = *eventType
		}
		event.EndpointID = endpointID

		if len(headersJSON) > 0 {
			if err := json.Unmarshal(headersJSON, &event.Headers); err != nil {
				return nil, err
			}
		}

		events = append(events, event)
	}

	return events, rows.Err()
}

func (s *Store) scanDeliveryAttempt(row pgx.Row) (domain.DeliveryAttempt, error) {
	var attempt domain.DeliveryAttempt
	err := row.Scan(
		&attempt.ID,
		&attempt.EventID,
		&attempt.StatusCode,
		&attempt.ResponseBody,
		&attempt.Error,
		&attempt.DurationMs,
		&attempt.AttemptNumber,
		&attempt.AttemptedAt,
	)
	return attempt, err
}

func (s *Store) scanDeliveryAttemptFromRows(rows pgx.Rows) (domain.DeliveryAttempt, error) {
	var attempt domain.DeliveryAttempt
	err := rows.Scan(
		&attempt.ID,
		&attempt.EventID,
		&attempt.StatusCode,
		&attempt.ResponseBody,
		&attempt.Error,
		&attempt.DurationMs,
		&attempt.AttemptNumber,
		&attempt.AttemptedAt,
	)
	return attempt, err
}

func (s *Store) scanEndpoint(row pgx.Row) (domain.Endpoint, error) {
	var endpoint domain.Endpoint
	var headersJSON []byte
	var description *string

	err := row.Scan(
		&endpoint.ID,
		&endpoint.ClientID,
		&endpoint.URL,
		&description,
		&endpoint.EventTypes,
		&endpoint.Status,
		&endpoint.MaxRetries,
		&endpoint.RetryBackoffMs,
		&endpoint.RetryBackoffMax,
		&endpoint.RetryBackoffMult,
		&endpoint.TimeoutMs,
		&endpoint.RateLimitPerSec,
		&endpoint.CircuitThreshold,
		&endpoint.CircuitResetMs,
		&headersJSON,
		&endpoint.CreatedAt,
		&endpoint.UpdatedAt,
	)
	if err != nil {
		return domain.Endpoint{}, err
	}

	if description != nil {
		endpoint.Description = *description
	}

	if len(headersJSON) > 0 {
		if err := json.Unmarshal(headersJSON, &endpoint.CustomHeaders); err != nil {
			return domain.Endpoint{}, err
		}
	}
	if endpoint.CustomHeaders == nil {
		endpoint.CustomHeaders = make(map[string]string)
	}

	return endpoint, nil
}

func (s *Store) scanEndpoints(rows pgx.Rows) ([]domain.Endpoint, error) {
	var endpoints []domain.Endpoint
	for rows.Next() {
		var endpoint domain.Endpoint
		var headersJSON []byte
		var description *string

		err := rows.Scan(
			&endpoint.ID,
			&endpoint.ClientID,
			&endpoint.URL,
			&description,
			&endpoint.EventTypes,
			&endpoint.Status,
			&endpoint.MaxRetries,
			&endpoint.RetryBackoffMs,
			&endpoint.RetryBackoffMax,
			&endpoint.RetryBackoffMult,
			&endpoint.TimeoutMs,
			&endpoint.RateLimitPerSec,
			&endpoint.CircuitThreshold,
			&endpoint.CircuitResetMs,
			&headersJSON,
			&endpoint.CreatedAt,
			&endpoint.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if description != nil {
			endpoint.Description = *description
		}

		if len(headersJSON) > 0 {
			if err := json.Unmarshal(headersJSON, &endpoint.CustomHeaders); err != nil {
				return nil, err
			}
		}
		if endpoint.CustomHeaders == nil {
			endpoint.CustomHeaders = make(map[string]string)
		}

		endpoints = append(endpoints, endpoint)
	}

	return endpoints, rows.Err()
}
