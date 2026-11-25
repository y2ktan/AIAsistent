# Spec Delta

## ADDED Requirements

### Requirement: Hosted skills respect hosted tool availability
When Agent Skills are synchronized to a hosted ElevenLabs agent, their declarative instructions SHALL be limited to tools validated and available to that hosted workflow.

#### Scenario: Skill refers to an available hosted tool
- **WHEN** an enabled skill guides the agent to use a provider-managed tool or an application MCP tool that is synchronized, enabled, and allowlisted
- **THEN** the hosted workflow may use that tool subject to the provider and application authorization controls

#### Scenario: Skill refers to an unavailable tool
- **WHEN** an enabled skill refers to a tool that is disabled, unshared, unsynchronized, or outside the application allowlist
- **THEN** the tool is not made available by the skill text and the hosted agent cannot invoke it

#### Scenario: Hosted mode has no local runtime tool loop
- **WHEN** a hosted conversation loads an enabled Agent Skill
- **THEN** the skill remains declarative hosted-agent guidance and does not trigger local skill loading or local MCP execution by the application
