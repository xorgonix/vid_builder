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

// VideoSegment is one fixed-length anchor-to-anchor generation that contributes
// to a duration-based video.
type VideoSegment struct {
	ID           string
	RequestID    string
	WorkspaceID  string
	ProjectID    string
	VideoID      string
	ShotID       string
	Index        int
	Positive     string
	Negative     string
	StartAssetID sql.NullString
	EndAssetID   sql.NullString
	FrameCount   int
	FPS          float64
	Status       string
	AttemptID    sql.NullString
	AssetID      sql.NullString
	Error        string
	CreatedAt    string
	UpdatedAt    string
	Revision     int
}

// VideoAssembly is the ordered concatenation of a request's segment assets.
type VideoAssembly struct {
	ID               string
	RequestID        string
	WorkspaceID      string
	ProjectID        string
	VideoID          string
	ShotID           string
	Status           string
	AssetID          sql.NullString
	SegmentAssetJSON string
	Error            string
	CreatedAt        string
	UpdatedAt        string
	Revision         int
}

const segmentColumns = `id, request_id, workspace_id, project_id, video_id, shot_id, idx, positive, negative,
	start_asset_id, end_asset_id, frame_count, fps, status, attempt_id, asset_id, error, created_at, updated_at, revision`

// SegmentInput is one segment to persist when planning a duration video.
type SegmentInput struct {
	Positive     string
	Negative     string
	StartAssetID string
	EndAssetID   string
	FrameCount   int
	FPS          float64
	AttemptID    string
}

// InsertSegments writes a request's segments in index order.
func (s *Store) InsertSegments(ctx context.Context, requestID, workspaceID, projectID, videoID, shotID string, inputs []SegmentInput) ([]VideoSegment, error) {
	now := db.Now()
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := make([]VideoSegment, 0, len(inputs))
	for i, in := range inputs {
		seg := VideoSegment{
			ID: ids.New(), RequestID: requestID, WorkspaceID: workspaceID, ProjectID: projectID,
			VideoID: videoID, ShotID: shotID, Index: i, Positive: in.Positive, Negative: in.Negative,
			FrameCount: in.FrameCount, FPS: in.FPS, Status: "pending", CreatedAt: now, UpdatedAt: now, Revision: 1,
		}
		var start, end any
		if in.StartAssetID != "" {
			seg.StartAssetID = sql.NullString{String: in.StartAssetID, Valid: true}
			start = in.StartAssetID
		}
		if in.EndAssetID != "" {
			seg.EndAssetID = sql.NullString{String: in.EndAssetID, Valid: true}
			end = in.EndAssetID
		}
		var attempt any
		if in.AttemptID != "" {
			seg.AttemptID = sql.NullString{String: in.AttemptID, Valid: true}
			attempt = in.AttemptID
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO video_segments (`+segmentColumns+`)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			seg.ID, seg.RequestID, seg.WorkspaceID, seg.ProjectID, seg.VideoID, seg.ShotID, seg.Index,
			seg.Positive, seg.Negative, start, end, seg.FrameCount, seg.FPS, seg.Status, attempt, nil, "",
			now, now, 1); err != nil {
			return nil, fmt.Errorf("insert video segment: %w", err)
		}
		out = append(out, seg)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListSegments returns a request's segments in index order.
func (s *Store) ListSegments(ctx context.Context, requestID string) ([]VideoSegment, error) {
	rows, err := s.QueryContext(ctx, `SELECT `+segmentColumns+` FROM video_segments WHERE request_id=? ORDER BY idx`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VideoSegment
	for rows.Next() {
		seg, err := scanSegment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *seg)
	}
	return out, rows.Err()
}

// GetSegment fetches one segment by id.
func (s *Store) GetSegment(ctx context.Context, id string) (*VideoSegment, error) {
	row := s.QueryRowContext(ctx, `SELECT `+segmentColumns+` FROM video_segments WHERE id=?`, id)
	return scanSegment(row)
}

// SetSegmentRunning marks a segment running with its attempt id.
func (s *Store) SetSegmentRunning(ctx context.Context, id, attemptID string) error {
	var attempt any
	if attemptID != "" {
		attempt = attemptID
	}
	_, err := s.ExecContext(ctx, `UPDATE video_segments SET status='running', attempt_id=?, error='', revision=revision+1, updated_at=? WHERE id=?`,
		attempt, db.Now(), id)
	return err
}

// SetSegmentSucceeded records the produced segment asset.
func (s *Store) SetSegmentSucceeded(ctx context.Context, id, assetID string) error {
	_, err := s.ExecContext(ctx, `UPDATE video_segments SET status='succeeded', asset_id=?, error='', revision=revision+1, updated_at=? WHERE id=?`,
		assetID, db.Now(), id)
	return err
}

// SetSegmentFailed records a segment failure.
func (s *Store) SetSegmentFailed(ctx context.Context, id, errMsg string) error {
	_, err := s.ExecContext(ctx, `UPDATE video_segments SET status='failed', error=?, revision=revision+1, updated_at=? WHERE id=?`,
		errMsg, db.Now(), id)
	return err
}

// ResetSegmentForRetry returns a failed segment to pending, preserving its
// endpoints and prompt so only that segment is regenerated (spec: retry isolation).
func (s *Store) ResetSegmentForRetry(ctx context.Context, id string) error {
	res, err := s.ExecContext(ctx, `UPDATE video_segments SET status='pending', error='', revision=revision+1, updated_at=?
		WHERE id=? AND status IN ('failed','pending')`, db.Now(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("segment is not retryable")
	}
	return nil
}

// AllSegmentsSucceeded reports whether every segment of a request has succeeded.
func (s *Store) AllSegmentsSucceeded(ctx context.Context, requestID string) (bool, error) {
	var total, done int
	if err := s.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(status='succeeded'),0) FROM video_segments WHERE request_id=?`, requestID).Scan(&total, &done); err != nil {
		return false, err
	}
	return total > 0 && total == done, nil
}

// UpsertAssembly creates (or returns) the pending assembly for a request.
func (s *Store) UpsertAssembly(ctx context.Context, requestID, workspaceID, projectID, videoID, shotID string) (*VideoAssembly, error) {
	if a, err := s.GetAssembly(ctx, requestID); err == nil {
		return a, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	now := db.Now()
	a := &VideoAssembly{
		ID: ids.New(), RequestID: requestID, WorkspaceID: workspaceID, ProjectID: projectID,
		VideoID: videoID, ShotID: shotID, Status: "pending", SegmentAssetJSON: "[]",
		CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	if _, err := s.ExecContext(ctx, `INSERT INTO video_assemblies (id, request_id, workspace_id, project_id, video_id, shot_id, status, asset_id, segment_asset_ids_json, error, created_at, updated_at, revision)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.RequestID, a.WorkspaceID, a.ProjectID, a.VideoID, a.ShotID, a.Status, nil, a.SegmentAssetJSON, "", now, now, 1); err != nil {
		return nil, fmt.Errorf("insert video assembly: %w", err)
	}
	return a, nil
}

// GetAssembly returns the request's assembly, or ErrNotFound.
func (s *Store) GetAssembly(ctx context.Context, requestID string) (*VideoAssembly, error) {
	row := s.QueryRowContext(ctx, `SELECT id, request_id, workspace_id, project_id, video_id, shot_id, status, asset_id, segment_asset_ids_json, error, created_at, updated_at, revision
		FROM video_assemblies WHERE request_id=?`, requestID)
	var a VideoAssembly
	err := row.Scan(&a.ID, &a.RequestID, &a.WorkspaceID, &a.ProjectID, &a.VideoID, &a.ShotID, &a.Status, &a.AssetID, &a.SegmentAssetJSON, &a.Error, &a.CreatedAt, &a.UpdatedAt, &a.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ListAssembliesForVideo returns a video's duration assemblies, newest first.
func (s *Store) ListAssembliesForVideo(ctx context.Context, videoID string) ([]VideoAssembly, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, request_id, workspace_id, project_id, video_id, shot_id, status, asset_id, segment_asset_ids_json, error, created_at, updated_at, revision
		FROM video_assemblies WHERE video_id=? ORDER BY created_at DESC`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VideoAssembly
	for rows.Next() {
		var a VideoAssembly
		if err := rows.Scan(&a.ID, &a.RequestID, &a.WorkspaceID, &a.ProjectID, &a.VideoID, &a.ShotID, &a.Status, &a.AssetID, &a.SegmentAssetJSON, &a.Error, &a.CreatedAt, &a.UpdatedAt, &a.Revision); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetAssemblyRunning marks the assembly running.
func (s *Store) SetAssemblyRunning(ctx context.Context, id string) error {
	_, err := s.ExecContext(ctx, `UPDATE video_assemblies SET status='running', error='', revision=revision+1, updated_at=? WHERE id=?`, db.Now(), id)
	return err
}

// SetAssemblySucceeded records the assembled asset and the ordered segment lineage.
func (s *Store) SetAssemblySucceeded(ctx context.Context, id, assetID string, segmentAssetIDs []string) error {
	raw, _ := json.Marshal(segmentAssetIDs)
	_, err := s.ExecContext(ctx, `UPDATE video_assemblies SET status='succeeded', asset_id=?, segment_asset_ids_json=?, error='', revision=revision+1, updated_at=? WHERE id=?`,
		assetID, string(raw), db.Now(), id)
	return err
}

// SetAssemblyFailed records an assembly failure.
func (s *Store) SetAssemblyFailed(ctx context.Context, id, errMsg string) error {
	_, err := s.ExecContext(ctx, `UPDATE video_assemblies SET status='failed', error=?, revision=revision+1, updated_at=? WHERE id=?`, errMsg, db.Now(), id)
	return err
}

// EnqueueJob queues a job of any supported type for an aggregate.
func (s *Store) EnqueueJob(ctx context.Context, workspaceID, jobType, aggregateType, aggregateID string) (*Job, error) {
	now := db.Now()
	j := &Job{ID: ids.New(), WorkspaceID: workspaceID, JobType: jobType, AggregateType: aggregateType, AggregateID: aggregateID, State: "queued", CreatedAt: now}
	if _, err := s.ExecContext(ctx, `INSERT INTO jobs (id, workspace_id, job_type, aggregate_type, aggregate_id, state, priority, request_json, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, j.ID, workspaceID, jobType, aggregateType, aggregateID, "queued", 0, "{}", now); err != nil {
		return nil, err
	}
	return j, nil
}

func scanSegment(sc scanner) (*VideoSegment, error) {
	var x VideoSegment
	err := sc.Scan(&x.ID, &x.RequestID, &x.WorkspaceID, &x.ProjectID, &x.VideoID, &x.ShotID, &x.Index,
		&x.Positive, &x.Negative, &x.StartAssetID, &x.EndAssetID, &x.FrameCount, &x.FPS, &x.Status,
		&x.AttemptID, &x.AssetID, &x.Error, &x.CreatedAt, &x.UpdatedAt, &x.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &x, nil
}
