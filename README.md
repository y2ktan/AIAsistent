# Aegis Voice — Secure WebRTC RAG Voice & Vision AI Assistant

Enterprise-grade, low-latency multimodal voice-and-vision AI assistant built with **Next.js (App Router)**, **Go (`pion/webrtc/v4`)**, and **PostgreSQL 16 + `pgvector`**.

## Architecture

- **`web/` (Next.js + TypeScript + Tailwind CSS)**:
  - `/` — Immersive 60fps WebGL/Three.js audio-reactive voice & camera assistant with live PiP self-view, kinetic captions, and expandable RAG citations.
  - `/login` — Credential login and one-time initial administrator bootstrap flow.
  - `/admin` — Role-gated governance console for Users & Roles, RAG Knowledge Base, AI Agent Configuration (write-only encrypted secrets), MCP Servers, Agent Skills, and Security Audit Logs.
- **Audio Focus** is enabled by default and can be toggled in **`/admin` → AI Agent**. It isolates near-field speech with browser voice-isolation capture hints, speech-band Web Audio filtering, adaptive ambient-noise VAD, and downstream gating before audio is sent over WebRTC. `realtime` sessions also request provider-side `near_field` input noise reduction.
  - Authenticated callers receive only the non-secret `assistantName` and `audioFocusEnabled` fields from `GET /api/v1/rtc/config`; provider credentials and full admin configuration remain admin-only.
- **`server/` (Go + Pion WebRTC)**:
  - DTLS-SRTP Opus audio & VP8/H.264 video tracks + ordered `events` SCTP data channel.
  - Dual-mode `SessionAgent` (`realtime` WebSocket bridge & `cascaded` OpenAI-compatible STT → Vision/RAG/Agent Skills/MCP LLM → Streaming TTS); skills are progressively loaded as playbooks and external operations remain restricted to allowlisted MCP tools.
  - AES-256-GCM secret vault, Argon2id password hashing, rotating `HttpOnly; Secure; SameSite=Strict` sessions, CSRF + Origin validation, SSRF-safe outbound dialer, and token-bucket rate limiters.
- **`db/migrations/` (PostgreSQL + `pgvector`)**:
  - HNSW cosine similarity index (`vector_cosine_ops`) for RAG document chunks, plus `users`, `sessions`, `signaling_tokens`, `documents`, `agent_configs`, `mcp_servers`, `agent_skills`, and `audit_logs`. Migration `000008` adds extracted document text and separate ElevenLabs hosted-sync tracking without changing local document readiness.

## Agent Skills and MCP Governance

Administrators can upload declarative Agent Skills from **`/admin` → Agent Skills**. A skill is a Markdown `SKILL.md` (uploaded with a `.md` or `.markdown` extension) with `name` and `description` YAML frontmatter, or a `.zip` bundle containing `SKILL.md` and optional UTF-8 text references (`.md`, `.markdown`, `.txt`, `.json`, `.yaml`, or `.yml`). Optional `allowed-tools` frontmatter lists MCP tool names used by the playbook. ZIP bundles may place `SKILL.md` at the archive root or inside one top-level folder.

Uploads are limited to 2 MB compressed, 4 MB expanded, and 32 files. The server validates paths and text content, stores reference files in PostgreSQL, and never executes uploaded content. Re-uploading a skill with the same normalized name updates its content while preserving its identifier and enablement state. Administrators can inspect instructions and reference paths, check MCP tool readiness, enable or disable skills, and delete them. Disabled skills are omitted from agent discovery.

Both `realtime` and `cascaded` agents receive a compact catalog of enabled skill names and descriptions. The model loads relevant instructions with the built-in `load_skill` tool and reads listed references with `read_skill_file`. Skills guide analysis and MCP tool selection but cannot override system security rules or MCP allowlists. MCP tools are exposed only when their server is enabled and each tool is allowlisted; discovered descriptions and input schemas are passed to the model, with colliding tool names routed to their originating server. Tool calls are bounded to six per turn.

The admin API endpoints are:

| Method | Endpoint | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/admin/skills` | List skill metadata, instructions, and reference paths |
| `POST` | `/api/v1/admin/skills` | Upload a `.md`, `.markdown`, or `.zip` package as multipart field `file` |
| `PATCH` | `/api/v1/admin/skills/{id}` | Enable or disable a skill with `{"enabled": true}` |
| `DELETE` | `/api/v1/admin/skills/{id}` | Delete a skill |

These endpoints require an authenticated administrator and CSRF validation; uploads are rate-limited. Upload, update, and delete actions are written to the security audit log. MCP server enablement, tool descriptions, and per-tool allowlists are managed in **`/admin` → MCP Servers**.

## Environment Variables

Copy `.env.example` to `.env` (never commit `.env` or secret artifacts; `.gitignore` excludes `.env`, `.env.*`, `*.pem`, `*.key`):

```bash
cp .env.example .env
```

| Variable | Description |
| --- | --- |
| `POSTGRES_USER` | PostgreSQL username (no hardcoded default) |
| `POSTGRES_PASSWORD` | PostgreSQL password (no hardcoded default) |
| `POSTGRES_DB` | PostgreSQL database name |
| `MASTER_ENCRYPTION_KEY` | 32-byte key (64 hex chars or 32-byte base64) used for AES-256-GCM encryption of AI API keys and MCP auth headers |
| `ADMIN_BOOTSTRAP_TOKEN` | One-time secret token required to register the very first `admin` account when `users` is empty |
| `SERVER_PORT` | Go backend HTTP port (default `8080`) |
| `WEB_ORIGIN` | Allowed browser origin for CSRF `Origin` verification (default `http://localhost:3000`) |
| `HOSTED_MCP_PROXY_BASE_URL` | Optional public HTTPS origin routed directly to the Go backend's `/api/v1/hosted-mcp/{id}` endpoint; required for hosted MCP proxy mode, never inferred from browser headers or `WEB_ORIGIN` |
| `COOKIE_SECURE` | Set `true` in HTTPS production environments (`false` only for local HTTP dev) |
| `ALLOW_PRIVATE_AI_ENDPOINTS` | Set `false` in production to block SSRF to RFC1918/loopback/metadata IPs; set `true` only for local self-hosted AI/MCP endpoints |
| `ICE_UDP_MUX_PORT` | Single deterministic UDP/TCP port for Pion WebRTC ICE muxing (default `8443`) |
| `ICE_PUBLIC_IP` | Optional public IPv4/IPv6 advertised in ICE candidates when behind NAT |
| `STUN_URLS` | Comma-separated STUN server URLs (default `stun:stun.l.google.com:19302`) |
| `BACKEND_INTERNAL_URL` | Internal URL used by Next.js BFF to reach the Go backend (default `http://server:8080`) |

## Local Ollama Setup

Run `./setup-local-ai.sh` to configure the local AI stack. Its default LLM is `qwen3.8:latest`; override it with `OLLAMA_LLM_MODEL=<model-name> ./setup-local-ai.sh`. The script pulls the model if needed and updates the default agent configuration.

## One-Time Admin Bootstrap Flow

1. Generate strong random secrets for `MASTER_ENCRYPTION_KEY` (`openssl rand -hex 32`) and `ADMIN_BOOTSTRAP_TOKEN` (`openssl rand -base64 24`).
2. Start the stack with `docker compose up --build`.
3. Navigate to `http://localhost:3000/login`. When zero users exist in the database, the UI presents the **Initial Admin Bootstrap** form.
4. Enter the `ADMIN_BOOTSTRAP_TOKEN`, your admin email, and a strong password (minimum 12 characters).
5. Once the first admin is created, the bootstrap endpoint permanently disables itself (`409 Conflict` on any subsequent attempt). No default credentials are ever seeded.

## Pion WebRTC UDP Mux Port Settings

To avoid exposing thousands of ephemeral UDP ports in Docker, Pion's `SettingEngine` is configured with a single-port `ICEMux` (`ICE_UDP_MUX_PORT=8443` by default) and TCP ICE fallback on the same port (`8443/udp` and `8443/tcp` in `docker-compose.yml`). When deploying behind a cloud firewall or NAT, open `8443/udp` and `8443/tcp` and set `ICE_PUBLIC_IP` to your host's reachable public IP.

## Wake-Name, Pause, and Direct-Question Voice Gating

Voice sessions are designed for noisy rooms with background talkers and frequent thinking-out-loud pauses:

- **Configurable Assistant Name & Wake Aliases (`/admin` → AI Agent)**:
  - **Assistant Name** defaults to `Aegis` and can be customized to any non-empty English or Chinese name (up to 64 characters, such as `Jarvis` or `小智`).
  - **Wake Aliases** accept optional comma-separated English or Chinese alternate names (for example `小智, Aegis`), with automatic Simplified/Traditional Chinese character equivalence and STT vocabulary hinting.
  - The active primary `assistantName` is rendered on the `/` home page header and assistant transcript speaker badges.
- **Strict Wake-Name Gate & Name-Verified Barge-In**:
  - Speech that does not begin with a configured wake name or alias (optionally preceded by a brief greeting like `"hey"`, `"hi"`, `"喂"`, or `"你好"`) is silently discarded without interrupting active `retrieving`, `thinking`, or `speaking` states and without appearing in live captions.
  - Interrupting an in-progress assistant reply (`barge_in`) requires addressing the assistant by a configured wake name or alias.
- **Pause Tolerance & Direct-Question Gate**:
  - Client and server VAD hold active speech segments across pauses up to `2.5s`, and wake-addressed statements remain armed silently for up to `15s` across multi-clause thinking pauses without emitting filler acknowledgements (`"got it"`, `"mm-hmm"`, `"okay"`).
  - Both `cascaded` and `realtime` voice sessions generate a spoken reply only after the wake-addressed turn includes a direct question (English, Mandarin, Cantonese, or Malay).

## Voice Languages

Configure supported spoken languages in **`/admin` → AI Agent → Voice languages**:

- **Initial languages**: On a fresh install, English (default), Mandarin Chinese, Cantonese, and Malay are listed.
- **Default language rules**: Exactly one language must be marked as the default, and the default language must have an assigned voice. You cannot remove the default language until you select another default.
- **Per-language voices & fallback**: In `cascaded` mode, replies are spoken using the voice assigned to the reply's language. If a user speaks an unsupported language—or a supported language with no assigned voice—the assistant answers in the default language and notifies the user once per session that it cannot reply in that language.
- **Cantonese & Malay voices**: The bundled local Kokoro TTS engine provides English and Mandarin voices (`af_heart`, `zf_xiaoxiao`) out of the box. To speak Cantonese or Malay instead of falling back to the default language, configure a TTS engine that provides voices for them and assign those voice names in `/admin`.

## ElevenLabs Speech Provider

In **`/admin` → AI Agent**, cascaded sessions can use the existing OpenAI-compatible speech services or ElevenLabs for both STT and TTS. The LLM remains on its separately configured provider. New sessions use the saved selection; active sessions keep their starting provider.

- Enter the ElevenLabs API key only in the administrator form. The backend encrypts it separately and read responses show a masked preview containing only the final five characters. Leaving the field blank preserves the saved key; non-empty keys must contain more than five characters.
- Defaults are `scribe_v3` for STT and `eleven_flash_v2_5` for low-latency TTS. These models remain editable. The configured account is responsible for model availability and usage charges.
- Voice assignments are provider-specific. Discover available ElevenLabs voice IDs in `/admin`, or enter an ID manually when discovery is unavailable. Local voice names are never reused automatically as ElevenLabs IDs.
- ElevenLabs sends speech and generated text to an external cloud service. Errors remain visible and do not silently fall back to the local provider. Rotate a key by entering a replacement in `/admin`; do not place credentials in source, `.env` examples, logs, or tickets.
- Failed synthesis returns the call to listening and preserves the text reply. Payment/access failures do not retry other voices: HTTP 402 prompts you to check subscription, credits, and configured-voice access, but does not by itself prove a subscription problem. An audio response containing no speech packets is also reported as a failure.
- If the browser rejects reply-audio playback, the call shows a playback error instead of hiding it. Turn the speaker off and on to retry playback.
- Scribe supports Cantonese, but `eleven_flash_v2_5` does not list Cantonese in its model language list. Leave Cantonese without an ElevenLabs voice or choose a verified compatible model/voice before assigning it.

## Issues and Solutions

### ElevenLabs Hosted Agent (optional)

The `elevenlabs_agent` mode connects the browser directly to a private ElevenLabs agent. Unlike the cascaded ElevenLabs speech provider, the hosted agent owns speech recognition, turn-taking, RAG, model responses, and speech output. Existing `cascaded` and `realtime` settings remain saved when switching modes.

1. In the [ElevenLabs dashboard](https://elevenlabs.io/app/agents), configure a private, authentication-enabled agent. Supported workflows use `start`, `tool`, and `end` nodes with `unconditional`, `llm`, or `result` edge conditions. Server-side webhook tools and verified MCP references are supported; browser-client tools, standalone subagents/transfers, unknown nodes/conditions, schema overrides, and client overrides that can introduce tools or workflows remain rejected.
2. Select the LLM, voice, languages, and retention policy in the dashboard. Enable hosted RAG and verify indexing for dashboard-managed documents. Consult the [RAG guide](https://elevenlabs.io/docs/eleven-agents/customization/knowledge-base/rag) and [current model options](https://elevenlabs.io/docs/eleven-agents/customization/llm); the reference Python guide's Gemini model, keyword dictionary, mock order lookup, and MP3 output are not this integration.
3. In `/admin` -> AI Agent, select hosted mode, enter the private agent ID, and configure the existing write-only ElevenLabs key. The key must permit agent read/update and conversation-token issuance, plus Knowledge Base create/delete/read and RAG-index creation when application document synchronization or tracked-entry cleanup is needed. Choose **Synchronize application RAG documents** and **Synchronize application Agent Skills** independently; both default to enabled. Test the configuration and use **Save Encrypted Config**. Hosted activation always synchronizes `assistantName`, the security preamble, and `systemInstructions`; enabled local skills are included only when skill synchronization is enabled. A successful connection test proves access and compatibility, not microphone playback or RAG grounding.
4. Start a call as an authenticated application user and ask a question covered by an indexed policy. Confirm both the spoken answer and transcript before relying on it.

Hosted MCP integrations default to **disabled**, including after migration `000009`. In `/admin` -> MCP Servers, discover tools, choose the explicit allowlist, and select **direct** or **proxy**. Direct registration requires a public HTTPS endpoint, every currently discovered tool to be allowlisted, and explicit consent to share its authentication with ElevenLabs. Proxy mode sends only a dedicated encrypted-at-rest proxy credential to ElevenLabs and keeps the upstream credential local; it exposes only current enabled/discovered/allowlisted tools. Configure `HOSTED_MCP_PROXY_BASE_URL` on the Go server and route `/api/v1/hosted-mcp/{id}` directly to that server with Authorization preserved, not through the browser-session Next.js BFF. The endpoint supports bounded initialization, tool listing, and tool calls, plus the initialization notification, with per-server bearer authentication, rate limits, and timeouts.

Provider-executed tools can send conversation-derived data externally, cause side effects, and incur charges; they do not start the local conversational tool loop. When application skill synchronization is enabled, enabled skills' `allowed-tools` references must exist in the validated hosted inventory. Prompt/Knowledge Base synchronization preserves dashboard workflows and unrelated tools. Application-managed MCP resources use a persisted creation intent and non-secret `request_meta` ownership marker (which may reach the upstream service); after ambiguous creation, retry recovers only a unique verified marker/configuration match and never blindly creates another resource. Review sanitized sync status and use **Retry hosted sync / cleanup** (`POST /api/v1/admin/mcp-servers/{id}/sync`). Failed or incomplete revocation retains a disabled ownership record until remote cleanup is verified. Unverifiable recovery may require administrator assistance. Disable and clean up managed integrations before changing the provider key or agent ID.

Hosted mode is initially **audio with optional application RAG and skills**: it has no runtime application skill/MCP bridge, camera input, local citation panels, or application voice/language overrides. Enabled Agent Skills are declarative prompt instructions, not callable tools. With application RAG synchronization enabled, `ready` documents in the app's admin workspace sync additively to the hosted Knowledge Base, preserving dashboard-managed documents. Uploads and reindexing trigger sync automatically. Deletion removes a tracked hosted copy even when application RAG sync is disabled, when credentials and ownership evidence exist. Local pgvector readiness is independent of hosted sync failures. Keep LLM, voice, TTS model, languages, turn-taking, and retention settings in the ElevenLabs dashboard.

**Independent synchronization:** The admin API fields are `elevenLabsRagSyncEnabled` and `elevenLabsAgentSkillsSyncEnabled`. Omitted fields preserve saved values; explicit `false` disables that domain. Disabling RAG on a hosted save detaches only tracked application attachments, verifies dashboard attachment preservation, deletes and verifies each tracked Knowledge Base entry, then clears its local hosted state to `not_synced`. Local documents and chunks remain intact. With no tracked entries, disabling requires no Knowledge Base operation. Disabling skills rebuilds and verifies the base-only application prompt; it does not preserve a dashboard prompt in the application-owned prompt field, but leaves unrelated dashboard configuration untouched. Subsequent disabled-domain document or skill mutations stay local.

**Cleanup, retry, and re-enabling:** A failed cleanup or base-prompt verification prevents the policy save; the previously saved choice remains active. Partial remote cleanup cannot be rolled back automatically: verified removals are recorded individually, and repeating **Save Encrypted Config** targets the remaining tracked entries. Re-enable either checkbox and save to synchronize current ready documents or enabled skills again, including current tool-reference validation. **Sync / retry hosted agent** in RAG Knowledge (`POST /api/v1/admin/agent-config/sync`) always synchronizes the base prompt and selected MCP integrations, and only the enabled application domains. Disabled domains are reported as disabled, not failed; document rows retain last-known hosted state. Private-agent, workflow, tool, and MCP validation remain required even when both domains are disabled.

The app's strict wake-name, direct-question, 15-second thought buffer, 2.5-second VAD hold, name-verified interruption, and Audio Focus filtering graph do not run in hosted mode. ElevenLabs controls those conversation behaviors. Speech and conversation data leave the application's backend boundary and may incur provider costs; review dashboard retention and call limits.

**Access and revocation:** The browser requests `POST /api/v1/elevenlabs/conversation-token` with an empty body or `{}`, its application cookie, a valid CSRF header, and Origin. The server validates its configured private agent/version, workflow, tool inventory, application MCP synchronization, and enabled skill references when skill synchronization is enabled before returning transient `conversationToken`, `conversationId`, `mode`, and `assistantName`. It never accepts a browser-selected agent or returns the API key. Responses are non-cacheable; do not store or log connection tokens. Invalid application authentication, CSRF/Origin, request limits, incompatible configuration, changed mode, and provider errors fail visibly without switching to a local mode.

The client ends calls on logout, navigation, disconnect, or detected authentication loss. Hosted calls recheck application authentication every 30 seconds and on foreground return. Server-side deactivation immediately denies further access, but the Go server cannot forcibly end an existing direct provider connection owned by an uncooperative browser. Browser timer suspension can delay detection; token expiry does not establish that an active call ends.

**Upgrade/rollback:** The additive migrations preserve existing modes, credentials, voices, and documents. Migration `000010` adds both synchronization policies with `NOT NULL DEFAULT TRUE`, including existing and zero-row configurations. Deploy backend and frontend together, then explicitly select the desired hosted policies. To roll back, select the previous mode before restoring older software. Keep the additive columns and expanded constraint; do not run a destructive down migration or copy credentials into scripts. Older binaries ignore the policy columns and return to always-enabled application synchronization: do not roll back while relying on disabled hosted synchronization unless that behavior is acceptable. Selecting cascaded ElevenLabs again requires its own configured default-language voice.

For microphone permission, connection, or playback failures, follow the visible error and explicitly retry. A new start never silently falls back to another mode.

**SDK media safety:** The pinned React/client SDK uses a tested, install-time patch to mute microphone tracks before publication and apply speaker volume before attachment. `npm ci` applies the patch fail-closed and copies the SDK worklet to a same-origin static asset; Docker includes both steps. Review and retest `web/patches/` before upgrading these dependencies. Do not replace this with a late React mute update, which can briefly transmit audio during connection.

### Restart Local STT and TTS

If voice transcription fails while using the local AI stack, restart only the STT and TTS services from the repository root. The local AI Compose file must be included:

```bash
docker compose -f docker-compose.yml -f docker-compose.local-ai.yml up -d --force-recreate stt tts
docker compose -f docker-compose.yml -f docker-compose.local-ai.yml ps
```

This recreates STT and TTS without stopping the web app, backend, or database. If either service is not running, inspect its logs:

```bash
docker compose -f docker-compose.yml -f docker-compose.local-ai.yml logs --tail=100 stt tts
```