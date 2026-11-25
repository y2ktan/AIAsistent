# Design

## Context

The hosted save path currently runs one `syncHostedResources` sequence: compile and patch the hosted prompt, synchronize every ready local document, reconcile hosted MCP resources, and validate skill tool availability. Document upload/reindex handlers and Agent Skill create/update/delete handlers independently invoke the corresponding sync path whenever hosted mode is active. `AgentConfigRecord` and the `agent_configs` table have no policy fields for those two synchronization domains, while the admin UI always describes both as automatic.

The hosted prompt is application-owned at the fields the patch writes: assistant name and a compiled prompt. The Knowledge Base sync tracks application-created ElevenLabs document IDs separately from dashboard-owned attachments. These ownership boundaries allow disabling either feature without touching unrelated provider configuration.

## Goals / Non-Goals

**Goals:**

- Persist and expose separate `hosted RAG synchronization` and `hosted Agent Skill synchronization` choices.
- Default both choices to enabled for zero-row, upgraded, and newly created configurations.
- Allow hosted activation and token issuance with either or both choices disabled.
- Stop disabled-domain lifecycle and manual-sync provider operations.
- Remove application-owned content when a previously enabled domain is disabled, while preserving dashboard-owned hosted content and local application records.

**Non-Goals:**

- Removing or replacing provider-managed dashboard prompts, workflows, tools, Knowledge Base documents, or skills.
- Adding per-document or per-skill hosted inclusion controls; each domain remains one global switch.
- Changing local cascaded/realtime RAG and Agent Skill behavior.
- Adding a separate hosted mode or a different voice transport.
- Automatically restoring application-managed provider resources when the setting is later enabled; normal synchronization handles that on save or explicit retry.

## Decisions

### 1. Store two independent enabled-by-default booleans

Add `elevenlabs_rag_sync_enabled` and `elevenlabs_agent_skills_sync_enabled` to `agent_configs`, both `BOOLEAN NOT NULL DEFAULT TRUE`. Map them to API and TypeScript fields named `elevenLabsRagSyncEnabled` and `elevenLabsAgentSkillsSyncEnabled`.

The admin update request uses pointer booleans. An omitted field retains the stored value, while an explicit `false` is accepted. This avoids JavaScript/JSON zero-value ambiguity and preserves compatibility with older clients. The zero-row fallback returns `true` for both fields.

Alternative rejected: one combined “basic hosted mode” switch would prevent administrators from using hosted RAG without application skills or vice versa, contrary to the requested independent controls.

### 2. Keep connection and validation independent from synchronization policy

Hosted credential, private-agent, workflow, version, workflow-tool, and MCP validation always run. RAG synchronization is conditionally invoked, and skill loading/compilation and hosted skill-tool availability checks occur only when Agent Skill synchronization is enabled.

The prompt patch remains mandatory for the application-owned assistant name and base system prompt. With Agent Skill synchronization disabled, the compiler receives no skill records and produces only the security boundary, persona, and application system instructions. This removes previously injected application skill sections without requiring the application to parse or preserve a provider dashboard prompt.

Alternative rejected: skipping the prompt patch entirely would leave stale application skills active after an administrator disabled them and would make the control ineffective.

### 3. Reconcile disabled domains before reporting the new setting active

When Agent Skill synchronization transitions from enabled to disabled, activation rebuilds and verifies the hosted prompt without application skill sections before the configuration is persisted.

When RAG synchronization transitions from enabled to disabled, the server reads the current agent attachment list and detaches only tracked application document IDs, deletes those tracked provider Knowledge Base entries through the existing bounded client, preserves unmanaged attachments, and updates local synchronization state to `not_synced`. Provider cleanup is verified before the disabled setting is persisted. A failed cleanup leaves the previous enabled setting unchanged and reports a retryable sanitized error.

When disabled from an initial deployment with no tracked application entries, no Knowledge Base read, upload, index, attachment, or delete operation is required.

Alternative rejected: merely stopping future uploads would leave previously synchronized application documents active, so the agent would still be using application RAG after the control was disabled.

### 4. Gate lifecycle and manual synchronization by domain

Document upload, reindex, and delete behavior continues to update local pgvector data. Hosted document synchronization runs after local mutations only if application RAG synchronization is enabled, except that deletion may still clean a tracked provider entry for revocation correctness.

Agent Skill upload, update, and delete behavior continues to persist local skill records. Hosted prompt resynchronization and hosted tool-availability validation run only if Agent Skill synchronization is enabled.

Manual hosted sync validates the agent, synchronizes only enabled domains, always reconciles selected MCP resources, and returns success if disabled domains are present. The UI describes the exact domains affected rather than reporting “prompt, skills, and documents” unconditionally.

### 5. Preserve ownership and expose state without secrets

The admin configuration response includes only the two booleans and the existing safe hosted status fields. It does not expose ElevenLabs content, provider IDs beyond fields already allowed by the API, or credentials.

The admin UI places both controls next to the hosted agent configuration, defaults their unsaved state to enabled, and displays disabled status in the RAG and Agent Skill views. Disabling text explains that dashboard-managed hosted content remains untouched and that only application-managed synchronization stops.

Audit metadata records the two policy values without recording prompts, document text, skill content, or credentials.

### 6. Keep synchronization changes explicit and testable

Add focused Go tests for:

- migration/default behavior and strict admin request fields;
- omission versus explicit `false`;
- hosted save with RAG disabled, skills disabled, and both disabled;
- disabled-domain lifecycle handlers making no provider calls;
- disabling active RAG cleaning only tracked entries;
- cleanup failure preserving the prior setting;
- prompt compilation excluding skills when disabled and still retaining persona/security instructions;
- manual sync gating and MCP synchronization remaining active.

Add focused admin UI tests for defaults, independent toggles, disabled status copy, and sync button behavior.

## Risks / Trade-offs

- [Risk] Disabling RAG fails after some provider resources were deleted. → Reconcile idempotently by tracked ID, save local state after each verified removal, and retry remaining entries without touching unmanaged attachments.
- [Risk] Disabling skills leaves stale provider prompt content. → Patch and verify the base-only hosted prompt before persisting the disabled setting.
- [Risk] Old clients omit the new fields and accidentally re-enable a previously disabled option. → Use pointer request fields and preserve stored booleans when omitted.
- [Risk] Existing deployments depend on current automatic synchronization. → Add database defaults of `TRUE` and return `TRUE` for zero-row/default records; require an explicit administrator change to disable either domain.
- [Risk] Administrators interpret “disabled” as deletion of dashboard-owned content. → Preserve unmanaged provider attachments and explain ownership in UI copy and failure messages.

## Migration Plan

1. Add a repeat-safe migration for two `NOT NULL DEFAULT TRUE` `agent_configs` columns.
2. Deploy backend support that accepts the fields additively and treats omitted values as stored values.
3. Deploy the admin controls and status messaging alongside the backend.
4. On rollback, the added columns can remain in place. Older binaries ignore them; synchronization returns to the previous always-enabled behavior after a rollback, so administrators requiring disabled synchronization must not return to an older binary without accepting that behavior.
