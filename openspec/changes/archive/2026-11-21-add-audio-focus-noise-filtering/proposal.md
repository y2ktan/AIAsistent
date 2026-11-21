# Proposal

## Why

In noisy environments (offices, cafés, transit, or background chatter/HVAC hum), raw microphone audio passes environmental noise into the WebRTC stream and holds the client's fixed `0.03` RMS voice-activity detector open—causing false assistant barge-in interruptions and preventing utterance end-of-speech (`vad.speech_end`) from firing. Adding **Audio Focus** (enabled by default, and configurable on `/admin`) isolates the near-field speaker's voice and filters ambient environment noise so conversations work reliably in noisy spaces while still allowing administrators to disable processing when needed.

## What Changes

- Add an **Audio Focus** (`audioFocusEnabled`) setting to AI Agent configuration, **enabled by default (`true`)** across database migrations, backend defaults, and the `/admin` governance console.
- Allow administrators on `/admin` (**AI Agent** tab) to enable or disable **Audio Focus** and persist changes with existing security and audit logging.
- Expose a minimal authenticated session audio configuration (`GET /api/v1/rtc/config` returning `{ "audioFocusEnabled": boolean }`) so both `user` and `admin` voice sessions apply the active setting without exposing admin-only provider configuration.
- When **Audio Focus** is enabled, process inbound browser microphone capture through a speaker voice-isolation pipeline (requesting OS/browser `voiceIsolation` where supported, speech-band filtering, dynamic speech compression, and adaptive downward noise gating into a processed outbound WebRTC `MediaStreamTrack`) paired with an adaptive noise-floor VAD so background noise neither false-triggers barge-in nor blocks turn completion.
- When **Audio Focus** is enabled in `realtime` pipeline mode, configure near-field input noise reduction (`input_audio_noise_reduction: { "type": "near_field" }`) on the upstream Realtime session; omit/disable it when **Audio Focus** is turned off.
- When **Audio Focus** is disabled on `/admin`, bypass the Audio Focus processing chain and use standard microphone capture.

## Capabilities

### New Capabilities

<!-- No new capabilities are introduced. -->

### Modified Capabilities

- `admin-governance-console`: Allow administrators on `/admin` to enable or disable **Audio Focus** (defaulting to enabled) as part of AI Agent configuration and record updates in the audit log.
- `webrtc-media-streaming`: Isolate near-field speaker voice, filter ambient environmental noise on the outbound microphone WebRTC track, and adapt voice-activity/barge-in thresholds above ambient noise when **Audio Focus** is enabled, bypassing processing when disabled.
- `voice-vision-ai-agent`: Configure near-field input audio noise reduction on `realtime` multimodal sessions according to the active **Audio Focus** setting.

## Impact

- `db/migrations/000004_audio_focus.up.sql` & `000004_audio_focus.down.sql`: Add `audio_focus_enabled BOOLEAN NOT NULL DEFAULT TRUE` to `agent_configs`.
- `server/internal/api/` (`store.go`, `pg_store.go`, `handlers.go`, `router.go`, `api_test.go`): Persist `AudioFocusEnabled` (default `true`), accept `*bool` updates on `PUT /api/v1/admin/agent-config`, expose `GET /api/v1/rtc/config` for authenticated callers, and pass `AudioFocusEnabled` into `agent.AgentConfig`.
- `server/internal/agent/` (`types.go`, `realtime.go`, `agent_test.go`): Propagate `AudioFocusEnabled` into Realtime `session.update` (`input_audio_noise_reduction`).
- `web/src/lib/audio-focus.ts` (new helper module), `web/src/hooks/useVoiceAgentRTC.ts`, `web/src/app/admin/page.tsx`, and `web/src/__tests__/`: Web Audio speaker-focus graph, adaptive noise-floor VAD state machine, `/admin` Audio Focus toggle (default enabled), and unit/UI tests.
