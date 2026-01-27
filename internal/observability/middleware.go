package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// HTTPMiddleware creates an HTTP middleware that records metrics and traces.
func HTTPMiddleware(metrics *Metrics, tracer *Tracer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Start tracing span
			ctx := r.Context()
			if tracer != nil {
				var span Span
				ctx, span = tracer.StartSpan(ctx, SpanHTTPRequest,
					WithSpanKind(SpanKindServer),
					WithAttributes(map[string]any{
						AttrHTTPMethod: r.Method,
						AttrHTTPURL:    r.URL.String(),
					}),
				)
				defer func() {
					span.End()
				}()
			}

			// Extract trace context from incoming headers
			if tracer != nil {
				ctx = tracer.Extract(ctx, HTTPHeaderCarrier(r.Header))
			}

			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r.WithContext(ctx))

			duration := time.Since(start)
			status := ww.Status()
			statusStr := strconv.Itoa(status)
			path := r.URL.Path

			// Record metrics
			if metrics != nil {
				metrics.HTTPRequestTotal(ctx, r.Method, path, statusStr)
				metrics.HTTPRequestDuration(ctx, r.Method, path, duration)
			}

			// Set span attributes
			if tracer != nil {
				span := tracer.SpanFromContext(ctx)
				span.SetAttribute(AttrHTTPStatusCode, status)
				if status >= 400 {
					span.SetStatus(SpanStatusError, http.StatusText(status))
				} else {
					span.SetStatus(SpanStatusOK, "")
				}
			}
		})
	}
}

// MetricsOnlyMiddleware creates middleware that only records metrics (no tracing).
func MetricsOnlyMiddleware(metrics *Metrics) func(http.Handler) http.Handler {
	return HTTPMiddleware(metrics, nil)
}

// TracingOnlyMiddleware creates middleware that only adds tracing (no metrics).
func TracingOnlyMiddleware(tracer *Tracer) func(http.Handler) http.Handler {
	return HTTPMiddleware(nil, tracer)
}
