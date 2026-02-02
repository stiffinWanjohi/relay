package eventtype

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stiffinWanjohi/relay/internal/domain"
)

// Store provides persistence for event types.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a new event type store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Create persists a new event type.
func (s *Store) Create(ctx context.Context, et domain.EventType) (domain.EventType, error) {
	query := `
		INSERT INTO event_types (id, client_id, name, description, schema, schema_version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, client_id, name, description, schema, schema_version, created_at, updated_at
	`

	created, err := s.scanEventType(s.pool.QueryRow(ctx, query,
		et.ID,
		et.ClientID,
		et.Name,
		nullString(et.Description),
		et.Schema,
		nullString(et.SchemaVersion),
		et.CreatedAt,
		et.UpdatedAt,
	))
	if err != nil {
		// Check for unique constraint violation
		if strings.Contains(err.Error(), "unique_client_event_type") {
			return domain.EventType{}, domain.ErrDuplicateEventType
		}
		return domain.EventType{}, err
	}

	return created, nil
}

// GetByID retrieves an event type by ID.
func (s *Store) GetByID(ctx context.Context, id uuid.UUID) (domain.EventType, error) {
	query := `
		SELECT id, client_id, name, description, schema, schema_version, created_at, updated_at
		FROM event_types
		WHERE id = $1
	`

	et, err := s.scanEventType(s.pool.QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.EventType{}, domain.ErrEventTypeNotFound
	}
	return et, err
}

// GetByName retrieves an event type by client ID and name.
func (s *Store) GetByName(ctx context.Context, clientID, name string) (domain.EventType, error) {
	query := `
		SELECT id, client_id, name, description, schema, schema_version, created_at, updated_at
		FROM event_types
		WHERE client_id = $1 AND name = $2
	`

	et, err := s.scanEventType(s.pool.QueryRow(ctx, query, clientID, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.EventType{}, domain.ErrEventTypeNotFound
	}
	return et, err
}

// List retrieves event types for a client with pagination.
func (s *Store) List(ctx context.Context, clientID string, limit, offset int) ([]domain.EventType, error) {
	query := `
		SELECT id, client_id, name, description, schema, schema_version, created_at, updated_at
		FROM event_types
		WHERE client_id = $1
		ORDER BY name ASC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.pool.Query(ctx, query, clientID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanEventTypes(rows)
}

// Count returns the total number of event types for a client.
func (s *Store) Count(ctx context.Context, clientID string) (int64, error) {
	query := `SELECT COUNT(*) FROM event_types WHERE client_id = $1`
	var count int64
	err := s.pool.QueryRow(ctx, query, clientID).Scan(&count)
	return count, err
}

// Update updates an existing event type.
func (s *Store) Update(ctx context.Context, et domain.EventType) (domain.EventType, error) {
	query := `
		UPDATE event_types
		SET description = $2, schema = $3, schema_version = $4, updated_at = NOW()
		WHERE id = $1
		RETURNING id, client_id, name, description, schema, schema_version, created_at, updated_at
	`

	updated, err := s.scanEventType(s.pool.QueryRow(ctx, query,
		et.ID,
		nullString(et.Description),
		et.Schema,
		nullString(et.SchemaVersion),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.EventType{}, domain.ErrEventTypeNotFound
	}
	return updated, err
}

// Delete removes an event type by ID.
func (s *Store) Delete(ctx context.Context, id uuid.UUID) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM event_types WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return domain.ErrEventTypeNotFound
	}
	return nil
}

// Exists checks if an event type with the given name exists for a client.
func (s *Store) Exists(ctx context.Context, clientID, name string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM event_types WHERE client_id = $1 AND name = $2)`
	var exists bool
	err := s.pool.QueryRow(ctx, query, clientID, name).Scan(&exists)
	return exists, err
}

// nullString returns nil if s is empty, otherwise returns s.
func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *Store) scanEventType(row pgx.Row) (domain.EventType, error) {
	var et domain.EventType
	var description, schemaVersion *string

	err := row.Scan(
		&et.ID,
		&et.ClientID,
		&et.Name,
		&description,
		&et.Schema,
		&schemaVersion,
		&et.CreatedAt,
		&et.UpdatedAt,
	)
	if err != nil {
		return domain.EventType{}, err
	}

	if description != nil {
		et.Description = *description
	}
	if schemaVersion != nil {
		et.SchemaVersion = *schemaVersion
	}

	return et, nil
}

func (s *Store) scanEventTypes(rows pgx.Rows) ([]domain.EventType, error) {
	var types []domain.EventType
	for rows.Next() {
		var et domain.EventType
		var description, schemaVersion *string

		err := rows.Scan(
			&et.ID,
			&et.ClientID,
			&et.Name,
			&description,
			&et.Schema,
			&schemaVersion,
			&et.CreatedAt,
			&et.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if description != nil {
			et.Description = *description
		}
		if schemaVersion != nil {
			et.SchemaVersion = *schemaVersion
		}

		types = append(types, et)
	}

	return types, rows.Err()
}
