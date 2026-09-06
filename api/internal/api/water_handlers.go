package api

import (
	"context"
	"database/sql"
	"math"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// WaterEntry records a drink, independently of imported daily totals. Keeping
// fractional millilitres avoids rounding four quarter-gallons differently
// from one gallon. The displayed daily total is rounded only after summing.
type WaterEntry struct {
	ID        int64   `json:"id"`
	AmountMl  float64 `json:"amount_ml"`
	CreatedAt string  `json:"created_at"`
}

func waterFactor(unit string) float64 {
	switch unit {
	case "gal":
		return 3785.411784 // US liquid gallon, also used by 75hard.
	case "oz":
		return 29.5735295625
	case "ml":
		return 1
	case "l":
		return 1000
	}
	return 0
}

func (s *Server) waterForDate(ctx context.Context, userID int64, date string) ([]WaterEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, amount_ml, created_at FROM water_entries WHERE user_id=? AND on_date=? AND deleted_at IS NULL ORDER BY id DESC`, userID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []WaterEntry{}
	for rows.Next() {
		var entry WaterEntry
		if err := rows.Scan(&entry.ID, &entry.AmountMl, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *Server) HandleAddWater(w http.ResponseWriter, r *http.Request) {
	date, ok := s.dateParam(w, r)
	if !ok {
		return
	}
	var req struct {
		Amount    float64 `json:"amount"`
		Unit      string  `json:"unit"`
		RequestID string  `json:"request_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	amount := req.Amount * waterFactor(req.Unit)
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 1 || amount > 20000 {
		respondError(w, 400, "Enter a positive amount in US gallons, fluid ounces, litres or millilitres.", "invalid_water")
		return
	}
	if len(req.RequestID) < 8 || len(req.RequestID) > 100 || strings.TrimSpace(req.RequestID) != req.RequestID {
		respondError(w, 400, "A unique request_id is required for each drink.", "invalid_request_id")
		return
	}
	ctx, id := r.Context(), UserID(r.Context())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		respondError(w, 500, "Could not log water.", "internal")
		return
	}
	defer tx.Rollback()
	// The first write serializes additions from several devices. A repeated
	// request ID is a retry, and must never add a second drink.
	if _, err = tx.ExecContext(ctx, `INSERT INTO days(user_id,on_date) VALUES(?,?) ON CONFLICT(user_id,on_date) DO UPDATE SET updated_at=updated_at`, id, date); err != nil {
		respondError(w, 500, "Could not log water.", "internal")
		return
	}
	var oldDate string
	var oldAmount float64
	err = tx.QueryRowContext(ctx, `SELECT on_date,amount_ml FROM water_entries WHERE user_id=? AND request_id=?`, id, req.RequestID).Scan(&oldDate, &oldAmount)
	if err == nil {
		if oldDate != date || math.Abs(oldAmount-amount) > 0.0001 {
			respondError(w, 409, "That request already recorded a different drink.", "request_conflict")
			return
		}
		if err = tx.Commit(); err != nil {
			respondError(w, 500, "Could not load water.", "internal")
			return
		}
		s.waterResult(w, r, date)
		return
	}
	if err != sql.ErrNoRows {
		respondError(w, 500, "Could not log water.", "internal")
		return
	}
	// Preserve the current total from an import or a manual body-metrics edit
	// as a baseline before the first drink is added.
	if _, err = tx.ExecContext(ctx, `UPDATE days SET water_baseline_ml=COALESCE(water_ml,0) WHERE user_id=? AND on_date=? AND NOT EXISTS(SELECT 1 FROM water_entries WHERE user_id=? AND on_date=? AND deleted_at IS NULL)`, id, date, id, date); err != nil {
		respondError(w, 500, "Could not log water.", "internal")
		return
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO water_entries(user_id,on_date,amount_ml,request_id) VALUES(?,?,?,?)`, id, date, amount, req.RequestID); err != nil {
		respondError(w, 500, "Could not log water.", "internal")
		return
	}
	var total float64
	if err = tx.QueryRowContext(ctx, `SELECT water_baseline_ml+(SELECT COALESCE(SUM(amount_ml),0) FROM water_entries WHERE user_id=? AND on_date=? AND deleted_at IS NULL) FROM days WHERE user_id=? AND on_date=?`, id, date, id, date).Scan(&total); err != nil {
		respondError(w, 500, "Could not log water.", "internal")
		return
	}
	if total > 20000 {
		respondError(w, 400, "The daily total cannot exceed 20,000 ml. Check the amount and unit.", "invalid_water")
		return
	}
	if err = refreshWater(ctx, tx, id, date); err != nil {
		respondError(w, 500, "Could not log water.", "internal")
		return
	}
	if err = tx.Commit(); err != nil {
		respondError(w, 500, "Could not log water.", "internal")
		return
	}
	s.waterResult(w, r, date)
}

func refreshWater(ctx context.Context, tx *sql.Tx, id int64, date string) error {
	_, err := tx.ExecContext(ctx, `UPDATE days SET
        water_ml=CAST(ROUND(MAX(0,water_baseline_ml+(SELECT COALESCE(SUM(amount_ml),0) FROM water_entries WHERE user_id=? AND on_date=? AND deleted_at IS NULL))) AS INTEGER),
        manual_fields=CASE WHEN instr(','||manual_fields||',',',water_ml,')>0 THEN manual_fields ELSE trim(manual_fields||',water_ml',',') END,
        source='manual',updated_at=CURRENT_TIMESTAMP WHERE user_id=? AND on_date=?`, id, date, id, date)
	return err
}

func (s *Server) HandleDeleteWater(w http.ResponseWriter, r *http.Request) {
	date, ok := s.dateParam(w, r)
	if !ok {
		return
	}
	ctx, id := r.Context(), UserID(r.Context())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		respondError(w, 500, "Could not undo drink.", "internal")
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE water_entries SET deleted_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=? AND on_date=? AND deleted_at IS NULL`, chi.URLParam(r, "waterID"), id, date)
	if err != nil {
		respondError(w, 500, "Could not undo drink.", "internal")
		return
	}
	if count, _ := result.RowsAffected(); count == 0 {
		respondError(w, 404, "Drink not found.", "not_found")
		return
	}
	if err = refreshWater(ctx, tx, id, date); err != nil {
		respondError(w, 500, "Could not undo drink.", "internal")
		return
	}
	if err = tx.Commit(); err != nil {
		respondError(w, 500, "Could not undo drink.", "internal")
		return
	}
	s.waterResult(w, r, date)
}

func (s *Server) waterResult(w http.ResponseWriter, r *http.Request, date string) {
	day, err := s.loadDay(r.Context(), UserID(r.Context()), date)
	if err != nil {
		respondError(w, 500, "Water saved. Refresh to see your day.", "internal")
		return
	}
	respondJSON(w, 200, day)
}
