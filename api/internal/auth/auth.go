// Package auth issues and validates the app's JWTs and hashes passwords.
//
// Claims is field- and tag-identical to go-login's Claims so tokens minted by
// the OAuth flow validate here without translation.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// BcryptCost is the work factor for password hashing.
var BcryptCost = 12

// SetBcryptCost overrides the hashing cost. Intended for tests only.
func SetBcryptCost(cost int) { BcryptCost = cost }

// ErrInvalidToken is returned for any token that fails to parse or validate.
var ErrInvalidToken = errors.New("auth: invalid token")

// Claims is the JWT payload. Wire-compatible with go-login.
type Claims struct {
	UserID          int64  `json:"user_id"`
	Email           string `json:"email"`
	SecurityVersion int64  `json:"security_version,omitempty"`
	MFA             bool   `json:"mfa,omitempty"`
	AuthTime        int64  `json:"auth_time,omitempty"`
	jwt.RegisteredClaims
}

// GenerateToken mints an HS256 token for the user.
func GenerateToken(userID int64, email, secret string, expiry time.Duration) (string, error) {
	return GenerateSessionToken(userID, email, secret, expiry, 0, false)
}

// GenerateSessionToken retains go-login's claims and adds revocation and
// completed-factor state. Challenge tokens are never valid session JWTs.
func GenerateSessionToken(userID int64, email, secret string, expiry time.Duration, version int64, mfa bool) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:          userID,
		Email:           email,
		SecurityVersion: version,
		MFA:             mfa,
		AuthTime:        now.Unix(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// ValidateToken parses and verifies a token, rejecting anything not signed
// with HMAC so a caller cannot downgrade to "alg": "none".
func ValidateToken(tokenString, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, ErrInvalidToken
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// HashPassword returns a bcrypt hash of password.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return string(b), nil
}

// VerifyPassword reports whether password matches the stored hash.
func VerifyPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
