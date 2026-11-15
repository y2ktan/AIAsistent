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

	"github.com/pion/webrtc/v4/pkg/media/oggreader"

	"aegis-voice/server/internal/mcp"
	"aegis-voice/server/internal/rag"
	"aegis-voice/server/internal/rtc"
	"aegis-voice/server/internal/security"
)

type CascadedAdapter struct {
	deps       Dependencies
	httpClient *http.Client
	mu         sync.Mutex
	turnCancel context.CancelFunc
	speaking   bool
}

func NewCascadedAdapter(deps Dependencies) *CascadedAdapter {
	return &CascadedAdapter{
		deps:       deps,
		// Generous timeout: self-hosted CPU models can take well over a minute on cold start.
		httpClient: security.NewSafeHTTPClient(deps.Config.AllowPrivate, 180*time.Second),
	}
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
	c.speaking = false
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	c.deps.Sink.SendEvent(rtc.NewBargeInEvent())
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
	c.BargeIn()
	return nil
}

func (c *CascadedAdapter) ProcessTurn(ctx context.Context, audioData []byte) error {
	c.BargeIn()

	turnCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.turnCancel = cancel
	c.mu.Unlock()
	defer cancel()

	cfg := c.deps.Config
	c.deps.Sink.SendEvent(rtc.NewStateEvent("thinking"))

	// 1. Speech-to-Text
	userText, err := c.transcribe(turnCtx, audioData)
	if err != nil {
		// A barge-in cancels the in-flight turn; that is not a user-facing failure.
		if turnCtx.Err() != nil {
			return turnCtx.Err()
		}
		c.deps.Sink.SendEvent(rtc.NewErrorEvent("Speech transcription failed."))
		c.deps.Sink.SendEvent(rtc.NewStateEvent("listening"))
		return err
	}
	if strings.TrimSpace(userText) == "" {
		c.deps.Sink.SendEvent(rtc.NewStateEvent("listening"))
		return nil
	}
	c.deps.Sink.SendEvent(rtc.NewTranscriptEvent("transcript.final", "user", userText))

	// 2. RAG Retrieval
	var ragContext string
	if c.deps.RAGService != nil && c.deps.Embedder != nil {
		c.deps.Sink.SendEvent(rtc.NewStateEvent("retrieving"))
		hits, err := c.deps.RAGService.Retrieve(turnCtx, userText, cfg.RAGTopK, cfg.RAGMinSimilarity, c.deps.Embedder)
		if err == nil && len(hits) > 0 {
			c.deps.Sink.SendEvent(rtc.Event{
				Type:      "citation",
				Citations: hits,
				Timestamp: time.Now().UnixMilli(),
			})
			ragContext = WrapUntrustedRAGContext(hits)
		}
	}

	// 3. Vision frame + LLM Chat Completions with MCP tool loop
	c.deps.Sink.SendEvent(rtc.NewStateEvent("thinking"))
	var videoFrame []byte
	if frame, ok := c.deps.Sink.LatestVideoFrame(); ok {
		videoFrame = frame
	}

	reply, err := c.chatWithTools(turnCtx, userText, ragContext, videoFrame)
	if err != nil {
		if errors.Is(err, context.Canceled) || turnCtx.Err() != nil {
			return err
		}
		c.deps.Sink.SendEvent(rtc.NewErrorEvent("Assistant failed to generate a response."))
		c.deps.Sink.SendEvent(rtc.NewStateEvent("listening"))
		return err
	}

	reply = SanitizeAssistantOutput(reply, []string{cfg.APIKey})
	c.deps.Sink.SendEvent(rtc.NewTranscriptEvent("transcript.final", "assistant", reply))

	// 4. Streaming TTS
	if cfg.TTSBaseURL != "" && reply != "" {
		c.deps.Sink.SendEvent(rtc.NewStateEvent("speaking"))
		c.setSpeaking(true)
		err := c.synthesizeAndStream(turnCtx, reply)
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

func (c *CascadedAdapter) transcribe(ctx context.Context, audioData []byte) (string, error) {
	baseURL := strings.TrimRight(c.deps.Config.STTBaseURL, "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(c.deps.Config.LLMBaseURL, "/")
	}
	policy := security.SSRFPolicy{AllowPrivate: c.deps.Config.AllowPrivate}
	if _, err := policy.ValidateHTTPURL(baseURL); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	model := c.deps.Config.STTModel
	if model == "" {
		model = "whisper-1"
	}
	_ = mw.WriteField("model", model)
	filename := "audio.wav"
	if bytes.HasPrefix(audioData, []byte("OggS")) {
		filename = "audio.ogg"
	}
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	_, _ = fw.Write(audioData)
	_ = mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/audio/transcriptions", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if c.deps.Config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.deps.Config.APIKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("stt status %d", resp.StatusCode)
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&out); err != nil {
		return "", err
	}
	return out.Text, nil
}

func (c *CascadedAdapter) chatWithTools(ctx context.Context, userText, ragContext string, videoFrame []byte) (string, error) {
	baseURL := strings.TrimRight(c.deps.Config.LLMBaseURL, "/")
	policy := security.SSRFPolicy{AllowPrivate: c.deps.Config.AllowPrivate}
	if _, err := policy.ValidateHTTPURL(baseURL); err != nil {
		return "", err
	}

	messages := []map[string]any{
		{"role": "system", "content": BuildSystemPrompt(c.deps.Config.SystemInstructions)},
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

	// Build tool definitions from enabled MCP servers
	var tools []map[string]any
	toolToServer := make(map[string]mcp.ServerConfig)
	for _, srv := range c.deps.MCPServers {
		if !srv.Enabled {
			continue
		}
		for _, tName := range srv.AllowedTools {
			toolToServer[tName] = srv
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tName,
					"description": "MCP tool from " + srv.Name,
					"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
				},
			})
		}
	}

	model := c.deps.Config.LLMModel
	if model == "" {
		model = "gpt-4o"
	}

	for round := 0; round < 3; round++ {
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
			if srv, ok := toolToServer[tc.Function.Name]; ok && c.deps.MCPHost != nil {
				var args map[string]any
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
				res, callErr := c.deps.MCPHost.CallTool(ctx, srv, tc.Function.Name, args)
				if callErr != nil {
					toolOut = "tool execution failed: " + security.RedactString(callErr.Error())
				} else {
					toolOut = WrapUntrustedToolOutput(tc.Function.Name, res)
				}
			} else {
				toolOut = "tool not permitted"
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

func (c *CascadedAdapter) synthesizeAndStream(ctx context.Context, text string) error {
	baseURL := strings.TrimRight(c.deps.Config.TTSBaseURL, "/")
	policy := security.SSRFPolicy{AllowPrivate: c.deps.Config.AllowPrivate}
	if _, err := policy.ValidateHTTPURL(baseURL); err != nil {
		return err
	}

	model := c.deps.Config.TTSModel
	if model == "" {
		model = "tts-1"
	}
	voice := c.deps.Config.TTSVoice
	if voice == "" {
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
		ogg, _, oggErr := oggreader.NewWith(br)
		if oggErr != nil {
			return oggErr
		}
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			payload, pageHeader, err := ogg.ParseNextPage()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if pageHeader.GranulePosition == 0 ||
				bytes.HasPrefix(payload, []byte("OpusHead")) ||
				bytes.HasPrefix(payload, []byte("OpusTags")) {
				continue
			}
			if len(payload) > 0 {
				if wErr := c.deps.Sink.WriteAudioSample(payload, 20*time.Millisecond); wErr != nil {
					return wErr
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(20 * time.Millisecond):
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
