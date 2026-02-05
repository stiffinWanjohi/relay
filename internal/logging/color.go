package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
)

const (
	colorReset  = "\033[0m"
	colorDim    = "\033[2m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorCyan   = "\033[36m"
	colorBlue   = "\033[34m"
)

// ColorHandler is a colored slog handler for terminal output.
type ColorHandler struct {
	opts   *slog.HandlerOptions
	out    *os.File
	attrs  []slog.Attr
	groups []string
	mu     *sync.Mutex
}

// NewColorHandler creates a new colored log handler.
func NewColorHandler(out *os.File, opts *slog.HandlerOptions) *ColorHandler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	return &ColorHandler{opts: opts, out: out, mu: &sync.Mutex{}}
}

func (h *ColorHandler) Enabled(_ context.Context, level slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.opts.Level != nil {
		minLevel = h.opts.Level.Level()
	}
	return level >= minLevel
}

func (h *ColorHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Color based on level
	levelColor := colorGreen
	switch r.Level {
	case slog.LevelDebug:
		levelColor = colorBlue
	case slog.LevelWarn:
		levelColor = colorYellow
	case slog.LevelError:
		levelColor = colorRed
	}

	// Format: [time] level msg key=value...
	timeStr := r.Time.Format("15:04:05")

	_, _ = fmt.Fprintf(h.out, "%s%s%s %s%-5s%s %s",
		colorDim, timeStr, colorReset,
		levelColor, r.Level.String(), colorReset,
		r.Message)

	// Print pre-existing attrs (from WithAttrs)
	for _, a := range h.attrs {
		_, _ = fmt.Fprintf(h.out, " %s%s=%s%v%s", colorDim, a.Key, colorCyan, a.Value, colorReset)
	}

	// Print record attributes
	r.Attrs(func(a slog.Attr) bool {
		_, _ = fmt.Fprintf(h.out, " %s%s=%s%v%s", colorDim, a.Key, colorCyan, a.Value, colorReset)
		return true
	})

	_, _ = fmt.Fprintln(h.out)
	return nil
}

func (h *ColorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ColorHandler{
		opts:   h.opts,
		out:    h.out,
		attrs:  append(h.attrs, attrs...),
		groups: h.groups,
		mu:     h.mu, // Share mutex
	}
}

func (h *ColorHandler) WithGroup(name string) slog.Handler {
	return &ColorHandler{
		opts:   h.opts,
		out:    h.out,
		attrs:  h.attrs,
		groups: append(h.groups, name),
		mu:     h.mu, // Share mutex
	}
}
