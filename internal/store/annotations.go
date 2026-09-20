package store

import (
	"context"
	"database/sql"
	"errors"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

// Annotation is a review note attached to a workspace/project/video/shot/asset.
type Annotation struct {
	ID          string
	WorkspaceID string
	ProjectID   string
	VideoID     sql.NullString
	ShotID      sql.NullString
	AssetID     sql.NullString
	TimecodeMs  sql.NullInt64
	Body        string
	Status      string
	CreatedBy   string
	AssignedTo  sql.NullString
	CreatedAt   string
}

// CreateAnnotation inserts a review annotation.
func (s *Store) CreateAnnotation(ctx context.Context, workspaceID, projectID, videoID, shotID, assetID, body, createdBy string) (*Annotation, error) {
	now := db.Now()
	a := &Annotation{ID: ids.New(), WorkspaceID: workspaceID, ProjectID: projectID, Body: body, Status: "open", CreatedBy: createdBy, CreatedAt: now}
	if videoID != "" {
		a.VideoID = sql.NullString{String: videoID, Valid: true}
	}
	if shotID != "" {
		a.ShotID = sql.NullString{String: shotID, Valid: true}
	}
	if assetID != "" {
		a.AssetID = sql.NullString{String: assetID, Valid: true}
	}
	_, err := s.ExecContext(ctx, `INSERT INTO annotations (id, workspace_id, project_id, video_id, shot_id, asset_id, body, status, created_by_user_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?, 'open', ?, ?, ?)`,
		a.ID, workspaceID, projectID, nullStr(videoID), nullStr(shotID), nullStr(assetID), body, createdBy, now, now)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// ListAnnotationsForVideo returns annotations for a video ordered newest first.
func (s *Store) ListAnnotationsForVideo(ctx context.Context, videoID string) ([]Annotation, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, workspace_id, project_id, video_id, shot_id, asset_id, timecode_ms, body, status, created_by_user_id, assigned_to_user_id, created_at
		FROM annotations WHERE video_id=? ORDER BY created_at DESC`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Annotation
	for rows.Next() {
		var a Annotation
		if err := rows.Scan(&a.ID, &a.WorkspaceID, &a.ProjectID, &a.VideoID, &a.ShotID, &a.AssetID, &a.TimecodeMs, &a.Body, &a.Status, &a.CreatedBy, &a.AssignedTo, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAnnotation fetches an annotation in workspace scope.
func (s *Store) GetAnnotation(ctx context.Context, workspaceID, id string) (*Annotation, error) {
	row := s.QueryRowContext(ctx, `SELECT id, workspace_id, project_id, video_id, shot_id, asset_id, timecode_ms, body, status, created_by_user_id, assigned_to_user_id, created_at
		FROM annotations WHERE id=? AND workspace_id=?`, id, workspaceID)
	var a Annotation
	err := row.Scan(&a.ID, &a.WorkspaceID, &a.ProjectID, &a.VideoID, &a.ShotID, &a.AssetID, &a.TimecodeMs, &a.Body, &a.Status, &a.CreatedBy, &a.AssignedTo, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &a, err
}

// SetAnnotationStatus opens or resolves an annotation.
func (s *Store) SetAnnotationStatus(ctx context.Context, id, status string) error {
	if status != "open" && status != "resolved" {
		status = "open"
	}
	_, err := s.ExecContext(ctx, `UPDATE annotations SET status=?, updated_at=? WHERE id=?`, status, db.Now(), id)
	return err
}

// ReassignAnnotation sets the assigned user.
func (s *Store) ReassignAnnotation(ctx context.Context, id, userID string) error {
	_, err := s.ExecContext(ctx, `UPDATE annotations SET assigned_to_user_id=?, updated_at=? WHERE id=?`, nullStr(userID), db.Now(), id)
	return err
}
