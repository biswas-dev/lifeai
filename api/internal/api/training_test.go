package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func trainerSession() map[string]any {
	return map[string]any{
		"date": "2026-10-09", "activity": "Trainer session", "minutes": 60, "started_at": "2026-10-09T10:30:00Z",
		"exercises": []map[string]any{
			{"slug": "cat-cow", "sets": []map[string]any{{"reps": 8}}},
			{"name": "Goblet squats", "sets": []map[string]any{{"reps": 10, "weight": 16}, {"reps": 10, "weight": 22}}},
			{"slug": "incline-dumbbell-press", "notes": "bench angle adjusted", "sets": []map[string]any{
				{"reps": 10, "weight": 17.5}, {"reps": 10, "weight": 20}, {"reps": 10, "weight": 20}, {"reps": 8, "weight": 25}, {"reps": 10, "weight": 22},
			}},
			{"name": "Trainer's mystery stretch"},
		},
	}
}

func TestStrengthSessionRoundTrip(t *testing.T) {
	_, h := newTestServer(t)
	c := signup(t, h, "lifter@example.com")
	code, wk := c.do("POST", "/api/workouts", trainerSession())
	if code != 201 {
		t.Fatalf("create: %d %v", code, wk)
	}
	if wk["kind"] != "strength" {
		t.Fatalf("exercises should default the kind to strength: %v", wk["kind"])
	}
	exercises := wk["exercises"].([]any)
	if len(exercises) != 4 {
		t.Fatalf("exercises: %v", exercises)
	}
	goblet := exercises[1].(map[string]any)
	if goblet["slug"] != "goblet-squat" || goblet["name"] != "Goblet squats" || len(goblet["sets"].([]any)) != 2 {
		t.Fatalf("a recognised name should resolve to its slug: %v", goblet)
	}
	if cat := exercises[0].(map[string]any); cat["name"] != "Cat–cow" {
		t.Fatalf("a slug alone should take the catalog name: %v", cat)
	}
	custom := exercises[3].(map[string]any)
	if custom["slug"] != "" || custom["name"] != "Trainer's mystery stretch" || len(custom["sets"].([]any)) != 0 {
		t.Fatalf("custom exercise: %v", custom)
	}
	set := exercises[2].(map[string]any)["sets"].([]any)[0].(map[string]any)
	if set["weight"] != 17.5 || set["unit"] != "lb" || set["reps"] != float64(10) || set["seconds"] != nil {
		t.Fatalf("set: %v", set)
	}

	_, day := c.do("GET", "/api/days/2026-10-09", nil)
	if got := day["workouts"].([]any)[0].(map[string]any)["exercises"].([]any); len(got) != 4 {
		t.Fatalf("day view lost exercises: %v", got)
	}

	id := int64(wk["id"].(float64))
	edit := trainerSession()
	edit["exercises"] = []map[string]any{{"slug": "lateral-raise", "sets": []map[string]any{{"reps": 12, "weight": 7.5}}}}
	code, wk = c.do("PUT", fmt.Sprintf("/api/workouts/%d", id), edit)
	if code != 200 || len(wk["exercises"].([]any)) != 1 {
		t.Fatalf("update should replace exercises: %d %v", code, wk)
	}
	c.do("POST", "/api/workouts", map[string]any{"date": "2026-10-12", "minutes": 45, "exercises": []map[string]any{{"slug": "lateral-raise", "sets": []map[string]any{{"reps": 12, "weight": 10}}}}})
	_, history := c.doList("GET", "/api/exercises/lateral-raise/history")
	if len(history) != 2 || history[0]["date"] != "2026-10-12" || history[1]["date"] != "2026-10-09" {
		t.Fatalf("history newest first: %v", history)
	}

	other := signup(t, h, "stranger@example.com")
	if code, _ := other.do("GET", fmt.Sprintf("/api/workouts/%d", id), nil); code != 404 {
		t.Fatalf("foreign read: %d", code)
	}
	if code, _ := other.do("PUT", fmt.Sprintf("/api/workouts/%d", id), edit); code != 404 {
		t.Fatalf("foreign update: %d", code)
	}
	if _, foreign := other.doList("GET", "/api/exercises/lateral-raise/history"); len(foreign) != 0 {
		t.Fatal("history leaked across accounts")
	}
}

func TestStrengthSessionValidation(t *testing.T) {
	_, h := newTestServer(t)
	c := signup(t, h, "careful@example.com")
	for name, ex := range map[string]map[string]any{
		"unknown slug": {"slug": "levitation"},
		"no name":      {"notes": "?"},
		"bad unit":     {"slug": "goblet-squat", "sets": []map[string]any{{"reps": 5, "weight": 10, "unit": "stone"}}},
		"bad reps":     {"slug": "goblet-squat", "sets": []map[string]any{{"reps": -1}}},
	} {
		code, res := c.do("POST", "/api/workouts", map[string]any{"minutes": 30, "exercises": []map[string]any{ex}})
		if code != 400 {
			t.Errorf("%s: %d %v", name, code, res)
		}
	}
}

func TestWeeklyExerciseGoal(t *testing.T) {
	_, h := newTestServer(t)
	c := signup(t, h, "weekly@example.com")
	if code, g := c.do("PUT", "/api/goals", map[string]any{"weekly_workout_minutes": 300, "daily_kcal": 1840}); code != 200 || g["weekly_workout_minutes"] != float64(300) {
		t.Fatalf("goal: %d %v", code, g)
	}
	// Friday 2026-10-09 belongs to the week of Monday 2026-10-05.
	for _, w := range []map[string]any{
		{"date": "2026-10-04", "kind": "walk", "minutes": 40}, // previous Sunday
		{"date": "2026-10-05", "kind": "strength", "minutes": 60},
		{"date": "2026-10-07", "kind": "walk", "minutes": 31},
		{"date": "2026-10-08", "kind": "cycle", "minutes": 25},
		{"date": "2026-10-11", "kind": "walk", "minutes": 20}, // Sunday, same week
	} {
		if code, res := c.do("POST", "/api/workouts", w); code != 201 {
			t.Fatalf("seed: %d %v", code, res)
		}
	}
	_, week := c.do("GET", "/api/training/week?date=2026-10-09", nil)
	if week["from"] != "2026-10-05" || week["to"] != "2026-10-11" || week["minutes"] != float64(136) || week["goal"] != float64(300) || week["sessions"] != float64(4) {
		t.Fatalf("week: %v", week)
	}
	_, day := c.do("GET", "/api/days/2026-10-05", nil)
	if day["week"].(map[string]any)["minutes"] != float64(136) {
		t.Fatalf("day carries the week: %v", day["week"])
	}
}

func TestNutritionPlanAndPlannedMeals(t *testing.T) {
	_, h := newTestServer(t)
	c := signup(t, h, "planner@example.com")
	_, empty := c.do("GET", "/api/nutrition-plan", nil)
	if empty["plan"] != nil {
		t.Fatalf("no plan yet: %v", empty)
	}
	plan := map[string]any{
		"name": "1-7 PM", "window_start": "13:00", "window_end": "19:00",
		"targets": map[string]any{"kcal": 1800, "kcal_max": 1850, "protein_g": 140, "protein_g_max": 150},
		"meals": []map[string]any{
			{"key": "lunch", "time": "13:00", "title": "Drink + lunch", "slot": "lunch", "options": []map[string]any{{"key": "default", "title": "Chicken, rice, lentils", "kcal": 700, "protein_g": 68}}},
			{"key": "dinner", "time": "18:15", "end_time": "18:45", "title": "Dinner", "slot": "dinner", "options": []map[string]any{
				{"key": "salmon", "title": "Salmon", "kcal": 720, "protein_g": 49, "days": []int{1, 4}},
				{"key": "tofu", "title": "Tofu bhurji", "kcal": 730, "protein_g": 49, "days": []int{3, 6}},
			}},
		},
	}
	code, saved := c.do("PUT", "/api/nutrition-plan", plan)
	if code != 200 || saved["plan"].(map[string]any)["window_end"] != "19:00" {
		t.Fatalf("save: %d %v", code, saved)
	}
	bad := map[string]any{"name": "x", "window_start": "1pm", "window_end": "19:00", "meals": plan["meals"]}
	if code, _ := c.do("PUT", "/api/nutrition-plan", bad); code != 400 {
		t.Fatalf("bad time accepted: %d", code)
	}
	dup := map[string]any{"name": "x", "meals": []any{plan["meals"].([]map[string]any)[0], plan["meals"].([]map[string]any)[0]}}
	if code, _ := c.do("PUT", "/api/nutrition-plan", dup); code != 400 {
		t.Fatalf("duplicate meal keys accepted: %d", code)
	}
	code, meal := c.do("POST", "/api/meals", map[string]any{"date": "2026-10-12", "name": "Salmon dinner", "slot": "dinner", "kcal": 720, "protein_g": 49, "plan_key": "dinner:salmon"})
	if code != 201 || meal["plan_key"] != "dinner:salmon" {
		t.Fatalf("planned meal: %d %v", code, meal)
	}
}

// fakeStrava records uploads and lists them back as the athlete's activities.
type fakeStrava struct {
	mu      sync.Mutex
	created []url.Values
	updated []url.Values
}

func (f *fakeStrava) server(t *testing.T) *httptest.Server {
	r := chi.NewRouter()
	r.Post("/activities", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.created = append(f.created, r.PostForm)
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 777})
	})
	r.Put("/activities/{id}", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.updated = append(f.updated, r.PostForm)
		f.mu.Unlock()
		_, _ = io.WriteString(w, `{"id":777}`)
	})
	r.Get("/athlete/activities", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			_, _ = io.WriteString(w, `[]`)
			return
		}
		_, _ = io.WriteString(w, `[{"id":777,"name":"Trainer session","type":"WeightTraining","sport_type":"WeightTraining","moving_time":3600,"elapsed_time":3600,"start_date":"2026-10-09T10:30:00Z","start_date_local":"2026-10-09T06:30:00Z"}]`)
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func connectStrava(t *testing.T, s *Server, userID int64, scope string) {
	t.Helper()
	access, _ := s.cipher.Seal("access")
	refresh, _ := s.cipher.Seal("refresh")
	if _, err := s.db.Exec(`INSERT INTO strava_accounts (user_id, athlete_id, username, access_token_enc, refresh_token_enc, expires_at, scope, last_error) VALUES (?, 1, 'me', ?, ?, ?, ?, '')`,
		userID, access, refresh, time.Now().Add(time.Hour).Unix(), scope); err != nil {
		t.Fatal(err)
	}
}

func TestPushStrengthSessionToStrava(t *testing.T) {
	s, h := newTestServer(t)
	s.cfg.StravaClientID, s.cfg.StravaClientSecret = "id", "secret"
	fake := &fakeStrava{}
	s.stravaAPI = fake.server(t).URL
	c := signup(t, h, "strava-lifter@example.com")
	s.db.Exec(`UPDATE users SET timezone='America/Toronto' WHERE email='strava-lifter@example.com'`)
	var uid int64
	s.db.QueryRow(`SELECT id FROM users WHERE email='strava-lifter@example.com'`).Scan(&uid)
	_, wk := c.do("POST", "/api/workouts", trainerSession())
	path := fmt.Sprintf("/api/workouts/%d/strava", int64(wk["id"].(float64)))

	if code, _ := c.do("POST", path, nil); code != 400 {
		t.Fatalf("not connected: %d", code)
	}
	connectStrava(t, s, uid, "read,activity:read_all")
	if code, res := c.do("POST", path, nil); code != 403 || res["code"] != "strava_scope" {
		t.Fatalf("a read-only connection must ask to reconnect: %d %v", code, res)
	}
	if _, st := c.do("GET", "/api/strava/status", nil); st["can_upload"] != false {
		t.Fatalf("status: %v", st)
	}
	s.db.Exec(`UPDATE strava_accounts SET scope='read,activity:read_all,activity:write' WHERE user_id=?`, uid)

	code, up := c.do("POST", path, nil)
	if code != 200 || up["strava_id"] != "777" || up["created"] != true || up["url"] != "https://www.strava.com/activities/777" {
		t.Fatalf("upload: %d %v", code, up)
	}
	form := fake.created[0]
	if form.Get("sport_type") != "WeightTraining" || form.Get("elapsed_time") != "3600" || form.Get("start_date_local") != "2026-10-09T06:30:00" || form.Get("trainer") != "1" {
		t.Fatalf("activity form: %v", form)
	}
	desc := form.Get("description")
	for _, want := range []string{"Goblet squats: 16 lb × 10, 22 lb × 10", "Incline dumbbell chest press: 17.5 lb × 10, 2×(20 lb × 10), 25 lb × 8, 22 lb × 10 (bench angle adjusted)", "Cat–cow: 8 reps"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description missing %q:\n%s", want, desc)
		}
	}

	// The next import finds the same activity and keeps one session, with
	// the exercises logged here.
	if _, err := s.syncStrava(context.Background(), uid); err != nil {
		t.Fatal(err)
	}
	_, day := c.do("GET", "/api/days/2026-10-09", nil)
	workouts := day["workouts"].([]any)
	if len(workouts) != 1 {
		t.Fatalf("upload then import duplicated the session: %v", workouts)
	}
	if w := workouts[0].(map[string]any); w["strava_id"] != "777" || len(w["exercises"].([]any)) != 4 || w["source"] != "manual" {
		t.Fatalf("merged session: %v", w)
	}

	// Pushing again updates the linked activity instead of creating another.
	if code, up = c.do("POST", path, nil); code != 200 || up["created"] != false || len(fake.created) != 1 || len(fake.updated) != 1 {
		t.Fatalf("second push: %d %v created=%d updated=%d", code, up, len(fake.created), len(fake.updated))
	}
}

func TestMCPTrainingTools(t *testing.T) {
	s, h := newTestServer(t)
	r := chi.NewRouter()
	r.Mount("/", h)
	r.HandleFunc("/mcp", s.HandleMCP)
	c := signup(t, r, "agent-lifter@example.com")
	c.do("PUT", "/api/goals", map[string]any{"daily_kcal": 1840, "protein_g": 150})
	_, tok := c.do("POST", "/api/tokens", map[string]any{"name": "agent", "scopes": []string{"read", "write"}})
	token := tok["secret"].(string)
	call := func(name string, args any) map[string]any {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
		code, res := mcpCall(t, r, token, string(b))
		result, _ := res["result"].(map[string]any)
		if code != 200 || result == nil || result["isError"] == true {
			t.Fatalf("%s: %d %v", name, code, res)
		}
		return result
	}
	text := func(result map[string]any) string {
		return result["content"].([]any)[0].(map[string]any)["text"].(string)
	}

	if got := text(call("list_exercises", map[string]any{"muscle": "side-delts"})); !strings.Contains(got, `"lateral-raise"`) {
		t.Fatalf("list_exercises: %s", got)
	}
	logged := call("log_workout", trainerSession())["structuredContent"].(map[string]any)
	if logged["kind"] != "strength" || len(logged["exercises"].([]any)) != 4 {
		t.Fatalf("log_workout: %v", logged)
	}
	goals := call("set_goals", map[string]any{"weekly_workout_minutes": 300})["structuredContent"].(map[string]any)
	if goals["weekly_workout_minutes"] != float64(300) || goals["daily_kcal"] != float64(1840) || goals["protein_g"] != float64(150) {
		t.Fatalf("set_goals must merge, not replace: %v", goals)
	}
	week := call("get_week_training", map[string]any{"date": "2026-10-09"})["structuredContent"].(map[string]any)
	if week["minutes"] != float64(60) || week["goal"] != float64(300) {
		t.Fatalf("week: %v", week)
	}
	if got := text(call("get_exercise_history", map[string]any{"name": "incline chest press"})); !strings.Contains(got, `"weight":25`) {
		t.Fatalf("history by name: %s", got)
	}
	form := call("get_exercise", map[string]any{"slug": "one arm row"})["structuredContent"].(map[string]any)
	if form["slug"] != "one-arm-dumbbell-row" || len(form["cues"].([]any)) == 0 {
		t.Fatalf("get_exercise resolves names: %v", form)
	}
}

func TestTrainingScheduleMarksFixedSessions(t *testing.T) {
	_, h := newTestServer(t)
	c := signup(t, h, "scheduled@example.com")
	bad := map[string]any{"fixed": []map[string]any{{"title": "Trainer", "days": []int{1}, "start": "6:30", "end": "07:30"}}}
	if code, _ := c.do("PUT", "/api/training/schedule", bad); code != 400 {
		t.Fatalf("bad time accepted: %d", code)
	}
	schedule := map[string]any{
		"fixed": []map[string]any{{"title": "Weight training with trainer", "kind": "strength", "days": []int{1, 5}, "start": "06:30", "end": "07:30"}},
		"notes": "Walks and rides whenever they fit",
	}
	code, saved := c.do("PUT", "/api/training/schedule", schedule)
	if code != 200 || saved["schedule"].(map[string]any)["fixed"].([]any)[0].(map[string]any)["key"] != "weight-training-with-trainer" {
		t.Fatalf("save: %d %v", code, saved)
	}
	c.do("PUT", "/api/goals", map[string]any{"weekly_workout_minutes": 300})
	_, monday := c.do("POST", "/api/workouts", map[string]any{"date": "2026-10-05", "kind": "strength", "minutes": 60})
	c.do("POST", "/api/workouts", map[string]any{"date": "2026-10-07", "kind": "walk", "minutes": 31})
	_, week := c.do("GET", "/api/training/week?date=2026-10-07", nil)
	planned := week["planned"].([]any)
	if len(planned) != 2 {
		t.Fatalf("planned: %v", planned)
	}
	mon, fri := planned[0].(map[string]any), planned[1].(map[string]any)
	if mon["date"] != "2026-10-05" || mon["status"] != "done" || mon["workout_id"] != monday["id"] || mon["minutes"] != float64(60) {
		t.Fatalf("Monday: %v", mon)
	}
	if fri["date"] != "2026-10-09" || fri["status"] == "done" || fri["workout_id"] != nil {
		t.Fatalf("a walk must not count as the Friday strength session: %v", fri)
	}
}

func TestPlannedSessionsStatusAndFreeform(t *testing.T) {
	sc := &TrainingSchedule{Fixed: []ScheduleSlot{{Key: "trainer", Title: "Trainer", Kind: "strength", Days: []int{1, 5}, Start: "06:30", End: "07:30"}}}
	days := []string{"2026-10-05", "2026-10-06", "2026-10-07", "2026-10-08", "2026-10-09", "2026-10-10", "2026-10-11"}
	got := plannedSessions(sc, days, "2026-10-07", map[string][]weekWorkout{})
	if len(got) != 2 || got[0].Status != "missed" || got[1].Status != "upcoming" {
		t.Fatalf("statuses: %+v", got)
	}
	got = plannedSessions(sc, days, "2026-10-09", map[string][]weekWorkout{"2026-10-09": {{id: 7, kind: "walk"}, {id: 8, kind: "strength"}}})
	if got[1].Status != "done" || *got[1].WorkoutID != 8 {
		t.Fatalf("match by kind: %+v", got[1])
	}
	if (ScheduleSlot{Start: "23:30", End: "00:15"}).Minutes() != 45 {
		t.Fatal("a slot across midnight")
	}
}

func TestMCPBackfillFromTranscript(t *testing.T) {
	s, h := newTestServer(t)
	r := chi.NewRouter()
	r.Mount("/", h)
	r.HandleFunc("/mcp", s.HandleMCP)
	c := signup(t, r, "transcript@example.com")
	s.db.Exec(`UPDATE users SET timezone='America/Toronto' WHERE email='transcript@example.com'`)
	c.do("PUT", "/api/training/schedule", map[string]any{"fixed": []map[string]any{{"key": "trainer", "title": "Weight training with trainer", "kind": "strength", "days": []int{1, 5}, "start": "06:30", "end": "07:30"}}})
	_, tok := c.do("POST", "/api/tokens", map[string]any{"name": "agent", "scopes": []string{"read", "write"}})
	call := func(args any) map[string]any {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "backfill_session", "arguments": args}})
		_, res := mcpCall(t, r, tok["secret"].(string), string(b))
		result := res["result"].(map[string]any)
		if result["isError"] == true {
			t.Fatalf("backfill: %v", result)
		}
		return result["structuredContent"].(map[string]any)
	}

	// A Friday with nothing logged: created in the fixed slot.
	got := call(map[string]any{"date": "2026-10-16", "exercises": []map[string]any{{"name": "goblet squats", "sets": []map[string]any{{"weight": 22, "reps": 10}}}}})
	w := got["workout"].(map[string]any)
	if got["action"] != "created" || got["slot"] != "trainer" || w["activity"] != "Weight training with trainer" || w["minutes"] != float64(60) || w["started_at"] != "2026-10-16T10:30:00Z" {
		t.Fatalf("created: %v", got)
	}

	// A quick-logged Monday session gets its exercises filled in, not duplicated.
	c.do("POST", "/api/workouts", map[string]any{"date": "2026-10-12", "kind": "strength", "activity": "Trainer", "minutes": 55})
	got = call(map[string]any{"date": "2026-10-12", "notes": "From transcript", "exercises": []map[string]any{{"slug": "lateral-raise", "sets": []map[string]any{{"weight": 7.5, "reps": 12}}}}})
	w = got["workout"].(map[string]any)
	if got["action"] != "updated" || w["activity"] != "Trainer" || w["minutes"] != float64(55) || len(w["exercises"].([]any)) != 1 {
		t.Fatalf("updated: %v", got)
	}
	got = call(map[string]any{"date": "2026-10-12", "append": true, "exercises": []map[string]any{{"slug": "crunch"}}})
	if ex := got["workout"].(map[string]any)["exercises"].([]any); len(ex) != 2 || ex[0].(map[string]any)["sets"].([]any)[0].(map[string]any)["weight"] != 7.5 {
		t.Fatalf("append kept earlier sets: %v", ex)
	}
	_, day := c.do("GET", "/api/days/2026-10-12", nil)
	if n := len(day["workouts"].([]any)); n != 1 {
		t.Fatalf("backfill duplicated the session: %d", n)
	}
}
