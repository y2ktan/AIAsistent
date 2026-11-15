# Tasks

## 1. Monorepo Infrastructure & Database Schema

- [x] 1.1 Scaffold `web/` (Next.js App Router + TypeScript + Tailwind CSS), `server/` (Go module with Pion WebRTC & `pgx`), `db/migrations/`, `.env.example`, and `docker-compose.yml` (`pgvector/pgvector:pg16`, `server`, `web` running as non-root users) and verify `docker compose config` validates cleanly
- [x] 1.2 Create SQL up/down migrations for `users`, `sessions`, `signaling_tokens`, `documents`, `document_chunks` (`vector` + HNSW cosine index), `agent_configs`, `mcp_servers`, and `audit_logs`, and verify migrations apply cleanly against PostgreSQL `pgvector`
- [x] 1.3 Document environment variables, one-time admin bootstrap flow, and Pion UDP mux port settings in `README.md` and verify `.gitignore` excludes `.env` and secret artifacts

## 2. Core Security Primitives, Auth, Sessions & RBAC

- [x] 2.1 Implement Go AES-256-GCM secret vault, Argon2id password hasher, structured secret-redacting logger, and SSRF-safe `net.Dialer` blocking private/loopback/metadata IPs, and verify `go test ./internal/security/...` passes
- [x] 2.2 Implement Go authentication, rotating `HttpOnly`/`Secure`/`SameSite=Strict` session management, CSRF token/Origin middleware, per-IP/per-user token-bucket rate limiters, RBAC (`user`/`admin`) guards, and one-time bootstrap admin initialization, and verify `go test ./internal/auth/...` covers login, lockout/rate-limit, CSRF rejection, RBAC `403`, and mid-flight session revocation
- [x] 2.3 Implement single-use 60-second WebRTC `signaling_token` issuance and atomic consumption endpoints, and verify replay attempts return HTTP `401 Unauthorized` in unit/integration tests

## 3. RAG Document Ingestion & `pgvector` Retrieval Engine

- [x] 3.1 Implement upload validator enforcing file size limits and binary magic-byte inspection (`PDF`, `DOCX`, UTF-8 `MD`/`TXT`) plus text extractors and overlapping semantic chunker, and verify `go test ./internal/rag/...` rejects spoofed/oversized payloads and chunks valid documents accurately
- [x] 3.2 Implement embedding client, transactional `pgvector` chunk persistence, similarity search with score threshold and citation metadata, and atomic document delete/reindex handlers, and verify integration tests confirm deleted documents never appear in retrieval results

## 4. Pion WebRTC Media Server, Dual-Mode AI Agent & MCP Host

- [x] 4.1 Implement Pion WebRTC manager (`pion/webrtc/v4`) with single-port UDP mux + TCP fallback, DTLS-SRTP Opus audio inbound/outbound tracks, on-demand video keyframe/frame sampler, ordered `events` SCTP data channel, and peer cleanup on disconnect/session revocation, and verify `go test ./internal/rtc/...` establishes a peer connection and exchanges tracks/events
- [x] 4.2 Implement MCP client host supporting Streamable HTTP/SSE discovery, SSRF policy validation, bounded tool-call timeouts, and per-server tool allowlists, and verify `go test ./internal/mcp/...` enforces timeouts and blocks unpermitted tools/private IPs
- [x] 4.3 Implement Dual-Mode `SessionAgent` orchestrator (`RealtimeAdapter` WebSocket bridge + `CascadedAdapter` OpenAI-compatible STT -> Vision/RAG/MCP LLM -> Streaming TTS), barge-in cancellation, and `<untrusted_knowledge_context>` prompt-injection/secret-leakage guardrails, and verify `go test ./internal/agent/...` passes with mock Realtime and OpenAI-compatible servers

## 5. Admin Backend APIs & Audit Trail

- [x] 5.1 Implement `/api/v1/admin/*` endpoints for user CRUD/role updates (with last-admin lockout protection), RAG document upload/list/reindex/delete, write-only encrypted AI config (`****last4` masking + server-side health check), MCP server management, and append-only `audit_logs` queries, and verify `go test ./internal/api/...` validates schema enforcement, BOLA/RBAC, secret masking, and audit entries

## 6. Next.js Security Middleware, Login Page & Admin Console

- [x] 6.1 Configure Next.js middleware and response headers (`Content-Security-Policy`, `Strict-Transport-Security`, `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `Permissions-Policy` for `camera=(self), microphone=(self)`) plus route protection redirecting unauthenticated users to `/login` and non-admins away from `/admin`, and verify header/redirect tests pass
- [x] 6.2 Build `/login` (and one-time initial admin bootstrap screen when zero users exist) with refined obsidian glassmorphic UI, CSRF integration, and generic error handling, and verify login/logout flows work end-to-end
- [x] 6.3 Build `/admin` governance console with tabs for Users & Roles, RAG Knowledge Base (drag-and-drop upload, status badges, chunk counts, reindex/delete), AI Agent Configuration (mode selector, endpoint, masked write-only key input, connection test), MCP Servers (discovery & tool toggles), and Security Audit Logs, and verify Next.js typecheck and component tests pass

## 7. "Wow" Conversational Home Page & WebRTC Client

- [x] 7.1 Build browser Pion WebRTC client hook managing ephemeral signaling token exchange, microphone/speaker/camera track toggles without renegotiation drops, Web Audio `AnalyserNode` FFT extraction, barge-in playback flush, and `events` data channel parsing, and verify client unit tests pass
- [x] 7.2 Build `/` immersive talking-assistant experience featuring a 60fps WebGL/Three.js audio-reactive liquid-plasma orb (`listening`, `retrieving`, `thinking`, `speaking` palettes + `prefers-reduced-motion` fallback), floating glassmorphic media dock (call, mic, speaker, camera), live PiP camera self-view with vision pulse, kinetic streaming captions, and expandable RAG citation cards, and verify `npm run build` and `npm run lint` succeed in `web/`

## 8. End-to-End Stack & Security Verification

- [x] 8.1 Build and start the full Docker Compose stack (`postgres`, `server`, `web`) and verify container health checks, non-root runtime user execution, HTTP security headers, auth/RBAC enforcement, RAG ingestion, MCP configuration, and Pion WebRTC voice/camera signaling succeed end-to-end
