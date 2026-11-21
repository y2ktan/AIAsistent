# Design

## Context

`useVoiceAgentRTC.ts` currently captures `navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true } })`, attaches the raw microphone `MediaStreamTrack` directly to `RTCPeerConnection`, and drives `vad.speech_start` / `vad.speech_end` from a fixed `SPEECH_RMS = 0.03` threshold on an unfiltered `AnalyserNode`. Meanwhile, `/api/v1/admin/agent-config` is restricted to `admin` users, and `RealtimeAdapter.sendSessionUpdate()` does not configure upstream input audio noise reduction. See `proposal.md` for motivation and `specs/` for behavioral requirements.

## Goals / Non-Goals

**Goals:**
- Isolate near-field speaker voice and attenuate ambient environmental noise before WebRTC transmission without adding external cloud dependencies, CDN downloads, or CGO native libraries.
- Keep `vad.speech_start` (including barge-in) and `vad.speech_end` accurate in noisy rooms where ambient RMS exceeds `0.03`.
- Default `audioFocusEnabled` to `true` across existing and new installations while letting administrators toggle it on `/admin`.

**Non-Goals:**
- Server-side Opus decoding/re-encoding DSP in Go (processing at the browser capture source avoids server CPU overhead and benefits both `cascaded` and `realtime` modes).
- Push-to-talk or text-chat fallback UI (scoped out to Method 1 only).

## Decisions

### 1. Default-Enabled Persistence & Pointer-Safe Config Updates
- **Choice**: Add `audio_focus_enabled BOOLEAN NOT NULL DEFAULT TRUE` in `db/migrations/000004_audio_focus.up.sql` (reversible in `000004_audio_focus.down.sql`), set `AudioFocusEnabled: true` in `PGAdminStore.GetAgentConfig` when no `'default'` row exists yet, and decode `audioFocusEnabled *bool` in `HandleUpdateAgentConfig` (defaulting to the existing stored value or `true` when omitted from legacy JSON payloads).
- **Rationale**: In Go, decoding into a plain `bool` defaults omitted fields to `false`, which would silently flip a default-`true` setting off for callers not sending the new field. Recording `"audioFocusEnabled"` in the `admin.agent_config.update` audit metadata provides full governance visibility.

### 2. Authenticated Session Audio Config Endpoint (`GET /api/v1/rtc/config`)
- **Choice**: Expose `GET /api/v1/rtc/config` behind `RequireAuth` returning `{ "audioFocusEnabled": bool }`. `startCall()` fetches this setting (falling back to `true` if unreachable) before invoking `getUserMedia`.
- **Rationale**: `/api/v1/admin/agent-config` is `admin`-only and contains sensitive configuration metadata, while `POST /api/v1/rtc/token` is rate-limited (`sigLimiter`) and mints single-use signaling tokens that should not be consumed before microphone permission succeeds.

### 3. Browser Speaker-Focus Web Audio Pipeline (`web/src/lib/audio-focus.ts`)
- **Choice**: Encapsulate constraint building, Web Audio DSP graph wiring, and adaptive VAD in `web/src/lib/audio-focus.ts`:
  - **Capture constraints**: When `audioFocusEnabled` is `true`, request `echoCancellation: true`, `noiseSuppression: true`, `autoGainControl: true`, plus `voiceIsolation: true` (W3C/Chromium OS-level voice isolation hint; ignored safely by browsers that do not recognize it). When `false`, omit `voiceIsolation` and disable extra filtering.
  - **Web Audio voice-isolation graph** (when `audioFocusEnabled` is `true` and `createMediaStreamDestination` is available):
    1. `BiquadFilterNode` (`highpass`, `95 Hz`, `Q = 0.707`) to strip low-frequency HVAC, fan, traffic, and desk rumble.
    2. `BiquadFilterNode` (`peaking`, `1800 Hz`, `+2.5 dB`, `Q = 1.0`) to emphasize primary human vocal formants.
    3. `BiquadFilterNode` (`lowpass`, `3800 Hz`, `Q = 0.707`) to attenuate high-frequency hiss, keyboard clicks, and clatter.
    4. `DynamicsCompressorNode` (`threshold: -24 dB`, `knee: 18`, `ratio: 3`, `attack: 0.005s`, `release: 0.15s`) to stabilize near-field speaker volume.
    5. `GainNode` acting as a smooth downward expander / noise gate driven by the adaptive VAD state (`gain = 1.0` when speech or onset is detected, smoothly ramping via `setTargetAtTime` to `0.08` during non-speech ambient segments) feeding `MediaStreamAudioDestinationNode` for `pc.addTrack(...)`.
  - **Lifecycle & mute safety**: `toggleMic` updates `.enabled` on both the raw microphone track(s) and the processed destination track(s); `stopCall` stops both streams and disconnects graph nodes.
- **Alternatives considered**: Bundling a third-party neural WASM binary was deferred in favor of native OS `voiceIsolation` + Web Audio speech-band filtering + downward expansion + adaptive VAD, which requires zero new dependencies, works in every modern browser, and has near-zero latency.

### 4. Adaptive Noise-Floor VAD with Hysteresis
- **Choice**: Feed the speech-band-filtered signal (before the downward gate `GainNode`) into `micAnalyser` and run a pure `AdaptiveVAD` tracker every `50ms`:
  - Track a rolling `noiseFloor` (initialized at `0.015`, clamped to `[0.008, 0.12]`). When not speaking and current RMS is near/below the speech trigger, update `noiseFloor` with asymmetric exponential smoothing (faster downward adaptation `alpha = 0.15` when noise drops, slower upward adaptation `alpha = 0.04` during steady ambience so speech onsets do not inflate the floor).
  - Compute onset threshold `startThreshold = max(0.03, noiseFloor * 2.5 + 0.015)` and sustain threshold `holdThreshold = max(0.022, noiseFloor * 1.8 + 0.008)`, keeping `START_TICKS = 3` (`150ms`) and `HANGOVER_MS = 900`.
  - When `audioFocusEnabled` is `false`, preserve the fixed `0.03` threshold behavior.
- **Rationale**: In a room with steady `0.05` RMS noise, `noiseFloor` converges to `~0.05`, raising `startThreshold` to `~0.14` (near-field speaker level) and `holdThreshold` to `~0.098`. Ambient noise never false-triggers `vad.speech_start`, and once the user stops speaking and energy returns to `0.05`, `vad.speech_end` fires reliably after `900ms`.

### 5. Realtime Provider Near-Field Noise Reduction
- **Choice**: Add `AudioFocusEnabled bool` to `agent.AgentConfig`. In `RealtimeAdapter.sendSessionUpdate()`, set `"input_audio_noise_reduction": map[string]any{"type": "near_field"}` on the `session` object when `AudioFocusEnabled` is `true`, and omit the field when `false`.
- **Rationale**: Complements client-side Audio Focus with model-native near-field speaker isolation on OpenAI-compatible Realtime endpoints while keeping `AudioFocusEnabled = false` clean.

## Risks / Trade-offs

- [Aggressive noise-gate attenuation could clip soft initial consonants] → Drive the `AnalyserNode` ahead of the gate `GainNode`, open gain immediately (`attack ~8ms`) as soon as a single frame crosses `holdThreshold`, and keep a non-zero floor (`0.08` rather than `0.0`) so natural acoustic transitions are preserved for Whisper/Realtime STT.
- [Sudden loud transient noise could briefly spike RMS] → `START_TICKS = 3` (`150ms` sustained speech-band energy) filters brief clicks/knocks from triggering `vad.speech_start` or barge-in.
- [Older/mocked test environments may lack `BiquadFilterNode` or `createMediaStreamDestination`] → Gracefully fall back to the raw `MediaStream` if any Web Audio node factory is missing so calls never fail to connect.

## Migration Plan

1. Apply `db/migrations/000004_audio_focus.up.sql`, which adds `audio_focus_enabled BOOLEAN NOT NULL DEFAULT TRUE` to `agent_configs` without downtime; existing `'default'` rows automatically receive `TRUE`.
2. Rollback via `000004_audio_focus.down.sql` (`ALTER TABLE agent_configs DROP COLUMN IF EXISTS audio_focus_enabled;`).
