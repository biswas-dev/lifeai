package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/biswas-dev/lifeai/api/internal/auth"
)

// User is the account shape returned to the SPA. It never carries the hash.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	AvatarURL    string `json:"avatar_url"`
	Timezone     string `json:"timezone"`
	IsAdmin      bool   `json:"is_admin"`
	AuthProvider string `json:"auth_provider"`
	WeightUnit   string `json:"weight_unit"`
	// DOB is YYYY-MM-DD; Sex is male, female, other or empty; HeightCm is
	// what BMI and the coach use.
	DOB       string   `json:"dob"`
	Sex       string   `json:"sex"`
	HeightCm  *float64 `json:"height_cm"`
	CreatedAt string   `json:"created_at"`
	// Hard75Eligible says whether the settings screen should offer the
	// 75hard connection at all.
	Hard75Eligible bool `json:"hard75_eligible"`
}

type authResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

type signupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
}

// HandleSignup creates an account and returns a token.
func (s *Server) HandleSignup(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AllowSignup {
		respondError(w, http.StatusForbidden, "registration is closed", "signup_disabled")
		return
	}
	var req signupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	email := normalizeEmail(req.Email)
	if !validEmail(email) {
		respondError(w, http.StatusBadRequest, "a valid email is required", "invalid_email")
		return
	}
	if len(req.Password) < 8 {
		respondError(w, http.StatusBadRequest, "password must be at least 8 characters", "weak_password")
		return
	}
	tz := strings.TrimSpace(req.Timezone)
	if _, err := time.LoadLocation(tz); tz == "" || err != nil {
		tz = "UTC"
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.log.Error("hash password", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "could not create account", "internal")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name, _, _ = strings.Cut(email, "@")
	}
	res, err := s.db.ExecContext(r.Context(),
		`INSERT INTO users (email, password_hash, name, timezone, auth_provider) VALUES (?, ?, ?, ?, 'password')`,
		email, hash, name, tz)
	if err != nil {
		if isUniqueViolation(err) {
			respondError(w, http.StatusConflict, "an account with that email already exists", "email_taken")
			return
		}
		s.log.Error("create user", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "could not create account", "internal")
		return
	}
	id, _ := res.LastInsertId()
	user, err := s.getUser(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load account", "internal")
		return
	}
	s.issueToken(w, user)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// HandleLogin exchanges credentials for a token.
func (s *Server) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	email := normalizeEmail(req.Email)
	var (
		id   int64
		hash string
	)
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id, password_hash FROM users WHERE lower(email) = ? AND deleted_at IS NULL`,
		email).Scan(&id, &hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			auth.HashPassword(req.Password) //nolint:errcheck // timing padding
			respondError(w, http.StatusUnauthorized, "invalid email or password", "invalid_credentials")
			return
		}
		s.log.Error("login lookup", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "could not sign in", "internal")
		return
	}
	if err := auth.VerifyPassword(hash, req.Password); err != nil {
		respondError(w, http.StatusUnauthorized, "invalid email or password", "invalid_credentials")
		return
	}
	user, err := s.getUser(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load account", "internal")
		return
	}
	s.issueToken(w, user)
}

// HandleMe returns the signed-in account.
func (s *Server) HandleMe(w http.ResponseWriter, r *http.Request) {
	user, err := s.getUser(r.Context(), UserID(r.Context()))
	if err != nil {
		respondError(w, http.StatusNotFound, "account not found", "not_found")
		return
	}
	respondJSON(w, http.StatusOK, user)
}

type updateProfileRequest struct {
	Name       *string  `json:"name"`
	Timezone   *string  `json:"timezone"`
	WeightUnit *string  `json:"weight_unit"`
	DOB        *string  `json:"dob"`
	Sex        *string  `json:"sex"`
	HeightCm   *float64 `json:"height_cm"`
}

// HandleUpdateProfile updates the display name, timezone and unit preference.
func (s *Server) HandleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	var req updateProfileRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	userID := UserID(ctx)
	if req.Name != nil {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE users SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			strings.TrimSpace(*req.Name), userID); err != nil {
			respondError(w, http.StatusInternalServerError, "could not update profile", "internal")
			return
		}
	}
	if req.Timezone != nil {
		tz := strings.TrimSpace(*req.Timezone)
		if _, err := time.LoadLocation(tz); err != nil || tz == "" {
			respondError(w, http.StatusBadRequest, "unknown timezone", "invalid_timezone")
			return
		}
		if _, err := s.db.ExecContext(ctx,
			`UPDATE users SET timezone = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, tz, userID); err != nil {
			respondError(w, http.StatusInternalServerError, "could not update profile", "internal")
			return
		}
	}
	if req.WeightUnit != nil {
		unit := strings.ToLower(strings.TrimSpace(*req.WeightUnit))
		if unit != "kg" && unit != "lb" {
			respondError(w, http.StatusBadRequest, "weight unit must be kg or lb", "invalid_unit")
			return
		}
		if _, err := s.db.ExecContext(ctx,
			`UPDATE users SET weight_unit = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, unit, userID); err != nil {
			respondError(w, http.StatusInternalServerError, "could not update profile", "internal")
			return
		}
	}
	if req.DOB != nil {
		dob := strings.TrimSpace(*req.DOB)
		if dob != "" {
			if _, err := time.Parse("2006-01-02", dob); err != nil {
				respondError(w, http.StatusBadRequest, "dob must be YYYY-MM-DD", "invalid_dob")
				return
			}
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE users SET dob = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, dob, userID); err != nil {
			respondError(w, http.StatusInternalServerError, "could not update profile", "internal")
			return
		}
	}
	if req.Sex != nil {
		sex := strings.ToLower(strings.TrimSpace(*req.Sex))
		if sex != "" && sex != "male" && sex != "female" && sex != "other" {
			respondError(w, http.StatusBadRequest, "sex must be male, female or other", "invalid_sex")
			return
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE users SET sex = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, sex, userID); err != nil {
			respondError(w, http.StatusInternalServerError, "could not update profile", "internal")
			return
		}
	}
	if req.HeightCm != nil {
		var h any
		if *req.HeightCm > 0 {
			if *req.HeightCm < 50 || *req.HeightCm > 272 {
				respondError(w, http.StatusBadRequest, "height must be between 50 and 272 cm", "invalid_height")
				return
			}
			h = *req.HeightCm
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE users SET height_cm = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, h, userID); err != nil {
			respondError(w, http.StatusInternalServerError, "could not update profile", "internal")
			return
		}
	}
	user, err := s.getUser(ctx, userID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load account", "internal")
		return
	}
	respondJSON(w, http.StatusOK, user)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// HandleChangePassword rotates the account password.
func (s *Server) HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req changePasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.NewPassword) < 8 {
		respondError(w, http.StatusBadRequest, "password must be at least 8 characters", "weak_password")
		return
	}
	userID := UserID(r.Context())
	var hash string
	if err := s.db.QueryRowContext(r.Context(),
		`SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash); err != nil {
		respondError(w, http.StatusNotFound, "account not found", "not_found")
		return
	}
	if err := auth.VerifyPassword(hash, req.CurrentPassword); err != nil {
		respondError(w, http.StatusUnauthorized, "current password is incorrect", "invalid_credentials")
		return
	}
	newHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not update password", "internal")
		return
	}
	if _, err := s.db.ExecContext(r.Context(),
		`UPDATE users SET password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, newHash, userID); err != nil {
		respondError(w, http.StatusInternalServerError, "could not update password", "internal")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) issueToken(w http.ResponseWriter, user User) {
	token, err := auth.GenerateToken(user.ID, user.Email, s.cfg.JWTSecret, s.cfg.JWTExpiry)
	if err != nil {
		s.log.Error("generate token", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "could not issue token", "internal")
		return
	}
	respondJSON(w, http.StatusOK, authResponse{Token: token, User: user})
}

func (s *Server) getUser(ctx context.Context, id int64) (User, error) {
	var u User
	var height sql.NullFloat64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, name, avatar_url, timezone, is_admin, auth_provider, weight_unit, dob, sex, height_cm, created_at
		 FROM users WHERE id = ? AND deleted_at IS NULL`, id).
		Scan(&u.ID, &u.Email, &u.Name, &u.AvatarURL, &u.Timezone, &u.IsAdmin, &u.AuthProvider,
			&u.WeightUnit, &u.DOB, &u.Sex, &height, &u.CreatedAt)
	u.HeightCm = floatPtr(height)
	if err == nil {
		u.Hard75Eligible = s.cfg.Hard75Allowed(u.Email)
	}
	return u, err
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validEmail(email string) bool {
	at := strings.Index(email, "@")
	if at < 1 || at == len(email)-1 {
		return false
	}
	domain := email[at+1:]
	return strings.Contains(domain, ".") && !strings.Contains(email, " ")
}
