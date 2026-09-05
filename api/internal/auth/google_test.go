package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/biswas-dev/lifeai/api/internal/config"
	"github.com/biswas-dev/lifeai/api/internal/db"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
)

func TestGoogleSignIn(t *testing.T) {
	for _, tc := range []struct {
		name, email, domain, cookieMode     string
		existing, verified, signup, success bool
	}{
		{name: "existing Gmail keeps account", email: "owner@gmail.com", existing: true, verified: true, success: true},
		{name: "new Google user without invite", email: "new@gmail.com", verified: true, signup: true, success: true},
		{name: "new third-party address", email: "new@example.com", verified: true, signup: true, success: true},
		{name: "verified Workspace link", email: "owner@example.com", domain: "example.com", existing: true, verified: true, success: true},
		{name: "unverified email rejected", email: "owner@gmail.com", existing: true, signup: true},
		{name: "third-party email cannot take over password account", email: "owner@example.com", existing: true, verified: true, signup: true},
		{name: "closed registration", email: "new@gmail.com", verified: true},
		{name: "missing browser cookie", email: "owner@gmail.com", existing: true, verified: true, cookieMode: "missing"},
		{name: "different browser cookie", email: "owner@gmail.com", existing: true, verified: true, cookieMode: "wrong"},
		{name: "expired state", email: "owner@gmail.com", existing: true, verified: true, cookieMode: "expired"},
		{name: "tampered state", email: "owner@gmail.com", existing: true, verified: true, cookieMode: "tampered"},
		{name: "cancelled sign-in", email: "owner@gmail.com", existing: true, verified: true, cookieMode: "cancelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			if _, err := database.Migrate(); err != nil {
				t.Fatal(err)
			}
			var existingID int64
			if tc.existing {
				result, err := database.Exec(`INSERT INTO users(email,password_hash,name) VALUES (?, 'retained-password', 'Original name')`, tc.email)
				if err != nil {
					t.Fatal(err)
				}
				existingID, _ = result.LastInsertId()
			}
			cfg := &config.Config{AppURL: "https://lifeai.example", GoogleClientID: "test-client",
				GoogleClientSecret: "test-secret", OAuthStateSecret: "test-state-secret",
				OAuthSuccessURL: "https://lifeai.example/oauth/callback", OAuthErrorURL: "https://lifeai.example/login",
				JWTSecret: "test-jwt-secret", JWTExpiry: time.Hour, AllowSignup: tc.signup}
			h := NewGoogleHandler(cfg, db.NewOAuthStore(database), zap.NewNop())
			start := httptest.NewRecorder()
			h.Initiate(start, httptest.NewRequest(http.MethodGet, cfg.AppURL+"/api/auth/google", nil))
			location, _ := url.Parse(start.Header().Get("Location"))
			params := location.Query()
			cookie := start.Result().Cookies()[0]
			if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Domain != "" || cookie.Path != "/" {
				t.Fatal("unsafe OAuth cookie")
			}
			if params.Get("scope") != "openid email profile" || params.Get("code_challenge_method") != "S256" {
				t.Fatal("missing minimal scopes or PKCE")
			}
			calls := 0
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch r.URL.Path {
				case "/token":
					if r.Method != http.MethodPost {
						t.Error("token exchange must POST")
					}
					_ = r.ParseForm()
					if r.Form.Get("client_secret") != cfg.GoogleClientSecret || r.Form.Get("redirect_uri") != cfg.AppURL+"/api/auth/google/callback" || googleChallenge(r.Form.Get("code_verifier")) != params.Get("code_challenge") {
						t.Error("incorrect token exchange binding")
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test-access-token"})
				case "/userinfo":
					if r.Header.Get("Authorization") != "Bearer test-access-token" {
						t.Error("missing provider token")
					}
					_ = json.NewEncoder(w).Encode(googleProfile{Sub: "google-subject", Email: tc.email, EmailVerified: tc.verified, HostedDomain: tc.domain, Name: "Google name"})
				default:
					t.Error("unexpected provider request")
				}
			}))
			defer provider.Close()
			h.tokenURL, h.userInfoURL = provider.URL+"/token", provider.URL+"/userinfo"
			state := params.Get("state")
			if tc.cookieMode == "expired" {
				state, _ = jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
					Subject: googleChallenge(cookie.Value), Issuer: "lifeai-google", Audience: jwt.ClaimStrings{cfg.GoogleClientID},
					ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
				}).SignedString([]byte(cfg.OAuthStateSecret))
			}
			if tc.cookieMode == "tampered" {
				state += "invalid"
			}
			callback := httptest.NewRequest(http.MethodGet, cfg.AppURL+"/api/auth/google/callback?code=valid-code&state="+url.QueryEscape(state), nil)
			if tc.cookieMode == "cancelled" {
				callback.URL.RawQuery += "&error=access_denied"
			}
			if tc.cookieMode == "wrong" {
				cookie.Value = strings.Repeat("a", 43)
			}
			if tc.cookieMode != "missing" {
				callback.AddCookie(cookie)
			}
			result := httptest.NewRecorder()
			h.Callback(result, callback)
			destination, _ := url.Parse(result.Header().Get("Location"))
			fragment, _ := url.ParseQuery(destination.Fragment)
			token := fragment.Get("token")
			if (token != "") != tc.success {
				t.Fatalf("success=%v, redirect=%s", tc.success, destination.Path+"?"+destination.RawQuery)
			}
			if destination.Query().Get("token") != "" {
				t.Fatal("session leaked into query")
			}
			if result.Result().Cookies()[0].MaxAge != -1 {
				t.Fatal("OAuth cookie not consumed")
			}
			if tc.cookieMode != "" && calls != 0 {
				t.Fatal("invalid browser state reached Google")
			}
			var users, links int
			_ = database.QueryRow(`SELECT count(*) FROM users`).Scan(&users)
			_ = database.QueryRow(`SELECT count(*) FROM oauth_providers`).Scan(&links)
			if tc.success {
				claims, err := ValidateToken(token, cfg.JWTSecret)
				if err != nil || claims.Email != tc.email {
					t.Fatal("invalid app session")
				}
				if users != 1 || links != 1 {
					t.Fatalf("users=%d links=%d", users, links)
				}
				if tc.existing {
					var password, name string
					_ = database.QueryRow(`SELECT password_hash,name FROM users WHERE id=?`, existingID).Scan(&password, &name)
					if claims.UserID != existingID || password != "retained-password" || name != "Original name" {
						t.Fatal("existing account changed")
					}
				}
			} else if links != 0 || (!tc.existing && users != 0) {
				t.Fatal("failed login mutated accounts")
			}
		})
	}
}
