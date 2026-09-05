package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/biswas-dev/lifeai/api/internal/integrations/hard75"
)

// Hard75Status describes the connection as shown in settings. The token
// itself is never returned.
type Hard75Status struct {
	Eligible    bool         `json:"eligible"`
	Connected   bool         `json:"connected"`
	Enabled     bool         `json:"enabled"`
	BaseURL     string       `json:"base_url"`
	TokenHint   string       `json:"token_hint"`
	LastSyncAt  *string      `json:"last_sync_at"`
	LastStatus  string       `json:"last_status"`
	LastError   string       `json:"last_error"`
	LastSummary *SyncSummary `json:"last_summary"`
	Running     bool         `json:"running"`
	// CanStore is false when the server has no ENCRYPTION_KEY.
	CanStore bool `json:"can_store"`
}

// HandleHard75Status reports the connection.
func (s *Server) HandleHard75Status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, err := s.hard75Status(ctx, UserID(ctx))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load integration", "internal")
		return
	}
	respondJSON(w, http.StatusOK, st)
}

type connectHard75Request struct {
	BaseURL string `json:"base_url"`
	Token   string `json:"token"`
	Enabled *bool  `json:"enabled"`
}

// HandleHard75Connect stores or updates the connection. The token is checked
// against 75hard before it is saved, so a typo fails here rather than at 2am.
func (s *Server) HandleHard75Connect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := UserID(ctx)
	if TokenAuth(ctx) {
		respondError(w, http.StatusForbidden, "sign in to change integrations", "token_forbidden")
		return
	}
	user, err := s.getUser(ctx, userID)
	if err != nil || !s.cfg.Hard75Allowed(user.Email) {
		respondError(w, http.StatusForbidden, "the 75hard bridge is not available for this account", "not_eligible")
		return
	}
	if !s.cipher.Enabled() {
		respondError(w, http.StatusServiceUnavailable, "this server has no ENCRYPTION_KEY, so it cannot store a token", "no_encryption_key")
		return
	}
	var req connectHard75Request
	if !decodeJSON(w, r, &req) {
		return
	}

	// An update with no token keeps the stored one (pause / resume).
	if strings.TrimSpace(req.Token) == "" {
		if req.Enabled == nil {
			respondError(w, http.StatusBadRequest, "a token is required", "missing_token")
			return
		}
		res, err := s.db.ExecContext(ctx, `UPDATE integrations SET enabled = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ? AND provider = ?`,
			boolInt(*req.Enabled), userID, hard75Provider)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "could not update integration", "internal")
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			respondError(w, http.StatusNotFound, "75hard is not connected", "not_connected")
			return
		}
		st, _ := s.hard75Status(ctx, userID)
		respondJSON(w, http.StatusOK, st)
		return
	}

	baseURL := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://75hard.biswas.me"
	}
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		respondError(w, http.StatusBadRequest, "base_url must be an http(s) origin", "invalid_url")
		return
	}
	token := strings.TrimSpace(req.Token)
	checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	remote, err := hard75.New(baseURL, token).Me(checkCtx)
	if err != nil {
		if errors.Is(err, hard75.ErrUnauthorized) {
			respondError(w, http.StatusBadRequest, "75hard rejected that token", "token_rejected")
			return
		}
		respondError(w, http.StatusBadGateway, "could not reach 75hard: "+err.Error(), "unreachable")
		return
	}
	enc, err := s.cipher.Seal(token)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not store the token", "internal")
		return
	}
	hint := token
	if len(hint) > 4 {
		hint = hint[len(hint)-4:]
	}
	enabled := 1
	if req.Enabled != nil && !*req.Enabled {
		enabled = 0
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO integrations (user_id, provider, base_url, token_enc, token_hint, enabled, last_status, last_error)
		VALUES (?, ?, ?, ?, ?, ?, 'connected', '')
		ON CONFLICT(user_id, provider) DO UPDATE SET base_url = excluded.base_url, token_enc = excluded.token_enc,
			token_hint = excluded.token_hint, enabled = excluded.enabled, last_status = 'connected', last_error = '', updated_at = CURRENT_TIMESTAMP`,
		userID, hard75Provider, baseURL, enc, hint, enabled); err != nil {
		respondError(w, http.StatusInternalServerError, "could not store the connection", "internal")
		return
	}
	st, _ := s.hard75Status(ctx, userID)
	respondJSON(w, http.StatusOK, map[string]any{"status": st, "remote_account": remote.Email})
}

// HandleHard75Disconnect removes the connection. Imported data stays.
func (s *Server) HandleHard75Disconnect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if TokenAuth(ctx) {
		respondError(w, http.StatusForbidden, "sign in to change integrations", "token_forbidden")
		return
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM integrations WHERE user_id = ? AND provider = ?`, UserID(ctx), hard75Provider); err != nil {
		respondError(w, http.StatusInternalServerError, "could not disconnect", "internal")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// HandleHard75Sync runs a pull now, in the background, and returns at once.
func (s *Server) HandleHard75Sync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := UserID(ctx)
	if s.syncer == nil {
		respondError(w, http.StatusServiceUnavailable, "the 75hard bridge is not running", "sync_disabled")
		return
	}
	var connected int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM integrations WHERE user_id = ? AND provider = ? AND enabled = 1`, userID, hard75Provider).Scan(&connected)
	if connected == 0 {
		respondError(w, http.StatusBadRequest, "75hard is not connected", "not_connected")
		return
	}
	if s.syncer.isRunning(userID) {
		respondError(w, http.StatusConflict, ErrSyncRunning.Error(), "sync_running")
		return
	}
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		_, _ = s.syncer.SyncUser(bg, userID)
	}()
	respondJSON(w, http.StatusAccepted, map[string]string{"status": "running"})
}

func (h *Hard75Syncer) isRunning(userID int64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.running[userID]
}

func (s *Server) hard75Status(ctx context.Context, userID int64) (Hard75Status, error) {
	st := Hard75Status{CanStore: s.cipher.Enabled()}
	user, err := s.getUser(ctx, userID)
	if err != nil {
		return st, err
	}
	st.Eligible = s.cfg.Hard75Allowed(user.Email)
	var (
		enabled  int
		lastSync sql.NullString
		summary  string
	)
	err = s.db.QueryRowContext(ctx, `
		SELECT base_url, token_hint, enabled, last_sync_at, last_status, last_error, last_summary
		  FROM integrations WHERE user_id = ? AND provider = ?`, userID, hard75Provider).
		Scan(&st.BaseURL, &st.TokenHint, &enabled, &lastSync, &st.LastStatus, &st.LastError, &summary)
	if errors.Is(err, sql.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	st.Connected = true
	st.Enabled = enabled == 1
	st.LastSyncAt = strPtr(lastSync)
	if summary != "" {
		var sum SyncSummary
		if json.Unmarshal([]byte(summary), &sum) == nil {
			st.LastSummary = &sum
		}
	}
	if s.syncer != nil {
		st.Running = s.syncer.isRunning(userID)
	}
	return st, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
