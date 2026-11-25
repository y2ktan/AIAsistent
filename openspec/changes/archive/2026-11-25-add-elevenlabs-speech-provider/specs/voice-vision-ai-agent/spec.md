# Spec Delta

## ADDED Requirements

### Requirement: ElevenLabs cascaded speech processing
When ElevenLabs is the selected cascaded speech provider, the system SHALL use it for transcription and speech synthesis while preserving the existing wake gate, reply-language rules, LLM processing, and interruption behavior.

#### Scenario: ElevenLabs speech turn
- **WHEN** a user asks a wake-addressed question in an ElevenLabs-backed cascaded session
- **THEN** the system transcribes the audio through ElevenLabs, processes the question with the configured LLM, and streams the reply using the active provider's reply-language voice

#### Scenario: Provider changes during a session
- **WHEN** an administrator changes the speech provider while a session is active
- **THEN** that session retains its starting provider and new sessions use the newly saved provider

#### Scenario: ElevenLabs speech request fails
- **WHEN** ElevenLabs rejects a speech request or is unreachable
- **THEN** the system reports a safe speech error without exposing secrets and does not automatically retry with the local provider

#### Scenario: Spoken reply is interrupted
- **WHEN** a valid interruption occurs while ElevenLabs audio is streaming
- **THEN** the current speech output stops and the session retains the existing interruption semantics
