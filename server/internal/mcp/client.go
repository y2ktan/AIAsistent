package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"aegis-voice/server/internal/security"
)

var (
	ErrServerDisabled   = errors.New("mcp server is disabled")
	ErrToolNotPermitted = errors.New("tool is not in server allowlist")
)

type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

type ServerConfig struct {
	ID           string
	Name         string
	Transport    string
	EndpointURL  string
	AuthHeader   string
	Enabled      bool
	AllowedTools []string
	TimeoutMs    int
}

type Host struct {
	allowPrivate bool
	httpClient   *http.Client
}

func NewHost(allowPrivate bool) *Host {
	return &Host{
		allowPrivate: allowPrivate,
		httpClient:   security.NewSafeHTTPClient(allowPrivate, 30*time.Second),
	}
}

func (h *Host) effectiveTimeout(ms int) time.Duration {
	if ms <= 0 {
		return 10 * time.Second
	}
	if ms > 60000 {
		return 60 * time.Second
	}
	return time.Duration(ms) * time.Millisecond
}

func (h *Host) DiscoverTools(ctx context.Context, srv ServerConfig) ([]ToolDef, error) {
	policy := security.SSRFPolicy{AllowPrivate: h.allowPrivate}
	if _, err := policy.ValidateHTTPURL(srv.EndpointURL); err != nil {
		return nil, err
	}

	tCtx, cancel := context.WithTimeout(ctx, h.effectiveTimeout(srv.TimeoutMs))
	defer cancel()

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
		"params":  map[string]any{},
	}
	raw, err := h.doJSONRPC(tCtx, srv, reqBody)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Result struct {
			Tools []ToolDef `json:"tools"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode tools/list response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("mcp error (%d): %s", resp.Error.Code, security.RedactString(resp.Error.Message))
	}
	return resp.Result.Tools, nil
}

func (h *Host) CallTool(ctx context.Context, srv ServerConfig, toolName string, args map[string]any) (string, error) {
	if !srv.Enabled {
		return "", ErrServerDisabled
	}
	allowed := false
	for _, t := range srv.AllowedTools {
		if t == toolName {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("%w: %s", ErrToolNotPermitted, toolName)
	}

	policy := security.SSRFPolicy{AllowPrivate: h.allowPrivate}
	if _, err := policy.ValidateHTTPURL(srv.EndpointURL); err != nil {
		return "", err
	}

	tCtx, cancel := context.WithTimeout(ctx, h.effectiveTimeout(srv.TimeoutMs))
	defer cancel()

	if args == nil {
		args = map[string]any{}
	}
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}
	raw, err := h.doJSONRPC(tCtx, srv, reqBody)
	if err != nil {
		return "", err
	}

	var resp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("decode tools/call response: %w", err)
	}
	if resp.Error != nil {
		return "", fmt.Errorf("mcp error (%d): %s", resp.Error.Code, security.RedactString(resp.Error.Message))
	}

	var out strings.Builder
	for _, c := range resp.Result.Content {
		if c.Text != "" {
			if out.Len() > 0 {
				out.WriteByte('\n')
			}
			out.WriteString(security.RedactString(c.Text))
		}
	}
	return out.String(), nil
}

func (h *Host) doJSONRPC(ctx context.Context, srv ServerConfig, payload map[string]any) ([]byte, error) {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.EndpointURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if srv.AuthHeader != "" {
		req.Header.Set("Authorization", srv.AuthHeader)
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("mcp server returned status %d", resp.StatusCode)
	}

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "text/event-stream") {
		scanner := bufio.NewScanner(io.LimitReader(resp.Body, 4*1024*1024))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if data != "" && data != "[DONE]" {
					return []byte(data), nil
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("empty SSE stream from mcp server")
	}

	return io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
}
