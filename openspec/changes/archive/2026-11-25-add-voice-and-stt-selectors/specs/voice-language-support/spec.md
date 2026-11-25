# Spec Delta

## MODIFIED Requirements

### Requirement: Per-language voice assignment
The system SHALL let administrators assign a speech voice to each supported language from discovered provider voices when available—falling back to manual entry otherwise—and SHALL treat a supported language with no assigned voice as understood but not spoken.

#### Scenario: Administrator assigns a voice to a language
- **WHEN** an administrator sets a voice for a supported language and saves
- **THEN** sessions started afterwards speak replies in that language with that voice

#### Scenario: Administrator selects a voice from discovered provider voices
- **WHEN** the configured text-to-speech provider exposes available voices and an administrator opens `/admin`
- **THEN** the Voice languages card offers a dropdown populated with those voices (including an empty option for no voice)

#### Scenario: Voice input falls back when provider does not list voices
- **WHEN** the configured text-to-speech provider does not expose a voice list or is unreachable
- **THEN** `/admin` falls back to a text input so the administrator can still enter or clear a voice name

#### Scenario: Language saved without a voice
- **WHEN** an administrator adds a language and leaves its voice empty
- **THEN** the language is saved and the settings view indicates that the assistant will answer that language in the default language
