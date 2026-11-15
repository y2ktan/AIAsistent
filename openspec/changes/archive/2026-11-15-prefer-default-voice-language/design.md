# Design: Prefer Configured Default Voice Language

## Context

In `server/internal/agent/language.go`, `ResolveReplyLanguage` already falls back to `settings.Find(settings.DefaultCode)` when an utterance is undetermined and there is no previous reply in the session (`LastReplyCode == ""`). However, `DetectUtteranceLanguage` currently hardcodes language preferences when resolving script-contradicting or missing STT labels:
- For `domScript == "han"`, it checks `"zh"` then `"yue"` regardless of `settings.DefaultCode`.
- For `domScript == "latin"`, it checks `"en"` then the first Latin language in `settings.Languages` regardless of `settings.DefaultCode`.

Additionally, `server/internal/agent/realtime.go` builds a `LANGUAGE POLICY` instruction that tells the model to reply in the default language when a spoken language is unsupported, but does not state that undetermined utterances also fall back to the default language.

## Goals / Non-Goals

**Goals:**
- Ensure `DetectUtteranceLanguage` prefers `settings.DefaultCode` whenever `domScript` is `"han"` or `"latin"` and the STT label is missing or contradicts `domScript` (provided `DefaultCode` belongs to that script family).
- Keep the existing session continuity rule for mid-conversation undetermined utterances: use `LastReplyCode` if present, otherwise fall back to `settings.DefaultCode`.
- Update `realtime.go` `LANGUAGE POLICY` instructions so undetermined utterances explicitly fall back to the configured default language.
- Add unit tests verifying non-English default languages (e.g., Mandarin Chinese, Cantonese, Malay).

**Non-Goals:**
- Changing the admin UI or database schema (`voice_languages` already stores `is_default` and returns `defaultLanguage`).
- Changing STT model behavior or passing a forced `language` parameter to Whisper (which would prevent automatic multi-language detection when the user switches languages).

## Decisions

### 1. Same-script disambiguation order in `DetectUtteranceLanguage`
When `domScript != ""` and the STT label is absent or does not match `domScript`:
1. Check `settings.DefaultCode` first: if its catalog entry has `Script == domScript`, return it immediately.
2. Otherwise, for `"han"`, fall back to `"zh"`, then `"yue"`, then `"zh"`.
3. Otherwise, for `"latin"`, fall back to `"en"`, then the first configured language with `Script == "latin"`, then `"en"`.

**Rationale:** `GuardVoiceForReply` already follows this pattern (checking `settings.DefaultCode` before iterating `settings.Languages`). Applying the same rule in `DetectUtteranceLanguage` makes detection and voice selection consistent when an admin sets a non-English default such as Mandarin Chinese, Cantonese, or Malay.

### 2. Undetermined utterance behavior
When `wordUnits < 2` or no language can be determined (`detectedCode == "" && detectedName == ""`), `ResolveReplyLanguage` continues to return `LastReplyCode` if a prior voiced reply exists in the session, and otherwise returns `defaultLang` (`settings.DefaultCode`).

**Rationale:** Preserves natural mid-conversation acknowledgements ("OK", "好") in the active language while ensuring any initial or unseeded undetermined utterance uses the configured default language (e.g., Mandarin Chinese when `DefaultCode == "zh"`).

### 3. Realtime policy prompt
Update `langPolicy` in `server/internal/agent/realtime.go` to state that if the user's language cannot be determined, the assistant should reply in its previous reply language or in `<defaultLanguage>` if there is no previous reply.

## Risks / Trade-offs

- **LLM compliance on short inputs:** Small local models (such as `llama3:latest`) may still occasionally answer in English if the user prompt is in English and the system instruction is short; `GuardVoiceForReply` ensures TTS voice still matches the actual reply script.
- **Same-script STT mislabeling:** If STT returns `"en"` for a Malay utterance, `entry.Script == "latin"` matches `domScript == "latin"`, so STT's label is trusted unless STT returns a non-Latin label or empty label. This is unchanged and documented as an existing limitation of script-based heuristics.
