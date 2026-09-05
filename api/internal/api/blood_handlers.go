package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	ai "github.com/anchoo2kewl/go-ai"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/biswas-dev/lifeai/api/internal/blood"
	"github.com/biswas-dev/lifeai/api/internal/dates"
)

// BloodReport is a lab report with its markers.
type BloodReport struct {
	ID          int64          `json:"id"`
	TakenOn     string         `json:"taken_on"`
	Lab         string         `json:"lab"`
	OrderedBy   string         `json:"ordered_by"`
	Notes       string         `json:"notes"`
	FileName    string         `json:"file_name,omitempty"`
	FileBytes   int64          `json:"file_bytes,omitempty"`
	HasFile     bool           `json:"has_file"`
	FileURL     string         `json:"file_url,omitempty"`
	ParseStatus string         `json:"parse_status"`
	ParseError  string         `json:"parse_error,omitempty"`
	Markers     []BloodMarker  `json:"markers"`
	Counts      map[string]int `json:"counts"`
	CreatedAt   string         `json:"created_at"`
}

// BloodMarker is one value in a report.
type BloodMarker struct {
	ID        int64    `json:"id"`
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	Category  string   `json:"category"`
	Value     *float64 `json:"value"`
	ValueText string   `json:"value_text"`
	Unit      string   `json:"unit"`
	RefLow    *float64 `json:"ref_low"`
	RefHigh   *float64 `json:"ref_high"`
	RefText   string   `json:"ref_text"`
	Flag      string   `json:"flag"`
	Watch     bool     `json:"watch"`
}

// HandleListBloodReports lists reports newest first, without markers.
func (s *Server) HandleListBloodReports(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, taken_on, lab, ordered_by, notes, file_name, file_bytes, file_path <> '', parse_status, parse_error, created_at
		  FROM blood_reports WHERE user_id = ? ORDER BY taken_on DESC, id DESC`, UserID(ctx))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not list reports", "internal")
		return
	}
	defer rows.Close()
	out := []BloodReport{}
	for rows.Next() {
		var rep BloodReport
		if err := rows.Scan(&rep.ID, &rep.TakenOn, &rep.Lab, &rep.OrderedBy, &rep.Notes, &rep.FileName, &rep.FileBytes, &rep.HasFile, &rep.ParseStatus, &rep.ParseError, &rep.CreatedAt); err != nil {
			respondError(w, http.StatusInternalServerError, "could not read reports", "internal")
			return
		}
		rep.Markers = []BloodMarker{}
		rep.Counts = s.markerCounts(ctx, rep.ID)
		if rep.HasFile {
			rep.FileURL = fmt.Sprintf("/api/blood/reports/%d/file", rep.ID)
		}
		out = append(out, rep)
	}
	respondJSON(w, http.StatusOK, out)
}

// HandleGetBloodReport returns one report with its markers.
func (s *Server) HandleGetBloodReport(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "reportID"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid report id", "invalid_id")
		return
	}
	rep, err := s.bloodReportByID(r.Context(), UserID(r.Context()), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "report not found", "not_found")
		return
	}
	respondJSON(w, http.StatusOK, rep)
}

// HandleServeBloodFile streams the original document.
func (s *Server) HandleServeBloodFile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "reportID"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid report id", "invalid_id")
		return
	}
	var path, name string
	if err := s.db.QueryRowContext(r.Context(), `SELECT file_path, file_name FROM blood_reports WHERE id = ? AND user_id = ?`, id, UserID(r.Context())).Scan(&path, &name); err != nil || path == "" {
		respondError(w, http.StatusNotFound, "no file for that report", "not_found")
		return
	}
	f, err := s.photos.Open(path)
	if err != nil {
		respondError(w, http.StatusNotFound, "file missing", "not_found")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="`+strings.ReplaceAll(name, `"`, "")+`"`)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = io.Copy(w, f)
}

// bloodMarkerPayload is a marker as submitted by a client.
type bloodMarkerPayload struct {
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	Value     *float64 `json:"value"`
	ValueText string   `json:"value_text"`
	Unit      string   `json:"unit"`
	RefLow    *float64 `json:"ref_low"`
	RefHigh   *float64 `json:"ref_high"`
	RefText   string   `json:"ref_text"`
	Flag      string   `json:"flag"`
}

type bloodReportRequest struct {
	TakenOn   string               `json:"taken_on"`
	Lab       string               `json:"lab"`
	OrderedBy string               `json:"ordered_by"`
	Notes     string               `json:"notes"`
	Markers   []bloodMarkerPayload `json:"markers"`
	// Text, when given, is parsed the same way an uploaded file is.
	Text string `json:"text"`
}

// HandleCreateBloodReport creates a report from typed markers or pasted text.
func (s *Server) HandleCreateBloodReport(w http.ResponseWriter, r *http.Request) {
	var req bloodReportRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	userID := UserID(ctx)
	markers := toMarkers(req.Markers)
	status := "manual"
	parsed := blood.Report{}
	if len(markers) == 0 && strings.TrimSpace(req.Text) != "" {
		parsed = s.parseBloodText(ctx, userID, req.Text)
		markers = parsed.Markers
		status = "parsed"
	}
	takenOn := strings.TrimSpace(req.TakenOn)
	if takenOn == "" {
		takenOn = parsed.TakenOn
	}
	if takenOn == "" {
		takenOn = s.today(ctx)
	}
	if !dates.Valid(takenOn) {
		respondError(w, http.StatusBadRequest, "taken_on must be YYYY-MM-DD", "invalid_date")
		return
	}
	lab := strings.TrimSpace(req.Lab)
	if lab == "" {
		lab = parsed.Lab
	}
	orderedBy := strings.TrimSpace(req.OrderedBy)
	if orderedBy == "" {
		orderedBy = parsed.OrderedBy
	}
	id, err := s.insertBloodReport(ctx, userID, takenOn, lab, orderedBy, strings.TrimSpace(req.Notes), "", "", 0, req.Text, status, markers)
	if err != nil {
		s.log.Error("create blood report", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "could not save report", "internal")
		return
	}
	rep, _ := s.bloodReportByID(ctx, userID, id)
	respondJSON(w, http.StatusCreated, rep)
}

// HandleUploadBloodReport accepts a PDF (or text file), stores it, extracts
// the text and parses the markers.
func (s *Server) HandleUploadBloodReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := UserID(ctx)
	r.Body = http.MaxBytesReader(w, r.Body, 30<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		respondError(w, http.StatusRequestEntityTooLarge, "file is too large (30 MB limit)", "too_large")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("file")
	if err != nil {
		respondError(w, http.StatusBadRequest, "a file field is required", "missing_file")
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 30<<20))
	if err != nil {
		respondError(w, http.StatusBadRequest, "could not read file", "bad_file")
		return
	}
	name := strings.TrimSpace(header.Filename)
	if name == "" {
		name = "report.pdf"
	}

	var text string
	isPDF := bytes.HasPrefix(raw, []byte("%PDF"))
	if isPDF {
		text, err = pdfText(ctx, raw)
		if err != nil {
			s.log.Warn("pdf text extraction failed", zap.Error(err))
		}
	} else {
		text = string(raw)
	}

	filePath := ""
	if isPDF {
		saved, err := s.photos.SaveDocument(raw, userID, "blood", name, 30<<20)
		if err != nil {
			s.log.Error("store report file", zap.Error(err))
			respondError(w, http.StatusInternalServerError, "could not store the file", "internal")
			return
		}
		filePath = saved.RelPath
	}

	status, parseErr := "parsed", ""
	parsed := blood.Report{}
	if strings.TrimSpace(text) == "" {
		status, parseErr = "failed", "no text could be read from the file; add the markers by hand"
	} else {
		parsed = s.parseBloodText(ctx, userID, text)
		if len(parsed.Markers) == 0 {
			status, parseErr = "failed", "no markers recognised; add them by hand or paste the text"
		}
	}
	takenOn := strings.TrimSpace(r.FormValue("taken_on"))
	if takenOn == "" {
		takenOn = parsed.TakenOn
	}
	if takenOn == "" {
		takenOn = s.today(ctx)
	}
	if !dates.Valid(takenOn) {
		takenOn = s.today(ctx)
	}
	id, err := s.insertBloodReport(ctx, userID, takenOn, parsed.Lab, parsed.OrderedBy, strings.TrimSpace(r.FormValue("notes")),
		filePath, name, int64(len(raw)), text, status, parsed.Markers)
	if err != nil {
		s.log.Error("insert blood report", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "could not save report", "internal")
		return
	}
	if parseErr != "" {
		_, _ = s.db.ExecContext(ctx, `UPDATE blood_reports SET parse_error = ? WHERE id = ?`, parseErr, id)
	}
	rep, _ := s.bloodReportByID(ctx, userID, id)
	respondJSON(w, http.StatusCreated, rep)
}

type updateBloodReportRequest struct {
	TakenOn   *string               `json:"taken_on"`
	Lab       *string               `json:"lab"`
	OrderedBy *string               `json:"ordered_by"`
	Notes     *string               `json:"notes"`
	Markers   *[]bloodMarkerPayload `json:"markers"`
}

// HandleUpdateBloodReport edits a report; sending markers replaces them.
func (s *Server) HandleUpdateBloodReport(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "reportID"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid report id", "invalid_id")
		return
	}
	var req updateBloodReportRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	userID := UserID(ctx)
	if _, err := s.bloodReportByID(ctx, userID, id); err != nil {
		respondError(w, http.StatusNotFound, "report not found", "not_found")
		return
	}
	set := func(col string, v any) bool {
		if _, err := s.db.ExecContext(ctx, `UPDATE blood_reports SET `+col+` = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, v, id); err != nil {
			respondError(w, http.StatusInternalServerError, "could not update report", "internal")
			return false
		}
		return true
	}
	if req.TakenOn != nil {
		if !dates.Valid(*req.TakenOn) {
			respondError(w, http.StatusBadRequest, "taken_on must be YYYY-MM-DD", "invalid_date")
			return
		}
		if !set("taken_on", *req.TakenOn) {
			return
		}
	}
	if req.Lab != nil && !set("lab", strings.TrimSpace(*req.Lab)) {
		return
	}
	if req.OrderedBy != nil && !set("ordered_by", strings.TrimSpace(*req.OrderedBy)) {
		return
	}
	if req.Notes != nil && !set("notes", strings.TrimSpace(*req.Notes)) {
		return
	}
	if req.Markers != nil {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "could not update report", "internal")
			return
		}
		defer tx.Rollback() //nolint:errcheck
		if _, err := tx.ExecContext(ctx, `DELETE FROM blood_markers WHERE report_id = ?`, id); err != nil {
			respondError(w, http.StatusInternalServerError, "could not update report", "internal")
			return
		}
		if err := insertMarkers(ctx, tx, id, toMarkers(*req.Markers)); err != nil {
			respondError(w, http.StatusInternalServerError, "could not update report", "internal")
			return
		}
		if _, err := tx.ExecContext(ctx, `UPDATE blood_reports SET parse_status = 'manual', parse_error = '', updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id); err != nil {
			respondError(w, http.StatusInternalServerError, "could not update report", "internal")
			return
		}
		if err := tx.Commit(); err != nil {
			respondError(w, http.StatusInternalServerError, "could not update report", "internal")
			return
		}
	}
	rep, _ := s.bloodReportByID(ctx, userID, id)
	respondJSON(w, http.StatusOK, rep)
}

// HandleDeleteBloodReport removes a report and its file.
func (s *Server) HandleDeleteBloodReport(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "reportID"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid report id", "invalid_id")
		return
	}
	ctx := r.Context()
	var path string
	if err := s.db.QueryRowContext(ctx, `SELECT file_path FROM blood_reports WHERE id = ? AND user_id = ?`, id, UserID(ctx)).Scan(&path); err != nil {
		respondError(w, http.StatusNotFound, "report not found", "not_found")
		return
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM blood_reports WHERE id = ? AND user_id = ?`, id, UserID(ctx)); err != nil {
		respondError(w, http.StatusInternalServerError, "could not delete report", "internal")
		return
	}
	if path != "" {
		s.photos.Remove(path)
	}
	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// MarkerSeries is one marker across every report.
type MarkerSeries struct {
	Code          string            `json:"code"`
	Name          string            `json:"name"`
	Category      string            `json:"category"`
	Unit          string            `json:"unit"`
	LowerIsBetter bool              `json:"lower_is_better"`
	Watch         bool              `json:"watch"`
	RefLow        *float64          `json:"ref_low"`
	RefHigh       *float64          `json:"ref_high"`
	Points        []MarkerPoint     `json:"points"`
	Latest        *MarkerPoint      `json:"latest,omitempty"`
	Change        *float64          `json:"change,omitempty"`
	Flag          string            `json:"flag"`
	Extra         map[string]string `json:"-"`
}

// MarkerPoint is one reading.
type MarkerPoint struct {
	Date     string  `json:"date"`
	Value    float64 `json:"value"`
	Flag     string  `json:"flag"`
	ReportID int64   `json:"report_id"`
}

// HandleMarkerSeries returns every marker's history, for the charts.
func (s *Server) HandleMarkerSeries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	series, err := s.markerSeries(ctx, UserID(ctx))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load markers", "internal")
		return
	}
	respondJSON(w, http.StatusOK, series)
}

func (s *Server) markerSeries(ctx context.Context, userID int64) ([]MarkerSeries, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.code, m.name, m.unit, m.value, m.ref_low, m.ref_high, m.flag, r.taken_on, r.id
		  FROM blood_markers m JOIN blood_reports r ON r.id = m.report_id
		 WHERE r.user_id = ? AND m.code <> '' AND m.value IS NOT NULL
		 ORDER BY r.taken_on ASC, r.id ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byCode := map[string]*MarkerSeries{}
	var order []string
	for rows.Next() {
		var (
			code, name, unit, flag, date string
			value                        float64
			low, high                    sql.NullFloat64
			reportID                     int64
		)
		if err := rows.Scan(&code, &name, &unit, &value, &low, &high, &flag, &date, &reportID); err != nil {
			return nil, err
		}
		seriesKey := code + "|" + unit
		ms, ok := byCode[seriesKey]
		if !ok {
			ms = &MarkerSeries{Code: code, Name: name, Unit: unit, Points: []MarkerPoint{}}
			if d, ok := blood.Lookup(code); ok {
				ms.Name, ms.Category, ms.LowerIsBetter, ms.Watch = d.Name, d.Category, d.LowerIsBetter, d.Watch
			}
			byCode[seriesKey] = ms
			order = append(order, seriesKey)
		}
		ms.Points = append(ms.Points, MarkerPoint{Date: date, Value: value, Flag: flag, ReportID: reportID})
		// The latest report's range and unit win.
		ms.RefLow, ms.RefHigh, ms.Unit, ms.Flag = floatPtr(low), floatPtr(high), unit, flag
	}
	out := make([]MarkerSeries, 0, len(order))
	// Definitions order first, then anything unknown.
	seen := map[string]bool{}
	for _, d := range blood.Definitions {
		for _, key := range order {
			if ms := byCode[key]; ms.Code == d.Code {
				finishSeries(ms)
				out = append(out, *ms)
				seen[key] = true
			}
		}
	}

	for _, code := range order {
		if !seen[code] {
			finishSeries(byCode[code])
			out = append(out, *byCode[code])
		}
	}
	return out, nil
}

func finishSeries(ms *MarkerSeries) {
	if len(ms.Points) == 0 {
		return
	}
	last := ms.Points[len(ms.Points)-1]
	ms.Latest = &last
	if len(ms.Points) > 1 {
		c := last.Value - ms.Points[0].Value
		ms.Change = &c
	}
}

// ---- internals ----

func toMarkers(in []bloodMarkerPayload) []blood.Marker {
	out := make([]blood.Marker, 0, len(in))
	for _, p := range in {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		m := blood.Marker{Code: strings.TrimSpace(p.Code), Name: name, Value: p.Value, ValueText: strings.TrimSpace(p.ValueText),
			Unit: strings.TrimSpace(p.Unit), RefLow: p.RefLow, RefHigh: p.RefHigh, RefText: strings.TrimSpace(p.RefText), Flag: strings.TrimSpace(p.Flag)}
		if m.Code == "" {
			m.Code = blood.Canonical(name, m.Unit)
		}
		if _, ok := blood.Lookup(m.Code); !ok {
			m.Code = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(m.Code, " ", "_"), "-", "_"))
		}
		if m.ValueText == "" && m.Value != nil {
			m.ValueText = strings.TrimSpace(strconv.FormatFloat(*m.Value, 'f', -1, 64) + " " + m.Unit)
		}
		if m.RefText == "" && (m.RefLow != nil || m.RefHigh != nil) {
			switch {
			case m.RefLow != nil && m.RefHigh != nil:
				m.RefText = fmt.Sprintf("%g - %g %s", *m.RefLow, *m.RefHigh, m.Unit)
			case m.RefHigh != nil:
				m.RefText = fmt.Sprintf("< %g %s", *m.RefHigh, m.Unit)
			default:
				m.RefText = fmt.Sprintf(">= %g %s", *m.RefLow, m.Unit)
			}
		}
		if m.Flag == "" {
			m.Flag = blood.Flag(m.Value, m.RefLow, m.RefHigh)
		}
		out = append(out, m)
	}
	return out
}

func (s *Server) insertBloodReport(ctx context.Context, userID int64, takenOn, lab, orderedBy, notes, filePath, fileName string, fileBytes int64, rawText, status string, markers []blood.Marker) (int64, error) {
	if len(rawText) > 400000 {
		rawText = rawText[:400000]
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	res, err := tx.ExecContext(ctx, `
		INSERT INTO blood_reports (user_id, taken_on, lab, ordered_by, notes, file_path, file_name, file_bytes, raw_text, parse_status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, userID, takenOn, lab, orderedBy, notes, filePath, fileName, fileBytes, rawText, status)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if err := insertMarkers(ctx, tx, id, markers); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func insertMarkers(ctx context.Context, tx *sql.Tx, reportID int64, markers []blood.Marker) error {
	for i, m := range markers {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO blood_markers (report_id, code, name, value, value_text, unit, ref_low, ref_high, ref_text, flag, sort_order)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			reportID, m.Code, m.Name, nullFloat(m.Value), m.ValueText, m.Unit, nullFloat(m.RefLow), nullFloat(m.RefHigh), m.RefText, m.Flag, i); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) markerCounts(ctx context.Context, reportID int64) map[string]int {
	counts := map[string]int{"total": 0, "normal": 0, "abnormal": 0}
	rows, err := s.db.QueryContext(ctx, `SELECT flag, COUNT(*) FROM blood_markers WHERE report_id = ? GROUP BY flag`, reportID)
	if err != nil {
		return counts
	}
	defer rows.Close()
	for rows.Next() {
		var flag string
		var n int
		if rows.Scan(&flag, &n) == nil {
			counts["total"] += n
			switch flag {
			case "normal", "negative":
				counts["normal"] += n
			case "high", "low", "abnormal", "positive":
				counts["abnormal"] += n
			}
		}
	}
	return counts
}

func (s *Server) bloodReportByID(ctx context.Context, userID, id int64) (BloodReport, error) {
	var rep BloodReport
	err := s.db.QueryRowContext(ctx, `
		SELECT id, taken_on, lab, ordered_by, notes, file_name, file_bytes, file_path <> '', parse_status, parse_error, created_at
		  FROM blood_reports WHERE id = ? AND user_id = ?`, id, userID).
		Scan(&rep.ID, &rep.TakenOn, &rep.Lab, &rep.OrderedBy, &rep.Notes, &rep.FileName, &rep.FileBytes, &rep.HasFile, &rep.ParseStatus, &rep.ParseError, &rep.CreatedAt)
	if err != nil {
		return rep, err
	}
	if rep.HasFile {
		rep.FileURL = fmt.Sprintf("/api/blood/reports/%d/file", rep.ID)
	}
	rep.Markers = []BloodMarker{}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, code, name, value, value_text, unit, ref_low, ref_high, ref_text, flag
		  FROM blood_markers WHERE report_id = ? ORDER BY sort_order, id`, id)
	if err != nil {
		return rep, err
	}
	defer rows.Close()
	for rows.Next() {
		var m BloodMarker
		var value, low, high sql.NullFloat64
		if err := rows.Scan(&m.ID, &m.Code, &m.Name, &value, &m.ValueText, &m.Unit, &low, &high, &m.RefText, &m.Flag); err != nil {
			return rep, err
		}
		m.Value, m.RefLow, m.RefHigh = floatPtr(value), floatPtr(low), floatPtr(high)
		if d, ok := blood.Lookup(m.Code); ok {
			m.Category, m.Watch = d.Category, d.Watch
		}
		rep.Markers = append(rep.Markers, m)
	}
	rep.Counts = s.markerCounts(ctx, id)
	return rep, nil
}

// pdfText extracts text with pdftotext (poppler), which the container ships.
func pdfText(ctx context.Context, raw []byte) (string, error) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		return "", errors.New("pdftotext is not installed")
	}
	tmp, err := os.CreateTemp("", "lifeai-report-*.pdf")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return "", err
	}
	tmp.Close()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "pdftotext", "-layout", tmp.Name(), "-").Output()
	if err != nil {
		return "", fmt.Errorf("pdftotext: %w", err)
	}
	return string(out), nil
}

const bloodExtractPrompt = `You read the text of a blood test report and list every measured marker.

Reply with a single JSON object and nothing else:
{"taken_on":"YYYY-MM-DD or empty","lab":"lab name or empty","markers":[{"name":"test name as printed","value":number or null,"value_text":"result as printed","unit":"unit","ref_low":number or null,"ref_high":number or null,"ref_text":"reference range as printed","flag":"normal|high|low|abnormal|see_details|"}]}

Rules:
- One entry per test. Include tests with no numeric result (write value null and keep value_text).
- Copy numbers exactly; never convert units.
- ref_low / ref_high come from the reference range: "< 5.2" means ref_high 5.2 and no ref_low; "129 - 165" means both.
- flag is what the report says; if it says nothing, derive it from the range.
- Never invent a test that is not in the text.`

// parseBloodText runs the deterministic parser and, when AI is available,
// the model too; whichever found more markers wins. The parser is trusted
// on its own layout, so a Dynacare report never pays for a model call.
func (s *Server) parseBloodText(ctx context.Context, userID int64, text string) blood.Report {
	parsed := blood.Parse(text)
	if len(parsed.Markers) >= 10 || !s.ai.Enabled() || s.checkAIQuota(ctx, userID) != nil {
		return parsed
	}
	body := text
	if len(body) > 40000 {
		body = body[:40000]
	}
	callCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	resp, err := s.aiChain().Complete(callCtx, ai.Request{
		System:    bloodExtractPrompt,
		Messages:  []ai.Message{ai.UserText(body)},
		MaxTokens: 8000,
		JSON:      true,
	})
	meta := aiMetaFrom(resp, hashFor(nil, body))
	if err != nil {
		s.recordAIRun(ctx, userID, "blood_extract", meta, err)
		return parsed
	}
	var out struct {
		TakenOn string               `json:"taken_on"`
		Lab     string               `json:"lab"`
		Markers []bloodMarkerPayload `json:"markers"`
	}
	if err := ai.ExtractJSON(resp.Text, &out); err != nil {
		s.recordAIRun(ctx, userID, "blood_extract", meta, err)
		return parsed
	}
	b, _ := json.Marshal(out)
	meta.ResultJSON = string(b)
	s.recordAIRun(ctx, userID, "blood_extract", meta, nil)
	markers := toMarkers(out.Markers)
	if len(markers) <= len(parsed.Markers) {
		return parsed
	}
	rep := blood.Report{TakenOn: parsed.TakenOn, Lab: parsed.Lab, OrderedBy: parsed.OrderedBy, Markers: markers}
	if rep.TakenOn == "" && dates.Valid(out.TakenOn) {
		rep.TakenOn = out.TakenOn
	}
	if rep.Lab == "" {
		rep.Lab = strings.TrimSpace(out.Lab)
	}
	return rep
}
