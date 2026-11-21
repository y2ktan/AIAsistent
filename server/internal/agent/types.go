package agent

import (
	"context"
	"time"

	"aegis-voice/server/internal/mcp"
	"aegis-voice/server/internal/rag"
	"aegis-voice/server/internal/rtc"
)

type AgentConfig struct {
	Mode               string
	RealtimeWSURL      string
	RealtimeModel      string
	STTBaseURL         string
	STTModel           string
	LLMBaseURL         string
	LLMModel           string
	TTSBaseURL         string
	TTSModel           string
	TTSVoice           string
	EmbeddingBaseURL   string
	EmbeddingModel     string
	SystemInstructions string
	APIKey             string
	RAGTopK            int
	RAGMinSimilarity   float64
	AudioFocusEnabled  bool
	AllowPrivate       bool
	VoiceLanguages     VoiceLanguageSettings
}

type EventSink interface {
	SendEvent(ev rtc.Event)
	WriteAudioSample(data []byte, duration time.Duration) error
	LatestVideoFrame() ([]byte, bool)
}

type SessionAgent interface {
	Start(ctx context.Context) error
	BargeIn()
	Close() error
}

type Dependencies struct {
	Config     AgentConfig
	Sink       EventSink
	RAGService *rag.Service
	Embedder   rag.Embedder
	MCPHost    *mcp.Host
	MCPServers []mcp.ServerConfig
	Skills     []AgentSkill
}
