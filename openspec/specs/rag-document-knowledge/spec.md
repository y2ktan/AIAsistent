# rag-document-knowledge Specification

## Purpose

Manages secure document ingestion, text extraction, semantic chunking, vector indexing, similarity retrieval, and knowledge lifecycle operations for grounding assistant responses.

## Requirements

### Requirement: Validated multi-format document ingestion
The system SHALL accept administrator uploads of supported text documents (PDF, Markdown, plain text, and DOCX), verify file content signatures and size limits, extract compressed and Unicode PDF text, use OCR when a PDF has no usable text layer, and index extracted chunks as vector embeddings.

#### Scenario: Administrator uploads a valid PDF document
- **WHEN** an administrator uploads a valid text-based PDF within the configured size limit
- **THEN** the system extracts its readable text, records the document, generates vector embeddings for its chunks, and transitions its status from `processing` to `ready`

#### Scenario: PDF contains compressed or Chinese text
- **WHEN** an administrator uploads a valid PDF whose text uses compressed streams or supported Unicode characters, including Chinese
- **THEN** the system extracts the readable text without silently treating supported text as empty

#### Scenario: Scanned PDF uses OCR fallback
- **WHEN** an administrator uploads a valid PDF with no usable text layer and OCR is available within configured resource limits
- **THEN** the system OCRs its pages, indexes the recognized text, and transitions the document to `ready`

#### Scenario: PDF extraction and OCR both produce no usable text
- **WHEN** an administrator uploads a PDF from which neither text extraction nor configured OCR can produce usable text
- **THEN** the system marks the document `failed` with an actionable sanitized diagnostic and does not create empty chunks or report successful ingestion

#### Scenario: Spoofed or oversized file upload rejected
- **WHEN** a file exceeds the size threshold or its binary magic bytes do not match an allowed document type
- **THEN** the system rejects the upload with HTTP `400 Bad Request` or `413 Payload Too Large` without parsing or storing the file

### Requirement: Failed PDF ingestion is recoverable
The system SHALL allow an administrator to replace and reprocess the source of a failed PDF without requiring the application to retain the original uploaded binary as durable storage.

#### Scenario: Administrator replaces a failed PDF source
- **WHEN** an administrator selects a failed PDF record and uploads its corrected or replacement source
- **THEN** the system reruns extraction, OCR when needed, chunking, and embedding for that record and displays the resulting ready or failed status

#### Scenario: Replacement upload is invalid
- **WHEN** an administrator replaces a failed PDF with an unsupported or oversized file
- **THEN** the system rejects the replacement without discarding the existing document record or reporting it as ready

### Requirement: Semantic similarity retrieval with citation metadata
The system SHALL retrieve the highest-relevance indexed document chunks exceeding a configurable similarity threshold for a query and attach source title and chunk identifiers.

#### Scenario: Query matches indexed knowledge chunks
- **WHEN** the conversational pipeline submits a semantic retrieval query matching indexed documents
- **THEN** the system returns the top-ranked chunks ordered by vector similarity along with document name and section metadata

#### Scenario: Query has no relevant matches above threshold
- **WHEN** no indexed chunk meets the minimum similarity score for a query
- **THEN** the system returns an empty retrieval set so the assistant does not fabricate document citations

### Requirement: Atomic document deletion and reindexing
The system SHALL allow administrators to delete or reindex any ingested document and SHALL immediately remove deleted chunks from future retrieval results.

#### Scenario: Administrator deletes an indexed document
- **WHEN** an administrator deletes a previously ingested document
- **THEN** the system removes the document record and all associated vector chunks so subsequent queries never retrieve its content

#### Scenario: Embedding failure marks document status safely
- **WHEN** embedding generation fails during ingestion due to an upstream provider error
- **THEN** the system marks the document status as `failed` with a sanitized diagnostic reason and exposes a retry/reindex action

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
