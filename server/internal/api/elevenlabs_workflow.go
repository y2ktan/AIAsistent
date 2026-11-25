package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type hostedPosition struct {
	X *float64 `json:"x"`
	Y *float64 `json:"y"`
}

type hostedWorkflowNode struct {
	Type              string          `json:"type"`
	Position          *hostedPosition `json:"position"`
	EdgeOrder         *[]string       `json:"edge_order"`
	Outcome           string          `json:"outcome,omitempty"`
	AdditionalToolIDs []string        `json:"additional_tool_ids,omitempty"`
	Conversation      json.RawMessage `json:"conversation_config,omitempty"`
	AdditionalPrompt  string          `json:"additional_prompt,omitempty"`
	Label             string          `json:"label,omitempty"`
	EntryBehavior     string          `json:"entry_behavior,omitempty"`
	AllowInterrupt    bool            `json:"allow_interruptions_on_entry,omitempty"`
	AdditionalKB      json.RawMessage `json:"additional_knowledge_base,omitempty"`
	Tools             []struct {
		ToolID          string          `json:"tool_id"`
		SchemaOverrides json.RawMessage `json:"schema_overrides,omitempty"`
	} `json:"tools,omitempty"`
}

type hostedWorkflowCondition struct {
	Type       string          `json:"type"`
	Label      *string         `json:"label"`
	Condition  string          `json:"condition,omitempty"`
	Successful *bool           `json:"successful,omitempty"`
	Expression json.RawMessage `json:"expression,omitempty"`
}

type hostedWorkflowEdge struct {
	Source   string                   `json:"source"`
	Target   string                   `json:"target"`
	Forward  *hostedWorkflowCondition `json:"forward_condition"`
	Backward *hostedWorkflowCondition `json:"backward_condition"`
	Reroutes []hostedPosition         `json:"reroutes"`
}

func decodeHostedStrict(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func hostedWorkflowError(reason string) error {
	return fmt.Errorf("The hosted workflow is malformed or unsupported (%s). Supported nodes: start, end, override_agent, tool; conditions: unconditional, llm, result, expression.", reason)
}

func validateHostedWorkflow(raw json.RawMessage) error {
	_, err := hostedWorkflowTools(raw)
	return err
}

func hostedWorkflowTools(raw json.RawMessage) ([]string, error) {
	inventory, err := inspectHostedWorkflow(raw)
	if err != nil {
		return nil, err
	}
	return inventory.ToolIDs, nil
}

type hostedWorkflowInventory struct {
	ToolIDs      []string
	Tools        []hostedTool
	MCPServerIDs []string
}

func inspectHostedWorkflow(raw json.RawMessage) (hostedWorkflowInventory, error) {
	var inventory hostedWorkflowInventory
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return inventory, nil
	}
	var graph struct {
		Nodes        map[string]hostedWorkflowNode `json:"nodes"`
		Edges        map[string]hostedWorkflowEdge `json:"edges"`
		PreventLoops bool                          `json:"prevent_subagent_loops"`
		Subgraphs    map[string]json.RawMessage    `json:"subgraphs,omitempty"`
	}
	invalidShape := hostedWorkflowError("invalid graph structure")
	if len(raw) > 256*1024 {
		return inventory, hostedWorkflowError("workflow exceeds the size limit")
	}
	if json.Unmarshal(raw, &graph) != nil {
		return inventory, invalidShape
	}
	if graph.Nodes == nil || graph.Edges == nil {
		return inventory, hostedWorkflowError("nodes or edges are missing")
	}
	if len(graph.Nodes) > 128 || len(graph.Edges) > 256 {
		return inventory, hostedWorkflowError("too many nodes or edges")
	}
	if len(graph.Subgraphs) != 0 {
		return inventory, hostedWorkflowError("nonempty subgraphs are not supported")
	}
	if len(graph.Nodes) == 0 {
		if len(graph.Edges) != 0 {
			return inventory, hostedWorkflowError("edges exist without nodes")
		}
		return inventory, nil
	}
	start := ""
	for id, node := range graph.Nodes {
		if !validHostedID(id) {
			return inventory, hostedWorkflowError("invalid node identifier")
		}
		if node.Position != nil && (node.Position.X == nil || node.Position.Y == nil) {
			return inventory, hostedWorkflowError("invalid node position")
		}
		edgeOrder := []string(nil)
		if node.EdgeOrder != nil {
			edgeOrder = *node.EdgeOrder
		}
		switch node.Type {
		case "start":
			if start != "" || node.Outcome != "" || len(node.Tools) != 0 {
				return inventory, hostedWorkflowError("invalid start node")
			}
			start = id
		case "end":
			if (node.Outcome != "" && node.Outcome != "success" && node.Outcome != "failure") || len(edgeOrder) != 0 || len(node.Tools) != 0 {
				return inventory, hostedWorkflowError("invalid end node")
			}
		case "override_agent":
			if len(node.Outcome) != 0 || len(node.Tools) != 0 ||
				node.EntryBehavior != "" && node.EntryBehavior != "auto" && node.EntryBehavior != "generate_immediately" && node.EntryBehavior != "wait_for_user" {
				return inventory, hostedWorkflowError("invalid override_agent node")
			}
			for _, toolID := range node.AdditionalToolIDs {
				if !validHostedID(toolID) {
					return inventory, hostedWorkflowError("invalid override_agent tool reference")
				}
			}
			override, err := inspectHostedWorkflowOverride(node.Conversation)
			if err != nil {
				return inventory, hostedWorkflowError("invalid override_agent configuration")
			}
			inventory.ToolIDs = append(inventory.ToolIDs, node.AdditionalToolIDs...)
			inventory.ToolIDs = append(inventory.ToolIDs, override.ToolIDs...)
			inventory.Tools = append(inventory.Tools, override.Tools...)
			inventory.MCPServerIDs = append(inventory.MCPServerIDs, override.MCPServerIDs...)
		case "tool":
			if len(node.Tools) == 0 || node.Outcome != "" {
				return inventory, hostedWorkflowError("invalid tool node")
			}
			for _, tool := range node.Tools {
				if !validHostedID(tool.ToolID) {
					return inventory, hostedWorkflowError("invalid tool node reference")
				}
				if err := validateHostedSchemaOverrides(tool.SchemaOverrides); err != nil {
					return inventory, hostedWorkflowError("invalid tool schema override")
				}
				inventory.ToolIDs = append(inventory.ToolIDs, tool.ToolID)
			}
		default:
			return inventory, hostedWorkflowError("unsupported node type")
		}
		seen := make(map[string]bool)
		for _, edgeID := range edgeOrder {
			edge, ok := graph.Edges[edgeID]
			if !ok || seen[edgeID] || edge.Source != id {
				return inventory, hostedWorkflowError("edge order does not match edges")
			}
			seen[edgeID] = true
		}
		for edgeID, edge := range graph.Edges {
			if edge.Source == id && !seen[edgeID] {
				return inventory, hostedWorkflowError("edge is missing from edge order")
			}
		}
	}
	if start == "" || len(inventory.ToolIDs) > 64 || len(inventory.Tools) > 64 || len(inventory.MCPServerIDs) > 64 {
		return inventory, hostedWorkflowError("missing start node or too many tool references")
	}
	for id, edge := range graph.Edges {
		if !validHostedID(id) {
			return inventory, hostedWorkflowError("invalid edge identifier")
		}
		if _, ok := graph.Nodes[edge.Source]; !ok {
			return inventory, hostedWorkflowError("edge source is missing")
		}
		if _, ok := graph.Nodes[edge.Target]; !ok || edge.Target == start {
			return inventory, hostedWorkflowError("edge target is invalid")
		}
		for _, c := range []*hostedWorkflowCondition{edge.Forward, edge.Backward} {
			if c == nil {
				continue
			}
			switch c.Type {
			case "unconditional":
				if c.Condition != "" || c.Successful != nil || len(c.Expression) != 0 {
					return inventory, hostedWorkflowError("invalid unconditional condition")
				}
			case "llm":
				if c.Condition == "" || len(c.Condition) > 8192 || c.Successful != nil || len(c.Expression) != 0 {
					return inventory, hostedWorkflowError("invalid LLM condition")
				}
			case "result":
				if c.Successful == nil || c.Condition != "" || len(c.Expression) != 0 || graph.Nodes[edge.Source].Type != "tool" {
					return inventory, hostedWorkflowError("invalid result condition")
				}
			case "expression":
				if len(c.Expression) == 0 || bytes.Equal(bytes.TrimSpace(c.Expression), []byte("null")) ||
					c.Condition != "" || c.Successful != nil {
					return inventory, hostedWorkflowError("invalid expression condition")
				}
			default:
				return inventory, hostedWorkflowError("unsupported edge condition")
			}
		}
		for _, p := range edge.Reroutes {
			if p.X == nil || p.Y == nil {
				return inventory, hostedWorkflowError("invalid edge reroute position")
			}
		}
	}
	reached := make(map[string]bool)
	queue := []string{start}
	for len(queue) != 0 {
		id := queue[0]
		queue = queue[1:]
		if reached[id] {
			continue
		}
		reached[id] = true
		if graph.Nodes[id].EdgeOrder == nil {
			continue
		}
		for _, edgeID := range *graph.Nodes[id].EdgeOrder {
			queue = append(queue, graph.Edges[edgeID].Target)
		}
	}
	if len(reached) != len(graph.Nodes) {
		return inventory, hostedWorkflowError("one or more nodes are unreachable")
	}
	return inventory, nil
}

func validateHostedSchemaOverrides(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("{}")) {
		return nil
	}
	if len(trimmed) > 32*1024 {
		return errors.New("schema overrides are too large")
	}
	var overrides map[string]struct {
		Source string `json:"source"`
	}
	if json.Unmarshal(trimmed, &overrides) != nil || overrides == nil {
		return errors.New("invalid schema overrides")
	}
	for _, override := range overrides {
		switch override.Source {
		case "constant", "dynamic_variable", "llm", "omit":
		default:
			return errors.New("unsupported schema override")
		}
	}
	return nil
}

func inspectHostedWorkflowOverride(raw json.RawMessage) (hostedWorkflowInventory, error) {
	var inventory hostedWorkflowInventory
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return inventory, nil
	}
	if len(trimmed) > 128*1024 {
		return inventory, errors.New("workflow override is too large")
	}
	var override struct {
		Agent struct {
			Prompt struct {
				Tools      []hostedTool `json:"tools"`
				ToolIDs    []string     `json:"tool_ids"`
				MCP        []string     `json:"mcp_server_ids"`
				NativeMCP  []string     `json:"native_mcp_server_ids"`
				Additional []string     `json:"additional_tool_ids"`
			} `json:"prompt"`
		} `json:"agent"`
	}
	if err := json.Unmarshal(raw, &override); err != nil {
		return inventory, err
	}
	inventory.ToolIDs = append(inventory.ToolIDs, override.Agent.Prompt.ToolIDs...)
	inventory.ToolIDs = append(inventory.ToolIDs, override.Agent.Prompt.Additional...)
	inventory.Tools = append(inventory.Tools, override.Agent.Prompt.Tools...)
	inventory.MCPServerIDs = append(inventory.MCPServerIDs, override.Agent.Prompt.MCP...)
	inventory.MCPServerIDs = append(inventory.MCPServerIDs, override.Agent.Prompt.NativeMCP...)
	for _, toolID := range inventory.ToolIDs {
		if !validHostedID(toolID) {
			return inventory, errors.New("invalid workflow tool reference")
		}
	}
	for _, serverID := range inventory.MCPServerIDs {
		if !validHostedID(serverID) {
			return inventory, errors.New("invalid workflow MCP reference")
		}
	}
	for _, tool := range inventory.Tools {
		if err := validateHostedTool(tool); err != nil {
			return inventory, err
		}
	}
	return inventory, nil
}
