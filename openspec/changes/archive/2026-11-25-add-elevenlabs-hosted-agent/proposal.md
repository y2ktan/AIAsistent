# Proposal

## Why

The application currently uses ElevenLabs only for cascaded speech recognition and synthesis; knowledge retrieval and reasoning remain in the Go pipeline. An optional hosted-agent mode lets ElevenLabs answer microphone input using its own attached knowledge base and configured language model without replacing existing sessions.

## What Changes

- Add an administrator-selected `elevenlabs_agent` conversation mode, alongside unchanged `cascaded` and `realtime` modes. Existing installations retain their saved mode.
- Configure a private ElevenLabs agent ID and reuse the existing separately encrypted ElevenLabs credential. Issue voice conversation tokens server-side only after application authentication, CSRF validation, and rate limiting.
- Connect the browser directly to the hosted agent through the official React SDK and WebRTC; retain the current voice UI, microphone/speaker controls, transcripts, and visible errors.
- Let ElevenLabs control turn-taking, interruptions, speech, RAG, and model responses in this mode. Clearly distinguish this opt-in behavior from strict wake-name, question, and pause gating in existing modes.
- Automatically synchronize the application's `ready` pgvector documents to the ElevenLabs Knowledge Base (enabling hosted RAG and preserving manually attached dashboard documents) across hosted activation, document uploads, reindexing, deletions, and manual retry, and automatically override the hosted agent's prompt and persona (`assistantName`, `systemInstructions`, and enabled declarative Agent Skills) server-side via the ElevenLabs Agent API. Keep LLM selection, voice, TTS model, languages, turn-taking, and retention managed in the ElevenLabs dashboard, with client-side overrides disabled.
- Scope the initial version to speech, synchronized prompt/skill guidance, and hosted RAG/model answers. Compile enabled Agent Skills into the synchronized hosted system prompt rather than a runtime backend tool bridge, and do not expose application MCP tools, camera input, or local citation panels in hosted mode; display these boundaries instead of implying full backend parity.
- Preserve local/provider settings and knowledge across mode switches. Report provider failures without automatically switching modes.
- Record the approved hosted revocation exception: server-side account/session invalidation immediately denies new access, but ending an existing direct provider call relies on client authentication checks rather than the Go media manager.

## Capabilities

### New Capabilities

- `elevenlabs-hosted-conversation`: Private hosted-agent authorization, direct browser voice sessions, server-synchronized prompt/persona/skills and additive knowledge grounding alongside dashboard-owned model/voice settings, session controls, errors, and explicit feature boundaries.

### Modified Capabilities

- `admin-governance-console`: Configure, test, and synchronize hosted-agent mode, agent ID, prompt/persona/skills, and local document sync status, disclose synchronized versus dashboard-owned settings and external processing, and preserve existing configuration.
- `rag-document-knowledge`: Synchronize local `ready` documents and deletions additively with the ElevenLabs Knowledge Base and RAG index without coupling local pgvector readiness to upstream cloud availability.
- `voice-vision-ai-agent`: Add hosted spoken answering and explicitly scope existing wake, pause, vision, local grounding, and MCP/runtime tool orchestration to the existing backend modes.
- `webrtc-media-streaming`: Distinguish backend media/control transport from hosted SDK transport and its audio-only behavior.
- `voice-language-support`: Keep application-managed language and voice policies in existing modes while delegating hosted language behavior to the dashboard.
- `agent-skill-orchestration`: Synchronize enabled declarative Agent Skills into the hosted agent's system prompt server-side while excluding runtime skill-loader tools and application MCP execution in hosted sessions.
- `auth-and-session-security`: Preserve immediate backend-session revocation while distinguishing client-detected termination of direct hosted calls and provider-issued access from local single-use signaling tokens.

## Impact

- Go admin record/store/handlers/router, ElevenLabs Knowledge Base and Agent `PATCH` synchronization service, authenticated session-config and hosted-token APIs, and their tests; additive PostgreSQL migrations for agent ID, expanded mode constraint, extracted document text, and per-document ElevenLabs sync state.
- Next.js admin and home pages, per-document hosted sync status and manual sync/retry controls, a mode-routing voice-session interface, an isolated hosted SDK adapter, dependency manifest/lockfile, security-header compatibility, and focused tests.
- ElevenLabs private-agent configuration, write-capable API key permissions (agent read/write, knowledge-base read/write/RAG-index, and conversation token issuance), cloud processing, retention, concurrency, and usage costs. No account configuration or credential access is authorized by this planning change.
- Existing reference guide remains illustrative: its keyword retrieval, direct Gemini call, mock order tool, and saved MP3 are not the hosted integration.
- No backend runtime tool/MCP bridge, camera integration, live local RAG citations, realtime-adapter repairs, or changes to existing cascaded speech defaults. External client, webhook, and MCP tools must remain disabled on the connected hosted agent.
