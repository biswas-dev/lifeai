package api

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/biswas-dev/lifeai/api/internal/dates"
)

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
