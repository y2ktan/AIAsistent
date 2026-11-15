package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aegis-voice/server/internal/security"
)

func TestMCPDiscoveryAllowlistTimeoutAndSSRF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		switch req.Method {
		case "tools/list":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[{\"name\":\"get_weather\",\"description\":\"Get weather\"},{\"name\":\"delete_db\",\"description\":\"Dangerous\"}]}}\n\n"))
		case "tools/call":
			if req.Params.Name == "slow_tool" {
				time.Sleep(250 * time.Millisecond)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Sunny 72F"}]}}`))
		}
	}))
	defer srv.Close()

	// 1. Strict SSRF blocks loopback httptest server
	strictHost := NewHost(false)
	_, err := strictHost.DiscoverTools(context.Background(), ServerConfig{
		EndpointURL: srv.URL,
		Enabled:     true,
	})
	if !errors.Is(err, security.ErrBlockedSSRF) {
		t.Fatalf("expected ErrBlockedSSRF for loopback when allowPrivate=false, got %v", err)
	}

	// Metadata IP also blocked
	_, err = strictHost.DiscoverTools(context.Background(), ServerConfig{
		EndpointURL: "http://169.254.169.254/latest/meta-data",
		Enabled:     true,
	})
	if !errors.Is(err, security.ErrBlockedSSRF) {
		t.Fatalf("expected ErrBlockedSSRF for metadata IP, got %v", err)
	}

	// 2. Permissive host discovers SSE tools
	host := NewHost(true)
	cfg := ServerConfig{
		Name:         "test-mcp",
		EndpointURL:  srv.URL,
		Enabled:      true,
		AllowedTools: []string{"get_weather", "slow_tool"},
		TimeoutMs:    80,
	}
	tools, err := host.DiscoverTools(context.Background(), cfg)
	if err != nil || len(tools) != 2 {
		t.Fatalf("expected 2 discovered tools, got %+v err=%v", tools, err)
	}

	// 3. Permitted tool succeeds
	res, err := host.CallTool(context.Background(), cfg, "get_weather", map[string]any{"city": "NYC"})
	if err != nil || res != "Sunny 72F" {
		t.Fatalf("expected Sunny 72F, got %q err=%v", res, err)
	}

	// 4. Unpermitted tool blocked by allowlist
	_, err = host.CallTool(context.Background(), cfg, "delete_db", nil)
	if !errors.Is(err, ErrToolNotPermitted) {
		t.Fatalf("expected ErrToolNotPermitted for delete_db, got %v", err)
	}

	// 5. Bounded timeout enforced
	_, err = host.CallTool(context.Background(), cfg, "slow_tool", nil)
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
}
