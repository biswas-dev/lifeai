ALTER TABLE users ADD COLUMN security_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN totp_secret_enc TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN totp_last_step INTEGER NOT NULL DEFAULT -1;
ALTER TABLE users ADD COLUMN mfa_failures INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN mfa_locked_until INTEGER NOT NULL DEFAULT 0;

CREATE TABLE recovery_codes (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    PRIMARY KEY (user_id, code_hash)
);

CREATE TABLE passkeys (
    credential_id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    public_key BLOB NOT NULL,
    sign_count INTEGER NOT NULL DEFAULT 0,
    backup_eligible INTEGER NOT NULL DEFAULT 0,
    backed_up INTEGER NOT NULL DEFAULT 0,
    transports_json TEXT NOT NULL DEFAULT '[]',
    attestation_type TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at TEXT
);
CREATE INDEX idx_passkeys_user ON passkeys(user_id);

-- Opaque, browser-bound, short-lived challenges. TOTP enrollment secrets
-- inside data_json are separately encrypted with the integration cipher.
CREATE TABLE auth_challenges (
    token_hash TEXT PRIMARY KEY,
    binding_hash TEXT NOT NULL,
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    security_version INTEGER NOT NULL DEFAULT 0,
    kind TEXT NOT NULL,
    data_json TEXT NOT NULL DEFAULT '{}',
    expires_at INTEGER NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_auth_challenges_expiry ON auth_challenges(expires_at);
