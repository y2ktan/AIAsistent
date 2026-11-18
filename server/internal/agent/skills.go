package agent

import (
	"fmt"
	"html"
	"path"
	"sort"
	"strings"
)

type AgentSkill struct {
	Name           string
	Description    string
	Instructions   string
	ReferenceFiles map[string]string
	AllowedTools   []string
	Enabled        bool
}

type SkillToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
}

func enabledSkills(skills []AgentSkill) []AgentSkill {
	enabled := make([]AgentSkill, 0, len(skills))
	for _, skill := range skills {
		if skill.Enabled {
			enabled = append(enabled, skill)
		}
	}
	return enabled
}

func BuildSkillsCatalog(skills []AgentSkill) string {
	skills = enabledSkills(skills)
	if len(skills) == 0 {
		return ""
	}
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})
	var b strings.Builder
	b.WriteString("<available_skills>\n")
	for _, skill := range skills {
		fmt.Fprintf(&b, "- name: %s\n  description: %s\n", skill.Name, strings.TrimSpace(skill.Description))
	}
	b.WriteString("</available_skills>\n")
	b.WriteString("When a skill is relevant, call load_skill to read its instructions before proceeding. Use read_skill_file to load only the referenced file needed.")
	return b.String()
}

func SkillToolDefinitions(skills []AgentSkill) []SkillToolDefinition {
	skills = enabledSkills(skills)
	if len(skills) == 0 {
		return nil
	}
	definitions := []SkillToolDefinition{
		{
			Name:        "load_skill",
			Description: "Load the full instructions for an enabled Agent Skill relevant to the user's request.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "The enabled Agent Skill name from the available skills catalog."},
				},
				"required": []string{"name"},
			},
		},
	}
	for _, skill := range skills {
		if len(skill.ReferenceFiles) > 0 {
			definitions = append(definitions, SkillToolDefinition{
				Name:        "read_skill_file",
				Description: "Read a bundled reference file from an enabled Agent Skill.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"skill": map[string]any{"type": "string", "description": "The enabled Agent Skill name."},
						"path":  map[string]any{"type": "string", "description": "A relative path from the skill's reference file list."},
					},
					"required": []string{"skill", "path"},
				},
			})
			break
		}
	}
	return definitions
}

func HandleSkillTool(skills []AgentSkill, toolName string, args map[string]any) (string, bool) {
	if toolName != "load_skill" && toolName != "read_skill_file" {
		return "", false
	}
	for _, skill := range enabledSkills(skills) {
		if toolName == "load_skill" {
			name, _ := args["name"].(string)
			if !strings.EqualFold(strings.TrimSpace(name), skill.Name) {
				continue
			}
			return formatLoadedSkill(skill), true
		}

		name, _ := args["skill"].(string)
		if !strings.EqualFold(strings.TrimSpace(name), skill.Name) {
			continue
		}
		filePath, _ := args["path"].(string)
		content, ok := skillReference(skill, filePath)
		if !ok {
			return "skill reference file not available", true
		}
		return fmt.Sprintf("<agent_skill_reference skill=%q path=%q>\n%s\n</agent_skill_reference>", html.EscapeString(skill.Name), html.EscapeString(path.Clean(filePath)), content), true
	}
	return "skill not available", true
}

func formatLoadedSkill(skill AgentSkill) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<agent_skill name=%q>\n", html.EscapeString(skill.Name))
	fmt.Fprintf(&b, "Description: %s\n\nInstructions:\n%s\n", strings.TrimSpace(skill.Description), strings.TrimSpace(skill.Instructions))
	if len(skill.AllowedTools) > 0 {
		fmt.Fprintf(&b, "\nSuggested MCP tools: %s\n", strings.Join(skill.AllowedTools, ", "))
	}
	if len(skill.ReferenceFiles) > 0 {
		paths := make([]string, 0, len(skill.ReferenceFiles))
		for filePath := range skill.ReferenceFiles {
			paths = append(paths, filePath)
		}
		sort.Strings(paths)
		fmt.Fprintf(&b, "\nReference files: %s\n", strings.Join(paths, ", "))
	}
	b.WriteString("</agent_skill>")
	return b.String()
}

func skillReference(skill AgentSkill, filePath string) (string, bool) {
	if strings.Contains(filePath, "\\") || path.IsAbs(filePath) {
		return "", false
	}
	clean := path.Clean(filePath)
	if clean == "." || clean != filePath || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	content, ok := skill.ReferenceFiles[clean]
	return content, ok
}
