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

### Requirement: User barge-in speech interruption
The system SHALL detect when the user begins speaking while the assistant is outputting speech and SHALL immediately halt outbound assistant audio playback.

#### Scenario: User interrupts assistant response
- **WHEN** the user speaks above the voice activity threshold while assistant speech is streaming
- **THEN** the system stops streaming current assistant audio frames and sends an interruption control event so the client flushes buffered playback

### Requirement: Real-time control and transcript data channel
The system SHALL maintain an ordered data channel synchronized with the media session to stream live partial/final transcripts, assistant state transitions, and RAG or tool status events.

#### Scenario: Assistant transitions across conversational states
- **WHEN** the backend moves between `listening`, `thinking`, `retrieving`, and `speaking` states
- **THEN** the system emits structured state events and incremental transcript deltas to the client in real time
