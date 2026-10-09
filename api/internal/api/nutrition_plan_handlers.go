package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
)

// NutritionPlan is a person's planned eating day: an eating window, daily
// targets, and the meals they intend to eat, each with one or more options
// (a dinner rotation, say) that can be pinned to days of the week.
type NutritionPlan struct {
	Name string `json:"name" jsonschema:"e.g. 1-7 PM plan"`
	// Source says where the plan came from, e.g. a nutritionist and a date.
	Source string `json:"source,omitempty"`
	// WindowStart and WindowEnd bound the eating window, HH:MM local. Both
	// empty means no fasting window.
	WindowStart string      `json:"window_start,omitempty" jsonschema:"HH:MM, first calories"`
	WindowEnd   string      `json:"window_end,omitempty" jsonschema:"HH:MM, last calories"`
	Targets     PlanTargets `json:"targets"`
	Meals       []PlanMeal  `json:"meals"`
	// Notes are reminders shown with the plan: measuring rules, cautions.
	Notes     []string `json:"notes,omitempty"`
	UpdatedAt string   `json:"updated_at,omitempty"`
}

// PlanTargets are daily targets; the max fields make a range.
type PlanTargets struct {
	Kcal        *float64 `json:"kcal,omitempty"`
	KcalMax     *float64 `json:"kcal_max,omitempty"`
	ProteinG    *float64 `json:"protein_g,omitempty"`
	ProteinGMax *float64 `json:"protein_g_max,omitempty"`
	CarbsG      *float64 `json:"carbs_g,omitempty"`
	FatG        *float64 `json:"fat_g,omitempty"`
}

// PlanMeal is one planned eating occasion.
type PlanMeal struct {
	Key     string       `json:"key" jsonschema:"short id, unique in the plan, e.g. lunch"`
	Time    string       `json:"time" jsonschema:"HH:MM"`
	EndTime string       `json:"end_time,omitempty" jsonschema:"HH:MM, for a window such as 18:15-18:45"`
	Title   string       `json:"title"`
	Slot    string       `json:"slot" jsonschema:"breakfast, lunch, dinner or snack"`
	Options []PlanOption `json:"options"`
}

// PlanOption is one way to eat a planned meal.
type PlanOption struct {
	Key      string   `json:"key" jsonschema:"short id, unique within the meal"`
	Title    string   `json:"title"`
	Items    []string `json:"items" jsonschema:"what to eat, with portions"`
	Kcal     float64  `json:"kcal"`
	ProteinG float64  `json:"protein_g"`
	CarbsG   float64  `json:"carbs_g"`
	FatG     float64  `json:"fat_g"`
	// Days pins the option to weekdays, 0 = Sunday. Empty means any day.
	Days []int `json:"days,omitempty" jsonschema:"weekdays this option is planned for, 0=Sunday...6=Saturday"`
}

var (
	clockRE = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
	keyRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)
)

func (p *NutritionPlan) validate() error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		p.Name = "My plan"
	}
	if (p.WindowStart == "") != (p.WindowEnd == "") {
		return badInput("invalid_plan", "set both window_start and window_end, or neither")
	}
	for _, t := range []string{p.WindowStart, p.WindowEnd} {
		if t != "" && !clockRE.MatchString(t) {
			return badInput("invalid_plan", "times must be HH:MM (24-hour)")
		}
	}
	if len(p.Meals) == 0 || len(p.Meals) > 12 {
		return badInput("invalid_plan", "a plan needs 1-12 meals")
	}
	if len(p.Notes) > 30 {
		return badInput("invalid_plan", "at most 30 notes")
	}
	seen := map[string]bool{}
	for i := range p.Meals {
		m := &p.Meals[i]
		m.Key, m.Title, m.Slot = strings.ToLower(strings.TrimSpace(m.Key)), strings.TrimSpace(m.Title), strings.ToLower(strings.TrimSpace(m.Slot))
		if !keyRE.MatchString(m.Key) || seen[m.Key] {
			return badInput("invalid_plan", "meal keys must be unique, lower-case letters, digits, - or _")
		}
		seen[m.Key] = true
		if !clockRE.MatchString(m.Time) || (m.EndTime != "" && !clockRE.MatchString(m.EndTime)) {
			return badInput("invalid_plan", "%s: times must be HH:MM (24-hour)", m.Key)
		}
		if !validSlot(m.Slot) {
			return badInput("invalid_plan", "%s: slot must be breakfast, lunch, dinner or snack", m.Key)
		}
		if len(m.Options) == 0 || len(m.Options) > 14 {
			return badInput("invalid_plan", "%s: each meal needs 1-14 options", m.Key)
		}
		opts := map[string]bool{}
		for j := range m.Options {
			o := &m.Options[j]
			o.Key, o.Title = strings.ToLower(strings.TrimSpace(o.Key)), strings.TrimSpace(o.Title)
			if !keyRE.MatchString(o.Key) || opts[o.Key] {
				return badInput("invalid_plan", "%s: option keys must be unique, lower-case letters, digits, - or _", m.Key)
			}
			opts[o.Key] = true
			if o.Kcal < 0 || o.Kcal > 10000 || o.ProteinG < 0 || o.CarbsG < 0 || o.FatG < 0 {
				return badInput("invalid_plan", "%s/%s: nutrition values out of range", m.Key, o.Key)
			}
			for _, d := range o.Days {
				if d < 0 || d > 6 {
					return badInput("invalid_plan", "%s/%s: days are 0 (Sunday) to 6 (Saturday)", m.Key, o.Key)
				}
			}
			if o.Items == nil {
				o.Items = []string{}
			}
		}
	}
	return nil
}

func (s *Server) nutritionPlan(ctx context.Context, userID int64) (*NutritionPlan, error) {
	var raw, updated string
	err := s.db.QueryRowContext(ctx, `SELECT plan_json, updated_at FROM nutrition_plans WHERE user_id = ?`, userID).Scan(&raw, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p NutritionPlan
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, err
	}
	p.UpdatedAt = updated
	return &p, nil
}

func (s *Server) saveNutritionPlan(ctx context.Context, userID int64, p NutritionPlan) (*NutritionPlan, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	p.UpdatedAt = ""
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO nutrition_plans (user_id, plan_json) VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET plan_json = excluded.plan_json, updated_at = CURRENT_TIMESTAMP`, userID, string(raw)); err != nil {
		return nil, err
	}
	return s.nutritionPlan(ctx, userID)
}

// HandleGetNutritionPlan returns {"plan": ...}, null when none is set.
func (s *Server) HandleGetNutritionPlan(w http.ResponseWriter, r *http.Request) {
	p, err := s.nutritionPlan(r.Context(), UserID(r.Context()))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load the plan", "internal")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"plan": p})
}

// HandleSaveNutritionPlan replaces the plan.
func (s *Server) HandleSaveNutritionPlan(w http.ResponseWriter, r *http.Request) {
	var p NutritionPlan
	if !decodeJSON(w, r, &p) {
		return
	}
	saved, err := s.saveNutritionPlan(r.Context(), UserID(r.Context()), p)
	if err != nil {
		respondSaveError(w, err, "plan")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"plan": saved})
}
