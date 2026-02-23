package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/apteva/agent/a2a"
	"github.com/apteva/agent/config"
)

const (
	// DefaultCardCacheTTL is how long a successful Agent Card stays cached.
	DefaultCardCacheTTL = 5 * time.Minute

	// DefaultCardErrorTTL is how long a failed fetch stays cached (avoids retry storms).
	DefaultCardErrorTTL = 30 * time.Second

	// DefaultCardFetchTimeout is the HTTP timeout for fetching an Agent Card.
	DefaultCardFetchTimeout = 3 * time.Second
)

// cardCacheEntry stores a cached Agent Card with expiry.
type cardCacheEntry struct {
	card      *a2a.AgentCard
	fetchedAt time.Time
	err       error // non-nil if last fetch failed
}

// CardProber fetches and caches A2A Agent Cards from discovered peers.
type CardProber struct {
	cache    map[string]*cardCacheEntry // keyed by agent URL
	mu       sync.RWMutex
	client   *http.Client
	ttl      time.Duration
	errorTTL time.Duration
}

// NewCardProber creates a new Agent Card prober.
func NewCardProber() *CardProber {
	return &CardProber{
		cache: make(map[string]*cardCacheEntry),
		client: &http.Client{
			Timeout: DefaultCardFetchTimeout,
		},
		ttl:      DefaultCardCacheTTL,
		errorTTL: DefaultCardErrorTTL,
	}
}

// EnrichAgents enriches agents that have features["a2a"]==true by probing
// their Agent Card endpoint. Non-A2A agents pass through unchanged.
// Probing happens concurrently. If a card fetch fails, the agent keeps its basic info.
func (p *CardProber) EnrichAgents(agents []*config.AgentInfo) {
	// Identify which agents need probing
	var toProbe []*config.AgentInfo
	for _, agent := range agents {
		if agent.Features != nil && agent.Features["a2a"] {
			toProbe = append(toProbe, agent)
		}
	}

	if len(toProbe) == 0 {
		return
	}

	// Probe concurrently
	var wg sync.WaitGroup
	for _, agent := range toProbe {
		wg.Add(1)
		go func(a *config.AgentInfo) {
			defer wg.Done()
			p.enrichAgent(a)
		}(agent)
	}
	wg.Wait()
}

// enrichAgent fetches the Agent Card for a single agent and populates
// its Skills and A2ACapabilities fields.
func (p *CardProber) enrichAgent(agent *config.AgentInfo) {
	card, err := p.getCard(agent.URL)
	if err != nil {
		// Don't log on every cycle — only on first failure (cache miss)
		return
	}

	// Map a2a.AgentSkill -> config.AgentSkillInfo
	skills := make([]config.AgentSkillInfo, 0, len(card.Skills))
	for _, s := range card.Skills {
		skills = append(skills, config.AgentSkillInfo{
			ID:          s.ID,
			Name:        s.Name,
			Description: s.Description,
			Tags:        s.Tags,
			Examples:    s.Examples,
		})
	}
	agent.Skills = skills

	// Map capabilities
	agent.A2ACapabilities = &config.A2ACapabilityInfo{
		Streaming:         card.Capabilities.Streaming,
		PushNotifications: card.Capabilities.PushNotifications,
		InputModes:        card.DefaultInputModes,
		OutputModes:       card.DefaultOutputModes,
	}

	// Enrich description from card if the discovery-level description is empty
	if agent.Description == "" && card.Description != "" {
		agent.Description = card.Description
	}
}

// getCard returns a cached card or fetches it from the network.
func (p *CardProber) getCard(agentURL string) (*a2a.AgentCard, error) {
	p.mu.RLock()
	entry, ok := p.cache[agentURL]
	p.mu.RUnlock()

	if ok {
		ttl := p.ttl
		if entry.err != nil {
			ttl = p.errorTTL // shorter TTL for errors
		}
		if time.Since(entry.fetchedAt) < ttl {
			return entry.card, entry.err
		}
	}

	// Fetch from network
	card, err := p.fetchCard(agentURL)

	if err != nil {
		log.Printf("🔍 Card probe failed for %s: %v", agentURL, err)
	} else {
		log.Printf("🔍 Probed Agent Card for %s: %d skills", agentURL, len(card.Skills))
	}

	// Cache result (even errors)
	p.mu.Lock()
	p.cache[agentURL] = &cardCacheEntry{
		card:      card,
		fetchedAt: time.Now(),
		err:       err,
	}
	p.mu.Unlock()

	return card, err
}

// fetchCard performs the HTTP GET to /.well-known/agent.json.
func (p *CardProber) fetchCard(agentURL string) (*a2a.AgentCard, error) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultCardFetchTimeout)
	defer cancel()

	url := agentURL + "/.well-known/agent.json"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var card a2a.AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	return &card, nil
}

// InvalidateCache removes the cached card for a given agent URL.
func (p *CardProber) InvalidateCache(agentURL string) {
	p.mu.Lock()
	delete(p.cache, agentURL)
	p.mu.Unlock()
}

// CacheSize returns the number of cached entries.
func (p *CardProber) CacheSize() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.cache)
}
