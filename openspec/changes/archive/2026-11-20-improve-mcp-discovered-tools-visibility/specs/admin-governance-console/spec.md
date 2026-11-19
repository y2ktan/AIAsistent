# Spec Delta

## MODIFIED Requirements

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
