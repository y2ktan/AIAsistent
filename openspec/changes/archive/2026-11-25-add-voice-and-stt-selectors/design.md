# Design: TTS Voice and STT Model Selectors in Admin Console

## Context

In the current local AI setup:
- Kokoro TTS (`http://tts:8880/v1`) exposes `GET /v1/audio/voices` returning `{ "voices": ["af_alloy", ...], "default_voice": "af_heart" }` (72 voices).
- faster-whisper STT (`http://stt:8000/v1`) exposes `GET /v1/models` returning `{ "object": "list", "data": [{ "id": "Systran/faster-whisper-small", ... }, ...] }` (704 models).
- Both services run on the internal Docker network, so the browser cannot query them directly. Meanwhile, cloud providers such as OpenAI's TTS endpoint do not expose `/v1/audio/voices` (returning 404).
- On `/admin`, `web/src/app/admin/page.tsx` currently renders a plain text `<input>` for each language's voice and has no input field for `cfg.sttModel` despite `sttModel` already being stored in `agent_configs` and included in `GET`/`PUT /api/v1/admin/agent-config`.

## Goals / Non-Goals

**Goals:**
- Proxy TTS voice discovery (`GET /api/v1/admin/tts-voices`) and STT model discovery (`GET /api/v1/admin/stt-models`) through the Go server behind existing admin auth/CSRF/role middleware and SSRF policy.
- Degrade gracefully: return `{ voices: [], supported: false }` or `{ models: [], supported: false }` (HTTP 200) when the configured provider returns non-2xx, is unconfigured, or is unreachable, so `/admin` never breaks for OpenAI or offline providers.
- Render `<select>` dropdowns in `/admin` when `supported: true` (preserving any custom saved value as an option or switching to text input when `supported: false` / custom mode).
- Group or prioritize Kokoro voices matching the language row's prefix (e.g., `z*` for Mandarin Chinese, `a*`/`b*` for English, `j*` for Japanese) while still allowing any voice to be chosen.

**Non-Goals:**
- Validating `VoiceLanguage.Voice` against `/audio/voices` on `PUT /api/v1/admin/voice-languages` (would break saving settings when TTS is temporarily stopped or when using custom/cloud voice IDs).
- Filtering out non-Whisper models server-side in a way that hides custom STT models; instead, prioritize `Systran/faster-whisper-*` at the top and cap/include the rest cleanly.

## Decisions

### 1. Backend discovery endpoints
Add two read-only admin endpoints in `server/internal/api/handlers.go` and `router.go`:
- `GET /api/v1/admin/tts-voices`: Reads the stored `agent_configs` record, resolves `TTSBaseURL` (falling back to `LLMBaseURL` if empty, matching `cascaded.go`), validates it with `security.SSRFPolicy{AllowPrivate: s.allowPrivate}`, sends a short-timeout (5s) `GET <base>/audio/voices` with the decrypted API key if set, and returns `{ "voices": [...], "supported": true }` on 200 with a non-empty `voices` array, or `{ "voices": [], "supported": false }` otherwise.
- `GET /api/v1/admin/stt-models`: Same pattern against `STTBaseURL` (falling back to `LLMBaseURL`) calling `GET <base>/models`, extracting `data[].id`, prioritizing `Systran/faster-whisper-*` / `*whisper*` IDs first, and returning `{ "models": [...], "supported": true }` or `{ "models": [], "supported": false }`.

### 2. Frontend dropdown with automatic text-input fallback
- **Voice languages card:** Fetch `/api/v1/admin/tts-voices` alongside `voice-languages` in `loadAll()`. When `supported` is true and `voices.length > 0`, render a `<select>` with:
  - `""` → `"None (fallback to default)"`
  - Recommended voices for that language's script/code prefix (e.g. `zh` → `z*`, `en` → `a*`/`b*`, `ja` → `j*`, `es` → `e*`, `fr` → `f*`, `hi` → `h*`, `it` → `i*`, `pt` → `p*`) in an `<optgroup label="Recommended">`
  - Remaining voices in `<optgroup label="All voices">`
  - If the currently saved `lang.voice` is non-empty and not in `voices`, include it as an explicit option so existing values are never lost.
  - When `supported` is false, render the existing text `<input>`.
- **AI Agent config form (cascaded mode):** Add an `STT Model` field bound to `cfg.sttModel`. Fetch `/api/v1/admin/stt-models` in `loadAll()`. When `supported` is true and `models.length > 0`, render a `<select>` (including `cfg.sttModel` if not in the list); otherwise render a text `<input>`.
- Re-fetch `/api/v1/admin/tts-voices` and `/api/v1/admin/stt-models` after `Save Encrypted Config` succeeds so changing `TTS Base URL` or `STT Base URL` refreshes the available lists immediately.

## Risks / Trade-offs

- **Large STT model list (704 items):** Faster-whisper's `/v1/models` returns 704 HuggingFace entries. Placing `Systran/faster-whisper-*` models first in a `"Recommended (faster-whisper)"` `<optgroup>` keeps the common choices immediately visible at the top of the `<select>`.
- **Provider compatibility:** Non-Kokoro TTS providers (such as OpenAI) return 404 on `/v1/audio/voices`; returning `{ supported: false }` keeps the UI functional with text input fallback.
