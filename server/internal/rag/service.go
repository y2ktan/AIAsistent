package rag

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"aegis-voice/server/internal/security"
)

type Service struct {
	store    Store
	maxBytes int64
}

func NewService(store Store, maxBytes int64) *Service {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxUploadBytes
	}
	return &Service{store: store, maxBytes: maxBytes}
}

func (s *Service) Ingest(ctx context.Context, title, filename string, data []byte, uploadedBy string, embedder Embedder) (*Document, error) {
	docType, mimeType, err := ValidateUpload(filename, data, s.maxBytes)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(title) == "" {
		title = strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	}

	sum := sha256.Sum256(data)
	doc := &Document{
		Title:      title,
		Filename:   filepath.Base(filename),
		MimeType:   mimeType,
		SizeBytes:  int64(len(data)),
		SHA256Hash: hex.EncodeToString(sum[:]),
		Status:     "processing",
		UploadedBy: &uploadedBy,
	}
	if err := s.store.CreateDocument(ctx, doc); err != nil {
		return nil, err
	}

	if err := s.indexDocumentData(ctx, doc, docType, data, embedder); err != nil {
		return doc, err
	}
	return s.store.GetDocument(ctx, doc.ID)
}

func (s *Service) ReplaceFailedPDF(
	ctx context.Context,
	docID, filename string,
	data []byte,
	embedder Embedder,
) (*Document, error) {
	docType, mimeType, err := ValidateUpload(filename, data, s.maxBytes)
	if err != nil {
		return nil, err
	}
	if docType != DocTypePDF {
		return nil, ErrUnsupportedType
	}
	doc, err := s.store.GetDocument(ctx, docID)
	if err != nil {
		return nil, err
	}
	if doc.Status != "failed" || doc.MimeType != "application/pdf" {
		return nil, ErrDocumentNotFailed
	}

	text, err := ExtractText(docType, data)
	if err != nil {
		return s.failDocumentReplacement(ctx, doc, err)
	}
	chunks := ChunkText(text, 2400, 360)
	if len(chunks) == 0 {
		return s.failDocumentReplacement(ctx, doc, ErrExtractionFailed)
	}
	if embedder == nil {
		return s.failDocumentReplacement(ctx, doc, errors.New("embedding provider not configured"))
	}
	chunkTexts := make([]string, len(chunks))
	for i, chunk := range chunks {
		chunkTexts[i] = chunk.Content
	}
	vectors, err := embedder.Embed(ctx, chunkTexts)
	if err != nil {
		return s.failDocumentReplacement(ctx, doc, err)
	}
	if len(vectors) != len(chunks) {
		return s.failDocumentReplacement(ctx, doc, errors.New("embedding provider returned an invalid vector count"))
	}
	dbChunks := make([]DocumentChunk, len(chunks))
	for i, chunk := range chunks {
		dbChunks[i] = DocumentChunk{
			DocumentID: docID,
			ChunkIndex: chunk.Index,
			Content:    chunk.Content,
			TokenCount: chunk.TokenCount,
			Embedding:  vectors[i],
		}
	}

	sum := sha256.Sum256(data)
	if err := s.store.ReplaceFailedDocument(
		ctx, docID, filepath.Base(filename), mimeType, int64(len(data)), hex.EncodeToString(sum[:]),
		dbChunks, text,
	); err != nil {
		return nil, err
	}
	return s.store.GetDocument(ctx, docID)
}

func (s *Service) failDocumentReplacement(ctx context.Context, doc *Document, failure error) (*Document, error) {
	if err := s.store.UpdateDocumentStatus(
		ctx, doc.ID, "failed", security.RedactString(failure.Error()), doc.ChunkCount,
	); err != nil {
		return nil, errors.Join(failure, fmt.Errorf("failed to record replacement failure: %w", err))
	}
	updated, err := s.store.GetDocument(ctx, doc.ID)
	if err != nil {
		return nil, errors.Join(failure, fmt.Errorf("failed to reload document after replacement failure: %w", err))
	}
	return updated, failure
}

func (s *Service) ReindexWithText(ctx context.Context, docID string, embedder Embedder, chunksText []string) error {
	chunks := make([]DocumentChunk, len(chunksText))
	for i, text := range chunksText {
		chunks[i] = DocumentChunk{ChunkIndex: i, Content: text}
	}
	return s.reindexWithText(ctx, docID, embedder, chunksText, reconstructTextFromChunks(chunks))
}

func (s *Service) reindexWithText(ctx context.Context, docID string, embedder Embedder, chunksText []string, extractedText string) error {
	if embedder == nil {
		_ = s.store.UpdateDocumentStatus(ctx, docID, "failed", "embedding provider not configured", 0)
		return errors.New("embedding provider not configured")
	}
	vecs, err := embedder.Embed(ctx, chunksText)
	if err != nil {
		_ = s.store.UpdateDocumentStatus(ctx, docID, "failed", security.RedactString(err.Error()), 0)
		return err
	}
	var dbChunks []DocumentChunk
	for i, txt := range chunksText {
		dbChunks = append(dbChunks, DocumentChunk{
			DocumentID: docID,
			ChunkIndex: i,
			Content:    txt,
			TokenCount: estimateTokens(txt),
			Embedding:  vecs[i],
		})
	}
	return s.store.ReplaceDocumentChunks(ctx, docID, dbChunks, extractedText)
}

func (s *Service) Reindex(ctx context.Context, docID string, embedder Embedder) error {
	doc, err := s.store.GetDocument(ctx, docID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(doc.ExtractedText) == "" {
		return ErrExtractionFailed
	}
	chunks := ChunkText(doc.ExtractedText, 2400, 360)
	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = chunk.Content
	}
	return s.reindexWithText(ctx, docID, embedder, texts, doc.ExtractedText)
}

func (s *Service) indexDocumentData(ctx context.Context, doc *Document, docType DocType, data []byte, embedder Embedder) error {
	text, err := ExtractText(docType, data)
	if err != nil {
		_ = s.store.UpdateDocumentStatus(ctx, doc.ID, "failed", security.RedactString(err.Error()), 0)
		return err
	}
	chunks := ChunkText(text, 2400, 360)
	if len(chunks) == 0 {
		_ = s.store.UpdateDocumentStatus(ctx, doc.ID, "failed", "document contained no extractable text", 0)
		return ErrExtractionFailed
	}

	if embedder == nil {
		_ = s.store.UpdateDocumentStatus(ctx, doc.ID, "failed", "embedding provider not configured", 0)
		return errors.New("embedding provider not configured")
	}

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}
	vecs, err := embedder.Embed(ctx, texts)
	if err != nil {
		_ = s.store.UpdateDocumentStatus(ctx, doc.ID, "failed", security.RedactString(err.Error()), 0)
		return err
	}

	dbChunks := make([]DocumentChunk, len(chunks))
	for i, c := range chunks {
		dbChunks[i] = DocumentChunk{
			DocumentID: doc.ID,
			ChunkIndex: c.Index,
			Content:    c.Content,
			TokenCount: c.TokenCount,
			Embedding:  vecs[i],
		}
	}
	return s.store.ReplaceDocumentChunks(ctx, doc.ID, dbChunks, text)
}

func (s *Service) Delete(ctx context.Context, docID string) error {
	return s.store.DeleteDocument(ctx, docID)
}

func (s *Service) List(ctx context.Context) ([]Document, error) {
	return s.store.ListDocuments(ctx)
}

func (s *Service) GetDocument(ctx context.Context, docID string) (*Document, error) {
	return s.store.GetDocument(ctx, docID)
}

func (s *Service) ListReadyDocumentsForHostedSync(ctx context.Context) ([]Document, error) {
	return s.store.ListReadyDocumentsForHostedSync(ctx)
}

func (s *Service) UpdateElevenLabsSyncState(
	ctx context.Context,
	docID, elevenLabsDocID, syncedHash, agentID, status, errMsg string,
) error {
	return s.store.UpdateElevenLabsSyncState(ctx, docID, elevenLabsDocID, syncedHash, agentID, status, errMsg)
}

func (s *Service) Retrieve(ctx context.Context, query string, topK int, minSimilarity float64, embedder Embedder) ([]RetrievedChunk, error) {
	if strings.TrimSpace(query) == "" || embedder == nil {
		return nil, nil
	}
	vecs, err := embedder.Embed(ctx, []string{query})
	if err != nil || len(vecs) == 0 {
		return nil, err
	}
	return s.store.SearchSimilar(ctx, vecs[0], topK, minSimilarity)
}
