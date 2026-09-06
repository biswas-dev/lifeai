ALTER TABLE users ADD COLUMN water_unit TEXT NOT NULL DEFAULT 'gal';
ALTER TABLE days ADD COLUMN water_baseline_ml REAL NOT NULL DEFAULT 0;
UPDATE days SET water_baseline_ml = COALESCE(water_ml, 0);

CREATE TABLE water_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    on_date TEXT NOT NULL,
    amount_ml REAL NOT NULL CHECK(amount_ml > 0),
    request_id TEXT NOT NULL,
    deleted_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(user_id, on_date) REFERENCES days(user_id, on_date) ON DELETE CASCADE,
    UNIQUE(user_id, request_id)
);
CREATE INDEX idx_water_entries_day ON water_entries(user_id, on_date, id);

-- Several providers can identify the same workout. Keep each identity even
-- after matching, so changes to duration or start time cannot import it twice.
CREATE TABLE workout_sources (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source TEXT NOT NULL,
    external_id TEXT NOT NULL,
    workout_id INTEGER NOT NULL REFERENCES workouts(id) ON DELETE CASCADE,
    PRIMARY KEY(user_id, source, external_id)
);
CREATE INDEX idx_workout_sources_workout ON workout_sources(workout_id);
INSERT INTO workout_sources(user_id, source, external_id, workout_id)
SELECT user_id, source, external_id, id FROM workouts WHERE external_id <> '';
