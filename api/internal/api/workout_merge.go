package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// MergeWorkoutsInput links two rows that describe one session.
type MergeWorkoutsInput struct {
	MergeID int64 `json:"merge_id" jsonschema:"the session to fold in and remove, e.g. the imported Strava copy"`
	// UseTiming takes the merged row's start and length, e.g. a recording's
	// measured times over a scheduled slot.
	UseTiming bool `json:"use_timing,omitempty" jsonschema:"take the start time and length from the merged session (e.g. Strava's recorded times)"`
}

type mergeWorkoutsToolInput struct {
	KeepID int64 `json:"keep_id" jsonschema:"the session to keep, usually the one with exercises"`
	MergeWorkoutsInput
}

// createWorkout logs a new session and links an imported recording of it.
// A person's own times are kept.
func (s *Server) createWorkout(ctx context.Context, userID int64, in workoutInput) (Workout, error) {
	wk, err := s.saveWorkout(ctx, userID, 0, in)
	if err != nil {
		return wk, err
	}
	if linked, ok, err := s.linkRecordedSession(ctx, userID, wk.ID, false); err != nil {
		return wk, err
	} else if ok {
		return linked, nil
	}
	return wk, nil
}

// mergeWorkouts folds mergeID into keepID: source identities, so later syncs
// update the kept row; missing calories, distance and heart rate; notes; and
// exercises when the kept row has none. The merged row is then removed.
func (s *Server) mergeWorkouts(ctx context.Context, userID, keepID, mergeID int64, useTiming bool) (Workout, error) {
	if keepID == mergeID {
		return Workout{}, badInput("same_workout", "choose two different sessions")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Workout{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	load := func(id int64) (importedWorkout, error) {
		return readImportedWorkout(tx.QueryRowContext(ctx, `SELECT `+importedWorkoutColumns+` FROM workouts WHERE id=? AND user_id=?`, id, userID))
	}
	keep, err := load(keepID)
	if err != nil {
		return Workout{}, err
	}
	other, err := load(mergeID)
	if err != nil {
		return Workout{}, err
	}
	if keep.Kcal == nil {
		keep.Kcal = other.Kcal
	}
	if keep.DistanceKm == nil {
		keep.DistanceKm = other.DistanceKm
	}
	if keep.AvgHR == nil {
		keep.AvgHR = other.AvgHR
	}
	if keep.Kind == "other" && other.Kind != "other" {
		keep.Kind = other.Kind
	}
	if (useTiming || keep.StartedAt.IsZero()) && !other.StartedAt.IsZero() {
		keep.StartedAt, keep.Minutes, keep.Date = other.StartedAt, other.Minutes, other.Date
	}
	if note := strings.TrimSpace(other.Notes); note != "" && !strings.Contains(keep.Notes, note) {
		keep.Notes = strings.TrimSpace(keep.Notes + "\n\n" + note)
	}
	var keptExercises int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workout_exercises WHERE workout_id=? AND user_id=?`, keepID, userID).Scan(&keptExercises); err != nil {
		return Workout{}, err
	}
	if keptExercises == 0 {
		if _, err = tx.ExecContext(ctx, `UPDATE workout_exercises SET workout_id=? WHERE workout_id=? AND user_id=?`, keepID, mergeID, userID); err != nil {
			return Workout{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE workout_sources SET workout_id=? WHERE workout_id=? AND user_id=?`, keepID, mergeID, userID); err != nil {
		return Workout{}, err
	}
	if other.ExternalID != "" {
		if _, err = tx.ExecContext(ctx, `INSERT INTO workout_sources(user_id,source,external_id,workout_id) VALUES(?,?,?,?) ON CONFLICT(user_id,source,external_id) DO UPDATE SET workout_id=excluded.workout_id`, userID, other.Source, other.ExternalID, keepID); err != nil {
			return Workout{}, err
		}
	}
	if keep.ExternalID != "" {
		if _, err = tx.ExecContext(ctx, `INSERT INTO workout_sources(user_id,source,external_id,workout_id) VALUES(?,?,?,?) ON CONFLICT(user_id,source,external_id) DO UPDATE SET workout_id=excluded.workout_id`, userID, keep.Source, keep.ExternalID, keepID); err != nil {
			return Workout{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM workouts WHERE id=? AND user_id=?`, mergeID, userID); err != nil {
		return Workout{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO days(user_id,on_date) VALUES(?,?) ON CONFLICT(user_id,on_date) DO NOTHING`, userID, keep.Date); err != nil {
		return Workout{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE workouts SET on_date=?,kind=?,minutes=?,kcal=?,distance_km=?,avg_hr=?,notes=?,started_at=? WHERE id=? AND user_id=?`,
		keep.Date, keep.Kind, keep.Minutes, nullFloat(keep.Kcal), nullFloat(keep.DistanceKm), nullInt(keep.AvgHR), keep.Notes, nullableStartedAt(keep.StartedAt), keepID, userID); err != nil {
		return Workout{}, err
	}
	if err = tx.Commit(); err != nil {
		return Workout{}, err
	}
	return s.workoutByID(ctx, userID, keepID)
}

// linkRecordedSession finds the one imported recording of a person's own
// session (e.g. Strava's "Morning Workout" for a trainer session) and folds
// it in. It does nothing when there is none or more than one candidate.
func (s *Server) linkRecordedSession(ctx context.Context, userID, workoutID int64, useTiming bool) (Workout, bool, error) {
	own, err := readImportedWorkout(s.db.QueryRowContext(ctx, `SELECT `+importedWorkoutColumns+` FROM workouts WHERE id=? AND user_id=?`, workoutID, userID))
	if err != nil {
		return Workout{}, false, err
	}
	if own.Source != "manual" {
		return Workout{}, false, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+importedWorkoutColumns+` FROM workouts WHERE user_id=? AND on_date=? AND id<>? AND source<>'manual'`, userID, own.Date, workoutID)
	if err != nil {
		return Workout{}, false, err
	}
	var matches []int64
	for rows.Next() {
		candidate, err := readImportedWorkout(rows)
		if err != nil {
			rows.Close()
			return Workout{}, false, err
		}
		if sameImportedWorkout(own, candidate) {
			matches = append(matches, candidate.ID)
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil || len(matches) != 1 {
		return Workout{}, false, err
	}
	wk, err := s.mergeWorkouts(ctx, userID, workoutID, matches[0], useTiming)
	return wk, err == nil, err
}

// HandleMergeWorkouts folds another session into this one.
func (s *Server) HandleMergeWorkouts(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "workoutID"), 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid workout id", "invalid_id")
		return
	}
	var in MergeWorkoutsInput
	if !decodeJSON(w, r, &in) {
		return
	}
	wk, err := s.mergeWorkouts(r.Context(), UserID(r.Context()), id, in.MergeID, in.UseTiming)
	if errors.Is(err, sql.ErrNoRows) {
		respondError(w, http.StatusNotFound, "workout not found", "not_found")
		return
	}
	if err != nil {
		respondSaveError(w, err, "workout")
		return
	}
	respondJSON(w, http.StatusOK, wk)
}
