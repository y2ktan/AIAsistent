package rag

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

var ErrNotFound = errors.New("document not found")

type PGStore struct {
	pool *pgxpool.Pool
}

func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

func (s *PGStore) CreateDocument(ctx context.Context, doc *Document) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO documents (title, filename, mime_type, size_bytes, sha256_hash, status, uploaded_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7::uuid)
		RETURNING id::text, created_at, updated_at
	`, doc.Title, doc.Filename, doc.MimeType, doc.SizeBytes, doc.SHA256Hash, doc.Status, doc.UploadedBy).
		Scan(&doc.ID, &doc.CreatedAt, &doc.UpdatedAt)
}

func (s *PGStore) UpdateDocumentStatus(ctx context.Context, docID, status, errMsg string, chunkCount int) error {
	ct, err := s.pool.Exec(ctx, `
		UPDATE documents
		SET status = $2, error_message = $3, chunk_count = $4, updated_at = NOW()
		WHERE id = $1::uuid
	`, docID, status, errMsg, chunkCount)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGStore) ReplaceDocumentChunks(ctx context.Context, docID string, chunks []DocumentChunk) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM document_chunks WHERE document_id = $1::uuid`, docID); err != nil {
		return err
	}

	for _, c := range chunks {
		vec := pgvector.NewVector(c.Embedding)
		if _, err := tx.Exec(ctx, `
			INSERT INTO document_chunks (document_id, chunk_index, content, token_count, embedding)
			VALUES ($1::uuid, $2, $3, $4, $5)
		`, docID, c.ChunkIndex, c.Content, c.TokenCount, vec); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE documents
		SET status = 'ready', error_message = '', chunk_count = $2, updated_at = NOW()
		WHERE id = $1::uuid
	`, docID, len(chunks)); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *PGStore) DeleteDocument(ctx context.Context, docID string) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM documents WHERE id = $1::uuid`, docID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGStore) GetDocument(ctx context.Context, docID string) (*Document, error) {
	d := &Document{}
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, title, filename, mime_type, size_bytes, sha256_hash, status, error_message, chunk_count, uploaded_by::text, created_at, updated_at
		FROM documents WHERE id = $1::uuid
	`, docID).Scan(
		&d.ID, &d.Title, &d.Filename, &d.MimeType, &d.SizeBytes, &d.SHA256Hash,
		&d.Status, &d.ErrorMessage, &d.ChunkCount, &d.UploadedBy, &d.CreatedAt, &d.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

func (s *PGStore) ListDocuments(ctx context.Context) ([]Document, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, title, filename, mime_type, size_bytes, sha256_hash, status, error_message, chunk_count, uploaded_by::text, created_at, updated_at
		FROM documents ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []Document
	for rows.Next() {
		var d Document
		if err := rows.Scan(
			&d.ID, &d.Title, &d.Filename, &d.MimeType, &d.SizeBytes, &d.SHA256Hash,
			&d.Status, &d.ErrorMessage, &d.ChunkCount, &d.UploadedBy, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	return docs, rows.Err()
}

func (s *PGStore) SearchSimilar(ctx context.Context, queryVec []float32, topK int, minSimilarity float64) ([]RetrievedChunk, error) {
	if topK <= 0 {
		topK = 5
	}
	vec := pgvector.NewVector(queryVec)
	rows, err := s.pool.Query(ctx, `
		SELECT d.id::text, d.title, dc.chunk_index, dc.content, (1 - (dc.embedding <=> $1)) AS similarity
		FROM document_chunks dc
		JOIN documents d ON d.id = dc.document_id
		WHERE d.status = 'ready'
		  AND dc.embedding IS NOT NULL
		  AND (1 - (dc.embedding <=> $1)) >= $2
		ORDER BY dc.embedding <=> $1 ASC
		LIMIT $3
	`, vec, minSimilarity, topK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []RetrievedChunk
	for rows.Next() {
		var r RetrievedChunk
		if err := rows.Scan(&r.DocumentID, &r.DocumentTitle, &r.ChunkIndex, &r.Content, &r.Similarity); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}
