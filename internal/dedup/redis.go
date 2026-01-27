package dedup

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/stiffinWanjohi/relay/internal/domain"
)

const (
	// Default TTL for idempotency keys
	defaultTTL = 24 * time.Hour

	// Key prefix for idempotency keys
	keyPrefix = "relay:idemp:"
)

// Checker provides idempotency checking using Redis.
type Checker struct {
	client *redis.Client
	ttl    time.Duration
}

// NewChecker creates a new idempotency checker.
func NewChecker(client *redis.Client) *Checker {
	return &Checker{
		client: client,
		ttl:    defaultTTL,
	}
}

// WithTTL sets a custom TTL for idempotency keys.
func (c *Checker) WithTTL(ttl time.Duration) *Checker {
	return &Checker{
		client: c.client,
		ttl:    ttl,
	}
}

// Check checks if an idempotency key already exists.
// Returns the existing event ID if found, otherwise returns uuid.Nil.
func (c *Checker) Check(ctx context.Context, idempotencyKey string) (uuid.UUID, error) {
	key := keyPrefix + idempotencyKey

	result, err := c.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, err
	}

	eventID, err := uuid.Parse(result)
	if err != nil {
		return uuid.Nil, err
	}

	return eventID, nil
}

// Set stores an idempotency key with the associated event ID.
// Returns ErrDuplicateEvent if the key already exists with a different event ID.
func (c *Checker) Set(ctx context.Context, idempotencyKey string, eventID uuid.UUID) error {
	key := keyPrefix + idempotencyKey

	// Use SETNX (SetNX) to atomically set only if not exists
	wasSet, err := c.client.SetNX(ctx, key, eventID.String(), c.ttl).Result()
	if err != nil {
		return err
	}

	if !wasSet {
		// Key already exists, check if it's the same event ID
		existing, err := c.Check(ctx, idempotencyKey)
		if err != nil {
			return err
		}
		if existing != eventID {
			return domain.ErrDuplicateEvent
		}
	}

	return nil
}

// CheckAndSet atomically checks and sets an idempotency key.
// Returns the existing event ID if duplicate, or uuid.Nil and sets the new key.
func (c *Checker) CheckAndSet(ctx context.Context, idempotencyKey string, eventID uuid.UUID) (uuid.UUID, error) {
	key := keyPrefix + idempotencyKey

	// Use a Lua script for atomic check-and-set
	script := redis.NewScript(`
		local existing = redis.call('GET', KEYS[1])
		if existing then
			return existing
		end
		redis.call('SET', KEYS[1], ARGV[1], 'EX', ARGV[2])
		return nil
	`)

	result, err := script.Run(ctx, c.client, []string{key}, eventID.String(), int(c.ttl.Seconds())).Result()
	if errors.Is(err, redis.Nil) {
		// Key was set successfully
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, err
	}

	// Key already existed, parse the existing event ID
	existingID, err := uuid.Parse(result.(string))
	if err != nil {
		return uuid.Nil, err
	}

	return existingID, nil
}

// Delete removes an idempotency key.
func (c *Checker) Delete(ctx context.Context, idempotencyKey string) error {
	key := keyPrefix + idempotencyKey
	return c.client.Del(ctx, key).Err()
}

// Extend extends the TTL of an existing idempotency key.
func (c *Checker) Extend(ctx context.Context, idempotencyKey string) error {
	key := keyPrefix + idempotencyKey
	return c.client.Expire(ctx, key, c.ttl).Err()
}
