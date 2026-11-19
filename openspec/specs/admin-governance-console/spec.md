# admin-governance-console Specification

## Purpose

Provides administrators with secure management workflows for user accounts and roles, RAG document ingestion, write-only AI agent credentials and endpoints, Agent Skills, MCP server connections, and security audit logs.

## Requirements

### Requirement: User account and role lifecycle management
The system SHALL allow administrators on `/admin` to create users, assign or update roles (`user` or `admin`), activate or deactivate accounts, and prevent removal or demotion of the last active administrator.

#### Scenario: Administrator creates a new user account
- **WHEN** an administrator submits a valid email, password policy-compliant credential, and role on `/admin`
- **THEN** the system creates the account with hashed credentials, logs an audit entry, and lists the user in the directory

#### Scenario: Administrator attempts to deactivate the sole remaining admin
- **WHEN** an administrator attempts to deactivate or demote the only remaining active `admin` account
- **THEN** the system rejects the operation with HTTP `409 Conflict` to prevent administrative lockout

### Requirement: RAG document management workspace
The system SHALL provide an administrative document panel displaying ingestion status, chunk counts, upload controls, reindex triggers, and deletion actions.

#### Scenario: Administrator monitors document ingestion progress
- **WHEN** an administrator uploads one or more documents in `/admin`
- **THEN** the document table reflects status transitions (`processing`, `ready`, or `failed`) and total indexed chunk count

### Requirement: Write-only AI provider key, endpoint, and model configuration
The system SHALL allow administrators to configure active AI pipeline mode (`realtime` or `cascaded`), provider base URLs, model names (including selecting from discovered speech-to-text models when available, with manual entry fallback), system instructions, and API keys while treating secrets as write-only after submission.

#### Scenario: Administrator updates AI API key and endpoint
- **WHEN** an administrator saves a new AI provider endpoint and API key
- **THEN** the system encrypts the key at rest, applies the configuration to subsequent sessions, and returns only a masked key preview (for example, `****abcd`) on all read responses

#### Scenario: Administrator selects an STT model from discovered provider models
- **WHEN** the configured speech-to-text provider exposes available models and an administrator opens `/admin` in `cascaded` mode
- **THEN** the AI Agent form offers an STT Model dropdown populated with discovered models, falling back to a text input when discovery is unavailable

#### Scenario: Administrator tests AI endpoint connectivity
- **WHEN** an administrator triggers a connection health check for the configured AI provider
- **THEN** the backend verifies connectivity server-side and returns a sanitized pass/fail status without exposing stored credentials to the browser

### Requirement: MCP server registration and policy configuration
The system SHALL allow administrators to register, enable, disable, and test Model Context Protocol (MCP) server connections with explicit transport settings, visible tool descriptions, tool allowlists, and independent visibility controls for each server's discovered tool list.

#### Scenario: Administrator registers and tests an MCP server
- **WHEN** an administrator configures an MCP server URL/transport and runs tool discovery
- **THEN** the system validates the target against outbound security policies, lists discovered tools with their descriptions, and allows toggling both server enablement and which tools are exposed to the agent

#### Scenario: Administrator disables an MCP server without deleting it
- **WHEN** an administrator toggles an existing MCP server to disabled on `/admin`
- **THEN** the server configuration and allowlist remain saved, and none of its tools are exposed to or callable by the conversational agent until re-enabled

#### Scenario: Administrator hides one server's discovered tools
- **WHEN** an administrator hides the discovered-tools list for an MCP server
- **THEN** only that server's discovered-tools list is collapsed, while the server card, server state, discovered tools, and saved allowlist remain unchanged

#### Scenario: Tool checkbox update preserves discovered-tools visibility
- **WHEN** an administrator enables or disables a discovered tool with its checkbox
- **THEN** the system saves the updated allowlist and preserves that server's current discovered-tools visibility

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

### Requirement: Immutable administrative security audit log
The system SHALL record structured audit events for logins, failed authentications, user/role mutations, document ingestion/deletions, Agent Skill uploads/updates/deletions, and AI/MCP configuration updates, viewable by administrators.

#### Scenario: Configuration change recorded in audit trail
- **WHEN** an administrator modifies AI credentials, MCP settings, Agent Skills, or user roles
- **THEN** an append-only audit record capturing actor ID, timestamp, action type, and redacted metadata becomes visible in the audit log view
