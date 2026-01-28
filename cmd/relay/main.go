package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]

	switch cmd {
	case "serve", "server", "api":
		runServe()
	case "worker":
		runWorker()
	case "all":
		runAll()
	case "send", "create":
		runCLI("send", os.Args[2:])
	case "get":
		runCLI("get", os.Args[2:])
	case "list", "ls":
		runCLI("list", os.Args[2:])
	case "replay":
		runCLI("replay", os.Args[2:])
	case "stats":
		runCLI("stats", os.Args[2:])
	case "health":
		runCLI("health", os.Args[2:])
	case "openapi":
		runCLI("openapi", os.Args[2:])
	case "version", "-v", "--version":
		fmt.Printf("relay version %s\n", version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`relay - High-performance webhook delivery system

Usage:
  relay <command> [arguments]

Server Commands:
  serve     Start the API server
  worker    Start the background worker
  all       Start both API server and worker (for development)

CLI Commands:
  send      Create and send a webhook event
  get       Get event details by ID
  list      List events (optionally filtered by status)
  replay    Replay a failed/dead event
  stats     Show queue statistics
  health    Check service health
  openapi   Show or serve OpenAPI specification

Other:
  version   Show version information
  help      Show this help message

Environment Variables:
  DATABASE_URL     PostgreSQL connection string
  REDIS_URL        Redis address (default: localhost:6379)
  SIGNING_KEY      Webhook signing key (min 32 chars)
  API_ADDR         API server address (default: :8080)
  RELAY_URL        Base URL for CLI commands (default: http://localhost:8080)
  RELAY_API_KEY    API key for CLI authentication

Examples:
  # Start the API server
  relay serve

  # Start the worker
  relay worker

  # Start both (development)
  relay all

  # Send a webhook via CLI
  relay send --dest https://example.com/webhook --payload '{"order_id": 123}'

  # List failed events
  relay list --status failed

  # Check queue stats
  relay stats

Documentation:
  https://github.com/stiffinWanjohi/relay
`)
}
