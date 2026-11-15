package auth

import (
	"encoding/json"
	"net/http"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type bootstrapRequest struct {
	BootstrapToken string `json:"bootstrapToken"`
	Email          string `json:"email"`
	Password       string `json:"password"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeStrictJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func (s *Service) HandleBootstrapStatus(w http.ResponseWriter, r *http.Request) {
	needs, err := s.NeedsBootstrap(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check bootstrap status"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"needsBootstrap": needs})
}

func (s *Service) HandleBootstrap(w http.ResponseWriter, r *http.Request) {
	var req bootstrapRequest
	if err := decodeStrictJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	user, rawSess, csrf, err := s.BootstrapAdmin(r.Context(), req.BootstrapToken, req.Email, req.Password, ClientIP(r), r.UserAgent())
	if err != nil {
		if err == ErrBootstrapDisabled {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "bootstrap is already complete"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.SetSessionCookie(w, rawSess)
	writeJSON(w, http.StatusCreated, map[string]any{
		"user":      user,
		"csrfToken": csrf,
	})
}

func (s *Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeStrictJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	user, rawSess, csrf, err := s.Login(r.Context(), req.Email, req.Password, ClientIP(r), r.UserAgent())
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	s.SetSessionCookie(w, rawSess)
	writeJSON(w, http.StatusOK, map[string]any{
		"user":      user,
		"csrfToken": csrf,
	})
}

func (s *Service) HandleLogout(w http.ResponseWriter, r *http.Request) {
	sess, okS := SessionFromContext(r.Context())
	user, okU := UserFromContext(r.Context())
	if okS && okU {
		_ = s.Logout(r.Context(), sess, user, ClientIP(r))
	}
	s.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

func (s *Service) HandleMe(w http.ResponseWriter, r *http.Request) {
	user, okU := UserFromContext(r.Context())
	sess, okS := SessionFromContext(r.Context())
	if !okU || !okS {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":      user,
		"csrfToken": sess.CSRFToken,
	})
}

func (s *Service) HandleIssueSignalingToken(w http.ResponseWriter, r *http.Request) {
	user, okU := UserFromContext(r.Context())
	sess, okS := SessionFromContext(r.Context())
	if !okU || !okS {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	tok, exp, err := s.IssueSignalingToken(r.Context(), user, sess)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue signaling token"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":     tok,
		"expiresAt": exp,
	})
}
