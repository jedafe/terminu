// Ref: .teos/sections/01, .teos/conventions/sqlite-migration-naming.md
// SQLite open + embedded migration runner (modernc.org/sqlite, WAL).
package db

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/jedafe/terminu/migrations"
	_ "modernc.org/sqlite"
)

func Open(ctx context.Context, path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", path)
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1) // WAL: single writer (ADR-003)
	if err := d.PingContext(ctx); err != nil {
		d.Close()
		return nil, err
	}
	if err := migrate(ctx, d); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// migrate applies NNNNNN_*.up.sql in order, tracked in schema_migrations.
// Naming per sqlite-migration-naming.md — the convention is the contract.
func migrate(ctx context.Context, d *sql.DB) error {
	if _, err := d.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')))`); err != nil {
		return err
	}
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return err
	}
	var ups []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			ups = append(ups, e.Name())
		}
	}
	sort.Strings(ups)
	for _, name := range ups {
		var applied int
		if err := d.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM schema_migrations WHERE version=?`, name).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			return err
		}
		if _, err := d.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := d.ExecContext(ctx,
			`INSERT INTO schema_migrations(version) VALUES(?)`, name); err != nil {
			return err
		}
	}
	return nil
}
