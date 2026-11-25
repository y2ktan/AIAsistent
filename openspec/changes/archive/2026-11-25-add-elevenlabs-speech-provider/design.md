# Design

## Context

See `proposal.md` for motivation. `CascadedAdapter` currently uses OpenAI-compatible `/audio/transcriptions` and `/audio/speech`, a shared provider key, and Ogg Opus packet parsing for WebRTC output. It transcribes before wake-name gating and uses STT language information to select the reply language and voice. `voice_languages` holds a shared list, a single voice per language, and a default; settings are loaded when a session starts. Admin config is saved as a singleton row, secrets use the server vault, and migrations are rerun on startup. The existing realtime adapter is independent and its microphone-input path is not wired end to end.

## Goals / Non-Goals

**Goals:**
- Keep the existing provider and session behavior intact until an administrator explicitly selects ElevenLabs.
- Isolate credentials and voice IDs by provider and preserve configurations across switches.
- Handle provider-specific audio formats and failures without weakening the existing wake, language, and interruption paths.

**Non-Goals:**
- Implement an ElevenLabs conversational agent, replace the existing LLM, or repair realtime mode.
- Claim an unverified language, account tier, voice, or model is supported.

## Decisions

### Provider and session boundary

Add a single `local` / `elevenlabs` speech-provider setting for cascaded sessions, defaulting to `local` for new and existing installations. Here `local` means the currently configured OpenAI-compatible speech services, which can themselves be remote. Select both speech services together; keep LLM and embedding configuration and credentials untouched. Resolve the provider, models, credential, and voices once when starting a session. Present the setting as cascaded-only and leave realtime unchanged.

Use narrowly scoped provider-specific transcription and synthesis methods behind the existing cascaded turn flow rather than replacing the conversational adapter. This retains wake gating, language resolution, RAG, tools, event delivery, and cancellation; it also avoids pretending ElevenLabs implements the existing API.

### Storage, editing, and validation

Add repeat-safe migrations for provider selection, a separate ElevenLabs ciphertext and masked preview, and ElevenLabs STT/TTS model defaults. The admin interface and API show only a masked preview containing the key's final five characters; reject non-empty keys of five characters or fewer so a preview cannot disclose an entire key. Use the existing vault, retaining a missing or blank submitted key as unchanged and never reusing the LLM key. Decryption failures must surface safely, not turn into an empty credential. Redact provider errors and audit metadata; do not log audio, transcript bodies, authorization headers, or secrets.

Keep the existing shared language list and default. Store a separate per-language ElevenLabs voice ID alongside the existing local voice, without copying local voice names into the ElevenLabs field. Existing rows and setup scripts remain local-compatible; provide explicit editing of both providers' voices so the ElevenLabs key and default voice can be configured before switching. Preserve the existing initial-settings behavior when the language table is empty. Voice discovery for ElevenLabs returns both display names and stable IDs, while manual entry remains possible if discovery fails. Ensure existing clients and discovery behavior for local providers remain compatible.

Validate activation of ElevenLabs against a saved or simultaneously submitted key and an ElevenLabs voice for the default language, and validate the local default voice before switching back. Language/default updates must also preserve the active provider's default-voice invariant. Persist related updates atomically or validate them transactionally to prevent inconsistent provider/voice combinations during concurrent saves. Unsupported or unvoiced non-default languages retain the existing fallback and once-per-session notice. Switching providers does not delete either mapping. Audit updates without key material.

An alternative is one shared voice field, but provider identifiers are not interchangeable and switching would destroy the previous configuration. An independent STT/TTS selector was rejected in favor of the agreed single switch.

### ElevenLabs requests and defaults

Use the official server-side HTTPS endpoints for batch transcription (`/v1/speech-to-text`) and streamed synthesis (`/v1/text-to-speech/{voice_id}/stream`), using the ElevenLabs credential only for ElevenLabs requests. Map the transcription's text and language fields to the existing turn processing. Where the selected model supports it, send wake names as documented keyterms; validate the actual API contract instead of sending the OpenAI `prompt` field. Disable non-speech event tags where supported so they do not corrupt wake matching.

Start with `scribe_v3` for file-based STT and `eleven_flash_v2_5` for TTS, separately editable by the administrator. Flash v2.5 is documented as fast and lower-cost; Scribe v3 is a current batch model, not a proven cheapest or fastest option. Before implementation locks in defaults, verify model availability, account access, pricing, languages, and a compatible API version. The documented Flash v2.5 language list does not establish Cantonese support, so the interface and documentation must not promise it. Preserve the existing language policy and report unsupported-provider errors rather than claiming improved coverage.

The authorized account check succeeded for a short TTS request, and model output confirmed `eleven_flash_v2_5` is available with text-to-speech support. Voice discovery returned stable IDs and names, including English and Mandarin voices compatible with Flash v2.5. The saved model response did not include a model named `scribe_v3` because this endpoint lists speech-synthesis models, so STT model availability remains governed by documented API validation at runtime. Public model documentation lists Cantonese for Scribe but not for Flash v2.5.

Request a documented Opus output format and verify the actual container/framing with an integration sample before connecting it to `NewOggPacketReader`. If the service returns a different framing, implement a correct parser or a supported conversion; arbitrary HTTP read chunks are not Opus packets. Deliver packet durations accurately and honor cancellation, backpressure, and the existing safe HTTP/SSRF policy. Keep timeout and response size limits bounded.

### Admin experience and operational safety

The admin UI shows the selected provider, write-only key, model defaults, provider-specific voices, and the fact that selecting ElevenLabs sends speech and generated text externally and incurs provider charges. Existing sessions do not switch mid-turn. A failed request emits a sanitized user-facing error and remains on the selected provider; no automatic local retry. Provider-aware discovery uses server-side credentials only and returns clear failure status without exposing the secret.

The requested onboarding initializer was run using the inspected CLI version 1.12.2 with telemetry disabled and no detected ElevenLabs key. It returned a browser authorization step; that step was not run because it would access and transmit credentials. No key or generated credentials were added to the project. The integration does not depend on onboarding-generated credentials.

## Risks / Trade-offs

- [Cloud processing and usage costs] -> Explicit admin disclosure; provider stays local until selected.
- [Model availability, pricing, and language coverage change] -> Verify current official documentation and account entitlement; allow model IDs to be adjusted; avoid unsupported-language claims.
- [Provider audio format is not compatible with WebRTC] -> Validate actual output framing and packet timing; do not treat network chunks as audio packets.
- [Shared language/default changes race provider activation] -> Transactional validation and tests for both update paths.
- [Existing voice behavior has two uncompleted manual listening checks] -> Preserve those checks and add provider-specific end-to-end listening; avoid claiming current playback is already proven.
- [Wake names or language labels differ between providers] -> Map documented keyterm and language response fields and test real representative utterances; retain safe fallback behavior.

## Migration Plan

1. Apply repeat-safe schema changes, default selection to `local`, retain existing keys and voices, and allow ElevenLabs configuration to be saved without activating it.
2. Deploy server and UI together; configure the new key and default-language voice, then explicitly select ElevenLabs. Existing sessions continue with their snapshots.
3. To roll back the selection, switch back to `local`; retain its settings and voices. To roll back software, restore previous server and UI versions while retaining additive columns for later redeployment. Any destructive down migration requires an explicit operational decision because it removes the new settings.
