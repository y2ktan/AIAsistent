package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"aegis-voice/server/internal/agent"
	"aegis-voice/server/internal/mcp"
	"aegis-voice/server/internal/security"
	"aegis-voice/server/internal/skills"
)

const maxHostedPromptBytes = 32 * 1024

type hostedAgent struct {
	AgentID   string  `json:"agent_id"`
	Name      string  `json:"name"`
	VersionID *string `json:"version_id"`
	Platform  *struct {
		Auth *struct {
			Enabled *bool `json:"enable_auth"`
		} `json:"auth"`
		Overrides *struct {
			FromWebhook bool `json:"enable_conversation_initiation_client_data_from_webhook"`
			Workflow    bool `json:"enable_starting_workflow_node_id_from_client"`
			Procedures  bool `json:"enable_procedure_ids_from_client"`
			ExtraBody   bool `json:"custom_llm_extra_body"`
			Config      struct {
				Agent struct {
					Prompt struct {
						Tools       bool `json:"tool_ids"`
						NativeMCP   bool `json:"native_mcp_server_ids"`
						StrictTools bool `json:"tool_ids_strict"`
					} `json:"prompt"`
				} `json:"agent"`
			} `json:"conversation_config_override"`
		} `json:"overrides"`
	} `json:"platform_settings"`
	Conversation *struct {
		Agent *struct {
			Subagents json.RawMessage `json:"subagent_config"`
			Prompt    *struct {
				Text          string                 `json:"prompt"`
				KnowledgeBase []json.RawMessage      `json:"knowledge_base"`
				RAG           json.RawMessage        `json:"rag"`
				Tools         []hostedTool           `json:"tools"`
				ToolIDs       []string               `json:"tool_ids"`
				MCP           []string               `json:"mcp_server_ids"`
				NativeMCP     []string               `json:"native_mcp_server_ids"`
				BuiltIn       map[string]*hostedTool `json:"built_in_tools"`
			} `json:"prompt"`
		} `json:"agent"`
	} `json:"conversation_config"`
	Workflow            json.RawMessage            `json:"workflow"`
	ValidatedTools      []hostedTool               `json:"-"`
	ValidatedMCP        map[string][]mcp.ToolDef   `json:"-"`
	ValidatedMCPConfigs map[string]json.RawMessage `json:"-"`
}

type hostedKnowledgeBaseEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	UsageMode string `json:"usage_mode"`
}

type hostedTool struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	MCPServerID string `json:"mcp_server_id"`
	MCPToolName string `json:"mcp_tool_name"`
	APISchema   *struct {
		URL    string `json:"url"`
		Method string `json:"method"`
	} `json:"api_schema"`
	Params struct {
		Type string `json:"system_tool_type"`
	} `json:"params"`
}

type hostedProviderError struct {
	status  int
	message string
}

func (e *hostedProviderError) Error() string {
	if e.message != "" {
		return e.message
	}
	switch e.status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "ElevenLabs denied access. Check the key permissions and private agent."
	case http.StatusNotFound:
		return "The configured ElevenLabs agent or tool was not found."
	case http.StatusPaymentRequired:
		return "ElevenLabs requires payment or account access. Check credits and subscription."
	case http.StatusTooManyRequests:
		return "ElevenLabs is limiting requests. Try again later."
	default:
		return "ElevenLabs could not complete the request."
	}
}

func validHostedID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func hostedRequest(ctx context.Context, client *http.Client, key, path string, out any) error {
	return hostedJSONRequest(ctx, client, key, http.MethodGet, path, nil, out)
}

func hostedPatch(ctx context.Context, client *http.Client, key, path string, payload any) error {
	return hostedJSONRequest(ctx, client, key, http.MethodPatch, path, payload, nil)
}

func hostedJSONRequest(ctx context.Context, client *http.Client, key, method, path string, payload, out any) error {
	var requestBody io.Reader
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			return errors.New("Could not prepare the ElevenLabs request.")
		}
		requestBody = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(elevenLabsAdminAPIBaseURL, "/")+path, requestBody)
	if err != nil {
		return errors.New("Could not prepare the ElevenLabs request.")
	}
	req.Header.Set("xi-api-key", key)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return &hostedProviderError{message: "ElevenLabs is unreachable or the request timed out."}
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return &hostedProviderError{status: resp.StatusCode}
	}
	const maxResponse = 2 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(body) > maxResponse {
		return &hostedProviderError{message: "Could not read a bounded ElevenLabs response."}
	}
	if out != nil && json.Unmarshal(body, out) != nil {
		return &hostedProviderError{message: "ElevenLabs returned an invalid response."}
	}
	return nil
}

func newHostedClient(allowPrivate bool) *http.Client {
	client := security.NewSafeHTTPClient(allowPrivate, 10*time.Second)
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client
}

func validateHostedTool(tool hostedTool) error {
	switch tool.Type {
	case "webhook":
		if !validHostedID(tool.Name) || tool.APISchema == nil {
			return errors.New("The hosted webhook tool is incomplete.")
		}
		u, err := url.Parse(tool.APISchema.URL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			return errors.New("The hosted webhook tool requires a valid HTTPS URL.")
		}
		switch tool.APISchema.Method {
		case "GET", "POST", "PUT", "PATCH", "DELETE":
			return nil
		}
		return errors.New("The hosted webhook tool has an unsupported method.")
	case "mcp":
		if validHostedID(tool.MCPServerID) && tool.MCPToolName != "" && validHostedID(tool.Name) {
			return nil
		}
		return errors.New("The hosted MCP tool has an invalid reference.")
	case "system", "":
	default:
		return errors.New("The hosted agent has an unknown or browser-client tool.")
	}
	switch tool.Params.Type {
	case "end_call", "skip_turn", "language_detection", "knowledge_base", "knowledge_base_rag":
		return nil
	default:
		return errors.New("The ElevenLabs agent has an unsupported system tool.")
	}
}

func validateHostedSubagents(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var config struct {
		Agents []json.RawMessage `json:"agents"`
		Self   json.RawMessage   `json:"self_subagent"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil || len(config.Agents) != 0 || len(config.Self) > 0 && string(config.Self) != "null" {
		return errors.New("Use a simple ElevenLabs agent without subagents.")
	}
	return nil
}

func inspectHostedAgent(ctx context.Context, client *http.Client, key, id string) (string, error) {
	config, err := loadHostedAgent(ctx, client, key, id)
	if err != nil {
		return "", err
	}
	return *config.VersionID, nil
}

func loadHostedAgent(ctx context.Context, client *http.Client, key, id string) (*hostedAgent, error) {
	if key == "" || !validHostedID(id) {
		return nil, errors.New("Configure an ElevenLabs key and valid private agent ID.")
	}
	var config hostedAgent
	if err := hostedRequest(ctx, client, key, "/v1/convai/agents/"+url.PathEscape(id), &config); err != nil {
		return nil, err
	}
	if _, err := validateHostedAgentConfig(ctx, client, key, id, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func validateHostedAgentConfig(ctx context.Context, client *http.Client, key, id string, config *hostedAgent) (string, error) {
	if config == nil {
		return "", errors.New("ElevenLabs returned incomplete agent configuration.")
	}
	if config.AgentID != id || config.VersionID == nil || !validHostedID(*config.VersionID) {
		return "", errors.New("ElevenLabs did not provide a verifiable agent version.")
	}
	if config.Platform == nil || config.Platform.Auth == nil || config.Platform.Auth.Enabled == nil || !*config.Platform.Auth.Enabled {
		return "", errors.New("Enable authentication on the private ElevenLabs agent.")
	}
	if config.Platform.Overrides != nil {
		o := config.Platform.Overrides
		if o.FromWebhook || o.Workflow || o.Procedures || o.ExtraBody || o.Config.Agent.Prompt.Tools || o.Config.Agent.Prompt.NativeMCP || o.Config.Agent.Prompt.StrictTools {
			return "", errors.New("Disable external tool and workflow client overrides on the ElevenLabs agent.")
		}
	}
	workflow, err := inspectHostedWorkflow(config.Workflow)
	if err != nil {
		return "", err
	}
	if config.Conversation == nil || config.Conversation.Agent == nil || config.Conversation.Agent.Prompt == nil {
		return "", errors.New("ElevenLabs returned incomplete agent configuration.")
	}
	if err := validateHostedSubagents(config.Conversation.Agent.Subagents); err != nil {
		return "", err
	}
	prompt := config.Conversation.Agent.Prompt
	config.ValidatedTools = nil
	config.ValidatedMCP = make(map[string][]mcp.ToolDef)
	config.ValidatedMCPConfigs = make(map[string]json.RawMessage)
	mcpIDs := append(append([]string{}, prompt.MCP...), prompt.NativeMCP...)
	mcpIDs = append(mcpIDs, workflow.MCPServerIDs...)
	config.ValidatedTools = append(config.ValidatedTools, workflow.Tools...)
	for _, tool := range workflow.Tools {
		if tool.Type == "mcp" {
			mcpIDs = append(mcpIDs, tool.MCPServerID)
		}
	}
	for _, tool := range prompt.Tools {
		if err := validateHostedTool(tool); err != nil {
			return "", err
		}
		if tool.Type == "mcp" {
			mcpIDs = append(mcpIDs, tool.MCPServerID)
		}
		config.ValidatedTools = append(config.ValidatedTools, tool)
	}
	for _, tool := range prompt.BuiltIn {
		if tool == nil {
			continue
		}
		if err := validateHostedTool(*tool); err != nil {
			return "", err
		}
		config.ValidatedTools = append(config.ValidatedTools, *tool)
	}
	toolIDs := append(append([]string{}, prompt.ToolIDs...), workflow.ToolIDs...)
	if len(toolIDs) > 64 || len(prompt.Tools) > 64 {
		return "", errors.New("The ElevenLabs agent has too many tool references to validate.")
	}
	seenTools := make(map[string]bool)
	for _, toolID := range toolIDs {
		if !validHostedID(toolID) {
			return "", errors.New("ElevenLabs returned an invalid tool reference.")
		}
		if seenTools[toolID] {
			continue
		}
		seenTools[toolID] = true
		var response struct {
			ID   string     `json:"id"`
			Tool hostedTool `json:"tool_config"`
		}
		if err := hostedRequest(ctx, client, key, "/v1/convai/tools/"+url.PathEscape(toolID), &response); err != nil {
			return "", err
		}
		if response.ID != toolID {
			return "", errors.New("ElevenLabs returned an unverifiable tool reference.")
		}
		if err := validateHostedTool(response.Tool); err != nil {
			return "", err
		}
		if response.Tool.Type == "mcp" {
			mcpIDs = append(mcpIDs, response.Tool.MCPServerID)
		}
		config.ValidatedTools = append(config.ValidatedTools, response.Tool)
	}
	if len(mcpIDs) > 64 {
		return "", errors.New("The hosted agent has too many MCP references.")
	}
	seenMCP := make(map[string]bool)
	for _, mcpID := range mcpIDs {
		if !validHostedID(mcpID) {
			return "", errors.New("The hosted agent has an invalid MCP reference.")
		}
		if seenMCP[mcpID] {
			continue
		}
		seenMCP[mcpID] = true
		resource, err := readHostedMCP(ctx, client, key, mcpID)
		if err != nil {
			return "", err
		}
		if !validHostedMCPProviderConfig(resource.Config) {
			return "", errors.New("ElevenLabs returned an unverifiable MCP resource.")
		}
		inventory, err := hostedMCPToolInventory(ctx, client, key, mcpID)
		if err != nil {
			return "", err
		}
		config.ValidatedMCP[mcpID] = inventory
		config.ValidatedMCPConfigs[mcpID] = resource.Config
	}
	for _, tool := range config.ValidatedTools {
		if tool.Type == "mcp" {
			found := false
			for _, available := range config.ValidatedMCP[tool.MCPServerID] {
				if available.Name == tool.MCPToolName {
					found = true
				}
			}
			if !found {
				return "", errors.New("Hosted MCP tool reference could not be resolved.")
			}
		}
	}
	return *config.VersionID, nil
}

func (s *Server) hostedKey(config *AgentConfigRecord) (string, error) {
	if config == nil || len(config.ElevenLabsAPIKeyCiphertext) == 0 {
		return "", errors.New("Configure an ElevenLabs API key before selecting hosted mode.")
	}
	key, err := s.vault.Decrypt(config.ElevenLabsAPIKeyCiphertext)
	if err != nil || strings.TrimSpace(key) == "" {
		return "", errors.New("Could not load the ElevenLabs credential.")
	}
	return key, nil
}

func (s *Server) validateHostedConfig(ctx context.Context, config *AgentConfigRecord) (string, error) {
	key, err := s.hostedKey(config)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return inspectHostedAgent(ctx, newHostedClient(s.allowPrivate), key, config.ElevenLabsAgentID)
}

func compileHostedPrompt(config *AgentConfigRecord, skillRecords []skills.Skill) (string, error) {
	if config == nil {
		return "", errors.New("Hosted agent configuration is unavailable.")
	}
	assistantName := strings.TrimSpace(config.AssistantName)
	if assistantName == "" {
		assistantName = agent.DefaultAssistantName
	}
	if !utf8.ValidString(assistantName) || !utf8.ValidString(config.SystemInstructions) {
		return "", errors.New("Hosted prompt content must be valid UTF-8.")
	}

	var prompt strings.Builder
	prompt.WriteString(agent.SecurityBoundaryPreamble)
	prompt.WriteString("\n\nHOSTED ASSISTANT PERSONA\nAssistant name: ")
	prompt.WriteString(strconv.Quote(assistantName))
	prompt.WriteString("\n\nAPPLICATION SYSTEM INSTRUCTIONS\n")
	prompt.WriteString(strings.TrimSpace(config.SystemInstructions))

	if !config.ElevenLabsSkillsSyncEnabled {
		skillRecords = nil
	}
	sort.Slice(skillRecords, func(i, j int) bool {
		if skillRecords[i].Name == skillRecords[j].Name {
			return skillRecords[i].ID < skillRecords[j].ID
		}
		return skillRecords[i].Name < skillRecords[j].Name
	})
	for _, skill := range skillRecords {
		if !skill.Enabled {
			continue
		}
		if !utf8.ValidString(skill.Name) || !utf8.ValidString(skill.Description) || !utf8.ValidString(skill.Instructions) {
			return "", errors.New("Enabled Agent Skills must contain valid UTF-8.")
		}
		prompt.WriteString("\n\nDECLARATIVE AGENT SKILL\nName: ")
		prompt.WriteString(strconv.Quote(skill.Name))
		prompt.WriteString("\nDescription: ")
		prompt.WriteString(strings.TrimSpace(skill.Description))
		prompt.WriteString("\nInstructions:\n")
		prompt.WriteString(skill.Instructions)

		referencePaths := make([]string, 0, len(skill.ReferenceFiles))
		for path := range skill.ReferenceFiles {
			referencePaths = append(referencePaths, path)
		}
		sort.Strings(referencePaths)
		for _, path := range referencePaths {
			content := skill.ReferenceFiles[path]
			if !utf8.ValidString(path) || !utf8.ValidString(content) {
				return "", errors.New("Agent Skill reference files must contain valid UTF-8.")
			}
			prompt.WriteString("\n\nSKILL REFERENCE ")
			prompt.WriteString(strconv.Quote(path))
			prompt.WriteString("\n")
			prompt.WriteString(content)
		}
	}
	if prompt.Len() > maxHostedPromptBytes {
		return "", fmt.Errorf("Hosted prompt exceeds the %d-byte limit; reduce enabled Agent Skills or references.", maxHostedPromptBytes)
	}
	return prompt.String(), nil
}

func (s *Server) syncHostedPrompt(ctx context.Context, config *AgentConfigRecord) error {
	if config == nil || config.Mode != "elevenlabs_agent" {
		return errors.New("Hosted prompt synchronization requires hosted mode.")
	}
	var skillRecords []skills.Skill
	if config.ElevenLabsSkillsSyncEnabled {
		var err error
		skillRecords, err = s.adminStore.ListAgentSkills(ctx)
		if err != nil {
			return errors.New("Could not load Agent Skills for hosted synchronization.")
		}
	}
	prompt, err := compileHostedPrompt(config, skillRecords)
	if err != nil {
		return err
	}
	key, err := s.hostedKey(config)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client := newHostedClient(s.allowPrivate)
	if _, err := inspectHostedAgent(ctx, client, key, config.ElevenLabsAgentID); err != nil {
		return err
	}
	assistantName := strings.TrimSpace(config.AssistantName)
	if assistantName == "" {
		assistantName = agent.DefaultAssistantName
	}
	payload := map[string]any{
		"name": assistantName,
		"conversation_config": map[string]any{
			"agent": map[string]any{
				"prompt": map[string]string{"prompt": prompt},
			},
		},
	}
	if err := hostedPatch(
		ctx,
		client,
		key,
		"/v1/convai/agents/"+url.PathEscape(config.ElevenLabsAgentID),
		payload,
	); err != nil {
		return err
	}
	verified, err := loadHostedAgent(ctx, client, key, config.ElevenLabsAgentID)
	if err != nil {
		return fmt.Errorf("Hosted prompt was updated but could not be verified: %w", err)
	}
	if !config.ElevenLabsSkillsSyncEnabled && verified.Conversation.Agent.Prompt.Text != prompt {
		return errors.New("Hosted base-only prompt could not be verified. Retry the configuration save or synchronization.")
	}
	return nil
}

func (s *Server) syncHostedPromptIfActive(ctx context.Context) error {
	config, err := s.adminStore.GetAgentConfig(ctx)
	if err != nil {
		return errors.New("Could not load hosted configuration for Agent Skill synchronization.")
	}
	if config == nil || config.Mode != "elevenlabs_agent" || !config.ElevenLabsSkillsSyncEnabled {
		return nil
	}
	if err := s.validateHostedSkillAvailability(ctx, config); err != nil {
		return err
	}
	return s.syncHostedPrompt(ctx, config)
}

func hostedErrorStatus(err error) int {
	var providerErr *hostedProviderError
	if errors.As(err, &providerErr) {
		return http.StatusBadGateway
	}
	return http.StatusBadRequest
}

func (s *Server) HandleHostedConversationToken(w http.ResponseWriter, r *http.Request) {
	s.withHostedMCPAdmin(w, r, func(locked *Server, r *http.Request) { locked.hostedConversationToken(w, r) })
}

func (s *Server) hostedConversationToken(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Hosted access requires an Origin header."})
		return
	}
	var body struct{}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&body)
	if err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Hosted access does not accept agent or provider overrides."})
		return
	}
	if err == nil {
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Hosted access accepts only one empty request."})
			return
		}
	}
	config, err := s.adminStore.GetAgentConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not load hosted configuration."})
		return
	}
	if config.Mode != "elevenlabs_agent" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Conversation mode changed. Refresh and start again."})
		return
	}
	key, err := s.hostedKey(config)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	client := newHostedClient(s.allowPrivate)
	verified, err := loadHostedAgent(ctx, client, key, config.ElevenLabsAgentID)
	if err != nil {
		writeJSON(w, hostedErrorStatus(err), map[string]string{"error": err.Error()})
		return
	}
	if err := s.validateHostedToolAdmission(ctx, verified); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if config.ElevenLabsSkillsSyncEnabled {
		if err := s.validateHostedSkills(ctx, verified); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
	}
	version := *verified.VersionID
	query := url.Values{"agent_id": {config.ElevenLabsAgentID}, "version_id": {version}}
	var response struct {
		Token          string `json:"token"`
		ConversationID string `json:"conversation_id"`
	}
	if err := hostedRequest(ctx, client, key, "/v1/convai/conversation/token?"+query.Encode(), &response); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(response.Token) == "" || !validHostedID(response.ConversationID) {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "ElevenLabs returned incomplete conversation access."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"conversationToken": response.Token,
		"conversationId":    response.ConversationID,
		"mode":              config.Mode,
		"assistantName":     config.AssistantName,
	})
}

func (s *Server) testHostedConfig(w http.ResponseWriter, r *http.Request, config *AgentConfigRecord) {
	version, err := s.validateHostedConfig(r.Context(), config)
	if err != nil {
		writeJSON(w, hostedErrorStatus(err), map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": fmt.Sprintf("Private hosted agent and supported workflow verified (version %s). Test voice and grounding separately.", version)})
}
