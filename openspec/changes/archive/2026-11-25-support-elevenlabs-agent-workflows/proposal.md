# Proposal

## Why

Hosted ElevenLabs activation currently rejects any non-empty agent workflow, so administrators cannot use their dashboard-authored conversation flows. Supporting workflows also needs a deliberate way to expose existing application MCP servers without silently sharing their credentials or bypassing the app's per-tool allowlists.

## What Changes

- Accept supported ElevenLabs workflow graphs during hosted-agent validation and session admission, including supported workflow nodes that execute provider-managed tools; keep unknown or unverifiable workflow/tool configurations fail-closed.
- Support the two external-tool sources separately: provider-managed tools remain configured in ElevenLabs, while each application MCP server can be explicitly left unshared, registered directly with ElevenLabs, or exposed through an authenticated application proxy.
- Preserve the application MCP allowlist in hosted mode. Direct registration is allowed only when the endpoint and provider contract can enforce the configured tool set; the proxy filters calls to enabled, allowed tools and keeps the MCP endpoint credential in the application.
- Synchronize selected MCP server configuration and workflow-compatible references on hosted activation and relevant admin changes, preserving dashboard-managed tools and workflow settings. Revalidate the saved configuration and version before issuing hosted access.
- Persist MCP creation intent before provider creation and include a non-secret ownership marker in provider configuration. Recover ambiguous creation only through an exactly verified marker match; block absent, duplicate, or mismatched recovery results instead of risking a duplicate or orphaned resource.
- Disclose that hosted workflows may call external services, where credentials are stored for the selected integration, and that workflow tools are provider-executed rather than the existing local conversation tool loop.
- Keep existing `cascaded` and `realtime` tool behavior unchanged. Do not expose browser client tools, accept client-selected workflows/tools, or add camera, local citations, or a browser-to-backend conversational tool bridge.

## Capabilities

### New Capabilities
- `elevenlabs-hosted-workflows`: Validate and run dashboard-authored hosted workflows with provider-managed tools and explicitly selected application MCP integrations.

### Modified Capabilities
- `admin-governance-console`: Let administrators choose and inspect the hosted integration mode and synchronization state for each MCP server, with clear credential and external-processing disclosure.
- `agent-skill-orchestration`: Define how hosted declarative Agent Skills can guide use of the workflow's explicitly available tools without enabling the local runtime skill/MCP tool loop.
- `web-app-security-hardening`: Require authenticated, allowlist-enforcing proxy access and preserve secret redaction, SSRF protections, and fail-closed tool authorization for hosted workflow calls.

## Impact

- Hosted-agent inspection, workflow graph validation, token admission, admin configuration synchronization, and mocked provider/API tests in the Go server.
- MCP server configuration and status fields, existing `/admin` MCP controls, and a public HTTPS application proxy endpoint for servers configured to keep credentials local.
- ElevenLabs API permissions and workspace resources: agents, provider-side workflows/tools, and managed MCP-server create/read/update/delete. Direct MCP mode shares that server's required credential with ElevenLabs; proxy mode shares only a dedicated proxy credential.
- Provider-side workflow execution, external service processing, account permissions, and usage costs. No live provider account, credentials, or deployment is changed by this proposal.
