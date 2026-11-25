package rag

import (
	"context"
	"time"
)

type Document struct {
	ID                   string    `json:"id"`
	Title                string    `json:"title"`
	Filename             string    `json:"filename"`
	MimeType             string    `json:"mimeType"`
	SizeBytes            int64     `json:"sizeBytes"`
	SHA256Hash           string    `json:"sha256Hash"`
	Status               string    `json:"status"`
	ErrorMessage         string    `json:"errorMessage,omitempty"`
	ChunkCount           int       `json:"chunkCount"`
	UploadedBy           *string   `json:"uploadedBy,omitempty"`
	ExtractedText        string    `json:"-"`
	ElevenLabsDocID      string    `json:"-"`
	ElevenLabsSyncedHash string    `json:"-"`
	ElevenLabsAgentID    string    `json:"-"`
	ElevenLabsSyncStatus string    `json:"elevenLabsSyncStatus"`
	ElevenLabsSyncError  string    `json:"elevenLabsSyncError,omitempty"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

type DocumentChunk struct {
	ID         string
	DocumentID string
	ChunkIndex int
	Content    string
	TokenCount int
	Embedding  []float32
}

type RetrievedChunk struct {
	DocumentID    string  `json:"documentId"`
	DocumentTitle string  `json:"documentTitle"`
	ChunkIndex    int     `json:"chunkIndex"`
	Content       string  `json:"content"`
	Similarity    float64 `json:"similarity"`
}

type Store interface {
	CreateDocument(ctx context.Context, doc *Document) error
	UpdateDocumentStatus(ctx context.Context, docID, status, errMsg string, chunkCount int) error
	ReplaceDocumentChunks(ctx context.Context, docID string, chunks []DocumentChunk, extractedText string) error
	DeleteDocument(ctx context.Context, docID string) error
	GetDocument(ctx context.Context, docID string) (*Document, error)
	ListDocuments(ctx context.Context) ([]Document, error)
	ListReadyDocumentsForHostedSync(ctx context.Context) ([]Document, error)
	UpdateElevenLabsSyncState(ctx context.Context, docID, elevenLabsDocID, syncedHash, agentID, status, errMsg string) error
	SearchSimilar(ctx context.Context, queryVec []float32, topK int, minSimilarity float64) ([]RetrievedChunk, error)
}
