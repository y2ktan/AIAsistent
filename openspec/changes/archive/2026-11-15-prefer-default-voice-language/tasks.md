# Tasks

## 1. Utterance language detection with default-language preference

- [x] 1.1 Update `DetectUtteranceLanguage` in `server/internal/agent/language.go` so that when `domScript` is `"han"` or `"latin"` and the speech-to-text label is missing or contradicts `domScript`, it checks `settings.DefaultCode` first and returns it if its catalog entry matches `domScript`, before falling back to the existing script-family order.
- [x] 1.2 Add table-driven unit tests in `server/internal/agent/language_test.go` covering:
  - Mandarin Chinese (`zh`) as `DefaultCode`: undetermined initial utterance resolves to `zh`; undetermined mid-session utterance after an English reply keeps `en`; script-contradicting Han utterance resolves to `zh`.
  - Cantonese (`yue`) as `DefaultCode` (with a voice): Han utterance with missing or English STT label resolves to `yue` instead of `zh`.
  - Malay (`ms`) as `DefaultCode` (with a voice): Latin utterance with missing or Chinese STT label resolves to `ms` instead of `en`.

## 2. Realtime policy prompt

- [x] 2.1 Update `langPolicy` in `server/internal/agent/realtime.go` to state that if the user's language cannot be determined, the assistant should reply in its previous reply language or in the configured default language if there is no previous reply.
- [x] 2.2 Update `TestRealtimeAdapterWebSocketBridge` in `server/internal/agent/agent_test.go` to verify the updated undetermined-language instruction is included in `session.update` (including when `DefaultCode` is Mandarin Chinese).

## 3. Verification

- [x] 3.1 Run `go test -race ./...` and `go vet ./...` in `golang:1.25-bookworm` and confirm all Go tests pass.
- [x] 3.2 Run `openspec validate "prefer-default-voice-language" --strict` and confirm validation passes.
