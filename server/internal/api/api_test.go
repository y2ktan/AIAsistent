package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"aegis-voice/server/internal/agent"
	"aegis-voice/server/internal/auth"
	"aegis-voice/server/internal/mcp"
	"aegis-voice/server/internal/rag"
	"aegis-voice/server/internal/rtc"
	"aegis-voice/server/internal/security"
	"aegis-voice/server/internal/skills"
)

type mockAllStore struct {
	mu              sync.Mutex
	users           map[string]*auth.User
	sessions        map[string]*auth.Session
	signalingTokens map[string]*auth.SignalingToken
	agentCfg        *AgentConfigRecord
	voiceLangs      *agent.VoiceLanguageSettings
	mcpServers      map[string]*MCPServerRecord
	agentSkills     map[string]*skills.Skill
	audits          []AuditLogRecord
	seq             int
}

func newMockAllStore() *mockAllStore {
	return &mockAllStore{
		users:           make(map[string]*auth.User),
		sessions:        make(map[string]*auth.Session),
		signalingTokens: make(map[string]*auth.SignalingToken),
		mcpServers:      make(map[string]*MCPServerRecord),
		agentSkills:     make(map[string]*skills.Skill),
		agentCfg:        &AgentConfigRecord{Name: "default", Mode: "cascaded", AudioFocusEnabled: true},
	}
}

func (m *mockAllStore) CountUsers(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.users), nil
}

func (m *mockAllStore) BootstrapFirstAdmin(_ context.Context, email, passwordHash string) (*auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.users) > 0 {
		return nil, auth.ErrBootstrapDisabled
	}
	u := &auth.User{ID: "admin-1", Email: email, PasswordHash: passwordHash, Role: auth.RoleAdmin, IsActive: true}
	m.users[u.ID] = u
	return u, nil
}

func (m *mockAllStore) GetUserByEmail(_ context.Context, email string) (*auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, auth.ErrNotFound
}

func (m *mockAllStore) GetUserByID(_ context.Context, id string) (*auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return nil, auth.ErrNotFound
	}
	return u, nil
}

func (m *mockAllStore) CreateSession(_ context.Context, sess *auth.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	sess.ID = "sess-" + string(rune('0'+m.seq))
	m.sessions[string(sess.TokenHash)] = sess
	return nil
}

func (m *mockAllStore) GetActiveSessionByTokenHash(_ context.Context, tokenHash []byte) (*auth.Session, *auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[string(tokenHash)]
	if !ok || s.RevokedAt != nil {
		return nil, nil, auth.ErrNotFound
	}
	u, ok := m.users[s.UserID]
	if !ok || !u.IsActive {
		return nil, nil, auth.ErrNotFound
	}
	return s, u, nil
}

func (m *mockAllStore) TouchSession(_ context.Context, _ string) error          { return nil }
func (m *mockAllStore) RevokeSession(_ context.Context, _ string) error         { return nil }
func (m *mockAllStore) RevokeAllUserSessions(_ context.Context, _ string) error { return nil }

func (m *mockAllStore) CreateSignalingToken(_ context.Context, tok *auth.SignalingToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.signalingTokens[string(tok.TokenHash)] = tok
	return nil
}

func (m *mockAllStore) ConsumeSignalingToken(_ context.Context, tokenHash []byte) (*auth.SignalingToken, *auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tok, ok := m.signalingTokens[string(tokenHash)]
	if !ok || tok.ConsumedAt != nil {
		return nil, nil, auth.ErrInvalidToken
	}
	now := time.Now()
	tok.ConsumedAt = &now
	return tok, m.users[tok.UserID], nil
}

func (m *mockAllStore) InsertAuditLog(_ context.Context, entry auth.AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	m.audits = append(m.audits, AuditLogRecord{
		ID:         int64(m.seq),
		ActorID:    entry.ActorID,
		ActorEmail: entry.ActorEmail,
		Action:     entry.Action,
		TargetType: entry.TargetType,
		TargetID:   entry.TargetID,
		Outcome:    entry.Outcome,
		Metadata:   entry.Metadata,
	})
	return nil
}

func (m *mockAllStore) ListUsers(_ context.Context) ([]auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []auth.User
	for _, u := range m.users {
		out = append(out, *u)
	}
	return out, nil
}

func (m *mockAllStore) CreateUser(_ context.Context, email, passwordHash, role string) (*auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	u := &auth.User{ID: "user-" + string(rune('0'+m.seq)), Email: email, PasswordHash: passwordHash, Role: role, IsActive: true}
	m.users[u.ID] = u
	return u, nil
}

func (m *mockAllStore) UpdateUser(_ context.Context, userID, role string, isActive bool) (*auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return nil, ErrNotFound
	}
	if u.Role == auth.RoleAdmin && u.IsActive && (role != auth.RoleAdmin || !isActive) {
		activeAdmins := 0
		for _, other := range m.users {
			if other.ID != userID && other.Role == auth.RoleAdmin && other.IsActive {
				activeAdmins++
			}
		}
		if activeAdmins == 0 {
			return nil, ErrLastAdmin
		}
	}
	u.Role = role
	u.IsActive = isActive
	return u, nil
}

func (m *mockAllStore) GetAgentConfig(_ context.Context) (*AgentConfigRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *m.agentCfg
	return &cp, nil
}

func (m *mockAllStore) UpsertAgentConfig(_ context.Context, rec *AgentConfigRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec.ID = "cfg-1"
	if len(rec.APIKeyCiphertext) == 0 && m.agentCfg != nil {
		rec.APIKeyCiphertext = m.agentCfg.APIKeyCiphertext
		rec.APIKeyLast4 = m.agentCfg.APIKeyLast4
	}
	cp := *rec
	m.agentCfg = &cp
	return nil
}

func (m *mockAllStore) GetVoiceLanguages(_ context.Context) (agent.VoiceLanguageSettings, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.voiceLangs == nil {
		return agent.VoiceLanguageSettings{}, false, nil
	}
	cp := *m.voiceLangs
	cp.Languages = append([]agent.VoiceLanguage(nil), m.voiceLangs.Languages...)
	return cp, true, nil
}

func (m *mockAllStore) ReplaceVoiceLanguages(_ context.Context, cfg agent.VoiceLanguageSettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := cfg
	cp.Languages = append([]agent.VoiceLanguage(nil), cfg.Languages...)
	m.voiceLangs = &cp
	return nil
}

func (m *mockAllStore) ListMCPServers(_ context.Context) ([]MCPServerRecord, error) { return nil, nil }
func (m *mockAllStore) GetMCPServer(_ context.Context, _ string) (*MCPServerRecord, error) {
	return nil, ErrNotFound
}
func (m *mockAllStore) UpsertMCPServer(_ context.Context, rec *MCPServerRecord) (*MCPServerRecord, error) {
	return rec, nil
}
func (m *mockAllStore) DeleteMCPServer(_ context.Context, _ string) error { return nil }
func cloneTestSkill(skill *skills.Skill) *skills.Skill {
	cp := *skill
	cp.AllowedTools = append([]string(nil), skill.AllowedTools...)
	cp.ReferenceFiles = make(map[string]string, len(skill.ReferenceFiles))
	for name, content := range skill.ReferenceFiles {
		cp.ReferenceFiles[name] = content
	}
	if skill.UploadedBy != nil {
		uploadedBy := *skill.UploadedBy
		cp.UploadedBy = &uploadedBy
	}
	return &cp
}

func (m *mockAllStore) ListAgentSkills(_ context.Context) ([]skills.Skill, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]skills.Skill, 0, len(m.agentSkills))
	for _, skill := range m.agentSkills {
		out = append(out, *cloneTestSkill(skill))
	}
	return out, nil
}

func (m *mockAllStore) GetAgentSkill(_ context.Context, id string) (*skills.Skill, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, skill := range m.agentSkills {
		if skill.ID == id {
			return cloneTestSkill(skill), nil
		}
	}
	return nil, ErrNotFound
}

func (m *mockAllStore) UpsertAgentSkillByName(_ context.Context, skill *skills.Skill) (*skills.Skill, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.agentSkills {
		if existing.Name == skill.Name {
			skill.ID = existing.ID
			skill.Enabled = existing.Enabled
			skill.CreatedAt = existing.CreatedAt
			skill.UpdatedAt = time.Now()
			m.agentSkills[skill.ID] = cloneTestSkill(skill)
			return cloneTestSkill(skill), nil
		}
	}
	m.seq++
	if skill.ID == "" {
		skill.ID = "skill-" + string(rune('0'+m.seq))
	}
	if skill.CreatedAt.IsZero() {
		skill.CreatedAt = time.Now()
	}
	skill.UpdatedAt = skill.CreatedAt
	m.agentSkills[skill.ID] = cloneTestSkill(skill)
	return cloneTestSkill(skill), nil
}

func (m *mockAllStore) UpdateAgentSkillEnabled(_ context.Context, id string, enabled bool) (*skills.Skill, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	skill, ok := m.agentSkills[id]
	if !ok {
		return nil, ErrNotFound
	}
	skill.Enabled = enabled
	skill.UpdatedAt = time.Now()
	return cloneTestSkill(skill), nil
}

func (m *mockAllStore) DeleteAgentSkill(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.agentSkills[id]; !ok {
		return ErrNotFound
	}
	delete(m.agentSkills, id)
	return nil
}

func (m *mockAllStore) ListAuditLogs(_ context.Context, _, _ int) ([]AuditLogRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]AuditLogRecord(nil), m.audits...), nil
}

func createAgentSkillUploadRequest(t *testing.T, filename string, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/skills", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func createAgentSkillZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var body bytes.Buffer
	writer := zip.NewWriter(&body)
	for filename, content := range files {
		entry, err := writer.Create(filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

func TestAgentSkillsAdminAPI(t *testing.T) {
	store := newMockAllStore()
	vault, err := security.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(store, auth.Config{
		WebOrigin:           "https://aegis.local",
		AdminBootstrapToken: "boot-123",
	}, nil)
	rtcMgr, err := rtc.NewManager(rtc.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rtcMgr.Close()

	router := NewServer(authSvc, store, store, rag.NewService(nil, 0), rtcMgr, mcp.NewHost(false), vault, false, nil).Router()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewBufferString(`{"bootstrapToken":"boot-123","email":"admin@aegis.local","password":"AdminPassword!2026"}`))
	req.Header.Set("Origin", "https://aegis.local")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap failed: %d %s", rec.Code, rec.Body.String())
	}
	adminCookie := rec.Result().Cookies()[0]
	var bootstrap struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &bootstrap); err != nil {
		t.Fatal(err)
	}

	sendAdmin := func(req *http.Request, includeCSRF bool) *httptest.ResponseRecorder {
		req.AddCookie(adminCookie)
		req.Header.Set("Origin", "https://aegis.local")
		if includeCSRF {
			req.Header.Set("X-CSRF-Token", bootstrap.CSRFToken)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}

	markdown := []byte("---\nname: Weekly Report\ndescription: Summarize weekly reports.\nallowed-tools: reports__list\n---\n\nRead and summarize the latest report.\n")
	rec = sendAdmin(createAgentSkillUploadRequest(t, "weekly-report.md", markdown), true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Markdown upload failed: %d %s", rec.Code, rec.Body.String())
	}
	var firstUpload struct {
		Skill adminAgentSkillResponse `json:"skill"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &firstUpload); err != nil {
		t.Fatal(err)
	}
	if firstUpload.Skill.Name != "weekly-report" || firstUpload.Skill.SourceFormat != "md" || firstUpload.Skill.Enabled != true {
		t.Fatalf("unexpected Markdown upload response: %+v", firstUpload.Skill)
	}
	skillID := firstUpload.Skill.ID

	rec = sendAdmin(httptest.NewRequest(http.MethodPatch, "/api/v1/admin/skills/"+skillID, bytes.NewBufferString(`{"enabled":false}`)), true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Fatalf("disable skill failed: %d %s", rec.Code, rec.Body.String())
	}

	zipData := createAgentSkillZip(t, map[string]string{
		"weekly/SKILL.md":            "---\nname: weekly-report\ndescription: Updated weekly report workflow.\n---\n\nUse the report reference and summarize changes.\n",
		"weekly/references/guide.md": "Internal reference content must not be exposed by list API.",
	})
	rec = sendAdmin(createAgentSkillUploadRequest(t, "weekly-report.zip", zipData), true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("ZIP re-upload failed: %d %s", rec.Code, rec.Body.String())
	}
	var secondUpload struct {
		Skill adminAgentSkillResponse `json:"skill"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &secondUpload); err != nil {
		t.Fatal(err)
	}
	if secondUpload.Skill.ID != skillID || secondUpload.Skill.Enabled || secondUpload.Skill.SourceFormat != "zip" {
		t.Fatalf("re-upload did not preserve identifier/disabled state: %+v", secondUpload.Skill)
	}
	if secondUpload.Skill.Description != "Updated weekly report workflow." ||
		len(secondUpload.Skill.ReferenceFilePaths) != 1 ||
		secondUpload.Skill.ReferenceFilePaths[0] != "references/guide.md" {
		t.Fatalf("ZIP skill metadata not updated: %+v", secondUpload.Skill)
	}

	rec = sendAdmin(createAgentSkillUploadRequest(t, "invalid.md", []byte("# missing frontmatter\n")), true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid upload status = %d, want 400: %s", rec.Code, rec.Body.String())
	}

	rec = sendAdmin(httptest.NewRequest(http.MethodGet, "/api/v1/admin/skills", nil), false)
	if rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), "Use the report reference") ||
		!strings.Contains(rec.Body.String(), "references/guide.md") ||
		strings.Contains(rec.Body.String(), "Internal reference content") {
		t.Fatalf("unexpected skills list response: %d %s", rec.Code, rec.Body.String())
	}

	rec = sendAdmin(httptest.NewRequest(http.MethodPatch, "/api/v1/admin/skills/"+skillID, bytes.NewBufferString(`{"enabled":true}`)), true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":true`) {
		t.Fatalf("enable skill failed: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewBufferString(`{"email":"user@aegis.local","password":"UserPassword!2026","role":"user"}`))
	rec = sendAdmin(req, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create regular user failed: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"email":"user@aegis.local","password":"UserPassword!2026"}`))
	req.Header.Set("Origin", "https://aegis.local")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("regular user login failed: %d %s", rec.Code, rec.Body.String())
	}
	userCookie := rec.Result().Cookies()[0]
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/skills", nil)
	req.AddCookie(userCookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin skills access status = %d, want 403", rec.Code)
	}

	rec = sendAdmin(httptest.NewRequest(http.MethodPatch, "/api/v1/admin/skills/"+skillID, bytes.NewBufferString(`{"enabled":false}`)), false)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF token status = %d, want 403", rec.Code)
	}

	rec = sendAdmin(httptest.NewRequest(http.MethodDelete, "/api/v1/admin/skills/"+skillID, nil), true)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete skill failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = sendAdmin(httptest.NewRequest(http.MethodGet, "/api/v1/admin/skills", nil), false)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"skills":[]`) {
		t.Fatalf("deleted skill remained in list: %d %s", rec.Code, rec.Body.String())
	}

	rec = sendAdmin(httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs", nil), false)
	for _, action := range []string{"admin.skill.upload", "admin.skill.update", "admin.skill.delete"} {
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), action) {
			t.Fatalf("audit log missing %q: %d %s", action, rec.Code, rec.Body.String())
		}
	}
}

func TestAdminAPISchemaRBACMaskingAndLastAdmin(t *testing.T) {
	store := newMockAllStore()
	vault, _ := security.NewVault(make([]byte, 32))
	authSvc := auth.NewService(store, auth.Config{
		WebOrigin:           "https://aegis.local",
		AdminBootstrapToken: "boot-123",
	}, nil)
	rtcMgr, _ := rtc.NewManager(rtc.Config{}, nil)
	defer rtcMgr.Close()

	srv := NewServer(authSvc, store, store, rag.NewService(nil, 0), rtcMgr, mcp.NewHost(false), vault, false, nil)
	router := srv.Router()

	// Bootstrap admin
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewBufferString(`{"bootstrapToken":"boot-123","email":"admin@aegis.local","password":"AdminPassword!2026"}`))
	req.Header.Set("Origin", "https://aegis.local")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap failed: %d %s", rec.Code, rec.Body.String())
	}
	var bootResp struct {
		CSRFToken string `json:"csrfToken"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &bootResp)
	adminCookie := rec.Result().Cookies()[0]

	// 1. Unknown JSON field rejected with 400
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/agent-config", bytes.NewBufferString(`{"mode":"cascaded","unknownField":123}`))
	req.AddCookie(adminCookie)
	req.Header.Set("Origin", "https://aegis.local")
	req.Header.Set("X-CSRF-Token", bootResp.CSRFToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on unknown field, got %d", rec.Code)
	}

	// 2. SSRF URL rejected with 400
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/agent-config", bytes.NewBufferString(`{"mode":"cascaded","llmBaseUrl":"http://169.254.169.254/latest"}`))
	req.AddCookie(adminCookie)
	req.Header.Set("Origin", "https://aegis.local")
	req.Header.Set("X-CSRF-Token", bootResp.CSRFToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on SSRF metadata URL, got %d", rec.Code)
	}

	// 3. Valid agent-config encrypts key & masks GET response
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/agent-config", bytes.NewBufferString(`{"mode":"cascaded","llmBaseUrl":"https://api.openai.com/v1","apiKey":"sk-super-secret-9999"}`))
	req.AddCookie(adminCookie)
	req.Header.Set("Origin", "https://aegis.local")
	req.Header.Set("X-CSRF-Token", bootResp.CSRFToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 saving agent config, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/agent-config", nil)
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, "sk-super-secret") || !strings.Contains(body, "****9999") {
		t.Fatalf("expected masked key preview ****9999 and no plaintext, got: %s", body)
	}
	if !strings.Contains(body, `"audioFocusEnabled":true`) {
		t.Fatalf("expected omitted audioFocusEnabled update to preserve default true, got: %s", body)
	}

	// Audio Focus can be disabled explicitly and is available to authenticated voice sessions.
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/agent-config", bytes.NewBufferString(`{"mode":"cascaded","audioFocusEnabled":false}`))
	req.AddCookie(adminCookie)
	req.Header.Set("Origin", "https://aegis.local")
	req.Header.Set("X-CSRF-Token", bootResp.CSRFToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"audioFocusEnabled":false`) {
		t.Fatalf("expected audio focus to be disabled, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/rtc/config", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated rtc config request to be rejected, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/rtc/config", nil)
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "{\"audioFocusEnabled\":false}\n" {
		t.Fatalf("expected authenticated rtc config with audioFocusEnabled=false, got %d: %s", rec.Code, rec.Body.String())
	}

	// 4. Last-admin lockout protection returns 409 Conflict
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/admin/users/admin-1", bytes.NewBufferString(`{"role":"user","isActive":false}`))
	req.AddCookie(adminCookie)
	req.Header.Set("Origin", "https://aegis.local")
	req.Header.Set("X-CSRF-Token", bootResp.CSRFToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict for demoting last admin, got %d: %s", rec.Code, rec.Body.String())
	}

	// 5. Audit logs recorded
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs", nil)
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "admin.agent_config.update") || !strings.Contains(rec.Body.String(), `"audioFocusEnabled":false`) {
		t.Fatalf("expected audit log entry, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestVoiceLanguagesAdminAPI(t *testing.T) {
	store := newMockAllStore()
	store.agentCfg.TTSVoice = "af_heart"
	vault, _ := security.NewVault(make([]byte, 32))
	authSvc := auth.NewService(store, auth.Config{
		WebOrigin:           "https://aegis.local",
		AdminBootstrapToken: "boot-123",
	}, nil)
	rtcMgr, _ := rtc.NewManager(rtc.Config{}, nil)
	defer rtcMgr.Close()

	srv := NewServer(authSvc, store, store, rag.NewService(nil, 0), rtcMgr, mcp.NewHost(false), vault, false, nil)
	router := srv.Router()

	// Bootstrap admin
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewBufferString(`{"bootstrapToken":"boot-123","email":"admin@aegis.local","password":"AdminPassword!2026"}`))
	req.Header.Set("Origin", "https://aegis.local")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap failed: %d", rec.Code)
	}
	var bootResp struct {
		CSRFToken string `json:"csrfToken"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &bootResp)
	adminCookie := rec.Result().Cookies()[0]

	// Create regular user and log them in
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewBufferString(`{"email":"user@aegis.local","password":"UserPassword!2026","role":"user"}`))
	req.AddCookie(adminCookie)
	req.Header.Set("Origin", "https://aegis.local")
	req.Header.Set("X-CSRF-Token", bootResp.CSRFToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user failed: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"email":"user@aegis.local","password":"UserPassword!2026"}`))
	req.Header.Set("Origin", "https://aegis.local")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("user login failed: %d", rec.Code)
	}
	var userLogin struct {
		CSRFToken string `json:"csrfToken"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &userLogin)
	userCookie := rec.Result().Cookies()[0]

	// 1. First read shows initial 4 languages with English as default
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/voice-languages", nil)
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET voice-languages failed: %d %s", rec.Code, rec.Body.String())
	}
	var getResp struct {
		Languages       []agent.VoiceLanguage `json:"languages"`
		DefaultLanguage string                `json:"defaultLanguage"`
		Saved           bool                  `json:"saved"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &getResp)
	if getResp.Saved || getResp.DefaultLanguage != "en" || len(getResp.Languages) != 4 || getResp.Languages[0].Voice != "af_heart" {
		t.Fatalf("unexpected initial GET response: %+v", getResp)
	}

	// 2. Non-admin gets 403
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/voice-languages", bytes.NewBufferString(`{"defaultLanguage":"en","languages":[{"code":"en","voice":"af_heart"}]}`))
	req.AddCookie(userCookie)
	req.Header.Set("Origin", "https://aegis.local")
	req.Header.Set("X-CSRF-Token", userLogin.CSRFToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", rec.Code)
	}

	// 3. Add Japanese and remove Malay, then verify persistence
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/voice-languages", bytes.NewBufferString(`{"defaultLanguage":"en","languages":[{"code":"en","voice":"af_heart"},{"code":"zh","voice":"zf_xiaoxiao"},{"code":"ja","voice":""}]}`))
	req.AddCookie(adminCookie)
	req.Header.Set("Origin", "https://aegis.local")
	req.Header.Set("X-CSRF-Token", bootResp.CSRFToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT voice-languages failed: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/voice-languages", nil)
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &getResp)
	if !getResp.Saved || len(getResp.Languages) != 3 || getResp.Languages[2].Code != "ja" {
		t.Fatalf("expected persisted 3 languages with ja, got %+v", getResp)
	}

	// 4. Invalid saves return 400 and leave stored list unchanged
	badPayloads := []string{
		`{"defaultLanguage":"en","languages":[{"code":"en","voice":"af_heart"},{"code":"en","voice":"v2"}]}`,
		`{"defaultLanguage":"en","languages":[{"code":"zh","voice":"zf_xiaoxiao"}]}`,
		`{"defaultLanguage":"en","languages":[{"code":"en","voice":""}]}`,
	}
	for _, body := range badPayloads {
		req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/voice-languages", bytes.NewBufferString(body))
		req.AddCookie(adminCookie)
		req.Header.Set("Origin", "https://aegis.local")
		req.Header.Set("X-CSRF-Token", bootResp.CSRFToken)
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for payload %s, got %d %s", body, rec.Code, rec.Body.String())
		}
	}

	// Verify stored list unchanged after rejections
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/voice-languages", nil)
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &getResp)
	if len(getResp.Languages) != 3 {
		t.Fatalf("expected stored list unchanged (3), got %d", len(getResp.Languages))
	}

	// 5. Audit log recorded
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs", nil)
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "admin.voice_languages.update") {
		t.Fatalf("expected audit log entry for voice_languages, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAdminProviderDiscoveryAPIs(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/voices":
			_, _ = w.Write([]byte(`{"voices":["af_heart","zf_xiaoxiao"]}`))
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"other-model"},{"id":"openai/whisper-large"},{"id":"Systran/faster-whisper-small"},{"id":"Systran/faster-whisper-medium"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

	store := newMockAllStore()
	store.agentCfg.TTSBaseURL = provider.URL + "/v1"
	store.agentCfg.STTBaseURL = provider.URL + "/v1"
	vault, err := security.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(store, auth.Config{
		WebOrigin:           "https://aegis.local",
		AdminBootstrapToken: "boot-123",
	}, nil)
	rtcMgr, err := rtc.NewManager(rtc.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rtcMgr.Close()

	router := NewServer(authSvc, store, store, rag.NewService(nil, 0), rtcMgr, mcp.NewHost(false), vault, true, nil).Router()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewBufferString(`{"bootstrapToken":"boot-123","email":"admin@aegis.local","password":"AdminPassword!2026"}`))
	req.Header.Set("Origin", "https://aegis.local")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap failed: %d %s", rec.Code, rec.Body.String())
	}
	adminCookie := rec.Result().Cookies()[0]
	var bootstrap struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &bootstrap); err != nil {
		t.Fatal(err)
	}

	getAdmin := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(adminCookie)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	var voicesResponse struct {
		Voices    []string `json:"voices"`
		Supported bool     `json:"supported"`
	}
	rec = getAdmin("/api/v1/admin/tts-voices")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET tts-voices failed: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &voicesResponse); err != nil {
		t.Fatal(err)
	}
	if !voicesResponse.Supported || strings.Join(voicesResponse.Voices, ",") != "af_heart,zf_xiaoxiao" {
		t.Fatalf("unexpected TTS voices response: %+v", voicesResponse)
	}

	var modelsResponse struct {
		Models    []string `json:"models"`
		Supported bool     `json:"supported"`
	}
	rec = getAdmin("/api/v1/admin/stt-models")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET stt-models failed: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &modelsResponse); err != nil {
		t.Fatal(err)
	}
	wantModels := []string{
		"Systran/faster-whisper-small",
		"Systran/faster-whisper-medium",
		"openai/whisper-large",
		"other-model",
	}
	if !modelsResponse.Supported || strings.Join(modelsResponse.Models, ",") != strings.Join(wantModels, ",") {
		t.Fatalf("unexpected prioritized STT models: %+v", modelsResponse)
	}

	for _, baseURL := range []string{provider.URL + "/missing/v1", "http://127.0.0.1:1/v1"} {
		store.mu.Lock()
		store.agentCfg.TTSBaseURL = baseURL
		store.mu.Unlock()
		rec = getAdmin("/api/v1/admin/tts-voices")
		if rec.Code != http.StatusOK {
			t.Fatalf("unsupported provider should return 200, got %d %s", rec.Code, rec.Body.String())
		}
		voicesResponse = struct {
			Voices    []string `json:"voices"`
			Supported bool     `json:"supported"`
		}{}
		if err := json.Unmarshal(rec.Body.Bytes(), &voicesResponse); err != nil {
			t.Fatal(err)
		}
		if voicesResponse.Supported || len(voicesResponse.Voices) != 0 {
			t.Fatalf("expected unsupported response for %q, got %+v", baseURL, voicesResponse)
		}
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewBufferString(`{"email":"user@aegis.local","password":"UserPassword!2026","role":"user"}`))
	req.AddCookie(adminCookie)
	req.Header.Set("Origin", "https://aegis.local")
	req.Header.Set("X-CSRF-Token", bootstrap.CSRFToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user failed: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"email":"user@aegis.local","password":"UserPassword!2026"}`))
	req.Header.Set("Origin", "https://aegis.local")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("user login failed: %d %s", rec.Code, rec.Body.String())
	}
	userCookie := rec.Result().Cookies()[0]
	for _, path := range []string{"/api/v1/admin/tts-voices", "/api/v1/admin/stt-models"} {
		req = httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(userCookie)
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for non-admin on %s, got %d %s", path, rec.Code, rec.Body.String())
		}
	}
}
