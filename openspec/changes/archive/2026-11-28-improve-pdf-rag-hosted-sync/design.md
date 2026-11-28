# Design

## Context

See `proposal.md` for motivation and the delta specs for observable requirements.

The current PDF extractor scans raw and zlib-decompressed byte streams with regular expressions. It does not cover the range of PDF encodings and Unicode font maps used by real documents. The document table stores extracted text and chunks, but not uploaded source bytes, so failed documents cannot be reprocessed without the administrator supplying the source again. Hosted document synchronization already selects locally `ready` documents, and the saved RAG policy is a hard gate; the admin page currently offers a global sync action but no per-document action.

The server is built into a minimal Debian Bookworm container. PDF parsing and OCR therefore need explicit runtime packaging and bounded resource use.

## Goals / Non-Goals

**Goals:**
- Reliably extract UTF-8 text from compressed and Unicode PDFs, including Chinese content.
- OCR scanned PDFs locally when ordinary extraction yields no usable text.
- Allow an administrator to replace and reprocess a failed PDF without persistently retaining source binaries.
- Synchronize or retry one ready document from the admin UI, preserving unrelated managed and dashboard-owned entries.
- Enforce the persisted RAG-sync policy as a server-side hard opt-out for every manual and automatic document-sync path.

**Non-Goals:**
- Preserve exact PDF layout, tables, typography, or page appearance in RAG text.
- Send uploaded files to a third-party OCR service.
- Change document retention policy, chunking semantics, local retrieval, or ElevenLabs dashboard-owned knowledge.
- Allow a per-document action to bypass the global application RAG setting.

## Decisions

### Use Poppler for PDF text and rendering, with local Tesseract OCR fallback

Package Poppler utilities and Tesseract in the server container. Use Poppler's UTF-8 text extraction first; when it yields no usable text, render bounded pages and invoke Tesseract with installed English and Chinese language data. Keep OCR behind a small extraction boundary so document ingestion does not depend on PDF-specific details.

This replaces handwritten parsing of PDF operators, which cannot reliably handle compression, font encodings, and ToUnicode maps. A remote OCR API was considered but rejected because it would transmit uploaded documents to another service and add provider credentials, availability, and cost concerns. A pure-Go PDF parser alone would not meet the scanned-document requirement.

Invoke fixed executables with argument arrays rather than a shell. Use a private temporary directory, context deadlines, page and output-size limits, and cleanup on every exit path. If OCR tools are unavailable or fail, return a sanitized actionable extraction error and leave the document failed; never treat empty text as success. Document the required packages for local development as well as the production image.

### Replace a failed document source without durable binary storage

Add an authenticated administrator replacement-upload flow for failed PDF records. Validate the replacement as a PDF and run extraction/OCR and embedding through the existing ingestion path, retaining the record identity while atomically replacing its extracted text and chunks only after successful processing. Keep uploaded bytes in request memory or bounded temporary storage only; do not add a persistent original-file column or object store.

If replacement processing fails, retain the document record, mark it failed with an actionable sanitized diagnostic, and do not expose partial chunks as ready. This matches the current storage model, which persists extracted text rather than originals, and avoids a new durable sensitive-document store.

### Add targeted hosted document synchronization under the existing global policy

Add an admin-only endpoint for one document and reuse the existing synchronization/verification helpers. Before any provider request, load the saved config and require hosted mode, enabled application RAG synchronization, a locally `ready` document, and valid configured credentials/agent. When the global policy is disabled, both the global sync path and per-document endpoint must return a clear policy response without contacting ElevenLabs.

The targeted path creates or updates only the selected document's managed Knowledge Base entry and status, then patches/verifies the agent's Knowledge Base list while retaining other tracked application entries and unmanaged dashboard entries. Existing hash-based idempotency remains in use. The global sync remains available for full reconciliation, and automatic upload/reindex synchronization continues to honor the same persisted policy.

### Make the admin actions reflect authoritative state

Show actionable local extraction errors separately from hosted sync state. Add a replacement-upload action for failed PDFs and a per-document sync/retry action for ready documents. When application sync is disabled, display that the action is unavailable and direct the administrator to enable and save the setting in AI Agent settings; do not present a click that appears to sync while the server will intentionally do nothing.

Refresh the row's status after replacement or sync, and surface provider or OCR failure diagnostics only after existing sanitization. Keep the existing global sync control for reconciliation of all ready documents.

## Risks / Trade-offs

- [OCR increases image, CPU, and memory use] → Bound page count, rendering resolution, temporary disk use, output size, and execution time; reject work exceeding configured limits with a visible diagnostic.
- [OCR may misread Chinese text or complex layouts] → Install appropriate Chinese language data, prefer the PDF text layer whenever usable, and avoid claiming layout fidelity.
- [Poppler and Tesseract increase container size and patching responsibility] → Pin packages through the base distribution, keep them updated, and document the runtime dependency.
- [PDF tools may encounter malformed or hostile files] → Execute without a shell or elevated privileges, enforce timeouts and size/page limits, isolate temporary files, and return sanitized errors.
- [Targeted hosted updates could accidentally remove other knowledge entries] → Reconcile from the current agent configuration plus tracked application ownership; verify that unmanaged entries remain before reporting success.
- [A disabled policy can surprise an administrator expecting a manual override] → Keep the disabled state explicit in the UI and require enabling and saving the global setting before any local document sync.
- [Existing failed documents have no stored source bytes] → Require re-upload/replacement for recovery; do not claim that a metadata-only retry can re-extract the original PDF.

## Migration Plan

1. Add PDF/OCR runtime packages and extraction behavior, with configuration for processing limits and documented local prerequisites.
2. Deploy the server and admin UI changes without requiring a database migration unless implementation demonstrates that existing document fields cannot represent replacement/reprocessing state.
3. Administrators re-upload failed PDFs through the replacement action (or upload them again after deployment), then enable and save application RAG synchronization in AI Agent settings.
4. Administrators use the document row's Sync / retry action for one ready file or the existing global reconciliation action for all ready files. The server verifies each hosted update and preserves dashboard-managed content.
5. Rollback by reverting the application image and UI. Previously stored text/chunks and hosted entries remain governed by existing state; administrators can continue using global synchronization. Failed PDFs can be re-uploaded after forward-fixing.


