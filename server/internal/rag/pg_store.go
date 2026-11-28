package rag

import (
	"context"
	"errors"
	"sort"
	"strings"

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

func (s *PGStore) ReplaceDocumentChunks(ctx context.Context, docID string, chunks []DocumentChunk, extractedText string) error {
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
		SET status = 'ready', error_message = '', chunk_count = $2, extracted_text = $3,
		    elevenlabs_sync_status = 'not_synced', elevenlabs_sync_error = '', updated_at = NOW()
		WHERE id = $1::uuid
	`, docID, len(chunks), extractedText); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *PGStore) ReplaceFailedDocument(
	ctx context.Context,
	docID, filename, mimeType string,
	sizeBytes int64,
	sha256Hash string,
	chunks []DocumentChunk,
	extractedText string,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status, currentMimeType string
	if err := tx.QueryRow(ctx, `
		SELECT status, mime_type FROM documents WHERE id = $1::uuid FOR UPDATE
	`, docID).Scan(&status, &currentMimeType); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if status != "failed" || currentMimeType != "application/pdf" {
		return ErrDocumentNotFailed
	}

	if _, err := tx.Exec(ctx, `DELETE FROM document_chunks WHERE document_id = $1::uuid`, docID); err != nil {
		return err
	}
	for _, chunk := range chunks {
		if _, err := tx.Exec(ctx, `
			INSERT INTO document_chunks (document_id, chunk_index, content, token_count, embedding)
			VALUES ($1::uuid, $2, $3, $4, $5)
		`, docID, chunk.ChunkIndex, chunk.Content, chunk.TokenCount, pgvector.NewVector(chunk.Embedding)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE documents
		SET filename = $2, mime_type = $3, size_bytes = $4, sha256_hash = $5,
		    status = 'ready', error_message = '', chunk_count = $6, extracted_text = $7,
		    elevenlabs_sync_status = 'not_synced', elevenlabs_sync_error = '', updated_at = NOW()
		WHERE id = $1::uuid
	`, docID, filename, mimeType, sizeBytes, sha256Hash, len(chunks), extractedText); err != nil {
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
		SELECT id::text, title, filename, mime_type, size_bytes, sha256_hash, status, error_message, chunk_count,
		       uploaded_by::text, extracted_text, elevenlabs_doc_id, elevenlabs_synced_hash, elevenlabs_agent_id,
		       elevenlabs_sync_status, elevenlabs_sync_error, created_at, updated_at
		FROM documents WHERE id = $1::uuid
	`, docID).Scan(
		&d.ID, &d.Title, &d.Filename, &d.MimeType, &d.SizeBytes, &d.SHA256Hash,
		&d.Status, &d.ErrorMessage, &d.ChunkCount, &d.UploadedBy, &d.ExtractedText,
		&d.ElevenLabsDocID, &d.ElevenLabsSyncedHash, &d.ElevenLabsAgentID,
		&d.ElevenLabsSyncStatus, &d.ElevenLabsSyncError, &d.CreatedAt, &d.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

func (s *PGStore) ListDocuments(ctx context.Context) ([]Document, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, title, filename, mime_type, size_bytes, sha256_hash, status, error_message, chunk_count,
		       uploaded_by::text, elevenlabs_doc_id, elevenlabs_synced_hash, elevenlabs_agent_id,
		       elevenlabs_sync_status, elevenlabs_sync_error, created_at, updated_at
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
			&d.Status, &d.ErrorMessage, &d.ChunkCount, &d.UploadedBy, &d.ElevenLabsDocID,
			&d.ElevenLabsSyncedHash, &d.ElevenLabsAgentID, &d.ElevenLabsSyncStatus,
			&d.ElevenLabsSyncError, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	return docs, rows.Err()
}

func (s *PGStore) ListReadyDocumentsForHostedSync(ctx context.Context) ([]Document, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.id::text, d.title, d.filename, d.mime_type, d.size_bytes, d.sha256_hash, d.status,
		       d.error_message, d.chunk_count, d.uploaded_by::text, d.extracted_text,
		       d.elevenlabs_doc_id, d.elevenlabs_synced_hash, d.elevenlabs_agent_id,
		       d.elevenlabs_sync_status, d.elevenlabs_sync_error, d.created_at, d.updated_at,
		       COALESCE(dc.chunk_index, -1), COALESCE(dc.content, '')
		FROM documents d
		LEFT JOIN document_chunks dc ON dc.document_id = d.id
		WHERE d.status = 'ready'
		ORDER BY d.created_at, d.id, dc.chunk_index
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []Document
	var chunks []DocumentChunk
	currentID := ""
	appendCurrent := func() {
		if len(docs) == 0 {
			return
		}
		if docs[len(docs)-1].ExtractedText == "" {
			docs[len(docs)-1].ExtractedText = reconstructTextFromChunks(chunks)
		}
		chunks = nil
	}

	for rows.Next() {
		var d Document
		var chunkIndex int
		var content string
		if err := rows.Scan(
			&d.ID, &d.Title, &d.Filename, &d.MimeType, &d.SizeBytes, &d.SHA256Hash, &d.Status,
			&d.ErrorMessage, &d.ChunkCount, &d.UploadedBy, &d.ExtractedText,
			&d.ElevenLabsDocID, &d.ElevenLabsSyncedHash, &d.ElevenLabsAgentID,
			&d.ElevenLabsSyncStatus, &d.ElevenLabsSyncError, &d.CreatedAt, &d.UpdatedAt,
			&chunkIndex, &content,
		); err != nil {
			return nil, err
		}
		if d.ID != currentID {
			appendCurrent()
			docs = append(docs, d)
			currentID = d.ID
		}
		if chunkIndex >= 0 {
			chunks = append(chunks, DocumentChunk{ChunkIndex: chunkIndex, Content: content})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	appendCurrent()
	return docs, nil
}

func (s *PGStore) UpdateElevenLabsSyncState(
	ctx context.Context,
	docID, elevenLabsDocID, syncedHash, agentID, status, errMsg string,
) error {
	ct, err := s.pool.Exec(ctx, `
		UPDATE documents
		SET elevenlabs_doc_id = $2, elevenlabs_synced_hash = $3, elevenlabs_agent_id = $4,
		    elevenlabs_sync_status = $5, elevenlabs_sync_error = $6, updated_at = NOW()
		WHERE id = $1::uuid
	`, docID, elevenLabsDocID, syncedHash, agentID, status, errMsg)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func reconstructTextFromChunks(chunks []DocumentChunk) string {
	if len(chunks) == 0 {
		return ""
	}
	sort.SliceStable(chunks, func(i, j int) bool {
		return chunks[i].ChunkIndex < chunks[j].ChunkIndex
	})
	var result []rune
	for _, chunk := range chunks {
		next := []rune(chunk.Content)
		if len(next) == 0 {
			continue
		}
		if len(result) == 0 {
			result = append(result, next...)
			continue
		}
		overlap := matchingChunkOverlap(result, next)
		if overlap == 0 {
			result = append(result, '\n', '\n')
		}
		result = append(result, next[overlap:]...)
	}
	return strings.TrimSpace(string(result))
}

func matchingChunkOverlap(previous, next []rune) int {
	maxOverlap := 360
	minOverlap := 320
	if len(previous) < maxOverlap {
		maxOverlap = len(previous)
	}
	if len(next) < maxOverlap {
		maxOverlap = len(next)
	}
	for overlap := maxOverlap; overlap >= minOverlap; overlap-- {
		matches := true
		for i := 0; i < overlap; i++ {
			if previous[len(previous)-overlap+i] != next[i] {
				matches = false
				break
			}
		}
		if matches {
			return overlap
		}
	}
	return 0
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
