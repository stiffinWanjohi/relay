package backoff

import (
	"math/rand"
	"time"
)

// Schedule defines the retry backoff schedule.
// Based on the specification:
// Attempt 1:  1 second
// Attempt 2:  5 seconds
// Attempt 3:  30 seconds
// Attempt 4:  2 minutes
// Attempt 5:  10 minutes
// Attempt 6:  30 minutes
// Attempt 7:  1 hour
// Attempt 8:  2 hours
// Attempt 9:  6 hours
// Attempt 10: 24 hours → dead letter
var Schedule = []time.Duration{
	1 * time.Second,
	5 * time.Second,
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
	1 * time.Hour,
	2 * time.Hour,
	6 * time.Hour,
	24 * time.Hour,
}

// Calculator computes backoff durations with optional jitter.
type Calculator struct {
	schedule    []time.Duration
	jitterRatio float64
}

// NewCalculator creates a new backoff calculator with the default schedule.
func NewCalculator() *Calculator {
	return &Calculator{
		schedule:    Schedule,
		jitterRatio: 0.1, // 10% jitter by default
	}
}

// WithSchedule sets a custom backoff schedule.
func (c *Calculator) WithSchedule(schedule []time.Duration) *Calculator {
	return &Calculator{
		schedule:    schedule,
		jitterRatio: c.jitterRatio,
	}
}

// WithJitter sets the jitter ratio (0.0 to 1.0).
func (c *Calculator) WithJitter(ratio float64) *Calculator {
	return &Calculator{
		schedule:    c.schedule,
		jitterRatio: ratio,
	}
}

// Duration returns the backoff duration for a given attempt number (1-indexed).
// Returns the last schedule entry for attempts beyond the schedule length.
func (c *Calculator) Duration(attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}

	index := attempt - 1
	if index >= len(c.schedule) {
		index = len(c.schedule) - 1
	}

	base := c.schedule[index]
	return c.addJitter(base)
}

// NextAttemptTime returns the time for the next attempt based on the current attempt count.
func (c *Calculator) NextAttemptTime(attempt int) time.Time {
	return time.Now().UTC().Add(c.Duration(attempt))
}

// MaxAttempts returns the maximum number of attempts in the schedule.
func (c *Calculator) MaxAttempts() int {
	return len(c.schedule)
}

// addJitter adds random jitter to the duration.
func (c *Calculator) addJitter(d time.Duration) time.Duration {
	if c.jitterRatio <= 0 {
		return d
	}

	jitter := float64(d) * c.jitterRatio
	// Add or subtract up to jitterRatio of the duration
	delta := (rand.Float64()*2 - 1) * jitter
	return time.Duration(float64(d) + delta)
}

// ShouldRetry returns true if the attempt count is within the retry schedule.
func (c *Calculator) ShouldRetry(attempt int) bool {
	return attempt < len(c.schedule)
}

// TotalDuration returns the total duration of all retries in the schedule.
func (c *Calculator) TotalDuration() time.Duration {
	var total time.Duration
	for _, d := range c.schedule {
		total += d
	}
	return total
}
