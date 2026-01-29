package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/stiffinWanjohi/relay/internal/app"
	"github.com/stiffinWanjohi/relay/internal/auth"
	"github.com/stiffinWanjohi/relay/internal/config"
)

func cmdAPIKey(args []string) error {
	if len(args) == 0 {
		printAPIKeyUsage()
		return nil
	}

	switch args[0] {
	case "create":
		return cmdAPIKeyCreate(args[1:])
	case "list", "ls":
		return cmdAPIKeyList(args[1:])
	case "revoke":
		return cmdAPIKeyRevoke(args[1:])
	case "help", "-h", "--help":
		printAPIKeyUsage()
		return nil
	default:
		return fmt.Errorf("unknown apikey subcommand: %s", args[0])
	}
}

func printAPIKeyUsage() {
	fmt.Print(`relay apikey - Manage API keys

Usage:
  relay apikey <command> [options]

Commands:
  create    Create a new API key
  list      List all API keys for a client
  revoke    Revoke an API key

Examples:
  # Create a new API key
  relay apikey create --client myapp --name "Production Key"

  # Create with expiration and rate limit
  relay apikey create --client myapp --name "Test Key" --expires 30d --rate-limit 100

  # List API keys for a client
  relay apikey list --client myapp

  # Revoke an API key
  relay apikey revoke --id <key-id>

`)
}

func cmdAPIKeyCreate(args []string) error {
	fs := flag.NewFlagSet("apikey create", flag.ExitOnError)
	clientID := fs.String("client", "", "Client ID (required)")
	name := fs.String("name", "", "Key name/description")
	rateLimit := fs.Int("rate-limit", 1000, "Rate limit (requests per minute)")
	expires := fs.String("expires", "", "Expiration duration (e.g., 30d, 1y, 24h)")
	createClient := fs.Bool("create-client", false, "Create client if it doesn't exist")
	clientName := fs.String("client-name", "", "Client name (used with --create-client)")
	clientEmail := fs.String("client-email", "", "Client email (used with --create-client)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *clientID == "" {
		return errors.New("--client is required")
	}

	ctx := context.Background()
	svc, err := getDBPool(ctx)
	if err != nil {
		return err
	}
	defer svc.Pool.Close()

	store := auth.NewStore(svc.Pool)

	// Check if client exists or create it
	_, err = store.GetClient(ctx, *clientID)
	if err != nil {
		if *createClient {
			cName := *clientName
			if cName == "" {
				cName = *clientID
			}
			_, err = store.CreateClient(ctx, auth.Client{
				ID:       *clientID,
				Name:     cName,
				Email:    *clientEmail,
				IsActive: true,
			})
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}
			fmt.Printf("Created client: %s\n", *clientID)
		} else {
			return fmt.Errorf("client %q not found (use --create-client to create it)", *clientID)
		}
	}

	// Parse expiration
	var expiresAt *time.Time
	if *expires != "" {
		duration, err := parseDuration(*expires)
		if err != nil {
			return fmt.Errorf("invalid expiration: %w", err)
		}
		t := time.Now().Add(duration)
		expiresAt = &t
	}

	// Create API key
	rawKey, apiKey, err := store.CreateAPIKey(ctx, *clientID, *name, nil, *rateLimit, expiresAt)
	if err != nil {
		return fmt.Errorf("failed to create API key: %w", err)
	}

	fmt.Println()
	fmt.Println("API Key created successfully!")
	fmt.Println()
	fmt.Printf("  Key ID:     %s\n", apiKey.ID)
	fmt.Printf("  Client:     %s\n", apiKey.ClientID)
	if apiKey.Name != "" {
		fmt.Printf("  Name:       %s\n", apiKey.Name)
	}
	fmt.Printf("  Rate Limit: %d req/min\n", apiKey.RateLimit)
	if expiresAt != nil {
		fmt.Printf("  Expires:    %s\n", expiresAt.Format(time.RFC3339))
	}
	fmt.Println()
	fmt.Println("  API Key (save this - it won't be shown again):")
	fmt.Printf("  %s\n", rawKey)
	fmt.Println()

	return nil
}

func cmdAPIKeyList(args []string) error {
	fs := flag.NewFlagSet("apikey list", flag.ExitOnError)
	clientID := fs.String("client", "", "Client ID (required)")
	showAll := fs.Bool("all", false, "Show revoked keys too")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *clientID == "" {
		return errors.New("--client is required")
	}

	ctx := context.Background()
	svc, err := getDBPool(ctx)
	if err != nil {
		return err
	}
	defer svc.Pool.Close()

	store := auth.NewStore(svc.Pool)

	keys, err := store.ListAPIKeys(ctx, *clientID)
	if err != nil {
		return fmt.Errorf("failed to list API keys: %w", err)
	}

	if len(keys) == 0 {
		fmt.Printf("No API keys found for client %q\n", *clientID)
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tPREFIX\tNAME\tRATE LIMIT\tSTATUS\tEXPIRES\tLAST USED")
	_, _ = fmt.Fprintln(w, "--\t------\t----\t----------\t------\t-------\t---------")

	for _, key := range keys {
		if !*showAll && !key.IsActive {
			continue
		}

		status := "active"
		if !key.IsActive {
			status = "revoked"
		} else if key.ExpiresAt != nil && time.Now().After(*key.ExpiresAt) {
			status = "expired"
		}

		expires := "-"
		if key.ExpiresAt != nil {
			expires = key.ExpiresAt.Format("2006-01-02")
		}

		lastUsed := "never"
		if key.LastUsedAt != nil {
			lastUsed = key.LastUsedAt.Format("2006-01-02 15:04")
		}

		name := key.Name
		if name == "" {
			name = "-"
		}

		_, _ = fmt.Fprintf(w, "%s\t%s...\t%s\t%d/min\t%s\t%s\t%s\n",
			key.ID, key.KeyPrefix, name, key.RateLimit, status, expires, lastUsed)
	}

	return w.Flush()
}

func cmdAPIKeyRevoke(args []string) error {
	fs := flag.NewFlagSet("apikey revoke", flag.ExitOnError)
	keyID := fs.String("id", "", "API Key ID to revoke (required)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *keyID == "" {
		return errors.New("--id is required")
	}

	ctx := context.Background()
	svc, err := getDBPool(ctx)
	if err != nil {
		return err
	}
	defer svc.Pool.Close()

	store := auth.NewStore(svc.Pool)

	err = store.RevokeAPIKey(ctx, *keyID)
	if err != nil {
		return fmt.Errorf("failed to revoke API key: %w", err)
	}

	fmt.Printf("API key %s has been revoked\n", *keyID)
	return nil
}

func getDBPool(ctx context.Context) (*app.Services, error) {
	databaseURL := os.Getenv("DATABASE_URL")

	// If DATABASE_URL not set, try to auto-detect from running compose containers
	if databaseURL == "" {
		databaseURL = detectDatabaseURL()
	}

	if databaseURL == "" {
		return nil, config.MissingDatabaseError()
	}

	// For apikey commands, we only need the database - create minimal config
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			URL:      databaseURL,
			MaxConns: 5,
			MinConns: 1,
		},
	}

	pool, err := app.ConnectPostgres(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return &app.Services{
		Config: cfg,
		Pool:   pool,
		Logger: app.NewLogger(),
	}, nil
}

// detectDatabaseURL tries to build DATABASE_URL from compose.yaml if containers are running.
func detectDatabaseURL() string {
	cfg, err := loadDockerConfig()
	if err != nil {
		return ""
	}

	// Check if postgres container is running
	if !containerRunning(cfg.PostgresContainer) {
		return ""
	}

	return fmt.Sprintf("postgres://%s:%s@localhost:%s/%s?sslmode=disable",
		cfg.PostgresUser, cfg.PostgresPassword, cfg.PostgresPort, cfg.PostgresDB)
}

// parseDuration parses duration strings like "30d", "1y", "24h"
func parseDuration(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, errors.New("invalid duration format")
	}

	unit := s[len(s)-1]
	value := s[:len(s)-1]

	var multiplier time.Duration
	switch unit {
	case 's':
		multiplier = time.Second
	case 'm':
		multiplier = time.Minute
	case 'h':
		multiplier = time.Hour
	case 'd':
		multiplier = 24 * time.Hour
	case 'w':
		multiplier = 7 * 24 * time.Hour
	case 'y':
		multiplier = 365 * 24 * time.Hour
	default:
		// Try standard Go duration parsing
		return time.ParseDuration(s)
	}

	var n int
	_, err := fmt.Sscanf(value, "%d", &n)
	if err != nil {
		return 0, fmt.Errorf("invalid duration value: %s", value)
	}

	return time.Duration(n) * multiplier, nil
}
