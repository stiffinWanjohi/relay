package queue

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func BenchmarkQueueStats(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	// Add some items to each queue type
	for i := 0; i < 100; i++ {
		_ = q.Enqueue(ctx, uuid.New())
	}
	for i := 0; i < 50; i++ {
		_ = q.EnqueueDelayed(ctx, uuid.New(), 1*time.Hour)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = q.Stats(ctx)
	}
}

func BenchmarkQueueRecoverStaleMessages(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client).WithBlockingTimeout(10 * time.Millisecond)
	ctx := context.Background()

	// Pre-populate processing queue with stale messages
	for i := 0; i < 100; i++ {
		_ = q.Enqueue(ctx, uuid.New())
		msg, _ := q.Dequeue(ctx)
		if msg != nil {
			// Leave in processing queue (simulating stale)
			_ = msg
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Recovery with a very short threshold so all messages are considered stale
		_, _ = q.RecoverStaleMessages(ctx, 1*time.Nanosecond)
	}
}

func BenchmarkQueueThroughput(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client).WithBlockingTimeout(1 * time.Millisecond)
	ctx := context.Background()

	// Use multiple goroutines to simulate real load
	for _, workers := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("workers-%d", workers), func(b *testing.B) {
			// Pre-populate
			for i := 0; i < b.N; i++ {
				_ = q.Enqueue(ctx, uuid.New())
			}

			b.ResetTimer()

			done := make(chan struct{})
			count := make(chan int, workers)

			for w := 0; w < workers; w++ {
				go func() {
					processed := 0
					for {
						select {
						case <-done:
							count <- processed
							return
						default:
							msg, err := q.Dequeue(ctx)
							if err == nil && msg != nil {
								_ = q.Ack(ctx, msg)
								processed++
							}
						}
					}
				}()
			}

			time.Sleep(time.Duration(b.N) * time.Microsecond * 10)
			close(done)

			total := 0
			for w := 0; w < workers; w++ {
				total += <-count
			}
		})
	}
}
