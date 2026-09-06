package api

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/biswas-dev/lifeai/api/internal/dates"
	"github.com/biswas-dev/lifeai/api/internal/health"
)

type importedWorkout struct {
	ID int64
	health.WorkoutImport
	identities map[string]string
}

func workoutRank(source string) int {
	// Preserve a person's own entry, then prefer the direct activity provider
	// over a copy that travelled through a challenge or another device.
	switch source {
	case "manual":
		return 0
	case "strava":
		return 1
	case "apple":
		return 2
	case "samsung":
		return 3
	case "75hard":
		return 9
	}
	return 5
}

func normalizedActivity(activity string) string {
	return strings.Join(strings.Fields(strings.ToLower(activity)), " ")
}

func sameImportedWorkout(a, b importedWorkout) bool {
	if a.StartedAt.IsZero() || b.StartedAt.IsZero() {
		return false
	}
	// Two distinct records from the same provider are separate activities.
	// This also prevents an Apple import from joining two Strava activities.
	for source, external := range a.identities {
		if other, ok := b.identities[source]; ok && other != external {
			return false
		}
	}
	if a.Source == b.Source {
		return false
	}
	nameMatches := normalizedActivity(a.Activity) != "" && normalizedActivity(a.Activity) == normalizedActivity(b.Activity)
	fromStrava := (a.Source == "75hard" && strings.Contains(strings.ToLower(a.Notes), "imported from strava") && b.Source == "strava") ||
		(b.Source == "75hard" && strings.Contains(strings.ToLower(b.Notes), "imported from strava") && a.Source == "strava")
	// A custom Strava title such as "Lunch break" makes 75hard's generic
	// outdoor type map to cardio even when Strava knows it was a walk.
	kindMatches := (a.Kind == b.Kind && a.Kind != "other") || ((a.Kind == "other" || b.Kind == "other") && nameMatches) ||
		(fromStrava && nameMatches && ((a.Source == "75hard" && a.Kind == "cardio") || (b.Source == "75hard" && b.Kind == "cardio")))
	if !kindMatches {
		return false
	}
	if health.SameWorkout(a.StartedAt, a.Minutes, b.StartedAt, b.Minutes) {
		return true
	}
	// 75hard sometimes credits elapsed time where Strava reports moving
	// time. An explicitly Strava-derived copy with the exact same start and
	// activity name still represents one effort despite that difference.
	delta := a.StartedAt.Sub(b.StartedAt)
	if delta < 0 {
		delta = -delta
	}
	return fromStrava && nameMatches && delta <= 2*time.Second
}

func combineWorkout(a, b importedWorkout) importedWorkout {
	if workoutRank(b.Source) < workoutRank(a.Source) {
		a, b = b, a
	}
	if a.Kcal == nil {
		a.Kcal = b.Kcal
	}
	if a.DistanceKm == nil {
		a.DistanceKm = b.DistanceKm
	}
	if a.AvgHR == nil {
		a.AvgHR = b.AvgHR
	}
	if a.StartedAt.IsZero() {
		a.StartedAt = b.StartedAt
	}
	if strings.TrimSpace(a.Activity) == "" {
		a.Activity = b.Activity
	}
	if a.Kind == "other" && b.Kind != "other" {
		a.Kind = b.Kind
	}
	if note := strings.TrimSpace(b.Notes); note != "" && !strings.Contains(a.Notes, note) {
		a.Notes = strings.TrimSpace(a.Notes + "\n\n" + note)
	}
	for source, external := range b.identities {
		a.identities[source] = external
	}
	return a
}

func readImportedWorkout(row scanner) (importedWorkout, error) {
	var out importedWorkout
	var started sql.NullString
	var kcal, distance sql.NullFloat64
	var hr sql.NullInt64
	err := row.Scan(&out.ID, &out.Date, &out.Kind, &out.Activity, &out.Minutes, &kcal, &distance, &hr, &out.Notes, &started, &out.Source, &out.ExternalID)
	out.Kcal, out.DistanceKm, out.AvgHR = floatPtr(kcal), floatPtr(distance), intPtr(hr)
	if started.Valid {
		out.StartedAt, _ = parseDBTime(started.String)
	}
	out.identities = map[string]string{}
	if out.ExternalID != "" {
		out.identities[out.Source] = out.ExternalID
	}
	return out, err
}

const importedWorkoutColumns = `id,on_date,kind,activity,minutes,kcal,distance_km,avg_hr,notes,started_at,source,external_id`

// importWorkout remembers every source identity on one canonical workout.
// Identity lookup, matching, merging and insertion share a write transaction,
// so simultaneous nightly and Strava syncs cannot both create the same row.
func (s *Server) importWorkout(ctx context.Context, userID int64, wk health.WorkoutImport) (bool, error) {
	if !dates.Valid(wk.Date) || wk.Minutes <= 0 {
		return false, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE users SET updated_at=updated_at WHERE id=?`, userID); err != nil {
		return false, err
	}
	incoming := importedWorkout{WorkoutImport: wk, identities: map[string]string{}}
	if wk.ExternalID != "" {
		incoming.identities[wk.Source] = wk.ExternalID
	}
	var currentID int64
	if wk.ExternalID != "" {
		err = tx.QueryRowContext(ctx, `SELECT workout_id FROM workout_sources WHERE user_id=? AND source=? AND external_id=?`, userID, wk.Source, wk.ExternalID).Scan(&currentID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
	}
	current := incoming
	if currentID != 0 {
		current, err = readImportedWorkout(tx.QueryRowContext(ctx, `SELECT `+importedWorkoutColumns+` FROM workouts WHERE id=? AND user_id=?`, currentID, userID))
		if err != nil {
			return false, err
		}
		if workoutRank(wk.Source) <= workoutRank(current.Source) {
			// The same provider may correct its date, name, start or duration.
			incoming.ID = currentID
			current = combineWorkout(incoming, current)
		} else {
			current = combineWorkout(current, incoming)
		}
		current.ID = currentID
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+importedWorkoutColumns+` FROM workouts WHERE user_id=? AND on_date IN (?,?,?) AND id<>?`, userID, wk.Date, dates.AddDays(wk.Date, -1), dates.AddDays(wk.Date, 1), currentID)
	if err != nil {
		return false, err
	}
	candidates := []importedWorkout{}
	for rows.Next() {
		candidate, readErr := readImportedWorkout(rows)
		if readErr != nil {
			rows.Close()
			return false, readErr
		}
		candidates = append(candidates, candidate)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close()
	// Read aliases after closing the row cursor, also supporting single-
	// connection databases used by tests and small deployments.
	for i := -1; i < len(candidates); i++ {
		candidate := &current
		if i >= 0 {
			candidate = &candidates[i]
		}
		if candidate.ID == 0 {
			continue
		}
		aliases, err := tx.QueryContext(ctx, `SELECT source,external_id FROM workout_sources WHERE workout_id=? AND user_id=?`, candidate.ID, userID)
		if err != nil {
			return false, err
		}
		for aliases.Next() {
			var source, external string
			if err = aliases.Scan(&source, &external); err != nil {
				aliases.Close()
				return false, err
			}
			candidate.identities[source] = external
		}
		if err = aliases.Err(); err != nil {
			aliases.Close()
			return false, err
		}
		aliases.Close()
	}
	matches := []importedWorkout{}
	for _, candidate := range candidates {
		if sameImportedWorkout(current, candidate) {
			matches = append(matches, candidate)
		}
	}
	keepID, removeID := currentID, int64(0)
	// Ambiguous overlapping records are left separate instead of guessing.
	if len(matches) == 1 {
		candidate := matches[0]
		keepID = candidate.ID
		if currentID != 0 {
			keepID, removeID = currentID, candidate.ID
			if candidate.ID < currentID {
				keepID, removeID = candidate.ID, currentID
			}
		}
		current = combineWorkout(current, candidate)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO days(user_id,on_date) VALUES(?,?) ON CONFLICT(user_id,on_date) DO NOTHING`, userID, current.Date); err != nil {
		return false, err
	}
	if removeID != 0 {
		if _, err = tx.ExecContext(ctx, `UPDATE workout_sources SET workout_id=? WHERE workout_id=? AND user_id=?`, keepID, removeID, userID); err != nil {
			return false, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM workouts WHERE id=? AND user_id=?`, removeID, userID); err != nil {
			return false, err
		}
	}
	values := []any{current.Date, current.Kind, strings.TrimSpace(current.Activity), current.Minutes, nullFloat(current.Kcal), nullFloat(current.DistanceKm), nullInt(current.AvgHR), strings.TrimSpace(current.Notes), nullableStartedAt(current.StartedAt), current.Source, current.ExternalID}
	inserted := keepID == 0
	if inserted {
		values = append(values, userID)
		result, err := tx.ExecContext(ctx, `INSERT INTO workouts(on_date,kind,activity,minutes,kcal,distance_km,avg_hr,notes,started_at,source,external_id,user_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, values...)
		if err != nil {
			return false, err
		}
		keepID, err = result.LastInsertId()
		if err != nil {
			return false, err
		}
	} else {
		values = append(values, keepID, userID)
		if _, err = tx.ExecContext(ctx, `UPDATE workouts SET on_date=?,kind=?,activity=?,minutes=?,kcal=?,distance_km=?,avg_hr=?,notes=?,started_at=?,source=?,external_id=? WHERE id=? AND user_id=?`, values...); err != nil {
			return false, err
		}
	}
	for source, external := range current.identities {
		if _, err = tx.ExecContext(ctx, `INSERT INTO workout_sources(user_id,source,external_id,workout_id) VALUES(?,?,?,?) ON CONFLICT(user_id,source,external_id) DO UPDATE SET workout_id=excluded.workout_id`, userID, source, external, keepID); err != nil {
			return false, err
		}
	}
	return inserted, tx.Commit()
}
