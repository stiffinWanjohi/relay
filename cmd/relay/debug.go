package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const defaultDebugBaseURL = "http://localhost:8080"

func getDebugBaseURL() string {
	baseURL := os.Getenv("RELAY_URL")
	if baseURL == "" {
		baseURL = defaultDebugBaseURL
	}
	return baseURL
}

type debugEndpoint struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	URL       string    `json:"url"`
}

type capturedRequest struct {
	ID          string            `json:"id"`
	ReceivedAt  time.Time         `json:"received_at"`
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	Headers     map[string]string `json:"headers"`
	Body        string            `json:"body"`
	ContentType string            `json:"content_type"`
	RemoteAddr  string            `json:"remote_addr"`
	DurationNs  int64             `json:"duration_ns"`
}

func runDebug() {
	fs := flag.NewFlagSet("debug", flag.ExitOnError)
	listOnly := fs.Bool("list", false, "List captured requests and exit")
	endpointID := fs.String("endpoint", "", "Use existing endpoint ID")
	_ = fs.Parse(os.Args[2:])

	baseURL := getDebugBaseURL()
	apiKey := os.Getenv("RELAY_API_KEY")

	fmt.Println()
	fmt.Println(bold("  Webhook Debugger"))
	fmt.Println()

	var endpoint *debugEndpoint
	var err error

	if *endpointID != "" {
		// Use existing endpoint
		endpoint, err = getDebugEndpoint(baseURL, apiKey, *endpointID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s %v\n", fail("Error:"), err)
			os.Exit(1)
		}
		if endpoint == nil {
			fmt.Fprintf(os.Stderr, "  %s Endpoint not found or expired\n", fail("Error:"))
			os.Exit(1)
		}
	} else {
		// Create new endpoint
		fmt.Print("  Creating debug endpoint... ")
		endpoint, err = createDebugEndpoint(baseURL, apiKey)
		if err != nil {
			fmt.Println(fail("FAILED"))
			fmt.Fprintf(os.Stderr, "\n  %s %v\n", fail("Error:"), err)
			os.Exit(1)
		}
		fmt.Println(success("OK"))
	}

	fmt.Println()
	fmt.Println(dim("  ─────────────────────────────────────────────────────────"))
	fmt.Printf("  Endpoint ID: %s\n", bold(endpoint.ID))
	fmt.Printf("  Webhook URL: %s\n", cyan(endpoint.URL))
	fmt.Printf("  Expires:     %s\n", dim(endpoint.ExpiresAt.Format(time.RFC3339)))
	fmt.Println(dim("  ─────────────────────────────────────────────────────────"))
	fmt.Println()

	if *listOnly {
		// Just list requests and exit
		listCapturedRequests(baseURL, apiKey, endpoint.ID)
		return
	}

	fmt.Println(dim("  Send webhooks to the URL above. Requests will appear here."))
	fmt.Println(dim("  Press Ctrl+C to stop."))
	fmt.Println()

	// Set up signal handler
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println()
		fmt.Println(dim("  Stopping..."))
		cancel()
	}()

	// Stream requests via SSE
	streamRequests(ctx, baseURL, apiKey, endpoint.ID)

	fmt.Println()
	fmt.Printf("  Debug session ended. Endpoint expires at %s\n", dim(endpoint.ExpiresAt.Format(time.RFC3339)))
	fmt.Println()
}

func createDebugEndpoint(baseURL, apiKey string) (*debugEndpoint, error) {
	req, err := http.NewRequest(http.MethodPost, baseURL+"/debug/endpoints", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var endpoint debugEndpoint
	if err := json.NewDecoder(resp.Body).Decode(&endpoint); err != nil {
		return nil, err
	}

	return &endpoint, nil
}

func getDebugEndpoint(baseURL, apiKey, endpointID string) (*debugEndpoint, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/debug/endpoints/"+endpointID, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var endpoint debugEndpoint
	if err := json.NewDecoder(resp.Body).Decode(&endpoint); err != nil {
		return nil, err
	}

	return &endpoint, nil
}

func listCapturedRequests(baseURL, apiKey, endpointID string) {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/debug/endpoints/"+endpointID+"/requests", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s %v\n", fail("Error:"), err)
		return
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s %v\n", fail("Error:"), err)
		return
	}
	defer resp.Body.Close()

	var result struct {
		Requests []*capturedRequest `json:"requests"`
		Count    int                `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "  %s %v\n", fail("Error:"), err)
		return
	}

	if result.Count == 0 {
		fmt.Println("  No requests captured yet.")
		return
	}

	fmt.Printf("  Captured %d request(s):\n\n", result.Count)
	for _, r := range result.Requests {
		printCapturedRequest(r)
	}
}

func streamRequests(ctx context.Context, baseURL, apiKey, endpointID string) {
	url := baseURL + "/debug/endpoints/" + endpointID + "/stream"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s %v\n", fail("Error:"), err)
		return
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 0} // No timeout for SSE
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return // Context cancelled, normal exit
		}
		fmt.Fprintf(os.Stderr, "  %s %v\n", fail("Error:"), err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "  %s Server returned %d\n", fail("Error:"), resp.StatusCode)
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	var eventType string
	var data strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// End of event
			if eventType == "request" && data.Len() > 0 {
				var req capturedRequest
				if err := json.Unmarshal([]byte(data.String()), &req); err == nil {
					printCapturedRequest(&req)
				}
			}
			eventType = ""
			data.Reset()
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			data.WriteString(strings.TrimPrefix(line, "data: "))
		}
	}
}

func printCapturedRequest(r *capturedRequest) {
	fmt.Printf("  %s %s %s\n",
		dim(r.ReceivedAt.Format("15:04:05")),
		methodColor(r.Method),
		r.Path,
	)
	fmt.Printf("  %s ID: %s\n", dim("│"), dim(r.ID))
	fmt.Printf("  %s From: %s\n", dim("│"), dim(r.RemoteAddr))
	if r.ContentType != "" {
		fmt.Printf("  %s Content-Type: %s\n", dim("│"), dim(r.ContentType))
	}
	if len(r.Headers) > 0 {
		fmt.Printf("  %s Headers:\n", dim("│"))
		for k, v := range r.Headers {
			if k != "Content-Type" && k != "Content-Length" {
				fmt.Printf("  %s   %s: %s\n", dim("│"), dim(k), dim(truncate(v, 60)))
			}
		}
	}
	if r.Body != "" {
		body := r.Body
		if len(body) > 200 {
			body = body[:200] + "..."
		}
		fmt.Printf("  %s Body: %s\n", dim("│"), body)
	}
	fmt.Printf("  %s\n", dim("└──"))
	fmt.Println()
}

func methodColor(method string) string {
	switch method {
	case "GET":
		return cyan(method)
	case "POST":
		return green(method)
	case "PUT", "PATCH":
		return yellow(method)
	case "DELETE":
		return red(method)
	default:
		return method
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func green(s string) string {
	return "\033[32m" + s + "\033[0m"
}

func red(s string) string {
	return "\033[31m" + s + "\033[0m"
}
