# Spec Delta

## ADDED Requirements

### Requirement: Hosted MCP proxy authorization
The hosted MCP proxy SHALL authenticate each provider request with a dedicated per-server credential and SHALL authorize the server and tool against current application policy before forwarding any request.

#### Scenario: Proxy request is unauthenticated
- **WHEN** a provider request reaches the proxy without a valid credential for the requested MCP server
- **THEN** the proxy rejects it before contacting the configured MCP endpoint

#### Scenario: Proxy request targets an unavailable tool
- **WHEN** an authenticated request targets a disabled server or a tool not currently enabled and allowlisted for that server
- **THEN** the proxy denies the request before forwarding it

#### Scenario: Proxy forwards an authorized tool call
- **WHEN** a request has a valid per-server credential and targets a currently enabled, allowlisted tool on an eligible endpoint
- **THEN** the proxy forwards only that tool call using the application's stored endpoint authentication and existing outbound URL safety checks

#### Scenario: Proxy integration is disabled or revoked
- **WHEN** an administrator disables, removes, or rotates a server's hosted proxy integration
- **THEN** the previous proxy credential is rejected for subsequent requests

#### Scenario: Proxy handles secrets and failures
- **WHEN** a proxy request succeeds or fails
- **THEN** diagnostics omit proxy credentials, upstream authentication material, and raw tool arguments, while reporting a sanitized outcome
