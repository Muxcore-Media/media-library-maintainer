package internal

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (m *Module) initDB(ctx context.Context) error {
	dir := filepath.Dir(m.dbPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}
	db, err := sql.Open("sqlite", m.dbPath)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return err
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS rule_groups (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			enabled INTEGER DEFAULT 1,
			scope TEXT NOT NULL,
			collection_id TEXT DEFAULT '',
			definition_json TEXT NOT NULL DEFAULT '{}',
			outcome TEXT NOT NULL DEFAULT 'candidate',
			arr_action TEXT NOT NULL DEFAULT 'delete',
			auto_act_enabled INTEGER DEFAULT 0,
			auto_act_delay_days INTEGER DEFAULT 14,
			tag_enabled INTEGER DEFAULT 0,
			arr_tag TEXT DEFAULT '',
			max_actions_per_run INTEGER DEFAULT 50,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS collections (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			enabled INTEGER DEFAULT 1,
			grace_days INTEGER DEFAULT 7,
			arr_action TEXT NOT NULL DEFAULT 'delete',
			leaving_soon_enabled INTEGER DEFAULT 1,
			leaving_soon_label TEXT DEFAULT 'Leaving Soon',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS candidates (
			id TEXT PRIMARY KEY,
			scope TEXT NOT NULL,
			item_id TEXT NOT NULL,
			title TEXT DEFAULT '',
			year INTEGER DEFAULT 0,
			tmdb_id INTEGER DEFAULT 0,
			imdb_id TEXT DEFAULT '',
			matched_rule_ids TEXT DEFAULT '[]',
			arr_action TEXT NOT NULL DEFAULT 'delete',
			status TEXT NOT NULL DEFAULT 'pending',
			collection_id TEXT DEFAULT '',
			added_at TEXT NOT NULL,
			act_after TEXT DEFAULT '',
			postponed_until TEXT DEFAULT '',
			completed_at TEXT DEFAULT '',
			error TEXT DEFAULT '',
			criteria_json TEXT DEFAULT '{}',
			size_bytes INTEGER DEFAULT 0,
			UNIQUE(scope, item_id)
		)`,
		`CREATE TABLE IF NOT EXISTS protections (
			id TEXT PRIMARY KEY,
			scope TEXT NOT NULL,
			item_id TEXT NOT NULL,
			title TEXT DEFAULT '',
			reason TEXT DEFAULT '',
			expires_at TEXT DEFAULT '',
			created_at TEXT NOT NULL,
			UNIQUE(scope, item_id)
		)`,
		`CREATE TABLE IF NOT EXISTS exclusion_lists (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			list_url TEXT NOT NULL,
			api_key TEXT DEFAULT '',
			tmdb_ids_json TEXT DEFAULT '[]',
			last_synced TEXT DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS import_exclusions (
			scope TEXT NOT NULL,
			tmdb_id INTEGER NOT NULL,
			imdb_id TEXT DEFAULT '',
			title TEXT DEFAULT '',
			created_at TEXT NOT NULL,
			PRIMARY KEY(scope, tmdb_id)
		)`,
		`CREATE TABLE IF NOT EXISTS leaving_soon_notified (
			scope TEXT NOT NULL,
			item_id TEXT NOT NULL,
			notified_at TEXT NOT NULL,
			PRIMARY KEY(scope, item_id)
		)`,
		`CREATE TABLE IF NOT EXISTS overlay_state (
			scope TEXT NOT NULL,
			item_id TEXT NOT NULL,
			original_poster_path TEXT NOT NULL DEFAULT '',
			overlay_label TEXT DEFAULT '',
			applied_at TEXT NOT NULL,
			PRIMARY KEY(scope, item_id)
		)`,
		`CREATE TABLE IF NOT EXISTS run_log (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			status TEXT NOT NULL,
			candidates_found INTEGER DEFAULT 0,
			actions_taken INTEGER DEFAULT 0,
			actions_failed INTEGER DEFAULT 0,
			dry_run INTEGER DEFAULT 0,
			error TEXT DEFAULT '',
			started_at TEXT NOT NULL,
			completed_at TEXT DEFAULT ''
		)`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			db.Close()
			return fmt.Errorf("schema: %w", err)
		}
	}
	_, _ = db.ExecContext(ctx, `ALTER TABLE rule_groups ADD COLUMN quality_profile_id TEXT DEFAULT ''`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE collections ADD COLUMN jellyfin_collection_id TEXT DEFAULT ''`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE collections ADD COLUMN plex_collection_key TEXT DEFAULT ''`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE overlay_state ADD COLUMN original_backdrop_path TEXT DEFAULT ''`)

	m.mu.Lock()
	m.db = db
	m.mu.Unlock()
	return nil
}

func nowRFC() string { return time.Now().UTC().Format(time.RFC3339) }

func newID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}
