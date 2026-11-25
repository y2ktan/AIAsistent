# Proposal: Add TTS Voice and STT Model Selectors in Admin Console

## Why

In `/admin`, assigning a speech voice in the **Voice languages** card currently requires typing an exact voice ID (such as `af_heart` or `zf_xiaoxiao`) from memory, and the **AI Agent** configuration form has no visible selector for `STT Model` (`sttModel`), even though the backend stores and uses it. Both local services already expose discovery endpoints over the internal Docker network (`GET /v1/audio/voices` on Kokoro TTS returning 72 voices, and `GET /v1/models` on faster-whisper STT returning available Whisper models), which the browser cannot reach directly.

## What Changes

- Add two authenticated, SSRF-validated admin discovery endpoints on the Go server:
  - `GET /api/v1/admin/tts-voices`: queries `<TTSBaseURL>/audio/voices` and returns `{ voices: string[], supported: boolean }`, returning `supported: false` (rather than failing the page) when the configured TTS provider does not expose `/audio/voices` or is unreachable.
  - `GET /api/v1/admin/stt-models`: queries `<STTBaseURL>/models` (filtering/prioritizing Whisper models cleanly) and returns `{ models: string[], supported: boolean }`, returning `supported: false` when unavailable.
- Update the **Voice languages** card in `web/src/app/admin/page.tsx` so each language's voice field renders a `<select>` dropdown populated from `/api/v1/admin/tts-voices` (including a `"None (fallback to default)"` option and prioritizing voices matching the row's language prefix when available), and automatically falls back to a text input when `supported` is `false` or when a custom/unlisted voice is already saved.
- Add an **STT Model** field to the **AI Agent** cascaded configuration form in `web/src/app/admin/page.tsx` that renders a `<select>` dropdown populated from `/api/v1/admin/stt-models` (prioritizing `Systran/faster-whisper-*` and preserving any currently saved value), and automatically falls back to a text input when `supported` is `false`.

## Capabilities

### New Capabilities
<!-- None -->

### Modified Capabilities
- `voice-language-support`: Support selecting per-language TTS voices from discovered provider voices with automatic text-input fallback.
- `admin-governance-console`: Support selecting the STT model from discovered provider models with automatic text-input fallback.

## Impact

- `server/internal/api/router.go`, `server/internal/api/handlers.go`, `server/internal/api/api_test.go`: Add `GET /api/v1/admin/tts-voices` and `GET /api/v1/admin/stt-models` behind the existing `admin` middleware with SSRF validation and unit tests.
- `web/src/lib/voice-languages.ts`, `web/src/__tests__/voice-languages.test.ts`: Add pure helpers for grouping/filtering discovered Kokoro voices by language and building selector options.
- `web/src/app/admin/page.tsx`: Replace the raw voice text input with a dropdown + fallback, and add the STT Model dropdown + fallback to the AI Agent form.
