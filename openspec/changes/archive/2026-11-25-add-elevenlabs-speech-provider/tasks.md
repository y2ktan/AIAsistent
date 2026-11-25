# Tasks

## 1. Provider contract and persistence

- [x] 1.2 Inspect the requested `npx -y @elevenlabs/cli@latest onboard init` workflow and its proposed effects; if safe and applicable, run it without exposing credentials, review its output and resulting changes, and document any blocked or intentionally omitted steps.
- [x] 1.3 Add repeat-safe migrations and store fields for the paired provider, separate encrypted ElevenLabs key/preview, models, and per-language ElevenLabs voice IDs; verify fresh initialization, upgrade with existing settings, repeated migration execution, and preservation of local voices and keys.
- [x] 1.4 Implement provider-aware language settings and transactional validation for configuration, default-language changes, and provider switching; test both switch directions, missing default voices, unvoiced non-default languages, concurrent updates, and preservation of inactive provider values.

## 2. Admin API and discovery

- [x] 2.1 Extend the admin config and language APIs to configure inactive ElevenLabs settings, select one provider for both services, encrypt the separate credential, expose only its final five characters in a masked preview, reject non-empty keys of five characters or fewer, preserve omitted keys, and snapshot settings for new sessions; verify with API tests covering masking, short keys, key preservation, authorization, audit redaction, invalid activation, decryption failure, and old-client compatibility.
- [x] 2.2 Implement ElevenLabs-aware voice discovery with stable IDs and display names and manual-entry fallback, without sending credentials to the browser; test successful discovery, unavailable provider, bad credentials, redaction, and unchanged local discovery.

## 3. Cascaded speech integration

- [x] 3.1 Add ElevenLabs batch transcription using the documented request and response fields, audio format, supported wake keyterms, and server-side credential; test language mapping, wake-gate inputs, unsupported optional fields, sanitized errors, cancellation, and no local fallback.
- [x] 3.2 Add ElevenLabs streamed synthesis using the selected language's provider-specific voice and model; test correct request and key, packet framing and timing with representative streaming fixtures, malformed audio, cancellation, and sanitized failures.
- [x] 3.3 Connect provider selection to session creation and the existing cascaded turn path without changing realtime, LLM, RAG, tools, wake gating, or language fallback; test provider switching, existing-session snapshots, interruptions, and local-provider regression.

## 4. Admin UI and documentation

- [x] 4.1 Add the cascaded-only paired provider selector, separate write-only key with a masked five-character preview, model settings, and provider-specific voice editing with discovery and manual fallback; verify UI tests cover key entry and masking, inactive setup, validation errors, provider switching, retained values, and cloud-data/cost disclosure.
- [x] 4.2 Document configuration, credential rotation, model defaults and verified language limitations, cloud data and charges, voice mapping, error behavior, and reverting to the existing provider; verify examples contain no credentials and match implemented UI and API.

## 5. End-to-end verification

- [x] 5.1 Run the relevant Go and web test suites, applicable build/lint checks, and `openspec validate add-elevenlabs-speech-provider --strict`; record any environment blockers without claiming a pass.
- [ ] 5.2 With explicit authorization and a replacement key supplied privately, verify live local and ElevenLabs spoken turns, language/voice switching, interruption, a provider failure, and switching back; check that ongoing sessions retain their starting provider and that no secrets appear in responses or logs. Record any unmet manual listening checks separately.
