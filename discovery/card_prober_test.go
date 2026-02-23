package discovery

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apteva/agent/a2a"
	"github.com/apteva/agent/config"
)

// newTestCard returns a realistic Agent Card for testing.
func newTestCard() *a2a.AgentCard {
	return &a2a.AgentCard{
		Name:        "Test Agent",
		Description: "A test agent for unit testing",
		URL:         "http://localhost:9999/a2a",
		Version:     "1.0.0",
		Capabilities: a2a.AgentCapabilities{
			Streaming:              true,
			PushNotifications:      false,
			StateTransitionHistory: true,
		},
		Skills: []a2a.AgentSkill{
			{
				ID:          "general",
				Name:        "Test Agent",
				Description: "A test agent for unit testing",
				Tags:        []string{"general"},
			},
			{
				ID:          "task-management",
				Name:        "Task Management",
				Description: "Create and manage tasks",
				Tags:        []string{"tasks"},
				Examples:    []string{"Create a task to check email", "List my tasks"},
			},
		},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
	}
}

// serveCard creates a test server that serves an Agent Card at /.well-known/agent.json.
func serveCard(card *a2a.AgentCard) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(card)
	})
	return httptest.NewServer(mux)
}

func TestCardProber_FetchAndEnrich(t *testing.T) {
	card := newTestCard()
	server := serveCard(card)
	defer server.Close()

	prober := NewCardProber()

	agents := []*config.AgentInfo{
		{
			ID:       "test-agent",
			Name:     "Test Agent",
			URL:      server.URL,
			Enabled:  true,
			Features: map[string]bool{"a2a": true},
		},
	}

	prober.EnrichAgents(agents)

	agent := agents[0]

	// Skills should be populated
	if len(agent.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(agent.Skills))
	}
	if agent.Skills[0].ID != "general" {
		t.Errorf("expected first skill ID 'general', got %q", agent.Skills[0].ID)
	}
	if agent.Skills[1].ID != "task-management" {
		t.Errorf("expected second skill ID 'task-management', got %q", agent.Skills[1].ID)
	}
	if agent.Skills[1].Name != "Task Management" {
		t.Errorf("expected skill name 'Task Management', got %q", agent.Skills[1].Name)
	}

	// Examples should be mapped
	if len(agent.Skills[1].Examples) != 2 {
		t.Errorf("expected 2 examples, got %d", len(agent.Skills[1].Examples))
	}

	// A2A capabilities should be populated
	if agent.A2ACapabilities == nil {
		t.Fatal("expected A2ACapabilities to be set")
	}
	if !agent.A2ACapabilities.Streaming {
		t.Error("expected streaming=true")
	}
	if agent.A2ACapabilities.PushNotifications {
		t.Error("expected push_notifications=false")
	}
}

func TestCardProber_SkipsNonA2A(t *testing.T) {
	card := newTestCard()
	server := serveCard(card)
	defer server.Close()

	prober := NewCardProber()

	agents := []*config.AgentInfo{
		{
			ID:       "non-a2a",
			Name:     "Regular Agent",
			URL:      server.URL,
			Enabled:  true,
			Features: map[string]bool{"tasks": true}, // no a2a
		},
	}

	prober.EnrichAgents(agents)

	if len(agents[0].Skills) != 0 {
		t.Errorf("non-A2A agent should not be enriched, got %d skills", len(agents[0].Skills))
	}
	if agents[0].A2ACapabilities != nil {
		t.Error("non-A2A agent should not have A2ACapabilities")
	}
}

func TestCardProber_CachesResults(t *testing.T) {
	var fetchCount int32
	card := newTestCard()

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fetchCount, 1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(card)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	prober := NewCardProber()

	agents := []*config.AgentInfo{
		{
			ID:       "cached-agent",
			Name:     "Cached",
			URL:      server.URL,
			Enabled:  true,
			Features: map[string]bool{"a2a": true},
		},
	}

	// First call — fetches from network
	prober.EnrichAgents(agents)
	if atomic.LoadInt32(&fetchCount) != 1 {
		t.Fatalf("expected 1 fetch, got %d", atomic.LoadInt32(&fetchCount))
	}

	// Second call — should use cache
	prober.EnrichAgents(agents)
	if atomic.LoadInt32(&fetchCount) != 1 {
		t.Fatalf("expected still 1 fetch (cached), got %d", atomic.LoadInt32(&fetchCount))
	}

	if prober.CacheSize() != 1 {
		t.Errorf("expected cache size 1, got %d", prober.CacheSize())
	}
}

func TestCardProber_ErrorTTL(t *testing.T) {
	prober := NewCardProber()
	prober.errorTTL = 50 * time.Millisecond // short TTL for testing

	agents := []*config.AgentInfo{
		{
			ID:       "unreachable",
			Name:     "Unreachable",
			URL:      "http://127.0.0.1:1", // port 1 — will fail
			Enabled:  true,
			Features: map[string]bool{"a2a": true},
		},
	}

	// First call — fails, caches error
	prober.EnrichAgents(agents)
	if prober.CacheSize() != 1 {
		t.Fatalf("expected cache size 1 after error, got %d", prober.CacheSize())
	}
	if len(agents[0].Skills) != 0 {
		t.Error("failed probe should not add skills")
	}

	// Wait for error TTL to expire
	time.Sleep(100 * time.Millisecond)

	// Now serve a real card
	card := newTestCard()
	server := serveCard(card)
	defer server.Close()

	agents[0].URL = server.URL
	prober.InvalidateCache("http://127.0.0.1:1") // clear old cache entry

	prober.EnrichAgents(agents)
	if len(agents[0].Skills) != 2 {
		t.Errorf("expected 2 skills after recovery, got %d", len(agents[0].Skills))
	}
}

func TestCardProber_InvalidateCache(t *testing.T) {
	card := newTestCard()
	server := serveCard(card)
	defer server.Close()

	prober := NewCardProber()

	agents := []*config.AgentInfo{
		{
			ID:       "inv-agent",
			URL:      server.URL,
			Enabled:  true,
			Features: map[string]bool{"a2a": true},
		},
	}

	prober.EnrichAgents(agents)
	if prober.CacheSize() != 1 {
		t.Fatalf("expected cache size 1, got %d", prober.CacheSize())
	}

	prober.InvalidateCache(server.URL)
	if prober.CacheSize() != 0 {
		t.Errorf("expected cache size 0 after invalidation, got %d", prober.CacheSize())
	}
}

func TestCardProber_EnrichesDescription(t *testing.T) {
	card := newTestCard()
	card.Description = "Rich description from Agent Card"
	server := serveCard(card)
	defer server.Close()

	prober := NewCardProber()

	agents := []*config.AgentInfo{
		{
			ID:          "desc-agent",
			Name:        "Desc Agent",
			URL:         server.URL,
			Description: "", // empty — should be enriched from card
			Enabled:     true,
			Features:    map[string]bool{"a2a": true},
		},
	}

	prober.EnrichAgents(agents)

	if agents[0].Description != "Rich description from Agent Card" {
		t.Errorf("expected description from card, got %q", agents[0].Description)
	}
}

func TestCardProber_KeepsExistingDescription(t *testing.T) {
	card := newTestCard()
	card.Description = "Card description"
	server := serveCard(card)
	defer server.Close()

	prober := NewCardProber()

	agents := []*config.AgentInfo{
		{
			ID:          "keep-desc",
			Name:        "Keep Desc",
			URL:         server.URL,
			Description: "Original description", // non-empty — should NOT be overwritten
			Enabled:     true,
			Features:    map[string]bool{"a2a": true},
		},
	}

	prober.EnrichAgents(agents)

	if agents[0].Description != "Original description" {
		t.Errorf("expected original description preserved, got %q", agents[0].Description)
	}
}

func TestCardProber_ConcurrentProbing(t *testing.T) {
	card1 := newTestCard()
	card1.Name = "Agent One"
	server1 := serveCard(card1)
	defer server1.Close()

	card2 := newTestCard()
	card2.Name = "Agent Two"
	card2.Skills = append(card2.Skills, a2a.AgentSkill{
		ID:          "extra-skill",
		Name:        "Extra Skill",
		Description: "An extra skill",
	})
	server2 := serveCard(card2)
	defer server2.Close()

	prober := NewCardProber()

	agents := []*config.AgentInfo{
		{
			ID:       "agent-one",
			Name:     "One",
			URL:      server1.URL,
			Enabled:  true,
			Features: map[string]bool{"a2a": true},
		},
		{
			ID:       "agent-two",
			Name:     "Two",
			URL:      server2.URL,
			Enabled:  true,
			Features: map[string]bool{"a2a": true},
		},
	}

	prober.EnrichAgents(agents)

	if len(agents[0].Skills) != 2 {
		t.Errorf("agent one: expected 2 skills, got %d", len(agents[0].Skills))
	}
	if len(agents[1].Skills) != 3 {
		t.Errorf("agent two: expected 3 skills, got %d", len(agents[1].Skills))
	}

	if prober.CacheSize() != 2 {
		t.Errorf("expected 2 cached entries, got %d", prober.CacheSize())
	}
}

func TestCardProber_HTTP404(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	prober := NewCardProber()

	agents := []*config.AgentInfo{
		{
			ID:       "no-card",
			URL:      server.URL,
			Enabled:  true,
			Features: map[string]bool{"a2a": true},
		},
	}

	prober.EnrichAgents(agents)

	if len(agents[0].Skills) != 0 {
		t.Errorf("404 should not produce skills, got %d", len(agents[0].Skills))
	}
	if agents[0].A2ACapabilities != nil {
		t.Error("404 should not produce A2ACapabilities")
	}

	// Error should be cached
	if prober.CacheSize() != 1 {
		t.Errorf("expected error cached, got cache size %d", prober.CacheSize())
	}
}

func TestCardProber_InvalidJSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{not valid json"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	prober := NewCardProber()

	agents := []*config.AgentInfo{
		{
			ID:       "bad-json",
			URL:      server.URL,
			Enabled:  true,
			Features: map[string]bool{"a2a": true},
		},
	}

	prober.EnrichAgents(agents)

	if len(agents[0].Skills) != 0 {
		t.Errorf("invalid JSON should not produce skills, got %d", len(agents[0].Skills))
	}
}

func TestCardProber_NilFeatures(t *testing.T) {
	prober := NewCardProber()

	agents := []*config.AgentInfo{
		{
			ID:       "nil-features",
			Name:     "No Features",
			URL:      "http://localhost:9999",
			Enabled:  true,
			Features: nil, // nil map
		},
	}

	// Should not panic
	prober.EnrichAgents(agents)

	if len(agents[0].Skills) != 0 {
		t.Error("nil features should not be enriched")
	}
}

func TestCardProber_EmptyAgentList(t *testing.T) {
	prober := NewCardProber()
	// Should not panic on empty/nil slices
	prober.EnrichAgents(nil)
	prober.EnrichAgents([]*config.AgentInfo{})
}
