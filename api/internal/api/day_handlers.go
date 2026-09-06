package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/biswas-dev/lifeai/api/internal/dates"
)

// Metrics are the body numbers recorded against a day.
type Metrics struct {
	WeightKg   *float64 `json:"weight_kg"`
	BodyFatPct *float64 `json:"body_fat_pct"`
	RestingHR  *int     `json:"resting_hr"`
	SleepHours *float64 `json:"sleep_hours"`
	Steps      *int     `json:"steps"`
	WaterMl    *int     `json:"water_ml"`
	Mood       *int     `json:"mood"`
	Energy     *int     `json:"energy"`
	Note       string   `json:"note"`
	Source     string   `json:"source"`
}

// Totals are the day's rolled-up nutrition and training numbers.
type Totals struct {
	Kcal              float64 `json:"kcal"`
	ProteinG          float64 `json:"protein_g"`
	CarbsG            float64 `json:"carbs_g"`
	FatG              float64 `json:"fat_g"`
	WorkoutMinutes    int     `json:"workout_minutes"`
	WorkoutKcal       float64 `json:"workout_kcal"`
	MeditationMinutes int     `json:"meditation_minutes"`
	Meals             int     `json:"meals"`
}

// Day is everything logged on one calendar date.
type Day struct {
	Date        string         `json:"date"`
	IsToday     bool           `json:"is_today"`
	Metrics     Metrics        `json:"metrics"`
	Water       []WaterEntry   `json:"water"`
	Meals       []Meal         `json:"meals"`
	Workouts    []Workout      `json:"workouts"`
	Meditations []Meditation   `json:"meditations"`
	Journal     []JournalEntry `json:"journal"`
	Photos      []Photo        `json:"photos"`
	Totals      Totals         `json:"totals"`
	Goals       Goals          `json:"goals"`
}

// DaySummary is one row of the calendar / history list.
type DaySummary struct {
	Date              string   `json:"date"`
	WaterMl           *int     `json:"water_ml"`
	Kcal              float64  `json:"kcal"`
	ProteinG          float64  `json:"protein_g"`
	WeightKg          *float64 `json:"weight_kg"`
	WorkoutMinutes    int      `json:"workout_minutes"`
	MeditationMinutes int      `json:"meditation_minutes"`
	Meals             int      `json:"meals"`
	Photos            int      `json:"photos"`
	Journal           int      `json:"journal"`
	Steps             *int     `json:"steps"`
	SleepHours        *float64 `json:"sleep_hours"`
	Mood              *int     `json:"mood"`
}

// dateParam reads and validates the {date} route parameter. "today" is
// accepted as an alias for the caller's current date.
func (s *Server) dateParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	d := strings.TrimSpace(chi.URLParam(r, "date"))
	if d == "today" || d == "" {
		return s.today(r.Context()), true
	}
	if !dates.Valid(d) {
		respondError(w, http.StatusBadRequest, "date must be YYYY-MM-DD", "invalid_date")
		return "", false
	}
	return d, true
}

// HandleGetToday returns the current day in the caller's zone.
func (s *Server) HandleGetToday(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	day, err := s.loadDay(ctx, UserID(ctx), s.today(ctx))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load today", "internal")
		return
	}
	respondJSON(w, http.StatusOK, day)
}

// HandleGetDay returns one date in full.
func (s *Server) HandleGetDay(w http.ResponseWriter, r *http.Request) {
	date, ok := s.dateParam(w, r)
	if !ok {
		return
	}
	day, err := s.loadDay(r.Context(), UserID(r.Context()), date)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load day", "internal")
		return
	}
	respondJSON(w, http.StatusOK, day)
}

type updateDayRequest struct {
	WeightKg   *float64 `json:"weight_kg"`
	BodyFatPct *float64 `json:"body_fat_pct"`
	RestingHR  *int     `json:"resting_hr"`
	SleepHours *float64 `json:"sleep_hours"`
	Steps      *int     `json:"steps"`
	WaterMl    *int     `json:"water_ml"`
	Mood       *int     `json:"mood"`
	Energy     *int     `json:"energy"`
	Note       *string  `json:"note"`
	// Clear lists fields to set back to "not recorded".
	Clear []string `json:"clear"`
}

// HandleUpdateDay records body metrics for a date. Only the fields present
// are changed; a field can be cleared by naming it in "clear".
func (s *Server) HandleUpdateDay(w http.ResponseWriter, r *http.Request) {
	date, ok := s.dateParam(w, r)
	if !ok {
		return
	}
	var req updateDayRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.WeightKg != nil && (*req.WeightKg < 20 || *req.WeightKg > 400) {
		respondError(w, http.StatusBadRequest, "weight must be between 20 and 400 kg", "invalid_weight")
		return
	}
	if req.BodyFatPct != nil && (*req.BodyFatPct < 1 || *req.BodyFatPct > 70) {
		respondError(w, http.StatusBadRequest, "body fat must be between 1 and 70 percent", "invalid_body_fat")
		return
	}
	if req.RestingHR != nil && (*req.RestingHR < 25 || *req.RestingHR > 220) {
		respondError(w, http.StatusBadRequest, "resting heart rate out of range", "invalid_hr")
		return
	}
	if req.SleepHours != nil && (*req.SleepHours < 0 || *req.SleepHours > 24) {
		respondError(w, http.StatusBadRequest, "sleep hours out of range", "invalid_sleep")
		return
	}
	if req.Steps != nil && (*req.Steps < 0 || *req.Steps > 200000) {
		respondError(w, http.StatusBadRequest, "steps out of range", "invalid_steps")
		return
	}
	if req.WaterMl != nil && (*req.WaterMl < 0 || *req.WaterMl > 20000) {
		respondError(w, http.StatusBadRequest, "water out of range", "invalid_water")
		return
	}
	for _, v := range []*int{req.Mood, req.Energy} {
		if v != nil && (*v < 1 || *v > 5) {
			respondError(w, http.StatusBadRequest, "mood and energy are 1 to 5", "invalid_scale")
			return
		}
	}

	ctx := r.Context()
	userID := UserID(ctx)
	if err := s.ensureDay(ctx, userID, date); err != nil {
		respondError(w, http.StatusInternalServerError, "could not save day", "internal")
		return
	}

	sets := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := []any{}
	set := func(col string, v any) {
		sets = append(sets, col+" = ?")
		args = append(args, v)
	}
	if req.WeightKg != nil {
		set("weight_kg", *req.WeightKg)
	}
	if req.BodyFatPct != nil {
		set("body_fat_pct", *req.BodyFatPct)
	}
	if req.RestingHR != nil {
		set("resting_hr", *req.RestingHR)
	}
	if req.SleepHours != nil {
		set("sleep_hours", *req.SleepHours)
	}
	if req.Steps != nil {
		set("steps", *req.Steps)
	}
	if req.WaterMl != nil {
		set("water_ml", *req.WaterMl)
		sets = append(sets, "water_baseline_ml = ? - (SELECT COALESCE(SUM(amount_ml),0) FROM water_entries WHERE user_id=days.user_id AND on_date=days.on_date AND deleted_at IS NULL)")
		args = append(args, *req.WaterMl)
	}
	if req.Mood != nil {
		set("mood", *req.Mood)
	}
	if req.Energy != nil {
		set("energy", *req.Energy)
	}
	if req.Note != nil {
		n := strings.TrimSpace(*req.Note)
		if len(n) > 4000 {
			n = n[:4000]
		}
		set("note", n)
	}
	clearable := map[string]bool{"weight_kg": true, "body_fat_pct": true, "resting_hr": true,
		"sleep_hours": true, "steps": true, "water_ml": true, "mood": true, "energy": true}
	for _, c := range req.Clear {
		if clearable[c] {
			sets = append(sets, c+" = NULL")
			if c == "water_ml" {
				sets = append(sets, "water_baseline_ml = -(SELECT COALESCE(SUM(amount_ml),0) FROM water_entries WHERE user_id=days.user_id AND on_date=days.on_date AND deleted_at IS NULL)")
			}
		}
	}
	// A hand edit takes the row over from an import, and the fields touched
	// are remembered so a later import never overwrites them.
	sets = append(sets, "source = 'manual'")
	touched := []string{}
	for col, v := range map[string]bool{
		"weight_kg": req.WeightKg != nil, "body_fat_pct": req.BodyFatPct != nil, "resting_hr": req.RestingHR != nil,
		"sleep_hours": req.SleepHours != nil, "steps": req.Steps != nil, "water_ml": req.WaterMl != nil,
	} {
		if v {
			touched = append(touched, col)
		}
	}
	for _, c := range req.Clear {
		if clearable[c] {
			touched = append(touched, c)
		}
	}
	if len(touched) > 0 {
		if err := s.markManual(ctx, userID, date, touched); err != nil {
			respondError(w, http.StatusInternalServerError, "could not save day", "internal")
			return
		}
	}
	args = append(args, userID, date)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE days SET `+strings.Join(sets, ", ")+` WHERE user_id = ? AND on_date = ?`, args...); err != nil {
		respondError(w, http.StatusInternalServerError, "could not save day", "internal")
		return
	}
	day, err := s.loadDay(ctx, userID, date)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load day", "internal")
		return
	}
	respondJSON(w, http.StatusOK, day)
}

// HandleListDays returns one summary per date in [from, to], including dates
// with nothing logged, so a calendar can render the whole range.
func (s *Server) HandleListDays(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	today := s.today(ctx)
	to := r.URL.Query().Get("to")
	from := r.URL.Query().Get("from")
	if to == "" {
		to = today
	}
	if from == "" {
		from = dates.AddDays(to, -29)
	}
	if !dates.Valid(from) || !dates.Valid(to) {
		respondError(w, http.StatusBadRequest, "from and to must be YYYY-MM-DD", "invalid_date")
		return
	}
	if dates.DaysBetween(from, to) > 400 {
		respondError(w, http.StatusBadRequest, "range too large", "invalid_range")
		return
	}
	out, err := s.daySummaries(ctx, UserID(ctx), from, to)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not list days", "internal")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// ensureDay creates the day row if it does not exist.
func (s *Server) ensureDay(ctx context.Context, userID int64, date string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO days (user_id, on_date) VALUES (?, ?) ON CONFLICT(user_id, on_date) DO NOTHING`, userID, date)
	return err
}

func (s *Server) loadMetrics(ctx context.Context, userID int64, date string) (Metrics, error) {
	var (
		m                      Metrics
		weight, fat, sleep     sql.NullFloat64
		hr, steps, water, mood sql.NullInt64
		energy                 sql.NullInt64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT weight_kg, body_fat_pct, resting_hr, sleep_hours, steps, water_ml, mood, energy, note, source
		  FROM days WHERE user_id = ? AND on_date = ?`, userID, date).
		Scan(&weight, &fat, &hr, &sleep, &steps, &water, &mood, &energy, &m.Note, &m.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return Metrics{Source: "manual"}, nil
	}
	if err != nil {
		return m, err
	}
	m.WeightKg, m.BodyFatPct, m.SleepHours = floatPtr(weight), floatPtr(fat), floatPtr(sleep)
	m.RestingHR, m.Steps, m.WaterMl, m.Mood, m.Energy = intPtr(hr), intPtr(steps), intPtr(water), intPtr(mood), intPtr(energy)
	return m, nil
}

func (s *Server) loadDay(ctx context.Context, userID int64, date string) (Day, error) {
	day := Day{Date: date, IsToday: date == s.today(ctx)}
	var err error
	if day.Metrics, err = s.loadMetrics(ctx, userID, date); err != nil {
		return day, err
	}
	if day.Water, err = s.waterForDate(ctx, userID, date); err != nil {
		return day, err
	}
	if day.Meals, err = s.mealsForDate(ctx, userID, date); err != nil {
		return day, err
	}
	if day.Workouts, err = s.workoutsForDate(ctx, userID, date); err != nil {
		return day, err
	}
	if day.Meditations, err = s.meditationsForDate(ctx, userID, date); err != nil {
		return day, err
	}
	if day.Journal, err = s.journalForDate(ctx, userID, date); err != nil {
		return day, err
	}
	if day.Photos, err = s.photosForDate(ctx, userID, date); err != nil {
		return day, err
	}
	if day.Goals, err = s.goals(ctx, userID); err != nil {
		return day, err
	}
	for _, m := range day.Meals {
		day.Totals.Kcal += m.Kcal
		day.Totals.ProteinG += m.ProteinG
		day.Totals.CarbsG += m.CarbsG
		day.Totals.FatG += m.FatG
	}
	day.Totals.Meals = len(day.Meals)
	for _, wk := range day.Workouts {
		day.Totals.WorkoutMinutes += wk.Minutes
		if wk.Kcal != nil {
			day.Totals.WorkoutKcal += *wk.Kcal
		}
	}
	for _, m := range day.Meditations {
		day.Totals.MeditationMinutes += m.Minutes
	}
	return day, nil
}

func (s *Server) daySummaries(ctx context.Context, userID int64, from, to string) ([]DaySummary, error) {
	byDate := map[string]*DaySummary{}
	for _, d := range dates.Range(from, to) {
		byDate[d] = &DaySummary{Date: d}
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT on_date, weight_kg, steps, sleep_hours, mood, water_ml FROM days
		 WHERE user_id = ? AND on_date BETWEEN ? AND ?`, userID, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			d                  string
			weight, sleep      sql.NullFloat64
			steps, mood, water sql.NullInt64
		)
		if err := rows.Scan(&d, &weight, &steps, &sleep, &mood, &water); err != nil {
			rows.Close()
			return nil, err
		}
		if sum := byDate[d]; sum != nil {
			sum.WaterMl = intPtr(water)
			sum.WeightKg, sum.Steps, sum.SleepHours, sum.Mood = floatPtr(weight), intPtr(steps), floatPtr(sleep), intPtr(mood)
		}
	}
	rows.Close()

	agg := func(query string, fn func(sum *DaySummary, a float64, b float64)) error {
		rows, err := s.db.QueryContext(ctx, query, userID, from, to)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d string
			var a, b float64
			if err := rows.Scan(&d, &a, &b); err != nil {
				return err
			}
			if sum := byDate[d]; sum != nil {
				fn(sum, a, b)
			}
		}
		return rows.Err()
	}
	if err := agg(`SELECT on_date, COALESCE(SUM(kcal),0), COALESCE(SUM(protein_g),0) FROM meals WHERE user_id = ? AND on_date BETWEEN ? AND ? GROUP BY on_date`,
		func(sum *DaySummary, a, b float64) { sum.Kcal, sum.ProteinG = a, b }); err != nil {
		return nil, err
	}
	if err := agg(`SELECT on_date, COUNT(*), 0 FROM meals WHERE user_id = ? AND on_date BETWEEN ? AND ? GROUP BY on_date`,
		func(sum *DaySummary, a, _ float64) { sum.Meals = int(a) }); err != nil {
		return nil, err
	}
	if err := agg(`SELECT on_date, COALESCE(SUM(minutes),0), 0 FROM workouts WHERE user_id = ? AND on_date BETWEEN ? AND ? GROUP BY on_date`,
		func(sum *DaySummary, a, _ float64) { sum.WorkoutMinutes = int(a) }); err != nil {
		return nil, err
	}
	if err := agg(`SELECT on_date, COALESCE(SUM(minutes),0), 0 FROM meditations WHERE user_id = ? AND on_date BETWEEN ? AND ? GROUP BY on_date`,
		func(sum *DaySummary, a, _ float64) { sum.MeditationMinutes = int(a) }); err != nil {
		return nil, err
	}
	if err := agg(`SELECT on_date, COUNT(*), 0 FROM photos WHERE user_id = ? AND on_date BETWEEN ? AND ? GROUP BY on_date`,
		func(sum *DaySummary, a, _ float64) { sum.Photos = int(a) }); err != nil {
		return nil, err
	}
	if err := agg(`SELECT on_date, COUNT(*), 0 FROM journal_entries WHERE user_id = ? AND on_date BETWEEN ? AND ? GROUP BY on_date`,
		func(sum *DaySummary, a, _ float64) { sum.Journal = int(a) }); err != nil {
		return nil, err
	}

	out := make([]DaySummary, 0, len(byDate))
	for _, d := range dates.Range(from, to) {
		out = append(out, *byDate[d])
	}
	return out, nil
}
