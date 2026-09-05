package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	gologin "github.com/anchoo2kewl/go-login"
	"github.com/biswas-dev/lifeai/api/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
)

type securityClient struct {
	t       *testing.T
	h       http.Handler
	token   string
	cookies map[string]*http.Cookie
	ip      string
}

func signTestClaims(t *testing.T, claims *auth.Claims, secret string) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func (c *securityClient) request(method, path string, body any, want int) (map[string]any, *httptest.ResponseRecorder) {
	c.t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = c.ip + ":1234"
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for _, cookie := range c.cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	for _, cookie := range rec.Result().Cookies() {
		if cookie.MaxAge < 0 {
			delete(c.cookies, cookie.Name)
		} else {
			c.cookies[cookie.Name] = cookie
		}
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if want != 0 && rec.Code != want {
		c.t.Fatalf("%s %s: got %d want %d: %v", method, path, rec.Code, want, out)
	}
	return out, rec
}

func securityFixture(t *testing.T) (*Server, *securityClient) {
	t.Helper()
	s, _ := newTestServer(t)
	s.cfg.OAuthSuccessURL = "http://localhost/oauth/callback"
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		s.MountSecurityRoutes(r)
		r.Post("/auth/signup", s.HandleSignup)
		r.Post("/auth/login", s.HandleLogin)
		r.Post("/auth/reset-password", s.HandleResetPassword)
		r.Get("/auth/verified-google", func(w http.ResponseWriter, r *http.Request) { s.CompleteGoogleLogin(w, r, 1) })
		r.Group(func(r chi.Router) { r.Use(s.JWTAuth); r.Get("/me", s.HandleMe); r.Post("/tokens", s.HandleCreateToken) })
	})
	c := &securityClient{t: t, h: r, cookies: map[string]*http.Cookie{}, ip: "192.0.2.10"}
	result, _ := c.request("POST", "/api/auth/signup", map[string]string{"email": "owner@example.com", "password": "password123", "name": "Owner"}, 200)
	c.token = result["token"].(string)
	return s, c
}

func (c *securityClient) login() string {
	c.t.Helper()
	result, _ := c.request("POST", "/api/auth/login", map[string]string{"email": "owner@example.com", "password": "password123"}, 200)
	if result["token"] != nil || result["mfa_required"] != true {
		c.t.Fatal("password bypassed second factor")
	}
	return result["challenge"].(string)
}

func enableTestTOTP(t *testing.T, c *securityClient) (string, []string) {
	t.Helper()
	begin, rec := c.request("POST", "/api/security/totp/begin", nil, 200)
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("setup secret can be cached")
	}
	secret := begin["secret"].(string)
	code, _ := gologin.TOTPCode(secret, time.Now())
	result, _ := c.request("POST", "/api/security/totp/confirm", map[string]any{"challenge": begin["challenge"], "code": code}, 200)
	c.token = result["token"].(string)
	raw := result["recovery_codes"].([]any)
	codes := []string{}
	for _, item := range raw {
		codes = append(codes, item.(string))
	}
	return secret, codes
}

func TestTOTPEnrollmentAndLoginCannotBypassSecondFactor(t *testing.T) {
	s, c := securityFixture(t)
	old := c.token
	secret, codes := enableTestTOTP(t, c)
	var encrypted string
	s.db.QueryRow(`SELECT totp_secret_enc FROM users WHERE id=1`).Scan(&encrypted)
	if encrypted == secret || strings.Contains(encrypted, secret) {
		t.Fatal("plaintext TOTP secret stored")
	}
	var hashes string
	s.db.QueryRow(`SELECT group_concat(code_hash) FROM recovery_codes`).Scan(&hashes)
	for _, code := range codes {
		if strings.Contains(hashes, code) {
			t.Fatal("plaintext recovery code stored")
		}
	}
	fresh := c.token
	c.token = old
	c.request("GET", "/api/me", nil, 401)
	c.token = fresh
	challenge := c.login()
	outsider := &securityClient{t: t, h: c.h, cookies: map[string]*http.Cookie{}, ip: "192.0.2.11"}
	outsider.request("POST", "/api/auth/mfa/verify", map[string]string{"challenge": challenge, "code": codes[0]}, 401)
	c.request("POST", "/api/auth/mfa/verify", map[string]string{"challenge": challenge, "code": "000000"}, 401)
	// The enrollment code itself was consumed, so it cannot immediately sign in.
	code, _ := gologin.TOTPCode(secret, time.Now())
	c.request("POST", "/api/auth/mfa/verify", map[string]string{"challenge": challenge, "code": code}, 401)
	result, _ := c.request("POST", "/api/auth/mfa/verify", map[string]string{"challenge": challenge, "code": strings.ToLower(strings.ReplaceAll(codes[0], "-", ""))}, 200)
	claims, err := auth.ValidateToken(result["token"].(string), s.cfg.JWTSecret)
	if err != nil || !claims.MFA || claims.UserID != 1 {
		t.Fatal("MFA session was not issued for the existing user")
	}
	c.request("POST", "/api/auth/mfa/verify", map[string]string{"challenge": challenge, "code": codes[1]}, 401)
	challenge = c.login()
	c.request("POST", "/api/auth/mfa/verify", map[string]string{"challenge": challenge, "code": codes[0]}, 401)
	// A fresh, valid authenticator code succeeds and is persisted as consumed.
	_, _ = s.db.Exec(`UPDATE users SET totp_last_step=-1 WHERE id=1`)
	code, _ = gologin.TOTPCode(secret, time.Now())
	c.request("POST", "/api/auth/mfa/verify", map[string]string{"challenge": challenge, "code": code}, 200)
	_, rec := c.request("GET", "/api/auth/verified-google", nil, 302)
	location, _ := url.Parse(rec.Header().Get("Location"))
	fragment, _ := url.ParseQuery(location.Fragment)
	if fragment.Get("challenge") == "" || fragment.Get("token") != "" {
		t.Fatal("Google login bypassed 2FA")
	}
	status, _ := c.request("GET", "/api/security/", nil, 200)
	if status["totp_enabled"] != true || status["recovery_codes_remaining"] != float64(9) {
		t.Fatalf("incorrect status: %v", status)
	}
}

func TestMFAAttemptLimitExpiryAndAccountLock(t *testing.T) {
	s, c := securityFixture(t)
	_, codes := enableTestTOTP(t, c)
	challenge := c.login()
	for i := 0; i < 5; i++ {
		c.request("POST", "/api/auth/mfa/verify", map[string]string{"challenge": challenge, "code": "wrong"}, 401)
	}
	out, _ := c.request("POST", "/api/auth/mfa/verify", map[string]string{"challenge": challenge, "code": codes[0]}, 401)
	if out["code"] != "challenge_expired" {
		t.Fatal("exhausted challenge accepted")
	}
	challenge = c.login()
	_, _ = s.db.Exec(`UPDATE auth_challenges SET expires_at=? WHERE token_hash=?`, time.Now().Add(-time.Minute).Unix(), securityHash(challenge))
	c.request("POST", "/api/auth/mfa/verify", map[string]string{"challenge": challenge, "code": codes[0]}, 401)
	_, _ = s.db.Exec(`UPDATE users SET mfa_locked_until=? WHERE id=1`, time.Now().Add(time.Minute).Unix())
	challenge = c.login()
	c.request("POST", "/api/auth/mfa/verify", map[string]string{"challenge": challenge, "code": codes[0]}, 401)
	var remaining int
	s.db.QueryRow(`SELECT count(*) FROM recovery_codes WHERE user_id=1`).Scan(&remaining)
	if remaining != 10 {
		t.Fatal("locked verification consumed a valid recovery code")
	}
}

func TestRecoveryCodeCanOnlyCompleteOneConcurrentLogin(t *testing.T) {
	s, first := securityFixture(t)
	_, codes := enableTestTOTP(t, first)
	second := &securityClient{t: t, h: first.h, cookies: map[string]*http.Cookie{}, ip: "192.0.2.12"}
	firstChallenge, secondChallenge := first.login(), second.login()
	results := make(chan int, 2)
	go func() {
		_, rec := first.request("POST", "/api/auth/mfa/verify", map[string]string{"challenge": firstChallenge, "code": codes[0]}, 0)
		results <- rec.Code
	}()
	go func() {
		_, rec := second.request("POST", "/api/auth/mfa/verify", map[string]string{"challenge": secondChallenge, "code": codes[0]}, 0)
		results <- rec.Code
	}()
	a, b := <-results, <-results
	if !((a == 200 && b == 401) || (a == 401 && b == 200)) {
		t.Fatalf("concurrent recovery results %d, %d", a, b)
	}
	var remaining int
	s.db.QueryRow(`SELECT count(*) FROM recovery_codes WHERE user_id=1`).Scan(&remaining)
	if remaining != 9 {
		t.Fatal("recovery code consumption was not atomic")
	}
}

func TestSecurityRequiresInteractiveRecentAuthentication(t *testing.T) {
	s, c := securityFixture(t)
	token, _ := c.request("POST", "/api/tokens", map[string]any{"name": "API agent", "scopes": []string{"read", "write"}}, 201)
	jwtToken := c.token
	c.token = token["secret"].(string)
	c.request("GET", "/api/security/", nil, 403)
	c.request("POST", "/api/security/totp/begin", nil, 403)
	c.request("POST", "/api/security/passkeys/begin", nil, 403)
	claims, _ := auth.ValidateToken(jwtToken, s.cfg.JWTSecret)
	claims.AuthTime = time.Now().Add(-10 * time.Minute).Unix()
	c.token = signTestClaims(t, claims, s.cfg.JWTSecret)
	out, _ := c.request("POST", "/api/security/totp/begin", nil, 403)
	if out["code"] != "reauth_required" {
		t.Fatal("missing recent authentication check")
	}
	out, _ = c.request("POST", "/api/security/reauth", map[string]string{"password": "password123"}, 200)
	c.token = out["token"].(string)
	begin, _ := c.request("POST", "/api/security/totp/begin", nil, 200)
	secret := begin["secret"].(string)
	code, _ := gologin.TOTPCode(secret, time.Now())
	_, _ = s.db.Exec(`UPDATE users SET security_version=security_version+1 WHERE id=1`)
	c.request("POST", "/api/security/totp/confirm", map[string]any{"challenge": begin["challenge"], "code": code}, 401)
}

func TestRecoveryRotationDisableAndPasswordReset(t *testing.T) {
	s, c := securityFixture(t)
	_, oldCodes := enableTestTOTP(t, c)
	oldToken := c.token
	rotated, _ := c.request("POST", "/api/security/recovery-codes", nil, 200)
	c.token = rotated["token"].(string)
	challenge := c.login()
	c.request("POST", "/api/auth/mfa/verify", map[string]string{"challenge": challenge, "code": oldCodes[0]}, 401)
	newCode := rotated["recovery_codes"].([]any)[0].(string)
	c.request("POST", "/api/auth/mfa/verify", map[string]string{"challenge": challenge, "code": newCode}, 200)
	// Password recovery must still pass through the second-factor gate.
	_, _ = s.db.Exec(`INSERT INTO password_resets(user_id,token_hash,expires_at) VALUES(1,?,?)`, securityHash("reset-token"), time.Now().UTC().Add(time.Hour))
	reset, _ := c.request("POST", "/api/auth/reset-password", map[string]string{"token": "reset-token", "new_password": "new-password123"}, 200)
	if reset["mfa_required"] != true || reset["token"] != nil {
		t.Fatal("password reset bypassed 2FA")
	}
	current := c.token
	c.token = oldToken
	c.request("GET", "/api/me", nil, 401)
	c.token = current
	disabled, _ := c.request("DELETE", "/api/security/totp", nil, 200)
	c.token = disabled["token"].(string)
	status, _ := c.request("GET", "/api/security/", nil, 200)
	if status["totp_enabled"] != false || status["recovery_codes_remaining"] != float64(0) {
		t.Fatal("disable did not remove factor and recovery codes")
	}
	out, _ := c.request("POST", "/api/auth/login", map[string]string{"email": "owner@example.com", "password": "new-password123"}, 200)
	if out["token"] == nil {
		t.Fatal("password login did not resume after disabling 2FA")
	}
}
