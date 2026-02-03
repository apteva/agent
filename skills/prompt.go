package skills

import (
	"fmt"
	"strings"

	"github.com/apteva/agent/config"
)

// BuildAllSkillsPrompt builds the system prompt section with ALL skills and their full instructions
func (m *Manager) BuildAllSkillsPrompt() string {
	if !m.enabled || len(m.skills) == 0 {
		return ""
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString("\n\n## Skills\n\n")
	sb.WriteString("Follow these skill instructions when relevant to the user's request:\n")

	for _, skill := range m.skills {
		// Skill header
		if skill.Icon != "" {
			sb.WriteString(fmt.Sprintf("\n### %s %s\n", skill.Icon, skill.Name))
		} else {
			sb.WriteString(fmt.Sprintf("\n### %s\n", skill.Name))
		}

		// Description
		sb.WriteString(fmt.Sprintf("*%s*\n\n", skill.Description))

		// Full instructions
		sb.WriteString(skill.Instructions)
		sb.WriteString("\n")
	}

	return sb.String()
}

// BuildFullPrompt builds a prompt section with full instructions for specific skills
// Use this when a skill has been matched/activated
func (m *Manager) BuildFullPrompt(skillNames []string) string {
	if !m.enabled || len(skillNames) == 0 {
		return ""
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var sb strings.Builder

	for _, name := range skillNames {
		for _, skill := range m.skills {
			if skill.Name == name {
				sb.WriteString(fmt.Sprintf("\n\n## Active Skill: %s", skill.Name))
				if skill.Icon != "" {
					sb.WriteString(fmt.Sprintf(" %s", skill.Icon))
				}
				sb.WriteString("\n\n")
				sb.WriteString(skill.Instructions)
				sb.WriteString("\n")
				break
			}
		}
	}

	return sb.String()
}

// BuildSkillPrompt builds the full prompt for a single activated skill
func BuildSkillPrompt(skill *config.Skill) string {
	if skill == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n\n## Active Skill: %s", skill.Name))
	if skill.Icon != "" {
		sb.WriteString(fmt.Sprintf(" %s", skill.Icon))
	}
	sb.WriteString("\n\n")
	sb.WriteString(skill.Instructions)
	sb.WriteString("\n")

	return sb.String()
}

// GetSkillsSummary returns a brief summary of available skills
func (m *Manager) GetSkillsSummary() string {
	if !m.enabled || len(m.skills) == 0 {
		return "No skills enabled"
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var names []string
	for _, skill := range m.skills {
		if skill.Icon != "" {
			names = append(names, fmt.Sprintf("%s %s", skill.Icon, skill.Name))
		} else {
			names = append(names, skill.Name)
		}
	}

	return strings.Join(names, ", ")
}
