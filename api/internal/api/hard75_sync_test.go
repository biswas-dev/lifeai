package api

import "testing"

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
