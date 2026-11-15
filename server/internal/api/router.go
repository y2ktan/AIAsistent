package api

import (
	"net/http"

	"aegis-voice/server/internal/auth"
)

func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()

	loginLimiter := auth.NewKeyedRateLimiter(5.0/60.0, 5)
	sigLimiter := auth.NewKeyedRateLimiter(10.0/60.0, 10)
	uploadLimiter := auth.NewKeyedRateLimiter(10.0/3600.0, 5)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Auth routes
	mux.Handle("GET /api/v1/auth/bootstrap-status", http.HandlerFunc(s.authSvc.HandleBootstrapStatus))
	mux.Handle("POST /api/v1/auth/bootstrap",
		auth.RateLimitMiddleware(loginLimiter, auth.ClientIP)(
			s.authSvc.RequireOriginOnly(http.HandlerFunc(s.authSvc.HandleBootstrap)),
		),
	)
	mux.Handle("POST /api/v1/auth/login",
		auth.RateLimitMiddleware(loginLimiter, auth.ClientIP)(
			s.authSvc.RequireOriginOnly(http.HandlerFunc(s.authSvc.HandleLogin)),
		),
	)
	mux.Handle("POST /api/v1/auth/logout",
		s.authSvc.RequireAuth(s.authSvc.RequireCSRF(http.HandlerFunc(s.authSvc.HandleLogout))),
	)
	mux.Handle("GET /api/v1/auth/me",
		s.authSvc.RequireAuth(http.HandlerFunc(s.authSvc.HandleMe)),
	)

	// WebRTC Signaling routes
	mux.Handle("POST /api/v1/rtc/token",
		auth.RateLimitMiddleware(sigLimiter, auth.ClientIP)(
			s.authSvc.RequireAuth(s.authSvc.RequireCSRF(http.HandlerFunc(s.authSvc.HandleIssueSignalingToken))),
		),
	)
	mux.Handle("POST /api/v1/rtc/offer",
		auth.RateLimitMiddleware(sigLimiter, auth.ClientIP)(
			s.authSvc.RequireAuth(s.authSvc.RequireCSRF(http.HandlerFunc(s.HandleRTCOffer))),
		),
	)

	// Admin routes (Zero-Trust: Auth + CSRF + Admin Role)
	admin := func(h http.HandlerFunc) http.Handler {
		return s.authSvc.RequireAuth(s.authSvc.RequireCSRF(s.authSvc.RequireRole(auth.RoleAdmin)(h)))
	}

	mux.Handle("GET /api/v1/admin/users", admin(s.HandleListUsers))
	mux.Handle("POST /api/v1/admin/users", admin(s.HandleCreateUser))
	mux.Handle("PATCH /api/v1/admin/users/{id}", admin(s.HandleUpdateUser))

	mux.Handle("GET /api/v1/admin/documents", admin(s.HandleListDocuments))
	mux.Handle("POST /api/v1/admin/documents",
		auth.RateLimitMiddleware(uploadLimiter, auth.ClientIP)(admin(s.HandleUploadDocument)),
	)
	mux.Handle("DELETE /api/v1/admin/documents/{id}", admin(s.HandleDeleteDocument))

	mux.Handle("GET /api/v1/admin/agent-config", admin(s.HandleGetAgentConfig))
	mux.Handle("PUT /api/v1/admin/agent-config", admin(s.HandleUpdateAgentConfig))
	mux.Handle("POST /api/v1/admin/agent-config/test", admin(s.HandleTestAgentConfig))

	mux.Handle("GET /api/v1/admin/mcp-servers", admin(s.HandleListMCPServers))
	mux.Handle("POST /api/v1/admin/mcp-servers", admin(s.HandleUpsertMCPServer))
	mux.Handle("PUT /api/v1/admin/mcp-servers/{id}", admin(s.HandleUpsertMCPServer))
	mux.Handle("POST /api/v1/admin/mcp-servers/{id}/discover", admin(s.HandleDiscoverMCPServer))
	mux.Handle("DELETE /api/v1/admin/mcp-servers/{id}", admin(s.HandleDeleteMCPServer))

	mux.Handle("GET /api/v1/admin/audit-logs", admin(s.HandleListAuditLogs))

	return SecurityHeadersMiddleware(mux)
}
