package api

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"

	"github.com/biswas-dev/lifeai/api/internal/dates"
)

// Point is one value on one date.
type Point struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

// Trend summarises one measurement over the window.
type Trend struct {
	First   *float64 `json:"first,omitempty"`
	Latest  *float64 `json:"latest,omitempty"`
	Change  *float64 `json:"change,omitempty"`
	Best    *float64 `json:"best,omitempty"`
	Average *float64 `json:"average,omitempty"`
	Count   int      `json:"count"`
}

// Stats is the trends page.
type Stats struct {
	From string `json:"from"`
	To   string `json:"to"`

	Weight    []Point `json:"weight"`
	BodyFat   []Point `json:"body_fat"`
	RestingHR []Point `json:"resting_hr"`
	Sleep     []Point `json:"sleep"`
	Steps     []Point `json:"steps"`
	Kcal      []Point `json:"kcal"`
	Protein   []Point `json:"protein"`
	Training  []Point `json:"training"`
	Mood      []Point `json:"mood"`

	WeightTrend    Trend `json:"weight_trend"`
	RestingHRTrend Trend `json:"resting_hr_trend"`
	BodyFatTrend   Trend `json:"body_fat_trend"`

	DaysLogged        int     `json:"days_logged"`
	DaysInWindow      int     `json:"days_in_window"`
	AvgKcal           float64 `json:"avg_kcal"`
	AvgProtein        float64 `json:"avg_protein"`
	AvgSleep          float64 `json:"avg_sleep"`
	AvgSteps          float64 `json:"avg_steps"`
	WorkoutCount      int     `json:"workout_count"`
	WorkoutMinutes    int     `json:"workout_minutes"`
	MeditationMinutes int     `json:"meditation_minutes"`
	MealsLogged       int     `json:"meals_logged"`
	PhotosTaken       int     `json:"photos_taken"`
	JournalEntries    int     `json:"journal_entries"`
	// Streak is consecutive days ending today with something logged.
	Streak int `json:"streak"`
	// KcalAdherence is the share of logged days within 10% of the target.
	KcalAdherence *float64 `json:"kcal_adherence,omitempty"`
}

// HandleGetStats returns trends over the last N days (default 90).
func (s *Server) HandleGetStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	days := 90
	if raw := r.URL.Query().Get("days"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 7 && n <= 730 {
			days = n
		}
	}
	st, err := s.stats(ctx, UserID(ctx), days)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not compute stats", "internal")
		return
	}
	respondJSON(w, http.StatusOK, st)
}

// Dashboard is the whole main page in one call.
type Dashboard struct {
	Today  Day          `json:"today"`
	Week   []DaySummary `json:"week"`
	Stats  Stats        `json:"stats"`
	Recent []Recipe     `json:"recent_recipes"`
}

// HandleGetDashboard returns today, the last seven days and 30-day trends.
func (s *Server) HandleGetDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := UserID(ctx)
	today := s.today(ctx)
	var (
		d   Dashboard
		err error
	)
	if d.Today, err = s.loadDay(ctx, userID, today); err != nil {
		respondError(w, http.StatusInternalServerError, "could not load today", "internal")
		return
	}
	if d.Week, err = s.daySummaries(ctx, userID, dates.AddDays(today, -6), today); err != nil {
		respondError(w, http.StatusInternalServerError, "could not load week", "internal")
		return
	}
	if d.Stats, err = s.stats(ctx, userID, 30); err != nil {
		respondError(w, http.StatusInternalServerError, "could not load stats", "internal")
		return
	}
	d.Recent = []Recipe{}
	rows, err := s.db.QueryContext(ctx, `SELECT `+recipeColumns+` FROM recipes WHERE user_id = ? ORDER BY favourite DESC, COALESCE(last_cooked_at, updated_at) DESC LIMIT 6`, userID)
	if err == nil {
		for rows.Next() {
			if rc, err := scanRecipe(rows); err == nil {
				d.Recent = append(d.Recent, rc)
			}
		}
		rows.Close()
	}
	respondJSON(w, http.StatusOK, d)
}

func (s *Server) stats(ctx context.Context, userID int64, days int) (Stats, error) {
	to := s.today(ctx)
	from := dates.AddDays(to, -(days - 1))
	st := Stats{From: from, To: to, DaysInWindow: days}
	st.Weight, st.BodyFat, st.RestingHR, st.Sleep, st.Steps, st.Kcal, st.Protein, st.Training, st.Mood =
		[]Point{}, []Point{}, []Point{}, []Point{}, []Point{}, []Point{}, []Point{}, []Point{}, []Point{}

	rows, err := s.db.QueryContext(ctx, `
		SELECT on_date, weight_kg, body_fat_pct, resting_hr, sleep_hours, steps, mood
		  FROM days WHERE user_id = ? AND on_date BETWEEN ? AND ? ORDER BY on_date`, userID, from, to)
	if err != nil {
		return st, err
	}
	var sleepSum, stepSum float64
	var sleepN, stepN int
	for rows.Next() {
		var (
			d                  string
			weight, fat, sleep sql.NullFloat64
			hr, steps, mood    sql.NullInt64
		)
		if err := rows.Scan(&d, &weight, &fat, &hr, &sleep, &steps, &mood); err != nil {
			rows.Close()
			return st, err
		}
		if weight.Valid {
			st.Weight = append(st.Weight, Point{d, weight.Float64})
		}
		if fat.Valid {
			st.BodyFat = append(st.BodyFat, Point{d, fat.Float64})
		}
		if hr.Valid {
			st.RestingHR = append(st.RestingHR, Point{d, float64(hr.Int64)})
		}
		if sleep.Valid {
			st.Sleep = append(st.Sleep, Point{d, sleep.Float64})
			sleepSum += sleep.Float64
			sleepN++
		}
		if steps.Valid {
			st.Steps = append(st.Steps, Point{d, float64(steps.Int64)})
			stepSum += float64(steps.Int64)
			stepN++
		}
		if mood.Valid {
			st.Mood = append(st.Mood, Point{d, float64(mood.Int64)})
		}
	}
	rows.Close()
	if sleepN > 0 {
		st.AvgSleep = sleepSum / float64(sleepN)
	}
	if stepN > 0 {
		st.AvgSteps = stepSum / float64(stepN)
	}

	rows, err = s.db.QueryContext(ctx, `
		SELECT on_date, SUM(kcal), SUM(protein_g), COUNT(*) FROM meals
		 WHERE user_id = ? AND on_date BETWEEN ? AND ? GROUP BY on_date ORDER BY on_date`, userID, from, to)
	if err != nil {
		return st, err
	}
	var kcalSum, protSum float64
	var kcalDays int
	for rows.Next() {
		var d string
		var kcal, prot float64
		var n int
		if err := rows.Scan(&d, &kcal, &prot, &n); err != nil {
			rows.Close()
			return st, err
		}
		st.MealsLogged += n
		if kcal > 0 {
			st.Kcal = append(st.Kcal, Point{d, kcal})
			st.Protein = append(st.Protein, Point{d, prot})
			kcalSum += kcal
			protSum += prot
			kcalDays++
		}
	}
	rows.Close()
	if kcalDays > 0 {
		st.AvgKcal = kcalSum / float64(kcalDays)
		st.AvgProtein = protSum / float64(kcalDays)
	}

	rows, err = s.db.QueryContext(ctx, `
		SELECT on_date, SUM(minutes), COUNT(*) FROM workouts
		 WHERE user_id = ? AND on_date BETWEEN ? AND ? GROUP BY on_date ORDER BY on_date`, userID, from, to)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var d string
		var mins, n int
		if err := rows.Scan(&d, &mins, &n); err != nil {
			rows.Close()
			return st, err
		}
		st.Training = append(st.Training, Point{d, float64(mins)})
		st.WorkoutMinutes += mins
		st.WorkoutCount += n
	}
	rows.Close()

	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(minutes),0) FROM meditations WHERE user_id = ? AND on_date BETWEEN ? AND ?`, userID, from, to).Scan(&st.MeditationMinutes)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM photos WHERE user_id = ? AND on_date BETWEEN ? AND ?`, userID, from, to).Scan(&st.PhotosTaken)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM journal_entries WHERE user_id = ? AND on_date BETWEEN ? AND ?`, userID, from, to).Scan(&st.JournalEntries)

	// A day counts as logged when anything at all was recorded against it.
	logged := map[string]bool{}
	for _, q := range []string{
		`SELECT on_date FROM meals WHERE user_id = ? AND on_date BETWEEN ? AND ?`,
		`SELECT on_date FROM workouts WHERE user_id = ? AND on_date BETWEEN ? AND ?`,
		`SELECT on_date FROM meditations WHERE user_id = ? AND on_date BETWEEN ? AND ?`,
		`SELECT on_date FROM photos WHERE user_id = ? AND on_date BETWEEN ? AND ?`,
		`SELECT on_date FROM journal_entries WHERE user_id = ? AND on_date BETWEEN ? AND ?`,
		`SELECT on_date FROM days WHERE user_id = ? AND on_date BETWEEN ? AND ? AND (weight_kg IS NOT NULL OR steps IS NOT NULL OR sleep_hours IS NOT NULL OR resting_hr IS NOT NULL OR water_ml > 0 OR note <> '')`,
	} {
		rows, err := s.db.QueryContext(ctx, q, userID, from, to)
		if err != nil {
			return st, err
		}
		for rows.Next() {
			var d string
			if rows.Scan(&d) == nil {
				logged[d] = true
			}
		}
		rows.Close()
	}
	st.DaysLogged = len(logged)
	for d := to; logged[d]; d = dates.AddDays(d, -1) {
		st.Streak++
	}

	st.WeightTrend = trendOf(st.Weight, true)
	st.RestingHRTrend = trendOf(st.RestingHR, true)
	st.BodyFatTrend = trendOf(st.BodyFat, true)

	if g, err := s.goals(ctx, userID); err == nil && g.DailyKcal != nil && *g.DailyKcal > 0 && len(st.Kcal) > 0 {
		within := 0
		target := float64(*g.DailyKcal)
		for _, p := range st.Kcal {
			if p.Value >= target*0.9 && p.Value <= target*1.1 {
				within++
			}
		}
		adh := float64(within) / float64(len(st.Kcal)) * 100
		st.KcalAdherence = &adh
	}
	return st, nil
}

// trendOf summarises a series. lowerIsBetter picks the "best" direction.
func trendOf(points []Point, lowerIsBetter bool) Trend {
	t := Trend{Count: len(points)}
	if len(points) == 0 {
		return t
	}
	first, latest := points[0].Value, points[len(points)-1].Value
	best, sum := points[0].Value, 0.0
	for _, p := range points {
		sum += p.Value
		if (lowerIsBetter && p.Value < best) || (!lowerIsBetter && p.Value > best) {
			best = p.Value
		}
	}
	change := latest - first
	avg := sum / float64(len(points))
	t.First, t.Latest, t.Change, t.Best, t.Average = &first, &latest, &change, &best, &avg
	return t
}
