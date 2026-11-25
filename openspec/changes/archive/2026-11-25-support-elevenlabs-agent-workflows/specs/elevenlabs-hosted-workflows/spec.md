# Spec Delta

## Purpose

Defines how hosted ElevenLabs conversations admit dashboard-authored workflows and invoke their provider-managed tools or explicitly exposed application MCP servers without bypassing the application's authorization and credential boundaries.

## ADDED Requirements

### Requirement: Hosted workflow admission
The system SHALL admit a private, versioned hosted agent with a supported workflow graph and SHALL validate its nodes, edges, and referenced tools before hosted activation or token issuance.

#### Scenario: Supported workflow is admitted
- **WHEN** an administrator activates or a user requests access to a private agent whose workflow graph and server-executable tool references pass validation
- **THEN** the system admits the configured agent version and continues to enforce authentication and hosted-session controls

#### Scenario: Current dashboard workflow envelope is admitted
- **WHEN** ElevenLabs returns bounded provider metadata, an `override_agent` node, or an expression condition in the documented dashboard workflow envelope and all nested tools or MCP servers pass validation
- **THEN** the system admits the workflow without requiring the administrator to remove or recreate it, while continuing to reject nonempty unsupported subgraphs, standalone-agent transfers, browser-client tools, and unknown node or condition types

#### Scenario: Invalid or unsupported workflow is rejected
- **WHEN** a workflow contains malformed edges, unknown node types, unresolved tool references, or unsupported browser-client tools
- **THEN** activation or token issuance fails with a sanitized diagnostic before access is issued and without falling back to a backend conversation

### Requirement: Provider-managed workflow execution
Hosted conversations SHALL execute supported workflow nodes and provider-managed server-side external tools through ElevenLabs, without invoking the existing local conversational MCP/tool loop.

#### Scenario: Workflow dispatches a provider-managed tool
- **WHEN** an admitted hosted workflow reaches a supported server-side tool action
- **THEN** ElevenLabs executes that configured action and continues the workflow according to its result

#### Scenario: Workflow requests a browser-client tool
- **WHEN** an agent workflow references a client-side tool that requires an application callback
- **THEN** the system rejects the configuration unless that tool is explicitly supported by a separately authorized server-side integration, and the browser does not receive a tool callback or secret

### Requirement: Explicit application MCP exposure
Each application MCP server SHALL remain unshared by default and SHALL require an administrator-selected hosted integration mode of disabled, direct provider registration, or authenticated application proxy.

#### Scenario: Administrator selects direct provider registration
- **WHEN** an administrator selects direct registration for an eligible HTTPS MCP server and confirms provider-side credential storage
- **THEN** the server is registered with ElevenLabs and attached to the hosted agent only if the provider configuration can preserve the application's enabled-tool allowlist

#### Scenario: Direct registration cannot preserve the allowlist
- **WHEN** an administrator selects direct registration but the endpoint is not eligible or the provider cannot enforce the configured allowlist
- **THEN** synchronization fails visibly and requires the administrator to choose proxy mode or adjust the explicit application allowlist

#### Scenario: Administrator selects the application proxy
- **WHEN** an administrator selects proxy mode for an application MCP server
- **THEN** ElevenLabs calls an authenticated HTTPS application endpoint that forwards only enabled, allowlisted tools, while the original MCP endpoint credential remains stored by the application

#### Scenario: MCP server remains disabled for hosted use
- **WHEN** an application MCP server has no hosted integration mode or is disabled
- **THEN** it is not attached to the hosted agent and cannot be invoked by its workflow

### Requirement: Hosted tool synchronization ownership
Hosted synchronization SHALL preserve dashboard-managed workflow and tool resources, update or remove only application-managed MCP resources, and retain a separate visible failure state when provider synchronization fails.

#### Scenario: Application MCP changes while hosted mode is active
- **WHEN** an administrator changes or removes an application MCP server selected for hosted use
- **THEN** synchronization updates or removes only its tracked provider-side resource and references while leaving unrelated dashboard-managed tools and workflow structure intact

#### Scenario: Hosted tool synchronization fails
- **WHEN** provider registration, attachment, or verification fails
- **THEN** the failure is reported as unsynchronized, no unverified tool is admitted for a new hosted call, and a retry does not duplicate or orphan managed resources

#### Scenario: Provider creation succeeds but its response is lost
- **WHEN** an application MCP resource creation has an ambiguous outcome or its returned mapping cannot be persisted
- **THEN** the system retains the creation intent persisted before the attempt and recovers a mapping only after bounded provider discovery and resource inspection verify exactly one matching non-secret ownership marker and intended configuration

#### Scenario: Creation recovery cannot establish ownership
- **WHEN** recovery finds no matching resource, multiple matches, a configuration mismatch, or an unverifiable provider response
- **THEN** the integration remains unavailable with a sanitized recovery diagnostic, no additional creation is attempted, and display names alone never authorize resource adoption or deletion

#### Scenario: Concurrent retries or restart follow an ambiguous creation
- **WHEN** multiple application instances reconcile the same server or reconciliation resumes after a process restart
- **THEN** durable creation-attempt state prevents a second creation after an ambiguous attempt and retries recover and persist the verified mapping before attachment

#### Scenario: Administrator disables an unresolved creation
- **WHEN** an administrator disables or removes a server whose provider creation is unresolved
- **THEN** the system retains its disabled local ownership and creation-intent record until recovery and verified remote cleanup complete, and does not report hosted revocation as complete
