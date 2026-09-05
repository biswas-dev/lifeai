package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	gologin "github.com/anchoo2kewl/go-login"
	"github.com/biswas-dev/lifeai/api/internal/config"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
)

// GoogleHandler uses go-login's user store and session format with browser-bound
// state, PKCE, verified email linking, and Lifeai's open-registration policy.
type GoogleHandler struct {
	cfg                                  *config.Config
	store                                gologin.UserStore
	logger                               *zap.Logger
	client                               *http.Client
	permissionURL, tokenURL, userInfoURL string
}

func NewGoogleHandler(cfg *config.Config, store gologin.UserStore, logger *zap.Logger) *GoogleHandler {
	return &GoogleHandler{cfg: cfg, store: store, logger: logger,
		client:        &http.Client{Timeout: 10 * time.Second},
		permissionURL: "https://accounts.google.com/o/oauth2/v2/auth",
		tokenURL:      "https://oauth2.googleapis.com/token",
		userInfoURL:   "https://www.googleapis.com/oauth2/v3/userinfo"}
}

func (h *GoogleHandler) cookie(value string, maxAge int) *http.Cookie {
	secure := strings.HasPrefix(h.cfg.AppURL, "https://")
	name := "lifeai_google_oauth"
	if secure {
		name = "__Host-" + name
	}
	return &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true,
		Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: maxAge}
}

func googleChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func (h *GoogleHandler) Initiate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	verifier := base64.RawURLEncoding.EncodeToString(randomGoogleBytes())
	claims := jwt.RegisteredClaims{Subject: googleChallenge(verifier), Issuer: "lifeai-google",
		Audience:  jwt.ClaimStrings{h.cfg.GoogleClientID},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(10 * time.Minute))}
	state, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(h.cfg.OAuthStateSecret))
	if err != nil {
		h.fail(w, r, "Could not start Google sign-in. Please try again.")
		return
	}
	http.SetCookie(w, h.cookie(verifier, 600))
	params := url.Values{"client_id": {h.cfg.GoogleClientID},
		"redirect_uri":  {h.cfg.AppURL + "/api/auth/google/callback"},
		"response_type": {"code"}, "scope": {"openid email profile"},
		"state": {state}, "code_challenge": {claims.Subject},
		"code_challenge_method": {"S256"}, "prompt": {"select_account"}}
	http.Redirect(w, r, h.permissionURL+"?"+params.Encode(), http.StatusFound)
}

func randomGoogleBytes() []byte {
	b := make([]byte, 32)
	// crypto/rand.Read only returns successfully when the buffer is filled.
	_, _ = rand.Read(b)
	return b
}

func (h *GoogleHandler) Callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	cookie, cookieErr := r.Cookie(h.cookie("", 0).Name)
	http.SetCookie(w, h.cookie("", -1))
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(r.URL.Query().Get("state"), claims,
		func(*jwt.Token) (any, error) { return []byte(h.cfg.OAuthStateSecret), nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired(),
		jwt.WithIssuer("lifeai-google"), jwt.WithAudience(h.cfg.GoogleClientID))
	if err != nil || cookieErr != nil || len(cookie.Value) != 43 ||
		subtle.ConstantTimeCompare([]byte(claims.Subject), []byte(googleChallenge(cookie.Value))) != 1 {
		h.fail(w, r, "Your sign-in session expired. Please try Google sign-in again.")
		return
	}
	if r.URL.Query().Get("error") != "" {
		h.fail(w, r, "Google sign-in was cancelled. Please try again.")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		h.fail(w, r, "Google did not return a sign-in code.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	profile, err := h.profile(ctx, code, cookie.Value)
	if err != nil {
		h.logger.Warn("google sign-in failed", zap.Error(err))
		h.fail(w, r, "Could not verify your Google account. Please try again.")
		return
	}
	user, err := h.resolveUser(ctx, profile)
	if err != nil {
		if errors.Is(err, errGoogleLink) {
			h.fail(w, r, "An account already uses this email. Please sign in with your password.")
			return
		}
		if errors.Is(err, errGoogleSignup) {
			h.fail(w, r, "New account registration is currently closed.")
			return
		}
		h.logger.Error("google account lookup failed", zap.Error(err))
		h.fail(w, r, "Could not sign in to Lifeai. Please try again.")
		return
	}
	token, err := gologin.GenerateToken(user.ID, user.Email, h.cfg.JWTSecret, h.cfg.JWTExpiry)
	if err != nil {
		h.fail(w, r, "Could not create your session. Please try again.")
		return
	}
	// Fragments are never sent to the server or recorded in its access logs.
	http.Redirect(w, r, h.cfg.OAuthSuccessURL+"#token="+url.QueryEscape(token), http.StatusFound)
}

type googleProfile struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	HostedDomain  string `json:"hd"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

func (h *GoogleHandler) profile(ctx context.Context, code, verifier string) (googleProfile, error) {
	form := url.Values{"code": {code}, "code_verifier": {verifier},
		"client_id": {h.cfg.GoogleClientID}, "client_secret": {h.cfg.GoogleClientSecret},
		"redirect_uri": {h.cfg.AppURL + "/api/auth/google/callback"}, "grant_type": {"authorization_code"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return googleProfile{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := h.getJSON(req, &token); err != nil {
		return googleProfile{}, err
	}
	if token.AccessToken == "" {
		return googleProfile{}, errors.New("google returned no access token")
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, h.userInfoURL, nil)
	if err != nil {
		return googleProfile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	var profile googleProfile
	if err := h.getJSON(req, &profile); err != nil {
		return profile, err
	}
	profile.Email = strings.ToLower(strings.TrimSpace(profile.Email))
	if profile.Sub == "" || !profile.EmailVerified || !strings.Contains(profile.Email, "@") {
		return profile, errors.New("google returned an unverified or incomplete identity")
	}
	return profile, nil
}

func (h *GoogleHandler) getJSON(req *http.Request, out any) error {
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("google endpoint returned status %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

var errGoogleLink = errors.New("Google is not authoritative for this existing email")
var errGoogleSignup = errors.New("registration is closed")

func (h *GoogleHandler) resolveUser(ctx context.Context, p googleProfile) (*gologin.User, error) {
	u, err := h.store.FindUserByProviderID(ctx, "google", p.Sub)
	if err != nil || u != nil {
		return u, err
	}
	u, err = h.store.FindUserByEmail(ctx, p.Email)
	if err != nil {
		return nil, err
	}
	if u != nil {
		// Google is authoritative for Gmail and verified Workspace identities.
		// Other existing addresses require a separate ownership challenge.
		if !strings.HasSuffix(p.Email, "@gmail.com") && p.HostedDomain == "" {
			return nil, errGoogleLink
		}
		return h.store.LinkOAuthProvider(ctx, u.ID, "google", p.Sub)
	}
	if !h.cfg.AllowSignup {
		return nil, errGoogleSignup
	}
	return h.store.CreateOAuthUser(ctx, gologin.ProviderUserInfo{ProviderUserID: p.Sub,
		Email: p.Email, Name: p.Name, AvatarURL: p.Picture}, "google", "")
}

func (h *GoogleHandler) fail(w http.ResponseWriter, r *http.Request, message string) {
	http.Redirect(w, r, h.cfg.OAuthErrorURL+"?error="+url.QueryEscape(message), http.StatusFound)
}
