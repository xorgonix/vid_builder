package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

// DimensionsForFormat returns width/height defaults for a video format.
func DimensionsForFormat(format string) (int, int) {
	switch format {
	case "vertical":
		return 1080, 1920
	case "square":
		return 1080, 1080
	default:
		return 1920, 1080
	}
}

// ListProjects returns non-archived projects for a workspace.
func (s *Store) ListProjects(ctx context.Context, workspaceID string) ([]Project, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, workspace_id, title, description, status, revision, created_at, updated_at
		FROM projects WHERE workspace_id = ? AND archived_at IS NULL ORDER BY updated_at DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.WorkspaceID, &p.Title, &p.Description, &p.Status, &p.Revision, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProject fetches a project scoped to a workspace.
func (s *Store) GetProject(ctx context.Context, workspaceID, projectID string) (*Project, error) {
	row := s.QueryRowContext(ctx, `SELECT id, workspace_id, title, description, status, revision, created_at, updated_at
		FROM projects WHERE id = ? AND workspace_id = ?`, projectID, workspaceID)
	var p Project
	err := row.Scan(&p.ID, &p.WorkspaceID, &p.Title, &p.Description, &p.Status, &p.Revision, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// CreateProjectWithVideo transactionally creates project, first video, root
// section, discovery thread and draft edit (spec §15.1).
func (s *Store) CreateProjectWithVideo(ctx context.Context, workspaceID, userID, title, description, videoTitle, purpose, format string) (*Project, *Video, error) {
	if videoTitle == "" {
		videoTitle = title
	}
	w, h := DimensionsForFormat(format)
	now := db.Now()
	p := &Project{ID: ids.New(), WorkspaceID: workspaceID, Title: title, Description: description, Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now}
	v := &Video{ID: ids.New(), ProjectID: p.ID, Title: videoTitle, Purpose: purpose, Format: format, Width: w, Height: h, FPSNum: 30, FPSDen: 1, Status: "developing", Revision: 1, CreatedAt: now, UpdatedAt: now}

	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `INSERT INTO projects (id, workspace_id, title, description, status, created_by_user_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, p.ID, workspaceID, title, description, "active", userID, now, now); err != nil {
		return nil, nil, fmt.Errorf("insert project: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO videos (id, project_id, title, purpose, format, width, height, fps_num, fps_den, status, position, created_by_user_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.ID, p.ID, v.Title, v.Purpose, v.Format, v.Width, v.Height, v.FPSNum, v.FPSDen, v.Status, 0, userID, now, now); err != nil {
		return nil, nil, fmt.Errorf("insert video: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sections (id, video_id, title, purpose, position, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?)`, ids.New(), v.ID, "Root", "Implicit root section", 0, now, now); err != nil {
		return nil, nil, fmt.Errorf("insert root section: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO discovery_threads (id, project_id, video_id, title, status, created_by_user_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, ids.New(), p.ID, v.ID, "Discovery", "open", userID, now, now); err != nil {
		return nil, nil, fmt.Errorf("insert discovery thread: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO edits (id, video_id, name, status, created_by_user_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?)`, ids.New(), v.ID, "Main Edit", "draft", userID, now, now); err != nil {
		return nil, nil, fmt.Errorf("insert draft edit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return p, v, nil
}

// ListVideos returns videos for a project ordered by position.
func (s *Store) ListVideos(ctx context.Context, projectID string) ([]Video, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, project_id, title, purpose, audience, desired_reaction, format, width, height, fps_num, fps_den, target_duration_ms, status, position, revision, created_at, updated_at
		FROM videos WHERE project_id = ? ORDER BY position, created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVideos(rows)
}

// GetVideo fetches a video scoped to its project.
func (s *Store) GetVideo(ctx context.Context, projectID, videoID string) (*Video, error) {
	row := s.QueryRowContext(ctx, `SELECT id, project_id, title, purpose, audience, desired_reaction, format, width, height, fps_num, fps_den, target_duration_ms, status, position, revision, created_at, updated_at
		FROM videos WHERE id = ? AND project_id = ?`, videoID, projectID)
	v, err := scanVideo(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, err
}

func scanVideos(rows *sql.Rows) ([]Video, error) {
	var out []Video
	for rows.Next() {
		v, err := scanVideoFields(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(dest ...any) error }

func scanVideo(row *sql.Row) (*Video, error) {
	return scanVideoFields(row)
}

func scanVideoFields(sc scanner) (*Video, error) {
	var v Video
	err := sc.Scan(&v.ID, &v.ProjectID, &v.Title, &v.Purpose, &v.Audience, &v.DesiredReaction, &v.Format, &v.Width, &v.Height,
		&v.FPSNum, &v.FPSDen, &v.TargetDurationMs, &v.Status, &v.Position, &v.Revision, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// UpdateVideo updates editable video metadata with an optimistic revision check.
func (s *Store) UpdateVideo(ctx context.Context, projectID, videoID, title, purpose, audience, reaction, format string, targetDurationMs, expectedRevision int) error {
	w, h := DimensionsForFormat(format)
	now := db.Now()
	res, err := s.ExecContext(ctx, `UPDATE videos SET title=?, purpose=?, audience=?, desired_reaction=?, format=?, width=?, height=?, target_duration_ms=?, revision=revision+1, updated_at=?
		WHERE id=? AND project_id=? AND revision=?`, title, purpose, audience, reaction, format, w, h, targetDurationMs, now, videoID, projectID, expectedRevision)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	return nil
}

// CreateVideo adds another deliverable to a project.
func (s *Store) CreateVideo(ctx context.Context, projectID, userID, title, purpose, format string) (*Video, error) {
	w, h := DimensionsForFormat(format)
	now := db.Now()
	var pos int
	if err := s.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),-1)+1 FROM videos WHERE project_id=?`, projectID).Scan(&pos); err != nil {
		return nil, err
	}
	v := &Video{ID: ids.New(), ProjectID: projectID, Title: title, Purpose: purpose, Format: format, Width: w, Height: h, FPSNum: 30, FPSDen: 1, Status: "developing", Position: pos, Revision: 1, CreatedAt: now, UpdatedAt: now}
	_, err := s.ExecContext(ctx, `INSERT INTO videos (id, project_id, title, purpose, format, width, height, fps_num, fps_den, status, position, created_by_user_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.ID, projectID, title, purpose, format, w, h, v.FPSNum, v.FPSDen, v.Status, pos, userID, now, now)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// SetVideoAudienceAndDuration sets optional video fields after creation.
func (s *Store) SetVideoAudienceAndDuration(ctx context.Context, projectID, videoID, audience string, durationMs int) error {
	_, err := s.ExecContext(ctx, `UPDATE videos SET audience=?, target_duration_ms=?, updated_at=? WHERE id=? AND project_id=?`,
		audience, durationMs, db.Now(), videoID, projectID)
	return err
}

// CountVideosForProject returns the number of non-archived videos.
func (s *Store) CountVideosForProject(ctx context.Context, projectID string) (int, error) {
	var n int
	err := s.QueryRowContext(ctx, `SELECT COUNT(*) FROM videos WHERE project_id=? AND status != 'archived'`, projectID).Scan(&n)
	return n, err
}

// VideoStructureCounts returns section/scene/shot counts for a video.
func (s *Store) VideoStructureCounts(ctx context.Context, videoID string) (sections, scenes, shots int, err error) {
	if err = s.QueryRowContext(ctx, `SELECT COUNT(*) FROM sections WHERE video_id=?`, videoID).Scan(&sections); err != nil {
		return
	}
	if err = s.QueryRowContext(ctx, `SELECT COUNT(*) FROM scenes WHERE section_id IN (SELECT id FROM sections WHERE video_id=?)`, videoID).Scan(&scenes); err != nil {
		return
	}
	err = s.QueryRowContext(ctx, `SELECT COUNT(*) FROM shots WHERE scene_id IN (SELECT id FROM scenes WHERE section_id IN (SELECT id FROM sections WHERE video_id=?))`, videoID).Scan(&shots)
	return
}

// HasCurrentBrief reports whether a project has a non-superseded brief.
func (s *Store) HasCurrentBrief(ctx context.Context, projectID string) (bool, error) {
	var n int
	err := s.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_briefs WHERE project_id=? AND superseded_at IS NULL`, projectID).Scan(&n)
	return n > 0, err
}
