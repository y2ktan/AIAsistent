package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"aegis-voice/server/internal/agent"
	"aegis-voice/server/internal/auth"
	"aegis-voice/server/internal/mcp"
	"aegis-voice/server/internal/rag"
	"aegis-voice/server/internal/rtc"
	"aegis-voice/server/internal/security"
)

type Server struct {
	authSvc      *auth.Service
	authStore    auth.Store
	adminStore   AdminStore
	ragSvc       *rag.Service
	rtcMgr       *rtc.Manager
	mcpHost      *mcp.Host
	vault        *security.Vault
	allowPrivate bool
	logger       *slog.Logger
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
	return &Server{
		authSvc:      authSvc,
		authStore:    authStore,
		adminStore:   adminStore,
		ragSvc:       ragSvc,
		rtcMgr:       rtcMgr,
		mcpHost:      mcpHost,
		vault:        vault,
		allowPrivate: allowPrivate,
		logger:       logger,
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
	return dec.Decode(dst)
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
	writeJSON(w, http.StatusCreated, map[string]any{"document": doc})
}

func (s *Server) HandleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	docID := r.PathValue("id")
	if err := s.ragSvc.Delete(r.Context(), docID); err != nil {
		if errors.Is(err, rag.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "document not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete document"})
		return
	}
	s.audit(r.Context(), r, "admin.rag.delete", "document", docID, "success", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) HandleGetAgentConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.adminStore.GetAgentConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load agent config"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": cfg})
}

func (s *Server) HandleDiscoverTTSVoices(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.adminStore.GetAgentConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load agent config"})
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
	var req struct {
		Mode               string  `json:"mode"`
		RealtimeWSURL      string  `json:"realtimeWsUrl"`
		RealtimeModel      string  `json:"realtimeModel"`
		STTBaseURL         string  `json:"sttBaseUrl"`
		STTModel           string  `json:"sttModel"`
		LLMBaseURL         string  `json:"llmBaseUrl"`
		LLMModel           string  `json:"llmModel"`
		TTSBaseURL         string  `json:"ttsBaseUrl"`
		TTSModel           string  `json:"ttsModel"`
		TTSVoice           string  `json:"ttsVoice"`
		EmbeddingBaseURL   string  `json:"embeddingBaseUrl"`
		EmbeddingModel     string  `json:"embeddingModel"`
		EmbeddingDim       int     `json:"embeddingDim"`
		SystemInstructions string  `json:"systemInstructions"`
		APIKey             string  `json:"apiKey"`
		RAGTopK            int     `json:"ragTopK"`
		RAGMinSimilarity   float64 `json:"ragMinSimilarity"`
	}
	if err := decodeStrictJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	if req.Mode != "realtime" && req.Mode != "cascaded" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be realtime or cascaded"})
		return
	}

	policy := security.SSRFPolicy{AllowPrivate: s.allowPrivate}
	for _, u := range []string{req.STTBaseURL, req.LLMBaseURL, req.TTSBaseURL, req.EmbeddingBaseURL} {
		if strings.TrimSpace(u) != "" {
			if _, err := policy.ValidateHTTPURL(u); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid or blocked endpoint URL"})
				return
			}
		}
	}
	if strings.TrimSpace(req.RealtimeWSURL) != "" {
		if _, err := policy.ValidateWebSocketURL(req.RealtimeWSURL); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid or blocked realtime URL"})
			return
		}
	}

	rec := &AgentConfigRecord{
		Name:               "default",
		Mode:               req.Mode,
		RealtimeWSURL:      req.RealtimeWSURL,
		RealtimeModel:      req.RealtimeModel,
		STTBaseURL:         req.STTBaseURL,
		STTModel:           req.STTModel,
		LLMBaseURL:         req.LLMBaseURL,
		LLMModel:           req.LLMModel,
		TTSBaseURL:         req.TTSBaseURL,
		TTSModel:           req.TTSModel,
		TTSVoice:           req.TTSVoice,
		EmbeddingBaseURL:   req.EmbeddingBaseURL,
		EmbeddingModel:     req.EmbeddingModel,
		EmbeddingDim:       req.EmbeddingDim,
		SystemInstructions: req.SystemInstructions,
		RAGTopK:            req.RAGTopK,
		RAGMinSimilarity:   req.RAGMinSimilarity,
	}
	if rec.EmbeddingDim <= 0 {
		rec.EmbeddingDim = 1536
	}
	if rec.RAGTopK <= 0 {
		rec.RAGTopK = 5
	}

	if strings.TrimSpace(req.APIKey) != "" {
		ct, err := s.vault.Encrypt(req.APIKey)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encrypt api key"})
			return
		}
		rec.APIKeyCiphertext = ct
		rec.APIKeyLast4 = security.MaskLast4(req.APIKey)
	}

	if err := s.adminStore.UpsertAgentConfig(r.Context(), rec); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save agent config"})
		return
	}
	s.audit(r.Context(), r, "admin.agent_config.update", "agent_config", rec.ID, "success", map[string]any{"mode": rec.Mode})
	writeJSON(w, http.StatusOK, map[string]any{"config": rec})
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
			Code:  code,
			Name:  name,
			Voice: strings.TrimSpace(l.Voice),
		})
	}

	if err := agent.ValidateVoiceLanguageSettings(next); err != nil {
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

func (s *Server) HandleTestAgentConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.adminStore.GetAgentConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load config"})
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
	writeJSON(w, http.StatusOK, map[string]any{"servers": servers})
}

func (s *Server) HandleUpsertMCPServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Name         string   `json:"name"`
		Transport    string   `json:"transport"`
		EndpointURL  string   `json:"endpointUrl"`
		AuthHeader   string   `json:"authHeader"`
		Enabled      bool     `json:"enabled"`
		AllowedTools []string `json:"allowedTools"`
		TimeoutMs    int      `json:"timeoutMs"`
	}
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
	if strings.TrimSpace(req.AuthHeader) != "" {
		ct, err := s.vault.Encrypt(req.AuthHeader)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encrypt auth header"})
			return
		}
		rec.AuthHeaderCiphertext = ct
		rec.AuthHeaderLast4 = security.MaskLast4(req.AuthHeader)
	}

	saved, err := s.adminStore.UpsertMCPServer(r.Context(), rec)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save mcp server"})
		return
	}
	s.audit(r.Context(), r, "admin.mcp.upsert", "mcp_server", saved.ID, "success", map[string]any{"name": saved.Name, "enabled": saved.Enabled})
	writeJSON(w, http.StatusOK, map[string]any{"server": saved})
}

func (s *Server) HandleDiscoverMCPServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	srv, err := s.adminStore.GetMCPServer(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "mcp server not found"})
		return
	}
	var authHdr string
	if len(srv.AuthHeaderCiphertext) > 0 {
		authHdr, _ = s.vault.Decrypt(srv.AuthHeaderCiphertext)
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
	updated, err := s.adminStore.UpsertMCPServer(r.Context(), srv)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save discovered tools"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"server": updated, "tools": tools})
}

func (s *Server) HandleDeleteMCPServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.adminStore.DeleteMCPServer(r.Context(), id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "mcp server not found"})
		return
	}
	s.audit(r.Context(), r, "admin.mcp.delete", "mcp_server", id, "success", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) HandleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := s.adminStore.ListAuditLogs(r.Context(), 100, 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list audit logs"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs})
}

func (s *Server) HandleRTCOffer(w http.ResponseWriter, r *http.Request) {
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
	var apiKey string
	if len(cfgRec.APIKeyCiphertext) > 0 {
		apiKey, _ = s.vault.Decrypt(cfgRec.APIKeyCiphertext)
	}

	mcpRecs, _ := s.adminStore.ListMCPServers(ctx)
	var mcpConfigs []mcp.ServerConfig
	for _, m := range mcpRecs {
		var hdr string
		if len(m.AuthHeaderCiphertext) > 0 {
			hdr, _ = s.vault.Decrypt(m.AuthHeaderCiphertext)
		}
		mcpConfigs = append(mcpConfigs, mcp.ServerConfig{
			ID:           m.ID,
			Name:         m.Name,
			Transport:    m.Transport,
			EndpointURL:  m.EndpointURL,
			AuthHeader:   hdr,
			Enabled:      m.Enabled,
			AllowedTools: m.AllowedTools,
			TimeoutMs:    m.TimeoutMs,
		})
	}

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
			AllowPrivate:       s.allowPrivate,
			VoiceLanguages:     vl,
		},
		Sink:       ps,
		RAGService: s.ragSvc,
		Embedder:   emb,
		MCPHost:    s.mcpHost,
		MCPServers: mcpConfigs,
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
		// Only interrupt when the assistant is audibly speaking; speech (or noise)
		// while it is thinking must not cancel the in-flight LLM request.
		if cascaded.IsSpeaking() {
			cascaded.BargeIn()
		}
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
