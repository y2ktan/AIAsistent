# Spec Delta

## MODIFIED Requirements

### Requirement: RAG document management workspace
The system SHALL provide an administrative document panel displaying local ingestion status, chunk counts, upload controls, reindex triggers, and deletion actions, actionable extraction errors and source replacement for failed PDFs, and, when an ElevenLabs hosted agent is configured, per-document hosted Knowledge Base sync status and sync/retry actions for locally `ready` documents when application RAG synchronization is enabled.

#### Scenario: Administrator monitors document ingestion progress
- **WHEN** an administrator uploads one or more documents in `/admin`
- **THEN** the document table reflects status transitions (`processing`, `ready`, or `failed`) and total indexed chunk count

#### Scenario: Administrator sees and recovers a PDF extraction failure
- **WHEN** a PDF upload fails during extraction or OCR
- **THEN** the document panel displays an actionable sanitized reason and allows an administrator to replace and reprocess the failed PDF source

#### Scenario: Administrator monitors and retries hosted Knowledge Base sync
- **WHEN** an administrator views `/admin` with a configured hosted agent or retries synchronization for a ready document after an upstream ElevenLabs failure
- **THEN** the document panel displays each document's hosted sync state without altering its local pgvector status and retries synchronization server-side

#### Scenario: Administrator synchronizes one ready document
- **WHEN** hosted mode is active, application RAG synchronization is enabled, and an administrator requests sync or retry for one locally `ready` document
- **THEN** the server synchronizes only that document to the configured hosted agent and the panel reports its resulting sync state

#### Scenario: Application RAG synchronization is disabled
- **WHEN** application RAG synchronization is disabled and an administrator views or requests document synchronization
- **THEN** the panel clearly identifies the saved policy as disabled, directs the administrator to enable and save it in AI Agent settings, and does not send an ElevenLabs document synchronization request

#### Scenario: Administrator requests sync for a document that is not ready
- **WHEN** an administrator requests hosted synchronization for a document whose local status is not `ready`
- **THEN** the system rejects the request with an actionable status and does not send that document to ElevenLabs
