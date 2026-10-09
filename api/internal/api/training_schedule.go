package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/biswas-dev/lifeai/api/internal/dates"
)

// TrainingSchedule is the person's week: fixed sessions at set times, with
// everything else freeform, fitted in wherever it goes.
type TrainingSchedule struct {
	Fixed []ScheduleSlot `json:"fixed"`
	// Notes describe the freeform part, e.g. "walks, rides, mobility".
	Notes     string `json:"notes,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// ScheduleSlot is a recurring fixed session.
type ScheduleSlot struct {
	Key   string `json:"key" jsonschema:"short id, e.g. trainer"`
	Title string `json:"title" jsonschema:"e.g. Weight training with trainer"`
	Kind  string `json:"kind" jsonschema:"workout kind a session must have to count, e.g. strength"`
	Days  []int  `json:"days" jsonschema:"weekdays, 0=Sunday...6=Saturday"`
	Start string `json:"start" jsonschema:"HH:MM local"`
	End   string `json:"end" jsonschema:"HH:MM local"`
}

// Minutes is the slot's planned length.
func (s ScheduleSlot) Minutes() int {
	m := minutesOfDay(s.End) - minutesOfDay(s.Start)
	if m <= 0 {
		m += 24 * 60
	}
	return m
}

func minutesOfDay(clock string) int {
	if len(clock) != 5 {
		return 0
	}
	return int(clock[0]-'0')*600 + int(clock[1]-'0')*60 + int(clock[3]-'0')*10 + int(clock[4]-'0')
}

// PlannedSession is one fixed session in a particular week.
type PlannedSession struct {
	Date    string `json:"date"`
	Key     string `json:"key"`
	Title   string `json:"title"`
	Kind    string `json:"kind"`
	Start   string `json:"start"`
	End     string `json:"end"`
	Minutes int    `json:"minutes"`
	// Status is done, today, upcoming or missed.
	Status    string `json:"status"`
	WorkoutID *int64 `json:"workout_id"`
}

func (sc *TrainingSchedule) validate() error {
	if len(sc.Fixed) > 14 {
		return badInput("invalid_schedule", "at most 14 fixed sessions")
	}
	seen := map[string]bool{}
	for i := range sc.Fixed {
		f := &sc.Fixed[i]
		f.Key, f.Title, f.Kind = strings.ToLower(strings.TrimSpace(f.Key)), strings.TrimSpace(f.Title), strings.ToLower(strings.TrimSpace(f.Kind))
		if f.Key == "" {
			f.Key = strings.Trim(strings.ToLower(strings.Join(strings.Fields(f.Title), "-")), "-")
		}
		if !keyRE.MatchString(f.Key) || seen[f.Key] {
			return badInput("invalid_schedule", "fixed session keys must be unique, lower-case letters, digits, - or _")
		}
		seen[f.Key] = true
		if f.Title == "" {
			return badInput("invalid_schedule", "each fixed session needs a title")
		}
		if f.Kind == "" {
			f.Kind = "strength"
		}
		if !validWorkoutKind(f.Kind) {
			return badInput("invalid_schedule", "%s: unknown workout kind", f.Title)
		}
		if !clockRE.MatchString(f.Start) || !clockRE.MatchString(f.End) {
			return badInput("invalid_schedule", "%s: start and end must be HH:MM (24-hour)", f.Title)
		}
		if len(f.Days) == 0 {
			return badInput("invalid_schedule", "%s: choose at least one day", f.Title)
		}
		for _, d := range f.Days {
			if d < 0 || d > 6 {
				return badInput("invalid_schedule", "%s: days are 0 (Sunday) to 6 (Saturday)", f.Title)
			}
		}
	}
	if len(sc.Notes) > 1000 {
		sc.Notes = sc.Notes[:1000]
	}
	return nil
}

func (s *Server) trainingSchedule(ctx context.Context, userID int64) (*TrainingSchedule, error) {
	var raw, updated string
	err := s.db.QueryRowContext(ctx, `SELECT schedule_json, updated_at FROM training_schedules WHERE user_id = ?`, userID).Scan(&raw, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sc TrainingSchedule
	if err := json.Unmarshal([]byte(raw), &sc); err != nil {
		return nil, err
	}
	sc.UpdatedAt = updated
	return &sc, nil
}

func (s *Server) saveTrainingSchedule(ctx context.Context, userID int64, sc TrainingSchedule) (*TrainingSchedule, error) {
	if err := sc.validate(); err != nil {
		return nil, err
	}
	sc.UpdatedAt = ""
	if sc.Fixed == nil {
		sc.Fixed = []ScheduleSlot{}
	}
	raw, err := json.Marshal(sc)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO training_schedules (user_id, schedule_json) VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET schedule_json = excluded.schedule_json, updated_at = CURRENT_TIMESTAMP`, userID, string(raw)); err != nil {
		return nil, err
	}
	return s.trainingSchedule(ctx, userID)
}

// HandleGetTrainingSchedule returns {"schedule": ...}, null when none is set.
func (s *Server) HandleGetTrainingSchedule(w http.ResponseWriter, r *http.Request) {
	sc, err := s.trainingSchedule(r.Context(), UserID(r.Context()))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not load the schedule", "internal")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"schedule": sc})
}

// HandleSaveTrainingSchedule replaces the schedule.
func (s *Server) HandleSaveTrainingSchedule(w http.ResponseWriter, r *http.Request) {
	var sc TrainingSchedule
	if !decodeJSON(w, r, &sc) {
		return
	}
	saved, err := s.saveTrainingSchedule(r.Context(), UserID(r.Context()), sc)
	if err != nil {
		respondSaveError(w, err, "schedule")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"schedule": saved})
}

// plannedSessions lays a schedule over one week and matches each fixed
// session to a logged workout of the same kind on that date.
func plannedSessions(sc *TrainingSchedule, days []string, today string, byDate map[string][]weekWorkout) []PlannedSession {
	out := []PlannedSession{}
	if sc == nil {
		return out
	}
	used := map[int64]bool{}
	for _, d := range days {
		wd := weekdayIndex(d)
		for _, f := range sc.Fixed {
			if !containsInt(f.Days, wd) {
				continue
			}
			ps := PlannedSession{Date: d, Key: f.Key, Title: f.Title, Kind: f.Kind, Start: f.Start, End: f.End, Minutes: f.Minutes()}
			for _, w := range byDate[d] {
				if w.kind == f.Kind && !used[w.id] {
					id := w.id
					ps.WorkoutID, used[w.id] = &id, true
					break
				}
			}
			switch {
			case ps.WorkoutID != nil:
				ps.Status = "done"
			case d == today:
				ps.Status = "today"
			case d > today:
				ps.Status = "upcoming"
			default:
				ps.Status = "missed"
			}
			out = append(out, ps)
		}
	}
	return out
}

type weekWorkout struct {
	id   int64
	kind string
}

func weekdayIndex(date string) int {
	t, err := dates.Parse(date)
	if err != nil {
		return -1
	}
	return int(t.Weekday())
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// BackfillInput is a session reconstructed after the fact, typically from a
// recording or transcript of a trainer session.
type BackfillInput struct {
	Date      string          `json:"date" jsonschema:"YYYY-MM-DD of the session"`
	Slot      string          `json:"slot,omitempty" jsonschema:"fixed-session key from get_training_schedule; default: the fixed session scheduled that day"`
	Activity  string          `json:"activity,omitempty" jsonschema:"title; default the slot title"`
	Minutes   int             `json:"minutes,omitempty" jsonschema:"default the slot length"`
	StartedAt string          `json:"started_at,omitempty" jsonschema:"RFC3339; default the slot start in the person's timezone"`
	Notes     string          `json:"notes,omitempty" jsonschema:"session notes: trainer feedback, anything the transcript left uncertain"`
	Exercises []exerciseInput `json:"exercises" jsonschema:"in the order performed, warm-up first; leave reps or weight out rather than guessing"`
	// Append keeps exercises already logged and adds these after them.
	Append bool `json:"append,omitempty" jsonschema:"add to exercises already logged instead of replacing them"`
}

// BackfillResult says what happened.
type BackfillResult struct {
	Action  string  `json:"action"`
	Slot    string  `json:"slot,omitempty"`
	Workout Workout `json:"workout"`
}

// backfillSession fills in a session after the fact: it updates the session
// already logged for that date's fixed slot (or of the slot's kind), or
// creates one with the slot's time and length.
func (s *Server) backfillSession(ctx context.Context, userID int64, in BackfillInput) (BackfillResult, error) {
	if !dates.Valid(in.Date) {
		return BackfillResult{}, badInput("invalid_date", "date must be YYYY-MM-DD")
	}
	sc, err := s.trainingSchedule(ctx, userID)
	if err != nil {
		return BackfillResult{}, err
	}
	var slot *ScheduleSlot
	if sc != nil {
		wd := weekdayIndex(in.Date)
		for i := range sc.Fixed {
			f := &sc.Fixed[i]
			if (in.Slot != "" && f.Key == in.Slot) || (in.Slot == "" && containsInt(f.Days, wd)) {
				slot = f
				break
			}
		}
	}
	if in.Slot != "" && slot == nil {
		return BackfillResult{}, badInput("unknown_slot", "no fixed session %q in the schedule", in.Slot)
	}
	kind := "strength"
	if slot != nil {
		kind = slot.Kind
	}
	existing, err := s.queryWorkouts(ctx, userID, `SELECT `+workoutColumns+` FROM workouts WHERE user_id = ? AND on_date = ? AND kind = ? ORDER BY started_at, id`, userID, in.Date, kind)
	if err != nil {
		return BackfillResult{}, err
	}
	res := BackfillResult{Action: "created"}
	if slot != nil {
		res.Slot = slot.Key
	}
	body := workoutInput{Date: in.Date, Kind: kind, Activity: in.Activity, Minutes: in.Minutes, Notes: in.Notes, Exercises: in.Exercises}
	var id int64
	if len(existing) > 0 {
		cur := existing[0]
		id, res.Action = cur.ID, "updated"
		if body.Activity == "" {
			body.Activity = cur.Activity
		}
		if body.Minutes == 0 {
			body.Minutes = cur.Minutes
		}
		if body.Notes == "" {
			body.Notes = cur.Notes
		}
		body.Kcal, body.DistanceKm, body.AvgHR, body.StartedAt = cur.Kcal, cur.DistanceKm, cur.AvgHR, cur.StartedAt
		if in.Append {
			kept := make([]exerciseInput, 0, len(cur.Exercises)+len(in.Exercises))
			for _, e := range cur.Exercises {
				sets := make([]setInput, 0, len(e.Sets))
				for _, st := range e.Sets {
					sets = append(sets, setInput{Reps: st.Reps, Weight: st.Weight, Unit: st.Unit, Seconds: st.Seconds})
				}
				kept = append(kept, exerciseInput{Slug: e.Slug, Name: e.Name, Notes: e.Notes, Sets: sets})
			}
			body.Exercises = append(kept, in.Exercises...)
		}
	}
	if slot != nil {
		if body.Activity == "" {
			body.Activity = slot.Title
		}
		if body.Minutes == 0 {
			body.Minutes = slot.Minutes()
		}
		if body.StartedAt == nil {
			start := slotStart(in.Date, slot.Start, s.userLocation(ctx))
			body.StartedAt = &start
		}
	}
	if in.StartedAt != "" {
		body.StartedAt = &in.StartedAt
	}
	if body.Minutes == 0 {
		body.Minutes = 60
	}
	res.Workout, err = s.saveWorkout(ctx, userID, id, body)
	return res, err
}

func slotStart(date, clock string, loc *time.Location) string {
	t, err := time.ParseInLocation("2006-01-02 15:04", date+" "+clock, loc)
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// HandleBackfillSession fills in a session after the fact. See backfillSession.
func (s *Server) HandleBackfillSession(w http.ResponseWriter, r *http.Request) {
	var in BackfillInput
	if !decodeJSON(w, r, &in) {
		return
	}
	res, err := s.backfillSession(r.Context(), UserID(r.Context()), in)
	if err != nil {
		respondSaveError(w, err, "session")
		return
	}
	respondJSON(w, http.StatusOK, res)
}
