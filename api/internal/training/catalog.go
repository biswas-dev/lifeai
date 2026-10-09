// Package training holds the exercise catalog: names, the muscles each
// movement trains, and how to perform it well. The same catalog serves the
// web app's picker and form guides and the MCP tools, so a slug logged by an
// agent means exactly what it means on screen.
package training

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed exercises.json
var catalogJSON []byte

// Exercise is one movement in the catalog.
type Exercise struct {
	Slug     string   `json:"slug"`
	Name     string   `json:"name"`
	Aliases  []string `json:"aliases,omitempty"`
	Category string   `json:"category"`
	// Equipment is bodyweight or dumbbell.
	Equipment string `json:"equipment"`
	// Mode is how a set is recorded: weight_reps, reps or time.
	Mode string `json:"mode"`
	// PerSide marks movements done one side at a time; reps are per side.
	PerSide bool `json:"per_side,omitempty"`
	// Load says what a logged weight means: "per dumbbell" or "one dumbbell".
	Load      string   `json:"load,omitempty"`
	Primary   []string `json:"primary"`
	Secondary []string `json:"secondary"`
	// Demo names the public-domain Free Exercise DB entry the photos come
	// from; empty when there are none.
	Demo     string   `json:"demo,omitempty"`
	Images   []string `json:"images"`
	Summary  string   `json:"summary"`
	Steps    []string `json:"steps"`
	Cues     []string `json:"cues"`
	Mistakes []string `json:"mistakes"`
}

// Categories in display order.
var Categories = []string{"warmup", "legs", "push", "pull", "arms", "core"}

// Muscles the catalog uses; the web app's body map draws each one.
var Muscles = []string{
	"chest", "front-delts", "side-delts", "rear-delts", "biceps", "triceps", "forearms",
	"abs", "obliques", "lats", "upper-back", "traps", "lower-back",
	"glutes", "hip-flexors", "quads", "adductors", "hamstrings", "calves",
}

var (
	catalog []Exercise
	bySlug  map[string]Exercise
)

func init() {
	if err := load(catalogJSON); err != nil {
		panic(err)
	}
}

func load(raw []byte) error {
	var list []Exercise
	if err := json.Unmarshal(raw, &list); err != nil {
		return fmt.Errorf("training: catalog: %w", err)
	}
	known := map[string]bool{}
	for _, m := range Muscles {
		known[m] = true
	}
	cats := map[string]bool{}
	for _, c := range Categories {
		cats[c] = true
	}
	index := map[string]Exercise{}
	for i, e := range list {
		if e.Slug == "" || e.Name == "" || index[e.Slug].Slug != "" {
			return fmt.Errorf("training: catalog entry %d has a missing or duplicate slug %q", i, e.Slug)
		}
		if !cats[e.Category] {
			return fmt.Errorf("training: %s: unknown category %q", e.Slug, e.Category)
		}
		switch e.Mode {
		case "weight_reps", "reps", "time":
		default:
			return fmt.Errorf("training: %s: unknown mode %q", e.Slug, e.Mode)
		}
		for _, m := range append(append([]string{}, e.Primary...), e.Secondary...) {
			if !known[m] {
				return fmt.Errorf("training: %s: unknown muscle %q", e.Slug, m)
			}
		}
		if e.Aliases == nil {
			e.Aliases = []string{}
		}
		if e.Secondary == nil {
			e.Secondary = []string{}
		}
		e.Images = []string{}
		if e.Demo != "" {
			e.Images = []string{"/exercises/" + e.Slug + "-0.jpg", "/exercises/" + e.Slug + "-1.jpg"}
		}
		list[i] = e
		index[e.Slug] = e
	}
	catalog, bySlug = list, index
	return nil
}

// All returns the catalog in display order.
func All() []Exercise {
	out := make([]Exercise, len(catalog))
	copy(out, catalog)
	return out
}

// Get finds an exercise by slug.
func Get(slug string) (Exercise, bool) {
	e, ok := bySlug[strings.TrimSpace(strings.ToLower(slug))]
	return e, ok
}

// Search filters by free text (name or alias), category and muscle. Empty
// filters match everything; results keep catalog order.
func Search(q, category, muscle string) []Exercise {
	q = strings.ToLower(strings.TrimSpace(q))
	out := []Exercise{}
	for _, e := range catalog {
		if category != "" && e.Category != category {
			continue
		}
		if muscle != "" && !contains(e.Primary, muscle) && !contains(e.Secondary, muscle) {
			continue
		}
		if q != "" && !matches(e, q) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// Resolve maps a name an agent or person typed to a catalog slug, trying
// the slug, the name and the aliases. It returns "" when nothing matches.
func Resolve(name string) string {
	n := normal(name)
	if n == "" {
		return ""
	}
	if e, ok := bySlug[strings.ReplaceAll(n, " ", "-")]; ok {
		return e.Slug
	}
	// A name beats an alias: "Dumbbell deadlift" is an alias of the RDL, but
	// an exercise named exactly that elsewhere should win.
	alias := ""
	for _, e := range catalog {
		if normal(e.Name) == n {
			return e.Slug
		}
		for _, a := range e.Aliases {
			if alias == "" && normal(a) == n {
				alias = e.Slug
			}
		}
	}
	return alias
}

func matches(e Exercise, q string) bool {
	if strings.Contains(strings.ToLower(e.Name), q) || strings.Contains(e.Slug, q) {
		return true
	}
	for _, a := range e.Aliases {
		if strings.Contains(strings.ToLower(a), q) {
			return true
		}
	}
	return false
}

func normal(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("-", " ", "–", " ", "'", "", "’", "", "↔", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
