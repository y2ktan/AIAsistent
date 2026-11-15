package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"aegis-voice/server/internal/security"
)

type memStore struct {
	mu              sync.Mutex
	users           map[string]*User
	sessions        map[string]*Session
	signalingTokens map[string]*SignalingToken
	audits          []AuditEntry
	nextID          int
}

func newMemStore() *memStore {
	return &memStore{
		users:           make(map[string]*User),
		sessions:        make(map[string]*Session),
		signalingTokens: make(map[string]*SignalingToken),
	}
}

func (m *memStore) CountUsers(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.users), nil
}

func (m *memStore) BootstrapFirstAdmin(_ context.Context, email, passwordHash string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.users) > 0 {
		return nil, ErrBootstrapDisabled
	}
	m.nextID++
	u := &User{
		ID:           "00000000-0000-0000-0000-000000000001",
		Email:        email,
		PasswordHash: passwordHash,
		Role:         RoleAdmin,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	m.users[u.ID] = u
	return u, nil
}

func (m *memStore) GetUserByEmail(_ context.Context, email string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, ErrNotFound
}

func (m *memStore) GetUserByID(_ context.Context, id string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	return u, nil
}

func (m *memStore) CreateSession(_ context.Context, sess *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	sess.ID = "sess-" + string(rune('0'+m.nextID))
	sess.CreatedAt = time.Now()
	m.sessions[string(sess.TokenHash)] = sess
	return nil
}

func (m *memStore) GetActiveSessionByTokenHash(_ context.Context, tokenHash []byte) (*Session, *User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[string(tokenHash)]
	if !ok || sess.RevokedAt != nil || time.Now().After(sess.ExpiresAt) {
		return nil, nil, ErrNotFound
	}
	u, ok := m.users[sess.UserID]
	if !ok || !u.IsActive {
		return nil, nil, ErrNotFound
	}
	return sess, u, nil
}

func (m *memStore) TouchSession(_ context.Context, _ string) error { return nil }

func (m *memStore) RevokeSession(_ context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for _, s := range m.sessions {
		if s.ID == sessionID {
			s.RevokedAt = &now
		}
	}
	return nil
}

func (m *memStore) RevokeAllUserSessions(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for _, s := range m.sessions {
		if s.UserID == userID {
			s.RevokedAt = &now
		}
	}
	return nil
}

func (m *memStore) CreateSignalingToken(_ context.Context, tok *SignalingToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	tok.ID = "sig-" + string(rune('0'+m.nextID))
	tok.CreatedAt = time.Now()
	m.signalingTokens[string(tok.TokenHash)] = tok
	return nil
}

func (m *memStore) ConsumeSignalingToken(_ context.Context, tokenHash []byte) (*SignalingToken, *User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tok, ok := m.signalingTokens[string(tokenHash)]
	if !ok || tok.ConsumedAt != nil || time.Now().After(tok.ExpiresAt) {
		return nil, nil, ErrInvalidToken
	}
	u, ok := m.users[tok.UserID]
	if !ok || !u.IsActive {
		return nil, nil, ErrInvalidToken
	}
	now := time.Now()
	tok.ConsumedAt = &now
	return tok, u, nil
}

func (m *memStore) InsertAuditLog(_ context.Context, entry AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.audits = append(m.audits, entry)
	return nil
}

func TestBootstrapLoginRBACCSRFAndRevocation(t *testing.T) {
	store := newMemStore()
	svc := NewService(store, Config{
		WebOrigin:           "https://aegis.local",
		CookieSecure:        true,
		AdminBootstrapToken: "bootstrap-secret-2026",
	}, nil)

	// 1. Bootstrap fails with wrong token
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewBufferString(`{"bootstrapToken":"wrong","email":"admin@aegis.local","password":"StrongPassword!2026"}`))
	rec := httptest.NewRecorder()
	svc.HandleBootstrap(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for wrong bootstrap token, got %d", rec.Code)
	}

	// 2. Bootstrap succeeds with right token
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewBufferString(`{"bootstrapToken":"bootstrap-secret-2026","email":"admin@aegis.local","password":"StrongPassword!2026"}`))
	rec = httptest.NewRecorder()
	svc.HandleBootstrap(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for valid bootstrap, got %d: %s", rec.Code, rec.Body.String())
	}

	// 3. Second bootstrap returns 409 Conflict
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewBufferString(`{"bootstrapToken":"bootstrap-secret-2026","email":"admin2@aegis.local","password":"StrongPassword!2026"}`))
	rec = httptest.NewRecorder()
	svc.HandleBootstrap(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 on second bootstrap, got %d", rec.Code)
	}

	// 4. Add a regular user
	pwHash, _ := security.HashPassword("UserPassword!2026")
	store.mu.Lock()
	store.users["user-1"] = &User{
		ID:           "user-1",
		Email:        "user@aegis.local",
		PasswordHash: pwHash,
		Role:         RoleUser,
		IsActive:     true,
	}
	store.mu.Unlock()

	// 5. Login as regular user
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"email":"user@aegis.local","password":"UserPassword!2026"}`))
	rec = httptest.NewRecorder()
	svc.HandleLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on login, got %d", rec.Code)
	}
	var loginResp struct {
		User      User   `json:"user"`
		CSRFToken string `json:"csrfToken"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &loginResp)
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("expected HttpOnly, Secure, SameSite=Strict cookie, got %+v", cookies)
	}
	sessCookie := cookies[0]

	// 6. RBAC 403 for user on admin route
	adminHandler := svc.RequireAuth(svc.RequireCSRF(svc.RequireRole(RoleAdmin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))))
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", nil)
	req.AddCookie(sessCookie)
	req.Header.Set("Origin", "https://aegis.local")
	req.Header.Set("X-CSRF-Token", loginResp.CSRFToken)
	rec = httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for non-admin, got %d", rec.Code)
	}

	// 7. CSRF rejection on missing token or wrong Origin
	userPostHandler := svc.RequireAuth(svc.RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	req = httptest.NewRequest(http.MethodPost, "/api/v1/rtc/token", nil)
	req.AddCookie(sessCookie)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("X-CSRF-Token", loginResp.CSRFToken)
	rec = httptest.NewRecorder()
	userPostHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched Origin, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/rtc/token", nil)
	req.AddCookie(sessCookie)
	req.Header.Set("Origin", "https://aegis.local")
	rec = httptest.NewRecorder()
	userPostHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for missing X-CSRF-Token, got %d", rec.Code)
	}

	// 8. Signaling token issuance & single-use consumption + replay rejection
	sigHandler := svc.RequireAuth(svc.RequireCSRF(http.HandlerFunc(svc.HandleIssueSignalingToken)))
	req = httptest.NewRequest(http.MethodPost, "/api/v1/rtc/token", nil)
	req.AddCookie(sessCookie)
	req.Header.Set("Origin", "https://aegis.local")
	req.Header.Set("X-CSRF-Token", loginResp.CSRFToken)
	rec = httptest.NewRecorder()
	sigHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 issuing signaling token, got %d", rec.Code)
	}
	var sigResp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &sigResp)
	if sigResp.Token == "" {
		t.Fatal("expected non-empty signaling token")
	}

	if _, _, err := svc.ConsumeSignalingToken(context.Background(), sigResp.Token); err != nil {
		t.Fatalf("expected first ConsumeSignalingToken to succeed, got %v", err)
	}
	if _, _, err := svc.ConsumeSignalingToken(context.Background(), sigResp.Token); err == nil {
		t.Fatal("expected replay ConsumeSignalingToken to fail")
	}

	// 9. Mid-flight session revocation
	revokedCallbackCalled := false
	svc.SetOnSessionRevoked(func(userID, sessionID string) {
		if userID == "user-1" {
			revokedCallbackCalled = true
		}
	})
	_ = svc.RevokeUserSessions(context.Background(), "user-1")
	if !revokedCallbackCalled {
		t.Fatal("expected onSessionRevoked callback to fire")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(sessCookie)
	rec = httptest.NewRecorder()
	svc.RequireAuth(http.HandlerFunc(svc.HandleMe)).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after mid-flight revocation, got %d", rec.Code)
	}
}

func TestRateLimiterReturns429(t *testing.T) {
	rl := NewKeyedRateLimiter(0.01, 2)
	h := RateLimitMiddleware(rl, func(r *http.Request) string { return "1.2.3.4" })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/login", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d expected 200, got %d", i, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/login", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on 429")
	}
}
