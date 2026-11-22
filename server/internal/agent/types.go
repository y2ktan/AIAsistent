package agent

import (
	"context"
	"strings"
	"time"
	"unicode"

	"aegis-voice/server/internal/mcp"
	"aegis-voice/server/internal/rag"
	"aegis-voice/server/internal/rtc"
)

const DefaultAssistantName = "Aegis"

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
	AssistantName      string
	WakeAliases        []string
	AllowPrivate       bool
	VoiceLanguages     VoiceLanguageSettings
	SpeechProvider     string
	ElevenLabsSTTModel string
	ElevenLabsTTSModel string
	ElevenLabsAPIKey   string
}

var traditionalToSimplifiedRune = map[rune]rune{
	'賈': '贾', '維': '维', '愛': '爱', '靈': '灵', '語': '语', '音': '音',
	'機': '机', '器': '器', '號': '号', '智': '智', '慧': '慧', '寶': '宝',
	'貝': '贝', '藍': '蓝', '雲': '云', '電': '电', '腦': '脑', '網': '网',
	'絡': '络', '統': '统', '總': '总', '經': '经', '濟': '济', '學': '学',
	'問': '问', '題': '题', '嗎': '吗', '麼': '么', '裡': '里', '個': '个',
	'誰': '谁', '幾': '几', '點': '点', '邊': '边', '係': '系', '對': '对',
	'無': '无', '開': '开', '關': '关', '門': '门', '間': '间', '時': '时',
	'實': '实', '現': '现', '見': '见', '視': '视', '聽': '听', '說': '说',
	'話': '话', '請': '请', '謝': '谢', '幫': '帮', '導': '导', '師': '师',
	'醫': '医', '護': '护', '車': '车', '飛': '飞', '風': '风', '龍': '龙',
	'鳳': '凤', '華': '华', '國': '国', '東': '东', '亞': '亚', '歐': '欧',
	'萬': '万', '億': '亿', '雙': '双', '體': '体', '會': '会', '員': '员',
	'長': '长', '張': '张', '陳': '陈', '劉': '刘', '楊': '杨', '黃': '黄',
	'趙': '赵', '吳': '吴', '鄭': '郑', '孫': '孙', '馬': '马', '朱': '朱',
	'處': '处', '務': '务', '動': '动', '應': '应', '該': '该', '樣': '样',
	'為': '为', '什': '什', '怎': '怎', '沒': '没', '還': '还', '這': '这',
	'進': '进', '遠': '远', '連': '连', '過': '过', '運': '运', '達': '达',
	'囉': '喽', '兒': '儿', '歲': '岁', '週': '周', '覺': '觉',
}

// FoldWakeRune normalizes Latin case and common Traditional/Simplified CJK variants.
func FoldWakeRune(r rune) rune {
	r = unicode.ToLower(r)
	if mapped, ok := traditionalToSimplifiedRune[r]; ok {
		return mapped
	}
	return r
}

// CanonicalWakeKey returns a case-folded, CJK-script-folded, whitespace-normalized key for wake-phrase deduplication.
func CanonicalWakeKey(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
			continue
		}
		lastSpace = false
		b.WriteRune(FoldWakeRune(r))
	}
	return b.String()
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
