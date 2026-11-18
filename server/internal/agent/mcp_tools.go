package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"aegis-voice/server/internal/mcp"
)

const maxToolRounds = 6

type MCPToolBinding struct {
	Name        string
	Description string
	InputSchema map[string]any
	Server      mcp.ServerConfig
	RawName     string
}

func BuildMCPToolRegistry(servers []mcp.ServerConfig) []MCPToolBinding {
	type candidate struct {
		server    mcp.ServerConfig
		rawName   string
		baseName  string
		slug      string
		uniqueRaw bool
	}

	toolCounts := make(map[string]int)
	var candidates []candidate
	for _, server := range servers {
		if !server.Enabled {
			continue
		}
		seen := make(map[string]bool)
		for _, rawName := range server.AllowedTools {
			if rawName == "" || seen[rawName] {
				continue
			}
			seen[rawName] = true
			toolCounts[rawName]++
			candidates = append(candidates, candidate{
				server:  server,
				rawName: rawName,
				slug:    serverSlug(server),
			})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.server.ID != right.server.ID {
			return left.server.ID < right.server.ID
		}
		if left.server.Name != right.server.Name {
			return left.server.Name < right.server.Name
		}
		return left.rawName < right.rawName
	})

	reserved := map[string]bool{
		"search_knowledge_base": true,
		"load_skill":            true,
		"read_skill_file":       true,
	}
	for i := range candidates {
		item := &candidates[i]
		normalizedRaw := normalizeFunctionName(item.rawName, 64)
		item.uniqueRaw = toolCounts[item.rawName] == 1 && !reserved[normalizedRaw]
		item.baseName = normalizedRaw
		if !item.uniqueRaw {
			item.baseName = qualifyToolName(item.slug, normalizedRaw)
		}
	}

	nameCounts := make(map[string]int)
	for _, item := range candidates {
		nameCounts[item.baseName]++
	}
	for i := range candidates {
		item := &candidates[i]
		if nameCounts[item.baseName] > 1 {
			item.baseName = qualifyToolName(item.slug, normalizeFunctionName(item.rawName, 40))
		}
	}
	nameCounts = make(map[string]int)
	for _, item := range candidates {
		nameCounts[item.baseName]++
	}

	bindings := make([]MCPToolBinding, 0, len(candidates))
	usedNames := make(map[string]bool, len(candidates))
	for _, item := range candidates {
		name := item.baseName
		if nameCounts[name] > 1 || usedNames[name] {
			sum := sha256.Sum256([]byte(item.server.ID + "\x00" + item.server.Name + "\x00" + item.rawName))
			suffix := hex.EncodeToString(sum[:4])
			name = qualifyToolName(item.slug, normalizeFunctionName(item.rawName, 30)) + "__" + suffix
		}
		for suffix := 2; usedNames[name]; suffix++ {
			name = fmt.Sprintf("%s_%d", truncateFunctionName(item.baseName, 64-suffixDigits(suffix)-1), suffix)
		}
		usedNames[name] = true

		description, schema := discoveredToolMetadata(item.server, item.rawName)
		if description == "" {
			description = "MCP tool from " + item.server.Name
		}
		bindings = append(bindings, MCPToolBinding{
			Name:        name,
			Description: description,
			InputSchema: normalizeInputSchema(schema),
			Server:      item.server,
			RawName:     item.rawName,
		})
	}
	return bindings
}

func discoveredToolMetadata(server mcp.ServerConfig, rawName string) (string, map[string]any) {
	for _, tool := range server.DiscoveredTools {
		if tool.Name == rawName {
			return tool.Description, tool.InputSchema
		}
	}
	return "", nil
}

func normalizeInputSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	normalized := make(map[string]any, len(schema)+2)
	for key, value := range schema {
		normalized[key] = value
	}
	if schemaType, ok := normalized["type"].(string); ok && schemaType == "object" {
		if _, ok := normalized["properties"]; !ok || normalized["properties"] == nil {
			normalized["properties"] = map[string]any{}
		}
	}
	return normalized
}

func serverSlug(server mcp.ServerConfig) string {
	source := strings.TrimSpace(server.Name)
	if source == "" {
		source = server.ID
	}
	slug := normalizeFunctionName(strings.ToLower(source), 24)
	if slug == "" {
		return "mcp"
	}
	return slug
}

func qualifyToolName(server, tool string) string {
	return truncateFunctionName(server, 24) + "__" + truncateFunctionName(tool, 38)
}

func normalizeFunctionName(name string, maxLength int) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.TrimSpace(name) {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if !valid {
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
			continue
		}
		b.WriteRune(r)
		lastUnderscore = r == '_'
	}
	normalized := truncateFunctionName(strings.Trim(b.String(), "_-"), maxLength)
	if normalized == "" {
		return "tool"
	}
	return normalized
}

func truncateFunctionName(name string, maxLength int) string {
	if maxLength < 1 {
		return ""
	}
	if len(name) > maxLength {
		return strings.TrimRight(name[:maxLength], "_-")
	}
	return name
}

func suffixDigits(n int) int {
	digits := 1
	for n >= 10 {
		n /= 10
		digits++
	}
	return digits
}
