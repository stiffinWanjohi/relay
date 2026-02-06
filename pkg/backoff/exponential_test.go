package backoff

import (
	"testing"
	"time"
)

func TestSchedule(t *testing.T) {
	expected := []time.Duration{
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

	if len(Schedule) != len(expected) {
		t.Fatalf("expected %d schedule entries, got %d", len(expected), len(Schedule))
	}

	for i, want := range expected {
		if Schedule[i] != want {
			t.Errorf("Schedule[%d] = %v, want %v", i, Schedule[i], want)
		}
	}
}

func TestNewCalculator(t *testing.T) {
	calc := NewCalculator()

	if calc == nil {
		t.Fatal("expected non-nil calculator")
		return
	}
	if len(calc.schedule) != len(Schedule) {
		t.Errorf("expected default schedule length %d, got %d", len(Schedule), len(calc.schedule))
	}
	if calc.jitterRatio != 0.1 {
		t.Errorf("expected default jitter ratio 0.1, got %f", calc.jitterRatio)
	}
}

func TestCalculator_WithSchedule(t *testing.T) {
	customSchedule := []time.Duration{
		100 * time.Millisecond,
		500 * time.Millisecond,
		1 * time.Second,
	}

	calc := NewCalculator().WithSchedule(customSchedule)

	if len(calc.schedule) != len(customSchedule) {
		t.Errorf("expected schedule length %d, got %d", len(customSchedule), len(calc.schedule))
	}
	for i, want := range customSchedule {
		if calc.schedule[i] != want {
			t.Errorf("schedule[%d] = %v, want %v", i, calc.schedule[i], want)
		}
	}
	// Jitter ratio should be preserved
	if calc.jitterRatio != 0.1 {
		t.Errorf("jitter ratio should be preserved, got %f", calc.jitterRatio)
	}
}

func TestCalculator_WithJitter(t *testing.T) {
	calc := NewCalculator().WithJitter(0.2)

	if calc.jitterRatio != 0.2 {
		t.Errorf("expected jitter ratio 0.2, got %f", calc.jitterRatio)
	}
	// Schedule should be preserved
	if len(calc.schedule) != len(Schedule) {
		t.Errorf("schedule should be preserved")
	}
}

func TestCalculator_WithJitter_Zero(t *testing.T) {
	calc := NewCalculator().WithJitter(0)

	// With 0 jitter, duration should be exact
	duration := calc.Duration(1)
	if duration != Schedule[0] {
		t.Errorf("with 0 jitter, expected %v, got %v", Schedule[0], duration)
	}
}

func TestCalculator_Duration_ValidAttempts(t *testing.T) {
	calc := NewCalculator().WithJitter(0) // Disable jitter for predictable tests

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 1 * time.Second},
		{2, 5 * time.Second},
		{3, 30 * time.Second},
		{4, 2 * time.Minute},
		{5, 10 * time.Minute},
		{6, 30 * time.Minute},
		{7, 1 * time.Hour},
		{8, 2 * time.Hour},
		{9, 6 * time.Hour},
		{10, 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := calc.Duration(tt.attempt)
			if got != tt.want {
				t.Errorf("Duration(%d) = %v, want %v", tt.attempt, got, tt.want)
			}
		})
	}
}

func TestCalculator_Duration_BeyondSchedule(t *testing.T) {
	calc := NewCalculator().WithJitter(0)

	// Attempts beyond schedule should return last entry
	for attempt := 11; attempt <= 20; attempt++ {
		got := calc.Duration(attempt)
		want := Schedule[len(Schedule)-1]
		if got != want {
			t.Errorf("Duration(%d) = %v, want %v", attempt, got, want)
		}
	}
}

func TestCalculator_Duration_InvalidAttempts(t *testing.T) {
	calc := NewCalculator().WithJitter(0)

	// Zero or negative attempts should be treated as 1
	for _, attempt := range []int{0, -1, -100} {
		got := calc.Duration(attempt)
		want := Schedule[0]
		if got != want {
			t.Errorf("Duration(%d) = %v, want %v", attempt, got, want)
		}
	}
}

func TestCalculator_Duration_WithJitter(t *testing.T) {
	calc := NewCalculator().WithJitter(0.1)
	baseDuration := Schedule[0] // 1 second

	// Run multiple times to check jitter bounds
	for range 100 {
		duration := calc.Duration(1)
		minAllowed := time.Duration(float64(baseDuration) * 0.9)
		maxAllowed := time.Duration(float64(baseDuration) * 1.1)

		if duration < minAllowed || duration > maxAllowed {
			t.Errorf("Duration with jitter out of bounds: got %v, expected between %v and %v",
				duration, minAllowed, maxAllowed)
		}
	}
}

func TestCalculator_Duration_JitterDistribution(t *testing.T) {
	calc := NewCalculator().WithJitter(0.1)
	baseDuration := Schedule[0]

	// Collect durations to check distribution (should be roughly uniform)
	var above, below int
	for range 1000 {
		duration := calc.Duration(1)
		if duration > baseDuration {
			above++
		} else {
			below++
		}
	}

	// Should be roughly 50/50 (allowing for randomness)
	ratio := float64(above) / float64(below)
	if ratio < 0.5 || ratio > 2.0 {
		t.Errorf("jitter distribution seems skewed: above=%d, below=%d, ratio=%.2f",
			above, below, ratio)
	}
}

func TestCalculator_NextAttemptTime(t *testing.T) {
	calc := NewCalculator().WithJitter(0)

	before := time.Now().UTC()
	nextTime := calc.NextAttemptTime(1)
	after := time.Now().UTC()

	// Next attempt time should be approximately now + 1 second
	expectedMin := before.Add(1 * time.Second)
	expectedMax := after.Add(1 * time.Second)

	if nextTime.Before(expectedMin) || nextTime.After(expectedMax) {
		t.Errorf("NextAttemptTime not in expected range: got %v, expected between %v and %v",
			nextTime, expectedMin, expectedMax)
	}
}

func TestCalculator_MaxAttempts(t *testing.T) {
	calc := NewCalculator()
	if calc.MaxAttempts() != 10 {
		t.Errorf("expected MaxAttempts 10, got %d", calc.MaxAttempts())
	}

	customCalc := calc.WithSchedule([]time.Duration{time.Second, time.Second, time.Second})
	if customCalc.MaxAttempts() != 3 {
		t.Errorf("expected MaxAttempts 3 for custom schedule, got %d", customCalc.MaxAttempts())
	}
}

func TestCalculator_ShouldRetry(t *testing.T) {
	calc := NewCalculator()

	tests := []struct {
		attempt int
		want    bool
	}{
		{0, true}, // Treat as attempt 1
		{1, true},
		{5, true},
		{9, true},
		{10, false}, // At limit
		{11, false},
		{100, false},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := calc.ShouldRetry(tt.attempt)
			if got != tt.want {
				t.Errorf("ShouldRetry(%d) = %v, want %v", tt.attempt, got, tt.want)
			}
		})
	}
}

func TestCalculator_TotalDuration(t *testing.T) {
	calc := NewCalculator()

	// Calculate expected total
	expected := time.Duration(0)
	for _, d := range Schedule {
		expected += d
	}

	// Should be approximately 34 hours based on schedule
	if expected != calc.TotalDuration() {
		t.Errorf("TotalDuration = %v, want %v", calc.TotalDuration(), expected)
	}

	// Verify actual value
	want := 1*time.Second + 5*time.Second + 30*time.Second +
		2*time.Minute + 10*time.Minute + 30*time.Minute +
		1*time.Hour + 2*time.Hour + 6*time.Hour + 24*time.Hour

	if calc.TotalDuration() != want {
		t.Errorf("TotalDuration = %v, want %v", calc.TotalDuration(), want)
	}
}

func TestCalculator_TotalDuration_CustomSchedule(t *testing.T) {
	customSchedule := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		3 * time.Second,
	}

	calc := NewCalculator().WithSchedule(customSchedule)
	want := 6 * time.Second

	if calc.TotalDuration() != want {
		t.Errorf("TotalDuration for custom schedule = %v, want %v", calc.TotalDuration(), want)
	}
}

func TestCalculator_Chaining(t *testing.T) {
	customSchedule := []time.Duration{time.Second}

	calc := NewCalculator().
		WithSchedule(customSchedule).
		WithJitter(0.5)

	if len(calc.schedule) != 1 {
		t.Errorf("expected schedule length 1, got %d", len(calc.schedule))
	}
	if calc.jitterRatio != 0.5 {
		t.Errorf("expected jitter ratio 0.5, got %f", calc.jitterRatio)
	}
}

func TestCalculator_Immutability(t *testing.T) {
	original := NewCalculator()
	originalJitter := original.jitterRatio

	_ = original.WithJitter(0.5)

	if original.jitterRatio != originalJitter {
		t.Error("WithJitter should not mutate original calculator")
	}

	originalScheduleLen := len(original.schedule)
	_ = original.WithSchedule([]time.Duration{time.Second})

	if len(original.schedule) != originalScheduleLen {
		t.Error("WithSchedule should not mutate original calculator")
	}
}

func TestCalculator_addJitter_Negative(t *testing.T) {
	calc := NewCalculator().WithJitter(-0.1) // Negative jitter

	// Should return base duration without jitter
	duration := calc.Duration(1)
	if duration != Schedule[0] {
		t.Errorf("negative jitter should be treated as no jitter, got %v", duration)
	}
}

func TestCalculator_EmptySchedule(t *testing.T) {
	calc := NewCalculator().WithSchedule([]time.Duration{})

	// Edge case: empty schedule
	// This should panic or return 0, depending on implementation
	defer func() {
		if r := recover(); r != nil {
			// Panic is acceptable for empty schedule
			t.Log("recovered from empty schedule panic as expected")
		}
	}()

	duration := calc.Duration(1)
	// If no panic, duration should be 0 or some default
	t.Logf("empty schedule returned duration: %v", duration)
}

func TestSchedule_IncreasingOrder(t *testing.T) {
	for i := 1; i < len(Schedule); i++ {
		if Schedule[i] <= Schedule[i-1] {
			t.Errorf("Schedule should be monotonically increasing, but Schedule[%d]=%v <= Schedule[%d]=%v",
				i, Schedule[i], i-1, Schedule[i-1])
		}
	}
}

func BenchmarkCalculator_Duration(b *testing.B) {
	calc := NewCalculator()
	for b.Loop() {
		calc.Duration(5)
	}
}

func BenchmarkCalculator_Duration_WithJitter(b *testing.B) {
	calc := NewCalculator().WithJitter(0.1)
	for b.Loop() {
		calc.Duration(5)
	}
}

func BenchmarkCalculator_Duration_NoJitter(b *testing.B) {
	calc := NewCalculator().WithJitter(0)
	for b.Loop() {
		calc.Duration(5)
	}
}
