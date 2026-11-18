# Tasks

## 1. Database Schema & Skill Package Validator

- [x] 1.1 Create `db/migrations/000003_agent_skills.up.sql` and `db/migrations/000003_agent_skills.down.sql` defining the `agent_skills` table (`id`, unique `name`, `description`, `instructions`, `reference_files` JSONB, `allowed_tools` `TEXT[]`, `source_format`, `filename`, `size_bytes`, `sha256_hash`, `enabled`, `uploaded_by`, `created_at`, `updated_at`) and `idx_agent_skills_enabled`, and verify both SQL files parse cleanly and follow existing migration conventions.
- [x] 1.2 Implement `server/internal/skills/` (`types.go`, `validator.go`) to validate and parse standalone `.md` / `.markdown` (`SKILL.md`) uploads and `.zip` skill bundles (`PK\x03\x04` magic bytes, root or single-folder `SKILL.md`, YAML frontmatter `name`, `description`, optional `allowed-tools`/`allowed_tools`, UTF-8 companion `.md`/`.txt`/`.json`/`.yaml`/`.yml` files, 2 MB compressed / 4 MB uncompressed / 32-file limits, and rejection of null/binary/executable files or path traversal), and verify `go build ./internal/skills/...` succeeds.
- [x] 1.3 Add unit tests in `server/internal/skills/skills_test.go` covering valid standalone `SKILL.md` uploads, valid `.zip` bundles with reference files and `__MACOSX`/`.DS_Store` noise, frontmatter `allowed-tools` parsing, and rejection of missing frontmatter, binary/script entries, path-traversal (`../`) entries, and oversized archives, and verify `go test -race ./internal/skills/...` passes.

## 2. MCP Protocol Handshake & Schema Preservation

- [x] 2.1 Extend `mcp.ServerConfig` in `server/internal/mcp/client.go` with `DiscoveredTools []ToolDef` and add Streamable HTTP `initialize` + `Mcp-Session-Id` negotiation (with automatic fallback to stateless JSON-RPC when `initialize` returns `-32601` or HTTP `400`/`404`/`405`), and verify `go build ./internal/mcp/...` compiles cleanly.
- [x] 2.2 Expand `server/internal/mcp/mcp_test.go` to test stateful MCP servers requiring `initialize` and `Mcp-Session-Id` on `tools/list` and `tools/call` alongside existing stateless SSE/JSON-RPC servers, and verify `go test -race ./internal/mcp/...` passes.

## 3. Agent Runtime Skill Discovery, Activation & MCP Orchestration

- [x] 3.1 Add skill types and progressive disclosure helpers in `server/internal/agent/skills.go`, `types.go`, and `guardrails.go`: subordinate `<available_skills>` catalog rendering beneath `securityBoundaryPreamble`, register built-in `load_skill` and conditional `read_skill_file` function tools, and enforce safe responses for unknown or disabled skills, verifying `go build ./internal/agent/...` succeeds.
- [x] 3.2 Implement MCP tool schema resolution and collision-safe routing in `server/internal/agent/` so `CascadedAdapter` and `RealtimeAdapter` expose each allowed MCP tool's discovered `Description` and normalized JSON `InputSchema`, disambiguate duplicate/reserved tool names via `<server_slug>__<tool_name>`, route invocations back to the originating `mcp.ServerConfig` and raw tool name, and enforce a 6-round per-turn tool-call ceiling in both adapters, verifying `go build ./internal/agent/...` succeeds.
- [x] 3.3 Add unit tests in `server/internal/agent/agent_test.go` verifying: `<available_skills>` omits disabled skills; `load_skill` + `read_skill_file` + schema-validated MCP tool call + response synthesis succeed end-to-end in `CascadedAdapter` and `RealtimeAdapter`; duplicate MCP tool names across two servers disambiguate and route accurately; and secret redaction / unpermitted-tool blocking remain intact, verifying `go test -race ./internal/agent/...` passes.

## 4. Admin Skills API & Store Layer

- [x] 4.1 Extend `AdminStore` (`server/internal/api/store.go`) and `PGAdminStore` (`server/internal/api/pg_store.go`) with `ListAgentSkills`, `GetAgentSkill`, `UpsertAgentSkillByName` (preserving existing `id` and `enabled` on re-upload), `UpdateAgentSkillEnabled`, and `DeleteAgentSkill`, and wire enabled skills plus `MCPServerRecord.DiscoveredTools` into `attachSessionAgent` in `server/internal/api/handlers.go`, verifying `go build ./internal/api/...` succeeds.
- [x] 4.2 Register and implement `GET /api/v1/admin/skills`, `POST /api/v1/admin/skills` (rate-limited multipart upload), `PATCH /api/v1/admin/skills/{id}`, and `DELETE /api/v1/admin/skills/{id}` in `server/internal/api/router.go` and `handlers.go` behind `admin` Auth + CSRF + RBAC middleware with `admin.skill.upload`, `admin.skill.update`, and `admin.skill.delete` audit entries, verifying `go build ./...` succeeds.
- [x] 4.3 Add API unit tests in `server/internal/api/api_test.go` covering skill `.md` and `.zip` upload, re-upload upsert by skill name, invalid upload `400` rejection, enable/disable `PATCH`, `DELETE`, audit log emission, and non-admin/missing-CSRF `403` enforcement, verifying `go test -race ./internal/api/...` passes.

## 5. Admin Governance Console — Agent Skills Tab & MCP Parity

- [x] 5.1 Create `web/src/lib/agent-skills.ts` and `web/src/__tests__/agent-skills.test.ts` with pure helpers for computing skill MCP tool readiness badges against active `MCPServer` configurations and formatting skill metadata, and verify `npm test` in `web/` passes.
- [x] 5.2 Update `web/src/app/admin/page.tsx` to add the dedicated **Agent Skills** tab (drag-and-drop `.md`/`.markdown`/`.zip` upload, parsed metadata & MCP tool readiness badges, enable/disable toggle, collapsible instructions preview, and delete) and enhance **MCP Servers** cards with a server **Enabled / Disabled** toggle and discovered tool descriptions, verifying `npm run build` in `web/` succeeds without TypeScript or ESLint errors.
- [x] 5.3 Update `README.md` architecture and governance sections to document Agent Skills (`SKILL.md` / `.zip` bundles) and their interaction with MCP servers, and verify documented paths and endpoints match the implementation.

## 6. Cross-Cutting Integration Verification

- [x] 6.1 Run the full Go backend check (`go vet ./...` and `go test -race ./...` in `server/`) and Next.js frontend check (`npm test` and `npm run build` in `web/`) and confirm all suites pass with zero regressions.
- [x] 6.2 Run `openspec validate "add-agent-skills-and-mcp-orchestration" --strict` and confirm strict OpenSpec validation passes.

## Workflow follow-up

- Archive the change with `openspec-archive-change` after implementation and review are complete.
