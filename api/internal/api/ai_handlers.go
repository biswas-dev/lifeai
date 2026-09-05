package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/biswas-dev/lifeai/api/internal/aifeatures"
	"github.com/biswas-dev/lifeai/api/internal/dates"
)

// AIStatus tells the client what the AI features can do.
type AIStatus struct {
	Enabled       bool     `json:"enabled"`
	Providers     []string `json:"providers"`
	Vision        []string `json:"vision"`
	DailyLimit    int      `json:"daily_limit"`
	UsedToday     int      `json:"used_today"`
	RemainingCall int      `json:"remaining"`
}

// HandleAIStatus reports whether AI is on and how much quota is left.
func (s *Server) HandleAIStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	used := s.aiRunsLast24h(ctx, UserID(ctx))
	remaining := s.cfg.AIDailyLimit - used
	if remaining < 0 {
		remaining = 0
	}
	respondJSON(w, http.StatusOK, AIStatus{
		Enabled:       s.ai.Enabled(),
		Providers:     orEmpty(s.ai.Providers()),
		Vision:        orEmpty(s.ai.VisionProviders()),
		DailyLimit:    s.cfg.AIDailyLimit,
		UsedToday:     used,
		RemainingCall: remaining,
	})
}

func orEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func (s *Server) aiRunsLast24h(ctx context.Context, userID int64) int {
	var n int
	_ = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ai_runs WHERE user_id = ? AND created_at > ?`,
		userID, time.Now().UTC().Add(-24*time.Hour)).Scan(&n)
	return n
}

// checkAIQuota returns ErrAIQuota when the caller has used the allowance.
func (s *Server) checkAIQuota(ctx context.Context, userID int64) error {
	if s.aiRunsLast24h(ctx, userID) >= s.cfg.AIDailyLimit {
		return ErrAIQuota
	}
	return nil
}

// recordAIRun writes the ledger row. Errors are logged, never surfaced: the
// ledger must not fail a request that already got its answer.
func (s *Server) recordAIRun(ctx context.Context, userID int64, feature string, m aifeatures.Meta, callErr error) {
	errText := ""
	if callErr != nil {
		errText = callErr.Error()
		if len(errText) > 500 {
			errText = errText[:500]
		}
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO ai_runs (user_id, feature, provider, model, input_hash, result_json, tokens_in, tokens_out, attempts, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, feature, m.Provider, m.Model, m.InputHash, m.ResultJSON, m.TokensIn, m.TokensOut, max1(m.Attempts), errText); err != nil {
		s.log.Warn("record ai run", zap.Error(err))
	}
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// cachedResult returns the stored result for identical inputs, if any.
func (s *Server) cachedResult(ctx context.Context, userID int64, feature, hash string) (string, bool) {
	var result string
	err := s.db.QueryRowContext(ctx, `
		SELECT result_json FROM ai_runs
		 WHERE user_id = ? AND feature = ? AND input_hash = ? AND error = '' AND result_json <> ''
		 ORDER BY created_at DESC LIMIT 1`, userID, feature, hash).Scan(&result)
	if err != nil || result == "" {
		return "", false
	}
	return result, true
}

func decodeCached(result string, v any) error {
	return json.Unmarshal([]byte(result), v)
}

// hashFor fingerprints AI inputs for the result cache.
func hashFor(data []byte, text string) string {
	h := sha256.New()
	h.Write(data)
	h.Write([]byte{0})
	h.Write([]byte(text))
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Server) aiGate(w http.ResponseWriter, r *http.Request) bool {
	if !s.ai.Enabled() {
		respondError(w, http.StatusServiceUnavailable, "AI features are not configured on this server", "ai_disabled")
		return false
	}
	if err := s.checkAIQuota(r.Context(), UserID(r.Context())); err != nil {
		respondError(w, http.StatusTooManyRequests, err.Error(), "ai_quota")
		return false
	}
	return true
}

func aiErrorResponse(w http.ResponseWriter, err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		respondError(w, http.StatusGatewayTimeout, "the model took too long to answer; try again", "ai_timeout")
		return
	}
	respondError(w, http.StatusBadGateway, "the AI provider could not answer: "+strings.TrimPrefix(err.Error(), "aifeatures: "), "ai_failed")
}

// ---- food ----

type analyzeFoodRequest struct {
	PhotoID int64  `json:"photo_id"`
	Hint    string `json:"hint"`
}

// HandleAnalyzeFood estimates a photo synchronously and returns the estimate
// without logging a meal — the sheet decides what to do with it.
func (s *Server) HandleAnalyzeFood(w http.ResponseWriter, r *http.Request) {
	if !s.aiGate(w, r) {
		return
	}
	var req analyzeFoodRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	userID := UserID(ctx)
	var relPath string
	if err := s.db.QueryRowContext(ctx, `SELECT rel_path FROM photos WHERE id = ? AND user_id = ?`, req.PhotoID, userID).Scan(&relPath); err != nil {
		respondError(w, http.StatusNotFound, "photo not found", "not_found")
		return
	}
	f, err := s.photos.Open(relPath)
	if err != nil {
		respondError(w, http.StatusNotFound, "photo file missing", "not_found")
		return
	}
	data, err := readAll(f)
	f.Close()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not read photo", "internal")
		return
	}
	hint := strings.TrimSpace(req.Hint)
	hash := hashFor(data, hint)
	if cached, ok := s.cachedResult(ctx, userID, aifeatures.FeatureFoodPhoto, hash); ok {
		var est aifeatures.FoodEstimate
		if decodeCached(cached, &est) == nil && est.Kcal > 0 {
			respondJSON(w, http.StatusOK, map[string]any{"estimate": est, "cached": true})
			return
		}
	}
	est, meta, err := s.ai.EstimateFood(ctx, data, "image/jpeg", hint)
	s.recordAIRun(ctx, userID, aifeatures.FeatureFoodPhoto, meta, err)
	if err != nil {
		aiErrorResponse(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"estimate": est, "cached": false, "provider": meta.Provider, "model": meta.Model})
}

// ---- recipes ----

type suggestRecipesRequest struct {
	Ingredients []string `json:"ingredients"`
	Preferences string   `json:"preferences"`
	MealSlot    string   `json:"meal_slot"`
	PhotoID     *int64   `json:"photo_id"`
	Date        string   `json:"date"`
}

// HandleSuggestRecipes proposes meals that fit what is left of the day.
func (s *Server) HandleSuggestRecipes(w http.ResponseWriter, r *http.Request) {
	if !s.aiGate(w, r) {
		return
	}
	var req suggestRecipesRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	userID := UserID(ctx)
	date := req.Date
	if !dates.Valid(date) {
		date = s.today(ctx)
	}
	day, err := s.loadDay(ctx, userID, date)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load day", "internal")
		return
	}
	ai := aifeatures.RecipeRequest{
		Ingredients: cleanLines(req.Ingredients, 40),
		Preferences: strings.TrimSpace(req.Preferences),
		MealSlot:    strings.ToLower(strings.TrimSpace(req.MealSlot)),
		Goals:       day.Goals.Notes,
	}
	if day.Goals.DailyKcal != nil {
		ai.RemainingKcal = float64(*day.Goals.DailyKcal) - day.Totals.Kcal
	} else {
		ai.RemainingKcal = 2000 - day.Totals.Kcal
	}
	if ai.RemainingKcal < 150 {
		ai.RemainingKcal = 150
	}
	if day.Goals.ProteinG != nil {
		ai.RemainingProtein = float64(*day.Goals.ProteinG) - day.Totals.ProteinG
	}
	if req.PhotoID != nil {
		var relPath string
		if err := s.db.QueryRowContext(ctx, `SELECT rel_path FROM photos WHERE id = ? AND user_id = ?`, *req.PhotoID, userID).Scan(&relPath); err != nil {
			respondError(w, http.StatusNotFound, "photo not found", "not_found")
			return
		}
		if f, err := s.photos.Open(relPath); err == nil {
			ai.Image, _ = readAll(f)
			ai.MediaType = "image/jpeg"
			f.Close()
		}
	}
	recipes, meta, err := s.ai.SuggestRecipes(ctx, ai)
	s.recordAIRun(ctx, userID, aifeatures.FeatureRecipes, meta, err)
	if err != nil {
		aiErrorResponse(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"recipes":        recipes,
		"remaining_kcal": ai.RemainingKcal,
		"provider":       meta.Provider,
	})
}

type importRecipeRequest struct {
	Text string `json:"text"`
}

// HandleImportRecipe structures pasted recipe text. It does not save; the
// client shows the result for review and then POSTs it to /recipes.
func (s *Server) HandleImportRecipe(w http.ResponseWriter, r *http.Request) {
	if !s.aiGate(w, r) {
		return
	}
	var req importRecipeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	text := strings.TrimSpace(req.Text)
	if len(text) < 20 {
		respondError(w, http.StatusBadRequest, "paste the whole recipe, not just a title", "too_short")
		return
	}
	if len(text) > 20000 {
		text = text[:20000]
	}
	ctx := r.Context()
	userID := UserID(ctx)
	hash := hashFor(nil, text)
	if cached, ok := s.cachedResult(ctx, userID, aifeatures.FeatureRecipeImport, hash); ok {
		var rc aifeatures.Recipe
		if decodeCached(cached, &rc) == nil && rc.Name != "" {
			respondJSON(w, http.StatusOK, map[string]any{"recipe": rc, "cached": true})
			return
		}
	}
	rc, meta, err := s.ai.ImportRecipe(ctx, text)
	s.recordAIRun(ctx, userID, aifeatures.FeatureRecipeImport, meta, err)
	if err != nil {
		aiErrorResponse(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"recipe": rc, "cached": false})
}

// ---- plan and coach ----

// history assembles the brief the plan and the note are written from.
func (s *Server) history(ctx context.Context, userID int64, windowDays int) (aifeatures.History, error) {
	st, err := s.stats(ctx, userID, windowDays)
	if err != nil {
		return aifeatures.History{}, err
	}
	today, err := s.loadDay(ctx, userID, s.today(ctx))
	if err != nil {
		return aifeatures.History{}, err
	}
	h := aifeatures.History{
		WindowDays:      windowDays,
		DaysLogged:      st.DaysLogged,
		AvgKcal:         st.AvgKcal,
		AvgProtein:      st.AvgProtein,
		WorkoutCount:    st.WorkoutCount,
		WorkoutMinutes:  st.WorkoutMinutes,
		MeditationMin:   st.MeditationMinutes,
		AvgSleep:        st.AvgSleep,
		AvgSteps:        st.AvgSteps,
		Goals:           today.Goals.Notes,
		TodayKcal:       today.Totals.Kcal,
		TodayProtein:    today.Totals.ProteinG,
		TodayWorkoutMin: today.Totals.WorkoutMinutes,
		TodayNote:       today.Metrics.Note,
		WorkoutKinds:    map[string]int{},
	}
	if today.Goals.DailyKcal != nil {
		h.KcalTarget = *today.Goals.DailyKcal
	}
	if today.Goals.ProteinG != nil {
		h.ProteinTarget = *today.Goals.ProteinG
	}
	if today.Goals.TargetWeightKg != nil {
		h.TargetWeight = *today.Goals.TargetWeightKg
	}
	if st.WeightTrend.First != nil {
		h.WeightFirst = *st.WeightTrend.First
	}
	if st.WeightTrend.Latest != nil {
		h.WeightLatest = *st.WeightTrend.Latest
	}
	if st.RestingHRTrend.Latest != nil {
		h.RestingHRLatest = int(*st.RestingHRTrend.Latest)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT kind, COUNT(*) FROM workouts WHERE user_id = ? AND on_date BETWEEN ? AND ? GROUP BY kind`, userID, st.From, st.To)
	if err == nil {
		for rows.Next() {
			var k string
			var n int
			if rows.Scan(&k, &n) == nil {
				h.WorkoutKinds[k] = n
			}
		}
		rows.Close()
	}
	series, err := s.markerSeries(ctx, userID)
	if err != nil {
		return h, err
	}
	var healthContext strings.Builder
	for _, marker := range series {
		if marker.Latest != nil && (marker.Watch || marker.Flag == "high" || marker.Flag == "low") {
			fmt.Fprintf(&healthContext, "%s: %g %s on %s; lab flag: %s.\n", marker.Name, marker.Latest.Value, marker.Unit, marker.Latest.Date, marker.Flag)
		}
	}
	h.HealthContext = healthContext.String()
	return h, nil
}

type planRequest struct {
	Force bool `json:"force"`
}

// HandleBuildPlan writes a week of guidance from the record. Cached per
// distinct brief, so re-opening the page is free until something changes.
func (s *Server) HandleBuildPlan(w http.ResponseWriter, r *http.Request) {
	if !s.ai.Enabled() {
		respondError(w, http.StatusServiceUnavailable, "AI features are not configured on this server", "ai_disabled")
		return
	}
	var req planRequest
	if r.ContentLength > 0 && !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	userID := UserID(ctx)
	h, err := s.history(ctx, userID, 28)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not build history", "internal")
		return
	}
	hash := hashFor(nil, aifeatures.RenderHistory(h))
	if !req.Force {
		if cached, ok := s.cachedResult(ctx, userID, aifeatures.FeaturePlan, hash); ok {
			var plan aifeatures.Plan
			if decodeCached(cached, &plan) == nil && len(plan.Days) > 0 {
				respondJSON(w, http.StatusOK, map[string]any{"plan": plan, "cached": true})
				return
			}
		}
	}
	if err := s.checkAIQuota(ctx, userID); err != nil {
		respondError(w, http.StatusTooManyRequests, err.Error(), "ai_quota")
		return
	}
	plan, meta, err := s.ai.BuildPlan(ctx, h)
	s.recordAIRun(ctx, userID, aifeatures.FeaturePlan, meta, err)
	if err != nil {
		aiErrorResponse(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"plan": plan, "cached": false})
}

// HandleCoachNote writes the short daily note. One per distinct state of the
// day: the brief includes today's totals, so logging a meal earns a new note.
func (s *Server) HandleCoachNote(w http.ResponseWriter, r *http.Request) {
	if !s.ai.Enabled() {
		respondError(w, http.StatusServiceUnavailable, "AI features are not configured on this server", "ai_disabled")
		return
	}
	ctx := r.Context()
	userID := UserID(ctx)
	h, err := s.history(ctx, userID, 14)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not build history", "internal")
		return
	}
	hash := hashFor(nil, aifeatures.RenderHistory(h))
	if cached, ok := s.cachedResult(ctx, userID, aifeatures.FeatureCoach, hash); ok {
		var note aifeatures.CoachNote
		if decodeCached(cached, &note) == nil && note.Note != "" {
			respondJSON(w, http.StatusOK, map[string]any{"note": note, "cached": true})
			return
		}
	}
	if err := s.checkAIQuota(ctx, userID); err != nil {
		respondError(w, http.StatusTooManyRequests, err.Error(), "ai_quota")
		return
	}
	note, meta, err := s.ai.DailyNote(ctx, h)
	s.recordAIRun(ctx, userID, aifeatures.FeatureCoach, meta, err)
	if err != nil {
		aiErrorResponse(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"note": note, "cached": false})
}
