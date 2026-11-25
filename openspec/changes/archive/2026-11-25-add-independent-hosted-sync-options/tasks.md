# Tasks

## 1. Persisting independent hosted synchronization policy

- [x] 1.1 Add a repeat-safe migration and `AgentConfigRecord` fields for enabled-by-default hosted RAG and Agent Skill synchronization; verify zero-row and upgraded database behavior in persistence tests.
- [x] 1.2 Extend PostgreSQL reads/writes, store fixtures, strict admin request handling, response JSON, and audit metadata with independent booleans; verify omitted fields preserve stored values while explicit `false` values persist.
- [x] 1.3 Preserve both policy values across mode switches and hosted-agent or credential updates; verify existing inactive-configuration preservation tests and add focused hosted-policy regression cases.

## 2. Gating hosted synchronization and enforcing cleanup

- [x] 2.1 Refactor hosted prompt compilation and synchronization to include Agent Skills only when the skill policy is enabled while always retaining the security preamble, persona, and system instructions; verify base-only and skill-enabled prompt payloads.
- [x] 2.2 Gate Agent Skill upload, enable/disable, delete, hosted skill availability validation, and manual synchronization by the Agent Skill policy; verify disabled operations make no ElevenLabs prompt request and local skill CRUD still succeeds.
- [x] 2.3 Gate document upload, reindex, hosted manual synchronization, and hosted save document work by the RAG policy while leaving local pgvector processing unchanged; verify disabled RAG operations make no Knowledge Base or agent attachment requests.
- [x] 2.4 Implement ownership-safe cleanup when RAG synchronization is changed from enabled to disabled: detach and delete only tracked application entries, preserve dashboard entries and local documents, update local hosted state to `not_synced`, and persist the disabled setting only after verification; verify success, partial failure, and idempotent retry cases.
- [x] 2.5 Keep hosted agent/workflow/tool and MCP validation active regardless of both policies; verify token issuance and manual sync succeed with either or both disabled while selected MCP resources still reconcile.

## 3. Updating administration controls and documentation

- [x] 3.1 Add separate hosted RAG and Agent Skill synchronization controls to the admin hosted-agent form, include enabled defaults and explicit disabled-state ownership explanations, and verify the controls independently update and submit `false`.
- [x] 3.2 Update hosted RAG, Agent Skill, and manual sync status text to reflect disabled domains without reporting them as failed; verify focused UI tests for each enabled/disabled combination.
- [x] 3.3 Update related operator documentation to describe independent synchronization, transition cleanup, dashboard-content preservation, rollback behavior, and re-enabling; verify the documented admin flow matches the rendered controls and API fields.

## 4. Verifying the hosted configuration change

- [x] 4.1 Run focused Go persistence, hosted synchronization, cleanup, lifecycle handler, and token-admission tests; verify all scenarios for RAG-only, skills-only, neither, and both-enabled configurations pass.
- [x] 4.2 Run the applicable admin frontend tests plus TypeScript and lint checks; verify no type, rendering, or form submission regression is introduced.
- [x] 4.3 Run `openspec validate --strict`; verify every proposal capability has a corresponding delta spec and all specification files pass strict validation.

## Workflow follow-up

- Archive the change after implementation review is complete.
- Verify the archived main specs and project behavior.
