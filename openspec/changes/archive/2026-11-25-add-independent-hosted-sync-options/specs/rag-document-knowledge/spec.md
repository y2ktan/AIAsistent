# Spec Delta

## MODIFIED Requirements

### Requirement: Additive ElevenLabs Knowledge Base synchronization
When `elevenlabs_agent` mode is active, activated, or manually synchronized and application RAG synchronization is enabled, the server SHALL upload `ready` local documents to the ElevenLabs Knowledge Base from extracted or chunk text, trigger RAG indexing for documents of at least 500 bytes, enable hosted RAG, and attach them with `usage_mode: "auto"` while preserving unmanaged dashboard documents. Disabling application RAG synchronization SHALL detach and remove only application-managed hosted entries after preserving the local pgvector documents and dashboard-owned entries; local document deletion SHALL remove its tracked ElevenLabs entry when credentials and ownership evidence exist.

#### Scenario: Ready local documents sync and preserve manual dashboard entries
- **WHEN** an administrator activates `elevenlabs_agent` or uploads/reindexes a local document while `elevenlabs_agent` is active, application RAG synchronization is enabled, and the hosted agent also has an unmanaged dashboard document
- **THEN** the backend creates or replaces the tracked ElevenLabs Knowledge Base text entry, triggers RAG indexing when at least 500 bytes, enables hosted RAG, and updates the agent's `knowledge_base` list while retaining the unmanaged dashboard document

#### Scenario: Short local document attached without failing RAG index threshold
- **WHEN** a `ready` local document has fewer than 500 UTF-8 bytes of text during enabled hosted Knowledge Base synchronization
- **THEN** the backend creates and attaches the ElevenLabs Knowledge Base document without failing on ElevenLabs' 500-byte RAG indexing minimum

#### Scenario: Deleting a synced local document cleans up ElevenLabs Knowledge Base
- **WHEN** an administrator deletes a local document that has a tracked ElevenLabs `documentation_id`
- **THEN** the system removes the local document and chunks from pgvector and deletes the tracked ElevenLabs Knowledge Base entry with `force=true` without removing unmanaged dashboard documents

#### Scenario: Upstream ElevenLabs sync failure preserves local pgvector readiness
- **WHEN** local text extraction and pgvector indexing succeed during document upload, but enabled ElevenLabs Knowledge Base upload or indexing fails
- **THEN** the local document remains `ready` for backend modes while its hosted sync state is recorded as `failed` with a sanitized error and retry capability

#### Scenario: Application RAG synchronization is disabled
- **WHEN** application RAG synchronization is disabled and an administrator uploads, reindexes, or manually synchronizes local documents
- **THEN** the system preserves local pgvector operations and does not call ElevenLabs Knowledge Base or agent attachment synchronization for those documents

#### Scenario: Administrator turns application RAG synchronization off
- **WHEN** application RAG synchronization is enabled with application-managed hosted entries and an administrator disables it
- **THEN** the system detaches and removes only tracked application-managed entries, preserves local documents and unmanaged dashboard entries, and completes the setting change only after provider cleanup is verified

#### Scenario: Hosted RAG cleanup cannot be verified
- **WHEN** disabling application RAG synchronization cannot detach or remove a tracked application-managed entry
- **THEN** hosted save fails with a sanitized diagnostic, the previous synchronization setting remains active, and subsequent retry attempts target only the tracked application-managed resources
