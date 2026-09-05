package api

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/biswas-dev/lifeai/api/internal/auth"
)

// Reset links are available only in explicitly enabled local development.
// Production recovery is handled through the operator CLI until email is configured.

type forgotRequest struct {
	Email string `json:"email"`
}

// HandleForgotPassword issues a reset token for an account. The response is
// identical whether or not the account exists.
func (s *Server) HandleForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if s.cfg.IsProduction() || !s.cfg.ResetTokenInResponse {
		respondError(w, http.StatusServiceUnavailable, "Self-service reset is not configured. Contact the site administrator to recover your account.", "reset_unavailable")
		return
	}
	email := normalizeEmail(req.Email)
	out := map[string]any{"ok": true, "message": "If that account exists, a reset link has been issued."}

	var id int64
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id FROM users WHERE lower(email) = ? AND deleted_at IS NULL`, email).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		respondJSON(w, http.StatusOK, out)
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not start a reset", "internal")
		return
	}

	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		respondError(w, http.StatusInternalServerError, "could not start a reset", "internal")
		return
	}
	token := hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	if _, err := s.db.ExecContext(r.Context(),
		`INSERT INTO password_resets (user_id, token_hash, expires_at) VALUES (?, ?, ?)`,
		id, hex.EncodeToString(sum[:]), time.Now().UTC().Add(time.Hour)); err != nil {
		respondError(w, http.StatusInternalServerError, "could not start a reset", "internal")
		return
	}
	resetURL := s.cfg.AppURL + "/reset-password?token=" + token
	s.log.Info("password reset issued", zap.Int64("user_id", id))
	if s.cfg.ResetTokenInResponse {
		out["token"] = token
		out["reset_url"] = resetURL
	}
	respondJSON(w, http.StatusOK, out)
}

type resetRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// HandleResetPassword consumes a reset token and signs the user in.
func (s *Server) HandleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.NewPassword) < 8 {
		respondError(w, http.StatusBadRequest, "password must be at least 8 characters", "weak_password")
		return
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(req.Token)))
	hash := hex.EncodeToString(sum[:])

	var (
		resetID, userID int64
		expires         time.Time
		used            sql.NullTime
	)
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id, user_id, expires_at, used_at FROM password_resets WHERE token_hash = ?`, hash).
		Scan(&resetID, &userID, &expires, &used)
	if err != nil || used.Valid || time.Now().After(expires) {
		respondError(w, http.StatusBadRequest, "that reset link is not valid", "invalid_token")
		return
	}
	newHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not reset password", "internal")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not reset password", "internal")
		return
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE users SET password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, newHash, userID); err != nil {
		respondError(w, http.StatusInternalServerError, "could not reset password", "internal")
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE password_resets SET used_at = CURRENT_TIMESTAMP WHERE id = ?`, resetID); err != nil {
		respondError(w, http.StatusInternalServerError, "could not reset password", "internal")
		return
	}
	if err := tx.Commit(); err != nil {
		respondError(w, http.StatusInternalServerError, "could not reset password", "internal")
		return
	}
	user, err := s.getUser(r.Context(), userID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load account", "internal")
		return
	}
	s.completeLogin(w, r, user.ID, false, false)
}
