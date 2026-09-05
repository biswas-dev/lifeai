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
	DailyKcal      *int     `json:"daily_kcal"`
	ProteinG       *int     `json:"protein_g"`
	CarbsG         *int     `json:"carbs_g"`
	FatG           *int     `json:"fat_g"`
	TargetWeightKg *float64 `json:"target_weight_kg"`
	Steps          *int     `json:"steps"`
	WaterMl        *int     `json:"water_ml"`
	SleepHours     *float64 `json:"sleep_hours"`
	WorkoutMinutes *int     `json:"workout_minutes"`
	Notes          string   `json:"notes"`
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
	if g.DailyKcal != nil && (*g.DailyKcal < 0 || *g.DailyKcal > 20000) {
		respondError(w, http.StatusBadRequest, "daily calories out of range", "invalid_goal")
		return
	}
	if g.TargetWeightKg != nil && (*g.TargetWeightKg < 20 || *g.TargetWeightKg > 400) {
		respondError(w, http.StatusBadRequest, "target weight out of range", "invalid_goal")
		return
	}
	g.Notes = strings.TrimSpace(g.Notes)
	if len(g.Notes) > 2000 {
		g.Notes = g.Notes[:2000]
	}
	_, err := s.db.ExecContext(r.Context(), `
		INSERT INTO goals (user_id, daily_kcal, protein_g, carbs_g, fat_g, target_weight_kg, steps, water_ml, sleep_hours, workout_minutes, notes, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id) DO UPDATE SET
			daily_kcal = excluded.daily_kcal, protein_g = excluded.protein_g, carbs_g = excluded.carbs_g,
			fat_g = excluded.fat_g, target_weight_kg = excluded.target_weight_kg, steps = excluded.steps,
			water_ml = excluded.water_ml, sleep_hours = excluded.sleep_hours, workout_minutes = excluded.workout_minutes,
			notes = excluded.notes, updated_at = CURRENT_TIMESTAMP`,
		UserID(r.Context()), nullInt(g.DailyKcal), nullInt(g.ProteinG), nullInt(g.CarbsG), nullInt(g.FatG),
		nullFloat(g.TargetWeightKg), nullInt(g.Steps), nullInt(g.WaterMl), nullFloat(g.SleepHours),
		nullInt(g.WorkoutMinutes), g.Notes)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not save goals", "internal")
		return
	}
	respondJSON(w, http.StatusOK, g)
}

func (s *Server) goals(ctx context.Context, userID int64) (Goals, error) {
	var (
		g                                        Goals
		kcal, prot, carbs, fat, steps, water, wm sql.NullInt64
		weight, sleep                            sql.NullFloat64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT daily_kcal, protein_g, carbs_g, fat_g, target_weight_kg, steps, water_ml, sleep_hours, workout_minutes, notes
		  FROM goals WHERE user_id = ?`, userID).
		Scan(&kcal, &prot, &carbs, &fat, &weight, &steps, &water, &sleep, &wm, &g.Notes)
	if errors.Is(err, sql.ErrNoRows) {
		return Goals{}, nil
	}
	if err != nil {
		return g, err
	}
	g.DailyKcal, g.ProteinG, g.CarbsG, g.FatG = intPtr(kcal), intPtr(prot), intPtr(carbs), intPtr(fat)
	g.TargetWeightKg, g.SleepHours = floatPtr(weight), floatPtr(sleep)
	g.Steps, g.WaterMl, g.WorkoutMinutes = intPtr(steps), intPtr(water), intPtr(wm)
	return g, nil
}
