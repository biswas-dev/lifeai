package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/biswas-dev/lifeai/api/internal/dates"
	"github.com/biswas-dev/lifeai/api/internal/training"
)

// Workout is a logged training session. A strength session also lists its
// exercises and sets.
type Workout struct {
	ID         int64             `json:"id"`
	Date       string            `json:"date"`
	Kind       string            `json:"kind"`
	Activity   string            `json:"activity"`
	Minutes    int               `json:"minutes"`
	Kcal       *float64          `json:"kcal"`
	DistanceKm *float64          `json:"distance_km"`
	AvgHR      *int              `json:"avg_hr"`
	Notes      string            `json:"notes"`
	StartedAt  *string           `json:"started_at"`
	Source     string            `json:"source"`
	Sources    []string          `json:"sources"`
	Exercises  []WorkoutExercise `json:"exercises"`
	// StravaID is set once the session exists on Strava, whether imported
	// from there or uploaded from here.
	StravaID string `json:"strava_id,omitempty"`
}

// WorkoutExercise is one movement within a session.
type WorkoutExercise struct {
	ID int64 `json:"id"`
	// Slug refers to the exercise catalog; empty for a custom movement.
	Slug  string       `json:"slug"`
	Name  string       `json:"name"`
	Notes string       `json:"notes"`
	Sets  []WorkoutSet `json:"sets"`
}

// WorkoutSet is one set. Which fields are filled depends on the movement:
// weight and reps, reps alone, or a hold in seconds.
type WorkoutSet struct {
	Reps    *int     `json:"reps"`
	Weight  *float64 `json:"weight"`
	Unit    string   `json:"unit"`
	Seconds *int     `json:"seconds"`
}

// WorkoutKinds are the shapes a session can take.
var WorkoutKinds = []string{"strength", "cardio", "walk", "run", "cycle", "swim", "yoga", "hiit", "sport", "other"}

func validWorkoutKind(k string) bool {
	for _, v := range WorkoutKinds {
		if v == k {
			return true
		}
	}
	return false
}

// workoutInput is a session as written by the app, the REST API or an agent.
type workoutInput struct {
	Date       string          `json:"date"`
	Kind       string          `json:"kind"`
	Activity   string          `json:"activity"`
	Minutes    int             `json:"minutes"`
	Kcal       *float64        `json:"kcal"`
	DistanceKm *float64        `json:"distance_km"`
	AvgHR      *int            `json:"avg_hr"`
	Notes      string          `json:"notes"`
	StartedAt  *string         `json:"started_at"`
	Exercises  []exerciseInput `json:"exercises"`
}

type exerciseInput struct {
	Slug  string     `json:"slug,omitempty" jsonschema:"catalog slug from list_exercises; omit for a custom movement"`
	Name  string     `json:"name,omitempty" jsonschema:"display name; required when there is no slug. A recognised name is matched to the catalog"`
	Notes string     `json:"notes,omitempty" jsonschema:"coaching cues, variation or anything uncertain"`
	Sets  []setInput `json:"sets,omitempty" jsonschema:"in order; may be empty when sets were not counted"`
}

type setInput struct {
	Reps    *int     `json:"reps,omitempty" jsonschema:"repetitions (per side for one-sided movements)"`
	Weight  *float64 `json:"weight,omitempty" jsonschema:"load: per dumbbell, or the single dumbbell for goblet squats and one-arm rows"`
	Unit    string   `json:"unit,omitempty" jsonschema:"lb (default) or kg"`
	Seconds *int     `json:"seconds,omitempty" jsonschema:"duration of a hold or timed set"`
}

// inputError is a request a person can correct; it becomes a 400.
type inputError struct{ msg, code string }

func (e *inputError) Error() string { return e.msg }

func badInput(code, format string, a ...any) error {
	return &inputError{msg: fmt.Sprintf(format, a...), code: code}
}

func respondSaveError(w http.ResponseWriter, err error, what string) {
	var ie *inputError
	if errors.As(err, &ie) {
		respondError(w, http.StatusBadRequest, ie.msg, ie.code)
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		respondError(w, http.StatusNotFound, what+" not found", "not_found")
		return
	}
	respondError(w, http.StatusInternalServerError, "could not save "+what, "internal")
}

// normalise validates a session in place, filling defaults.
func (s *Server) normaliseWorkout(ctx context.Context, in *workoutInput) (startedAt any, err error) {
	in.Date = strings.TrimSpace(in.Date)
	if in.Date == "" || in.Date == "today" {
		in.Date = s.today(ctx)
	}
	if !dates.Valid(in.Date) {
		return nil, badInput("invalid_date", "date must be YYYY-MM-DD")
	}
	in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	if in.Kind == "" {
		in.Kind = "other"
		if len(in.Exercises) > 0 {
			in.Kind = "strength"
		}
	}
	if !validWorkoutKind(in.Kind) {
		return nil, badInput("invalid_kind", "unknown workout kind")
	}
	if in.Minutes <= 0 || in.Minutes > 24*60 {
		return nil, badInput("invalid_minutes", "minutes must be between 1 and 1440")
	}
	in.Activity, in.Notes = strings.TrimSpace(in.Activity), strings.TrimSpace(in.Notes)
	if in.StartedAt != nil && strings.TrimSpace(*in.StartedAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*in.StartedAt))
		if err != nil {
			return nil, badInput("invalid_time", "started_at must be RFC3339")
		}
		startedAt = t.UTC()
	}
	if len(in.Exercises) > 60 {
		return nil, badInput("too_many_exercises", "a session can hold at most 60 exercises")
	}
	for i := range in.Exercises {
		if err := normaliseExercise(&in.Exercises[i]); err != nil {
			return nil, err
		}
	}
	return startedAt, nil
}

func normaliseExercise(ex *exerciseInput) error {
	ex.Slug, ex.Name, ex.Notes = strings.ToLower(strings.TrimSpace(ex.Slug)), strings.TrimSpace(ex.Name), strings.TrimSpace(ex.Notes)
	if ex.Slug == "" {
		ex.Slug = training.Resolve(ex.Name)
	}
	if ex.Slug != "" {
		known, ok := training.Get(ex.Slug)
		if !ok {
			return badInput("unknown_exercise", "unknown exercise %q; list_exercises has the catalog, or send a name without a slug", ex.Slug)
		}
		if ex.Name == "" {
			ex.Name = known.Name
		}
	}
	if ex.Name == "" {
		return badInput("invalid_exercise", "each exercise needs a catalog slug or a name")
	}
	if len(ex.Name) > 120 {
		ex.Name = ex.Name[:120]
	}
	if len(ex.Sets) > 40 {
		return badInput("too_many_sets", "%s: at most 40 sets", ex.Name)
	}
	for i := range ex.Sets {
		st := &ex.Sets[i]
		st.Unit = strings.ToLower(strings.TrimSpace(st.Unit))
		if st.Unit == "" || st.Unit == "lbs" {
			st.Unit = "lb"
		}
		if st.Unit != "lb" && st.Unit != "kg" {
			return badInput("invalid_unit", "%s: unit must be lb or kg", ex.Name)
		}
		if st.Reps != nil && (*st.Reps < 0 || *st.Reps > 1000) {
			return badInput("invalid_set", "%s: reps must be 0-1000", ex.Name)
		}
		if st.Weight != nil && (*st.Weight < 0 || *st.Weight > 2000) {
			return badInput("invalid_set", "%s: weight must be 0-2000", ex.Name)
		}
		if st.Seconds != nil && (*st.Seconds < 0 || *st.Seconds > 36000) {
			return badInput("invalid_set", "%s: seconds must be 0-36000", ex.Name)
		}
	}
	return nil
}

// saveWorkout creates (id 0) or replaces a session and its exercises in one
// transaction.
func (s *Server) saveWorkout(ctx context.Context, userID, id int64, in workoutInput) (Workout, error) {
	started, err := s.normaliseWorkout(ctx, &in)
	if err != nil {
		return Workout{}, err
	}
	if err := s.ensureDay(ctx, userID, in.Date); err != nil {
		return Workout{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Workout{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	values := []any{in.Date, in.Kind, in.Activity, in.Minutes, nullFloat(in.Kcal), nullFloat(in.DistanceKm), nullInt(in.AvgHR), in.Notes, started}
	if id == 0 {
		res, err := tx.ExecContext(ctx, `INSERT INTO workouts (on_date, kind, activity, minutes, kcal, distance_km, avg_hr, notes, started_at, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, append(values, userID)...)
		if err != nil {
			return Workout{}, err
		}
		if id, err = res.LastInsertId(); err != nil {
			return Workout{}, err
		}
	} else {
		res, err := tx.ExecContext(ctx, `UPDATE workouts SET on_date = ?, kind = ?, activity = ?, minutes = ?, kcal = ?, distance_km = ?, avg_hr = ?, notes = ?, started_at = ? WHERE id = ? AND user_id = ?`, append(values, id, userID)...)
		if err != nil {
			return Workout{}, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return Workout{}, sql.ErrNoRows
		}
	}
	if err := replaceExercises(ctx, tx, userID, id, in.Exercises); err != nil {
		return Workout{}, err
	}
	if err := tx.Commit(); err != nil {
		return Workout{}, err
	}
	return s.workoutByID(ctx, userID, id)
}

func replaceExercises(ctx context.Context, tx *sql.Tx, userID, workoutID int64, list []exerciseInput) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM workout_exercises WHERE workout_id = ? AND user_id = ?`, workoutID, userID); err != nil {
		return err
	}
	for i, ex := range list {
		res, err := tx.ExecContext(ctx, `INSERT INTO workout_exercises (workout_id, user_id, position, exercise_slug, name, notes) VALUES (?, ?, ?, ?, ?, ?)`,
			workoutID, userID, i, ex.Slug, ex.Name, ex.Notes)
		if err != nil {
			return err
		}
		exID, _ := res.LastInsertId()
		for j, st := range ex.Sets {
			if _, err := tx.ExecContext(ctx, `INSERT INTO workout_sets (exercise_id, position, reps, weight, unit, seconds) VALUES (?, ?, ?, ?, ?, ?)`,
				exID, j, nullInt(st.Reps), nullFloat(st.Weight), st.Unit, nullInt(st.Seconds)); err != nil {
				return err
			}
		}
	}
	return nil
}

// HandleCreateWorkout logs a session, with exercises when it was strength work.
func (s *Server) HandleCreateWorkout(w http.ResponseWriter, r *http.Request) {
	var req workoutInput
	if !decodeJSON(w, r, &req) {
		return
	}
	wk, err := s.saveWorkout(r.Context(), UserID(r.Context()), 0, req)
	if err != nil {
		respondSaveError(w, err, "workout")
		return
	}
	respondJSON(w, http.StatusCreated, wk)
}

// HandleUpdateWorkout replaces a session's details and exercises.
func (s *Server) HandleUpdateWorkout(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "workoutID"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid workout id", "invalid_id")
		return
	}
	var req workoutInput
	if !decodeJSON(w, r, &req) {
		return
	}
	wk, err := s.saveWorkout(r.Context(), UserID(r.Context()), id, req)
	if err != nil {
		respondSaveError(w, err, "workout")
		return
	}
	respondJSON(w, http.StatusOK, wk)
}

// HandleGetWorkout returns one session.
func (s *Server) HandleGetWorkout(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "workoutID"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid workout id", "invalid_id")
		return
	}
	wk, err := s.workoutByID(r.Context(), UserID(r.Context()), id)
	if errors.Is(err, sql.ErrNoRows) {
		respondError(w, http.StatusNotFound, "workout not found", "not_found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load workout", "internal")
		return
	}
	respondJSON(w, http.StatusOK, wk)
}

// HandleListWorkouts lists sessions newest first. Query: from, to, kind, limit.
func (s *Server) HandleListWorkouts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	out, err := s.listWorkouts(r.Context(), UserID(r.Context()), q.Get("from"), q.Get("to"), q.Get("kind"), limit)
	if err != nil {
		respondSaveError(w, err, "workouts")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

func (s *Server) listWorkouts(ctx context.Context, userID int64, from, to, kind string, limit int) ([]Workout, error) {
	if to == "" {
		to = s.today(ctx)
	}
	if from == "" {
		from = dates.AddDays(to, -90)
	}
	if !dates.Valid(from) || !dates.Valid(to) {
		return nil, badInput("invalid_date", "from and to must be YYYY-MM-DD")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT ` + workoutColumns + ` FROM workouts WHERE user_id = ? AND on_date BETWEEN ? AND ?`
	args := []any{userID, from, to}
	if kind = strings.ToLower(strings.TrimSpace(kind)); kind != "" {
		query += ` AND kind = ?`
		args = append(args, kind)
	}
	query += ` ORDER BY on_date DESC, started_at DESC, id DESC LIMIT ?`
	return s.queryWorkouts(ctx, userID, query, append(args, limit)...)
}

// HandleDeleteWorkout removes a session.
func (s *Server) HandleDeleteWorkout(w http.ResponseWriter, r *http.Request) {
	s.deleteOwned(w, r, "workouts", chi.URLParam(r, "workoutID"))
}

const workoutColumns = `id, on_date, kind, activity, minutes, kcal, distance_km, avg_hr, notes, started_at, source, COALESCE((SELECT json_group_array(source) FROM (SELECT DISTINCT source FROM workout_sources WHERE workout_id=workouts.id ORDER BY source)),'[]'), COALESCE((SELECT external_id FROM workout_sources WHERE workout_id=workouts.id AND source='strava' LIMIT 1), '')`

func (s *Server) workoutByID(ctx context.Context, userID, id int64) (Workout, error) {
	list, err := s.queryWorkouts(ctx, userID, `SELECT `+workoutColumns+` FROM workouts WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return Workout{}, err
	}
	if len(list) == 0 {
		return Workout{}, sql.ErrNoRows
	}
	return list[0], nil
}

func (s *Server) workoutsForDate(ctx context.Context, userID int64, date string) ([]Workout, error) {
	return s.queryWorkouts(ctx, userID, `SELECT `+workoutColumns+` FROM workouts WHERE user_id = ? AND on_date = ? ORDER BY started_at, id`, userID, date)
}

// queryWorkouts runs a workout query and attaches each session's exercises.
func (s *Server) queryWorkouts(ctx context.Context, userID int64, query string, args ...any) ([]Workout, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := []Workout{}
	for rows.Next() {
		wk, err := scanWorkout(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, wk)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	return out, s.attachExercises(ctx, userID, out)
}

func scanWorkout(row scanner) (Workout, error) {
	var (
		wk         Workout
		kcal, dist sql.NullFloat64
		hr         sql.NullInt64
		started    sql.NullString
		sources    string
	)
	err := row.Scan(&wk.ID, &wk.Date, &wk.Kind, &wk.Activity, &wk.Minutes, &kcal, &dist, &hr, &wk.Notes, &started, &wk.Source, &sources, &wk.StravaID)
	_ = json.Unmarshal([]byte(sources), &wk.Sources)
	if len(wk.Sources) == 0 {
		wk.Sources = []string{wk.Source}
	}
	wk.Kcal, wk.DistanceKm, wk.AvgHR, wk.StartedAt = floatPtr(kcal), floatPtr(dist), intPtr(hr), strPtr(started)
	wk.Exercises = []WorkoutExercise{}
	return wk, err
}

func (s *Server) attachExercises(ctx context.Context, userID int64, list []Workout) error {
	if len(list) == 0 {
		return nil
	}
	index := map[int64]int{}
	ids := make([]any, 0, len(list)+1)
	ids = append(ids, userID)
	for i, wk := range list {
		index[wk.ID] = i
		ids = append(ids, wk.ID)
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(list)), ",")
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.workout_id, e.exercise_slug, e.name, e.notes, st.reps, st.weight, st.unit, st.seconds
		  FROM workout_exercises e LEFT JOIN workout_sets st ON st.exercise_id = e.id
		 WHERE e.user_id = ? AND e.workout_id IN (`+marks+`)
		 ORDER BY e.workout_id, e.position, st.position`, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()
	var lastEx int64
	for rows.Next() {
		var (
			exID, workoutID int64
			ex              WorkoutExercise
			reps, seconds   sql.NullInt64
			weight          sql.NullFloat64
			unit            sql.NullString
		)
		if err := rows.Scan(&exID, &workoutID, &ex.Slug, &ex.Name, &ex.Notes, &reps, &weight, &unit, &seconds); err != nil {
			return err
		}
		wk := &list[index[workoutID]]
		if exID != lastEx {
			ex.ID, ex.Sets = exID, []WorkoutSet{}
			wk.Exercises = append(wk.Exercises, ex)
			lastEx = exID
		}
		if unit.Valid {
			cur := &wk.Exercises[len(wk.Exercises)-1]
			cur.Sets = append(cur.Sets, WorkoutSet{Reps: intPtr(reps), Weight: floatPtr(weight), Unit: unit.String, Seconds: intPtr(seconds)})
		}
	}
	return rows.Err()
}

// ExerciseHistory is one past session's work on a movement.
type ExerciseHistory struct {
	WorkoutID int64        `json:"workout_id"`
	Date      string       `json:"date"`
	Name      string       `json:"name"`
	Notes     string       `json:"notes"`
	Sets      []WorkoutSet `json:"sets"`
}

// HandleExerciseHistory lists recent sessions of one exercise, newest first,
// so the app can show "last time" and progress. ?name= finds a custom one.
func (s *Server) HandleExerciseHistory(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := s.exerciseHistory(r.Context(), UserID(r.Context()), chi.URLParam(r, "slug"), r.URL.Query().Get("name"), limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load history", "internal")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

func (s *Server) exerciseHistory(ctx context.Context, userID int64, slug, name string, limit int) ([]ExerciseHistory, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "custom" || slug == "-" {
		slug = ""
	}
	where, arg := `e.exercise_slug = ?`, any(slug)
	if slug == "" {
		where, arg = `e.exercise_slug = '' AND lower(e.name) = lower(?)`, strings.TrimSpace(name)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.workout_id, w.on_date, e.name, e.notes FROM workout_exercises e JOIN workouts w ON w.id = e.workout_id
		 WHERE e.user_id = ? AND `+where+` ORDER BY w.on_date DESC, w.started_at DESC, e.id DESC LIMIT ?`, userID, arg, limit)
	if err != nil {
		return nil, err
	}
	out := []ExerciseHistory{}
	var exIDs []int64
	for rows.Next() {
		var h ExerciseHistory
		var exID int64
		if err := rows.Scan(&exID, &h.WorkoutID, &h.Date, &h.Name, &h.Notes); err != nil {
			rows.Close()
			return nil, err
		}
		h.Sets = []WorkoutSet{}
		out = append(out, h)
		exIDs = append(exIDs, exID)
	}
	rows.Close()
	for i, exID := range exIDs {
		sets, err := s.db.QueryContext(ctx, `SELECT reps, weight, unit, seconds FROM workout_sets WHERE exercise_id = ? ORDER BY position`, exID)
		if err != nil {
			return nil, err
		}
		for sets.Next() {
			var reps, seconds sql.NullInt64
			var weight sql.NullFloat64
			var st WorkoutSet
			if err := sets.Scan(&reps, &weight, &st.Unit, &seconds); err != nil {
				sets.Close()
				return nil, err
			}
			st.Reps, st.Weight, st.Seconds = intPtr(reps), floatPtr(weight), intPtr(seconds)
			out[i].Sets = append(out[i].Sets, st)
		}
		sets.Close()
	}
	return out, nil
}

// HandleListExercises serves the catalog. Query: q, category, muscle.
func (s *Server) HandleListExercises(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	respondJSON(w, http.StatusOK, map[string]any{
		"categories": training.Categories,
		"muscles":    training.Muscles,
		"exercises":  training.Search(q.Get("q"), q.Get("category"), q.Get("muscle")),
	})
}

// WeekTraining is exercise minutes for the Monday-to-Sunday week holding a
// date, against the weekly goal, with the fixed sessions from the schedule.
type WeekTraining struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Minutes  int    `json:"minutes"`
	Goal     *int   `json:"goal"`
	Sessions int    `json:"sessions"`
	Days     []struct {
		Date    string `json:"date"`
		Minutes int    `json:"minutes"`
	} `json:"days"`
	// Planned lists the schedule's fixed sessions this week and whether each
	// has been done.
	Planned []PlannedSession `json:"planned"`
	// ScheduledLeft is the minutes of fixed sessions still to come.
	ScheduledLeft int `json:"scheduled_left"`
	// FreeformNeeded is what the goal still needs beyond the fixed sessions
	// left: walks, rides, anything that fits.
	FreeformNeeded int `json:"freeform_needed"`
}

func weekStart(date string) string {
	t, err := dates.Parse(date)
	if err != nil {
		return date
	}
	offset := (int(t.Weekday()) + 6) % 7 // Monday = 0
	return dates.AddDays(date, -offset)
}

func (s *Server) weekTraining(ctx context.Context, userID int64, date string) (WeekTraining, error) {
	wt := WeekTraining{From: weekStart(date)}
	wt.To = dates.AddDays(wt.From, 6)
	minutesBy := map[string]int{}
	workoutsBy := map[string][]weekWorkout{}
	rows, err := s.db.QueryContext(ctx, `SELECT id, on_date, kind, minutes FROM workouts WHERE user_id = ? AND on_date BETWEEN ? AND ? ORDER BY on_date, started_at, id`, userID, wt.From, wt.To)
	if err != nil {
		return wt, err
	}
	for rows.Next() {
		var w weekWorkout
		var d string
		var mins int
		if err := rows.Scan(&w.id, &d, &w.kind, &mins); err != nil {
			rows.Close()
			return wt, err
		}
		minutesBy[d] += mins
		workoutsBy[d] = append(workoutsBy[d], w)
		wt.Minutes += mins
		wt.Sessions++
	}
	rows.Close()
	days := dates.Range(wt.From, wt.To)
	for _, d := range days {
		wt.Days = append(wt.Days, struct {
			Date    string `json:"date"`
			Minutes int    `json:"minutes"`
		}{d, minutesBy[d]})
	}
	sc, err := s.trainingSchedule(ctx, userID)
	if err != nil {
		return wt, err
	}
	wt.Planned = plannedSessions(sc, days, s.today(ctx), workoutsBy)
	for _, p := range wt.Planned {
		if p.Status == "today" || p.Status == "upcoming" {
			wt.ScheduledLeft += p.Minutes
		}
	}
	g, err := s.goals(ctx, userID)
	wt.Goal = g.WeeklyWorkoutMinutes
	if wt.Goal != nil {
		wt.FreeformNeeded = max(0, *wt.Goal-wt.Minutes-wt.ScheduledLeft)
	}
	return wt, err
}

// HandleWeekTraining reports the week's exercise minutes. ?date= picks the week.
func (s *Server) HandleWeekTraining(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if date == "" {
		date = s.today(r.Context())
	}
	if !dates.Valid(date) {
		respondError(w, http.StatusBadRequest, "date must be YYYY-MM-DD", "invalid_date")
		return
	}
	wt, err := s.weekTraining(r.Context(), UserID(r.Context()), date)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load the week", "internal")
		return
	}
	respondJSON(w, http.StatusOK, wt)
}

// describeWorkout renders a session as plain text, for Strava's description
// and for agents: one line per exercise, consecutive identical sets folded.
func describeWorkout(wk Workout) string {
	var lines []string
	for _, ex := range wk.Exercises {
		line := ex.Name
		if parts := describeSets(ex.Sets); parts != "" {
			line += ": " + parts
		}
		if ex.Notes != "" {
			line += " (" + ex.Notes + ")"
		}
		lines = append(lines, line)
	}
	out := strings.Join(lines, "\n")
	if wk.Notes != "" {
		if out != "" {
			out += "\n\n"
		}
		out += wk.Notes
	}
	return out
}

func describeSets(sets []WorkoutSet) string {
	var parts []string
	for i := 0; i < len(sets); {
		j := i + 1
		for j < len(sets) && describeSet(sets[j]) == describeSet(sets[i]) {
			j++
		}
		part := describeSet(sets[i])
		if n := j - i; n > 1 {
			part = fmt.Sprintf("%d×(%s)", n, part)
		}
		if part != "" {
			parts = append(parts, part)
		}
		i = j
	}
	return strings.Join(parts, ", ")
}

func describeSet(st WorkoutSet) string {
	var bits []string
	if st.Weight != nil && *st.Weight > 0 {
		bits = append(bits, fmtFloat(*st.Weight, 1)+" "+st.Unit)
	}
	if st.Reps != nil {
		bits = append(bits, strconv.Itoa(*st.Reps)+" reps")
	}
	if st.Seconds != nil {
		bits = append(bits, strconv.Itoa(*st.Seconds)+" s")
	}
	if len(bits) == 2 && st.Reps != nil && st.Weight != nil {
		return fmtFloat(*st.Weight, 1) + " " + st.Unit + " × " + strconv.Itoa(*st.Reps)
	}
	return strings.Join(bits, " · ")
}
