package api

import (
	"context"
	"strings"
	"testing"

	"github.com/biswas-dev/lifeai/api/internal/integrations/hard75"
)

func TestHard75CheckInsPreserveHabitsAndManualWater(t *testing.T) {
	s, handler := newTestServer(t)
	c := signup(t, handler, "checkins@example.com")
	var userID int64
	if err := s.db.QueryRow(`SELECT id FROM users WHERE email=?`, "checkins@example.com").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	water, pages := 128.0, 10.0
	day := &hard75.Day{ProgramID: 7, DayNumber: 1, Date: "2026-07-01", Entries: []hard75.Entry{
		{Key: "water", Title: "Drink water", Unit: "oz", Value: &water, Done: true},
		{Key: "reading", Title: "Read", Unit: "pages", Value: &pages, Done: true},
		{Key: "diet", Title: "Follow eating plan", Done: true},
	}}
	pull := func() {
		t.Helper()
		if err := s.syncer.importDay(context.Background(), userID, day, nil, &SyncSummary{}); err != nil {
			t.Fatal(err)
		}
	}
	pull()
	var ml int
	if err := s.db.QueryRow(`SELECT water_ml FROM days WHERE user_id=? AND on_date=?`, userID, day.Date).Scan(&ml); err != nil || ml != 3785 {
		t.Fatalf("water conversion: %d, %v", ml, err)
	}
	if code, _ := c.do("PATCH", "/api/days/"+day.Date, map[string]any{"water_ml": 2400}); code != 200 {
		t.Fatalf("manual water: %d", code)
	}
	pages = 12
	pull()
	if err := s.db.QueryRow(`SELECT water_ml FROM days WHERE user_id=? AND on_date=?`, userID, day.Date).Scan(&ml); err != nil || ml != 2400 {
		t.Fatalf("manual reading replaced: %d, %v", ml, err)
	}
	var count int
	var body string
	if err := s.db.QueryRow(`SELECT COUNT(*), MAX(body) FROM journal_entries WHERE user_id=? AND external_id='checkin:7:1'`, userID).Scan(&count, &body); err != nil {
		t.Fatal(err)
	}
	if count != 1 || !strings.Contains(body, "Read: 12 pages") || !strings.Contains(body, "Follow eating plan (completed)") {
		t.Fatalf("check-in missing or duplicated: %d %q", count, body)
	}
}

func TestMapWorkoutKind(t *testing.T) {
	cases := map[[2]string]string{
		{"outdoor", "Morning walk"}:    "walk",
		{"indoor", "Treadmill run"}:    "run",
		{"outdoor", "Bike ride"}:       "cycle",
		{"indoor", "Push day"}:         "strength",
		{"indoor", "Yoga flow"}:        "yoga",
		{"outdoor", ""}:                "cardio",
		{"indoor", ""}:                 "other",
		{"outdoor", "Tennis with Sam"}: "sport",
		{"indoor", "HIIT circuit"}:     "hiit",
	}
	for in, want := range cases {
		if got := MapWorkoutKind(in[0], in[1]); got != want {
			t.Errorf("MapWorkoutKind(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}
