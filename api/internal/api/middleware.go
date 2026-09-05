package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	goapi "github.com/anchoo2kewl/go-api"
	"go.uber.org/zap"

	"github.com/biswas-dev/lifeai/api/internal/auth"
	"github.com/biswas-dev/lifeai/api/internal/dates"
)

type contextKey string

const (
	// UserIDKey carries the authenticated user's id.
	UserIDKey contextKey = "user_id"
	// UserEmailKey carries the authenticated user's email.
	UserEmailKey     contextKey = "user_email"
	SessionClaimsKey contextKey = "session_claims"
)

// UserID returns the authenticated user's id from the request context.
func UserID(ctx context.Context) int64 {
	id, _ := ctx.Value(UserIDKey).(int64)
	return id
}

// JWTAuth rejects requests without a valid bearer token and puts the user's
// identity on the request context. It accepts a session JWT or a personal
// API token, recognised by its prefix.
func (s *Server) JWTAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" {
			respondError(w, http.StatusUnauthorized, "authorization required", "unauthorized")
			return
		}
		scheme, credential, ok := strings.Cut(header, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") {
			respondError(w, http.StatusUnauthorized, "invalid authorization header", "unauthorized")
			return
		}

		if TokenScheme.Issued(credential) {
			userID, record, err := s.tokenAuthenticator().Authenticate(r.Context(), credential, r.Method)
			if err != nil {
				code := "unauthorized"
				if errors.Is(err, goapi.ErrScopeDenied) {
					code = "token_scope_denied"
				}
				respondError(w, goapi.StatusFor(err), goapi.PublicMessage(err), code)
				return
			}
			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			ctx = context.WithValue(ctx, TokenScopeKey, record.Scopes)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		claims, err := auth.ValidateToken(credential, s.cfg.JWTSecret)
		if err != nil {
			respondError(w, http.StatusUnauthorized, "invalid or expired token", "unauthorized")
			return
		}
		var deleted, needsMFA bool
		var version int64
		err = s.db.QueryRowContext(r.Context(),
			`SELECT deleted_at IS NOT NULL, security_version, totp_secret_enc <> '' FROM users WHERE id = ?`, claims.UserID).Scan(&deleted, &version, &needsMFA)
		if err != nil || deleted || version != claims.SecurityVersion || (needsMFA && !claims.MFA) {
			respondError(w, http.StatusUnauthorized, "account unavailable", "unauthorized")
			return
		}
		ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
		ctx = context.WithValue(ctx, UserEmailKey, claims.Email)
		ctx = context.WithValue(ctx, SessionClaimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// userLocation resolves the caller's timezone for date arithmetic.
func (s *Server) userLocation(ctx context.Context) *time.Location {
	var tz string
	if err := s.db.QueryRowContext(ctx,
		`SELECT timezone FROM users WHERE id = ?`, UserID(ctx)).Scan(&tz); err != nil {
		return time.UTC
	}
	return dates.LoadLocation(tz)
}

// today is the caller's current calendar date.
func (s *Server) today(ctx context.Context) string {
	return dates.Today(s.userLocation(ctx))
}

// ZapLogger logs each request with its status and duration.
func ZapLogger(log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)
			fields := []zap.Field{
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", rw.status),
				zap.Duration("took", time.Since(start)),
				zap.String("ip", ClientIP(r)),
			}
			switch {
			case rw.status >= 500:
				log.Error("request", fields...)
			case rw.status >= 400:
				log.Warn("request", fields...)
			default:
				log.Info("request", fields...)
			}
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// RateLimitMiddleware is a per-IP token bucket refilled once a minute.
// In-process and best-effort; nginx does the real rate limiting at the edge.
func RateLimitMiddleware(perMinute int) func(http.Handler) http.Handler {
	type bucket struct {
		tokens int
		reset  time.Time
	}
	var (
		mu      sync.Mutex
		buckets = map[string]*bucket{}
	)
	go func() {
		for range time.Tick(10 * time.Minute) {
			mu.Lock()
			now := time.Now()
			for ip, b := range buckets {
				if now.After(b.reset.Add(10 * time.Minute)) {
					delete(buckets, ip)
				}
			}
			mu.Unlock()
		}
	}()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ClientIP(r)
			now := time.Now()
			mu.Lock()
			b, ok := buckets[ip]
			if !ok || now.After(b.reset) {
				b = &bucket{tokens: perMinute, reset: now.Add(time.Minute)}
				buckets[ip] = b
			}
			allowed := b.tokens > 0
			if allowed {
				b.tokens--
			}
			mu.Unlock()
			if !allowed {
				w.Header().Set("Retry-After", "60")
				respondError(w, http.StatusTooManyRequests, "too many requests", "rate_limited")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP resolves the caller's address, preferring the headers Cloudflare
// and nginx set in front of us.
func ClientIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(xff)
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
