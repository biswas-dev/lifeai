-- Supplements a person takes, e.g. a greens powder or creatine, with the
-- exact product, dose and an optional product photo.
CREATE TABLE supplements (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    product TEXT NOT NULL DEFAULT '',
    dose TEXT NOT NULL DEFAULT '',
    taken_with TEXT NOT NULL DEFAULT '',
    product_url TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    started_on TEXT NOT NULL DEFAULT '',
    active INTEGER NOT NULL DEFAULT 1,
    position INTEGER NOT NULL DEFAULT 0,
    image BLOB,
    image_type TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_supplements_user ON supplements(user_id, active, position);

-- At most one record per supplement per day: taken, with what.
CREATE TABLE supplement_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    supplement_id INTEGER NOT NULL REFERENCES supplements(id) ON DELETE CASCADE,
    on_date TEXT NOT NULL,
    taken_with TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, supplement_id, on_date)
);
CREATE INDEX idx_supplement_logs_day ON supplement_logs(user_id, on_date);
