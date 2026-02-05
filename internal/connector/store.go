package connector

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Errors
var (
	ErrConnectorNotFound  = errors.New("connector not found")
	ErrDuplicateConnector = errors.New("connector with this name already exists")
)

// StoredConnector represents a connector with persistence metadata.
type StoredConnector struct {
	ID        uuid.UUID     `json:"id"`
	ClientID  string        `json:"client_id"`
	Name      string        `json:"name"`
	Type      ConnectorType `json:"type"`
	Enabled   bool          `json:"enabled"`
	Config    Config        `json:"config"`
	Template  Template      `json:"template"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// ToConnector converts a StoredConnector to a Connector.
func (sc *StoredConnector) ToConnector() *Connector {
	return &Connector{
		Type:     sc.Type,
		Config:   sc.Config,
		Template: sc.Template,
	}
}

// Store provides PostgreSQL persistence for connectors.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a new connector store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Create persists a new connector.
func (s *Store) Create(ctx context.Context, clientID, name string, connector *Connector) (*StoredConnector, error) {
	configJSON, err := json.Marshal(connector.Config)
	if err != nil {
		return nil, err
	}

	templateJSON, err := json.Marshal(connector.Template)
	if err != nil {
		return nil, err
	}

	id := uuid.New()
	now := time.Now().UTC()

	query := `
		INSERT INTO connectors (id, client_id, name, type, enabled, config, template, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, client_id, name, type, enabled, config, template, created_at, updated_at
	`

	stored, err := s.scanConnector(s.pool.QueryRow(ctx, query,
		id,
		clientID,
		name,
		string(connector.Type),
		true, // enabled by default
		configJSON,
		templateJSON,
		now,
		now,
	))
	if err != nil {
		if strings.Contains(err.Error(), "unique_client_connector") {
			return nil, ErrDuplicateConnector
		}
		return nil, err
	}

	log.Info("connector created", "id", stored.ID, "name", name, "type", connector.Type)
	return stored, nil
}

// GetByID retrieves a connector by ID.
func (s *Store) GetByID(ctx context.Context, id uuid.UUID) (*StoredConnector, error) {
	query := `
		SELECT id, client_id, name, type, enabled, config, template, created_at, updated_at
		FROM connectors
		WHERE id = $1
	`

	stored, err := s.scanConnector(s.pool.QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrConnectorNotFound
	}
	return stored, err
}

// GetByName retrieves a connector by client ID and name.
func (s *Store) GetByName(ctx context.Context, clientID, name string) (*StoredConnector, error) {
	query := `
		SELECT id, client_id, name, type, enabled, config, template, created_at, updated_at
		FROM connectors
		WHERE client_id = $1 AND name = $2
	`

	stored, err := s.scanConnector(s.pool.QueryRow(ctx, query, clientID, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrConnectorNotFound
	}
	return stored, err
}

// List retrieves all connectors for a client.
func (s *Store) List(ctx context.Context, clientID string) ([]*StoredConnector, error) {
	query := `
		SELECT id, client_id, name, type, enabled, config, template, created_at, updated_at
		FROM connectors
		WHERE client_id = $1
		ORDER BY name ASC
	`

	rows, err := s.pool.Query(ctx, query, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanConnectors(rows)
}

// ListEnabled retrieves all enabled connectors for a client.
func (s *Store) ListEnabled(ctx context.Context, clientID string) ([]*StoredConnector, error) {
	query := `
		SELECT id, client_id, name, type, enabled, config, template, created_at, updated_at
		FROM connectors
		WHERE client_id = $1 AND enabled = true
		ORDER BY name ASC
	`

	rows, err := s.pool.Query(ctx, query, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanConnectors(rows)
}

// Update updates an existing connector.
func (s *Store) Update(ctx context.Context, id uuid.UUID, connector *Connector, enabled bool) (*StoredConnector, error) {
	configJSON, err := json.Marshal(connector.Config)
	if err != nil {
		return nil, err
	}

	templateJSON, err := json.Marshal(connector.Template)
	if err != nil {
		return nil, err
	}

	query := `
		UPDATE connectors
		SET type = $2, enabled = $3, config = $4, template = $5, updated_at = NOW()
		WHERE id = $1
		RETURNING id, client_id, name, type, enabled, config, template, created_at, updated_at
	`

	stored, err := s.scanConnector(s.pool.QueryRow(ctx, query,
		id,
		string(connector.Type),
		enabled,
		configJSON,
		templateJSON,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrConnectorNotFound
	}

	log.Info("connector updated", "id", id)
	return stored, err
}

// SetEnabled enables or disables a connector.
func (s *Store) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	query := `UPDATE connectors SET enabled = $2, updated_at = NOW() WHERE id = $1`
	result, err := s.pool.Exec(ctx, query, id, enabled)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrConnectorNotFound
	}
	log.Info("connector enabled state changed", "id", id, "enabled", enabled)
	return nil
}

// Delete removes a connector by ID.
func (s *Store) Delete(ctx context.Context, id uuid.UUID) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM connectors WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrConnectorNotFound
	}
	log.Info("connector deleted", "id", id)
	return nil
}

// DeleteByName removes a connector by client ID and name.
func (s *Store) DeleteByName(ctx context.Context, clientID, name string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM connectors WHERE client_id = $1 AND name = $2`, clientID, name)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrConnectorNotFound
	}
	log.Info("connector deleted", "client_id", clientID, "name", name)
	return nil
}

func (s *Store) scanConnector(row pgx.Row) (*StoredConnector, error) {
	var sc StoredConnector
	var connectorType string
	var configJSON, templateJSON []byte

	err := row.Scan(
		&sc.ID,
		&sc.ClientID,
		&sc.Name,
		&connectorType,
		&sc.Enabled,
		&configJSON,
		&templateJSON,
		&sc.CreatedAt,
		&sc.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	sc.Type = ConnectorType(connectorType)

	if err := json.Unmarshal(configJSON, &sc.Config); err != nil {
		return nil, err
	}

	if len(templateJSON) > 0 {
		if err := json.Unmarshal(templateJSON, &sc.Template); err != nil {
			return nil, err
		}
	}

	return &sc, nil
}

func (s *Store) scanConnectors(rows pgx.Rows) ([]*StoredConnector, error) {
	var connectors []*StoredConnector
	for rows.Next() {
		var sc StoredConnector
		var connectorType string
		var configJSON, templateJSON []byte

		err := rows.Scan(
			&sc.ID,
			&sc.ClientID,
			&sc.Name,
			&connectorType,
			&sc.Enabled,
			&configJSON,
			&templateJSON,
			&sc.CreatedAt,
			&sc.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		sc.Type = ConnectorType(connectorType)

		if err := json.Unmarshal(configJSON, &sc.Config); err != nil {
			return nil, err
		}

		if len(templateJSON) > 0 {
			if err := json.Unmarshal(templateJSON, &sc.Template); err != nil {
				return nil, err
			}
		}

		connectors = append(connectors, &sc)
	}

	return connectors, rows.Err()
}
