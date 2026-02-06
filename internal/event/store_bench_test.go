package event

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stiffinWanjohi/relay/internal/domain"
)

func BenchmarkHeadersMarshal_Empty(b *testing.B) {
	headers := make(map[string]string)

	b.ResetTimer()
	for b.Loop() {
		_, _ = json.Marshal(headers)
	}
}

func BenchmarkHeadersMarshal_Small(b *testing.B) {
	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer token123",
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = json.Marshal(headers)
	}
}

func BenchmarkHeadersMarshal_Large(b *testing.B) {
	headers := make(map[string]string)
	for i := range 20 {
		headers["X-Custom-Header-"+string(rune('A'+i))] = "value-" + string(rune('A'+i))
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = json.Marshal(headers)
	}
}

func BenchmarkHeadersUnmarshal_Small(b *testing.B) {
	data := []byte(`{"Content-Type":"application/json","Authorization":"Bearer token123"}`)
	var headers map[string]string

	b.ResetTimer()
	for b.Loop() {
		_ = json.Unmarshal(data, &headers)
	}
}

func BenchmarkPayloadMarshal_Small(b *testing.B) {
	payload := map[string]any{
		"user_id": "123",
		"action":  "purchase",
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = json.Marshal(payload)
	}
}

func BenchmarkPayloadMarshal_Medium(b *testing.B) {
	payload := map[string]any{
		"user_id":    "123",
		"action":     "purchase",
		"product_id": "456",
		"quantity":   5,
		"price":      99.99,
		"metadata": map[string]any{
			"source":    "web",
			"campaign":  "summer_sale",
			"timestamp": time.Now().Unix(),
		},
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = json.Marshal(payload)
	}
}

func BenchmarkPayloadMarshal_Large(b *testing.B) {
	items := make([]map[string]any, 100)
	for i := range 100 {
		items[i] = map[string]any{
			"product_id": uuid.New().String(),
			"name":       "Product " + string(rune('A'+i%26)),
			"quantity":   i + 1,
			"price":      float64(i) * 9.99,
		}
	}
	payload := map[string]any{
		"order_id":   uuid.New().String(),
		"user_id":    "123",
		"items":      items,
		"total":      9999.99,
		"created_at": time.Now().Format(time.RFC3339),
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = json.Marshal(payload)
	}
}

func BenchmarkNewEvent(b *testing.B) {
	idempotencyKey := "key-123"
	destination := "https://webhook.example.com/events"
	payload := json.RawMessage(`{"user_id":"123","action":"purchase"}`)
	headers := map[string]string{"Content-Type": "application/json"}

	b.ResetTimer()
	for b.Loop() {
		_ = domain.NewEvent(idempotencyKey, destination, payload, headers)
	}
}

func BenchmarkNewEventForEndpoint(b *testing.B) {
	clientID := "client-123"
	eventType := "order.created"
	idempotencyKey := "key-123"
	endpoint := domain.Endpoint{
		ID:               uuid.New(),
		ClientID:         clientID,
		URL:              "https://webhook.example.com/events",
		EventTypes:       []string{"order.created", "order.updated"},
		Status:           domain.EndpointStatusActive,
		MaxRetries:       10,
		RetryBackoffMs:   1000,
		RetryBackoffMax:  86400000,
		RetryBackoffMult: 2.0,
		TimeoutMs:        30000,
		CustomHeaders:    map[string]string{"X-Webhook-Source": "relay"},
	}
	payload := json.RawMessage(`{"user_id":"123","action":"purchase"}`)
	headers := map[string]string{"Content-Type": "application/json"}

	b.ResetTimer()
	for b.Loop() {
		_ = domain.NewEventForEndpoint(clientID, eventType, idempotencyKey, endpoint, payload, headers)
	}
}

func BenchmarkUUIDGeneration(b *testing.B) {
	for b.Loop() {
		_ = uuid.New()
	}
}

func BenchmarkUUIDToString(b *testing.B) {
	id := uuid.New()

	b.ResetTimer()
	for b.Loop() {
		_ = id.String()
	}
}

func BenchmarkNewDeliveryAttempt(b *testing.B) {
	eventID := uuid.New()

	b.ResetTimer()
	for b.Loop() {
		_ = domain.DeliveryAttempt{
			ID:            uuid.New(),
			EventID:       eventID,
			StatusCode:    200,
			ResponseBody:  "OK",
			DurationMs:    150,
			AttemptNumber: 1,
			AttemptedAt:   time.Now().UTC(),
		}
	}
}

func BenchmarkNewDeliveryAttemptWithError(b *testing.B) {
	eventID := uuid.New()
	errMsg := "connection refused"

	b.ResetTimer()
	for b.Loop() {
		_ = domain.DeliveryAttempt{
			ID:            uuid.New(),
			EventID:       eventID,
			StatusCode:    0,
			Error:         errMsg,
			DurationMs:    5000,
			AttemptNumber: 3,
			AttemptedAt:   time.Now().UTC(),
		}
	}
}

func BenchmarkNewEndpoint(b *testing.B) {
	clientID := "client-123"
	url := "https://webhook.example.com/events"
	eventTypes := []string{"order.created", "order.updated", "order.cancelled"}

	b.ResetTimer()
	for b.Loop() {
		_ = domain.NewEndpoint(clientID, url, eventTypes)
	}
}

func BenchmarkEndpointWithCustomHeaders(b *testing.B) {
	clientID := "client-123"
	url := "https://webhook.example.com/events"
	eventTypes := []string{"order.created"}
	customHeaders := map[string]string{
		"X-Webhook-Source":  "relay",
		"X-Custom-Header-1": "value1",
		"X-Custom-Header-2": "value2",
	}

	b.ResetTimer()
	for b.Loop() {
		ep := domain.NewEndpoint(clientID, url, eventTypes)
		ep.CustomHeaders = customHeaders
		_ = ep
	}
}

func BenchmarkOutboxEntryCreation(b *testing.B) {
	eventID := uuid.New()

	b.ResetTimer()
	for b.Loop() {
		_ = OutboxEntry{
			ID:        uuid.New(),
			EventID:   eventID,
			CreatedAt: time.Now().UTC(),
		}
	}
}

func BenchmarkIdempotencyKeyGeneration_UUID(b *testing.B) {
	for b.Loop() {
		_ = uuid.New().String()
	}
}

func BenchmarkIdempotencyKeyGeneration_Composite(b *testing.B) {
	baseKey := "order-123"
	endpointID := uuid.New()

	b.ResetTimer()
	for b.Loop() {
		_ = baseKey + ":" + endpointID.String()
	}
}

func BenchmarkQueueStatsCreation(b *testing.B) {
	for b.Loop() {
		_ = QueueStats{
			Queued:     100,
			Delivering: 10,
			Delivered:  5000,
			Failed:     50,
			Dead:       5,
		}
	}
}

func BenchmarkEndpointStatsCreation(b *testing.B) {
	for b.Loop() {
		stats := EndpointStats{
			TotalEvents:  1000,
			Delivered:    950,
			Failed:       40,
			Pending:      10,
			AvgLatencyMs: 125.5,
		}
		if stats.TotalEvents > 0 {
			stats.SuccessRate = float64(stats.Delivered) / float64(stats.TotalEvents)
		}
		_ = stats
	}
}

func BenchmarkMapMerge_Small(b *testing.B) {
	base := map[string]string{
		"Content-Type": "application/json",
	}
	custom := map[string]string{
		"X-Custom": "value",
	}

	b.ResetTimer()
	for b.Loop() {
		merged := make(map[string]string, len(base)+len(custom))
		maps.Copy(merged, base)
		maps.Copy(merged, custom)
		_ = merged
	}
}

func BenchmarkMapMerge_Large(b *testing.B) {
	base := make(map[string]string)
	for i := range 10 {
		base["Base-Header-"+string(rune('A'+i))] = "value"
	}
	custom := make(map[string]string)
	for i := range 10 {
		custom["Custom-Header-"+string(rune('A'+i))] = "value"
	}

	b.ResetTimer()
	for b.Loop() {
		merged := make(map[string]string, len(base)+len(custom))
		maps.Copy(merged, base)
		maps.Copy(merged, custom)
		_ = merged
	}
}

func BenchmarkSliceContains_Small(b *testing.B) {
	eventTypes := []string{"order.created", "order.updated", "order.cancelled"}
	target := "order.updated"

	b.ResetTimer()
	for b.Loop() {
		found := slices.Contains(eventTypes, target)
		_ = found
	}
}

func BenchmarkSliceContains_Large(b *testing.B) {
	eventTypes := make([]string, 50)
	for i := range 50 {
		eventTypes[i] = "event.type." + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	target := eventTypes[40]

	b.ResetTimer()
	for b.Loop() {
		found := slices.Contains(eventTypes, target)
		_ = found
	}
}

func BenchmarkSliceContainsWildcard(b *testing.B) {
	eventTypes := []string{"order.created", "*", "order.cancelled"}
	target := "user.signup"

	b.ResetTimer()
	for b.Loop() {
		found := false
		for _, et := range eventTypes {
			if et == target || et == "*" {
				found = true
				break
			}
		}
		_ = found
	}
}

func BenchmarkNewEvent_Parallel(b *testing.B) {
	idempotencyKey := "key-123"
	destination := "https://webhook.example.com/events"
	payload := json.RawMessage(`{"user_id":"123","action":"purchase"}`)
	headers := map[string]string{"Content-Type": "application/json"}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = domain.NewEvent(idempotencyKey, destination, payload, headers)
		}
	})
}

func BenchmarkJSONMarshal_Parallel(b *testing.B) {
	payload := map[string]any{
		"user_id": "123",
		"action":  "purchase",
		"amount":  99.99,
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = json.Marshal(payload)
		}
	})
}

func BenchmarkUUID_Parallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = uuid.New()
		}
	})
}

func BenchmarkNullString_Empty(b *testing.B) {
	s := ""

	b.ResetTimer()
	for b.Loop() {
		_ = nullString(s)
	}
}

func BenchmarkNullString_NonEmpty(b *testing.B) {
	s := "client-123"

	b.ResetTimer()
	for b.Loop() {
		_ = nullString(s)
	}
}
