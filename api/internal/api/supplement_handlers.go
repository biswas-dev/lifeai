package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/biswas-dev/lifeai/api/internal/dates"
)

// Supplement is something taken daily, e.g. a greens powder or creatine.
type Supplement struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Product    string `json:"product"`
	Dose       string `json:"dose"`
	TakenWith  string `json:"taken_with"`
	ProductURL string `json:"product_url"`
	Notes      string `json:"notes"`
	StartedOn  string `json:"started_on"`
	Active     bool   `json:"active"`
	Position   int    `json:"position"`
	// ImageURL is set when a product photo is stored; it changes with the photo.
	ImageURL string `json:"image_url,omitempty"`
}

// SupplementLog records one supplement taken on one day.
type SupplementLog struct {
	SupplementID int64  `json:"supplement_id"`
	Date         string `json:"date"`
	TakenWith    string `json:"taken_with"`
	Notes        string `json:"notes"`
}

// SupplementDay is a supplement with whether it was taken on a date, plus
// how consistently it has been taken since it was started.
type SupplementDay struct {
	Supplement
	Taken *SupplementLog `json:"taken"`
	// Streak counts consecutive days taken, ending on the date (or the day
	// before, when the date is not logged yet).
	Streak int `json:"streak"`
	// DaysTaken and DaysSince count from started_on to the date inclusive.
	DaysTaken int      `json:"days_taken"`
	DaysSince int      `json:"days_since"`
	Missed    []string `json:"missed"`
}

type supplementInput struct {
	Name       *string `json:"name,omitempty" jsonschema:"short name, e.g. Creatine"`
	Product    *string `json:"product,omitempty" jsonschema:"exact product, e.g. Nutratology Creatine Monohydrate (micronized, unflavoured)"`
	Dose       *string `json:"dose,omitempty" jsonschema:"amount per day, e.g. 1 scoop (5 g)"`
	TakenWith  *string `json:"taken_with,omitempty" jsonschema:"usual way it is taken, e.g. cold water; the default for each log"`
	ProductURL *string `json:"product_url,omitempty" jsonschema:"where it is bought"`
	Notes      *string `json:"notes,omitempty"`
	StartedOn  *string `json:"started_on,omitempty" jsonschema:"YYYY-MM-DD first day taken"`
	Active     *bool   `json:"active,omitempty" jsonschema:"false to stop showing it each day"`
	Position   *int    `json:"position,omitempty"`
}

type saveSupplementToolInput struct {
	ID int64 `json:"id,omitempty" jsonschema:"omit to add a new supplement"`
	supplementInput
}

type logSupplementToolInput struct {
	ID        int64  `json:"id,omitempty" jsonschema:"supplement id; or give name"`
	Name      string `json:"name,omitempty" jsonschema:"supplement name or product, e.g. creatine"`
	Date      string `json:"date,omitempty" jsonschema:"YYYY-MM-DD, or 'today' (default)"`
	Taken     *bool  `json:"taken,omitempty" jsonschema:"false clears the day"`
	TakenWith string `json:"taken_with,omitempty" jsonschema:"e.g. cold water; default the supplement's usual"`
	Notes     string `json:"notes,omitempty"`
}

type backfillSupplementToolInput struct {
	ID   int64  `json:"id,omitempty" jsonschema:"supplement id; or give name"`
	Name string `json:"name,omitempty" jsonschema:"supplement name or product"`
	SupplementBackfill
}

const supplementColumns = `id, name, product, dose, taken_with, product_url, notes, started_on, active, position, image_type, CAST(strftime('%s', updated_at) AS INTEGER)`

func scanSupplement(row scanner) (Supplement, error) {
	var sp Supplement
	var imageType string
	var version int64
	err := row.Scan(&sp.ID, &sp.Name, &sp.Product, &sp.Dose, &sp.TakenWith, &sp.ProductURL, &sp.Notes, &sp.StartedOn, &sp.Active, &sp.Position, &imageType, &version)
	if imageType != "" {
		sp.ImageURL = fmt.Sprintf("/api/supplements/%d/image?v=%d", sp.ID, version)
	}
	return sp, err
}

func (s *Server) supplements(ctx context.Context, userID int64, activeOnly bool) ([]Supplement, error) {
	q := `SELECT ` + supplementColumns + ` FROM supplements WHERE user_id = ?`
	if activeOnly {
		q += ` AND active = 1`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY position, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Supplement{}
	for rows.Next() {
		sp, err := scanSupplement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

func (s *Server) supplementByID(ctx context.Context, userID, id int64) (Supplement, error) {
	return scanSupplement(s.db.QueryRowContext(ctx, `SELECT `+supplementColumns+` FROM supplements WHERE id = ? AND user_id = ?`, id, userID))
}

// resolveSupplement finds a supplement by id, or by a name that matches its
// name or product without regard to case.
func (s *Server) resolveSupplement(ctx context.Context, userID, id int64, name string) (Supplement, error) {
	if id != 0 {
		sp, err := s.supplementByID(ctx, userID, id)
		if errors.Is(err, sql.ErrNoRows) {
			return sp, badInput("unknown_supplement", "no supplement %d", id)
		}
		return sp, err
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return Supplement{}, badInput("unknown_supplement", "give a supplement id or name")
	}
	all, err := s.supplements(ctx, userID, false)
	if err != nil {
		return Supplement{}, err
	}
	var found []Supplement
	for _, sp := range all {
		if strings.ToLower(sp.Name) == name {
			return sp, nil
		}
		if strings.Contains(strings.ToLower(sp.Name+" "+sp.Product), name) {
			found = append(found, sp)
		}
	}
	if len(found) != 1 {
		return Supplement{}, badInput("unknown_supplement", "%d supplements match %q", len(found), name)
	}
	return found[0], nil
}

func (s *Server) saveSupplement(ctx context.Context, userID, id int64, in supplementInput) (Supplement, error) {
	sp := Supplement{Active: true}
	if id != 0 {
		var err error
		if sp, err = s.supplementByID(ctx, userID, id); err != nil {
			return sp, err
		}
	} else {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM supplements WHERE user_id = ?`, userID).Scan(&n); err != nil {
			return sp, err
		}
		sp.Position = n
	}
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	set(&sp.Name, in.Name)
	set(&sp.Product, in.Product)
	set(&sp.Dose, in.Dose)
	set(&sp.TakenWith, in.TakenWith)
	set(&sp.ProductURL, in.ProductURL)
	set(&sp.Notes, in.Notes)
	set(&sp.StartedOn, in.StartedOn)
	if in.Active != nil {
		sp.Active = *in.Active
	}
	if in.Position != nil {
		sp.Position = *in.Position
	}
	if sp.Name == "" || len(sp.Name) > 80 || len(sp.Product) > 200 || len(sp.Dose) > 80 || len(sp.TakenWith) > 80 || len(sp.Notes) > 2000 {
		return sp, badInput("invalid_supplement", "a supplement needs a name (up to 80 characters); keep product, dose and notes short")
	}
	if sp.StartedOn != "" && !dates.Valid(sp.StartedOn) {
		return sp, badInput("invalid_date", "started_on must be YYYY-MM-DD")
	}
	if sp.ProductURL != "" && !strings.HasPrefix(sp.ProductURL, "https://") && !strings.HasPrefix(sp.ProductURL, "http://") {
		return sp, badInput("invalid_url", "product_url must be a web address")
	}
	if id == 0 {
		res, err := s.db.ExecContext(ctx, `INSERT INTO supplements (user_id, name, product, dose, taken_with, product_url, notes, started_on, active, position) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			userID, sp.Name, sp.Product, sp.Dose, sp.TakenWith, sp.ProductURL, sp.Notes, sp.StartedOn, sp.Active, sp.Position)
		if err != nil {
			return sp, err
		}
		id, _ = res.LastInsertId()
	} else if _, err := s.db.ExecContext(ctx, `UPDATE supplements SET name = ?, product = ?, dose = ?, taken_with = ?, product_url = ?, notes = ?, started_on = ?, active = ?, position = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ?`,
		sp.Name, sp.Product, sp.Dose, sp.TakenWith, sp.ProductURL, sp.Notes, sp.StartedOn, sp.Active, sp.Position, id, userID); err != nil {
		return sp, err
	}
	return s.supplementByID(ctx, userID, id)
}

// takeSupplement records a supplement as taken on a date; taking it again
// updates how it was taken. takenWith defaults to the supplement's usual.
func (s *Server) takeSupplement(ctx context.Context, userID int64, sp Supplement, date, takenWith, notes string) (SupplementLog, error) {
	if !dates.Valid(date) {
		return SupplementLog{}, badInput("invalid_date", "date must be YYYY-MM-DD")
	}
	takenWith, notes = strings.TrimSpace(takenWith), strings.TrimSpace(notes)
	if takenWith == "" {
		takenWith = sp.TakenWith
	}
	if len(takenWith) > 80 || len(notes) > 500 {
		return SupplementLog{}, badInput("invalid_log", "keep taken_with and notes short")
	}
	if err := s.ensureDay(ctx, userID, date); err != nil {
		return SupplementLog{}, err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO supplement_logs (user_id, supplement_id, on_date, taken_with, notes) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, supplement_id, on_date) DO UPDATE SET taken_with = excluded.taken_with, notes = excluded.notes`, userID, sp.ID, date, takenWith, notes)
	return SupplementLog{SupplementID: sp.ID, Date: date, TakenWith: takenWith, Notes: notes}, err
}

func (s *Server) untakeSupplement(ctx context.Context, userID, supplementID int64, date string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM supplement_logs WHERE user_id = ? AND supplement_id = ? AND on_date = ?`, userID, supplementID, date)
	return err
}

// SupplementBackfill marks every day in a range as taken, except skipped days.
type SupplementBackfill struct {
	From      string   `json:"from,omitempty" jsonschema:"YYYY-MM-DD first day; default the supplement's started_on"`
	To        string   `json:"to,omitempty" jsonschema:"YYYY-MM-DD last day; default today"`
	Skip      []string `json:"skip,omitempty" jsonschema:"YYYY-MM-DD days it was not taken"`
	TakenWith string   `json:"taken_with,omitempty" jsonschema:"default the supplement's usual"`
}

func (s *Server) backfillSupplement(ctx context.Context, userID int64, sp Supplement, in SupplementBackfill) (map[string]any, error) {
	if in.From == "" {
		in.From = sp.StartedOn
	}
	if in.To == "" {
		in.To = s.today(ctx)
	}
	if !dates.Valid(in.From) || !dates.Valid(in.To) || in.From > in.To {
		return nil, badInput("invalid_range", "from and to must be YYYY-MM-DD with from on or before to")
	}
	skip := map[string]bool{}
	for _, d := range in.Skip {
		skip[strings.TrimSpace(d)] = true
	}
	logged, skipped := 0, []string{}
	for d, n := in.From, 0; d <= in.To; d, n = dates.AddDays(d, 1), n+1 {
		if n > 366 {
			return nil, badInput("invalid_range", "backfill at most a year at a time")
		}
		if skip[d] {
			if err := s.untakeSupplement(ctx, userID, sp.ID, d); err != nil {
				return nil, err
			}
			skipped = append(skipped, d)
			continue
		}
		if _, err := s.takeSupplement(ctx, userID, sp, d, in.TakenWith, ""); err != nil {
			return nil, err
		}
		logged++
	}
	return map[string]any{"supplement": sp.Name, "from": in.From, "to": in.To, "logged": logged, "skipped": skipped}, nil
}

// supplementDays lists active supplements started by the date, each with its
// log for that date and its consistency since it started.
func (s *Server) supplementDays(ctx context.Context, userID int64, date string) ([]SupplementDay, error) {
	list, err := s.supplements(ctx, userID, false)
	if err != nil {
		return nil, err
	}
	out := []SupplementDay{}
	for _, sp := range list {
		if sp.StartedOn != "" && sp.StartedOn > date {
			continue
		}
		day := SupplementDay{Supplement: sp, Missed: []string{}}
		taken := map[string]SupplementLog{}
		rows, err := s.db.QueryContext(ctx, `SELECT on_date, taken_with, notes FROM supplement_logs WHERE user_id = ? AND supplement_id = ? AND on_date <= ?`, userID, sp.ID, date)
		if err != nil {
			return nil, err
		}
		first := sp.StartedOn
		for rows.Next() {
			l := SupplementLog{SupplementID: sp.ID}
			if err := rows.Scan(&l.Date, &l.TakenWith, &l.Notes); err != nil {
				rows.Close()
				return nil, err
			}
			taken[l.Date] = l
			if first == "" || l.Date < first {
				first = l.Date
			}
		}
		rows.Close()
		if !sp.Active && len(taken) == 0 {
			continue
		}
		if l, ok := taken[date]; ok {
			day.Taken = &l
		}
		if first != "" {
			for d := first; d <= date; d = dates.AddDays(d, 1) {
				day.DaysSince++
				if _, ok := taken[d]; ok {
					day.DaysTaken++
				} else if d != date {
					day.Missed = append(day.Missed, d)
				}
			}
		}
		d := date
		if day.Taken == nil {
			d = dates.AddDays(date, -1)
		}
		for ; ; d = dates.AddDays(d, -1) {
			if _, ok := taken[d]; !ok {
				break
			}
			day.Streak++
		}
		if !sp.Active && day.Taken == nil {
			continue
		}
		out = append(out, day)
	}
	return out, nil
}

func supplementID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "supplementID"), 10, 64)
	return id, err == nil
}

// HandleListSupplements lists supplements; with ?date it adds each one's
// log and consistency for that day.
func (s *Server) HandleListSupplements(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), UserID(r.Context())
	if d := r.URL.Query().Get("date"); d != "" {
		if d == "today" {
			d = s.today(ctx)
		}
		if !dates.Valid(d) {
			respondError(w, http.StatusBadRequest, "date must be YYYY-MM-DD", "invalid_date")
			return
		}
		out, err := s.supplementDays(ctx, uid, d)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "could not load supplements", "internal")
			return
		}
		respondJSON(w, http.StatusOK, out)
		return
	}
	out, err := s.supplements(ctx, uid, false)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load supplements", "internal")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// HandleCreateSupplement adds a supplement.
func (s *Server) HandleCreateSupplement(w http.ResponseWriter, r *http.Request) {
	var in supplementInput
	if !decodeJSON(w, r, &in) {
		return
	}
	sp, err := s.saveSupplement(r.Context(), UserID(r.Context()), 0, in)
	if err != nil {
		respondSaveError(w, err, "supplement")
		return
	}
	respondJSON(w, http.StatusCreated, sp)
}

// HandleUpdateSupplement changes the given fields of a supplement.
func (s *Server) HandleUpdateSupplement(w http.ResponseWriter, r *http.Request) {
	id, ok := supplementID(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "invalid supplement id", "invalid_id")
		return
	}
	var in supplementInput
	if !decodeJSON(w, r, &in) {
		return
	}
	sp, err := s.saveSupplement(r.Context(), UserID(r.Context()), id, in)
	if errors.Is(err, sql.ErrNoRows) {
		respondError(w, http.StatusNotFound, "supplement not found", "not_found")
		return
	}
	if err != nil {
		respondSaveError(w, err, "supplement")
		return
	}
	respondJSON(w, http.StatusOK, sp)
}

// HandleDeleteSupplement removes a supplement and its history.
func (s *Server) HandleDeleteSupplement(w http.ResponseWriter, r *http.Request) {
	s.deleteOwned(w, r, "supplements", chi.URLParam(r, "supplementID"))
}

// HandleTakeSupplement marks a supplement taken on {date}.
func (s *Server) HandleTakeSupplement(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), UserID(r.Context())
	id, ok := supplementID(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "invalid supplement id", "invalid_id")
		return
	}
	date, ok := s.dateParam(w, r)
	if !ok {
		return
	}
	var in struct {
		TakenWith string `json:"taken_with"`
		Notes     string `json:"notes"`
	}
	if r.ContentLength != 0 && !decodeJSON(w, r, &in) {
		return
	}
	sp, err := s.supplementByID(ctx, uid, id)
	if errors.Is(err, sql.ErrNoRows) {
		respondError(w, http.StatusNotFound, "supplement not found", "not_found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load supplement", "internal")
		return
	}
	l, err := s.takeSupplement(ctx, uid, sp, date, in.TakenWith, in.Notes)
	if err != nil {
		respondSaveError(w, err, "supplement")
		return
	}
	respondJSON(w, http.StatusOK, l)
}

// HandleUntakeSupplement clears {date}'s record.
func (s *Server) HandleUntakeSupplement(w http.ResponseWriter, r *http.Request) {
	id, ok := supplementID(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "invalid supplement id", "invalid_id")
		return
	}
	date, ok := s.dateParam(w, r)
	if !ok {
		return
	}
	if err := s.untakeSupplement(r.Context(), UserID(r.Context()), id, date); err != nil {
		respondError(w, http.StatusInternalServerError, "could not update", "internal")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// HandleBackfillSupplement marks a range of days taken, minus skipped days.
func (s *Server) HandleBackfillSupplement(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), UserID(r.Context())
	id, ok := supplementID(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "invalid supplement id", "invalid_id")
		return
	}
	var in SupplementBackfill
	if !decodeJSON(w, r, &in) {
		return
	}
	sp, err := s.supplementByID(ctx, uid, id)
	if errors.Is(err, sql.ErrNoRows) {
		respondError(w, http.StatusNotFound, "supplement not found", "not_found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load supplement", "internal")
		return
	}
	out, err := s.backfillSupplement(ctx, uid, sp, in)
	if err != nil {
		respondSaveError(w, err, "supplement")
		return
	}
	respondJSON(w, http.StatusOK, out)
}

const maxSupplementImage = 2 << 20

var supplementImageTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}

// HandlePutSupplementImage stores a product photo sent as the raw body.
func (s *Server) HandlePutSupplementImage(w http.ResponseWriter, r *http.Request) {
	id, ok := supplementID(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "invalid supplement id", "invalid_id")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSupplementImage))
	if err != nil {
		respondError(w, http.StatusRequestEntityTooLarge, "the photo must be under 2 MB", "too_large")
		return
	}
	kind := http.DetectContentType(body)
	if !supplementImageTypes[kind] {
		respondError(w, http.StatusBadRequest, "use a JPEG, PNG or WebP photo", "invalid_image")
		return
	}
	res, err := s.db.ExecContext(r.Context(), `UPDATE supplements SET image = ?, image_type = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ?`, body, kind, id, UserID(r.Context()))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not save the photo", "internal")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondError(w, http.StatusNotFound, "supplement not found", "not_found")
		return
	}
	sp, _ := s.supplementByID(r.Context(), UserID(r.Context()), id)
	respondJSON(w, http.StatusOK, sp)
}

// HandleDeleteSupplementImage removes the product photo.
func (s *Server) HandleDeleteSupplementImage(w http.ResponseWriter, r *http.Request) {
	id, ok := supplementID(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "invalid supplement id", "invalid_id")
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE supplements SET image = NULL, image_type = '', updated_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ?`, id, UserID(r.Context())); err != nil {
		respondError(w, http.StatusInternalServerError, "could not remove the photo", "internal")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// HandleSupplementImage serves the product photo.
func (s *Server) HandleSupplementImage(w http.ResponseWriter, r *http.Request) {
	id, ok := supplementID(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "invalid supplement id", "invalid_id")
		return
	}
	var body []byte
	var kind string
	err := s.db.QueryRowContext(r.Context(), `SELECT image, image_type FROM supplements WHERE id = ? AND user_id = ? AND image_type <> ''`, id, UserID(r.Context())).Scan(&body, &kind)
	if err != nil {
		respondError(w, http.StatusNotFound, "no photo", "not_found")
		return
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(body)
}
