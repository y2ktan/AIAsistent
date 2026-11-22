package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aegis-voice/server/internal/agent"
	"aegis-voice/server/internal/auth"
	"aegis-voice/server/internal/skills"
)

type PGAdminStore struct {
	pool *pgxpool.Pool
}

func NewPGAdminStore(pool *pgxpool.Pool) *PGAdminStore {
	return &PGAdminStore{pool: pool}
}

func (s *PGAdminStore) ListUsers(ctx context.Context) ([]auth.User, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, email, role, is_active, created_at, updated_at
		FROM users ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []auth.User
	for rows.Next() {
		var u auth.User
		if err := rows.Scan(&u.ID, &u.Email, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *PGAdminStore) CreateUser(ctx context.Context, email, passwordHash, role string) (*auth.User, error) {
	u := &auth.User{}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, role, is_active)
		VALUES ($1, $2, $3, TRUE)
		RETURNING id::text, email, role, is_active, created_at, updated_at
	`, email, passwordHash, role).Scan(&u.ID, &u.Email, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

func (s *PGAdminStore) UpdateUser(ctx context.Context, userID, role string, isActive bool) (*auth.User, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentRole string
	var currentActive bool
	err = tx.QueryRow(ctx, `SELECT role, is_active FROM users WHERE id = $1::uuid`, userID).Scan(&currentRole, &currentActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if currentRole == auth.RoleAdmin && currentActive && (role != auth.RoleAdmin || !isActive) {
		var remaining int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND is_active = TRUE AND id <> $1::uuid`, userID).Scan(&remaining); err != nil {
			return nil, err
		}
		if remaining == 0 {
			return nil, ErrLastAdmin
		}
	}

	u := &auth.User{}
	err = tx.QueryRow(ctx, `
		UPDATE users SET role = $2, is_active = $3, updated_at = NOW()
		WHERE id = $1::uuid
		RETURNING id::text, email, role, is_active, created_at, updated_at
	`, userID, role, isActive).Scan(&u.ID, &u.Email, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *PGAdminStore) GetAgentConfig(ctx context.Context) (*AgentConfigRecord, error) {
	r := &AgentConfigRecord{}
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, name, mode, realtime_ws_url, realtime_model,
		       stt_base_url, stt_model, llm_base_url, llm_model,
		       tts_base_url, tts_model, tts_voice,
		       embedding_base_url, embedding_model, embedding_dim,
		       system_instructions, api_key_ciphertext, api_key_last4,
		       rag_top_k, rag_min_similarity, audio_focus_enabled,
		       assistant_name, wake_aliases, speech_provider,
		       elevenlabs_stt_model, elevenlabs_tts_model,
		       elevenlabs_api_key_ciphertext, elevenlabs_api_key_last5, updated_at
		FROM agent_configs WHERE name = 'default'
	`).Scan(
		&r.ID, &r.Name, &r.Mode, &r.RealtimeWSURL, &r.RealtimeModel,
		&r.STTBaseURL, &r.STTModel, &r.LLMBaseURL, &r.LLMModel,
		&r.TTSBaseURL, &r.TTSModel, &r.TTSVoice,
		&r.EmbeddingBaseURL, &r.EmbeddingModel, &r.EmbeddingDim,
		&r.SystemInstructions, &r.APIKeyCiphertext, &r.APIKeyLast4,
		&r.RAGTopK, &r.RAGMinSimilarity, &r.AudioFocusEnabled,
		&r.AssistantName, &r.WakeAliases, &r.SpeechProvider,
		&r.ElevenLabsSTTModel, &r.ElevenLabsTTSModel,
		&r.ElevenLabsAPIKeyCiphertext, &r.ElevenLabsAPIKeyLast5, &r.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return &AgentConfigRecord{
			Name:               "default",
			Mode:               "cascaded",
			STTModel:           "whisper-1",
			LLMModel:           "gpt-4o",
			TTSModel:           "tts-1",
			TTSVoice:           "alloy",
			EmbeddingModel:     "text-embedding-3-small",
			EmbeddingDim:       1536,
			RAGTopK:            5,
			RAGMinSimilarity:   0.35,
			AudioFocusEnabled:  true,
			AssistantName:      agent.DefaultAssistantName,
			WakeAliases:        []string{},
			SpeechProvider:     "local",
			ElevenLabsSTTModel: "scribe_v3",
			ElevenLabsTTSModel: "eleven_flash_v2_5",
		}, nil
	}
	if err != nil {
		return nil, err
	}
	if r.AssistantName == "" {
		r.AssistantName = agent.DefaultAssistantName
	}
	if r.WakeAliases == nil {
		r.WakeAliases = []string{}
	}
	return r, nil
}

func (s *PGAdminStore) UpsertAgentConfig(ctx context.Context, rec *AgentConfigRecord) error {
	assistantName := rec.AssistantName
	if assistantName == "" {
		assistantName = agent.DefaultAssistantName
	}
	wakeAliases := rec.WakeAliases
	if wakeAliases == nil {
		wakeAliases = []string{}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(734215)`); err != nil {
		return err
	}
	provider := normalizeSpeechProvider(rec.SpeechProvider)
	if provider == "elevenlabs" {
		var voice string
		if err := tx.QueryRow(ctx, `
			SELECT elevenlabs_voice
			FROM voice_languages
			WHERE is_default = TRUE
		`).Scan(&voice); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		} else if errors.Is(err, pgx.ErrNoRows) || strings.TrimSpace(voice) == "" {
			return errors.New("the default language must have an ElevenLabs voice")
		}
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO agent_configs (
			name, mode, realtime_ws_url, realtime_model,
			stt_base_url, stt_model, llm_base_url, llm_model,
			tts_base_url, tts_model, tts_voice,
			embedding_base_url, embedding_model, embedding_dim,
			system_instructions, api_key_ciphertext, api_key_last4,
			rag_top_k, rag_min_similarity, audio_focus_enabled,
			assistant_name, wake_aliases, speech_provider,
			elevenlabs_stt_model, elevenlabs_tts_model,
			elevenlabs_api_key_ciphertext, elevenlabs_api_key_last5, updated_at
		) VALUES (
			'default', $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, NOW()
		)
		ON CONFLICT (name) DO UPDATE SET
			mode = EXCLUDED.mode,
			realtime_ws_url = EXCLUDED.realtime_ws_url,
			realtime_model = EXCLUDED.realtime_model,
			stt_base_url = EXCLUDED.stt_base_url,
			stt_model = EXCLUDED.stt_model,
			llm_base_url = EXCLUDED.llm_base_url,
			llm_model = EXCLUDED.llm_model,
			tts_base_url = EXCLUDED.tts_base_url,
			tts_model = EXCLUDED.tts_model,
			tts_voice = EXCLUDED.tts_voice,
			embedding_base_url = EXCLUDED.embedding_base_url,
			embedding_model = EXCLUDED.embedding_model,
			embedding_dim = EXCLUDED.embedding_dim,
			system_instructions = EXCLUDED.system_instructions,
			api_key_ciphertext = COALESCE(EXCLUDED.api_key_ciphertext, agent_configs.api_key_ciphertext),
			api_key_last4 = CASE WHEN EXCLUDED.api_key_last4 <> '' THEN EXCLUDED.api_key_last4 ELSE agent_configs.api_key_last4 END,
			rag_top_k = EXCLUDED.rag_top_k,
			rag_min_similarity = EXCLUDED.rag_min_similarity,
			audio_focus_enabled = EXCLUDED.audio_focus_enabled,
			assistant_name = EXCLUDED.assistant_name,
			wake_aliases = EXCLUDED.wake_aliases,
			speech_provider = EXCLUDED.speech_provider,
			elevenlabs_stt_model = EXCLUDED.elevenlabs_stt_model,
			elevenlabs_tts_model = EXCLUDED.elevenlabs_tts_model,
			elevenlabs_api_key_ciphertext = COALESCE(EXCLUDED.elevenlabs_api_key_ciphertext, agent_configs.elevenlabs_api_key_ciphertext),
			elevenlabs_api_key_last5 = CASE WHEN EXCLUDED.elevenlabs_api_key_last5 <> '' THEN EXCLUDED.elevenlabs_api_key_last5 ELSE agent_configs.elevenlabs_api_key_last5 END,
			updated_at = NOW()
		RETURNING id::text, updated_at
	`,
		rec.Mode, rec.RealtimeWSURL, rec.RealtimeModel,
		rec.STTBaseURL, rec.STTModel, rec.LLMBaseURL, rec.LLMModel,
		rec.TTSBaseURL, rec.TTSModel, rec.TTSVoice,
		rec.EmbeddingBaseURL, rec.EmbeddingModel, rec.EmbeddingDim,
		rec.SystemInstructions, rec.APIKeyCiphertext, rec.APIKeyLast4,
		rec.RAGTopK, rec.RAGMinSimilarity, rec.AudioFocusEnabled,
		assistantName, wakeAliases, provider,
		normalizeElevenLabsSTTModel(rec.ElevenLabsSTTModel), normalizeElevenLabsTTSModel(rec.ElevenLabsTTSModel),
		rec.ElevenLabsAPIKeyCiphertext, rec.ElevenLabsAPIKeyLast5,
	).Scan(&rec.ID, &rec.UpdatedAt)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func normalizeSpeechProvider(provider string) string {
	if provider == "elevenlabs" {
		return provider
	}
	return "local"
}

func normalizeElevenLabsSTTModel(model string) string {
	if strings.TrimSpace(model) == "" {
		return "scribe_v3"
	}
	return model
}

func normalizeElevenLabsTTSModel(model string) string {
	if strings.TrimSpace(model) == "" {
		return "eleven_flash_v2_5"
	}
	return model
}

func (s *PGAdminStore) ListMCPServers(ctx context.Context) ([]MCPServerRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, name, transport, endpoint_url, auth_header_ciphertext, auth_header_last4,
		       enabled, allowed_tools, discovered_tools, timeout_ms, created_at, updated_at
		FROM mcp_servers ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MCPServerRecord
	for rows.Next() {
		var m MCPServerRecord
		var toolsJSON []byte
		if err := rows.Scan(
			&m.ID, &m.Name, &m.Transport, &m.EndpointURL, &m.AuthHeaderCiphertext, &m.AuthHeaderLast4,
			&m.Enabled, &m.AllowedTools, &toolsJSON, &m.TimeoutMs, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(toolsJSON, &m.DiscoveredTools)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *PGAdminStore) GetMCPServer(ctx context.Context, id string) (*MCPServerRecord, error) {
	var m MCPServerRecord
	var toolsJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, name, transport, endpoint_url, auth_header_ciphertext, auth_header_last4,
		       enabled, allowed_tools, discovered_tools, timeout_ms, created_at, updated_at
		FROM mcp_servers WHERE id = $1::uuid
	`, id).Scan(
		&m.ID, &m.Name, &m.Transport, &m.EndpointURL, &m.AuthHeaderCiphertext, &m.AuthHeaderLast4,
		&m.Enabled, &m.AllowedTools, &toolsJSON, &m.TimeoutMs, &m.CreatedAt, &m.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(toolsJSON, &m.DiscoveredTools)
	return &m, nil
}

func (s *PGAdminStore) UpsertMCPServer(ctx context.Context, rec *MCPServerRecord) (*MCPServerRecord, error) {
	toolsJSON, _ := json.Marshal(rec.DiscoveredTools)
	if len(toolsJSON) == 0 {
		toolsJSON = []byte("[]")
	}
	if rec.AllowedTools == nil {
		rec.AllowedTools = []string{}
	}
	if rec.ID == "" {
		err := s.pool.QueryRow(ctx, `
			INSERT INTO mcp_servers (name, transport, endpoint_url, auth_header_ciphertext, auth_header_last4, enabled, allowed_tools, discovered_tools, timeout_ms)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9)
			RETURNING id::text, created_at, updated_at
		`, rec.Name, rec.Transport, rec.EndpointURL, rec.AuthHeaderCiphertext, rec.AuthHeaderLast4, rec.Enabled, rec.AllowedTools, string(toolsJSON), rec.TimeoutMs).
			Scan(&rec.ID, &rec.CreatedAt, &rec.UpdatedAt)
		return rec, err
	}

	err := s.pool.QueryRow(ctx, `
		UPDATE mcp_servers SET
			name = $2, transport = $3, endpoint_url = $4,
			auth_header_ciphertext = COALESCE($5, auth_header_ciphertext),
			auth_header_last4 = CASE WHEN $6 <> '' THEN $6 ELSE auth_header_last4 END,
			enabled = $7, allowed_tools = $8, discovered_tools = $9::jsonb, timeout_ms = $10, updated_at = NOW()
		WHERE id = $1::uuid
		RETURNING created_at, updated_at
	`, rec.ID, rec.Name, rec.Transport, rec.EndpointURL, rec.AuthHeaderCiphertext, rec.AuthHeaderLast4, rec.Enabled, rec.AllowedTools, string(toolsJSON), rec.TimeoutMs).
		Scan(&rec.CreatedAt, &rec.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rec, err
}

func (s *PGAdminStore) DeleteMCPServer(ctx context.Context, id string) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM mcp_servers WHERE id = $1::uuid`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const agentSkillColumns = `
	id::text, name, description, instructions, reference_files, allowed_tools,
	source_format, filename, size_bytes, sha256_hash, enabled, uploaded_by::text,
	created_at, updated_at
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAgentSkill(row rowScanner) (*skills.Skill, error) {
	var skill skills.Skill
	var referenceFilesJSON []byte
	err := row.Scan(
		&skill.ID, &skill.Name, &skill.Description, &skill.Instructions, &referenceFilesJSON,
		&skill.AllowedTools, &skill.SourceFormat, &skill.Filename, &skill.SizeBytes,
		&skill.SHA256Hash, &skill.Enabled, &skill.UploadedBy, &skill.CreatedAt, &skill.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(referenceFilesJSON, &skill.ReferenceFiles); err != nil {
		return nil, err
	}
	if skill.ReferenceFiles == nil {
		skill.ReferenceFiles = map[string]string{}
	}
	if skill.AllowedTools == nil {
		skill.AllowedTools = []string{}
	}
	return &skill, nil
}

func (s *PGAdminStore) ListAgentSkills(ctx context.Context) ([]skills.Skill, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+agentSkillColumns+`
		FROM agent_skills
		ORDER BY created_at ASC, name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []skills.Skill
	for rows.Next() {
		skill, err := scanAgentSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *skill)
	}
	return out, rows.Err()
}

func (s *PGAdminStore) GetAgentSkill(ctx context.Context, id string) (*skills.Skill, error) {
	skill, err := scanAgentSkill(s.pool.QueryRow(ctx, `
		SELECT `+agentSkillColumns+`
		FROM agent_skills
		WHERE id = $1::uuid
	`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return skill, err
}

func (s *PGAdminStore) UpsertAgentSkillByName(ctx context.Context, skill *skills.Skill) (*skills.Skill, error) {
	referenceFilesJSON, err := json.Marshal(skill.ReferenceFiles)
	if err != nil {
		return nil, err
	}
	if skill.ReferenceFiles == nil {
		referenceFilesJSON = []byte("{}")
	}
	allowedTools := skill.AllowedTools
	if allowedTools == nil {
		allowedTools = []string{}
	}
	return scanAgentSkill(s.pool.QueryRow(ctx, `
		INSERT INTO agent_skills (
			name, description, instructions, reference_files, allowed_tools,
			source_format, filename, size_bytes, sha256_hash, uploaded_by
		) VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9, $10::uuid)
		ON CONFLICT (name) DO UPDATE SET
			description = EXCLUDED.description,
			instructions = EXCLUDED.instructions,
			reference_files = EXCLUDED.reference_files,
			allowed_tools = EXCLUDED.allowed_tools,
			source_format = EXCLUDED.source_format,
			filename = EXCLUDED.filename,
			size_bytes = EXCLUDED.size_bytes,
			sha256_hash = EXCLUDED.sha256_hash,
			uploaded_by = EXCLUDED.uploaded_by,
			updated_at = NOW()
		RETURNING `+agentSkillColumns+`
	`,
		skill.Name, skill.Description, skill.Instructions, string(referenceFilesJSON), allowedTools,
		skill.SourceFormat, skill.Filename, skill.SizeBytes, skill.SHA256Hash, skill.UploadedBy,
	))
}

func (s *PGAdminStore) UpdateAgentSkillEnabled(ctx context.Context, id string, enabled bool) (*skills.Skill, error) {
	skill, err := scanAgentSkill(s.pool.QueryRow(ctx, `
		UPDATE agent_skills
		SET enabled = $2, updated_at = NOW()
		WHERE id = $1::uuid
		RETURNING `+agentSkillColumns+`
	`, id, enabled))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return skill, err
}

func (s *PGAdminStore) DeleteAgentSkill(ctx context.Context, id string) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM agent_skills WHERE id = $1::uuid`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGAdminStore) ListAuditLogs(ctx context.Context, limit, offset int) ([]AuditLogRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, actor_id::text, actor_email, action, target_type, target_id, ip_address, outcome, metadata, created_at
		FROM audit_logs ORDER BY created_at DESC LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AuditLogRecord
	for rows.Next() {
		var r AuditLogRecord
		var metaBytes []byte
		if err := rows.Scan(&r.ID, &r.ActorID, &r.ActorEmail, &r.Action, &r.TargetType, &r.TargetID, &r.IPAddress, &r.Outcome, &metaBytes, &r.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(metaBytes, &r.Metadata)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *PGAdminStore) GetVoiceLanguages(ctx context.Context) (agent.VoiceLanguageSettings, bool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT code, name, voice, elevenlabs_voice, is_default
		FROM voice_languages
		ORDER BY position ASC, code ASC
	`)
	if err != nil {
		return agent.VoiceLanguageSettings{}, false, err
	}
	defer rows.Close()

	var out agent.VoiceLanguageSettings
	for rows.Next() {
		var l agent.VoiceLanguage
		var isDef bool
		if err := rows.Scan(&l.Code, &l.Name, &l.Voice, &l.ElevenLabsVoice, &isDef); err != nil {
			return agent.VoiceLanguageSettings{}, false, err
		}
		out.Languages = append(out.Languages, l)
		if isDef {
			out.DefaultCode = l.Code
		}
	}
	if err := rows.Err(); err != nil {
		return agent.VoiceLanguageSettings{}, false, err
	}
	if len(out.Languages) == 0 {
		return agent.VoiceLanguageSettings{}, false, nil
	}
	return out, true, nil
}

func (s *PGAdminStore) ReplaceVoiceLanguages(ctx context.Context, cfg agent.VoiceLanguageSettings) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(734215)`); err != nil {
		return err
	}
	var provider string
	if err := tx.QueryRow(ctx, `SELECT speech_provider FROM agent_configs WHERE name = 'default'`).Scan(&provider); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err := agent.ValidateVoiceLanguageSettingsForProvider(cfg, normalizeSpeechProvider(provider)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM voice_languages`); err != nil {
		return err
	}
	for i, l := range cfg.Languages {
		isDef := l.Code == cfg.DefaultCode
		if _, err := tx.Exec(ctx, `
			INSERT INTO voice_languages (code, name, voice, elevenlabs_voice, is_default, position, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, NOW())
		`, l.Code, l.Name, l.Voice, l.ElevenLabsVoice, isDef, i); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
