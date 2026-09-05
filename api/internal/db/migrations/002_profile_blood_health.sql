-- Profile, blood work, and multi-source health samples.

ALTER TABLE users ADD COLUMN dob TEXT NOT NULL DEFAULT '';
-- male | female | other | ''
ALTER TABLE users ADD COLUMN sex TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN height_cm REAL;

-- Comma-separated names of metric columns a person typed in by hand, which
-- an import must never overwrite.
ALTER TABLE days ADD COLUMN manual_fields TEXT NOT NULL DEFAULT '';

-- One lab report: a date, where it came from, the file, and the raw text
-- pulled out of it so a marker can always be checked against the source.
CREATE TABLE blood_reports (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    taken_on     TEXT    NOT NULL,
    lab          TEXT    NOT NULL DEFAULT '',
    ordered_by   TEXT    NOT NULL DEFAULT '',
    notes        TEXT    NOT NULL DEFAULT '',
    file_path    TEXT    NOT NULL DEFAULT '',
    file_name    TEXT    NOT NULL DEFAULT '',
    file_bytes   INTEGER NOT NULL DEFAULT 0,
    raw_text     TEXT    NOT NULL DEFAULT '',
    -- '' | parsed | ai | manual | failed
    parse_status TEXT    NOT NULL DEFAULT '',
    parse_error  TEXT    NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_blood_reports_user ON blood_reports(user_id, taken_on DESC);

-- One measured value. code is the canonical marker (hba1c, ldl, alt …) so
-- the same test from two labs lines up on one chart; name is what the lab
-- called it.
CREATE TABLE blood_markers (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    report_id  INTEGER NOT NULL REFERENCES blood_reports(id) ON DELETE CASCADE,
    code       TEXT    NOT NULL DEFAULT '',
    name       TEXT    NOT NULL,
    value      REAL,
    value_text TEXT    NOT NULL DEFAULT '',
    unit       TEXT    NOT NULL DEFAULT '',
    ref_low    REAL,
    ref_high   REAL,
    ref_text   TEXT    NOT NULL DEFAULT '',
    -- normal | high | low | abnormal | see_details | ''
    flag       TEXT    NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_blood_markers_report ON blood_markers(report_id, sort_order);
CREATE INDEX idx_blood_markers_code ON blood_markers(code);

-- Every metric reading from every source, before the day's value is
-- resolved. Two watches and a scale can all report a weight; the day shows
-- one, chosen by source precedence, and the rest are kept here.
CREATE TABLE health_samples (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    on_date    TEXT    NOT NULL,
    -- weight_kg | body_fat_pct | resting_hr | sleep_hours | steps | water_ml
    metric     TEXT    NOT NULL,
    -- apple | samsung | strava | 75hard | webhook | manual
    source     TEXT    NOT NULL,
    value      REAL    NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, on_date, metric, source)
);
CREATE INDEX idx_health_samples_user ON health_samples(user_id, on_date);

CREATE TABLE strava_accounts (
    user_id           INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    athlete_id        INTEGER NOT NULL DEFAULT 0,
    username          TEXT    NOT NULL DEFAULT '',
    access_token_enc  TEXT    NOT NULL,
    refresh_token_enc TEXT    NOT NULL,
    expires_at        INTEGER NOT NULL DEFAULT 0,
    scope             TEXT    NOT NULL DEFAULT '',
    last_sync_at      DATETIME,
    last_error        TEXT    NOT NULL DEFAULT '',
    imported          INTEGER NOT NULL DEFAULT 0,
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Import runs, for the settings screen.
CREATE TABLE import_runs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source      TEXT    NOT NULL,
    file_name   TEXT    NOT NULL DEFAULT '',
    summary     TEXT    NOT NULL DEFAULT '',
    error       TEXT    NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_import_runs_user ON import_runs(user_id, created_at DESC);
