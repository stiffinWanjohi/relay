package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

const (
	defaultBaseURL = "http://localhost:8080"
	version        = "1.0.0"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	baseURL := os.Getenv("RELAY_URL")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	apiKey := os.Getenv("RELAY_API_KEY")

	client := &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 30 * time.Second},
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "send", "create":
		err = cmdSend(client, args)
	case "get":
		err = cmdGet(client, args)
	case "list", "ls":
		err = cmdList(client, args)
	case "replay":
		err = cmdReplay(client, args)
	case "stats":
		err = cmdStats(client)
	case "health":
		err = cmdHealth(client)
	case "version", "-v", "--version":
		fmt.Printf("relay version %s\n", version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`relay - Webhook delivery CLI

Usage:
  relay <command> [arguments]

Commands:
  send      Create and send a webhook event
  get       Get event details by ID
  list      List events (optionally filtered by status)
  replay    Replay a failed/dead event
  stats     Show queue statistics
  health    Check service health
  version   Show version information
  help      Show this help message

Environment Variables:
  RELAY_URL      Base URL of Relay API (default: http://localhost:8080)
  RELAY_API_KEY  API key for authentication

Examples:
  # Send a webhook
  relay send --dest https://example.com/webhook --payload '{"order_id": 123}'

  # Send with idempotency key
  relay send --dest https://example.com/webhook --payload '{"order_id": 123}' --key order-123

  # List failed events
  relay list --status failed

  # Get event details
  relay get <event-id>

  # Replay a dead event
  relay replay <event-id>

  # Check queue stats
  relay stats
`)
}

// Client handles API communication.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func (c *Client) do(method, path string, body any) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		var errResp map[string]any
		if json.Unmarshal(respBody, &errResp) == nil {
			if msg, ok := errResp["error"].(string); ok {
				return nil, fmt.Errorf("%s (HTTP %d)", msg, resp.StatusCode)
			}
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func cmdSend(c *Client, args []string) error {
	var dest, payload, key string
	var headers []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dest", "-d":
			if i+1 >= len(args) {
				return fmt.Errorf("--dest requires a value")
			}
			i++
			dest = args[i]
		case "--payload", "-p":
			if i+1 >= len(args) {
				return fmt.Errorf("--payload requires a value")
			}
			i++
			payload = args[i]
		case "--key", "-k":
			if i+1 >= len(args) {
				return fmt.Errorf("--key requires a value")
			}
			i++
			key = args[i]
		case "--header", "-H":
			if i+1 >= len(args) {
				return fmt.Errorf("--header requires a value")
			}
			i++
			headers = append(headers, args[i])
		default:
			// If no flag, treat as payload
			if payload == "" && !strings.HasPrefix(args[i], "-") {
				payload = args[i]
			}
		}
	}

	if dest == "" {
		return fmt.Errorf("--dest is required")
	}
	if payload == "" {
		return fmt.Errorf("--payload is required")
	}
	if key == "" {
		key = fmt.Sprintf("cli-%d", time.Now().UnixNano())
	}

	// Parse headers
	headerMap := make(map[string]string)
	for _, h := range headers {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			headerMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	// Parse payload as JSON
	var payloadJSON json.RawMessage
	if err := json.Unmarshal([]byte(payload), &payloadJSON); err != nil {
		return fmt.Errorf("invalid JSON payload: %v", err)
	}

	reqBody := map[string]any{
		"destination": dest,
		"payload":     payloadJSON,
	}
	if len(headerMap) > 0 {
		reqBody["headers"] = headerMap
	}

	req, _ := http.NewRequest("POST", c.baseURL+"/api/v1/events", nil)
	req.Header.Set("X-Idempotency-Key", key)

	// Build request manually to include idempotency header
	jsonBody, _ := json.Marshal(reqBody)
	httpReq, _ := http.NewRequest("POST", c.baseURL+"/api/v1/events", bytes.NewReader(jsonBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Idempotency-Key", key)
	if c.apiKey != "" {
		httpReq.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var event map[string]any
	if err := json.Unmarshal(body, &event); err != nil {
		return err
	}

	if resp.StatusCode == http.StatusConflict {
		fmt.Printf("Event already exists (idempotent):\n")
	} else if resp.StatusCode >= 400 {
		if msg, ok := event["error"].(string); ok {
			return fmt.Errorf("%s", msg)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	} else {
		fmt.Printf("Event created:\n")
	}

	printEvent(event)
	return nil
}

func cmdGet(c *Client, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("event ID required")
	}

	body, err := c.do("GET", "/api/v1/events/"+args[0], nil)
	if err != nil {
		return err
	}

	var event map[string]any
	if err := json.Unmarshal(body, &event); err != nil {
		return err
	}

	printEvent(event)

	// Print delivery attempts if present
	if attempts, ok := event["deliveryAttempts"].([]any); ok && len(attempts) > 0 {
		fmt.Printf("\nDelivery Attempts:\n")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "#\tStatus\tDuration\tTime\tError")
		fmt.Fprintln(w, "-\t------\t--------\t----\t-----")
		for _, a := range attempts {
			attempt := a.(map[string]any)
			num := int(attempt["attemptNumber"].(float64))
			statusCode := "-"
			if sc, ok := attempt["statusCode"].(float64); ok {
				statusCode = fmt.Sprintf("%d", int(sc))
			}
			duration := fmt.Sprintf("%dms", int(attempt["durationMs"].(float64)))
			timestamp := attempt["attemptedAt"].(string)
			errMsg := ""
			if e, ok := attempt["error"].(string); ok && e != "" {
				errMsg = e
			}
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", num, statusCode, duration, timestamp[:19], errMsg)
		}
		w.Flush()
	}

	return nil
}

func cmdList(c *Client, args []string) error {
	status := ""
	limit := "20"

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--status", "-s":
			if i+1 >= len(args) {
				return fmt.Errorf("--status requires a value")
			}
			i++
			status = args[i]
		case "--limit", "-l":
			if i+1 >= len(args) {
				return fmt.Errorf("--limit requires a value")
			}
			i++
			limit = args[i]
		}
	}

	path := "/api/v1/events?limit=" + limit
	if status != "" {
		path += "&status=" + status
	}

	body, err := c.do("GET", path, nil)
	if err != nil {
		return err
	}

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		return err
	}

	events, ok := resp["data"].([]any)
	if !ok {
		return fmt.Errorf("unexpected response format")
	}

	if len(events) == 0 {
		fmt.Println("No events found")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tStatus\tAttempts\tDestination\tCreated")
	fmt.Fprintln(w, "--\t------\t--------\t-----------\t-------")

	for _, e := range events {
		event := e.(map[string]any)
		id := event["id"].(string)[:8]
		status := event["status"].(string)
		attempts := fmt.Sprintf("%d/%d", int(event["attempts"].(float64)), int(event["maxAttempts"].(float64)))
		dest := event["destination"].(string)
		if len(dest) > 40 {
			dest = dest[:37] + "..."
		}
		created := event["createdAt"].(string)[:19]
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", id, status, attempts, dest, created)
	}
	w.Flush()

	if pagination, ok := resp["pagination"].(map[string]any); ok {
		if hasMore, ok := pagination["hasMore"].(bool); ok && hasMore {
			fmt.Println("\n(more events available, use --limit to see more)")
		}
	}

	return nil
}

func cmdReplay(c *Client, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("event ID required")
	}

	body, err := c.do("POST", "/api/v1/events/"+args[0]+"/replay", nil)
	if err != nil {
		return err
	}

	var event map[string]any
	if err := json.Unmarshal(body, &event); err != nil {
		return err
	}

	fmt.Println("Event replayed:")
	printEvent(event)
	return nil
}

func cmdStats(c *Client) error {
	body, err := c.do("GET", "/api/v1/stats", nil)
	if err != nil {
		return err
	}

	var stats map[string]any
	if err := json.Unmarshal(body, &stats); err != nil {
		return err
	}

	fmt.Println("Queue Statistics:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	
	for _, key := range []string{"queued", "delivering", "delivered", "failed", "dead", "pending", "processing", "delayed"} {
		if val, ok := stats[key]; ok {
			fmt.Fprintf(w, "  %s:\t%v\n", key, val)
		}
	}
	w.Flush()

	return nil
}

func cmdHealth(c *Client) error {
	body, err := c.do("GET", "/health", nil)
	if err != nil {
		return err
	}

	var health map[string]any
	if err := json.Unmarshal(body, &health); err != nil {
		return err
	}

	if status, ok := health["status"].(string); ok && status == "ok" {
		fmt.Println("✓ Relay is healthy")
		return nil
	}

	fmt.Println("✗ Relay is unhealthy")
	return fmt.Errorf("service unhealthy")
}

func printEvent(event map[string]any) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "  ID:\t%s\n", event["id"])
	fmt.Fprintf(w, "  Status:\t%s\n", event["status"])
	fmt.Fprintf(w, "  Destination:\t%s\n", event["destination"])
	fmt.Fprintf(w, "  Attempts:\t%v/%v\n", event["attempts"], event["maxAttempts"])
	if key, ok := event["idempotencyKey"]; ok {
		fmt.Fprintf(w, "  Idempotency Key:\t%s\n", key)
	}
	if created, ok := event["createdAt"]; ok {
		fmt.Fprintf(w, "  Created:\t%s\n", created)
	}
	if delivered, ok := event["deliveredAt"]; ok && delivered != nil {
		fmt.Fprintf(w, "  Delivered:\t%s\n", delivered)
	}
	w.Flush()
}
