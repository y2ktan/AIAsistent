# Proposal

## Why

PDF uploads can be stored with a generic failed status because the current extractor supports only a narrow subset of PDF text encodings and compressed streams, with no OCR fallback. Separately, application-to-ElevenLabs RAG synchronization is independently disabled by policy, but the admin workflow does not make the required enable-then-sync steps sufficiently clear or offer a focused per-document retry.

## What Changes

- Improve PDF text extraction for compressed and Unicode content, including Chinese text, and provide OCR fallback for scanned or image-only PDFs with bounded, secure processing.
- Give failed PDF ingestion actionable status and recovery guidance, including an admin path to replace/re-upload a failed document source without retaining the original binary indefinitely.
- Add clear per-document hosted sync and retry controls for locally `ready` documents, with visible progress, success, and sanitized failure states.
- Keep the saved application RAG synchronization setting as a hard opt-out: no manual or per-document action may contact ElevenLabs while it is disabled. Explain how to enable and save the setting before retrying.
- Preserve local pgvector data, unmanaged ElevenLabs dashboard knowledge, and existing document lifecycle behavior.

## Capabilities

### New Capabilities

### Modified Capabilities
- `rag-document-knowledge`: Improve PDF extraction with OCR fallback and make extraction failures recoverable and understandable.
- `admin-governance-console`: Provide clear ingestion diagnostics and per-document hosted sync/retry controls governed by the saved RAG policy.
- `elevenlabs-hosted-conversation`: Define targeted synchronization of ready application documents to the configured hosted agent while preserving dashboard-managed knowledge and respecting the global opt-out.

## Impact

- Backend: `server/internal/rag` PDF extraction, OCR processing, document reprocessing, and document sync service integration.
- API: authenticated admin document replacement and targeted hosted-sync/retry operations, with eligibility checks and sanitized errors.
- Admin UI: failed-ingestion recovery and per-document hosted status/actions, including a clear disabled-policy explanation.
- Dependencies and operations: a PDF parser and OCR engine/runtime may be needed; selection must account for CJK support, bounded resource use, deployment packaging, and privacy.
- Tests and docs: cover text-based and scanned PDFs, extraction failures, sync policy combinations, and preservation of unmanaged hosted documents.
