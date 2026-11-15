# Tasks

## 1. Backend discovery endpoints

- [x] 1.1 Add `GET /api/v1/admin/tts-voices` in `server/internal/api/handlers.go` and `server/internal/api/router.go` behind the `admin` middleware. Validate the configured `TTSBaseURL` (falling back to `LLMBaseURL`) with `SSRFPolicy`, request `<base>/audio/voices` with a 5s timeout and decrypted API key if set, and return `{ "voices": [...], "supported": true }` on 200 with a non-empty list or `{ "voices": [], "supported": false }` when unsupported, unconfigured, or unreachable.
- [x] 1.2 Add `GET /api/v1/admin/stt-models` in `server/internal/api/handlers.go` and `server/internal/api/router.go` behind the `admin` middleware. Validate the configured `STTBaseURL` (falling back to `LLMBaseURL`) with `SSRFPolicy`, request `<base>/models` with a 5s timeout, prioritize `Systran/faster-whisper-*` / `*whisper*` model IDs first, and return `{ "models": [...], "supported": true }` or `{ "models": [], "supported": false }`.
- [x] 1.3 Add API unit tests in `server/internal/api/api_test.go` covering: Kokoro-style `/audio/voices` returning `supported: true`; 404/unreachable TTS returning `supported: false`; Whisper-style `/models` returning `supported: true` with `faster-whisper` models prioritized; and non-admin callers receiving `403`.

## 2. Frontend voice and STT model helpers

- [x] 2.1 Add pure helpers in `web/src/lib/voice-languages.ts` to partition discovered TTS voices into `recommended` (matching the language's Kokoro prefix such as `z` for `zh`, `a`/`b` for `en`, `j` for `ja`, `e` for `es`, `f` for `fr`, `h` for `hi`, `i` for `it`, `p` for `pt`) and `others`, and to ensure any existing custom saved voice or STT model is preserved in the option list.
- [x] 2.2 Add unit tests in `web/src/__tests__/voice-languages.test.ts` verifying voice partitioning for Mandarin (`zh`), English (`en`), and languages without Kokoro prefixes (such as `yue` / `ms`), plus preservation of unlisted saved values.

## 3. Admin UI selectors with fallback

- [x] 3.1 Update `web/src/app/admin/page.tsx` to fetch `/api/v1/admin/tts-voices` and `/api/v1/admin/stt-models` on load and after saving AI Agent config.
- [x] 3.2 Replace the free-text voice input in the **Voice languages** card with a `<select>` dropdown (showing `"None (fallback to default)"`, `Recommended`, and `All voices` groups) when `supported` is true, falling back to the text `<input>` when `supported` is false.
- [x] 3.3 Add the **STT Model** field in the **AI Agent** cascaded form in `web/src/app/admin/page.tsx`, rendering a `<select>` dropdown when `supported` is true and falling back to a text `<input>` when `supported` is false.

## 4. Verification

- [x] 4.1 Run `go vet ./...` and `go test -race ./...` in `golang:1.25-bookworm` and `npm test` + `npm run build` in `web/`.
- [x] 4.2 Run `openspec validate "add-voice-and-stt-selectors" --strict` and confirm validation passes.
