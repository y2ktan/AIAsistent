package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"aegis-voice/server/internal/agent"
	"aegis-voice/server/internal/auth"
	"aegis-voice/server/internal/rag"
	"aegis-voice/server/internal/security"
	"aegis-voice/server/internal/skills"
)

const hostedFixture = `{"agent_id":"agent_fixture","version_id":"version_fixture","platform_settings":{"auth":{"enable_auth":true}},"conversation_config":{"agent":{"prompt":{"tool_ids":[],"tools":[],"mcp_server_ids":[],"native_mcp_server_ids":[],"built_in_tools":{"end_call":null}}}}}`
const hostedFixtureKey = "fixture-elevenlabs-secret"

func TestInspectHostedAgent(t *testing.T) {
	tests := []struct {
		name     string
		response string
		tool     string
		status   int
		ok       bool
	}{
		{name: "private rag only", response: hostedFixture, ok: true},
		{name: "empty workflow", response: strings.TrimSuffix(hostedFixture, "}") + `,"workflow":{"nodes":{ },"edges":{},"prevent_subagent_loops":false}}`, ok: true},
		{name: "public", response: strings.Replace(hostedFixture, `"enable_auth":true`, `"enable_auth":false`, 1)},
		{name: "missing auth", response: strings.Replace(hostedFixture, `"auth":{"enable_auth":true}`, `"auth":{}`, 1)},
		{name: "missing version", response: strings.Replace(hostedFixture, `"version_id":"version_fixture"`, `"version_id":null`, 1)},
		{name: "missing prompt", response: `{"agent_id":"agent_fixture","version_id":"version_fixture","platform_settings":{"auth":{"enable_auth":true}}}`},
		{name: "inline webhook", response: strings.Replace(hostedFixture, `"tools":[]`, `"tools":[{"type":"webhook"}]`, 1)},
		{name: "inline system retrieval", response: strings.Replace(hostedFixture, `"tools":[]`, `"tools":[{"type":"system","params":{"system_tool_type":"knowledge_base_rag"}}]`, 1), ok: true},
		{name: "system transfer", response: strings.Replace(hostedFixture, `"end_call":null`, `"transfer_to_agent":{"type":"system","params":{"system_tool_type":"transfer_to_agent"}}`, 1)},
		{name: "malformed system", response: strings.Replace(hostedFixture, `"end_call":null`, `"end_call":{}`, 1)},
		{name: "native mcp", response: strings.Replace(hostedFixture, `"native_mcp_server_ids":[]`, `"native_mcp_server_ids":["mcp_external"]`, 1)},
		{name: "referenced system", response: strings.Replace(hostedFixture, `"tool_ids":[]`, `"tool_ids":["tool_fixture"]`, 1), tool: `{"id":"tool_fixture","tool_config":{"type":"system","params":{"system_tool_type":"skip_turn"}}}`, ok: true},
		{name: "referenced client", response: strings.Replace(hostedFixture, `"tool_ids":[]`, `"tool_ids":["tool_fixture"]`, 1), tool: `{"id":"tool_fixture","tool_config":{"type":"client"}}`},
		{name: "reference mismatch", response: strings.Replace(hostedFixture, `"tool_ids":[]`, `"tool_ids":["tool_fixture"]`, 1), tool: `{"id":"another","tool_config":{"type":"system","params":{"system_tool_type":"skip_turn"}}}`},
		{name: "workflow", response: strings.TrimSuffix(hostedFixture, "}") + `,"workflow":{"nodes":{"start":{}},"edges":{}}}`},
		{name: "unknown workflow", response: strings.TrimSuffix(hostedFixture, "}") + `,"workflow":{"future_nodes":[]}}`},
		{name: "subagents", response: strings.Replace(hostedFixture, `"agent":{"prompt"`, `"agent":{"subagent_config":{"agents":[{}]},"prompt"`, 1)},
		{name: "tool overrides", response: strings.Replace(hostedFixture, `"enable_auth":true}}`, `"enable_auth":true},"overrides":{"conversation_config_override":{"agent":{"prompt":{"tool_ids":true}}}}}`, 1)},
		{name: "strict tool overrides", response: strings.Replace(hostedFixture, `"enable_auth":true}}`, `"enable_auth":true},"overrides":{"conversation_config_override":{"agent":{"prompt":{"tool_ids_strict":true}}}}}`, 1)},
		{name: "dynamic webhook", response: strings.Replace(hostedFixture, `"enable_auth":true}}`, `"enable_auth":true},"overrides":{"enable_conversation_initiation_client_data_from_webhook":true}}`, 1)},
		{name: "malformed", response: `{`},
		{name: "oversized", response: strings.Repeat(" ", (2<<20)+1)},
		{name: "denied", response: `{"secret":"never disclose"}`, status: http.StatusUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("xi-api-key") != hostedFixtureKey {
					t.Error("missing server-held credential")
				}
				if test.status != 0 {
					w.WriteHeader(test.status)
				}
				body := test.response
				if strings.HasPrefix(r.URL.Path, "/v1/convai/tools/") {
					body = test.tool
				}
				if _, err := w.Write([]byte(body)); err != nil {
					t.Error(err)
				}
			}))
			defer upstream.Close()
			previous := elevenLabsAdminAPIBaseURL
			elevenLabsAdminAPIBaseURL = upstream.URL
			defer func() { elevenLabsAdminAPIBaseURL = previous }()
			version, err := inspectHostedAgent(context.Background(), newHostedClient(true), hostedFixtureKey, "agent_fixture")
			if (err == nil) != test.ok {
				t.Fatalf("version=%q err=%v; expected success=%v", version, err, test.ok)
			}
			if err != nil && strings.Contains(err.Error(), "never disclose") {
				t.Fatal("upstream error body disclosed")
			}
		})
	}
}

func newHostedAPITest(t *testing.T, role string) (*mockAllStore, http.Handler, *http.Cookie) {
	t.Helper()
	store := newMockAllStore()
	vault, err := security.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := vault.Encrypt(hostedFixtureKey)
	if err != nil {
		t.Fatal(err)
	}
	store.agentCfg.ElevenLabsAPIKeyCiphertext = ciphertext
	store.agentCfg.ElevenLabsAPIKeyLast5 = maskLast5(hostedFixtureKey)
	store.agentCfg.ElevenLabsAgentID = "agent_fixture"
	store.agentCfg.LLMBaseURL = "https://llm.example/v1"
	store.agentCfg.LLMModel = "retained-model"
	store.agentCfg.SpeechProvider = "elevenlabs"
	store.voiceLangs = &agent.VoiceLanguageSettings{DefaultCode: "en", Languages: []agent.VoiceLanguage{{Code: "en", Name: "English", Voice: "local-voice"}}}
	store.users["fixture-user"] = &auth.User{ID: "fixture-user", Role: role, IsActive: true}
	hash := sha256.Sum256([]byte("fixture-session"))
	store.sessions[string(hash[:])] = &auth.Session{ID: "fixture-session-id", UserID: "fixture-user", TokenHash: hash[:], CSRFToken: "fixture-csrf", LastSeenAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	service := auth.NewService(store, auth.Config{WebOrigin: "https://aegis.local"}, nil)
	router := NewServer(service, store, store, rag.NewService(store.ragStore, 0), nil, nil, vault, true, nil).Router()
	return store, router, &http.Cookie{Name: auth.SessionCookieName, Value: "fixture-session"}
}

func TestHostedSyncOptionsDefaultEnabled(t *testing.T) {
	config := newMockAllStore().agentCfg
	if !config.ElevenLabsRAGSyncEnabled || !config.ElevenLabsSkillsSyncEnabled {
		t.Fatal("new hosted synchronization policies must default to enabled")
	}
}

func TestHostedSyncOptionsStrictAdminPersistence(t *testing.T) {
	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	response := hostedAPIRequest(router, cookie, http.MethodPut, "/api/v1/admin/agent-config", `{"mode":"realtime","elevenLabsRagSyncEnabled":false,"elevenLabsAgentSkillsSyncEnabled":false}`)
	if response.Code != http.StatusOK {
		t.Fatalf("explicit disabled policies failed: %d %s", response.Code, response.Body.String())
	}
	if store.agentCfg.ElevenLabsRAGSyncEnabled || store.agentCfg.ElevenLabsSkillsSyncEnabled {
		t.Fatal("explicit false hosted policies did not persist")
	}

	response = hostedAPIRequest(router, cookie, http.MethodPut, "/api/v1/admin/agent-config", `{"mode":"realtime"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("omitted policies failed: %d %s", response.Code, response.Body.String())
	}
	if store.agentCfg.ElevenLabsRAGSyncEnabled || store.agentCfg.ElevenLabsSkillsSyncEnabled {
		t.Fatal("omitted hosted policies did not preserve stored false values")
	}
	if !strings.Contains(response.Body.String(), `"elevenLabsRagSyncEnabled":false`) ||
		!strings.Contains(response.Body.String(), `"elevenLabsAgentSkillsSyncEnabled":false`) {
		t.Fatalf("response omitted hosted policy values: %s", response.Body.String())
	}
}

func TestHostedSyncOptionsSurviveModeAndHostedIdentityUpdates(t *testing.T) {
	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	store.agentCfg.ElevenLabsRAGSyncEnabled = false
	store.agentCfg.ElevenLabsSkillsSyncEnabled = false

	response := hostedAPIRequest(router, cookie, http.MethodPut, "/api/v1/admin/agent-config", `{"mode":"cascaded","speechProvider":"local"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("mode switch failed: %d %s", response.Code, response.Body.String())
	}
	if store.agentCfg.Mode != "cascaded" || store.agentCfg.ElevenLabsRAGSyncEnabled || store.agentCfg.ElevenLabsSkillsSyncEnabled {
		t.Fatal("mode switch did not preserve hosted synchronization policies")
	}

	response = hostedAPIRequest(router, cookie, http.MethodPut, "/api/v1/admin/agent-config", `{"mode":"realtime","elevenLabsAgentId":"agent_replacement","elevenLabsApiKey":"replacement-key"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("hosted identity update failed: %d %s", response.Code, response.Body.String())
	}
	if store.agentCfg.ElevenLabsAgentID != "agent_replacement" ||
		store.agentCfg.ElevenLabsRAGSyncEnabled || store.agentCfg.ElevenLabsSkillsSyncEnabled {
		t.Fatal("hosted identity update lost synchronization policies")
	}
}

func hostedAPIRequest(router http.Handler, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if cookie != nil {
		request.AddCookie(cookie)
	}
	request.Header.Set("Origin", "https://aegis.local")
	request.Header.Set("X-CSRF-Token", "fixture-csrf")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func hostedPromptFromPayload(t *testing.T, payload map[string]any) string {
	t.Helper()
	conversationConfig, ok := payload["conversation_config"].(map[string]any)
	if !ok {
		t.Fatalf("hosted patch omitted conversation_config: %#v", payload)
	}
	agentConfig, ok := conversationConfig["agent"].(map[string]any)
	if !ok {
		t.Fatalf("hosted patch omitted agent prompt: %#v", conversationConfig)
	}
	promptConfig, ok := agentConfig["prompt"].(map[string]any)
	if !ok {
		t.Fatalf("hosted patch omitted prompt settings: %#v", agentConfig)
	}
	prompt, ok := promptConfig["prompt"].(string)
	if !ok {
		t.Fatalf("hosted patch prompt is not a string: %#v", promptConfig)
	}
	return prompt
}

func TestHostedConfigPreservesInactiveSettings(t *testing.T) {
	var patchCalls atomic.Int32
	var patchPayloads []map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/convai/tools/") {
			_, _ = w.Write([]byte(hostedWebhookFixture))
			return
		}
		if r.Method == http.MethodPatch {
			patchCalls.Add(1)
			if r.Header.Get("xi-api-key") != hostedFixtureKey || r.Header.Get("Content-Type") != "application/json" {
				t.Error("hosted update omitted provider authorization or JSON content type")
			}
			var patchPayload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&patchPayload); err != nil {
				t.Errorf("decode hosted update: %v", err)
			} else {
				patchPayloads = append(patchPayloads, patchPayload)
			}
		}
		if _, err := w.Write([]byte(strings.TrimSuffix(hostedFixture, "}") + `,"workflow":` + hostedWorkflowFixture + `}`)); err != nil {
			t.Error(err)
		}
	}))
	defer upstream.Close()
	previous := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = previous }()
	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	beforeKey := append([]byte(nil), store.agentCfg.ElevenLabsAPIKeyCiphertext...)
	response := hostedAPIRequest(router, cookie, http.MethodPut, "/api/v1/admin/agent-config", `{"mode":"elevenlabs_agent","elevenLabsAgentId":"agent_fixture","systemInstructions":"Answer with the synchronized application policy."}`)
	if response.Code != http.StatusOK {
		t.Fatalf("activation failed: %d %s", response.Code, response.Body.String())
	}
	if store.agentCfg.Mode != "elevenlabs_agent" || store.agentCfg.LLMBaseURL != "https://llm.example/v1" || store.agentCfg.LLMModel != "retained-model" || !bytes.Equal(beforeKey, store.agentCfg.ElevenLabsAPIKeyCiphertext) {
		t.Fatal("hosted activation lost inactive configuration")
	}
	if store.agentCfg.SystemInstructions != "Answer with the synchronized application policy." {
		t.Fatal("hosted activation did not save updated system instructions")
	}
	if strings.Contains(response.Body.String(), hostedFixtureKey) {
		t.Fatal("key disclosed")
	}
	if patchCalls.Load() != 2 || len(patchPayloads) != 2 || patchPayloads[0]["name"] != "Aegis" {
		t.Fatalf("hosted save did not patch prompt and knowledge settings: calls=%d payloads=%#v", patchCalls.Load(), patchPayloads)
	}
	if prompt := hostedPromptFromPayload(t, patchPayloads[0]); !strings.HasPrefix(prompt, agent.SecurityBoundaryPreamble) ||
		!strings.Contains(prompt, "Answer with the synchronized application policy.") {
		t.Fatalf("hosted patch omitted security boundary or system instructions: %q", prompt)
	}
	conversationConfig := patchPayloads[0]["conversation_config"].(map[string]any)
	agentConfig := conversationConfig["agent"].(map[string]any)
	promptConfig := agentConfig["prompt"].(map[string]any)
	if len(patchPayloads[0]) != 2 || len(conversationConfig) != 1 || len(agentConfig) != 1 || len(promptConfig) != 1 {
		t.Fatalf("hosted prompt patch included dashboard-owned settings or override flags: %#v", patchPayloads[0])
	}
	docConversation := patchPayloads[1]["conversation_config"].(map[string]any)
	docAgent := docConversation["agent"].(map[string]any)
	docPrompt := docAgent["prompt"].(map[string]any)
	ragConfig := docPrompt["rag"].(map[string]any)
	if len(patchPayloads[1]) != 1 || len(docConversation) != 1 || len(docAgent) != 1 || len(docPrompt) != 2 {
		t.Fatalf("knowledge patch overwrote workflow or tool configuration: %#v", patchPayloads[1])
	}
	if enabled, ok := ragConfig["enabled"].(bool); !ok || !enabled {
		t.Fatalf("hosted document sync did not enable RAG: %#v", patchPayloads[1])
	}
	for _, body := range []string{`{"mode":"invalid"}`, `{"mode":"cascaded","speechProvider":"elevenlabs"}`} {
		response = hostedAPIRequest(router, cookie, http.MethodPut, "/api/v1/admin/agent-config", body)
		if response.Code != http.StatusBadRequest || store.agentCfg.Mode != "elevenlabs_agent" {
			t.Fatalf("invalid switch changed configuration: %d %s", response.Code, response.Body.String())
		}
	}
	response = hostedAPIRequest(router, cookie, http.MethodPut, "/api/v1/admin/agent-config", `{"mode":"cascaded","speechProvider":"local"}`)
	if response.Code != http.StatusOK || store.agentCfg.LLMModel != "retained-model" || store.agentCfg.ElevenLabsAgentID != "agent_fixture" {
		t.Fatalf("return to local lost configuration: %d %s", response.Code, response.Body.String())
	}
}

func TestCompileHostedPromptIncludesOnlyEnabledSkillsDeterministically(t *testing.T) {
	config := &AgentConfigRecord{AssistantName: "Aegis", SystemInstructions: "Follow the application policy.", ElevenLabsSkillsSyncEnabled: true}
	skillRecords := []skills.Skill{
		{
			ID: "z", Name: "Zulu", Description: "Second skill", Instructions: "Zulu instructions.",
			ReferenceFiles: map[string]string{"z.md": "Zulu reference.", "a.md": "First reference."}, Enabled: true,
		},
		{ID: "disabled", Name: "Disabled", Instructions: "Must not appear.", Enabled: false},
		{ID: "a", Name: "Alpha", Description: "First skill", Instructions: "Alpha instructions.", Enabled: true},
	}

	prompt, err := compileHostedPrompt(config, skillRecords)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(prompt, agent.SecurityBoundaryPreamble) {
		t.Fatal("security boundary was not first in the hosted prompt")
	}
	for _, expected := range []string{
		"Aegis",
		"Follow the application policy.",
		"Alpha instructions.",
		"Zulu instructions.",
		"First reference.",
		"Zulu reference.",
	} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("hosted prompt omitted %q", expected)
		}
	}
	if strings.Contains(prompt, "Must not appear.") {
		t.Fatal("disabled skill instructions were included")
	}
	alphaIndex := strings.Index(prompt, "Name: \"Alpha\"")
	zuluIndex := strings.Index(prompt, "Name: \"Zulu\"")
	firstReferenceIndex := strings.Index(prompt, "SKILL REFERENCE \"a.md\"")
	secondReferenceIndex := strings.Index(prompt, "SKILL REFERENCE \"z.md\"")
	if alphaIndex < 0 || zuluIndex < 0 || firstReferenceIndex < 0 || secondReferenceIndex < 0 ||
		alphaIndex > zuluIndex || firstReferenceIndex > secondReferenceIndex {
		t.Fatal("hosted skills or references were not ordered deterministically")
	}
}

func TestCompileHostedPromptRejectsOverBudgetSkillBundle(t *testing.T) {
	_, err := compileHostedPrompt(&AgentConfigRecord{AssistantName: "Aegis", ElevenLabsSkillsSyncEnabled: true}, []skills.Skill{
		{Name: "large", Enabled: true, Instructions: strings.Repeat("x", maxHostedPromptBytes)},
	})
	if err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("expected prompt budget error, got %v", err)
	}
}

func TestCompileHostedPromptDisabledSkillsRetainsBase(t *testing.T) {
	prompt, err := compileHostedPrompt(&AgentConfigRecord{
		AssistantName: "Support", SystemInstructions: "Follow the application policy.",
	}, []skills.Skill{
		{Name: "invalid", Enabled: true, Instructions: "\xff" + strings.Repeat("x", maxHostedPromptBytes)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(prompt, agent.SecurityBoundaryPreamble) ||
		!strings.Contains(prompt, `Assistant name: "Support"`) ||
		!strings.Contains(prompt, "Follow the application policy.") ||
		strings.Contains(prompt, "DECLARATIVE AGENT SKILL") {
		t.Fatalf("disabled skills did not produce a base-only prompt: %q", prompt)
	}
}

func TestHostedSkillMutationsSynchronizePrompt(t *testing.T) {
	var patches []map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode hosted patch: %v", err)
			} else {
				patches = append(patches, payload)
			}
		}
		if _, err := w.Write([]byte(hostedFixture)); err != nil {
			t.Error(err)
		}
	}))
	defer upstream.Close()
	previous := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = previous }()

	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	store.agentCfg.Mode = "elevenlabs_agent"
	store.agentCfg.SystemInstructions = "Hosted policy."
	requestWithAuth := func(req *http.Request) *httptest.ResponseRecorder {
		req.AddCookie(cookie)
		req.Header.Set("Origin", "https://aegis.local")
		req.Header.Set("X-CSRF-Token", "fixture-csrf")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	promptFromPatch := func(index int) string {
		t.Helper()
		return hostedPromptFromPayload(t, patches[index])
	}

	upload := createAgentSkillUploadRequest(t, "security.zip", createAgentSkillZip(t, map[string]string{
		"SKILL.md":             "---\nname: policy-helper\ndescription: Help with policy questions.\n---\n\nUse the synchronized policy skill.",
		"references/policy.md": "Policy reference content.",
	}))
	if rec := requestWithAuth(upload); rec.Code != http.StatusCreated {
		t.Fatalf("hosted skill upload failed: %d %s", rec.Code, rec.Body.String())
	}
	var uploaded struct {
		Skill adminAgentSkillResponse `json:"skill"`
	}
	uploadResponse := requestWithAuth(createAgentSkillUploadRequest(t, "temporary.md", []byte("---\nname: temporary\ndescription: temp\n---\n\nTemporary.")))
	if uploadResponse.Code != http.StatusCreated {
		t.Fatalf("temporary skill upload failed: %d %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	if err := json.Unmarshal(uploadResponse.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	if rec := hostedAPIRequest(router, cookie, http.MethodDelete, "/api/v1/admin/skills/"+uploaded.Skill.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("temporary hosted skill delete failed: %d %s", rec.Code, rec.Body.String())
	}

	if len(patches) != 3 || !strings.Contains(promptFromPatch(0), "Use the synchronized policy skill.") ||
		!strings.Contains(promptFromPatch(0), "Policy reference content.") {
		t.Fatalf("skill upload did not synchronize instructions and references: %#v", patches)
	}
	if !strings.Contains(promptFromPatch(1), "Temporary.") || strings.Contains(promptFromPatch(2), "Temporary.") {
		t.Fatalf("skill delete did not remove its prompt content: %#v", patches)
	}
	var policySkillID string
	for id, skill := range store.agentSkills {
		if skill.Name == "policy-helper" {
			policySkillID = id
		}
	}
	if policySkillID == "" {
		t.Fatal("uploaded skill was not persisted")
	}
	if rec := hostedAPIRequest(router, cookie, http.MethodPatch, "/api/v1/admin/skills/"+policySkillID, `{"enabled":false}`); rec.Code != http.StatusOK {
		t.Fatalf("hosted skill disable failed: %d %s", rec.Code, rec.Body.String())
	}
	if len(patches) != 4 || strings.Contains(promptFromPatch(3), "Use the synchronized policy skill.") ||
		strings.Contains(promptFromPatch(3), "Policy reference content.") {
		t.Fatalf("skill toggle did not remove disabled content: %#v", patches)
	}
}

func TestHostedPromptPatchFailureDoesNotSaveConfig(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"detail":"private provider diagnostic"}`))
			return
		}
		_, _ = w.Write([]byte(hostedFixture))
	}))
	defer upstream.Close()
	previous := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = previous }()

	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	response := hostedAPIRequest(
		router,
		cookie,
		http.MethodPut,
		"/api/v1/admin/agent-config",
		`{"mode":"elevenlabs_agent","elevenLabsAgentId":"agent_fixture"}`,
	)
	if response.Code != http.StatusBadGateway || store.agentCfg.Mode != "cascaded" {
		t.Fatalf("failed hosted synchronization changed saved config: %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "private provider diagnostic") {
		t.Fatal("provider response body was disclosed")
	}
}

func TestHostedDisabledSkillMutationsStayLocal(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()
	previous := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = previous }()

	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	store.agentCfg.Mode = "elevenlabs_agent"
	store.agentCfg.ElevenLabsSkillsSyncEnabled = false
	req := createAgentSkillUploadRequest(t, "local.md", []byte("---\nname: local\ndescription: Local skill\n---\n\nLocal instructions."))
	req.AddCookie(cookie)
	req.Header.Set("Origin", "https://aegis.local")
	req.Header.Set("X-CSRF-Token", "fixture-csrf")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusCreated {
		t.Fatalf("local upload failed: %d %s", response.Code, response.Body.String())
	}
	var uploaded struct {
		Skill adminAgentSkillResponse `json:"skill"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []string{"false", "true"} {
		response = hostedAPIRequest(router, cookie, http.MethodPatch, "/api/v1/admin/skills/"+uploaded.Skill.ID, `{"enabled":`+enabled+`}`)
		if response.Code != http.StatusOK || store.agentSkills[uploaded.Skill.ID].Enabled != (enabled == "true") {
			t.Fatalf("local toggle failed: %d %s", response.Code, response.Body.String())
		}
	}
	response = hostedAPIRequest(router, cookie, http.MethodDelete, "/api/v1/admin/skills/"+uploaded.Skill.ID, "")
	if response.Code != http.StatusOK || len(store.agentSkills) != 0 || calls.Load() != 0 {
		t.Fatalf("disabled skills contacted provider or failed local deletion: %d calls=%d %s", response.Code, calls.Load(), response.Body.String())
	}
}

func TestHostedManualSyncWithSkillsDisabled(t *testing.T) {
	var prompt string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if _, named := payload["name"]; named {
				prompt = hostedPromptFromPayload(t, payload)
			}
		}
		encoded, _ := json.Marshal(prompt)
		_, _ = w.Write([]byte(strings.Replace(hostedFixture, `"prompt":{"tool_ids"`, `"prompt":{"prompt":`+string(encoded)+`,"tool_ids"`, 1)))
	}))
	defer upstream.Close()
	previous := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = previous }()
	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	store.agentCfg.Mode = "elevenlabs_agent"
	store.agentCfg.ElevenLabsSkillsSyncEnabled = false
	store.agentSkills["unavailable"] = &skills.Skill{
		ID: "unavailable", Name: "unavailable", Enabled: true,
		Instructions: strings.Repeat("x", maxHostedPromptBytes), AllowedTools: []string{"unavailable"},
	}
	response := hostedAPIRequest(router, cookie, http.MethodPost, "/api/v1/admin/agent-config/sync", `{}`)
	if response.Code != http.StatusOK || !strings.HasPrefix(prompt, agent.SecurityBoundaryPreamble) ||
		strings.Contains(prompt, "DECLARATIVE AGENT SKILL") {
		t.Fatalf("disabled skills blocked manual base prompt sync: %d %s", response.Code, response.Body.String())
	}
}

func TestHostedDisabledSkillSaveRequiresVerifiedBasePrompt(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(hostedFixture))
	}))
	defer upstream.Close()
	previous := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = previous }()
	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	response := hostedAPIRequest(router, cookie, http.MethodPut, "/api/v1/admin/agent-config",
		`{"mode":"elevenlabs_agent","elevenLabsAgentSkillsSyncEnabled":false}`)
	if response.Code == http.StatusOK || !store.agentCfg.ElevenLabsSkillsSyncEnabled ||
		!strings.Contains(response.Body.String(), "base-only prompt could not be verified") {
		t.Fatalf("unverified skill removal persisted: %d %s", response.Code, response.Body.String())
	}
}

func TestHostedTokenAdmission(t *testing.T) {
	var tokenCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("xi-api-key") != hostedFixtureKey {
			t.Error("provider credential missing")
		}

		body := hostedFixture
		if r.URL.Path == "/v1/convai/conversation/token" {
			tokenCalls.Add(1)
			if r.URL.Query().Get("agent_id") != "agent_fixture" || r.URL.Query().Get("version_id") != "version_fixture" {
				t.Error("token not bound to validated agent version")
			}
			body = `{"token":"transient-fixture-token","conversation_id":"conv_fixture"}`
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Error(err)
		}
	}))
	defer upstream.Close()
	previous := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = previous }()
	const path = "/api/v1/elevenlabs/conversation-token"
	tests := []struct {
		name   string
		body   string
		cookie bool
		csrf   string
		origin string
		active bool
		mode   string
		want   int
	}{
		{name: "valid", body: `{}`, cookie: true, csrf: "fixture-csrf", origin: "https://aegis.local", active: true, mode: "elevenlabs_agent", want: http.StatusOK},
		{name: "unauthenticated", want: http.StatusUnauthorized},
		{name: "deactivated", cookie: true, want: http.StatusUnauthorized},
		{name: "missing csrf", cookie: true, origin: "https://aegis.local", active: true, want: http.StatusForbidden},
		{name: "wrong origin", cookie: true, csrf: "fixture-csrf", origin: "https://other.local", active: true, want: http.StatusForbidden},
		{name: "missing origin", cookie: true, csrf: "fixture-csrf", active: true, want: http.StatusForbidden},
		{name: "override", body: `{"agentId":"another"}`, cookie: true, csrf: "fixture-csrf", origin: "https://aegis.local", active: true, mode: "elevenlabs_agent", want: http.StatusBadRequest},
		{name: "extra payload", body: `{} {}`, cookie: true, csrf: "fixture-csrf", origin: "https://aegis.local", active: true, mode: "elevenlabs_agent", want: http.StatusBadRequest},
		{name: "mode race", body: `{}`, cookie: true, csrf: "fixture-csrf", origin: "https://aegis.local", active: true, mode: "cascaded", want: http.StatusConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, router, cookie := newHostedAPITest(t, auth.RoleUser)
			store.agentCfg.Mode = test.mode
			store.users["fixture-user"].IsActive = test.active
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(test.body))
			if test.cookie {
				request.AddCookie(cookie)
			}
			request.Header.Set("Origin", test.origin)
			request.Header.Set("X-CSRF-Token", test.csrf)
			response := httptest.NewRecorder()
			before := tokenCalls.Load()
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("got %d %s; want %d", response.Code, response.Body.String(), test.want)
			}
			if test.name == "valid" {
				var access struct {
					Token          string `json:"conversationToken"`
					ConversationID string `json:"conversationId"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &access); err != nil ||
					strings.TrimSpace(access.Token) == "" || strings.TrimSpace(access.ConversationID) == "" {
					t.Fatalf("successful access response was incomplete: %#v (error: %v)", access, err)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil || len(fields) != 4 ||
					strings.Contains(response.Body.String(), "agent_fixture") ||
					strings.Contains(response.Body.String(), hostedFixtureKey) {
					t.Fatalf("token response exposed admin-only data: %s (error: %v)", response.Body.String(), err)
				}
			}
			if test.want != http.StatusOK && tokenCalls.Load() != before {
				t.Fatal("denied request issued a token")
			}
			if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), hostedFixtureKey) {
				t.Fatal("response cache or credential leak")
			}
		})
	}
	store, router, cookie := newHostedAPITest(t, auth.RoleUser)
	store.agentCfg.Mode = "elevenlabs_agent"
	for i := 0; i < 10; i++ {
		if response := hostedAPIRequest(router, cookie, http.MethodPost, path, `{}`); response.Code != http.StatusOK {
			t.Fatalf("request %d unexpectedly denied: %s", i, response.Body.String())
		}
	}
	before := tokenCalls.Load()
	if response := hostedAPIRequest(router, cookie, http.MethodPost, path, `{}`); response.Code != http.StatusTooManyRequests || tokenCalls.Load() != before {
		t.Fatal("issuer limit did not block token creation")
	}
	response := hostedAPIRequest(router, cookie, http.MethodPost, "/api/v1/rtc/offer", `{"sdp":"fixture","signalingToken":"fixture"}`)
	if response.Code != http.StatusConflict {
		t.Fatal("hosted mode entered backend signaling")
	}
	response = hostedAPIRequest(router, cookie, http.MethodGet, "/api/v1/rtc/config", "")
	var config map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	if string(config["mode"]) != `"elevenlabs_agent"` || strings.Contains(response.Body.String(), "agent_fixture") || strings.Contains(response.Body.String(), hostedFixtureKey) {
		t.Fatal("session configuration did not expose safe hosted metadata")
	}
	var capabilities struct {
		Camera         bool `json:"camera"`
		LocalKnowledge bool `json:"localKnowledge"`
		StrictWakeGate bool `json:"strictWakeGate"`
	}
	if err := json.Unmarshal(config["capabilities"], &capabilities); err != nil ||
		capabilities.Camera || capabilities.LocalKnowledge || capabilities.StrictWakeGate {
		t.Fatalf("hosted session exposed backend-only capabilities: %#v (error: %v)", capabilities, err)
	}
}

func seedHostedDocument(store *mockAllStore, doc rag.Document) {
	store.ragStore.mu.Lock()
	defer store.ragStore.mu.Unlock()
	if doc.ID == "" {
		doc.ID = "doc-fixture"
	}
	doc.Status = "ready"
	cp := doc
	store.ragStore.docs[doc.ID] = &cp
}

func hostedAgentWithKnowledgeBase(entries string) string {
	return strings.Replace(
		hostedFixture,
		`"prompt":{"tool_ids"`,
		`"prompt":{"knowledge_base":[`+entries+`],"rag":{"enabled":false},"tool_ids"`,
		1,
	)
}

func TestHostedDocumentSyncThresholdAndPreservesDashboardEntries(t *testing.T) {
	agentResponse := hostedAgentWithKnowledgeBase(
		`{"id":"dashboard_doc","name":"Dashboard policy","type":"text","usage_mode":"auto"}`,
	)
	var createCalls, indexCalls int
	var patches []map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/convai/agents/"):
			_, _ = w.Write([]byte(agentResponse))
		case r.Method == http.MethodPatch:
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode hosted patch: %v", err)
			} else {
				patches = append(patches, payload)
			}
			_, _ = w.Write([]byte(hostedFixture))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/convai/knowledge-base/text":
			var request struct {
				Name string `json:"name"`
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode Knowledge Base document: %v", err)
			}
			createCalls++
			_, _ = w.Write([]byte(`{"id":"kb_doc_` + string(rune('0'+createCalls)) + `"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rag-index"):
			indexCalls++
			var request map[string]string
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request["model"] != hostedRAGModel {
				t.Errorf("unexpected RAG index request: %#v, error: %v", request, err)
			}
			documentID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/convai/knowledge-base/"), "/rag-index")
			_, _ = w.Write([]byte(`{"id":"` + documentID + `","status":"succeeded"}`))
		default:
			t.Errorf("unexpected provider request: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	previous := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = previous }()

	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	store.agentCfg.Mode = "elevenlabs_agent"
	seedHostedDocument(store, rag.Document{ID: "doc-short", Title: "Short policy", ExtractedText: strings.Repeat("s", 499)})
	seedHostedDocument(store, rag.Document{ID: "doc-long", Title: "Long policy", ExtractedText: strings.Repeat("l", 500)})

	response := hostedAPIRequest(router, cookie, http.MethodPost, "/api/v1/admin/agent-config/sync", `{}`)
	if response.Code != http.StatusOK {
		t.Fatalf("hosted sync failed: %d %s", response.Code, response.Body.String())
	}
	if createCalls != 2 || indexCalls != 1 {
		t.Fatalf("expected two KB uploads and one index request, got uploads=%d indexes=%d", createCalls, indexCalls)
	}
	if len(patches) != 2 {
		t.Fatalf("expected prompt and knowledge patches, got %d", len(patches))
	}
	conversationConfig := patches[1]["conversation_config"].(map[string]any)
	agentConfig := conversationConfig["agent"].(map[string]any)
	promptConfig := agentConfig["prompt"].(map[string]any)
	entries := promptConfig["knowledge_base"].([]any)
	found := map[string]string{}
	for _, raw := range entries {
		entry := raw.(map[string]any)
		found[entry["id"].(string)] = entry["usage_mode"].(string)
	}
	if found["dashboard_doc"] != "auto" || found["kb_doc_1"] != "auto" || found["kb_doc_2"] != "auto" {
		t.Fatalf("managed or unmanaged Knowledge Base entry missing: %#v", entries)
	}
	ragConfig := promptConfig["rag"].(map[string]any)
	if enabled, ok := ragConfig["enabled"].(bool); !ok || !enabled {
		t.Fatalf("hosted RAG was not enabled: %#v", ragConfig)
	}
	for _, id := range []string{"doc-short", "doc-long"} {
		doc, err := store.ragStore.GetDocument(context.Background(), id)
		if err != nil || doc.Status != "ready" || doc.ElevenLabsSyncStatus != "synced" {
			t.Fatalf("local document state changed unexpectedly: doc=%#v err=%v", doc, err)
		}
	}
}

func TestTargetedHostedDocumentSyncPreservesUnmanagedEntries(t *testing.T) {
	agentResponse := hostedAgentWithKnowledgeBase(
		`{"id":"dashboard_doc","name":"Dashboard policy","type":"text","usage_mode":"auto"},` +
			`{"id":"local_other","name":"Other app policy","type":"text","usage_mode":"auto"}`,
	)
	var providerCalls, createCalls, indexCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/convai/agents/"):
			_, _ = w.Write([]byte(agentResponse))
		case r.Method == http.MethodPatch:
			var patch map[string]any
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				t.Errorf("decode targeted sync patch: %v", err)
				http.Error(w, "bad patch", http.StatusBadRequest)
				return
			}
			current := map[string]any{}
			if err := json.Unmarshal([]byte(agentResponse), &current); err != nil {
				t.Fatal(err)
			}
			currentConfig := current["conversation_config"].(map[string]any)
			currentPrompt := currentConfig["agent"].(map[string]any)["prompt"].(map[string]any)
			patchConfig := patch["conversation_config"].(map[string]any)
			patchPrompt := patchConfig["agent"].(map[string]any)["prompt"].(map[string]any)
			currentPrompt["knowledge_base"] = patchPrompt["knowledge_base"]
			currentPrompt["rag"] = patchPrompt["rag"]
			encoded, err := json.Marshal(current)
			if err != nil {
				t.Fatal(err)
			}
			agentResponse = string(encoded)
			_, _ = w.Write(encoded)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/convai/knowledge-base/text":
			createCalls.Add(1)
			_, _ = w.Write([]byte(`{"id":"kb_target"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rag-index"):
			indexCalls.Add(1)
			documentID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/convai/knowledge-base/"), "/rag-index")
			_, _ = w.Write([]byte(`{"id":"` + documentID + `","status":"succeeded"}`))
		default:
			t.Errorf("unexpected targeted sync provider request: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	previous := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = previous }()

	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	store.agentCfg.Mode = "elevenlabs_agent"
	seedHostedDocument(store, rag.Document{
		ID: "local-target", Title: "Target policy", ExtractedText: strings.Repeat("target policy ", 50),
	})
	seedHostedDocument(store, rag.Document{
		ID: "local-other-doc", Title: "Other app policy", ExtractedText: strings.Repeat("other policy ", 50),
		ElevenLabsDocID: "local_other", ElevenLabsAgentID: "agent_fixture",
		ElevenLabsSyncedHash: "existing-hash", ElevenLabsSyncStatus: "synced",
	})

	response := hostedAPIRequest(
		router, cookie, http.MethodPost, "/api/v1/admin/documents/local-target/sync", `{}`,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("targeted document sync failed: %d %s", response.Code, response.Body.String())
	}
	if createCalls.Load() != 1 || indexCalls.Load() != 1 {
		t.Fatalf("expected only the selected document to upload/index, got uploads=%d indexes=%d", createCalls.Load(), indexCalls.Load())
	}
	var result struct {
		Document rag.Document `json:"document"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Document.ElevenLabsSyncStatus != "synced" {
		t.Fatalf("targeted document status was not updated: %+v", result.Document)
	}
	storedTarget, err := store.ragStore.GetDocument(context.Background(), "local-target")
	if err != nil || storedTarget.ElevenLabsDocID != "kb_target" {
		t.Fatalf("hosted document ID was not stored: doc=%+v err=%v", storedTarget, err)
	}

	var agentState map[string]any
	if err := json.Unmarshal([]byte(agentResponse), &agentState); err != nil {
		t.Fatal(err)
	}
	conversationConfig := agentState["conversation_config"].(map[string]any)
	promptConfig := conversationConfig["agent"].(map[string]any)["prompt"].(map[string]any)
	entries := promptConfig["knowledge_base"].([]any)
	found := map[string]bool{}
	for _, raw := range entries {
		found[raw.(map[string]any)["id"].(string)] = true
	}
	if !found["dashboard_doc"] || !found["local_other"] || !found["kb_target"] {
		t.Fatalf("targeted sync removed a dashboard or unrelated managed entry: %#v", entries)
	}
	if providerCalls.Load() < 4 {
		t.Fatalf("expected provider read, upload, index and attachment verification calls; got %d", providerCalls.Load())
	}
}

func TestTargetedHostedDocumentSyncPolicyAndReadinessGateProvider(t *testing.T) {
	var providerCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		http.Error(w, "provider must not be contacted", http.StatusServiceUnavailable)
	}))
	defer upstream.Close()
	previous := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = previous }()

	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	store.agentCfg.Mode = "elevenlabs_agent"
	store.agentCfg.ElevenLabsRAGSyncEnabled = false
	store.ragStore.mu.Lock()
	store.ragStore.docs["failed-doc"] = &rag.Document{ID: "failed-doc", Status: "failed", MimeType: "application/pdf"}
	store.ragStore.mu.Unlock()
	response := hostedAPIRequest(router, cookie, http.MethodPost, "/api/v1/admin/documents/failed-doc/sync", `{}`)
	if response.Code != http.StatusConflict ||
		!strings.Contains(strings.ToLower(response.Body.String()), "enable and save") ||
		providerCalls.Load() != 0 {
		t.Fatalf("disabled policy was not enforced before provider access: %d %s calls=%d", response.Code, response.Body.String(), providerCalls.Load())
	}

	store.agentCfg.ElevenLabsRAGSyncEnabled = true
	response = hostedAPIRequest(router, cookie, http.MethodPost, "/api/v1/admin/documents/failed-doc/sync", `{}`)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "locally ready") || providerCalls.Load() != 0 {
		t.Fatalf("non-ready document was not rejected before provider access: %d %s calls=%d", response.Code, response.Body.String(), providerCalls.Load())
	}
}

func TestHostedDisabledRAGKeepsDocumentLifecycleLocal(t *testing.T) {
	var knowledgeCalls, attachmentCalls, embeddingCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/convai/knowledge-base") {
			knowledgeCalls.Add(1)
			http.Error(w, "unexpected knowledge request", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/embeddings" {
			embeddingCalls.Add(1)
			_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1,0.2,0.3]}]}`))
			return
		}
		if r.Method == http.MethodPatch {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if _, named := payload["name"]; !named {
				attachmentCalls.Add(1)
			}
		}
		_, _ = w.Write([]byte(hostedFixture))
	}))
	defer upstream.Close()
	previous := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = previous }()
	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	store.agentCfg.Mode = "elevenlabs_agent"
	store.agentCfg.ElevenLabsRAGSyncEnabled = false
	store.agentCfg.EmbeddingBaseURL = upstream.URL
	store.agentCfg.EmbeddingModel = "fixture"
	store.agentCfg.EmbeddingDim = 3
	req := createAgentSkillUploadRequest(t, "local.txt", []byte("A short local policy."))
	req.URL.Path = "/api/v1/admin/documents"
	req.AddCookie(cookie)
	req.Header.Set("Origin", "https://aegis.local")
	req.Header.Set("X-CSRF-Token", "fixture-csrf")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	var uploaded struct {
		Document rag.Document `json:"document"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusCreated || uploaded.Document.Status != "ready" {
		t.Fatalf("local ingest failed: %d %s", response.Code, response.Body.String())
	}
	response = hostedAPIRequest(router, cookie, http.MethodPost, "/api/v1/admin/documents/"+uploaded.Document.ID+"/reindex", `{}`)
	if response.Code != http.StatusOK || embeddingCalls.Load() != 2 {
		t.Fatalf("local reindex failed: %d %s embeddings=%d", response.Code, response.Body.String(), embeddingCalls.Load())
	}
	for _, operation := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/admin/agent-config/sync", `{}`},
		{http.MethodPut, "/api/v1/admin/agent-config", `{"mode":"elevenlabs_agent"}`},
	} {
		response = hostedAPIRequest(router, cookie, operation.method, operation.path, operation.body)
		if response.Code != http.StatusOK {
			t.Fatalf("disabled RAG blocked hosted operation: %d %s", response.Code, response.Body.String())
		}
	}
	if knowledgeCalls.Load() != 0 || attachmentCalls.Load() != 0 {
		t.Fatalf("disabled RAG contacted provider: knowledge=%d attachments=%d", knowledgeCalls.Load(), attachmentCalls.Load())
	}
}

func TestHostedDocumentReplacementFailureClearsDeletedTrackingAndKeepsLocalReady(t *testing.T) {
	agentResponse := hostedAgentWithKnowledgeBase(
		`{"id":"old_kb_doc","name":"Old local document","type":"text","usage_mode":"auto"},` +
			`{"id":"dashboard_doc","name":"Dashboard policy","type":"text","usage_mode":"auto"}`,
	)
	var patches []map[string]any
	deletedOldDocument := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/convai/agents/"):
			_, _ = w.Write([]byte(agentResponse))
		case r.Method == http.MethodPatch:
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode hosted patch: %v", err)
			} else {
				patches = append(patches, payload)
			}
			_, _ = w.Write([]byte(hostedFixture))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/convai/knowledge-base/old_kb_doc":
			if r.URL.Query().Get("force") != "true" {
				t.Error("replacement deletion did not use force=true")
			}
			deletedOldDocument = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/convai/knowledge-base/text":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"detail":"sensitive provider response"}`))
		default:
			t.Errorf("unexpected provider request: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	previous := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = previous }()

	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	store.agentCfg.Mode = "elevenlabs_agent"
	seedHostedDocument(store, rag.Document{
		ID: "doc-replaced", Title: "Updated policy", ExtractedText: "Updated text.",
		ElevenLabsDocID: "old_kb_doc", ElevenLabsSyncedHash: "old_hash",
		ElevenLabsAgentID: "agent_fixture", ElevenLabsSyncStatus: "synced",
	})
	response := hostedAPIRequest(router, cookie, http.MethodPost, "/api/v1/admin/agent-config/sync", `{}`)
	if response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "sensitive provider response") {
		t.Fatalf("expected sanitized hosted sync failure, got %d %s", response.Code, response.Body.String())
	}
	doc, err := store.ragStore.GetDocument(context.Background(), "doc-replaced")
	if err != nil || !deletedOldDocument || doc.Status != "ready" || doc.ElevenLabsSyncStatus != "failed" {
		t.Fatalf("local readiness or sync failure state was not preserved: doc=%#v deleted=%v err=%v", doc, deletedOldDocument, err)
	}
	if doc.ElevenLabsDocID != "" || doc.ElevenLabsSyncedHash != "" {
		t.Fatalf("tracking still refers to the deleted hosted document: %#v", doc)
	}
	if len(patches) != 2 {
		t.Fatalf("expected prompt and knowledge patches, got %d", len(patches))
	}
	conversationConfig := patches[1]["conversation_config"].(map[string]any)
	agentConfig := conversationConfig["agent"].(map[string]any)
	promptConfig := agentConfig["prompt"].(map[string]any)
	entries := promptConfig["knowledge_base"].([]any)
	for _, raw := range entries {
		entry := raw.(map[string]any)
		if entry["id"] == "old_kb_doc" {
			t.Fatal("deleted managed document remained attached to the hosted agent")
		}
	}
}

func TestDeleteDocumentRemovesTrackedHostedDocument(t *testing.T) {
	deleted := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v1/convai/knowledge-base/kb_to_delete" || r.URL.Query().Get("force") != "true" {
			t.Errorf("unexpected deletion request: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
			return
		}

		deleted = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	previous := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = previous }()

	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	seedHostedDocument(store, rag.Document{
		ID: "doc-delete", Title: "Old policy", ElevenLabsDocID: "kb_to_delete",
		ElevenLabsAgentID: "agent_fixture", ElevenLabsSyncStatus: "synced",
	})
	response := hostedAPIRequest(router, cookie, http.MethodDelete, "/api/v1/admin/documents/doc-delete", "")
	if response.Code != http.StatusOK || !deleted {
		t.Fatalf("tracked hosted document was not deleted: %d %s deleted=%v", response.Code, response.Body.String(), deleted)
	}
}

func TestHostedRAGDisableCleanupAndRetry(t *testing.T) {
	for _, failure := range []string{"none", "detach", "detachment verification", "partial delete", "deletion verification"} {
		t.Run(failure, func(t *testing.T) {
			entries := []json.RawMessage{
				json.RawMessage(`{"id":"kb_one","type":"text","usage_mode":"auto"}`),
				json.RawMessage(`{"id":"kb_two","type":"text","usage_mode":"auto"}`),
				json.RawMessage(`{"id":"dashboard_doc","type":"text","usage_mode":"prompt","custom":"preserved"}`),
			}
			deleted := make(map[string]bool)
			deleteCalls := make(map[string]int)
			fail := failure != "none"
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasPrefix(r.URL.Path, "/v1/convai/knowledge-base/"):
					id := strings.TrimPrefix(r.URL.Path, "/v1/convai/knowledge-base/")
					if id != "kb_one" && id != "kb_two" {
						t.Errorf("cleanup targeted unmanaged document %q", id)
					}
					if r.Method == http.MethodDelete {
						deleteCalls[id]++
						if fail && failure == "partial delete" && id == "kb_two" {
							w.WriteHeader(http.StatusForbidden)
							return
						}
						if r.URL.Query().Get("force") != "true" {
							t.Error("cleanup omitted force=true")
						}
						deleted[id] = true
						w.WriteHeader(http.StatusNoContent)
						return
					}
					if deleted[id] && !(fail && failure == "deletion verification") {
						http.NotFound(w, r)
					} else {
						_, _ = w.Write([]byte(`{"id":"` + id + `"}`))
					}
					return
				case r.Method == http.MethodPatch:
					var payload map[string]any
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
					}
					if _, named := payload["name"]; !named {
						prompt := payload["conversation_config"].(map[string]any)["agent"].(map[string]any)["prompt"].(map[string]any)
						if len(prompt) != 1 {
							t.Error("cleanup overwrote RAG, prompt, or tools")
						}
						if fail && failure == "detach" {
							w.WriteHeader(http.StatusForbidden)
							return
						}
						if !(fail && failure == "detachment verification") {
							raw, _ := json.Marshal(prompt["knowledge_base"])
							if err := json.Unmarshal(raw, &entries); err != nil {
								t.Error(err)
							}
						}
					}
				}
				raw, _ := json.Marshal(entries)
				_, _ = w.Write([]byte(hostedAgentWithKnowledgeBase(strings.Trim(string(raw), "[]"))))
			}))
			defer upstream.Close()
			previous := elevenLabsAdminAPIBaseURL
			elevenLabsAdminAPIBaseURL = upstream.URL
			defer func() { elevenLabsAdminAPIBaseURL = previous }()
			store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
			store.agentCfg.Mode = "elevenlabs_agent"
			for _, suffix := range []string{"one", "two"} {
				seedHostedDocument(store, rag.Document{
					ID: "local_" + suffix, Title: suffix, ExtractedText: "Retained local text",
					ElevenLabsDocID: "kb_" + suffix, ElevenLabsAgentID: "agent_fixture",
					ElevenLabsSyncedHash: "old_hash", ElevenLabsSyncStatus: "synced",
				})
			}
			body := `{"mode":"elevenlabs_agent","elevenLabsRagSyncEnabled":false}`
			response := hostedAPIRequest(router, cookie, http.MethodPut, "/api/v1/admin/agent-config", body)
			if fail {
				if response.Code == http.StatusOK || !store.agentCfg.ElevenLabsRAGSyncEnabled {
					t.Fatalf("failed cleanup persisted disabled policy: %d %s", response.Code, response.Body.String())
				}
				if failure == "partial delete" {
					doc, _ := store.ragStore.GetDocument(context.Background(), "local_one")
					if doc.ElevenLabsDocID != "" || doc.ElevenLabsSyncStatus != "not_synced" {
						t.Fatal("verified partial cleanup did not clear completed tracking")
					}
				}
				fail = false
				response = hostedAPIRequest(router, cookie, http.MethodPut, "/api/v1/admin/agent-config", body)
			}
			if response.Code != http.StatusOK || store.agentCfg.ElevenLabsRAGSyncEnabled {
				t.Fatalf("cleanup/retry failed: %d %s", response.Code, response.Body.String())
			}
			if len(entries) != 1 || !strings.Contains(string(entries[0]), `"custom":"preserved"`) {
				t.Fatalf("dashboard attachment was not preserved: %s", entries)
			}
			for _, suffix := range []string{"one", "two"} {
				doc, err := store.ragStore.GetDocument(context.Background(), "local_"+suffix)
				if err != nil || doc.Status != "ready" || doc.ExtractedText != "Retained local text" ||
					doc.ElevenLabsDocID != "" || doc.ElevenLabsAgentID != "" ||
					doc.ElevenLabsSyncStatus != "not_synced" || doc.ElevenLabsSyncedHash != "" {
					t.Fatalf("cleanup changed local document or left hosted tracking: %#v err=%v", doc, err)
				}
			}
			if failure == "partial delete" && deleteCalls["kb_one"] != 1 {
				t.Fatal("retry repeated a completed removal")
			}
			before := deleteCalls["kb_one"] + deleteCalls["kb_two"]
			response = hostedAPIRequest(router, cookie, http.MethodPut, "/api/v1/admin/agent-config", body)
			if response.Code != http.StatusOK || deleteCalls["kb_one"]+deleteCalls["kb_two"] != before {
				t.Fatal("repeated disabled save was not idempotent")
			}
		})
	}
}
