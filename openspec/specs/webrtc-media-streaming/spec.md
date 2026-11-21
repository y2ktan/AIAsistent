# webrtc-media-streaming Specification

## Purpose

Defines bidirectional encrypted real-time audio, video, and control event streaming between the user's browser and the conversational backend during active voice sessions.

## Requirements

### Requirement: Encrypted peer-to-peer audio and video session establishment
The system SHALL negotiate an encrypted real-time media session (DTLS-SRTP) carrying inbound user microphone audio, optional inbound user camera video, and outbound assistant speech audio.

#### Scenario: Authenticated user starts a conversation session
- **WHEN** an authenticated user activates a voice session with a valid signaling token
- **THEN** the system completes SDP offer/answer and ICE negotiation over TLS and establishes encrypted audio and data channels

#### Scenario: Peer disconnection cleans up server resources
- **WHEN** the user ends the session or the peer connection drops beyond the recovery timeout
- **THEN** the system closes all associated media tracks, cancels in-flight speech synthesis, and releases session memory

### Requirement: Independent speaker/microphone and camera track toggling
The system SHALL allow the user to independently enable or disable audio (microphone/speaker) and camera video before or during an active conversation without dropping the peer connection.

#### Scenario: User turns camera on mid-conversation
- **WHEN** the user enables the camera during an active audio conversation
- **THEN** video frames begin streaming to the backend for visual context while continuous audio conversation remains uninterrupted

#### Scenario: User turns camera off mid-conversation
- **WHEN** the user disables the camera during an active session
- **THEN** outbound video capture stops immediately and the backend ceases sampling visual frames from that session

#### Scenario: User mutes microphone or speaker
- **WHEN** the user toggles mute on microphone input or speaker output
- **THEN** audio capture or playback halts immediately while preserving session state until unmuted

### Requirement: Speaker voice focus and ambient noise filtering
When Audio Focus is enabled, the system SHALL isolate near-field speaker frequencies and attenuate ambient environmental noise on inbound microphone capture before streaming audio over WebRTC, and SHALL bypass Audio Focus filtering when disabled.

#### Scenario: Voice session starts with Audio Focus enabled
- **WHEN** an authenticated user starts a voice session while Audio Focus is enabled (including by default)
- **THEN** the client requests voice-isolation capture constraints where supported and routes microphone audio through the speaker-focus noise-filtering graph before attaching the outbound WebRTC audio track

#### Scenario: Voice session starts with Audio Focus disabled
- **WHEN** an authenticated user starts a voice session after an administrator disables Audio Focus on `/admin`
- **THEN** the client bypasses the speaker-focus filtering graph and attaches the standard microphone audio track to the WebRTC peer connection

### Requirement: User barge-in speech interruption
The system SHALL preserve outbound assistant audio playback during raw voice-activity onsets and unaddressed background speech, and SHALL halt outbound assistant audio playback only when transcribed speech is verified to begin with a configured wake name or alias or when an explicit interruption control event is received.

#### Scenario: User interrupts assistant response
- **WHEN** assistant speech is streaming and a newly transcribed user utterance begins with the configured assistant name or a wake alias (with or without a short leading greeting)
- **THEN** the system stops streaming current assistant audio frames and sends an interruption control event so the client flushes buffered playback

#### Scenario: Steady ambient noise does not trigger false barge-in or stall end-of-speech
- **WHEN** continuous environmental background noise is present while Audio Focus is enabled
- **THEN** the voice activity detector adapts its noise floor so background noise alone does not emit `vad.speech_start` and emits `vad.speech_end` after the post-speech hold period

#### Scenario: Background speech does not interrupt assistant response
- **WHEN** assistant speech is streaming and background talkers speak above the voice activity threshold without starting their utterance with a configured wake name or alias
- **THEN** outbound assistant audio playback continues uninterrupted to completion

### Requirement: Pause-tolerant voice activity end-of-speech hold
The system SHALL hold an active speech segment open across brief thinking pauses of up to 2.5 seconds before emitting `vad.speech_end` so natural mid-thought pauses remain within a single captured utterance.

#### Scenario: Brief thinking pause stays within one utterance
- **WHEN** the speaker begins talking and pauses for less than 2.5 seconds before continuing to speak
- **THEN** the voice activity detector keeps the current speech segment active without emitting a premature `vad.speech_end`

#### Scenario: Sustained silence completes speech segment
- **WHEN** speech energy remains below the sustain threshold for at least 2.5 seconds after speaking
- **THEN** the voice activity detector emits `vad.speech_end` so the backend can evaluate the captured utterance

### Requirement: Real-time control and transcript data channel
The system SHALL maintain an ordered data channel synchronized with the media session to stream live partial/final transcripts, assistant state transitions, and RAG or tool status events.

#### Scenario: Assistant transitions across conversational states
- **WHEN** the backend moves between `listening`, `thinking`, `retrieving`, and `speaking` states
- **THEN** the system emits structured state events and incremental transcript deltas to the client in real time
