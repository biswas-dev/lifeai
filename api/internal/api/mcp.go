package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	goapi "github.com/anchoo2kewl/go-api"
	gomcp "github.com/anchoo2kewl/go-mcp"
	"go.uber.org/zap"
	"go.uber.org/zap/exp/zapslog"

	"github.com/biswas-dev/lifeai/api/internal/auth"
	"github.com/biswas-dev/lifeai/api/internal/blood"
	"github.com/biswas-dev/lifeai/api/internal/dates"
	"github.com/biswas-dev/lifeai/api/internal/training"
	"github.com/biswas-dev/lifeai/api/internal/version"
)

// MCP over streamable HTTP, served from the same binary at /mcp by go-mcp.
//
// Every tool reads or writes the caller's own record and none of them calls
// a model: the point is to hand an agent the numbers so it can do the
// analysis itself, rather than paying for the same analysis twice. Tools
// call the same functions as the REST handlers, so a session logged by an
// agent is validated exactly like one logged in the app.

const mcpInstructions = "lifeai holds one person's food, training, body metrics, blood work, recipes, nutrition plan and journal. " +
	"Start with get_health_summary for the computed picture, then drill into days, stats, workouts, blood markers or recipes. " +
	"Dates are YYYY-MM-DD in the person's own timezone. For strength sessions, look up exercise slugs with list_exercises " +
	"and log sets with log_workout; weights are per dumbbell unless the exercise's load says one dumbbell."

// HandleMCP is the endpoint.
func (s *Server) HandleMCP(w http.ResponseWriter, r *http.Request) {
	s.mcpOnce.Do(func() { s.mcpHandler = s.newMCP().Handler() })
	s.mcpHandler.ServeHTTP(w, r)
}

func (s *Server) newMCP() *gomcp.Server {
	origins := append([]string{s.cfg.AppURL}, s.cfg.CORSAllowedOrigins...)
	srv := gomcp.New(gomcp.Options{
		Name: "lifeai", Version: version.Version, Instructions: mcpInstructions,
		AllowedOrigins: origins, Authenticate: s.mcpAuthenticate,
		Logger: zapslogLogger(s.log),
	})
	s.registerMCPTools(srv)
	return srv
}

func zapslogLogger(l *zap.Logger) *slog.Logger { return slog.New(zapslog.NewHandler(l.Core())) }

// mcpAuthenticate accepts a personal API token or a session JWT. A session
// may read and write; a token carries its own scopes.
func (s *Server) mcpAuthenticate(r *http.Request) (context.Context, gomcp.Principal, error) {
	credential, ok := gomcp.BearerToken(r)
	if !ok {
		return nil, gomcp.Principal{}, gomcp.Unauthorized("authorization required: send Authorization: Bearer <lifeai API token>")
	}
	if TokenScheme.Issued(credential) {
		userID, record, err := s.tokenAuthenticator().Authenticate(r.Context(), credential, http.MethodGet)
		if err != nil {
			return nil, gomcp.Principal{}, &gomcp.AuthError{Status: goapi.StatusFor(err), Message: goapi.PublicMessage(err)}
		}
		ctx := context.WithValue(r.Context(), UserIDKey, userID)
		ctx = context.WithValue(ctx, TokenScopeKey, record.Scopes)
		return ctx, gomcp.Principal{Subject: strconv.FormatInt(userID, 10), Scopes: record.Scopes}, nil
	}
	claims, err := auth.ValidateToken(credential, s.cfg.JWTSecret)
	if err != nil {
		return nil, gomcp.Principal{}, gomcp.Unauthorized("invalid or expired token")
	}
	var active bool
	if err := s.db.QueryRowContext(r.Context(), `SELECT deleted_at IS NULL FROM users WHERE id = ?`, claims.UserID).Scan(&active); err != nil || !active {
		return nil, gomcp.Principal{}, gomcp.Unauthorized("account unavailable")
	}
	ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
	return ctx, gomcp.Principal{Subject: strconv.FormatInt(claims.UserID, 10), Scopes: []string{gomcp.ScopeRead, gomcp.ScopeWrite}}, nil
}

func (s *Server) mcpDate(ctx context.Context, d string) (string, error) {
	d = strings.TrimSpace(d)
	if d == "" || d == "today" {
		return s.today(ctx), nil
	}
	if !dates.Valid(d) {
		return "", errors.New("date must be YYYY-MM-DD")
	}
	return d, nil
}

// asError turns a REST-layer error into the message an agent should read.
func asError(err error) error {
	var ie *inputError
	if errors.As(err, &ie) {
		return errors.New(ie.msg)
	}
	return err
}

// ---- tool inputs ----

type noInput struct{}

type dateInput struct {
	Date string `json:"date,omitempty" jsonschema:"YYYY-MM-DD, or 'today' (default)"`
}

type idInput struct {
	ID int64 `json:"id"`
}

type rangeInput struct {
	From string `json:"from,omitempty" jsonschema:"YYYY-MM-DD"`
	To   string `json:"to,omitempty" jsonschema:"YYYY-MM-DD, default today"`
}

type statsInput struct {
	Days int `json:"days,omitempty" jsonschema:"window length, 7 to 730 (default 90)"`
}

type goalsInput struct {
	DailyKcal            *int     `json:"daily_kcal,omitempty" jsonschema:"kcal per day"`
	ProteinG             *int     `json:"protein_g,omitempty" jsonschema:"grams per day"`
	CarbsG               *int     `json:"carbs_g,omitempty" jsonschema:"grams per day"`
	FatG                 *int     `json:"fat_g,omitempty" jsonschema:"grams per day"`
	TargetWeightKg       *float64 `json:"target_weight_kg,omitempty"`
	Steps                *int     `json:"steps,omitempty" jsonschema:"per day"`
	WaterMl              *int     `json:"water_ml,omitempty" jsonschema:"per day"`
	SleepHours           *float64 `json:"sleep_hours,omitempty"`
	WorkoutMinutes       *int     `json:"workout_minutes,omitempty" jsonschema:"exercise minutes per day"`
	WeeklyWorkoutMinutes *int     `json:"weekly_workout_minutes,omitempty" jsonschema:"exercise minutes per Monday-to-Sunday week, e.g. 300"`
	MeditationMinutes    *int     `json:"meditation_minutes,omitempty" jsonschema:"per day"`
	Notes                *string  `json:"notes,omitempty" jsonschema:"free text the coach reads"`
	Clear                []string `json:"clear,omitempty" jsonschema:"names of targets to remove, e.g. [\"steps\"]"`
}

type metricsInput struct {
	Date       string   `json:"date,omitempty" jsonschema:"YYYY-MM-DD, default today"`
	WeightKg   *float64 `json:"weight_kg,omitempty"`
	BodyFatPct *float64 `json:"body_fat_pct,omitempty"`
	RestingHR  *float64 `json:"resting_hr,omitempty"`
	SleepHours *float64 `json:"sleep_hours,omitempty"`
	Steps      *float64 `json:"steps,omitempty"`
	WaterMl    *float64 `json:"water_ml,omitempty"`
	Mood       *float64 `json:"mood,omitempty" jsonschema:"1-5"`
	Energy     *float64 `json:"energy,omitempty" jsonschema:"1-5"`
	Note       *string  `json:"note,omitempty"`
}

type mealInput struct {
	Date     string  `json:"date,omitempty" jsonschema:"YYYY-MM-DD, default today"`
	Name     string  `json:"name"`
	Slot     string  `json:"slot,omitempty" jsonschema:"breakfast, lunch, dinner or snack; default from the clock"`
	Kcal     float64 `json:"kcal"`
	ProteinG float64 `json:"protein_g,omitempty"`
	CarbsG   float64 `json:"carbs_g,omitempty"`
	FatG     float64 `json:"fat_g,omitempty"`
	Notes    string  `json:"notes,omitempty"`
	PlanKey  string  `json:"plan_key,omitempty" jsonschema:"meal_key:option_key from the nutrition plan when this is a planned meal, e.g. dinner:salmon"`
}

type workoutToolInput struct {
	Date       string          `json:"date,omitempty" jsonschema:"YYYY-MM-DD, default today"`
	Kind       string          `json:"kind,omitempty" jsonschema:"strength, cardio, walk, run, cycle, swim, yoga, hiit, sport or other; default strength when exercises are given"`
	Activity   string          `json:"activity,omitempty" jsonschema:"title, e.g. Trainer session — full body"`
	Minutes    int             `json:"minutes"`
	StartedAt  string          `json:"started_at,omitempty" jsonschema:"RFC3339 start time"`
	Kcal       *float64        `json:"kcal,omitempty"`
	DistanceKm *float64        `json:"distance_km,omitempty"`
	AvgHR      *int            `json:"avg_hr,omitempty"`
	Notes      string          `json:"notes,omitempty"`
	Exercises  []exerciseInput `json:"exercises,omitempty" jsonschema:"strength work in order"`
}

func (in workoutToolInput) toInput() workoutInput {
	out := workoutInput{Date: in.Date, Kind: in.Kind, Activity: in.Activity, Minutes: in.Minutes, Kcal: in.Kcal, DistanceKm: in.DistanceKm, AvgHR: in.AvgHR, Notes: in.Notes, Exercises: in.Exercises}
	if in.StartedAt != "" {
		out.StartedAt = &in.StartedAt
	}
	return out
}

type updateWorkoutInput struct {
	ID int64 `json:"id"`
	workoutToolInput
}

type listWorkoutsInput struct {
	From  string `json:"from,omitempty" jsonschema:"YYYY-MM-DD, default 90 days before to"`
	To    string `json:"to,omitempty" jsonschema:"YYYY-MM-DD, default today"`
	Kind  string `json:"kind,omitempty" jsonschema:"filter, e.g. strength"`
	Limit int    `json:"limit,omitempty" jsonschema:"default 100"`
}

type exerciseSearchInput struct {
	Q        string `json:"q,omitempty" jsonschema:"name or alias to search"`
	Category string `json:"category,omitempty" jsonschema:"warmup, legs, push, pull, arms or core"`
	Muscle   string `json:"muscle,omitempty" jsonschema:"e.g. chest, quads, side-delts"`
}

type exerciseHistoryInput struct {
	Slug  string `json:"slug,omitempty" jsonschema:"catalog slug"`
	Name  string `json:"name,omitempty" jsonschema:"for a custom exercise without a slug"`
	Limit int    `json:"limit,omitempty" jsonschema:"sessions, default 10"`
}

type meditationInput struct {
	Date    string `json:"date,omitempty"`
	Minutes int    `json:"minutes"`
	Style   string `json:"style,omitempty" jsonschema:"guided, unguided, breathwork, body_scan, walking or other"`
	Notes   string `json:"notes,omitempty"`
}

type journalInput struct {
	Date  string `json:"date,omitempty"`
	Title string `json:"title,omitempty"`
	Body  string `json:"body"`
}

type listJournalInput struct {
	Q     string `json:"q,omitempty" jsonschema:"search text"`
	Limit int    `json:"limit,omitempty" jsonschema:"default 50"`
}

type listPhotosInput struct {
	Kind  string `json:"kind,omitempty" jsonschema:"progress, food or ingredients"`
	Date  string `json:"date,omitempty" jsonschema:"YYYY-MM-DD"`
	Limit int    `json:"limit,omitempty" jsonschema:"1-100; default 30"`
}

type getPhotoInput struct {
	ID       int64 `json:"id" jsonschema:"photo id"`
	FullSize bool  `json:"full_size,omitempty" jsonschema:"use the stored full image instead of the thumbnail"`
}

type listRecipesInput struct {
	Q         string `json:"q,omitempty"`
	Tag       string `json:"tag,omitempty"`
	Favourite bool   `json:"favourite,omitempty"`
}

type addRecipeInput struct {
	Name           string   `json:"name"`
	Summary        string   `json:"summary,omitempty"`
	Minutes        int      `json:"minutes,omitempty"`
	Servings       int      `json:"servings,omitempty"`
	KcalPerServing float64  `json:"kcal_per_serving,omitempty"`
	ProteinG       float64  `json:"protein_g,omitempty"`
	CarbsG         float64  `json:"carbs_g,omitempty"`
	FatG           float64  `json:"fat_g,omitempty"`
	Ingredients    []string `json:"ingredients"`
	Steps          []string `json:"steps,omitempty"`
	Tags           []string `json:"tags,omitempty"`
}

type cookInput struct {
	ID       int64   `json:"id"`
	Date     string  `json:"date,omitempty"`
	Slot     string  `json:"slot,omitempty"`
	Servings float64 `json:"servings,omitempty" jsonschema:"default 1"`
}

type markerFilterInput struct {
	Code     string `json:"code,omitempty" jsonschema:"e.g. hba1c, ldl, alt"`
	Category string `json:"category,omitempty" jsonschema:"sugar, lipids, liver, kidney, thyroid or blood"`
}

type bloodReportInput struct {
	TakenOn string               `json:"taken_on" jsonschema:"YYYY-MM-DD"`
	Lab     string               `json:"lab,omitempty"`
	Notes   string               `json:"notes,omitempty"`
	Markers []bloodMarkerPayload `json:"markers"`
}

// ---- tools ----

func (s *Server) registerMCPTools(srv *gomcp.Server) {
	uid := UserID

	gomcp.Register(srv, gomcp.Tool{Name: "get_health_summary", Description: "The computed picture: profile with BMI, goals, latest blood markers (watch list and anything out of range), 30- and 90-day training and nutrition stats, today, and plain-language signals. Start here."},
		func(ctx context.Context, _ noInput) (HealthSummary, error) { return s.healthSummary(ctx, uid(ctx)) })

	gomcp.Register(srv, gomcp.Tool{Name: "get_profile", Description: "Name, email, timezone, date of birth, sex, height and unit preference."},
		func(ctx context.Context, _ noInput) (any, error) { return s.getUser(ctx, uid(ctx)) })

	gomcp.Register(srv, gomcp.Tool{Name: "get_goals", Description: "Daily calorie and macro targets, target weight, steps, water, sleep, daily and weekly exercise minutes, and the person's own notes on what they are working toward."},
		func(ctx context.Context, _ noInput) (Goals, error) { return s.goals(ctx, uid(ctx)) })

	gomcp.Register(srv, gomcp.Tool{Name: "set_goals", Description: "Change targets. Only the fields given change; list names in clear to remove a target.", Write: true, Idempotent: true},
		func(ctx context.Context, in goalsInput) (Goals, error) {
			g, err := s.goals(ctx, uid(ctx))
			if err != nil {
				return g, err
			}
			set := func(dst **int, v *int) {
				if v != nil {
					*dst = v
				}
			}
			set(&g.DailyKcal, in.DailyKcal)
			set(&g.ProteinG, in.ProteinG)
			set(&g.CarbsG, in.CarbsG)
			set(&g.FatG, in.FatG)
			set(&g.Steps, in.Steps)
			set(&g.WaterMl, in.WaterMl)
			set(&g.WorkoutMinutes, in.WorkoutMinutes)
			set(&g.WeeklyWorkoutMinutes, in.WeeklyWorkoutMinutes)
			set(&g.MeditationMinutes, in.MeditationMinutes)
			if in.TargetWeightKg != nil {
				g.TargetWeightKg = in.TargetWeightKg
			}
			if in.SleepHours != nil {
				g.SleepHours = in.SleepHours
			}
			if in.Notes != nil {
				g.Notes = *in.Notes
			}
			for _, name := range in.Clear {
				switch name {
				case "daily_kcal":
					g.DailyKcal = nil
				case "protein_g":
					g.ProteinG = nil
				case "carbs_g":
					g.CarbsG = nil
				case "fat_g":
					g.FatG = nil
				case "target_weight_kg":
					g.TargetWeightKg = nil
				case "steps":
					g.Steps = nil
				case "water_ml":
					g.WaterMl = nil
				case "sleep_hours":
					g.SleepHours = nil
				case "workout_minutes":
					g.WorkoutMinutes = nil
				case "weekly_workout_minutes":
					g.WeeklyWorkoutMinutes = nil
				case "meditation_minutes":
					g.MeditationMinutes = nil
				case "notes":
					g.Notes = ""
				default:
					return g, fmt.Errorf("cannot clear %q", name)
				}
			}
			saved, err := s.saveGoals(ctx, uid(ctx), g)
			return saved, asError(err)
		})

	gomcp.Register(srv, gomcp.Tool{Name: "get_day", Description: "Everything logged on one date: body metrics, meals with items, workouts with exercises and sets, meditation, journal, photos, totals against goals, and the week's exercise minutes."},
		func(ctx context.Context, in dateInput) (Day, error) {
			d, err := s.mcpDate(ctx, in.Date)
			if err != nil {
				return Day{}, err
			}
			return s.loadDay(ctx, uid(ctx), d)
		})

	gomcp.Register(srv, gomcp.Tool{Name: "list_days", Description: "One summary row per date in a range (kcal, protein, weight, training minutes, steps, sleep, mood). Default: the last 30 days."},
		func(ctx context.Context, in rangeInput) ([]DaySummary, error) {
			to := in.To
			if to == "" {
				to = s.today(ctx)
			}
			from := in.From
			if from == "" {
				from = dates.AddDays(to, -29)
			}
			if !dates.Valid(from) || !dates.Valid(to) {
				return nil, errors.New("from and to must be YYYY-MM-DD")
			}
			if dates.DaysBetween(from, to) > 400 {
				return nil, errors.New("range too large (400 days max)")
			}
			return s.daySummaries(ctx, uid(ctx), from, to)
		})

	gomcp.Register(srv, gomcp.Tool{Name: "get_stats", Description: "Trends over a window: weight, body fat, resting HR, sleep, steps, kcal, protein, training series with averages, streak and calorie adherence."},
		func(ctx context.Context, in statsInput) (any, error) {
			days := in.Days
			if days < 7 || days > 730 {
				days = 90
			}
			return s.stats(ctx, uid(ctx), days)
		})

	gomcp.Register(srv, gomcp.Tool{Name: "log_metrics", Description: "Record body metrics for a date. Only the fields given change.", Write: true, Idempotent: true},
		func(ctx context.Context, in metricsInput) (Day, error) {
			userID := uid(ctx)
			d, err := s.mcpDate(ctx, in.Date)
			if err != nil {
				return Day{}, err
			}
			if err := s.ensureDay(ctx, userID, d); err != nil {
				return Day{}, err
			}
			var touched []string
			for col, v := range map[string]*float64{"weight_kg": in.WeightKg, "body_fat_pct": in.BodyFatPct, "resting_hr": in.RestingHR, "sleep_hours": in.SleepHours,
				"steps": in.Steps, "water_ml": in.WaterMl, "mood": in.Mood, "energy": in.Energy} {
				if v == nil {
					continue
				}
				if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`UPDATE days SET %s = ?, source = 'manual', updated_at = CURRENT_TIMESTAMP WHERE user_id = ? AND on_date = ?`, col), *v, userID, d); err != nil {
					return Day{}, err
				}
				touched = append(touched, col)
			}
			if in.Note != nil {
				if _, err := s.db.ExecContext(ctx, `UPDATE days SET note = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ? AND on_date = ?`, strings.TrimSpace(*in.Note), userID, d); err != nil {
					return Day{}, err
				}
			}
			if len(touched) > 0 {
				_ = s.markManual(ctx, userID, d, touched)
			}
			return s.loadDay(ctx, userID, d)
		})

	gomcp.Register(srv, gomcp.Tool{Name: "log_meal", Description: "Log a meal with its calories and macros. Set plan_key to tick off a meal from the nutrition plan.", Write: true},
		func(ctx context.Context, in mealInput) (Meal, error) {
			userID := uid(ctx)
			d, err := s.mcpDate(ctx, in.Date)
			if err != nil {
				return Meal{}, err
			}
			slot := strings.ToLower(strings.TrimSpace(in.Slot))
			if !validSlot(slot) {
				slot = slotForTime(time.Now().In(s.userLocation(ctx)))
			}
			if err := s.ensureDay(ctx, userID, d); err != nil {
				return Meal{}, err
			}
			res, err := s.db.ExecContext(ctx, `INSERT INTO meals (user_id, on_date, name, slot, kcal, protein_g, carbs_g, fat_g, source, notes, plan_key) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'manual', ?, ?)`,
				userID, d, strings.TrimSpace(in.Name), slot, in.Kcal, in.ProteinG, in.CarbsG, in.FatG, strings.TrimSpace(in.Notes), strings.TrimSpace(in.PlanKey))
			if err != nil {
				return Meal{}, err
			}
			id, _ := res.LastInsertId()
			return s.mealByID(ctx, userID, id)
		})

	gomcp.Register(srv, gomcp.Tool{Name: "log_workout", Description: "Log a training session. For strength work include exercises with their sets (catalog slugs from list_exercises; a recognised name is matched automatically).", Write: true},
		func(ctx context.Context, in workoutToolInput) (Workout, error) {
			wk, err := s.saveWorkout(ctx, uid(ctx), 0, in.toInput())
			return wk, asError(err)
		})

	gomcp.Register(srv, gomcp.Tool{Name: "backfill_session", Description: "Fill in a training session after the fact, e.g. from a recording or transcript of a trainer session. " +
		"Updates the session already logged that day for the fixed slot (or of its kind), otherwise creates it with the slot's time and length. " +
		"Match each movement to a catalog slug (list_exercises), keep the order performed, give weights in the unit used (lb by default; per dumbbell unless the exercise's load says one dumbbell), " +
		"and put anything uncertain in the exercise notes rather than guessing numbers.", Write: true, Idempotent: true},
		func(ctx context.Context, in BackfillInput) (BackfillResult, error) {
			res, err := s.backfillSession(ctx, uid(ctx), in)
			return res, asError(err)
		})

	gomcp.Register(srv, gomcp.Tool{Name: "update_workout", Description: "Replace a session's details and exercises. Send the whole session: omitted exercises are removed.", Write: true, Destructive: true, Idempotent: true},
		func(ctx context.Context, in updateWorkoutInput) (Workout, error) {
			wk, err := s.saveWorkout(ctx, uid(ctx), in.ID, in.toInput())
			return wk, asError(err)
		})

	gomcp.Register(srv, gomcp.Tool{Name: "delete_workout", Description: "Delete a session. One imported from Strava or 75hard may return on the next sync.", Write: true, Destructive: true, Idempotent: true},
		func(ctx context.Context, in idInput) (map[string]any, error) {
			res, err := s.db.ExecContext(ctx, `DELETE FROM workouts WHERE id = ? AND user_id = ?`, in.ID, uid(ctx))
			if err != nil {
				return nil, err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return nil, errors.New("workout not found")
			}
			return map[string]any{"deleted": in.ID}, nil
		})

	gomcp.Register(srv, gomcp.Tool{Name: "get_workout", Description: "One session with its exercises and sets."},
		func(ctx context.Context, in idInput) (Workout, error) {
			wk, err := s.workoutByID(ctx, uid(ctx), in.ID)
			if err != nil {
				return wk, errors.New("workout not found")
			}
			return wk, nil
		})

	gomcp.Register(srv, gomcp.Tool{Name: "list_workouts", Description: "Sessions newest first, with exercises and sets. Filter by date range and kind."},
		func(ctx context.Context, in listWorkoutsInput) ([]Workout, error) {
			out, err := s.listWorkouts(ctx, uid(ctx), in.From, in.To, in.Kind, in.Limit)
			return out, asError(err)
		})

	gomcp.Register(srv, gomcp.Tool{Name: "get_week_training", Description: "Exercise minutes for the Monday-to-Sunday week holding a date, per day, against the weekly goal; the fixed sessions from the schedule with done/today/upcoming/missed; and the freeform minutes still needed."},
		func(ctx context.Context, in dateInput) (WeekTraining, error) {
			d, err := s.mcpDate(ctx, in.Date)
			if err != nil {
				return WeekTraining{}, err
			}
			return s.weekTraining(ctx, uid(ctx), d)
		})

	gomcp.Register(srv, gomcp.Tool{Name: "get_training_schedule", Description: "The weekly training schedule: fixed sessions (title, kind, weekdays, start and end) and notes on the freeform part. Null when none is set. get_week_training shows which fixed sessions are done."},
		func(ctx context.Context, _ noInput) (map[string]any, error) {
			sc, err := s.trainingSchedule(ctx, uid(ctx))
			return map[string]any{"schedule": sc}, err
		})

	gomcp.Register(srv, gomcp.Tool{Name: "set_training_schedule", Description: "Replace the weekly training schedule. A logged workout of the slot's kind on a scheduled day counts as that fixed session.", Write: true, Destructive: true, Idempotent: true},
		func(ctx context.Context, in TrainingSchedule) (map[string]any, error) {
			sc, err := s.saveTrainingSchedule(ctx, uid(ctx), in)
			return map[string]any{"schedule": sc}, asError(err)
		})

	gomcp.Register(srv, gomcp.Tool{Name: "list_exercises", Description: "The exercise catalog: slug, name, category, muscles trained, how a set is recorded (weight_reps, reps or time) and what a logged weight means. Filter by text, category or muscle."},
		func(_ context.Context, in exerciseSearchInput) ([]map[string]any, error) {
			out := []map[string]any{}
			for _, e := range training.Search(in.Q, in.Category, in.Muscle) {
				out = append(out, map[string]any{"slug": e.Slug, "name": e.Name, "category": e.Category, "equipment": e.Equipment, "mode": e.Mode,
					"per_side": e.PerSide, "load": e.Load, "primary": e.Primary, "secondary": e.Secondary})
			}
			return out, nil
		})

	gomcp.Register(srv, gomcp.Tool{Name: "get_exercise", Description: "One exercise in full: muscles, step-by-step form, cues and common mistakes."},
		func(_ context.Context, in struct {
			Slug string `json:"slug"`
		}) (training.Exercise, error) {
			e, ok := training.Get(in.Slug)
			if !ok {
				if slug := training.Resolve(in.Slug); slug != "" {
					e, ok = training.Get(slug)
				}
			}
			if !ok {
				return e, fmt.Errorf("unknown exercise %q", in.Slug)
			}
			return e, nil
		})

	gomcp.Register(srv, gomcp.Tool{Name: "get_exercise_history", Description: "Recent sessions of one exercise, newest first, with every set: for progress and choosing the next weight."},
		func(ctx context.Context, in exerciseHistoryInput) ([]ExerciseHistory, error) {
			slug := in.Slug
			if slug == "" {
				slug = training.Resolve(in.Name)
			}
			return s.exerciseHistory(ctx, uid(ctx), slug, in.Name, in.Limit)
		})

	gomcp.Register(srv, gomcp.Tool{Name: "push_workout_to_strava", Description: "Post a session to the connected Strava account (exercises become the description), or update the activity it is already linked to.", Write: true, Idempotent: true},
		func(ctx context.Context, in idInput) (StravaUpload, error) {
			up, err := s.pushWorkoutToStrava(ctx, uid(ctx), in.ID)
			if errors.Is(err, errStravaNotConnected) {
				return up, errors.New("Strava is not connected; connect it in lifeai Settings")
			}
			return up, err
		})

	gomcp.Register(srv, gomcp.Tool{Name: "get_nutrition_plan", Description: "The nutrition plan: eating window, daily targets and planned meals with options and their weekdays. Null when none is set."},
		func(ctx context.Context, _ noInput) (map[string]any, error) {
			p, err := s.nutritionPlan(ctx, uid(ctx))
			return map[string]any{"plan": p}, err
		})

	gomcp.Register(srv, gomcp.Tool{Name: "set_nutrition_plan", Description: "Replace the nutrition plan.", Write: true, Destructive: true, Idempotent: true},
		func(ctx context.Context, in NutritionPlan) (map[string]any, error) {
			p, err := s.saveNutritionPlan(ctx, uid(ctx), in)
			return map[string]any{"plan": p}, asError(err)
		})

	gomcp.Register(srv, gomcp.Tool{Name: "log_meditation", Description: "Log a meditation sitting.", Write: true},
		func(ctx context.Context, in meditationInput) (map[string]any, error) {
			d, err := s.mcpDate(ctx, in.Date)
			if err != nil {
				return nil, err
			}
			if in.Minutes <= 0 {
				return nil, errors.New("minutes must be positive")
			}
			if err := s.ensureDay(ctx, uid(ctx), d); err != nil {
				return nil, err
			}
			style := strings.ToLower(strings.TrimSpace(in.Style))
			if style == "" {
				style = "guided"
			}
			if _, err := s.db.ExecContext(ctx, `INSERT INTO meditations (user_id, on_date, minutes, style, notes, started_at) VALUES (?, ?, ?, ?, ?, ?)`, uid(ctx), d, in.Minutes, style, strings.TrimSpace(in.Notes), time.Now().UTC()); err != nil {
				return nil, err
			}
			return map[string]any{"ok": true, "date": d, "minutes": in.Minutes}, nil
		})

	gomcp.Register(srv, gomcp.Tool{Name: "add_journal", Description: "Write a journal entry against a date.", Write: true},
		func(ctx context.Context, in journalInput) (JournalEntry, error) {
			d, err := s.mcpDate(ctx, in.Date)
			if err != nil {
				return JournalEntry{}, err
			}
			body := strings.TrimSpace(in.Body)
			if body == "" {
				return JournalEntry{}, errors.New("body is required")
			}
			if err := s.ensureDay(ctx, uid(ctx), d); err != nil {
				return JournalEntry{}, err
			}
			res, err := s.db.ExecContext(ctx, `INSERT INTO journal_entries (user_id, on_date, title, body) VALUES (?, ?, ?, ?)`, uid(ctx), d, strings.TrimSpace(in.Title), body)
			if err != nil {
				return JournalEntry{}, err
			}
			id, _ := res.LastInsertId()
			return scanJournal(s.db.QueryRowContext(ctx, `SELECT `+journalColumns+` FROM journal_entries WHERE id = ?`, id))
		})

	gomcp.Register(srv, gomcp.Tool{Name: "list_journal", Description: "Journal entries, newest first, optionally searched."},
		func(ctx context.Context, in listJournalInput) ([]JournalEntry, error) {
			limit := in.Limit
			if limit <= 0 || limit > 500 {
				limit = 50
			}
			query := `SELECT ` + journalColumns + ` FROM journal_entries WHERE user_id = ?`
			a := []any{uid(ctx)}
			if q := strings.ToLower(strings.TrimSpace(in.Q)); q != "" {
				query += ` AND (lower(title) LIKE ? OR lower(body) LIKE ?)`
				a = append(a, "%"+q+"%", "%"+q+"%")
			}
			query += ` ORDER BY on_date DESC, id DESC LIMIT ?`
			rows, err := s.db.QueryContext(ctx, query, append(a, limit)...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := []JournalEntry{}
			for rows.Next() {
				if e, err := scanJournal(rows); err == nil {
					out = append(out, e)
				}
			}
			return out, rows.Err()
		})

	gomcp.Register(srv, gomcp.Tool{Name: "list_photos", Description: "List your meal, ingredient and progress photos, newest first. Get an image with get_photo for your own visual analysis; no app model calls."},
		func(ctx context.Context, in listPhotosInput) ([]Photo, error) {
			query := `SELECT ` + photoColumns + ` FROM photos WHERE user_id = ?`
			values := []any{uid(ctx)}
			if in.Kind != "" {
				query += ` AND kind = ?`
				values = append(values, in.Kind)
			}
			if in.Date != "" {
				if !dates.Valid(in.Date) {
					return nil, errors.New("date must be YYYY-MM-DD")
				}
				query += ` AND on_date = ?`
				values = append(values, in.Date)
			}
			limit := in.Limit
			if limit == 0 {
				limit = 30
			}
			if limit < 1 || limit > 100 {
				return nil, errors.New("limit must be 1-100")
			}
			rows, err := s.db.QueryContext(ctx, query+` ORDER BY on_date DESC,id DESC LIMIT ?`, append(values, limit)...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := []Photo{}
			for rows.Next() {
				p, err := scanPhoto(rows)
				if err != nil {
					return nil, err
				}
				out = append(out, p)
			}
			return out, rows.Err()
		})

	gomcp.Register(srv, gomcp.Tool{Name: "get_photo", Description: "Read one of your photos as image content for visual analysis by your agent. Default is a small thumbnail to save tokens; set full_size for detail."},
		func(ctx context.Context, in getPhotoInput) (gomcp.Result, error) {
			if in.ID < 1 {
				return gomcp.Result{}, errors.New("a positive photo id is required")
			}
			var path, thumb, mime string
			if err := s.db.QueryRowContext(ctx, `SELECT rel_path,thumb_path,mime FROM photos WHERE id=? AND user_id=?`, in.ID, uid(ctx)).Scan(&path, &thumb, &mime); err != nil {
				return gomcp.Result{}, errors.New("photo not found")
			}
			if !in.FullSize && thumb != "" {
				path = thumb
			}
			f, err := s.photos.Open(path)
			if err != nil {
				return gomcp.Result{}, errors.New("photo unavailable")
			}
			defer f.Close()
			b, err := io.ReadAll(io.LimitReader(f, (16<<20)+1))
			if err != nil {
				return gomcp.Result{}, err
			}
			if len(b) > 16<<20 {
				return gomcp.Result{}, errors.New("photo too large")
			}
			return gomcp.Image(b, mime), nil
		})

	gomcp.Register(srv, gomcp.Tool{Name: "list_recipes", Description: "The recipe library with per-serving macros. Filter by search text, tag or favourites."},
		func(ctx context.Context, in listRecipesInput) ([]Recipe, error) {
			query := `SELECT ` + recipeColumns + ` FROM recipes WHERE user_id = ?`
			a := []any{uid(ctx)}
			if q := strings.ToLower(strings.TrimSpace(in.Q)); q != "" {
				query += ` AND (lower(name) LIKE ? OR lower(summary) LIKE ? OR lower(ingredients_json) LIKE ? OR tags LIKE ?)`
				like := "%" + q + "%"
				a = append(a, like, like, like, like)
			}
			if tag := strings.ToLower(strings.TrimSpace(in.Tag)); tag != "" {
				query += ` AND (',' || tags || ',') LIKE ?`
				a = append(a, "%,"+tag+",%")
			}
			if in.Favourite {
				query += ` AND favourite = 1`
			}
			rows, err := s.db.QueryContext(ctx, query+` ORDER BY favourite DESC, updated_at DESC LIMIT 300`, a...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			out := []Recipe{}
			for rows.Next() {
				if rc, err := scanRecipe(rows); err == nil {
					out = append(out, rc)
				}
			}
			return out, rows.Err()
		})

	gomcp.Register(srv, gomcp.Tool{Name: "get_recipe", Description: "One recipe in full: ingredients, steps, macros."},
		func(ctx context.Context, in idInput) (any, error) { return s.recipeByID(ctx, uid(ctx), in.ID) })

	gomcp.Register(srv, gomcp.Tool{Name: "add_recipe", Description: "Add a recipe to the library. Macros are per serving.", Write: true},
		func(ctx context.Context, in addRecipeInput) (any, error) {
			req := recipeRequest{Name: strings.TrimSpace(in.Name), Summary: strings.TrimSpace(in.Summary), Ingredients: in.Ingredients, Steps: in.Steps, Tags: in.Tags, Source: "import",
				Minutes: in.Minutes, Servings: in.Servings, KcalPerServing: in.KcalPerServing, ProteinG: in.ProteinG, CarbsG: in.CarbsG, FatG: in.FatG}
			if msg, _ := req.validate(); msg != "" {
				return nil, errors.New(msg)
			}
			ing, _ := json.Marshal(req.Ingredients)
			steps, _ := json.Marshal(req.Steps)
			res, err := s.db.ExecContext(ctx, `
				INSERT INTO recipes (user_id, name, summary, minutes, servings, kcal_per_serving, protein_g, carbs_g, fat_g, ingredients_json, steps_json, tags, source)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'import')`,
				uid(ctx), req.Name, req.Summary, req.Minutes, req.Servings, req.KcalPerServing, req.ProteinG, req.CarbsG, req.FatG, string(ing), string(steps), strings.Join(req.Tags, ","))
			if err != nil {
				return nil, err
			}
			id, _ := res.LastInsertId()
			return s.recipeByID(ctx, uid(ctx), id)
		})

	gomcp.Register(srv, gomcp.Tool{Name: "cook_recipe", Description: "Log a meal from a recipe, scaled by servings eaten.", Write: true},
		func(ctx context.Context, in cookInput) (Meal, error) {
			userID := uid(ctx)
			rc, err := s.recipeByID(ctx, userID, in.ID)
			if err != nil {
				return Meal{}, errors.New("recipe not found")
			}
			d, err := s.mcpDate(ctx, in.Date)
			if err != nil {
				return Meal{}, err
			}
			servings := in.Servings
			if servings <= 0 {
				servings = 1
			}
			slot := strings.ToLower(strings.TrimSpace(in.Slot))
			if !validSlot(slot) {
				slot = slotForTime(time.Now().In(s.userLocation(ctx)))
			}
			if err := s.ensureDay(ctx, userID, d); err != nil {
				return Meal{}, err
			}
			res, err := s.db.ExecContext(ctx, `INSERT INTO meals (user_id, on_date, photo_id, recipe_id, name, slot, kcal, protein_g, carbs_g, fat_g, source) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'recipe')`,
				userID, d, rc.PhotoID, rc.ID, rc.Name, slot, rc.KcalPerServing*servings, rc.ProteinG*servings, rc.CarbsG*servings, rc.FatG*servings)
			if err != nil {
				return Meal{}, err
			}
			_, _ = s.db.ExecContext(ctx, `UPDATE recipes SET times_cooked = times_cooked + 1, last_cooked_at = CURRENT_TIMESTAMP WHERE id = ?`, rc.ID)
			mid, _ := res.LastInsertId()
			return s.mealByID(ctx, userID, mid)
		})

	gomcp.Register(srv, gomcp.Tool{Name: "list_blood_reports", Description: "Lab reports newest first, with counts of normal and abnormal markers."},
		func(ctx context.Context, _ noInput) ([]map[string]any, error) {
			rows, err := s.db.QueryContext(ctx, `SELECT id, taken_on, lab, ordered_by, notes, parse_status FROM blood_reports WHERE user_id = ? ORDER BY taken_on DESC`, uid(ctx))
			if err != nil {
				return nil, err
			}
			type report struct {
				id                              int64
				takenOn, lab, by, notes, status string
			}
			var reports []report
			for rows.Next() {
				var r report
				if rows.Scan(&r.id, &r.takenOn, &r.lab, &r.by, &r.notes, &r.status) == nil {
					reports = append(reports, r)
				}
			}
			rows.Close()
			out := []map[string]any{}
			for _, r := range reports {
				out = append(out, map[string]any{"id": r.id, "taken_on": r.takenOn, "lab": r.lab, "ordered_by": r.by, "notes": r.notes, "parse_status": r.status, "counts": s.markerCounts(ctx, r.id)})
			}
			return out, nil
		})

	gomcp.Register(srv, gomcp.Tool{Name: "get_blood_report", Description: "One report with every marker: value, unit, reference range, flag."},
		func(ctx context.Context, in idInput) (any, error) { return s.bloodReportByID(ctx, uid(ctx), in.ID) })

	gomcp.Register(srv, gomcp.Tool{Name: "get_blood_markers", Description: "Every marker across every report as a series, with reference ranges and the direction that counts as improvement. Filter by code (e.g. hba1c, ldl, alt) or category (sugar, lipids, liver, kidney, thyroid, blood)."},
		func(ctx context.Context, in markerFilterInput) ([]MarkerSeries, error) {
			series, err := s.markerSeries(ctx, uid(ctx))
			if err != nil {
				return nil, err
			}
			code, cat := strings.ToLower(strings.TrimSpace(in.Code)), strings.ToLower(strings.TrimSpace(in.Category))
			if code == "" && cat == "" {
				return series, nil
			}
			out := []MarkerSeries{}
			for _, ms := range series {
				if (code != "" && ms.Code == code) || (cat != "" && ms.Category == cat) {
					out = append(out, ms)
				}
			}
			return out, nil
		})

	gomcp.Register(srv, gomcp.Tool{Name: "add_blood_report", Description: "Record a lab report by hand. Each marker needs a name and value; units and ranges are optional. Names are matched to canonical codes automatically.", Write: true},
		func(ctx context.Context, in bloodReportInput) (any, error) {
			if !dates.Valid(in.TakenOn) {
				return nil, errors.New("taken_on must be YYYY-MM-DD")
			}
			if len(in.Markers) == 0 {
				return nil, errors.New("markers must be a list of {name, value, unit, ref_low, ref_high}")
			}
			id, err := s.insertBloodReport(ctx, uid(ctx), in.TakenOn, strings.TrimSpace(in.Lab), "", strings.TrimSpace(in.Notes), "", "", 0, "", "manual", toMarkers(in.Markers))
			if err != nil {
				return nil, err
			}
			return s.bloodReportByID(ctx, uid(ctx), id)
		})

	gomcp.Register(srv, gomcp.Tool{Name: "list_marker_definitions", Description: "The canonical marker codes lifeai knows, with categories, so marker queries and manual entries use the right names."},
		func(context.Context, noInput) ([]map[string]any, error) {
			out := make([]map[string]any, 0, len(blood.Definitions))
			for _, d := range blood.Definitions {
				out = append(out, map[string]any{"code": d.Code, "name": d.Name, "category": d.Category, "lower_is_better": d.LowerIsBetter, "watch": d.Watch})
			}
			return out, nil
		})
}
