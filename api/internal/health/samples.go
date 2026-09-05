// Package health holds the multi-source import model: readings from Apple
// Health, Samsung Health, Strava, a webhook, or 75hard, and the rules that
// turn many readings into one value for the day.
package health

import (
	"strings"
	"time"
)

// Metrics a source can supply for a day.
const (
	MetricWeight    = "weight_kg"
	MetricBodyFat   = "body_fat_pct"
	MetricRestingHR = "resting_hr"
	MetricSleep     = "sleep_hours"
	MetricSteps     = "steps"
	MetricWater     = "water_ml"
)

// Metrics lists every metric a sample may carry, in column order.
var Metrics = []string{MetricWeight, MetricBodyFat, MetricRestingHR, MetricSleep, MetricSteps, MetricWater}

// ValidMetric reports whether m is a known metric.
func ValidMetric(m string) bool {
	for _, k := range Metrics {
		if k == m {
			return true
		}
	}
	return false
}

// Sources, in precedence order: when two sources report the same metric on
// the same day, the earlier one in this list is what the day shows. Apple
// and Samsung both aggregate a phone and a watch already; Strava only ever
// has workouts; 75hard is a hand-typed number from another app.
var Precedence = []string{"manual", "apple", "samsung", "webhook", "garmin", "strava", "75hard"}

// Rank returns the precedence of a source; unknown sources rank last.
func Rank(source string) int {
	for i, s := range Precedence {
		if s == source {
			return i
		}
	}
	return len(Precedence)
}

// Sample is one reading of one metric on one day from one source.
type Sample struct {
	Date   string
	Metric string
	Source string
	Value  float64
}

// WorkoutImport is a session as reported by a source, before dedup.
type WorkoutImport struct {
	Source     string
	ExternalID string
	Date       string
	Kind       string
	Activity   string
	Minutes    int
	Kcal       *float64
	DistanceKm *float64
	AvgHR      *int
	StartedAt  time.Time
	Notes      string
}

// SameWorkout reports whether two sessions are the same effort seen by two
// devices: they start within three minutes of each other and run for
// roughly the same time. A phone and a watch disagree by seconds; two
// different workouts an hour apart do not.
func SameWorkout(aStart time.Time, aMinutes int, bStart time.Time, bMinutes int) bool {
	if aStart.IsZero() || bStart.IsZero() {
		return false
	}
	d := aStart.Sub(bStart)
	if d < 0 {
		d = -d
	}
	if d > 3*time.Minute {
		return false
	}
	lo, hi := aMinutes, bMinutes
	if lo > hi {
		lo, hi = hi, lo
	}
	if lo == 0 {
		return hi <= 5
	}
	return float64(hi-lo)/float64(hi) <= 0.25
}

// KindFromActivity maps a free-text activity or a vendor type to a lifeai
// workout kind.
func KindFromActivity(raw string) string {
	a := strings.ToLower(raw)
	switch {
	case strings.Contains(a, "walk") || strings.Contains(a, "hik"):
		return "walk"
	case strings.Contains(a, "run") || strings.Contains(a, "jog"):
		return "run"
	case strings.Contains(a, "cycl") || strings.Contains(a, "bik") || strings.Contains(a, "ride") || strings.Contains(a, "spin"):
		return "cycle"
	case strings.Contains(a, "swim"):
		return "swim"
	case strings.Contains(a, "yoga") || strings.Contains(a, "pilates") || strings.Contains(a, "stretch") || strings.Contains(a, "flexib") || strings.Contains(a, "mobility"):
		return "yoga"
	case strings.Contains(a, "hiit") || strings.Contains(a, "interval") || strings.Contains(a, "crossfit") || strings.Contains(a, "circuit"):
		return "hiit"
	case strings.Contains(a, "strength") || strings.Contains(a, "weight") || strings.Contains(a, "lift") || strings.Contains(a, "functional") || strings.Contains(a, "core"):
		return "strength"
	case strings.Contains(a, "tennis") || strings.Contains(a, "soccer") || strings.Contains(a, "football") || strings.Contains(a, "basketball") || strings.Contains(a, "badminton") || strings.Contains(a, "squash") || strings.Contains(a, "golf") || strings.Contains(a, "cricket") || strings.Contains(a, "volleyball") || strings.Contains(a, "hockey"):
		return "sport"
	case strings.Contains(a, "elliptical") || strings.Contains(a, "rower") || strings.Contains(a, "rowing") || strings.Contains(a, "stair") || strings.Contains(a, "cardio") || strings.Contains(a, "dance") || strings.Contains(a, "mixed"):
		return "cardio"
	}
	return "other"
}
