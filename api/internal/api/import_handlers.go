package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/biswas-dev/lifeai/api/internal/dates"
	"github.com/biswas-dev/lifeai/api/internal/health"
)

// ImportSummary is what one import did.
type ImportSummary struct {
	Source          string `json:"source"`
	Samples         int    `json:"samples"`
	Workouts        int    `json:"workouts"`
	WorkoutsSkipped int    `json:"workouts_skipped"`
	DaysTouched     int    `json:"days_touched"`
	Records         int    `json:"records,omitempty"`
	Files           int    `json:"files,omitempty"`
}

// HandleImportApple accepts an Apple Health export.zip (or export.xml).
func (s *Server) HandleImportApple(w http.ResponseWriter, r *http.Request) {
	s.handleImportFile(w, r, "apple")
}

// HandleImportSamsung accepts a Samsung Health data zip.
func (s *Server) HandleImportSamsung(w http.ResponseWriter, r *http.Request) {
	s.handleImportFile(w, r, "samsung")
}

func (s *Server) handleImportFile(w http.ResponseWriter, r *http.Request, source string) {
	ctx := r.Context()
	userID := UserID(ctx)
	const limit = 2 << 30
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	// Large exports go to a temp file rather than memory.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		respondError(w, http.StatusRequestEntityTooLarge, "file is too large", "too_large")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("file")
	if err != nil {
		respondError(w, http.StatusBadRequest, "a file field is required", "missing_file")
		return
	}
	defer file.Close()

	// zip needs a ReaderAt with a size; a multipart file gives one when it
	// spilled to disk, and a small in-memory one is copied out.
	tmp, err := os.CreateTemp("", "lifeai-import-*")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not stage the file", "internal")
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	size, err := io.Copy(tmp, file)
	if err != nil {
		respondError(w, http.StatusBadRequest, "could not read the file", "bad_file")
		return
	}

	var samples []health.Sample
	var workouts []health.WorkoutImport
	sum := ImportSummary{Source: source}
	name := strings.ToLower(header.Filename)
	switch source {
	case "apple":
		var res *health.AppleResult
		if strings.HasSuffix(name, ".xml") {
			if _, err = tmp.Seek(0, io.SeekStart); err == nil {
				res, err = health.ParseAppleXML(tmp)
			}
		} else {
			res, err = health.ParseAppleZip(tmp, size)
		}
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error(), "bad_export")
			return
		}
		samples, workouts, sum.Records = res.Samples, res.Workouts, res.Records
	case "samsung":
		res, err := health.ParseSamsungZip(tmp, size)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error(), "bad_export")
			return
		}
		samples, workouts, sum.Files = res.Samples, res.Workouts, len(res.Files)
	}
	if err := s.applyImport(ctx, userID, source, samples, workouts, &sum); err != nil {
		s.log.Error("apply import", zap.String("source", source), zap.Error(err))
		s.recordImport(ctx, userID, source, header.Filename, sum, err)
		respondError(w, http.StatusInternalServerError, "could not save the import", "internal")
		return
	}
	s.recordImport(ctx, userID, source, header.Filename, sum, nil)
	respondJSON(w, http.StatusOK, sum)
}

// HandleImportWebhook accepts a JSON payload from a phone automation. It is
// meant to be called with an API token, so a shortcut can push every night.
func (s *Server) HandleImportWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := UserID(ctx)
	body, err := io.ReadAll(io.LimitReader(r.Body, 50<<20))
	if err != nil {
		respondError(w, http.StatusBadRequest, "could not read body", "bad_body")
		return
	}
	res, err := health.ParseWebhook(body, "webhook")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), "bad_payload")
		return
	}
	sum := ImportSummary{Source: res.Source}
	if err := s.applyImport(ctx, userID, res.Source, res.Samples, res.Workouts, &sum); err != nil {
		s.recordImport(ctx, userID, res.Source, "webhook", sum, err)
		respondError(w, http.StatusInternalServerError, "could not save the import", "internal")
		return
	}
	s.recordImport(ctx, userID, res.Source, "webhook", sum, nil)
	respondJSON(w, http.StatusOK, sum)
}

// ImportRun is one past import, for the settings screen.
type ImportRun struct {
	ID        int64          `json:"id"`
	Source    string         `json:"source"`
	FileName  string         `json:"file_name"`
	Summary   *ImportSummary `json:"summary"`
	Error     string         `json:"error"`
	CreatedAt string         `json:"created_at"`
}

// HandleListImports lists recent imports.
func (s *Server) HandleListImports(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.db.QueryContext(ctx, `SELECT id, source, file_name, summary, error, created_at FROM import_runs WHERE user_id = ? ORDER BY id DESC LIMIT 30`, UserID(ctx))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not list imports", "internal")
		return
	}
	defer rows.Close()
	out := []ImportRun{}
	for rows.Next() {
		var run ImportRun
		var summary string
		if err := rows.Scan(&run.ID, &run.Source, &run.FileName, &summary, &run.Error, &run.CreatedAt); err != nil {
			continue
		}
		if summary != "" {
			var sum ImportSummary
			if json.Unmarshal([]byte(summary), &sum) == nil {
				run.Summary = &sum
			}
		}
		out = append(out, run)
	}
	respondJSON(w, http.StatusOK, out)
}

func (s *Server) recordImport(ctx context.Context, userID int64, source, fileName string, sum ImportSummary, err error) {
	b, _ := json.Marshal(sum)
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	_, _ = s.db.ExecContext(ctx, `INSERT INTO import_runs (user_id, source, file_name, summary, error) VALUES (?, ?, ?, ?, ?)`,
		userID, source, fileName, string(b), errText)
}

// applyImport writes samples and workouts, then resolves each touched day.
func (s *Server) applyImport(ctx context.Context, userID int64, source string, samples []health.Sample, workouts []health.WorkoutImport, sum *ImportSummary) error {
	touched := map[string]bool{}
	for _, smp := range samples {
		if !dates.Valid(smp.Date) || !health.ValidMetric(smp.Metric) {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO health_samples (user_id, on_date, metric, source, value) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(user_id, on_date, metric, source) DO UPDATE SET value = excluded.value, created_at = CURRENT_TIMESTAMP`,
			userID, smp.Date, smp.Metric, smp.Source, smp.Value); err != nil {
			return err
		}
		touched[smp.Date] = true
		sum.Samples++
	}
	for _, wk := range workouts {
		inserted, err := s.importWorkout(ctx, userID, wk)
		if err != nil {
			return err
		}
		if inserted {
			sum.Workouts++
		} else {
			sum.WorkoutsSkipped++
		}
		touched[wk.Date] = true
	}
	for d := range touched {
		if err := s.ensureDay(ctx, userID, d); err != nil {
			return err
		}
		if err := s.resolveDay(ctx, userID, d); err != nil {
			return err
		}
	}
	sum.DaysTouched = len(touched)
	return nil
}

// importWorkout inserts a session unless the same effort is already there
// from any source. Returns whether a row was inserted.
func (s *Server) importWorkout(ctx context.Context, userID int64, wk health.WorkoutImport) (bool, error) {
	if !dates.Valid(wk.Date) || wk.Minutes <= 0 {
		return false, nil
	}
	// Already imported from this source: refresh the numbers.
	var existingID int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM workouts WHERE user_id = ? AND source = ? AND external_id = ?`, userID, wk.Source, wk.ExternalID).Scan(&existingID)
	if err == nil {
		_, err = s.db.ExecContext(ctx, `UPDATE workouts SET kind = ?, activity = ?, minutes = ?, kcal = COALESCE(?, kcal), distance_km = COALESCE(?, distance_km), avg_hr = COALESCE(?, avg_hr) WHERE id = ?`,
			wk.Kind, wk.Activity, wk.Minutes, nullFloat(wk.Kcal), nullFloat(wk.DistanceKm), nullInt(wk.AvgHR), existingID)
		return false, err
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	// The same effort seen by another device: keep the first, fill gaps.
	rows, err := s.db.QueryContext(ctx, `SELECT id, minutes, started_at FROM workouts WHERE user_id = ? AND on_date IN (?, ?, ?) AND started_at IS NOT NULL`,
		userID, wk.Date, dates.AddDays(wk.Date, -1), dates.AddDays(wk.Date, 1))
	if err != nil {
		return false, err
	}
	dupID := int64(0)
	for rows.Next() {
		var id int64
		var mins int
		var started sql.NullString
		if rows.Scan(&id, &mins, &started) != nil || !started.Valid {
			continue
		}
		st, ok := parseDBTime(started.String)
		if ok && health.SameWorkout(wk.StartedAt, wk.Minutes, st, mins) {
			dupID = id
			break
		}
	}
	rows.Close()
	if dupID != 0 {
		_, err = s.db.ExecContext(ctx, `UPDATE workouts SET kcal = COALESCE(kcal, ?), distance_km = COALESCE(distance_km, ?), avg_hr = COALESCE(avg_hr, ?) WHERE id = ?`,
			nullFloat(wk.Kcal), nullFloat(wk.DistanceKm), nullInt(wk.AvgHR), dupID)
		return false, err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO workouts (user_id, on_date, kind, activity, minutes, kcal, distance_km, avg_hr, notes, started_at, source, external_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, wk.Date, wk.Kind, strings.TrimSpace(wk.Activity), wk.Minutes, nullFloat(wk.Kcal), nullFloat(wk.DistanceKm), nullInt(wk.AvgHR),
		strings.TrimSpace(wk.Notes), nullableStartedAt(wk.StartedAt), wk.Source, wk.ExternalID)
	return err == nil, err
}

// parseDBTime reads the timestamp forms SQLite hands back.
func parseDBTime(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05-07:00", "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05", "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// resolveDay writes the best sample of each metric onto the day, skipping
// any field the person typed by hand.
func (s *Server) resolveDay(ctx context.Context, userID int64, date string) error {
	var manual string
	_ = s.db.QueryRowContext(ctx, `SELECT manual_fields FROM days WHERE user_id = ? AND on_date = ?`, userID, date).Scan(&manual)
	isManual := map[string]bool{}
	for _, f := range strings.Split(manual, ",") {
		if f = strings.TrimSpace(f); f != "" {
			isManual[f] = true
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT metric, source, value FROM health_samples WHERE user_id = ? AND on_date = ?`, userID, date)
	if err != nil {
		return err
	}
	best := map[string]struct {
		rank  int
		value float64
	}{}
	for rows.Next() {
		var metric, source string
		var value float64
		if rows.Scan(&metric, &source, &value) != nil {
			continue
		}
		r := health.Rank(source)
		if cur, ok := best[metric]; !ok || r < cur.rank {
			best[metric] = struct {
				rank  int
				value float64
			}{r, value}
		}
	}
	rows.Close()
	for metric, b := range best {
		if isManual[metric] || !health.ValidMetric(metric) {
			continue
		}
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`UPDATE days SET %s = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ? AND on_date = ?`, metric), b.value, userID, date); err != nil {
			return err
		}
	}
	return nil
}

// markManual records fields a person typed, so imports leave them alone.
func (s *Server) markManual(ctx context.Context, userID int64, date string, fields []string) error {
	var manual string
	_ = s.db.QueryRowContext(ctx, `SELECT manual_fields FROM days WHERE user_id = ? AND on_date = ?`, userID, date).Scan(&manual)
	set := map[string]bool{}
	for _, f := range strings.Split(manual, ",") {
		if f = strings.TrimSpace(f); f != "" {
			set[f] = true
		}
	}
	for _, f := range fields {
		set[f] = true
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE days SET manual_fields = ? WHERE user_id = ? AND on_date = ?`, strings.Join(out, ","), userID, date)
	return err
}

func nullableStartedAt(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}
