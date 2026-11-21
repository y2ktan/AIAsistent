# Tasks

## 1. Database Migration & Backend Audio Focus Configuration

- [x] 1.1 Create `db/migrations/000004_audio_focus.up.sql` and `db/migrations/000004_audio_focus.down.sql` adding `audio_focus_enabled BOOLEAN NOT NULL DEFAULT TRUE` on `agent_configs` (and dropping it cleanly on rollback), and verify both SQL files are present and syntactically consistent with existing migrations.
- [x] 1.2 Extend `AgentConfigRecord` (`server/internal/api/store.go`), `PGAdminStore` (`server/internal/api/pg_store.go`), `HandleUpdateAgentConfig` (`*bool` decoding preserving default/existing `true` when omitted, plus `audioFocusEnabled` audit metadata), `GET /api/v1/rtc/config` (`server/internal/api/router.go` and `handlers.go`), and `attachSessionAgent`, and add unit tests in `server/internal/api/api_test.go` covering default `true`, toggling `false`/`true`, omitted-field preservation, audit logging, and authenticated `GET /api/v1/rtc/config`; verify `go test -race ./internal/api/...` in `server/` passes.
- [x] 1.3 Add `AudioFocusEnabled` to `agent.AgentConfig` (`server/internal/agent/types.go`) and update `RealtimeAdapter.sendSessionUpdate()` (`server/internal/agent/realtime.go`) to include `input_audio_noise_reduction: { "type": "near_field" }` when enabled and omit it when disabled, with unit tests in `server/internal/agent/agent_test.go`; verify `go test -race ./internal/agent/...` in `server/` passes.

## 2. Browser Audio Focus Pipeline, Adaptive VAD & Admin Console Toggle

- [x] 2.1 Create `web/src/lib/audio-focus.ts` with microphone constraint helpers (`voiceIsolation` when Audio Focus is enabled), the Web Audio speaker-focus graph (`highpass`, vocal `peaking`, `lowpass`, `DynamicsCompressorNode`, downward noise-gate `GainNode`, and `MediaStreamAudioDestinationNode` with raw-stream fallback), and the `AdaptiveVAD` rolling noise-floor detector, plus unit tests in `web/src/__tests__/audio-focus.test.ts` covering noisy-room non-triggering, near-field speech onset, and timely `vad.speech_end`; verify `npx vitest run src/__tests__/audio-focus.test.ts` in `web/` passes.
- [x] 2.2 Integrate `GET /api/v1/rtc/config` (defaulting to `true`), the Audio Focus audio graph, and `AdaptiveVAD` into `web/src/hooks/useVoiceAgentRTC.ts` (synchronizing mute and teardown across raw and processed tracks), add the **Audio Focus** enable/disable toggle (default `true`) in `web/src/app/admin/page.tsx`, document Audio Focus in `README.md`, and add admin/RTC tests in `web/src/__tests__/admin-form-autofill.test.ts` and `web/src/__tests__/rtc-hook.test.ts`; verify `npm test` in `web/` passes.

## 3. Cross-Cutting Integration Verification

- [x] 3.1 Run full Go backend checks (`go vet ./...` and `go test -race ./...` in `server/`), full Next.js frontend checks (`npm test` and `npm run build` in `web/`), and `openspec validate "add-audio-focus-noise-filtering" --strict` to confirm zero regressions.

## Workflow follow-up

- Archive the change with `openspec-archive-change` after implementation and review are complete.
