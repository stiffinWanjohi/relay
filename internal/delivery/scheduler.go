package delivery

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// DRRScheduler implements Deficit Round-Robin scheduling for fair
// multi-tenant event processing. This prevents "noisy neighbor" problems
// where one tenant with many events starves others.
type DRRScheduler struct {
	client  *redis.Client
	quantum int64 // bytes to add per round
	mu      sync.Mutex
}

// DRRConfig holds configuration for the DRR scheduler.
type DRRConfig struct {
	// Quantum is the number of "credits" (typically bytes) added per round.
	// Higher quantum = more events processed per tenant per round.
	Quantum int64
}

// DefaultDRRConfig returns the default DRR configuration.
func DefaultDRRConfig() DRRConfig {
	return DRRConfig{
		Quantum: 65536, // 64KB per round - roughly 1-10 typical webhook payloads
	}
}

// NewDRRScheduler creates a new Deficit Round-Robin scheduler.
func NewDRRScheduler(client *redis.Client, config DRRConfig) *DRRScheduler {
	quantum := config.Quantum
	if quantum <= 0 {
		quantum = DefaultDRRConfig().Quantum
	}

	return &DRRScheduler{
		client:  client,
		quantum: quantum,
	}
}

const (
	drrDeficitKey = "relay:drr:deficit"    // Hash: client_id -> deficit
	drrActiveKey  = "relay:drr:active"     // Set: active client_ids
	drrLastServed = "relay:drr:lastserved" // String: last served client_id
	drrDeficitTTL = 1 * time.Hour          // TTL for deficit entries
)

// RegisterClient marks a client as active for scheduling.
func (s *DRRScheduler) RegisterClient(ctx context.Context, clientID string) error {
	return s.client.SAdd(ctx, drrActiveKey, clientID).Err()
}

// UnregisterClient removes a client from active scheduling.
func (s *DRRScheduler) UnregisterClient(ctx context.Context, clientID string) error {
	pipe := s.client.Pipeline()
	pipe.SRem(ctx, drrActiveKey, clientID)
	pipe.HDel(ctx, drrDeficitKey, clientID)
	_, err := pipe.Exec(ctx)
	return err
}

// SelectNextClient selects the next client to process using DRR algorithm.
// Returns empty string if no clients are active or all have zero deficit.
func (s *DRRScheduler) SelectNextClient(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get all active clients
	clients, err := s.client.SMembers(ctx, drrActiveKey).Result()
	if err != nil || len(clients) == 0 {
		return "", err
	}

	// Get last served client to continue round-robin from there
	lastServed, _ := s.client.Get(ctx, drrLastServed).Result()

	// Find starting index
	startIdx := 0
	for i, c := range clients {
		if c == lastServed {
			startIdx = (i + 1) % len(clients)
			break
		}
	}

	// Try each client in round-robin order
	for i := 0; i < len(clients); i++ {
		idx := (startIdx + i) % len(clients)
		clientID := clients[idx]

		// Get current deficit
		deficit, err := s.client.HGet(ctx, drrDeficitKey, clientID).Int64()
		if err != nil && err != redis.Nil {
			continue
		}

		// If no deficit entry, add quantum (new client joining)
		if err == redis.Nil {
			deficit = s.quantum
			s.client.HSet(ctx, drrDeficitKey, clientID, deficit)
		}

		if deficit > 0 {
			// Record this client as last served
			s.client.Set(ctx, drrLastServed, clientID, drrDeficitTTL)
			return clientID, nil
		}
	}

	// All clients exhausted - add quantum to all and try again
	if err := s.addQuantumToAll(ctx, clients); err != nil {
		return "", err
	}

	// Return first client after adding quantum
	if len(clients) > 0 {
		s.client.Set(ctx, drrLastServed, clients[startIdx], drrDeficitTTL)
		return clients[startIdx], nil
	}

	return "", nil
}

// RecordDelivery records that a delivery was made, consuming deficit.
func (s *DRRScheduler) RecordDelivery(ctx context.Context, clientID string, payloadSize int64) error {
	// Subtract payload size from deficit
	newDeficit, err := s.client.HIncrBy(ctx, drrDeficitKey, clientID, -payloadSize).Result()
	if err != nil {
		return err
	}

	// If deficit went negative, set to 0
	if newDeficit < 0 {
		return s.client.HSet(ctx, drrDeficitKey, clientID, 0).Err()
	}

	return nil
}

// GetDeficit returns the current deficit for a client.
func (s *DRRScheduler) GetDeficit(ctx context.Context, clientID string) (int64, error) {
	deficit, err := s.client.HGet(ctx, drrDeficitKey, clientID).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return deficit, err
}

// addQuantumToAll adds quantum to all active clients.
func (s *DRRScheduler) addQuantumToAll(ctx context.Context, clients []string) error {
	pipe := s.client.Pipeline()
	for _, clientID := range clients {
		pipe.HIncrBy(ctx, drrDeficitKey, clientID, s.quantum)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// Stats returns current scheduler statistics.
func (s *DRRScheduler) Stats(ctx context.Context) (DRRStats, error) {
	clients, err := s.client.SMembers(ctx, drrActiveKey).Result()
	if err != nil {
		return DRRStats{}, err
	}

	deficits := make(map[string]int64)
	for _, clientID := range clients {
		deficit, _ := s.client.HGet(ctx, drrDeficitKey, clientID).Int64()
		deficits[clientID] = deficit
	}

	return DRRStats{
		ActiveClients: clients,
		Deficits:      deficits,
		Quantum:       s.quantum,
	}, nil
}

// DRRStats holds scheduler statistics.
type DRRStats struct {
	ActiveClients []string
	Deficits      map[string]int64
	Quantum       int64
}

// Reset clears all scheduler state.
func (s *DRRScheduler) Reset(ctx context.Context) error {
	pipe := s.client.Pipeline()
	pipe.Del(ctx, drrDeficitKey)
	pipe.Del(ctx, drrActiveKey)
	pipe.Del(ctx, drrLastServed)
	_, err := pipe.Exec(ctx)
	return err
}
