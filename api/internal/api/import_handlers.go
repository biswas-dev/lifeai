package api

import (
	"context"
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

// parseDBTime reads the timestamp forms SQLite hands back.
func parseDBTime(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999999 -0700 MST", "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05-07:00", "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05", "2006-01-02T15:04:05Z"} {
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
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`UPDATE days SET %s = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ? AND on_date = ? AND instr(','||manual_fields||',',?)=0`, metric), b.value, userID, date, ","+metric+","); err != nil {
			return err
		}
	}
	return nil
}

// markManual records fields a person typed, so imports leave them alone.
func (s *Server) markManual(ctx context.Context, userID int64, date string, fields []string) error {
	seen := map[string]bool{}
	parts := []string{"manual_fields"}
	args := []any{}
	for _, field := range fields {
		if seen[field] {
			continue
		}
		seen[field] = true
		parts = append(parts, "CASE WHEN instr(','||manual_fields||',',?)>0 THEN '' ELSE ? END")
		args = append(args, ","+field+",", ","+field)
	}
	args = append(args, userID, date)
	_, err := s.db.ExecContext(ctx, `UPDATE days SET manual_fields=trim(`+strings.Join(parts, "||")+`,',') WHERE user_id=? AND on_date=?`, args...)
	return err
}

func nullableStartedAt(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}
