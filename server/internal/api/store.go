package api

import (
	"context"
	"errors"
	"time"

	"aegis-voice/server/internal/agent"
	"aegis-voice/server/internal/auth"
	"aegis-voice/server/internal/mcp"
	"aegis-voice/server/internal/skills"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrLastAdmin = errors.New("cannot deactivate or demote the last active administrator")
)

type AgentConfigRecord struct {
	ID                         string    `json:"id"`
	Name                       string    `json:"name"`
	Mode                       string    `json:"mode"`
	RealtimeWSURL              string    `json:"realtimeWsUrl"`
	RealtimeModel              string    `json:"realtimeModel"`
	STTBaseURL                 string    `json:"sttBaseUrl"`
	STTModel                   string    `json:"sttModel"`
	LLMBaseURL                 string    `json:"llmBaseUrl"`
	LLMModel                   string    `json:"llmModel"`
	TTSBaseURL                 string    `json:"ttsBaseUrl"`
	TTSModel                   string    `json:"ttsModel"`
	TTSVoice                   string    `json:"ttsVoice"`
	EmbeddingBaseURL           string    `json:"embeddingBaseUrl"`
	EmbeddingModel             string    `json:"embeddingModel"`
	EmbeddingDim               int       `json:"embeddingDim"`
	SystemInstructions         string    `json:"systemInstructions"`
	APIKeyCiphertext           []byte    `json:"-"`
	APIKeyLast4                string    `json:"apiKeyPreview"`
	RAGTopK                    int       `json:"ragTopK"`
	RAGMinSimilarity           float64   `json:"ragMinSimilarity"`
	AudioFocusEnabled          bool      `json:"audioFocusEnabled"`
	AssistantName              string    `json:"assistantName"`
	WakeAliases                []string  `json:"wakeAliases"`
	SpeechProvider             string    `json:"speechProvider"`
	ElevenLabsSTTModel         string    `json:"elevenLabsSttModel"`
	ElevenLabsTTSModel         string    `json:"elevenLabsTtsModel"`
	ElevenLabsAPIKeyCiphertext []byte    `json:"-"`
	ElevenLabsAPIKeyLast5      string    `json:"elevenLabsApiKeyPreview"`
	UpdatedAt                  time.Time `json:"updatedAt"`
}

type MCPServerRecord struct {
	ID                   string        `json:"id"`
	Name                 string        `json:"name"`
	Transport            string        `json:"transport"`
	EndpointURL          string        `json:"endpointUrl"`
	AuthHeaderCiphertext []byte        `json:"-"`
	AuthHeaderLast4      string        `json:"authHeaderPreview"`
	Enabled              bool          `json:"enabled"`
	AllowedTools         []string      `json:"allowedTools"`
	DiscoveredTools      []mcp.ToolDef `json:"discoveredTools"`
	TimeoutMs            int           `json:"timeoutMs"`
	CreatedAt            time.Time     `json:"createdAt"`
	UpdatedAt            time.Time     `json:"updatedAt"`
}

type AuditLogRecord struct {
	ID         int64          `json:"id"`
	ActorID    *string        `json:"actorId,omitempty"`
	ActorEmail string         `json:"actorEmail"`
	Action     string         `json:"action"`
	TargetType string         `json:"targetType"`
	TargetID   string         `json:"targetId"`
	IPAddress  string         `json:"ipAddress"`
	Outcome    string         `json:"outcome"`
	Metadata   map[string]any `json:"metadata"`
	CreatedAt  time.Time      `json:"createdAt"`
}

type AdminStore interface {
	ListUsers(ctx context.Context) ([]auth.User, error)
	CreateUser(ctx context.Context, email, passwordHash, role string) (*auth.User, error)
	UpdateUser(ctx context.Context, userID, role string, isActive bool) (*auth.User, error)
	GetAgentConfig(ctx context.Context) (*AgentConfigRecord, error)
	UpsertAgentConfig(ctx context.Context, rec *AgentConfigRecord) error
	GetVoiceLanguages(ctx context.Context) (agent.VoiceLanguageSettings, bool, error)
	ReplaceVoiceLanguages(ctx context.Context, cfg agent.VoiceLanguageSettings) error
	ListMCPServers(ctx context.Context) ([]MCPServerRecord, error)
	GetMCPServer(ctx context.Context, id string) (*MCPServerRecord, error)
	UpsertMCPServer(ctx context.Context, rec *MCPServerRecord) (*MCPServerRecord, error)
	DeleteMCPServer(ctx context.Context, id string) error
	ListAgentSkills(ctx context.Context) ([]skills.Skill, error)
	GetAgentSkill(ctx context.Context, id string) (*skills.Skill, error)
	UpsertAgentSkillByName(ctx context.Context, skill *skills.Skill) (*skills.Skill, error)
	UpdateAgentSkillEnabled(ctx context.Context, id string, enabled bool) (*skills.Skill, error)
	DeleteAgentSkill(ctx context.Context, id string) error
	ListAuditLogs(ctx context.Context, limit, offset int) ([]AuditLogRecord, error)
}
