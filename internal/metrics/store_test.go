package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupTestStore(t *testing.T) (*Store, func()) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	store := NewStore(client)

	cleanup := func() {
		_ = client.Close()
		mr.Close()
	}

	return store, cleanup
}

func TestStore_RecordDelivery(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	record := DeliveryRecord{
		Timestamp:  time.Now().UTC(),
		EventID:    "evt_123",
		EndpointID: "ep_456",
		EventType:  "order.created",
		Outcome:    OutcomeSuccess,
		StatusCode: 200,
		LatencyMs:  150,
		AttemptNum: 1,
		ClientID:   "client_789",
	}

	err := store.RecordDelivery(ctx, record)
	if err != nil {
		t.Fatalf("RecordDelivery failed: %v", err)
	}

	// Verify we can retrieve it
	start := time.Now().UTC().Add(-time.Hour)
	end := time.Now().UTC().Add(time.Hour)

	records, err := store.GetDeliveryRecords(ctx, start, end, 0)
	if err != nil {
		t.Fatalf("GetDeliveryRecords failed: %v", err)
	}

	if len(records) != 1 {
		t.Errorf("expected 1 record, got %d", len(records))
	}

	if records[0].EventID != "evt_123" {
		t.Errorf("expected event_id evt_123, got %s", records[0].EventID)
	}
}

func TestStore_GetDeliveryRecordsByEndpoint(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Record deliveries for different endpoints
	_ = store.RecordDelivery(ctx, DeliveryRecord{
		Timestamp:  time.Now().UTC(),
		EventID:    "evt_1",
		EndpointID: "ep_A",
		Outcome:    OutcomeSuccess,
		LatencyMs:  100,
	})
	_ = store.RecordDelivery(ctx, DeliveryRecord{
		Timestamp:  time.Now().UTC(),
		EventID:    "evt_2",
		EndpointID: "ep_B",
		Outcome:    OutcomeSuccess,
		LatencyMs:  200,
	})
	_ = store.RecordDelivery(ctx, DeliveryRecord{
		Timestamp:  time.Now().UTC(),
		EventID:    "evt_3",
		EndpointID: "ep_A",
		Outcome:    OutcomeFailure,
		LatencyMs:  300,
	})

	start := time.Now().UTC().Add(-time.Hour)
	end := time.Now().UTC().Add(time.Hour)

	records, err := store.GetDeliveryRecordsByEndpoint(ctx, "ep_A", start, end, 0)
	if err != nil {
		t.Fatalf("GetDeliveryRecordsByEndpoint failed: %v", err)
	}

	if len(records) != 2 {
		t.Errorf("expected 2 records for ep_A, got %d", len(records))
	}
}

func TestStore_GetFailureRate(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Record 7 successes, 3 failures = 30% failure rate
	for i := range 7 {
		_ = store.RecordDelivery(ctx, DeliveryRecord{
			Timestamp:  time.Now().UTC(),
			EventID:    "success_" + string(rune('0'+i)),
			EndpointID: "ep_1",
			Outcome:    OutcomeSuccess,
			LatencyMs:  100,
		})
	}
	for i := range 3 {
		_ = store.RecordDelivery(ctx, DeliveryRecord{
			Timestamp:  time.Now().UTC(),
			EventID:    "failure_" + string(rune('0'+i)),
			EndpointID: "ep_1",
			Outcome:    OutcomeFailure,
			LatencyMs:  100,
		})
	}

	rate, err := store.GetFailureRate(ctx, time.Hour)
	if err != nil {
		t.Fatalf("GetFailureRate failed: %v", err)
	}

	expected := 0.3
	if rate < expected-0.01 || rate > expected+0.01 {
		t.Errorf("expected failure rate ~%.2f, got %.2f", expected, rate)
	}
}

func TestStore_GetSuccessRate(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Record 8 successes, 2 failures = 80% success rate
	for i := range 8 {
		_ = store.RecordDelivery(ctx, DeliveryRecord{
			Timestamp:  time.Now().UTC(),
			EventID:    "success_" + string(rune('0'+i)),
			EndpointID: "ep_1",
			Outcome:    OutcomeSuccess,
			LatencyMs:  100,
		})
	}
	for i := range 2 {
		_ = store.RecordDelivery(ctx, DeliveryRecord{
			Timestamp:  time.Now().UTC(),
			EventID:    "failure_" + string(rune('0'+i)),
			EndpointID: "ep_1",
			Outcome:    OutcomeFailure,
			LatencyMs:  100,
		})
	}

	rate, err := store.GetSuccessRate(ctx, time.Hour)
	if err != nil {
		t.Fatalf("GetSuccessRate failed: %v", err)
	}

	expected := 0.8
	if rate < expected-0.01 || rate > expected+0.01 {
		t.Errorf("expected success rate ~%.2f, got %.2f", expected, rate)
	}
}

func TestStore_GetAverageLatency(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Record deliveries with latencies: 100, 200, 300, 400 = avg 250
	latencies := []int64{100, 200, 300, 400}
	for i, lat := range latencies {
		_ = store.RecordDelivery(ctx, DeliveryRecord{
			Timestamp:  time.Now().UTC(),
			EventID:    "evt_" + string(rune('0'+i)),
			EndpointID: "ep_1",
			Outcome:    OutcomeSuccess,
			LatencyMs:  lat,
		})
	}

	avg, err := store.GetAverageLatency(ctx, time.Hour)
	if err != nil {
		t.Fatalf("GetAverageLatency failed: %v", err)
	}

	expected := 250.0
	if avg != expected {
		t.Errorf("expected average latency %.2f, got %.2f", expected, avg)
	}
}

func TestStore_GetLatencyPercentiles(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Record 100 deliveries with latencies 1-100ms
	for i := 1; i <= 100; i++ {
		_ = store.RecordDelivery(ctx, DeliveryRecord{
			Timestamp:  time.Now().UTC(),
			EventID:    "evt_" + string(rune(i)),
			EndpointID: "ep_1",
			Outcome:    OutcomeSuccess,
			LatencyMs:  int64(i),
		})
	}

	p50, p95, p99, err := store.GetLatencyPercentiles(ctx, time.Hour)
	if err != nil {
		t.Fatalf("GetLatencyPercentiles failed: %v", err)
	}

	// With 100 values from 1-100:
	// p50 should be ~50, p95 should be ~95, p99 should be ~99
	if p50 < 45 || p50 > 55 {
		t.Errorf("expected p50 ~50, got %.2f", p50)
	}
	if p95 < 90 || p95 > 100 {
		t.Errorf("expected p95 ~95, got %.2f", p95)
	}
	if p99 < 95 || p99 > 100 {
		t.Errorf("expected p99 ~99, got %.2f", p99)
	}
}

func TestStore_GetStats(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Record mixed outcomes
	_ = store.RecordDelivery(ctx, DeliveryRecord{
		Timestamp: time.Now().UTC(), EventID: "1", EndpointID: "ep", Outcome: OutcomeSuccess, LatencyMs: 100,
	})
	_ = store.RecordDelivery(ctx, DeliveryRecord{
		Timestamp: time.Now().UTC(), EventID: "2", EndpointID: "ep", Outcome: OutcomeSuccess, LatencyMs: 200,
	})
	_ = store.RecordDelivery(ctx, DeliveryRecord{
		Timestamp: time.Now().UTC(), EventID: "3", EndpointID: "ep", Outcome: OutcomeFailure, LatencyMs: 300,
	})
	_ = store.RecordDelivery(ctx, DeliveryRecord{
		Timestamp: time.Now().UTC(), EventID: "4", EndpointID: "ep", Outcome: OutcomeTimeout, LatencyMs: 5000,
	})

	start := time.Now().UTC().Add(-time.Hour)
	end := time.Now().UTC().Add(time.Hour)

	stats, err := store.GetStats(ctx, start, end)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.TotalCount != 4 {
		t.Errorf("expected total_count 4, got %d", stats.TotalCount)
	}
	if stats.SuccessCount != 2 {
		t.Errorf("expected success_count 2, got %d", stats.SuccessCount)
	}
	if stats.FailureCount != 1 {
		t.Errorf("expected failure_count 1, got %d", stats.FailureCount)
	}
	if stats.TimeoutCount != 1 {
		t.Errorf("expected timeout_count 1, got %d", stats.TimeoutCount)
	}
	if stats.SuccessRate != 0.5 {
		t.Errorf("expected success_rate 0.5, got %.2f", stats.SuccessRate)
	}
	if stats.MinLatencyMs != 100 {
		t.Errorf("expected min_latency 100, got %d", stats.MinLatencyMs)
	}
	if stats.MaxLatencyMs != 5000 {
		t.Errorf("expected max_latency 5000, got %d", stats.MaxLatencyMs)
	}
}

func TestStore_GetBreakdownByEventType(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Record events of different types
	for i := range 5 {
		_ = store.RecordDelivery(ctx, DeliveryRecord{
			Timestamp: time.Now().UTC(), EventID: "order_" + string(rune('0'+i)),
			EndpointID: "ep", EventType: "order.created", Outcome: OutcomeSuccess, LatencyMs: 100,
		})
	}
	for i := range 3 {
		_ = store.RecordDelivery(ctx, DeliveryRecord{
			Timestamp: time.Now().UTC(), EventID: "user_" + string(rune('0'+i)),
			EndpointID: "ep", EventType: "user.created", Outcome: OutcomeSuccess, LatencyMs: 150,
		})
	}
	_ = store.RecordDelivery(ctx, DeliveryRecord{
		Timestamp: time.Now().UTC(), EventID: "payment",
		EndpointID: "ep", EventType: "payment.failed", Outcome: OutcomeFailure, LatencyMs: 200,
	})

	start := time.Now().UTC().Add(-time.Hour)
	end := time.Now().UTC().Add(time.Hour)

	breakdown, err := store.GetBreakdownByEventType(ctx, start, end, 10)
	if err != nil {
		t.Fatalf("GetBreakdownByEventType failed: %v", err)
	}

	if len(breakdown) != 3 {
		t.Errorf("expected 3 event types, got %d", len(breakdown))
	}

	// Should be sorted by count descending
	if breakdown[0].Key != "order.created" || breakdown[0].Count != 5 {
		t.Errorf("expected first item to be order.created with count 5, got %s with %d", breakdown[0].Key, breakdown[0].Count)
	}
}

func TestStore_GetBreakdownByEndpoint(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Record deliveries to different endpoints
	for i := range 10 {
		_ = store.RecordDelivery(ctx, DeliveryRecord{
			Timestamp: time.Now().UTC(), EventID: "a_" + string(rune('0'+i)),
			EndpointID: "ep_popular", Outcome: OutcomeSuccess, LatencyMs: 100,
		})
	}
	for i := range 2 {
		_ = store.RecordDelivery(ctx, DeliveryRecord{
			Timestamp: time.Now().UTC(), EventID: "b_" + string(rune('0'+i)),
			EndpointID: "ep_quiet", Outcome: OutcomeSuccess, LatencyMs: 100,
		})
	}

	start := time.Now().UTC().Add(-time.Hour)
	end := time.Now().UTC().Add(time.Hour)

	breakdown, err := store.GetBreakdownByEndpoint(ctx, start, end, 10)
	if err != nil {
		t.Fatalf("GetBreakdownByEndpoint failed: %v", err)
	}

	if len(breakdown) != 2 {
		t.Errorf("expected 2 endpoints, got %d", len(breakdown))
	}

	if breakdown[0].Key != "ep_popular" || breakdown[0].Count != 10 {
		t.Errorf("expected first item to be ep_popular with count 10")
	}
}

func TestStore_GetSuccessRateTimeSeries(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()

	// Record deliveries at different times
	// Hour 0: 100% success
	for i := range 5 {
		_ = store.RecordDelivery(ctx, DeliveryRecord{
			Timestamp: now.Add(-2 * time.Hour), EventID: "h0_" + string(rune('0'+i)),
			EndpointID: "ep", Outcome: OutcomeSuccess, LatencyMs: 100,
		})
	}

	// Hour 1: 50% success
	for i := range 2 {
		_ = store.RecordDelivery(ctx, DeliveryRecord{
			Timestamp: now.Add(-time.Hour), EventID: "h1_s_" + string(rune('0'+i)),
			EndpointID: "ep", Outcome: OutcomeSuccess, LatencyMs: 100,
		})
	}
	for i := range 2 {
		_ = store.RecordDelivery(ctx, DeliveryRecord{
			Timestamp: now.Add(-time.Hour), EventID: "h1_f_" + string(rune('0'+i)),
			EndpointID: "ep", Outcome: OutcomeFailure, LatencyMs: 100,
		})
	}

	start := now.Add(-3 * time.Hour)
	end := now

	series, err := store.GetSuccessRateTimeSeries(ctx, start, end, time.Hour)
	if err != nil {
		t.Fatalf("GetSuccessRateTimeSeries failed: %v", err)
	}

	if len(series) != 3 {
		t.Errorf("expected 3 points, got %d", len(series))
	}
}

func TestStore_QueueDepth(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Initially should be 0
	depth, err := store.GetQueueDepth(ctx)
	if err != nil {
		t.Fatalf("GetQueueDepth failed: %v", err)
	}
	if depth != 0 {
		t.Errorf("expected initial depth 0, got %d", depth)
	}

	// Set depth
	err = store.SetQueueDepth(ctx, 42)
	if err != nil {
		t.Fatalf("SetQueueDepth failed: %v", err)
	}

	depth, err = store.GetQueueDepth(ctx)
	if err != nil {
		t.Fatalf("GetQueueDepth failed: %v", err)
	}
	if depth != 42 {
		t.Errorf("expected depth 42, got %d", depth)
	}
}

func TestStore_GetErrorCount(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// 3 successes, 2 failures, 1 timeout = 3 errors
	for i := range 3 {
		_ = store.RecordDelivery(ctx, DeliveryRecord{
			Timestamp: time.Now().UTC(), EventID: "s" + string(rune('0'+i)),
			EndpointID: "ep", Outcome: OutcomeSuccess, LatencyMs: 100,
		})
	}
	for i := range 2 {
		_ = store.RecordDelivery(ctx, DeliveryRecord{
			Timestamp: time.Now().UTC(), EventID: "f" + string(rune('0'+i)),
			EndpointID: "ep", Outcome: OutcomeFailure, LatencyMs: 100,
		})
	}
	_ = store.RecordDelivery(ctx, DeliveryRecord{
		Timestamp: time.Now().UTC(), EventID: "t",
		EndpointID: "ep", Outcome: OutcomeTimeout, LatencyMs: 5000,
	})

	count, err := store.GetErrorCount(ctx, time.Hour)
	if err != nil {
		t.Fatalf("GetErrorCount failed: %v", err)
	}

	if count != 3 {
		t.Errorf("expected 3 errors, got %d", count)
	}
}

func TestStore_GetDeliveryCount(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	for i := range 7 {
		_ = store.RecordDelivery(ctx, DeliveryRecord{
			Timestamp: time.Now().UTC(), EventID: "e" + string(rune('0'+i)),
			EndpointID: "ep", Outcome: OutcomeSuccess, LatencyMs: 100,
		})
	}

	count, err := store.GetDeliveryCount(ctx, time.Hour)
	if err != nil {
		t.Fatalf("GetDeliveryCount failed: %v", err)
	}

	if count != 7 {
		t.Errorf("expected 7 deliveries, got %d", count)
	}
}

func TestStore_EmptyResults(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// All methods should handle empty data gracefully
	rate, err := store.GetFailureRate(ctx, time.Hour)
	if err != nil || rate != 0 {
		t.Errorf("expected failure rate 0 for empty data, got %.2f, err: %v", rate, err)
	}

	rate, err = store.GetSuccessRate(ctx, time.Hour)
	if err != nil || rate != 1 { // 1 - 0 = 1
		t.Errorf("expected success rate 1 for empty data, got %.2f, err: %v", rate, err)
	}

	avg, err := store.GetAverageLatency(ctx, time.Hour)
	if err != nil || avg != 0 {
		t.Errorf("expected avg latency 0 for empty data, got %.2f, err: %v", avg, err)
	}

	p50, p95, p99, err := store.GetLatencyPercentiles(ctx, time.Hour)
	if err != nil || p50 != 0 || p95 != 0 || p99 != 0 {
		t.Errorf("expected all percentiles 0 for empty data")
	}

	count, err := store.GetDeliveryCount(ctx, time.Hour)
	if err != nil || count != 0 {
		t.Errorf("expected count 0 for empty data, got %d, err: %v", count, err)
	}
}
