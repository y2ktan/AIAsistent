# Design

## Context

See `proposal.md` for motivation and the delta specs for behavior contracts. The user confirmed provider-owned turns/voice/language/LLM, automatic server-side synchronization of `assistantName`, `systemInstructions`, and enabled Agent Skills, additive synchronization of local pgvector documents to the ElevenLabs Knowledge Base (preserving unmanaged dashboard documents), an initial RAG-only external-tool boundary, and the explicit direct-connection revocation exception.

Observed integration points:

- `server/internal/api/store.go` and `pg_store.go` hold the singleton agent configuration, including an independently encrypted ElevenLabs key. `HandleUpdateAgentConfig` accepts only two modes and builds a replacement record. Preserve inactive fields deliberately rather than hiding controls and accidentally submitting empty values.
- `db/migrations/000001_init.up.sql` restricts `agent_configs.mode` to `realtime` or `cascaded`, and stores ingested `documents` metadata plus chunked text in `document_chunks` without raw file blobs. Startup reruns `.up.sql` migrations; new migrations must be repeat-safe.
- `GET /api/v1/rtc/config` currently exposes only assistant name and Audio Focus. The generic Next.js BFF forwards existing authentication, Origin, and CSRF headers and applies no-store security headers.
- `HandleRTCOffer` and `attachSessionAgent` create Pion sessions and load local configuration, skills, MCP, embeddings, and speech adapters. A hosted session must not enter this path.
- `web/src/app/page.tsx` consumes `useVoiceAgentRTC` directly. The hook owns Pion signaling, audio filtering, VAD, transcripts, citations, and media resources. The home page already stops the call on logout.
- Existing frontend suites use Vitest and mock the voice hook. Go API tests already cover configuration encryption, key masking, authentication, and preservation of voice settings.
- `web/src/lib/security-headers.ts` allows WebSockets but restricts HTTPS connections to self. Hosted SDK signaling may need additional specific HTTPS origins; do not assume current CSP works.

The tagged guide's Gemini call and keyword dictionary are not ElevenLabs hosted APIs. No account, credential, actual hosted-agent configuration, or paid voice session was inspected during planning.

## Goals / Non-Goals

**Goals:**
- Keep the hosted transport isolated from the existing backend media pipeline.
- Admit authenticated application users to one configured private, RAG-only agent without exposing the provider key.
- Automatically synchronize `assistantName`, `systemInstructions`, enabled declarative Agent Skills, and additive `ready` local documents to the configured hosted agent server-side, while keeping voice, TTS model, languages, LLM choice, turn-taking, and retention dashboard-owned.
- Preserve familiar audio controls and transcripts while making capability differences visible.
- Preserve existing configuration and session behavior through upgrades and mode switches.

**Non-Goals:**
- No backend runtime MCP/tool bridge, hosted camera path, local citation emulation, or overwriting of dashboard voice, language, or LLM settings.
- No broad audio/provider abstraction rewrite or repair of the existing realtime input path.
- No account onboarding, automatic agent creation, or automatic LLM migration.

## Decisions

### 1. Add a distinct mode, not a speech-provider value

Use `mode = elevenlabs_agent` with a new `elevenLabsAgentId` field. Keep `speechProvider = local | elevenlabs` as the cascaded-only setting. Reuse the existing ElevenLabs encrypted key and masked preview; a separate duplicate credential would introduce unnecessary administration and storage.

Add the agent ID column and widen the existing mode constraint without changing stored modes. Update API and UI types and preserve all inactive provider fields on save. Apply local speech/default-voice activation checks only to cascaded selection, including the current transactional check in `PGAdminStore.UpsertAgentConfig`. Retained local-language edits still follow their existing consistency rules.

Alternatives rejected: pointing `LLMBaseURL` at ElevenLabs is not a hosted conversation integration; extending `speechProvider` would falsely imply that local RAG, tools, and gating still run.

### 2. Validate and synchronize a private, simple RAG-only agent server-side

Extend the existing administrator connection-test route with a hosted branch, and add a server-side synchronization client for hosted saves and resource changes. Using the candidate agent ID and saved/submitted credential, read `GET /v1/convai/agents/{agent_id}` with a bounded safe HTTP client. Validate private authentication (`enable_auth == true`), confirm client-side runtime override flags remain disabled, and inspect external tool references in the returned configuration. Resolve referenced tool definitions when necessary; reject configurations with client, webhook, or MCP operations. Provider system tools for retrieval/turn control do not violate the RAG-only boundary.

The initial adapter supports a simple single agent. Reject unsupported workflow configurations rather than treating an incomplete inspection as evidence of no tools. Missing or unrecognized security-critical fields fail validation explicitly. Confirm the current documented configuration fields in the provider contract tests; do not guess them or return a pass on parse errors.

When an administrator saves or activates `elevenlabs_agent` (or updates `assistantName`, `systemInstructions`, or enabled Agent Skills while `elevenlabs_agent` is active), synchronize the hosted prompt and persona server-side via `PATCH /v1/convai/agents/{agent_id}`:

- Compose `conversation_config.agent.prompt.prompt` deterministically with the immutable security boundary preamble first, followed by the configured `assistantName`, `systemInstructions`, and each enabled Agent Skill's `SKILL.md` instructions plus bundled UTF-8 reference files clearly delimited as declarative skill playbooks.
- Enforce a bounded total prompt budget before sending `PATCH`; fail safely with a diagnostic if enabled skills exceed the budget rather than truncating the security preamble or silently dropping instructions.
- Update the agent's `name` (or persona framing) without modifying dashboard-selected LLM, TTS model, voice ID, language presets, turn/VAD settings, or enabling client-side `conversation_config_override` toggles.

Activation performs inspection and synchronization before persistence; session issuance repeats inspection so out-of-band dashboard tool changes are not hidden behind a stale admin save. Every `PATCH` publishes a new `version_id`; inspect/capture that version and pass `version_id` during token issuance so the checked configuration and admitted conversation agree. Require a verifiable configuration/version rather than silently ignoring unsupported version pinning.

A connection test verifies read access and structural compatibility without mutating the agent, whereas saving/activating `elevenlabs_agent` verifies write permissions and performs synchronization. Neither proves end-to-end speech playback or spoken answer quality, which require explicit acceptance checks.

### 3. Issue short-lived connection access through the authenticated backend

Add `POST /api/v1/elevenlabs/conversation-token` under existing active-session authentication and CSRF/Origin middleware, with issuance throttling comparable to signaling and keyed by authenticated user. Accept an empty request; never accept a browser-selected agent ID, provider URL, or key.

Load the saved hosted configuration, fail if the mode changed, decrypt its ElevenLabs key safely, validate the private RAG-only agent, then call:

```text
GET https://api.elevenlabs.io/v1/convai/conversation/token
    ?agent_id=<configured-id>&version_id=<validated-version>
xi-api-key: <server-held-secret>
```

Return a minimal typed response containing `conversationToken`, `conversationId`, mode, and the non-secret UI label. Preserve no-store headers through the BFF. Bound request time and response size; validate required fields and non-empty tokens. Distinguish invalid application authentication, incompatible configuration, rate limiting, and sanitized upstream failure. Do not return raw provider payloads or log keys, tokens, audio, transcript content, or dashboard prompt/configuration.

Add `mode` and safe capability metadata to the existing session-config response for routing. Do not include agent configuration or credentials there. A mode race produces a visible retry/configuration-refresh response, never a silent fallback. Explicitly reject new Pion offers for hosted selection before a hosted configuration can fall through to cascaded adapter construction.

Alternative rejected: a public agent ID in the browser bypasses application access control.

### 4. Use the official React SDK in an isolated audio adapter

Add `@elevenlabs/react` through the existing npm manifest and lockfile during implementation. Its current documentation requires `ConversationProvider`; use the installed version's typed hooks rather than copying an older unwrapped `useConversation` example.

Introduce a small mode-routing session facade for the home page. Retain `useVoiceAgentRTC` for both existing backend modes; isolate hosted lifecycle and SDK callbacks in a hosted adapter mounted only for hosted selection. Do not initialize two microphone connections or violate React hook ordering. Only the selected adapter is started. Keep existing helper exports/tests compatible where possible.

```text
Home voice UI
  |
  +-- backend mode --> existing RTC adapter --> Go/Pion pipeline
  |
  +-- hosted mode --> token API --> React SDK --> ElevenLabs WebRTC
```

Store the conversation token only in transient memory. The SDK connects with `conversationToken` and explicit voice/WebRTC selection; do not construct a second Pion peer or use a WebSocket signed URL in place of a WebRTC token.

Map SDK connection state, speaking/listening, user/assistant messages, and corrections to the existing UI-facing state and transcript shape. Display `retrieving` or `thinking` only if a documented event supports the claim; otherwise speaking/listening is sufficient. Handle tentative versus final messages without duplicate transcript entries and preserve corrections after interruption.

Use SDK microphone mute and output volume controls. Keep microphone and speaker mute independent, including pre-call intent. Feed SDK input/output energy to the existing orb where supported; do not create duplicate playback solely for visualization.

The installed React 1.23.1/client 1.37.1 SDK publishes an unmuted microphone before applying React mute state. The user approved maintaining a reproducible SDK extension to preserve strict start-muted behavior. Pin the SDK versions, apply a fail-closed package patch during installation, and test that microphone tracks are muted before publication and speaker volume is set before attachment. Pass only these local media controls, never dashboard persona/language/knowledge overrides. SDK upgrades require reviewing and retesting this patch.

Gate camera, local citations, and active-tool indicators off in hosted mode and clear stale data when transport changes. Keep the application's `assistantName` as the UI speaker label (matching the server-synchronized persona). Show provider ownership of turn-taking, voice, languages, LLM, and noise processing; the existing Audio Focus graph and wake/pause rules do not run in this mode. Because prompt, persona, enabled skills, and Knowledge Base entries synchronize strictly server-side via `PATCH`, the browser SDK never sends runtime prompt, language, voice, RAG, or skill `conversation_config_override` payloads.

### 5. Own lifecycle cleanup and expose the direct-connection limit

Serialize start/end operations and use a cancellation/generation guard so ending or leaving during token acquisition prevents a late SDK start. If cancellation races an already-started session, end it immediately. Always clear the token and release owned resources after failure or disconnect.

On call end, logout, unmount, or detected authentication loss, invoke SDK teardown and stop owned tracks/playback. Revalidate application authentication during hosted calls, proposed every 30 seconds and on foreground return; on non-success or unavailable validation, stop the call with a safe message. Cancel polling on teardown. Preserve displayed transcript text after unexpected disconnection.

The Go session manager cannot forcibly close a direct provider connection owned by a remote browser. Account/session revocation blocks further token issuance; cooperative clients stop when authentication checks detect revocation. The user explicitly approved this exception to the existing immediate-media-termination contract, captured in the security delta. This is not a server-enforced instantaneous termination guarantee, and provider token expiry is not proof that an established call ends. Browser timer throttling or suspension can delay detection beyond the proposed 30-second polling cadence. Document that operational limitation instead of inventing an undocumented remote-end API. A stricter revocation requirement would need a separately scoped design.

### 6. Additive Knowledge Base synchronization, partial dashboard ownership, and least-privilege browser connectivity

Add a repeat-safe migration extending `documents` with `extracted_text` (populated on upload) plus ElevenLabs sync tracking columns (`elevenlabs_doc_id`, `elevenlabs_synced_hash`, `elevenlabs_agent_id`, `elevenlabs_sync_status`, and `elevenlabs_sync_error`). For pre-existing `ready` rows where `extracted_text` is empty, reconstruct document text deterministically from ordered `document_chunks` (trimming exact 360-character boundary overlaps where present) so existing pgvector knowledge synchronizes without re-uploading source files.

Synchronize local `ready` documents additively when `elevenlabs_agent` is activated/saved, when local documents are uploaded, reindexed, or deleted while `elevenlabs_agent` is active, or when an administrator invokes an explicit `/admin` hosted-sync/retry endpoint:

1. For each `ready` local document whose `elevenlabs_doc_id` is missing, targets a different `agent_id`, or has a changed SHA-256 content hash, create a text Knowledge Base entry via `POST /v1/convai/knowledge-base/text` using the document title and reconstructed/extracted text, and delete any superseded tracked `elevenlabs_doc_id` (`DELETE /v1/convai/knowledge-base/{documentation_id}?force=true`).
2. When a created entry has at least 500 UTF-8 bytes of text, trigger RAG indexing via `POST /v1/convai/knowledge-base/{documentation_id}/rag-index` (using the agent's configured RAG embedding model or `e5_mistral_7b_instruct` default) and record `indexing` or `synced` status; entries smaller than 500 bytes skip RAG index computation per ElevenLabs' documented minimum and attach directly without failing.
3. Read the current agent configuration via `GET /v1/convai/agents/{agent_id}`, preserve all unmanaged `conversation_config.agent.prompt.knowledge_base` entries that do not match any application-tracked `elevenlabs_doc_id`, append/update all current application-tracked entries with `usage_mode: "auto"`, ensure `conversation_config.agent.prompt.rag.enabled = true`, and apply via `PATCH /v1/convai/agents/{agent_id}`.
4. When a local document is deleted in `/admin`, remove its local record/chunks and best-effort delete its tracked `elevenlabs_doc_id` (`?force=true`) when hosted credentials exist, without touching unmanaged dashboard documents.
5. Keep failure domains isolated: if ElevenLabs KB upload, RAG indexing, or `PATCH` fails during a local document upload, local pgvector status still becomes `ready`, while `elevenlabs_sync_status` becomes `failed` with a redacted diagnostic and `/admin` retry control. Do not download hosted conversations for local persistence.

Document partial dashboard ownership clearly: `assistantName`, `systemInstructions`, enabled Agent Skills, and `ready` local documents synchronize automatically from `/admin` (while preserving additional dashboard-attached KB documents), whereas private authentication, disabled external tools, LLM choice, voice, TTS model, languages, turn-taking, and retention remain configured in the ElevenLabs dashboard. Do not copy the guide's Gemini 2.5 Flash model as a mandatory default; dashboard availability and the provider's current deprecation policy govern model choice.

Add only the exact HTTPS signaling origins required by the installed SDK and configured default provider location to CSP. Preserve the current microphone/camera Permissions Policy. Do not add wildcard HTTPS, broad frame access, or unrelated script origins. Verify actual WebRTC signaling origins in browser acceptance testing; URLs cannot be inferred solely from the API token host. Initial support targets the default provider location, not a new residency selector.

## Risks / Trade-offs

- [Provider changes APIs or SDK hooks] -> Use typed current APIs, mocked provider contracts, strict response validation, and a real session check; no account compatibility claim from documentation alone.
- [External tools hidden in dashboard references/workflows] -> Inspect referenced definitions, reject unsupported workflows, and validate the version used for new token issuance; do not rely on absence of a single inline array.
- [Workspace KB size tier limits, async RAG indexing latency, or oversized skill prompts] -> Track per-document hosted sync state (`synced`, `indexing`, `failed`, `not_synced`) separately from local pgvector `ready` status, enforce bounded prompt budgets before `PATCH`, handle `<500B` non-RAG KB entries cleanly, and expose `/admin` retry diagnostics.
- [Concurrent dashboard and `/admin` edits] -> Read-modify-write via `GET` + `PATCH` preserves unmanaged dashboard `knowledge_base` entries and untouched LLM/voice/language fields while publishing a verifiable new `version_id`.
- [Direct media bypasses Go revocation and gating] -> Make provider-owned turns explicit, stop cooperative clients on auth loss, and document the server-enforcement limit.
- [Cloud privacy, cost, retention, and concurrency] -> Disclose speech, prompt/skill, and document cloud transmission before activation/start, require a private agent, throttle issuance, and avoid extra transcript/audio logging.
- [Stale settings or double capture] -> Preserve inactive configuration, isolate adapter mounting, cancel late starts, and test mode races and teardown.
- [Language, noise-filtering, camera, and citation expectations] -> Explicit feature metadata and disabled unsupported controls, with no fabricated parity.
- [Credential reuse permissions] -> Require write-capable account permissions for agent inspection/update, Knowledge Base create/delete/RAG-index, and voice-token issuance; retain the existing masked-key handling.

## Migration Plan

1. Add repeat-safe migrations for the agent ID, expanded mode constraint, `documents.extracted_text`, and per-document ElevenLabs sync tracking columns. Preserve existing rows, secrets, voices, and documents.
2. Deploy backend and frontend together; the saved mode remains unchanged and hosted access or cloud synchronization stays inactive until explicitly selected.
3. An administrator configures a private, tools-disabled agent with desired LLM/voice/language/retention in the ElevenLabs dashboard, then saves its ID and write-capable key on `/admin`; activation validates compatibility and automatically synchronizes `assistantName`, `systemInstructions`, enabled Agent Skills, and `ready` local documents (enabling hosted RAG while preserving any manual dashboard documents).
4. Validate an authenticated microphone question grounded in a synchronized local policy document and an enabled Agent Skill, plus transcripts, interruption, mute, end/logout cleanup, denied access, and visible provider/playback errors. Verify both existing modes still route to their original adapters.
5. Roll back selection by choosing the previous mode without deleting hosted settings or local documents. Before rolling back software, select a mode the older version accepts. Retain additive storage; a destructive down migration requires separate approval and must not run against hosted-mode rows without an explicit conversion decision.

## Verification Plan

- Extend Go API and RAG tests for mode validation/preservation, private/tool-free agent inspection, server-side `PATCH` prompt/persona/skill composition and budget limits, additive KB sync (`POST /v1/convai/knowledge-base/text`, `rag-index` >= 500B, `<500B` handling, unmanaged dashboard doc preservation, `?force=true` deletion, pre-existing chunk reconstruction, and upstream failure isolation), encrypted-key handling, token issuance auth/CSRF/Origin/rate limits, version consistency after `PATCH`, sanitized provider failures, no-store responses, and hosted/Pion routing separation.
- Extend Vitest coverage with SDK and API mocks for routing, provider wrapping, transcript updates, unsupported controls, independent mute, token/start cancellation, auth-loss/disconnect cleanup, `/admin` synchronized-vs-dashboard disclosure, per-document hosted sync badges/retry controls, and absence of local RAG/MCP/camera turn calls.
- Run targeted Go tests, `gofmt` checks for changed Go files, targeted Vitest selectors, `npx tsc --noEmit`, and ESLint on changed frontend files. The current `npm run lint` invokes `next lint`; use the installed ESLint CLI if that script is unavailable under Next 15 rather than expanding this change to unrelated tooling repairs. Run the production build for SDK/App Router integration.
- Manual cloud checks require separately authorized credentials and usage. Leave those tasks open until performed; mocked tests do not prove microphone playback or RAG grounding.

## References

- Agent retrieval: https://elevenlabs.io/docs/api-reference/agents/get
- Agent update (`PATCH`): https://elevenlabs.io/docs/api-reference/agents/update
- Create text KB document: https://elevenlabs.io/docs/api-reference/knowledge-base/create-from-text
- Delete KB document: https://elevenlabs.io/docs/api-reference/knowledge-base/delete
- Hosted knowledge and RAG indexing: https://elevenlabs.io/docs/eleven-agents/customization/knowledge-base/rag
- Runtime overrides security flags: https://elevenlabs.io/docs/eleven-agents/customization/personalization/overrides
- WebRTC token contract: https://elevenlabs.io/docs/api-reference/conversations/get-webrtc-token
- React provider, authentication, controls, and events: https://elevenlabs.io/docs/eleven-agents/libraries/react
- Agent LLM selection: https://elevenlabs.io/docs/eleven-agents/customization/llm
