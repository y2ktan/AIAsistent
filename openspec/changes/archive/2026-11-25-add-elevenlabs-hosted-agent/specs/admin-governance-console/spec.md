# Spec Delta

## MODIFIED Requirements

### Requirement: RAG document management workspace
The system SHALL provide an administrative document panel displaying local ingestion status, chunk counts, upload controls, reindex triggers, and deletion actions, and when an ElevenLabs hosted agent is configured SHALL additionally display per-document hosted Knowledge Base sync status (`synced`, `indexing`, `failed`, or `not_synced`) and a manual hosted-sync/retry trigger.

#### Scenario: Administrator monitors document ingestion progress
- **WHEN** an administrator uploads one or more documents in `/admin`
- **THEN** the document table reflects status transitions (`processing`, `ready`, or `failed`) and total indexed chunk count

#### Scenario: Administrator monitors and retries hosted Knowledge Base sync
- **WHEN** an administrator views `/admin` with a configured hosted agent or triggers a hosted sync retry after an upstream ElevenLabs failure
- **THEN** the document panel displays each document's hosted sync state without altering its local pgvector status and retries synchronization server-side

### Requirement: Write-only AI provider key, endpoint, and model configuration
The system SHALL allow administrators to configure active AI pipeline mode (`realtime`, `cascaded`, or `elevenlabs_agent`), provider base URLs, model names (including selecting from discovered speech-to-text models when available, with manual entry fallback), system instructions, and API keys while treating secrets as write-only after submission. In `elevenlabs_agent` mode, `assistantName`, `systemInstructions`, enabled Agent Skills, and `ready` local documents SHALL synchronize server-side to the configured hosted agent, while LLM selection, voice, TTS model, and language configuration SHALL remain dashboard-owned.

#### Scenario: Administrator updates AI API key and endpoint
- **WHEN** an administrator saves a new AI provider endpoint and API key
- **THEN** the system encrypts the key at rest, applies the configuration to subsequent sessions, and returns only a masked key preview (for example, `****abcd`) on all read responses

#### Scenario: Administrator selects an STT model from discovered provider models
- **WHEN** the configured speech-to-text provider exposes available models and an administrator opens `/admin` in `cascaded` mode
- **THEN** the AI Agent form offers an STT Model dropdown populated with discovered models, falling back to a text input when discovery is unavailable

#### Scenario: Administrator tests AI endpoint connectivity
- **WHEN** an administrator triggers a connection health check for the configured AI provider
- **THEN** the backend verifies connectivity server-side and returns a sanitized pass/fail status without exposing stored credentials to the browser

#### Scenario: Administrator selects hosted mode
- **WHEN** an administrator selects `elevenlabs_agent`
- **THEN** the form offers a private agent ID, the existing write-only ElevenLabs credential, `assistantName`, and `systemInstructions`, identifies which settings synchronize automatically versus remain dashboard-owned, and preserves inactive provider fields

## ADDED Requirements

### Requirement: Hosted-agent activation validation and synchronization
The system SHALL reject hosted activation without an agent ID and a saved or submitted ElevenLabs credential. Before completing hosted activation or saving updated hosted settings, the server SHALL validate private-agent authentication and the absence of external tools, and SHALL synchronize `assistantName`, `systemInstructions`, enabled Agent Skills, and `ready` local documents without requiring local speech voices, endpoints, or embedding settings.

#### Scenario: Complete private RAG-only agent activated and synchronized
- **WHEN** an administrator activates a reachable private agent without external tools using a valid write-capable ElevenLabs credential
- **THEN** the server synchronizes the application's prompt/persona, enabled Agent Skills, and `ready` local documents to the hosted agent, enables hosted RAG while preserving unmanaged dashboard documents, and saves `elevenlabs_agent` without changing local voices, language defaults, or provider settings

#### Scenario: Incomplete, incompatible, or non-writable agent
- **WHEN** an administrator attempts activation with missing credentials, a missing or inaccessible agent, insufficient write permissions, an oversized skill prompt, a public agent, or enabled external tools
- **THEN** the save fails with a safe diagnostic and leaves the previous active mode unchanged

#### Scenario: Provider validation unavailable
- **WHEN** the provider cannot be reached during activation validation or synchronization
- **THEN** activation fails visibly without treating unavailable validation as success

### Requirement: Hosted ownership and processing disclosure
The hosted settings SHALL distinguish synchronized application settings (`assistantName`, `systemInstructions`, enabled Agent Skills, and additive `ready` local documents) from ElevenLabs dashboard-owned settings (LLM, voice, TTS model, languages, turn-taking, and retention). They SHALL disclose cloud speech and document processing, potential charges, provider turn-taking, and the absence of camera, application MCP tools, and local live citations.

#### Scenario: Administrator configures hosted mode
- **WHEN** an administrator opens the hosted-agent settings
- **THEN** the form displays which settings synchronize automatically versus remain dashboard-owned and hides inapplicable local speech/LLM endpoint controls while preserving their saved values

#### Scenario: Local document operations synchronize when hosted mode is active
- **WHEN** an administrator uploads, reindexes, or deletes a document in the application's document workspace while `elevenlabs_agent` is active
- **THEN** the operation updates local pgvector storage and automatically synchronizes the corresponding ElevenLabs Knowledge Base entry while preserving unmanaged dashboard documents
