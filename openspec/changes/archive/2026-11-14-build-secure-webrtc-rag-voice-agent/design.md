# Design

## Context

This greenfield repository initializes an enterprise conversational voice-and-vision AI platform (see `proposal.md` - Why). Because **Pion** (`github.com/pion/webrtc/v4`) is a Go-native WebRTC stack and the interactive UI is built on **Next.js**, the system is structured as a containerized multi-service monorepo orchestrated via Docker Compose with **PostgreSQL + `pgvector`** as the single durable data and vector store. Every architectural boundary incorporates the 2026 A10 Networks Web Application Security Best Practices (Zero-Trust request verification, injection & SSRF defenses, rotating sessions, BOLA/rate-limiting controls, encryption in transit/at rest, and AI prompt/leakage guardrails).

```text
+-----------------------------------------------------------------------+
|                        Browser Client (Next.js UI)                    |
|  /login (Auth)   |   / (3D Audio Orb + PiP Cam)   |   /admin (RBAC)   |
+--------+-------------------------+--------------------------+---------+
         | HTTPS + Cookie + CSRF   | DTLS-SRTP (Opus + VP8)   | HTTPS
         |                         | + SCTP DataChannel       |
         v                         |                          v
+----------------------------------+------------------------------------+
|                   Next.js App Router BFF (`web/`)                     |
|  * Edge/Node Middleware (CSP, HSTS, Permissions-Policy, Route Guard)  |
|  * Server Actions / API Proxy -> Go Backend over internal network     |
+----------------------------------+------------------------------------+
                                   |
                                   v
+-----------------------------------------------------------------------+
|                    Go Backend & Pion Media Engine (`server/`)         |
|  +--------------------+  +---------------------+  +----------------+  |
|  | Auth, RBAC, CSRF & |  | Pion PeerConnection |  | RAG Ingestion  |  |
|  | Rate-Limit Layer   |  | Opus <-> PCM, VP8   |  | & pgvector     |  |
|  +--------------------+  +----------+----------+  +--------+-------+  |
|                                     |                      |          |
|                          +----------v----------------------+-------+  |
|                          | Dual-Mode Voice/Vision Orchestrator     |  |
|                          | * Mode A: Realtime Multimodal WebSocket |  |
|                          | * Mode B: Cascaded STT -> VLM -> TTS    |  |
|                          | * MCP Client Host + Prompt Guardrails   |  |
|                          +--------------------+--------------------+  |
+-----------------------------------------------|-----------------------+
                                                |
                                                v
+-----------------------------------------------------------------------+
|                    PostgreSQL 16 + `pgvector` (`db/`)                 |
|  users | sessions | signaling_tokens | documents | document_chunks    |
|  agent_configs (AES-256-GCM) | mcp_servers | audit_logs               |
+-----------------------------------------------------------------------+
```

## Goals / Non-Goals

**Goals:**
- Achieve sub-second conversational turn responsiveness and instant user barge-in over Pion WebRTC audio/video tracks and SCTP data channels.
- Support seamless runtime switching between native Realtime Multimodal WebSocket providers and modular OpenAI-compatible STT/Vision-LLM/TTS endpoints without redeploying containers.
- Enforce defense-in-depth security across HTTP, WebRTC signaling, database queries, RAG untrusted text, outbound AI/MCP connections, and stored credentials.
- Deliver a high-craft "wow" spatial voice UI that runs smoothly at 60fps with graceful accessibility fallbacks.

**Non-Goals:**
- Multi-party conference rooms (each Pion peer connection serves a 1:1 authenticated user-to-assistant session).
- Public anonymous guest access (all conversational and administrative capabilities require authenticated sessions).
- Arbitrary local shell/stdio command execution from untrusted admin input for MCP servers inside production containers; MCP connections use Streamable HTTP / SSE transports with SSRF validation (or pre-allowlisted container binaries only).

## Decisions

### 1. Two-Runtime Monorepo (`web/` Next.js + `server/` Go Pion + `db/` Postgres/pgvector)
- **Choice**: Keep Next.js focused on UI rendering, security headers, and BFF proxying while Go owns auth/session persistence, Pion WebRTC media handling, RAG indexing/search, MCP client execution, and AI provider orchestration.
- **Rationale**: Pion runs natively in Go with low-overhead goroutines per media track, avoiding fragile CGO/FFI bridges inside Node.js and keeping encrypted secrets (`MASTER_ENCRYPTION_KEY`) strictly inside the Go backend process.
- **Alternatives considered**: Running Go purely as a media sidecar while Next.js owns Postgres and AI calls — rejected because splitting RAG, MCP, and encryption logic across TypeScript and Go duplicates security validation and increases media-to-LLM latency.

### 2. WebRTC Signaling & Media Pipeline with Pion (`pion/webrtc/v4`)
- **Choice**:
  1. Authenticated client requests a 60-second, single-use `signaling_token` (`POST /api/v1/rtc/token`).
  2. Client opens signaling (`POST /api/v1/rtc/offer` + trickle ICE or authenticated WebSocket) presenting the token.
  3. Pion negotiates Opus audio (`48kHz`), VP8/H.264 video, and an ordered SCTP `events` data channel (`state`, `transcript.delta`, `citation`, `tool.call`, `barge_in`, `media.toggle`).
  4. Video frames are decoded/sampled on-demand (latest keyframe/frame at ~0.5–1 fps while camera toggle is enabled) and attached as vision context when answering user utterances, keeping bandwidth and token cost bounded.
- **Alternatives considered**: Streaming raw PCM/JPEG over WebSockets — rejected because WebRTC provides built-in jitter buffering, echo-cancellation compatibility, congestion control, and mandatory DTLS-SRTP encryption.

### 3. Dual-Mode AI Orchestrator (`Orchestrator` Interface)
- **Choice**: Define a unified Go `SessionAgent` interface with two concrete adapters selected per session from active `agent_configs`:
  - **`RealtimeAdapter`**: Bridges Pion Opus/PCM audio frames and sampled camera frames into a bidirectional Realtime WebSocket API (e.g., OpenAI Realtime format), registering `search_knowledge_base` (RAG) and enabled MCP tools as callable functions.
  - **`CascadedAdapter`**: Uses server-side VAD + barge-in cancellation to segment user utterances -> OpenAI-compatible `/audio/transcriptions` (STT) -> `pgvector` semantic retrieval -> `/chat/completions` (streaming with vision image frame + MCP tool loop) -> sentence-chunked `/audio/speech` (TTS) encoded back to Opus RTP packets.
- **Alternatives considered**: Supporting only Realtime WebSockets — rejected because many enterprise/self-hosted OpenAI-compatible endpoints only expose standard REST/streaming STT, VLM, and TTS routes.

### 4. PostgreSQL + `pgvector` RAG Pipeline & Prompt-Injection Isolation
- **Choice**:
  - Validate uploads via MIME + magic-byte inspection (`%PDF-`, ZIP/DOCX header, valid UTF-8 for `.md`/`.txt`) and enforce strict byte caps (default 20 MB).
  - Extract text in Go, split into overlapping semantic token/character windows (~800 tokens, 120-token overlap), compute embeddings via the configured embedding endpoint, and index in `document_chunks` using an HNSW cosine index (`vector_cosine_ops`).
  - Wrap retrieved passages and MCP tool outputs inside explicit untrusted delimiters (`<untrusted_knowledge_context>...</untrusted_knowledge_context>`) with system-prompt instructions forbidding instruction-following from retrieved data, and scan outputs to block secret/canary leakage.
- **Alternatives considered**: Separate vector database (Qdrant/Milvus) — rejected to minimize operational surface area since `pgvector` HNSW provides transactional consistency with `documents` deletion/reindexing inside PostgreSQL.

### 5. A10-Aligned Security Architecture
- **Choice**:
  - **Authentication & Sessions**: Argon2id (`m=64MB, t=3, p=2`) password hashes; opaque 256-bit random session tokens stored as SHA-256 hashes in `sessions` with idle (30m) and absolute (12h) expiry, token rotation on login, and `__Host-` / `HttpOnly; Secure; SameSite=Strict` cookies.
  - **CSRF & BOLA**: Synchronizer/double-submit CSRF header (`X-CSRF-Token`) + strict `Origin` verification on every mutating cookie endpoint; centralized middleware enforcing `user` vs `admin` role and owner scoping.
  - **Secrets & SSRF**: `MASTER_ENCRYPTION_KEY` (32-byte key from environment/Docker secret) encrypts provider API keys and MCP headers using AES-256-GCM with unique 96-bit nonces per write; custom `net.Dialer` `Control` hook blocks RFC1918, loopback (`127.0.0.0/8`, `::1`), link-local (`169.254.0.0/16`), and metadata IPs at connection time (defeating DNS rebinding) unless `ALLOW_PRIVATE_AI_ENDPOINTS=true` is explicitly configured for local development.
  - **Rate Limiting**: Token-bucket limiters keyed by client IP and user ID across login (`5 req/min`), signaling (`10 req/min`), document upload (`10 req/hour`), and general APIs.

### 6. "Wow" Conversational UI Architecture
- **Choice**: Dark obsidian spatial aesthetic (`#05070F` deep canvas with bioluminescent cyan/violet/amber state palettes) centered on a 60fps WebGL shader / Three.js reactive liquid-plasma orb driven by Web Audio `AnalyserNode` FFT bins from both local mic and remote Pion audio tracks, paired with glassmorphic floating dock controls, rounded PiP camera self-view with active vision pulse, and live kinetic captions with expandable RAG source cards.
- **Alternatives considered**: Standard chat-bubble layout with a mic button — rejected because a speech-first talking assistant requires peripheral visual state feedback ("listening", "retrieving", "thinking", "speaking") without forcing users to read dense text walls.

## Risks / Trade-offs

- **[Opus C-library build complexity in Go containers]** -> Use multi-stage Debian-slim/Alpine Docker builds with pinned `libopus` / pure-Go Opus framing where supported, verified in CI/container health checks so developers run `docker compose up --build` reproducibly.
- **[UDP port mapping friction in Docker Desktop / restrictive NATs]** -> Configure Pion `SettingEngine` with an explicit narrow UDP port mux (`ICE_UDP_MUX_PORT`, e.g., single muxed UDP port `8443` + TCP ICE fallback) so Docker Compose only exposes a single deterministic media port instead of thousands of ephemeral UDP ports.
- **[High visual load on low-power devices]** -> Automatically downgrade shader complexity when frame times exceed 20ms or when `prefers-reduced-motion: reduce` is active.
- **[Embedding dimension mismatch when Admin changes embedding models]** -> Record embedding dimension metadata on `agent_configs` and prompt/trigger a safe background reindex when switching model dimensions.

## Migration Plan

1. Provision environment variables via `.env` (generated from `.env.example` with fresh random `MASTER_ENCRYPTION_KEY` and one-time `ADMIN_BOOTSTRAP_TOKEN` — never committed).
2. Start stack via `docker compose up --build` (`postgres` with `pgvector/pgvector:pg16`, `server` running SQL migrations on startup, and `web` Next.js production server).
3. Complete one-time bootstrap admin creation using `ADMIN_BOOTSTRAP_TOKEN` (immediately disabled once the first admin exists), configure AI provider credentials/MCP in `/admin`, ingest initial RAG documents, and verify voice/camera conversation on `/`.
4. **Rollback**: Tear down containers (`docker compose down`) or roll back SQL migrations cleanly via numbered down-migrations while keeping encrypted database volumes intact.
