package mcp

import (
	"fmt"
	"strings"
)

// Skill represents a skill with tools and instructions
type Skill struct {
	Name         string                 `json:"name"`
	DisplayName  string                 `json:"display_name"`
	Description  string                 `json:"description"`
	Version      string                 `json:"version"`
	Instructions string                 `json:"instructions"`
	Tools        []string               `json:"tools"`
	ClaudeNative map[string]interface{} `json:"claude_native,omitempty"`
	Category     string                 `json:"category"`
	Tags         []string               `json:"tags"`
	Icon         string                 `json:"icon"`
}

// GetSkillTools returns all MCP tools referenced by enabled skills
func GetSkillTools(skills []Skill) []string {
	toolSet := make(map[string]bool)

	for _, skill := range skills {
		for _, tool := range skill.Tools {
			toolSet[tool] = true
		}
	}

	tools := make([]string, 0, len(toolSet))
	for tool := range toolSet {
		tools = append(tools, tool)
	}
	return tools
}

// BuildSkillsPromptSection builds the system prompt section for skills (for non-Claude providers)
func BuildSkillsPromptSection(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n## Active Skills\n")

	for _, skill := range skills {
		sb.WriteString(fmt.Sprintf("\n### %s", skill.DisplayName))
		if skill.Icon != "" {
			sb.WriteString(fmt.Sprintf(" %s", skill.Icon))
		}
		sb.WriteString("\n")
		sb.WriteString(skill.Instructions)
		sb.WriteString("\n")
	}

	return sb.String()
}

// BuildClaudeNativeSkills converts skills to Claude native skills format
func BuildClaudeNativeSkills(skills []Skill) []map[string]interface{} {
	nativeSkills := make([]map[string]interface{}, 0, len(skills))

	for _, skill := range skills {
		if skill.ClaudeNative != nil && len(skill.ClaudeNative) > 0 {
			nativeSkills = append(nativeSkills, skill.ClaudeNative)
		} else {
			nativeSkill := map[string]interface{}{
				"name":         skill.Name,
				"description":  skill.Description,
				"instructions": skill.Instructions,
			}
			nativeSkills = append(nativeSkills, nativeSkill)
		}
	}

	return nativeSkills
}
