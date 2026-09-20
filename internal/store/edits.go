package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

// Edit is an ordered arrangement of selected clips.
type Edit struct {
	ID        string
	VideoID   string
	Name      string
	Status    string
	Revision  int
	CreatedAt string
	UpdatedAt string
}

// EditItem is one clip placement in an edit.
type EditItem struct {
	ID            string
	EditID        string
	ShotID        sql.NullString
	AssetID       string
	Position      int
	SourceInMs    int
	SourceOutMs   int
	DurationMs    int
	TransitionIn  string
	TransitionOut string
	Revision      int
}

// Export is an FFmpeg assembly receipt.
type Export struct {
	ID           string
	EditID       string
	JobID        sql.NullString
	AssetID      sql.NullString
	ProfileJSON  string
	Status       string
	CreatedAt    string
	ErrorMessage sql.NullString
}

// GetEditByID fetches an edit without video scope (used by the worker).
func (s *Store) GetEditByID(ctx context.Context, editID string) (*Edit, error) {
	row := s.QueryRowContext(ctx, `SELECT id, video_id, name, status, revision, created_at, updated_at FROM edits WHERE id=?`, editID)
	var e Edit
	err := row.Scan(&e.ID, &e.VideoID, &e.Name, &e.Status, &e.Revision, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &e, err
}

// ListEdits returns edits for a video.
func (s *Store) ListEdits(ctx context.Context, videoID string) ([]Edit, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, video_id, name, status, revision, created_at, updated_at FROM edits WHERE video_id=? ORDER BY created_at`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Edit
	for rows.Next() {
		var e Edit
		if err := rows.Scan(&e.ID, &e.VideoID, &e.Name, &e.Status, &e.Revision, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PrimaryEdit returns the first edit for a video, creating one if none exist.
func (s *Store) PrimaryEdit(ctx context.Context, videoID, userID string) (*Edit, error) {
	edits, err := s.ListEdits(ctx, videoID)
	if err != nil {
		return nil, err
	}
	if len(edits) > 0 {
		return &edits[0], nil
	}
	now := db.Now()
	e := &Edit{ID: ids.New(), VideoID: videoID, Name: "Main Edit", Status: "draft", Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := s.ExecContext(ctx, `INSERT INTO edits (id, video_id, name, status, created_by_user_id, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
		e.ID, videoID, e.Name, e.Status, userID, now, now); err != nil {
		return nil, err
	}
	return e, nil
}

// GetEdit fetches an edit scoped to a video.
func (s *Store) GetEdit(ctx context.Context, videoID, editID string) (*Edit, error) {
	row := s.QueryRowContext(ctx, `SELECT id, video_id, name, status, revision, created_at, updated_at FROM edits WHERE id=? AND video_id=?`, editID, videoID)
	var e Edit
	err := row.Scan(&e.ID, &e.VideoID, &e.Name, &e.Status, &e.Revision, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &e, err
}

// ListEditItems returns edit items ordered by position.
func (s *Store) ListEditItems(ctx context.Context, editID string) ([]EditItem, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, edit_id, shot_id, asset_id, position, source_in_ms, source_out_ms, duration_ms, transition_in, transition_out, revision
		FROM edit_items WHERE edit_id=? ORDER BY position, created_at`, editID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EditItem
	for rows.Next() {
		var it EditItem
		if err := rows.Scan(&it.ID, &it.EditID, &it.ShotID, &it.AssetID, &it.Position, &it.SourceInMs, &it.SourceOutMs, &it.DurationMs, &it.TransitionIn, &it.TransitionOut, &it.Revision); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// AddEditItem appends a clip to an edit.
func (s *Store) AddEditItem(ctx context.Context, editID, shotID, assetID string, durationMs int, transitionIn, transitionOut string) (*EditItem, error) {
	now := db.Now()
	var pos int
	if err := s.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),-1)+1 FROM edit_items WHERE edit_id=?`, editID).Scan(&pos); err != nil {
		return nil, err
	}
	if transitionIn != "cut" && transitionIn != "fade" && transitionIn != "dissolve" {
		transitionIn = "cut"
	}
	if transitionOut != "cut" && transitionOut != "fade" && transitionOut != "dissolve" {
		transitionOut = "cut"
	}
	var shot any
	if shotID != "" {
		shot = shotID
	}
	it := &EditItem{ID: ids.New(), EditID: editID, AssetID: assetID, Position: pos, DurationMs: durationMs, TransitionIn: transitionIn, TransitionOut: transitionOut, Revision: 1}
	_, err := s.ExecContext(ctx, `INSERT INTO edit_items (id, edit_id, shot_id, asset_id, position, source_in_ms, source_out_ms, duration_ms, transition_in, transition_out, created_at, updated_at)
		VALUES (?,?,?,?,?,0,?,?,?,?,?,?)`, it.ID, editID, shot, assetID, pos, durationMs, durationMs, transitionIn, transitionOut, now, now)
	if err != nil {
		return nil, err
	}
	return it, nil
}

// UpdateEditItem updates trims/transition with an optimistic revision check.
func (s *Store) UpdateEditItem(ctx context.Context, id string, sourceInMs, sourceOutMs, durationMs int, transitionIn, transitionOut string, expectedRevision int) error {
	res, err := s.ExecContext(ctx, `UPDATE edit_items SET source_in_ms=?, source_out_ms=?, duration_ms=?, transition_in=?, transition_out=?, revision=revision+1, updated_at=? WHERE id=? AND revision=?`,
		sourceInMs, sourceOutMs, durationMs, transitionIn, transitionOut, db.Now(), id, expectedRevision)
	return affectedOrConflict(res, err)
}

// MoveEditItem swaps an item with its neighbour.
func (s *Store) MoveEditItem(ctx context.Context, id string, direction int) error {
	return s.movePosition(ctx, "edit_items", "edit_id", id, direction)
}

// DeleteEditItem removes an item from an edit (the asset is untouched).
func (s *Store) DeleteEditItem(ctx context.Context, id string) error {
	_, err := s.ExecContext(ctx, `DELETE FROM edit_items WHERE id=?`, id)
	return err
}

// SetEditStatus updates an edit status.
func (s *Store) SetEditStatus(ctx context.Context, editID, status string) error {
	_, err := s.ExecContext(ctx, `UPDATE edits SET status=?, updated_at=? WHERE id=?`, status, db.Now(), editID)
	return err
}

// CreateExport queues an export record.
func (s *Store) CreateExport(ctx context.Context, editID, jobID, profileJSON string) (*Export, error) {
	now := db.Now()
	e := &Export{ID: ids.New(), EditID: editID, ProfileJSON: profileJSON, Status: "queued", CreatedAt: now}
	_, err := s.ExecContext(ctx, `INSERT INTO exports (id, edit_id, job_id, profile_json, status, created_by_user_id, created_at) VALUES (?,?,?,?,?,?,?)`,
		e.ID, editID, jobID, profileJSON, "queued", "", now)
	if err != nil {
		return nil, err
	}
	return e, nil
}

// GetExport fetches an export.
func (s *Store) GetExport(ctx context.Context, id string) (*Export, error) {
	row := s.QueryRowContext(ctx, `SELECT id, edit_id, job_id, asset_id, profile_json, status, created_at, error_message FROM exports WHERE id=?`, id)
	var e Export
	err := row.Scan(&e.ID, &e.EditID, &e.JobID, &e.AssetID, &e.ProfileJSON, &e.Status, &e.CreatedAt, &e.ErrorMessage)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &e, err
}

// CompleteExport records the exported asset.
func (s *Store) CompleteExport(ctx context.Context, id, assetID string) error {
	_, err := s.ExecContext(ctx, `UPDATE exports SET asset_id=?, status='complete' WHERE id=?`, assetID, id)
	return err
}

// FailExport records an export failure.
func (s *Store) FailExport(ctx context.Context, id, message string) error {
	_, err := s.ExecContext(ctx, `UPDATE exports SET status='failed', error_message=? WHERE id=?`, message, id)
	return err
}

// EmitExportJob creates a durable export job for the worker.
func (s *Store) EmitExportJob(ctx context.Context, workspaceID, exportID string) (*Job, error) {
	now := db.Now()
	j := &Job{ID: ids.New(), WorkspaceID: workspaceID, JobType: "export", AggregateType: "export", AggregateID: exportID, State: "queued", CreatedAt: now}
	if _, err := s.ExecContext(ctx, `INSERT INTO jobs (id, workspace_id, job_type, aggregate_type, aggregate_id, state, priority, request_json, created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		j.ID, j.WorkspaceID, "export", "export", exportID, "queued", 0, "{}", now); err != nil {
		return nil, err
	}
	if _, err := s.ExecContext(ctx, `INSERT INTO job_events (id, job_id, sequence, phase, level, message, progress_basis_points, created_at) VALUES (?,?,0,'queued','info','Export queued',0,?)`, ids.New(), j.ID, now); err != nil {
		return nil, err
	}
	return j, nil
}

// SetExportJob ties a queued export to its job.
func (s *Store) SetExportJob(ctx context.Context, exportID, jobID string) error {
	_, err := s.ExecContext(ctx, `UPDATE exports SET job_id=? WHERE id=?`, jobID, exportID)
	return err
}

var _ = fmt.Sprintf
