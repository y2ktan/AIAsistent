package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`))
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

func TestMCPInitializeSessionHeaderAndStatelessFallback(t *testing.T) {
	var initCount int
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode JSON-RPC request: %v", err)
			return
		}
		methods = append(methods, req.Method)
		switch req.Method {
		case "initialize":
			initCount++
			sessionID := fmt.Sprintf("session-%d", initCount)
			w.Header().Set("Mcp-Session-Id", sessionID)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test","version":"1"}}}`)
		case "notifications/initialized":
			wantSession := fmt.Sprintf("session-%d", initCount)
			if got := r.Header.Get("Mcp-Session-Id"); got != wantSession {
				t.Errorf("initialized notification session = %q, want %q", got, wantSession)
			}
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			if got, want := r.Header.Get("Mcp-Session-Id"), "session-1"; got != want {
				t.Errorf("tools/list session = %q, want %q", got, want)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"get_weather","description":"Get weather","inputSchema":{"type":"object","properties":{"city":{"type":"string"}}}}]}}`))
		case "tools/call":
			if got, want := r.Header.Get("Mcp-Session-Id"), "session-2"; got != want {
				t.Errorf("tools/call session = %q, want %q", got, want)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Sunny"}]}}`))
		default:
			t.Errorf("unexpected method %q", req.Method)
		}
	}))
	defer srv.Close()

	host := NewHost(true)
	cfg := ServerConfig{
		Name:         "stateful",
		EndpointURL:  srv.URL,
		Enabled:      true,
		AllowedTools: []string{"get_weather"},
	}
	tools, err := host.DiscoverTools(context.Background(), cfg)
	if err != nil {
		t.Fatalf("DiscoverTools failed: %v", err)
	}
	if len(tools) != 1 || tools[0].InputSchema["type"] != "object" {
		t.Fatalf("unexpected discovered tools: %+v", tools)
	}
	output, err := host.CallTool(context.Background(), cfg, "get_weather", map[string]any{"city": "Tokyo"})
	if err != nil || output != "Sunny" {
		t.Fatalf("CallTool output = %q, error = %v", output, err)
	}
	wantMethods := []string{
		"initialize", "notifications/initialized", "tools/list",
		"initialize", "notifications/initialized", "tools/call",
	}
	if fmt.Sprint(methods) != fmt.Sprint(wantMethods) {
		t.Fatalf("methods = %v, want %v", methods, wantMethods)
	}
}

func TestMCPInitializeHTTPFailureFallsBackToStateless(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var listed bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Method string `json:"method"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				if req.Method == "initialize" {
					w.WriteHeader(status)
					return
				}
				if req.Method == "tools/list" {
					listed = true
					_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
					return
				}
				t.Errorf("unexpected request method %q", req.Method)
			}))
			defer srv.Close()

			_, err := NewHost(true).DiscoverTools(context.Background(), ServerConfig{EndpointURL: srv.URL})
			if err != nil {
				t.Fatalf("DiscoverTools failed: %v", err)
			}
			if !listed {
				t.Fatal("tools/list was not called after initialize fallback")
			}
		})
	}
}
