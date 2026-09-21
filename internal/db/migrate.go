package db

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migration is one forward-only schema change. Numbered SQL files under
// internal/db/migrations are applied in ascending version order, each in its
// own transaction, and recorded with a checksum.
type migration struct {
	Version    int
	Name       string
	SQL        string   // applied as a single batch when Statements is nil
	Statements []string // applied one at a time (legacy backfill)
	Tolerant   bool     // ignore "duplicate column" errors
}

// legacyColumnBackfill upgrades databases created before a column existed.
// SQLite has no ADD COLUMN IF NOT EXISTS, so this migration is tolerant.
// New migrations must not need tolerance because each runs exactly once.
var legacyColumnBackfill = []string{
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
	`ALTER TABLE shot_specs ADD COLUMN prompt_layers_json TEXT NOT NULL DEFAULT '[]'`,
	`ALTER TABLE projects ADD COLUMN default_model_profile_id TEXT`,
}

const legacyVersion = 2

// loadMigrations reads numbered SQL files and appends the legacy backfill.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("db: read migrations dir: %w", err)
	}
	var out []migration
	seen := map[int]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".sql")
		parts := strings.SplitN(base, "_", 2)
		if len(parts) < 2 {
			return nil, fmt.Errorf("db: migration %q must be named NNNN_name.sql", e.Name())
		}
		version, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("db: migration %q has a non-numeric version: %w", e.Name(), err)
		}
		if other, ok := seen[version]; ok {
			return nil, fmt.Errorf("db: duplicate migration version %d (%s and %s)", version, other, e.Name())
		}
		seen[version] = e.Name()
		raw, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("db: read migration %s: %w", e.Name(), err)
		}
		out = append(out, migration{Version: version, Name: parts[1], SQL: string(raw)})
	}
	if _, ok := seen[legacyVersion]; ok {
		return nil, fmt.Errorf("db: migration version %d is reserved for the legacy column backfill", legacyVersion)
	}
	out = append(out, migration{
		Version:    legacyVersion,
		Name:       "v0_01_column_backfill",
		Statements: legacyColumnBackfill,
		Tolerant:   true,
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

func migrationChecksum(m migration) string {
	src := m.SQL
	if m.Statements != nil {
		src = strings.Join(m.Statements, ";\n")
	}
	sum := sha256.Sum256([]byte(src))
	return hex.EncodeToString(sum[:])
}

// migrate applies all pending migrations, verifying checksums of applied ones.
func (d *DB) migrate() error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		checksum   TEXT NOT NULL,
		applied_at TEXT NOT NULL
	) STRICT`); err != nil {
		return fmt.Errorf("db: create schema_migrations: %w", err)
	}

	applied := map[int]string{}
	rows, err := d.Query(`SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("db: read schema_migrations: %w", err)
	}
	for rows.Next() {
		var v int
		var sum string
		if err := rows.Scan(&v, &sum); err != nil {
			rows.Close()
			return fmt.Errorf("db: scan schema_migrations: %w", err)
		}
		applied[v] = sum
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	migs, err := loadMigrations()
	if err != nil {
		return err
	}
	for _, m := range migs {
		sum := migrationChecksum(m)
		if prev, ok := applied[m.Version]; ok {
			if prev != sum {
				return fmt.Errorf("db: migration %04d (%s) changed after it was applied; refusing to continue", m.Version, m.Name)
			}
			continue
		}
		if err := d.applyMigration(m, sum); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) applyMigration(m migration, sum string) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if m.Statements != nil {
		for _, stmt := range m.Statements {
			if _, err := tx.Exec(stmt); err != nil {
				if m.Tolerant && strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
					continue
				}
				return fmt.Errorf("db: migration %04d (%s): %q: %w", m.Version, m.Name, stmt, err)
			}
		}
	} else if _, err := tx.Exec(m.SQL); err != nil {
		return fmt.Errorf("db: migration %04d (%s): %w", m.Version, m.Name, err)
	}

	if _, err := tx.Exec(`INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES (?,?,?,?)`,
		m.Version, m.Name, sum, Now()); err != nil {
		return fmt.Errorf("db: record migration %04d: %w", m.Version, err)
	}
	return tx.Commit()
}

// MigrationVersion returns the highest applied migration version.
func (d *DB) MigrationVersion() (int, error) {
	var v int
	if err := d.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}
