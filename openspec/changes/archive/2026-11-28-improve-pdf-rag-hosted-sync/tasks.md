# Tasks

## 1. PDF Extraction and OCR

- [x] 1.1 Package the PDF text/rendering and OCR runtime tools with the server image, include English and Chinese language data, document local-development prerequisites, and verify the server image builds with the required executables available.
- [x] 1.2 Replace regex-only PDF parsing with UTF-8 text extraction and bounded local OCR fallback; cover compressed streams, Unicode/Chinese text, scanned PDFs, empty output, limits, and tool failures with focused `go test ./internal/rag` cases.

## 2. Failed Document Recovery

- [x] 2.1 Add a replacement-ingestion path for failed PDF records that validates the new source and atomically updates extracted text and chunks only after successful extraction/OCR and embedding; verify failure leaves the record non-ready and focused RAG service/store tests pass.
- [x] 2.2 Add an authenticated admin replacement-upload endpoint and recovery controls with actionable sanitized extraction status; verify invalid/oversized replacements and successful reprocessing in API tests, and document how to recover a failed upload.

## 3. Targeted ElevenLabs Document Sync

- [x] 3.1 Add a per-document hosted sync/retry endpoint that requires hosted mode, saved RAG sync enabled, and local `ready` status before provider access; verify disabled policy and non-ready documents make no provider calls, and dashboard-managed entries survive successful synchronization in API tests.
- [x] 3.2 Add per-document sync/retry controls and clear disabled-policy guidance in the admin page, refresh row status after actions, and verify success, indexing, failure, and disabled states with focused frontend tests.

## 4. Integration Verification

- [x] 4.1 Verify the end-to-end admin flow from PDF upload or replacement through local `ready` status and single-document ElevenLabs sync, including strict no-provider-call behavior when application RAG sync is disabled; run relevant Go and web test suites and `openspec validate --change improve-pdf-rag-hosted-sync --strict`.
