# elevenlabs-hosted-conversation Specification

## Purpose

Defines authenticated access to an optional ElevenLabs hosted voice agent whose speech, turn-taking, language, voice, and LLM selection remain provider-managed while its prompt, persona, enabled Agent Skills, and additive document knowledge synchronize server-side from the application.

## Requirements

### Requirement: Authenticated private hosted-agent access
The system SHALL issue hosted voice access only to an active authenticated application user, using the administrator-configured private agent and server-held credential. Issuance SHALL require CSRF and Origin validation, rate limiting, and a non-cacheable response. Requests SHALL NOT select another agent.

#### Scenario: Active user starts hosted voice
- **WHEN** an active authenticated user submits a valid hosted session request while hosted mode is configured
- **THEN** the server obtains access for the configured private agent and returns only the connection token and necessary non-secret session metadata

#### Scenario: Unauthorized or invalid session request
- **WHEN** a request has no valid active session, fails CSRF or Origin validation, or attempts to substitute an agent ID
- **THEN** access is denied without requesting a provider token

#### Scenario: Issuance limit reached
- **WHEN** a user exceeds the configured session issuance limit
- **THEN** the server responds with a rate-limit error without starting another provider request

### Requirement: Credential and token confidentiality
The system SHALL keep provider API keys and administrator-only configuration out of browser session responses. Conversation tokens SHALL remain transient and SHALL NOT appear in logs, audit metadata, URLs owned by the application, persistent browser storage, or error messages.

#### Scenario: Inspect session traffic and errors
- **WHEN** a user receives a connection response or an upstream request fails
- **THEN** the browser receives no API key, raw upstream error body, or unrelated administrator configuration, and logs contain no connection token

### Requirement: Hosted speech and model answering
In `elevenlabs_agent` mode, the system SHALL connect microphone input to the configured hosted agent and play its spoken replies while displaying user and assistant transcripts. The hosted agent SHALL own speech processing and model responses; the application SHALL NOT run its local STT, LLM, retrieval, or TTS pipeline for that session.

#### Scenario: User asks a spoken question
- **WHEN** a connected user asks a question
- **THEN** the hosted agent processes the speech and returns a spoken answer with transcript updates in the application's voice UI

### Requirement: Synchronized and dashboard knowledge grounding
Hosted sessions SHALL ground document answers using the configured agent's attached ElevenLabs Knowledge Base and RAG index, combining local `ready` application documents synchronized server-side with any additional unmanaged documents attached in the ElevenLabs dashboard. During a hosted voice call, the application SHALL NOT query its local pgvector store at turn time.

#### Scenario: Synchronized local or dashboard policy answers a question
- **WHEN** a local `ready` policy document has been synchronized and indexed on the configured hosted agent (alongside any dashboard-attached documents) and the user asks a question covered by that policy
- **THEN** the spoken answer reflects the attached ElevenLabs Knowledge Base without calling the application's pgvector store during the turn

#### Scenario: Unmanaged dashboard documents preserved alongside synced local documents
- **WHEN** the configured hosted agent also has a document attached directly in the ElevenLabs dashboard that is not tracked by the application
- **THEN** synchronizing local documents preserves that dashboard document on the agent alongside the application-managed documents

### Requirement: Provider-owned conversation behavior with server-synchronized persona
Hosted sessions SHALL apply the server-synchronized agent prompt (`assistantName`, `systemInstructions`, and enabled declarative Agent Skills) while using the agent's dashboard-defined turn-taking, interruption, language, voice, TTS model, and LLM settings without client-side runtime overrides. Hosted settings SHALL disclose provider ownership, external processing, potential charges, and application feature limits before activation; the voice screen SHALL NOT repeat this disclosure.

#### Scenario: Speech without a wake name
- **WHEN** a user speaks without the application's configured wake name in a hosted session
- **THEN** the application does not discard the utterance through its backend wake gate and allows the hosted agent's turn policy to handle it

#### Scenario: Synchronized persona and skills apply while voice and language remain dashboard-owned
- **WHEN** an administrator has synchronized `assistantName`, `systemInstructions`, and enabled Agent Skills while the hosted agent's voice or language configuration differs from the application's saved language table
- **THEN** the hosted agent uses the server-synchronized prompt and persona without sending client-side voice or language overrides or promising the application's language fallback behavior

#### Scenario: Hosted disclosure remains in settings, not the voice screen
- **WHEN** an administrator configures hosted mode and a user opens the hosted voice screen
- **THEN** hosted settings disclose provider ownership, external processing, potential charges, and application feature limits, while the voice screen omits the repeated disclosure

### Requirement: Audio controls and hosted session cleanup
The system SHALL let users independently mute microphone capture and speaker playback without ending a hosted connection. Ending the call, leaving the voice view, logging out, or detecting invalid application authentication SHALL disconnect the hosted session and release owned microphone and playback resources.

#### Scenario: Microphone and speaker toggles
- **WHEN** the user mutes either microphone or speaker during hosted voice
- **THEN** that input or output stops while the connection and the other control remain available

#### Scenario: Start with microphone muted
- **WHEN** the user mutes the microphone before starting hosted voice
- **THEN** no unmuted microphone track is published during connection setup, and unmuting remains an explicit user action

#### Scenario: Start with speaker muted
- **WHEN** the user mutes the speaker before starting hosted voice
- **THEN** speaker volume is muted before attaching reply playback, independently of microphone intent

#### Scenario: End or authentication loss
- **WHEN** the user ends the call, leaves the voice view, logs out, or the application detects session invalidation
- **THEN** the client disconnects from the hosted agent and stops its owned audio resources without leaving an active capture stream

### Requirement: Hosted failure visibility
The system SHALL report safe connection, permission, provider, and playback errors and SHALL NOT automatically fall back to another application mode. A failed or disconnected call SHALL leave controls in a recoverable state without duplicate active conversations.

#### Scenario: Provider token request fails
- **WHEN** the provider rejects access or cannot be reached
- **THEN** the UI displays a sanitized connection error and does not establish a local backend conversation

#### Scenario: Browser permission or playback failure
- **WHEN** microphone permission is denied or reply playback is blocked
- **THEN** the UI identifies the failing audio capability and releases unused resources or presents an explicit playback retry

#### Scenario: Connection ends unexpectedly
- **WHEN** a hosted connection drops or is terminated by the provider
- **THEN** the UI clears connection state and owned resources, preserves already displayed transcript text, and requires an explicit new start

### Requirement: RAG-only hosted feature boundary
The initial hosted mode SHALL be audio-only and SHALL NOT expose runtime backend skill-loader tools, application MCP tools, local citation data, or camera frames. Enabled declarative Agent Skills SHALL be compiled into the server-synchronized hosted prompt instead of exposed as runtime backend tools. The connected agent SHALL have external client, webhook, and MCP tools disabled; provider system tools for turn control or hosted retrieval are permitted.

#### Scenario: Unsupported controls in hosted mode
- **WHEN** a user opens or starts hosted voice
- **THEN** camera and application MCP/tool-activity controls are unavailable and local citations are cleared

#### Scenario: Agent configured with external tools
- **WHEN** activation testing or session issuance finds external tools enabled on the configured hosted agent
- **THEN** the application rejects hosted access with a safe instruction to use a RAG-only agent rather than executing or forwarding those tools

### Requirement: Explicit mode selection and session stability
The system SHALL retain the configured conversation mode and all inactive-provider settings across upgrades and switches. Active connections SHALL NOT switch application transport or agent ID when administrators change configuration; subsequent starts SHALL use the newly saved selection.

#### Scenario: Mode changed during a call
- **WHEN** an administrator selects another mode or agent ID during a connected call
- **THEN** the call retains its starting connection and subsequent calls use the newly saved configuration

#### Scenario: Switch back to cascaded
- **WHEN** an administrator returns to cascaded mode
- **THEN** its previously saved speech, LLM, knowledge, and language settings remain available and determine new cascaded sessions

### Requirement: Optional hosted application augmentation
Hosted-agent connection SHALL remain available when application RAG synchronization, Agent Skill synchronization, or both are disabled, provided the configured private agent, authentication, workflow, and tool references remain valid.

#### Scenario: Hosted agent connects without application RAG or skills
- **WHEN** a private hosted agent is configured with both application RAG and Agent Skill synchronization disabled
- **THEN** the application validates and issues hosted access without requiring local ready documents, local Agent Skills, or an enabled hosted RAG configuration

#### Scenario: Hosted agent connects with only one application domain
- **WHEN** either application RAG or Agent Skill synchronization is enabled while the other is disabled
- **THEN** hosted access uses only the enabled application domain and does not require the disabled domain to be ready or available

#### Scenario: Dashboard-owned hosted knowledge remains available
- **WHEN** application RAG synchronization is disabled and the ElevenLabs agent retains dashboard-managed Knowledge Base documents
- **THEN** the hosted conversation may use those dashboard-managed documents without the application attaching or inspecting local documents

#### Scenario: A disabled domain does not block token issuance
- **WHEN** a session requests hosted access while the selected RAG or Agent Skill synchronization domain is disabled
- **THEN** token issuance and hosted workflow validation proceed normally and do not report the disabled domain as an application synchronization failure
