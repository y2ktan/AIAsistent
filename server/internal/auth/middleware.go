package auth

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
)

type contextKey string

const (
	ctxUserKey    contextKey = "auth_user"
	ctxSessionKey contextKey = "auth_session"
)

func UserFromContext(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(ctxUserKey).(*User)
	return u, ok && u != nil
}

func SessionFromContext(ctx context.Context) (*Session, bool) {
	s, ok := ctx.Value(ctxSessionKey).(*Session)
	return s, ok && s != nil
}

func ClientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil || cookie.Value == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		sess, user, err := s.AuthenticateSession(r.Context(), cookie.Value)
		if err != nil {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), ctxSessionKey, sess)
		ctx = context.WithValue(ctx, ctxUserKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Service) RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if s.cfg.WebOrigin != "" {
			origin := r.Header.Get("Origin")
			if origin != "" && !strings.EqualFold(origin, s.cfg.WebOrigin) {
				http.Error(w, `{"error":"forbidden: invalid origin"}`, http.StatusForbidden)
				return
			}
		}
		sess, ok := SessionFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"forbidden: missing session"}`, http.StatusForbidden)
			return
		}
		csrfHeader := r.Header.Get("X-CSRF-Token")
		if csrfHeader == "" || subtle.ConstantTimeCompare([]byte(csrfHeader), []byte(sess.CSRFToken)) != 1 {
			http.Error(w, `{"error":"forbidden: invalid csrf token"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) RequireOriginOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if s.cfg.WebOrigin != "" {
				origin := r.Header.Get("Origin")
				if origin != "" && !strings.EqualFold(origin, s.cfg.WebOrigin) {
					http.Error(w, `{"error":"forbidden: invalid origin"}`, http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			if user.Role != role {
				_ = s.store.InsertAuditLog(r.Context(), AuditEntry{
					ActorID:    &user.ID,
					ActorEmail: user.Email,
					Action:     "rbac.denied",
					TargetType: "route",
					TargetID:   r.URL.Path,
					IPAddress:  ClientIP(r),
					Outcome:    "denied",
					Metadata:   map[string]any{"requiredRole": role, "actualRole": user.Role},
				})
				http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
