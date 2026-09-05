package dates

import (
	"testing"
	"time"
)

func TestRangeInclusive(t *testing.T) {
	got := Range("2026-02-27", "2026-03-02")
	want := []string{"2026-02-27", "2026-02-28", "2026-03-01", "2026-03-02"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if Range("2026-03-02", "2026-03-01") != nil {
		t.Fatal("inverted range should be empty")
	}
}

func TestLocalCrossesMidnight(t *testing.T) {
	// 03:30 UTC on the 2nd is still the 1st in Los Angeles.
	utc := time.Date(2026, 3, 2, 3, 30, 0, 0, time.UTC)
	if got := Local(utc, LoadLocation("America/Los_Angeles")); got != "2026-03-01" {
		t.Fatalf("got %s", got)
	}
	if got := Local(utc, LoadLocation("not/a/zone")); got != "2026-03-02" {
		t.Fatalf("unknown zone should fall back to UTC, got %s", got)
	}
}

func TestDaysBetween(t *testing.T) {
	if DaysBetween("2026-01-01", "2026-01-31") != 30 {
		t.Fatal("wrong day count")
	}
	if AddDays("2026-12-31", 1) != "2027-01-01" {
		t.Fatal("wrong year rollover")
	}
	if Valid("2026-13-01") || !Valid("2026-12-01") {
		t.Fatal("validation wrong")
	}
}
