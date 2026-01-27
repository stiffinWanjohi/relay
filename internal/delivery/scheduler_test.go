package delivery

import (
	"context"
	"slices"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupSchedulerTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	return mr, client
}

func TestDefaultDRRConfig(t *testing.T) {
	config := DefaultDRRConfig()

	if config.Quantum != 65536 {
		t.Errorf("Quantum = %d, want 65536", config.Quantum)
	}
}

func TestNewDRRScheduler(t *testing.T) {
	_, client := setupSchedulerTestRedis(t)
	defer client.Close()

	t.Run("with default config", func(t *testing.T) {
		config := DefaultDRRConfig()
		scheduler := NewDRRScheduler(client, config)

		if scheduler == nil {
			t.Fatal("NewDRRScheduler returned nil")
		}
		if scheduler.client != client {
			t.Error("client not set correctly")
		}
		if scheduler.quantum != config.Quantum {
			t.Errorf("quantum = %d, want %d", scheduler.quantum, config.Quantum)
		}
	})

	t.Run("with zero quantum uses default", func(t *testing.T) {
		config := DRRConfig{Quantum: 0}
		scheduler := NewDRRScheduler(client, config)

		expectedQuantum := DefaultDRRConfig().Quantum
		if scheduler.quantum != expectedQuantum {
			t.Errorf("quantum = %d, want %d (default)", scheduler.quantum, expectedQuantum)
		}
	})

	t.Run("with negative quantum uses default", func(t *testing.T) {
		config := DRRConfig{Quantum: -100}
		scheduler := NewDRRScheduler(client, config)

		expectedQuantum := DefaultDRRConfig().Quantum
		if scheduler.quantum != expectedQuantum {
			t.Errorf("quantum = %d, want %d (default)", scheduler.quantum, expectedQuantum)
		}
	})

	t.Run("with custom quantum", func(t *testing.T) {
		config := DRRConfig{Quantum: 1024}
		scheduler := NewDRRScheduler(client, config)

		if scheduler.quantum != 1024 {
			t.Errorf("quantum = %d, want 1024", scheduler.quantum)
		}
	})
}

func TestDRRScheduler_RegisterClient(t *testing.T) {
	_, client := setupSchedulerTestRedis(t)
	defer client.Close()

	scheduler := NewDRRScheduler(client, DefaultDRRConfig())
	ctx := context.Background()

	err := scheduler.RegisterClient(ctx, "client-1")
	if err != nil {
		t.Fatalf("RegisterClient error: %v", err)
	}

	// Verify client is in active set
	members, err := client.SMembers(ctx, drrActiveKey).Result()
	if err != nil {
		t.Fatalf("SMembers error: %v", err)
	}

	if !slices.Contains(members, "client-1") {
		t.Error("client-1 not found in active set")
	}
}

func TestDRRScheduler_UnregisterClient(t *testing.T) {
	_, client := setupSchedulerTestRedis(t)
	defer client.Close()

	scheduler := NewDRRScheduler(client, DefaultDRRConfig())
	ctx := context.Background()

	// Register first
	scheduler.RegisterClient(ctx, "client-1")

	// Unregister
	err := scheduler.UnregisterClient(ctx, "client-1")
	if err != nil {
		t.Fatalf("UnregisterClient error: %v", err)
	}

	// Verify client is not in active set
	members, _ := client.SMembers(ctx, drrActiveKey).Result()
	if slices.Contains(members, "client-1") {
		t.Error("client-1 should not be in active set after unregister")
	}

	// Verify deficit is also removed
	exists, _ := client.HExists(ctx, drrDeficitKey, "client-1").Result()
	if exists {
		t.Error("client-1 deficit should be removed after unregister")
	}
}

func TestDRRScheduler_SelectNextClient_NoClients(t *testing.T) {
	_, client := setupSchedulerTestRedis(t)
	defer client.Close()

	scheduler := NewDRRScheduler(client, DefaultDRRConfig())
	ctx := context.Background()

	clientID, err := scheduler.SelectNextClient(ctx)
	if err != nil {
		t.Fatalf("SelectNextClient error: %v", err)
	}
	if clientID != "" {
		t.Errorf("expected empty string for no clients, got %q", clientID)
	}
}

func TestDRRScheduler_SelectNextClient_SingleClient(t *testing.T) {
	_, client := setupSchedulerTestRedis(t)
	defer client.Close()

	scheduler := NewDRRScheduler(client, DefaultDRRConfig())
	ctx := context.Background()

	scheduler.RegisterClient(ctx, "client-1")

	clientID, err := scheduler.SelectNextClient(ctx)
	if err != nil {
		t.Fatalf("SelectNextClient error: %v", err)
	}
	if clientID != "client-1" {
		t.Errorf("expected client-1, got %q", clientID)
	}
}

func TestDRRScheduler_SelectNextClient_MultipleClients(t *testing.T) {
	_, client := setupSchedulerTestRedis(t)
	defer client.Close()

	scheduler := NewDRRScheduler(client, DefaultDRRConfig())
	ctx := context.Background()

	// Register multiple clients
	scheduler.RegisterClient(ctx, "client-1")
	scheduler.RegisterClient(ctx, "client-2")
	scheduler.RegisterClient(ctx, "client-3")

	// Select should return one of the clients
	clientID, err := scheduler.SelectNextClient(ctx)
	if err != nil {
		t.Fatalf("SelectNextClient error: %v", err)
	}

	validClients := map[string]bool{"client-1": true, "client-2": true, "client-3": true}
	if !validClients[clientID] {
		t.Errorf("unexpected client ID: %q", clientID)
	}
}

func TestDRRScheduler_SelectNextClient_RoundRobin(t *testing.T) {
	_, client := setupSchedulerTestRedis(t)
	defer client.Close()

	config := DRRConfig{Quantum: 1000} // Small quantum for testing
	scheduler := NewDRRScheduler(client, config)
	ctx := context.Background()

	// Register clients
	scheduler.RegisterClient(ctx, "client-1")
	scheduler.RegisterClient(ctx, "client-2")

	// Track which clients are selected
	selections := make(map[string]int)

	// Make multiple selections with deliveries to drain deficit
	for range 10 {
		clientID, _ := scheduler.SelectNextClient(ctx)
		if clientID != "" {
			selections[clientID]++
			// Record delivery to consume deficit
			scheduler.RecordDelivery(ctx, clientID, 500)
		}
	}

	// Both clients should have been selected
	if selections["client-1"] == 0 {
		t.Error("client-1 was never selected")
	}
	if selections["client-2"] == 0 {
		t.Error("client-2 was never selected")
	}
}

func TestDRRScheduler_RecordDelivery(t *testing.T) {
	_, client := setupSchedulerTestRedis(t)
	defer client.Close()

	config := DRRConfig{Quantum: 1000}
	scheduler := NewDRRScheduler(client, config)
	ctx := context.Background()

	scheduler.RegisterClient(ctx, "client-1")

	// Select to initialize deficit
	scheduler.SelectNextClient(ctx)

	// Get initial deficit
	initialDeficit, _ := scheduler.GetDeficit(ctx, "client-1")

	// Record delivery
	payloadSize := int64(200)
	err := scheduler.RecordDelivery(ctx, "client-1", payloadSize)
	if err != nil {
		t.Fatalf("RecordDelivery error: %v", err)
	}

	// Deficit should be reduced
	newDeficit, _ := scheduler.GetDeficit(ctx, "client-1")
	expectedDeficit := initialDeficit - payloadSize
	if expectedDeficit < 0 {
		expectedDeficit = 0
	}

	if newDeficit != expectedDeficit {
		t.Errorf("deficit = %d, want %d", newDeficit, expectedDeficit)
	}
}

func TestDRRScheduler_RecordDelivery_NegativeDeficitCappedAtZero(t *testing.T) {
	_, client := setupSchedulerTestRedis(t)
	defer client.Close()

	config := DRRConfig{Quantum: 100}
	scheduler := NewDRRScheduler(client, config)
	ctx := context.Background()

	scheduler.RegisterClient(ctx, "client-1")
	scheduler.SelectNextClient(ctx) // Initialize with quantum (100)

	// Record delivery larger than quantum
	err := scheduler.RecordDelivery(ctx, "client-1", 500)
	if err != nil {
		t.Fatalf("RecordDelivery error: %v", err)
	}

	// Deficit should be capped at 0, not negative
	deficit, _ := scheduler.GetDeficit(ctx, "client-1")
	if deficit != 0 {
		t.Errorf("deficit = %d, want 0 (capped)", deficit)
	}
}

func TestDRRScheduler_GetDeficit(t *testing.T) {
	_, client := setupSchedulerTestRedis(t)
	defer client.Close()

	scheduler := NewDRRScheduler(client, DefaultDRRConfig())
	ctx := context.Background()

	t.Run("non-existent client returns 0", func(t *testing.T) {
		deficit, err := scheduler.GetDeficit(ctx, "non-existent")
		if err != nil {
			t.Fatalf("GetDeficit error: %v", err)
		}
		if deficit != 0 {
			t.Errorf("deficit = %d, want 0", deficit)
		}
	})

	t.Run("existing client returns deficit", func(t *testing.T) {
		scheduler.RegisterClient(ctx, "client-1")
		scheduler.SelectNextClient(ctx) // Initialize deficit

		deficit, err := scheduler.GetDeficit(ctx, "client-1")
		if err != nil {
			t.Fatalf("GetDeficit error: %v", err)
		}
		if deficit <= 0 {
			t.Errorf("deficit = %d, want > 0", deficit)
		}
	})
}

func TestDRRScheduler_Stats(t *testing.T) {
	_, client := setupSchedulerTestRedis(t)
	defer client.Close()

	config := DRRConfig{Quantum: 1000}
	scheduler := NewDRRScheduler(client, config)
	ctx := context.Background()

	// Register clients
	scheduler.RegisterClient(ctx, "client-1")
	scheduler.RegisterClient(ctx, "client-2")

	// Initialize deficits
	scheduler.SelectNextClient(ctx)

	stats, err := scheduler.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats error: %v", err)
	}

	if len(stats.ActiveClients) != 2 {
		t.Errorf("ActiveClients count = %d, want 2", len(stats.ActiveClients))
	}

	if stats.Quantum != 1000 {
		t.Errorf("Quantum = %d, want 1000", stats.Quantum)
	}

	// Check deficits map
	if len(stats.Deficits) != 2 {
		t.Errorf("Deficits count = %d, want 2", len(stats.Deficits))
	}
}

func TestDRRScheduler_Reset(t *testing.T) {
	_, client := setupSchedulerTestRedis(t)
	defer client.Close()

	scheduler := NewDRRScheduler(client, DefaultDRRConfig())
	ctx := context.Background()

	// Register and select
	scheduler.RegisterClient(ctx, "client-1")
	scheduler.RegisterClient(ctx, "client-2")
	scheduler.SelectNextClient(ctx)

	// Reset
	err := scheduler.Reset(ctx)
	if err != nil {
		t.Fatalf("Reset error: %v", err)
	}

	// Verify all state is cleared
	members, _ := client.SMembers(ctx, drrActiveKey).Result()
	if len(members) != 0 {
		t.Errorf("active clients not cleared: %v", members)
	}

	deficits, _ := client.HGetAll(ctx, drrDeficitKey).Result()
	if len(deficits) != 0 {
		t.Errorf("deficits not cleared: %v", deficits)
	}
}

func TestDRRScheduler_SelectNextClient_AllZeroDeficit(t *testing.T) {
	_, client := setupSchedulerTestRedis(t)
	defer client.Close()

	config := DRRConfig{Quantum: 100}
	scheduler := NewDRRScheduler(client, config)
	ctx := context.Background()

	// Register clients
	scheduler.RegisterClient(ctx, "client-1")
	scheduler.RegisterClient(ctx, "client-2")

	// Initialize and exhaust deficits
	for range 10 {
		clientID, _ := scheduler.SelectNextClient(ctx)
		if clientID != "" {
			scheduler.RecordDelivery(ctx, clientID, 200) // Larger than quantum
		}
	}

	// All deficits should be 0, but selecting should add quantum and return a client
	clientID, err := scheduler.SelectNextClient(ctx)
	if err != nil {
		t.Fatalf("SelectNextClient error: %v", err)
	}
	if clientID == "" {
		t.Error("expected a client to be selected after quantum refresh")
	}
}

func TestDRRScheduler_ConcurrentAccess(t *testing.T) {
	_, client := setupSchedulerTestRedis(t)
	defer client.Close()

	scheduler := NewDRRScheduler(client, DefaultDRRConfig())
	ctx := context.Background()

	// Register clients
	for i := range 5 {
		scheduler.RegisterClient(ctx, "client-"+string(rune('A'+i)))
	}

	done := make(chan bool, 100)

	// Concurrent selections
	for range 50 {
		go func() {
			scheduler.SelectNextClient(ctx)
			done <- true
		}()
	}

	// Concurrent deliveries
	for range 50 {
		go func() {
			scheduler.RecordDelivery(ctx, "client-A", 100)
			done <- true
		}()
	}

	// Wait for all
	for range 100 {
		<-done
	}

	// Should not panic or deadlock
	stats, err := scheduler.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats error after concurrent access: %v", err)
	}
	if len(stats.ActiveClients) != 5 {
		t.Errorf("ActiveClients = %d, want 5", len(stats.ActiveClients))
	}
}

func TestDRRScheduler_SelectNextClient_RedisError(t *testing.T) {
	mr, client := setupSchedulerTestRedis(t)

	scheduler := NewDRRScheduler(client, DefaultDRRConfig())
	ctx := context.Background()

	// Close redis
	mr.Close()
	client.Close()

	clientID, err := scheduler.SelectNextClient(ctx)
	// Should return error or empty string, but not panic
	if clientID != "" && err == nil {
		t.Error("expected error or empty result when Redis is down")
	}
}
