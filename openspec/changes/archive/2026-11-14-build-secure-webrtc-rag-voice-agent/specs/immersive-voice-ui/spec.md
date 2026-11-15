# Spec Delta

## Purpose

Defines the interactive, audio-reactive visual home experience where authenticated users converse with the assistant using speech and live camera video.

## ADDED Requirements

### Requirement: Audio-reactive animated conversational stage
The system SHALL render a responsive central visual stage on the home page whose motion, glow, and waveform dynamics react in real time to user microphone amplitude, assistant speech energy, and conversational state.

#### Scenario: Visualizer reflects speaking and thinking states
- **WHEN** the session transitions from `listening` to `thinking` and then `speaking`
- **THEN** the central visualizer smoothly shifts its animation state and modulates amplitude directly from live audio frequency data

#### Scenario: Reduced-motion preference respected
- **WHEN** a user device advertises `prefers-reduced-motion: reduce`
- **THEN** the interface replaces high-velocity particle or shader motion with gentle opacity and scale feedback while preserving full state clarity

### Requirement: Accessible media controls and live camera preview
The system SHALL provide prominent, keyboard-accessible controls on the home page to start/end a call, mute/unmute microphone and speaker audio, and toggle the user camera with a live self-view overlay.

#### Scenario: User activates camera during voice session
- **WHEN** the user clicks the camera toggle on the home page and grants browser camera permission
- **THEN** a floating picture-in-picture camera preview appears with an active vision indicator while video streams to the assistant

#### Scenario: Browser denies microphone or camera permission
- **WHEN** the browser blocks media device access
- **THEN** the interface displays an inline, non-crashing recovery prompt explaining how to re-enable device permissions

### Requirement: Real-time transcript and grounded citation drawer
The system SHALL display streaming user and assistant captions alongside interactive source citation badges and tool-execution indicators synchronized with spoken output.

#### Scenario: Grounded response displays document citation
- **WHEN** the assistant answers using retrieved document passages
- **THEN** the transcript card displays clickable source badges showing the matched document title and relevant excerpt preview

#### Scenario: Admin navigation surfaced only to administrators
- **WHEN** an authenticated user with role `admin` views the home page
- **THEN** a navigation link to `/admin` is rendered, whereas users with role `user` do not see administrative navigation controls
