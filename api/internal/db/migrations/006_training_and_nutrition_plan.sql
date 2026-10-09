-- Strength sessions: the exercises in a workout, in order, and their sets.
-- exercise_slug refers to the built-in catalog; it is '' for a movement the
-- catalog does not know, which keeps its typed name.
CREATE TABLE workout_exercises (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workout_id INTEGER NOT NULL REFERENCES workouts(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    exercise_slug TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    notes TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_workout_exercises_workout ON workout_exercises(workout_id, position);
CREATE INDEX idx_workout_exercises_slug ON workout_exercises(user_id, exercise_slug);

-- Weight is what the person lifted in their own unit: per dumbbell for most
-- dumbbell work, or the single dumbbell for goblet squats and one-arm rows.
CREATE TABLE workout_sets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exercise_id INTEGER NOT NULL REFERENCES workout_exercises(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    reps INTEGER,
    weight REAL,
    unit TEXT NOT NULL DEFAULT 'lb' CHECK(unit IN ('lb', 'kg')),
    seconds INTEGER
);
CREATE INDEX idx_workout_sets_exercise ON workout_sets(exercise_id, position);

ALTER TABLE goals ADD COLUMN weekly_workout_minutes INTEGER;

-- One nutrition plan per person, stored as the JSON document the app edits.
CREATE TABLE nutrition_plans (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    plan_json TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Which planned meal a logged meal fulfils, e.g. "lunch" or "dinner:salmon".
ALTER TABLE meals ADD COLUMN plan_key TEXT NOT NULL DEFAULT '';

-- A weekly training schedule: fixed sessions (e.g. strength with a trainer,
-- Monday and Friday 06:30-07:30); everything else is freeform.
CREATE TABLE training_schedules (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    schedule_json TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
