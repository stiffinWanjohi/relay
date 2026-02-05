package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/stiffinWanjohi/relay/internal/alerting"
	"github.com/stiffinWanjohi/relay/internal/api/graphql"
	"github.com/stiffinWanjohi/relay/internal/api/rest"
	"github.com/stiffinWanjohi/relay/internal/auth"
	"github.com/stiffinWanjohi/relay/internal/connector"
	"github.com/stiffinWanjohi/relay/internal/debug"
	"github.com/stiffinWanjohi/relay/internal/dedup"
	"github.com/stiffinWanjohi/relay/internal/delivery"
	"github.com/stiffinWanjohi/relay/internal/event"
	"github.com/stiffinWanjohi/relay/internal/eventtype"
	"github.com/stiffinWanjohi/relay/internal/logging"
	"github.com/stiffinWanjohi/relay/internal/logstream"
	"github.com/stiffinWanjohi/relay/internal/metrics"
	"github.com/stiffinWanjohi/relay/internal/queue"
)

var apiLog = logging.Component("api")

// ServerConfig holds server configuration.
type ServerConfig struct {
	EnableAuth       bool
	EnablePlayground bool
	EnableDocs       bool                  // Enable REST API docs (/docs)
	MetricsHandler   http.Handler          // Optional Prometheus metrics handler
	RateLimiter      *delivery.RateLimiter // Optional rate limiter
	GlobalRateLimit  int                   // Global requests per second (0 = unlimited)
	ClientRateLimit  int                   // Per-client requests per second (0 = unlimited)
	DebugService      *debug.Service        // Optional debug service for webhook debugger
	MetricsStore      *metrics.Store        // Optional metrics store for analytics
	LogStreamHub      *logstream.Hub        // Optional log streaming hub
	AlertEngine       *alerting.Engine      // Optional alerting engine
	AlertingStore     *alerting.Store       // Optional alerting persistence store
	ConnectorRegistry *connector.Registry   // Optional connector registry
	ConnectorStore    *connector.Store      // Optional connector persistence store
}

// Server represents the HTTP server.
type Server struct {
	router *chi.Mux
}

// NewServer creates a new HTTP server.
func NewServer(store *event.Store, eventTypeStore *eventtype.Store, q *queue.Queue, d *dedup.Checker, authValidator auth.APIKeyValidator, cfg ServerConfig) *Server {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(loggingMiddleware())

	// Rate limiting (applied to all routes)
	if cfg.RateLimiter != nil && (cfg.GlobalRateLimit > 0 || cfg.ClientRateLimit > 0) {
		r.Use(RateLimitMiddleware(cfg.RateLimiter, RateLimitConfig{
			GlobalLimit: cfg.GlobalRateLimit,
			ClientLimit: cfg.ClientRateLimit,
		}))
	}

	// Create resolver
	resolver := graphql.NewResolver(store, eventTypeStore, q, d)
	if cfg.MetricsStore != nil {
		resolver = resolver.WithMetricsStore(cfg.MetricsStore)
	}
	if cfg.AlertEngine != nil {
		resolver = resolver.WithAlertEngine(cfg.AlertEngine)
	}
	if cfg.ConnectorRegistry != nil {
		resolver = resolver.WithConnectorRegistry(cfg.ConnectorRegistry)
	}

	// Create GraphQL server
	srv := handler.NewDefaultServer(graphql.NewExecutableSchema(graphql.Config{
		Resolvers: resolver,
	}))

	s := &Server{
		router: r,
	}

	// Public routes (no auth required)
	r.Get("/health", s.healthHandler)

	// Metrics endpoint (Prometheus)
	if cfg.MetricsHandler != nil {
		r.Handle("/metrics", cfg.MetricsHandler)
	}

	// Protected routes
	r.Group(func(r chi.Router) {
		if cfg.EnableAuth && authValidator != nil {
			r.Use(auth.Middleware(authValidator))
		}
		r.Handle("/graphql", srv)
	})

	// Playground (only in development)
	if cfg.EnablePlayground {
		r.Get("/playground", playground.Handler("Relay GraphQL", "/graphql"))
	}

	// REST API and documentation
	restHandler := rest.NewHandler(store, eventTypeStore, q, d)
	if cfg.AlertEngine != nil {
		restHandler = restHandler.WithAlertEngine(cfg.AlertEngine)
	}
	if cfg.AlertingStore != nil {
		restHandler = restHandler.WithAlertingStore(cfg.AlertingStore)
	}
	if cfg.ConnectorRegistry != nil {
		restHandler = restHandler.WithConnectorRegistry(cfg.ConnectorRegistry)
	}
	if cfg.ConnectorStore != nil {
		restHandler = restHandler.WithConnectorStore(cfg.ConnectorStore)
	}
	if cfg.MetricsStore != nil {
		restHandler = restHandler.WithMetricsStore(cfg.MetricsStore)
	}
	r.Mount("/", restHandler.Router())

	// Debug endpoints (webhook debugger)
	if cfg.DebugService != nil {
		debugHandler := debug.NewHandler(cfg.DebugService)
		r.Mount("/debug", debugHandler.Router())
	}

	// Log streaming endpoints
	if cfg.LogStreamHub != nil {
		logHandler := logstream.NewHandler(cfg.LogStreamHub)
		r.Mount("/logs", logHandler.Router())
	}

	return s
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.router
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func loggingMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			apiLog.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"duration", time.Since(start),
			)
		})
	}
}
