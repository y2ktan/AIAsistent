# voice-vision-ai-agent Specification

## Purpose

Governs multimodal conversational intelligence, including dual-mode speech and vision orchestration, external Model Context Protocol (MCP) tool invocation, and AI prompt/output guardrails.

## Requirements

### Requirement: Dual-mode spoken and visual question answering
The system SHALL support both native real-time multimodal streaming endpoints and a modular speech-to-text, vision/chat language model, and streaming text-to-speech pipeline based on active administrator configuration, orchestrating enabled Agent Skills alongside RAG context and MCP tools in both modes.

#### Scenario: Realtime multimodal mode processes speech and camera context
- **WHEN** the active agent mode is set to `realtime` and the user addresses the configured assistant name or wake alias with a direct spoken question while the camera is enabled
- **THEN** the system streams user audio and sampled video frames to the configured realtime endpoint, exposes enabled Agent Skills and permitted MCP tools, and returns synthesized spoken audio plus transcript

#### Scenario: Modular OpenAI-compatible mode answers spoken question
- **WHEN** the active agent mode is set to `cascaded` against an OpenAI-compatible endpoint and the user completes a wake-addressed direct question (either in one segment or across an armed thinking-pause window)
- **THEN** the system transcribes the speech with wake-vocabulary hinting, evaluates the accumulated query with any active camera frame, RAG context, enabled Agent Skills, and MCP tools across multi-step tool rounds, and streams synthesized speech back to the user

### Requirement: Wake-name prefix gating and background speech rejection
The system SHALL ignore spoken utterances when no wake window is armed unless the utterance explicitly begins with the configured assistant name or a wake alias (optionally preceded by a short English or Chinese greeting), without cancelling in-flight turns or generating spoken output.

#### Scenario: Unaddressed background speech ignored while idle or thinking
- **WHEN** background speakers talk without starting with a configured wake name or alias while the assistant is `listening`, `retrieving`, `thinking`, or `speaking`
- **THEN** the system discards the background utterance without cancelling any in-flight turn or generating assistant speech

#### Scenario: English wake name with optional greeting accepted with word boundary
- **WHEN** the configured name is `Aegis` and the user begins with `Aegis, what is the status?` or `Hey Aegis, what is the status?`
- **THEN** the system recognizes the wake prefix, whereas an utterance beginning with an unrelated lookalike prefix word is rejected

#### Scenario: Chinese wake name or alias accepted without space delimiter
- **WHEN** `小智` is configured as the primary name or a wake alias and the user begins with `小智今天有什么安排？` or `你好小智，今天有什么安排？`
- **THEN** the system recognizes the Chinese wake prefix even without whitespace between the name and the following clause

### Requirement: Armed thinking-pause accumulation and zero-filler silence
When a wake-addressed utterance does not yet ask a direct question, the system SHALL arm a 15-second silent accumulation window across subsequent speech segments, SHALL never emit filler acknowledgements during pauses, and SHALL expire the buffer silently if no question arrives before timeout.

#### Scenario: Thinking out loud after wake name stays silent without filler
- **WHEN** the user says `Hey Aegis, let me look at the third-quarter numbers...` and pauses for several seconds without asking a question
- **THEN** the assistant stays completely silent, emits no filler acknowledgement (such as `got it` or `mm-hmm`), and arms the 15-second accumulation window

#### Scenario: Paused follow-up question combines with buffered context
- **WHEN** the user speaks additional context and asks a direct question (such as `why did server latency spike?`) before the 15-second armed wake window expires
- **THEN** the system combines the buffered wake-addressed clauses with the direct question and answers the full thought in one reply

#### Scenario: Armed wake window expires silently when user abandons thought
- **WHEN** 15 seconds elapse with no direct question after the user armed the wake window
- **THEN** the system clears the buffered thought silently and returns to unarmed `listening` without speaking

### Requirement: Direct-question response gating across pipeline modes
In both `cascaded` and `realtime` modes, the system SHALL generate a spoken or transcript assistant reply only when a wake-addressed or armed-buffered utterance contains a direct question, remaining silent on statements and incomplete thoughts.

#### Scenario: Wake-addressed statement without a question receives no reply
- **WHEN** the user addresses the assistant's name followed only by a declarative observation (such as `Aegis, I am opening the dashboard now`)
- **THEN** the assistant does not speak a reply or emit conversational filler and holds the thought silently until a direct question arrives or the wake window expires

#### Scenario: Wake-addressed direct question receives grounded reply without filler
- **WHEN** the user addresses the assistant's name and asks a direct question in a supported language
- **THEN** the assistant answers the question directly without leading backchannel filler phrases

#### Scenario: Realtime mode gates responses on wake-addressed question transcription
- **WHEN** a `realtime` session is active and speech is captured in a room with background talkers and pauses
- **THEN** the session suppresses automatic upstream voice-activity response creation and interruption, triggering `response.create` only after input transcription confirms a wake-addressed direct question

### Requirement: Realtime near-field input noise reduction alignment
When the active agent pipeline mode is `realtime`, the system SHALL configure upstream near-field input audio noise reduction whenever Audio Focus is enabled and SHALL omit near-field noise reduction when Audio Focus is disabled.

#### Scenario: Realtime session starts with Audio Focus enabled
- **WHEN** a `realtime` session initializes while Audio Focus is enabled
- **THEN** the `session.update` payload sent to the Realtime provider includes `input_audio_noise_reduction` configured for `near_field` speaker isolation

#### Scenario: Realtime session starts with Audio Focus disabled
- **WHEN** a `realtime` session initializes while Audio Focus is disabled
- **THEN** the `session.update` payload omits `near_field` input noise reduction

### Requirement: Grounded answers prioritized from retrieved knowledge
The system SHALL consult ingested knowledge base context when answering factual domain questions and SHALL surface source citations alongside spoken responses.

#### Scenario: User asks question covered by ingested document
- **WHEN** the user speaks a question matching content in an indexed document
- **THEN** the assistant incorporates the retrieved passage into its spoken answer and emits source document metadata to the client

#### Scenario: Unconfigured or unreachable AI endpoint handled gracefully
- **WHEN** the configured AI provider endpoint fails or no active credential is configured
- **THEN** the system notifies the user via a safe status message without exposing raw provider URLs, stack traces, or credentials

### Requirement: Model Context Protocol (MCP) tool discovery and execution
The system SHALL connect to administrator-enabled MCP servers (negotiating Streamable HTTP initialization when required), expose their permitted tools with discovered descriptions and JSON input schemas to the active conversational agent, disambiguate colliding tool names across servers, and enforce execution timeouts on tool calls.

#### Scenario: Assistant invokes an enabled MCP tool during conversation
- **WHEN** answering a user utterance requires data from a configured and enabled MCP server
- **THEN** the system exposes the tool's discovered description and parameter schema to the LLM, executes the MCP tool call within a bounded timeout, emits a tool-activity indicator, and incorporates the sanitized result into the spoken reply

#### Scenario: Streamable HTTP MCP server requires protocol initialization
- **WHEN** an enabled MCP server requires an MCP `initialize` handshake and `Mcp-Session-Id` header before `tools/list` or `tools/call`
- **THEN** the MCP client negotiates session initialization and forwards the session header while remaining compatible with stateless JSON-RPC MCP endpoints

#### Scenario: Multiple enabled MCP servers expose a same-named tool
- **WHEN** two enabled MCP servers both allow a tool with the same raw name (such as `search`)
- **THEN** the agent exposes distinct disambiguated function names to the LLM and routes each invocation back to its originating MCP server and raw tool name

### Requirement: Prompt-injection isolation and output guardrails
The system SHALL subordinate administrator-uploaded Agent Skill instructions strictly beneath immutable system security boundary rules, isolate untrusted retrieved document text and external MCP tool outputs from system instructions, and prevent disclosure of system secrets or internal configurations.

#### Scenario: Ingested document or spoken input contains prompt-injection instructions
- **WHEN** a retrieved passage, external MCP tool output, or spoken utterance instructs the assistant to reveal API keys, system prompts, or bypass role rules
- **THEN** the system treats external data strictly as untrusted content and refuses to disclose secrets or execute unauthorized actions

#### Scenario: Skill playbook cannot override secret protection or MCP allowlists
- **WHEN** an active Agent Skill references an unpermitted MCP tool or contains text conflicting with the system security boundary preamble
- **THEN** the system enforces the security boundary preamble above skill content, blocks calls to unpermitted MCP tools, and redacts secrets from assistant output
