# Spec Delta

## ADDED Requirements

### Requirement: Hosted MCP integration governance
The administration console SHALL let an administrator explicitly select and review how each application MCP server is exposed to a hosted ElevenLabs agent.

#### Scenario: Hosted integration defaults to disabled
- **WHEN** an administrator creates or opens an application MCP server that has no hosted integration choice
- **THEN** the console shows that it is not shared with hosted agents and does not register it with ElevenLabs

#### Scenario: Administrator chooses direct registration
- **WHEN** an administrator selects direct provider registration for an eligible MCP server
- **THEN** the console explains that the endpoint and configured authentication material are shared with ElevenLabs and requires confirmation before saving that choice

#### Scenario: Administrator chooses the application proxy
- **WHEN** an administrator selects the application proxy for an MCP server
- **THEN** the console explains that ElevenLabs will invoke the application over HTTPS, that the original MCP credential remains in the application, and which allowlisted tools can be called

#### Scenario: Administrator reviews synchronization state
- **WHEN** the hosted MCP integration is pending, synchronized, or failed
- **THEN** the console shows that state and a sanitized reason, and does not represent a failed integration as ready for hosted use

#### Scenario: Administrator disables or removes an integration
- **WHEN** an administrator disables or removes an MCP server's hosted integration
- **THEN** the console confirms the resulting hosted availability and the system prevents new hosted calls from using that integration

### Requirement: Hosted workflow external-action disclosure
Before enabling hosted workflows with external tools, the administration console SHALL disclose that provider-executed workflow actions can send conversation-derived data to external services and may cause external side effects.

#### Scenario: Administrator enables a workflow with external actions
- **WHEN** an administrator saves a hosted configuration containing a supported external workflow action or selected MCP integration
- **THEN** the console presents the external-processing and side-effect disclosure before the configuration becomes active
