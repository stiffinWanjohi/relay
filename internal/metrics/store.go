package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/stiffinWanjohi/relay/internal/logging"
)

var log = logging.Component("metrics")

// Redis key prefixes
const (
	keyPrefixDelivery     = "metrics:delivery:"      // Sorted set: timestamp -> delivery record
	keyPrefixLatency      = "metrics:latency:"       // Sorted set: timestamp -> latency value
	keyPrefixHourlyRollup = "metrics:rollup:hourly:" // Hash: hour -> aggregated stats
	keyPrefixDailyRollup  = "metrics:rollup:daily:"  // Hash: day -> aggregated stats
	keyPrefixCounters     = "metrics:counters:"      // Hash: metric name -> count
	keyPrefixRateLimit    = "metrics:ratelimit:"     // Sorted set: timestamp -> rate limit event

	// TTLs
	rawMetricsTTL   = 24 * time.Hour      // Keep raw data for 24 hours
	hourlyRollupTTL = 7 * 24 * time.Hour  // Keep hourly rollups for 7 days
	dailyRollupTTL  = 90 * 24 * time.Hour // Keep daily rollups for 90 days
)

// DeliveryOutcome represents the result of a webhook delivery attempt.
type DeliveryOutcome string

const (
	OutcomeSuccess DeliveryOutcome = "success"
	OutcomeFailure DeliveryOutcome = "failure"
	OutcomeTimeout DeliveryOutcome = "timeout"
)

// DeliveryRecord captures a single delivery attempt.
type DeliveryRecord struct {
	Timestamp  time.Time       `json:"ts"`
	EventID    string          `json:"event_id"`
	EndpointID string          `json:"endpoint_id"`
	EventType  string          `json:"event_type"`
	Outcome    DeliveryOutcome `json:"outcome"`
	StatusCode int             `json:"status_code"`
	LatencyMs  int64           `json:"latency_ms"`
	Error      string          `json:"error,omitempty"`
	AttemptNum int             `json:"attempt"`
	ClientID   string          `json:"client_id"`
}

// AggregatedStats holds pre-computed statistics for a time period.
type AggregatedStats struct {
	Period       string  `json:"period"` // "2024-01-15T10:00:00Z" for hourly, "2024-01-15" for daily
	TotalCount   int64   `json:"total_count"`
	SuccessCount int64   `json:"success_count"`
	FailureCount int64   `json:"failure_count"`
	TimeoutCount int64   `json:"timeout_count"`
	SuccessRate  float64 `json:"success_rate"`
	FailureRate  float64 `json:"failure_rate"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	P50LatencyMs float64 `json:"p50_latency_ms"`
	P95LatencyMs float64 `json:"p95_latency_ms"`
	P99LatencyMs float64 `json:"p99_latency_ms"`
	MinLatencyMs int64   `json:"min_latency_ms"`
	MaxLatencyMs int64   `json:"max_latency_ms"`
}

// TimeSeriesPoint represents a single point in a time series.
type TimeSeriesPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

// BreakdownItem represents a breakdown by dimension.
type BreakdownItem struct {
	Key         string  `json:"key"`
	Count       int64   `json:"count"`
	SuccessRate float64 `json:"success_rate"`
	AvgLatency  float64 `json:"avg_latency_ms"`
}

// RateLimitEvent captures a rate limit occurrence.
type RateLimitEvent struct {
	Timestamp  time.Time `json:"ts"`
	EndpointID string    `json:"endpoint_id"`
	EventID    string    `json:"event_id"`
	Limit      int       `json:"limit"`
}

// Store provides metrics storage and querying capabilities.
type Store struct {
	client *redis.Client
}

// NewStore creates a new metrics store.
func NewStore(client *redis.Client) *Store {
	return &Store{client: client}
}

// RecordDelivery records a delivery attempt.
func (s *Store) RecordDelivery(ctx context.Context, record DeliveryRecord) error {
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	}

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}

	score := float64(record.Timestamp.UnixMilli())
	key := keyPrefixDelivery + "all"

	pipe := s.client.Pipeline()

	// Store in main sorted set
	pipe.ZAdd(ctx, key, redis.Z{Score: score, Member: string(data)})
	pipe.Expire(ctx, key, rawMetricsTTL)

	// Store by endpoint
	endpointKey := keyPrefixDelivery + "endpoint:" + record.EndpointID
	pipe.ZAdd(ctx, endpointKey, redis.Z{Score: score, Member: string(data)})
	pipe.Expire(ctx, endpointKey, rawMetricsTTL)

	// Store by event type
	if record.EventType != "" {
		eventTypeKey := keyPrefixDelivery + "event_type:" + record.EventType
		pipe.ZAdd(ctx, eventTypeKey, redis.Z{Score: score, Member: string(data)})
		pipe.Expire(ctx, eventTypeKey, rawMetricsTTL)
	}

	// Store by client
	if record.ClientID != "" {
		clientKey := keyPrefixDelivery + "client:" + record.ClientID
		pipe.ZAdd(ctx, clientKey, redis.Z{Score: score, Member: string(data)})
		pipe.Expire(ctx, clientKey, rawMetricsTTL)
	}

	// Store latency for percentile calculations
	latencyKey := keyPrefixLatency + "all"
	latencyData := fmt.Sprintf("%d:%d", record.Timestamp.UnixMilli(), record.LatencyMs)
	pipe.ZAdd(ctx, latencyKey, redis.Z{Score: score, Member: latencyData})
	pipe.Expire(ctx, latencyKey, rawMetricsTTL)

	// Increment counters
	counterKey := keyPrefixCounters + "totals"
	pipe.HIncrBy(ctx, counterKey, "total", 1)
	pipe.HIncrBy(ctx, counterKey, string(record.Outcome), 1)

	_, err = pipe.Exec(ctx)
	if err != nil {
		log.Error("failed to record delivery", "error", err, "event_id", record.EventID)
		return fmt.Errorf("record delivery: %w", err)
	}

	log.Debug("recorded delivery",
		"event_id", record.EventID,
		"outcome", record.Outcome,
		"latency_ms", record.LatencyMs,
	)

	return nil
}

// RecordRateLimit records a rate limit event.
func (s *Store) RecordRateLimit(ctx context.Context, event RateLimitEvent) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal rate limit event: %w", err)
	}

	score := float64(event.Timestamp.UnixMilli())

	pipe := s.client.Pipeline()

	// Store in main sorted set
	key := keyPrefixRateLimit + "all"
	pipe.ZAdd(ctx, key, redis.Z{Score: score, Member: string(data)})
	pipe.Expire(ctx, key, rawMetricsTTL)

	// Store by endpoint
	if event.EndpointID != "" {
		endpointKey := keyPrefixRateLimit + "endpoint:" + event.EndpointID
		pipe.ZAdd(ctx, endpointKey, redis.Z{Score: score, Member: string(data)})
		pipe.Expire(ctx, endpointKey, rawMetricsTTL)
	}

	// Increment counter
	pipe.HIncrBy(ctx, keyPrefixCounters+"totals", "rate_limited", 1)

	_, err = pipe.Exec(ctx)
	if err != nil {
		log.Error("failed to record rate limit", "error", err, "endpoint_id", event.EndpointID)
		return fmt.Errorf("record rate limit: %w", err)
	}

	log.Debug("recorded rate limit",
		"endpoint_id", event.EndpointID,
		"event_id", event.EventID,
		"limit", event.Limit,
	)

	return nil
}

// GetRateLimitEvents retrieves rate limit events within a time window.
func (s *Store) GetRateLimitEvents(ctx context.Context, start, end time.Time, limit int64) ([]RateLimitEvent, error) {
	key := keyPrefixRateLimit + "all"
	return s.getRateLimitEventsFromKey(ctx, key, start, end, limit)
}

// GetRateLimitEventsByEndpoint retrieves rate limit events for a specific endpoint.
func (s *Store) GetRateLimitEventsByEndpoint(ctx context.Context, endpointID string, start, end time.Time, limit int64) ([]RateLimitEvent, error) {
	key := keyPrefixRateLimit + "endpoint:" + endpointID
	return s.getRateLimitEventsFromKey(ctx, key, start, end, limit)
}

// GetRateLimitCount returns the total number of rate limit events.
func (s *Store) GetRateLimitCount(ctx context.Context) (int64, error) {
	count, err := s.client.HGet(ctx, keyPrefixCounters+"totals", "rate_limited").Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return count, err
}

func (s *Store) getRateLimitEventsFromKey(ctx context.Context, key string, start, end time.Time, limit int64) ([]RateLimitEvent, error) {
	startScore := strconv.FormatInt(start.UnixMilli(), 10)
	endScore := strconv.FormatInt(end.UnixMilli(), 10)

	opt := &redis.ZRangeBy{
		Min:   startScore,
		Max:   endScore,
		Count: limit,
	}

	results, err := s.client.ZRangeByScore(ctx, key, opt).Result()
	if err != nil {
		return nil, fmt.Errorf("get rate limit events: %w", err)
	}

	events := make([]RateLimitEvent, 0, len(results))
	for _, data := range results {
		var event RateLimitEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			log.Warn("failed to unmarshal rate limit event", "error", err)
			continue
		}
		events = append(events, event)
	}

	return events, nil
}

// GetDeliveryRecords retrieves delivery records within a time window.
func (s *Store) GetDeliveryRecords(ctx context.Context, start, end time.Time, limit int64) ([]DeliveryRecord, error) {
	key := keyPrefixDelivery + "all"
	return s.getRecordsFromKey(ctx, key, start, end, limit)
}

// GetDeliveryRecordsByEndpoint retrieves records for a specific endpoint.
func (s *Store) GetDeliveryRecordsByEndpoint(ctx context.Context, endpointID string, start, end time.Time, limit int64) ([]DeliveryRecord, error) {
	key := keyPrefixDelivery + "endpoint:" + endpointID
	return s.getRecordsFromKey(ctx, key, start, end, limit)
}

// GetDeliveryRecordsByEventType retrieves records for a specific event type.
func (s *Store) GetDeliveryRecordsByEventType(ctx context.Context, eventType string, start, end time.Time, limit int64) ([]DeliveryRecord, error) {
	key := keyPrefixDelivery + "event_type:" + eventType
	return s.getRecordsFromKey(ctx, key, start, end, limit)
}

func (s *Store) getRecordsFromKey(ctx context.Context, key string, start, end time.Time, limit int64) ([]DeliveryRecord, error) {
	minScore := strconv.FormatInt(start.UnixMilli(), 10)
	maxScore := strconv.FormatInt(end.UnixMilli(), 10)

	var results []string
	var err error

	if limit > 0 {
		results, err = s.client.ZRangeByScore(ctx, key, &redis.ZRangeBy{
			Min:   minScore,
			Max:   maxScore,
			Count: limit,
		}).Result()
	} else {
		results, err = s.client.ZRangeByScore(ctx, key, &redis.ZRangeBy{
			Min: minScore,
			Max: maxScore,
		}).Result()
	}

	if err != nil {
		return nil, fmt.Errorf("get records: %w", err)
	}

	records := make([]DeliveryRecord, 0, len(results))
	for _, data := range results {
		var record DeliveryRecord
		if err := json.Unmarshal([]byte(data), &record); err != nil {
			log.Warn("failed to unmarshal record", "error", err)
			continue
		}
		records = append(records, record)
	}

	return records, nil
}

// GetFailureRate returns the failure rate within a time window.
func (s *Store) GetFailureRate(ctx context.Context, window time.Duration) (float64, error) {
	end := time.Now().UTC()
	start := end.Add(-window)

	records, err := s.GetDeliveryRecords(ctx, start, end, 0)
	if err != nil {
		return 0, err
	}

	if len(records) == 0 {
		return 0, nil
	}

	var failures int
	for _, r := range records {
		if r.Outcome == OutcomeFailure || r.Outcome == OutcomeTimeout {
			failures++
		}
	}

	return float64(failures) / float64(len(records)), nil
}

// GetSuccessRate returns the success rate within a time window.
func (s *Store) GetSuccessRate(ctx context.Context, window time.Duration) (float64, error) {
	failureRate, err := s.GetFailureRate(ctx, window)
	if err != nil {
		return 0, err
	}
	return 1 - failureRate, nil
}

// GetAverageLatency returns the average latency within a time window.
func (s *Store) GetAverageLatency(ctx context.Context, window time.Duration) (float64, error) {
	end := time.Now().UTC()
	start := end.Add(-window)

	records, err := s.GetDeliveryRecords(ctx, start, end, 0)
	if err != nil {
		return 0, err
	}

	if len(records) == 0 {
		return 0, nil
	}

	var total int64
	for _, r := range records {
		total += r.LatencyMs
	}

	return float64(total) / float64(len(records)), nil
}

// GetLatencyPercentiles returns p50, p95, p99 latencies within a time window.
func (s *Store) GetLatencyPercentiles(ctx context.Context, window time.Duration) (p50, p95, p99 float64, err error) {
	end := time.Now().UTC()
	start := end.Add(-window)

	records, err := s.GetDeliveryRecords(ctx, start, end, 0)
	if err != nil {
		return 0, 0, 0, err
	}

	if len(records) == 0 {
		return 0, 0, 0, nil
	}

	latencies := make([]int64, len(records))
	for i, r := range records {
		latencies[i] = r.LatencyMs
	}
	slices.Sort(latencies)

	p50 = float64(latencies[len(latencies)*50/100])
	p95 = float64(latencies[len(latencies)*95/100])
	p99Index := len(latencies) * 99 / 100
	if p99Index >= len(latencies) {
		p99Index = len(latencies) - 1
	}
	p99 = float64(latencies[p99Index])

	return p50, p95, p99, nil
}

// GetQueueDepth returns the current queue depth from the queue stats.
// This requires the queue to be passed in or queried separately.
func (s *Store) GetQueueDepth(ctx context.Context) (int64, error) {
	// Read from a gauge we set elsewhere
	val, err := s.client.Get(ctx, keyPrefixCounters+"queue_depth").Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return val, err
}

// SetQueueDepth updates the current queue depth gauge.
func (s *Store) SetQueueDepth(ctx context.Context, depth int64) error {
	return s.client.Set(ctx, keyPrefixCounters+"queue_depth", depth, 0).Err()
}

// GetErrorCount returns the number of errors within a time window.
func (s *Store) GetErrorCount(ctx context.Context, window time.Duration) (int64, error) {
	end := time.Now().UTC()
	start := end.Add(-window)

	records, err := s.GetDeliveryRecords(ctx, start, end, 0)
	if err != nil {
		return 0, err
	}

	var count int64
	for _, r := range records {
		if r.Outcome == OutcomeFailure || r.Outcome == OutcomeTimeout {
			count++
		}
	}

	return count, nil
}

// GetDeliveryCount returns the number of deliveries within a time window.
func (s *Store) GetDeliveryCount(ctx context.Context, window time.Duration) (int64, error) {
	end := time.Now().UTC()
	start := end.Add(-window)

	records, err := s.GetDeliveryRecords(ctx, start, end, 0)
	if err != nil {
		return 0, err
	}

	return int64(len(records)), nil
}

// GetStats returns aggregated statistics for a time window.
func (s *Store) GetStats(ctx context.Context, start, end time.Time) (*AggregatedStats, error) {
	records, err := s.GetDeliveryRecords(ctx, start, end, 0)
	if err != nil {
		return nil, err
	}

	stats := &AggregatedStats{
		Period: start.Format(time.RFC3339),
	}

	if len(records) == 0 {
		return stats, nil
	}

	latencies := make([]int64, 0, len(records))
	var totalLatency int64
	var minLatency, maxLatency int64 = -1, 0

	for _, r := range records {
		stats.TotalCount++
		latencies = append(latencies, r.LatencyMs)
		totalLatency += r.LatencyMs

		if minLatency == -1 || r.LatencyMs < minLatency {
			minLatency = r.LatencyMs
		}
		if r.LatencyMs > maxLatency {
			maxLatency = r.LatencyMs
		}

		switch r.Outcome {
		case OutcomeSuccess:
			stats.SuccessCount++
		case OutcomeFailure:
			stats.FailureCount++
		case OutcomeTimeout:
			stats.TimeoutCount++
		}
	}

	if stats.TotalCount > 0 {
		stats.SuccessRate = float64(stats.SuccessCount) / float64(stats.TotalCount)
		stats.FailureRate = float64(stats.FailureCount+stats.TimeoutCount) / float64(stats.TotalCount)
		stats.AvgLatencyMs = float64(totalLatency) / float64(stats.TotalCount)
		stats.MinLatencyMs = minLatency
		stats.MaxLatencyMs = maxLatency

		// Calculate percentiles
		slices.Sort(latencies)
		stats.P50LatencyMs = float64(latencies[len(latencies)*50/100])
		stats.P95LatencyMs = float64(latencies[len(latencies)*95/100])
		p99Index := len(latencies) * 99 / 100
		if p99Index >= len(latencies) {
			p99Index = len(latencies) - 1
		}
		stats.P99LatencyMs = float64(latencies[p99Index])
	}

	return stats, nil
}

// GetSuccessRateTimeSeries returns success rate over time with the given granularity.
func (s *Store) GetSuccessRateTimeSeries(ctx context.Context, start, end time.Time, granularity time.Duration) ([]TimeSeriesPoint, error) {
	var points []TimeSeriesPoint

	for t := start; t.Before(end); t = t.Add(granularity) {
		periodEnd := t.Add(granularity)
		if periodEnd.After(end) {
			periodEnd = end
		}

		records, err := s.GetDeliveryRecords(ctx, t, periodEnd, 0)
		if err != nil {
			return nil, err
		}

		var successRate float64
		if len(records) > 0 {
			var successes int
			for _, r := range records {
				if r.Outcome == OutcomeSuccess {
					successes++
				}
			}
			successRate = float64(successes) / float64(len(records))
		}

		points = append(points, TimeSeriesPoint{
			Timestamp: t,
			Value:     successRate,
		})
	}

	return points, nil
}

// GetLatencyTimeSeries returns average latency over time with the given granularity.
func (s *Store) GetLatencyTimeSeries(ctx context.Context, start, end time.Time, granularity time.Duration) ([]TimeSeriesPoint, error) {
	var points []TimeSeriesPoint

	for t := start; t.Before(end); t = t.Add(granularity) {
		periodEnd := t.Add(granularity)
		if periodEnd.After(end) {
			periodEnd = end
		}

		records, err := s.GetDeliveryRecords(ctx, t, periodEnd, 0)
		if err != nil {
			return nil, err
		}

		var avgLatency float64
		if len(records) > 0 {
			var total int64
			for _, r := range records {
				total += r.LatencyMs
			}
			avgLatency = float64(total) / float64(len(records))
		}

		points = append(points, TimeSeriesPoint{
			Timestamp: t,
			Value:     avgLatency,
		})
	}

	return points, nil
}

// GetBreakdownByEventType returns delivery stats broken down by event type.
func (s *Store) GetBreakdownByEventType(ctx context.Context, start, end time.Time, limit int) ([]BreakdownItem, error) {
	records, err := s.GetDeliveryRecords(ctx, start, end, 0)
	if err != nil {
		return nil, err
	}

	return s.computeBreakdown(records, func(r DeliveryRecord) string { return r.EventType }, limit), nil
}

// GetBreakdownByEndpoint returns delivery stats broken down by endpoint.
func (s *Store) GetBreakdownByEndpoint(ctx context.Context, start, end time.Time, limit int) ([]BreakdownItem, error) {
	records, err := s.GetDeliveryRecords(ctx, start, end, 0)
	if err != nil {
		return nil, err
	}

	return s.computeBreakdown(records, func(r DeliveryRecord) string { return r.EndpointID }, limit), nil
}

// GetBreakdownByStatus returns delivery stats broken down by outcome status.
func (s *Store) GetBreakdownByStatus(ctx context.Context, start, end time.Time) ([]BreakdownItem, error) {
	records, err := s.GetDeliveryRecords(ctx, start, end, 0)
	if err != nil {
		return nil, err
	}

	return s.computeBreakdown(records, func(r DeliveryRecord) string { return string(r.Outcome) }, 10), nil
}

func (s *Store) computeBreakdown(records []DeliveryRecord, keyFn func(DeliveryRecord) string, limit int) []BreakdownItem {
	type stats struct {
		count        int64
		successes    int64
		totalLatency int64
	}

	byKey := make(map[string]*stats)

	for _, r := range records {
		key := keyFn(r)
		if key == "" {
			key = "(unknown)"
		}

		st, ok := byKey[key]
		if !ok {
			st = &stats{}
			byKey[key] = st
		}

		st.count++
		st.totalLatency += r.LatencyMs
		if r.Outcome == OutcomeSuccess {
			st.successes++
		}
	}

	items := make([]BreakdownItem, 0, len(byKey))
	for key, st := range byKey {
		item := BreakdownItem{
			Key:   key,
			Count: st.count,
		}
		if st.count > 0 {
			item.SuccessRate = float64(st.successes) / float64(st.count)
			item.AvgLatency = float64(st.totalLatency) / float64(st.count)
		}
		items = append(items, item)
	}

	// Sort by count descending
	slices.SortFunc(items, func(a, b BreakdownItem) int {
		return int(b.Count - a.Count) // Descending order
	})

	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}

	return items
}

// CleanupOldData removes metrics data older than the TTL.
func (s *Store) CleanupOldData(ctx context.Context) error {
	cutoff := time.Now().UTC().Add(-rawMetricsTTL).UnixMilli()
	cutoffStr := strconv.FormatInt(cutoff, 10)

	// Get all delivery keys
	keys, err := s.client.Keys(ctx, keyPrefixDelivery+"*").Result()
	if err != nil {
		return fmt.Errorf("list keys: %w", err)
	}

	pipe := s.client.Pipeline()
	for _, key := range keys {
		pipe.ZRemRangeByScore(ctx, key, "-inf", cutoffStr)
	}

	// Cleanup latency data
	latencyKeys, err := s.client.Keys(ctx, keyPrefixLatency+"*").Result()
	if err != nil {
		return fmt.Errorf("list latency keys: %w", err)
	}
	for _, key := range latencyKeys {
		pipe.ZRemRangeByScore(ctx, key, "-inf", cutoffStr)
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("cleanup old data: %w", err)
	}

	log.Debug("cleaned up old metrics data", "cutoff", time.UnixMilli(cutoff))
	return nil
}
