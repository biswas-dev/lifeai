package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	gologin "github.com/anchoo2kewl/go-login"
	"github.com/biswas-dev/lifeai/api/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

type passkeyStore struct{ db *db.DB }

func (s passkeyStore) PasskeyUserByID(ctx context.Context, id int64) (gologin.PasskeyUser, error) {
	var u gologin.PasskeyUser
	err := s.db.QueryRowContext(ctx, `SELECT id,email,name FROM users WHERE id=? AND deleted_at IS NULL`, id).Scan(&u.ID, &u.Email, &u.DisplayName)
	if err != nil {
		return u, gologin.ErrPasskeyUnknownUser
	}
	return u, nil
}

func (s passkeyStore) PasskeyCredentials(ctx context.Context, id int64) ([]gologin.PasskeyCredential, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT credential_id,public_key,sign_count,backup_eligible,transports_json,attestation_type FROM passkeys WHERE user_id=?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []gologin.PasskeyCredential{}
	for rows.Next() {
		var c gologin.PasskeyCredential
		var encoded, transports string
		// go-login 0.3 maps BackedUp to WebAuthn's immutable BackupEligible
		// flag on reads. Supply eligibility here; actual backup state is kept
		// separately for display and updated after verified assertions.
		if err = rows.Scan(&encoded, &c.PublicKey, &c.SignCount, &c.BackedUp, &transports, &c.AttestationType); err != nil {
			return nil, err
		}
		c.ID, err = base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(transports), &c.Transports); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Apply stricter policy to both the browser options and the server session.
// Changing only the browser request would not enforce verification on replies.
func requirePasskeyVerification(options any, session []byte) ([]byte, error) {
	switch opts := options.(type) {
	case *protocol.CredentialCreation:
		opts.Response.AuthenticatorSelection.UserVerification = protocol.VerificationRequired
		opts.Response.AuthenticatorSelection.ResidentKey = protocol.ResidentKeyRequirementRequired
		required := true
		opts.Response.AuthenticatorSelection.RequireResidentKey = &required
	case *protocol.CredentialAssertion:
		opts.Response.UserVerification = protocol.VerificationRequired
	default:
		return nil, errors.New("unexpected passkey options")
	}
	var data webauthn.SessionData
	if err := json.Unmarshal(session, &data); err != nil {
		return nil, err
	}
	data.UserVerification = protocol.VerificationRequired
	return json.Marshal(data)
}

func (s *Server) HandlePasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if s.passkeys == nil {
		respondError(w, 503, "Passkeys require a supported browser and HTTPS.", "unavailable")
		return
	}
	u, err := (passkeyStore{s.db}).PasskeyUserByID(r.Context(), UserID(r.Context()))
	if err != nil {
		respondError(w, 401, "Sign in again.", "unauthorized")
		return
	}
	var count int
	if err = s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM passkeys WHERE user_id=?`, u.ID).Scan(&count); err != nil {
		respondError(w, 500, "Could not load passkeys.", "internal")
		return
	}
	if count >= 20 {
		respondError(w, 409, "Remove an unused passkey before adding another.", "passkey_limit")
		return
	}
	options, session, err := s.passkeys.BeginRegistration(r.Context(), u)
	if err == nil {
		session, err = requirePasskeyVerification(options, session)
	}
	if err != nil {
		respondError(w, 500, "Could not begin passkey setup.", "internal")
		return
	}
	challenge, err := s.newChallenge(w, r, "passkey_register", u.ID, string(session))
	if err != nil {
		respondError(w, 500, "Could not begin passkey setup.", "internal")
		return
	}
	respondJSON(w, 200, map[string]any{"challenge": challenge, "options": options})
}

type passkeyFinishRequest struct {
	Challenge  string          `json:"challenge"`
	Name       string          `json:"name"`
	Credential json.RawMessage `json:"credential"`
}

func (s *Server) HandlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if s.passkeys == nil {
		respondError(w, 503, "Passkeys are unavailable.", "unavailable")
		return
	}
	var req passkeyFinishRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	c, err := s.readChallenge(r, "passkey_register", req.Challenge, true)
	if err != nil || c.UserID != UserID(r.Context()) {
		respondError(w, 401, "Passkey setup expired. Start again.", "challenge_expired")
		return
	}
	http.SetCookie(w, s.challengeCookie("passkey_register", "", -1))
	u, err := (passkeyStore{s.db}).PasskeyUserByID(r.Context(), c.UserID)
	if err != nil {
		respondError(w, 401, "Account unavailable.", "unauthorized")
		return
	}
	credential, err := s.passkeys.FinishRegistration(r.Context(), u, []byte(c.Data), req.Credential)
	if err != nil {
		respondError(w, 400, "Passkey could not be verified. Try again on this site with device verification enabled.", "passkey_rejected")
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(req.Credential)
	if err != nil {
		respondError(w, 400, "Passkey could not be verified.", "passkey_rejected")
		return
	}
	eligible := parsed.Response.AttestationObject.AuthData.Flags.HasBackupEligible()
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "My passkey"
	}
	if len(name) > 80 {
		respondError(w, 400, "Use a passkey name under 80 characters.", "invalid_name")
		return
	}
	transports, _ := json.Marshal(credential.Transports)
	tx, err := s.securityTransaction(r.Context(), u.ID)
	if err != nil {
		respondError(w, 500, "Could not save passkey.", "internal")
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO passkeys(credential_id,user_id,public_key,sign_count,backup_eligible,backed_up,transports_json,attestation_type,name) VALUES(?,?,?,?,?,?,?,?,?)`,
		base64.RawURLEncoding.EncodeToString(credential.ID), u.ID, credential.PublicKey, credential.SignCount, eligible, credential.BackedUp, string(transports), credential.AttestationType, name)
	if err != nil {
		respondError(w, 409, "That passkey is already registered or could not be saved. Try another passkey.", "passkey_conflict")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE users SET security_version=security_version+1 WHERE id=? AND security_version=?`, u.ID, c.Version); err != nil {
		respondError(w, 500, "Could not update account security.", "internal")
		return
	}
	if err = tx.Commit(); err != nil {
		respondError(w, 500, "Could not save passkey.", "internal")
		return
	}
	s.securityResult(w, r, u.ID, true, nil)
}

func (s *Server) HandlePasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if s.passkeys == nil {
		respondError(w, 503, "Passkeys are unavailable.", "unavailable")
		return
	}
	options, session, err := s.passkeys.BeginLogin(r.Context())
	if err == nil {
		session, err = requirePasskeyVerification(options, session)
	}
	if err != nil {
		respondError(w, 500, "Could not start passkey sign-in.", "internal")
		return
	}
	challenge, err := s.newChallenge(w, r, "passkey_login", 0, string(session))
	if err != nil {
		respondError(w, 500, "Could not start passkey sign-in.", "internal")
		return
	}
	respondJSON(w, 200, map[string]any{"challenge": challenge, "options": options})
}

func (s *Server) HandlePasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.passkeys == nil {
		respondError(w, 503, "Passkeys are unavailable.", "unavailable")
		return
	}
	var req passkeyFinishRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	c, err := s.readChallenge(r, "passkey_login", req.Challenge, true)
	if err != nil {
		respondError(w, 401, "Passkey sign-in expired. Try again.", "challenge_expired")
		return
	}
	http.SetCookie(w, s.challengeCookie("passkey_login", "", -1))
	u, credential, err := s.passkeys.FinishLogin(r.Context(), []byte(c.Data), req.Credential)
	if err != nil {
		respondError(w, 401, "Passkey was not accepted. Try another sign-in method.", "passkey_rejected")
		return
	}
	var version int64
	if err = s.db.QueryRowContext(r.Context(), `SELECT security_version FROM users WHERE id=? AND deleted_at IS NULL`, u.ID).Scan(&version); err != nil {
		respondError(w, 401, "Account unavailable.", "unauthorized")
		return
	}
	res, err := s.db.ExecContext(r.Context(), `UPDATE passkeys SET sign_count=?,backed_up=?,last_used_at=CURRENT_TIMESTAMP WHERE credential_id=? AND user_id=? AND (sign_count<? OR (sign_count=0 AND ?=0))`,
		credential.SignCount, credential.BackedUp, base64.RawURLEncoding.EncodeToString(credential.ID), u.ID, credential.SignCount, credential.SignCount)
	if err != nil {
		respondError(w, 500, "Could not finish passkey sign-in.", "internal")
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		respondError(w, 401, "Passkey is no longer available. Try another sign-in method.", "passkey_rejected")
		return
	}
	// User verification is required by the server: possession plus a device
	// PIN or biometric satisfies the account's second-factor requirement.
	out, err := s.sessionResponse(r.Context(), u.ID, true, version)
	if err != nil {
		respondError(w, 401, "Account security changed. Sign in again.", "passkey_rejected")
		return
	}
	respondJSON(w, 200, out)
}

func (s *Server) HandlePasskeyDelete(w http.ResponseWriter, r *http.Request) {
	id := UserID(r.Context())
	tx, err := s.securityTransaction(r.Context(), id)
	if err != nil {
		respondError(w, 500, "Could not remove passkey.", "internal")
		return
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(r.Context(), `DELETE FROM passkeys WHERE credential_id=? AND user_id=?`, chi.URLParam(r, "credentialID"), id)
	if err != nil {
		respondError(w, 500, "Could not remove passkey.", "internal")
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		respondError(w, 404, "Passkey not found.", "not_found")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE users SET security_version=security_version+1 WHERE id=?`, id); err != nil {
		respondError(w, 500, "Could not update account security.", "internal")
		return
	}
	if err = tx.Commit(); err != nil {
		respondError(w, 500, "Could not remove passkey.", "internal")
		return
	}
	s.securityResult(w, r, id, true, nil)
}
