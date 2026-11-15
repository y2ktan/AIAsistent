package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PGStore struct {
	pool *pgxpool.Pool
}

func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

func (s *PGStore) CountUsers(ctx context.Context) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

func (s *PGStore) BootstrapFirstAdmin(ctx context.Context, email, passwordHash string) (*User, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, ErrBootstrapDisabled
	}

	u := &User{}
	err = tx.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, role, is_active)
		VALUES ($1, $2, 'admin', TRUE)
		RETURNING id::text, email, password_hash, role, is_active, created_at, updated_at
	`, email, passwordHash).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert bootstrap admin: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *PGStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	u := &User{}
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, email, password_hash, role, is_active, created_at, updated_at
		FROM users WHERE LOWER(email) = LOWER($1)
	`, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *PGStore) GetUserByID(ctx context.Context, id string) (*User, error) {
	u := &User{}
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, email, password_hash, role, is_active, created_at, updated_at
		FROM users WHERE id = $1::uuid
	`, id).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *PGStore) CreateSession(ctx context.Context, sess *Session) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO sessions (user_id, token_hash, csrf_token, ip_address, user_agent, last_seen_at, expires_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7)
		RETURNING id::text, created_at
	`, sess.UserID, sess.TokenHash, sess.CSRFToken, sess.IPAddress, sess.UserAgent, sess.LastSeenAt, sess.ExpiresAt).
		Scan(&sess.ID, &sess.CreatedAt)
}

func (s *PGStore) GetActiveSessionByTokenHash(ctx context.Context, tokenHash []byte) (*Session, *User, error) {
	sess := &Session{}
	u := &User{}
	err := s.pool.QueryRow(ctx, `
		SELECT s.id::text, s.user_id::text, s.token_hash, s.csrf_token, s.ip_address, s.user_agent,
		       s.last_seen_at, s.expires_at, s.revoked_at, s.created_at,
		       u.id::text, u.email, u.password_hash, u.role, u.is_active, u.created_at, u.updated_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1
		  AND s.revoked_at IS NULL
		  AND s.expires_at > NOW()
		  AND u.is_active = TRUE
	`, tokenHash).Scan(
		&sess.ID, &sess.UserID, &sess.TokenHash, &sess.CSRFToken, &sess.IPAddress, &sess.UserAgent,
		&sess.LastSeenAt, &sess.ExpiresAt, &sess.RevokedAt, &sess.CreatedAt,
		&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	return sess, u, err
}

func (s *PGStore) TouchSession(ctx context.Context, sessionID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET last_seen_at = NOW() WHERE id = $1::uuid`, sessionID)
	return err
}

func (s *PGStore) RevokeSession(ctx context.Context, sessionID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET revoked_at = NOW() WHERE id = $1::uuid AND revoked_at IS NULL`, sessionID)
	return err
}

func (s *PGStore) RevokeAllUserSessions(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET revoked_at = NOW() WHERE user_id = $1::uuid AND revoked_at IS NULL`, userID)
	return err
}

func (s *PGStore) CreateSignalingToken(ctx context.Context, tok *SignalingToken) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO signaling_tokens (user_id, session_id, token_hash, expires_at)
		VALUES ($1::uuid, $2::uuid, $3, $4)
		RETURNING id::text, created_at
	`, tok.UserID, tok.SessionID, tok.TokenHash, tok.ExpiresAt).Scan(&tok.ID, &tok.CreatedAt)
}

func (s *PGStore) ConsumeSignalingToken(ctx context.Context, tokenHash []byte) (*SignalingToken, *User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tok := &SignalingToken{}
	err = tx.QueryRow(ctx, `
		UPDATE signaling_tokens st
		SET consumed_at = NOW()
		FROM sessions s, users u
		WHERE st.token_hash = $1
		  AND st.consumed_at IS NULL
		  AND st.expires_at > NOW()
		  AND s.id = st.session_id
		  AND s.revoked_at IS NULL
		  AND s.expires_at > NOW()
		  AND u.id = st.user_id
		  AND u.is_active = TRUE
		RETURNING st.id::text, st.user_id::text, st.session_id::text, st.token_hash, st.expires_at, st.consumed_at, st.created_at
	`, tokenHash).Scan(&tok.ID, &tok.UserID, &tok.SessionID, &tok.TokenHash, &tok.ExpiresAt, &tok.ConsumedAt, &tok.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrInvalidToken
	}
	if err != nil {
		return nil, nil, err
	}

	u := &User{}
	err = tx.QueryRow(ctx, `
		SELECT id::text, email, password_hash, role, is_active, created_at, updated_at
		FROM users WHERE id = $1::uuid
	`, tok.UserID).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return tok, u, nil
}

func (s *PGStore) InsertAuditLog(ctx context.Context, entry AuditEntry) error {
	metaBytes, err := json.Marshal(entry.Metadata)
	if err != nil || entry.Metadata == nil {
		metaBytes = []byte("{}")
	}
	outcome := entry.Outcome
	if outcome == "" {
		outcome = "success"
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO audit_logs (actor_id, actor_email, action, target_type, target_id, ip_address, outcome, metadata)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8::jsonb)
	`, entry.ActorID, entry.ActorEmail, entry.Action, entry.TargetType, entry.TargetID, entry.IPAddress, outcome, string(metaBytes))
	return err
}
