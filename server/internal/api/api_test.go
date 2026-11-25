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

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

type mockAllStore struct {
	mu              sync.Mutex
	hostedMCPMu     sync.Mutex
	users           map[string]*auth.User
	sessions        map[string]*auth.Session
	signalingTokens map[string]*auth.SignalingToken
	agentCfg        *AgentConfigRecord
	voiceLangs      *agent.VoiceLanguageSettings
	mcpServers      map[string]*MCPServerRecord
	agentSkills     map[string]*skills.Skill
	audits          []AuditLogRecord
	ragStore        *mockRAGStore
	seq             int
}

func newMockAllStore() *mockAllStore {
	return &mockAllStore{
		users:           make(map[string]*auth.User),
		sessions:        make(map[string]*auth.Session),
		signalingTokens: make(map[string]*auth.SignalingToken),
		mcpServers:      make(map[string]*MCPServerRecord),
		agentSkills:     make(map[string]*skills.Skill),
		ragStore:        newMockRAGStore(),
		agentCfg: &AgentConfigRecord{
			Name: "default", Mode: "cascaded", AudioFocusEnabled: true, AssistantName: "Aegis", WakeAliases: []string{},
			ElevenLabsRAGSyncEnabled: true, ElevenLabsSkillsSyncEnabled: true,
		},
	}
}

type mockRAGStore struct {
	mu     sync.Mutex
	docs   map[string]*rag.Document
	chunks map[string][]rag.DocumentChunk
	seq    int
}

func newMockRAGStore() *mockRAGStore {
	return &mockRAGStore{docs: make(map[string]*rag.Document), chunks: make(map[string][]rag.DocumentChunk)}
}

func (m *mockRAGStore) CreateDocument(_ context.Context, doc *rag.Document) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	doc.ID = "doc-" + string(rune('0'+m.seq))
	doc.CreatedAt = time.Now()
	doc.UpdatedAt = doc.CreatedAt
	cp := *doc
	m.docs[doc.ID] = &cp
	return nil
}

func (m *mockRAGStore) UpdateDocumentStatus(_ context.Context, id, status, message string, chunks int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, ok := m.docs[id]
	if !ok {
		return rag.ErrNotFound
	}
	doc.Status, doc.ErrorMessage, doc.ChunkCount = status, message, chunks
	return nil
}

func (m *mockRAGStore) ReplaceDocumentChunks(_ context.Context, id string, chunks []rag.DocumentChunk, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, ok := m.docs[id]
	if !ok {
		return rag.ErrNotFound
	}
	m.chunks[id] = append([]rag.DocumentChunk(nil), chunks...)
	doc.Status, doc.ErrorMessage, doc.ChunkCount = "ready", "", len(chunks)
	doc.ExtractedText = text
	doc.ElevenLabsSyncStatus = "not_synced"
	doc.ElevenLabsSyncError = ""
	return nil
}

func (m *mockRAGStore) DeleteDocument(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.docs[id]; !ok {
		return rag.ErrNotFound
	}
	delete(m.docs, id)
	delete(m.chunks, id)
	return nil
}

func (m *mockRAGStore) GetDocument(_ context.Context, id string) (*rag.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, ok := m.docs[id]
	if !ok {
		return nil, rag.ErrNotFound
	}
	cp := *doc
	return &cp, nil
}

func (m *mockRAGStore) ListDocuments(_ context.Context) ([]rag.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]rag.Document, 0, len(m.docs))
	for _, doc := range m.docs {
		out = append(out, *doc)
	}
	return out, nil
}

func (m *mockRAGStore) ListReadyDocumentsForHostedSync(_ context.Context) ([]rag.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []rag.Document
	for id, doc := range m.docs {
		if doc.Status != "ready" {
			continue
		}
		cp := *doc
		if cp.ExtractedText == "" {
			var chunks []string
			for _, chunk := range m.chunks[id] {
				chunks = append(chunks, chunk.Content)
			}
			cp.ExtractedText = strings.Join(chunks, "\n\n")
		}
		out = append(out, cp)
	}
	return out, nil
}

func (m *mockRAGStore) UpdateElevenLabsSyncState(
	_ context.Context,
	id, hostedID, contentHash, agentID, status, message string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, ok := m.docs[id]
	if !ok {
		return rag.ErrNotFound
	}
	doc.ElevenLabsDocID = hostedID
	doc.ElevenLabsSyncedHash = contentHash
	doc.ElevenLabsAgentID = agentID
	doc.ElevenLabsSyncStatus = status
	doc.ElevenLabsSyncError = message
	return nil
}

func (m *mockRAGStore) SearchSimilar(context.Context, []float32, int, float64) ([]rag.RetrievedChunk, error) {
	return nil, nil
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
	cp.WakeAliases = append([]string{}, m.agentCfg.WakeAliases...)
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
	if len(rec.ElevenLabsAPIKeyCiphertext) == 0 && m.agentCfg != nil {
		rec.ElevenLabsAPIKeyCiphertext = m.agentCfg.ElevenLabsAPIKeyCiphertext
		rec.ElevenLabsAPIKeyLast5 = m.agentCfg.ElevenLabsAPIKeyLast5
	}
	cp := *rec
	cp.WakeAliases = append([]string{}, rec.WakeAliases...)
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

func (m *mockAllStore) WithHostedMCPLock(_ context.Context, fn func(AdminStore) error) error {
	m.hostedMCPMu.Lock()
	defer m.hostedMCPMu.Unlock()
	return fn(&lockedMockAdminStore{AdminStore: m})
}

type lockedMockAdminStore struct{ AdminStore }

func (m *lockedMockAdminStore) WithHostedMCPLock(_ context.Context, fn func(AdminStore) error) error {
	return fn(m)
}
func (m *mockAllStore) ListMCPServers(_ context.Context) ([]MCPServerRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MCPServerRecord, 0, len(m.mcpServers))
	for _, rec := range m.mcpServers {
		cp := *rec
		normalizeHostedMCP(&cp)
		out = append(out, cp)
	}
	return out, nil
}
func (m *mockAllStore) GetMCPServer(_ context.Context, id string) (*MCPServerRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.mcpServers[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *rec
	normalizeHostedMCP(&cp)
	return &cp, nil
}
func (m *mockAllStore) UpsertMCPServer(_ context.Context, rec *MCPServerRecord) (*MCPServerRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	normalizeHostedMCP(rec)
	if rec.ID == "" {
		rec.ID = uuid.NewString()
	}
	cp := *rec
	m.mcpServers[rec.ID] = &cp
	return rec, nil
}
func (m *mockAllStore) DeleteMCPServer(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.mcpServers[id]; !ok {
		return ErrNotFound
	}
	delete(m.mcpServers, id)
	return nil
}
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

	store.mu.Lock()
	store.voiceLangs = &agent.VoiceLanguageSettings{
		DefaultCode: "en",
		Languages: []agent.VoiceLanguage{{
			Code: "en", Name: "English", Voice: "af_heart", ElevenLabsVoice: "voice-123",
		}},
	}
	store.mu.Unlock()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/agent-config", bytes.NewBufferString(`{"mode":"cascaded","elevenLabsApiKey":"short"}`))
	req.AddCookie(adminCookie)
	req.Header.Set("Origin", "https://aegis.local")
	req.Header.Set("X-CSRF-Token", bootResp.CSRFToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected short ElevenLabs key to be rejected, got %d: %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/agent-config", bytes.NewBufferString(`{"mode":"cascaded","speechProvider":"elevenlabs","elevenLabsApiKey":"eleven-super-secret-12345"}`))
	req.AddCookie(adminCookie)
	req.Header.Set("Origin", "https://aegis.local")
	req.Header.Set("X-CSRF-Token", bootResp.CSRFToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected ElevenLabs config save, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "eleven-super-secret") || !strings.Contains(rec.Body.String(), "****12345") {
		t.Fatalf("expected only last five ElevenLabs characters, got: %s", rec.Body.String())
	}
	encryptedCopy := append([]byte(nil), store.agentCfg.ElevenLabsAPIKeyCiphertext...)
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/agent-config", bytes.NewBufferString(`{"mode":"cascaded","speechProvider":"elevenlabs"}`))
	req.AddCookie(adminCookie)
	req.Header.Set("Origin", "https://aegis.local")
	req.Header.Set("X-CSRF-Token", bootResp.CSRFToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Equal(encryptedCopy, store.agentCfg.ElevenLabsAPIKeyCiphertext) {
		t.Fatalf("expected omitted ElevenLabs key to be preserved, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "****12345") {
		t.Fatalf("expected preserved ElevenLabs preview, got: %s", rec.Body.String())
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
	if rec.Code != http.StatusOK || rec.Body.String() != "{\"assistantName\":\"Aegis\",\"audioFocusEnabled\":false,\"capabilities\":{\"camera\":true,\"localKnowledge\":true,\"strictWakeGate\":true},\"mode\":\"cascaded\"}\n" {
		t.Fatalf("expected authenticated rtc config with assistantName=Aegis and audioFocusEnabled=false, got %d: %s", rec.Code, rec.Body.String())
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
			if r.Header.Get("xi-api-key") != "" {
				_, _ = w.Write([]byte(`[{"model_id":"eleven_flash_v2_5","name":"Eleven Flash v2.5","can_do_text_to_speech":true},{"model_id":"scribe_v3","name":"Scribe v3","can_do_text_to_speech":false}]`))
			} else {
				_, _ = w.Write([]byte(`{"data":[{"id":"other-model"},{"id":"openai/whisper-large"},{"id":"Systran/faster-whisper-small"},{"id":"Systran/faster-whisper-medium"}]}`))
			}
		case "/v2/voices":
			_, _ = w.Write([]byte(`{"voices":[{"voice_id":"voice-123","name":"Test Voice","labels":{"language":"en"}}],"has_more":false}`))
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

	oldElevenLabsBase := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = provider.URL
	t.Cleanup(func() { elevenLabsAdminAPIBaseURL = oldElevenLabsBase })
	store.mu.Lock()
	store.agentCfg.ElevenLabsAPIKeyCiphertext, _ = vault.Encrypt("eleven-test-key")
	store.mu.Unlock()
	rec = getAdmin("/api/v1/admin/tts-voices?provider=elevenlabs")
	if rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"id":"voice-123"`) ||
		!strings.Contains(rec.Body.String(), `"name":"Test Voice"`) ||
		!strings.Contains(rec.Body.String(), `"language":"en"`) ||
		!strings.Contains(rec.Body.String(), `"id":"eleven_flash_v2_5"`) {
		t.Fatalf("expected draft ElevenLabs voice options, got %d %s", rec.Code, rec.Body.String())
	}
	store.mu.Lock()
	store.agentCfg.SpeechProvider = "elevenlabs"
	store.mu.Unlock()
	rec = getAdmin("/api/v1/admin/tts-voices")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"voice-123"`) || !strings.Contains(rec.Body.String(), `"name":"Test Voice"`) {
		t.Fatalf("expected ElevenLabs voice options, got %d %s", rec.Code, rec.Body.String())
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

func TestAssistantWakeNameAdminAndRTCConfig(t *testing.T) {
	store := newMockAllStore()
	vault, _ := security.NewVault(make([]byte, 32))
	authSvc := auth.NewService(store, auth.Config{
		WebOrigin:           "https://aegis.local",
		AdminBootstrapToken: "boot-123",
	}, nil)
	rtcMgr, _ := rtc.NewManager(rtc.Config{}, nil)
	defer rtcMgr.Close()

	router := NewServer(authSvc, store, store, rag.NewService(nil, 0), rtcMgr, mcp.NewHost(false), vault, false, nil).Router()

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

	sendAdmin := func(method, path, body string) *httptest.ResponseRecorder {
		var r *http.Request
		if body == "" {
			r = httptest.NewRequest(method, path, nil)
		} else {
			r = httptest.NewRequest(method, path, bytes.NewBufferString(body))
			r.Header.Set("Origin", "https://aegis.local")
			r.Header.Set("X-CSRF-Token", bootResp.CSRFToken)
		}
		r.AddCookie(adminCookie)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}

	// 1. Default assistantName is "Aegis" with empty wakeAliases []
	rec = sendAdmin(http.MethodGet, "/api/v1/admin/agent-config", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET agent-config failed: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"assistantName":"Aegis"`) || !strings.Contains(rec.Body.String(), `"wakeAliases":[]`) {
		t.Fatalf("expected default assistantName=Aegis and empty wakeAliases=[], got: %s", rec.Body.String())
	}

	// 2. Save primary name + English/Chinese wakeAliases (trimmed + case/script deduplicated)
	rec = sendAdmin(http.MethodPut, "/api/v1/admin/agent-config", `{
		"mode": "cascaded",
		"llmBaseUrl": "https://api.openai.com/v1",
		"apiKey": "sk-secret-wake-1234",
		"assistantName": "  Jarvis  ",
		"wakeAliases": [" 小智 ", "Aegis", "小智", "jarvis", "", "贾维斯", "賈維斯"]
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT agent-config wake names failed: %d %s", rec.Code, rec.Body.String())
	}
	var cfgResp struct {
		Config AgentConfigRecord `json:"config"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cfgResp); err != nil {
		t.Fatal(err)
	}
	if cfgResp.Config.AssistantName != "Jarvis" {
		t.Fatalf("expected trimmed assistantName Jarvis, got %q", cfgResp.Config.AssistantName)
	}
	wantAliases := []string{"小智", "Aegis", "贾维斯"}
	if len(cfgResp.Config.WakeAliases) != len(wantAliases) {
		t.Fatalf("expected deduplicated wakeAliases %v, got %v", wantAliases, cfgResp.Config.WakeAliases)
	}
	for i, wAlias := range wantAliases {
		if cfgResp.Config.WakeAliases[i] != wAlias {
			t.Fatalf("alias[%d]: expected %q, got %q", i, wAlias, cfgResp.Config.WakeAliases[i])
		}
	}

	// 3. Omitting assistantName and wakeAliases preserves stored values
	rec = sendAdmin(http.MethodPut, "/api/v1/admin/agent-config", `{"mode":"cascaded","llmBaseUrl":"https://api.openai.com/v1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT omitted wake fields failed: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &cfgResp)
	if cfgResp.Config.AssistantName != "Jarvis" || len(cfgResp.Config.WakeAliases) != 3 {
		t.Fatalf("expected omitted update to preserve Jarvis + 3 aliases, got %+v", cfgResp.Config)
	}

	// 4. Blank or oversized assistantName is rejected with 400 and leaves stored config unchanged
	for _, invalidPayload := range []string{
		`{"mode":"cascaded","assistantName":"   "}`,
		`{"mode":"cascaded","assistantName":"` + strings.Repeat("智", 65) + `"}`,
	} {
		rec = sendAdmin(http.MethodPut, "/api/v1/admin/agent-config", invalidPayload)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid assistantName payload %s, got %d %s", invalidPayload, rec.Code, rec.Body.String())
		}
	}
	rec = sendAdmin(http.MethodGet, "/api/v1/admin/agent-config", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &cfgResp)
	if cfgResp.Config.AssistantName != "Jarvis" {
		t.Fatalf("expected failed validation to leave stored assistantName=Jarvis intact, got %q", cfgResp.Config.AssistantName)
	}

	// 5. Audit log records assistantName and wakeAliases
	rec = sendAdmin(http.MethodGet, "/api/v1/admin/audit-logs", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"assistantName":"Jarvis"`) || !strings.Contains(rec.Body.String(), `"wakeAliases":["小智","Aegis","贾维斯"]`) {
		t.Fatalf("expected audit log with assistantName and wakeAliases, got %d %s", rec.Code, rec.Body.String())
	}

	// 6. Authenticated non-admin user retrieves safe assistantName via /api/v1/rtc/config without secrets
	rec = sendAdmin(http.MethodPost, "/api/v1/admin/users", `{"email":"voiceuser@aegis.local","password":"UserPassword!2026","role":"user"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user failed: %d %s", rec.Code, rec.Body.String())
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"email":"voiceuser@aegis.local","password":"UserPassword!2026"}`))
	loginReq.Header.Set("Origin", "https://aegis.local")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("user login failed: %d", loginRec.Code)
	}
	userCookie := loginRec.Result().Cookies()[0]

	rtcReq := httptest.NewRequest(http.MethodGet, "/api/v1/rtc/config", nil)
	rtcReq.AddCookie(userCookie)
	rtcRec := httptest.NewRecorder()
	router.ServeHTTP(rtcRec, rtcReq)
	if rtcRec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/rtc/config failed: %d %s", rtcRec.Code, rtcRec.Body.String())
	}
	if rtcRec.Body.String() != "{\"assistantName\":\"Jarvis\",\"audioFocusEnabled\":true,\"capabilities\":{\"camera\":true,\"localKnowledge\":true,\"strictWakeGate\":true},\"mode\":\"cascaded\"}\n" {
		t.Fatalf("expected safe rtc config payload, got: %s", rtcRec.Body.String())
	}
}

func TestAttachSessionAgentSpeechStartDoesNotTriggerRawBargeIn(t *testing.T) {
	aiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer aiSrv.Close()

	store := newMockAllStore()
	store.agentCfg.LLMBaseURL = aiSrv.URL
	vault, _ := security.NewVault(make([]byte, 32))
	authSvc := auth.NewService(store, auth.Config{WebOrigin: "https://aegis.local"}, nil)
	rtcMgr, err := rtc.NewManager(rtc.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rtcMgr.Close()

	srv := NewServer(authSvc, store, store, rag.NewService(nil, 0), rtcMgr, mcp.NewHost(true), vault, true, nil)

	clientPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = clientPC.Close() }()

	dc, err := clientPC.CreateDataChannel("events", nil)
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan rtc.Event, 16)
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		var ev rtc.Event
		if err := json.Unmarshal(msg.Data, &ev); err == nil {
			events <- ev
		}
	})

	offer, err := clientPC.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gather := webrtc.GatheringCompletePromise(clientPC)
	if err := clientPC.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gather

	ready := make(chan *rtc.PeerSession, 1)
	_, answerSDP, err := rtcMgr.HandleOffer(context.Background(), "u-1", "s-1", clientPC.LocalDescription().SDP, func(ps *rtc.PeerSession) {
		srv.attachSessionAgent(ps)
		ready <- ps
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := clientPC.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answerSDP}); err != nil {
		t.Fatal(err)
	}

	var serverPeer *rtc.PeerSession
	select {
	case serverPeer = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for attachSessionAgent")
	}

	// Drain initial listening event(s)
	deadline := time.Now().Add(2 * time.Second)
	sawListening := false
	for time.Now().Before(deadline) && !sawListening {
		select {
		case ev := <-events:
			if ev.Type == "state" && ev.State == "listening" {
				sawListening = true
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !sawListening {
		t.Fatal("expected initial listening state event")
	}

	// Raw vad.speech_start MUST NOT emit barge_in
	if serverPeer.OnSpeechStart == nil {
		t.Fatal("expected OnSpeechStart callback to be wired")
	}
	serverPeer.OnSpeechStart()

	select {
	case ev := <-events:
		if ev.Type == "barge_in" {
			t.Fatalf("expected raw OnSpeechStart not to trigger barge_in, got %+v", ev)
		}
	case <-time.After(150 * time.Millisecond):
	}
}
