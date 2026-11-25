package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"

	"aegis-voice/server/internal/security"
)

func normalizeHostedMCP(rec *MCPServerRecord) {
	if rec.HostedMode == "" {
		rec.HostedMode = "disabled"
	}
	if rec.HostedSyncStatus == "" {
		rec.HostedSyncStatus = "disabled"
		if rec.HostedMode != "disabled" {
			rec.HostedSyncStatus = "pending"
		}
	}
}

type hostedMCPAdminRequest struct {
	Name                             string   `json:"name"`
	Transport                        string   `json:"transport"`
	EndpointURL                      string   `json:"endpointUrl"`
	AuthHeader                       *string  `json:"authHeader"`
	Enabled                          bool     `json:"enabled"`
	AllowedTools                     []string `json:"allowedTools"`
	TimeoutMs                        int      `json:"timeoutMs"`
	HostedMode                       *string  `json:"hostedMode"`
	DirectCredentialSharingConfirmed bool     `json:"directCredentialSharingConfirmed"`
}

func hostedPublicHTTPS(raw string, origin bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" ||
		(origin && (u.RawQuery != "" || (u.Path != "" && u.Path != "/"))) {
		return nil, errors.New("Hosted MCP requires a configured public HTTPS URL without user credentials.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", u.Hostname())
	if err != nil || len(ips) == 0 {
		return nil, errors.New("Hosted MCP HTTPS hostname could not be verified as public.")
	}
	for _, ip := range ips {
		if security.IsBlockedIP(ip) {
			return nil, errors.New("Hosted MCP requires a public HTTPS endpoint, not a private address.")
		}
	}
	return u, nil
}

func (s *Server) withHostedMCPAdmin(w http.ResponseWriter, r *http.Request, fn func(*Server, *http.Request)) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := s.adminStore.WithHostedMCPLock(ctx, func(store AdminStore) error {
		locked := *s
		locked.adminStore = store
		fn(&locked, r.WithContext(ctx))
		return nil
	}); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Hosted MCP synchronization is busy or unavailable. Retry shortly."})
	}
}

func (s *Server) saveHostedMCP(w http.ResponseWriter, r *http.Request, rec *MCPServerRecord, req hostedMCPAdminRequest) {
	if rec.ID != "" {
		old, err := s.adminStore.GetMCPServer(r.Context(), rec.ID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "MCP server not found."})
			return
		}
		rec.AuthHeaderCiphertext, rec.AuthHeaderLast4 = old.AuthHeaderCiphertext, old.AuthHeaderLast4
		rec.DiscoveredTools = old.DiscoveredTools
		rec.HostedMode, rec.HostedResourceID, rec.HostedAgentID = old.HostedMode, old.HostedResourceID, old.HostedAgentID
		rec.HostedProxyCredential = old.HostedProxyCredential
		rec.HostedCreationIntentID, rec.HostedCreationConfig, rec.HostedCreationAttempted = old.HostedCreationIntentID, old.HostedCreationConfig, old.HostedCreationAttempted
		if rec.EndpointURL != old.EndpointURL || (req.AuthHeader != nil && *req.AuthHeader != "") {
			rec.DiscoveredTools = nil
		}
	}
	normalizeHostedMCP(rec)
	if req.HostedMode != nil {
		rec.HostedMode = *req.HostedMode
	}
	switch rec.HostedMode {
	case "disabled":
		rec.HostedProxyCredential = nil
	case "direct":
		rec.HostedProxyCredential = nil
		if (rec.Enabled || req.HostedMode != nil) && !req.DirectCredentialSharingConfirmed {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Confirm sharing the MCP endpoint and authentication with ElevenLabs."})
			return
		}
		if rec.Enabled || req.HostedMode != nil {
			if _, err := hostedPublicHTTPS(rec.EndpointURL, false); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
	case "proxy":
		if rec.Enabled || req.HostedMode != nil {
			if _, err := hostedPublicHTTPS(s.hostedProxyBaseURL, true); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Configure a valid public HTTPS HOSTED_MCP_PROXY_BASE_URL before choosing proxy mode."})
				return
			}
		}
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Hosted MCP mode must be disabled, direct, or proxy."})
		return
	}
	if req.AuthHeader != nil && *req.AuthHeader != "" {
		ct, err := s.vault.Encrypt(*req.AuthHeader)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not encrypt MCP authentication."})
			return
		}
		rec.AuthHeaderCiphertext, rec.AuthHeaderLast4 = ct, security.MaskLast4(*req.AuthHeader)
	}
	if rec.HostedMode == "proxy" && rec.Enabled && len(rec.HostedProxyCredential) == 0 {
		var token [32]byte
		if _, err := rand.Read(token[:]); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not generate a proxy credential."})
			return
		}
		ct, err := s.vault.Encrypt(base64.RawURLEncoding.EncodeToString(token[:]))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not encrypt a proxy credential."})
			return
		}
		rec.HostedProxyCredential = ct
	}
	if !rec.Enabled {
		rec.HostedProxyCredential = nil
	}
	rec.HostedSyncStatus, rec.HostedSyncError = "pending", ""
	if rec.HostedMode == "disabled" && rec.HostedResourceID == "" && !rec.HostedCreationAttempted {
		rec.HostedSyncStatus = "disabled"
	}
	saved, err := s.adminStore.UpsertMCPServer(r.Context(), rec)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not save MCP configuration."})
		return
	}
	if err := s.syncChangedHostedMCP(r.Context(), saved); err != nil {
		s.audit(r.Context(), r, "admin.mcp.upsert", "mcp_server", saved.ID, "failure", map[string]any{"hostedMode": saved.HostedMode})
		writeJSON(w, http.StatusBadGateway, map[string]any{"server": saved, "error": err.Error()})
		return
	}
	s.audit(r.Context(), r, "admin.mcp.upsert", "mcp_server", saved.ID, "success", map[string]any{"name": saved.Name, "enabled": saved.Enabled, "hostedMode": saved.HostedMode})
	writeJSON(w, http.StatusOK, map[string]any{"server": saved})
}

func (s *Server) syncChangedHostedMCP(ctx context.Context, rec *MCPServerRecord) error {
	config, err := s.adminStore.GetAgentConfig(ctx)
	if err != nil {
		return errors.New("MCP choice saved, but hosted configuration could not be loaded.")
	}
	if config.Mode != "elevenlabs_agent" && rec.HostedResourceID == "" && !rec.HostedCreationAttempted {
		return nil
	}
	return s.reconcileHostedMCPServer(ctx, config, rec)
}

func (s *Server) deleteHostedMCP(w http.ResponseWriter, r *http.Request) {
	rec, err := s.adminStore.GetMCPServer(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "MCP server not found."})
		return
	}
	rec.Enabled, rec.HostedMode, rec.HostedProxyCredential = false, "disabled", nil
	rec.HostedSyncStatus, rec.HostedSyncError = "revocation_pending", ""
	if _, err := s.adminStore.UpsertMCPServer(r.Context(), rec); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not disable MCP server before cleanup."})
		return
	}
	if err := s.syncChangedHostedMCP(r.Context(), rec); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"server": rec, "error": err.Error()})
		return
	}
	if err := s.adminStore.DeleteMCPServer(r.Context(), rec.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Remote cleanup completed, but local deletion failed."})
		return
	}
	s.audit(r.Context(), r, "admin.mcp.delete", "mcp_server", rec.ID, "success", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) HandleRetryHostedMCP(w http.ResponseWriter, r *http.Request) {
	s.withHostedMCPAdmin(w, r, func(locked *Server, r *http.Request) {
		rec, err := locked.adminStore.GetMCPServer(r.Context(), r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "MCP server not found."})
			return
		}
		if err := locked.syncChangedHostedMCP(r.Context(), rec); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"server": rec, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"server": rec})
	})
}

func allowedHostedMCPTools(rec *MCPServerRecord) map[string]bool {
	allowed := make(map[string]bool)
	for _, name := range rec.AllowedTools {
		for _, tool := range rec.DiscoveredTools {
			if name == tool.Name {
				allowed[name] = true
				break
			}
		}
	}
	return allowed
}
