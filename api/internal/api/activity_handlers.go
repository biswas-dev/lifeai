package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/biswas-dev/lifeai/api/internal/dates"
)

// Workout is a logged training session.
type Workout struct {
	ID         int64    `json:"id"`
	Date       string   `json:"date"`
	Kind       string   `json:"kind"`
	Activity   string   `json:"activity"`
	Minutes    int      `json:"minutes"`
	Kcal       *float64 `json:"kcal"`
	DistanceKm *float64 `json:"distance_km"`
	AvgHR      *int     `json:"avg_hr"`
	Notes      string   `json:"notes"`
	StartedAt  *string  `json:"started_at"`
	Source     string   `json:"source"`
	Sources    []string `json:"sources"`
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

type workoutRequest struct {
	Date       string   `json:"date"`
	Kind       string   `json:"kind"`
	Activity   string   `json:"activity"`
	Minutes    int      `json:"minutes"`
	Kcal       *float64 `json:"kcal"`
	DistanceKm *float64 `json:"distance_km"`
	AvgHR      *int     `json:"avg_hr"`
	Notes      string   `json:"notes"`
	StartedAt  *string  `json:"started_at"`
}

// HandleCreateWorkout logs a session.
func (s *Server) HandleCreateWorkout(w http.ResponseWriter, r *http.Request) {
	var req workoutRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	userID := UserID(ctx)
	date := strings.TrimSpace(req.Date)
	if date == "" {
		date = s.today(ctx)
	}
	if !dates.Valid(date) {
		respondError(w, http.StatusBadRequest, "date must be YYYY-MM-DD", "invalid_date")
		return
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "" {
		kind = "other"
	}
	if !validWorkoutKind(kind) {
		respondError(w, http.StatusBadRequest, "unknown workout kind", "invalid_kind")
		return
	}
	if req.Minutes <= 0 || req.Minutes > 24*60 {
		respondError(w, http.StatusBadRequest, "minutes must be between 1 and 1440", "invalid_minutes")
		return
	}
	var started any
	if req.StartedAt != nil && *req.StartedAt != "" {
		t, err := time.Parse(time.RFC3339, *req.StartedAt)
		if err != nil {
			respondError(w, http.StatusBadRequest, "started_at must be RFC3339", "invalid_time")
			return
		}
		started = t.UTC()
	}
	if err := s.ensureDay(ctx, userID, date); err != nil {
		respondError(w, http.StatusInternalServerError, "could not save workout", "internal")
		return
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO workouts (user_id, on_date, kind, activity, minutes, kcal, distance_km, avg_hr, notes, started_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, date, kind, strings.TrimSpace(req.Activity), req.Minutes, nullFloat(req.Kcal), nullFloat(req.DistanceKm),
		nullInt(req.AvgHR), strings.TrimSpace(req.Notes), started)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not save workout", "internal")
		return
	}
	id, _ := res.LastInsertId()
	wk, err := s.workoutByID(ctx, userID, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load workout", "internal")
		return
	}
	respondJSON(w, http.StatusCreated, wk)
}

// HandleDeleteWorkout removes a session.
func (s *Server) HandleDeleteWorkout(w http.ResponseWriter, r *http.Request) {
	s.deleteOwned(w, r, "workouts", chi.URLParam(r, "workoutID"))
}

const workoutColumns = `id, on_date, kind, activity, minutes, kcal, distance_km, avg_hr, notes, started_at, source, COALESCE((SELECT json_group_array(source) FROM (SELECT DISTINCT source FROM workout_sources WHERE workout_id=workouts.id ORDER BY source)),'[]')`

func (s *Server) workoutByID(ctx context.Context, userID, id int64) (Workout, error) {
	return scanWorkout(s.db.QueryRowContext(ctx, `SELECT `+workoutColumns+` FROM workouts WHERE id = ? AND user_id = ?`, id, userID))
}

func (s *Server) workoutsForDate(ctx context.Context, userID int64, date string) ([]Workout, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+workoutColumns+` FROM workouts WHERE user_id = ? AND on_date = ? ORDER BY started_at, id`, userID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Workout{}
	for rows.Next() {
		wk, err := scanWorkout(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, wk)
	}
	return out, rows.Err()
}

func scanWorkout(row scanner) (Workout, error) {
	var (
		wk         Workout
		kcal, dist sql.NullFloat64
		hr         sql.NullInt64
		started    sql.NullString
		sources    string
	)
	err := row.Scan(&wk.ID, &wk.Date, &wk.Kind, &wk.Activity, &wk.Minutes, &kcal, &dist, &hr, &wk.Notes, &started, &wk.Source, &sources)
	_ = json.Unmarshal([]byte(sources), &wk.Sources)
	if len(wk.Sources) == 0 {
		wk.Sources = []string{wk.Source}
	}
	wk.Kcal, wk.DistanceKm, wk.AvgHR, wk.StartedAt = floatPtr(kcal), floatPtr(dist), intPtr(hr), strPtr(started)
	return wk, err
}

// Meditation is a sitting.
type Meditation struct {
	ID        int64   `json:"id"`
	Date      string  `json:"date"`
	Minutes   int     `json:"minutes"`
	Style     string  `json:"style"`
	Notes     string  `json:"notes"`
	StartedAt *string `json:"started_at"`
	Source    string  `json:"source"`
}

// MeditationStyles are the shapes a sitting can take.
var MeditationStyles = []string{"guided", "unguided", "breathwork", "body_scan", "walking", "other"}

type meditationRequest struct {
	Date    string `json:"date"`
	Minutes int    `json:"minutes"`
	Style   string `json:"style"`
	Notes   string `json:"notes"`
}

// HandleCreateMeditation logs a sitting.
func (s *Server) HandleCreateMeditation(w http.ResponseWriter, r *http.Request) {
	var req meditationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	userID := UserID(ctx)
	date := strings.TrimSpace(req.Date)
	if date == "" {
		date = s.today(ctx)
	}
	if !dates.Valid(date) {
		respondError(w, http.StatusBadRequest, "date must be YYYY-MM-DD", "invalid_date")
		return
	}
	if req.Minutes <= 0 || req.Minutes > 24*60 {
		respondError(w, http.StatusBadRequest, "minutes must be between 1 and 1440", "invalid_minutes")
		return
	}
	style := strings.ToLower(strings.TrimSpace(req.Style))
	valid := false
	for _, v := range MeditationStyles {
		if v == style {
			valid = true
		}
	}
	if !valid {
		style = "guided"
	}
	if err := s.ensureDay(ctx, userID, date); err != nil {
		respondError(w, http.StatusInternalServerError, "could not save meditation", "internal")
		return
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO meditations (user_id, on_date, minutes, style, notes, started_at) VALUES (?, ?, ?, ?, ?, ?)`,
		userID, date, req.Minutes, style, strings.TrimSpace(req.Notes), time.Now().UTC())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not save meditation", "internal")
		return
	}
	id, _ := res.LastInsertId()
	m, err := scanMeditation(s.db.QueryRowContext(ctx, `SELECT `+meditationColumns+` FROM meditations WHERE id = ?`, id))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load meditation", "internal")
		return
	}
	respondJSON(w, http.StatusCreated, m)
}

// HandleDeleteMeditation removes a sitting.
func (s *Server) HandleDeleteMeditation(w http.ResponseWriter, r *http.Request) {
	s.deleteOwned(w, r, "meditations", chi.URLParam(r, "meditationID"))
}

const meditationColumns = `id, on_date, minutes, style, notes, started_at, source`

func (s *Server) meditationsForDate(ctx context.Context, userID int64, date string) ([]Meditation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+meditationColumns+` FROM meditations WHERE user_id = ? AND on_date = ? ORDER BY started_at, id`, userID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Meditation{}
	for rows.Next() {
		m, err := scanMeditation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func scanMeditation(row scanner) (Meditation, error) {
	var m Meditation
	var started sql.NullString
	err := row.Scan(&m.ID, &m.Date, &m.Minutes, &m.Style, &m.Notes, &started, &m.Source)
	m.StartedAt = strPtr(started)
	return m, err
}

// JournalEntry is a piece of writing against a date.
type JournalEntry struct {
	ID        int64  `json:"id"`
	Date      string `json:"date"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Source    string `json:"source"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	// Snippet is set on search results.
	Snippet string `json:"snippet,omitempty"`
}

type journalRequest struct {
	Date  string `json:"date"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// HandleListJournal lists entries newest first, optionally searched.
func (s *Server) HandleListJournal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit := 100
	query := `SELECT ` + journalColumns + ` FROM journal_entries WHERE user_id = ?`
	args := []any{UserID(ctx)}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q != "" {
		query += ` AND (lower(title) LIKE ? OR lower(body) LIKE ?)`
		like := "%" + strings.ToLower(q) + "%"
		args = append(args, like, like)
	}
	query += ` ORDER BY on_date DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not list journal", "internal")
		return
	}
	defer rows.Close()
	out := []JournalEntry{}
	for rows.Next() {
		e, err := scanJournal(rows)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "could not read journal", "internal")
			return
		}
		if q != "" {
			e.Snippet = snippet(e.Body, q)
		}
		out = append(out, e)
	}
	respondJSON(w, http.StatusOK, out)
}

// HandleCreateJournal writes an entry.
func (s *Server) HandleCreateJournal(w http.ResponseWriter, r *http.Request) {
	var req journalRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	userID := UserID(ctx)
	date := strings.TrimSpace(req.Date)
	if date == "" {
		date = s.today(ctx)
	}
	if !dates.Valid(date) {
		respondError(w, http.StatusBadRequest, "date must be YYYY-MM-DD", "invalid_date")
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		respondError(w, http.StatusBadRequest, "an entry needs some text", "empty_entry")
		return
	}
	if err := s.ensureDay(ctx, userID, date); err != nil {
		respondError(w, http.StatusInternalServerError, "could not save entry", "internal")
		return
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO journal_entries (user_id, on_date, title, body) VALUES (?, ?, ?, ?)`,
		userID, date, strings.TrimSpace(req.Title), body)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not save entry", "internal")
		return
	}
	id, _ := res.LastInsertId()
	e, err := scanJournal(s.db.QueryRowContext(ctx, `SELECT `+journalColumns+` FROM journal_entries WHERE id = ?`, id))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load entry", "internal")
		return
	}
	respondJSON(w, http.StatusCreated, e)
}

// HandleUpdateJournal edits an entry.
func (s *Server) HandleUpdateJournal(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "entryID"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid entry id", "invalid_id")
		return
	}
	var req struct {
		Title *string `json:"title"`
		Body  *string `json:"body"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	userID := UserID(ctx)
	if req.Title != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE journal_entries SET title = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ?`,
			strings.TrimSpace(*req.Title), id, userID); err != nil {
			respondError(w, http.StatusInternalServerError, "could not update entry", "internal")
			return
		}
	}
	if req.Body != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE journal_entries SET body = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ?`,
			strings.TrimSpace(*req.Body), id, userID); err != nil {
			respondError(w, http.StatusInternalServerError, "could not update entry", "internal")
			return
		}
	}
	e, err := scanJournal(s.db.QueryRowContext(ctx, `SELECT `+journalColumns+` FROM journal_entries WHERE id = ? AND user_id = ?`, id, userID))
	if err != nil {
		respondError(w, http.StatusNotFound, "entry not found", "not_found")
		return
	}
	respondJSON(w, http.StatusOK, e)
}

// HandleDeleteJournal removes an entry.
func (s *Server) HandleDeleteJournal(w http.ResponseWriter, r *http.Request) {
	s.deleteOwned(w, r, "journal_entries", chi.URLParam(r, "entryID"))
}

const journalColumns = `id, on_date, title, body, source, created_at, updated_at`

func (s *Server) journalForDate(ctx context.Context, userID int64, date string) ([]JournalEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+journalColumns+` FROM journal_entries WHERE user_id = ? AND on_date = ? ORDER BY id`, userID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JournalEntry{}
	for rows.Next() {
		e, err := scanJournal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanJournal(row scanner) (JournalEntry, error) {
	var e JournalEntry
	err := row.Scan(&e.ID, &e.Date, &e.Title, &e.Body, &e.Source, &e.CreatedAt, &e.UpdatedAt)
	return e, err
}

// snippet returns the text around the first match of q, for search results.
func snippet(body, q string) string {
	lower := strings.ToLower(body)
	i := strings.Index(lower, strings.ToLower(q))
	if i < 0 {
		if len(body) > 160 {
			return body[:160] + "…"
		}
		return body
	}
	start := i - 60
	if start < 0 {
		start = 0
	}
	end := i + len(q) + 100
	if end > len(body) {
		end = len(body)
	}
	out := body[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(body) {
		out += "…"
	}
	return out
}

// deleteOwned deletes a row from table by id, scoped to the caller.
func (s *Server) deleteOwned(w http.ResponseWriter, r *http.Request, table, rawID string) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid id", "invalid_id")
		return
	}
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM `+table+` WHERE id = ? AND user_id = ?`, id, UserID(r.Context()))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not delete", "internal")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondError(w, http.StatusNotFound, "not found", "not_found")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
