package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
)

var useColors = true

func init() {
	// Disable colors if NO_COLOR is set
	if os.Getenv("NO_COLOR") != "" {
		useColors = false
		return
	}
	// Disable on Windows without modern terminal
	if runtime.GOOS == "windows" && os.Getenv("WT_SESSION") == "" && os.Getenv("TERM") == "" {
		useColors = false
	}
}

func style(c, text string) string {
	if !useColors {
		return text
	}
	return c + text + colorReset
}

// Text styling
func bold(text string) string    { return style(colorBold, text) }
func dim(text string) string     { return style(colorDim, text) }
func yellow(text string) string  { return style(colorYellow, text) }
func cyan(text string) string    { return style(colorCyan, text) }
func success(text string) string { return style(colorGreen+colorBold, text) }
func fail(text string) string    { return style(colorRed+colorBold, text) }

// Suppress unused warnings for colors we might use later.
var _ = strings.Repeat

// Step indicator [1/4]
func printStep(num, total int, text string) {
	fmt.Printf("  %s%d/%d%s %s",
		style(colorDim, "["),
		num, total,
		style(colorDim, "]"),
		text)
}

// Print OK in green
func printOK() {
	fmt.Println(success("OK"))
}

// Print FAILED in red
func printFailed() {
	fmt.Println(fail("FAILED"))
}

// Print URL
func printURL(label, url string) {
	fmt.Printf("  %s %s\n", style(colorDim, label), cyan(url))
}

// Print box with content
func printBox(lines ...string) {
	fmt.Println()
	fmt.Println(style(colorYellow, "  ┌────────────────────────────────────────────────────────────┐"))
	for _, line := range lines {
		fmt.Printf(style(colorYellow, "  │ ")+"%-58s"+style(colorYellow, " │")+"\n", line)
	}
	fmt.Println(style(colorYellow, "  └────────────────────────────────────────────────────────────┘"))
}
