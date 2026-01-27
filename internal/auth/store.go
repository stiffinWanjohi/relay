package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// APIKey represents an API key record.
type APIKey struct {
	ID         string
	ClientID   string
	KeyHash    string
	KeyPrefix  string
	Name       string
	Scopes     []string
	RateLimit  int
	IsActive   bool
	ExpiresAt  *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
	LastUsedAt *time.Time
}

// Client represents a client/tenant.
type Client struct {
	ID                 string
	Name               string
	Email              string
	WebhookURLPatterns []string
	MaxEventsPerDay    int64
	IsActive           bool
	Metadata           map[string]any
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Store provides database operations for authentication.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a new authentication store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ValidateAPIKey validates an API key and returns the client ID.
func (s *Store) ValidateAPIKey(ctx context.Context, apiKey string) (string, error) {
	keyHash := hashAPIKey(apiKey)

	var clientID string
	var isActive bool
	var expiresAt *time.Time

	err := s.pool.QueryRow(ctx, `
		SELECT client_id, is_active, expires_at
		FROM api_keys
		WHERE key_hash = $1
	`, keyHash).Scan(&clientID, &isActive, &expiresAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidAPIKey
	}
	if err != nil {
		return "", err
	}

	if !isActive {
		return "", ErrInvalidAPIKey
	}

	if expiresAt != nil && time.Now().After(*expiresAt) {
		return "", ErrInvalidAPIKey
	}

	// Update last_used_at asynchronously (fire and forget)
	go func() {
		_, _ = s.pool.Exec(context.Background(), `
			UPDATE api_keys SET last_used_at = NOW() WHERE key_hash = $1
		`, keyHash)
	}()

	return clientID, nil
}

// GetClient retrieves a client by ID.
func (s *Store) GetClient(ctx context.Context, clientID string) (*Client, error) {
	var client Client
	var metadata []byte

	err := s.pool.QueryRow(ctx, `
		SELECT id, name, email, webhook_url_patterns, max_events_per_day, 
		       is_active, metadata, created_at, updated_at
		FROM clients
		WHERE id = $1
	`, clientID).Scan(
		&client.ID, &client.Name, &client.Email, &client.WebhookURLPatterns,
		&client.MaxEventsPerDay, &client.IsActive, &metadata,
		&client.CreatedAt, &client.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}

	return &client, nil
}

// CreateClient creates a new client.
func (s *Store) CreateClient(ctx context.Context, client Client) (*Client, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO clients (id, name, email, webhook_url_patterns, max_events_per_day, is_active, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at
	`, client.ID, client.Name, client.Email, client.WebhookURLPatterns,
		client.MaxEventsPerDay, client.IsActive, client.Metadata,
	).Scan(&client.CreatedAt, &client.UpdatedAt)

	if err != nil {
		return nil, err
	}

	return &client, nil
}

// CreateAPIKey creates a new API key and returns the raw key (only shown once).
func (s *Store) CreateAPIKey(ctx context.Context, clientID string, name string, scopes []string, rateLimit int, expiresAt *time.Time) (rawKey string, key *APIKey, err error) {
	rawKey = generateAPIKey()
	keyHash := hashAPIKey(rawKey)
	keyPrefix := rawKey[:8]

	var apiKey APIKey
	err = s.pool.QueryRow(ctx, `
		INSERT INTO api_keys (client_id, key_hash, key_prefix, name, scopes, rate_limit, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, client_id, key_hash, key_prefix, name, scopes, rate_limit, is_active, expires_at, created_at, updated_at
	`, clientID, keyHash, keyPrefix, name, scopes, rateLimit, expiresAt,
	).Scan(
		&apiKey.ID, &apiKey.ClientID, &apiKey.KeyHash, &apiKey.KeyPrefix,
		&apiKey.Name, &apiKey.Scopes, &apiKey.RateLimit, &apiKey.IsActive,
		&apiKey.ExpiresAt, &apiKey.CreatedAt, &apiKey.UpdatedAt,
	)

	if err != nil {
		return "", nil, err
	}

	return rawKey, &apiKey, nil
}

// RevokeAPIKey revokes an API key.
func (s *Store) RevokeAPIKey(ctx context.Context, keyID string) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE api_keys SET is_active = FALSE, updated_at = NOW()
		WHERE id = $1
	`, keyID)

	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrInvalidAPIKey
	}

	return nil
}

// ListAPIKeys lists all API keys for a client (without revealing the actual keys).
func (s *Store) ListAPIKeys(ctx context.Context, clientID string) ([]APIKey, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, client_id, key_hash, key_prefix, name, scopes, rate_limit, 
		       is_active, expires_at, created_at, updated_at, last_used_at
		FROM api_keys
		WHERE client_id = $1
		ORDER BY created_at DESC
	`, clientID)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var key APIKey
		err := rows.Scan(
			&key.ID, &key.ClientID, &key.KeyHash, &key.KeyPrefix, &key.Name,
			&key.Scopes, &key.RateLimit, &key.IsActive, &key.ExpiresAt,
			&key.CreatedAt, &key.UpdatedAt, &key.LastUsedAt,
		)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}

	return keys, rows.Err()
}

// hashAPIKey creates a SHA-256 hash of the API key.
func hashAPIKey(apiKey string) string {
	hash := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(hash[:])
}

// generateAPIKey generates a new random API key.
func generateAPIKey() string {
	// Generate 32 random bytes for a 64-character hex string
	// In production, use crypto/rand
	return "rly_" + generateRandomHex(32)
}

// generateRandomHex generates a random hex string of the specified byte length.
func generateRandomHex(byteLen int) string {
	b := make([]byte, byteLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
