// Package config loads runtime configuration from the environment.
//
// A plain struct plus a handful of getEnv helpers — the same shape 75hard and
// taskai use. No config library, no file formats.
package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds every tunable the server reads at startup.
type Config struct {
	Env      string
	Port     string
	AppURL   string
	LogLevel string

	DBPath string

	JWTSecret string
	JWTExpiry time.Duration

	PhotosDir       string
	MaxUploadBytes  int64
	MaxPhotoEdge    int
	ThumbEdge       int
	RateLimitPerMin int

	FrontendDist string

	CORSAllowedOrigins []string

	// OAuth (go-login). Empty client IDs disable the provider.
	GoogleClientID     string
	GoogleClientSecret string
	GitHubClientID     string
	GitHubClientSecret string
	OAuthStateSecret   string
	OAuthSuccessURL    string
	OAuthErrorURL      string

	// Strava. Empty credentials disable the integration; the Connect button
	// then says the server has no Strava app configured.
	StravaClientID     string
	StravaClientSecret string
	StravaSyncInterval time.Duration

	// EncryptionKey protects third-party credentials at rest (the 75hard
	// token, for one). Without it those integrations cannot be configured,
	// which is a feature being unavailable rather than a startup failure.
	EncryptionKey string

	// Seeded on first boot so a fresh deploy is usable immediately.
	AdminEmail    string
	AdminPassword string

	AllowSignup          bool
	ResetTokenInResponse bool

	// AIDailyLimit caps paid model calls per user per rolling 24 hours.
	AIDailyLimit int

	// Hard75AllowedEmails lists the accounts that may connect a 75hard
	// account. The bridge is personal for now; opening it up is a matter of
	// adding an address here (or "*" for everyone).
	Hard75AllowedEmails []string
	// Hard75SyncInterval is how often connected accounts are pulled.
	Hard75SyncInterval time.Duration
}

// Load reads configuration from the environment, applying defaults suited to
// local development, and fatals on configuration that is unsafe in production.
func Load() *Config {
	c := &Config{
		Env:      getEnv("ENV", "development"),
		Port:     getEnv("PORT", "8088"),
		AppURL:   getEnv("APP_URL", "http://localhost:8088"),
		LogLevel: getEnv("LOG_LEVEL", "info"),

		DBPath: getEnv("DB_PATH", "./data/lifeai.db"),

		JWTSecret: getEnv("JWT_SECRET", "dev-secret-change-me"),
		JWTExpiry: time.Duration(getEnvInt("JWT_EXPIRY_HOURS", 24*30)) * time.Hour,

		PhotosDir:       getEnv("PHOTOS_DIR", "./data/photos"),
		MaxUploadBytes:  int64(getEnvInt("MAX_UPLOAD_MB", 15)) << 20,
		MaxPhotoEdge:    getEnvInt("MAX_PHOTO_EDGE", 1600),
		ThumbEdge:       getEnvInt("THUMB_EDGE", 320),
		RateLimitPerMin: getEnvInt("RATE_LIMIT_PER_MIN", 300),

		FrontendDist: getEnv("FRONTEND_DIST", ""),

		CORSAllowedOrigins: getEnvList("CORS_ALLOWED_ORIGINS", "http://localhost:5176,http://localhost:8088"),

		GoogleClientID:     getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: getEnv("GOOGLE_CLIENT_SECRET", ""),
		GitHubClientID:     getEnv("LOGIN_GITHUB_CLIENT_ID", ""),
		GitHubClientSecret: getEnv("LOGIN_GITHUB_CLIENT_SECRET", ""),
		OAuthStateSecret:   getEnv("OAUTH_STATE_SECRET", ""),
		OAuthSuccessURL:    getEnv("OAUTH_SUCCESS_URL", "http://localhost:5176/oauth/callback"),
		OAuthErrorURL:      getEnv("OAUTH_ERROR_URL", "http://localhost:5176/login"),

		StravaClientID:     getEnv("STRAVA_CLIENT_ID", ""),
		StravaClientSecret: getEnv("STRAVA_CLIENT_SECRET", ""),
		StravaSyncInterval: time.Duration(getEnvInt("STRAVA_SYNC_MINUTES", 30)) * time.Minute,

		EncryptionKey: getEnv("ENCRYPTION_KEY", ""),

		AdminEmail:    getEnv("ADMIN_EMAIL", ""),
		AdminPassword: getEnv("ADMIN_PASSWORD", ""),

		AllowSignup:          getEnvBool("ALLOW_SIGNUP", true),
		ResetTokenInResponse: getEnvBool("RESET_TOKEN_IN_RESPONSE", false),

		AIDailyLimit: getEnvInt("AI_DAILY_LIMIT", 60),

		Hard75AllowedEmails: getEnvList("HARD75_ALLOWED_EMAILS", "anchoo2kewl@gmail.com"),
		Hard75SyncInterval:  time.Duration(getEnvInt("HARD75_SYNC_HOURS", 24)) * time.Hour,
	}

	if c.IsProduction() {
		if c.JWTSecret == "dev-secret-change-me" {
			log.Fatal("config: JWT_SECRET must be set in production")
		}
		if c.OAuthStateSecret != "" && c.OAuthStateSecret == c.JWTSecret {
			log.Fatal("config: OAUTH_STATE_SECRET must differ from JWT_SECRET")
		}
	}
	if c.Hard75SyncInterval < time.Hour {
		c.Hard75SyncInterval = time.Hour
	}
	return c
}

// IsProduction reports whether the server is running in production.
func (c *Config) IsProduction() bool { return c.Env == "production" }

// OAuthEnabled reports whether at least one OAuth provider is configured.
func (c *Config) OAuthEnabled() bool {
	return c.OAuthStateSecret != "" && (c.GoogleClientID != "" || c.GitHubClientID != "")
}

// Hard75Allowed reports whether the email may connect a 75hard account.
func (c *Config) Hard75Allowed(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, e := range c.Hard75AllowedEmails {
		if e == "*" || strings.ToLower(e) == email {
			return true
		}
	}
	return false
}

func getEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func getEnvList(key, fallback string) []string {
	raw := getEnv(key, fallback)
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
