package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"aegis-voice/server/internal/security"
)

const (
	SessionCookieName    = "aegis_session"
	SessionIdleTimeout   = 30 * time.Minute
	SessionMaxLifetime   = 12 * time.Hour
	SignalingTokenTTL    = 60 * time.Second
)

type Config struct {
	WebOrigin           string
	CookieSecure        bool
	AdminBootstrapToken string
}

type Service struct {
	store            Store
	cfg              Config
	logger           *slog.Logger
	onSessionRevoked func(userID, sessionID string)
}

func NewService(store Store, cfg Config, logger *slog.Logger) *Service {
	if logger == nil {
		logger = security.NewRedactingLogger(nil)
	}
	return &Service{
		store:  store,
		cfg:    cfg,
		logger: logger,
	}
}

func (s *Service) SetOnSessionRevoked(fn func(userID, sessionID string)) {
	s.onSessionRevoked = fn
}

func generateOpaqueToken(numBytes int) (raw string, hash []byte, err error) {
	b := make([]byte, numBytes)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(raw))
	return raw, sum[:], nil
}

func hashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

func (s *Service) NeedsBootstrap(ctx context.Context) (bool, error) {
	count, err := s.store.CountUsers(ctx)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

func (s *Service) BootstrapAdmin(ctx context.Context, bootstrapToken, email, password, ip, ua string) (*User, string, string, error) {
	if s.cfg.AdminBootstrapToken == "" {
		return nil, "", "", errors.New("bootstrap token is not configured on server")
	}
	if subtle.ConstantTimeCompare([]byte(bootstrapToken), []byte(s.cfg.AdminBootstrapToken)) != 1 {
		_ = s.store.InsertAuditLog(ctx, AuditEntry{
			ActorEmail: email,
			Action:     "auth.bootstrap",
			TargetType: "user",
			IPAddress:  ip,
			Outcome:    "denied",
			Metadata:   map[string]any{"reason": "invalid bootstrap token"},
		})
		return nil, "", "", errors.New("invalid bootstrap token")
	}

	email = strings.TrimSpace(strings.ToLower(email))
	if !strings.Contains(email, "@") || len(email) < 5 {
		return nil, "", "", errors.New("invalid email address")
	}
	hash, err := security.HashPassword(password)
	if err != nil {
		return nil, "", "", err
	}

	user, err := s.store.BootstrapFirstAdmin(ctx, email, hash)
	if err != nil {
		return nil, "", "", err
	}

	rawSess, csrf, err := s.issueSession(ctx, user.ID, ip, ua)
	if err != nil {
		return nil, "", "", err
	}

	_ = s.store.InsertAuditLog(ctx, AuditEntry{
		ActorID:    &user.ID,
		ActorEmail: user.Email,
		Action:     "auth.bootstrap",
		TargetType: "user",
		TargetID:   user.ID,
		IPAddress:  ip,
		Outcome:    "success",
	})
	return user, rawSess, csrf, nil
}

func (s *Service) Login(ctx context.Context, email, password, ip, ua string) (*User, string, string, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil || !user.IsActive {
		// Perform dummy Argon2id check timing-safety or generic failure
		_ = s.store.InsertAuditLog(ctx, AuditEntry{
			ActorEmail: email,
			Action:     "auth.login",
			TargetType: "session",
			IPAddress:  ip,
			Outcome:    "failure",
			Metadata:   map[string]any{"reason": "invalid credentials"},
		})
		return nil, "", "", errors.New("invalid credentials")
	}

	ok, err := security.VerifyPassword(password, user.PasswordHash)
	if err != nil || !ok {
		_ = s.store.InsertAuditLog(ctx, AuditEntry{
			ActorID:    &user.ID,
			ActorEmail: user.Email,
			Action:     "auth.login",
			TargetType: "session",
			IPAddress:  ip,
			Outcome:    "failure",
			Metadata:   map[string]any{"reason": "invalid credentials"},
		})
		return nil, "", "", errors.New("invalid credentials")
	}

	rawSess, csrf, err := s.issueSession(ctx, user.ID, ip, ua)
	if err != nil {
		return nil, "", "", err
	}

	_ = s.store.InsertAuditLog(ctx, AuditEntry{
		ActorID:    &user.ID,
		ActorEmail: user.Email,
		Action:     "auth.login",
		TargetType: "session",
		IPAddress:  ip,
		Outcome:    "success",
	})
	return user, rawSess, csrf, nil
}

func (s *Service) issueSession(ctx context.Context, userID, ip, ua string) (string, string, error) {
	rawTok, tokHash, err := generateOpaqueToken(32)
	if err != nil {
		return "", "", err
	}
	csrfTok, _, err := generateOpaqueToken(24)
	if err != nil {
		return "", "", err
	}
	now := time.Now().UTC()
	sess := &Session{
		UserID:     userID,
		TokenHash:  tokHash,
		CSRFToken:  csrfTok,
		IPAddress:  ip,
		UserAgent:  ua,
		LastSeenAt: now,
		ExpiresAt:  now.Add(SessionMaxLifetime),
	}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		return "", "", err
	}
	return rawTok, csrfTok, nil
}

func (s *Service) AuthenticateSession(ctx context.Context, rawToken string) (*Session, *User, error) {
	if rawToken == "" {
		return nil, nil, ErrInvalidToken
	}
	sess, user, err := s.store.GetActiveSessionByTokenHash(ctx, hashToken(rawToken))
	if err != nil {
		return nil, nil, ErrInvalidToken
	}
	if time.Since(sess.LastSeenAt) > SessionIdleTimeout {
		_ = s.store.RevokeSession(ctx, sess.ID)
		return nil, nil, ErrInvalidToken
	}
	_ = s.store.TouchSession(ctx, sess.ID)
	return sess, user, nil
}

func (s *Service) Logout(ctx context.Context, sess *Session, user *User, ip string) error {
	if err := s.store.RevokeSession(ctx, sess.ID); err != nil {
		return err
	}
	if s.onSessionRevoked != nil {
		s.onSessionRevoked(user.ID, sess.ID)
	}
	_ = s.store.InsertAuditLog(ctx, AuditEntry{
		ActorID:    &user.ID,
		ActorEmail: user.Email,
		Action:     "auth.logout",
		TargetType: "session",
		TargetID:   sess.ID,
		IPAddress:  ip,
		Outcome:    "success",
	})
	return nil
}

func (s *Service) RevokeUserSessions(ctx context.Context, userID string) error {
	if err := s.store.RevokeAllUserSessions(ctx, userID); err != nil {
		return err
	}
	if s.onSessionRevoked != nil {
		s.onSessionRevoked(userID, "")
	}
	return nil
}

func (s *Service) IssueSignalingToken(ctx context.Context, user *User, sess *Session) (string, time.Time, error) {
	raw, hash, err := generateOpaqueToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	exp := time.Now().UTC().Add(SignalingTokenTTL)
	tok := &SignalingToken{
		UserID:    user.ID,
		SessionID: sess.ID,
		TokenHash: hash,
		ExpiresAt: exp,
	}
	if err := s.store.CreateSignalingToken(ctx, tok); err != nil {
		return "", time.Time{}, err
	}
	return raw, exp, nil
}

func (s *Service) ConsumeSignalingToken(ctx context.Context, rawToken string) (*SignalingToken, *User, error) {
	if rawToken == "" {
		return nil, nil, ErrInvalidToken
	}
	return s.store.ConsumeSignalingToken(ctx, hashToken(rawToken))
}

func (s *Service) SetSessionCookie(w http.ResponseWriter, rawToken string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    rawToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(SessionMaxLifetime.Seconds()),
	})
}

func (s *Service) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}
