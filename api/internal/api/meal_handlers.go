package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/biswas-dev/lifeai/api/internal/dates"
)

// Meal is one eating occasion.
type Meal struct {
	ID       int64      `json:"id"`
	Date     string     `json:"date"`
	PhotoID  *int64     `json:"photo_id"`
	PhotoURL string     `json:"photo_url,omitempty"`
	RecipeID *int64     `json:"recipe_id"`
	Name     string     `json:"name"`
	Slot     string     `json:"slot"`
	Kcal     float64    `json:"kcal"`
	ProteinG float64    `json:"protein_g"`
	CarbsG   float64    `json:"carbs_g"`
	FatG     float64    `json:"fat_g"`
	Source   string     `json:"source"`
	Notes    string     `json:"notes"`
	EatenAt  string     `json:"eaten_at"`
	Items    []MealItem `json:"items"`
	// EstimateStatus is '' for a hand-entered meal, or pending/done/failed
	// while a photo is being estimated in the background.
	EstimateStatus string `json:"estimate_status"`
	EstimateError  string `json:"estimate_error,omitempty"`
}

// MealItem is one component of a meal.
type MealItem struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Qty        float64  `json:"qty"`
	Unit       string   `json:"unit"`
	Kcal       float64  `json:"kcal"`
	ProteinG   float64  `json:"protein_g"`
	CarbsG     float64  `json:"carbs_g"`
	FatG       float64  `json:"fat_g"`
	Confidence *float64 `json:"confidence"`
}

type mealItemPayload struct {
	Name     string  `json:"name"`
	Qty      float64 `json:"qty"`
	Unit     string  `json:"unit"`
	Kcal     float64 `json:"kcal"`
	ProteinG float64 `json:"protein_g"`
	CarbsG   float64 `json:"carbs_g"`
	FatG     float64 `json:"fat_g"`
}

type createMealRequest struct {
	Date     string            `json:"date"`
	PhotoID  *int64            `json:"photo_id"`
	Name     string            `json:"name"`
	Slot     string            `json:"slot"`
	Kcal     float64           `json:"kcal"`
	ProteinG float64           `json:"protein_g"`
	CarbsG   float64           `json:"carbs_g"`
	FatG     float64           `json:"fat_g"`
	Notes    string            `json:"notes"`
	Items    []mealItemPayload `json:"items"`
}

var mealSlots = map[string]bool{"breakfast": true, "lunch": true, "dinner": true, "snack": true}

func validSlot(s string) bool { return mealSlots[s] }

// slotForTime guesses the meal from the clock.
func slotForTime(t time.Time) string {
	switch h := t.Hour(); {
	case h < 5:
		return "snack"
	case h < 11:
		return "breakfast"
	case h < 15:
		return "lunch"
	case h < 17:
		return "snack"
	case h < 22:
		return "dinner"
	default:
		return "snack"
	}
}

// HandleCreateMeal logs a meal by hand, optionally itemised. When items are
// given the totals are summed from them rather than trusted from the client.
func (s *Server) HandleCreateMeal(w http.ResponseWriter, r *http.Request) {
	var req createMealRequest
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
	slot := strings.ToLower(strings.TrimSpace(req.Slot))
	if slot == "" {
		slot = slotForTime(time.Now().In(s.userLocation(ctx)))
	}
	if !validSlot(slot) {
		respondError(w, http.StatusBadRequest, "slot must be breakfast, lunch, dinner or snack", "invalid_slot")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" && len(req.Items) == 0 && req.Kcal == 0 {
		respondError(w, http.StatusBadRequest, "a name, items or calories are required", "empty_meal")
		return
	}
	if req.PhotoID != nil {
		var n int
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM photos WHERE id = ? AND user_id = ?`, *req.PhotoID, userID).Scan(&n)
		if n == 0 {
			respondError(w, http.StatusBadRequest, "photo not found", "invalid_photo")
			return
		}
	}
	kcal, prot, carbs, fat := req.Kcal, req.ProteinG, req.CarbsG, req.FatG
	if len(req.Items) > 0 {
		kcal, prot, carbs, fat = 0, 0, 0, 0
		for _, it := range req.Items {
			kcal += it.Kcal
			prot += it.ProteinG
			carbs += it.CarbsG
			fat += it.FatG
		}
	}
	if kcal < 0 || kcal > 20000 {
		respondError(w, http.StatusBadRequest, "calories out of range", "invalid_kcal")
		return
	}
	if err := s.ensureDay(ctx, userID, date); err != nil {
		respondError(w, http.StatusInternalServerError, "could not save meal", "internal")
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not save meal", "internal")
		return
	}
	defer tx.Rollback() //nolint:errcheck
	res, err := tx.ExecContext(ctx, `
		INSERT INTO meals (user_id, on_date, photo_id, name, slot, kcal, protein_g, carbs_g, fat_g, source, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'manual', ?)`,
		userID, date, req.PhotoID, name, slot, kcal, prot, carbs, fat, strings.TrimSpace(req.Notes))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not save meal", "internal")
		return
	}
	id, _ := res.LastInsertId()
	if err := insertItems(ctx, tx, id, req.Items); err != nil {
		respondError(w, http.StatusInternalServerError, "could not save meal items", "internal")
		return
	}
	if err := tx.Commit(); err != nil {
		respondError(w, http.StatusInternalServerError, "could not save meal", "internal")
		return
	}
	meal, err := s.mealByID(ctx, userID, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load meal", "internal")
		return
	}
	respondJSON(w, http.StatusCreated, meal)
}

type updateMealRequest struct {
	Name     *string            `json:"name"`
	Slot     *string            `json:"slot"`
	Date     *string            `json:"date"`
	Kcal     *float64           `json:"kcal"`
	ProteinG *float64           `json:"protein_g"`
	CarbsG   *float64           `json:"carbs_g"`
	FatG     *float64           `json:"fat_g"`
	Notes    *string            `json:"notes"`
	Items    *[]mealItemPayload `json:"items"`
}

// HandleUpdateMeal edits a meal. Replacing the items recomputes the totals;
// editing a total by hand marks the meal as manual, so an AI estimate a
// person has corrected is no longer presented as an estimate.
func (s *Server) HandleUpdateMeal(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "mealID"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid meal id", "invalid_id")
		return
	}
	var req updateMealRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	userID := UserID(ctx)
	if _, err := s.mealByID(ctx, userID, id); err != nil {
		respondError(w, http.StatusNotFound, "meal not found", "not_found")
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not update meal", "internal")
		return
	}
	defer tx.Rollback() //nolint:errcheck

	set := func(col string, v any) error {
		_, err := tx.ExecContext(ctx, `UPDATE meals SET `+col+` = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, v, id)
		return err
	}
	fail := func() {
		respondError(w, http.StatusInternalServerError, "could not update meal", "internal")
	}
	if req.Name != nil {
		if err := set("name", strings.TrimSpace(*req.Name)); err != nil {
			fail()
			return
		}
	}
	if req.Slot != nil {
		slot := strings.ToLower(strings.TrimSpace(*req.Slot))
		if !validSlot(slot) {
			respondError(w, http.StatusBadRequest, "slot must be breakfast, lunch, dinner or snack", "invalid_slot")
			return
		}
		if err := set("slot", slot); err != nil {
			fail()
			return
		}
	}
	if req.Date != nil {
		if !dates.Valid(*req.Date) {
			respondError(w, http.StatusBadRequest, "date must be YYYY-MM-DD", "invalid_date")
			return
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO days (user_id, on_date) VALUES (?, ?) ON CONFLICT DO NOTHING`, userID, *req.Date); err != nil {
			fail()
			return
		}
		if err := set("on_date", *req.Date); err != nil {
			fail()
			return
		}
	}
	if req.Notes != nil {
		if err := set("notes", strings.TrimSpace(*req.Notes)); err != nil {
			fail()
			return
		}
	}
	if req.Items != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM meal_items WHERE meal_id = ?`, id); err != nil {
			fail()
			return
		}
		if err := insertItems(ctx, tx, id, *req.Items); err != nil {
			fail()
			return
		}
		var kcal, prot, carbs, fat float64
		for _, it := range *req.Items {
			kcal += it.Kcal
			prot += it.ProteinG
			carbs += it.CarbsG
			fat += it.FatG
		}
		if _, err := tx.ExecContext(ctx, `UPDATE meals SET kcal = ?, protein_g = ?, carbs_g = ?, fat_g = ?, source = 'manual', estimate_status = CASE WHEN estimate_status = '' THEN '' ELSE 'done' END WHERE id = ?`,
			kcal, prot, carbs, fat, id); err != nil {
			fail()
			return
		}
	} else if req.Kcal != nil || req.ProteinG != nil || req.CarbsG != nil || req.FatG != nil {
		for col, v := range map[string]*float64{"kcal": req.Kcal, "protein_g": req.ProteinG, "carbs_g": req.CarbsG, "fat_g": req.FatG} {
			if v != nil {
				if *v < 0 || *v > 20000 {
					respondError(w, http.StatusBadRequest, "value out of range", "invalid_value")
					return
				}
				if err := set(col, *v); err != nil {
					fail()
					return
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE meals SET source = 'manual', estimate_status = CASE WHEN estimate_status = '' THEN '' ELSE 'done' END WHERE id = ?`, id); err != nil {
			fail()
			return
		}
	}
	if err := tx.Commit(); err != nil {
		fail()
		return
	}
	meal, err := s.mealByID(ctx, userID, id)
	if err != nil {
		fail()
		return
	}
	respondJSON(w, http.StatusOK, meal)
}

// HandleDeleteMeal removes a meal and its items.
func (s *Server) HandleDeleteMeal(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "mealID"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid meal id", "invalid_id")
		return
	}
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM meals WHERE id = ? AND user_id = ?`, id, UserID(r.Context()))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not delete meal", "internal")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		respondError(w, http.StatusNotFound, "meal not found", "not_found")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// HandleRetryEstimate re-runs a background estimate that failed.
func (s *Server) HandleRetryEstimate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "mealID"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid meal id", "invalid_id")
		return
	}
	ctx := r.Context()
	userID := UserID(ctx)
	if s.food == nil || !s.ai.Enabled() {
		respondError(w, http.StatusServiceUnavailable, "AI estimation is not configured", "ai_disabled")
		return
	}
	var (
		photoID sql.NullInt64
		relPath string
		notes   string
	)
	err = s.db.QueryRowContext(ctx, `
		SELECT m.photo_id, COALESCE(p.rel_path, ''), m.notes FROM meals m
		  LEFT JOIN photos p ON p.id = m.photo_id
		 WHERE m.id = ? AND m.user_id = ?`, id, userID).Scan(&photoID, &relPath, &notes)
	if err != nil {
		respondError(w, http.StatusNotFound, "meal not found", "not_found")
		return
	}
	if !photoID.Valid || relPath == "" {
		respondError(w, http.StatusBadRequest, "that meal has no photo to estimate from", "no_photo")
		return
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE meals SET estimate_status = 'pending', estimate_error = '' WHERE id = ?`, id); err != nil {
		respondError(w, http.StatusInternalServerError, "could not queue estimate", "internal")
		return
	}
	s.food.Enqueue(estimateJob{userID: userID, mealID: id, photoID: photoID.Int64, relPath: relPath, hint: notes})
	respondJSON(w, http.StatusAccepted, map[string]string{"status": "pending"})
}

func insertItems(ctx context.Context, tx *sql.Tx, mealID int64, items []mealItemPayload) error {
	for i, it := range items {
		name := strings.TrimSpace(it.Name)
		if name == "" {
			continue
		}
		unit := strings.TrimSpace(it.Unit)
		if unit == "" {
			unit = "serving"
		}
		qty := it.Qty
		if qty <= 0 {
			qty = 1
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO meal_items (meal_id, name, qty, unit, kcal, protein_g, carbs_g, fat_g, sort_order)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			mealID, name, qty, unit, it.Kcal, it.ProteinG, it.CarbsG, it.FatG, i); err != nil {
			return err
		}
	}
	return nil
}

const mealColumns = `id, on_date, photo_id, recipe_id, name, slot, kcal, protein_g, carbs_g, fat_g, source, notes, eaten_at, estimate_status, estimate_error`

func (s *Server) mealByID(ctx context.Context, userID, id int64) (Meal, error) {
	m, err := scanMeal(s.db.QueryRowContext(ctx, `SELECT `+mealColumns+` FROM meals WHERE id = ? AND user_id = ?`, id, userID))
	if err != nil {
		return m, err
	}
	m.Items, err = s.itemsForMeal(ctx, id)
	return m, err
}

func (s *Server) mealsForDate(ctx context.Context, userID int64, date string) ([]Meal, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+mealColumns+` FROM meals WHERE user_id = ? AND on_date = ? ORDER BY eaten_at ASC, id ASC`, userID, date)
	if err != nil {
		return nil, err
	}
	out := []Meal{}
	for rows.Next() {
		m, err := scanMeal(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, m)
	}
	rows.Close()
	for i := range out {
		if out[i].Items, err = s.itemsForMeal(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Server) itemsForMeal(ctx context.Context, mealID int64) ([]MealItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, qty, unit, kcal, protein_g, carbs_g, fat_g, confidence
		  FROM meal_items WHERE meal_id = ? ORDER BY sort_order, id`, mealID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MealItem{}
	for rows.Next() {
		var it MealItem
		var conf sql.NullFloat64
		if err := rows.Scan(&it.ID, &it.Name, &it.Qty, &it.Unit, &it.Kcal, &it.ProteinG, &it.CarbsG, &it.FatG, &conf); err != nil {
			return nil, err
		}
		it.Confidence = floatPtr(conf)
		out = append(out, it)
	}
	return out, rows.Err()
}

func scanMeal(row scanner) (Meal, error) {
	var m Meal
	var photoID, recipeID sql.NullInt64
	err := row.Scan(&m.ID, &m.Date, &photoID, &recipeID, &m.Name, &m.Slot, &m.Kcal, &m.ProteinG, &m.CarbsG, &m.FatG,
		&m.Source, &m.Notes, &m.EatenAt, &m.EstimateStatus, &m.EstimateError)
	if err != nil {
		return m, err
	}
	if photoID.Valid {
		m.PhotoID = &photoID.Int64
		m.PhotoURL = "/api/photos/" + strconv.FormatInt(photoID.Int64, 10) + "/file"
	}
	if recipeID.Valid {
		m.RecipeID = &recipeID.Int64
	}
	m.Items = []MealItem{}
	return m, nil
}

// queueFoodEstimate creates the meal a food photo represents and hands it to
// the background estimator. The row is written now so the photo appears in
// the day immediately; the numbers arrive behind it.
func (s *Server) queueFoodEstimate(ctx context.Context, userID, photoID int64, date, relPath, slot, hint string) {
	slot = strings.ToLower(strings.TrimSpace(slot))
	if !validSlot(slot) {
		slot = slotForTime(time.Now().In(s.userLocation(ctx)))
	}
	status := "pending"
	if s.food == nil || !s.ai.Enabled() {
		status = ""
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO meals (user_id, on_date, photo_id, name, slot, source, notes, estimate_status)
		VALUES (?, ?, ?, '', ?, 'ai', ?, ?)`, userID, date, photoID, slot, hint, status)
	if err != nil {
		s.log.Error("create meal from photo", zap.Error(err))
		return
	}
	if status == "" {
		return
	}
	mealID, _ := res.LastInsertId()
	s.food.Enqueue(estimateJob{userID: userID, mealID: mealID, photoID: photoID, relPath: relPath, hint: hint})
}

// ErrMealNotFound is returned by lookups that miss.
var ErrMealNotFound = errors.New("meal not found")
