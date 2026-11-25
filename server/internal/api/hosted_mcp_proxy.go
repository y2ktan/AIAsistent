package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"aegis-voice/server/internal/mcp"
)

func (s *Server) HandleHostedMCPProxy(w http.ResponseWriter, r *http.Request) {
	rec, err := s.adminStore.GetMCPServer(r.Context(), r.PathValue("id"))
	if err != nil || rec == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid hosted MCP credential."})
		return
	}
	expected, err := s.vault.Decrypt(rec.HostedProxyCredential)
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	a, b := sha256.Sum256([]byte(expected)), sha256.Sum256([]byte(provided))
	if err != nil || expected == "" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || subtle.ConstantTimeCompare(a[:], b[:]) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid hosted MCP credential."})
		return
	}
	if !rec.Enabled || rec.HostedMode != "proxy" || (rec.HostedSyncStatus != "synced" && rec.HostedSyncStatus != "pending") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Hosted MCP integration is unavailable."})
		return
	}
	if !s.hostedProxyLimiter.Allow(rec.ID) {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "Hosted MCP rate limit exceeded."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(10 * time.Second))
	defer func() { _ = http.NewResponseController(w).SetReadDeadline(time.Time{}) }()
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	d.DisallowUnknownFields()
	if d.Decode(&req) != nil || req.JSONRPC != "2.0" || !errors.Is(d.Decode(new(any)), io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid bounded JSON-RPC request."})
		return
	}
	if req.Method == "notifications/initialized" && len(req.ID) == 0 {
		if len(req.Params) != 0 && decodeHostedStrict(req.Params, new(struct{})) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid initialization notification."})
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var id any
	if json.Unmarshal(req.ID, &id) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON-RPC request ID is required."})
		return
	}
	switch v := id.(type) {
	case string:
		if len(v) > 128 {
			writeJSON(w, 400, map[string]string{"error": "Invalid JSON-RPC ID."})
			return
		}
	case float64:
	default:
		writeJSON(w, 400, map[string]string{"error": "Invalid JSON-RPC ID."})
		return
	}
	reply := func(result any) {
		writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	rpcError := func(code int, message string) {
		writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string                     `json:"protocolVersion"`
			Capabilities    map[string]json.RawMessage `json:"capabilities"`
			ClientInfo      struct {
				Name    string `json:"name"`
				Version string `json:"version"`
				Title   string `json:"title,omitempty"`
			} `json:"clientInfo"`
		}
		if decodeHostedStrict(req.Params, &p) != nil || p.ProtocolVersion == "" || p.ClientInfo.Name == "" {
			rpcError(-32602, "Invalid initialization parameters.")
			return
		}
		reply(map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "aegis-hosted-mcp", "version": "1.0.0"}})
	case "tools/list":
		if len(req.Params) != 0 {
			var p struct{}
			if decodeHostedStrict(req.Params, &p) != nil {
				rpcError(-32602, "Unsupported tool-list parameters.")
				return
			}
		}
		tools := make([]mcp.ToolDef, 0)
		allowed := allowedHostedMCPTools(rec)
		for _, tool := range rec.DiscoveredTools {
			if allowed[tool.Name] {
				tools = append(tools, tool)
			}
		}
		reply(map[string]any{"tools": tools})
	case "tools/call":
		var p struct {
			Name      string                     `json:"name"`
			Arguments map[string]any             `json:"arguments"`
			Meta      map[string]json.RawMessage `json:"_meta,omitempty"`
		}
		if decodeHostedStrict(req.Params, &p) != nil || p.Name == "" {
			rpcError(-32602, "Invalid tool-call parameters.")
			return
		}
		if rec.HostedSyncStatus != "synced" || !allowedHostedMCPTools(rec)[p.Name] {
			rpcError(-32602, "Tool is unavailable or not allowlisted.")
			return
		}
		authHeader := ""
		if len(rec.AuthHeaderCiphertext) > 0 {
			authHeader, err = s.vault.Decrypt(rec.AuthHeaderCiphertext)
			if err != nil {
				rpcError(-32603, "Could not load upstream authentication.")
				return
			}
		}
		text, err := s.mcpHost.CallTool(ctx, mcp.ServerConfig{ID: rec.ID, Name: rec.Name, Transport: rec.Transport, EndpointURL: rec.EndpointURL, AuthHeader: authHeader, Enabled: rec.Enabled, AllowedTools: rec.AllowedTools, DiscoveredTools: rec.DiscoveredTools, TimeoutMs: rec.TimeoutMs}, p.Name, p.Arguments)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("hosted MCP call failed", "serverId", rec.ID)
			}
			rpcError(-32603, "Upstream MCP tool call failed.")
			return
		}
		text = strings.ReplaceAll(text, expected, "[REDACTED]")
		if authHeader != "" {
			text = strings.ReplaceAll(text, authHeader, "[REDACTED]")
			if strings.HasPrefix(authHeader, "Bearer ") {
				text = strings.ReplaceAll(text, strings.TrimPrefix(authHeader, "Bearer "), "[REDACTED]")
			}
		}
		if len(text) > 256*1024 {
			rpcError(-32603, "Upstream MCP result exceeded the response limit.")
			return
		}
		reply(map[string]any{"content": []map[string]string{{"type": "text", "text": text}}, "isError": false})
	default:
		rpcError(-32601, "Unsupported MCP method.")
	}
}
