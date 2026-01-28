package rest

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
)

//go:embed swagger-ui
var swaggerUI embed.FS

// Router creates a chi router with REST API routes.
func (h *Handler) Router() chi.Router {
	r := chi.NewRouter()

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		// Events
		r.Post("/events", h.CreateEvent)
		r.Get("/events", h.ListEvents)
		r.Get("/events/{eventId}", h.GetEvent)
		r.Post("/events/{eventId}/replay", h.ReplayEvent)
		r.Post("/events/batch/retry", h.BatchRetry)

		// Stats
		r.Get("/stats", h.GetStats)
	})

	// OpenAPI spec
	r.Get("/openapi.yaml", h.serveOpenAPISpec)
	r.Get("/openapi.json", h.serveOpenAPISpecJSON)

	// Swagger UI
	r.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/", http.StatusMovedPermanently)
	})
	r.Get("/docs/*", h.serveSwaggerUI)

	return r
}

func (h *Handler) serveOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-yaml")
	http.ServeFile(w, r, "cmd/relay/openapi.yaml")
}

func (h *Handler) serveOpenAPISpecJSON(w http.ResponseWriter, r *http.Request) {
	// For now, serve YAML. Could convert to JSON if needed.
	w.Header().Set("Content-Type", "application/x-yaml")
	http.ServeFile(w, r, "cmd/relay/openapi.yaml")
}

func (h *Handler) serveSwaggerUI(w http.ResponseWriter, r *http.Request) {
	// Strip /docs prefix and serve from embedded filesystem
	subFS, err := fs.Sub(swaggerUI, "swagger-ui")
	if err != nil {
		http.Error(w, "Swagger UI not available", http.StatusInternalServerError)
		return
	}

	// Get the path after /docs/
	urlPath := chi.URLParam(r, "*")
	if urlPath == "" || urlPath == "/" {
		urlPath = "index.html"
	}
	_ = urlPath // Used for documentation, actual serving handled by FileServer

	http.StripPrefix("/docs/", http.FileServer(http.FS(subFS))).ServeHTTP(w, r)
}
