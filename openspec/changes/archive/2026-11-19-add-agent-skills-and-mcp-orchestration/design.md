# Design: Agent Skills Governance and Full MCP Orchestration

## Context

See `proposal.md` ("Why") for problem motivation and `specs/` for the behavioral contract. Today in Aegis Voice:
- `server/internal/api/handlers.go` (`attachSessionAgent`) loads `MCPServerRecord` rows from PostgreSQL (`mcp_servers`), where `discovered_tools` JSONB already persists each `mcp.ToolDef{Name, Description, InputSchema}`, yet `mcp.ServerConfig` only carries `AllowedTools []string`. Consequently, `CascadedAdapter.chatWithTools` and `RealtimeAdapter.sendSessionUpdate` synthesize empty `{ "type": "object", "properties": {} }` schemas and generic `"MCP tool from <Server>"` descriptions, and `CascadedAdapter` caps tool loops at 3 rounds (`round < 3`).
- `server/internal/mcp/client.go` issues raw `tools/list` and `tools/call` JSON-RPC POSTs without negotiating MCP `initialize` / `Mcp-Session-Id` headers required by stateful Streamable HTTP MCP servers.
- `server/internal/rag/validator.go` already establishes magic-byte, UTF-8, and zip-entry validation patterns for admin uploads behind `RequireAuth` + `RequireCSRF` + `RequireRole(RoleAdmin)` + `RateLimitMiddleware`.
- `server/internal/agent/guardrails.go` prepends `securityBoundaryPreamble` above `SystemInstructions` and wraps external RAG/MCP data in `<untrusted_*>` tags.

## Goals / Non-Goals

**Goals:**
- Persist validated, declarative Agent Skills (`SKILL.md` and `.zip` bundles) in PostgreSQL with zero filesystem execution surface.
- Keep per-turn voice latency low via progressive skill disclosure (compact `<available_skills>` summary in system prompt + on-demand `load_skill` / `read_skill_file` internal tools) identically across `cascaded` and `realtime` pipelines.
- Supply complete discovered MCP JSON schemas and descriptions to the LLM, handle Streamable HTTP `initialize` sessions transparently, prevent cross-server tool name collisions, and allow multi-step skill + MCP reasoning loops.
- Provide a cohesive `/admin` experience across **Agent Skills** and **MCP Servers**, including MCP tool-readiness cross-checks on skill cards and server-level enable/disable toggles.

**Non-Goals:**
- Executing arbitrary scripts (`.sh`, `.py`, `.js`, binaries) bundled inside uploaded skill archives on the Aegis Voice server. All active side effects and external queries must run through administrator-governed MCP servers.
- Per-user custom skill uploads (skills are organization-wide governance artifacts managed exclusively by `admin` users, matching RAG documents and MCP servers).
- Modifying the end-user voice UI (`web/src/app/page.tsx`), which already renders `tool.call` activity indicators emitted by the backend.

## Decisions

### 1. Database-backed declarative skill packages (`000003_agent_skills` + `server/internal/skills`)
Store skills in a dedicated PostgreSQL table `agent_skills` rather than extracting files onto container disk:
- **Columns**: `id UUID PRIMARY KEY`, `name TEXT NOT NULL UNIQUE` (normalized lowercase slug `[a-z0-9][a-z0-9_-]{0,63}`), `description TEXT NOT NULL`, `instructions TEXT NOT NULL` (Markdown body without frontmatter), `reference_files JSONB NOT NULL DEFAULT '{}'::jsonb` (`map[string]string` of clean relative path to UTF-8 text), `allowed_tools TEXT[] NOT NULL DEFAULT '{}'`, `source_format TEXT NOT NULL CHECK (source_format IN ('md', 'zip'))`, `filename TEXT NOT NULL`, `size_bytes BIGINT NOT NULL`, `sha256_hash TEXT NOT NULL`, `enabled BOOLEAN NOT NULL DEFAULT TRUE`, `uploaded_by UUID REFERENCES users(id) ON DELETE SET NULL`, `created_at TIMESTAMPTZ`, `updated_at TIMESTAMPTZ`.
- **Validation & Parsing (`server/internal/skills`)**:
  - Enforce max compressed upload size of `2 MB` and max uncompressed archive total of `4 MB` across at most `32` files (`io.LimitReader` per entry and cumulative counter to defeat zip bombs).
  - For `.md` / `.markdown`: verify UTF-8 without null bytes (`0x00`), parse leading `---` YAML frontmatter (`name`, `description`, optional `allowed-tools` / `allowed_tools` parsed from space/comma string or YAML list) using a minimal dependency-free frontmatter parser, and require a non-empty Markdown body.
  - For `.zip`: verify `PK\x03\x04` magic header, ignore benign macOS archive noise (`__MACOSX/`, `.DS_Store`), reject any entry with `..`, leading `/`, `\`, symlink mode bits, or disallowed extensions, locate `SKILL.md` at archive root or single top-level folder (`<skill-dir>/SKILL.md`), and store companion `.md`, `.markdown`, `.txt`, `.json`, `.yaml`, and `.yml` UTF-8 files keyed by normalized relative path (`references/api.md`, etc.). Reject archives containing executable/script/binary files.
  - **Upsert-by-name**: Uploading a package whose normalized `name` matches an existing row updates its content, metadata, and `updated_at` in place while preserving its `id` and existing `enabled` flag (new skills default to `enabled = true`).
- **Alternatives considered**:
  - *Filesystem directory storage*: Rejected because containers are stateless/read-only-friendly, multi-replica deployments would diverge, and writing user-supplied archives to disk increases path-traversal/execution surface.
  - *Ingesting skills into RAG `document_chunks`*: Rejected because RAG chunks fragment procedural multi-step instructions and are explicitly marked `<untrusted_knowledge_context>` ("NOT instructions") in `guardrails.go`.

### 2. Progressive skill disclosure via `<available_skills>` + built-in `load_skill` / `read_skill_file` tools
Expose skills to the LLM in two stages across both `CascadedAdapter` and `RealtimeAdapter`:

```text
  System Prompt Hierarchy:
  +-------------------------------------------------------------------+
  | 1. securityBoundaryPreamble (immutable, highest priority)         |
  | 2. Admin SystemInstructions + Voice Language Policy               |
  | 3. <available_skills> catalog (enabled skills: name, description, |
  |    hasReferences) + brief guidance to call load_skill first       |
  +-------------------------------------------------------------------+
```

- **Built-in internal tools**:
  - `load_skill` (`{ "name": string }`): Registered whenever at least one enabled skill exists. Looks up the enabled skill in `Dependencies.Skills`; returns `<agent_skill name="...">` with the full Markdown `instructions` plus an index of bundled `referenceFiles` paths (if any), or a safe not-available message if unknown/disabled.
  - `read_skill_file` (`{ "skill": string, "path": string }`): Registered whenever at least one enabled skill has non-empty `ReferenceFiles`. Returns `<agent_skill_reference skill="..." path="...">` for that relative file.
- **Trust & Guardrails**: Skill instructions are administrator-authored governance artifacts, so they are NOT wrapped in `<untrusted_tool_output>`, allowing the LLM to follow their procedural steps. However, `securityBoundaryPreamble` explicitly states that neither user prompts, tool outputs, nor `<agent_skill>` playbooks may disclose secrets/credentials or bypass MCP tool allowlists, and `SanitizeAssistantOutput` still redacts secrets on every assistant turn.
- **Alternatives considered**:
  - *Injecting all enabled `SKILL.md` bodies into the system prompt on every turn*: Simpler (saves one LLM tool round), but scales poorly past 2–3 skills, increases voice TTFT on every casual greeting, and dilutes model attention across unrelated playbooks.

### 3. MCP full schema forwarding, tool disambiguation, and Streamable HTTP session support
Fix the end-to-end MCP execution path so skill playbooks can reliably drive MCP calls:
- **Preserve schemas**: Add `DiscoveredTools []ToolDef` to `mcp.ServerConfig` and copy `m.DiscoveredTools` in `attachSessionAgent`. Build a lookup helper in `server/internal/agent` that matches each `AllowedTools` entry against `DiscoveredTools` to populate the real `description` and normalized JSON `inputSchema` (defaulting to `{"type":"object","properties":{}}` only when a tool was manually allowlisted before discovery or omitted `inputSchema`, and ensuring `"properties"` is present when `"type": "object"` for OpenAI strict compatibility).
- **Collision-safe tool registry**:
  - Reserve built-in tool names (`search_knowledge_base`, `load_skill`, `read_skill_file`).
  - When an allowed MCP tool name is unique across all enabled MCP servers and does not collide with reserved names, expose its exact name (`tName`) so `SKILL.md` instructions referencing standard MCP tool names work out of the box.
  - When two enabled MCP servers allow the same raw `tName` (or `tName` collides with a reserved name), expose `<sanitized_server_name>__<tName>` to the LLM while mapping it internally back to `(ServerConfig, rawToolName)` when invoking `MCPHost.CallTool`.
- **Streamable HTTP `initialize` handshake (`server/internal/mcp/client.go`)**:
  - Before `tools/list` or `tools/call`, attempt an MCP `initialize` request (`protocolVersion: "2024-11-05"`), capture any `Mcp-Session-Id` header from the response, send a best-effort `notifications/initialized` notification with that header, and attach `Mcp-Session-Id` to the subsequent `tools/list` or `tools/call` request.
  - If `initialize` returns a JSON-RPC method-not-found error (`-32601`) or HTTP `400`/`404`/`405` on lightweight/stateless JSON-RPC servers (including existing unit-test mocks), fall back cleanly to sending `tools/list` or `tools/call` directly without a session header.
- **Multi-step round budget**: Increase `CascadedAdapter.chatWithTools` max rounds from `3` to `6` (`const maxToolRounds = 6`) so `load_skill` + optional `read_skill_file` + 2–3 MCP tool calls + final synthesis complete within a single turn without runaway loops. (`RealtimeAdapter` already loops asynchronously via `conversation.item.create` + `response.create`; add a per-utterance tool call guard of `6` there as well to prevent infinite realtime tool loops.)

### 4. Admin Governance Console UX (`/admin` dedicated Agent Skills tab + MCP parity)
- **API Endpoints (`server/internal/api`)**:
  - `GET /api/v1/admin/skills` -> `{ "skills": [...] }` (includes metadata, `instructions`, `referenceFilePaths` list, `allowedTools`, `enabled`, timestamps).
  - `POST /api/v1/admin/skills` -> `multipart/form-data` (`file`) behind `uploadLimiter` + `admin` middleware; validates via `skills.ParseUpload`, upserts in `AdminStore`, emits `admin.skill.upload` audit log.
  - `PATCH /api/v1/admin/skills/{id}` -> strict JSON `{ "enabled": bool }` to toggle skill activation; emits `admin.skill.update` audit log.
  - `DELETE /api/v1/admin/skills/{id}` -> deletes skill; emits `admin.skill.delete` audit log.
- **Frontend (`web/src/app/admin/page.tsx` + `web/src/lib/agent-skills.ts`)**:
  - Add `"skills"` to `Tab` (`Agent Skills` with `Sparkles` or `BookOpen` icon from `lucide-react`) right next to `MCP Servers`.
  - Drag-and-drop + file input accepting `.md,.markdown,.zip`.
  - Pure helper in `web/src/lib/agent-skills.ts` computes MCP tool readiness for each skill's `allowedTools` hints against the loaded `mcpServers` state (`ready`: tool is in an enabled MCP server's `allowedTools`; `unconfigured`: tool is not currently enabled/allowed on any active MCP server) so admins immediately see if a skill needs an MCP tool turned on.
  - Add an **Enabled / Disabled** toggle button and tool `description` subtitle to **MCP Servers** cards in `web/src/app/admin/page.tsx` using the existing `PUT /api/v1/admin/mcp-servers/{id}` endpoint.

## Risks / Trade-offs

- **[Extra LLM round-trip when loading a skill in voice mode]** → Progressive disclosure adds one fast internal tool call (`load_skill`, zero network/MCP latency, sub-millisecond in-memory lookup) only on turns that match a skill, while keeping non-skill conversational turns lean and fast.
- **[Local models without tool-calling support]** → `CascadedAdapter.chatWithTools` already detects `400 Bad Request` tool-unsupported errors from local models (e.g., older Ollama models) and gracefully degrades to plain chat without failing the turn.
- **[Malicious or malformed `.zip` uploads]** → Mitigated by admin-only RBAC + CSRF + upload rate limiting, `PK\x03\x04` magic-byte check, strict path sanitization, strict text-only extension & UTF-8/null-byte checks, per-file and total uncompressed byte caps, and storing extracted text in PostgreSQL rather than on the filesystem.
- **[Realtime tool-call loops]** → Adding a bounded per-turn tool-call counter (`maxToolRounds = 6`) in both `CascadedAdapter` and `RealtimeAdapter` prevents runaway billing or hung voice sessions if a skill or model loops on tool calls.

## Migration Plan

1. Apply `db/migrations/000003_agent_skills.up.sql` to create `agent_skills` and its indexes (`idx_agent_skills_enabled`). Existing `agent_configs` and `mcp_servers` rows require no data migration—already stored `mcp_servers.discovered_tools` JSONB is immediately leveraged once the updated server binary starts.
2. Deploy updated Go backend (`server`) and Next.js frontend (`web`). Sessions with zero uploaded skills behave identically to before, except enabled MCP tools now receive their full discovered descriptions and input schemas.
3. Rollback: Revert application containers and run `db/migrations/000003_agent_skills.down.sql` (`DROP TABLE IF EXISTS agent_skills;`).
