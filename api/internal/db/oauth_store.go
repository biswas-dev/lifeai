package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	gologin "github.com/anchoo2kewl/go-login"
	"golang.org/x/crypto/bcrypt"
)

// OAuthStore implements gologin.UserStore against the users table.
type OAuthStore struct {
	db *DB
}

// NewOAuthStore builds the store go-login writes through.
func NewOAuthStore(d *DB) *OAuthStore { return &OAuthStore{db: d} }

// FindUserByProviderID looks up a user by their identity at an OAuth provider.
func (s *OAuthStore) FindUserByProviderID(ctx context.Context, provider, providerUserID string) (*gologin.User, error) {
	var u gologin.User
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.email
		FROM oauth_providers op
		JOIN users u ON u.id = op.user_id
		WHERE op.provider = ? AND op.provider_user_id = ? AND u.deleted_at IS NULL`,
		provider, providerUserID).Scan(&u.ID, &u.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("oauth: find by provider id: %w", err)
	}
	return &u, nil
}

// FindUserByEmail looks up an existing account by email.
func (s *OAuthStore) FindUserByEmail(ctx context.Context, email string) (*gologin.User, error) {
	var u gologin.User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email FROM users WHERE lower(email) = ? AND deleted_at IS NULL`,
		strings.ToLower(strings.TrimSpace(email))).Scan(&u.ID, &u.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("oauth: find by email: %w", err)
	}
	return &u, nil
}

// GetUserAuthProvider reports how the account signs in.
func (s *OAuthStore) GetUserAuthProvider(ctx context.Context, userID int64) (string, error) {
	var provider string
	if err := s.db.QueryRowContext(ctx,
		`SELECT auth_provider FROM users WHERE id = ?`, userID).Scan(&provider); err != nil {
		return "", fmt.Errorf("oauth: get auth provider: %w", err)
	}
	return provider, nil
}

// CreateOAuthUser provisions an account from a provider profile. inviteCode is
// ignored: registration is open.
func (s *OAuthStore) CreateOAuthUser(ctx context.Context, info gologin.ProviderUserInfo, provider, inviteCode string) (*gologin.User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("oauth: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	name := strings.TrimSpace(info.Name)
	if name == "" {
		name = strings.TrimSpace(info.FirstName + " " + info.LastName)
	}
	if name == "" {
		name, _, _ = strings.Cut(info.Email, "@")
	}

	placeholder, err := randomPlaceholderHash()
	if err != nil {
		return nil, err
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO users (email, password_hash, name, avatar_url, auth_provider)
		VALUES (?, ?, ?, ?, ?)`,
		strings.ToLower(strings.TrimSpace(info.Email)), placeholder, name, info.AvatarURL, provider)
	if err != nil {
		return nil, fmt.Errorf("oauth: create user: %w", err)
	}
	userID, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("oauth: user id: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO oauth_providers (user_id, provider, provider_user_id) VALUES (?, ?, ?)`,
		userID, provider, info.ProviderUserID); err != nil {
		return nil, fmt.Errorf("oauth: link provider: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("oauth: commit: %w", err)
	}
	return &gologin.User{ID: userID, Email: info.Email}, nil
}

// ValidateInviteCode always reports the code as valid: registration is open.
func (s *OAuthStore) ValidateInviteCode(ctx context.Context, code string) (*gologin.InviteInfo, error) {
	return &gologin.InviteInfo{Code: code}, nil
}

// LinkOAuthProvider attaches a provider identity to an existing account.
func (s *OAuthStore) LinkOAuthProvider(ctx context.Context, userID int64, provider, providerUserID string) (*gologin.User, error) {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO oauth_providers (user_id, provider, provider_user_id)
		VALUES (?, ?, ?)
		ON CONFLICT(user_id, provider) DO NOTHING`,
		userID, provider, providerUserID); err != nil {
		return nil, fmt.Errorf("oauth: link provider: %w", err)
	}
	linked, err := s.FindUserByProviderID(ctx, provider, providerUserID)
	if err != nil || linked == nil || linked.ID != userID {
		return nil, errors.New("oauth: account already linked to a different identity")
	}
	var u gologin.User
	if err := s.db.QueryRowContext(ctx,
		`SELECT id, email FROM users WHERE id = ?`, userID).Scan(&u.ID, &u.Email); err != nil {
		return nil, fmt.Errorf("oauth: reload user: %w", err)
	}
	return &u, nil
}

func randomPlaceholderHash() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("oauth: random: %w", err)
	}
	h, err := bcrypt.GenerateFromPassword([]byte(hex.EncodeToString(buf)), bcrypt.MinCost)
	if err != nil {
		return "", fmt.Errorf("oauth: placeholder hash: %w", err)
	}
	return string(h), nil
}
