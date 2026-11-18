# Proposal: Add Agent Skills Governance and Full MCP Orchestration

## Why

Aegis Voice already lets administrators register Model Context Protocol (MCP) servers and discover tools, but two gaps prevent the assistant from reliably analyzing complex user requests with external tools:
1. **Incomplete MCP orchestration at runtime**: Discovered MCP tool `description` and `inputSchema` metadata are stored in PostgreSQL (`mcp_servers.discovered_tools`) yet dropped when building session agents (`attachSessionAgent`), leaving the LLM with empty parameter schemas (`{}`), generic descriptions, no Streamable HTTP `initialize` session support, tool-name collisions across servers, and a tight 3-round tool loop ceiling.
2. **No governed Agent Skills layer**: Administrators have no way in `/admin` to upload reusable procedural playbooks (`SKILL.md` or `.zip` skill bundles) that teach the LLM *how* to analyze domain-specific requests, *which* MCP tools to invoke and in what sequence, and *how* to synthesize the spoken response—similar to the existing governance workflows for RAG documents and MCP servers.

## What Changes

- **Agent Skill package ingestion and storage**:
  - Add a new `agent_skills` database migration (`000003_agent_skills`) storing validated declarative skill packages (`slug`/`name`, `description`, `instructions_markdown`, bundled UTF-8 reference files JSONB, optional `allowed_tools` hints, `source_format` (`md` | `zip`), `sha256_hash`, `enabled`, and audit metadata).
  - Add strict server-side upload validation and parsing (`server/internal/skills`) accepting single Markdown (`SKILL.md` / `.md`) files and `.zip` skill bundles (`SKILL.md` + supporting `.md`/`.txt`/`.json`/`.yaml` reference files) while rejecting binary/executable payloads, zip-slip paths, compression bombs, and missing YAML frontmatter (`name`, `description`)—enforcing declarative instructions only with zero host code execution.
- **Admin Governance Console — Dedicated Agent Skills tab & MCP parity**:
  - Add authenticated, CSRF-protected, rate-limited admin endpoints (`GET /api/v1/admin/skills`, `POST /api/v1/admin/skills`, `PATCH /api/v1/admin/skills/{id}`, `DELETE /api/v1/admin/skills/{id}`) with structured audit logging (`admin.skill.upload`, `admin.skill.update`, `admin.skill.delete`).
  - Add a dedicated **Agent Skills** tab on `/admin` (`web/src/app/admin/page.tsx`) next to **MCP Servers** featuring drag-and-drop `.md`/`.zip` upload (replacing an existing skill slug cleanly or creating a new one), parsed metadata cards (`name`, `description`, bundled reference file count, referenced MCP tool status badges), an **Enable / Disable** toggle, an expandable instructions preview, and **Delete**.
  - Enhance the **MCP Servers** tab on `/admin` with an **Enable / Disable** server toggle and visible tool descriptions alongside allowlist checkboxes so administrators can govern MCP servers and skills consistently.
- **Runtime Agent Skills + MCP tool orchestration (`cascaded` and `realtime`)**:
  - Inject a compact `<available_skills>` catalog (`name` + `description` of enabled skills, subordinated beneath `SECURITY BOUNDARY RULES`) into the system prompt and expose built-in `load_skill` (and `read_skill_file` when reference files exist) internal tools so the LLM progressively loads matching skill playbooks on demand without bloating every voice turn.
  - Pass full discovered MCP tool `description` and JSON `inputSchema` from `MCPServerRecord.DiscoveredTools` into `Dependencies.MCPServers` and both `CascadedAdapter` and `RealtimeAdapter`, disambiguate colliding tool names across MCP servers, add MCP `initialize` / `Mcp-Session-Id` handshake support in `server/internal/mcp/client.go` with stateless fallback, and raise the cascaded tool-call loop ceiling (from 3 to 6 rounds) so skill loading + multi-step MCP execution + final answer synthesis succeed reliably.

## Capabilities

### New Capabilities
- `agent-skill-orchestration`: Ingestion, validation, progressive system-prompt discovery, on-demand `load_skill` / `read_skill_file` activation, and skill-guided MCP analysis/response synthesis for declarative `SKILL.md` and `.zip` skill bundles.

### Modified Capabilities
- `admin-governance-console`: Add the dedicated **Agent Skills** management tab (upload, enable/disable, inspect, delete), add server enable/disable and tool description visibility to **MCP Servers**, and record skill lifecycle events in the immutable security audit log.
- `voice-vision-ai-agent`: Forward full discovered MCP tool descriptions and JSON input schemas to the LLM, support MCP Streamable HTTP session initialization and multi-server tool disambiguation, expand multi-step tool loop orchestration across `cascaded` and `realtime` modes, and enforce prompt-injection guardrails over skill instructions and MCP outputs.

## Impact

- **Database**: `db/migrations/000003_agent_skills.up.sql` and `000003_agent_skills.down.sql`.
- **Go Backend**:
  - New `server/internal/skills/` package (validator, YAML frontmatter + Markdown parser, `.zip` safe extractor, types, and unit tests).
  - `server/internal/mcp/client.go` & `mcp_test.go`: Preserve `ToolDef` schemas on `ServerConfig` and support MCP `initialize` / `Mcp-Session-Id` negotiation with stateless JSON-RPC fallback.
  - `server/internal/agent/` (`types.go`, `cascaded.go`, `realtime.go`, `guardrails.go`, `skills.go`, `agent_test.go`): `<available_skills>` prompt builder, built-in `load_skill` / `read_skill_file` handlers, full MCP JSON schema exposure, multi-server tool routing/disambiguation, and expanded tool-call loop budget.
  - `server/internal/api/` (`store.go`, `pg_store.go`, `router.go`, `handlers.go`, `api_test.go`): Skill CRUD/upload endpoints, audit log entries, and passing enabled skills + full MCP tool definitions into `attachSessionAgent`.
- **Next.js Frontend**:
  - `web/src/app/admin/page.tsx` (and extracted helper/test modules under `web/src/lib/` & `web/src/__tests__/`): New **Agent Skills** tab (`md`/`zip` upload, enable/disable, preview, MCP tool readiness badges, delete) and **MCP Servers** server-level enable/disable toggle + tool descriptions.
- **Dependencies**: No new external third-party services required; uses Go stdlib (`archive/zip`, `unicode/utf8`) and lightweight frontmatter parsing.
