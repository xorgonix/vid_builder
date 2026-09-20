// Package db opens the SQLite database, applies the schema and seeds initial data.
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"vidbuild/internal/ids"
)

//go:embed schema.sql
var schemaFS embed.FS

// DB wraps *sql.DB with helpers used across services.
type DB struct {
	*sql.DB
}

// Open opens (or creates) the SQLite database at path, applies pragmas and
// the embedded schema, and seeds the bootstrap admin user + personal workspace
// on first run. Path's parent directory is created if missing.
func Open(path string, busyTimeoutMs, maxConns int) (*DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("db: create data dir: %w", err)
		}
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)",
		filepath.ToSlash(path), busyTimeoutMs)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}
	sqlDB.SetMaxOpenConns(maxConns)
	sqlDB.SetMaxIdleConns(maxConns)
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	d := &DB{sqlDB}
	if err := d.migrate(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *DB) migrate() error {
	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("db: read schema: %w", err)
	}
	if _, err := d.Exec(string(schema)); err != nil {
		return fmt.Errorf("db: apply schema: %w", err)
	}
	// Column additions for databases created before a column existed. SQLite has
	// no ADD COLUMN IF NOT EXISTS, so duplicate-column errors are ignored.
	for _, stmt := range columnMigrations {
		if _, err := d.Exec(stmt); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
				continue
			}
			return fmt.Errorf("db: migration %q: %w", stmt, err)
		}
	}
	return nil
}

// columnMigrations add columns introduced after the initial schema. Each is
// safe to re-run.
var columnMigrations = []string{
	`ALTER TABLE generation_requests ADD COLUMN workflow_version_id TEXT`,
	`ALTER TABLE generation_requests ADD COLUMN model_profile_id TEXT`,
	`ALTER TABLE generation_attempts ADD COLUMN workflow_version_id TEXT`,
	`ALTER TABLE generation_attempts ADD COLUMN workflow_graph_sha256 TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE generation_attempts ADD COLUMN campaign_item_id TEXT`,
	`ALTER TABLE generation_attempts ADD COLUMN resolved_negative_prompts_json TEXT NOT NULL DEFAULT '{}'`,
	`ALTER TABLE generation_attempts ADD COLUMN bound_inputs_json TEXT NOT NULL DEFAULT '{}'`,
	`ALTER TABLE generation_attempts ADD COLUMN provider_request_receipt_json TEXT NOT NULL DEFAULT '{}'`,
	`ALTER TABLE generation_attempts ADD COLUMN cost_micros INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE generation_attempts ADD COLUMN currency TEXT NOT NULL DEFAULT 'USD'`,
	`ALTER TABLE generation_attempts ADD COLUMN peak_vram_mb INTEGER`,
	`ALTER TABLE workflow_versions ADD COLUMN tested_comfyui_version TEXT NOT NULL DEFAULT ''`,
}

// SeedBootstraps creates the initial system admin user (if no users exist) and
// that user's personal workspace. It returns the admin credentials used.
func (d *DB) SeedBootstrap(adminEmail, adminPasswordHash, displayName string) error {
	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return fmt.Errorf("db: seed count users: %w", err)
	}
	if count > 0 {
		return nil
	}
	now := Now()
	adminID := ids.New()
	_, err := d.Exec(`INSERT INTO users (id, email_normalized, display_name, password_hash, status, is_system_admin, created_at, updated_at)
		VALUES (?,?,?,?,'active',1,?,?)`,
		adminID, NormalizeEmail(adminEmail), displayName, adminPasswordHash, now, now)
	if err != nil {
		return fmt.Errorf("db: seed admin user: %w", err)
	}

	wsID := ids.New()
	_, err = d.Exec(`INSERT INTO workspaces (id, name, slug, kind, created_by_user_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?)`,
		wsID, "Personal", "personal", "personal", adminID, now, now)
	if err != nil {
		return fmt.Errorf("db: seed workspace: %w", err)
	}
	_, err = d.Exec(`INSERT INTO workspace_members (id, workspace_id, user_id, role, status, joined_at)
		VALUES (?,?,?,'owner','active',?)`,
		ids.New(), wsID, adminID, now)
	if err != nil {
		return fmt.Errorf("db: seed membership: %w", err)
	}
	return nil
}
