package domain

import (
	"encoding/json"
	"testing"
)

func BenchmarkNewEvent(b *testing.B) {
	payload := json.RawMessage(`{"order_id": 12345, "amount": 99.99, "items": ["item1", "item2", "item3"]}`)
	headers := map[string]string{
		"Content-Type": "application/json",
		"X-Custom":     "value",
	}

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		_ = NewEvent("idemp-key-"+string(rune(i)), "https://example.com/webhook", payload, headers)
		i++
	}
}

func BenchmarkEventStatusTransitions(b *testing.B) {
	payload := json.RawMessage(`{"test": true}`)
	evt := NewEvent("test-key", "https://example.com/webhook", payload, nil)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		e := evt.MarkDelivering()
		e = e.IncrementAttempts()
		_ = e.MarkDelivered()
	}
}

func BenchmarkEventShouldRetry(b *testing.B) {
	payload := json.RawMessage(`{"test": true}`)
	evt := NewEvent("test-key", "https://example.com/webhook", payload, nil)
	evt.Attempts = 5
	evt.Status = EventStatusFailed

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_ = evt.ShouldRetry()
	}
}

func BenchmarkNewDeliveryAttempt(b *testing.B) {
	payload := json.RawMessage(`{"test": true}`)
	evt := NewEvent("test-key", "https://example.com/webhook", payload, nil)

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		attempt := NewDeliveryAttempt(evt.ID, i%10+1)
		_ = attempt.WithSuccess(200, `{"ok": true}`, 45)
		i++
	}
}

func BenchmarkEventJSONMarshal(b *testing.B) {
	payload := json.RawMessage(`{"order_id": 12345, "amount": 99.99, "items": ["item1", "item2", "item3"]}`)
	headers := map[string]string{"Content-Type": "application/json"}
	evt := NewEvent("test-key", "https://example.com/webhook", payload, headers)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, _ = json.Marshal(evt)
	}
}

func BenchmarkEventJSONUnmarshal(b *testing.B) {
	payload := json.RawMessage(`{"order_id": 12345, "amount": 99.99}`)
	evt := NewEvent("test-key", "https://example.com/webhook", payload, nil)
	data, _ := json.Marshal(evt)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		var e Event
		_ = json.Unmarshal(data, &e)
	}
}
