# Proposal

## Why

Administrators currently depend on the configured OpenAI-compatible speech services and cannot choose ElevenLabs for speech recognition and synthesis. A selectable cloud speech provider offers another latency, quality, and language-coverage tradeoff without replacing the existing language model.

## What Changes

- Add one administrator-controlled speech provider selection for cascaded sessions: the existing local/OpenAI-compatible speech services or ElevenLabs for both STT and TTS. Keep the existing provider as the default and preserve its configuration when switching.
- Add an admin setting for a separate, encrypted, write-only ElevenLabs API key, showing only its last five characters after saving, and provider-specific speech model and language-voice settings. Apply changes to new sessions and audit configuration updates without recording secrets.
- Use balanced ElevenLabs model defaults, with `scribe_v3` for file-based transcription and `eleven_flash_v2_5` for speech synthesis as initial choices; verify current availability, pricing, and language support during implementation. Do not promise Cantonese support without verification.
- Route audio through the chosen provider while preserving wake gating, language handling, interruptions, and the existing LLM, RAG, and tool flows. Surface provider failures safely without automatic fallback.
- Expose provider-aware voice discovery and settings on the admin page; protect existing local voices from being mistaken for ElevenLabs voice IDs.

Out of scope: switching the LLM, an ElevenLabs end-to-end conversational agent, adding realtime-mode audio integration, and automatic provider fallback.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `admin-governance-console`: Manage a paired speech-provider selection and independent ElevenLabs credentials and models.
- `voice-language-support`: Preserve separate language-to-voice assignments for each speech provider.
- `voice-vision-ai-agent`: Route cascaded STT and TTS through the selected provider while preserving conversation behavior and reporting failures.

## Impact

- Go server speech adapters, admin configuration handlers, discovery endpoints, session configuration, and tests.
- PostgreSQL migrations for provider selection, credentials, models, and per-provider voices; existing installations must retain local behavior.
- Admin UI, voice settings, tests, and setup documentation. ElevenLabs requests transmit speech and generated text externally and may incur usage charges.
- The ElevenLabs onboarding command requested during exploration is not run during planning; implementation should assess its effects before using it rather than committing generated credentials or unreviewed changes.
