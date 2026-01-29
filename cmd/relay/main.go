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
	case "start", "all":
		runStart()
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
	case "apikey":
		if err := cmdAPIKey(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
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

Getting Started:
  relay start --docker      Start with Docker (easiest, no setup required)
  relay start               Start with existing PostgreSQL/Redis

Commands:
  start           Start Relay (API server + worker)
                  --docker    Auto-start PostgreSQL and Redis containers

  send            Send a webhook event
                  --dest      Destination URL (required)
                  --payload   JSON payload (required)
                  --key       Idempotency key (optional)
                  --header    Custom header (can repeat)

  get <id>        Get event details by ID

  list            List events
                  --status    Filter by status (queued/delivered/failed/dead)
                  --limit     Max results (default: 20)

  replay <id>     Replay a failed or dead event

  stats           Show queue statistics

  health          Check service health

  apikey          Manage API keys
                  create      Create a new API key
                  list        List API keys for a client
                  revoke      Revoke an API key

  version         Show version
  help            Show this help

Environment Variables:
  DATABASE_URL     PostgreSQL connection string (required unless --docker)
  REDIS_URL        Redis address (default: localhost:6379)
  SIGNING_KEY      Webhook signing key (auto-generated if not set)
  API_ADDR         API server address (default: :8080)
  RELAY_URL        Base URL for CLI (default: http://localhost:8080)
  RELAY_API_KEY    API key for CLI authentication

Examples:
  # Start with Docker (easiest)
  relay start --docker

  # Start with existing database
  export DATABASE_URL="postgres://user:pass@localhost:5432/relay"
  relay start

  # Send a webhook
  relay send --dest https://example.com/webhook --payload '{"order": 123}'

  # List failed events
  relay list --status failed

  # Replay a failed event
  relay replay abc123

  # Create an API key
  relay apikey create --client myapp --name "Production"

Documentation: https://github.com/stiffinWanjohi/relay
`)
}
