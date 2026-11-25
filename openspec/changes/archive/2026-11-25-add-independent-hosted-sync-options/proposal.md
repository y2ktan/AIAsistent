# Proposal

## Why

Hosted ElevenLabs connections currently always synchronize application documents and enabled Agent Skills. Administrators who want provider-managed persona, knowledge, or skill configuration must be able to connect without application RAG or Agent Skill content while retaining the same authenticated hosted voice transport and workflow validation.

## What Changes

- Add independent administrator controls for synchronizing local RAG documents and synchronizing enabled Agent Skills to an ElevenLabs hosted agent.
- Preserve synchronization by default for existing and newly created configurations unless an administrator explicitly disables either option.
- Skip disabled synchronization domains without blocking hosted-agent activation, connection testing, token issuance, workflows, or MCP synchronization.
- Keep disabled domains visible in the admin UI with clear ownership and last-known status, and do not make local document or skill changes trigger provider synchronization for that domain.
- Preserve unrelated dashboard-managed prompts, Knowledge Base documents, and Agent Skill configuration; this change does not remove provider-owned hosted configuration merely because application synchronization is disabled.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `admin-governance-console`: Add independent hosted RAG and Agent Skill synchronization controls, persistence, and disclosures.
- `elevenlabs-hosted-conversation`: Permit hosted activation and access when either application synchronization domain is disabled.
- `rag-document-knowledge`: Make application-to-hosted Knowledge Base synchronization conditional while retaining local pgvector behavior.
- `agent-skill-orchestration`: Make hosted Agent Skill prompt synchronization conditional while retaining local-mode skill behavior.

## Impact

- Additive `agent_configs` persistence fields and strict admin API request/response fields with enabled-by-default upgrade behavior.
- Hosted prompt and document synchronization decisions, admin save activation, manual hosted sync, document lifecycle handlers, and Agent Skill lifecycle handlers in the Go API.
- Admin configuration and synchronization controls, hosted status copy, and focused frontend tests.
- No change to ElevenLabs credentials, voice transport, workflow validation, dashboard-owned workflow/tool behavior, MCP exposure, or local cascaded/realtime pipelines.
