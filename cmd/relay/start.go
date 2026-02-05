package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stiffinWanjohi/relay/internal/api"
	"github.com/stiffinWanjohi/relay/internal/app"
	"github.com/stiffinWanjohi/relay/internal/auth"
	"github.com/stiffinWanjohi/relay/internal/config"
	"github.com/stiffinWanjohi/relay/internal/dedup"
	"github.com/stiffinWanjohi/relay/internal/delivery"
	"github.com/stiffinWanjohi/relay/internal/event"
	"github.com/stiffinWanjohi/relay/internal/eventtype"
	_ "github.com/stiffinWanjohi/relay/internal/observability/otel"
	"github.com/stiffinWanjohi/relay/internal/outbox"
	"github.com/stiffinWanjohi/relay/internal/queue"
	"github.com/stiffinWanjohi/relay/migrations"
)

func runStart() {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	useDocker := fs.Bool("docker", false, "Auto-start PostgreSQL and Redis using Docker")
	_ = fs.Parse(os.Args[2:])

	fmt.Println()
	fmt.Println(bold("  Relay") + dim(" - Webhook Delivery System"))
	fmt.Println()

	if *useDocker {
		printStep(1, 4, "Starting Docker services...\n")
		if err := startDockerServices(); err != nil {
			fmt.Fprintf(os.Stderr, "\n  %s %v\n", fail("Error:"), err)
			os.Exit(1)
		}
		fmt.Println()
	}

	if os.Getenv("DATABASE_URL") == "" {
		fmt.Fprintf(os.Stderr, "  %s %v\n", fail("Error:"), config.MissingDatabaseError())
		os.Exit(1)
	}

	signingKey, isNewKey, err := config.GetOrCreateSigningKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s %v\n", fail("Error:"), err)
		os.Exit(1)
	}
	_ = os.Setenv("SIGNING_KEY", signingKey)
	if isNewKey {
		dataDir, _ := config.GetRelayDataDir()
		fmt.Printf("  %s Generated signing key %s\n", success("✓"), dim("("+dataDir+"/signing.key)"))
		fmt.Println()
	}

	stepNum := 1
	totalSteps := 3
	if *useDocker {
		stepNum = 2
		totalSteps = 4
	}

	printStep(stepNum, totalSteps, "Running database migrations... ")
	if err := autoRunMigrations(); err != nil {
		printFailed()
		fmt.Fprintf(os.Stderr, "\n  %s %v\n", fail("Error:"), err)
		os.Exit(1)
	}
	printOK()
	stepNum++

	printStep(stepNum, totalSteps, "Connecting to services... ")
	cfg, err := config.LoadConfig()
	if err != nil {
		printFailed()
		fmt.Fprintf(os.Stderr, "\n  %s %v\n", fail("Error:"), err)
		os.Exit(1)
	}

	ctx := context.Background()
	services, err := app.InitAll(ctx, cfg)
	if err != nil {
		printFailed()
		fmt.Fprintf(os.Stderr, "\n  %s %v\n", fail("Error:"), err)
		os.Exit(1)
	}
	defer services.Close(ctx)
	printOK()
	stepNum++

	printStep(stepNum, totalSteps, "Checking API keys... ")
	apiKey, isNew, err := ensureDefaultAPIKey(services.Pool, *useDocker)
	if err != nil {
		printFailed()
		fmt.Fprintf(os.Stderr, "\n  %s %v\n", fail("Error:"), err)
		os.Exit(1)
	}
	if isNew {
		fmt.Println(success("CREATED"))
		printBox(
			success("NEW API KEY")+" (save this - shown only once!)",
			"",
			"  "+bold(apiKey),
			"",
			dim("Use in requests: X-API-Key: <key>"),
		)
		if *useDocker {
			dataDir, _ := config.GetRelayDataDir()
			fmt.Printf("\n  %s Saved to %s\n", success("✓"), dim(dataDir+"/api.key"))
		}
	} else if apiKey == "" {
		fmt.Println(yellow("NONE"))
		fmt.Println()
		fmt.Println(dim("  No API keys found. Create one with:"))
		fmt.Println("    " + cyan("./relay apikey create --client default --create-client"))
		fmt.Println()
	} else {
		printOK()
	}

	fmt.Println()
	runServer(services)
}

func autoRunMigrations() error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL environment variable is required")
	}

	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, databaseURL)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	err = m.Up()
	if errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	return err
}

func ensureDefaultAPIKey(pool *pgxpool.Pool, isDevMode bool) (string, bool, error) {
	ctx := context.Background()
	store := auth.NewStore(pool)

	keys, _ := store.ListAPIKeys(ctx, config.DefaultClientID)
	for _, key := range keys {
		if key.IsActive {
			return "exists", false, nil
		}
	}

	if !isDevMode {
		return "", false, nil
	}

	_, _ = store.CreateClient(ctx, auth.Client{
		ID:       config.DefaultClientID,
		Name:     config.DefaultClientName,
		IsActive: true,
	})

	rawKey, _, err := store.CreateAPIKey(ctx, config.DefaultClientID, config.DefaultAPIKeyName, nil, 1000, nil)
	if err != nil {
		return "", false, fmt.Errorf("failed to create API key: %w", err)
	}

	if err := config.SaveAPIKey(rawKey); err != nil {
		fmt.Fprintf(os.Stderr, "  %s could not save API key to file: %v\n", yellow("Warning:"), err)
	}

	return rawKey, true, nil
}

func runServer(svc *app.Services) {
	logger := svc.Logger
	cfg := svc.Config

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	// Print server info
	fmt.Println(dim("  ─────────────────────────────────────────────────────────"))
	printURL("API:       ", fmt.Sprintf("http://localhost%s", cfg.API.Addr))
	printURL("Playground:", fmt.Sprintf("http://localhost%s/playground", cfg.API.Addr))
	fmt.Println(dim("  ─────────────────────────────────────────────────────────"))
	fmt.Println()
	fmt.Println(dim("  Usage:"))
	fmt.Println("    " + dim("# Get your API key"))
	fmt.Println("    " + cyan("./relay apikey list --client default"))
	fmt.Println()
	fmt.Println("    " + dim("# Send a webhook"))
	fmt.Printf("    "+cyan("curl -X POST http://localhost%s/api/v1/events \\\n"), cfg.API.Addr)
	fmt.Println(cyan("      -H 'X-API-Key: <key>' -H 'Content-Type: application/json' \\"))
	fmt.Println(cyan("      -d '{\"destination\":\"https://example.com\",\"payload\":{\"hi\":\"there\"}}'"))
	fmt.Println()
	fmt.Println(dim("  Press Ctrl+C to stop"))
	fmt.Println()

	store := event.NewStore(svc.Pool)
	eventTypeStore := eventtype.NewStore(svc.Pool)
	q := queue.NewQueue(svc.Redis).WithMetrics(svc.Metrics)
	dedupChecker := dedup.NewChecker(svc.Redis)
	authStore := auth.NewStore(svc.Pool)
	rateLimiter := delivery.NewRateLimiter(svc.Redis)

	outboxProcessor := outbox.NewProcessor(store, q, outbox.ProcessorConfig{
		PollInterval:    cfg.Outbox.PollInterval,
		BatchSize:       cfg.Outbox.BatchSize,
		CleanupInterval: cfg.Outbox.CleanupInterval,
		RetentionPeriod: cfg.Outbox.RetentionPeriod,
	}).WithMetrics(svc.Metrics)
	outboxProcessor.Start(ctx)

	serverCfg := api.ServerConfig{
		EnableAuth:       cfg.Auth.Enabled,
		EnablePlayground: cfg.Auth.EnablePlayground,
	}
	server := api.NewServer(store, eventTypeStore, q, dedupChecker, authStore, serverCfg)

	httpServer := &http.Server{
		Addr:         cfg.API.Addr,
		Handler:      server.Handler(),
		ReadTimeout:  cfg.API.ReadTimeout,
		WriteTimeout: cfg.API.WriteTimeout,
		IdleTimeout:  cfg.API.IdleTimeout,
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
		}
	}()

	workerConfig := delivery.WorkerConfig{
		Concurrency:         cfg.Worker.Concurrency,
		VisibilityTime:      cfg.Worker.VisibilityTimeout,
		SigningKey:          cfg.Worker.SigningKey,
		CircuitConfig:       delivery.DefaultCircuitConfig(),
		Metrics:             svc.Metrics,
		RateLimiter:         rateLimiter,
		NotificationService: svc.Notification,
		NotifyOnTrip:        cfg.Notification.NotifyOnTrip,
		NotifyOnRecover:     cfg.Notification.NotifyOnRecover,
		EnablePriorityQueue: true, // Enable priority queue processing
	}

	worker := delivery.NewWorker(q, store, workerConfig)
	worker.Start(ctx)

	// Start FIFO worker for ordered delivery endpoints
	fifoWorkerConfig := delivery.FIFOWorkerConfig{
		SigningKey:          cfg.Worker.SigningKey,
		CircuitConfig:       delivery.DefaultCircuitConfig(),
		Metrics:             svc.Metrics,
		RateLimiter:         rateLimiter,
		NotificationService: svc.Notification,
		NotifyOnTrip:        cfg.Notification.NotifyOnTrip,
		NotifyOnRecover:     cfg.Notification.NotifyOnRecover,
	}
	fifoWorker := delivery.NewFIFOWorker(q, store, fifoWorkerConfig)
	fifoWorker.Start(ctx)

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				recovered, err := q.RecoverStaleMessages(ctx, 5*time.Minute)
				if err != nil {
					logger.Error("failed to recover stale messages", "error", err)
				} else if recovered > 0 {
					logger.Info("recovered stale messages", "count", recovered)
				}
			}
		}
	}()

	logger.Info("relay started", "addr", cfg.API.Addr, "workers", cfg.Worker.Concurrency)

	app.WaitForShutdown()

	fmt.Println()
	fmt.Println(dim("  Shutting down..."))

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.API.ShutdownTimeout)
	defer shutdownCancel()

	_ = httpServer.Shutdown(shutdownCtx)
	_ = worker.StopAndWait(cfg.Worker.ShutdownTimeout)
	_ = fifoWorker.StopAndWait(cfg.Worker.ShutdownTimeout)
	_ = outboxProcessor.StopAndWait(cfg.API.ShutdownTimeout)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}

	fmt.Println(dim("  Stopped"))
}
