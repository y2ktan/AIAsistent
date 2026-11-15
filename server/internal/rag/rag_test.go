package rag

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type memRAGStore struct {
	mu     sync.Mutex
	docs   map[string]*Document
	chunks map[string][]DocumentChunk
	seq    int
}

func newMemRAGStore() *memRAGStore {
	return &memRAGStore{
		docs:   make(map[string]*Document),
		chunks: make(map[string][]DocumentChunk),
	}
}

func (m *memRAGStore) CreateDocument(_ context.Context, doc *Document) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	doc.ID = "doc-" + string(rune('0'+m.seq))
	doc.CreatedAt = time.Now()
	doc.UpdatedAt = time.Now()
	cp := *doc
	m.docs[doc.ID] = &cp
	return nil
}

func (m *memRAGStore) UpdateDocumentStatus(_ context.Context, docID, status, errMsg string, chunkCount int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.docs[docID]
	if !ok {
		return ErrNotFound
	}
	d.Status = status
	d.ErrorMessage = errMsg
	d.ChunkCount = chunkCount
	return nil
}

func (m *memRAGStore) ReplaceDocumentChunks(_ context.Context, docID string, chunks []DocumentChunk) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.docs[docID]
	if !ok {
		return ErrNotFound
	}
	m.chunks[docID] = append([]DocumentChunk(nil), chunks...)
	d.Status = "ready"
	d.ErrorMessage = ""
	d.ChunkCount = len(chunks)
	return nil
}

func (m *memRAGStore) DeleteDocument(_ context.Context, docID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.docs[docID]; !ok {
		return ErrNotFound
	}
	delete(m.docs, docID)
	delete(m.chunks, docID)
	return nil
}

func (m *memRAGStore) GetDocument(_ context.Context, docID string) (*Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.docs[docID]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *d
	return &cp, nil
}

func (m *memRAGStore) ListDocuments(_ context.Context) ([]Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Document
	for _, d := range m.docs {
		out = append(out, *d)
	}
	return out, nil
}

func cosineSim(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		if i >= len(b) {
			break
		}
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func (m *memRAGStore) SearchSimilar(_ context.Context, queryVec []float32, topK int, minSimilarity float64) ([]RetrievedChunk, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var results []RetrievedChunk
	for docID, d := range m.docs {
		if d.Status != "ready" {
			continue
		}
		for _, c := range m.chunks[docID] {
			sim := cosineSim(queryVec, c.Embedding)
			if sim >= minSimilarity {
				results = append(results, RetrievedChunk{
					DocumentID:    docID,
					DocumentTitle: d.Title,
					ChunkIndex:    c.ChunkIndex,
					Content:       c.Content,
					Similarity:    sim,
				})
			}
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})
	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

type mockEmbedder struct {
	fail bool
}

func (e *mockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if e.fail {
		return nil, errors.New("upstream error with Bearer sk-secret1234567890")
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		lower := strings.ToLower(t)
		v := []float32{0.01, 0.01, 0.01}
		if strings.Contains(lower, "webrtc") || strings.Contains(lower, "pion") {
			v = []float32{1, 0, 0}
		} else if strings.Contains(lower, "database") || strings.Contains(lower, "pgvector") {
			v = []float32{0, 1, 0}
		}
		out[i] = v
	}
	return out, nil
}

func buildTestDOCX(t *testing.T, text string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:body></w:document>`))
	_ = zw.Close()
	return buf.Bytes()
}

func TestValidateUploadRejectsSpoofedAndOversized(t *testing.T) {
	// Spoofed PDF (ELF header renamed to .pdf)
	if _, _, err := ValidateUpload("evil.pdf", []byte("\x7fELF\x01\x01\x01"), 1024); !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("expected ErrUnsupportedType for spoofed pdf, got %v", err)
	}
	// Spoofed DOCX (non-zip)
	if _, _, err := ValidateUpload("evil.docx", []byte("not a zip"), 1024); !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("expected ErrUnsupportedType for spoofed docx, got %v", err)
	}
	// Binary null bytes in .txt
	if _, _, err := ValidateUpload("bin.txt", []byte("hello\x00world"), 1024); !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("expected ErrUnsupportedType for binary txt, got %v", err)
	}
	// Oversized
	if _, _, err := ValidateUpload("big.md", []byte("12345678901"), 10); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("expected ErrPayloadTooLarge, got %v", err)
	}
	// Valid PDF
	if dt, _, err := ValidateUpload("ok.pdf", []byte("%PDF-1.7\n"), 1024); err != nil || dt != DocTypePDF {
		t.Fatalf("expected valid pdf, got dt=%v err=%v", dt, err)
	}
	// Valid DOCX
	docx := buildTestDOCX(t, "Hello DOCX")
	if dt, _, err := ValidateUpload("ok.docx", docx, 10240); err != nil || dt != DocTypeDOCX {
		t.Fatalf("expected valid docx, got dt=%v err=%v", dt, err)
	}
}

func TestExtractionAndChunking(t *testing.T) {
	docx := buildTestDOCX(t, "Aegis Voice uses Pion WebRTC.")
	txt, err := ExtractText(DocTypeDOCX, docx)
	if err != nil || !strings.Contains(txt, "Pion WebRTC") {
		t.Fatalf("DOCX extraction failed: %q err=%v", txt, err)
	}

	pdf := []byte("%PDF-1.4\n1 0 obj\n<< /Length 44 >>\nstream\nBT /F1 12 Tf 72 712 Td (Pion WebRTC Guide) Tj ET\nendstream\nendobj")
	ptxt, err := ExtractText(DocTypePDF, pdf)
	if err != nil || !strings.Contains(ptxt, "Pion WebRTC Guide") {
		t.Fatalf("PDF extraction failed: %q err=%v", ptxt, err)
	}

	longText := strings.Repeat("Sentence one about Pion WebRTC. ", 200)
	chunks := ChunkText(longText, 500, 100)
	if len(chunks) < 5 {
		t.Fatalf("expected multiple overlapping chunks, got %d", len(chunks))
	}
}

func TestIngestRetrieveAndDelete(t *testing.T) {
	store := newMemRAGStore()
	svc := NewService(store, DefaultMaxUploadBytes)
	emb := &mockEmbedder{}

	doc, err := svc.Ingest(context.Background(), "Pion Architecture", "pion.md", []byte("# Pion\nWe use Pion WebRTC for media."), "admin-1", emb)
	if err != nil || doc.Status != "ready" {
		t.Fatalf("expected ready document, got doc=%+v err=%v", doc, err)
	}

	// Retrieve matching query
	hits, err := svc.Retrieve(context.Background(), "How does Pion WebRTC work?", 3, 0.5, emb)
	if err != nil || len(hits) != 1 || hits[0].DocumentTitle != "Pion Architecture" {
		t.Fatalf("expected 1 matching citation, got %+v err=%v", hits, err)
	}

	// Query below threshold returns empty
	empty, err := svc.Retrieve(context.Background(), "unrelated topic xyz", 3, 0.9, emb)
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected 0 results below threshold, got %+v", empty)
	}

	// Delete removes document from future retrieval
	if err := svc.Delete(context.Background(), doc.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	afterDel, err := svc.Retrieve(context.Background(), "Pion WebRTC", 3, 0.5, emb)
	if err != nil || len(afterDel) != 0 {
		t.Fatalf("expected 0 results after deletion, got %+v", afterDel)
	}

	// Embedding failure marks document failed with redacted message
	badEmb := &mockEmbedder{fail: true}
	failDoc, err := svc.Ingest(context.Background(), "Broken", "broken.txt", []byte("some text"), "admin-1", badEmb)
	if err == nil {
		t.Fatal("expected error when embedder fails")
	}
	stored, _ := store.GetDocument(context.Background(), failDoc.ID)
	if stored.Status != "failed" || strings.Contains(stored.ErrorMessage, "secret1234567890") {
		t.Fatalf("expected failed status with redacted error, got %+v", stored)
	}
}
