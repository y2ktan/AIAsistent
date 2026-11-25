package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const hostedWorkflowFixture = `{"nodes":{"start":{"type":"start","position":{"x":0,"y":0},"edge_order":["first"]},"action":{"type":"tool","position":{"x":1,"y":0},"edge_order":["last"],"tools":[{"tool_id":"tool_fixture"}]},"end":{"type":"end","position":{"x":2,"y":0},"edge_order":[],"outcome":"success"}},"edges":{"first":{"source":"start","target":"action","forward_condition":{"type":"unconditional","label":null},"backward_condition":null,"reroutes":[]},"last":{"source":"action","target":"end","forward_condition":{"type":"result","label":null,"successful":true},"backward_condition":null,"reroutes":[]}},"prevent_subagent_loops":true}`
const hostedWebhookFixture = `{"id":"tool_fixture","tool_config":{"type":"webhook","name":"lookup","api_schema":{"url":"https://example.com/lookup","method":"POST"}}}`
const hostedOverrideWorkflowFixture = `{"version":1,"nodes":{"start_node":{"type":"start","position":{"x":0,"y":0},"edge_order":["start_to_support"]},"support_agent":{"type":"override_agent","position":{"x":1,"y":0},"edge_order":["support_to_end"],"conversation_config":{"agent":{"prompt":{"tool_ids":["tool_fixture"],"tools":[],"mcp_server_ids":[],"native_mcp_server_ids":[]}}},"additional_prompt":"Help with support.","additional_knowledge_base":[],"additional_tool_ids":[],"label":"Support agent","entry_behavior":"auto","allow_interruptions_on_entry":false},"end_node":{"type":"end","position":{"x":2,"y":0},"edge_order":[],"outcome":"success"}},"edges":{"start_to_support":{"source":"start_node","target":"support_agent","forward_condition":{"type":"unconditional","label":null},"backward_condition":null,"reroutes":[]},"support_to_end":{"source":"support_agent","target":"end_node","forward_condition":{"type":"llm","label":null,"condition":"Resolved."},"backward_condition":null,"reroutes":[]}},"prevent_subagent_loops":false,"subgraphs":{}}`

func TestHostedWorkflowGraph(t *testing.T) {
	for _, tt := range []struct {
		name, raw string
		ok        bool
	}{
		{"supported graph", hostedWorkflowFixture, true},
		{"llm edge", strings.Replace(hostedWorkflowFixture, `"type":"unconditional","label":null`, `"type":"llm","label":null,"condition":"Continue"`, 1), true},
		{"dangling edge", strings.Replace(hostedWorkflowFixture, `"target":"action"`, `"target":"missing"`, 1), false},
		{"missing order", strings.Replace(hostedWorkflowFixture, `"edge_order":["first"]`, `"edge_order":[]`, 1), false},
		{"duplicate edge", strings.Replace(hostedWorkflowFixture, `"edge_order":["first"]`, `"edge_order":["first","first"]`, 1), false},
		{"client node", strings.Replace(hostedWorkflowFixture, `"type":"tool"`, `"type":"client"`, 1), false},
		{"standalone transfer", strings.Replace(hostedWorkflowFixture, `"type":"tool"`, `"type":"standalone_agent"`, 1), false},
		{"unknown condition", strings.Replace(hostedWorkflowFixture, `"type":"result"`, `"type":"future"`, 1), false},
		{"invalid tool id", strings.Replace(hostedWorkflowFixture, `"tool_fixture"`, `"../tool"`, 1), false},
		{"missing start", strings.Replace(hostedWorkflowFixture, `"type":"start"`, `"type":"tool"`, 1), false},
		{"missing position", strings.Replace(hostedWorkflowFixture, `"position":{"x":0,"y":0}`, `"position":{}`, 1), false},
		{"supported override", strings.Replace(hostedWorkflowFixture, `"tool_id":"tool_fixture"`, `"tool_id":"tool_fixture","schema_overrides":{"argument":{"source":"constant","value":"fixed"}}`, 1), true},
		{"unsupported override", strings.Replace(hostedWorkflowFixture, `"tool_id":"tool_fixture"`, `"tool_id":"tool_fixture","schema_overrides":{"argument":{"source":"future"}}`, 1), false},
		{"null nodes", `{"nodes":null,"edges":{}}`, false},
		{"trailing data", hostedWorkflowFixture + `{}`, false},
		{"empty", `{"nodes":{},"edges":{}}`, true},
		{"default no-op start-to-end", `{"nodes":{"start_node":{"type":"start","edge_order":["finish"]},"end_node":{"type":"end"}},"edges":{"finish":{"source":"start_node","target":"end_node","forward_condition":{"type":"unconditional"}}}}`, true},
		{"provider metadata", strings.TrimSuffix(hostedWorkflowFixture, "}") + `,"version":1,"subgraphs":{}}`, true},
		{"future provider metadata", strings.TrimSuffix(hostedWorkflowFixture, "}") + `,"version":2,"provider_ui_state":{"saved":true}}`, true},
		{"dashboard start-only graph", `{"nodes":{"start_node":{"type":"start","position":{"x":-680.5,"y":-40},"edge_order":[],"ui":{"selected":true}}},"edges":{},"prevent_subagent_loops":false,"version":7,"provider_ui_state":{"canvas":"workflow"}}`, true},
		{"override agent", hostedOverrideWorkflowFixture, true},
		{"expression edge", strings.Replace(hostedWorkflowFixture, `"type":"unconditional","label":null`, `"type":"expression","label":null,"expression":{"type":"boolean","value":true}`, 1), true},
		{"nonempty subgraphs", strings.TrimSuffix(hostedWorkflowFixture, "}") + `,"subgraphs":{"nested":{}}}`, false},
		{"invalid override tool id", strings.Replace(hostedOverrideWorkflowFixture, `"tool_fixture"`, `"../tool"`, 1), false},
		{"client tool in override agent", strings.Replace(hostedOverrideWorkflowFixture, `"tools":[]`, `"tools":[{"type":"client","name":"lookup"}]`, 1), false},
		{"null", `null`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := hostedWorkflowTools(json.RawMessage(tt.raw))
			if (err == nil) != tt.ok {
				t.Fatalf("expected ok=%v, got %v", tt.ok, err)
			}
		})
	}
}

func TestHostedOverrideAgentToolInspection(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v1/convai/tools/"):
			_, _ = w.Write([]byte(hostedWebhookFixture))
		default:
			_, _ = w.Write([]byte(strings.TrimSuffix(hostedFixture, "}") + `,"workflow":` + hostedOverrideWorkflowFixture + `}`))
		}
	}))
	defer upstream.Close()
	old := elevenLabsAdminAPIBaseURL
	elevenLabsAdminAPIBaseURL = upstream.URL
	defer func() { elevenLabsAdminAPIBaseURL = old }()

	config, err := loadHostedAgent(context.Background(), newHostedClient(true), hostedFixtureKey, "agent_fixture")
	if err != nil {
		t.Fatalf("official override workflow was rejected: %v", err)
	}
	if len(config.ValidatedTools) != 1 || config.ValidatedTools[0].Name != "lookup" {
		t.Fatalf("override workflow tool was not resolved: %#v", config.ValidatedTools)
	}
}

func TestHostedWorkflowResourceInspection(t *testing.T) {
	for _, tt := range []struct {
		name, tool, resource string
		status               int
		ok                   bool
	}{
		{"webhook", hostedWebhookFixture, "", 200, true},
		{"client", `{"id":"tool_fixture","tool_config":{"type":"client","name":"lookup"}}`, "", 200, false},
		{"unknown", `{"id":"tool_fixture","tool_config":{"type":"future"}}`, "", 200, false},
		{"tool mismatch", strings.Replace(hostedWebhookFixture, `"id":"tool_fixture"`, `"id":"other"`, 1), "", 200, false},
		{"missing tool", "", "", 404, false},
		{"mcp", `{"id":"tool_fixture","tool_config":{"type":"mcp","name":"lookup","mcp_tool_name":"lookup","mcp_server_id":"mcp_fixture"}}`, `{"id":"mcp_fixture","config":{"url":"https://example.com/mcp","transport":"STREAMABLE_HTTP"}}`, 200, true},
		{"mcp mismatch", `{"id":"tool_fixture","tool_config":{"type":"mcp","name":"lookup","mcp_tool_name":"lookup","mcp_server_id":"mcp_fixture"}}`, `{"id":"other","config":{"url":"https://example.com/mcp","transport":"SSE"}}`, 200, false},
		{"missing mcp", `{"id":"tool_fixture","tool_config":{"type":"mcp","name":"lookup","mcp_tool_name":"lookup","mcp_server_id":"mcp_fixture"}}`, `{}`, 200, false},
		{"null mcp URL", `{"id":"tool_fixture","tool_config":{"type":"mcp","name":"lookup","mcp_tool_name":"lookup","mcp_server_id":"mcp_fixture"}}`, `{"id":"mcp_fixture","config":{"url":null,"transport":"STREAMABLE_HTTP"}}`, 200, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/tools") && strings.HasPrefix(r.URL.Path, "/v1/convai/mcp-servers/"):
					_, _ = w.Write([]byte(`{"success":true,"tools":[{"name":"lookup","inputSchema":{"type":"object"}}]}`))
				case strings.HasPrefix(r.URL.Path, "/v1/convai/tools/"):
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.tool))
				case strings.HasPrefix(r.URL.Path, "/v1/convai/mcp-servers/"):
					_, _ = w.Write([]byte(tt.resource))
				default:
					_, _ = w.Write([]byte(strings.TrimSuffix(hostedFixture, "}") + `,"workflow":` + hostedWorkflowFixture + `}`))
				}
			}))
			defer upstream.Close()
			old := elevenLabsAdminAPIBaseURL
			elevenLabsAdminAPIBaseURL = upstream.URL
			defer func() { elevenLabsAdminAPIBaseURL = old }()
			_, err := inspectHostedAgent(context.Background(), newHostedClient(true), hostedFixtureKey, "agent_fixture")
			if (err == nil) != tt.ok {
				t.Fatalf("expected ok=%v, got %v", tt.ok, err)
			}
		})
	}
}
