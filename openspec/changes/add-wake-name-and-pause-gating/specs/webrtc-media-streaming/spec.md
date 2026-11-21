# Spec Delta

## ADDED Requirements

### Requirement: Pause-tolerant voice activity end-of-speech hold
The system SHALL hold an active speech segment open across brief thinking pauses of up to 2.5 seconds before emitting `vad.speech_end` so natural mid-thought pauses remain within a single captured utterance.

#### Scenario: Brief thinking pause stays within one utterance
- **WHEN** the speaker begins talking and pauses for less than 2.5 seconds before continuing to speak
- **THEN** the voice activity detector keeps the current speech segment active without emitting a premature `vad.speech_end`

#### Scenario: Sustained silence completes speech segment
- **WHEN** speech energy remains below the sustain threshold for at least 2.5 seconds after speaking
- **THEN** the voice activity detector emits `vad.speech_end` so the backend can evaluate the captured utterance

## MODIFIED Requirements

### Requirement: User barge-in speech interruption
The system SHALL preserve outbound assistant audio playback during raw voice-activity onsets and unaddressed background speech, and SHALL halt outbound assistant audio playback only when transcribed speech is verified to begin with a configured wake name or alias or when an explicit interruption control event is received.

#### Scenario: User interrupts assistant response
- **WHEN** assistant speech is streaming and a newly transcribed user utterance begins with the configured assistant name or a wake alias (with or without a short leading greeting)
- **THEN** the system stops streaming current assistant audio frames and sends an interruption control event

#### Scenario: Background speech does not interrupt assistant response
- **WHEN** assistant speech is streaming and background talkers speak above the voice activity threshold without starting their utterance with a configured wake name or alias
- **THEN** outbound assistant audio playback continues uninterrupted to completion

#### Scenario: Steady ambient noise does not trigger false barge-in or stall end-of-speech
- **WHEN** continuous environmental background noise is present while Audio Focus is enabled
- **THEN** the voice activity detector adapts its noise floor so background noise alone does not emit `vad.speech_start` and emits `vad.speech_end` after the post-speech hold period
