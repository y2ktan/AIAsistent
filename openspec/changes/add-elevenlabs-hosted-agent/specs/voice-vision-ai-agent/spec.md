# Spec Delta

## MODIFIED Requirements

### Requirement: Dual-mode spoken and visual question answering
The system SHALL support native real-time multimodal streaming and a modular speech-to-text, vision/chat, and streaming text-to-speech pipeline, orchestrating enabled Agent Skills alongside RAG and MCP tools in `realtime` and `cascaded` modes. It SHALL additionally support `elevenlabs_agent` as an audio-only hosted conversation mode using the configured agent's knowledge and model without the backend multimodal pipeline.

#### Scenario: Realtime multimodal mode processes speech and camera context
- **WHEN** the active agent mode is set to `realtime` and the user addresses the configured assistant name or wake alias with a direct spoken question while the camera is enabled
- **THEN** the system streams user audio and sampled video frames to the configured realtime endpoint, exposes enabled Agent Skills and permitted MCP tools, and returns synthesized spoken audio plus transcript

#### Scenario: Modular OpenAI-compatible mode answers spoken question
- **WHEN** the active agent mode is set to `cascaded` against an OpenAI-compatible endpoint and the user completes a wake-addressed direct question (either in one segment or across an armed thinking-pause window)
- **THEN** the system transcribes the speech with wake-vocabulary hinting, evaluates the accumulated query with any active camera frame, RAG context, enabled Agent Skills, and MCP tools across multi-step tool rounds, and streams synthesized speech back to the user

#### Scenario: Hosted audio mode answers spoken question
- **WHEN** the active agent mode is `elevenlabs_agent` and an authenticated user starts a hosted session and speaks
- **THEN** the configured private hosted agent handles speech, knowledge retrieval, model answering, and audio output without local camera or tool processing

### Requirement: Grounded answers prioritized from retrieved knowledge
In `cascaded` and `realtime` modes, the system SHALL consult ingested knowledge base context when answering factual domain questions and SHALL surface source citations alongside spoken responses. In `elevenlabs_agent` mode, document grounding SHALL use the agent's synchronized and dashboard-attached ElevenLabs knowledge without claiming local citation metadata.

#### Scenario: User asks question covered by ingested document
- **WHEN** the user speaks a question matching content in an indexed document in `cascaded` or `realtime` mode
- **THEN** the assistant incorporates the retrieved passage into its spoken answer and emits source document metadata to the client

#### Scenario: Unconfigured or unreachable AI endpoint handled gracefully
- **WHEN** the configured AI provider endpoint fails or no active credential is configured
- **THEN** the system notifies the user via a safe status message without exposing raw provider URLs, stack traces, or credentials

#### Scenario: Hosted grounding does not imply local citations
- **WHEN** the hosted agent answers from its attached knowledge
- **THEN** the application displays its answer without inventing document IDs, local chunk identifiers, similarity scores, or live local source citations

## ADDED Requirements

### Requirement: Backend conversation-policy applicability
The application's wake-name gate, armed thinking-pause accumulation, direct-question gate, name-verified barge-in, camera processing, and application skill/MCP orchestration SHALL apply only to `cascaded` and `realtime` modes. Hosted sessions SHALL use provider conversation policy and SHALL NOT invoke those backend paths.

#### Scenario: Existing mode keeps strict gating
- **WHEN** a cascaded or realtime session receives unaddressed background speech or a wake-addressed statement without a direct question
- **THEN** the existing rejection, accumulation, and interruption policies remain in effect

#### Scenario: Hosted session delegates turn policy
- **WHEN** speech reaches an `elevenlabs_agent` session
- **THEN** the provider handles its turns without the application promising the backend's strict wake-name, silence, or pause behavior
