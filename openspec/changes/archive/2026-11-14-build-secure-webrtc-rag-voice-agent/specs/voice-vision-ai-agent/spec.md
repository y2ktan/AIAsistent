# Spec Delta

## Purpose

Governs multimodal conversational intelligence, including dual-mode speech and vision orchestration, external Model Context Protocol (MCP) tool invocation, and AI prompt/output guardrails.

## ADDED Requirements

### Requirement: Dual-mode spoken and visual question answering
The system SHALL support both native real-time multimodal streaming endpoints and a modular speech-to-text, vision/chat language model, and streaming text-to-speech pipeline based on active administrator configuration.

#### Scenario: Realtime multimodal mode processes speech and camera context
- **WHEN** the active agent mode is set to `realtime` and the user asks a spoken question with the camera enabled
- **THEN** the system streams user audio and sampled video frames to the configured realtime endpoint and returns synthesized spoken audio plus transcript

#### Scenario: Modular OpenAI-compatible mode answers spoken question
- **WHEN** the active agent mode is set to `cascaded` against an OpenAI-compatible endpoint and the user finishes an utterance
- **THEN** the system transcribes the speech, evaluates the query with any active camera frame, RAG context, and MCP tools, and streams synthesized speech back to the user

### Requirement: Grounded answers prioritized from retrieved knowledge
The system SHALL consult ingested knowledge base context when answering factual domain questions and SHALL surface source citations alongside spoken responses.

#### Scenario: User asks question covered by ingested document
- **WHEN** the user speaks a question matching content in an indexed document
- **THEN** the assistant incorporates the retrieved passage into its spoken answer and emits source document metadata to the client

#### Scenario: Unconfigured or unreachable AI endpoint handled gracefully
- **WHEN** the configured AI provider endpoint fails or no active credential is configured
- **THEN** the system notifies the user via a safe status message without exposing raw provider URLs, stack traces, or credentials

### Requirement: Model Context Protocol (MCP) tool discovery and execution
The system SHALL connect to administrator-enabled MCP servers, expose their permitted tools to the active conversational agent, and enforce execution timeouts on tool calls.

#### Scenario: Assistant invokes an enabled MCP tool during conversation
- **WHEN** answering a user utterance requires data from a configured and enabled MCP server
- **THEN** the system executes the MCP tool call within a bounded timeout, emits a tool-activity indicator, and incorporates the sanitized result into the spoken reply

### Requirement: Prompt-injection isolation and output guardrails
The system SHALL isolate untrusted retrieved document text and external MCP tool outputs from system instructions and SHALL prevent disclosure of system secrets or internal configurations.

#### Scenario: Ingested document or spoken input contains prompt-injection instructions
- **WHEN** a retrieved passage or spoken utterance instructs the assistant to reveal API keys, system prompts, or bypass role rules
- **THEN** the system treats external data strictly as untrusted content and refuses to disclose secrets or execute unauthorized actions
