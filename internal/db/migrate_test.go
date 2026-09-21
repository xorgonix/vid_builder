package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := Open(path, 5000, 4)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestMigrateFreshDatabase(t *testing.T) {
	d := openTemp(t)

	v, err := d.MigrationVersion()
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if v < legacyVersion {
		t.Fatalf("expected version >= %d, got %d", legacyVersion, v)
	}

	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n < 2 {
		t.Fatalf("expected at least 2 recorded migrations, got %d", n)
	}

	// Core v0.01 tables must exist after migration.
	for _, tbl := range []string{"users", "workspaces", "projects", "videos", "sections", "scenes", "shots", "shot_specs", "assets", "jobs", "pairing_tokens", "app_settings"} {
		var c int
		if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&c); err != nil {
			t.Fatalf("lookup %s: %v", tbl, err)
		}
		if c != 1 {
			t.Fatalf("table %s missing after migration", tbl)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	d := openTemp(t)

	before, _ := d.MigrationVersion()
	if err := d.migrate(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if err := d.migrate(); err != nil {
		t.Fatalf("third migrate: %v", err)
	}
	after, _ := d.MigrationVersion()
	if before != after {
		t.Fatalf("version changed on re-run: %d -> %d", before, after)
	}

	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n < 2 {
		t.Fatalf("expected at least 2 recorded migrations, got %d", n)
	}
}

func TestMigrateDetectsChecksumChange(t *testing.T) {
	d := openTemp(t)

	if _, err := d.Exec(`UPDATE schema_migrations SET checksum='tampered' WHERE version=1`); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	err := d.migrate()
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
}

func TestMigrationVersionOnEmpty(t *testing.T) {
	d := openTemp(t)
	if _, err := d.Exec(`DELETE FROM schema_migrations`); err != nil {
		t.Fatalf("clear: %v", err)
	}
	v, err := d.MigrationVersion()
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if v != 0 {
		t.Fatalf("expected 0, got %d", v)
	}
}

// TestMigrateUpgradesV01Database simulates a real v0.01 database: the baseline
// tables exist without the later columns, and it already holds data. Opening it
// must apply the column backfill without losing rows.
func TestMigrateUpgradesV01Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v01.db")

	migs, err := loadMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var baseline migration
	for _, m := range migs {
		if m.Version == 1 {
			baseline = m
		}
	}
	if baseline.SQL == "" {
		t.Fatal("baseline migration 0001 not found")
	}

	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec(baseline.SQL); err != nil {
		t.Fatalf("apply baseline: %v", err)
	}
	now := Now()
	if _, err := raw.Exec(`INSERT INTO users (id,email_normalized,display_name,password_hash,status,is_system_admin,created_at,updated_at)
		VALUES ('u1','a@b.c','A','x','active',0,?,?)`, now, now); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO workspaces (id,name,slug,kind,created_by_user_id,created_at,updated_at)
		VALUES ('w1','W','w','personal','u1',?,?)`, now, now); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO projects (id,workspace_id,title,description,status,created_by_user_id,created_at,updated_at)
		VALUES ('p1','w1','Legacy project','','active','u1',?,?)`, now, now); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	// A pre-existing generation request + attempt in the OLD shape (no new columns).
	if _, err := raw.Exec(`INSERT INTO videos (id,project_id,title,created_by_user_id,created_at,updated_at)
		VALUES ('v1','p1','V','u1',?,?)`, now, now); err != nil {
		t.Fatalf("seed video: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO shot_specs (id,shot_id,kind,version,created_by_user_id,created_at)
		VALUES ('s1','nonexistent','motion',1,'u1',?)`, now); err == nil {
		// foreign keys may be off in raw connection for this table; ignore.
	}
	raw.Close()

	// Now upgrade via the real entrypoint.
	d, err := Open(path, 5000, 4)
	if err != nil {
		t.Fatalf("upgrade open: %v", err)
	}
	defer d.Close()

	var projects int
	if err := d.QueryRow(`SELECT COUNT(*) FROM projects WHERE id='p1'`).Scan(&projects); err != nil {
		t.Fatalf("project count: %v", err)
	}
	if projects != 1 {
		t.Fatalf("legacy project lost during migration (got %d)", projects)
	}
	if v, _ := d.MigrationVersion(); v < legacyVersion {
		t.Fatalf("expected version >= %d after upgrade, got %d", legacyVersion, v)
	}
	// The added columns must now be usable.
	if _, err := d.Exec(`UPDATE generation_attempts SET workflow_version_id='wv', currency='USD' WHERE 1=0`); err != nil {
		t.Fatalf("new columns not present after upgrade: %v", err)
	}
	if _, err := d.Exec(`UPDATE shot_specs SET prompt_layers_json='[]' WHERE 1=0`); err != nil {
		t.Fatalf("shot_specs.prompt_layers_json not present after upgrade: %v", err)
	}
	if _, err := d.Exec(`UPDATE projects SET default_model_profile_id=NULL WHERE 1=0`); err != nil {
		t.Fatalf("projects.default_model_profile_id not present after upgrade: %v", err)
	}
}

// TestMigrateBackfillsBeats proves the beats migration attaches one implicit
// beat to every existing scene and moves existing shots under it (spec §5.1).
func TestMigrateBackfillsBeats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v01beats.db")

	migs, err := loadMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var baseline migration
	for _, m := range migs {
		if m.Version == 1 {
			baseline = m
		}
	}

	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec(baseline.SQL); err != nil {
		t.Fatalf("apply baseline: %v", err)
	}
	now := Now()
	seed := []string{
		`INSERT INTO users (id,email_normalized,display_name,password_hash,status,is_system_admin,created_at,updated_at) VALUES ('u1','a@b.c','A','x','active',0,'` + now + `','` + now + `')`,
		`INSERT INTO workspaces (id,name,slug,kind,created_by_user_id,created_at,updated_at) VALUES ('w1','W','w','personal','u1','` + now + `','` + now + `')`,
		`INSERT INTO projects (id,workspace_id,title,description,status,created_by_user_id,created_at,updated_at) VALUES ('p1','w1','P','','active','u1','` + now + `','` + now + `')`,
		`INSERT INTO videos (id,project_id,title,created_by_user_id,created_at,updated_at) VALUES ('v1','p1','V','u1','` + now + `','` + now + `')`,
		`INSERT INTO sections (id,video_id,title,created_at,updated_at) VALUES ('sec1','v1','S','` + now + `','` + now + `')`,
		`INSERT INTO scenes (id,section_id,title,synopsis,purpose,duration_budget_ms,position,created_at,updated_at) VALUES ('sc1','sec1','Scene 1','syn','purp',4000,0,'` + now + `','` + now + `')`,
		`INSERT INTO shots (id,scene_id,title,purpose,treatment,duration_target_ms,position,status,readiness,created_by_user_id,created_at,updated_at) VALUES ('sh1','sc1','Shot 1','p','t',2000,0,'draft','incomplete','u1','` + now + `','` + now + `')`,
	}
	for _, stmt := range seed {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	raw.Close()

	d, err := Open(path, 5000, 4)
	if err != nil {
		t.Fatalf("upgrade open: %v", err)
	}
	defer d.Close()

	var beatID string
	var implicit int
	if err := d.QueryRow(`SELECT id, implicit FROM beats WHERE scene_id='sc1'`).Scan(&beatID, &implicit); err != nil {
		t.Fatalf("implicit beat not created: %v", err)
	}
	if implicit != 1 {
		t.Fatalf("beat implicit = %d, want 1", implicit)
	}

	var shotBeat string
	if err := d.QueryRow(`SELECT COALESCE(beat_id,'') FROM shots WHERE id='sh1'`).Scan(&shotBeat); err != nil {
		t.Fatalf("shot beat lookup: %v", err)
	}
	if shotBeat != beatID {
		t.Fatalf("shot beat_id = %q, want %q", shotBeat, beatID)
	}

	rows, err := d.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
	if rows.Next() {
		rows.Close()
		t.Fatal("foreign key violations after beats migration")
	}
	rows.Close()

	// beat_id is required for new shots.
	if _, err := d.Exec(`INSERT INTO shots (id,scene_id,title,purpose,treatment,duration_target_ms,position,status,readiness,created_by_user_id,created_at,updated_at)
		VALUES ('sh2','sc1','Shot 2','p','t',1000,1,'draft','incomplete','u1','` + now + `','` + now + `')`); err == nil {
		t.Fatal("expected a shot without beat_id to be rejected")
	}
}
