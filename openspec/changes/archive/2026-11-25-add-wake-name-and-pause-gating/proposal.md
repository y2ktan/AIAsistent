# Proposal

## Why

In environments with background conversations and frequent thinking-out-loud pauses, raw voice-activity spikes currently interrupt assistant speech mid-sentence, background remarks cancel in-flight turns before transcription checks who was addressed, and the `900ms` VAD silence cutoff fragments paused thoughts into disconnected, contextless turns or premature filler replies. Adding strict wake-name prefix gating (supporting English and Chinese names/aliases), name-verified barge-in, silent thinking-pause accumulation, direct-question gating, and an `/admin` assistant name configuration ensures the assistant never interrupts or replies to background speech or unfinished thoughts.

## What Changes

- **Wake-name prefix gating (English & Chinese)**: Ignore all speech unless it explicitly begins with the configured assistant name or a configured wake alias (optionally preceded by a short English or Chinese greeting such as `"Hey"`, `"Hi"`, `"你好"`, or `"嗨"`), enforcing word boundaries for Latin names and rune-prefix / Simplified-Traditional normalization for Chinese names, and biasing STT transcription with the configured wake vocabulary.
- **BREAKING (behavior change to barge-in)** **Name-verified speech interruption**: Remove raw acoustic `vad.speech_start` barge-in and pre-STT turn cancellation so background speakers never cut off assistant audio playback or cancel an in-flight `thinking`/`retrieving` turn; interrupt assistant speech only after transcription confirms a new utterance explicitly begins with a configured wake name or alias (while preserving manual mute and end-call controls).
- **Pause-tolerant VAD & armed silent thought buffer (`~15s`)**: Increase client `AdaptiveVAD` end-of-speech hold from `900ms` to `2500ms` for natural mid-sentence pauses, and maintain a server-side armed wake buffer (`15s` idle expiry) once wake-addressed so paused thinking-out-loud clauses accumulate silently without emitting filler acknowledgements (`"got it"`, `"mm-hmm"`, `"okay"`).
- **Direct-question response gate**: Respond only when the wake-addressed (or armed-buffered) utterance asks a direct question—combining multilingual question detection (English, Mandarin, Cantonese, Malay) with an LLM `[WAIT_FOR_QUESTION]` sentinel fallback when STT omits question punctuation, and applying equivalent server-gated turn detection (`create_response: false`, `interrupt_response: false`) in `realtime` mode.
- **Configurable Assistant Name & Wake Aliases on `/admin`**: Add `assistantName` (defaulting to `"Aegis"`) and `wakeAliases` (e.g., `["小智"]`) to AI Agent configuration in PostgreSQL, `/api/v1/admin/agent-config` (with audit logging), and `/admin` (**AI Agent** tab), and expose safe `assistantName` in `GET /api/v1/rtc/config` so the home page displays the configured identity.

## Capabilities

### New Capabilities

<!-- No new capabilities are introduced. -->

### Modified Capabilities

- `admin-governance-console`: Allow administrators on `/admin` to configure a primary `assistantName` (default `"Aegis"`) and optional `wakeAliases` (supporting English and Chinese), record updates in the audit log, and return `assistantName` safely via `GET /api/v1/rtc/config`.
- `webrtc-media-streaming`: Extend VAD speech-end hangover for thinking pauses and replace raw acoustic `vad.speech_start` barge-in with wake-name-verified speech interruption so background talkers do not cut off outbound assistant audio.
- `voice-vision-ai-agent`: Enforce wake-name prefix gating (English/Chinese + optional short greeting), silent `15s` armed thinking-pause accumulation with zero filler acknowledgements, and direct-question-only response gating across both `cascaded` and `realtime` modes.
- `immersive-voice-ui`: Display the configured `assistantName` in the home page header and assistant transcript label, and omit ignored background utterances from live conversation captions.

## Impact

- `db/migrations/000005_assistant_wake_name.up.sql` & `000005_assistant_wake_name.down.sql`: Add `assistant_name TEXT NOT NULL DEFAULT 'Aegis'` and `wake_aliases TEXT[] NOT NULL DEFAULT '{}'` to `agent_configs`.
- `server/internal/api/` (`store.go`, `pg_store.go`, `handlers.go`, `api_test.go`): Persist and validate `AssistantName` and `WakeAliases`, include wake-name metadata in `admin.agent_config.update` audit entries, expose `assistantName` in `GET /api/v1/rtc/config`, and remove raw `OnSpeechStart` acoustic barge-in in `attachSessionAgent`.
- `server/internal/agent/` (`types.go`, `wake.go`, `guardrails.go`, `cascaded.go`, `realtime.go`, `agent_test.go`): Implement Unicode/CJK wake-name & greeting matching, multilingual direct-question detection + `[WAIT_FOR_QUESTION]` sentinel, `15s` armed utterance accumulation buffer, STT `prompt` wake-vocabulary hinting, no-filler system guardrails, name-verified barge-in, and `realtime` transcription-gated turn handling.
- `web/src/lib/audio-focus.ts`, `web/src/hooks/useVoiceAgentRTC.ts`, `web/src/app/page.tsx`, `web/src/app/admin/page.tsx`, and `web/src/__tests__/`: Pause-tolerant `AdaptiveVAD` (`2500ms` hangover), dynamic `assistantName` rendering on `/`, Assistant Name + Wake Aliases inputs on `/admin`, and unit tests.
