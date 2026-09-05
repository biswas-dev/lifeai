// Command api is the lifeai server: REST API, photo storage and the built SPA
// in a single binary.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	ai "github.com/anchoo2kewl/go-ai"
	gologin "github.com/anchoo2kewl/go-login"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"

	"github.com/biswas-dev/lifeai/api/internal/aifeatures"
	"github.com/biswas-dev/lifeai/api/internal/api"
	"github.com/biswas-dev/lifeai/api/internal/api/spec"
	"github.com/biswas-dev/lifeai/api/internal/auth"
	"github.com/biswas-dev/lifeai/api/internal/config"
	"github.com/biswas-dev/lifeai/api/internal/db"
	"github.com/biswas-dev/lifeai/api/internal/photo"
	"github.com/biswas-dev/lifeai/api/internal/secret"
	"github.com/biswas-dev/lifeai/api/internal/version"
)

// aiRequestTimeout bounds a model-backed request.
const aiRequestTimeout = 4 * time.Minute

const healthImportTimeout = 15 * time.Minute

// Choose the deadline once: a nested timeout cannot extend its parent's
// shorter deadline, even when the inner route allows a long-running import.
func authenticatedTimeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		duration := time.Minute
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/import/"):
			duration = healthImportTimeout
		case strings.HasPrefix(r.URL.Path, "/api/ai/"), r.URL.Path == "/api/blood/reports/upload":
			duration = aiRequestTimeout
		}
		middleware.Timeout(duration)(next).ServeHTTP(w, r)
	})
}

func main() {
	cfg := config.Load()
	logger := config.MustInitLogger(cfg.Env, cfg.LogLevel)
	defer logger.Sync() //nolint:errcheck

	logger.Info("starting lifeai",
		zap.String("version", version.Version),
		zap.String("commit", version.GitCommit),
		zap.String("env", cfg.Env),
		zap.String("port", cfg.Port))

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		logger.Fatal("failed to open database", zap.Error(err))
	}
	defer database.Close()

	applied, err := database.Migrate()
	if err != nil {
		logger.Fatal("failed to run migrations", zap.Error(err))
	}
	if len(applied) > 0 {
		logger.Info("applied migrations", zap.Strings("versions", applied))
	}

	if err := seedAdmin(database, cfg, logger); err != nil {
		logger.Error("failed to seed admin user", zap.Error(err))
	}

	photos, err := photo.NewStore(cfg.PhotosDir, cfg.MaxPhotoEdge, cfg.ThumbEdge)
	if err != nil {
		logger.Fatal("failed to open photo store", zap.Error(err))
	}

	cipher, err := secret.New(cfg.EncryptionKey)
	if err != nil {
		logger.Fatal("bad ENCRYPTION_KEY", zap.Error(err))
	}
	if !cipher.Enabled() {
		logger.Info("no ENCRYPTION_KEY: integrations that store a credential are unavailable")
	}

	// AI_* is the text chain; AIV_* the vision chain. Both optional.
	aiSvc := aifeatures.New(nil)
	textChain, err := boundedChain("AI")
	if err != nil {
		if errors.Is(err, ai.ErrNoProviders) {
			logger.Info("ai features disabled: no AI_1_PROVIDER configured")
		} else {
			logger.Warn("ai chain not configured", zap.Error(err))
		}
	} else {
		logChain(logger, "text", textChain)
		visionChain, verr := boundedChain("AIV")
		if verr != nil {
			visionChain = nil
			logger.Info("no dedicated vision chain; photo analysis will use the text chain")
		} else {
			logChain(logger, "vision", visionChain)
		}
		aiSvc = aifeatures.NewWithVision(textChain, visionChain)
		logger.Info("ai features enabled",
			zap.Strings("text", aiSvc.Providers()), zap.Strings("vision", aiSvc.VisionProviders()))
	}

	server := api.NewServer(database, cfg, logger, photos, aiSvc, cipher)

	var estimator *api.FoodEstimator
	if aiSvc.Enabled() {
		estimator = api.NewFoodEstimator(server, 2, 64)
		server.SetFoodEstimator(estimator)
	}

	var syncer *api.Hard75Syncer
	var stravaPoller *api.StravaSyncer
	if cipher.Enabled() {
		syncer = api.NewHard75Syncer(server, cfg.Hard75SyncInterval)
		server.SetHard75Syncer(syncer)
		if cfg.StravaClientID != "" && cfg.StravaClientSecret != "" {
			stravaPoller = api.NewStravaSyncer(server, cfg.StravaSyncInterval)
		}
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(api.ZapLogger(logger))
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSAllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := database.PingContext(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"degraded","database":"down"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	var oauthHandler *gologin.Handler
	var googleHandler *auth.GoogleHandler
	if cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" && cfg.OAuthEnabled() {
		googleHandler = auth.NewGoogleHandler(cfg, db.NewOAuthStore(database), logger)
	}
	if cfg.GitHubClientID != "" && cfg.OAuthEnabled() {
		oauthCfg := &gologin.Config{
			SuccessURL:  cfg.OAuthSuccessURL,
			ErrorURL:    cfg.OAuthErrorURL,
			StateSecret: cfg.OAuthStateSecret,
			JWTSecret:   cfg.JWTSecret,
			JWTExpiry:   cfg.JWTExpiry,
			Logger:      logger,
		}
		if cfg.GitHubClientID != "" {
			oauthCfg.GitHub = &gologin.OAuthProviderConfig{
				ClientID:     cfg.GitHubClientID,
				ClientSecret: cfg.GitHubClientSecret,
				RedirectURL:  cfg.AppURL + "/api/auth/github/callback",
			}
		}
		oauthHandler, err = gologin.NewHandler(oauthCfg, db.NewOAuthStore(database))
		if err != nil {
			logger.Fatal("failed to init OAuth handler", zap.Error(err))
		}
		logger.Info("oauth enabled", zap.Bool("google", cfg.GoogleClientID != ""), zap.Bool("github", cfg.GitHubClientID != ""))
	}

	r.Route("/api", func(r chi.Router) {
		r.Method(http.MethodGet, "/openapi.yaml", spec.Document.Handler())
		r.Method(http.MethodHead, "/openapi.yaml", spec.Document.Handler())
		r.Get("/version", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(version.Get())
		})

		r.Route("/auth", func(r chi.Router) {
			r.Use(api.RateLimitMiddleware(20))
			r.Post("/signup", server.HandleSignup)
			r.Post("/login", server.HandleLogin)
			r.Post("/forgot-password", server.HandleForgotPassword)
			r.Post("/reset-password", server.HandleResetPassword)
			r.Get("/config", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]bool{
					"allow_signup": cfg.AllowSignup,
					"google":       googleHandler != nil,
					"github":       cfg.GitHubClientID != "" && cfg.OAuthEnabled(),
				})
			})
			if googleHandler != nil {
				r.Get("/google", googleHandler.Initiate)
				r.Get("/google/callback", googleHandler.Callback)
			}
			if oauthHandler != nil {
				r.Get("/github", oauthHandler.HandleGithubInitiate)
				r.Get("/github/callback", oauthHandler.HandleGithubCallback)
			}
		})

		// Strava sends the browser back with no bearer token; the callback
		// identifies the user from the signed state it issued.
		r.Group(func(r chi.Router) {
			r.Use(api.RateLimitMiddleware(20))
			r.Get("/strava/callback", server.HandleStravaCallback)
		})

		r.Group(func(r chi.Router) {
			r.Use(server.JWTAuth)
			r.Use(api.RateLimitMiddleware(cfg.RateLimitPerMin))
			r.Use(authenticatedTimeout)

			r.Get("/me", server.HandleMe)
			r.Patch("/me", server.HandleUpdateProfile)
			r.Post("/me/password", server.HandleChangePassword)

			r.Get("/goals", server.HandleGetGoals)
			r.Put("/goals", server.HandleSaveGoals)

			r.Get("/today", server.HandleGetToday)
			r.Get("/dashboard", server.HandleGetDashboard)
			r.Get("/stats", server.HandleGetStats)
			r.Get("/days", server.HandleListDays)
			r.Get("/days/{date}", server.HandleGetDay)
			r.Patch("/days/{date}", server.HandleUpdateDay)

			r.Get("/photos", server.HandleListPhotos)
			r.Post("/photos", server.HandleUploadPhoto)
			r.Get("/photos/{photoID}/file", server.HandleServePhoto)
			r.Patch("/photos/{photoID}", server.HandleUpdatePhoto)
			r.Delete("/photos/{photoID}", server.HandleDeletePhoto)

			r.Post("/meals", server.HandleCreateMeal)
			r.Patch("/meals/{mealID}", server.HandleUpdateMeal)
			r.Delete("/meals/{mealID}", server.HandleDeleteMeal)
			r.Post("/meals/{mealID}/estimate", server.HandleRetryEstimate)

			r.Get("/recipes", server.HandleListRecipes)
			r.Post("/recipes", server.HandleCreateRecipe)
			r.Get("/recipes/{recipeID}", server.HandleGetRecipe)
			r.Patch("/recipes/{recipeID}", server.HandleUpdateRecipe)
			r.Delete("/recipes/{recipeID}", server.HandleDeleteRecipe)
			r.Post("/recipes/{recipeID}/cook", server.HandleCookRecipe)

			r.Post("/workouts", server.HandleCreateWorkout)
			r.Delete("/workouts/{workoutID}", server.HandleDeleteWorkout)
			r.Post("/meditations", server.HandleCreateMeditation)
			r.Delete("/meditations/{meditationID}", server.HandleDeleteMeditation)

			r.Get("/journal", server.HandleListJournal)
			r.Post("/journal", server.HandleCreateJournal)
			r.Patch("/journal/{entryID}", server.HandleUpdateJournal)
			r.Delete("/journal/{entryID}", server.HandleDeleteJournal)

			r.Get("/tokens", server.HandleListTokens)
			r.Post("/tokens", server.HandleCreateToken)
			r.Delete("/tokens/{tokenID}", server.HandleRevokeToken)

			r.Get("/integrations/75hard", server.HandleHard75Status)
			r.Put("/integrations/75hard", server.HandleHard75Connect)
			r.Delete("/integrations/75hard", server.HandleHard75Disconnect)
			r.Post("/integrations/75hard/sync", server.HandleHard75Sync)

			r.Get("/blood/reports", server.HandleListBloodReports)
			r.Post("/blood/reports", server.HandleCreateBloodReport)
			r.Post("/blood/reports/upload", server.HandleUploadBloodReport)
			r.Get("/blood/reports/{reportID}", server.HandleGetBloodReport)
			r.Get("/blood/reports/{reportID}/file", server.HandleServeBloodFile)
			r.Patch("/blood/reports/{reportID}", server.HandleUpdateBloodReport)
			r.Delete("/blood/reports/{reportID}", server.HandleDeleteBloodReport)
			r.Get("/blood/markers", server.HandleMarkerSeries)

			r.Get("/analysis/health", server.HandleHealthSummary)

			// Health imports. The export files are large, so these get a
			// long timeout of their own.
			r.Group(func(r chi.Router) {
				r.Post("/import/apple-health", server.HandleImportApple)
				r.Post("/import/samsung-health", server.HandleImportSamsung)
				r.Post("/import/health", server.HandleImportWebhook)
			})
			r.Get("/import/runs", server.HandleListImports)

			r.Get("/strava/status", server.HandleStravaStatus)
			r.Post("/strava/connect", server.HandleStravaConnect)
			r.Post("/strava/sync", server.HandleStravaSync)
			r.Delete("/strava", server.HandleStravaDisconnect)

			r.Get("/ai/status", server.HandleAIStatus)
			// Paid upstream calls: rate limited harder, and given the time a
			// large model actually takes.
			r.Group(func(r chi.Router) {
				r.Use(api.RateLimitMiddleware(20))
				r.Post("/ai/food", server.HandleAnalyzeFood)
				r.Post("/ai/recipes", server.HandleSuggestRecipes)
				r.Post("/ai/import-recipe", server.HandleImportRecipe)
				r.Post("/ai/plan", server.HandleBuildPlan)
				r.Get("/ai/coach", server.HandleCoachNote)
			})
		})
	})

	// MCP: an agent's window onto the record, authenticated with an API
	// token. Lives outside /api because it is not REST.
	r.With(api.RateLimitMiddleware(cfg.RateLimitPerMin), middleware.Timeout(60*time.Second)).HandleFunc("/mcp", server.HandleMCP)

	r.NotFound(api.SPAHandler(cfg.FrontendDist).ServeHTTP)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      healthImportTimeout + 30*time.Second,
		IdleTimeout:       120 * time.Second,
	}

	bgCtx, stopBg := context.WithCancel(context.Background())
	defer stopBg()
	if estimator != nil {
		estimator.Start(bgCtx)
	}
	if syncer != nil {
		syncer.Start(bgCtx)
		logger.Info("75hard bridge enabled", zap.Duration("every", cfg.Hard75SyncInterval),
			zap.Strings("allowed", cfg.Hard75AllowedEmails))
	}
	if stravaPoller != nil {
		stravaPoller.Start(bgCtx)
		logger.Info("strava polling enabled", zap.Duration("every", cfg.StravaSyncInterval))
	}

	go func() {
		logger.Info("listening", zap.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("server error", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	logger.Info("shutting down")
	if estimator != nil {
		estimator.Stop()
	}
	if syncer != nil {
		syncer.Stop()
	}
	if stravaPoller != nil {
		stravaPoller.Stop()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", zap.Error(err))
	}
	logger.Info("stopped")
}

// boundedChain builds a provider chain with a per-slot timeout that leaves
// room for the fallback.
func boundedChain(prefix string) (*ai.Chain, error) {
	slots := ai.SlotsFromEnv(prefix)
	for i := range slots {
		if slots[i].Timeout == 0 {
			slots[i].Timeout = api.AISlotTimeout
		}
	}
	chain, err := ai.NewChainFromSlots(slots...)
	if err != nil {
		return nil, err
	}
	policy := ai.RetryFromEnv(prefix)
	if os.Getenv(prefix+"_MAX_ATTEMPTS") == "" {
		policy.MaxAttempts = api.AIMaxAttempts
	}
	return chain.WithRetry(policy), nil
}

func logChain(logger *zap.Logger, name string, chain *ai.Chain) {
	chain.OnFallback(func(provider string, err error) {
		logger.Warn("ai provider failed, falling through", zap.String("chain", name), zap.String("provider", provider), zap.Error(err))
	})
	chain.OnRetry(func(provider string, attempt int, delay time.Duration, err error) {
		logger.Warn("ai provider retrying", zap.String("chain", name), zap.String("provider", provider),
			zap.Int("attempt", attempt), zap.Duration("in", delay), zap.Error(err))
	})
}

// seedAdmin creates the configured admin account on first boot. It never
// touches an existing account.
func seedAdmin(database *db.DB, cfg *config.Config, logger *zap.Logger) error {
	if cfg.AdminEmail == "" || cfg.AdminPassword == "" {
		return nil
	}
	email := strings.ToLower(strings.TrimSpace(cfg.AdminEmail))
	var existing int
	if err := database.QueryRow(`SELECT COUNT(*) FROM users WHERE lower(email) = ?`, email).Scan(&existing); err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}
	hash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		return err
	}
	if _, err := database.Exec(
		`INSERT INTO users (email, password_hash, name, is_admin, auth_provider) VALUES (?, ?, ?, 1, 'password')`,
		email, hash, "Admin"); err != nil {
		return err
	}
	logger.Info("seeded admin user", zap.String("email", email))
	return nil
}
