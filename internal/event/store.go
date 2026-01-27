package event

import (
	"context"
	"encoding/json"
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
		INSERT INTO events (id, idempotency_key, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, idempotency_key, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, delivered_at, created_at, updated_at
	`

	return s.scanEvent(s.pool.QueryRow(ctx, query,
		event.ID,
		event.IdempotencyKey,
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
		INSERT INTO events (id, idempotency_key, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, idempotency_key, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, delivered_at, created_at, updated_at
	`

	createdEvent, err := s.scanEvent(tx.QueryRow(ctx, eventQuery,
		event.ID,
		event.IdempotencyKey,
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
		SELECT id, idempotency_key, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, delivered_at, created_at, updated_at
		FROM events
		WHERE id = $1
	`

	event, err := s.scanEvent(s.pool.QueryRow(ctx, query, id))
	if err == pgx.ErrNoRows {
		return domain.Event{}, domain.ErrEventNotFound
	}
	return event, err
}

// GetByIdempotencyKey retrieves an event by idempotency key.
func (s *Store) GetByIdempotencyKey(ctx context.Context, key string) (domain.Event, error) {
	query := `
		SELECT id, idempotency_key, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, delivered_at, created_at, updated_at
		FROM events
		WHERE idempotency_key = $1
	`

	event, err := s.scanEvent(s.pool.QueryRow(ctx, query, key))
	if err == pgx.ErrNoRows {
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
		SET destination = $2, payload = $3, headers = $4, status = $5, attempts = $6, max_attempts = $7, next_attempt_at = $8, delivered_at = $9
		WHERE id = $1
		RETURNING id, idempotency_key, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, delivered_at, created_at, updated_at
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
	if err == pgx.ErrNoRows {
		return domain.Event{}, domain.ErrEventNotFound
	}
	return updated, err
}

// ListByStatus retrieves events by status with pagination.
func (s *Store) ListByStatus(ctx context.Context, status domain.EventStatus, limit, offset int) ([]domain.Event, error) {
	query := `
		SELECT id, idempotency_key, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, delivered_at, created_at, updated_at
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
		SELECT id, idempotency_key, destination, payload, headers, status, attempts, max_attempts, next_attempt_at, delivered_at, created_at, updated_at
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

// GetUnprocessedOutbox retrieves unprocessed outbox entries.
func (s *Store) GetUnprocessedOutbox(ctx context.Context, limit int) ([]OutboxEntry, error) {
	query := `
		SELECT id, event_id, created_at, processed_at, attempts, last_error
		FROM outbox
		WHERE processed_at IS NULL
		ORDER BY created_at ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED
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

func (s *Store) scanEvent(row pgx.Row) (domain.Event, error) {
	var event domain.Event
	var headersJSON []byte

	err := row.Scan(
		&event.ID,
		&event.IdempotencyKey,
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

		err := rows.Scan(
			&event.ID,
			&event.IdempotencyKey,
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
