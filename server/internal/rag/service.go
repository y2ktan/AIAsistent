package rag

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

func (s *Service) ReindexWithText(ctx context.Context, docID string, embedder Embedder, chunksText []string) error {
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
	return s.store.ReplaceDocumentChunks(ctx, docID, dbChunks)
}

func (s *Service) indexDocumentData(ctx context.Context, doc *Document, docType DocType, data []byte, embedder Embedder) error {
	text, err := ExtractText(docType, data)
	if err != nil {
		_ = s.store.UpdateDocumentStatus(ctx, doc.ID, "failed", "text extraction failed", 0)
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
	return s.store.ReplaceDocumentChunks(ctx, doc.ID, dbChunks)
}

func (s *Service) Delete(ctx context.Context, docID string) error {
	return s.store.DeleteDocument(ctx, docID)
}

func (s *Service) List(ctx context.Context) ([]Document, error) {
	return s.store.ListDocuments(ctx)
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
