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

// Recipe is a reusable meal in the library.
type Recipe struct {
	ID             int64    `json:"id"`
	Name           string   `json:"name"`
	Summary        string   `json:"summary"`
	Minutes        int      `json:"minutes"`
	Servings       int      `json:"servings"`
	KcalPerServing float64  `json:"kcal_per_serving"`
	ProteinG       float64  `json:"protein_g"`
	CarbsG         float64  `json:"carbs_g"`
	FatG           float64  `json:"fat_g"`
	Ingredients    []string `json:"ingredients"`
	Steps          []string `json:"steps"`
	Tags           []string `json:"tags"`
	Favourite      bool     `json:"favourite"`
	PhotoID        *int64   `json:"photo_id"`
	PhotoURL       string   `json:"photo_url,omitempty"`
	Source         string   `json:"source"`
	TimesCooked    int      `json:"times_cooked"`
	LastCookedAt   *string  `json:"last_cooked_at"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

type recipeRequest struct {
	Name           string   `json:"name"`
	Summary        string   `json:"summary"`
	Minutes        int      `json:"minutes"`
	Servings       int      `json:"servings"`
	KcalPerServing float64  `json:"kcal_per_serving"`
	ProteinG       float64  `json:"protein_g"`
	CarbsG         float64  `json:"carbs_g"`
	FatG           float64  `json:"fat_g"`
	Ingredients    []string `json:"ingredients"`
	Steps          []string `json:"steps"`
	Tags           []string `json:"tags"`
	Favourite      *bool    `json:"favourite"`
	PhotoID        *int64   `json:"photo_id"`
	Source         string   `json:"source"`
}

func (req *recipeRequest) validate() (string, string) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return "a recipe needs a name", "invalid_name"
	}
	if len(req.Name) > 200 {
		req.Name = req.Name[:200]
	}
	if req.Servings <= 0 {
		req.Servings = 1
	}
	if req.Minutes < 0 {
		req.Minutes = 0
	}
	if req.KcalPerServing < 0 || req.KcalPerServing > 20000 {
		return "calories per serving out of range", "invalid_kcal"
	}
	req.Ingredients = cleanLines(req.Ingredients, 60)
	req.Steps = cleanLines(req.Steps, 40)
	req.Tags = cleanTags(req.Tags)
	if req.Source != "ai" && req.Source != "import" {
		req.Source = "manual"
	}
	return "", ""
}

func cleanLines(in []string, max int) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
			if len(out) == max {
				break
			}
		}
	}
	return out
}

func cleanTags(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		t = strings.ReplaceAll(t, ",", "")
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) == 12 {
			break
		}
	}
	return out
}

// HandleListRecipes lists the library, optionally searched or filtered.
func (s *Server) HandleListRecipes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := `SELECT ` + recipeColumns + ` FROM recipes WHERE user_id = ?`
	args := []any{UserID(ctx)}
	if q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); q != "" {
		query += ` AND (lower(name) LIKE ? OR lower(summary) LIKE ? OR lower(ingredients_json) LIKE ? OR tags LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like, like)
	}
	if tag := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("tag"))); tag != "" {
		query += ` AND (',' || tags || ',') LIKE ?`
		args = append(args, "%,"+tag+",%")
	}
	if isTrue(r.URL.Query().Get("favourite")) {
		query += ` AND favourite = 1`
	}
	switch r.URL.Query().Get("sort") {
	case "cooked":
		query += ` ORDER BY times_cooked DESC, updated_at DESC`
	case "name":
		query += ` ORDER BY lower(name)`
	case "kcal":
		query += ` ORDER BY kcal_per_serving ASC`
	default:
		query += ` ORDER BY favourite DESC, updated_at DESC`
	}
	query += ` LIMIT 500`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not list recipes", "internal")
		return
	}
	defer rows.Close()
	out := []Recipe{}
	for rows.Next() {
		rc, err := scanRecipe(rows)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "could not read recipes", "internal")
			return
		}
		out = append(out, rc)
	}
	respondJSON(w, http.StatusOK, out)
}

// HandleGetRecipe returns one recipe.
func (s *Server) HandleGetRecipe(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "recipeID"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid recipe id", "invalid_id")
		return
	}
	rc, err := s.recipeByID(r.Context(), UserID(r.Context()), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "recipe not found", "not_found")
		return
	}
	respondJSON(w, http.StatusOK, rc)
}

// HandleCreateRecipe adds a recipe to the library.
func (s *Server) HandleCreateRecipe(w http.ResponseWriter, r *http.Request) {
	var req recipeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if msg, code := req.validate(); msg != "" {
		respondError(w, http.StatusBadRequest, msg, code)
		return
	}
	ctx := r.Context()
	userID := UserID(ctx)
	ing, _ := json.Marshal(req.Ingredients)
	steps, _ := json.Marshal(req.Steps)
	fav := 0
	if req.Favourite != nil && *req.Favourite {
		fav = 1
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO recipes (user_id, name, summary, minutes, servings, kcal_per_serving, protein_g, carbs_g, fat_g,
		                     ingredients_json, steps_json, tags, favourite, photo_id, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, req.Name, strings.TrimSpace(req.Summary), req.Minutes, req.Servings, req.KcalPerServing,
		req.ProteinG, req.CarbsG, req.FatG, string(ing), string(steps), strings.Join(req.Tags, ","), fav, req.PhotoID, req.Source)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not save recipe", "internal")
		return
	}
	id, _ := res.LastInsertId()
	rc, err := s.recipeByID(ctx, userID, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load recipe", "internal")
		return
	}
	respondJSON(w, http.StatusCreated, rc)
}

// HandleUpdateRecipe replaces a recipe's editable fields. A partial body is
// merged over the stored recipe, so a favourite toggle is a one-field PATCH.
func (s *Server) HandleUpdateRecipe(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "recipeID"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid recipe id", "invalid_id")
		return
	}
	ctx := r.Context()
	userID := UserID(ctx)
	existing, err := s.recipeByID(ctx, userID, id)
	if err != nil {
		respondError(w, http.StatusNotFound, "recipe not found", "not_found")
		return
	}
	// Decode into a map first so we know which fields were sent.
	var raw map[string]json.RawMessage
	if !decodeJSON(w, r, &raw) {
		return
	}
	req := recipeRequest{
		Name: existing.Name, Summary: existing.Summary, Minutes: existing.Minutes, Servings: existing.Servings,
		KcalPerServing: existing.KcalPerServing, ProteinG: existing.ProteinG, CarbsG: existing.CarbsG, FatG: existing.FatG,
		Ingredients: existing.Ingredients, Steps: existing.Steps, Tags: existing.Tags, Favourite: &existing.Favourite,
		PhotoID: existing.PhotoID, Source: existing.Source,
	}
	body, _ := json.Marshal(raw)
	if err := json.Unmarshal(body, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", "invalid_body")
		return
	}
	if _, sent := raw["source"]; !sent {
		req.Source = existing.Source
	}
	if msg, code := req.validate(); msg != "" {
		respondError(w, http.StatusBadRequest, msg, code)
		return
	}
	ing, _ := json.Marshal(req.Ingredients)
	steps, _ := json.Marshal(req.Steps)
	fav := 0
	if req.Favourite != nil && *req.Favourite {
		fav = 1
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE recipes SET name = ?, summary = ?, minutes = ?, servings = ?, kcal_per_serving = ?, protein_g = ?, carbs_g = ?, fat_g = ?,
		       ingredients_json = ?, steps_json = ?, tags = ?, favourite = ?, photo_id = ?, source = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND user_id = ?`,
		req.Name, strings.TrimSpace(req.Summary), req.Minutes, req.Servings, req.KcalPerServing, req.ProteinG, req.CarbsG, req.FatG,
		string(ing), string(steps), strings.Join(req.Tags, ","), fav, req.PhotoID, req.Source, id, userID); err != nil {
		respondError(w, http.StatusInternalServerError, "could not update recipe", "internal")
		return
	}
	rc, err := s.recipeByID(ctx, userID, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load recipe", "internal")
		return
	}
	respondJSON(w, http.StatusOK, rc)
}

// HandleDeleteRecipe removes a recipe. Meals cooked from it keep their
// numbers and lose only the link.
func (s *Server) HandleDeleteRecipe(w http.ResponseWriter, r *http.Request) {
	s.deleteOwned(w, r, "recipes", chi.URLParam(r, "recipeID"))
}

type cookRequest struct {
	Date     string  `json:"date"`
	Slot     string  `json:"slot"`
	Servings float64 `json:"servings"`
	Notes    string  `json:"notes"`
}

// HandleCookRecipe logs a meal from a recipe, scaled by servings eaten.
func (s *Server) HandleCookRecipe(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "recipeID"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid recipe id", "invalid_id")
		return
	}
	var req cookRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	userID := UserID(ctx)
	rc, err := s.recipeByID(ctx, userID, id)
	if err != nil {
		respondError(w, http.StatusNotFound, "recipe not found", "not_found")
		return
	}
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
	servings := req.Servings
	if servings <= 0 {
		servings = 1
	}
	if servings > 20 {
		servings = 20
	}
	if err := s.ensureDay(ctx, userID, date); err != nil {
		respondError(w, http.StatusInternalServerError, "could not log meal", "internal")
		return
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO meals (user_id, on_date, photo_id, recipe_id, name, slot, kcal, protein_g, carbs_g, fat_g, source, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'recipe', ?)`,
		userID, date, rc.PhotoID, rc.ID, rc.Name, slot, rc.KcalPerServing*servings, rc.ProteinG*servings,
		rc.CarbsG*servings, rc.FatG*servings, strings.TrimSpace(req.Notes))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not log meal", "internal")
		return
	}
	mealID, _ := res.LastInsertId()
	_, _ = s.db.ExecContext(ctx, `UPDATE recipes SET times_cooked = times_cooked + 1, last_cooked_at = CURRENT_TIMESTAMP WHERE id = ?`, rc.ID)
	meal, err := s.mealByID(ctx, userID, mealID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load meal", "internal")
		return
	}
	respondJSON(w, http.StatusCreated, meal)
}

const recipeColumns = `id, name, summary, minutes, servings, kcal_per_serving, protein_g, carbs_g, fat_g, ingredients_json, steps_json, tags, favourite, photo_id, source, times_cooked, last_cooked_at, created_at, updated_at`

func (s *Server) recipeByID(ctx context.Context, userID, id int64) (Recipe, error) {
	return scanRecipe(s.db.QueryRowContext(ctx, `SELECT `+recipeColumns+` FROM recipes WHERE id = ? AND user_id = ?`, id, userID))
}

func scanRecipe(row scanner) (Recipe, error) {
	var (
		rc         Recipe
		ing, steps string
		tags       string
		fav        int
		photoID    sql.NullInt64
		lastCooked sql.NullString
	)
	err := row.Scan(&rc.ID, &rc.Name, &rc.Summary, &rc.Minutes, &rc.Servings, &rc.KcalPerServing, &rc.ProteinG, &rc.CarbsG, &rc.FatG,
		&ing, &steps, &tags, &fav, &photoID, &rc.Source, &rc.TimesCooked, &lastCooked, &rc.CreatedAt, &rc.UpdatedAt)
	if err != nil {
		return rc, err
	}
	_ = json.Unmarshal([]byte(ing), &rc.Ingredients)
	_ = json.Unmarshal([]byte(steps), &rc.Steps)
	if rc.Ingredients == nil {
		rc.Ingredients = []string{}
	}
	if rc.Steps == nil {
		rc.Steps = []string{}
	}
	rc.Tags = []string{}
	if tags != "" {
		rc.Tags = strings.Split(tags, ",")
	}
	rc.Favourite = fav == 1
	if photoID.Valid {
		rc.PhotoID = &photoID.Int64
		rc.PhotoURL = "/api/photos/" + strconv.FormatInt(photoID.Int64, 10) + "/file"
	}
	rc.LastCookedAt = strPtr(lastCooked)
	return rc, nil
}
