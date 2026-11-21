package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"aegis-voice/server/internal/mcp"
	"aegis-voice/server/internal/rtc"
	"aegis-voice/server/internal/security"
)

type RealtimeAdapter struct {
	deps              Dependencies
	mu                sync.Mutex
	writeMu           sync.Mutex
	conn              *websocket.Conn
	cancel            context.CancelFunc
	wakeGate          *WakeTurnGate
	bufferedItemIDs   []string
	speaking          bool
	responseActive    bool
	toolCallsThisTurn int
	toolLimitNotified bool
}

func NewRealtimeAdapter(deps Dependencies) *RealtimeAdapter {
	gate := NewWakeTurnGate(DefaultWakeWindow)
	r := &RealtimeAdapter{
		deps:     deps,
		wakeGate: gate,
	}
	gate.SetOnExpire(func() {
		r.mu.Lock()
		expiredIDs := append([]string(nil), r.bufferedItemIDs...)
		r.bufferedItemIDs = nil
		r.mu.Unlock()
		for _, id := range expiredIDs {
			if id != "" {
				_ = r.writeJSON(map[string]any{
					"type":    "conversation.item.delete",
					"item_id": id,
				})
			}
		}
	})
	return r
}

func (r *RealtimeAdapter) Start(ctx context.Context) error {
	cfg := r.deps.Config
	if cfg.RealtimeWSURL == "" {
		r.deps.Sink.SendEvent(rtc.NewErrorEvent("Realtime WebSocket URL is not configured."))
		return errors.New("realtime ws url not configured")
	}

	policy := security.SSRFPolicy{AllowPrivate: cfg.AllowPrivate}
	parsed, err := policy.ValidateWebSocketURL(cfg.RealtimeWSURL)
	if err != nil {
		return err
	}

	wsURL := parsed.String()
	if strings.HasPrefix(wsURL, "http://") {
		wsURL = "ws://" + strings.TrimPrefix(wsURL, "http://")
	} else if strings.HasPrefix(wsURL, "https://") {
		wsURL = "wss://" + strings.TrimPrefix(wsURL, "https://")
	}

	safeDialer := security.NewSafeDialer(cfg.AllowPrivate, 10*time.Second)
	dialer := websocket.Dialer{
		NetDialContext:   safeDialer.DialContext,
		HandshakeTimeout: 10 * time.Second,
	}
	headers := http.Header{}
	if cfg.APIKey != "" {
		headers.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	headers.Set("OpenAI-Beta", "realtime=v1")

	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		r.deps.Sink.SendEvent(rtc.NewErrorEvent("Failed to connect to Realtime AI provider."))
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.conn = conn
	r.cancel = cancel
	r.mu.Unlock()

	if err := r.sendSessionUpdate(); err != nil {
		_ = r.Close()
		return err
	}

	r.deps.Sink.SendEvent(rtc.NewStateEvent("listening"))
	go r.readLoop(runCtx)
	return nil
}

func (r *RealtimeAdapter) sendSessionUpdate() error {
	tools := []map[string]any{
		{
			"type":        "function",
			"name":        "search_knowledge_base",
			"description": "Search ingested organization documents for factual answers.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
				},
				"required": []string{"query"},
			},
		},
	}
	for _, tool := range BuildMCPToolRegistry(r.deps.MCPServers) {
		tools = append(tools, map[string]any{
			"type":        "function",
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.InputSchema,
		})
	}
	for _, definition := range SkillToolDefinitions(r.deps.Skills) {
		tools = append(tools, map[string]any{
			"type":        "function",
			"name":        definition.Name,
			"description": definition.Description,
			"parameters":  definition.Parameters,
		})
	}

	vl := r.deps.Config.VoiceLanguages
	if len(vl.Languages) == 0 {
		vl = InitialVoiceLanguageSettings(r.deps.Config.TTSVoice)
	}
	voiced := vl.VoicedLanguages()
	voicedNames := make([]string, 0, len(voiced))
	for _, l := range voiced {
		voicedNames = append(voicedNames, l.Name)
	}
	defLang, _ := vl.Find(vl.DefaultCode)
	defName := defLang.Name
	if defName == "" {
		defName = "English"
	}
	langPolicy := "\n\nLANGUAGE POLICY:\n- Supported spoken reply languages: " + strings.Join(voicedNames, ", ") + ".\n- Default language: " + defName + ".\n- Always reply in the language of the user's most recent utterance if it is in the supported spoken list. If the language of an utterance cannot be determined, reply in the language of your previous reply in this session, or in " + defName + " if there is no previous reply. Otherwise, interpret their message as well as you can, reply in " + defName + ", and tell them once per session (without repeating) that you cannot reply in that language yet."

	wakePrompt := BuildWakeVocabularyPrompt(r.deps.Config.AssistantName, r.deps.Config.WakeAliases)
	session := map[string]any{
		"instructions": BuildSystemPromptWithSkills(r.deps.Config.SystemInstructions, r.deps.Config.AssistantName, r.deps.Skills) + langPolicy,
		"tools":        tools,
		"turn_detection": map[string]any{
			"type":                "server_vad",
			"silence_duration_ms": 2500,
			"create_response":     false,
			"interrupt_response":  false,
		},
		"input_audio_transcription": map[string]any{
			"model":  "whisper-1",
			"prompt": wakePrompt,
		},
	}
	if r.deps.Config.AudioFocusEnabled {
		session["input_audio_noise_reduction"] = map[string]any{"type": "near_field"}
	}

	return r.writeJSON(map[string]any{
		"type":    "session.update",
		"session": session,
	})
}

func (r *RealtimeAdapter) SendAudioBase64(b64Audio string) error {
	return r.writeJSON(map[string]any{
		"type":  "input_audio_buffer.append",
		"audio": b64Audio,
	})
}

func (r *RealtimeAdapter) BargeIn() {
	r.mu.Lock()
	r.speaking = false
	r.responseActive = false
	r.mu.Unlock()
	_ = r.writeJSON(map[string]any{"type": "response.cancel"})
	r.deps.Sink.SendEvent(rtc.NewBargeInEvent())
}

func (r *RealtimeAdapter) isActiveOrSpeaking() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.speaking || r.responseActive
}

func (r *RealtimeAdapter) Close() error {
	if r.wakeGate != nil {
		r.wakeGate.Close()
	}
	r.mu.Lock()
	cancel := r.cancel
	conn := r.conn
	r.conn = nil
	r.cancel = nil
	r.speaking = false
	r.responseActive = false
	r.bufferedItemIDs = nil
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if conn != nil {
		return conn.Close()
	}
	return nil
}

func (r *RealtimeAdapter) writeJSON(v any) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	r.mu.Lock()
	conn := r.conn
	r.mu.Unlock()
	if conn == nil {
		return errors.New("realtime connection closed")
	}
	return conn.WriteJSON(v)
}

func (r *RealtimeAdapter) readLoop(ctx context.Context) {
	defer func() { _ = r.Close() }()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		r.mu.Lock()
		conn := r.conn
		r.mu.Unlock()
		if conn == nil {
			return
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var ev struct {
			Type       string `json:"type"`
			Delta      string `json:"delta"`
			CallID     string `json:"call_id"`
			Name       string `json:"name"`
			Arguments  string `json:"arguments"`
			ItemID     string `json:"item_id"`
			Transcript string `json:"transcript"`
		}
		if err := json.Unmarshal(msg, &ev); err != nil {
			continue
		}

		switch ev.Type {
		case "input_audio_buffer.speech_started":
			// Do not auto-interrupt on raw VAD onset; wait for input transcription to verify wake name.
			if !r.isActiveOrSpeaking() {
				r.deps.Sink.SendEvent(rtc.NewStateEvent("listening"))
			}
		case "conversation.item.input_audio_transcription.completed":
			r.handleCompletedTranscription(ev.ItemID, ev.Transcript)
		case "response.audio.delta":
			if ev.Delta != "" {
				if decoded, err := base64.StdEncoding.DecodeString(ev.Delta); err == nil {
					r.mu.Lock()
					r.speaking = true
					r.responseActive = true
					r.mu.Unlock()
					r.deps.Sink.SendEvent(rtc.NewStateEvent("speaking"))
					_ = r.deps.Sink.WriteAudioSample(decoded, 20*time.Millisecond)
				}
			}
		case "response.audio_transcript.delta":
			if ev.Delta != "" {
				clean := SanitizeAssistantOutput(ev.Delta, []string{r.deps.Config.APIKey})
				if !IsWaitForQuestionReply(clean) {
					r.deps.Sink.SendEvent(rtc.NewTranscriptEvent("transcript.delta", "assistant", clean))
				}
			}
		case "response.function_call_arguments.done":
			if !r.reserveToolCall() {
				go r.handleToolCallLimit(ctx, ev.CallID)
				continue
			}
			go r.handleToolCall(ctx, ev.CallID, ev.Name, ev.Arguments)
		case "response.done":
			r.mu.Lock()
			r.speaking = false
			r.responseActive = false
			r.mu.Unlock()
			r.deps.Sink.SendEvent(rtc.NewStateEvent("listening"))
		}
	}
}

func (r *RealtimeAdapter) handleCompletedTranscription(itemID, rawTranscript string) {
	text := strings.TrimSpace(rawTranscript)
	if text == "" {
		if itemID != "" {
			_ = r.writeJSON(map[string]any{
				"type":    "conversation.item.delete",
				"item_id": itemID,
			})
		}
		return
	}

	r.mu.Lock()
	if r.wakeGate == nil {
		r.wakeGate = NewWakeTurnGate(DefaultWakeWindow)
	}
	gate := r.wakeGate
	r.mu.Unlock()

	res := gate.Evaluate(text, r.deps.Config.AssistantName, r.deps.Config.WakeAliases)
	switch res.Action {
	case WakeActionIgnore:
		if itemID != "" {
			_ = r.writeJSON(map[string]any{
				"type":    "conversation.item.delete",
				"item_id": itemID,
			})
		}
		return
	case WakeActionBufferWait:
		if itemID != "" {
			r.mu.Lock()
			r.bufferedItemIDs = append(r.bufferedItemIDs, itemID)
			r.mu.Unlock()
		}
		if res.WakeMatched && r.isActiveOrSpeaking() {
			r.BargeIn()
		}
		r.deps.Sink.SendEvent(rtc.NewTranscriptEvent("transcript.final", "user", text))
		if !r.isActiveOrSpeaking() {
			r.deps.Sink.SendEvent(rtc.NewStateEvent("listening"))
		}
		return
	case WakeActionRespond:
		r.mu.Lock()
		r.bufferedItemIDs = nil
		r.mu.Unlock()
		if r.isActiveOrSpeaking() {
			r.BargeIn()
		}
		r.resetToolCallBudget()
		r.mu.Lock()
		r.responseActive = true
		r.mu.Unlock()
		r.deps.Sink.SendEvent(rtc.NewTranscriptEvent("transcript.final", "user", text))
		r.deps.Sink.SendEvent(rtc.NewStateEvent("thinking"))
		_ = r.writeJSON(map[string]any{"type": "response.create"})
	}
}

func (r *RealtimeAdapter) handleToolCall(ctx context.Context, callID, name, rawArgs string) {
	r.deps.Sink.SendEvent(rtc.Event{
		Type:      "tool.call",
		Tool:      name,
		Status:    "running",
		Timestamp: time.Now().UnixMilli(),
	})

	var args map[string]any
	_ = json.Unmarshal([]byte(rawArgs), &args)

	var output string
	if skillOutput, handled := HandleSkillTool(r.deps.Skills, name, args); handled {
		output = skillOutput
	} else if name == "search_knowledge_base" && r.deps.RAGService != nil && r.deps.Embedder != nil {
		q, _ := args["query"].(string)
		hits, err := r.deps.RAGService.Retrieve(ctx, q, r.deps.Config.RAGTopK, r.deps.Config.RAGMinSimilarity, r.deps.Embedder)
		if err == nil && len(hits) > 0 {
			r.deps.Sink.SendEvent(rtc.Event{Type: "citation", Citations: hits, Timestamp: time.Now().UnixMilli()})
			output = WrapUntrustedRAGContext(hits)
		} else {
			output = "No relevant documents found."
		}
	} else {
		var target *mcp.ServerConfig
		for _, binding := range BuildMCPToolRegistry(r.deps.MCPServers) {
			if binding.Name == name {
				server := binding.Server
				target = &server
				name = binding.RawName
				break
			}
		}
		if target != nil && r.deps.MCPHost != nil {
			res, err := r.deps.MCPHost.CallTool(ctx, *target, name, args)
			if err != nil {
				output = "tool failed: " + security.RedactString(err.Error())
			} else {
				output = WrapUntrustedToolOutput(name, res)
			}
		} else {
			output = "tool not permitted"
		}
	}

	_ = r.writeJSON(map[string]any{
		"type": "conversation.item.create",
		"item": map[string]any{
			"type":    "function_call_output",
			"call_id": callID,
			"output":  output,
		},
	})
	_ = r.writeJSON(map[string]any{"type": "response.create"})
}

func (r *RealtimeAdapter) resetToolCallBudget() {
	r.mu.Lock()
	r.toolCallsThisTurn = 0
	r.toolLimitNotified = false
	r.mu.Unlock()
}

func (r *RealtimeAdapter) reserveToolCall() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.toolCallsThisTurn >= maxToolRounds {
		return false
	}
	r.toolCallsThisTurn++
	return true
}

func (r *RealtimeAdapter) handleToolCallLimit(ctx context.Context, callID string) {
	r.deps.Sink.SendEvent(rtc.Event{
		Type:      "tool.call",
		Tool:      "tool_call_limit",
		Status:    "blocked",
		Timestamp: time.Now().UnixMilli(),
	})
	_ = r.writeJSON(map[string]any{
		"type": "conversation.item.create",
		"item": map[string]any{
			"type":    "function_call_output",
			"call_id": callID,
			"output":  "Tool-call budget exhausted. Synthesize the best response from the information already available and do not call additional tools.",
		},
	})

	r.mu.Lock()
	shouldContinue := !r.toolLimitNotified
	r.toolLimitNotified = true
	r.mu.Unlock()
	if shouldContinue && ctx.Err() == nil {
		_ = r.writeJSON(map[string]any{"type": "response.create"})
	}
}
