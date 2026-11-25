package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"aegis-voice/server/internal/agent"
	"aegis-voice/server/internal/auth"
	"aegis-voice/server/internal/mcp"
	"aegis-voice/server/internal/rag"
	"aegis-voice/server/internal/rtc"
	"aegis-voice/server/internal/security"
	"aegis-voice/server/internal/skills"
)

var elevenLabsAdminAPIBaseURL = "https://api.elevenlabs.io"

type Server struct {
	authSvc            *auth.Service
	authStore          auth.Store
	adminStore         AdminStore
	ragSvc             *rag.Service
	rtcMgr             *rtc.Manager
	mcpHost            *mcp.Host
	vault              *security.Vault
	allowPrivate       bool
	logger             *slog.Logger
	hostedProxyBaseURL string
	hostedProxyLimiter *auth.KeyedRateLimiter
}

func NewServer(
	authSvc *auth.Service,
	authStore auth.Store,
	adminStore AdminStore,
	ragSvc *rag.Service,
	rtcMgr *rtc.Manager,
	mcpHost *mcp.Host,
	vault *security.Vault,
	allowPrivate bool,
	logger *slog.Logger,
) *Server {
	if mcpHost == nil {
		mcpHost = mcp.NewHost(allowPrivate)
	}
	return &Server{
		authSvc:            authSvc,
		authStore:          authStore,
		adminStore:         adminStore,
		ragSvc:             ragSvc,
		rtcMgr:             rtcMgr,
		mcpHost:            mcpHost,
		vault:              vault,
		allowPrivate:       allowPrivate,
		logger:             logger,
		hostedProxyBaseURL: strings.TrimSpace(os.Getenv("HOSTED_MCP_PROXY_BASE_URL")),
		hostedProxyLimiter: auth.NewKeyedRateLimiter(60.0/60.0, 30),
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeStrictJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}

func (s *Server) audit(ctx context.Context, r *http.Request, action, targetType, targetID, outcome string, meta map[string]any) {
	var actorID *string
	var actorEmail string
	if u, ok := auth.UserFromContext(ctx); ok {
		actorID = &u.ID
		actorEmail = u.Email
	}
	_ = s.authStore.InsertAuditLog(ctx, auth.AuditEntry{
		ActorID:    actorID,
		ActorEmail: actorEmail,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		IPAddress:  auth.ClientIP(r),
		Outcome:    outcome,
		Metadata:   meta,
	})
}

func (s *Server) HandleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.adminStore.ListUsers(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list users"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *Server) HandleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := decodeStrictJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if !strings.Contains(email, "@") || len(email) < 5 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid email"})
		return
	}
	if req.Role != auth.RoleUser && req.Role != auth.RoleAdmin {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must be user or admin"})
		return
	}
	hash, err := security.HashPassword(req.Password)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	u, err := s.adminStore.CreateUser(r.Context(), email, hash, req.Role)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "user already exists or could not be created"})
		return
	}
	s.audit(r.Context(), r, "admin.user.create", "user", u.ID, "success", map[string]any{"role": u.Role})
	writeJSON(w, http.StatusCreated, map[string]any{"user": u})
}

func (s *Server) HandleUpdateUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	var req struct {
		Role     string `json:"role"`
		IsActive bool   `json:"isActive"`
	}
	if err := decodeStrictJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	if req.Role != auth.RoleUser && req.Role != auth.RoleAdmin {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must be user or admin"})
		return
	}
	u, err := s.adminStore.UpdateUser(r.Context(), userID, req.Role, req.IsActive)
	if err != nil {
		if errors.Is(err, ErrLastAdmin) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update user"})
		return
	}
	if !u.IsActive {
		_ = s.authSvc.RevokeUserSessions(r.Context(), u.ID)
	}
	s.audit(r.Context(), r, "admin.user.update", "user", u.ID, "success", map[string]any{"role": u.Role, "isActive": u.IsActive})
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (s *Server) HandleListDocuments(w http.ResponseWriter, r *http.Request) {
	docs, err := s.ragSvc.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list documents"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"documents": docs})
}

func (s *Server) HandleUploadDocument(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, rag.DefaultMaxUploadBytes+1024*1024)
	if err := r.ParseMultipartForm(rag.DefaultMaxUploadBytes); err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "upload exceeds maximum size"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing file field"})
		return
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, rag.DefaultMaxUploadBytes+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read file"})
		return
	}

	title := r.FormValue("title")
	actor, _ := auth.UserFromContext(r.Context())
	emb, _ := s.activeEmbedder(r.Context())

	doc, err := s.ragSvc.Ingest(r.Context(), title, header.Filename, data, actor.ID, emb)
	if err != nil {
		if errors.Is(err, rag.ErrPayloadTooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": err.Error()})
			return
		}
		if errors.Is(err, rag.ErrUnsupportedType) || errors.Is(err, rag.ErrEmptyFile) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		// Document persisted as failed
		writeJSON(w, http.StatusCreated, map[string]any{"document": doc, "warning": "document stored with failed status"})
		return
	}
	s.audit(r.Context(), r, "admin.rag.upload", "document", doc.ID, "success", map[string]any{"filename": doc.Filename, "chunks": doc.ChunkCount})
	if syncErr := s.syncHostedDocumentsIfActive(r.Context()); syncErr != nil {
		updated, getErr := s.ragSvc.GetDocument(r.Context(), doc.ID)
		if getErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "Document is ready locally, but hosted sync failed and its status could not be loaded.",
			})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"document": updated,
			"warning":  "Document is ready locally, but hosted synchronization failed: " + syncErr.Error(),
		})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"document": doc})
}

func (s *Server) HandleReindexDocument(w http.ResponseWriter, r *http.Request) {
	docID := r.PathValue("id")
	if _, err := s.ragSvc.GetDocument(r.Context(), docID); errors.Is(err, rag.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "document not found"})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load document"})
		return
	}
	embedder, err := s.activeEmbedder(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "embedding provider is not configured"})
		return
	}
	if err := s.ragSvc.Reindex(r.Context(), docID, embedder); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to reindex document"})
		return
	}
	doc, err := s.ragSvc.GetDocument(r.Context(), docID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "document was reindexed but its status could not be loaded"})
		return
	}
	s.audit(r.Context(), r, "admin.rag.reindex", "document", doc.ID, "success", map[string]any{"chunks": doc.ChunkCount})
	if syncErr := s.syncHostedDocumentsIfActive(r.Context()); syncErr != nil {
		updated, getErr := s.ragSvc.GetDocument(r.Context(), doc.ID)
		if getErr == nil {
			doc = updated
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"document": doc,
			"warning":  "Document was reindexed locally, but hosted synchronization failed: " + syncErr.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"document": doc})
}

func (s *Server) HandleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	docID := r.PathValue("id")
	doc, err := s.ragSvc.GetDocument(r.Context(), docID)
	if errors.Is(err, rag.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "document not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load document"})
		return
	}
	if err := s.ragSvc.Delete(r.Context(), docID); err != nil {
		if errors.Is(err, rag.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "document not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete document"})
		return
	}
	s.audit(r.Context(), r, "admin.rag.delete", "document", docID, "success", nil)
	if err := s.deleteTrackedHostedDocument(r.Context(), doc); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "Local document was deleted, but hosted Knowledge Base deletion failed: " + err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type adminAgentSkillResponse struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Description        string    `json:"description"`
	Instructions       string    `json:"instructions"`
	ReferenceFilePaths []string  `json:"referenceFilePaths"`
	AllowedTools       []string  `json:"allowedTools"`
	SourceFormat       string    `json:"sourceFormat"`
	Filename           string    `json:"filename"`
	SizeBytes          int64     `json:"sizeBytes"`
	SHA256Hash         string    `json:"sha256Hash"`
	Enabled            bool      `json:"enabled"`
	UploadedBy         *string   `json:"uploadedBy,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

func toAdminAgentSkillResponse(skill skills.Skill) adminAgentSkillResponse {
	referencePaths := make([]string, 0, len(skill.ReferenceFiles))
	for referencePath := range skill.ReferenceFiles {
		referencePaths = append(referencePaths, referencePath)
	}
	sort.Strings(referencePaths)
	allowedTools := append([]string(nil), skill.AllowedTools...)
	if allowedTools == nil {
		allowedTools = []string{}
	}
	return adminAgentSkillResponse{
		ID:                 skill.ID,
		Name:               skill.Name,
		Description:        skill.Description,
		Instructions:       skill.Instructions,
		ReferenceFilePaths: referencePaths,
		AllowedTools:       allowedTools,
		SourceFormat:       skill.SourceFormat,
		Filename:           skill.Filename,
		SizeBytes:          skill.SizeBytes,
		SHA256Hash:         skill.SHA256Hash,
		Enabled:            skill.Enabled,
		UploadedBy:         skill.UploadedBy,
		CreatedAt:          skill.CreatedAt,
		UpdatedAt:          skill.UpdatedAt,
	}
}

func (s *Server) HandleListAgentSkills(w http.ResponseWriter, r *http.Request) {
	skillList, err := s.adminStore.ListAgentSkills(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list Agent Skills"})
		return
	}
	response := make([]adminAgentSkillResponse, 0, len(skillList))
	for _, skill := range skillList {
		response = append(response, toAdminAgentSkillResponse(skill))
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": response})
}

func (s *Server) HandleUploadAgentSkill(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, skills.MaxUploadBytes+(1<<20))
	if err := r.ParseMultipartForm(skills.MaxUploadBytes); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "upload exceeds maximum size"})
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart upload"})
		}
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing file field"})
		return
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, skills.MaxUploadBytes+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read uploaded skill"})
		return
	}
	parsed, err := skills.ParseUpload(header.Filename, data)
	if errors.Is(err, skills.ErrUploadTooLarge) {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	skill := &skills.Skill{
		Name:           parsed.Name,
		Description:    parsed.Description,
		Instructions:   parsed.Instructions,
		ReferenceFiles: parsed.ReferenceFiles,
		AllowedTools:   parsed.AllowedTools,
		SourceFormat:   parsed.SourceFormat,
		Filename:       parsed.Filename,
		SizeBytes:      parsed.SizeBytes,
		SHA256Hash:     parsed.SHA256Hash,
		Enabled:        true,
	}
	if user, ok := auth.UserFromContext(r.Context()); ok {
		skill.UploadedBy = &user.ID
	}
	saved, err := s.adminStore.UpsertAgentSkillByName(r.Context(), skill)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save Agent Skill"})
		return
	}
	s.audit(r.Context(), r, "admin.skill.upload", "agent_skill", saved.ID, "success", map[string]any{
		"name":         saved.Name,
		"sourceFormat": saved.SourceFormat,
		"filename":     saved.Filename,
		"sizeBytes":    saved.SizeBytes,
	})
	if err := s.syncHostedPromptIfActive(r.Context()); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "Agent Skill was saved, but hosted prompt synchronization failed: " + err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"skill": toAdminAgentSkillResponse(*saved)})
}

func (s *Server) HandleUpdateAgentSkill(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeStrictJSON(r, &req); err != nil || req.Enabled == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "enabled must be provided"})
		return
	}
	skill, err := s.adminStore.UpdateAgentSkillEnabled(r.Context(), r.PathValue("id"), *req.Enabled)
	if errors.Is(err, ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Agent Skill not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update Agent Skill"})
		return
	}
	s.audit(r.Context(), r, "admin.skill.update", "agent_skill", skill.ID, "success", map[string]any{"enabled": skill.Enabled})
	if err := s.syncHostedPromptIfActive(r.Context()); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "Agent Skill was updated, but hosted prompt synchronization failed: " + err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skill": toAdminAgentSkillResponse(*skill)})
}

func (s *Server) HandleDeleteAgentSkill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.adminStore.DeleteAgentSkill(r.Context(), id); errors.Is(err, ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Agent Skill not found"})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete Agent Skill"})
		return
	}
	s.audit(r.Context(), r, "admin.skill.delete", "agent_skill", id, "success", nil)
	if err := s.syncHostedPromptIfActive(r.Context()); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "Agent Skill was deleted, but hosted prompt synchronization failed: " + err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func normalizeAgentConfigDefaults(cfg *AgentConfigRecord) {
	if cfg == nil {
		return
	}
	cfg.AssistantName = strings.TrimSpace(cfg.AssistantName)
	if cfg.AssistantName == "" {
		cfg.AssistantName = agent.DefaultAssistantName
	}
	if cfg.WakeAliases == nil {
		cfg.WakeAliases = []string{}
	}
	cfg.SpeechProvider = normalizeSpeechProvider(cfg.SpeechProvider)
	cfg.ElevenLabsSTTModel = normalizeElevenLabsSTTModel(cfg.ElevenLabsSTTModel)
	cfg.ElevenLabsTTSModel = normalizeElevenLabsTTSModel(cfg.ElevenLabsTTSModel)
}

func normalizeWakeAliases(primaryName string, rawAliases []string) ([]string, error) {
	seen := map[string]struct{}{
		agent.CanonicalWakeKey(primaryName): {},
	}
	cleaned := make([]string, 0, len(rawAliases))
	for _, raw := range rawAliases {
		alias := strings.TrimSpace(raw)
		if alias == "" {
			continue
		}
		if utf8.RuneCountInString(alias) > 64 {
			return nil, errors.New("each wake alias must be 64 characters or fewer")
		}
		key := agent.CanonicalWakeKey(alias)
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, alias)
		if len(cleaned) > 16 {
			return nil, errors.New("maximum of 16 wake aliases allowed")
		}
	}
	return cleaned, nil
}

func (s *Server) HandleGetAgentConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.adminStore.GetAgentConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load agent config"})
		return
	}
	normalizeAgentConfigDefaults(cfg)
	writeJSON(w, http.StatusOK, map[string]any{"config": cfg})
}

func (s *Server) HandleSyncHostedAgent(w http.ResponseWriter, r *http.Request) {
	s.withHostedMCPAdmin(w, r, func(locked *Server, r *http.Request) { locked.syncHostedAgent(w, r) })
}

func (s *Server) syncHostedAgent(w http.ResponseWriter, r *http.Request) {
	config, err := s.adminStore.GetAgentConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load hosted configuration"})
		return
	}
	if config == nil || config.Mode != "elevenlabs_agent" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "hosted mode must be active before synchronization"})
		return
	}
	if err := s.syncHostedResources(r.Context(), config); err != nil {
		writeJSON(w, hostedErrorStatus(err), map[string]string{"error": err.Error()})
		return
	}
	documents, err := s.ragSvc.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "hosted sync completed but document status could not be loaded"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "synced", "documents": documents})
}

func (s *Server) HandleDiscoverTTSVoices(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.adminStore.GetAgentConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load agent config"})
		return
	}
	provider := normalizeSpeechProvider(cfg.SpeechProvider)
	if requested := strings.TrimSpace(r.URL.Query().Get("provider")); requested != "" {
		if requested != "local" && requested != "elevenlabs" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported speech provider"})
			return
		}
		provider = requested
	}
	if provider == "elevenlabs" {
		if len(cfg.ElevenLabsAPIKeyCiphertext) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"voices": []string{}, "voiceOptions": []any{}, "supported": false})
			return
		}
		key, decryptErr := s.vault.Decrypt(cfg.ElevenLabsAPIKeyCiphertext)
		if decryptErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to decrypt ElevenLabs api key"})
			return
		}
		req, requestErr := http.NewRequestWithContext(r.Context(), http.MethodGet, strings.TrimRight(elevenLabsAdminAPIBaseURL, "/")+"/v2/voices?page_size=100&include_total_count=false", nil)
		if requestErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to build voice discovery request"})
			return
		}
		req.Header.Set("xi-api-key", key)
		resp, requestErr := security.NewSafeHTTPClient(s.allowPrivate, 10*time.Second).Do(req)
		if requestErr != nil {
			writeJSON(w, http.StatusOK, map[string]any{"voices": []string{}, "voiceOptions": []any{}, "supported": false})
			return
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			writeJSON(w, http.StatusOK, map[string]any{"voices": []string{}, "voiceOptions": []any{}, "supported": false})
			return
		}
		var payload struct {
			Voices []struct {
				ID     string `json:"voice_id"`
				Name   string `json:"name"`
				Labels struct {
					Language string `json:"language"`
				} `json:"labels"`
				Sharing struct {
					Labels struct {
						Language string `json:"language"`
					} `json:"labels"`
				} `json:"sharing"`
			} `json:"voices"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&payload); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"voices": []string{}, "voiceOptions": []any{}, "supported": false})
			return
		}
		voices := make([]string, 0, len(payload.Voices))
		options := make([]map[string]string, 0, len(payload.Voices))
		for _, voice := range payload.Voices {
			if strings.TrimSpace(voice.ID) == "" {
				continue
			}
			voices = append(voices, voice.ID)
			name := strings.TrimSpace(voice.Name)
			if name == "" {
				name = voice.ID
			}
			language := strings.TrimSpace(voice.Labels.Language)
			if language == "" {
				language = strings.TrimSpace(voice.Sharing.Labels.Language)
			}
			options = append(options, map[string]string{
				"id":       voice.ID,
				"name":     name,
				"language": language,
			})
		}

		modelOptions := make([]map[string]string, 0)
		modelReq, modelRequestErr := http.NewRequestWithContext(r.Context(), http.MethodGet, strings.TrimRight(elevenLabsAdminAPIBaseURL, "/")+"/v1/models", nil)
		if modelRequestErr == nil {
			modelReq.Header.Set("xi-api-key", key)
			modelResp, modelErr := security.NewSafeHTTPClient(s.allowPrivate, 10*time.Second).Do(modelReq)
			if modelErr == nil {
				func() {
					defer func() { _ = modelResp.Body.Close() }()
					if modelResp.StatusCode < 200 || modelResp.StatusCode >= 300 {
						return
					}
					var models []struct {
						ID       string `json:"model_id"`
						Name     string `json:"name"`
						CanDoTTS bool   `json:"can_do_text_to_speech"`
					}
					if json.NewDecoder(io.LimitReader(modelResp.Body, 8<<20)).Decode(&models) != nil {
						return
					}
					for _, model := range models {
						if !model.CanDoTTS || strings.TrimSpace(model.ID) == "" {
							continue
						}
						name := strings.TrimSpace(model.Name)
						if name == "" {
							name = model.ID
						}
						modelOptions = append(modelOptions, map[string]string{
							"id":   model.ID,
							"name": name,
						})
					}
				}()
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"voices":       voices,
			"voiceOptions": options,
			"modelOptions": modelOptions,
			"supported":    len(voices) > 0,
		})
		return
	}

	baseURL := strings.TrimSpace(cfg.TTSBaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(cfg.LLMBaseURL)
	}
	if baseURL == "" {
		writeJSON(w, http.StatusOK, map[string]any{"voices": []string{}, "supported": false})
		return
	}

	policy := security.SSRFPolicy{AllowPrivate: s.allowPrivate}
	endpoint := strings.TrimRight(baseURL, "/") + "/audio/voices"
	if _, err := policy.ValidateHTTPURL(baseURL); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"voices": []string{}, "supported": false})
		return
	}
	if _, err := policy.ValidateHTTPURL(endpoint); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"voices": []string{}, "supported": false})
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"voices": []string{}, "supported": false})
		return
	}
	if len(cfg.APIKeyCiphertext) > 0 {
		key, err := s.vault.Decrypt(cfg.APIKeyCiphertext)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to decrypt provider api key"})
			return
		}
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}

	resp, err := security.NewSafeHTTPClient(s.allowPrivate, 5*time.Second).Do(req)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"voices": []string{}, "supported": false})
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusOK, map[string]any{"voices": []string{}, "supported": false})
		return
	}

	var payload struct {
		Voices []string `json:"voices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"voices": []string{}, "supported": false})
		return
	}
	voices := uniqueNonEmptyStrings(payload.Voices)
	writeJSON(w, http.StatusOK, map[string]any{"voices": voices, "supported": len(voices) > 0})
}

func (s *Server) HandleDiscoverSTTModels(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.adminStore.GetAgentConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load agent config"})
		return
	}

	baseURL := strings.TrimSpace(cfg.STTBaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(cfg.LLMBaseURL)
	}
	if baseURL == "" {
		writeJSON(w, http.StatusOK, map[string]any{"models": []string{}, "supported": false})
		return
	}

	policy := security.SSRFPolicy{AllowPrivate: s.allowPrivate}
	endpoint := strings.TrimRight(baseURL, "/") + "/models"
	if _, err := policy.ValidateHTTPURL(baseURL); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"models": []string{}, "supported": false})
		return
	}
	if _, err := policy.ValidateHTTPURL(endpoint); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"models": []string{}, "supported": false})
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"models": []string{}, "supported": false})
		return
	}
	if len(cfg.APIKeyCiphertext) > 0 {
		key, err := s.vault.Decrypt(cfg.APIKeyCiphertext)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to decrypt provider api key"})
			return
		}
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}

	resp, err := security.NewSafeHTTPClient(s.allowPrivate, 5*time.Second).Do(req)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"models": []string{}, "supported": false})
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusOK, map[string]any{"models": []string{}, "supported": false})
		return
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"models": []string{}, "supported": false})
		return
	}
	models := prioritizeWhisperModels(payload.Data)
	writeJSON(w, http.StatusOK, map[string]any{"models": models, "supported": len(models) > 0})
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func prioritizeWhisperModels(entries []struct {
	ID string `json:"id"`
}) []string {
	models := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.ID) != "" {
			models = append(models, strings.TrimSpace(entry.ID))
		}
	}
	models = uniqueNonEmptyStrings(models)
	sort.SliceStable(models, func(i, j int) bool {
		return whisperModelPriority(models[i]) < whisperModelPriority(models[j])
	})
	return models
}

func whisperModelPriority(model string) int {
	model = strings.ToLower(model)
	if strings.HasPrefix(model, "systran/faster-whisper-") {
		return 0
	}
	if strings.Contains(model, "whisper") {
		return 1
	}
	return 2
}

func (s *Server) HandleUpdateAgentConfig(w http.ResponseWriter, r *http.Request) {
	s.withHostedMCPAdmin(w, r, func(locked *Server, r *http.Request) { locked.updateAgentConfig(w, r) })
}

func (s *Server) updateAgentConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode                        string    `json:"mode"`
		RealtimeWSURL               *string   `json:"realtimeWsUrl"`
		RealtimeModel               *string   `json:"realtimeModel"`
		STTBaseURL                  *string   `json:"sttBaseUrl"`
		STTModel                    *string   `json:"sttModel"`
		LLMBaseURL                  *string   `json:"llmBaseUrl"`
		LLMModel                    *string   `json:"llmModel"`
		TTSBaseURL                  *string   `json:"ttsBaseUrl"`
		TTSModel                    *string   `json:"ttsModel"`
		TTSVoice                    *string   `json:"ttsVoice"`
		EmbeddingBaseURL            *string   `json:"embeddingBaseUrl"`
		EmbeddingModel              *string   `json:"embeddingModel"`
		EmbeddingDim                *int      `json:"embeddingDim"`
		SystemInstructions          *string   `json:"systemInstructions"`
		APIKey                      string    `json:"apiKey"`
		RAGTopK                     *int      `json:"ragTopK"`
		RAGMinSimilarity            *float64  `json:"ragMinSimilarity"`
		AudioFocusEnabled           *bool     `json:"audioFocusEnabled"`
		AssistantName               *string   `json:"assistantName"`
		WakeAliases                 *[]string `json:"wakeAliases"`
		SpeechProvider              string    `json:"speechProvider"`
		ElevenLabsSTTModel          string    `json:"elevenLabsSttModel"`
		ElevenLabsTTSModel          string    `json:"elevenLabsTtsModel"`
		ElevenLabsAPIKey            string    `json:"elevenLabsApiKey"`
		ElevenLabsAgentID           *string   `json:"elevenLabsAgentId"`
		ElevenLabsRAGSyncEnabled    *bool     `json:"elevenLabsRagSyncEnabled"`
		ElevenLabsSkillsSyncEnabled *bool     `json:"elevenLabsAgentSkillsSyncEnabled"`
	}
	if err := decodeStrictJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	if req.Mode != "realtime" && req.Mode != "cascaded" && req.Mode != "elevenlabs_agent" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be realtime, cascaded, or elevenlabs_agent"})
		return
	}

	policy := security.SSRFPolicy{AllowPrivate: s.allowPrivate}
	for _, u := range []*string{req.STTBaseURL, req.LLMBaseURL, req.TTSBaseURL, req.EmbeddingBaseURL} {
		if u != nil && strings.TrimSpace(*u) != "" {
			if _, err := policy.ValidateHTTPURL(*u); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid or blocked endpoint URL"})
				return
			}
		}
	}
	if req.RealtimeWSURL != nil && strings.TrimSpace(*req.RealtimeWSURL) != "" {
		if _, err := policy.ValidateWebSocketURL(*req.RealtimeWSURL); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid or blocked realtime URL"})
			return
		}
	}

	existing, err := s.adminStore.GetAgentConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load agent config"})
		return
	}
	previous := AgentConfigRecord{ElevenLabsRAGSyncEnabled: true, ElevenLabsSkillsSyncEnabled: true}
	if existing != nil {
		previous = *existing
	}
	rec := &AgentConfigRecord{
		Name:                        "default",
		Mode:                        req.Mode,
		RealtimeWSURL:               updatedConfigValue(req.RealtimeWSURL, previous.RealtimeWSURL),
		RealtimeModel:               updatedConfigValue(req.RealtimeModel, previous.RealtimeModel),
		STTBaseURL:                  updatedConfigValue(req.STTBaseURL, previous.STTBaseURL),
		STTModel:                    updatedConfigValue(req.STTModel, previous.STTModel),
		LLMBaseURL:                  updatedConfigValue(req.LLMBaseURL, previous.LLMBaseURL),
		LLMModel:                    updatedConfigValue(req.LLMModel, previous.LLMModel),
		TTSBaseURL:                  updatedConfigValue(req.TTSBaseURL, previous.TTSBaseURL),
		TTSModel:                    updatedConfigValue(req.TTSModel, previous.TTSModel),
		TTSVoice:                    updatedConfigValue(req.TTSVoice, previous.TTSVoice),
		EmbeddingBaseURL:            updatedConfigValue(req.EmbeddingBaseURL, previous.EmbeddingBaseURL),
		EmbeddingModel:              updatedConfigValue(req.EmbeddingModel, previous.EmbeddingModel),
		EmbeddingDim:                updatedConfigValue(req.EmbeddingDim, previous.EmbeddingDim),
		SystemInstructions:          updatedConfigValue(req.SystemInstructions, previous.SystemInstructions),
		RAGTopK:                     updatedConfigValue(req.RAGTopK, previous.RAGTopK),
		RAGMinSimilarity:            updatedConfigValue(req.RAGMinSimilarity, previous.RAGMinSimilarity),
		SpeechProvider:              normalizeSpeechProvider(req.SpeechProvider),
		ElevenLabsSTTModel:          normalizeElevenLabsSTTModel(req.ElevenLabsSTTModel),
		ElevenLabsTTSModel:          normalizeElevenLabsTTSModel(req.ElevenLabsTTSModel),
		ElevenLabsRAGSyncEnabled:    updatedConfigValue(req.ElevenLabsRAGSyncEnabled, previous.ElevenLabsRAGSyncEnabled),
		ElevenLabsSkillsSyncEnabled: updatedConfigValue(req.ElevenLabsSkillsSyncEnabled, previous.ElevenLabsSkillsSyncEnabled),
	}
	if rec.EmbeddingDim <= 0 {
		rec.EmbeddingDim = 1536
	}
	if strings.TrimSpace(req.ElevenLabsAPIKey) != "" {
		key := strings.TrimSpace(req.ElevenLabsAPIKey)
		if len(key) <= 5 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ElevenLabs API key is too short"})
			return
		}
		ct, err := s.vault.Encrypt(key)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encrypt ElevenLabs api key"})
			return
		}
		rec.ElevenLabsAPIKeyCiphertext = ct
		rec.ElevenLabsAPIKeyLast5 = maskLast5(key)
	}
	if existing != nil {
		rec.ElevenLabsAgentID = existing.ElevenLabsAgentID
		if existing != nil && (rec.ElevenLabsAgentID != existing.ElevenLabsAgentID || req.ElevenLabsAPIKey != "") {
			servers, err := s.adminStore.ListMCPServers(r.Context())
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not check hosted MCP ownership before changing agents."})
				return
			}
			for _, server := range servers {
				if server.HostedResourceID != "" || server.HostedCreationAttempted {
					writeJSON(w, http.StatusConflict, map[string]string{"error": "Disable and verify cleanup of hosted MCP integrations before changing the ElevenLabs agent or key."})
					return
				}
			}
		}
		if rec.Mode == "elevenlabs_agent" {
			rec.RealtimeWSURL, rec.RealtimeModel = existing.RealtimeWSURL, existing.RealtimeModel
			rec.STTBaseURL, rec.STTModel = existing.STTBaseURL, existing.STTModel
			rec.LLMBaseURL, rec.LLMModel = existing.LLMBaseURL, existing.LLMModel
			rec.TTSBaseURL, rec.TTSModel, rec.TTSVoice = existing.TTSBaseURL, existing.TTSModel, existing.TTSVoice
			rec.EmbeddingBaseURL, rec.EmbeddingModel, rec.EmbeddingDim = existing.EmbeddingBaseURL, existing.EmbeddingModel, existing.EmbeddingDim
			rec.RAGTopK, rec.RAGMinSimilarity = existing.RAGTopK, existing.RAGMinSimilarity
			rec.SpeechProvider = existing.SpeechProvider
			rec.ElevenLabsSTTModel, rec.ElevenLabsTTSModel = existing.ElevenLabsSTTModel, existing.ElevenLabsTTSModel
		}
		if rec.ElevenLabsAPIKeyLast5 == "" {
			rec.ElevenLabsAPIKeyLast5 = existing.ElevenLabsAPIKeyLast5
		}
		if req.SpeechProvider == "" {
			rec.SpeechProvider = normalizeSpeechProvider(existing.SpeechProvider)
		}
		if req.ElevenLabsSTTModel == "" {
			rec.ElevenLabsSTTModel = normalizeElevenLabsSTTModel(existing.ElevenLabsSTTModel)
		}
		if req.ElevenLabsTTSModel == "" {
			rec.ElevenLabsTTSModel = normalizeElevenLabsTTSModel(existing.ElevenLabsTTSModel)
		}
	}
	if req.ElevenLabsAgentID != nil {
		rec.ElevenLabsAgentID = strings.TrimSpace(*req.ElevenLabsAgentID)
		if len(rec.ElevenLabsAgentID) > 128 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ElevenLabs agent ID is too long"})
			return
		}
	}
	if rec.Mode == "cascaded" && rec.SpeechProvider == "elevenlabs" {
		hasKey := len(rec.ElevenLabsAPIKeyCiphertext) > 0 || (existing != nil && len(existing.ElevenLabsAPIKeyCiphertext) > 0)
		if !hasKey {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "configure an ElevenLabs API key before selecting ElevenLabs"})
			return
		}
		vl, _, vlErr := s.effectiveVoiceLanguages(r.Context())
		if vlErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to validate voice languages"})
			return
		}
		if err := agent.ValidateVoiceLanguageSettingsForProvider(vl, rec.SpeechProvider); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	if rec.RAGTopK <= 0 {
		rec.RAGTopK = 5
	}
	rec.AudioFocusEnabled = true
	rec.AssistantName = agent.DefaultAssistantName
	rec.WakeAliases = []string{}
	if existing != nil {
		rec.AudioFocusEnabled = existing.AudioFocusEnabled
		if trimmedExisting := strings.TrimSpace(existing.AssistantName); trimmedExisting != "" {
			rec.AssistantName = trimmedExisting
		}
		if len(existing.WakeAliases) > 0 {
			rec.WakeAliases = append([]string{}, existing.WakeAliases...)
		}
	}
	if req.AudioFocusEnabled != nil {
		rec.AudioFocusEnabled = *req.AudioFocusEnabled
	}
	if req.AssistantName != nil {
		trimmedName := strings.TrimSpace(*req.AssistantName)
		if trimmedName == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "assistantName must not be empty"})
			return
		}
		if utf8.RuneCountInString(trimmedName) > 64 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "assistantName must be 64 characters or fewer"})
			return
		}
		rec.AssistantName = trimmedName
	}
	aliasSource := rec.WakeAliases
	if req.WakeAliases != nil {
		if len(*req.WakeAliases) > 32 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "maximum of 16 wake aliases allowed"})
			return
		}
		aliasSource = *req.WakeAliases
	}
	normalizedAliases, err := normalizeWakeAliases(rec.AssistantName, aliasSource)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	rec.WakeAliases = normalizedAliases

	if strings.TrimSpace(req.APIKey) != "" {
		ct, err := s.vault.Encrypt(req.APIKey)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encrypt api key"})
			return
		}
		rec.APIKeyCiphertext = ct
		rec.APIKeyLast4 = security.MaskLast4(req.APIKey)
	}

	if rec.Mode == "elevenlabs_agent" {
		candidate := *rec
		if len(candidate.ElevenLabsAPIKeyCiphertext) == 0 && existing != nil {
			candidate.ElevenLabsAPIKeyCiphertext = existing.ElevenLabsAPIKeyCiphertext
		}
		if !candidate.ElevenLabsRAGSyncEnabled {
			if err := s.cleanupDisabledHostedRAG(r.Context(), &candidate); err != nil {
				writeJSON(w, hostedErrorStatus(err), map[string]string{"error": err.Error()})
				return
			}
		}
		if err := s.syncHostedResources(r.Context(), &candidate); err != nil {
			writeJSON(w, hostedErrorStatus(err), map[string]string{"error": err.Error()})
			return
		}
	}
	if err := s.adminStore.UpsertAgentConfig(r.Context(), rec); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save agent config"})
		return
	}
	s.audit(r.Context(), r, "admin.agent_config.update", "agent_config", rec.ID, "success", map[string]any{
		"mode":                             rec.Mode,
		"audioFocusEnabled":                rec.AudioFocusEnabled,
		"assistantName":                    rec.AssistantName,
		"wakeAliases":                      rec.WakeAliases,
		"speechProvider":                   rec.SpeechProvider,
		"elevenLabsRagSyncEnabled":         rec.ElevenLabsRAGSyncEnabled,
		"elevenLabsAgentSkillsSyncEnabled": rec.ElevenLabsSkillsSyncEnabled,
		"elevenLabsKeySet":                 len(rec.ElevenLabsAPIKeyCiphertext) > 0 || (existing != nil && len(existing.ElevenLabsAPIKeyCiphertext) > 0),
	})
	writeJSON(w, http.StatusOK, map[string]any{"config": rec})
}

func updatedConfigValue[T any](value *T, previous T) T {
	if value != nil {
		return *value
	}
	return previous
}

func (s *Server) effectiveVoiceLanguages(ctx context.Context) (agent.VoiceLanguageSettings, bool, error) {
	vl, saved, err := s.adminStore.GetVoiceLanguages(ctx)
	if err != nil {
		return agent.VoiceLanguageSettings{}, false, err
	}
	if !saved {
		cfg, cfgErr := s.adminStore.GetAgentConfig(ctx)
		englishVoice := ""
		if cfgErr == nil && cfg != nil {
			englishVoice = cfg.TTSVoice
		}
		return agent.InitialVoiceLanguageSettings(englishVoice), false, nil
	}
	return vl, true, nil
}

func (s *Server) HandleGetVoiceLanguages(w http.ResponseWriter, r *http.Request) {
	vl, saved, err := s.effectiveVoiceLanguages(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load voice languages"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"languages":       vl.Languages,
		"defaultLanguage": vl.DefaultCode,
		"saved":           saved,
		"catalog":         agent.LanguageCatalog(),
	})
}

func (s *Server) HandleUpdateVoiceLanguages(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Languages       []agent.VoiceLanguage `json:"languages"`
		DefaultLanguage string                `json:"defaultLanguage"`
	}
	if err := decodeStrictJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	next := agent.VoiceLanguageSettings{
		DefaultCode: strings.ToLower(strings.TrimSpace(req.DefaultLanguage)),
		Languages:   make([]agent.VoiceLanguage, 0, len(req.Languages)),
	}
	for _, l := range req.Languages {
		code := strings.ToLower(strings.TrimSpace(l.Code))
		name := strings.TrimSpace(l.Name)
		if entry, ok := agent.LookupCatalogLanguage(code); ok {
			name = entry.Name
		}
		next.Languages = append(next.Languages, agent.VoiceLanguage{
			Code:            code,
			Name:            name,
			Voice:           strings.TrimSpace(l.Voice),
			ElevenLabsVoice: strings.TrimSpace(l.ElevenLabsVoice),
		})
	}

	cfg, cfgErr := s.adminStore.GetAgentConfig(r.Context())
	if cfgErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load agent config"})
		return
	}
	if err := agent.ValidateVoiceLanguageSettingsForProvider(next, normalizeSpeechProvider(cfg.SpeechProvider)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	prev, _, _ := s.effectiveVoiceLanguages(r.Context())
	prevSet := make(map[string]bool, len(prev.Languages))
	for _, l := range prev.Languages {
		prevSet[l.Code] = true
	}
	nextSet := make(map[string]bool, len(next.Languages))
	var added []string
	for _, l := range next.Languages {
		nextSet[l.Code] = true
		if !prevSet[l.Code] {
			added = append(added, l.Code)
		}
	}
	var removed []string
	for _, l := range prev.Languages {
		if !nextSet[l.Code] {
			removed = append(removed, l.Code)
		}
	}

	if err := s.adminStore.ReplaceVoiceLanguages(r.Context(), next); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save voice languages"})
		return
	}
	s.audit(r.Context(), r, "admin.voice_languages.update", "voice_languages", "default", "success", map[string]any{
		"added":           added,
		"removed":         removed,
		"defaultLanguage": next.DefaultCode,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"languages":       next.Languages,
		"defaultLanguage": next.DefaultCode,
		"saved":           true,
		"catalog":         agent.LanguageCatalog(),
	})
}

func maskLast5(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 5 {
		return strings.Repeat("*", len(key))
	}
	return "****" + key[len(key)-5:]
}

func (s *Server) HandleTestAgentConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.adminStore.GetAgentConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load config"})
		return
	}
	var candidate struct {
		Mode    string  `json:"mode"`
		AgentID *string `json:"elevenLabsAgentId"`
		Key     string  `json:"elevenLabsApiKey"`
	}
	if err := decodeStrictJSON(r, &candidate); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid test configuration"})
		return
	}
	if candidate.Mode != "" {
		if candidate.Mode != "elevenlabs_agent" && candidate.Mode != "cascaded" && candidate.Mode != "realtime" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid conversation mode"})
			return
		}
		cfg.Mode = candidate.Mode
	}
	if cfg.Mode == "elevenlabs_agent" {
		if candidate.AgentID != nil {
			cfg.ElevenLabsAgentID = strings.TrimSpace(*candidate.AgentID)
		}
		if key := strings.TrimSpace(candidate.Key); key != "" {
			if len(key) <= 5 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ElevenLabs API key is too short"})
				return
			}
			ciphertext, err := s.vault.Encrypt(key)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not prepare the ElevenLabs credential."})
				return
			}
			cfg.ElevenLabsAPIKeyCiphertext = ciphertext
		}
		s.testHostedConfig(w, r, cfg)
		return
	}
	target := cfg.LLMBaseURL
	if cfg.Mode == "realtime" && cfg.RealtimeWSURL != "" {
		target = cfg.RealtimeWSURL
	}
	if target == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "no endpoint configured"})
		return
	}

	policy := security.SSRFPolicy{AllowPrivate: s.allowPrivate}
	if _, err := policy.ValidateWebSocketURL(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "endpoint blocked by security policy"})
		return
	}

	if strings.HasPrefix(target, "http") {
		client := security.NewSafeHTTPClient(s.allowPrivate, 5*time.Second)
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, strings.TrimRight(target, "/")+"/models", nil)
		if len(cfg.APIKeyCiphertext) > 0 {
			if key, err := s.vault.Decrypt(cfg.APIKeyCiphertext); err == nil && key != "" {
				req.Header.Set("Authorization", "Bearer "+key)
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "endpoint unreachable"})
			return
		}
		_ = resp.Body.Close()
		writeJSON(w, http.StatusOK, map[string]any{"ok": resp.StatusCode < 500, "statusCode": resp.StatusCode})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "realtime URL validated"})
}

func (s *Server) HandleListMCPServers(w http.ResponseWriter, r *http.Request) {
	servers, err := s.adminStore.ListMCPServers(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list mcp servers"})
		return
	}
	for i := range servers {
		normalizeHostedMCP(&servers[i])
	}
	_, proxyErr := hostedPublicHTTPS(s.hostedProxyBaseURL, true)
	writeJSON(w, http.StatusOK, map[string]any{"servers": servers, "hostedProxyAvailable": proxyErr == nil})
}

func (s *Server) HandleUpsertMCPServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req hostedMCPAdminRequest
	if err := decodeStrictJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	if req.Transport != "streamable_http" && req.Transport != "sse" {
		req.Transport = "streamable_http"
	}
	policy := security.SSRFPolicy{AllowPrivate: s.allowPrivate}
	if _, err := policy.ValidateHTTPURL(req.EndpointURL); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mcp endpoint blocked or invalid"})
		return
	}

	rec := &MCPServerRecord{
		ID:           id,
		Name:         strings.TrimSpace(req.Name),
		Transport:    req.Transport,
		EndpointURL:  req.EndpointURL,
		Enabled:      req.Enabled,
		AllowedTools: req.AllowedTools,
		TimeoutMs:    req.TimeoutMs,
	}
	if rec.TimeoutMs <= 0 {
		rec.TimeoutMs = 10000
	}
	s.withHostedMCPAdmin(w, r, func(locked *Server, r *http.Request) {
		locked.saveHostedMCP(w, r, rec, req)
	})
}

func (s *Server) HandleDiscoverMCPServer(w http.ResponseWriter, r *http.Request) {
	s.withHostedMCPAdmin(w, r, func(locked *Server, r *http.Request) { locked.discoverHostedMCP(w, r) })
}

func (s *Server) discoverHostedMCP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	srv, err := s.adminStore.GetMCPServer(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "mcp server not found"})
		return
	}
	var authHdr string
	if len(srv.AuthHeaderCiphertext) > 0 {
		authHdr, err = s.vault.Decrypt(srv.AuthHeaderCiphertext)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not load MCP authentication."})
			return
		}
	}
	tools, err := s.mcpHost.DiscoverTools(r.Context(), mcp.ServerConfig{
		ID:          srv.ID,
		Name:        srv.Name,
		Transport:   srv.Transport,
		EndpointURL: srv.EndpointURL,
		AuthHeader:  authHdr,
		Enabled:     srv.Enabled,
		TimeoutMs:   srv.TimeoutMs,
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "mcp discovery failed"})
		return
	}
	srv.DiscoveredTools = tools
	if srv.HostedMode != "disabled" {
		srv.HostedSyncStatus = "pending"
	}
	updated, err := s.adminStore.UpsertMCPServer(r.Context(), srv)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save discovered tools"})
		return
	}
	if err := s.syncChangedHostedMCP(r.Context(), updated); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"server": updated, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"server": updated, "tools": tools})
}

func (s *Server) HandleDeleteMCPServer(w http.ResponseWriter, r *http.Request) {
	s.withHostedMCPAdmin(w, r, func(locked *Server, r *http.Request) { locked.deleteHostedMCP(w, r) })
}

func (s *Server) HandleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := s.adminStore.ListAuditLogs(r.Context(), 100, 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list audit logs"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs})
}

func (s *Server) HandleRTCConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.adminStore.GetAgentConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load voice session config"})
		return
	}
	normalizeAgentConfigDefaults(cfg)
	writeJSON(w, http.StatusOK, map[string]any{
		"audioFocusEnabled": cfg.AudioFocusEnabled,
		"assistantName":     cfg.AssistantName,
		"mode":              cfg.Mode,
		"capabilities": map[string]bool{
			"camera":         cfg.Mode != "elevenlabs_agent",
			"localKnowledge": cfg.Mode != "elevenlabs_agent",
			"strictWakeGate": cfg.Mode != "elevenlabs_agent",
		},
	})
}

func (s *Server) HandleRTCOffer(w http.ResponseWriter, r *http.Request) {
	config, err := s.adminStore.GetAgentConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load voice configuration"})
		return
	}
	if config.Mode == "elevenlabs_agent" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Hosted mode uses a provider conversation token, not backend signaling."})
		return
	}
	var req struct {
		SignalingToken string `json:"signalingToken"`
		SDP            string `json:"sdp"`
	}
	if err := decodeStrictJSON(r, &req); err != nil || req.SDP == "" || req.SignalingToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid offer payload"})
		return
	}

	tok, user, err := s.authSvc.ConsumeSignalingToken(r.Context(), req.SignalingToken)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired signaling token"})
		return
	}

	peerID, answerSDP, err := s.rtcMgr.HandleOffer(r.Context(), user.ID, tok.SessionID, req.SDP, func(ps *rtc.PeerSession) {
		s.attachSessionAgent(ps)
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to establish webrtc session"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"peerId": peerID,
		"sdp":    answerSDP,
	})
}

func (s *Server) activeEmbedder(ctx context.Context) (rag.Embedder, error) {
	cfg, err := s.adminStore.GetAgentConfig(ctx)
	if err != nil || cfg.EmbeddingBaseURL == "" {
		return nil, errors.New("embedding endpoint not configured")
	}
	var key string
	if len(cfg.APIKeyCiphertext) > 0 {
		key, _ = s.vault.Decrypt(cfg.APIKeyCiphertext)
	}
	return rag.NewOpenAIEmbedder(cfg.EmbeddingBaseURL, key, cfg.EmbeddingModel, s.allowPrivate)
}

func (s *Server) attachSessionAgent(ps *rtc.PeerSession) {
	ctx := ps.Context()
	cfgRec, err := s.adminStore.GetAgentConfig(ctx)
	if err != nil {
		ps.SendEvent(rtc.NewErrorEvent("Failed to load AI configuration."))
		return
	}
	if cfgRec.Mode == "elevenlabs_agent" {
		ps.SendEvent(rtc.NewErrorEvent("Conversation mode changed. End this call and start again."))
		ps.Close()
		return
	}
	var apiKey string
	if len(cfgRec.APIKeyCiphertext) > 0 {
		apiKey, _ = s.vault.Decrypt(cfgRec.APIKeyCiphertext)
	}
	var elevenLabsAPIKey string
	if len(cfgRec.ElevenLabsAPIKeyCiphertext) > 0 {
		var decryptErr error
		elevenLabsAPIKey, decryptErr = s.vault.Decrypt(cfgRec.ElevenLabsAPIKeyCiphertext)
		if decryptErr != nil {
			ps.SendEvent(rtc.NewErrorEvent("Failed to load ElevenLabs configuration."))
			return
		}
	}

	mcpRecs, err := s.adminStore.ListMCPServers(ctx)
	if err != nil {
		ps.SendEvent(rtc.NewErrorEvent("Failed to load MCP server configuration."))
		return
	}
	var mcpConfigs []mcp.ServerConfig
	for _, m := range mcpRecs {
		var hdr string
		if len(m.AuthHeaderCiphertext) > 0 {
			hdr, _ = s.vault.Decrypt(m.AuthHeaderCiphertext)
		}
		mcpConfigs = append(mcpConfigs, mcp.ServerConfig{
			ID:              m.ID,
			Name:            m.Name,
			Transport:       m.Transport,
			EndpointURL:     m.EndpointURL,
			AuthHeader:      hdr,
			Enabled:         m.Enabled,
			AllowedTools:    m.AllowedTools,
			DiscoveredTools: m.DiscoveredTools,
			TimeoutMs:       m.TimeoutMs,
		})
	}

	skillRecords, err := s.adminStore.ListAgentSkills(ctx)
	if err != nil {
		ps.SendEvent(rtc.NewErrorEvent("Failed to load Agent Skills."))
		return
	}
	var agentSkills []agent.AgentSkill
	for _, skill := range skillRecords {
		if !skill.Enabled {
			continue
		}
		agentSkills = append(agentSkills, agent.AgentSkill{
			Name:           skill.Name,
			Description:    skill.Description,
			Instructions:   skill.Instructions,
			ReferenceFiles: skill.ReferenceFiles,
			AllowedTools:   skill.AllowedTools,
			Enabled:        skill.Enabled,
		})
	}

	normalizeAgentConfigDefaults(cfgRec)
	vl, _, _ := s.effectiveVoiceLanguages(ctx)
	emb, _ := s.activeEmbedder(ctx)
	deps := agent.Dependencies{
		Config: agent.AgentConfig{
			Mode:               cfgRec.Mode,
			RealtimeWSURL:      cfgRec.RealtimeWSURL,
			RealtimeModel:      cfgRec.RealtimeModel,
			STTBaseURL:         cfgRec.STTBaseURL,
			STTModel:           cfgRec.STTModel,
			LLMBaseURL:         cfgRec.LLMBaseURL,
			LLMModel:           cfgRec.LLMModel,
			TTSBaseURL:         cfgRec.TTSBaseURL,
			TTSModel:           cfgRec.TTSModel,
			TTSVoice:           cfgRec.TTSVoice,
			EmbeddingBaseURL:   cfgRec.EmbeddingBaseURL,
			EmbeddingModel:     cfgRec.EmbeddingModel,
			SystemInstructions: cfgRec.SystemInstructions,
			APIKey:             apiKey,
			RAGTopK:            cfgRec.RAGTopK,
			RAGMinSimilarity:   cfgRec.RAGMinSimilarity,
			AudioFocusEnabled:  cfgRec.AudioFocusEnabled,
			AssistantName:      cfgRec.AssistantName,
			WakeAliases:        append([]string{}, cfgRec.WakeAliases...),
			AllowPrivate:       s.allowPrivate,
			VoiceLanguages:     vl,
			SpeechProvider:     normalizeSpeechProvider(cfgRec.SpeechProvider),
			ElevenLabsSTTModel: normalizeElevenLabsSTTModel(cfgRec.ElevenLabsSTTModel),
			ElevenLabsTTSModel: normalizeElevenLabsTTSModel(cfgRec.ElevenLabsTTSModel),
			ElevenLabsAPIKey:   elevenLabsAPIKey,
		},
		Sink:       ps,
		RAGService: s.ragSvc,
		Embedder:   emb,
		MCPHost:    s.mcpHost,
		MCPServers: mcpConfigs,
		Skills:     agentSkills,
	}

	var sa agent.SessionAgent
	if cfgRec.Mode == "realtime" {
		sa = agent.NewRealtimeAdapter(deps)
		ps.OnBargeIn = sa.BargeIn
		ps.OnClose = func() { _ = sa.Close() }
		_ = sa.Start(ctx)
		return
	}

	cascaded := agent.NewCascadedAdapter(deps)
	sa = cascaded
	ps.OnBargeIn = sa.BargeIn
	ps.OnClose = func() { _ = sa.Close() }
	if err := sa.Start(ctx); err != nil {
		return
	}

	var bufMu sync.Mutex
	var speaking bool
	var frames [][]byte
	const preRollMax = 15
	const maxUtteranceFrames = 1500

	ps.OnSpeechStart = func() {
		bufMu.Lock()
		speaking = true
		bufMu.Unlock()
	}

	ps.OnSpeechEnd = func() {
		bufMu.Lock()
		if !speaking || len(frames) < 5 {
			speaking = false
			frames = nil
			bufMu.Unlock()
			return
		}
		captured := make([][]byte, len(frames))
		copy(captured, frames)
		speaking = false
		frames = nil
		bufMu.Unlock()

		oggAudio := agent.EncodeOggOpus(captured)
		go func() {
			start := time.Now()
			err := cascaded.ProcessTurn(ctx, oggAudio)
			detectedLang, replyLang, fallback := cascaded.LastTurnLanguage()
			switch {
			case err == nil:
				s.logger.Info("cascaded turn complete", "peerId", ps.ID, "frames", len(captured), "detectedLang", detectedLang, "replyLang", replyLang, "fallback", fallback, "took", time.Since(start).String())
			case errors.Is(err, context.Canceled):
				s.logger.Info("cascaded turn cancelled by barge-in", "peerId", ps.ID, "took", time.Since(start).String())
			default:
				s.logger.Warn("cascaded turn failed", "peerId", ps.ID, "detectedLang", detectedLang, "replyLang", replyLang, "fallback", fallback, "error", err, "took", time.Since(start).String())
			}
		}()
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case f, ok := <-ps.AudioIn:
				if !ok {
					return
				}
				cp := make([]byte, len(f.Payload))
				copy(cp, f.Payload)
				bufMu.Lock()
				frames = append(frames, cp)
				if !speaking && len(frames) > preRollMax {
					frames = frames[len(frames)-preRollMax:]
				} else if speaking && len(frames) > maxUtteranceFrames {
					frames = frames[len(frames)-maxUtteranceFrames:]
				}
				bufMu.Unlock()
			}
		}
	}()
}
