package api

import (
	"context"
	"encoding/json"
	"errors"
)

func (s *Server) validateHostedToolAdmission(ctx context.Context, config *hostedAgent) error {
	records, err := s.adminStore.ListMCPServers(ctx)
	if err != nil {
		return errors.New("Could not verify application MCP synchronization.")
	}
	for i := range records {
		rec := &records[i]
		normalizeHostedMCP(rec)
		if rec.HostedSyncStatus == "revocation_pending" || rec.HostedSyncStatus == "recovery_pending" {
			return errors.New("Hosted MCP recovery or revocation is incomplete. Reconcile the integration before requesting hosted access.")
		}
		inventory, referenced := config.ValidatedMCP[rec.HostedResourceID]
		if !referenced {
			continue
		}
		if !rec.Enabled || rec.HostedMode == "disabled" || rec.HostedSyncStatus != "synced" || rec.HostedAgentID != config.AgentID {
			return errors.New("A referenced application MCP integration is not synchronized or enabled.")
		}
		if !sameHostedToolNames(inventory, allowedHostedMCPTools(rec)) {
			return errors.New("A referenced application MCP inventory no longer matches its allowlist.")
		}
		snapshot, err := s.vault.Decrypt(rec.HostedCreationConfig)
		var expected hostedMCPProviderConfig
		if err != nil || json.Unmarshal([]byte(snapshot), &expected) != nil ||
			expected.RequestMeta[hostedMCPOwnershipKey] != rec.HostedCreationIntentID ||
			!hostedMCPConfigMatches(config.ValidatedMCPConfigs[rec.HostedResourceID], expected) {
			return errors.New("A referenced application MCP configuration changed or could not be verified. Synchronize it before requesting hosted access.")
		}
	}
	return nil
}

func (s *Server) validateHostedSkills(ctx context.Context, config *hostedAgent) error {
	records, err := s.adminStore.ListAgentSkills(ctx)
	if err != nil {
		return errors.New("Could not verify hosted Agent Skill tool references.")
	}
	available := make(map[string]bool)
	for _, tool := range config.ValidatedTools {
		name := tool.Name
		if tool.Type == "system" || tool.Type == "" {
			name = tool.Params.Type
		}
		if name != "" {
			available[name] = true
		}
	}
	for _, inventory := range config.ValidatedMCP {
		for _, tool := range inventory {
			available[tool.Name] = true
		}
	}
	for _, skill := range records {
		if !skill.Enabled {
			continue
		}
		for _, name := range skill.AllowedTools {
			if !available[name] {
				return errors.New("An enabled Agent Skill references a tool unavailable to the validated hosted agent. Update the skill or synchronize its integration.")
			}
		}
	}
	return nil
}

func (s *Server) validateHostedSkillAvailability(ctx context.Context, config *AgentConfigRecord) error {
	key, err := s.hostedKey(config)
	if err != nil {
		return err
	}
	verified, err := loadHostedAgent(ctx, newHostedClient(s.allowPrivate), key, config.ElevenLabsAgentID)
	if err != nil {
		return err
	}
	if err := s.validateHostedToolAdmission(ctx, verified); err != nil {
		return err
	}
	if !config.ElevenLabsSkillsSyncEnabled {
		return nil
	}
	return s.validateHostedSkills(ctx, verified)
}
