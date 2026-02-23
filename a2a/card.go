package a2a

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/apteva/agent/config"
)

// BuildAgentCard generates an A2A Agent Card from the agent configuration.
// The card is served at /.well-known/agent.json for discovery.
func BuildAgentCard(cfg *config.AgentConfig, baseURL string) *AgentCard {
	baseURL = strings.TrimRight(baseURL, "/")

	card := &AgentCard{
		Name:        cfg.Name,
		Description: cfg.Description,
		URL:         baseURL + "/a2a",
		Version:     config.GetVersion(),
		Capabilities: AgentCapabilities{
			Streaming:              true,
			PushNotifications:      cfg.A2A != nil && cfg.A2A.PushNotifications,
			StateTransitionHistory: true,
		},
		DefaultInputModes:  []string{"text/plain", "application/json"},
		DefaultOutputModes: []string{"text/plain", "application/json"},
	}

	// Add image support if vision is enabled
	if cfg.LLM.Vision != nil && cfg.LLM.Vision.Enabled {
		card.DefaultInputModes = append(card.DefaultInputModes, "image/*")
	}

	// Build skills
	card.Skills = buildSkills(cfg)

	// Security: API key via header (matches our existing auth)
	card.SecuritySchemes = map[string]SecurityScheme{
		"apiKey": {
			Type:        "apiKey",
			In:          "header",
			Name:        "X-API-Key",
			Description: "API key for authentication",
		},
	}
	card.Security = []map[string][]string{
		{"apiKey": {}},
	}

	return card
}

// buildSkills generates AgentSkill entries from the agent configuration.
func buildSkills(cfg *config.AgentConfig) []AgentSkill {
	var skills []AgentSkill

	// General assistant skill (always present)
	skills = append(skills, AgentSkill{
		ID:          "general",
		Name:        cfg.Name,
		Description: cfg.Description,
		Tags:        []string{"general", "assistant"},
	})

	// Skills from config.Skills definitions
	if cfg.Skills != nil && cfg.Skills.Enabled {
		for _, s := range cfg.Skills.Definitions {
			if !s.Enabled {
				continue
			}
			skills = append(skills, AgentSkill{
				ID:          slugify(s.Name),
				Name:        s.Name,
				Description: s.Description,
				Tags:        s.Tags,
			})
		}
	}

	// Feature-based skills
	if cfg.Tasks != nil && cfg.Tasks.Enabled {
		skills = append(skills, AgentSkill{
			ID:          "task-management",
			Name:        "Task Management",
			Description: "Create, schedule, and manage tasks with recurrence support",
			Tags:        []string{"tasks", "scheduling"},
		})
	}

	if cfg.Operator != nil && cfg.Operator.Enabled {
		skills = append(skills, AgentSkill{
			ID:          "browser-automation",
			Name:        "Browser Automation",
			Description: "Control a web browser to interact with web pages",
			Tags:        []string{"browser", "automation", "operator"},
		})
	}

	if cfg.Memory != nil && cfg.Memory.Enabled {
		skills = append(skills, AgentSkill{
			ID:          "memory",
			Name:        "Long-term Memory",
			Description: "Store and recall information across conversations",
			Tags:        []string{"memory", "knowledge"},
		})
	}

	if cfg.FileSystem != nil && cfg.FileSystem.Enabled {
		skills = append(skills, AgentSkill{
			ID:          "filesystem",
			Name:        "File Management",
			Description: "Read, write, and manage files",
			Tags:        []string{"files", "filesystem"},
		})
	}

	// MCP server-based skills
	if cfg.MCP != nil && cfg.MCP.Enabled {
		for _, server := range cfg.MCP.Servers {
			if !server.Enabled {
				continue
			}
			skills = append(skills, AgentSkill{
				ID:          slugify(server.Name) + "-integration",
				Name:        titleCase(server.Name) + " Integration",
				Description: fmt.Sprintf("Access and interact with %s", server.Name),
				Tags:        []string{"mcp", server.Name, "integration"},
			})
		}
	}

	return skills
}

// titleCase converts "notion" to "Notion", "my-server" to "My-Server".
func titleCase(name string) string {
	if name == "" {
		return name
	}
	runes := []rune(name)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// slugify converts a name to a URL-safe ID.
func slugify(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	// Keep only alphanumeric and hyphens
	var result strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			result.WriteRune(c)
		}
	}
	return result.String()
}
