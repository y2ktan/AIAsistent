package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"aegis-voice/server/internal/mcp"
	"github.com/google/uuid"
)

const hostedMCPOwnershipKey = "aegis_creation_intent"

type hostedMCPProviderConfig struct {
	Name                string            `json:"name"`
	URL                 string            `json:"url"`
	Transport           string            `json:"transport"`
	ApprovalPolicy      string            `json:"approval_policy"`
	RequestHeaders      map[string]string `json:"request_headers"`
	RequestMeta         map[string]string `json:"request_meta"`
	ResponseTimeoutSecs int               `json:"response_timeout_secs"`
}

type hostedMCPResource struct {
	ID     string          `json:"id"`
	Config json.RawMessage `json:"config"`
}

func readHostedMCP(ctx context.Context, client *http.Client, key, id string) (*hostedMCPResource, error) {
	if !validHostedID(id) {
		return nil, errors.New("Invalid hosted MCP resource mapping.")
	}
	var resource hostedMCPResource
	if err := hostedRequest(ctx, client, key, "/v1/convai/mcp-servers/"+url.PathEscape(id), &resource); err != nil {
		return nil, err
	}
	if resource.ID != id || len(resource.Config) == 0 {
		return nil, errors.New("ElevenLabs returned an unverifiable MCP resource.")
	}
	return &resource, nil
}

func hostedMCPConfigMatches(raw json.RawMessage, expected hostedMCPProviderConfig) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	for _, field := range []string{"secret_token", "auth_connection"} {
		if len(fields[field]) != 0 && strings.TrimSpace(string(fields[field])) != "null" {
			return false
		}
	}
	var actual hostedMCPProviderConfig
	if json.Unmarshal(raw, &actual) != nil {
		return false
	}
	return reflect.DeepEqual(actual, expected)
}

func validHostedMCPProviderConfig(raw json.RawMessage) bool {
	var c struct {
		URL       json.RawMessage `json:"url"`
		Transport string          `json:"transport"`
	}
	if json.Unmarshal(raw, &c) != nil || (c.Transport != "SSE" && c.Transport != "STREAMABLE_HTTP") {
		return false
	}
	var endpoint string
	if json.Unmarshal(c.URL, &endpoint) == nil {
		u, err := url.Parse(endpoint)
		return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
	}
	var locator struct {
		SecretID string `json:"secret_id"`
	}
	return decodeHostedStrict(c.URL, &locator) == nil && validHostedID(locator.SecretID)
}
func hostedMCPMarker(raw json.RawMessage) string {
	var c struct {
		RequestMeta map[string]json.RawMessage `json:"request_meta"`
	}
	if json.Unmarshal(raw, &c) != nil {
		return ""
	}
	var marker string
	_ = json.Unmarshal(c.RequestMeta[hostedMCPOwnershipKey], &marker)
	return marker
}

func hostedMCPToolInventory(ctx context.Context, client *http.Client, key, id string) ([]mcp.ToolDef, error) {
	var response struct {
		Success bool          `json:"success"`
		Tools   []mcp.ToolDef `json:"tools"`
	}
	if err := hostedRequest(ctx, client, key, "/v1/convai/mcp-servers/"+url.PathEscape(id)+"/tools", &response); err != nil {
		return nil, err
	}
	if !response.Success || response.Tools == nil || len(response.Tools) > 256 {
		return nil, errors.New("ElevenLabs could not verify the MCP tool inventory.")
	}
	seen := make(map[string]bool)
	for _, tool := range response.Tools {
		if tool.Name == "" || seen[tool.Name] || tool.InputSchema == nil {
			return nil, errors.New("ElevenLabs returned an invalid MCP tool inventory.")
		}
		seen[tool.Name] = true
	}
	return response.Tools, nil
}

func sameHostedToolNames(tools []mcp.ToolDef, allowed map[string]bool) bool {
	if len(tools) != len(allowed) {
		return false
	}
	seen := make(map[string]bool)
	for _, tool := range tools {
		if !allowed[tool.Name] || seen[tool.Name] {
			return false
		}
		seen[tool.Name] = true
	}
	return true
}

func (s *Server) desiredHostedMCP(ctx context.Context, rec *MCPServerRecord) (hostedMCPProviderConfig, error) {
	c := hostedMCPProviderConfig{Name: rec.Name, ApprovalPolicy: "auto_approve_all", RequestHeaders: map[string]string{}, RequestMeta: map[string]string{hostedMCPOwnershipKey: rec.HostedCreationIntentID}, ResponseTimeoutSecs: rec.TimeoutMs / 1000}
	if c.ResponseTimeoutSecs < 5 {
		c.ResponseTimeoutSecs = 5
	}
	if c.ResponseTimeoutSecs > 60 {
		c.ResponseTimeoutSecs = 60
	}
	c.Transport = "STREAMABLE_HTTP"
	if rec.HostedMode == "direct" {
		if _, err := hostedPublicHTTPS(rec.EndpointURL, false); err != nil {
			return c, err
		}
		authHeader := ""
		if len(rec.AuthHeaderCiphertext) > 0 {
			var err error
			authHeader, err = s.vault.Decrypt(rec.AuthHeaderCiphertext)
			if err != nil {
				return c, errors.New("Could not load MCP endpoint authentication.")
			}
		}
		discovered, err := s.mcpHost.DiscoverTools(ctx, mcp.ServerConfig{ID: rec.ID, EndpointURL: rec.EndpointURL, Transport: rec.Transport, AuthHeader: authHeader, TimeoutMs: rec.TimeoutMs})
		if err != nil {
			return c, errors.New("Could not verify the current direct MCP tool inventory.")
		}
		if len(rec.DiscoveredTools) == 0 || !sameHostedToolNames(discovered, allowedHostedMCPTools(rec)) || len(rec.AllowedTools) != len(discovered) {
			return c, errors.New("Direct mode requires all currently discovered tools to be allowlisted. Rediscover tools or choose proxy mode.")
		}
		c.URL = rec.EndpointURL
		if rec.Transport == "sse" {
			c.Transport = "SSE"
		}
		if authHeader != "" {
			c.RequestHeaders["Authorization"] = authHeader
		}
	} else if rec.HostedMode == "proxy" {
		base, err := hostedPublicHTTPS(s.hostedProxyBaseURL, true)
		if err != nil {
			return c, errors.New("Configure a public HTTPS HOSTED_MCP_PROXY_BASE_URL for proxy mode.")
		}
		token, err := s.vault.Decrypt(rec.HostedProxyCredential)
		if err != nil || token == "" {
			return c, errors.New("Hosted MCP proxy credential is unavailable. Save the proxy configuration again.")
		}
		c.URL = strings.TrimRight(base.String(), "/") + "/api/v1/hosted-mcp/" + url.PathEscape(rec.ID)
		c.RequestHeaders["Authorization"] = "Bearer " + token
	} else {
		return c, errors.New("Hosted MCP integration is disabled.")
	}
	return c, nil
}

func (s *Server) saveHostedMCPState(ctx context.Context, rec *MCPServerRecord) error {
	if _, err := s.adminStore.UpsertMCPServer(ctx, rec); err != nil {
		return errors.New("Could not persist hosted MCP synchronization state.")
	}
	return nil
}

func (s *Server) failHostedMCP(ctx context.Context, rec *MCPServerRecord, err error) error {
	switch {
	case rec.HostedResourceID == "" && rec.HostedCreationAttempted:
		rec.HostedSyncStatus = "recovery_pending"
	case rec.HostedMode == "disabled" || !rec.Enabled:
		rec.HostedSyncStatus = "revocation_pending"
	default:
		rec.HostedSyncStatus = "failed"
	}
	rec.HostedSyncError = err.Error()
	// Preserve ownership even when the incoming request deadline expired.
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if saveErr := s.saveHostedMCPState(persist, rec); saveErr != nil {
		return saveErr
	}
	return err
}

func (s *Server) recoverHostedMCPCreation(ctx context.Context, client *http.Client, key string, rec *MCPServerRecord) error {
	plain, err := s.vault.Decrypt(rec.HostedCreationConfig)
	if err != nil {
		return errors.New("Could not load durable hosted MCP creation intent.")
	}
	var expected hostedMCPProviderConfig
	if json.Unmarshal([]byte(plain), &expected) != nil || expected.RequestMeta[hostedMCPOwnershipKey] != rec.HostedCreationIntentID {
		return errors.New("Invalid durable hosted MCP creation intent.")
	}
	var response struct {
		Resources []hostedMCPResource `json:"mcp_servers"`
	}
	if err := hostedRequest(ctx, client, key, "/v1/convai/mcp-servers", &response); err != nil {
		return err
	}
	if response.Resources == nil || len(response.Resources) > 256 {
		return errors.New("Could not recover creation from a bounded MCP resource list.")
	}
	candidates := make([]string, 0)
	for _, resource := range response.Resources {
		if hostedMCPMarker(resource.Config) == rec.HostedCreationIntentID {
			candidates = append(candidates, resource.ID)
		}
	}
	if len(candidates) != 1 {
		return errors.New("MCP creation recovery requires exactly one verified ownership match. No new creation was attempted; administrator assistance may be required.")
	}
	resource, err := readHostedMCP(ctx, client, key, candidates[0])
	if err != nil {
		return err
	}
	if !hostedMCPConfigMatches(resource.Config, expected) {
		return errors.New("MCP creation recovery configuration does not match its durable intent.")
	}
	rec.HostedResourceID = resource.ID
	return s.saveHostedMCPState(ctx, rec)
}

func (s *Server) attachHostedMCP(ctx context.Context, client *http.Client, key string, rec *MCPServerRecord, attach bool) error {
	agentID := rec.HostedAgentID
	if !validHostedID(agentID) {
		return errors.New("Hosted MCP agent attachment mapping is unavailable.")
	}
	var config hostedAgent
	if err := hostedRequest(ctx, client, key, "/v1/convai/agents/"+url.PathEscape(agentID), &config); err != nil {
		return err
	}
	if config.AgentID != agentID || config.Conversation == nil || config.Conversation.Agent == nil || config.Conversation.Agent.Prompt == nil {
		return errors.New("Could not inspect hosted MCP agent attachment.")
	}
	p := config.Conversation.Agent.Prompt
	filter := func(ids []string) []string {
		out := make([]string, 0, len(ids)+1)
		for _, id := range ids {
			if id != rec.HostedResourceID {
				out = append(out, id)
			}
		}
		return out
	}
	native, other := filter(p.NativeMCP), filter(p.MCP)
	if attach {
		native = append(native, rec.HostedResourceID)
	}
	payload := map[string]any{"conversation_config": map[string]any{"agent": map[string]any{"prompt": map[string]any{"native_mcp_server_ids": native, "mcp_server_ids": other}}}}
	if err := hostedPatch(ctx, client, key, "/v1/convai/agents/"+url.PathEscape(agentID), payload); err != nil {
		return err
	}
	var verified hostedAgent
	if err := hostedRequest(ctx, client, key, "/v1/convai/agents/"+url.PathEscape(agentID), &verified); err != nil {
		return err
	}
	if verified.AgentID != agentID || verified.Conversation == nil || verified.Conversation.Agent == nil || verified.Conversation.Agent.Prompt == nil {
		return errors.New("Could not verify hosted MCP attachment.")
	}
	vp := verified.Conversation.Agent.Prompt
	if !reflect.DeepEqual(vp.NativeMCP, native) || !reflect.DeepEqual(vp.MCP, other) {
		return errors.New("Hosted MCP attachment was not confirmed.")
	}
	return nil
}

func (s *Server) reconcileHostedMCPServer(ctx context.Context, config *AgentConfigRecord, rec *MCPServerRecord) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	normalizeHostedMCP(rec)
	enabled := rec.Enabled && rec.HostedMode != "disabled" && config.Mode == "elevenlabs_agent"
	if !enabled && rec.HostedResourceID == "" && !rec.HostedCreationAttempted {
		rec.HostedSyncStatus, rec.HostedSyncError = "disabled", ""
		return s.saveHostedMCPState(ctx, rec)
	}
	key, err := s.hostedKey(config)
	if err != nil {
		return s.failHostedMCP(ctx, rec, err)
	}
	client := newHostedClient(s.allowPrivate)
	if rec.HostedResourceID == "" && rec.HostedCreationAttempted {
		if err := s.recoverHostedMCPCreation(ctx, client, key, rec); err != nil {
			return s.failHostedMCP(ctx, rec, err)
		}
	}
	if !enabled {
		rec.HostedSyncStatus = "revocation_pending"
		if err := s.saveHostedMCPState(ctx, rec); err != nil {
			return err
		}
		if err := s.attachHostedMCP(ctx, client, key, rec, false); err != nil {
			return s.failHostedMCP(ctx, rec, err)
		}
		resource, err := readHostedMCP(ctx, client, key, rec.HostedResourceID)
		var providerErr *hostedProviderError
		if err == nil {
			if hostedMCPMarker(resource.Config) != rec.HostedCreationIntentID {
				return s.failHostedMCP(ctx, rec, errors.New("Hosted MCP ownership marker does not match; cleanup was blocked."))
			}
			if err = hostedJSONRequest(ctx, client, key, http.MethodDelete, "/v1/convai/mcp-servers/"+url.PathEscape(rec.HostedResourceID), nil, nil); err != nil {
				return s.failHostedMCP(ctx, rec, err)
			}
			_, err = readHostedMCP(ctx, client, key, rec.HostedResourceID)
		}
		if !errors.As(err, &providerErr) || providerErr.status != http.StatusNotFound {
			return s.failHostedMCP(ctx, rec, errors.New("Hosted MCP remote deletion could not be verified."))
		}
		rec.HostedResourceID, rec.HostedAgentID, rec.HostedCreationIntentID = "", "", ""
		rec.HostedCreationAttempted = false
		rec.HostedCreationConfig, rec.HostedProxyCredential = nil, nil
		rec.HostedSyncStatus, rec.HostedSyncError = "disabled", ""
		return s.saveHostedMCPState(ctx, rec)
	}
	if rec.HostedAgentID != "" && rec.HostedAgentID != config.ElevenLabsAgentID {
		return s.failHostedMCP(ctx, rec, errors.New("Disable the existing hosted MCP attachment before changing agents."))
	}
	rec.HostedAgentID = config.ElevenLabsAgentID
	if rec.HostedCreationIntentID == "" {
		rec.HostedCreationIntentID = uuid.NewString()
	}
	desired, err := s.desiredHostedMCP(ctx, rec)
	if err != nil {
		return s.failHostedMCP(ctx, rec, err)
	}
	rec.HostedSyncStatus, rec.HostedSyncError = "pending", ""
	if rec.HostedResourceID == "" {
		raw, err := json.Marshal(desired)
		if err != nil {
			return s.failHostedMCP(ctx, rec, errors.New("Could not prepare MCP creation intent."))
		}
		rec.HostedCreationConfig, err = s.vault.Encrypt(string(raw))
		if err != nil {
			return s.failHostedMCP(ctx, rec, errors.New("Could not encrypt MCP creation intent."))
		}
		rec.HostedCreationAttempted = true
		if err := s.saveHostedMCPState(ctx, rec); err != nil {
			return err
		}
		var created hostedMCPResource
		if err := hostedJSONRequest(ctx, client, key, http.MethodPost, "/v1/convai/mcp-servers", map[string]any{"config": desired}, &created); err != nil {
			return s.failHostedMCP(ctx, rec, err)
		}
		if !validHostedID(created.ID) || !hostedMCPConfigMatches(created.Config, desired) {
			return s.failHostedMCP(ctx, rec, errors.New("MCP creation returned an unverifiable mapping; recovery is required."))
		}
		rec.HostedResourceID = created.ID
		if err := s.saveHostedMCPState(ctx, rec); err != nil {
			return err
		}
	}
	resource, err := readHostedMCP(ctx, client, key, rec.HostedResourceID)
	if err != nil {
		return s.failHostedMCP(ctx, rec, err)
	}
	if hostedMCPMarker(resource.Config) != rec.HostedCreationIntentID {
		return s.failHostedMCP(ctx, rec, errors.New("Hosted MCP ownership marker does not match; update was blocked."))
	}
	// Persist pending before any update so admission cannot accept stale policy.
	if err := s.saveHostedMCPState(ctx, rec); err != nil {
		return err
	}
	if !hostedMCPConfigMatches(resource.Config, desired) {
		if err := hostedJSONRequest(ctx, client, key, http.MethodPatch, "/v1/convai/mcp-servers/"+url.PathEscape(rec.HostedResourceID), map[string]any{"config": desired}, nil); err != nil {
			return s.failHostedMCP(ctx, rec, err)
		}
		resource, err = readHostedMCP(ctx, client, key, rec.HostedResourceID)
		if err != nil || !hostedMCPConfigMatches(resource.Config, desired) {
			return s.failHostedMCP(ctx, rec, errors.New("Hosted MCP update could not be verified."))
		}
	}
	inventory, err := hostedMCPToolInventory(ctx, client, key, rec.HostedResourceID)
	if err != nil {
		return s.failHostedMCP(ctx, rec, err)
	}
	if !sameHostedToolNames(inventory, allowedHostedMCPTools(rec)) {
		return s.failHostedMCP(ctx, rec, errors.New("Hosted MCP inventory does not match the application allowlist."))
	}
	if err := s.attachHostedMCP(ctx, client, key, rec, true); err != nil {
		return s.failHostedMCP(ctx, rec, err)
	}
	snapshot, err := json.Marshal(desired)
	if err != nil {
		return s.failHostedMCP(ctx, rec, errors.New("Could not prepare verified MCP configuration."))
	}
	rec.HostedCreationConfig, err = s.vault.Encrypt(string(snapshot))
	if err != nil {
		return s.failHostedMCP(ctx, rec, errors.New("Could not persist encrypted verified MCP configuration."))
	}
	rec.HostedSyncStatus, rec.HostedSyncError = "synced", ""
	return s.saveHostedMCPState(ctx, rec)
}

func (s *Server) syncHostedMCP(ctx context.Context, config *AgentConfigRecord) error {
	return s.adminStore.WithHostedMCPLock(ctx, func(store AdminStore) error {
		locked := *s
		locked.adminStore = store
		records, err := store.ListMCPServers(ctx)
		if err != nil {
			return errors.New("Could not load application MCP integrations.")
		}
		for i := range records {
			if records[i].HostedMode == "disabled" && records[i].HostedResourceID == "" && !records[i].HostedCreationAttempted {
				continue
			}
			if err := locked.reconcileHostedMCPServer(ctx, config, &records[i]); err != nil {
				return err
			}
		}
		return nil
	})
}
