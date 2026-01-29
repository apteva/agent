package observability

import (
	"database/sql"
	"net/http"
)

// RegisterRoutes registers all observability-related routes
func RegisterRoutes(mux *http.ServeMux, db *sql.DB) {
	// Trace endpoints
	mux.HandleFunc("/traces", HandleTraces(db))
	mux.HandleFunc("/traces/", HandleTrace(db))
	mux.HandleFunc("/traces/stats", HandleTraceStats(db))

	// Span endpoints
	mux.HandleFunc("/spans", HandleSpans(db))
	mux.HandleFunc("/spans/", HandleSpan(db))

	// Event endpoints
	mux.HandleFunc("/events/query", HandleEventsQuery(db))
	mux.HandleFunc("/events/trace/", HandleEventsForTrace(db))

	// Usage/token tracking endpoints
	mux.HandleFunc("/usage", HandleUsage(db))
}
