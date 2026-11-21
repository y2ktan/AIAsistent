package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"time"

	"aegis-voice/server/internal/rag"
	"aegis-voice/server/internal/rtc"
	"aegis-voice/server/internal/security"
)

type CascadedAdapter struct {
	deps              Dependencies
	httpClient        *http.Client
	mu                sync.Mutex
	turnSeq           uint64
	activeTurnID      uint64
	turnCancel        context.CancelFunc
	speaking          bool
	disableVerboseSTT bool
	disablePromptSTT  bool
	wakeGate          *WakeTurnGate
	langState         *LanguageSessionState
	lastDetectedLang  string
	lastReplyLang     string
	lastFallback      bool
}

func NewCascadedAdapter(deps Dependencies) *CascadedAdapter {
	return &CascadedAdapter{
		deps: deps,
		// Generous timeout: self-hosted CPU models can take well over a minute on cold start.
		httpClient: security.NewSafeHTTPClient(deps.Config.AllowPrivate, 180*time.Second),
		wakeGate:   NewWakeTurnGate(DefaultWakeWindow),
		langState:  NewLanguageSessionState(),
	}
}

func (c *CascadedAdapter) voiceLanguages() VoiceLanguageSettings {
	if len(c.deps.Config.VoiceLanguages.Languages) > 0 {
		return c.deps.Config.VoiceLanguages
	}
	return InitialVoiceLanguageSettings(c.deps.Config.TTSVoice)
}

func (c *CascadedAdapter) LastTurnLanguage() (detected, reply string, fallback bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastDetectedLang, c.lastReplyLang, c.lastFallback
}

func (c *CascadedAdapter) Start(_ context.Context) error {
	if c.deps.Config.LLMBaseURL == "" {
		c.deps.Sink.SendEvent(rtc.NewErrorEvent("AI provider is not configured. Please ask an administrator to configure AI settings."))
		return errors.New("llm base url not configured")
	}
	c.deps.Sink.SendEvent(rtc.NewStateEvent("listening"))
	return nil
}

func (c *CascadedAdapter) BargeIn() {
	c.mu.Lock()
	cancel := c.turnCancel
	c.turnCancel = nil
	c.activeTurnID = 0
	c.speaking = false
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	c.deps.Sink.SendEvent(rtc.NewBargeInEvent())
}

func (c *CascadedAdapter) hasActiveTurn() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.turnCancel != nil || c.speaking
}

// IsSpeaking reports whether assistant audio is currently being streamed to the peer.
func (c *CascadedAdapter) IsSpeaking() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.speaking
}

func (c *CascadedAdapter) setSpeaking(v bool) {
	c.mu.Lock()
	c.speaking = v
	c.mu.Unlock()
}

func (c *CascadedAdapter) Close() error {
	if c.wakeGate != nil {
		c.wakeGate.Close()
	}
	c.BargeIn()
	return nil
}

func (c *CascadedAdapter) ProcessTurn(ctx context.Context, audioData []byte) error {
	cfg := c.deps.Config

	// 1. Non-destructive Speech-to-Text BEFORE cancelling any active turn or emitting thinking state.
	userText, sttLang, err := c.transcribe(ctx, audioData)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !c.hasActiveTurn() {
			c.deps.Sink.SendEvent(rtc.NewErrorEvent("Speech transcription failed."))
			c.deps.Sink.SendEvent(rtc.NewStateEvent("listening"))
		}
		return err
	}
	if strings.TrimSpace(userText) == "" {
		if !c.hasActiveTurn() {
			c.deps.Sink.SendEvent(rtc.NewStateEvent("listening"))
		}
		return nil
	}

	// 2. Wake-name prefix & pause-accumulation gate.
	c.mu.Lock()
	if c.wakeGate == nil {
		c.wakeGate = NewWakeTurnGate(DefaultWakeWindow)
	}
	gate := c.wakeGate
	c.mu.Unlock()

	gateRes := gate.Evaluate(userText, cfg.AssistantName, cfg.WakeAliases)
	switch gateRes.Action {
	case WakeActionIgnore:
		// Unaddressed background speech: do not interrupt in-flight turns or emit user captions.
		return nil
	case WakeActionBufferWait:
		// User addressed the wake name (or continued within the 15s armed window) without asking a question yet.
		if gateRes.WakeMatched && c.hasActiveTurn() {
			c.BargeIn()
		}
		c.deps.Sink.SendEvent(rtc.NewTranscriptEvent("transcript.final", "user", userText))
		if !c.hasActiveTurn() {
			c.deps.Sink.SendEvent(rtc.NewStateEvent("listening"))
		}
		return nil
	}

	// WakeActionRespond: wake-authorized direct question. Interrupt any active assistant speech/turn now.
	if c.hasActiveTurn() {
		c.BargeIn()
	}

	turnCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.turnSeq++
	myTurnID := c.turnSeq
	c.activeTurnID = myTurnID
	c.turnCancel = cancel
	c.mu.Unlock()
	defer func() {
		cancel()
		c.mu.Lock()
		if c.activeTurnID == myTurnID {
			c.turnCancel = nil
			c.activeTurnID = 0
		}
		c.mu.Unlock()
	}()

	c.deps.Sink.SendEvent(rtc.NewTranscriptEvent("transcript.final", "user", userText))
	c.deps.Sink.SendEvent(rtc.NewStateEvent("thinking"))

	queryText := gateRes.CombinedText
	if strings.TrimSpace(queryText) == "" {
		queryText = userText
	}

	vl := c.voiceLanguages()
	detectedCode, detectedName := DetectUtteranceLanguage(sttLang, queryText, vl)
	c.mu.Lock()
	if c.langState == nil {
		c.langState = NewLanguageSessionState()
	}
	langRes := ResolveReplyLanguage(detectedCode, detectedName, vl, c.langState)
	c.lastDetectedLang = detectedCode
	if c.lastDetectedLang == "" {
		c.lastDetectedLang = detectedName
	}
	c.lastReplyLang = langRes.ReplyCode
	c.lastFallback = langRes.Fallback
	c.mu.Unlock()
	langInstruction := BuildLanguageSystemInstruction(langRes)

	// 3. RAG Retrieval
	var ragContext string
	if c.deps.RAGService != nil && c.deps.Embedder != nil {
		c.deps.Sink.SendEvent(rtc.NewStateEvent("retrieving"))
		hits, err := c.deps.RAGService.Retrieve(turnCtx, queryText, cfg.RAGTopK, cfg.RAGMinSimilarity, c.deps.Embedder)
		if err == nil && len(hits) > 0 {
			c.deps.Sink.SendEvent(rtc.Event{
				Type:      "citation",
				Citations: hits,
				Timestamp: time.Now().UnixMilli(),
			})
			ragContext = WrapUntrustedRAGContext(hits)
		}
	}

	// 4. Vision frame + LLM Chat Completions with MCP tool loop
	c.deps.Sink.SendEvent(rtc.NewStateEvent("thinking"))
	var videoFrame []byte
	if frame, ok := c.deps.Sink.LatestVideoFrame(); ok {
		videoFrame = frame
	}

	reply, err := c.chatWithTools(turnCtx, queryText, ragContext, videoFrame, langInstruction)
	if err != nil {
		if errors.Is(err, context.Canceled) || turnCtx.Err() != nil {
			return err
		}
		c.deps.Sink.SendEvent(rtc.NewErrorEvent("Assistant failed to generate a response."))
		c.deps.Sink.SendEvent(rtc.NewStateEvent("listening"))
		return err
	}

	if IsWaitForQuestionReply(reply) {
		gate.RestoreBuffered(gateRes.Segments)
		if turnCtx.Err() == nil {
			c.deps.Sink.SendEvent(rtc.NewStateEvent("listening"))
		}
		return nil
	}

	reply = SanitizeAssistantOutput(reply, []string{cfg.APIKey})
	c.deps.Sink.SendEvent(rtc.NewTranscriptEvent("transcript.final", "assistant", reply))

	voice, actualCode := GuardVoiceForReply(reply, langRes, vl)
	c.mu.Lock()
	c.langState.LastReplyCode = actualCode
	c.lastReplyLang = actualCode
	c.mu.Unlock()

	// 5. Streaming TTS
	if cfg.TTSBaseURL != "" && reply != "" {
		c.deps.Sink.SendEvent(rtc.NewStateEvent("speaking"))
		c.setSpeaking(true)
		err := c.synthesizeAndStream(turnCtx, reply, voice)
		c.setSpeaking(false)
		if err != nil && !errors.Is(err, context.Canceled) && turnCtx.Err() == nil {
			c.deps.Sink.SendEvent(rtc.NewErrorEvent("Speech synthesis failed."))
		}
	}

	if turnCtx.Err() == nil {
		c.deps.Sink.SendEvent(rtc.NewStateEvent("listening"))
	}
	return nil
}

func (c *CascadedAdapter) transcribe(ctx context.Context, audioData []byte) (string, string, error) {
	baseURL := strings.TrimRight(c.deps.Config.STTBaseURL, "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(c.deps.Config.LLMBaseURL, "/")
	}
	policy := security.SSRFPolicy{AllowPrivate: c.deps.Config.AllowPrivate}
	if _, err := policy.ValidateHTTPURL(baseURL); err != nil {
		return "", "", err
	}

	wakePrompt := BuildWakeVocabularyPrompt(c.deps.Config.AssistantName, c.deps.Config.WakeAliases)

	c.mu.Lock()
	useVerbose := !c.disableVerboseSTT
	usePrompt := !c.disablePromptSTT && wakePrompt != ""
	c.mu.Unlock()

	for attempt := 0; attempt < 3; attempt++ {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		model := c.deps.Config.STTModel
		if model == "" {
			model = "whisper-1"
		}
		_ = mw.WriteField("model", model)
		if useVerbose {
			_ = mw.WriteField("response_format", "verbose_json")
		}
		if usePrompt {
			_ = mw.WriteField("prompt", wakePrompt)
		}
		filename := "audio.wav"
		if bytes.HasPrefix(audioData, []byte("OggS")) {
			filename = "audio.ogg"
		}
		fw, err := mw.CreateFormFile("file", filename)
		if err != nil {
			return "", "", err
		}
		_, _ = fw.Write(audioData)
		_ = mw.Close()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/audio/transcriptions", &buf)
		if err != nil {
			return "", "", err
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		if c.deps.Config.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.deps.Config.APIKey)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return "", "", err
		}
		if resp.StatusCode == http.StatusBadRequest && (useVerbose || usePrompt) {
			errBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			errLower := strings.ToLower(string(errBytes))
			if usePrompt && strings.Contains(errLower, "prompt") && !strings.Contains(errLower, "response_format") {
				c.mu.Lock()
				c.disablePromptSTT = true
				c.mu.Unlock()
				usePrompt = false
				continue
			}
			if useVerbose {
				c.mu.Lock()
				c.disableVerboseSTT = true
				c.mu.Unlock()
				useVerbose = false
				continue
			}
			if usePrompt {
				c.mu.Lock()
				c.disablePromptSTT = true
				c.mu.Unlock()
				usePrompt = false
				continue
			}
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_ = resp.Body.Close()
			return "", "", fmt.Errorf("stt status %d", resp.StatusCode)
		}
		var out struct {
			Text     string `json:"text"`
			Language string `json:"language"`
		}
		decErr := json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&out)
		_ = resp.Body.Close()
		if decErr != nil {
			return "", "", decErr
		}
		return out.Text, out.Language, nil
	}
	return "", "", errors.New("stt request failed")
}

func (c *CascadedAdapter) chatWithTools(ctx context.Context, userText, ragContext string, videoFrame []byte, langInstruction string) (string, error) {
	baseURL := strings.TrimRight(c.deps.Config.LLMBaseURL, "/")
	policy := security.SSRFPolicy{AllowPrivate: c.deps.Config.AllowPrivate}
	if _, err := policy.ValidateHTTPURL(baseURL); err != nil {
		return "", err
	}

	messages := []map[string]any{
		{"role": "system", "content": BuildSystemPromptWithSkills(c.deps.Config.SystemInstructions, c.deps.Config.AssistantName, c.deps.Skills)},
	}
	if langInstruction != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": langInstruction,
		})
	}
	if ragContext != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": "Retrieved knowledge context:\n" + ragContext,
		})
	}

	if len(videoFrame) > 0 {
		b64 := base64.StdEncoding.EncodeToString(videoFrame)
		messages = append(messages, map[string]any{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": userText},
				{"type": "image_url", "image_url": map[string]string{"url": "data:image/jpeg;base64," + b64}},
			},
		})
	} else {
		messages = append(messages, map[string]any{
			"role":    "user",
			"content": userText,
		})
	}

	// Build tool definitions from enabled MCP servers.
	var tools []map[string]any
	mcpTools := BuildMCPToolRegistry(c.deps.MCPServers)
	toolToBinding := make(map[string]MCPToolBinding, len(mcpTools))
	for _, tool := range mcpTools {
		toolToBinding[tool.Name] = tool
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.InputSchema,
			},
		})
	}
	for _, definition := range SkillToolDefinitions(c.deps.Skills) {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        definition.Name,
				"description": definition.Description,
				"parameters":  definition.Parameters,
			},
		})
	}

	model := c.deps.Config.LLMModel
	if model == "" {
		model = "gpt-4o"
	}

	for round := 0; round < maxToolRounds; round++ {
		payload := map[string]any{
			"model":    model,
			"messages": messages,
			// Voice replies should be short; this also bounds latency on self-hosted models.
			"max_tokens": 512,
		}
		if len(tools) > 0 {
			payload["tools"] = tools
		}
		body, _ := json.Marshal(payload)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		if c.deps.Config.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.deps.Config.APIKey)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return "", err
		}
		respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
		_ = resp.Body.Close()
		if err != nil {
			return "", err
		}
		if resp.StatusCode == http.StatusBadRequest && len(tools) > 0 &&
			bytes.Contains(bytes.ToLower(respBytes), []byte("tool")) {
			// Some local models (e.g. Ollama llama3) reject tool definitions; degrade to plain chat.
			tools = nil
			round--
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("llm status %d: %s", resp.StatusCode, security.RedactString(truncate(string(respBytes), 300)))
		}

		var parsed struct {
			Choices []struct {
				Message struct {
					Role      string `json:"role"`
					Content   string `json:"content"`
					ToolCalls []struct {
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(respBytes, &parsed); err != nil {
			return "", err
		}
		if len(parsed.Choices) == 0 {
			return "", errors.New("empty llm choices")
		}
		msg := parsed.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			return msg.Content, nil
		}

		messages = append(messages, map[string]any{
			"role":       "assistant",
			"content":    msg.Content,
			"tool_calls": msg.ToolCalls,
		})

		for _, tc := range msg.ToolCalls {
			c.deps.Sink.SendEvent(rtc.Event{
				Type:      "tool.call",
				Tool:      tc.Function.Name,
				Status:    "running",
				Timestamp: time.Now().UnixMilli(),
			})
			var toolOut string
			if binding, ok := toolToBinding[tc.Function.Name]; ok && c.deps.MCPHost != nil {
				var args map[string]any
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
				res, callErr := c.deps.MCPHost.CallTool(ctx, binding.Server, binding.RawName, args)
				if callErr != nil {
					toolOut = "tool execution failed: " + security.RedactString(callErr.Error())
				} else {
					toolOut = WrapUntrustedToolOutput(tc.Function.Name, res)
				}
			} else {
				var args map[string]any
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
				if skillOutput, handled := HandleSkillTool(c.deps.Skills, tc.Function.Name, args); handled {
					toolOut = skillOutput
				} else {
					toolOut = "tool not permitted"
				}
			}
			messages = append(messages, map[string]any{
				"role":         "tool",
				"tool_call_id": tc.ID,
				"content":      toolOut,
			})
		}
	}
	return "", errors.New("exceeded maximum tool call rounds")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func (c *CascadedAdapter) synthesizeAndStream(ctx context.Context, text, voice string) error {
	baseURL := strings.TrimRight(c.deps.Config.TTSBaseURL, "/")
	policy := security.SSRFPolicy{AllowPrivate: c.deps.Config.AllowPrivate}
	if _, err := policy.ValidateHTTPURL(baseURL); err != nil {
		return err
	}

	model := c.deps.Config.TTSModel
	if model == "" {
		model = "tts-1"
	}
	if strings.TrimSpace(voice) == "" {
		voice = c.deps.Config.TTSVoice
	}
	if strings.TrimSpace(voice) == "" {
		voice = "alloy"
	}
	payload, _ := json.Marshal(map[string]any{
		"model":           model,
		"input":           text,
		"voice":           voice,
		"response_format": "opus",
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/audio/speech", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.deps.Config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.deps.Config.APIKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("tts status %d", resp.StatusCode)
	}

	br := bufio.NewReader(resp.Body)
	magic, peekErr := br.Peek(4)
	if peekErr == nil && string(magic) == "OggS" {
		reader := NewOggPacketReader(br)
		start := time.Now()
		var sent time.Duration
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			pkt, err := reader.NextPacket()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			dur := OpusPacketDuration(pkt)
			if wErr := c.deps.Sink.WriteAudioSample(pkt, dur); wErr != nil {
				return wErr
			}
			sent += dur
			if wait := sent - time.Since(start); wait > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(wait):
				}
			}
		}
		return nil
	}

	buf := make([]byte, 960)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, err := br.Read(buf)
		if n > 0 {
			if wErr := c.deps.Sink.WriteAudioSample(buf[:n], 20*time.Millisecond); wErr != nil {
				return wErr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Keep rag import used cleanly
var _ = rag.RetrievedChunk{}
