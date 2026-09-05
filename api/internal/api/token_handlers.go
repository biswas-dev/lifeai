package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	goapi "github.com/anchoo2kewl/go-api"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// TokenScheme is this application's slice of the shared go-api token format.
var TokenScheme = goapi.NewScheme("lai_")

// APIToken is a token as listed. The secret itself is never included.
type APIToken struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type createTokenRequest struct {
	Name          string   `json:"name"`
	Scopes        []string `json:"scopes"`
	ExpiresInDays int      `json:"expires_in_days"`
}

// HandleCreateToken issues a new personal API token. The plaintext is
// returned exactly once.
func (s *Server) HandleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req createTokenRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	userID := UserID(ctx)
	if TokenAuth(ctx) {
		respondError(w, http.StatusForbidden,
			"sign in to create a token; an API token cannot issue another", "token_cannot_mint")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "API token"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	scopes := goapi.Scopes(req.Scopes).Normalise()
	var expiresAt any
	if req.ExpiresInDays > 0 {
		expiresAt = time.Now().UTC().AddDate(0, 0, req.ExpiresInDays)
	}
	cred, err := TokenScheme.Generate()
	if err != nil {
		s.log.Error("generate api token", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "could not generate a token", "internal")
		return
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO api_tokens (user_id, name, token_hash, prefix, scopes, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		userID, name, cred.Hash, cred.Prefix, scopes.String(), expiresAt)
	if err != nil {
		s.log.Error("create api token", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "could not create the token", "internal")
		return
	}
	id, _ := res.LastInsertId()
	token, err := s.tokenByID(ctx, userID, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load the token", "internal")
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{
		"token":     token,
		"secret":    cred.Plaintext,
		"discovery": goapi.NewDiscovery(s.cfg.AppURL, cred.Plaintext, scopes),
	})
}

// HandleListTokens lists the caller's tokens, secrets excluded.
func (s *Server) HandleListTokens(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, prefix, scopes, last_used_at, expires_at, created_at
		  FROM api_tokens WHERE user_id = ? AND revoked_at IS NULL ORDER BY created_at DESC`, UserID(ctx))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not list tokens", "internal")
		return
	}
	defer rows.Close()
	out := []APIToken{}
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "could not read tokens", "internal")
			return
		}
		out = append(out, t)
	}
	respondJSON(w, http.StatusOK, out)
}

// HandleRevokeToken retires a token.
func (s *Server) HandleRevokeToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "tokenID"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid token id", "invalid_id")
		return
	}
	res, err := s.db.ExecContext(r.Context(),
		`UPDATE api_tokens SET revoked_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		id, UserID(r.Context()))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not revoke the token", "internal")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondError(w, http.StatusNotFound, "no such token", "not_found")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) tokenByID(ctx context.Context, userID, id int64) (APIToken, error) {
	return scanAPIToken(s.db.QueryRowContext(ctx, `
		SELECT id, name, prefix, scopes, last_used_at, expires_at, created_at
		  FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID))
}

func scanAPIToken(row scanner) (APIToken, error) {
	var (
		t                 APIToken
		scopes            string
		lastUsed, expires sql.NullTime
	)
	err := row.Scan(&t.ID, &t.Name, &t.Prefix, &scopes, &lastUsed, &expires, &t.CreatedAt)
	t.Scopes = goapi.ParseScopes(scopes)
	if lastUsed.Valid {
		t.LastUsedAt = &lastUsed.Time
	}
	if expires.Valid {
		t.ExpiresAt = &expires.Time
	}
	return t, err
}

// TokenScopeKey carries the scopes when a request authenticated with an API
// token rather than a login.
const TokenScopeKey contextKey = "token_scopes"

// TokenAuth reports whether the request was authenticated with an API token.
func TokenAuth(ctx context.Context) bool {
	_, ok := ctx.Value(TokenScopeKey).(goapi.Scopes)
	return ok
}

type tokenStore struct{ s *Server }

func (t tokenStore) Lookup(ctx context.Context, hash string) (goapi.Record, int64, error) {
	var (
		userID  int64
		scopes  string
		active  bool
		deleted bool
		expires sql.NullTime
	)
	err := t.s.db.QueryRowContext(ctx, `
		SELECT tok.user_id, tok.scopes, tok.revoked_at IS NULL, tok.expires_at, u.deleted_at IS NOT NULL
		  FROM api_tokens tok JOIN users u ON u.id = tok.user_id
		 WHERE tok.token_hash = ?`, hash).
		Scan(&userID, &scopes, &active, &expires, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return goapi.Record{}, 0, goapi.ErrNotFound
	}
	if err != nil {
		return goapi.Record{}, 0, err
	}
	if deleted {
		return goapi.Record{}, 0, goapi.ErrSubjectUnavailable
	}
	rec := goapi.Record{Scopes: goapi.ParseScopes(scopes), Active: active}
	if expires.Valid {
		rec.ExpiresAt = &expires.Time
	}
	return rec, userID, nil
}

func (t tokenStore) Touch(ctx context.Context, hash string) error {
	_, err := t.s.db.ExecContext(ctx,
		`UPDATE api_tokens SET last_used_at = CURRENT_TIMESTAMP WHERE token_hash = ?`, hash)
	return err
}

func (s *Server) tokenAuthenticator() *goapi.Authenticator[int64] {
	a := goapi.NewAuthenticator[int64](TokenScheme, tokenStore{s})
	a.OnTouchError = func(err error) {
		s.log.Warn("recording api token use", zap.Error(err))
	}
	return a
}
