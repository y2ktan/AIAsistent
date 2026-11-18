# Spec Delta

## ADDED Requirements

### Requirement: Agent skill governance workspace
The system SHALL provide a dedicated Agent Skills tab on `/admin` allowing administrators to upload `.md` or `.zip` skill packages, replace an existing skill by name, inspect parsed metadata and instructions, view referenced MCP tool readiness, toggle skill enablement, and delete skills.

#### Scenario: Administrator uploads and inspects an Agent Skill on /admin
- **WHEN** an administrator uploads a valid `SKILL.md` or `.zip` skill bundle in the Agent Skills tab on `/admin`
- **THEN** the skill list displays the parsed skill name, description, source format, reference file count, referenced MCP tool availability, enablement toggle, instructions preview, and delete action

#### Scenario: Administrator re-uploads an existing skill to update it
- **WHEN** an administrator uploads a valid skill package whose normalized `name` matches an existing skill
- **THEN** the system updates the existing skill's instructions, description, and bundled references in place while preserving its identifier and recording an audit event

#### Scenario: Administrator disables or deletes an Agent Skill
- **WHEN** an administrator toggles an Agent Skill to disabled or deletes it on `/admin`
- **THEN** subsequent agent turns omit the skill from active discovery and activation, and the action is recorded in the audit log

## MODIFIED Requirements

### Requirement: MCP server registration and policy configuration
The system SHALL allow administrators to register, enable, disable, and test Model Context Protocol (MCP) server connections with explicit transport settings, visible tool descriptions, and tool allowlists.

#### Scenario: Administrator registers and tests an MCP server
- **WHEN** an administrator configures an MCP server URL/transport and runs tool discovery
- **THEN** the system validates the target against outbound security policies, lists discovered tools with their descriptions, and allows toggling both server enablement and which tools are exposed to the agent

#### Scenario: Administrator disables an MCP server without deleting it
- **WHEN** an administrator toggles an existing MCP server to disabled on `/admin`
- **THEN** the server configuration and allowlist remain saved, and none of its tools are exposed to or callable by the conversational agent until re-enabled

### Requirement: Immutable administrative security audit log
The system SHALL record structured audit events for logins, failed authentications, user/role mutations, document ingestion/deletions, Agent Skill uploads/updates/deletions, and AI/MCP configuration updates, viewable by administrators.

#### Scenario: Configuration change recorded in audit trail
- **WHEN** an administrator modifies AI credentials, MCP settings, Agent Skills, or user roles
- **THEN** an append-only audit record capturing actor ID, timestamp, action type, and redacted metadata becomes visible in the audit log view
