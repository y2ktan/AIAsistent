# Proposal

## Why

Users need a low-latency, multimodal voice-and-vision AI assistant capable of answering spoken questions accurately using organization-specific documents and external tools without compromising enterprise security. Combining a visually immersive Next.js experience, Pion WebRTC real-time media transport, PostgreSQL `pgvector` RAG grounding, MCP extensibility, and defense-in-depth web/AI security controls delivers an end-to-end, production-grade conversational platform from day one.

## What Changes

- Introduce a containerized multi-service stack orchestrated via Docker Compose comprising a **Next.js (App Router)** web application, a **Go backend service powered by Pion WebRTC**, and **PostgreSQL with `pgvector`**.
- Add a secure **Login Page (`/login`)** with Argon2id credential verification, brute-force rate limiting, CSRF protection, and rotating `HttpOnly`/`Secure`/`SameSite=Strict` session cookies enforcing Zero-Trust RBAC (`user` and `admin` roles).
- Add an immersive **Talking Assistant Home Page (`/`)** featuring a "wow" reactive 3D/WebGL audio orb and waveform HUD, independent microphone/speaker and camera toggles, live PiP camera preview, barge-in speech interruption, and real-time streaming transcripts with RAG citations.
- Implement **Pion WebRTC audio and video streaming** in Go over encrypted DTLS-SRTP media tracks and SCTP data channels, gated by short-lived, single-use signaling tokens.
- Implement a **Dual-Mode Voice & Vision AI Agent** supporting both native **Realtime Multimodal WebSocket APIs** and a **modular OpenAI-compatible STT -> Vision/Chat LLM -> Streaming TTS pipeline**, dynamically driven by Admin-configured endpoints, encrypted API keys, and **MCP (Model Context Protocol)** tool servers.
- Implement a **RAG Document Knowledge Engine** backed by PostgreSQL `pgvector` to validate, parse, chunk, embed, index, and semantically retrieve ingested documents with source attribution and prompt-injection isolation.
- Add a role-gated **Admin Console (`/admin`)** to manage users and roles, ingest/reindex/delete RAG documents, configure AI agent keys (write-only, AES-256-GCM encrypted at rest), endpoints, and MCP connections, and inspect security audit logs.
- Enforce **A10 Networks Web Application Security Best Practices (2026)** across every layer: SQL/command/prompt injection prevention, SSRF protections, broken authentication/session mitigation, BOLA & token-bucket API rate limiting, encryption at rest and in transit, secret redaction, hardened HTTP security headers (`CSP`, `HSTS`, `Permissions-Policy`), and non-root container configurations without default credentials.

## Capabilities

### New Capabilities
- `auth-and-session-security`: User login, role-based access control (`user`/`admin`), rotating secure sessions, session revocation, and ephemeral WebRTC signaling authentication.
- `webrtc-media-streaming`: Browser-to-Go Pion WebRTC peer connection lifecycle, DTLS-SRTP Opus audio and video track streaming, speaker/camera state synchronization, barge-in handling, and real-time event data channels.
- `voice-vision-ai-agent`: Dual-mode AI speech-and-vision orchestration (Realtime Multimodal WebSocket and OpenAI-compatible STT/Vision-LLM/TTS fallback), MCP client tool integration, and AI prompt/output guardrails.
- `rag-document-knowledge`: Secure document upload validation, text extraction, chunking, embedding generation, `pgvector` similarity search, citation grounding, and document lifecycle management.
- `immersive-voice-ui`: High-craft "wow" Next.js conversational interface with reactive WebGL/canvas voice visualization, glassmorphic spatial controls, camera preview, live captions, and grounded source cards.
- `admin-governance-console`: Administrative portal for user lifecycle & RBAC management, RAG document ingestion workflows, encrypted write-only AI key/endpoint/MCP configuration, and audit visibility.
- `web-app-security-hardening`: Defense-in-depth web, API, and AI security controls aligned with A10 Networks Web Application Security Best Practices covering injection defenses, API rate limiting, BOLA prevention, cryptographic secret storage, and hardened runtime headers/containers.

### Modified Capabilities

## Impact

- **Frontend (`web/`)**: New Next.js TypeScript application (App Router, Tailwind CSS, WebGL/Three.js or custom shader audio visualizer, WebRTC browser client, strict middleware & CSP headers).
- **Backend (`server/`)**: New Go service integrating `github.com/pion/webrtc/v4`, HTTP/WebSocket API router, PostgreSQL/`pgvector` repository layer (`pgx` parameterized queries), dual-mode AI provider adapters, MCP client host, RAG ingestion workers, and AES-256-GCM secret vault.
- **Database & Infrastructure (`docker-compose.yml`, `db/migrations/`)**: PostgreSQL 16+ with `pgvector` extension, schema migrations for `users`, `sessions`, `documents`, `document_chunks`, `agent_configs`, `mcp_servers`, and `audit_logs`, plus hardened multi-stage Dockerfiles.
