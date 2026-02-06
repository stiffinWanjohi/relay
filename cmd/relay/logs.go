package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// LogEntry represents a delivery log entry from the stream.
type LogEntry struct {
	Timestamp   time.Time         `json:"timestamp"`
	Level       string            `json:"level"`
	EventID     string            `json:"event_id"`
	EventType   string            `json:"event_type,omitempty"`
	EndpointID  string            `json:"endpoint_id,omitempty"`
	Destination string            `json:"destination,omitempty"`
	Status      string            `json:"status"`
	StatusCode  int               `json:"status_code,omitempty"`
	DurationMs  int64             `json:"duration_ms,omitempty"`
	Error       string            `json:"error,omitempty"`
	Attempt     int               `json:"attempt,omitempty"`
	MaxAttempts int               `json:"max_attempts,omitempty"`
	ClientID    string            `json:"client_id,omitempty"`
	Message     string            `json:"message"`
	Fields      map[string]string `json:"fields,omitempty"`
}

func runLogs() {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	follow := fs.Bool("follow", false, "Follow log output (stream in real-time)")
	fs.BoolVar(follow, "f", false, "Follow log output (shorthand)")
	eventType := fs.String("event-type", "", "Filter by event type (comma-separated)")
	endpointID := fs.String("endpoint", "", "Filter by endpoint ID (comma-separated)")
	status := fs.String("status", "", "Filter by status (comma-separated: queued,delivering,delivered,failed,dead)")
	level := fs.String("level", "", "Minimum log level (debug, info, warn, error)")
	serverAddr := fs.String("server", "http://localhost:8080", "Relay server address")
	jsonOutput := fs.Bool("json", false, "Output as JSON")

	_ = fs.Parse(os.Args[2:])

	if !*follow {
		fmt.Println()
		fmt.Println(bold("  Relay Logs"))
		fmt.Println()
		fmt.Println(dim("  Use --follow (-f) to stream logs in real-time"))
		fmt.Println()
		fmt.Println("  Examples:")
		fmt.Println("    " + cyan("relay logs --follow"))
		fmt.Println("    " + cyan("relay logs -f --level=warn"))
		fmt.Println("    " + cyan("relay logs -f --event-type=order.created"))
		fmt.Println("    " + cyan("relay logs -f --status=failed,dead"))
		fmt.Println("    " + cyan("relay logs -f --endpoint=abc123"))
		fmt.Println()
		return
	}

	// Build query parameters
	params := url.Values{}
	if *eventType != "" {
		params.Set("event_type", *eventType)
	}
	if *endpointID != "" {
		params.Set("endpoint_id", *endpointID)
	}
	if *status != "" {
		params.Set("status", *status)
	}
	if *level != "" {
		params.Set("level", *level)
	}

	streamURL := fmt.Sprintf("%s/logs/stream", strings.TrimSuffix(*serverAddr, "/"))
	if len(params) > 0 {
		streamURL += "?" + params.Encode()
	}

	if !*jsonOutput {
		fmt.Println()
		fmt.Println(bold("  Relay Logs") + dim(" - Streaming delivery logs"))
		fmt.Println(dim("  Press Ctrl+C to stop"))
		fmt.Println()
	}

	// Connect to SSE stream
	req, err := http.NewRequest("GET", streamURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s failed to create request: %v\n", fail("Error:"), err)
		os.Exit(1)
	}
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{
		Timeout: 0, // No timeout for streaming
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s failed to connect: %v\n", fail("Error:"), err)
		os.Exit(1)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "  %s server returned %d\n", fail("Error:"), resp.StatusCode)
		os.Exit(1)
	}

	// Read SSE stream
	scanner := bufio.NewScanner(resp.Body)
	var dataBuffer strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data: ") {
			dataBuffer.WriteString(strings.TrimPrefix(line, "data: "))
		} else if line == "" && dataBuffer.Len() > 0 {
			// End of event, process data
			data := dataBuffer.String()
			dataBuffer.Reset()

			var entry LogEntry
			if err := json.Unmarshal([]byte(data), &entry); err != nil {
				continue
			}

			if *jsonOutput {
				fmt.Println(data)
			} else {
				printLogEntry(&entry)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "\n  %s stream error: %v\n", fail("Error:"), err)
		os.Exit(1)
	}
}

func printLogEntry(entry *LogEntry) {
	// Format timestamp
	ts := entry.Timestamp.Local().Format("15:04:05")

	// Format level with color
	var levelStr string
	switch entry.Level {
	case "debug":
		levelStr = dim("DBG")
	case "info":
		levelStr = cyan("INF")
	case "warn":
		levelStr = yellow("WRN")
	case "error":
		levelStr = fail("ERR")
	default:
		levelStr = entry.Level
	}

	// Format status with color
	var statusStr string
	switch entry.Status {
	case "delivering":
		statusStr = cyan(entry.Status)
	case "delivered":
		statusStr = success(entry.Status)
	case "failed":
		statusStr = yellow(entry.Status)
	case "dead":
		statusStr = fail(entry.Status)
	case "circuit_open":
		statusStr = fail("circuit-open")
	case "circuit_closed":
		statusStr = success("circuit-closed")
	default:
		statusStr = entry.Status
	}

	// Build the log line
	line := fmt.Sprintf("  %s %s %s", dim(ts), levelStr, statusStr)

	// Add event ID (truncated)
	if entry.EventID != "" {
		eventID := entry.EventID
		if len(eventID) > 8 {
			eventID = eventID[:8]
		}
		line += fmt.Sprintf(" %s", dim(eventID))
	}

	// Add event type
	if entry.EventType != "" {
		line += fmt.Sprintf(" %s", bold(entry.EventType))
	}

	// Add destination (truncated)
	if entry.Destination != "" {
		dest := entry.Destination
		if len(dest) > 40 {
			dest = dest[:37] + "..."
		}
		line += fmt.Sprintf(" %s", dim(dest))
	}

	// Add status code for delivered/failed
	if entry.StatusCode > 0 {
		if entry.StatusCode >= 200 && entry.StatusCode < 300 {
			line += fmt.Sprintf(" %s", success(fmt.Sprintf("%d", entry.StatusCode)))
		} else if entry.StatusCode >= 400 {
			line += fmt.Sprintf(" %s", fail(fmt.Sprintf("%d", entry.StatusCode)))
		} else {
			line += fmt.Sprintf(" %d", entry.StatusCode)
		}
	}

	// Add duration
	if entry.DurationMs > 0 {
		line += fmt.Sprintf(" %s", dim(fmt.Sprintf("%dms", entry.DurationMs)))
	}

	// Add attempt info
	if entry.Attempt > 0 && entry.MaxAttempts > 0 {
		line += fmt.Sprintf(" %s", dim(fmt.Sprintf("(%d/%d)", entry.Attempt, entry.MaxAttempts)))
	}

	fmt.Println(line)

	// Print error on separate line if present
	if entry.Error != "" {
		fmt.Printf("         %s %s\n", fail("↳"), dim(entry.Error))
	}
}
