package debug

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/stiffinWanjohi/relay/internal/logging"
)

var log = logging.Component("debug")

const (
	// DefaultTTL is the default time-to-live for debug endpoints.
	DefaultTTL = 1 * time.Hour

	// MaxRequestsPerEndpoint is the maximum number of requests stored per endpoint.
	MaxRequestsPerEndpoint = 100

	// endpointKeyPrefix is the Redis key prefix for debug endpoints.
	endpointKeyPrefix = "relay:debug:endpoint:"

	// requestKeyPrefix is the Redis key prefix for captured requests.
	requestKeyPrefix = "relay:debug:requests:"
)

// CapturedRequest represents a captured webhook request.
type CapturedRequest struct {
	ID          string            `json:"id"`
	ReceivedAt  time.Time         `json:"received_at"`
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	Headers     map[string]string `json:"headers"`
	Body        string            `json:"body"`
	ContentType string            `json:"content_type"`
	RemoteAddr  string            `json:"remote_addr"`
	Duration    time.Duration     `json:"duration_ns"`
}

// Endpoint represents a debug endpoint.
type Endpoint struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	URL       string    `json:"url"`
}

// Service manages debug endpoints and captured requests.
type Service struct {
	client  *redis.Client
	baseURL string
	ttl     time.Duration

	// In-memory subscribers for real-time updates
	mu          sync.RWMutex
	subscribers map[string][]chan *CapturedRequest
}

// NewService creates a new debug service.
func NewService(client *redis.Client, baseURL string) *Service {
	return &Service{
		client:      client,
		baseURL:     baseURL,
		ttl:         DefaultTTL,
		subscribers: make(map[string][]chan *CapturedRequest),
	}
}

// WithTTL sets a custom TTL for debug endpoints.
func (s *Service) WithTTL(ttl time.Duration) *Service {
	s.ttl = ttl
	return s
}

// CreateEndpoint creates a new debug endpoint.
func (s *Service) CreateEndpoint(ctx context.Context) (*Endpoint, error) {
	id, err := generateID()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	endpoint := &Endpoint{
		ID:        id,
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
		URL:       s.baseURL + "/debug/" + id,
	}

	data, err := json.Marshal(endpoint)
	if err != nil {
		return nil, err
	}

	key := endpointKeyPrefix + id
	if err := s.client.Set(ctx, key, data, s.ttl).Err(); err != nil {
		return nil, err
	}

	log.Info("debug endpoint created",
		"endpoint_id", id,
		"expires_at", endpoint.ExpiresAt,
	)

	return endpoint, nil
}

// GetEndpoint retrieves a debug endpoint by ID.
func (s *Service) GetEndpoint(ctx context.Context, id string) (*Endpoint, error) {
	key := endpointKeyPrefix + id
	data, err := s.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var endpoint Endpoint
	if err := json.Unmarshal(data, &endpoint); err != nil {
		return nil, err
	}

	return &endpoint, nil
}

// DeleteEndpoint deletes a debug endpoint and its captured requests.
func (s *Service) DeleteEndpoint(ctx context.Context, id string) error {
	pipe := s.client.Pipeline()
	pipe.Del(ctx, endpointKeyPrefix+id)
	pipe.Del(ctx, requestKeyPrefix+id)
	_, err := pipe.Exec(ctx)

	if err == nil {
		log.Info("debug endpoint deleted", "endpoint_id", id)
	}

	return err
}

// CaptureRequest stores a captured request for a debug endpoint.
func (s *Service) CaptureRequest(ctx context.Context, endpointID string, req *CapturedRequest) error {
	// Verify endpoint exists
	endpoint, err := s.GetEndpoint(ctx, endpointID)
	if err != nil {
		return err
	}
	if endpoint == nil {
		return nil // Endpoint expired or doesn't exist
	}

	// Generate request ID if not set
	if req.ID == "" {
		req.ID, _ = generateID()
	}
	if req.ReceivedAt.IsZero() {
		req.ReceivedAt = time.Now()
	}

	data, err := json.Marshal(req)
	if err != nil {
		return err
	}

	key := requestKeyPrefix + endpointID

	// Use LPUSH + LTRIM to maintain a capped list
	pipe := s.client.Pipeline()
	pipe.LPush(ctx, key, data)
	pipe.LTrim(ctx, key, 0, MaxRequestsPerEndpoint-1)
	pipe.Expire(ctx, key, s.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}

	log.Debug("request captured",
		"endpoint_id", endpointID,
		"request_id", req.ID,
		"method", req.Method,
		"path", req.Path,
	)

	// Notify subscribers
	s.notifySubscribers(endpointID, req)

	return nil
}

// GetRequests retrieves captured requests for a debug endpoint.
func (s *Service) GetRequests(ctx context.Context, endpointID string, limit int) ([]*CapturedRequest, error) {
	if limit <= 0 || limit > MaxRequestsPerEndpoint {
		limit = MaxRequestsPerEndpoint
	}

	key := requestKeyPrefix + endpointID
	data, err := s.client.LRange(ctx, key, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}

	requests := make([]*CapturedRequest, 0, len(data))
	for _, d := range data {
		var req CapturedRequest
		if err := json.Unmarshal([]byte(d), &req); err != nil {
			continue // Skip malformed entries
		}
		requests = append(requests, &req)
	}

	return requests, nil
}

// GetRequest retrieves a specific captured request.
func (s *Service) GetRequest(ctx context.Context, endpointID, requestID string) (*CapturedRequest, error) {
	requests, err := s.GetRequests(ctx, endpointID, MaxRequestsPerEndpoint)
	if err != nil {
		return nil, err
	}

	for _, req := range requests {
		if req.ID == requestID {
			return req, nil
		}
	}

	return nil, nil
}

// Subscribe creates a channel that receives new captured requests for an endpoint.
func (s *Service) Subscribe(endpointID string) <-chan *CapturedRequest {
	ch := make(chan *CapturedRequest, 10)

	s.mu.Lock()
	s.subscribers[endpointID] = append(s.subscribers[endpointID], ch)
	s.mu.Unlock()

	return ch
}

// Unsubscribe removes a subscription channel.
func (s *Service) Unsubscribe(endpointID string, ch <-chan *CapturedRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()

	subs := s.subscribers[endpointID]
	for i, sub := range subs {
		if sub == ch {
			s.subscribers[endpointID] = append(subs[:i], subs[i+1:]...)
			close(sub)
			break
		}
	}

	// Clean up empty subscriber lists
	if len(s.subscribers[endpointID]) == 0 {
		delete(s.subscribers, endpointID)
	}
}

func (s *Service) notifySubscribers(endpointID string, req *CapturedRequest) {
	s.mu.RLock()
	subs := s.subscribers[endpointID]
	s.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- req:
		default:
			// Channel full, skip
		}
	}
}

func generateID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
