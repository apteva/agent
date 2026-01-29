package threads

import (
	"database/sql"
	"net/http"
)

// RegisterRoutes registers all thread-related routes
func RegisterRoutes(mux *http.ServeMux, db *sql.DB) {
	mux.HandleFunc("/threads", HandleThreads(db))
	mux.HandleFunc("/threads/", HandleThreadMessages(db))
}
