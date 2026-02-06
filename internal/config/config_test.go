package config

import (
	"os"
	"testing"
	"time"
)

func TestConstants(t *testing.T) {
	if MinSigningKeyLength != 32 {
		t.Errorf("MinSigningKeyLength = %d, want 32", MinSigningKeyLength)
	}
	if MaxPayloadSize != 1*1024*1024 {
		t.Errorf("MaxPayloadSize = %d, want 1MB", MaxPayloadSize)
	}
	if MaxURLLength != 2048 {
		t.Errorf("MaxURLLength = %d, want 2048", MaxURLLength)
	}
	if DefaultMaxAttempts != 10 {
		t.Errorf("DefaultMaxAttempts = %d, want 10", DefaultMaxAttempts)
	}
	if DefaultWorkerConcurrency != 10 {
		t.Errorf("DefaultWorkerConcurrency = %d, want 10", DefaultWorkerConcurrency)
	}
	if DefaultVisibilityTimeout != 30*time.Second {
		t.Errorf("DefaultVisibilityTimeout = %v, want 30s", DefaultVisibilityTimeout)
	}
}

func TestLoadConfig_MissingDatabaseURL(t *testing.T) {
	// Clear all relevant env vars
	clearEnv()

	cfg, err := LoadConfig()
	if err != ErrDatabaseURLRequired {
		t.Errorf("expected ErrDatabaseURLRequired, got %v", err)
	}
	if cfg != nil {
		t.Error("expected nil config on error")
	}
}

func TestLoadConfig_MissingSigningKey(t *testing.T) {
	clearEnv()
	_ = os.Setenv("DATABASE_URL", "postgres://localhost/relay")

	cfg, err := LoadConfig()
	if err != ErrSigningKeyRequired {
		t.Errorf("expected ErrSigningKeyRequired, got %v", err)
	}
	if cfg != nil {
		t.Error("expected nil config on error")
	}
}

func TestLoadConfig_SigningKeyTooShort(t *testing.T) {
	clearEnv()
	_ = os.Setenv("DATABASE_URL", "postgres://localhost/relay")
	_ = os.Setenv("SIGNING_KEY", "short") // Less than 32 chars

	cfg, err := LoadConfig()
	if err != ErrSigningKeyTooShort {
		t.Errorf("expected ErrSigningKeyTooShort, got %v", err)
	}
	if cfg != nil {
		t.Error("expected nil config on error")
	}
}

func TestLoadConfig_Success(t *testing.T) {
	clearEnv()
	_ = os.Setenv("DATABASE_URL", "postgres://localhost/relay")
	_ = os.Setenv("SIGNING_KEY", "this-is-a-32-character-key-here!")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
		return
	}

	// Verify database config
	if cfg.Database.URL != "postgres://localhost/relay" {
		t.Errorf("Database.URL = %s", cfg.Database.URL)
	}
	if cfg.Database.MaxConns != 100 {
		t.Errorf("Database.MaxConns = %d, want 100", cfg.Database.MaxConns)
	}
	if cfg.Database.MinConns != 10 {
		t.Errorf("Database.MinConns = %d, want 10", cfg.Database.MinConns)
	}

	// Verify Redis defaults
	if cfg.Redis.URL != "localhost:6379" {
		t.Errorf("Redis.URL = %s, want localhost:6379", cfg.Redis.URL)
	}
	if cfg.Redis.PoolSize != 100 {
		t.Errorf("Redis.PoolSize = %d, want 100", cfg.Redis.PoolSize)
	}

	// Verify API defaults
	if cfg.API.Addr != ":8080" {
		t.Errorf("API.Addr = %s, want :8080", cfg.API.Addr)
	}

	// Verify Worker config
	if cfg.Worker.Concurrency != DefaultWorkerConcurrency {
		t.Errorf("Worker.Concurrency = %d, want %d", cfg.Worker.Concurrency, DefaultWorkerConcurrency)
	}
	if cfg.Worker.SigningKey != "this-is-a-32-character-key-here!" {
		t.Errorf("Worker.SigningKey mismatch")
	}

	// Verify Auth defaults
	if !cfg.Auth.Enabled {
		t.Error("Auth.Enabled should default to true")
	}
	if cfg.Auth.EnablePlayground {
		t.Error("Auth.EnablePlayground should default to false")
	}
}

func TestLoadConfig_CustomValues(t *testing.T) {
	clearEnv()
	_ = os.Setenv("DATABASE_URL", "postgres://localhost/relay")
	_ = os.Setenv("DATABASE_MAX_CONNS", "50")
	_ = os.Setenv("DATABASE_MIN_CONNS", "5")
	_ = os.Setenv("DATABASE_MAX_CONN_LIFETIME", "2h")
	_ = os.Setenv("DATABASE_MAX_CONN_IDLE_TIME", "15m")
	_ = os.Setenv("REDIS_URL", "redis://custom:6380")
	_ = os.Setenv("REDIS_POOL_SIZE", "200")
	_ = os.Setenv("REDIS_READ_TIMEOUT", "5s")
	_ = os.Setenv("REDIS_WRITE_TIMEOUT", "5s")
	_ = os.Setenv("API_ADDR", ":9090")
	_ = os.Setenv("API_READ_TIMEOUT", "30s")
	_ = os.Setenv("API_WRITE_TIMEOUT", "30s")
	_ = os.Setenv("API_IDLE_TIMEOUT", "120s")
	_ = os.Setenv("API_SHUTDOWN_TIMEOUT", "60s")
	_ = os.Setenv("WORKER_CONCURRENCY", "20")
	_ = os.Setenv("WORKER_VISIBILITY_TIMEOUT", "60s")
	_ = os.Setenv("WORKER_SHUTDOWN_TIMEOUT", "45s")
	_ = os.Setenv("SIGNING_KEY", "this-is-a-32-character-key-here!")
	_ = os.Setenv("OUTBOX_POLL_INTERVAL", "2s")
	_ = os.Setenv("OUTBOX_BATCH_SIZE", "50")
	_ = os.Setenv("OUTBOX_CLEANUP_INTERVAL", "2h")
	_ = os.Setenv("OUTBOX_RETENTION_PERIOD", "48h")
	_ = os.Setenv("AUTH_ENABLED", "false")
	_ = os.Setenv("ENABLE_PLAYGROUND", "true")
	_ = os.Setenv("METRICS_PROVIDER", "otel")
	_ = os.Setenv("METRICS_ENDPOINT", "http://collector:4317")
	_ = os.Setenv("SERVICE_NAME", "custom-relay")
	_ = os.Setenv("SERVICE_VERSION", "2.0.0")
	_ = os.Setenv("ENVIRONMENT", "production")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify database config
	if cfg.Database.MaxConns != 50 {
		t.Errorf("Database.MaxConns = %d, want 50", cfg.Database.MaxConns)
	}
	if cfg.Database.MinConns != 5 {
		t.Errorf("Database.MinConns = %d, want 5", cfg.Database.MinConns)
	}
	if cfg.Database.MaxConnLifetime != 2*time.Hour {
		t.Errorf("Database.MaxConnLifetime = %v, want 2h", cfg.Database.MaxConnLifetime)
	}
	if cfg.Database.MaxConnIdleTime != 15*time.Minute {
		t.Errorf("Database.MaxConnIdleTime = %v, want 15m", cfg.Database.MaxConnIdleTime)
	}

	// Verify Redis config
	if cfg.Redis.URL != "redis://custom:6380" {
		t.Errorf("Redis.URL = %s", cfg.Redis.URL)
	}
	if cfg.Redis.PoolSize != 200 {
		t.Errorf("Redis.PoolSize = %d, want 200", cfg.Redis.PoolSize)
	}
	if cfg.Redis.ReadTimeout != 5*time.Second {
		t.Errorf("Redis.ReadTimeout = %v, want 5s", cfg.Redis.ReadTimeout)
	}
	if cfg.Redis.WriteTimeout != 5*time.Second {
		t.Errorf("Redis.WriteTimeout = %v, want 5s", cfg.Redis.WriteTimeout)
	}

	// Verify API config
	if cfg.API.Addr != ":9090" {
		t.Errorf("API.Addr = %s, want :9090", cfg.API.Addr)
	}
	if cfg.API.ReadTimeout != 30*time.Second {
		t.Errorf("API.ReadTimeout = %v, want 30s", cfg.API.ReadTimeout)
	}
	if cfg.API.WriteTimeout != 30*time.Second {
		t.Errorf("API.WriteTimeout = %v, want 30s", cfg.API.WriteTimeout)
	}
	if cfg.API.IdleTimeout != 120*time.Second {
		t.Errorf("API.IdleTimeout = %v, want 120s", cfg.API.IdleTimeout)
	}
	if cfg.API.ShutdownTimeout != 60*time.Second {
		t.Errorf("API.ShutdownTimeout = %v, want 60s", cfg.API.ShutdownTimeout)
	}

	// Verify Worker config
	if cfg.Worker.Concurrency != 20 {
		t.Errorf("Worker.Concurrency = %d, want 20", cfg.Worker.Concurrency)
	}
	if cfg.Worker.VisibilityTimeout != 60*time.Second {
		t.Errorf("Worker.VisibilityTimeout = %v, want 60s", cfg.Worker.VisibilityTimeout)
	}
	if cfg.Worker.ShutdownTimeout != 45*time.Second {
		t.Errorf("Worker.ShutdownTimeout = %v, want 45s", cfg.Worker.ShutdownTimeout)
	}

	// Verify Outbox config
	if cfg.Outbox.PollInterval != 2*time.Second {
		t.Errorf("Outbox.PollInterval = %v, want 2s", cfg.Outbox.PollInterval)
	}
	if cfg.Outbox.BatchSize != 50 {
		t.Errorf("Outbox.BatchSize = %d, want 50", cfg.Outbox.BatchSize)
	}
	if cfg.Outbox.CleanupInterval != 2*time.Hour {
		t.Errorf("Outbox.CleanupInterval = %v, want 2h", cfg.Outbox.CleanupInterval)
	}
	if cfg.Outbox.RetentionPeriod != 48*time.Hour {
		t.Errorf("Outbox.RetentionPeriod = %v, want 48h", cfg.Outbox.RetentionPeriod)
	}

	// Verify Auth config
	if cfg.Auth.Enabled {
		t.Error("Auth.Enabled should be false")
	}
	if !cfg.Auth.EnablePlayground {
		t.Error("Auth.EnablePlayground should be true")
	}

	// Verify Metrics config
	if cfg.Metrics.Provider != "otel" {
		t.Errorf("Metrics.Provider = %s, want otel", cfg.Metrics.Provider)
	}
	if cfg.Metrics.Endpoint != "http://collector:4317" {
		t.Errorf("Metrics.Endpoint = %s", cfg.Metrics.Endpoint)
	}
	if cfg.Metrics.ServiceName != "custom-relay" {
		t.Errorf("Metrics.ServiceName = %s", cfg.Metrics.ServiceName)
	}
	if cfg.Metrics.ServiceVersion != "2.0.0" {
		t.Errorf("Metrics.ServiceVersion = %s", cfg.Metrics.ServiceVersion)
	}
	if cfg.Metrics.Environment != "production" {
		t.Errorf("Metrics.Environment = %s", cfg.Metrics.Environment)
	}
}

func TestLoadConfig_InvalidEnvValues(t *testing.T) {
	clearEnv()
	_ = os.Setenv("DATABASE_URL", "postgres://localhost/relay")
	_ = os.Setenv("SIGNING_KEY", "this-is-a-32-character-key-here!")
	_ = os.Setenv("DATABASE_MAX_CONNS", "invalid")
	_ = os.Setenv("REDIS_READ_TIMEOUT", "invalid")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should use defaults for invalid values
	if cfg.Database.MaxConns != 100 {
		t.Errorf("should use default for invalid DATABASE_MAX_CONNS, got %d", cfg.Database.MaxConns)
	}
	if cfg.Redis.ReadTimeout != 3*time.Second {
		t.Errorf("should use default for invalid REDIS_READ_TIMEOUT, got %v", cfg.Redis.ReadTimeout)
	}
}

func TestValidateDestinationURL_Valid(t *testing.T) {
	validURLs := []string{
		"https://example.com/webhook",
		"https://api.example.com/v1/events",
		"http://example.com:8080/path",
		"https://sub.domain.example.com/webhook",
		"https://example.com/webhook?param=value",
	}

	for _, url := range validURLs {
		if err := ValidateDestinationURL(url); err != nil {
			t.Errorf("ValidateDestinationURL(%q) = %v, want nil", url, err)
		}
	}
}

func TestValidateDestinationURL_Empty(t *testing.T) {
	if err := ValidateDestinationURL(""); err != ErrInvalidURL {
		t.Errorf("expected ErrInvalidURL for empty URL, got %v", err)
	}
}

func TestValidateDestinationURL_TooLong(t *testing.T) {
	longURL := "https://example.com/" + string(make([]byte, MaxURLLength))
	if err := ValidateDestinationURL(longURL); err != ErrURLTooLong {
		t.Errorf("expected ErrURLTooLong, got %v", err)
	}
}

func TestValidateDestinationURL_InvalidScheme(t *testing.T) {
	invalidSchemes := []string{
		"ftp://example.com/file",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"data:text/html,<script>",
		"gopher://example.com",
	}

	for _, url := range invalidSchemes {
		if err := ValidateDestinationURL(url); err != ErrInvalidURLScheme {
			t.Errorf("ValidateDestinationURL(%q) = %v, want ErrInvalidURLScheme", url, err)
		}
	}
}

func TestValidateDestinationURL_InternalHosts(t *testing.T) {
	internalURLs := []string{
		// Localhost
		"http://localhost/webhook",
		"https://localhost:8080/webhook",
		"http://LOCALHOST/webhook",

		// Loopback
		"http://127.0.0.1/webhook",
		"http://127.0.0.255/webhook",
		"http://127.1.2.3/webhook",

		// Private ranges - 10.x.x.x
		"http://10.0.0.1/webhook",
		"http://10.255.255.255/webhook",

		// Private ranges - 172.16.x.x to 172.31.x.x
		"http://172.16.0.1/webhook",
		"http://172.31.255.255/webhook",
		"http://172.20.0.1/webhook",

		// Private ranges - 192.168.x.x
		"http://192.168.0.1/webhook",
		"http://192.168.1.100/webhook",

		// Link-local
		"http://169.254.1.1/webhook",
		"http://169.254.169.254/webhook",

		// Internal suffixes
		"http://server.local/webhook",
		"http://server.internal/webhook",
		"http://server.localhost/webhook",
		"http://server.localdomain/webhook",

		// IPv6 link-local
		"http://[fe80::1]/webhook",

		// IPv6 unique local (fc/fd)
		"http://[fc00::1]/webhook",
		"http://[fd00::1]/webhook",

		// Cloud metadata endpoints
		"http://169.254.169.254/latest/meta-data/",
		"http://metadata.google.internal/webhook",
		"http://metadata.goog/webhook",
	}

	for _, url := range internalURLs {
		if err := ValidateDestinationURL(url); err != ErrInternalURLBlocked {
			t.Errorf("ValidateDestinationURL(%q) = %v, want ErrInternalURLBlocked", url, err)
		}
	}
}

func TestValidateDestinationURL_AllowedPrivateRange(t *testing.T) {
	// 172.32+ should be allowed (not in private range)
	allowedURLs := []string{
		"http://172.32.0.1/webhook",
		"http://172.15.0.1/webhook",
	}

	for _, url := range allowedURLs {
		if err := ValidateDestinationURL(url); err != nil {
			t.Errorf("ValidateDestinationURL(%q) = %v, should be allowed", url, err)
		}
	}
}

func TestValidateDestinationURL_MalformedURL(t *testing.T) {
	malformed := []string{
		"://no-scheme",
		"not-a-url",
	}

	for _, url := range malformed {
		err := ValidateDestinationURL(url)
		if err == nil {
			t.Errorf("ValidateDestinationURL(%q) should return error", url)
		}
	}
}

func TestValidatePayloadSize_Valid(t *testing.T) {
	validPayloads := [][]byte{
		{},
		[]byte(`{"small": true}`),
		make([]byte, MaxPayloadSize), // Exactly at limit
	}

	for i, payload := range validPayloads {
		if err := ValidatePayloadSize(payload); err != nil {
			t.Errorf("ValidatePayloadSize[%d] = %v, want nil", i, err)
		}
	}
}

func TestValidatePayloadSize_TooLarge(t *testing.T) {
	largePayload := make([]byte, MaxPayloadSize+1)
	if err := ValidatePayloadSize(largePayload); err != ErrPayloadTooLarge {
		t.Errorf("expected ErrPayloadTooLarge, got %v", err)
	}
}

func TestValidateIdempotencyKey_Valid(t *testing.T) {
	validKeys := []string{
		"abc123",
		"uuid-format-key",
		"  padded  ", // Has non-whitespace
		"a",
	}

	for _, key := range validKeys {
		if err := ValidateIdempotencyKey(key); err != nil {
			t.Errorf("ValidateIdempotencyKey(%q) = %v, want nil", key, err)
		}
	}
}

func TestValidateIdempotencyKey_Invalid(t *testing.T) {
	invalidKeys := []string{
		"",
		"   ",
		"\t\n",
	}

	for _, key := range invalidKeys {
		if err := ValidateIdempotencyKey(key); err != ErrInvalidIdempotencyKey {
			t.Errorf("ValidateIdempotencyKey(%q) = %v, want ErrInvalidIdempotencyKey", key, err)
		}
	}
}

func TestGetEnv(t *testing.T) {
	clearEnv()

	// Test with no env var set
	result := getEnv("TEST_VAR", "default")
	if result != "default" {
		t.Errorf("getEnv should return default when var not set, got %s", result)
	}

	// Test with env var set
	_ = os.Setenv("TEST_VAR", "custom")
	result = getEnv("TEST_VAR", "default")
	if result != "custom" {
		t.Errorf("getEnv should return env value, got %s", result)
	}
}

func TestGetEnvInt(t *testing.T) {
	clearEnv()

	// Test with no env var set
	result := getEnvInt("TEST_INT", 42)
	if result != 42 {
		t.Errorf("getEnvInt should return default when var not set, got %d", result)
	}

	// Test with valid int
	_ = os.Setenv("TEST_INT", "100")
	result = getEnvInt("TEST_INT", 42)
	if result != 100 {
		t.Errorf("getEnvInt should return parsed int, got %d", result)
	}

	// Test with invalid int
	_ = os.Setenv("TEST_INT", "not-a-number")
	result = getEnvInt("TEST_INT", 42)
	if result != 42 {
		t.Errorf("getEnvInt should return default for invalid int, got %d", result)
	}
}

func TestGetEnvDuration(t *testing.T) {
	clearEnv()

	// Test with no env var set
	result := getEnvDuration("TEST_DUR", 5*time.Second)
	if result != 5*time.Second {
		t.Errorf("getEnvDuration should return default when var not set, got %v", result)
	}

	// Test with valid duration
	_ = os.Setenv("TEST_DUR", "10m")
	result = getEnvDuration("TEST_DUR", 5*time.Second)
	if result != 10*time.Minute {
		t.Errorf("getEnvDuration should return parsed duration, got %v", result)
	}

	// Test with invalid duration
	_ = os.Setenv("TEST_DUR", "not-a-duration")
	result = getEnvDuration("TEST_DUR", 5*time.Second)
	if result != 5*time.Second {
		t.Errorf("getEnvDuration should return default for invalid duration, got %v", result)
	}
}

func TestGetEnvBool(t *testing.T) {
	tests := []struct {
		value    string
		def      bool
		expected bool
	}{
		{"", true, true},   // Empty returns default
		{"", false, false}, // Empty returns default
		{"true", false, true},
		{"1", false, true},
		{"yes", false, true},
		{"false", true, false},
		{"0", true, false},
		{"no", true, false},
		{"invalid", true, false},
		{"TRUE", false, false}, // Case sensitive
	}

	for _, tt := range tests {
		clearEnv()
		if tt.value != "" {
			_ = os.Setenv("TEST_BOOL", tt.value)
		}
		result := getEnvBool("TEST_BOOL", tt.def)
		if result != tt.expected {
			t.Errorf("getEnvBool(%q, %v) = %v, want %v", tt.value, tt.def, result, tt.expected)
		}
	}
}

func TestIsInternalHost(t *testing.T) {
	tests := []struct {
		host     string
		expected bool
	}{
		// External hosts (allowed)
		{"example.com", false},
		{"api.example.com", false},
		{"example.com:8080", false},
		{"1.2.3.4", false},
		{"8.8.8.8", false},

		// Localhost
		{"localhost", true},
		{"localhost:8080", true},
		{"LOCALHOST", true},

		// Loopback
		{"127.0.0.1", true},
		{"127.0.0.1:80", true},
		{"127.255.255.255", true},

		// 10.x.x.x
		{"10.0.0.1", true},
		{"10.0.0.1:443", true},

		// 172.16-31.x.x
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"172.15.0.1", false}, // Not in private range
		{"172.32.0.1", false}, // Not in private range

		// 192.168.x.x
		{"192.168.0.1", true},
		{"192.168.1.1:22", true},

		// Link-local
		{"169.254.0.1", true},
		{"169.254.169.254", true},

		// Internal suffixes
		{"server.local", true},
		{"server.internal", true},
		{"server.localhost", true},
		{"server.localdomain", true},

		// IPv6 - note: ::1 loopback detection depends on implementation
		// The current implementation checks for fe80:/fc/fd prefixes but not ::1 directly
		{"fe80::1", true},
		{"[fe80::1]", true},
		{"fc00::1", true},
		{"fd00::1", true},

		// Metadata
		{"169.254.169.254", true},
		{"metadata.google.internal", true},
		{"metadata.goog", true},
	}

	for _, tt := range tests {
		result := isInternalHost(tt.host)
		if result != tt.expected {
			t.Errorf("isInternalHost(%q) = %v, want %v", tt.host, result, tt.expected)
		}
	}
}

func TestIsInternalHost_172Range(t *testing.T) {
	// Test edge cases of 172.16-31 range
	for second := 0; second <= 35; second++ {
		host := "172." + string(rune('0'+second/10)) + string(rune('0'+second%10)) + ".0.1"
		if second < 10 {
			host = "172." + string(rune('0'+second)) + ".0.1"
		}
		isPrivate := second >= 16 && second <= 31
		result := isInternalHost(host)
		if result != isPrivate {
			t.Errorf("isInternalHost(%q) = %v, want %v (second octet: %d)", host, result, isPrivate, second)
		}
	}
}

func TestErrors(t *testing.T) {
	errors := []error{
		ErrSigningKeyRequired,
		ErrSigningKeyTooShort,
		ErrDatabaseURLRequired,
		ErrRedisURLRequired,
		ErrInvalidURL,
		ErrInvalidURLScheme,
		ErrURLTooLong,
		ErrPayloadTooLarge,
		ErrInternalURLBlocked,
		ErrInvalidIdempotencyKey,
	}

	for _, err := range errors {
		if err == nil {
			t.Error("error should not be nil")
		}
		if err.Error() == "" {
			t.Errorf("error should have message: %v", err)
		}
	}
}

func TestValidateJSONSchema_Valid(t *testing.T) {
	validSchemas := [][]byte{
		nil,                          // Empty schema is valid
		[]byte(`{}`),                 // Empty object
		[]byte(`{"type": "object"}`), // Simple schema
		[]byte(`{"type": "object", "properties": {"name": {"type": "string"}}}`),                   // With properties
		[]byte(`{"type": "array", "items": {"type": "number"}}`),                                   // Array schema
		[]byte(`{"type": "object", "required": ["id"], "properties": {"id": {"type": "string"}}}`), // With required
	}

	for i, schema := range validSchemas {
		if err := ValidateJSONSchema(schema); err != nil {
			t.Errorf("ValidateJSONSchema[%d](%q) = %v, want nil", i, string(schema), err)
		}
	}
}

func TestValidateJSONSchema_Invalid(t *testing.T) {
	invalidSchemas := [][]byte{
		[]byte(`not json`),                 // Not valid JSON
		[]byte(`{"type": "invalid_type"}`), // Invalid type
		[]byte(`{"type": 123}`),            // Type should be string
	}

	for i, schema := range invalidSchemas {
		if err := ValidateJSONSchema(schema); err == nil {
			t.Errorf("ValidateJSONSchema[%d](%q) = nil, want error", i, string(schema))
		}
	}
}

func TestValidatePayloadAgainstSchema_Valid(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"required": ["order_id"],
		"properties": {
			"order_id": {"type": "string"},
			"amount": {"type": "number"}
		}
	}`)

	validPayloads := [][]byte{
		[]byte(`{"order_id": "123"}`),
		[]byte(`{"order_id": "abc", "amount": 99.99}`),
		[]byte(`{"order_id": "test", "extra": "allowed"}`), // Extra properties allowed by default
	}

	for i, payload := range validPayloads {
		if err := ValidatePayloadAgainstSchema(payload, schema); err != nil {
			t.Errorf("ValidatePayloadAgainstSchema[%d](%q) = %v, want nil", i, string(payload), err)
		}
	}
}

func TestValidatePayloadAgainstSchema_Invalid(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"required": ["order_id"],
		"properties": {
			"order_id": {"type": "string"},
			"amount": {"type": "number"}
		}
	}`)

	invalidPayloads := [][]byte{
		[]byte(`{}`),                // Missing required field
		[]byte(`{"order_id": 123}`), // Wrong type for order_id
		[]byte(`{"order_id": "abc", "amount": "str"}`), // Wrong type for amount
	}

	for i, payload := range invalidPayloads {
		if err := ValidatePayloadAgainstSchema(payload, schema); err == nil {
			t.Errorf("ValidatePayloadAgainstSchema[%d](%q) = nil, want error", i, string(payload))
		}
	}
}

func TestValidatePayloadAgainstSchema_NoSchema(t *testing.T) {
	// When no schema is provided, any payload should be valid
	payloads := [][]byte{
		[]byte(`{}`),
		[]byte(`{"anything": "goes"}`),
		[]byte(`[1, 2, 3]`),
	}

	for i, payload := range payloads {
		if err := ValidatePayloadAgainstSchema(payload, nil); err != nil {
			t.Errorf("ValidatePayloadAgainstSchema[%d] with nil schema = %v, want nil", i, err)
		}
		if err := ValidatePayloadAgainstSchema(payload, []byte{}); err != nil {
			t.Errorf("ValidatePayloadAgainstSchema[%d] with empty schema = %v, want nil", i, err)
		}
	}
}

func TestValidatePayloadAgainstSchema_InvalidJSON(t *testing.T) {
	schema := []byte(`{"type": "object"}`)
	invalidJSON := []byte(`not valid json`)

	err := ValidatePayloadAgainstSchema(invalidJSON, schema)
	if err == nil {
		t.Error("ValidatePayloadAgainstSchema with invalid JSON = nil, want error")
	}
}

// clearEnv removes all test-relevant environment variables
func clearEnv() {
	envVars := []string{
		"DATABASE_URL", "DATABASE_MAX_CONNS", "DATABASE_MIN_CONNS",
		"DATABASE_MAX_CONN_LIFETIME", "DATABASE_MAX_CONN_IDLE_TIME",
		"REDIS_URL", "REDIS_POOL_SIZE", "REDIS_READ_TIMEOUT", "REDIS_WRITE_TIMEOUT",
		"API_ADDR", "API_READ_TIMEOUT", "API_WRITE_TIMEOUT",
		"API_IDLE_TIMEOUT", "API_SHUTDOWN_TIMEOUT",
		"WORKER_CONCURRENCY", "WORKER_VISIBILITY_TIMEOUT", "WORKER_SHUTDOWN_TIMEOUT",
		"SIGNING_KEY",
		"OUTBOX_POLL_INTERVAL", "OUTBOX_BATCH_SIZE",
		"OUTBOX_CLEANUP_INTERVAL", "OUTBOX_RETENTION_PERIOD",
		"AUTH_ENABLED", "ENABLE_PLAYGROUND",
		"METRICS_PROVIDER", "METRICS_ENDPOINT",
		"SERVICE_NAME", "SERVICE_VERSION", "ENVIRONMENT",
		"TEST_VAR", "TEST_INT", "TEST_DUR", "TEST_BOOL",
	}
	for _, v := range envVars {
		_ = os.Unsetenv(v)
	}
}
