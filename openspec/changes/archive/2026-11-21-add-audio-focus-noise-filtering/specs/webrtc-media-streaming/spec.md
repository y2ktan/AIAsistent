# Spec Delta

## ADDED Requirements

### Requirement: Speaker voice focus and ambient noise filtering
When Audio Focus is enabled, the system SHALL isolate near-field speaker frequencies and attenuate ambient environmental noise on inbound microphone capture before streaming audio over WebRTC, and SHALL bypass Audio Focus filtering when disabled.

#### Scenario: Voice session starts with Audio Focus enabled
- **WHEN** an authenticated user starts a voice session while Audio Focus is enabled (including by default)
- **THEN** the client requests voice-isolation capture constraints where supported and routes microphone audio through the speaker-focus noise-filtering graph before attaching the outbound WebRTC audio track

#### Scenario: Voice session starts with Audio Focus disabled
- **WHEN** an authenticated user starts a voice session after an administrator disables Audio Focus on `/admin`
- **THEN** the client bypasses the speaker-focus filtering graph and attaches the standard microphone audio track to the WebRTC peer connection

## MODIFIED Requirements

### Requirement: User barge-in speech interruption
The system SHALL detect when the user begins speaking above an adaptive ambient noise floor (when Audio Focus is enabled) or voice activity threshold while the assistant is outputting speech and SHALL immediately halt outbound assistant audio playback.

#### Scenario: User interrupts assistant response
- **WHEN** the user speaks above the voice activity threshold while assistant speech is streaming
- **THEN** the system stops streaming current assistant audio frames and sends an interruption control event so the client flushes buffered playback

#### Scenario: Steady ambient noise does not trigger false barge-in or stall end-of-speech
- **WHEN** continuous environmental background noise is present while Audio Focus is enabled
- **THEN** the voice activity detector adapts its noise floor so background noise alone does not emit `vad.speech_start` interruptions and still emits `vad.speech_end` promptly after the speaker finishes talking
