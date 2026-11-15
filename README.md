# Aegis Voice — Secure WebRTC RAG Voice & Vision AI Assistant

Enterprise-grade, low-latency multimodal voice-and-vision AI assistant built with **Next.js (App Router)**, **Go (`pion/webrtc/v4`)**, and **PostgreSQL 16 + `pgvector`**.

## Architecture

- **`web/` (Next.js + TypeScript + Tailwind CSS)**:
  - `/` — Immersive 60fps WebGL/Three.js audio-reactive voice & camera assistant with live PiP self-view, kinetic captions, and expandable RAG citations.
  - `/login` — Credential login and one-time initial administrator bootstrap flow.
  - `/admin` — Role-gated governance console for Users & Roles, RAG Knowledge Base, AI Agent Configuration (write-only encrypted secrets), MCP Servers, and Security Audit Logs.
- **`server/` (Go + Pion WebRTC)**:
  - DTLS-SRTP Opus audio & VP8/H.264 video tracks + ordered `events` SCTP data channel.
  - Dual-mode `SessionAgent` (`realtime` WebSocket bridge & `cascaded` OpenAI-compatible STT → Vision/RAG/MCP LLM → Streaming TTS).
  - AES-256-GCM secret vault, Argon2id password hashing, rotating `HttpOnly; Secure; SameSite=Strict` sessions, CSRF + Origin validation, SSRF-safe outbound dialer, and token-bucket rate limiters.
- **`db/migrations/` (PostgreSQL + `pgvector`)**:
  - HNSW cosine similarity index (`vector_cosine_ops`) for RAG document chunks, plus `users`, `sessions`, `signaling_tokens`, `documents`, `agent_configs`, `mcp_servers`, and `audit_logs`.

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

## One-Time Admin Bootstrap Flow

1. Generate strong random secrets for `MASTER_ENCRYPTION_KEY` (`openssl rand -hex 32`) and `ADMIN_BOOTSTRAP_TOKEN` (`openssl rand -base64 24`).
2. Start the stack with `docker compose up --build`.
3. Navigate to `http://localhost:3000/login`. When zero users exist in the database, the UI presents the **Initial Admin Bootstrap** form.
4. Enter the `ADMIN_BOOTSTRAP_TOKEN`, your admin email, and a strong password (minimum 12 characters).
5. Once the first admin is created, the bootstrap endpoint permanently disables itself (`409 Conflict` on any subsequent attempt). No default credentials are ever seeded.

## Pion WebRTC UDP Mux Port Settings

To avoid exposing thousands of ephemeral UDP ports in Docker, Pion's `SettingEngine` is configured with a single-port `ICEMux` (`ICE_UDP_MUX_PORT=8443` by default) and TCP ICE fallback on the same port (`8443/udp` and `8443/tcp` in `docker-compose.yml`). When deploying behind a cloud firewall or NAT, open `8443/udp` and `8443/tcp` and set `ICE_PUBLIC_IP` to your host's reachable public IP.

## Voice Languages

Configure supported spoken languages in **`/admin` → AI Agent → Voice languages**:

- **Initial languages**: On a fresh install, English (default), Mandarin Chinese, Cantonese, and Malay are listed.
- **Default language rules**: Exactly one language must be marked as the default, and the default language must have an assigned voice. You cannot remove the default language until you select another default.
- **Per-language voices & fallback**: In `cascaded` mode, replies are spoken using the voice assigned to the reply's language. If a user speaks an unsupported language—or a supported language with no assigned voice—the assistant answers in the default language and notifies the user once per session that it cannot reply in that language.
- **Cantonese & Malay voices**: The bundled local Kokoro TTS engine provides English and Mandarin voices (`af_heart`, `zf_xiaoxiao`) out of the box. To speak Cantonese or Malay instead of falling back to the default language, configure a TTS engine that provides voices for them and assign those voice names in `/admin`.

