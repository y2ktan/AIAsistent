# Tasks

Go is run via container from the project root when `go` is not installed on the host:

```bash
docker run --rm -v "$PWD/server":/src:ro -w /src -e GOCACHE=/tmp/gocache -e GOFLAGS=-buildvcs=false golang:1.25-bookworm go test -race ./...
```

Frontend tests run inside `web/` with `npm test`.

## 1. Database Migration & Admin Wake-Name Configuration API

- [x] 1.1 Create `db/migrations/000005_assistant_wake_name.up.sql` and `db/migrations/000005_assistant_wake_name.down.sql` adding `assistant_name TEXT NOT NULL DEFAULT 'Aegis'` and `wake_aliases TEXT[] NOT NULL DEFAULT '{}'` on `agent_configs` with repeat-safe rollback statements, and verify both SQL files exist and match existing migration conventions.
- [x] 1.2 Extend `AgentConfigRecord` (`server/internal/api/store.go`), `PGAdminStore` (`server/internal/api/pg_store.go`), `HandleUpdateAgentConfig` (pointer-safe `assistantName *string` and `wakeAliases *[]string` decoding, HTTP `400` validation on blank/oversized `assistantName`, trimmed/deduplicated English and Chinese `wakeAliases`, and `assistantName`/`wakeAliases` audit log metadata), `HandleRTCConfig` (`GET /api/v1/rtc/config` returning `assistantName` alongside `audioFocusEnabled`), and `attachSessionAgent`, and add unit tests in `server/internal/api/api_test.go` covering default `"Aegis"`, English/Chinese alias persistence, omitted-field preservation, blank-name rejection, audit logging, and `/api/v1/rtc/config`; verify `go test -race ./internal/api/...` passes.

## 2. Wake-Name Prefix Gate, Armed Pause Buffer & Dual-Mode Agent Orchestration

- [x] 2.1 Create `server/internal/agent/wake.go` implementing `MatchWakePrefix` (optional short English/Chinese greetings, Latin word-boundary enforcement, CJK non-space prefix matching, and Simplified/Traditional equivalence), `IsDirectQuestion` (English, Mandarin, Cantonese, and Malay question punctuation, interrogatives, auxiliary inversions, and A-not-A patterns), and stateful `WakeTurnGate` (`15s` armed silent accumulation window with idle expiry), with table-driven unit tests in `server/internal/agent/wake_test.go`; verify `go test -race ./internal/agent/...` passes.
- [x] 2.2 Update `AgentConfig` (`server/internal/agent/types.go`) and `BuildSystemPrompt` (`server/internal/agent/guardrails.go`) to interpolate the configured `AssistantName` (default `"Aegis"`), forbid conversational filler acknowledgements (`"got it"`, `"mm-hmm"`, `"okay"`), and instruct `[WAIT_FOR_QUESTION]` sentinel output when an utterance lacks a direct question, with unit tests in `server/internal/agent/agent_test.go`; verify `go test -race ./internal/agent/...` passes.
- [x] 2.3 Refactor `CascadedAdapter` (`server/internal/agent/cascaded.go`) and `attachSessionAgent` (`server/internal/api/handlers.go`) to remove `OnSpeechStart` raw acoustic barge-in, send wake-vocabulary `prompt` hints to STT (with `400` fallback), evaluate `WakeTurnGate` after transcription and before cancelling in-flight turns so unaddressed background speech never interrupts `retrieving`/`thinking`/`speaking` or emits user transcripts, trigger `BargeIn()` on wake-addressed speech while speaking, accumulate paused clauses silently until a direct question arrives (suppressing `[WAIT_FOR_QUESTION]` replies), and add/update tests in `server/internal/agent/agent_test.go` and `server/internal/api/api_test.go`; verify `go test -race ./internal/agent/... ./internal/api/...` passes.
- [x] 2.4 Update `RealtimeAdapter` (`server/internal/agent/realtime.go`) to configure `turn_detection` (`silence_duration_ms: 2500`, `create_response: false`, `interrupt_response: false`) and `input_audio_transcription` (`model: "whisper-1"`, wake `prompt`), remove unconditional `BargeIn()` on `input_audio_buffer.speech_started`, gate `conversation.item.input_audio_transcription.completed` through `WakeTurnGate` (deleting ignored items, triggering `BargeIn()` when wake-addressed while speaking, and sending `response.create` only on direct questions), and add WebSocket unit tests in `server/internal/agent/agent_test.go`; verify `go test -race ./internal/agent/...` passes.

## 3. Pause-Tolerant Browser VAD, Home Identity Display & Admin Settings UI

- [x] 3.1 Update `AdaptiveVAD` in `web/src/lib/audio-focus.ts` to hold active speech segments across pauses up to `2500ms` before emitting `vad.speech_end`, and update/add unit tests in `web/src/__tests__/audio-focus.test.ts` verifying pauses `< 2500ms` keep the segment open and silence `>= 2500ms` emits `"end"`; verify `npx vitest run src/__tests__/audio-focus.test.ts` in `web/` passes.
- [x] 3.2 Update `useVoiceAgentRTC` (`web/src/hooks/useVoiceAgentRTC.ts`) and `HomePage` (`web/src/app/page.tsx`) to expose and render `assistantName` from `GET /api/v1/rtc/config` (defaulting to `"Aegis"`) in the header and assistant transcript label, add **Assistant Name** and **Wake Aliases** fields (supporting English and Chinese) to the AI Agent form in `web/src/app/admin/page.tsx`, document wake-name configuration and voice gating behavior in `README.md`, and add tests in `web/src/__tests__/rtc-hook.test.ts` and `web/src/__tests__/admin-form-autofill.test.ts`; verify `npm test` in `web/` passes.

## 4. Cross-Cutting Integration Verification

- [x] 4.1 Run full Go backend checks (`go vet ./...` and `go test -race ./...` via the Go 1.25 container), full Next.js frontend checks (`npm test` and `npm run build` in `web/`), and `openspec validate "add-wake-name-and-pause-gating" --strict` to confirm zero regressions.

## Workflow follow-up

- Archive the change with `openspec-archive-change` after implementation and review are complete.
