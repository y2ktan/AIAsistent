# Design

## Context

Today, three mechanisms cause voice sessions to fail when background speakers are present or when the user pauses while thinking out loud:
1. `AdaptiveVAD` (`web/src/lib/audio-focus.ts`) triggers `vad.speech_start` after `150ms` of any speech-band energy and `vad.speech_end` after `900ms` of silence, chopping natural thinking pauses into separate audio clips.
2. `attachSessionAgent` (`server/internal/api/handlers.go`) calls `cascaded.BargeIn()` immediately on `vad.speech_start` whenever `cascaded.IsSpeaking()` is true, and `CascadedAdapter.ProcessTurn` (`server/internal/agent/cascaded.go`) calls `c.BargeIn()` unconditionally at entry *before* running STT—allowing any background remark to cancel active `thinking`, `retrieving`, or `speaking` turns.
3. `RealtimeAdapter` (`server/internal/agent/realtime.go`) cancels responses on `input_audio_buffer.speech_started` and leaves upstream `server_vad` free to auto-interrupt and auto-create replies before checking wake words or question intent.

Meanwhile, `agent_configs.name` in PostgreSQL is the singleton row key (`'default'`), while `"Aegis"` is hardcoded in `guardrails.go` and `web/src/app/page.tsx`. See `proposal.md` for motivation and `specs/` for behavioral contracts.

## Goals / Non-Goals

**Goals:**
- Support arbitrary administrator-configured English and Chinese primary names and wake aliases immediately without model training, native CGO libraries, or external cloud wake-word SDKs.
- Guarantee background speech never interrupts outbound TTS or cancels an in-flight LLM/RAG/MCP turn in either `cascaded` or `realtime` mode.
- Preserve multi-clause thinking-out-loud context across pauses up to `15s` with strictly zero backchannel/filler audio.

**Non-Goals:**
- Biometric speaker voiceprint enrollment (gating relies on explicit wake-name prefix + near-field Audio Focus rather than storing biometric voice embeddings).
- Custom client-side neural wake-word model training per name change.

## Decisions

### 1. Dedicated `assistant_name` and `wake_aliases` Columns with Pointer-Safe Updates
- **Choice**: Add `assistant_name TEXT NOT NULL DEFAULT 'Aegis'` and `wake_aliases TEXT[] NOT NULL DEFAULT '{}'` in `db/migrations/000005_assistant_wake_name.up.sql` (reversible in `.down.sql`), keeping `agent_configs.name = 'default'` untouched as the config row key. Decode `assistantName *string` and `wakeAliases *[]string` in `HandleUpdateAgentConfig`:
  - When `assistantName` is omitted (`nil`), preserve the existing stored value (or `"Aegis"`). When provided, trim whitespace and reject empty values (`""`) or overly long names (`> 64` runes) with HTTP `400`.
  - When `wakeAliases` is provided, trim entries, drop empties and case/script duplicates of `assistantName`, cap list size/length, and persist. Record `assistantName` and `wakeAliases` in `admin.agent_config.update` audit metadata.
  - Extend `GET /api/v1/rtc/config` to return `{ "audioFocusEnabled": bool, "assistantName": string }` so `/` displays the active name without exposing admin-only settings.
- **Alternatives considered**: Reusing `agent_configs.name` was rejected because `WHERE name = 'default'` keys the singleton configuration row across `PGAdminStore`. Client-side neural wake-word WASM (e.g., Porcupine/openWakeWord) was rejected because arbitrary admin-typed English and Chinese names cannot work without custom per-phrase model training.

### 2. Unicode & CJK Wake-Name Prefix Matcher + STT Prompt Hinting (`server/internal/agent/wake.go`)
- **Choice**: Implement a deterministic, pure Go wake-prefix parser `MatchWakePrefix(text, assistantName, wakeAliases)`:
  1. Strip leading whitespace and punctuation (ASCII and CJK punctuation such as `,`，`。`、`！`、`？`、`、`、`—`).
  2. Optionally strip one leading short greeting token in English (`hey`, `hi`, `hello`, `ok`, `okay`) or Chinese (`你好`, `您好`, `嗨`, `喂`, `嘿`, `哈喽`, `请问`) followed by optional whitespace/punctuation—only if a configured wake name immediately follows.
  3. Compare against `[assistantName, ...wakeAliases]` (sorted longest-rune-length first so `"Aegis Pro"` matches before `"Aegis"`), using case-folding for Latin text and a built-in Simplified/Traditional equivalence table for common CJK characters (e.g., `小智`, `雅典娜`, `贾维斯`/`賈維斯`, `助手`).
  4. **Script-aware boundary check**: If the matched wake phrase ends in an ASCII/Latin letter or digit, require end-of-string or a non-letter/digit boundary (so `"Alex"` matches `"Alex, what time is it?"` but rejects `"Alexander is here"`). If it ends in a Han/CJK rune, allow immediate continuation without spaces (so `"小智今天有什么安排？"` matches `"小智"`).
  5. **STT Vocabulary Hinting**: Pass `"<assistantName>, <wakeAliases...>"` via the `prompt` multipart field in `CascadedAdapter.transcribe()` (falling back cleanly if a provider rejects `prompt`, mirroring `verbose_json` fallback) and `input_audio_transcription.prompt` in `RealtimeAdapter.sendSessionUpdate()`.
- **Rationale**: Regex `\b` fails on Chinese text without spaces, while Whisper occasionally transcribes uncommon names as homophones unless primed via `prompt`.

### 3. Non-Destructive Pre-Gate Transcription & Name-Verified Barge-In
- **Choice**:
  - In `attachSessionAgent` (`handlers.go`), remove `cascaded.BargeIn()` from `ps.OnSpeechStart` (`ps.OnSpeechStart` only marks `speaking = true` on the frame capture ring buffer).
  - Refactor `CascadedAdapter.ProcessTurn` (`cascaded.go`) so incoming `oggAudio` is transcribed **before** mutating `c.turnCancel`, emitting `"thinking"` state, or interrupting active TTS:
    - **Unaddressed & window unarmed (`Ignore`)**: Drop silently and return `nil`—an in-flight `retrieving`, `thinking`, or `speaking` turn continues untouched, and no user transcript event is emitted.
    - **Wake-addressed while speaking**: Invoke `c.BargeIn()` immediately after STT confirms the wake prefix, halting outbound TTS and emitting `barge_in`.
- **Rationale**: Eliminates both failure modes reported by users: background talkers neither chop ongoing TTS (`vad.speech_start`) nor cancel in-flight LLM turns (`vad.speech_end`). Explicit client `barge_in` control events and mute/end-call controls remain intact.

### 4. Two-Tier Thinking-Pause Tolerance (`2500ms` VAD Hold + `15s` Armed Buffer) & Direct-Question Gate
- **Choice**:
  - **Tier 1 — Client `AdaptiveVAD` (`web/src/lib/audio-focus.ts`)**: Increase post-speech hangover from `900ms` to `2500ms` (`HANGOVER_MS = 2500`) so 1–2 second pauses while thinking out loud stay inside one continuous audio segment.
  - **Tier 2 — Server `WakeTurnGate` (`server/internal/agent/wake.go`)**: Track per-session armed state (`armedUntil`, `segments []string`, and an expiry timer of `15 * time.Second`):
    ```text
    [Transcript] --> MatchWakePrefix?
                       |-- NO + Unarmed  --> Ignore (silent, keep active turn)
                       |-- YES           --> Arm(15s), append segment (BargeIn if speaking)
                       |-- NO + Armed    --> Refresh(15s), append segment
                                                |
                                                v
                                     IsDirectQuestion(joined)?
                                       |-- NO  --> BufferWait (silent, 0 filler)
                                       |-- YES --> Flush joined text --> LLM/TTS
    ```
  - **Multilingual Direct-Question Detector + Sentinel Fallback**:
    - `IsDirectQuestion(text)` checks the trailing/clause structure of the accumulated utterance across supported languages: `?`/`？` punctuation; English interrogative words (`what`, `why`, `how`, `when`, `where`, `who`, `which`, `whose`, `whom`) and clause-leading auxiliary inversions (`is`, `are`, `can`, `could`, `would`, `should`, `will`, `do`, `does`, `did`, `have`, `has`, `may`); Chinese question particles/pronouns (`吗`, `嗎`, `呢`, `么`, `麼`, `什么`, `什麼`, `为什么`, `為什麼`, `怎么`, `怎麼`, `如何`, `哪里`, `哪裡`, `哪个`, `哪個`, `谁`, `誰`, `多少`, `几`, `幾`) and A-not-A forms (`是不是`, `能不能`, `可不可以`, `有没有`, `有沒有`, `好不好`, `行不行`, `对不对`, `對不對`); Cantonese (`咩`, `點解`, `邊度`, `邊個`, `幾多`, `係唔係`, `得唔得`); and Malay (`apa`, `apakah`, `bagaimana`, `kenapa`, `mengapa`, `bila`, `mana`, `siapa`, `berapa`, `bolehkah`, `adakah`, `-kah`).
    - **Guardrail & `[WAIT_FOR_QUESTION]` Sentinel**: `BuildSystemPrompt` injects dynamic assistant identity (`assistantName`) plus strict voice rules forbidding conversational fillers (`"got it"`, `"mm-hmm"`, `"okay"`, `"sure"`) and instructing the model to reply with exact token `[WAIT_FOR_QUESTION]` if an utterance still lacks a direct question. If `[WAIT_FOR_QUESTION]` is returned, the adapter suppresses transcript/TTS output and keeps the armed buffer active until `15s` expiry.

### 5. Realtime Multimodal Mode Parity
- **Choice**: In `RealtimeAdapter.sendSessionUpdate()` (`realtime.go`):
  - Configure `turn_detection` with `{"type": "server_vad", "silence_duration_ms": 2500, "create_response": false, "interrupt_response": false}` and `input_audio_transcription` with `{"model": "whisper-1", "prompt": wakePrompt}`.
  - Remove `r.BargeIn()` from `input_audio_buffer.speech_started`.
  - Listen for `conversation.item.input_audio_transcription.completed`, evaluate its `transcript` with `WakeTurnGate`, delete ignored items via `conversation.item.delete` so background chatter does not pollute Realtime context, call `r.BargeIn()` on wake-addressed speech while speaking, and send `response.create` only when `WakeTurnGate` transitions to `Respond`.
- **Rationale**: Prevents OpenAI-compatible Realtime servers from auto-interrupting or auto-replying with `"mm-hmm"` before wake-name and direct-question verification.

## Risks / Trade-offs

- [Background speaker talks during the 15-second armed window right after the user says the wake name] → Near-field **Audio Focus** (enabled by default) attenuates far-field voices before WebRTC capture; if no direct question arrives within `15s`, the buffer expires silently without speaking, and re-saying the wake name cleanly resets/continues the user's intent.
- [STT omits `?` on a genuine spoken question] → `IsDirectQuestion` matches lexical interrogatives, auxiliary inversions, Chinese particles/A-not-A patterns, and Malay/Cantonese question markers even when punctuation is missing.
- [2.5s VAD hangover adds ~1.6s wait after user finishes speaking compared to 0.9s] → Intentional trade-off explicitly requested for thinking-out-loud environments so mid-thought pauses are never cut off prematurely.
- [Self-hosted STT endpoint rejects `prompt` multipart field] → `CascadedAdapter.transcribe()` retries without `prompt` on `400 Bad Request` and memoizes the capability flag per session, matching the existing `verbose_json` fallback pattern.

## Migration Plan

1. Apply `db/migrations/000005_assistant_wake_name.up.sql` adding `assistant_name TEXT NOT NULL DEFAULT 'Aegis'` and `wake_aliases TEXT[] NOT NULL DEFAULT '{}'` to `agent_configs`; existing rows default seamlessly to `'Aegis'` and `'{}'`.
2. Roll back cleanly via `db/migrations/000005_assistant_wake_name.down.sql` dropping `wake_aliases` and `assistant_name` with `IF EXISTS`.
