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

	"github.com/stiffinWanjohi/relay/internal/logging"
)

var log = logging.Component("auth")

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
	keyPrefix := apiKey[:min(8, len(apiKey))]

	var clientID string
	var isActive bool
	var expiresAt *time.Time

	err := s.pool.QueryRow(ctx, `
		SELECT client_id, is_active, expires_at
		FROM api_keys
		WHERE key_hash = $1
	`, keyHash).Scan(&clientID, &isActive, &expiresAt)

	if errors.Is(err, pgx.ErrNoRows) {
		log.Debug("API key not found", "key_prefix", keyPrefix)
		return "", ErrInvalidAPIKey
	}
	if err != nil {
		log.Error("failed to validate API key", "key_prefix", keyPrefix, "error", err)
		return "", err
	}

	if !isActive {
		log.Debug("API key is inactive", "key_prefix", keyPrefix, "client_id", clientID)
		return "", ErrInvalidAPIKey
	}

	if expiresAt != nil && time.Now().After(*expiresAt) {
		log.Debug("API key expired", "key_prefix", keyPrefix, "client_id", clientID, "expired_at", expiresAt)
		return "", ErrInvalidAPIKey
	}

	// Update last_used_at asynchronously (fire and forget)
	go func() {
		_, _ = s.pool.Exec(context.Background(), `
			UPDATE api_keys SET last_used_at = NOW() WHERE key_hash = $1
		`, keyHash)
	}()

	log.Debug("API key validated", "key_prefix", keyPrefix, "client_id", clientID)
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
		log.Debug("client not found", "client_id", clientID)
		return nil, ErrUnauthorized
	}
	if err != nil {
		log.Error("failed to get client", "client_id", clientID, "error", err)
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
		log.Error("failed to create client", "client_id", client.ID, "error", err)
		return nil, err
	}

	log.Info("client created", "client_id", client.ID, "name", client.Name)
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
		log.Error("failed to create API key", "client_id", clientID, "name", name, "error", err)
		return "", nil, err
	}

	log.Info("API key created", "key_id", apiKey.ID, "client_id", clientID, "key_prefix", keyPrefix, "name", name)
	return rawKey, &apiKey, nil
}

// RevokeAPIKey revokes an API key.
func (s *Store) RevokeAPIKey(ctx context.Context, keyID string) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE api_keys SET is_active = FALSE, updated_at = NOW()
		WHERE id = $1
	`, keyID)

	if err != nil {
		log.Error("failed to revoke API key", "key_id", keyID, "error", err)
		return err
	}

	if result.RowsAffected() == 0 {
		log.Debug("API key not found for revocation", "key_id", keyID)
		return ErrInvalidAPIKey
	}

	log.Info("API key revoked", "key_id", keyID)
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
		log.Error("failed to list API keys", "client_id", clientID, "error", err)
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
			log.Error("failed to scan API key row", "client_id", clientID, "error", err)
			return nil, err
		}
		keys = append(keys, key)
	}

	if err := rows.Err(); err != nil {
		log.Error("error iterating API keys", "client_id", clientID, "error", err)
		return nil, err
	}

	return keys, nil
}

// hashAPIKey creates a SHA-256 hash of the API key.
func hashAPIKey(apiKey string) string {
	hash := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(hash[:])
}

// generateAPIKey generates a new cryptographically secure random API key.
// Format: rly_<64 hex characters> (32 bytes of entropy = 256 bits)
func generateAPIKey() string {
	return "rly_" + generateRandomHex(32)
}

// generateRandomHex generates a cryptographically secure random hex string.
// Uses crypto/rand which reads from the OS's cryptographic random source.
// Panics if the system's secure random number generator fails (extremely rare).
func generateRandomHex(byteLen int) string {
	b := make([]byte, byteLen)
	n, err := rand.Read(b)
	if err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	if n != byteLen {
		panic("crypto/rand: insufficient random bytes")
	}
	return hex.EncodeToString(b)
}
