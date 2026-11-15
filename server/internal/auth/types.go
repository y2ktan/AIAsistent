package auth

import (
	"context"
	"errors"
	"time"
)

const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrAlreadyExists     = errors.New("already exists")
	ErrBootstrapDisabled = errors.New("bootstrap is disabled because users already exist")
	ErrInvalidToken      = errors.New("invalid or expired token")
)

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	IsActive     bool      `json:"isActive"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type Session struct {
	ID         string
	UserID     string
	TokenHash  []byte
	CSRFToken  string
	IPAddress  string
	UserAgent  string
	LastSeenAt time.Time
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

type SignalingToken struct {
	ID         string
	UserID     string
	SessionID  string
	TokenHash  []byte
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	CreatedAt  time.Time
}

type AuditEntry struct {
	ActorID    *string
	ActorEmail string
	Action     string
	TargetType string
	TargetID   string
	IPAddress  string
	Outcome    string
	Metadata   map[string]any
}

type Store interface {
	CountUsers(ctx context.Context) (int, error)
	BootstrapFirstAdmin(ctx context.Context, email, passwordHash string) (*User, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	GetUserByID(ctx context.Context, id string) (*User, error)
	CreateSession(ctx context.Context, sess *Session) error
	GetActiveSessionByTokenHash(ctx context.Context, tokenHash []byte) (*Session, *User, error)
	TouchSession(ctx context.Context, sessionID string) error
	RevokeSession(ctx context.Context, sessionID string) error
	RevokeAllUserSessions(ctx context.Context, userID string) error
	CreateSignalingToken(ctx context.Context, tok *SignalingToken) error
	ConsumeSignalingToken(ctx context.Context, tokenHash []byte) (*SignalingToken, *User, error)
	InsertAuditLog(ctx context.Context, entry AuditEntry) error
}
