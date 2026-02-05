package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/stiffinWanjohi/relay/internal/api/graphql"
	"github.com/stiffinWanjohi/relay/internal/api/rest"
	"github.com/stiffinWanjohi/relay/internal/auth"
	"github.com/stiffinWanjohi/relay/internal/dedup"
	"github.com/stiffinWanjohi/relay/internal/event"
	"github.com/stiffinWanjohi/relay/internal/eventtype"
	"github.com/stiffinWanjohi/relay/internal/logging"
	"github.com/stiffinWanjohi/relay/internal/queue"
)

var apiLog = logging.Component("api")

// ServerConfig holds server configuration.
type ServerConfig struct {
	EnableAuth       bool
	EnablePlayground bool
	EnableDocs       bool         // Enable REST API docs (/docs)
	MetricsHandler   http.Handler // Optional Prometheus metrics handler
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

	// Create resolver
	resolver := graphql.NewResolver(store, eventTypeStore, q, d)

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
	r.Mount("/", restHandler.Router())

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
