package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	gologin "github.com/anchoo2kewl/go-login"
	"github.com/biswas-dev/lifeai/api/internal/auth"
	"github.com/go-chi/chi/v5"
)

const securityWindow = 5 * time.Minute

// MountSecurityRoutes is shared by the production router and integration tests.
// Call it on the /api router. API/MCP tokens cannot manage account security.
func (s *Server) MountSecurityRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(RateLimitMiddleware(20))
		r.Post("/auth/mfa/verify", s.HandleMFAVerify)
		r.Post("/auth/passkeys/login/begin", s.HandlePasskeyLoginBegin)
		r.Post("/auth/passkeys/login/finish", s.HandlePasskeyLoginFinish)
	})
	r.Route("/security", func(r chi.Router) {
		r.Use(s.JWTAuth, s.sessionOnly, RateLimitMiddleware(20))
		r.Get("/", s.HandleSecurityStatus)
		r.Post("/reauth", s.HandleReauthenticate)
		r.Group(func(r chi.Router) {
			r.Use(s.recentAuthentication)
			r.Post("/totp/begin", s.HandleTOTPBegin)
			r.Post("/totp/confirm", s.HandleTOTPConfirm)
			r.Delete("/totp", s.HandleTOTPDisable)
			r.Post("/recovery-codes", s.HandleRecoveryCodes)
			r.Post("/passkeys/begin", s.HandlePasskeyRegisterBegin)
			r.Post("/passkeys/finish", s.HandlePasskeyRegisterFinish)
			r.Delete("/passkeys/{credentialID}", s.HandlePasskeyDelete)
		})
	})
}

func (s *Server) sessionOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if _, ok := r.Context().Value(SessionClaimsKey).(*auth.Claims); !ok {
			respondError(w, 403, "Sign in to manage account security.", "session_required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recentAuthentication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(SessionClaimsKey).(*auth.Claims)
		if !ok || claims.AuthTime == 0 || time.Now().Unix()-claims.AuthTime > int64(securityWindow.Seconds()) {
			respondError(w, 403, "Confirm your identity before changing security settings.", "reauth_required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityRandom() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
func securityHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *Server) challengeCookie(kind, value string, age int) *http.Cookie {
	secure := strings.HasPrefix(s.cfg.AppURL, "https://")
	name := "lifeai_" + kind
	if secure {
		name = "__Host-" + name
	}
	return &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: age}
}

type securityChallenge struct {
	UserID  int64
	Version int64
	Data    string
}

func (s *Server) newChallenge(w http.ResponseWriter, r *http.Request, kind string, userID int64, data string) (string, error) {
	w.Header().Set("Cache-Control", "no-store")
	token, binding := securityRandom(), securityRandom()
	var uid any
	var version int64
	if userID != 0 {
		uid = userID
		if err := s.db.QueryRowContext(r.Context(), `SELECT security_version FROM users WHERE id=? AND deleted_at IS NULL`, userID).Scan(&version); err != nil {
			return "", err
		}
	}
	// Expired ceremonies never survive indefinitely in the persistent database.
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM auth_challenges WHERE expires_at < ?`, time.Now().Unix()); err != nil {
		return "", err
	}
	_, err := s.db.ExecContext(r.Context(), `INSERT INTO auth_challenges(token_hash,binding_hash,user_id,security_version,kind,data_json,expires_at) VALUES(?,?,?,?,?,?,?)`,
		securityHash(token), securityHash(binding), uid, version, kind, data, time.Now().Add(securityWindow).Unix())
	if err != nil {
		return "", err
	}
	http.SetCookie(w, s.challengeCookie(kind, binding, int(securityWindow.Seconds())))
	return token, nil
}

func (s *Server) readChallenge(r *http.Request, kind, token string, singleUse bool) (securityChallenge, error) {
	var out securityChallenge
	cookie, err := r.Cookie(s.challengeCookie(kind, "", 0).Name)
	if err != nil || len(token) != 43 || len(cookie.Value) != 43 {
		return out, errors.New("invalid browser challenge")
	}
	query := `UPDATE auth_challenges SET attempts=attempts+1 WHERE token_hash=? AND binding_hash=? AND kind=? AND expires_at>? AND attempts<5 RETURNING COALESCE(user_id,0),security_version,data_json`
	if singleUse {
		query = `DELETE FROM auth_challenges WHERE token_hash=? AND binding_hash=? AND kind=? AND expires_at>? AND attempts<5 RETURNING COALESCE(user_id,0),security_version,data_json`
	}
	err = s.db.QueryRowContext(r.Context(), query, securityHash(token), securityHash(cookie.Value), kind, time.Now().Unix()).Scan(&out.UserID, &out.Version, &out.Data)
	if err != nil {
		return out, err
	}
	if out.UserID != 0 {
		var version int64
		err = s.db.QueryRowContext(r.Context(), `SELECT security_version FROM users WHERE id=? AND deleted_at IS NULL`, out.UserID).Scan(&version)
		if err != nil || version != out.Version {
			return out, errors.New("account security changed")
		}
	}
	return out, nil
}

func (s *Server) sessionResponse(ctx context.Context, userID int64, mfa bool, expectedVersion ...int64) (authResponse, error) {
	var out authResponse
	u, err := s.getUser(ctx, userID)
	if err != nil {
		return out, err
	}
	var version int64
	var enabled bool
	if err := s.db.QueryRowContext(ctx, `SELECT security_version,totp_secret_enc<>'' FROM users WHERE id=?`, userID).Scan(&version, &enabled); err != nil {
		return out, err
	}
	if enabled && !mfa {
		return out, errors.New("second factor required")
	}
	if len(expectedVersion) > 0 && version != expectedVersion[0] {
		return out, errors.New("account security changed; sign in again")
	}
	token, err := auth.GenerateSessionToken(u.ID, u.Email, s.cfg.JWTSecret, s.cfg.JWTExpiry, version, mfa)
	return authResponse{Token: token, User: u}, err
}

// CompleteGoogleLogin is called only after the provider identity is verified.
func (s *Server) CompleteGoogleLogin(w http.ResponseWriter, r *http.Request, userID int64) {
	s.completeLogin(w, r, userID, false, true)
}

func (s *Server) completeLogin(w http.ResponseWriter, r *http.Request, userID int64, mfa, redirect bool) {
	w.Header().Set("Cache-Control", "no-store")
	var enabled bool
	if err := s.db.QueryRowContext(r.Context(), `SELECT totp_secret_enc<>'' FROM users WHERE id=? AND deleted_at IS NULL`, userID).Scan(&enabled); err != nil {
		respondError(w, 500, "Could not sign in.", "internal")
		return
	}
	if enabled && !mfa {
		challenge, err := s.newChallenge(w, r, "mfa", userID, "{}")
		if err != nil {
			respondError(w, 500, "Could not start verification.", "internal")
			return
		}
		if redirect {
			http.Redirect(w, r, s.cfg.OAuthSuccessURL+"#challenge="+url.QueryEscape(challenge), http.StatusFound)
			return
		}
		respondJSON(w, 200, map[string]any{"mfa_required": true, "challenge": challenge})
		return
	}
	out, err := s.sessionResponse(r.Context(), userID, mfa)
	if err != nil {
		respondError(w, 500, "Could not create your session.", "internal")
		return
	}
	if redirect {
		http.Redirect(w, r, s.cfg.OAuthSuccessURL+"#token="+url.QueryEscape(out.Token), http.StatusFound)
		return
	}
	respondJSON(w, 200, out)
}

type mfaRequest struct {
	Challenge string `json:"challenge"`
	Code      string `json:"code"`
}

// verifyFactor runs in a write transaction. Consuming a TOTP time step or a
// recovery code is atomic, so concurrent requests cannot reuse either one.
func (s *Server) verifyFactor(ctx context.Context, tx *sql.Tx, userID int64, code string) (bool, error) {
	var encrypted string
	var last, locked int64
	var failures int
	if err := tx.QueryRowContext(ctx, `SELECT totp_secret_enc,totp_last_step,mfa_failures,mfa_locked_until FROM users WHERE id=? AND deleted_at IS NULL`, userID).Scan(&encrypted, &last, &failures, &locked); err != nil {
		return false, err
	}
	now := time.Now()
	if locked > now.Unix() {
		return false, nil
	}
	if encrypted == "" {
		return false, nil
	}
	secret, err := s.cipher.Open(encrypted)
	if err != nil {
		return false, err
	}
	step := matchingTOTPStep(secret, code, now)
	valid := step > last && step >= 0
	if valid {
		_, err = tx.ExecContext(ctx, `UPDATE users SET totp_last_step=?,mfa_failures=0,mfa_locked_until=0 WHERE id=?`, step, userID)
		return err == nil, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id=? AND code_hash=?`, userID, gologin.HashRecoveryCode(strings.TrimSpace(code)))
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		_, err = tx.ExecContext(ctx, `UPDATE users SET mfa_failures=0,mfa_locked_until=0 WHERE id=?`, userID)
		return err == nil, err
	}
	if locked != 0 {
		failures = 0
	}
	failures++
	locked = 0
	if failures >= 10 {
		locked = now.Add(5 * time.Minute).Unix()
	}
	_, err = tx.ExecContext(ctx, `UPDATE users SET mfa_failures=?,mfa_locked_until=? WHERE id=?`, failures, locked, userID)
	return false, err
}

func matchingTOTPStep(secret, code string, now time.Time) int64 {
	code = strings.TrimSpace(code)
	if !gologin.VerifyTOTP(secret, code, now) {
		return -1
	}
	matched := int64(-1)
	for offset := -1; offset <= 1; offset++ {
		at := now.Add(time.Duration(offset) * gologin.TOTPStep)
		candidate, err := gologin.TOTPCode(secret, at)
		if err == nil && subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) == 1 {
			matched = at.Unix() / 30
		}
	}
	return matched
}

func (s *Server) securityTransaction(ctx context.Context, userID int64) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	// Take the SQLite write lock before reading replay/attempt state.
	if _, err = tx.ExecContext(ctx, `UPDATE users SET mfa_failures=mfa_failures WHERE id=?`, userID); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if claims, ok := ctx.Value(SessionClaimsKey).(*auth.Claims); ok {
		var version int64
		if err = tx.QueryRowContext(ctx, `SELECT security_version FROM users WHERE id=? AND deleted_at IS NULL`, userID).Scan(&version); err != nil || version != claims.SecurityVersion {
			_ = tx.Rollback()
			return nil, errors.New("account security changed; authenticate again")
		}
	}
	return tx, nil
}

func (s *Server) HandleMFAVerify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var req mfaRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	challenge, err := s.readChallenge(r, "mfa", req.Challenge, false)
	if err != nil {
		respondError(w, 401, "Verification expired. Sign in again.", "challenge_expired")
		return
	}
	tx, err := s.securityTransaction(r.Context(), challenge.UserID)
	if err != nil {
		respondError(w, 500, "Could not verify code.", "internal")
		return
	}
	defer tx.Rollback()
	var version int64
	if err = tx.QueryRowContext(r.Context(), `SELECT security_version FROM users WHERE id=?`, challenge.UserID).Scan(&version); err != nil || version != challenge.Version {
		respondError(w, 401, "Sign in again.", "challenge_expired")
		return
	}
	valid, err := s.verifyFactor(r.Context(), tx, challenge.UserID, req.Code)
	if err != nil {
		respondError(w, 500, "Could not verify code.", "internal")
		return
	}
	if !valid {
		_ = tx.Commit()
		respondError(w, 401, "Code is invalid, already used, or temporarily locked. Use a fresh code or a recovery code.", "invalid_code")
		return
	}
	res, err := tx.ExecContext(r.Context(), `DELETE FROM auth_challenges WHERE token_hash=? AND expires_at>?`, securityHash(req.Challenge), time.Now().Unix())
	if err != nil {
		respondError(w, 500, "Could not complete verification.", "internal")
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		respondError(w, 401, "Verification expired. Sign in again.", "challenge_expired")
		return
	}
	if err = tx.Commit(); err != nil {
		respondError(w, 500, "Could not complete verification.", "internal")
		return
	}
	http.SetCookie(w, s.challengeCookie("mfa", "", -1))
	out, err := s.sessionResponse(r.Context(), challenge.UserID, true, challenge.Version)
	if err != nil {
		respondError(w, 401, "Account security changed. Sign in again.", "challenge_expired")
		return
	}
	respondJSON(w, 200, out)
}

func (s *Server) HandleReauthenticate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id := UserID(r.Context())
	var hash string
	var enabled bool
	if err := s.db.QueryRowContext(r.Context(), `SELECT password_hash,totp_secret_enc<>'' FROM users WHERE id=?`, id).Scan(&hash, &enabled); err != nil || auth.VerifyPassword(hash, req.Password) != nil {
		respondError(w, 401, "Password is incorrect. You can also sign in again with Google or a passkey.", "invalid_credentials")
		return
	}
	if enabled {
		tx, err := s.securityTransaction(r.Context(), id)
		if err != nil {
			respondError(w, 500, "Could not verify identity.", "internal")
			return
		}
		defer tx.Rollback()
		valid, err := s.verifyFactor(r.Context(), tx, id, req.Code)
		if err != nil {
			respondError(w, 500, "Could not verify identity.", "internal")
			return
		}
		if err = tx.Commit(); err != nil {
			respondError(w, 500, "Could not verify identity.", "internal")
			return
		}
		if !valid {
			respondError(w, 401, "Enter a fresh authenticator code or an unused recovery code.", "invalid_code")
			return
		}
	}
	claims := r.Context().Value(SessionClaimsKey).(*auth.Claims)
	out, err := s.sessionResponse(r.Context(), id, enabled, claims.SecurityVersion)
	if err != nil {
		respondError(w, 401, "Account security changed. Sign in again.", "unauthorized")
		return
	}
	respondJSON(w, 200, out)
}

func (s *Server) HandleSecurityStatus(w http.ResponseWriter, r *http.Request) {
	id := UserID(r.Context())
	var enabled bool
	var remaining int
	if err := s.db.QueryRowContext(r.Context(), `SELECT totp_secret_enc<>'' FROM users WHERE id=?`, id).Scan(&enabled); err != nil {
		respondError(w, 500, "Could not load security settings.", "internal")
		return
	}
	if err := s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM recovery_codes WHERE user_id=?`, id).Scan(&remaining); err != nil {
		respondError(w, 500, "Could not load security settings.", "internal")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT credential_id,name,created_at,COALESCE(last_used_at,''),backed_up FROM passkeys WHERE user_id=? ORDER BY created_at,credential_id`, id)
	if err != nil {
		respondError(w, 500, "Could not load passkeys.", "internal")
		return
	}
	defer rows.Close()
	keys := []map[string]any{}
	for rows.Next() {
		var key, name, created, used string
		var backed bool
		if err = rows.Scan(&key, &name, &created, &used, &backed); err != nil {
			respondError(w, 500, "Could not load passkeys.", "internal")
			return
		}
		keys = append(keys, map[string]any{"id": key, "name": name, "created_at": created, "last_used_at": used, "backed_up": backed})
	}
	if rows.Err() != nil {
		respondError(w, 500, "Could not load passkeys.", "internal")
		return
	}
	claims, _ := r.Context().Value(SessionClaimsKey).(*auth.Claims)
	fresh := claims != nil && claims.AuthTime != 0 && time.Now().Unix()-claims.AuthTime <= int64(securityWindow.Seconds())
	respondJSON(w, 200, map[string]any{"totp_enabled": enabled, "totp_available": s.cipher.Enabled(), "recovery_codes_remaining": remaining, "passkeys": keys, "passkeys_available": s.passkeys != nil, "recent_authentication": fresh})
}

func (s *Server) HandleTOTPBegin(w http.ResponseWriter, r *http.Request) {
	if !s.cipher.Enabled() {
		respondError(w, 503, "Authenticator setup is unavailable.", "unavailable")
		return
	}
	id := UserID(r.Context())
	var email, existing string
	if err := s.db.QueryRowContext(r.Context(), `SELECT email,totp_secret_enc FROM users WHERE id=?`, id).Scan(&email, &existing); err != nil {
		respondError(w, 500, "Could not start setup.", "internal")
		return
	}
	if existing != "" {
		respondError(w, 409, "Two-factor authentication is already enabled.", "already_enabled")
		return
	}
	secret, err := gologin.NewTOTPSecret()
	if err != nil {
		respondError(w, 500, "Could not start setup.", "internal")
		return
	}
	encrypted, err := s.cipher.Seal(secret)
	if err != nil {
		respondError(w, 500, "Could not start setup.", "internal")
		return
	}
	blob, _ := json.Marshal(map[string]string{"secret": encrypted})
	challenge, err := s.newChallenge(w, r, "totp_setup", id, string(blob))
	if err != nil {
		respondError(w, 500, "Could not start setup.", "internal")
		return
	}
	respondJSON(w, 200, map[string]string{"challenge": challenge, "secret": secret, "uri": gologin.TOTPURI("Lifeai", email, secret)})
}

func (s *Server) HandleTOTPConfirm(w http.ResponseWriter, r *http.Request) {
	var req mfaRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	c, err := s.readChallenge(r, "totp_setup", req.Challenge, false)
	if err != nil || c.UserID != UserID(r.Context()) {
		respondError(w, 401, "Setup expired. Start again.", "challenge_expired")
		return
	}
	var stored map[string]string
	if json.Unmarshal([]byte(c.Data), &stored) != nil {
		respondError(w, 500, "Could not confirm setup.", "internal")
		return
	}
	secret, err := s.cipher.Open(stored["secret"])
	if err != nil {
		respondError(w, 500, "Could not confirm setup.", "internal")
		return
	}
	step := matchingTOTPStep(secret, req.Code, time.Now())
	if step < 0 {
		respondError(w, 400, "Enter the six-digit code from your authenticator.", "invalid_code")
		return
	}
	codes, hashes, err := gologin.NewRecoveryCodes()
	if err != nil {
		respondError(w, 500, "Could not create recovery codes.", "internal")
		return
	}
	tx, err := s.securityTransaction(r.Context(), c.UserID)
	if err != nil {
		respondError(w, 500, "Could not enable two-factor authentication.", "internal")
		return
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(r.Context(), `UPDATE users SET totp_secret_enc=?,totp_last_step=?,security_version=security_version+1,mfa_failures=0,mfa_locked_until=0 WHERE id=? AND security_version=? AND totp_secret_enc=''`, stored["secret"], step, c.UserID, c.Version)
	if err != nil {
		respondError(w, 500, "Could not enable two-factor authentication.", "internal")
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		respondError(w, 409, "Account security changed. Start setup again.", "security_changed")
		return
	}
	if err = replaceRecoveryCodes(r.Context(), tx, c.UserID, hashes); err != nil {
		respondError(w, 500, "Could not save recovery codes.", "internal")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM auth_challenges WHERE user_id=?`, c.UserID); err != nil {
		respondError(w, 500, "Could not finish setup.", "internal")
		return
	}
	if err = tx.Commit(); err != nil {
		respondError(w, 500, "Could not finish setup.", "internal")
		return
	}
	http.SetCookie(w, s.challengeCookie("totp_setup", "", -1))
	s.securityResult(w, r, c.UserID, true, codes)
}

func replaceRecoveryCodes(ctx context.Context, tx *sql.Tx, id int64, hashes []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id=?`, id); err != nil {
		return err
	}
	for _, hash := range hashes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_codes(user_id,code_hash) VALUES(?,?)`, id, hash); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) securityResult(w http.ResponseWriter, r *http.Request, id int64, mfa bool, codes []string) {
	claims, ok := r.Context().Value(SessionClaimsKey).(*auth.Claims)
	if !ok {
		respondError(w, 401, "Sign in again.", "unauthorized")
		return
	}
	out, err := s.sessionResponse(r.Context(), id, mfa, claims.SecurityVersion+1)
	if err != nil {
		respondError(w, 500, "Sign in again to continue.", "internal")
		return
	}
	respondJSON(w, 200, struct {
		authResponse
		RecoveryCodes []string `json:"recovery_codes,omitempty"`
	}{out, codes})
}

func (s *Server) HandleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	id := UserID(r.Context())
	tx, err := s.securityTransaction(r.Context(), id)
	if err != nil {
		respondError(w, 500, "Could not change security settings.", "internal")
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `UPDATE users SET totp_secret_enc='',totp_last_step=-1,mfa_failures=0,mfa_locked_until=0,security_version=security_version+1 WHERE id=?`, id); err != nil {
		respondError(w, 500, "Could not disable two-factor authentication.", "internal")
		return
	}
	if err = replaceRecoveryCodes(r.Context(), tx, id, nil); err != nil {
		respondError(w, 500, "Could not remove recovery codes.", "internal")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM auth_challenges WHERE user_id=?`, id); err != nil {
		respondError(w, 500, "Could not finish security change.", "internal")
		return
	}
	if err = tx.Commit(); err != nil {
		respondError(w, 500, "Could not finish security change.", "internal")
		return
	}
	s.securityResult(w, r, id, true, nil)
}

func (s *Server) HandleRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	id := UserID(r.Context())
	codes, hashes, err := gologin.NewRecoveryCodes()
	if err != nil {
		respondError(w, 500, "Could not generate recovery codes.", "internal")
		return
	}
	tx, err := s.securityTransaction(r.Context(), id)
	if err != nil {
		respondError(w, 500, "Could not update recovery codes.", "internal")
		return
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(r.Context(), `UPDATE users SET security_version=security_version+1 WHERE id=? AND totp_secret_enc<>''`, id)
	if err != nil {
		respondError(w, 500, "Could not update recovery codes.", "internal")
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		respondError(w, 409, "Enable two-factor authentication first.", "totp_disabled")
		return
	}
	if err = replaceRecoveryCodes(r.Context(), tx, id, hashes); err != nil {
		respondError(w, 500, "Could not save recovery codes.", "internal")
		return
	}
	if err = tx.Commit(); err != nil {
		respondError(w, 500, "Could not save recovery codes.", "internal")
		return
	}
	s.securityResult(w, r, id, true, codes)
}
