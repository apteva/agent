package memories

import (
	"net/http"

	"github.com/apteva/agent/memory"
)

// MemoryManagerGetter is a function that returns the current memory manager
type MemoryManagerGetter func() *memory.MemoryManager

// RegisterRoutes registers all memory-related routes
// Uses a getter function to support dynamic enable/disable of memory
func RegisterRoutes(mux *http.ServeMux, getMemoryManager MemoryManagerGetter) {
	mux.HandleFunc("/memories", HandleMemories(getMemoryManager))
	mux.HandleFunc("/memories/", HandleMemoryOperations(getMemoryManager))
}
