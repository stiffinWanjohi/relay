package delivery

import (
	"fmt"
	"sync"
	"testing"
)

func BenchmarkCircuitBreakerIsOpen(b *testing.B) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	// Register a destination
	cb.RecordSuccess("https://example.com/webhook")

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_ = cb.IsOpen("https://example.com/webhook")
	}
}

func BenchmarkCircuitBreakerRecordSuccess(b *testing.B) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		cb.RecordSuccess("https://example.com/webhook")
	}
}

func BenchmarkCircuitBreakerRecordFailure(b *testing.B) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		cb.RecordFailure(fmt.Sprintf("https://example-%d.com/webhook", i))
		i++
	}
}

func BenchmarkCircuitBreakerMixedOperations(b *testing.B) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	destinations := make([]string, 100)
	for i := range 100 {
		destinations[i] = fmt.Sprintf("https://endpoint-%d.example.com/webhook", i)
	}

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		dest := destinations[i%len(destinations)]
		_ = cb.IsOpen(dest)
		if i%10 == 0 {
			cb.RecordFailure(dest)
		} else {
			cb.RecordSuccess(dest)
		}
		i++
	}
}

func BenchmarkCircuitBreakerParallel(b *testing.B) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	destinations := make([]string, 50)
	for i := range 50 {
		destinations[i] = fmt.Sprintf("https://endpoint-%d.example.com/webhook", i)
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			dest := destinations[i%len(destinations)]
			_ = cb.IsOpen(dest)
			if i%5 == 0 {
				cb.RecordFailure(dest)
			} else {
				cb.RecordSuccess(dest)
			}
			i++
		}
	})
}

func BenchmarkCircuitBreakerContention(b *testing.B) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	// All goroutines hit the same destination
	dest := "https://single-endpoint.example.com/webhook"

	for _, goroutines := range []int{1, 2, 4, 8, 16} {
		b.Run(fmt.Sprintf("goroutines-%d", goroutines), func(b *testing.B) {
			var wg sync.WaitGroup
			ops := b.N / goroutines

			b.ResetTimer()

			for g := range goroutines {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for range ops {
						_ = cb.IsOpen(dest)
						cb.RecordSuccess(dest)
					}
				}()
				_ = g // suppress unused warning
			}
			wg.Wait()
		})
	}
}

func BenchmarkCircuitBreakerGetState(b *testing.B) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	// Set up various states
	for i := range 50 {
		dest := fmt.Sprintf("https://endpoint-%d.example.com", i)
		if i%3 == 0 {
			// Trip some circuits
			for range 10 {
				cb.RecordFailure(dest)
			}
		} else {
			cb.RecordSuccess(dest)
		}
	}

	destinations := make([]string, 50)
	for i := range 50 {
		destinations[i] = fmt.Sprintf("https://endpoint-%d.example.com", i)
	}

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		_ = cb.GetState(destinations[i%len(destinations)])
		i++
	}
}

func BenchmarkCircuitBreakerStats(b *testing.B) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	// Populate with destinations
	for i := range 100 {
		cb.RecordSuccess(fmt.Sprintf("https://endpoint-%d.example.com", i))
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_ = cb.Stats()
	}
}
