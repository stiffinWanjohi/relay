package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// RelayConfigDir is the directory name for Relay configuration
	RelayConfigDir = ".relay"

	// SigningKeyFile is the filename for the signing key
	SigningKeyFile = "signing.key"

	// APIKeyFile is the filename for the default API key
	APIKeyFile = "api.key"

	// SigningKeyLength is the number of bytes for the signing key (32 bytes = 256 bits)
	SigningKeyLength = 32

	// DefaultClientID is the default client ID for auto-created API keys
	DefaultClientID = "default"

	// DefaultClientName is the display name for the default client
	DefaultClientName = "Default Client"

	// DefaultAPIKeyName is the name for the auto-created API key
	DefaultAPIKeyName = "Default API Key"
)

// GetRelayDataDir returns the Relay data directory, creating it if necessary.
// Returns ~/.relay by default, or RELAY_DATA_DIR if set.
func GetRelayDataDir() (string, error) {
	if dir := os.Getenv("RELAY_DATA_DIR"); dir != "" {
		return ensureDir(dir)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	return ensureDir(filepath.Join(home, RelayConfigDir))
}

// ensureDir creates the directory if it doesn't exist and returns the path.
func ensureDir(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	return dir, nil
}

// GetOrCreateSigningKey returns the signing key, generating one if it doesn't exist.
// Priority: 1) SIGNING_KEY env var, 2) ~/.relay/signing.key file, 3) generate new
// Returns the key, whether it was newly created, and any error.
func GetOrCreateSigningKey() (string, bool, error) {
	// First check environment variable
	if key := os.Getenv("SIGNING_KEY"); key != "" {
		if len(key) < MinSigningKeyLength {
			return "", false, ErrSigningKeyTooShort
		}
		return key, false, nil
	}

	// Get data directory
	dataDir, err := GetRelayDataDir()
	if err != nil {
		return "", false, err
	}

	keyPath := filepath.Join(dataDir, SigningKeyFile)

	// Try to read existing key
	if data, err := os.ReadFile(keyPath); err == nil {
		key := string(data)
		if len(key) >= MinSigningKeyLength {
			return key, false, nil
		}
	}

	// Generate new key
	key, err := generateSecureKey(SigningKeyLength)
	if err != nil {
		return "", false, fmt.Errorf("failed to generate signing key: %w", err)
	}

	// Save to file
	if err := os.WriteFile(keyPath, []byte(key), 0600); err != nil {
		return "", false, fmt.Errorf("failed to save signing key: %w", err)
	}

	return key, true, nil
}

// generateSecureKey generates a cryptographically secure random key.
func generateSecureKey(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SaveAPIKey saves an API key to ~/.relay/api.key for CLI auto-detection.
func SaveAPIKey(apiKey string) error {
	dataDir, err := GetRelayDataDir()
	if err != nil {
		return err
	}

	keyPath := filepath.Join(dataDir, APIKeyFile)
	return os.WriteFile(keyPath, []byte(apiKey), 0600)
}

// GetSavedAPIKey returns the API key from ~/.relay/api.key if it exists.
// Returns empty string if not found or RELAY_API_KEY env var takes precedence.
func GetSavedAPIKey() string {
	// Environment variable takes precedence (for production)
	if key := os.Getenv("RELAY_API_KEY"); key != "" {
		return key
	}

	// Try to read from file (for dev)
	dataDir, err := GetRelayDataDir()
	if err != nil {
		return ""
	}

	keyPath := filepath.Join(dataDir, APIKeyFile)
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return ""
	}

	return string(data)
}

// MissingDatabaseError returns a helpful error when DATABASE_URL is missing.
func MissingDatabaseError() error {
	return fmt.Errorf(`DATABASE_URL is required

Quick start options:

  1. Auto-start with Docker (easiest):
     relay start --docker

  2. Use Docker Compose:
     docker compose up -d
     relay start

  3. Connect to existing PostgreSQL:
     export DATABASE_URL="postgres://user:pass@localhost:5432/relay?sslmode=disable"
     relay start

For more info: https://github.com/stiffinWanjohi/relay`)
}
