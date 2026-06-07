package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "modernc.org/sqlite" // sqlite driver for database/sql
)

type SlugRow struct {
	ID        string
	Owner     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type EventRow struct {
	Slug      string
	Type      string
	SourceIP  string
	Timestamp time.Time
	Data      string
}

type Service interface {
	InsertEvents(ctx context.Context, events []EventRow) error
	InsertSlug(ctx context.Context, slug SlugRow) error
	LoadSlugs(ctx context.Context, since time.Time) ([]SlugRow, error)
	LoadEvents(ctx context.Context, since time.Time) ([]EventRow, error)
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
	DeleteSlug(ctx context.Context, slug string) error
	Close() error
}

type svc struct {
	db *sql.DB
}

var _ Service = (*svc)(nil)

func New(dbPath string) (Service, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
	}

	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode=WAL").Scan(&journalMode); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}

	if journalMode != "wal" {
		_ = db.Close()
		return nil, fmt.Errorf("expected WAL journal mode, got %q", journalMode)
	}

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	slog.Info("store initialized", "path", dbPath)

	return &svc{db: db}, nil
}

func (s *svc) InsertSlug(ctx context.Context, slug SlugRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO slugs (id, owner, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		slug.ID, slug.Owner, slug.CreatedAt.UTC(), slug.ExpiresAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert slug %s: %w", slug.ID, err)
	}

	return nil
}

func (s *svc) InsertEvents(ctx context.Context, events []EventRow) error {
	if len(events) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO events (slug, type, source_ip, timestamp, data) VALUES (?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare insert events: %w", err)
	}
	defer stmt.Close()

	for i, e := range events {
		if _, err := stmt.ExecContext(ctx, e.Slug, e.Type, e.SourceIP, e.Timestamp.UTC(), e.Data); err != nil {
			return fmt.Errorf("insert event %d: %w", i, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit events: %w", err)
	}

	return nil
}

func (s *svc) LoadSlugs(ctx context.Context, since time.Time) ([]SlugRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner, created_at, expires_at FROM slugs WHERE expires_at > ?`,
		since.UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("query slugs: %w", err)
	}
	defer rows.Close()

	var result []SlugRow

	for rows.Next() {
		var t SlugRow
		if err := rows.Scan(&t.ID, &t.Owner, &t.CreatedAt, &t.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan slug: %w", err)
		}

		result = append(result, t)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate slugs: %w", err)
	}

	return result, nil
}

func (s *svc) LoadEvents(ctx context.Context, since time.Time) ([]EventRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT slug, type, source_ip, timestamp, data FROM events WHERE timestamp > ?`,
		since.UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var result []EventRow

	for rows.Next() {
		var e EventRow
		if err := rows.Scan(&e.Slug, &e.Type, &e.SourceIP, &e.Timestamp, &e.Data); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}

		result = append(result, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}

	return result, nil
}

func (s *svc) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin delete tx: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx,
		`DELETE FROM events WHERE slug IN (SELECT id FROM slugs WHERE expires_at < ?)`,
		before.UTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("delete expired events: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		`DELETE FROM slugs WHERE expires_at < ?`,
		before.UTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("delete expired slugs: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit delete: %w", err)
	}

	return n, nil
}

func (s *svc) DeleteSlug(ctx context.Context, slug string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete slug tx: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE slug = ?`, slug); err != nil {
		return fmt.Errorf("delete events for slug %s: %w", slug, err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM slugs WHERE id = ?`, slug); err != nil {
		return fmt.Errorf("delete slug %s: %w", slug, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete slug %s: %w", slug, err)
	}

	return nil
}

func (s *svc) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close store: %w", err)
	}

	return nil
}

func migrate(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration tx: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	statements := []string{
		`CREATE TABLE IF NOT EXISTS slugs (
			id         TEXT PRIMARY KEY CHECK(length(id) <= 64),
			owner      TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			expires_at DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS events (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			slug       TEXT NOT NULL CHECK(length(slug) <= 128),
			type       TEXT NOT NULL,
			source_ip  TEXT NOT NULL,
			timestamp  DATETIME DEFAULT CURRENT_TIMESTAMP,
			data       JSON
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_slug ON events(slug)`,
		`CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp)`,
	}

	for _, stmt := range statements {
		if _, err := tx.Exec(stmt); err != nil {
			preview := stmt
			if len(preview) > 40 {
				preview = preview[:40]
			}

			return fmt.Errorf("exec %q: %w", preview, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}

	return nil
}
