package skills

import (
	"regexp"
	"strings"
	"sync"

	"github.com/apteva/agent/config"
)

// Manager manages skills loaded from config
type Manager struct {
	skills  []config.Skill
	enabled bool
	mu      sync.RWMutex
}

var (
	globalManager *Manager
	managerOnce   sync.Once
)

// GetManager returns the global skills manager
func GetManager() *Manager {
	managerOnce.Do(func() {
		globalManager = &Manager{
			skills:  []config.Skill{},
			enabled: false,
		}
	})
	return globalManager
}

// Initialize loads skills from config
func (m *Manager) Initialize(cfg *config.SkillsConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cfg == nil {
		m.enabled = false
		m.skills = []config.Skill{}
		return
	}

	m.enabled = cfg.Enabled
	m.skills = make([]config.Skill, 0, len(cfg.Definitions))

	// Only load enabled skills
	for _, skill := range cfg.Definitions {
		if skill.Enabled {
			m.skills = append(m.skills, skill)
		}
	}
}

// IsEnabled returns whether skills are enabled
func (m *Manager) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

// GetAll returns all enabled skills
func (m *Manager) GetAll() []config.Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]config.Skill, len(m.skills))
	copy(result, m.skills)
	return result
}

// GetByName returns a skill by name
func (m *Manager) GetByName(name string) *config.Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, skill := range m.skills {
		if skill.Name == name {
			return &skill
		}
	}
	return nil
}

// GetByCategory returns skills by category
func (m *Manager) GetByCategory(category string) []config.Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []config.Skill
	for _, skill := range m.skills {
		if skill.Category == category {
			result = append(result, skill)
		}
	}
	return result
}

// MatchSkills matches user input against skill name and description
// Returns skills that are relevant to the input
func (m *Manager) MatchSkills(input string) []config.Skill {
	if !m.enabled {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	input = strings.ToLower(input)
	var matched []config.Skill

	for _, skill := range m.skills {
		// Check if skill name appears in input (word boundary)
		namePattern := `\b` + regexp.QuoteMeta(strings.ToLower(skill.Name)) + `\b`
		if ok, _ := regexp.MatchString(namePattern, input); ok {
			matched = append(matched, skill)
			continue
		}

		// Check if any significant word from description appears in input
		descWords := strings.Fields(strings.ToLower(skill.Description))
		for _, word := range descWords {
			// Skip short common words
			if len(word) < 4 {
				continue
			}
			wordPattern := `\b` + regexp.QuoteMeta(word) + `\b`
			if ok, _ := regexp.MatchString(wordPattern, input); ok {
				matched = append(matched, skill)
				break
			}
		}
	}

	return matched
}

// GetRequiredTools returns the MCP/builtin tools required by a skill
func (m *Manager) GetRequiredTools(name string) []string {
	skill := m.GetByName(name)
	if skill == nil {
		return nil
	}
	return skill.Tools
}

// GetInstructions returns the full instructions for a skill
func (m *Manager) GetInstructions(name string) string {
	skill := m.GetByName(name)
	if skill == nil {
		return ""
	}
	return skill.Instructions
}

// Count returns the number of enabled skills
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.skills)
}
