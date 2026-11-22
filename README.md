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
  - HNSW cosine similarity index (`vector_cosine_ops`) for RAG document chunks, plus `users`, `sessions`, `signaling_tokens`, `documents`, `agent_configs`, `mcp_servers`, `agent_skills`, and `audit_logs`.

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
- Scribe supports Cantonese, but `eleven_flash_v2_5` does not list Cantonese in its model language list. Leave Cantonese without an ElevenLabs voice or choose a verified compatible model/voice before assigning it.

## Issues and Solutions

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