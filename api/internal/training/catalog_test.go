package training

import "testing"

func TestCatalogLoadsAndResolves(t *testing.T) {
	if len(All()) < 40 {
		t.Fatalf("catalog has %d exercises", len(All()))
	}
	e, ok := Get("goblet-squat")
	if !ok || e.Mode != "weight_reps" || e.Load != "one dumbbell" || len(e.Images) != 2 || e.Primary[0] != "quads" {
		t.Fatalf("goblet squat: %+v", e)
	}
	for in, want := range map[string]string{
		"Goblet squats":                           "goblet-squat",
		"incline chest press":                     "incline-dumbbell-press",
		"Dumbbell deadlift":                       "dumbbell-rdl",
		"cat cow":                                 "cat-cow",
		"Child's pose to cobra":                   "childs-pose-cobra",
		"Dumbbell lateral raise":                  "lateral-raise",
		"Bench-supported single-arm dumbbell row": "one-arm-dumbbell-row",
		"made up thing":                           "",
	} {
		if got := Resolve(in); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Search("", "core", "obliques"); len(got) < 3 {
		t.Fatalf("core/obliques: %d", len(got))
	}
	if got := Search("curl", "", ""); len(got) != 2 {
		t.Fatalf("curl search: %d", len(got))
	}
}

func TestCatalogRejectsBadEntries(t *testing.T) {
	defer func() { _ = load(catalogJSON) }()
	for _, raw := range []string{
		`[{"slug":"a","name":"A","category":"legs","mode":"reps","primary":["wings"]}]`,
		`[{"slug":"a","name":"A","category":"legs","mode":"reps","primary":[]},{"slug":"a","name":"B","category":"legs","mode":"reps","primary":[]}]`,
		`[{"slug":"a","name":"A","category":"cardio","mode":"reps","primary":[]}]`,
		`[{"slug":"a","name":"A","category":"legs","mode":"laps","primary":[]}]`,
	} {
		if err := load([]byte(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}
