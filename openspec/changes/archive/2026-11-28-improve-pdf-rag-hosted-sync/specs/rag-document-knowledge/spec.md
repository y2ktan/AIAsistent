# Spec Delta

## MODIFIED Requirements

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

## ADDED Requirements

### Requirement: Failed PDF ingestion is recoverable
The system SHALL allow an administrator to replace and reprocess the source of a failed PDF without requiring the application to retain the original uploaded binary as durable storage.

#### Scenario: Administrator replaces a failed PDF source
- **WHEN** an administrator selects a failed PDF record and uploads its corrected or replacement source
- **THEN** the system reruns extraction, OCR when needed, chunking, and embedding for that record and displays the resulting ready or failed status

#### Scenario: Replacement upload is invalid
- **WHEN** an administrator replaces a failed PDF with an unsupported or oversized file
- **THEN** the system rejects the replacement without discarding the existing document record or reporting it as ready
