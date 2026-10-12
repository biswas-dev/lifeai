package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/biswas-dev/lifeai/api/internal/health"
)

// A trainer session logged at the scheduled slot and Strava's recording of
// it (via 75hard: "Morning Workout", untyped, a few minutes later and longer)
// are one session, in whichever order they arrive.
func recordedGymSession() health.WorkoutImport {
	return health.WorkoutImport{Source: "75hard", ExternalID: "workout:77", Date: "2026-10-12", Kind: MapWorkoutKind("indoor", "Morning Workout"),
		Activity: "Morning Workout", Minutes: 82, StartedAt: time.Date(2026, 10, 12, 10, 34, 47, 0, time.UTC), Notes: "Imported from Strava"}
}

func trainerSchedule(c *client) {
	c.do("PUT", "/api/training/schedule", map[string]any{"fixed": []map[string]any{{"key": "trainer", "title": "Weight training with trainer", "kind": "strength", "days": []int{1, 5}, "start": "06:30", "end": "07:30"}}})
}

func TestStravaRecordingJoinsLoggedTrainerSession(t *testing.T) {
	s, h := newTestServer(t)
	c := signup(t, h, "gym-after@example.com")
	var uid int64
	s.db.QueryRow(`SELECT id FROM users WHERE email='gym-after@example.com'`).Scan(&uid)
	s.db.Exec(`UPDATE users SET timezone='America/Toronto' WHERE id=?`, uid)
	trainerSchedule(c)
	res, err := s.backfillSession(context.WithValue(context.Background(), UserIDKey, uid), uid, BackfillInput{Date: "2026-10-12", Exercises: []exerciseInput{{Slug: "goblet-squat"}}})
	if err != nil || res.Linked {
		t.Fatalf("backfill: %+v %v", res, err)
	}
	for i := 0; i < 2; i++ { // the nightly sync repeats
		if _, err := s.importWorkout(context.Background(), uid, recordedGymSession()); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := s.workoutsForDate(context.Background(), uid, "2026-10-12")
	if len(list) != 1 {
		t.Fatalf("recording stayed separate: %+v", list)
	}
	w := list[0]
	if w.ID != res.Workout.ID || w.Kind != "strength" || w.Source != "manual" || len(w.Exercises) != 1 || !hasSource(w, "75hard") || w.Minutes != 60 {
		t.Fatalf("merged session: %+v", w)
	}
}

func TestBackfillLinksImportedRecordingAndKeepsExercisesOnResync(t *testing.T) {
	s, h := newTestServer(t)
	c := signup(t, h, "gym-before@example.com")
	var uid int64
	s.db.QueryRow(`SELECT id FROM users WHERE email='gym-before@example.com'`).Scan(&uid)
	s.db.Exec(`UPDATE users SET timezone='America/Toronto' WHERE id=?`, uid)
	trainerSchedule(c)
	ctx := context.WithValue(context.Background(), UserIDKey, uid)
	if _, err := s.importWorkout(ctx, uid, recordedGymSession()); err != nil {
		t.Fatal(err)
	}
	res, err := s.backfillSession(ctx, uid, BackfillInput{Date: "2026-10-12", Exercises: []exerciseInput{{Slug: "goblet-squat", Sets: []setInput{{Weight: ptr(25.0)}}}}})
	if err != nil || !res.Linked {
		t.Fatalf("backfill did not link the recording: %+v %v", res, err)
	}
	if w := res.Workout; w.Minutes != 82 || *w.StartedAt != "2026-10-12T10:34:47Z" || w.Kind != "strength" || w.Activity != "Weight training with trainer" {
		t.Fatalf("measured timing not taken: %+v", w)
	}
	// The recording's identity now points at the trainer session; a resync
	// updates it rather than re-adding the copy or dropping the exercises.
	if _, err := s.importWorkout(ctx, uid, recordedGymSession()); err != nil {
		t.Fatal(err)
	}
	list, _ := s.workoutsForDate(ctx, uid, "2026-10-12")
	if len(list) != 1 || len(list[0].Exercises) != 1 || list[0].Minutes != 82 {
		t.Fatalf("after resync: %+v", list)
	}
	week, _ := s.weekTraining(ctx, uid, "2026-10-12")
	if week.Minutes != 82 {
		t.Fatalf("week counted %d minutes", week.Minutes)
	}
}

// An older imported row matching a newer logged session must not win the
// merge, since the exercises belong to the logged one.
func TestImportMergeKeepsRowWithExercises(t *testing.T) {
	s, h := newTestServer(t)
	c := signup(t, h, "keep-exercises@example.com")
	var uid int64
	s.db.QueryRow(`SELECT id FROM users WHERE email='keep-exercises@example.com'`).Scan(&uid)
	ctx := context.Background()
	rec := recordedGymSession()
	rec.StartedAt = time.Time{} // untimed at first, so it cannot match yet
	if _, err := s.importWorkout(ctx, uid, rec); err != nil {
		t.Fatal(err)
	}
	c.do("POST", "/api/workouts", map[string]any{"date": "2026-10-12", "kind": "strength", "activity": "Trainer", "minutes": 60, "started_at": "2026-10-12T10:30:00Z",
		"exercises": []map[string]any{{"slug": "step-up"}}})
	if _, err := s.importWorkout(ctx, uid, recordedGymSession()); err != nil {
		t.Fatal(err)
	}
	list, _ := s.workoutsForDate(ctx, uid, "2026-10-12")
	if len(list) != 1 || len(list[0].Exercises) != 1 || list[0].Source != "manual" {
		t.Fatalf("exercises lost in merge: %+v", list)
	}
}

func TestMergeWorkoutsEndpoint(t *testing.T) {
	s, h := newTestServer(t)
	c := signup(t, h, "merge@example.com")
	var uid int64
	s.db.QueryRow(`SELECT id FROM users WHERE email='merge@example.com'`).Scan(&uid)
	ctx := context.Background()
	// A walk recording never joins a strength session automatically.
	walk := health.WorkoutImport{Source: "75hard", ExternalID: "workout:5", Date: "2026-10-12", Kind: "walk", Activity: "Morning Walk", Minutes: 60, StartedAt: time.Date(2026, 10, 12, 10, 30, 0, 0, time.UTC), Notes: "Imported from Strava"}
	_, own := c.do("POST", "/api/workouts", map[string]any{"date": "2026-10-12", "kind": "strength", "activity": "Trainer", "minutes": 60, "started_at": "2026-10-12T10:30:00Z", "exercises": []map[string]any{{"slug": "step-up"}}})
	if _, err := s.importWorkout(ctx, uid, walk); err != nil {
		t.Fatal(err)
	}
	list, _ := s.workoutsForDate(ctx, uid, "2026-10-12")
	if len(list) != 2 {
		t.Fatalf("walk merged into strength: %+v", list)
	}
	other := list[0].ID
	if other == int64(own["id"].(float64)) {
		other = list[1].ID
	}
	path := fmt.Sprintf("/api/workouts/%d/merge", int64(own["id"].(float64)))
	if code, _ := c.do("POST", path, map[string]any{"merge_id": int64(own["id"].(float64))}); code != 400 {
		t.Fatalf("self merge: %d", code)
	}
	code, merged := c.do("POST", path, map[string]any{"merge_id": other, "use_timing": true})
	if code != 200 || merged["kind"] != "strength" || len(merged["exercises"].([]any)) != 1 || fmt.Sprint(merged["sources"]) != "[75hard]" {
		t.Fatalf("merge: %d %v", code, merged)
	}
	if _, err := s.importWorkout(ctx, uid, walk); err != nil {
		t.Fatal(err)
	}
	if list, _ = s.workoutsForDate(ctx, uid, "2026-10-12"); len(list) != 1 {
		t.Fatalf("merged copy came back on resync: %+v", list)
	}
}

func TestGenericWorkoutTitle(t *testing.T) {
	for title, want := range map[string]bool{"Morning Workout": true, "evening  weight training": true, "Workout": true, "Morning Walk": false, "Leg day workout": false} {
		if genericWorkoutTitle(title) != want {
			t.Errorf("%q: want %v", title, want)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func hasSource(w Workout, source string) bool {
	for _, s := range w.Sources {
		if s == source {
			return true
		}
	}
	return false
}
