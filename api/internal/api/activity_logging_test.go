package api

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/biswas-dev/lifeai/api/internal/health"
	"github.com/biswas-dev/lifeai/api/internal/integrations/hard75"
)

func TestWaterAddsRetriesUndoAndOwnership(t *testing.T) {
	s, h := newTestServer(t)
	c := signup(t, h, "water@example.com")
	date := time.Now().UTC().Format("2006-01-02")
	path := "/api/days/" + date + "/water"
	var day map[string]any
	for i := 0; i < 4; i++ {
		code, result := c.do("POST", path, map[string]any{"amount": .25, "unit": "gal", "request_id": fmt.Sprintf("drink-%08d", i)})
		if code != 200 {
			t.Fatalf("add: %d %v", code, result)
		}
		day = result
	}
	if day["metrics"].(map[string]any)["water_ml"] != float64(3785) || len(day["water"].([]any)) != 4 {
		t.Fatalf("quarter gallons did not sum accurately: %v", day)
	}
	retry := map[string]any{"amount": .25, "unit": "gal", "request_id": "drink-00000003"}
	code, day := c.do("POST", path, retry)
	if code != 200 || len(day["water"].([]any)) != 4 {
		t.Fatal("retry added a second drink")
	}
	other := signup(t, h, "other-water@example.com")
	id := int64(day["water"].([]any)[0].(map[string]any)["id"].(float64))
	if code, _ := other.do("DELETE", fmt.Sprintf("%s/%d", path, id), nil); code != 404 {
		t.Fatalf("foreign undo: %d", code)
	}
	code, day = c.do("DELETE", fmt.Sprintf("%s/%d", path, id), nil)
	if code != 200 || day["metrics"].(map[string]any)["water_ml"] != float64(2839) {
		t.Fatalf("undo: %d %v", code, day)
	}
	code, day = c.do("POST", path, retry)
	if code != 200 || len(day["water"].([]any)) != 3 {
		t.Fatal("late retry resurrected an undone drink")
	}
	if code, _ = c.do("POST", path, map[string]any{"amount": 500, "unit": "ml", "request_id": "drink-00000003"}); code != 409 {
		t.Fatalf("id reused for another drink: %d", code)
	}
	for _, amount := range []float64{0, -1, 20001} {
		if code, _ := c.do("POST", path, map[string]any{"amount": amount, "unit": "ml", "request_id": "invalid-drink"}); code != 400 {
			t.Fatalf("accepted invalid amount %f", amount)
		}
	}
	var uid int64
	s.db.QueryRow(`SELECT id FROM users WHERE email='water@example.com'`).Scan(&uid)
	summaries, err := s.daySummaries(context.Background(), uid, date, date)
	if err != nil || len(summaries) != 1 || summaries[0].WaterMl == nil || *summaries[0].WaterMl != 2839 {
		t.Fatalf("water missing from calendar: %+v %v", summaries, err)
	}
	_, stats := c.do("GET", "/api/stats?days=1", nil)
	if stats["days_logged"] != float64(1) {
		t.Fatalf("water-only day missing from stats: %v", stats)
	}
}

func TestWaterConcurrentDrinksKeepImportedBaseline(t *testing.T) {
	s, h := newTestServer(t)
	c := signup(t, h, "parallel-water@example.com")
	var uid int64
	s.db.QueryRow(`SELECT id FROM users WHERE email='parallel-water@example.com'`).Scan(&uid)
	ctx := context.Background()
	date := "2026-09-01"
	if err := s.applyImport(ctx, uid, "75hard", []health.Sample{{Date: date, Metric: health.MetricWater, Source: "75hard", Value: 1000}}, nil, &ImportSummary{}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			code, _ := c.do("POST", "/api/days/"+date+"/water", map[string]any{"amount": .125, "unit": "gal", "request_id": fmt.Sprintf("parallel-%08d", i)})
			codes <- code
		}(i)
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 200 {
			t.Fatalf("concurrent add: %d", code)
		}
	}
	_, day := c.do("GET", "/api/days/"+date, nil)
	if day["metrics"].(map[string]any)["water_ml"] != float64(4785) {
		t.Fatalf("lost an addition or baseline: %v", day)
	}
	if err := s.applyImport(ctx, uid, "apple", []health.Sample{{Date: date, Metric: health.MetricWater, Source: "apple", Value: 7000}}, nil, &ImportSummary{}); err != nil {
		t.Fatal(err)
	}
	_, day = c.do("GET", "/api/days/"+date, nil)
	if day["metrics"].(map[string]any)["water_ml"] != float64(4785) {
		t.Fatal("import overwrote hand-logged water")
	}
	c.do("PATCH", "/api/days/"+date, map[string]any{"water_ml": 500})
	_, day = c.do("POST", "/api/days/"+date+"/water", map[string]any{"amount": 250, "unit": "ml", "request_id": "after-set-total"})
	if day["metrics"].(map[string]any)["water_ml"] != float64(750) {
		t.Fatal("manual total did not establish new baseline")
	}
}

func TestHard75WaterGallons(t *testing.T) {
	s, h := newTestServer(t)
	signup(t, h, "source-water@example.com")
	var id int64
	s.db.QueryRow(`SELECT id FROM users WHERE email='source-water@example.com'`).Scan(&id)
	amount := 1.0
	if err := s.syncer.importCheckIn(context.Background(), id, &hard75.Day{Date: "2026-09-01", Entries: []hard75.Entry{{Key: "water", Unit: "gallons", Value: &amount}}}, &SyncSummary{}); err != nil {
		t.Fatal(err)
	}
	var ml int
	s.db.QueryRow(`SELECT water_ml FROM days WHERE user_id=? AND on_date='2026-09-01'`, id).Scan(&ml)
	if ml != 3785 {
		t.Fatalf("gallon converted to %d ml", ml)
	}
}

func TestWorkoutAliasesSurviveEitherImportOrderAndEdits(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprint(reverse), func(t *testing.T) {
			s, h := newTestServer(t)
			signup(t, h, "activities@example.com")
			var id int64
			s.db.QueryRow(`SELECT id FROM users WHERE email='activities@example.com'`).Scan(&id)
			start := time.Date(2026, 9, 1, 14, 0, 0, 0, time.UTC)
			ctx := context.Background()
			distance := 4.2
			copy := health.WorkoutImport{Source: "75hard", ExternalID: "workout:42", Date: "2026-09-01", Kind: "walk", Activity: "Morning walk", Minutes: 60, StartedAt: start, Notes: "Imported from Strava"}
			direct := health.WorkoutImport{Source: "strava", ExternalID: "10001", Date: copy.Date, Kind: "walk", Activity: copy.Activity, Minutes: 20, StartedAt: start, DistanceKm: &distance}
			imports := []health.WorkoutImport{copy, direct}
			if reverse {
				imports = []health.WorkoutImport{direct, copy}
			}
			for _, wk := range imports {
				if _, err := s.importWorkout(ctx, id, wk); err != nil {
					t.Fatal(err)
				}
			}
			assertOne := func() {
				t.Helper()
				var count, aliases int
				s.db.QueryRow(`SELECT COUNT(*) FROM workouts WHERE user_id=?`, id).Scan(&count)
				s.db.QueryRow(`SELECT COUNT(*) FROM workout_sources WHERE user_id=?`, id).Scan(&aliases)
				if count != 1 || aliases != 2 {
					t.Fatalf("workouts=%d aliases=%d", count, aliases)
				}
			}
			assertOne()
			workouts, err := s.workoutsForDate(ctx, id, copy.Date)
			if err != nil || workouts[0].Source != "strava" || workouts[0].Minutes != 20 || len(workouts[0].Sources) != 2 || math.Abs(*workouts[0].DistanceKm-distance) > .001 {
				t.Fatalf("canonical workout: %+v %v", workouts, err)
			}
			copy.Minutes = 90
			if _, err := s.importWorkout(ctx, id, copy); err != nil {
				t.Fatal(err)
			}
			direct.Minutes = 18
			direct.StartedAt = start.Add(2 * time.Hour)
			if _, err := s.importWorkout(ctx, id, direct); err != nil {
				t.Fatal(err)
			}
			assertOne()
			workouts, _ = s.workoutsForDate(ctx, id, copy.Date)
			if workouts[0].Minutes != 18 {
				t.Fatal("direct provider correction was lost")
			}
		})
	}
}

func TestWorkoutImportSeparatesDistinctAndAmbiguousSessions(t *testing.T) {
	s, h := newTestServer(t)
	signup(t, h, "distinct@example.com")
	var id int64
	s.db.QueryRow(`SELECT id FROM users WHERE email='distinct@example.com'`).Scan(&id)
	wk := health.WorkoutImport{Source: "strava", ExternalID: "1", Date: "2026-09-01", Kind: "walk", Activity: "Walking", Minutes: 30, StartedAt: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}
	ctx := context.Background()
	for _, external := range []string{"1", "2"} {
		wk.ExternalID = external
		if _, err := s.importWorkout(ctx, id, wk); err != nil {
			t.Fatal(err)
		}
	}
	wk.Source = "75hard"
	wk.ExternalID = "workout:3"
	if _, err := s.importWorkout(ctx, id, wk); err != nil {
		t.Fatal(err)
	}
	wk.Source = "apple"
	wk.ExternalID = "different-kind"
	wk.Kind = "strength"
	wk.Activity = "Weight training"
	if _, err := s.importWorkout(ctx, id, wk); err != nil {
		t.Fatal(err)
	}
	wk.Source = "samsung"
	wk.ExternalID = "no-time"
	wk.StartedAt = time.Time{}
	if _, err := s.importWorkout(ctx, id, wk); err != nil {
		t.Fatal(err)
	}
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM workouts WHERE user_id=?`, id).Scan(&count)
	if count != 5 {
		t.Fatalf("guessed ambiguous or unrelated matches: %d", count)
	}
}

func TestWorkoutConcurrentCrossSourceImports(t *testing.T) {
	s, h := newTestServer(t)
	signup(t, h, "concurrent-imports@example.com")
	var id int64
	s.db.QueryRow(`SELECT id FROM users WHERE email='concurrent-imports@example.com'`).Scan(&id)
	errors := make(chan error, 2)
	for _, source := range []string{"75hard", "strava"} {
		go func(source string) {
			_, err := s.importWorkout(context.Background(), id, health.WorkoutImport{Source: source, ExternalID: source + ":1", Date: "2026-09-01", Kind: "run", Activity: "Morning run", Minutes: 30, StartedAt: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)})
			errors <- err
		}(source)
	}
	for i := 0; i < 2; i++ {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM workouts WHERE user_id=?`, id).Scan(&count)
	if count != 1 {
		t.Fatalf("concurrent imports made %d workouts", count)
	}
}

func TestWorkoutResyncMergesExistingCopiesAndKeepsDetails(t *testing.T) {
	s, h := newTestServer(t)
	signup(t, h, "legacy-copies@example.com")
	var id int64
	s.db.QueryRow(`SELECT id FROM users WHERE email='legacy-copies@example.com'`).Scan(&id)
	ctx := context.Background()
	start := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	copy := health.WorkoutImport{Source: "75hard", ExternalID: "workout:9", Date: "2026-09-01", Kind: "run", Activity: "Morning run", Minutes: 30, Notes: "Felt good"}
	if _, err := s.importWorkout(ctx, id, copy); err != nil {
		t.Fatal(err)
	}
	distance := 5.0
	direct := health.WorkoutImport{Source: "strava", ExternalID: "9", Date: copy.Date, Kind: copy.Kind, Activity: copy.Activity, Minutes: 30, StartedAt: start, DistanceKm: &distance}
	if _, err := s.importWorkout(ctx, id, direct); err != nil {
		t.Fatal(err)
	}
	before, _ := s.workoutsForDate(ctx, id, copy.Date)
	if len(before) != 2 {
		t.Fatal("an untimed session should stay separate")
	}
	oldest := before[0].ID
	if before[1].ID < oldest {
		oldest = before[1].ID
	}
	copy.StartedAt = start
	if _, err := s.importWorkout(ctx, id, copy); err != nil {
		t.Fatal(err)
	}
	after, err := s.workoutsForDate(ctx, id, copy.Date)
	if err != nil || len(after) != 1 {
		t.Fatalf("resync copies: %+v %v", after, err)
	}
	w := after[0]
	if w.ID != oldest || w.Source != "strava" || len(w.Sources) != 2 || w.Notes != "Felt good" || w.DistanceKm == nil || *w.DistanceKm != distance {
		t.Fatalf("lost identity or details: %+v", w)
	}
}

func TestWorkoutUnknownKindsNeedMatchingNames(t *testing.T) {
	start := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	a := importedWorkout{WorkoutImport: health.WorkoutImport{Source: "apple", Kind: "other", Activity: "Tennis", Minutes: 30, StartedAt: start}}
	b := importedWorkout{WorkoutImport: health.WorkoutImport{Source: "samsung", Kind: "other", Activity: "Rowing", Minutes: 30, StartedAt: start}}
	if sameImportedWorkout(a, b) {
		t.Fatal("unrelated unknown activity types matched")
	}
	b.Activity = " tennis "
	if !sameImportedWorkout(a, b) {
		t.Fatal("matching names and times should match")
	}
}

func TestWorkoutStravaCopyWithCustomTitle(t *testing.T) {
	start := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	a := importedWorkout{WorkoutImport: health.WorkoutImport{Source: "75hard", Kind: MapWorkoutKind("outdoor", "Lunch break"), Activity: "Lunch break", Notes: "Imported from Strava", Minutes: 60, StartedAt: start}}
	b := importedWorkout{WorkoutImport: health.WorkoutImport{Source: "strava", Kind: "walk", Activity: "Lunch break", Minutes: 20, StartedAt: start}}
	if !sameImportedWorkout(a, b) || !sameImportedWorkout(b, a) {
		t.Fatal("generic 75hard category hid the original Strava activity")
	}
}
