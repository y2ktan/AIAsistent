# Proposal: Prefer Configured Default Voice Language

## Why

When an administrator sets a non-English default language in `/admin` (for example, Mandarin Chinese as the default with `zf_xiaoxiao`), language detection in `DetectUtteranceLanguage` still hardcodes `"zh"` before `"yue"` for Han script and `"en"` before other Latin-script languages when speech-to-text returns a missing or contradicting language label. As a result, ambiguous same-script utterances or script-contradicting transcripts do not prefer the configured default language. In addition, the realtime language policy prompt does not explicitly state that undetermined utterances fall back to the configured default language.

## What Changes

- Update utterance language detection so that when the dominant script matches multiple configured languages and the speech-to-text label is missing or contradicts the script, the configured `defaultLanguage` is preferred first if it belongs to that script family (and has a voice), before falling back to other configured languages in that script.
- Clarify in the `voice-language-support` specification that:
  - When an utterance's language cannot be determined (such as a one-word acknowledgement or unclear audio), the assistant replies in its previous reply language in that session, or in the configured default language (for example, Mandarin Chinese when set as default) if there is no previous reply.
  - When script-based disambiguation is needed across languages sharing a script family, the configured default language is preferred when it matches that script.
- Update realtime `session.update` language policy instructions to state that undetermined utterances fall back to the configured default language.
- Add unit tests covering non-English default languages (such as Mandarin Chinese, Cantonese, and Malay) for undetermined utterances, script-contradicting labels, and script-tie cases.

## Capabilities

### New Capabilities
<!-- None -->

### Modified Capabilities
- `voice-language-support`: Clarify and strengthen preference for the configured default language during undetermined utterances and same-script disambiguation.

## Impact

- `server/internal/agent/language.go`: Update `DetectUtteranceLanguage` (and helper lookups) to prefer `settings.DefaultCode` when resolving same-script ambiguity.
- `server/internal/agent/language_test.go`: Add test cases with Mandarin Chinese (and other non-English languages) set as `DefaultCode`.
- `server/internal/agent/realtime.go` & `server/internal/agent/agent_test.go`: Include undetermined-utterance default fallback in realtime policy instructions and verify in tests.
- `openspec/specs/voice-language-support/spec.md`: Modified requirements via delta spec.
