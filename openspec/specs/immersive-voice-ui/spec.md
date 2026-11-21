# immersive-voice-ui Specification

## Purpose

Defines the interactive, audio-reactive visual home experience where authenticated users converse with the assistant using speech and live camera video.

## Requirements

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

### Requirement: Configured assistant identity display and clean gated captions
The system SHALL display the active configured assistant name on the home page header and assistant transcript speaker labels, and SHALL omit unaddressed background speech discarded by the wake-name gate from live conversation captions.

#### Scenario: Home page renders custom configured assistant name
- **WHEN** an administrator has configured a custom primary assistant name (such as `Jarvis` or `小智`) and a user opens the home page and conducts a voice session
- **THEN** the home page header and assistant transcript badge render the configured assistant name instead of a hardcoded default

#### Scenario: Unaddressed background chatter omitted from live captions
- **WHEN** background speakers talk during an active voice session without addressing a configured wake name or alias while no wake window is armed
- **THEN** the home page does not append the discarded background utterance to the live transcript list

### Requirement: Real-time transcript and grounded citation drawer
The system SHALL display streaming user and assistant captions and tool-execution indicators synchronized with spoken output, SHALL display interactive non-skill RAG citation badges and excerpt previews only to authenticated administrators, and SHALL exclude Agent Skill (`SKILL.md`) definitions from home-page citation badges for every role during conversations.

#### Scenario: Grounded response displays document citation
- **WHEN** an authenticated administrator receives an assistant answer using retrieved passages from standard knowledge documents
- **THEN** the home page displays clickable source badges showing the matched document title and relevant excerpt preview

#### Scenario: Non-admin user does not see grounded document citations
- **WHEN** an authenticated user whose role is not `admin` receives an assistant answer using retrieved passages from standard knowledge documents
- **THEN** the home page omits all standard RAG citation badges and expandable excerpt previews while preserving the transcript, assistant response, tool indicators, and conversation controls

#### Scenario: Agent skill entries omitted from home-page citations
- **WHEN** a citation event during an ongoing conversation includes Agent Skill (`SKILL.md`) chunks—either alone or alongside standard knowledge document chunks
- **THEN** the home page omits the Agent Skill citation badges for administrators and non-admin users, renders only administrator-visible non-skill citations, and renders no citation section for a non-admin user

#### Scenario: Admin navigation surfaced only to administrators
- **WHEN** an authenticated user with role `admin` views the home page
- **THEN** a navigation link to `/admin` is rendered, whereas users with role `user` do not see administrative navigation controls
