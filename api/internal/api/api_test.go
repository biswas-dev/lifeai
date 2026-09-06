package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/biswas-dev/lifeai/api/internal/aifeatures"
	"github.com/biswas-dev/lifeai/api/internal/auth"
	"github.com/biswas-dev/lifeai/api/internal/config"
	"github.com/biswas-dev/lifeai/api/internal/db"
	"github.com/biswas-dev/lifeai/api/internal/photo"
	"github.com/biswas-dev/lifeai/api/internal/secret"
)

func newTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	auth.SetBcryptCost(4)
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	photos, err := photo.NewStore(t.TempDir(), 800, 160)
	if err != nil {
		t.Fatal(err)
	}
	cipher, _ := secret.New("test-key")
	cfg := &config.Config{
		Env: "test", AppURL: "http://localhost", JWTSecret: "test-secret", JWTExpiry: time.Hour,
		MaxUploadBytes: 10 << 20, AllowSignup: true, AIDailyLimit: 10, RateLimitPerMin: 1000,
		Hard75AllowedEmails: []string{"anchoo2kewl@gmail.com"},
	}
	s := NewServer(database, cfg, zap.NewNop(), photos, aifeatures.New(nil), cipher)
	s.SetHard75Syncer(NewHard75Syncer(s, time.Hour))
	Hard75Pace = 0

	r := chi.NewRouter()
	r.Post("/api/auth/signup", s.HandleSignup)
	r.Post("/api/auth/login", s.HandleLogin)
	r.Group(func(r chi.Router) {
		r.Use(s.JWTAuth)
		r.Get("/api/me", s.HandleMe)
		r.Put("/api/goals", s.HandleSaveGoals)
		r.Get("/api/today", s.HandleGetToday)
		r.Get("/api/days", s.HandleListDays)
		r.Get("/api/days/{date}", s.HandleGetDay)
		r.Patch("/api/days/{date}", s.HandleUpdateDay)
		r.Post("/api/days/{date}/water", s.HandleAddWater)
		r.Delete("/api/days/{date}/water/{waterID}", s.HandleDeleteWater)
		r.Post("/api/meals", s.HandleCreateMeal)
		r.Patch("/api/meals/{mealID}", s.HandleUpdateMeal)
		r.Post("/api/recipes", s.HandleCreateRecipe)
		r.Get("/api/recipes", s.HandleListRecipes)
		r.Patch("/api/recipes/{recipeID}", s.HandleUpdateRecipe)
		r.Post("/api/recipes/{recipeID}/cook", s.HandleCookRecipe)
		r.Post("/api/workouts", s.HandleCreateWorkout)
		r.Post("/api/journal", s.HandleCreateJournal)
		r.Get("/api/journal", s.HandleListJournal)
		r.Get("/api/stats", s.HandleGetStats)
		r.Get("/api/dashboard", s.HandleGetDashboard)
		r.Post("/api/tokens", s.HandleCreateToken)
		r.Get("/api/integrations/75hard", s.HandleHard75Status)
		r.Put("/api/integrations/75hard", s.HandleHard75Connect)
		r.Get("/api/blood/reports", s.HandleListBloodReports)
		r.Post("/api/blood/reports", s.HandleCreateBloodReport)
		r.Post("/api/blood/reports/upload", s.HandleUploadBloodReport)
		r.Get("/api/blood/markers", s.HandleMarkerSeries)
		r.Get("/api/analysis/health", s.HandleHealthSummary)
		r.Post("/api/import/health", s.HandleImportWebhook)
	})
	return s, r
}

type client struct {
	t     *testing.T
	h     http.Handler
	token string
}

func (c *client) do(method, path string, body any) (int, map[string]any) {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func (c *client) doList(method, path string) (int, []map[string]any) {
	c.t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	var out []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func signup(t *testing.T, h http.Handler, email string) *client {
	t.Helper()
	c := &client{t: t, h: h}
	code, res := c.do("POST", "/api/auth/signup", map[string]any{"email": email, "password": "password123", "name": "Test", "timezone": "UTC"})
	if code != 200 {
		t.Fatalf("signup: %d %v", code, res)
	}
	c.token = res["token"].(string)
	return c
}

func TestSignupLoginAndProfile(t *testing.T) {
	_, h := newTestServer(t)
	c := signup(t, h, "a@example.com")
	code, me := c.do("GET", "/api/me", nil)
	if code != 200 || me["email"] != "a@example.com" || me["hard75_eligible"] != false {
		t.Fatalf("me: %d %v", code, me)
	}
	code, _ = c.do("POST", "/api/auth/signup", map[string]any{"email": "A@example.com", "password": "password123"})
	if code != 409 {
		t.Fatalf("duplicate signup should conflict, got %d", code)
	}
	code, _ = c.do("POST", "/api/auth/login", map[string]any{"email": "a@example.com", "password": "wrong"})
	if code != 401 {
		t.Fatalf("bad password should be 401, got %d", code)
	}
	anon := &client{t: t, h: h}
	if code, _ := anon.do("GET", "/api/me", nil); code != 401 {
		t.Fatalf("no token should be 401, got %d", code)
	}
}

func TestDayTotalsAndMetrics(t *testing.T) {
	_, h := newTestServer(t)
	c := signup(t, h, "b@example.com")
	c.do("PUT", "/api/goals", map[string]any{"daily_kcal": 2000, "protein_g": 150})

	code, meal := c.do("POST", "/api/meals", map[string]any{
		"date": "2026-09-01", "name": "Eggs", "slot": "breakfast",
		"items": []map[string]any{{"name": "egg", "qty": 3, "unit": "piece", "kcal": 210, "protein_g": 18}, {"name": "toast", "kcal": 90, "carbs_g": 17}},
	})
	if code != 201 || meal["kcal"].(float64) != 300 || meal["protein_g"].(float64) != 18 {
		t.Fatalf("meal totals wrong: %d %v", code, meal)
	}
	c.do("POST", "/api/workouts", map[string]any{"date": "2026-09-01", "kind": "run", "minutes": 40, "kcal": 400})

	code, day := c.do("PATCH", "/api/days/2026-09-01", map[string]any{"weight_kg": 82.4, "sleep_hours": 7.5, "mood": 4})
	if code != 200 {
		t.Fatalf("patch day: %d %v", code, day)
	}
	totals := day["totals"].(map[string]any)
	if totals["kcal"].(float64) != 300 || totals["workout_minutes"].(float64) != 40 {
		t.Fatalf("day totals wrong: %v", totals)
	}
	metrics := day["metrics"].(map[string]any)
	if metrics["weight_kg"].(float64) != 82.4 || metrics["mood"].(float64) != 4 {
		t.Fatalf("metrics wrong: %v", metrics)
	}
	if code, _ := c.do("PATCH", "/api/days/2026-09-01", map[string]any{"mood": 9}); code != 400 {
		t.Fatalf("mood 9 should be rejected, got %d", code)
	}

	// Clearing a field.
	code, day = c.do("PATCH", "/api/days/2026-09-01", map[string]any{"clear": []string{"weight_kg"}})
	if code != 200 || day["metrics"].(map[string]any)["weight_kg"] != nil {
		t.Fatalf("clear failed: %v", day["metrics"])
	}

	code, list := c.doList("GET", "/api/days?from=2026-08-30&to=2026-09-02")
	if code != 200 || len(list) != 4 {
		t.Fatalf("days list: %d %d", code, len(list))
	}
	if list[2]["date"] != "2026-09-01" || list[2]["kcal"].(float64) != 300 || list[2]["workout_minutes"].(float64) != 40 {
		t.Fatalf("summary wrong: %v", list[2])
	}
}

func TestRecipesAndCooking(t *testing.T) {
	_, h := newTestServer(t)
	c := signup(t, h, "c@example.com")
	code, rc := c.do("POST", "/api/recipes", map[string]any{
		"name": "Chicken rice", "servings": 4, "kcal_per_serving": 550, "protein_g": 45, "carbs_g": 60, "fat_g": 12,
		"ingredients": []string{"800g chicken", "2 cups rice"}, "steps": []string{"cook", "eat"}, "tags": []string{"High-Protein", "Dinner", "high-protein"},
	})
	if code != 201 {
		t.Fatalf("create recipe: %d %v", code, rc)
	}
	tags := rc["tags"].([]any)
	if len(tags) != 2 || tags[0] != "high-protein" {
		t.Fatalf("tags not normalised: %v", tags)
	}
	id := int64(rc["id"].(float64))

	code, meal := c.do("POST", fmt.Sprintf("/api/recipes/%d/cook", id), map[string]any{"date": "2026-09-02", "slot": "dinner", "servings": 1.5})
	if code != 201 || meal["kcal"].(float64) != 825 || meal["source"] != "recipe" {
		t.Fatalf("cook: %d %v", code, meal)
	}
	code, rc = c.do("PATCH", fmt.Sprintf("/api/recipes/%d", id), map[string]any{"favourite": true})
	if code != 200 || rc["favourite"] != true || rc["times_cooked"].(float64) != 1 || rc["name"] != "Chicken rice" {
		t.Fatalf("patch recipe: %d %v", code, rc)
	}
	code, list := c.doList("GET", "/api/recipes?tag=dinner&favourite=1")
	if code != 200 || len(list) != 1 {
		t.Fatalf("filtered list: %d %d", code, len(list))
	}
	code, list = c.doList("GET", "/api/recipes?q=rice")
	if len(list) != 1 {
		t.Fatalf("search: %d %d", code, len(list))
	}
}

func TestStatsAndStreak(t *testing.T) {
	s, h := newTestServer(t)
	c := signup(t, h, "d@example.com")
	today := s.today(context.WithValue(context.Background(), UserIDKey, int64(1)))
	for i := 0; i < 3; i++ {
		d := time.Now().UTC().AddDate(0, 0, -i).Format("2006-01-02")
		c.do("PATCH", "/api/days/"+d, map[string]any{"weight_kg": 80.0 - float64(i)})
	}
	_ = today
	code, st := c.do("GET", "/api/stats?days=30", nil)
	if code != 200 {
		t.Fatalf("stats: %d %v", code, st)
	}
	if st["streak"].(float64) != 3 || st["days_logged"].(float64) != 3 {
		t.Fatalf("streak/days wrong: %v %v", st["streak"], st["days_logged"])
	}
	trend := st["weight_trend"].(map[string]any)
	if trend["change"].(float64) != 2 || trend["count"].(float64) != 3 {
		t.Fatalf("weight trend wrong: %v", trend)
	}
	code, dash := c.do("GET", "/api/dashboard", nil)
	if code != 200 || len(dash["week"].([]any)) != 7 {
		t.Fatalf("dashboard: %d", code)
	}
}

func TestAPITokenScopes(t *testing.T) {
	_, h := newTestServer(t)
	c := signup(t, h, "e@example.com")
	code, res := c.do("POST", "/api/tokens", map[string]any{"name": "ro", "scopes": []string{"read"}})
	if code != 201 {
		t.Fatalf("create token: %d %v", code, res)
	}
	ro := &client{t: t, h: h, token: res["secret"].(string)}
	if code, _ := ro.do("GET", "/api/me", nil); code != 200 {
		t.Fatalf("read token should read, got %d", code)
	}
	if code, _ := ro.do("POST", "/api/meals", map[string]any{"name": "x", "kcal": 1}); code != 403 {
		t.Fatalf("read token should not write, got %d", code)
	}
	if code, _ := ro.do("POST", "/api/tokens", map[string]any{"name": "escalate", "scopes": []string{"read", "write"}}); code != 403 {
		t.Fatalf("token should not mint tokens, got %d", code)
	}
}

// fakeHard75 is a minimal 75hard API for the sync test.
func fakeHard75(t *testing.T) *httptest.Server {
	t.Helper()
	var img bytes.Buffer
	im := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			im.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 5), 90, 255})
		}
	}
	_ = jpeg.Encode(&img, im, nil)

	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer 75h_good" {
			w.WriteHeader(401)
			return false
		}
		return true
	}
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/api/me", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			writeJSON(w, map[string]any{"id": 1, "email": "anchoo2kewl@gmail.com"})
		}
	})
	mux.HandleFunc("/api/programs", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			writeJSON(w, []map[string]any{{"id": 7, "name": "75 Hard", "start_date": "2026-08-01", "length_days": 75, "status": "active"}})
		}
	})
	mux.HandleFunc("/api/programs/7/days", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			writeJSON(w, []map[string]any{
				{"day_number": 1, "date": "2026-08-01", "status": "complete", "tasks_done": 6, "photo_count": 2},
				{"day_number": 2, "date": "2026-08-02", "status": "pending", "tasks_done": 0, "photo_count": 0},
				{"day_number": 3, "date": "2099-01-01", "status": "pending", "tasks_done": 3, "photo_count": 0},
			})
		}
	})
	mux.HandleFunc("/api/programs/7/days/1", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		writeJSON(w, map[string]any{
			"day_number": 1, "date": "2026-08-01", "status": "complete", "note": "felt strong", "weight_kg": 84.2, "resting_hr": 58,
			"photos": []map[string]any{{"id": 11, "kind": "progress", "pose": "front"}, {"id": 12, "kind": "food"}},
			"meals": []map[string]any{{"id": 21, "photo_id": 12, "name": "Oats", "slot": "breakfast", "kcal": 410, "protein_g": 20, "carbs_g": 60, "fat_g": 9, "source": "ai", "eaten_at": "2026-08-01T07:30:00Z", "estimate_status": "done",
				"items": []map[string]any{{"name": "oats", "qty": 80, "unit": "g", "kcal": 300, "protein_g": 10, "carbs_g": 54, "fat_g": 6}, {"name": "milk", "qty": 200, "unit": "ml", "kcal": 110, "protein_g": 10, "carbs_g": 6, "fat_g": 3}}}},
			"workouts":    []map[string]any{{"id": 31, "kind": "outdoor", "activity": "Morning walk", "minutes": 50, "kcal": 250, "started_at": "2026-08-01T06:00:00Z"}},
			"meditations": []map[string]any{{"id": 41, "minutes": 10, "style": "guided", "reflection": "calm"}},
		})
	})
	mux.HandleFunc("/api/photos", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			writeJSON(w, []map[string]any{
				{"id": 11, "kind": "progress", "pose": "front", "taken_at": "2026-08-01T21:00:00Z", "day_number": 1},
				{"id": 12, "kind": "food", "taken_at": "2026-08-01T07:29:00Z", "day_number": 1, "caption": "oats"},
			})
		}
	})
	mux.HandleFunc("/api/photos/11/file", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(img.Bytes())
		}
	})
	mux.HandleFunc("/api/photos/12/file", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(img.Bytes())
		}
	})
	mux.HandleFunc("/api/journal", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			writeJSON(w, []map[string]any{
				{"id": 51, "day_number": 1, "title": "Day one", "kind": "typed", "body": "Started today.", "created_at": "2026-08-01T22:00:00Z"},
				{"id": 52, "kind": "pdf", "body": "", "parsed_text": "Scanned page text", "created_at": "2026-08-05T22:00:00Z"},
				{"id": 53, "kind": "pdf", "body": "", "parsed_text": "", "created_at": "2026-08-06T22:00:00Z"},
			})
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestHard75SyncEndToEnd(t *testing.T) {
	s, h := newTestServer(t)
	fake := fakeHard75(t)

	// Not on the allow-list: cannot connect.
	other := signup(t, h, "other@example.com")
	if code, _ := other.do("PUT", "/api/integrations/75hard", map[string]any{"base_url": fake.URL, "token": "75h_good"}); code != 403 {
		t.Fatalf("ineligible account should be refused, got %d", code)
	}

	c := signup(t, h, "anchoo2kewl@gmail.com")
	if code, res := c.do("PUT", "/api/integrations/75hard", map[string]any{"base_url": fake.URL, "token": "75h_bad"}); code != 400 {
		t.Fatalf("bad token should be rejected: %d %v", code, res)
	}
	code, res := c.do("PUT", "/api/integrations/75hard", map[string]any{"base_url": fake.URL, "token": "75h_good"})
	if code != 200 || res["remote_account"] != "anchoo2kewl@gmail.com" {
		t.Fatalf("connect: %d %v", code, res)
	}

	var userID int64
	_ = s.db.QueryRow(`SELECT id FROM users WHERE email = 'anchoo2kewl@gmail.com'`).Scan(&userID)
	sum, err := s.syncer.SyncUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if sum.Photos != 2 || sum.Meals != 1 || sum.Workouts != 1 || sum.Meditations != 1 || sum.Journal != 2 || sum.Days != 1 {
		t.Fatalf("summary wrong: %+v", sum)
	}

	// Second run is idempotent.
	sum2, err := s.syncer.SyncUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if sum2.Photos != 0 {
		t.Fatalf("photos re-imported: %+v", sum2)
	}
	var meals, photos, workouts, journal int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM meals WHERE user_id = ?`, userID).Scan(&meals)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM photos WHERE user_id = ?`, userID).Scan(&photos)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM workouts WHERE user_id = ?`, userID).Scan(&workouts)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM journal_entries WHERE user_id = ?`, userID).Scan(&journal)
	if meals != 1 || photos != 2 || workouts != 1 || journal != 2 {
		t.Fatalf("duplicates after second sync: meals=%d photos=%d workouts=%d journal=%d", meals, photos, workouts, journal)
	}

	code, day := c.do("GET", "/api/days/2026-08-01", nil)
	if code != 200 {
		t.Fatalf("day: %d", code)
	}
	m := day["metrics"].(map[string]any)
	if m["weight_kg"].(float64) != 84.2 || m["resting_hr"].(float64) != 58 || m["note"] != "felt strong" {
		t.Fatalf("metrics not imported: %v", m)
	}
	mealsOut := day["meals"].([]any)
	meal := mealsOut[0].(map[string]any)
	if meal["source"] != "75hard" || meal["kcal"].(float64) != 410 || len(meal["items"].([]any)) != 2 || meal["photo_url"] == nil {
		t.Fatalf("meal not imported properly: %v", meal)
	}
	wk := day["workouts"].([]any)[0].(map[string]any)
	if wk["kind"] != "walk" || wk["minutes"].(float64) != 50 {
		t.Fatalf("workout mapping wrong: %v", wk)
	}
	med := day["meditations"].([]any)[0].(map[string]any)
	if !strings.Contains(med["notes"].(string), "calm") {
		t.Fatalf("meditation reflection lost: %v", med)
	}
	if len(day["photos"].([]any)) != 2 {
		t.Fatalf("photos not on the day: %v", day["photos"])
	}

	// A hand-entered weight survives the next pull.
	c.do("PATCH", "/api/days/2026-08-01", map[string]any{"weight_kg": 83.0})
	if _, err := s.syncer.SyncUser(context.Background(), userID); err != nil {
		t.Fatal(err)
	}
	_, day = c.do("GET", "/api/days/2026-08-01", nil)
	if day["metrics"].(map[string]any)["weight_kg"].(float64) != 83.0 {
		t.Fatal("import overwrote a manual weight")
	}

	// Journal: the PDF fell back to parsed text and the date came from created_at.
	code, entries := c.doList("GET", "/api/journal")
	if code != 200 || len(entries) != 2 {
		t.Fatalf("journal: %d %d", code, len(entries))
	}
	if entries[0]["date"] != "2026-08-05" || entries[0]["body"] != "Scanned page text" {
		t.Fatalf("pdf entry wrong: %v", entries[0])
	}

	code, st := c.do("GET", "/api/integrations/75hard", nil)
	if code != 200 || st["connected"] != true || st["last_status"] != "ok" || st["token_hint"] != "good" {
		t.Fatalf("status: %d %v", code, st)
	}
}
