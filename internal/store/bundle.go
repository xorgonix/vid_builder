package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

// queryMaps runs a query and returns rows as generic maps.
func (s *Store) queryMaps(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	rows, err := s.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			switch v := vals[i].(type) {
			case []byte:
				m[c] = string(v)
			default:
				m[c] = v
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// PackageAsset references a stored media file for packaging.
type PackageAsset struct {
	ID         string
	StorageURI string
	MimeType   string
	Checksum   string
	Kind       string
}

// ProjectBundle gathers a portable project export: relational rows plus a
// media manifest. Large binaries are referenced, not embedded in the JSON.
type ProjectBundle struct {
	Data    map[string]any
	Assets  []PackageAsset
	Project *Project
}

// BuildProjectBundle collects a project's rows and referenced media.
func (s *Store) BuildProjectBundle(ctx context.Context, projectID string) (*ProjectBundle, error) {
	project, err := s.GetProjectByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	data := map[string]any{}
	tables := []struct {
		key   string
		query string
	}{
		{"videos", `SELECT * FROM videos WHERE project_id=? ORDER BY position`},
		{"sections", `SELECT * FROM sections WHERE video_id IN (SELECT id FROM videos WHERE project_id=?) ORDER BY position`},
		{"scenes", `SELECT * FROM scenes WHERE section_id IN (SELECT id FROM sections WHERE video_id IN (SELECT id FROM videos WHERE project_id=?)) ORDER BY position`},
		{"shots", `SELECT * FROM shots WHERE scene_id IN (SELECT id FROM scenes WHERE section_id IN (SELECT id FROM sections WHERE video_id IN (SELECT id FROM videos WHERE project_id=?))) ORDER BY position`},
		{"characters", `SELECT * FROM characters WHERE project_id=?`},
		{"locations", `SELECT * FROM locations WHERE project_id=?`},
		{"style_rules", `SELECT * FROM style_rules WHERE project_id=?`},
		{"project_briefs", `SELECT * FROM project_briefs WHERE project_id=?`},
		{"proposals", `SELECT * FROM proposals WHERE project_id=?`},
		{"discovery_threads", `SELECT * FROM discovery_threads WHERE project_id=?`},
	}
	for _, t := range tables {
		rows, err := s.queryMaps(ctx, t.query, projectID)
		if err != nil {
			return nil, fmt.Errorf("bundle %s: %w", t.key, err)
		}
		data[t.key] = rows
	}
	// Shot specs and blockings via joins.
	if specs, err := s.queryMaps(ctx, `SELECT ss.* FROM shot_specs ss JOIN shots sh ON sh.id=ss.shot_id
		JOIN scenes sc ON sc.id=sh.scene_id JOIN sections se ON se.id=sc.section_id
		JOIN videos v ON v.id=se.video_id WHERE v.project_id=?`, projectID); err == nil {
		data["shot_specs"] = specs
	}
	if blockings, err := s.queryMaps(ctx, `SELECT b.* FROM shot_blockings b JOIN shot_specs ss ON ss.id=b.shot_spec_id
		JOIN shots sh ON sh.id=ss.shot_id JOIN scenes sc ON sc.id=sh.scene_id JOIN sections se ON se.id=sc.section_id
		JOIN videos v ON v.id=se.video_id WHERE v.project_id=?`, projectID); err == nil {
		data["shot_blockings"] = blockings
	}
	data["project"] = project

	assets, err := s.queryMaps(ctx, `SELECT id, storage_uri, mime_type, checksum_sha256, kind FROM assets WHERE project_id=? AND deleted_at IS NULL`, projectID)
	if err != nil {
		return nil, err
	}
	data["assets"] = assets
	bundle := &ProjectBundle{Data: data, Project: project}
	for _, a := range assets {
		bundle.Assets = append(bundle.Assets, PackageAsset{
			ID:         asString(a["id"]),
			StorageURI: asString(a["storage_uri"]),
			MimeType:   asString(a["mime_type"]),
			Checksum:   asString(a["checksum_sha256"]),
			Kind:       asString(a["kind"]),
		})
	}
	return bundle, nil
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// MarshalJSON renders the bundle data with a schema/manifest header.
func (b *ProjectBundle) MarshalJSON() ([]byte, error) {
	payload := map[string]any{
		"schema":  "vid-build-project-package",
		"version": 1,
		"data":    b.Data,
		"media":   b.Assets,
	}
	return json.MarshalIndent(payload, "", "  ")
}

// EmitPackageJob creates a durable job to build a project package.
func (s *Store) EmitPackageJob(ctx context.Context, workspaceID, projectID string) (*Job, error) {
	now := db.Now()
	j := &Job{ID: ids.New(), WorkspaceID: workspaceID, JobType: "package", AggregateType: "project", AggregateID: projectID, State: "queued", CreatedAt: now}
	if _, err := s.ExecContext(ctx, `INSERT INTO jobs (id, workspace_id, job_type, aggregate_type, aggregate_id, state, priority, request_json, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, j.ID, workspaceID, "package", "project", projectID, "queued", 0, "{}", now); err != nil {
		return nil, err
	}
	if _, err := s.ExecContext(ctx, `INSERT INTO job_events (id, job_id, sequence, phase, level, message, progress_basis_points, created_at)
		VALUES (?,?,0,'queued','info','Project package queued',0,?)`, ids.New(), j.ID, now); err != nil {
		return nil, err
	}
	return j, nil
}

// LatestPackageAsset returns the most recent package document for a project.
func (s *Store) LatestPackageAsset(ctx context.Context, projectID string) (*Asset, error) {
	row := s.QueryRowContext(ctx, `SELECT id, workspace_id, project_id, generation_attempt_id, kind, role, storage_uri, mime_type, size_bytes, checksum_sha256, width, height, duration_ms, created_at
		FROM assets WHERE project_id=? AND role='project_package' AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 1`, projectID)
	var a Asset
	err := row.Scan(&a.ID, &a.WorkspaceID, &a.ProjectID, &a.GenerationAttemptID, &a.Kind, &a.Role, &a.StorageURI, &a.MimeType, &a.SizeBytes, &a.Checksum, &a.Width, &a.Height, &a.DurationMs, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}
