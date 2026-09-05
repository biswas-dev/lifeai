// Package db owns the SQLite connection and the migration runner.
//
// Migrations are plain numbered .sql files applied in sorted order inside a
// transaction and recorded in schema_migrations — the same hand-rolled scheme
// 75hard and taskai use. No migration library.
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	// Pure-Go SQLite. No CGO, so the binary cross-compiles and the runtime
	// image needs nothing but ca-certificates.
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// DB wraps *sql.DB.
type DB struct {
	*sql.DB
}

// Open connects to the SQLite database at path, creating the parent directory
// if needed, with WAL, a busy timeout and enforced foreign keys.
func Open(path string) (*DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("db: create data dir: %w", err)
		}
	}

	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}

	// WAL allows many concurrent readers, and a handler routinely runs a
	// second query while iterating the rows of a first.
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(4)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return &DB{DB: sqlDB}, nil
}

// OpenMemory opens a private in-memory database, for tests.
func OpenMemory() (*DB, error) {
	sqlDB, err := sql.Open("sqlite", "file::memory:?cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	return &DB{DB: sqlDB}, nil
}

// Migrate applies every embedded migration that has not run yet, in filename
// order, each in its own transaction.
func (d *DB) Migrate() (applied []string, err error) {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return nil, fmt.Errorf("db: create schema_migrations: %w", err)
	}

	done := map[string]bool{}
	rows, err := d.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("db: read schema_migrations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		done[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("db: read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		if done[name] {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return applied, fmt.Errorf("db: read %s: %w", name, err)
		}
		tx, err := d.Begin()
		if err != nil {
			return applied, fmt.Errorf("db: begin %s: %w", name, err)
		}
		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			return applied, fmt.Errorf("db: apply %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, name); err != nil {
			_ = tx.Rollback()
			return applied, fmt.Errorf("db: record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return applied, fmt.Errorf("db: commit %s: %w", name, err)
		}
		applied = append(applied, name)
	}
	return applied, nil
}
