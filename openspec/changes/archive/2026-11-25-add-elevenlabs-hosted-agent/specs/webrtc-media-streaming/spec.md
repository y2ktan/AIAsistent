# Spec Delta

## MODIFIED Requirements

### Requirement: Encrypted peer-to-peer audio and video session establishment
The system SHALL negotiate encrypted real-time media carrying inbound microphone audio and outbound assistant speech. In `cascaded` and `realtime` modes, the backend media session SHALL additionally support optional inbound camera video and its existing signaling and control transport. In `elevenlabs_agent` mode, media SHALL connect directly to the hosted provider as audio-only.

#### Scenario: Authenticated user starts a conversation session
- **WHEN** an authenticated user activates a cascaded or realtime voice session with a valid signaling token
- **THEN** the system completes SDP offer/answer and ICE negotiation over TLS and establishes encrypted audio and data channels

#### Scenario: Peer disconnection cleans up server resources
- **WHEN** the user ends a backend media session or its peer connection drops beyond the recovery timeout
- **THEN** the system closes all associated media tracks, cancels in-flight speech synthesis, and releases session memory

#### Scenario: Hosted session connects directly
- **WHEN** an authenticated user starts hosted voice with valid provider-issued access
- **THEN** the browser establishes an encrypted audio connection with the hosted provider without creating a backend media peer

## ADDED Requirements

### Requirement: Backend media-policy applicability
The existing camera-track controls, Audio Focus filtering graph, name-verified interruption, 2.5-second VAD hold, and ordered backend control/transcript data channel SHALL apply to `cascaded` and `realtime` media sessions only. Hosted sessions SHALL provide audio controls and transcript updates through their provider connection without claiming those backend media policies.

#### Scenario: Hosted mode shows transport limitations
- **WHEN** the voice UI selects hosted mode
- **THEN** it disables camera capture, identifies provider-owned interruption and audio processing, and does not present the backend Audio Focus graph or pause hold as active

#### Scenario: Backend controls remain intact
- **WHEN** a user starts a cascaded or realtime session after previously using hosted mode
- **THEN** the configured backend camera, audio-focus, interruption, pause, and data-channel behavior remains available without residual hosted microphone capture
