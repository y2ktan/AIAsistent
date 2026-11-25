package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"aegis-voice/server/internal/auth"
	"aegis-voice/server/internal/mcp"
	"aegis-voice/server/internal/rag"
	"aegis-voice/server/internal/security"
	"aegis-voice/server/internal/skills"
	"github.com/google/uuid"
)

func newMCPTestServer(t *testing.T) (*mockAllStore, *Server) {
	t.Helper()
	store := newMockAllStore()
	vault, err := security.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := vault.Encrypt(hostedFixtureKey)
	if err != nil {
		t.Fatal(err)
	}
	store.agentCfg.Mode = "elevenlabs_agent"
	store.agentCfg.ElevenLabsAgentID = "agent_fixture"
	store.agentCfg.ElevenLabsAPIKeyCiphertext = encrypted
	s := NewServer(nil, store, store, rag.NewService(store.ragStore, 0), nil, mcp.NewHost(true), vault, true, nil)
	s.hostedProxyBaseURL = "https://8.8.8.8"
	return store, s
}

func newProxyMCPRecord(t *testing.T, s *Server, endpoint string) *MCPServerRecord {
	t.Helper()
	token, err := s.vault.Encrypt("fixture-proxy-credential")
	if err != nil {
		t.Fatal(err)
	}
	authHeader, err := s.vault.Encrypt("Bearer fixture-upstream-credential")
	if err != nil {
		t.Fatal(err)
	}
	rec := &MCPServerRecord{ID: uuid.NewString(), Name: "fixture", EndpointURL: endpoint, Transport: "streamable_http", Enabled: true, AllowedTools: []string{"lookup"}, DiscoveredTools: []mcp.ToolDef{{Name: "lookup", InputSchema: map[string]any{"type": "object"}}, {Name: "blocked", InputSchema: map[string]any{"type": "object"}}}, TimeoutMs: 1000, HostedMode: "proxy", HostedProxyCredential: token, AuthHeaderCiphertext: authHeader, HostedSyncStatus: "synced", HostedAgentID: "agent_fixture"}
	rec.HostedCreationIntentID = uuid.NewString()
	desired, err := s.desiredHostedMCP(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(desired)
	if err != nil {
		t.Fatal(err)
	}
	rec.HostedCreationConfig, err = s.vault.Encrypt(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.adminStore.UpsertMCPServer(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestHostedMCPProxyAuthorization(t *testing.T) {
	var forwarded atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-upstream-credential" {
			t.Error("missing upstream authentication")
		}
		var request struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request.Method == "tools/call" {
			forwarded.Add(1)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"safe lookup"}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer upstream.Close()
	store, s := newMCPTestServer(t)
	rec := newProxyMCPRecord(t, s, upstream.URL)
	for _, tt := range []struct {
		name, credential, body string
		status                 int
		forwarded              bool
	}{
		{"missing auth", "", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"lookup"}}`, 401, false},
		{"wrong auth", "wrong", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"lookup"}}`, 401, false},
		{"blocked", "fixture-proxy-credential", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"blocked"}}`, 200, false},
		{"unknown method", "fixture-proxy-credential", `{"jsonrpc":"2.0","id":1,"method":"resources/read"}`, 200, false},
		{"unknown field", "fixture-proxy-credential", `{"jsonrpc":"2.0","id":1,"method":"tools/list","url":"https://attacker.example"}`, 400, false},
		{"trailing data", "fixture-proxy-credential", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}{}`, 400, false},
		{"batch", "fixture-proxy-credential", `[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]`, 400, false},
		{"oversized", "fixture-proxy-credential", strings.Repeat(" ", 65537), 400, false},
		{"allowed", "fixture-proxy-credential", `{"jsonrpc":"2.0","id":"request","method":"tools/call","params":{"name":"lookup","arguments":{"query":"private arguments"}}}`, 200, true},
		{"list", "fixture-proxy-credential", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, 200, false},
		{"initialize", "fixture-proxy-credential", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}`, 200, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := forwarded.Load()
			request := httptest.NewRequest("POST", "/api/v1/hosted-mcp/"+rec.ID, strings.NewReader(tt.body))
			request.SetPathValue("id", rec.ID)
			if tt.credential != "" {
				request.Header.Set("Authorization", "Bearer "+tt.credential)
			}
			response := httptest.NewRecorder()
			s.HandleHostedMCPProxy(response, request)
			if response.Code != tt.status || (forwarded.Load() > before) != tt.forwarded {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, forwarded.Load()-before, response.Body.String())
			}
			if tt.name == "list" && (strings.Contains(response.Body.String(), "blocked") || !strings.Contains(response.Body.String(), "lookup")) {
				t.Fatal("list violated allowlist")
			}
			if strings.Contains(response.Body.String(), "credential") && tt.status == 200 {
				t.Fatal("secret leaked")
			}
		})
	}
	for _, state := range []string{"disabled", "failed"} {
		rec.HostedSyncStatus = state
		_, _ = store.UpsertMCPServer(context.Background(), rec)
		request := httptest.NewRequest("POST", "/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"lookup"}}`))
		request.SetPathValue("id", rec.ID)
		request.Header.Set("Authorization", "Bearer fixture-proxy-credential")
		before := forwarded.Load()
		response := httptest.NewRecorder()
		s.HandleHostedMCPProxy(response, request)
		if response.Code != 403 || forwarded.Load() != before {
			t.Fatal("failed state forwarded a call")
		}
	}
}

type hostedMCPProviderFixture struct {
	mu            sync.Mutex
	resources     map[string]json.RawMessage
	native        []string
	other         []string
	creates       int
	failCreate    bool
	failDelete    bool
	delayCreate   time.Duration
	tokenRequests int
	prompt        string
}

func (p *hostedMCPProviderFixture) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		if r.Header.Get("xi-api-key") != hostedFixtureKey {
			t.Error("missing provider credential")
		}
		if strings.HasPrefix(r.URL.Path, "/v1/convai/agents/") {
			if r.Method == "PATCH" {
				var body struct {
					Conversation struct {
						Agent struct {
							Prompt struct {
								Native []string `json:"native_mcp_server_ids"`
								MCP    []string `json:"mcp_server_ids"`
								Text   *string  `json:"prompt"`
							} `json:"prompt"`
						} `json:"agent"`
					} `json:"conversation_config"`
				}
				raw := make(map[string]json.RawMessage)
				_ = json.NewDecoder(r.Body).Decode(&raw)
				encoded, _ := json.Marshal(raw)
				_ = json.Unmarshal(encoded, &body)
				if _, ok := raw["workflow"]; ok {
					t.Error("attachment overwrote workflow")
				}
				if body.Conversation.Agent.Prompt.Native != nil {
					p.native = body.Conversation.Agent.Prompt.Native
				}
				if body.Conversation.Agent.Prompt.MCP != nil {
					p.other = body.Conversation.Agent.Prompt.MCP
				}
				if body.Conversation.Agent.Prompt.Text != nil {
					p.prompt = *body.Conversation.Agent.Prompt.Text
				}
			}
			native, _ := json.Marshal(p.native)
			other, _ := json.Marshal(p.other)
			fixture := strings.Replace(hostedFixture, `"native_mcp_server_ids":[]`, `"native_mcp_server_ids":`+string(native), 1)
			fixture = strings.Replace(fixture, `"mcp_server_ids":[]`, `"mcp_server_ids":`+string(other), 1)
			prompt, _ := json.Marshal(p.prompt)
			fixture = strings.Replace(fixture, `"prompt":{"tool_ids"`, `"prompt":{"prompt":`+string(prompt)+`,"tool_ids"`, 1)
			_, _ = w.Write([]byte(fixture))
			return
		}
		if r.URL.Path == "/v1/convai/conversation/token" {
			p.tokenRequests++
			writeJSON(w, 200, map[string]string{"token": "fixture-token", "conversation_id": "conversation_fixture"})
			return
		}
		if r.URL.Path == "/v1/convai/mcp-servers" {
			if r.Method == "POST" {
				var request struct {
					Config json.RawMessage `json:"config"`
				}
				_ = json.NewDecoder(r.Body).Decode(&request)
				p.creates++
				p.resources["mcp_fixture"] = request.Config
				if p.delayCreate > 0 {
					delay := p.delayCreate
					p.delayCreate = 0
					select {
					case <-r.Context().Done():
						return
					case <-time.After(delay):
					}
				}
				if p.failCreate {
					p.failCreate = false
					w.WriteHeader(504)
					return
				}
				writeJSON(w, 200, hostedMCPResource{ID: "mcp_fixture", Config: request.Config})
				return
			}
			resources := make([]hostedMCPResource, 0, len(p.resources))
			for id, c := range p.resources {
				resources = append(resources, hostedMCPResource{ID: id, Config: c})
			}
			writeJSON(w, 200, map[string]any{"mcp_servers": resources})
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/v1/convai/mcp-servers/")
		if strings.HasSuffix(id, "/tools") {
			writeJSON(w, 200, map[string]any{"success": true, "tools": []mcp.ToolDef{{Name: "lookup", InputSchema: map[string]any{"type": "object"}}}})
			return
		}
		config, ok := p.resources[id]
		if !ok {
			w.WriteHeader(404)
			return
		}
		if r.Method == "DELETE" {
			if p.failDelete {
				w.WriteHeader(503)
				return
			}
			delete(p.resources, id)
			w.WriteHeader(204)
			return
		}
		if r.Method == "PATCH" {
			var request struct {
				Config json.RawMessage `json:"config"`
			}
			_ = json.NewDecoder(r.Body).Decode(&request)
			p.resources[id] = request.Config
			config = request.Config
		}
		writeJSON(w, 200, hostedMCPResource{ID: id, Config: config})
	})
}

func TestHostedMCPAdminFailureRetainsDisabledOwnership(t *testing.T) {
	t.Setenv("HOSTED_MCP_PROXY_BASE_URL", "https://8.8.8.8")
	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	store.agentCfg.Mode = "elevenlabs_agent"
	provider := &hostedMCPProviderFixture{resources: map[string]json.RawMessage{}, native: []string{}, other: []string{}, failCreate: true}
	upstream := httptest.NewServer(provider.handler(t))
	defer upstream.Close()
	old := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = old }()
	response := hostedAPIRequest(router, cookie, "POST", "/api/v1/admin/mcp-servers", `{"name":"fixture","endpointUrl":"http://localhost:1","enabled":true,"hostedMode":"proxy","allowedTools":[]}`)
	if response.Code != 502 {
		t.Fatalf("provider failure reported success: %d %s", response.Code, response.Body.String())
	}
	records, _ := store.ListMCPServers(context.Background())
	if len(records) != 1 || records[0].HostedSyncStatus != "recovery_pending" {
		t.Fatal("saved choice and recovery state were lost")
	}
	id := records[0].ID
	// Permit verification using the application tool set.
	rec := records[0]
	rec.AllowedTools = []string{"lookup"}
	rec.DiscoveredTools = []mcp.ToolDef{{Name: "lookup", InputSchema: map[string]any{"type": "object"}}}
	_, _ = store.UpsertMCPServer(context.Background(), &rec)
	response = hostedAPIRequest(router, cookie, "POST", "/api/v1/admin/mcp-servers/"+id+"/sync", "")
	if response.Code != 200 {
		t.Fatalf("retry failed: %d %s", response.Code, response.Body.String())
	}
	provider.failDelete = true
	response = hostedAPIRequest(router, cookie, "DELETE", "/api/v1/admin/mcp-servers/"+id, "")
	if response.Code != 502 {
		t.Fatalf("incomplete cleanup reported deletion: %d", response.Code)
	}
	saved, err := store.GetMCPServer(context.Background(), id)
	if err != nil || saved.Enabled || saved.HostedMode != "disabled" || saved.HostedResourceID == "" || saved.HostedSyncStatus != "revocation_pending" {
		t.Fatal("disabled ownership record not retained")
	}
	provider.failDelete = false
	response = hostedAPIRequest(router, cookie, "DELETE", "/api/v1/admin/mcp-servers/"+id, "")
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
}

func TestHostedMCPTokenAdmission(t *testing.T) {
	for _, state := range []string{"synced", "pending", "failed", "revocation_pending", "recovery_pending", "drift", "skill_unavailable"} {
		t.Run(state, func(t *testing.T) {
			store, router, cookie := newHostedAPITest(t, auth.RoleUser)
			store.agentCfg.Mode = "elevenlabs_agent"
			rec := &MCPServerRecord{ID: uuid.NewString(), Name: "fixture", Enabled: true, HostedMode: "proxy", HostedSyncStatus: state, HostedResourceID: "mcp_fixture", HostedAgentID: "agent_fixture", AllowedTools: []string{"lookup"}, DiscoveredTools: []mcp.ToolDef{{Name: "lookup", InputSchema: map[string]any{"type": "object"}}}}
			if state == "drift" || state == "skill_unavailable" {
				rec.HostedSyncStatus = "synced"
			}
			_, _ = store.UpsertMCPServer(context.Background(), rec)
			rec.HostedCreationIntentID = uuid.NewString()
			expected := hostedMCPProviderConfig{Name: "fixture", URL: "https://8.8.8.8/mcp", Transport: "STREAMABLE_HTTP", ApprovalPolicy: "auto_approve_all", RequestHeaders: map[string]string{}, RequestMeta: map[string]string{hostedMCPOwnershipKey: rec.HostedCreationIntentID}, ResponseTimeoutSecs: 10}
			raw, _ := json.Marshal(expected)
			vault, _ := security.NewVault(make([]byte, 32))
			rec.HostedCreationConfig, _ = vault.Encrypt(string(raw))
			_, _ = store.UpsertMCPServer(context.Background(), rec)
			provider := &hostedMCPProviderFixture{resources: map[string]json.RawMessage{"mcp_fixture": raw}, native: []string{"mcp_fixture"}, other: []string{}}
			if state == "drift" {
				expected.URL = "https://1.1.1.1/mcp"
				changed, _ := json.Marshal(expected)
				provider.resources["mcp_fixture"] = changed
			}
			if state == "skill_unavailable" {
				store.agentSkills["skill"] = &skills.Skill{ID: "skill", Enabled: true, AllowedTools: []string{"unshared"}}
			}
			upstream := httptest.NewServer(provider.handler(t))
			defer upstream.Close()
			old := elevenLabsAdminAPIBaseURL
			elevenLabsAdminAPIBaseURL = upstream.URL
			defer func() { elevenLabsAdminAPIBaseURL = old }()
			response := hostedAPIRequest(router, cookie, "POST", "/api/v1/elevenlabs/conversation-token", `{}`)
			want := 409
			calls := 0
			if state == "synced" {
				want = 200
				calls = 1
			}
			if response.Code != want || provider.tokenRequests != calls {
				t.Fatalf("state=%s status=%d tokens=%d body=%s", state, response.Code, provider.tokenRequests, response.Body.String())
			}
		})
	}
}

func TestHostedMCPDirectAllowlistEligibility(t *testing.T) {
	for _, tt := range []struct {
		tools   []mcp.ToolDef
		allowed []string
		ok      bool
	}{
		{[]mcp.ToolDef{{Name: "lookup"}, {Name: "blocked"}}, []string{"lookup"}, false},
		{[]mcp.ToolDef{{Name: "lookup"}, {Name: "blocked"}}, []string{"lookup", "blocked"}, true},
		{[]mcp.ToolDef{{Name: "lookup"}, {Name: "lookup"}}, []string{"lookup"}, false},
		{[]mcp.ToolDef{{Name: "lookup"}}, []string{"missing"}, false},
	} {
		rec := &MCPServerRecord{DiscoveredTools: tt.tools, AllowedTools: tt.allowed}
		if sameHostedToolNames(tt.tools, allowedHostedMCPTools(rec)) != tt.ok {
			t.Fatal("direct tool eligibility ignored the exact allowlist")
		}
	}
}

func TestHostedMCPModeChangeRevokesProxyCredential(t *testing.T) {
	t.Setenv("HOSTED_MCP_PROXY_BASE_URL", "https://8.8.8.8")
	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	body := `{"name":"fixture","endpointUrl":"https://8.8.8.8/mcp","enabled":true,"hostedMode":"proxy","allowedTools":[]}`
	response := hostedAPIRequest(router, cookie, "POST", "/api/v1/admin/mcp-servers", body)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	records, _ := store.ListMCPServers(context.Background())
	id := records[0].ID
	credential := string(records[0].HostedProxyCredential)
	for _, mode := range []string{"direct", "proxy"} {
		body := `{"name":"fixture","endpointUrl":"https://8.8.8.8/mcp","enabled":true,"hostedMode":"` + mode + `","directCredentialSharingConfirmed":true,"allowedTools":[]}`
		response = hostedAPIRequest(router, cookie, "PUT", "/api/v1/admin/mcp-servers/"+id, body)
		if response.Code != 200 {
			t.Fatal(response.Body.String())
		}
		record, _ := store.GetMCPServer(context.Background(), id)
		if mode == "direct" && len(record.HostedProxyCredential) != 0 {
			t.Fatal("proxy credential retained in direct mode")
		}
		if mode == "proxy" && string(record.HostedProxyCredential) == credential {
			t.Fatal("mode transition reactivated a revoked proxy credential")
		}
	}
}
func TestHostedMCPReconcileRecoveryAndCleanup(t *testing.T) {
	store, s := newMCPTestServer(t)
	rec := newProxyMCPRecord(t, s, "http://localhost:1")
	provider := &hostedMCPProviderFixture{resources: map[string]json.RawMessage{}, native: []string{}, other: []string{}, delayCreate: 100 * time.Millisecond}
	upstream := httptest.NewServer(provider.handler(t))
	defer upstream.Close()
	old := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = old }()
	initial, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := s.syncHostedMCP(initial, store.agentCfg); err == nil {
		t.Fatal("ambiguous create reported success")
	}
	persisted, _ := store.GetMCPServer(context.Background(), rec.ID)
	if !persisted.HostedCreationAttempted || persisted.HostedResourceID != "" || persisted.HostedSyncStatus != "recovery_pending" {
		t.Fatalf("lost creation intent: %+v", persisted)
	}
	// A fresh server instance uses only persisted state to recover.
	restarted := *s
	if err := restarted.syncHostedMCP(context.Background(), store.agentCfg); err != nil {
		t.Fatal(err)
	}
	persisted, _ = store.GetMCPServer(context.Background(), rec.ID)
	if persisted.HostedSyncStatus != "synced" || provider.creates != 1 {
		t.Fatalf("retry duplicated creation: state=%s creates=%d", persisted.HostedSyncStatus, provider.creates)
	}
	persisted.Name = "updated fixture"
	_, _ = store.UpsertMCPServer(context.Background(), persisted)
	if err := s.syncHostedMCP(context.Background(), store.agentCfg); err != nil {
		t.Fatal(err)
	}
	var updated hostedMCPProviderConfig
	if json.Unmarshal(provider.resources["mcp_fixture"], &updated) != nil || updated.Name != persisted.Name || provider.creates != 1 {
		t.Fatal("mapped resource update created a duplicate or was not verified")
	}
	provider.resources["dashboard"] = json.RawMessage(`{"name":"fixture","url":"https://example.com/mcp","request_meta":{}}`)
	provider.failDelete = true
	persisted.HostedMode = "disabled"
	persisted.HostedProxyCredential = nil
	_, _ = store.UpsertMCPServer(context.Background(), persisted)
	if err := s.syncHostedMCP(context.Background(), store.agentCfg); err == nil {
		t.Fatal("cleanup failure reported success")
	}
	persisted, _ = store.GetMCPServer(context.Background(), rec.ID)
	if persisted.HostedResourceID == "" || persisted.HostedSyncStatus != "revocation_pending" {
		t.Fatal("ownership lost before cleanup")
	}
	provider.failDelete = false
	if err := s.syncHostedMCP(context.Background(), store.agentCfg); err != nil {
		t.Fatal(err)
	}
	persisted, _ = store.GetMCPServer(context.Background(), rec.ID)
	if persisted.HostedResourceID != "" || persisted.HostedCreationAttempted || provider.resources["dashboard"] == nil {
		t.Fatal("cleanup affected dashboard ownership")
	}
}

func TestHostedMCPCreationRecoveryRejectsUnverifiedMatches(t *testing.T) {
	for _, mode := range []string{"absent", "duplicate", "mismatch"} {
		t.Run(mode, func(t *testing.T) {
			store, s := newMCPTestServer(t)
			rec := newProxyMCPRecord(t, s, "http://localhost:1")
			rec.HostedCreationIntentID = uuid.NewString()
			rec.HostedCreationAttempted = true
			desired, err := s.desiredHostedMCP(context.Background(), rec)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(desired)
			rec.HostedCreationConfig, _ = s.vault.Encrypt(string(raw))
			_, _ = store.UpsertMCPServer(context.Background(), rec)
			provider := &hostedMCPProviderFixture{resources: map[string]json.RawMessage{}, native: []string{}, other: []string{}}
			switch mode {
			case "duplicate":
				provider.resources["one"] = raw
				provider.resources["two"] = raw
			case "mismatch":
				desired.URL = "https://1.1.1.1"
				raw, _ = json.Marshal(desired)
				provider.resources["one"] = raw
			}
			upstream := httptest.NewServer(provider.handler(t))
			defer upstream.Close()
			old := elevenLabsAdminAPIBaseURL
			elevenLabsAdminAPIBaseURL = upstream.URL
			defer func() { elevenLabsAdminAPIBaseURL = old }()
			if err := s.syncHostedMCP(context.Background(), store.agentCfg); err == nil || provider.creates != 0 {
				t.Fatal("unverified recovery created or adopted a resource")
			}
		})
	}
}

func TestHostedMCPConcurrentCreation(t *testing.T) {
	store, s := newMCPTestServer(t)
	newProxyMCPRecord(t, s, "http://localhost:1")
	provider := &hostedMCPProviderFixture{resources: map[string]json.RawMessage{}, native: []string{}, other: []string{}}
	upstream := httptest.NewServer(provider.handler(t))
	defer upstream.Close()
	old := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = old }()
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.syncHostedMCP(context.Background(), store.agentCfg); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if provider.creates != 1 {
		t.Fatalf("created %d resources", provider.creates)
	}
}

func TestHostedMCPAdminDefaultsAndSecrets(t *testing.T) {
	t.Setenv("HOSTED_MCP_PROXY_BASE_URL", "https://8.8.8.8")
	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	response := hostedAPIRequest(router, cookie, "POST", "/api/v1/admin/mcp-servers", `{"name":"fixture","endpointUrl":"https://8.8.8.8/mcp","enabled":true,"allowedTools":[],"authHeader":"Bearer private-secret"}`)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	records, _ := store.ListMCPServers(context.Background())
	if len(records) != 1 || records[0].HostedMode != "disabled" || records[0].HostedSyncStatus != "disabled" {
		t.Fatal("new server was automatically shared")
	}
	id := records[0].ID
	response = hostedAPIRequest(router, cookie, "PUT", "/api/v1/admin/mcp-servers/"+id, `{"name":"fixture","endpointUrl":"https://8.8.8.8/mcp","enabled":true,"allowedTools":[],"hostedMode":"proxy"}`)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	saved, _ := store.GetMCPServer(context.Background(), id)
	if string(saved.AuthHeaderCiphertext) != string(records[0].AuthHeaderCiphertext) || len(saved.HostedProxyCredential) == 0 {
		t.Fatal("credentials were not preserved or encrypted")
	}
	for _, body := range []string{
		`{"name":"fixture","endpointUrl":"https://8.8.8.8/mcp","hostedMode":"direct"}`,
		`{"name":"fixture","endpointUrl":"https://8.8.8.8/mcp","hostedMode":"future"}`,
		`{"name":"fixture","endpointUrl":"https://8.8.8.8/mcp","hostedResourceId":"injected"}`,
	} {
		if r := hostedAPIRequest(router, cookie, "PUT", "/api/v1/admin/mcp-servers/"+id, body); r.Code != 400 {
			t.Fatalf("invalid input accepted: %d %s", r.Code, r.Body.String())
		}
	}
	response = hostedAPIRequest(router, cookie, "GET", "/api/v1/admin/mcp-servers", "")
	for _, secret := range []string{"private-secret", "hostedProxyCredential", "hostedCreationConfig", "hostedCreationIntentId"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("response disclosed %s", secret)
		}
	}
	response = hostedAPIRequest(router, cookie, "DELETE", "/api/v1/admin/mcp-servers/"+id, "")
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
}

func TestHostedSyncPolicyCombinationsRetainMCPAndAdmission(t *testing.T) {
	for _, ragEnabled := range []bool{false, true} {
		for _, skillsEnabled := range []bool{false, true} {
			name := fmt.Sprintf("rag=%t/skills=%t", ragEnabled, skillsEnabled)
			t.Run(name, func(t *testing.T) {
				store, s := newMCPTestServer(t)
				store.agentCfg.ElevenLabsRAGSyncEnabled = ragEnabled
				store.agentCfg.ElevenLabsSkillsSyncEnabled = skillsEnabled
				rec := newProxyMCPRecord(t, s, "http://localhost:1")
				rec.HostedSyncStatus = "pending"
				_, _ = store.UpsertMCPServer(context.Background(), rec)
				allowed := "unavailable"
				if skillsEnabled {
					allowed = "lookup"
				}
				store.agentSkills["skill"] = &skills.Skill{ID: "skill", Name: "policy", Enabled: true, Instructions: "Policy instructions.", AllowedTools: []string{allowed}}
				provider := &hostedMCPProviderFixture{resources: map[string]json.RawMessage{}, native: []string{}, other: []string{}}
				upstream := httptest.NewServer(provider.handler(t))
				defer upstream.Close()
				old := elevenLabsAdminAPIBaseURL
				elevenLabsAdminAPIBaseURL = upstream.URL
				defer func() { elevenLabsAdminAPIBaseURL = old }()
				response := httptest.NewRecorder()
				s.syncHostedAgent(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`)))
				if response.Code != http.StatusOK || provider.creates != 1 {
					t.Fatalf("policy combination blocked MCP sync: %d %s creates=%d", response.Code, response.Body.String(), provider.creates)
				}
				if strings.Contains(provider.prompt, "Policy instructions.") != skillsEnabled {
					t.Fatal("prompt did not respect independent skill policy")
				}
				requestToken := func() *httptest.ResponseRecorder {
					req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
					req.Header.Set("Origin", "https://aegis.local")
					result := httptest.NewRecorder()
					s.hostedConversationToken(result, req)
					return result
				}
				response = requestToken()
				if response.Code != http.StatusOK || provider.tokenRequests != 1 {
					t.Fatalf("policy combination blocked token: %d %s", response.Code, response.Body.String())
				}
				saved, _ := store.GetMCPServer(context.Background(), rec.ID)
				saved.HostedSyncStatus = "revocation_pending"
				_, _ = store.UpsertMCPServer(context.Background(), saved)
				response = requestToken()
				if response.Code != http.StatusConflict || provider.tokenRequests != 1 {
					t.Fatal("disabled augmentation bypassed MCP revocation admission")
				}
			})
		}
	}
}

func TestHostedMCPPublicHTTPS(t *testing.T) {
	for _, raw := range []string{"", "http://8.8.8.8", "https://127.0.0.1", "https://localhost", "https://user:secret@8.8.8.8", "https://8.8.8.8/path", "https://8.8.8.8?redirect=1"} {
		if _, err := hostedPublicHTTPS(raw, true); err == nil {
			t.Fatalf("accepted invalid public origin %s", raw)
		}
	}
}

func TestHostedMCPDisableDoesNotRequireProxyDeployment(t *testing.T) {
	t.Setenv("HOSTED_MCP_PROXY_BASE_URL", "")
	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	_, s := newMCPTestServer(t)
	rec := newProxyMCPRecord(t, s, "http://localhost:1")
	_, _ = store.UpsertMCPServer(context.Background(), rec)
	response := hostedAPIRequest(router, cookie, "PUT", "/api/v1/admin/mcp-servers/"+rec.ID,
		`{"name":"fixture","endpointUrl":"http://localhost:1","enabled":false,"allowedTools":["lookup"]}`)
	if response.Code != http.StatusOK {
		t.Fatalf("missing proxy deployment prevented disabling: %d %s", response.Code, response.Body.String())
	}
	saved, err := store.GetMCPServer(context.Background(), rec.ID)
	if err != nil || saved.Enabled || len(saved.HostedProxyCredential) != 0 {
		t.Fatal("disable failed to revoke the local proxy credential")
	}
}

func TestHostedToolAdmissionAndSkillReferences(t *testing.T) {
	store, s := newMCPTestServer(t)
	rec := newProxyMCPRecord(t, s, "http://localhost:1")
	rec.HostedResourceID = "mcp_fixture"
	_, _ = store.UpsertMCPServer(context.Background(), rec)
	config := &hostedAgent{AgentID: "agent_fixture", ValidatedMCP: map[string][]mcp.ToolDef{"mcp_fixture": {{Name: "lookup", InputSchema: map[string]any{"type": "object"}}}}}
	snapshot, _ := s.vault.Decrypt(rec.HostedCreationConfig)
	config.ValidatedMCPConfigs = map[string]json.RawMessage{"mcp_fixture": json.RawMessage(snapshot)}
	store.agentSkills["skill"] = &skills.Skill{ID: "skill", Enabled: true, AllowedTools: []string{"lookup"}}
	if err := s.validateHostedToolAdmission(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	if err := s.validateHostedSkills(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"pending", "failed", "revocation_pending", "recovery_pending"} {
		rec.HostedSyncStatus = state
		_, _ = store.UpsertMCPServer(context.Background(), rec)
		if err := s.validateHostedToolAdmission(context.Background(), config); err == nil {
			t.Fatalf("admitted %s integration", state)
		}
	}
	store.agentSkills["skill"].AllowedTools = []string{"blocked"}
	if err := s.validateHostedSkills(context.Background(), config); err == nil {
		t.Fatal("skill introduced an unavailable tool")
	}
}

type failMappingStore struct {
	AdminStore
	fail bool
}

func (s *failMappingStore) UpsertMCPServer(ctx context.Context, rec *MCPServerRecord) (*MCPServerRecord, error) {
	if s.fail && rec.HostedResourceID != "" {
		s.fail = false
		return nil, errors.New("fixture mapping persistence failure")
	}
	return s.AdminStore.UpsertMCPServer(ctx, rec)
}

func TestHostedMCPCreationSurvivesMappingPersistenceFailure(t *testing.T) {
	store, s := newMCPTestServer(t)
	rec := newProxyMCPRecord(t, s, "http://localhost:1")
	provider := &hostedMCPProviderFixture{resources: map[string]json.RawMessage{}, native: []string{}, other: []string{}}
	upstream := httptest.NewServer(provider.handler(t))
	defer upstream.Close()
	old := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = old }()
	s.adminStore = &failMappingStore{AdminStore: store, fail: true}
	if err := s.reconcileHostedMCPServer(context.Background(), store.agentCfg, rec); err == nil {
		t.Fatal("mapping failure reported success")
	}
	s.adminStore = store
	if err := s.syncHostedMCP(context.Background(), store.agentCfg); err != nil {
		t.Fatal(err)
	}
	if provider.creates != 1 {
		t.Fatal("mapping failure duplicated creation")
	}
}

func TestHostedMCPProxyRateLimit(t *testing.T) {
	_, s := newMCPTestServer(t)
	rec := newProxyMCPRecord(t, s, "http://localhost:1")
	for i := 0; i < 31; i++ {
		request := httptest.NewRequest("POST", "/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		request.SetPathValue("id", rec.ID)
		request.Header.Set("Authorization", "Bearer fixture-proxy-credential")
		response := httptest.NewRecorder()
		s.HandleHostedMCPProxy(response, request)
		if i == 30 && response.Code != 429 {
			t.Fatalf("expected rate limit, got %d", response.Code)
		}
	}
}

func TestHostedMCPProxyTimeout(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer upstream.Close()
	_, s := newMCPTestServer(t)
	rec := newProxyMCPRecord(t, s, upstream.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	request := httptest.NewRequest("POST", "/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"lookup"}}`)).WithContext(ctx)
	request.SetPathValue("id", rec.ID)
	request.Header.Set("Authorization", "Bearer fixture-proxy-credential")
	response := httptest.NewRecorder()
	s.HandleHostedMCPProxy(response, request)
	if !strings.Contains(response.Body.String(), "Upstream MCP tool call failed") {
		t.Fatalf("timeout not surfaced: %s", response.Body.String())
	}
}
