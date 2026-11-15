# admin-governance-console Specification

## Purpose

Provides administrators with secure management workflows for user accounts and roles, RAG document ingestion, write-only AI agent credentials and endpoints, MCP server connections, and security audit logs.

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
The system SHALL allow administrators to register, enable, disable, and test Model Context Protocol (MCP) server connections with explicit transport settings and tool allowlists.

#### Scenario: Administrator registers and tests an MCP server
- **WHEN** an administrator configures an MCP server URL/transport and runs tool discovery
- **THEN** the system validates the target against outbound security policies, lists discovered tools, and allows toggling which tools are exposed to the agent

### Requirement: Immutable administrative security audit log
The system SHALL record structured audit events for logins, failed authentications, user/role mutations, document ingestion/deletions, and AI/MCP configuration updates, viewable by administrators.

#### Scenario: Configuration change recorded in audit trail
- **WHEN** an administrator modifies AI credentials, MCP settings, or user roles
- **THEN** an append-only audit record capturing actor ID, timestamp, action type, and redacted metadata becomes visible in the audit log view
