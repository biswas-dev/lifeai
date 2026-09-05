-- Core schema. Everything is keyed by user and by calendar date in the
-- user's own timezone (YYYY-MM-DD), never by a server-side timestamp.

CREATE TABLE users (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    email          TEXT    NOT NULL,
    password_hash  TEXT    NOT NULL,
    name           TEXT    NOT NULL DEFAULT '',
    avatar_url     TEXT    NOT NULL DEFAULT '',
    timezone       TEXT    NOT NULL DEFAULT 'UTC',
    is_admin       INTEGER NOT NULL DEFAULT 0,
    -- password | google | github
    auth_provider  TEXT    NOT NULL DEFAULT 'password',
    -- Display only. Weight is always stored in kilograms.
    weight_unit    TEXT    NOT NULL DEFAULT 'kg',
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at     DATETIME
);
CREATE UNIQUE INDEX idx_users_email ON users(lower(email));

CREATE TABLE oauth_providers (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider         TEXT    NOT NULL,
    provider_user_id TEXT    NOT NULL,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, provider),
    UNIQUE(provider, provider_user_id)
);

CREATE TABLE password_resets (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT    NOT NULL UNIQUE,
    expires_at DATETIME NOT NULL,
    used_at    DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_password_resets_user ON password_resets(user_id);

-- Personal API tokens (go-api). Only the hash is stored.
CREATE TABLE api_tokens (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT    NOT NULL DEFAULT '',
    token_hash   TEXT    NOT NULL UNIQUE,
    prefix       TEXT    NOT NULL,
    scopes       TEXT    NOT NULL DEFAULT 'read',
    last_used_at DATETIME,
    expires_at   DATETIME,
    revoked_at   DATETIME,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_api_tokens_user ON api_tokens(user_id, created_at DESC);

-- What the person is aiming for. One row per user; absent means no targets.
CREATE TABLE goals (
    user_id          INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    daily_kcal       INTEGER,
    protein_g        INTEGER,
    carbs_g          INTEGER,
    fat_g            INTEGER,
    target_weight_kg REAL,
    steps            INTEGER,
    water_ml         INTEGER,
    sleep_hours      REAL,
    workout_minutes  INTEGER,
    -- Free text the coach reads: "cut to 80kg by June", "marathon in October".
    notes            TEXT NOT NULL DEFAULT '',
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- One row per calendar day a person has touched. Holds the body metrics;
-- meals, workouts and the rest hang off (user_id, on_date) rather than a day
-- id, so a day exists the moment anything is logged against it.
CREATE TABLE days (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    on_date      TEXT    NOT NULL,
    weight_kg    REAL,
    body_fat_pct REAL,
    resting_hr   INTEGER,
    sleep_hours  REAL,
    steps        INTEGER,
    water_ml     INTEGER,
    -- 1..5, or NULL for not recorded.
    mood         INTEGER,
    energy       INTEGER,
    note         TEXT    NOT NULL DEFAULT '',
    -- manual | 75hard
    source       TEXT    NOT NULL DEFAULT 'manual',
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, on_date)
);

CREATE TABLE photos (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    on_date     TEXT    NOT NULL,
    -- progress | food | ingredients
    kind        TEXT    NOT NULL DEFAULT 'progress',
    -- front | side | back | '' for progress shots
    pose        TEXT    NOT NULL DEFAULT '',
    rel_path    TEXT    NOT NULL,
    thumb_path  TEXT    NOT NULL DEFAULT '',
    mime        TEXT    NOT NULL,
    width       INTEGER NOT NULL DEFAULT 0,
    height      INTEGER NOT NULL DEFAULT 0,
    bytes       INTEGER NOT NULL DEFAULT 0,
    sha256      TEXT    NOT NULL DEFAULT '',
    caption     TEXT    NOT NULL DEFAULT '',
    taken_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    source      TEXT    NOT NULL DEFAULT 'manual',
    -- Id at the source system, so a nightly pull never duplicates a row.
    external_id TEXT    NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_photos_user_date ON photos(user_id, on_date DESC, id DESC);
CREATE INDEX idx_photos_user_kind ON photos(user_id, kind, taken_at DESC);
CREATE UNIQUE INDEX idx_photos_external ON photos(user_id, source, external_id) WHERE external_id <> '';

-- A recipe is a reusable meal. Cooking one logs a meal that points back here.
CREATE TABLE recipes (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name             TEXT    NOT NULL,
    summary          TEXT    NOT NULL DEFAULT '',
    minutes          INTEGER NOT NULL DEFAULT 0,
    servings         INTEGER NOT NULL DEFAULT 1,
    kcal_per_serving REAL    NOT NULL DEFAULT 0,
    protein_g        REAL    NOT NULL DEFAULT 0,
    carbs_g          REAL    NOT NULL DEFAULT 0,
    fat_g            REAL    NOT NULL DEFAULT 0,
    -- JSON arrays of strings.
    ingredients_json TEXT    NOT NULL DEFAULT '[]',
    steps_json       TEXT    NOT NULL DEFAULT '[]',
    -- Comma-separated, lower case.
    tags             TEXT    NOT NULL DEFAULT '',
    favourite        INTEGER NOT NULL DEFAULT 0,
    photo_id         INTEGER REFERENCES photos(id) ON DELETE SET NULL,
    -- manual | ai | import
    source           TEXT    NOT NULL DEFAULT 'manual',
    times_cooked     INTEGER NOT NULL DEFAULT 0,
    last_cooked_at   DATETIME,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_recipes_user ON recipes(user_id, favourite DESC, updated_at DESC);

CREATE TABLE meals (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    on_date         TEXT    NOT NULL,
    photo_id        INTEGER REFERENCES photos(id) ON DELETE SET NULL,
    recipe_id       INTEGER REFERENCES recipes(id) ON DELETE SET NULL,
    name            TEXT    NOT NULL DEFAULT '',
    -- breakfast | lunch | dinner | snack
    slot            TEXT    NOT NULL DEFAULT 'snack',
    kcal            REAL    NOT NULL DEFAULT 0,
    protein_g       REAL    NOT NULL DEFAULT 0,
    carbs_g         REAL    NOT NULL DEFAULT 0,
    fat_g           REAL    NOT NULL DEFAULT 0,
    -- manual | ai | recipe | 75hard
    source          TEXT    NOT NULL DEFAULT 'manual',
    notes           TEXT    NOT NULL DEFAULT '',
    eaten_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- '' for a hand-entered meal; pending | done | failed while a photo is
    -- being estimated, so "no numbers yet" is not mistaken for zero calories.
    estimate_status TEXT    NOT NULL DEFAULT '',
    estimate_error  TEXT    NOT NULL DEFAULT '',
    external_id     TEXT    NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_meals_user_date ON meals(user_id, on_date);
CREATE UNIQUE INDEX idx_meals_external ON meals(user_id, source, external_id) WHERE external_id <> '';

CREATE TABLE meal_items (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    meal_id    INTEGER NOT NULL REFERENCES meals(id) ON DELETE CASCADE,
    name       TEXT    NOT NULL,
    qty        REAL    NOT NULL DEFAULT 1,
    unit       TEXT    NOT NULL DEFAULT 'serving',
    kcal       REAL    NOT NULL DEFAULT 0,
    protein_g  REAL    NOT NULL DEFAULT 0,
    carbs_g    REAL    NOT NULL DEFAULT 0,
    fat_g      REAL    NOT NULL DEFAULT 0,
    confidence REAL,
    sort_order INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_meal_items_meal ON meal_items(meal_id);

CREATE TABLE workouts (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    on_date     TEXT    NOT NULL,
    -- strength | cardio | walk | run | cycle | swim | yoga | sport | other
    kind        TEXT    NOT NULL DEFAULT 'other',
    activity    TEXT    NOT NULL DEFAULT '',
    minutes     INTEGER NOT NULL DEFAULT 0,
    kcal        REAL,
    distance_km REAL,
    avg_hr      INTEGER,
    notes       TEXT    NOT NULL DEFAULT '',
    started_at  DATETIME,
    source      TEXT    NOT NULL DEFAULT 'manual',
    external_id TEXT    NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_workouts_user_date ON workouts(user_id, on_date);
CREATE UNIQUE INDEX idx_workouts_external ON workouts(user_id, source, external_id) WHERE external_id <> '';

CREATE TABLE meditations (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    on_date     TEXT    NOT NULL,
    minutes     INTEGER NOT NULL DEFAULT 0,
    style       TEXT    NOT NULL DEFAULT 'guided',
    notes       TEXT    NOT NULL DEFAULT '',
    started_at  DATETIME,
    source      TEXT    NOT NULL DEFAULT 'manual',
    external_id TEXT    NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_meditations_user_date ON meditations(user_id, on_date);
CREATE UNIQUE INDEX idx_meditations_external ON meditations(user_id, source, external_id) WHERE external_id <> '';

CREATE TABLE journal_entries (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    on_date     TEXT    NOT NULL,
    title       TEXT    NOT NULL DEFAULT '',
    body        TEXT    NOT NULL DEFAULT '',
    source      TEXT    NOT NULL DEFAULT 'manual',
    external_id TEXT    NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_journal_user_date ON journal_entries(user_id, on_date DESC, id DESC);
CREATE UNIQUE INDEX idx_journal_external ON journal_entries(user_id, source, external_id) WHERE external_id <> '';

-- Every model call: the audit trail, the result cache and the quota counter.
CREATE TABLE ai_runs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    feature     TEXT    NOT NULL,
    provider    TEXT    NOT NULL DEFAULT '',
    model       TEXT    NOT NULL DEFAULT '',
    input_hash  TEXT    NOT NULL,
    result_json TEXT    NOT NULL DEFAULT '',
    tokens_in   INTEGER NOT NULL DEFAULT 0,
    tokens_out  INTEGER NOT NULL DEFAULT 0,
    attempts    INTEGER NOT NULL DEFAULT 1,
    error       TEXT    NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_ai_runs_cache ON ai_runs(user_id, feature, input_hash);
CREATE INDEX idx_ai_runs_quota ON ai_runs(user_id, created_at);

-- Connections to other systems. One row per (user, provider). The credential
-- is AES-GCM sealed with ENCRYPTION_KEY and never returned to a client.
CREATE TABLE integrations (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider      TEXT    NOT NULL,
    base_url      TEXT    NOT NULL DEFAULT '',
    token_enc     TEXT    NOT NULL DEFAULT '',
    token_hint    TEXT    NOT NULL DEFAULT '',
    enabled       INTEGER NOT NULL DEFAULT 1,
    last_sync_at  DATETIME,
    last_status   TEXT    NOT NULL DEFAULT '',
    last_error    TEXT    NOT NULL DEFAULT '',
    -- JSON: what the last pull brought in, for the settings screen.
    last_summary  TEXT    NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, provider)
);
