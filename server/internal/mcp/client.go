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
	ID              string
	Name            string
	Transport       string
	EndpointURL     string
	AuthHeader      string
	Enabled         bool
	AllowedTools    []string
	DiscoveredTools []ToolDef
	TimeoutMs       int
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
	raw, err := h.doInitializedJSONRPC(tCtx, srv, reqBody)
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
	raw, err := h.doInitializedJSONRPC(tCtx, srv, reqBody)
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
	raw, _, _, err := h.doJSONRPCResponse(ctx, srv, payload, "")
	return raw, err
}

func (h *Host) doInitializedJSONRPC(ctx context.Context, srv ServerConfig, payload map[string]any) ([]byte, error) {
	initialize := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "aegis-voice",
				"version": "1.0.0",
			},
		},
	}
	raw, sessionID, status, err := h.doJSONRPCResponse(ctx, srv, initialize, "")
	if err != nil {
		if status == http.StatusBadRequest || status == http.StatusNotFound || status == http.StatusMethodNotAllowed {
			return h.doJSONRPC(ctx, srv, payload)
		}
		return nil, err
	}

	var response struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("decode initialize response: %w", err)
	}
	if response.Error != nil {
		if response.Error.Code == -32601 {
			return h.doJSONRPC(ctx, srv, payload)
		}
		return nil, fmt.Errorf("mcp initialize error (%d): %s", response.Error.Code, security.RedactString(response.Error.Message))
	}
	if len(response.Result) == 0 || bytes.Equal(response.Result, []byte("null")) {
		return nil, errors.New("invalid mcp initialize response")
	}

	if sessionID != "" {
		notification := map[string]any{
			"jsonrpc": "2.0",
			"method":  "notifications/initialized",
		}
		_, _, _, _ = h.doJSONRPCResponse(ctx, srv, notification, sessionID)
	}
	result, _, _, err := h.doJSONRPCResponse(ctx, srv, payload, sessionID)
	return result, err
}

func (h *Host) doJSONRPCResponse(ctx context.Context, srv ServerConfig, payload map[string]any, sessionID string) ([]byte, string, int, error) {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.EndpointURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	if srv.AuthHeader != "" {
		req.Header.Set("Authorization", srv.AuthHeader)
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header.Get("Mcp-Session-Id"), resp.StatusCode, fmt.Errorf("mcp server returned status %d", resp.StatusCode)
	}

	sessionID = resp.Header.Get("Mcp-Session-Id")
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "text/event-stream") {
		scanner := bufio.NewScanner(io.LimitReader(resp.Body, 4*1024*1024))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if data != "" && data != "[DONE]" {
					return []byte(data), sessionID, resp.StatusCode, nil
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, sessionID, resp.StatusCode, err
		}
		return nil, sessionID, resp.StatusCode, errors.New("empty SSE stream from mcp server")
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	return raw, sessionID, resp.StatusCode, err
}
