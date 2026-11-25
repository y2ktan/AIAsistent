package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"aegis-voice/server/internal/auth"
)

func TestHostedActivationProviderFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
	}{
		{name: "invalid credential", status: http.StatusUnauthorized},
		{name: "inaccessible agent", status: http.StatusNotFound},
		{name: "timeout"},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.status == 0 {
					<-r.Context().Done()
					return
				}
				w.WriteHeader(test.status)
				if _, err := w.Write([]byte(`{"secret":"private provider response"}`)); err != nil {
					t.Error(err)
				}
			}))
			defer upstream.Close()
			previous := elevenLabsAdminAPIBaseURL
			elevenLabsAdminAPIBaseURL = upstream.URL
			defer func() { elevenLabsAdminAPIBaseURL = previous }()
			store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
			for _, path := range []string{"/api/v1/admin/agent-config/test", "/api/v1/admin/agent-config"} {
				method := http.MethodPost
				if path == "/api/v1/admin/agent-config" {
					method = http.MethodPut
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
				request := httptest.NewRequest(method, path, strings.NewReader(`{"mode":"elevenlabs_agent"}`)).WithContext(ctx)
				request.AddCookie(cookie)
				request.Header.Set("Origin", "https://aegis.local")
				request.Header.Set("X-CSRF-Token", "fixture-csrf")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				cancel()
				if response.Code != http.StatusBadGateway || store.agentCfg.Mode != "cascaded" || strings.Contains(response.Body.String(), "private provider") {
					t.Fatalf("activation failure changed configuration or leaked payload: %d %s", response.Code, response.Body.String())
				}
			}
		})
	}
}

func TestHostedTokenDeadline(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/convai/conversation/token" {
			<-r.Context().Done()
			return
		}
		if _, err := w.Write([]byte(hostedFixture)); err != nil {
			t.Error(err)
		}
	}))
	defer upstream.Close()
	previous := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = previous }()
	store, router, cookie := newHostedAPITest(t, auth.RoleUser)
	store.agentCfg.Mode = "elevenlabs_agent"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/elevenlabs/conversation-token", strings.NewReader(`{}`)).WithContext(ctx)
	request.AddCookie(cookie)
	request.Header.Set("Origin", "https://aegis.local")
	request.Header.Set("X-CSRF-Token", "fixture-csrf")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "timed out") {
		t.Fatalf("token deadline not surfaced: %d %s", response.Code, response.Body.String())
	}
}

func TestHostedTokenFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		agent  string
		key    bool
		want   int
	}{
		{name: "unauthorized provider", status: 401, key: true, want: 502},
		{name: "payment", status: 402, key: true, want: 502},
		{name: "rate limited", status: 429, key: true, want: 502},
		{name: "upstream failure", status: 500, key: true, want: 502},
		{name: "incomplete token", body: `{}`, key: true, want: 502},
		{name: "invalid token json", body: `{`, key: true, want: 502},
		{name: "dashboard tool changed", agent: strings.Replace(hostedFixture, `"tools":[]`, `"tools":[{"type":"webhook"}]`, 1), key: true, want: 400},
		{name: "corrupt credential", want: 400},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var tokenCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := hostedFixture
				if test.agent != "" {
					body = test.agent
				}
				if r.URL.Path == "/v1/convai/conversation/token" {
					tokenCalls.Add(1)
					if test.status != 0 {
						w.WriteHeader(test.status)
						body = `{"detail":"secret upstream payload"}`
					} else {
						body = test.body
					}
				}
				if _, err := w.Write([]byte(body)); err != nil {
					t.Error(err)
				}
			}))
			defer upstream.Close()
			previous := elevenLabsAdminAPIBaseURL
			elevenLabsAdminAPIBaseURL = upstream.URL
			defer func() { elevenLabsAdminAPIBaseURL = previous }()
			store, router, cookie := newHostedAPITest(t, auth.RoleUser)
			store.agentCfg.Mode = "elevenlabs_agent"
			if !test.key {
				store.agentCfg.ElevenLabsAPIKeyCiphertext = []byte("corrupt")
			}
			response := hostedAPIRequest(router, cookie, http.MethodPost, "/api/v1/elevenlabs/conversation-token", `{}`)
			if response.Code != test.want ||
				response.Header().Get("Cache-Control") != "no-store" ||
				strings.Contains(response.Body.String(), "secret upstream") ||
				strings.Contains(response.Body.String(), hostedFixtureKey) {
				t.Fatalf("unsafe or incorrect failure: %d %s", response.Code, response.Body.String())
			}
			if test.want == 400 && tokenCalls.Load() != 0 {
				t.Fatal("incompatible configuration issued a token")
			}
		})
	}
}

func TestHostedActivationValidationAndCandidateTest(t *testing.T) {
	var agentResponse atomic.Value
	agentResponse.Store(hostedFixture)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(agentResponse.Load().(string))); err != nil {
			t.Error(err)
		}
	}))
	defer upstream.Close()
	previous := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = previous }()
	store, router, cookie := newHostedAPITest(t, auth.RoleAdmin)
	response := hostedAPIRequest(router, cookie, http.MethodPost, "/api/v1/admin/agent-config/test", `{"mode":"elevenlabs_agent","elevenLabsAgentId":"agent_fixture","elevenLabsApiKey":"candidate-key"}`)
	if response.Code != http.StatusOK || store.agentCfg.Mode != "cascaded" {
		t.Fatalf("candidate test persisted configuration or failed: %d %s", response.Code, response.Body.String())
	}
	for _, body := range []string{
		`{"mode":"elevenlabs_agent","elevenLabsAgentId":""}`,
		`{"mode":"elevenlabs_agent","elevenLabsAgentId":"../other"}`,
		`{"mode":"elevenlabs_agent","elevenLabsApiKey":"short"}`,
	} {
		response = hostedAPIRequest(router, cookie, http.MethodPut, "/api/v1/admin/agent-config", body)
		if response.Code != http.StatusBadRequest || store.agentCfg.Mode != "cascaded" {
			t.Fatalf("invalid activation succeeded: %d %s", response.Code, response.Body.String())
		}
	}
	agentResponse.Store(strings.Replace(hostedFixture, `"enable_auth":true`, `"enable_auth":false`, 1))
	response = hostedAPIRequest(router, cookie, http.MethodPut, "/api/v1/admin/agent-config", `{"mode":"elevenlabs_agent"}`)
	if response.Code != http.StatusBadRequest || store.agentCfg.Mode != "cascaded" {
		t.Fatal("public agent activation changed configuration")
	}
}

func TestHostedRequestCancellationAndRedirect(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer upstream.Close()
	previous := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = previous }()
	var out hostedAgent
	if err := hostedRequest(context.Background(), newHostedClient(true), hostedFixtureKey, "/fixture", &out); err == nil || redirected.Load() != 0 {
		t.Fatal("provider redirect forwarded credentials")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := hostedRequest(ctx, newHostedClient(true), hostedFixtureKey, "/fixture", &out); err == nil {
		t.Fatal("cancelled request succeeded")
	}
}
