package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// composeConfig represents the docker compose file structure.
type composeConfig struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image         string            `yaml:"image"`
	ContainerName string            `yaml:"container_name"`
	Ports         []string          `yaml:"ports"`
	Environment   map[string]string `yaml:"environment"`
}

// dockerConfig holds parsed configuration from compose.yaml.
type dockerConfig struct {
	// PostgreSQL
	PostgresImage     string
	PostgresContainer string
	PostgresPort      string
	PostgresUser      string
	PostgresPassword  string
	PostgresDB        string
	// Redis
	RedisImage     string
	RedisContainer string
	RedisPort      string
	// API
	APIPort string
}

// loadDockerConfig reads all configuration from compose.yaml.
func loadDockerConfig() (*dockerConfig, error) {
	// Try compose.yaml first, then docker-compose.yaml
	data, err := os.ReadFile("compose.yaml")
	if err != nil {
		data, err = os.ReadFile("docker-compose.yaml")
		if err != nil {
			return nil, fmt.Errorf("compose.yaml not found. Create one or use manual setup:\n\n" +
				"  export DATABASE_URL=\"postgres://user:pass@localhost:5432/relay?sslmode=disable\"\n" +
				"  export REDIS_URL=\"localhost:6379\"\n" +
				"  relay start")
		}
	}

	var compose composeConfig
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return nil, fmt.Errorf("failed to parse compose.yaml: %w", err)
	}

	cfg := &dockerConfig{}

	// Parse postgres service
	pg, ok := compose.Services["postgres"]
	if !ok {
		return nil, fmt.Errorf("postgres service not found in compose.yaml")
	}
	cfg.PostgresImage = pg.Image
	cfg.PostgresContainer = pg.ContainerName
	if len(pg.Ports) > 0 {
		cfg.PostgresPort = parseHostPort(pg.Ports[0])
	}
	if pg.Environment != nil {
		cfg.PostgresUser = pg.Environment["POSTGRES_USER"]
		cfg.PostgresPassword = pg.Environment["POSTGRES_PASSWORD"]
		cfg.PostgresDB = pg.Environment["POSTGRES_DB"]
	}

	// Parse redis service
	redis, ok := compose.Services["redis"]
	if !ok {
		return nil, fmt.Errorf("redis service not found in compose.yaml")
	}
	cfg.RedisImage = redis.Image
	cfg.RedisContainer = redis.ContainerName
	if len(redis.Ports) > 0 {
		cfg.RedisPort = parseHostPort(redis.Ports[0])
	}

	// Parse relay service for port
	if relay, ok := compose.Services["relay"]; ok {
		if len(relay.Ports) > 0 {
			cfg.APIPort = parseHostPort(relay.Ports[0])
		}
	}

	// Validate required fields
	if cfg.PostgresImage == "" {
		return nil, fmt.Errorf("postgres image not specified in compose.yaml")
	}
	if cfg.PostgresContainer == "" {
		return nil, fmt.Errorf("postgres container_name not specified in compose.yaml")
	}
	if cfg.PostgresPort == "" {
		return nil, fmt.Errorf("postgres port not specified in compose.yaml")
	}
	if cfg.RedisImage == "" {
		return nil, fmt.Errorf("redis image not specified in compose.yaml")
	}
	if cfg.RedisContainer == "" {
		return nil, fmt.Errorf("redis container_name not specified in compose.yaml")
	}
	if cfg.RedisPort == "" {
		return nil, fmt.Errorf("redis port not specified in compose.yaml")
	}

	return cfg, nil
}

// parseHostPort extracts the host port from a port mapping like "5434:5432".
func parseHostPort(portMapping string) string {
	parts := strings.Split(portMapping, ":")
	if len(parts) >= 1 {
		return strings.TrimSpace(parts[0])
	}
	return portMapping
}

// dockerAvailable checks if Docker is available.
func dockerAvailable() bool {
	cmd := exec.Command("docker", "info")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// containerRunning checks if a container is running.
func containerRunning(name string) bool {
	cmd := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", name)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "true"
}

// containerExists checks if a container exists (running or stopped).
func containerExists(name string) bool {
	cmd := exec.Command("docker", "inspect", name)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// startPostgres starts or creates the PostgreSQL container.
func startPostgres(cfg *dockerConfig) error {
	if containerRunning(cfg.PostgresContainer) {
		return nil
	}

	if containerExists(cfg.PostgresContainer) {
		cmd := exec.Command("docker", "start", cfg.PostgresContainer)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	cmd := exec.Command("docker", "run", "-d",
		"--name", cfg.PostgresContainer,
		"-e", "POSTGRES_USER="+cfg.PostgresUser,
		"-e", "POSTGRES_PASSWORD="+cfg.PostgresPassword,
		"-e", "POSTGRES_DB="+cfg.PostgresDB,
		"-p", cfg.PostgresPort+":5432",
		"--restart", "unless-stopped",
		cfg.PostgresImage,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// startRedis starts or creates the Redis container.
func startRedis(cfg *dockerConfig) error {
	if containerRunning(cfg.RedisContainer) {
		return nil
	}

	if containerExists(cfg.RedisContainer) {
		cmd := exec.Command("docker", "start", cfg.RedisContainer)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	cmd := exec.Command("docker", "run", "-d",
		"--name", cfg.RedisContainer,
		"-p", cfg.RedisPort+":6379",
		"--restart", "unless-stopped",
		cfg.RedisImage,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// waitForPostgres waits for PostgreSQL to be ready via TCP connection.
func waitForPostgres(ctx context.Context, cfg *dockerConfig) error {
	addr := "localhost:" + cfg.PostgresPort
	for range 30 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Try TCP connection to verify port is accessible
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_ = conn.Close()
			// Also verify PostgreSQL is accepting queries
			if canConnectPostgres(cfg) {
				return nil
			}
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("PostgreSQL did not become ready in time")
}

// canConnectPostgres checks if PostgreSQL accepts connections.
func canConnectPostgres(cfg *dockerConfig) bool {
	cmd := exec.Command("docker", "exec", cfg.PostgresContainer,
		"pg_isready", "-U", cfg.PostgresUser, "-d", cfg.PostgresDB)
	return cmd.Run() == nil
}

// waitForRedis waits for Redis to be ready via TCP connection.
func waitForRedis(ctx context.Context, cfg *dockerConfig) error {
	addr := "localhost:" + cfg.RedisPort
	for range 30 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Try TCP connection first
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_ = conn.Close()
			// Verify Redis responds to PING
			cmd := exec.Command("docker", "exec", cfg.RedisContainer, "redis-cli", "ping")
			output, err := cmd.Output()
			if err == nil && strings.TrimSpace(string(output)) == "PONG" {
				return nil
			}
		}
		time.Sleep(1 * time.Second)
	}
	return errors.New("redis did not become ready in time")
}

// stopContainerIfRunning stops a container if it's running.
func stopContainerIfRunning(name string) {
	if containerRunning(name) {
		cmd := exec.Command("docker", "stop", name)
		_ = cmd.Run()
	}
}

// startDockerServices starts PostgreSQL and Redis containers.
// Stops any running api/worker containers to avoid port conflicts.
// Sets DATABASE_URL, REDIS_URL, and API_ADDR environment variables from compose.yaml.
func startDockerServices() error {
	if !dockerAvailable() {
		return errors.New("docker is not available. Please install Docker or use manual setup:\n\n" +
			"  export DATABASE_URL=\"postgres://user:pass@localhost:5432/relay?sslmode=disable\"\n" +
			"  export REDIS_URL=\"localhost:6379\"\n" +
			"  relay start")
	}

	cfg, err := loadDockerConfig()
	if err != nil {
		return err
	}

	// Stop containerized relay if running (we'll run it locally)
	stopContainerIfRunning("relay-app")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Start PostgreSQL
	fmt.Print("  Starting PostgreSQL... ")
	if err := startPostgres(cfg); err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("failed to start PostgreSQL: %w", err)
	}
	if err := waitForPostgres(ctx, cfg); err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("PostgreSQL not ready: %w", err)
	}
	fmt.Println("OK")

	// Start Redis
	fmt.Print("  Starting Redis... ")
	if err := startRedis(cfg); err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("failed to start Redis: %w", err)
	}
	if err := waitForRedis(ctx, cfg); err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("redis not ready: %w", err)
	}
	fmt.Println("OK")

	// Set environment variables from compose.yaml
	_ = os.Setenv("DATABASE_URL", fmt.Sprintf("postgres://%s:%s@localhost:%s/%s?sslmode=disable",
		cfg.PostgresUser, cfg.PostgresPassword, cfg.PostgresPort, cfg.PostgresDB))
	_ = os.Setenv("REDIS_URL", "localhost:"+cfg.RedisPort)

	// Set API port if specified in compose.yaml
	if cfg.APIPort != "" {
		_ = os.Setenv("API_ADDR", ":"+cfg.APIPort)
	}

	return nil
}
