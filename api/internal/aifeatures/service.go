// Package aifeatures turns a go-ai chain into the app's AI features:
// estimating a meal from a photo, suggesting recipes, turning pasted text
// into a recipe, writing a weekly plan and a short daily coaching note.
//
// The prompts live here as constants, the parsing is defensive, and every
// result is validated against what the app can actually store — a model is
// asked for JSON, not trusted to produce it.
package aifeatures

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	ai "github.com/anchoo2kewl/go-ai"
)

// Feature names, used for the run ledger and the cache key.
const (
	FeatureFoodPhoto    = "food_photo"
	FeatureRecipes      = "recipes"
	FeatureRecipeImport = "recipe_import"
	FeaturePlan         = "plan"
	FeatureCoach        = "coach"
)

// Service performs the app's AI calls against a provider chain. Vision gets
// its own chain because a provider's cheapest text model and its vision model
// are rarely the same one.
type Service struct {
	chain       *ai.Chain
	visionChain *ai.Chain
}

// New builds a Service using one chain for everything. A nil chain is allowed
// and reports Enabled() == false.
func New(chain *ai.Chain) *Service { return &Service{chain: chain} }

// NewWithVision builds a Service with a separate chain for image requests.
func NewWithVision(chain, vision *ai.Chain) *Service {
	return &Service{chain: chain, visionChain: vision}
}

func (s *Service) vision() *ai.Chain {
	if s.visionChain != nil && s.visionChain.Len() > 0 {
		return s.visionChain
	}
	return s.chain
}

// Enabled reports whether any provider is configured.
func (s *Service) Enabled() bool { return s != nil && s.chain != nil && s.chain.Len() > 0 }

// Providers lists the configured chain, primary first.
func (s *Service) Providers() []string {
	if !s.Enabled() {
		return nil
	}
	return s.chain.Names()
}

// VisionProviders lists the chain used for photo analysis.
func (s *Service) VisionProviders() []string {
	if !s.Enabled() {
		return nil
	}
	return s.vision().Names()
}

// ErrDisabled is returned when a feature is called with no provider set up.
var ErrDisabled = fmt.Errorf("aifeatures: no AI provider configured")

// Meta describes how a result was produced, for the ledger.
type Meta struct {
	Provider   string
	Model      string
	TokensIn   int
	TokensOut  int
	Attempts   int
	InputHash  string
	ResultJSON string
}

// ---- food photo ----

const foodSystemPrompt = `You estimate the nutritional content of a meal from a photograph.

Reply with a single JSON object and nothing else:
{"name":"short dish name","items":[{"name":"ingredient","qty":number,"unit":"g|ml|piece|serving","kcal":number,"protein_g":number,"carbs_g":number,"fat_g":number,"confidence":0.0-1.0}],"notes":"one short caveat"}

Rules:
- Estimate portion sizes from visible cues: plate size, utensils, the depth of the pile.
- One entry per distinguishable component. Do not exceed 12.
- kcal must be roughly consistent with the macros (protein 4, carbs 4, fat 9 per gram).
- confidence reflects how sure you are of BOTH the identification and the portion.
- If the photo does not show food, return {"name":"","items":[],"notes":"no food visible"}.
- Never invent a component you cannot see. An incomplete honest answer beats a confident wrong one.`

// FoodItem is one estimated component of a meal.
type FoodItem struct {
	Name       string  `json:"name"`
	Qty        float64 `json:"qty"`
	Unit       string  `json:"unit"`
	Kcal       float64 `json:"kcal"`
	ProteinG   float64 `json:"protein_g"`
	CarbsG     float64 `json:"carbs_g"`
	FatG       float64 `json:"fat_g"`
	Confidence float64 `json:"confidence"`
}

// FoodEstimate is a meal estimated from a photo.
type FoodEstimate struct {
	Name  string     `json:"name"`
	Items []FoodItem `json:"items"`
	Notes string     `json:"notes"`

	// Totals, summed here rather than asked of the model.
	Kcal     float64 `json:"kcal"`
	ProteinG float64 `json:"protein_g"`
	CarbsG   float64 `json:"carbs_g"`
	FatG     float64 `json:"fat_g"`
}

const foodRetryNudge = `

Your previous reply filled in the example rather than the photograph. Look at
the image and give real components with real calories. Do not use the words
"short dish name" or "ingredient".`

const foodAttempts = 2

// EstimateFood identifies a meal from a photo and estimates its nutrition.
func (s *Service) EstimateFood(ctx context.Context, image []byte, mediaType, hint string) (*FoodEstimate, Meta, error) {
	if !s.Enabled() {
		return nil, Meta{}, ErrDisabled
	}
	prompt := "Estimate the food in this photo."
	if h := strings.TrimSpace(hint); h != "" {
		prompt += " The person says it is: " + h
	}

	var lastMeta Meta
	var lastErr error
	for attempt := 0; attempt < foodAttempts; attempt++ {
		system := foodSystemPrompt
		if attempt > 0 {
			system += foodRetryNudge
		}
		resp, err := s.vision().Complete(ctx, ai.Request{
			System:   system,
			Messages: []ai.Message{ai.UserImage(prompt, mediaTypeOr(mediaType), image)},
			// Reasoning models spend tokens thinking out of this budget.
			MaxTokens: 9000,
			JSON:      true,
		})
		if err != nil {
			return nil, Meta{}, err
		}
		est, err := readEstimate(resp.Text)
		lastMeta = meta(resp, hashBytes(image, hint), jsonString(est))
		if err == nil {
			return &est, lastMeta, nil
		}
		lastErr = err
	}
	return nil, lastMeta, lastErr
}

func readEstimate(text string) (FoodEstimate, error) {
	var est FoodEstimate
	if err := ai.ExtractJSON(text, &est); err != nil {
		return est, fmt.Errorf("aifeatures: could not read the estimate: %w", err)
	}
	clean := est.Items[:0]
	for _, item := range est.Items {
		if strings.TrimSpace(item.Name) == "" || item.Kcal < 0 || item.Kcal > 5000 {
			continue
		}
		item.Name = strings.TrimSpace(item.Name)
		if item.Unit == "" {
			item.Unit = "serving"
		}
		if item.Qty <= 0 {
			item.Qty = 1
		}
		clean = append(clean, item)
		if len(clean) == 12 {
			break
		}
	}
	est.Items = clean
	est.Kcal, est.ProteinG, est.CarbsG, est.FatG = 0, 0, 0, 0
	for _, item := range est.Items {
		est.Kcal += item.Kcal
		est.ProteinG += item.ProteinG
		est.CarbsG += item.CarbsG
		est.FatG += item.FatG
	}
	est.Name = strings.TrimSpace(est.Name)
	return est, usableEstimate(est)
}

// ErrNoEstimate means the model answered without actually estimating anything.
var ErrNoEstimate = errors.New("aifeatures: the model did not return an estimate")

var schemaEchoes = map[string]bool{"short dish name": true, "ingredient": true}

func usableEstimate(est FoodEstimate) error {
	if len(est.Items) == 0 {
		if note := strings.TrimSpace(est.Notes); note != "" {
			return fmt.Errorf("%w: %s", ErrNoEstimate, note)
		}
		return fmt.Errorf("%w: nothing recognisable in the photo", ErrNoEstimate)
	}
	if schemaEchoes[strings.ToLower(est.Name)] {
		return fmt.Errorf("%w: it returned the example instead of an estimate", ErrNoEstimate)
	}
	echoed := 0
	for _, item := range est.Items {
		if schemaEchoes[strings.ToLower(item.Name)] {
			echoed++
		}
	}
	if echoed == len(est.Items) {
		return fmt.Errorf("%w: it returned the example instead of an estimate", ErrNoEstimate)
	}
	if est.Kcal <= 0 {
		return fmt.Errorf("%w: every item came back at zero calories", ErrNoEstimate)
	}
	return nil
}

// ---- recipes ----

const recipeSystemPrompt = `You suggest recipes that fit a person's remaining calorie and macro budget for the day.

Reply with a single JSON object and nothing else:
{"recipes":[{"name":"...","summary":"one sentence","minutes":number,"servings":number,"kcal_per_serving":number,"protein_g":number,"carbs_g":number,"fat_g":number,"ingredients":["200g chicken breast","..."],"steps":["...","..."],"tags":["high-protein","quick"]}]}

Rules:
- Return at most 3 recipes.
- Use the ingredients the person has where they are given; say plainly if something extra is needed.
- Respect the stated remaining budget. If it is very small, suggest something small rather than exceeding it.
- Keep steps to 8 or fewer, each one sentence.
- Give real quantities, not "some" or "to taste", for anything that affects the calorie count.
- Macros are per serving.`

// Recipe is one suggestion, in the same shape the recipe library stores.
type Recipe struct {
	Name           string   `json:"name"`
	Summary        string   `json:"summary"`
	Minutes        int      `json:"minutes"`
	Servings       int      `json:"servings"`
	KcalPerServing float64  `json:"kcal_per_serving"`
	ProteinG       float64  `json:"protein_g"`
	CarbsG         float64  `json:"carbs_g"`
	FatG           float64  `json:"fat_g"`
	Ingredients    []string `json:"ingredients"`
	Steps          []string `json:"steps"`
	Tags           []string `json:"tags"`
}

// RecipeRequest is the context a suggestion is built from.
type RecipeRequest struct {
	RemainingKcal    float64
	RemainingProtein float64
	Ingredients      []string
	Preferences      string
	MealSlot         string
	Goals            string
	Image            []byte
	MediaType        string
}

// SuggestRecipes proposes meals that fit the day's remaining budget.
func (s *Service) SuggestRecipes(ctx context.Context, req RecipeRequest) ([]Recipe, Meta, error) {
	if !s.Enabled() {
		return nil, Meta{}, ErrDisabled
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Remaining today: %.0f kcal", req.RemainingKcal)
	if req.RemainingProtein > 0 {
		fmt.Fprintf(&b, ", %.0fg protein still to hit", req.RemainingProtein)
	}
	b.WriteString(".\n")
	if req.MealSlot != "" {
		fmt.Fprintf(&b, "This is for %s.\n", req.MealSlot)
	}
	if len(req.Ingredients) > 0 {
		fmt.Fprintf(&b, "Ingredients on hand: %s.\n", strings.Join(req.Ingredients, ", "))
	}
	if req.Image != nil {
		b.WriteString("A photo of the available ingredients is attached; work from what you can see in it.\n")
	}
	if p := strings.TrimSpace(req.Preferences); p != "" {
		fmt.Fprintf(&b, "Preferences and restrictions: %s\n", p)
	}
	if g := strings.TrimSpace(req.Goals); g != "" {
		fmt.Fprintf(&b, "Their goals: %s\n", g)
	}

	msg := ai.UserText(b.String())
	chain := s.chain
	if req.Image != nil {
		msg = ai.UserImage(b.String(), mediaTypeOr(req.MediaType), req.Image)
		chain = s.vision()
	}
	resp, err := chain.Complete(ctx, ai.Request{
		System:    recipeSystemPrompt,
		Messages:  []ai.Message{msg},
		MaxTokens: 4000,
		JSON:      true,
	})
	if err != nil {
		return nil, Meta{}, err
	}
	var parsed struct {
		Recipes []Recipe `json:"recipes"`
	}
	hash := hashBytes(req.Image, b.String())
	if err := ai.ExtractJSON(resp.Text, &parsed); err != nil {
		return nil, meta(resp, hash, ""), fmt.Errorf("aifeatures: could not read the recipes: %w", err)
	}
	out := cleanRecipes(parsed.Recipes, 3)
	return out, meta(resp, hash, jsonString(out)), nil
}

const importSystemPrompt = `You turn pasted recipe text (from a website, a book, a message) into structured data.

Reply with a single JSON object and nothing else:
{"name":"...","summary":"one sentence","minutes":number,"servings":number,"kcal_per_serving":number,"protein_g":number,"carbs_g":number,"fat_g":number,"ingredients":["200g chicken breast","..."],"steps":["...","..."],"tags":["..."]}

Rules:
- Keep the ingredient quantities exactly as written; do not convert units.
- If the text gives no nutrition, estimate per-serving kcal and macros from the ingredients and say so in the summary.
- If servings are not stated, infer from the quantities and default to 2.
- Tags are short lower-case words: cuisine, meal type, diet ("vegan", "high-protein", "quick").`

// ImportRecipe structures pasted recipe text.
func (s *Service) ImportRecipe(ctx context.Context, text string) (*Recipe, Meta, error) {
	if !s.Enabled() {
		return nil, Meta{}, ErrDisabled
	}
	text = strings.TrimSpace(text)
	resp, err := s.chain.Complete(ctx, ai.Request{
		System:    importSystemPrompt,
		Messages:  []ai.Message{ai.UserText(text)},
		MaxTokens: 4000,
		JSON:      true,
	})
	if err != nil {
		return nil, Meta{}, err
	}
	hash := hashBytes(nil, text)
	var r Recipe
	if err := ai.ExtractJSON(resp.Text, &r); err != nil {
		return nil, meta(resp, hash, ""), fmt.Errorf("aifeatures: could not read the recipe: %w", err)
	}
	out := cleanRecipes([]Recipe{r}, 1)
	if len(out) == 0 {
		return nil, meta(resp, hash, ""), errors.New("aifeatures: the text did not look like a recipe")
	}
	return &out[0], meta(resp, hash, jsonString(out[0])), nil
}

func cleanRecipes(in []Recipe, max int) []Recipe {
	out := make([]Recipe, 0, len(in))
	for _, r := range in {
		r.Name = strings.TrimSpace(r.Name)
		if r.Name == "" || len(r.Ingredients) == 0 {
			continue
		}
		if r.Servings <= 0 {
			r.Servings = 1
		}
		if r.Tags == nil {
			r.Tags = []string{}
		}
		for i, t := range r.Tags {
			r.Tags[i] = strings.ToLower(strings.TrimSpace(t))
		}
		out = append(out, r)
		if len(out) == max {
			break
		}
	}
	return out
}

// ---- plan ----

const planSystemPrompt = `You write a one-week training and nutrition plan for a person from their logged record and goals.

Reply with a single JSON object and nothing else:
{"summary":"two sentences on where they are and what this week focuses on","focus":"a short phrase","days":[{"day":"Mon","training":"the session, or rest","nutrition":"one line","note":"one short line"}],"tips":["...","..."]}

Rules:
- Exactly 7 day entries, Mon to Sun.
- Match the training to what they have actually been doing; do not prescribe a 6-day split to someone who logged one walk.
- Include at least one rest or easy day.
- Nutrition lines should reference their kcal and protein targets where given.
- At most 4 tips.
- General fitness guidance only. No diagnosis, no medical advice; if the record suggests a problem, say to see a professional.`

// PlanDay is one day of a generated plan.
type PlanDay struct {
	Day       string `json:"day"`
	Training  string `json:"training"`
	Nutrition string `json:"nutrition"`
	Note      string `json:"note"`
}

// Plan is a week of personalised guidance.
type Plan struct {
	Summary string    `json:"summary"`
	Focus   string    `json:"focus"`
	Days    []PlanDay `json:"days"`
	Tips    []string  `json:"tips"`
}

// History is what the plan and the note are built from — the person's actual
// logged record over a window.
type History struct {
	WindowDays      int
	DaysLogged      int
	AvgKcal         float64
	KcalTarget      int
	AvgProtein      float64
	ProteinTarget   int
	WorkoutCount    int
	WorkoutMinutes  int
	WorkoutKinds    map[string]int
	MeditationMin   int
	AvgSleep        float64
	AvgSteps        float64
	WeightFirst     float64
	WeightLatest    float64
	TargetWeight    float64
	RestingHRLatest int
	Goals           string
	TodayKcal       float64
	TodayProtein    float64
	TodayWorkoutMin int
	TodayNote       string
	HealthContext   string
}

// BuildPlan writes a week of training and nutrition guidance.
func (s *Service) BuildPlan(ctx context.Context, h History) (*Plan, Meta, error) {
	if !s.Enabled() {
		return nil, Meta{}, ErrDisabled
	}
	brief := RenderHistory(h)
	resp, err := s.chain.Complete(ctx, ai.Request{
		System:    planSystemPrompt,
		Messages:  []ai.Message{ai.UserText(brief)},
		MaxTokens: 4000,
		JSON:      true,
	})
	if err != nil {
		return nil, Meta{}, err
	}
	var plan Plan
	hash := hashBytes(nil, brief)
	if err := ai.ExtractJSON(resp.Text, &plan); err != nil {
		return nil, meta(resp, hash, ""), fmt.Errorf("aifeatures: could not read the plan: %w", err)
	}
	if len(plan.Days) > 7 {
		plan.Days = plan.Days[:7]
	}
	if len(plan.Tips) > 4 {
		plan.Tips = plan.Tips[:4]
	}
	return &plan, meta(resp, hash, jsonString(plan)), nil
}

// ---- coaching note ----

const coachSystemPrompt = `You write one short daily note for a person tracking their food, training and body metrics.

Reply with a single JSON object and nothing else:
{"note":"two or three sentences","tone":"encouraging|direct|celebratory"}

Rules:
- Speak to their actual record: name what is going well and the one thing worth attention today.
- Never invent a fact that is not in the summary you were given.
- No medical advice.
- Say it plainly. No exclamation marks, no motivational slogans.`

// CoachNote is the short daily message.
type CoachNote struct {
	Note string `json:"note"`
	Tone string `json:"tone"`
}

// DailyNote writes a short note about where the person stands today.
func (s *Service) DailyNote(ctx context.Context, h History) (*CoachNote, Meta, error) {
	if !s.Enabled() {
		return nil, Meta{}, ErrDisabled
	}
	brief := RenderHistory(h)
	resp, err := s.chain.Complete(ctx, ai.Request{
		System:    coachSystemPrompt,
		Messages:  []ai.Message{ai.UserText(brief)},
		MaxTokens: 3000,
		JSON:      true,
	})
	if err != nil {
		return nil, Meta{}, err
	}
	var note CoachNote
	hash := hashBytes(nil, brief)
	if err := ai.ExtractJSON(resp.Text, &note); err != nil {
		return nil, meta(resp, hash, ""), fmt.Errorf("aifeatures: could not read the note: %w", err)
	}
	note.Note = strings.TrimSpace(note.Note)
	if note.Note == "" {
		return nil, meta(resp, hash, ""), fmt.Errorf("aifeatures: empty note")
	}
	return &note, meta(resp, hash, jsonString(note)), nil
}

// ---- helpers ----

// RenderHistory turns the record into a compact prose brief. Models reason
// better over a short readable summary, and it keeps the token count down.
func RenderHistory(h History) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Over the last %d days they logged %d of them.\n", h.WindowDays, h.DaysLogged)
	if h.AvgKcal > 0 {
		fmt.Fprintf(&b, "Average intake on logged days: %.0f kcal", h.AvgKcal)
		if h.KcalTarget > 0 {
			fmt.Fprintf(&b, " against a target of %d", h.KcalTarget)
		}
		b.WriteString(".\n")
	}
	if h.AvgProtein > 0 {
		fmt.Fprintf(&b, "Average protein: %.0fg", h.AvgProtein)
		if h.ProteinTarget > 0 {
			fmt.Fprintf(&b, " against a target of %d", h.ProteinTarget)
		}
		b.WriteString(".\n")
	}
	if h.WorkoutCount > 0 {
		fmt.Fprintf(&b, "Training: %d sessions, %d minutes", h.WorkoutCount, h.WorkoutMinutes)
		if len(h.WorkoutKinds) > 0 {
			parts := make([]string, 0, len(h.WorkoutKinds))
			for k, n := range h.WorkoutKinds {
				parts = append(parts, fmt.Sprintf("%s x%d", k, n))
			}
			fmt.Fprintf(&b, " (%s)", strings.Join(parts, ", "))
		}
		b.WriteString(".\n")
	} else {
		b.WriteString("No training logged in the window.\n")
	}
	if h.MeditationMin > 0 {
		fmt.Fprintf(&b, "Meditation: %d minutes.\n", h.MeditationMin)
	}
	if h.AvgSleep > 0 {
		fmt.Fprintf(&b, "Average sleep: %.1f hours.\n", h.AvgSleep)
	}
	if h.AvgSteps > 0 {
		fmt.Fprintf(&b, "Average steps: %.0f.\n", h.AvgSteps)
	}
	if h.WeightLatest > 0 {
		fmt.Fprintf(&b, "Weight: %.1f kg now", h.WeightLatest)
		if h.WeightFirst > 0 && h.WeightFirst != h.WeightLatest {
			fmt.Fprintf(&b, ", %+.1f kg over the window", h.WeightLatest-h.WeightFirst)
		}
		if h.TargetWeight > 0 {
			fmt.Fprintf(&b, ", target %.1f kg", h.TargetWeight)
		}
		b.WriteString(".\n")
	}
	if h.RestingHRLatest > 0 {
		fmt.Fprintf(&b, "Latest resting heart rate: %d bpm.\n", h.RestingHRLatest)
	}
	fmt.Fprintf(&b, "Today so far: %.0f kcal, %.0fg protein, %d minutes of training.\n",
		h.TodayKcal, h.TodayProtein, h.TodayWorkoutMin)
	if n := strings.TrimSpace(h.TodayNote); n != "" {
		fmt.Fprintf(&b, "Their note for today: %s\n", n)
	}
	if g := strings.TrimSpace(h.Goals); g != "" {
		fmt.Fprintf(&b, "Their stated goals: %s\n", g)
	}
	if h.HealthContext != "" {
		fmt.Fprintf(&b, "Recorded health context (observations, not a diagnosis):\n%s\n", h.HealthContext)
	}
	return b.String()
}

func meta(resp ai.Response, hash, result string) Meta {
	return Meta{
		Provider:   resp.Provider,
		Model:      resp.Model,
		TokensIn:   resp.Usage.InputTokens,
		TokensOut:  resp.Usage.OutputTokens,
		Attempts:   resp.Attempts,
		InputHash:  hash,
		ResultJSON: result,
	}
}

func hashBytes(data []byte, text string) string {
	h := sha256.New()
	h.Write(data)
	h.Write([]byte{0})
	h.Write([]byte(text))
	return hex.EncodeToString(h.Sum(nil))
}

func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func mediaTypeOr(mt string) string {
	if mt == "" {
		return "image/jpeg"
	}
	return mt
}

// Chain exposes the text chain for one-off structured calls that do not
// warrant a feature of their own.
func (s *Service) Chain() *ai.Chain {
	if s == nil {
		return nil
	}
	return s.chain
}

// MetaFrom builds a Meta from a raw response.
func MetaFrom(resp ai.Response, hash string) Meta { return meta(resp, hash, "") }
