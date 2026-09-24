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

// VideoSegment is one adjacent-anchor generation inside a Super Shot. The anchor
// ids are authoritative at planning time; asset ids are resolved after the
// anchors succeed (spec §5.4).
type VideoSegment struct {
	ID                 string
	RequestID          string
	SuperShotRequestID sql.NullString
	WorkspaceID        string
	ProjectID          string
	VideoID            string
	ShotID             string
	Index              int
	StartAnchorID      sql.NullString
	EndAnchorID        sql.NullString
	Positive           string
	Negative           string
	ActionPrompt       string
	RawAction          string
	CompiledPrompt     string
	ContinuityJSON     string
	StartAssetID       sql.NullString
	EndAssetID         sql.NullString
	StartOffsetMS      int
	EndOffsetMS        int
	DurationMS         int
	FrameCount         int
	FPSNum             int
	FPSDen             int
	ContextMode        string
	ContextInputID     sql.NullString
	DependsOnSegmentID sql.NullString
	Stale              bool
	Status             string
	AttemptID          sql.NullString
	SelectedAttemptID  sql.NullString
	AssetID            sql.NullString
	Error              string
	CreatedAt          string
	UpdatedAt          string
	Revision           int
	QueuedInputHash    string
	InputRevision      int
}

// VideoAssembly is the ordered concatenation of a request's segment assets.
type VideoAssembly struct {
	ID                 string
	RequestID          string
	SuperShotRequestID sql.NullString
	WorkspaceID        string
	ProjectID          string
	VideoID            string
	ShotID             string
	Status             string
	AssetID            sql.NullString
	SegmentAssetJSON   string
	PlannedDurationMS  int
	ActualDurationMS   int
	JoinsJSON          string
	FFmpegReceipt      string
	InputRevision      int
	InputHash          string
	MeasuredState      string
	Error              string
	CreatedAt          string
	UpdatedAt          string
	Revision           int
	QueuedInputHash    string
}

const segmentColumns = `id, request_id, super_shot_request_id, workspace_id, project_id, video_id, shot_id, idx,
	start_anchor_id, end_anchor_id, positive, negative, action_prompt, raw_action, compiled_prompt, continuity_json,
	start_asset_id, end_asset_id, start_offset_ms, end_offset_ms, duration_ms, frame_count,
	fps_num, fps_den, context_mode, context_input_id, depends_on_segment_id, stale,
	status, attempt_id, selected_attempt_id, asset_id, error, created_at, updated_at, revision,
	queued_input_hash, input_revision`

// SegmentInput is one segment to persist when planning a Super Shot.
type SegmentInput struct {
	StartAnchorID      string
	EndAnchorID        string
	Positive           string
	Negative           string
	ActionPrompt       string
	RawAction          string
	CompiledPrompt     string
	ContinuityJSON     string
	StartAssetID       string
	EndAssetID         string
	StartOffsetMS      int
	EndOffsetMS        int
	DurationMS         int
	FrameCount         int
	FPSNum             int
	FPSDen             int
	ContextMode        string
	DependsOnSegmentID string
	AttemptID          string
}

// InsertSegments writes a request's segments in index order. superShotID links
// them to their Super Shot plan.
func (s *Store) InsertSegments(ctx context.Context, requestID, superShotID, workspaceID, projectID, videoID, shotID string, inputs []SegmentInput) ([]VideoSegment, error) {
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out, err := insertSegmentsTx(ctx, tx, requestID, superShotID, workspaceID, projectID, videoID, shotID, inputs)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func insertSegmentsTx(ctx context.Context, tx *sql.Tx, requestID, superShotID, workspaceID, projectID, videoID, shotID string, inputs []SegmentInput) ([]VideoSegment, error) {
	now := db.Now()
	out := make([]VideoSegment, 0, len(inputs))
	for i, in := range inputs {
		seg := VideoSegment{
			ID: ids.New(), RequestID: requestID, WorkspaceID: workspaceID, ProjectID: projectID,
			VideoID: videoID, ShotID: shotID, Index: i, Positive: in.Positive, Negative: in.Negative,
			ActionPrompt: in.ActionPrompt, RawAction: in.RawAction, CompiledPrompt: defaultString(in.CompiledPrompt, in.ActionPrompt),
			ContinuityJSON: defaultJSONArray(in.ContinuityJSON),
			StartOffsetMS: in.StartOffsetMS, EndOffsetMS: in.EndOffsetMS, DurationMS: in.DurationMS,
			FrameCount: in.FrameCount, FPSNum: in.FPSNum, FPSDen: in.FPSDen,
			ContextMode: defaultString(in.ContextMode, "anchor_only"), Status: "pending",
			CreatedAt: now, UpdatedAt: now, Revision: 1,
		}
		if seg.RawAction == "" {
			seg.RawAction = seg.ActionPrompt
		}
		if superShotID != "" {
			seg.SuperShotRequestID = sql.NullString{String: superShotID, Valid: true}
		}
		ns := func(v string) sql.NullString {
			if v == "" {
				return sql.NullString{}
			}
			return sql.NullString{String: v, Valid: true}
		}
		seg.StartAnchorID = ns(in.StartAnchorID)
		seg.EndAnchorID = ns(in.EndAnchorID)
		seg.StartAssetID = ns(in.StartAssetID)
		seg.EndAssetID = ns(in.EndAssetID)
		seg.AttemptID = ns(in.AttemptID)
		seg.SelectedAttemptID = seg.AttemptID
		seg.DependsOnSegmentID = ns(in.DependsOnSegmentID)
		if _, err := tx.ExecContext(ctx, `INSERT INTO video_segments (`+segmentColumns+`)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			seg.ID, seg.RequestID, nullSQL(seg.SuperShotRequestID), seg.WorkspaceID, seg.ProjectID, seg.VideoID, seg.ShotID, seg.Index,
			nullSQL(seg.StartAnchorID), nullSQL(seg.EndAnchorID), seg.Positive, seg.Negative, seg.ActionPrompt, seg.RawAction, seg.CompiledPrompt, seg.ContinuityJSON,
			nullSQL(seg.StartAssetID), nullSQL(seg.EndAssetID), seg.StartOffsetMS, seg.EndOffsetMS, seg.DurationMS, seg.FrameCount,
			seg.FPSNum, seg.FPSDen, seg.ContextMode, nullSQL(seg.ContextInputID), nullSQL(seg.DependsOnSegmentID), boolInt(seg.Stale),
			seg.Status, nullSQL(seg.AttemptID), nullSQL(seg.SelectedAttemptID), nullSQL(seg.AssetID), "", now, now, 1, "", 0); err != nil {
			return nil, fmt.Errorf("insert video segment: %w", err)
		}
		out = append(out, seg)
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
	return scanSegments(rows)
}

// ListSegmentsForSuperShot returns a plan's segments in index order.
func (s *Store) ListSegmentsForSuperShot(ctx context.Context, superShotID string) ([]VideoSegment, error) {
	rows, err := s.QueryContext(ctx, `SELECT `+segmentColumns+` FROM video_segments WHERE super_shot_request_id=? ORDER BY idx`, superShotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSegments(rows)
}

// GetSegment fetches one segment by id.
func (s *Store) GetSegment(ctx context.Context, id string) (*VideoSegment, error) {
	row := s.QueryRowContext(ctx, `SELECT `+segmentColumns+` FROM video_segments WHERE id=?`, id)
	return scanSegment(row)
}

// SetSegmentRunning marks a segment running. An empty attemptID retains the
// persisted attempt link (contract C04: starting execution must never clear it).
func (s *Store) SetSegmentRunning(ctx context.Context, id, attemptID string) error {
	var attempt any
	if attemptID != "" {
		attempt = attemptID
	}
	_, err := s.ExecContext(ctx, `UPDATE video_segments SET status='running', attempt_id=COALESCE(?, attempt_id), stale=0, error='', revision=revision+1, updated_at=? WHERE id=?`,
		attempt, db.Now(), id)
	return err
}

// SetSegmentSucceeded records the produced segment asset and the resolved
// endpoint assets actually used.
func (s *Store) SetSegmentSucceeded(ctx context.Context, id, assetID, startAssetID, endAssetID string) error {
	_, err := s.ExecContext(ctx, `UPDATE video_segments SET status='succeeded', asset_id=?, start_asset_id=?, end_asset_id=?, stale=0, error='', revision=revision+1, updated_at=? WHERE id=?`,
		assetID, nullIfEmpty(startAssetID), nullIfEmpty(endAssetID), db.Now(), id)
	return err
}

// SetSegmentFailed records a segment failure.
func (s *Store) SetSegmentFailed(ctx context.Context, id, errMsg string) error {
	_, err := s.ExecContext(ctx, `UPDATE video_segments SET status='failed', error=?, revision=revision+1, updated_at=? WHERE id=?`,
		errMsg, db.Now(), id)
	return err
}

// UpdateSegmentAction edits a segment's movement prompt while still draft.
func (s *Store) UpdateSegmentAction(ctx context.Context, id, actionPrompt string, revision int) error {
	res, err := s.ExecContext(ctx, `UPDATE video_segments SET action_prompt=?, positive=?, revision=revision+1, updated_at=? WHERE id=? AND revision=?`,
		actionPrompt, actionPrompt, db.Now(), id, revision)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	return nil
}

// UpdateSegmentActionRaw edits a segment's raw interval action and the compiled
// prompt derived from it, with an optimistic revision check (contract C06).
func (s *Store) UpdateSegmentActionRaw(ctx context.Context, id, rawAction, compiledPrompt string, revision int) error {
	res, err := s.ExecContext(ctx, `UPDATE video_segments SET raw_action=?, compiled_prompt=?, action_prompt=?, positive=?, revision=revision+1, updated_at=? WHERE id=? AND revision=?`,
		rawAction, compiledPrompt, rawAction, compiledPrompt, db.Now(), id, revision)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	return nil
}

// QueueSegmentInput freezes the canonical input hash for a segment's queued
// work. Publication is only possible while this hash is still set (R2).
func (s *Store) QueueSegmentInput(ctx context.Context, id, hash string) error {
	_, err := s.ExecContext(ctx, `UPDATE video_segments SET queued_input_hash=? WHERE id=?`, hash, id)
	return err
}

// ClearSegmentQueuedInput invalidates the frozen snapshot: creative inputs were
// edited after enqueue, so any queued run must be superseded before it publishes
// (R2). It bumps the immutable input revision by one.
func (s *Store) ClearSegmentQueuedInput(ctx context.Context, id string) error {
	_, err := s.ExecContext(ctx, `UPDATE video_segments SET queued_input_hash='', input_revision=input_revision+1, revision=revision+1, updated_at=? WHERE id=?`, db.Now(), id)
	return err
}

// SetSegmentSucceededIfCurrent records a segment success only when the segment's
// current attempt still matches the attempt that produced the output AND the
// frozen queue-time input snapshot was not invalidated by an edit (C06, R2).
// A late result from a superseded attempt or stale inputs cannot overwrite the
// new selection.
func (s *Store) SetSegmentSucceededIfCurrent(ctx context.Context, id, expectedAttemptID, assetID, startAssetID, endAssetID string) (int64, error) {
	res, err := s.ExecContext(ctx, `UPDATE video_segments SET status='succeeded', asset_id=?, start_asset_id=?, end_asset_id=?, stale=0, error='', revision=revision+1, updated_at=?
		WHERE id=? AND (attempt_id IS ? OR attempt_id=?) AND queued_input_hash != ''`,
		assetID, nullIfEmpty(startAssetID), nullIfEmpty(endAssetID), db.Now(), id, nullIfEmpty(expectedAttemptID), expectedAttemptID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ResetSegmentForRetry returns a failed/stale segment to pending, preserving its
// anchors and prompt so only that segment is regenerated (spec §7.3).
func (s *Store) ResetSegmentForRetry(ctx context.Context, id string) error {
	res, err := s.ExecContext(ctx, `UPDATE video_segments SET status='pending', stale=0, error='', revision=revision+1, updated_at=?
		WHERE id=? AND status IN ('failed','pending','succeeded')`, db.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("segment is not retryable")
	}
	return nil
}

// MarkSegmentStale marks one segment stale (its output must not be assembled).
func (s *Store) MarkSegmentStale(ctx context.Context, id string) error {
	_, err := s.ExecContext(ctx, `UPDATE video_segments SET stale=1, revision=revision+1, updated_at=? WHERE id=?`, db.Now(), id)
	return err
}

// MarkDownstreamStale marks every segment that transitively depends on the given
// segment stale (spec §15.6). In anchor-only plans there are no such
// dependencies and this is a no-op.
func (s *Store) MarkDownstreamStale(ctx context.Context, requestID, segmentID string) (int, error) {
	segments, err := s.ListSegments(ctx, requestID)
	if err != nil {
		return 0, err
	}
	dependents := map[string]bool{segmentID: true}
	changed := true
	for changed {
		changed = false
		for _, seg := range segments {
			if seg.DependsOnSegmentID.Valid && dependents[seg.DependsOnSegmentID.String] && !dependents[seg.ID] {
				dependents[seg.ID] = true
				changed = true
			}
		}
	}
	n := 0
	for _, seg := range segments {
		if seg.ID == segmentID || !dependents[seg.ID] {
			continue
		}
		if !seg.Stale {
			if err := s.MarkSegmentStale(ctx, seg.ID); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}

// SetSegmentDependsOn records a context dependency between segments.
func (s *Store) SetSegmentDependsOn(ctx context.Context, segmentID, predecessorID string) error {
	_, err := s.ExecContext(ctx, `UPDATE video_segments SET depends_on_segment_id=?, revision=revision+1, updated_at=? WHERE id=?`,
		nullIfEmpty(predecessorID), db.Now(), segmentID)
	return err
}

// AllSegmentsReady reports whether every non-stale segment of a request has
// succeeded (stale segments block assembly).
func (s *Store) AllSegmentsReady(ctx context.Context, requestID string) (bool, error) {
	var total, done int
	if err := s.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(status='succeeded' AND stale=0),0) FROM video_segments WHERE request_id=?`, requestID).Scan(&total, &done); err != nil {
		return false, err
	}
	return total > 0 && total == done, nil
}

// UpsertAssembly creates (or returns) the pending assembly for a request.
func (s *Store) UpsertAssembly(ctx context.Context, requestID, superShotID, workspaceID, projectID, videoID, shotID string, plannedMS int) (*VideoAssembly, error) {
	if a, err := s.GetAssembly(ctx, requestID); err == nil {
		return a, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	now := db.Now()
	a := &VideoAssembly{
		ID: ids.New(), RequestID: requestID, WorkspaceID: workspaceID, ProjectID: projectID,
		VideoID: videoID, ShotID: shotID, Status: "pending", SegmentAssetJSON: "[]",
		PlannedDurationMS: plannedMS, JoinsJSON: "[]", MeasuredState: "unmeasured", CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	if superShotID != "" {
		a.SuperShotRequestID = sql.NullString{String: superShotID, Valid: true}
	}
	if _, err := s.ExecContext(ctx, `INSERT INTO video_assemblies (id, request_id, super_shot_request_id, workspace_id, project_id, video_id, shot_id, status, asset_id, segment_asset_ids_json, planned_duration_ms, actual_duration_ms, joins_json, ffmpeg_receipt, input_revision, input_hash, measured_state, error, created_at, updated_at, revision, queued_input_hash)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.RequestID, nullSQL(a.SuperShotRequestID), a.WorkspaceID, a.ProjectID, a.VideoID, a.ShotID, a.Status, nil, a.SegmentAssetJSON, a.PlannedDurationMS, 0, a.JoinsJSON, "", 0, "", a.MeasuredState, "", now, now, 1, ""); err != nil {
		return nil, fmt.Errorf("insert video assembly: %w", err)
	}
	return a, nil
}

// GetAssembly returns the request's assembly, or ErrNotFound.
func (s *Store) GetAssembly(ctx context.Context, requestID string) (*VideoAssembly, error) {
	row := s.QueryRowContext(ctx, `SELECT id, request_id, super_shot_request_id, workspace_id, project_id, video_id, shot_id, status, asset_id, segment_asset_ids_json, planned_duration_ms, actual_duration_ms, joins_json, ffmpeg_receipt, input_revision, input_hash, measured_state, error, created_at, updated_at, revision, queued_input_hash
		FROM video_assemblies WHERE request_id=?`, requestID)
	return scanAssembly(row)
}

// ListAssembliesForVideo returns a video's assemblies, newest first.
func (s *Store) ListAssembliesForVideo(ctx context.Context, videoID string) ([]VideoAssembly, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, request_id, super_shot_request_id, workspace_id, project_id, video_id, shot_id, status, asset_id, segment_asset_ids_json, planned_duration_ms, actual_duration_ms, joins_json, ffmpeg_receipt, input_revision, input_hash, measured_state, error, created_at, updated_at, revision, queued_input_hash
		FROM video_assemblies WHERE video_id=? ORDER BY created_at DESC`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VideoAssembly
	for rows.Next() {
		a, err := scanAssembly(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// InvalidateAssembly marks an existing assembly as needing a rebuild (spec §7.3,
// §11.11). After a segment or anchor changes, call this before re-running
// generation so TryAssemble does not incorrectly early-return on a stale success.
// The frozen queue-time input snapshot is dropped together with the status, so a
// running export cannot publish after the inputs changed (R2).
func (s *Store) InvalidateAssembly(ctx context.Context, requestID string) error {
	_, err := s.ExecContext(ctx, `UPDATE video_assemblies SET status='pending', queued_input_hash='', revision=revision+1, updated_at=? WHERE request_id=?`, db.Now(), requestID)
	return err
}

// SetAssemblyRunning marks the assembly running and freezes the ordered input
// snapshot consumed by the export (R2). Publication is only possible while this
// frozen hash is still intact.
func (s *Store) SetAssemblyRunning(ctx context.Context, id, inputHash string) error {
	_, err := s.ExecContext(ctx, `UPDATE video_assemblies SET status='running', queued_input_hash=?, error='', revision=revision+1, updated_at=? WHERE id=?`, inputHash, db.Now(), id)
	return err
}

// SetAssemblySucceeded records the assembled asset, ordered segment lineage and
// the duration/joins/input-snapshot receipts (spec §7.4, §15.7, contract C08).
// Publication is conditional on the still-running status and the intact frozen
// input snapshot: an edit during the export invalidates both atomically (R2).
func (s *Store) SetAssemblySucceeded(ctx context.Context, id, assetID string, segmentAssetIDs []string, actualDurationMS, inputRevision int, inputHash, joinsJSON, ffmpegReceipt, measuredState string) (int64, error) {
	raw, _ := json.Marshal(segmentAssetIDs)
	if measuredState == "" {
		measuredState = "measured"
	}
	res, err := s.ExecContext(ctx, `UPDATE video_assemblies SET status='succeeded', asset_id=?, segment_asset_ids_json=?, actual_duration_ms=?, input_revision=?, input_hash=?, measured_state=?, joins_json=?, ffmpeg_receipt=?, error='', queued_input_hash='', revision=revision+1, updated_at=? WHERE id=? AND status='running' AND queued_input_hash != ''`,
		assetID, string(raw), actualDurationMS, inputRevision, inputHash, measuredState, defaultJSONArray(joinsJSON), ffmpegReceipt, db.Now(), id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SetAssemblyFailed records an assembly failure.
func (s *Store) SetAssemblyFailed(ctx context.Context, id, errMsg string) error {
	_, err := s.ExecContext(ctx, `UPDATE video_assemblies SET status='failed', error=?, revision=revision+1, updated_at=? WHERE id=?`,
		errMsg, db.Now(), id)
	return err
}

// EnqueueJob queues a job of any supported type for an aggregate. When uniqueKey
// is non-empty the enqueue is idempotent: the key must identify immutable work
// (attempt, anchor revision or assembly revision, contract C05), so duplicate
// submission of the same work returns its existing job. Races on the unique
// index fall back to the winning insert.
func (s *Store) EnqueueJob(ctx context.Context, workspaceID, jobType, aggregateType, aggregateID, uniqueKey string) (*Job, error) {
	return s.enqueueJob(ctx, workspaceID, jobType, aggregateType, aggregateID, "", uniqueKey)
}

// EnqueueAttemptJob is EnqueueJob with an explicit immutable attempt link, so a
// job row records exactly which attempt it executes (contract C04/C05).
func (s *Store) EnqueueAttemptJob(ctx context.Context, workspaceID, jobType, aggregateType, aggregateID, attemptID, uniqueKey string) (*Job, error) {
	return s.enqueueJob(ctx, workspaceID, jobType, aggregateType, aggregateID, attemptID, uniqueKey)
}

func (s *Store) enqueueJob(ctx context.Context, workspaceID, jobType, aggregateType, aggregateID, attemptID, uniqueKey string) (*Job, error) {
	now := db.Now()
	if uniqueKey != "" {
		var existingID string
		err := s.QueryRowContext(ctx, `SELECT id FROM jobs WHERE unique_key=?`, uniqueKey).Scan(&existingID)
		if err == nil {
			return s.GetJob(ctx, existingID)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	j := &Job{ID: ids.New(), WorkspaceID: workspaceID, JobType: jobType, AggregateType: aggregateType, AggregateID: aggregateID, State: "queued", CreatedAt: now}
	_, err := s.ExecContext(ctx, `INSERT INTO jobs (id, workspace_id, job_type, aggregate_type, aggregate_id, state, priority, request_json, unique_key, attempt_id, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`, j.ID, workspaceID, jobType, aggregateType, aggregateID, "queued", 0, "{}", nullIfEmpty(uniqueKey), nullIfEmpty(attemptID), now)
	if err != nil {
		// Lost an insert race on the unique key: return the winning job.
		if uniqueKey != "" {
			var existingID string
			if qerr := s.QueryRowContext(ctx, `SELECT id FROM jobs WHERE unique_key=?`, uniqueKey).Scan(&existingID); qerr == nil {
				return s.GetJob(ctx, existingID)
			}
		}
		return nil, err
	}
	return j, nil
}

func scanSegments(rows *sql.Rows) ([]VideoSegment, error) {
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

func scanSegment(sc scanner) (*VideoSegment, error) {
	var x VideoSegment
	var stale int
	err := sc.Scan(&x.ID, &x.RequestID, &x.SuperShotRequestID, &x.WorkspaceID, &x.ProjectID, &x.VideoID, &x.ShotID, &x.Index,
		&x.StartAnchorID, &x.EndAnchorID, &x.Positive, &x.Negative, &x.ActionPrompt, &x.RawAction, &x.CompiledPrompt, &x.ContinuityJSON,
		&x.StartAssetID, &x.EndAssetID, &x.StartOffsetMS, &x.EndOffsetMS, &x.DurationMS, &x.FrameCount,
		&x.FPSNum, &x.FPSDen, &x.ContextMode, &x.ContextInputID, &x.DependsOnSegmentID, &stale,
		&x.Status, &x.AttemptID, &x.SelectedAttemptID, &x.AssetID, &x.Error, &x.CreatedAt, &x.UpdatedAt, &x.Revision,
		&x.QueuedInputHash, &x.InputRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	x.Stale = stale != 0
	return &x, nil
}

func scanAssembly(sc scanner) (*VideoAssembly, error) {
	var a VideoAssembly
	err := sc.Scan(&a.ID, &a.RequestID, &a.SuperShotRequestID, &a.WorkspaceID, &a.ProjectID, &a.VideoID, &a.ShotID,
		&a.Status, &a.AssetID, &a.SegmentAssetJSON, &a.PlannedDurationMS, &a.ActualDurationMS, &a.JoinsJSON, &a.FFmpegReceipt,
		&a.InputRevision, &a.InputHash, &a.MeasuredState, &a.Error, &a.CreatedAt, &a.UpdatedAt, &a.Revision, &a.QueuedInputHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func nullSQL(v sql.NullString) any {
	if v.Valid {
		return v.String
	}
	return nil
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func defaultString(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func defaultJSONArray(v string) string {
	if v == "" {
		return "[]"
	}
	return v
}

func defaultJSONObject(v string) string {
	if v == "" {
		return "{}"
	}
	return v
}
