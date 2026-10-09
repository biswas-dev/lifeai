package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
)

// Goals are the person's targets. Every field is optional.
type Goals struct {
	DailyKcal              *int     `json:"daily_kcal"`
	ProteinG               *int     `json:"protein_g"`
	CarbsG                 *int     `json:"carbs_g"`
	FatG                   *int     `json:"fat_g"`
	TargetWeightKg         *float64 `json:"target_weight_kg"`
	Steps                  *int     `json:"steps"`
	WaterMl                *int     `json:"water_ml"`
	SleepHours             *float64 `json:"sleep_hours"`
	WorkoutMinutes         *int     `json:"workout_minutes"`
	WeeklyWorkoutMinutes   *int     `json:"weekly_workout_minutes"`
	MeditationMinutes      *int     `json:"meditation_minutes"`
	QuickWaterMl           *int     `json:"quick_water_ml"`
	QuickWorkoutMinutes    *int     `json:"quick_workout_minutes"`
	QuickMeditationMinutes *int     `json:"quick_meditation_minutes"`
	Notes                  string   `json:"notes"`
}

// HandleGetGoals returns the caller's targets, all nil when none are set.
func (s *Server) HandleGetGoals(w http.ResponseWriter, r *http.Request) {
	g, err := s.goals(r.Context(), UserID(r.Context()))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load goals", "internal")
		return
	}
	respondJSON(w, http.StatusOK, g)
}

// HandleSaveGoals replaces the caller's targets.
func (s *Server) HandleSaveGoals(w http.ResponseWriter, r *http.Request) {
	var g Goals
	if !decodeJSON(w, r, &g) {
		return
	}
	saved, err := s.saveGoals(r.Context(), UserID(r.Context()), g)
	if err != nil {
		respondSaveError(w, err, "goals")
		return
	}
	respondJSON(w, http.StatusOK, saved)
}

// saveGoals validates and stores a complete set of targets.
func (s *Server) saveGoals(ctx context.Context, userID int64, g Goals) (Goals, error) {
	if g.DailyKcal != nil && (*g.DailyKcal < 0 || *g.DailyKcal > 20000) {
		return g, badInput("invalid_goal", "daily calories out of range")
	}
	if g.WeeklyWorkoutMinutes != nil && (*g.WeeklyWorkoutMinutes < 0 || *g.WeeklyWorkoutMinutes > 7*24*60) {
		return g, badInput("invalid_goal", "weekly exercise minutes out of range")
	}
	if g.TargetWeightKg != nil && (*g.TargetWeightKg < 20 || *g.TargetWeightKg > 400) {
		return g, badInput("invalid_goal", "target weight out of range")
	}
	defaultWater, defaultWorkout, defaultMeditation := 946, 20, 10
	if g.QuickWaterMl == nil {
		g.QuickWaterMl = &defaultWater
	}
	if g.QuickWorkoutMinutes == nil {
		g.QuickWorkoutMinutes = &defaultWorkout
	}
	if g.QuickMeditationMinutes == nil {
		g.QuickMeditationMinutes = &defaultMeditation
	}
	for _, quick := range []*int{g.QuickWaterMl, g.QuickWorkoutMinutes, g.QuickMeditationMinutes} {
		if quick != nil && (*quick <= 0 || *quick > 20000) {
			return g, badInput("invalid_goal", "quick-add amount out of range")
		}
	}
	g.Notes = strings.TrimSpace(g.Notes)
	if len(g.Notes) > 2000 {
		g.Notes = g.Notes[:2000]
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO goals (user_id, daily_kcal, protein_g, carbs_g, fat_g, target_weight_kg, steps, water_ml, sleep_hours, workout_minutes, weekly_workout_minutes, meditation_minutes, quick_water_ml, quick_workout_minutes, quick_meditation_minutes, notes, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id) DO UPDATE SET
			daily_kcal = excluded.daily_kcal, protein_g = excluded.protein_g, carbs_g = excluded.carbs_g,
			fat_g = excluded.fat_g, target_weight_kg = excluded.target_weight_kg, steps = excluded.steps,
			water_ml = excluded.water_ml, sleep_hours = excluded.sleep_hours, workout_minutes = excluded.workout_minutes,
			weekly_workout_minutes = excluded.weekly_workout_minutes, meditation_minutes = excluded.meditation_minutes, quick_water_ml = excluded.quick_water_ml,
			quick_workout_minutes = excluded.quick_workout_minutes, quick_meditation_minutes = excluded.quick_meditation_minutes,
			notes = excluded.notes, updated_at = CURRENT_TIMESTAMP`,
		userID, nullInt(g.DailyKcal), nullInt(g.ProteinG), nullInt(g.CarbsG), nullInt(g.FatG),
		nullFloat(g.TargetWeightKg), nullInt(g.Steps), nullInt(g.WaterMl), nullFloat(g.SleepHours),
		nullInt(g.WorkoutMinutes), nullInt(g.WeeklyWorkoutMinutes), nullInt(g.MeditationMinutes), nullInt(g.QuickWaterMl),
		nullInt(g.QuickWorkoutMinutes), nullInt(g.QuickMeditationMinutes), g.Notes)
	if err != nil {
		return g, err
	}
	return s.goals(ctx, userID)
}

func (s *Server) goals(ctx context.Context, userID int64) (Goals, error) {
	var (
		g                                                              Goals
		kcal, prot, carbs, fat, steps, water, wm, wwm, mm, qw, qwk, qm sql.NullInt64
		weight, sleep                                                  sql.NullFloat64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT daily_kcal, protein_g, carbs_g, fat_g, target_weight_kg, steps, water_ml, sleep_hours, workout_minutes, weekly_workout_minutes,
		       meditation_minutes, quick_water_ml, quick_workout_minutes, quick_meditation_minutes, notes
		  FROM goals WHERE user_id = ?`, userID).
		Scan(&kcal, &prot, &carbs, &fat, &weight, &steps, &water, &sleep, &wm, &wwm, &mm, &qw, &qwk, &qm, &g.Notes)
	if errors.Is(err, sql.ErrNoRows) {
		water, workout, meditation := 946, 20, 10
		return Goals{QuickWaterMl: &water, QuickWorkoutMinutes: &workout, QuickMeditationMinutes: &meditation}, nil
	}
	if err != nil {
		return g, err
	}
	g.DailyKcal, g.ProteinG, g.CarbsG, g.FatG = intPtr(kcal), intPtr(prot), intPtr(carbs), intPtr(fat)
	g.TargetWeightKg, g.SleepHours = floatPtr(weight), floatPtr(sleep)
	g.Steps, g.WaterMl, g.WorkoutMinutes = intPtr(steps), intPtr(water), intPtr(wm)
	g.WeeklyWorkoutMinutes, g.MeditationMinutes = intPtr(wwm), intPtr(mm)
	g.QuickWaterMl, g.QuickWorkoutMinutes, g.QuickMeditationMinutes = intPtr(qw), intPtr(qwk), intPtr(qm)
	return g, nil
}
