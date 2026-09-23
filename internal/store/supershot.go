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

// SuperShotRequest is one planned Super Shot: a requested duration compiled into
// a direct generation or an ordered chain of adjacent segments (spec §5.2).
type SuperShotRequest struct {
	ID                    string
	GenerationRequestID   sql.NullString
	WorkspaceID           string
	ProjectID             string
	VideoID               string
	ShotID                string
	WorkflowVersionID     string
	ModelProfileID        sql.NullString
	Strategy              string
	RequestedDurationMS   int
	PlannedDurationMS     int
	FPSNum                int
	FPSDen                int
	Width                 int
	Height                int
	BasePositive          string
	BaseNegative          string
	ContextMode           string
	Status                string
	PlanVersion           int
	LegacyInvalidPlan     bool
	PlanSummary           string
	ContinuityJSON        string
	CreatedByUserID       string
	CreatedAt             string
	UpdatedAt             string
	Revision              int
}

// VideoAnchor is one ordered boundary state (spec §5.3).
type VideoAnchor struct {
	ID                   string
	SuperShotRequestID   string
	Index                int
	TimeOffsetMS         int
	Kind                 string
	StatePrompt          string
	NegativePrompt       string
	ContinuityJSON       string
	Status               string
	AssetID              sql.NullString
	SourceAnchorID       sql.NullString
	Error                string
	CreatedAt            string
	UpdatedAt            string
	Revision             int
}

// MotionContext is a validated motion-context artifact (spec §15.5).
type MotionContext struct {
	ID                     string
	WorkspaceID            string
	ProjectID              string
	SuperShotRequestID     string
	ProducerSegmentID      string
	ProducerAttemptID      sql.NullString
	SourceAssetID          string
	SourceChecksum         string
	Mode                   string
	ArtifactAssetID        sql.NullString
	ArtifactChecksum       string
	EncodingVersion        string
	ModelFingerprint       string
	VAEFingerprint         string
	AdapterFingerprint     string
	WorkflowVersionID      string
	WorkflowSHA            string
	ContextContractVersion string
	Width                  int
	Height                 int
	FPSNum                 int
	FPSDen                 int
	SourceStartFrame       int
	SourceEndFrame         int
	LayoutJSON             string
	ValidationStatus       string
	CreatedAt              string
}

// AnchorInput is one anchor to persist with a draft plan.
type AnchorInput struct {
	Index          int
	TimeOffsetMS   int
	Kind           string
	StatePrompt    string
	NegativePrompt string
	ContinuityJSON string
	AssetID        string
	SourceAnchorID string
	Status         string
}

// SuperShotRequestInput is the plan header.
type SuperShotRequestInput struct {
	WorkspaceID         string
	ProjectID           string
	VideoID             string
	ShotID              string
	ShotSpecID          string
	SpecVersion         int
	WorkflowVersionID   string
	ModelProfileID      string
	Strategy            string
	RequestedDurationMS int
	PlannedDurationMS   int
	FPSNum              int
	FPSDen              int
	Width               int
	Height              int
	BasePositive        string
	BaseNegative        string
	ContextMode         string
	PlanSummary         string
	ContinuityJSON      string
}

// SuperShotPlan is a persisted draft plan.
type SuperShotPlan struct {
	GenerationRequest *GenerationRequest
	SuperShot         *SuperShotRequest
	Anchors           []VideoAnchor
	Segments          []VideoSegment
}

const superShotColumns = `id, generation_request_id, workspace_id, project_id, video_id, shot_id, workflow_version_id,
	model_profile_id, strategy, requested_duration_ms, planned_duration_ms, fps_num, fps_den, width, height,
	base_positive, base_negative, context_mode, status, plan_version, legacy_invalid_plan, plan_summary,
	continuity_json, created_by_user_id, created_at, updated_at, revision`

// CreateSuperShotPlan persists a draft plan: a linked generation request (not
// enqueued), the Super Shot request, its anchors and its segments. Generation
// begins only on approval (spec §4.8).
func (s *Store) CreateSuperShotPlan(ctx context.Context, in SuperShotRequestInput, anchors []AnchorInput, segments []SegmentInput, userID string) (*SuperShotPlan, error) {
	if len(anchors) != len(segments)+1 {
		return nil, fmt.Errorf("super shot: %d anchors must equal %d segments + 1", len(anchors), len(segments))
	}
	reqJSON, _ := json.Marshal(map[string]any{
		"super_shot": true, "duration_ms": in.RequestedDurationMS, "planned_duration_ms": in.PlannedDurationMS,
		"strategy": in.Strategy, "segments": len(segments), "anchors": len(anchors),
		"fps_num": in.FPSNum, "fps_den": in.FPSDen, "context_mode": in.ContextMode,
	})
	req, _, _, err := s.CreateGenerationRequest(ctx, RequestInput{
		WorkspaceID: in.WorkspaceID, ProjectID: in.ProjectID, VideoID: in.VideoID, ShotID: in.ShotID,
		ShotSpecID: in.ShotSpecID, SpecVersion: in.SpecVersion, OutputRole: "motion", QualityTier: "motion",
		CandidateCount: 1, WorkflowVersionID: in.WorkflowVersionID, ModelProfileID: in.ModelProfileID,
		RequestJSON: string(reqJSON), Enqueue: false,
	}, userID)
	if err != nil {
		return nil, err
	}

	now := db.Now()
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	ss := &SuperShotRequest{
		ID: ids.New(), WorkspaceID: in.WorkspaceID, ProjectID: in.ProjectID, VideoID: in.VideoID, ShotID: in.ShotID,
		WorkflowVersionID: in.WorkflowVersionID, Strategy: in.Strategy,
		RequestedDurationMS: in.RequestedDurationMS, PlannedDurationMS: in.PlannedDurationMS,
		FPSNum: in.FPSNum, FPSDen: in.FPSDen, Width: in.Width, Height: in.Height,
		BasePositive: in.BasePositive, BaseNegative: in.BaseNegative,
		ContextMode: defaultString(in.ContextMode, "anchor_only"), Status: "awaiting_approval",
		PlanVersion: 1, PlanSummary: in.PlanSummary, ContinuityJSON: defaultJSONArray(in.ContinuityJSON),
		CreatedByUserID: userID, CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	ss.GenerationRequestID = sql.NullString{String: req.ID, Valid: true}
	if in.ModelProfileID != "" {
		ss.ModelProfileID = sql.NullString{String: in.ModelProfileID, Valid: true}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO super_shot_requests (`+superShotColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		ss.ID, req.ID, ss.WorkspaceID, ss.ProjectID, ss.VideoID, ss.ShotID, ss.WorkflowVersionID,
		nullSQL(ss.ModelProfileID), ss.Strategy, ss.RequestedDurationMS, ss.PlannedDurationMS, ss.FPSNum, ss.FPSDen,
		ss.Width, ss.Height, ss.BasePositive, ss.BaseNegative, ss.ContextMode, ss.Status, ss.PlanVersion,
		0, ss.PlanSummary, ss.ContinuityJSON, ss.CreatedByUserID, now, now, 1); err != nil {
		return nil, fmt.Errorf("insert super shot request: %w", err)
	}

	outAnchors, err := insertAnchorsTx(ctx, tx, ss.ID, anchors)
	if err != nil {
		return nil, err
	}
	// Resolve each segment's adjacent anchors by order: segment i is bounded by
	// anchors i and i+1 (spec §6.6).
	for i := range segments {
		if i+1 < len(outAnchors) {
			segments[i].StartAnchorID = outAnchors[i].ID
			segments[i].EndAnchorID = outAnchors[i+1].ID
		}
	}
	outSegments, err := insertSegmentsTx(ctx, tx, req.ID, ss.ID, in.WorkspaceID, in.ProjectID, in.VideoID, in.ShotID, segments)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &SuperShotPlan{GenerationRequest: req, SuperShot: ss, Anchors: outAnchors, Segments: outSegments}, nil
}

// GetSuperShotRequest loads one plan.
func (s *Store) GetSuperShotRequest(ctx context.Context, id string) (*SuperShotRequest, error) {
	row := s.QueryRowContext(ctx, `SELECT `+superShotColumns+` FROM super_shot_requests WHERE id=?`, id)
	return scanSuperShot(row)
}

// ListSuperShotRequestsForVideo lists a video's plans, newest first.
func (s *Store) ListSuperShotRequestsForVideo(ctx context.Context, videoID string) ([]SuperShotRequest, error) {
	rows, err := s.QueryContext(ctx, `SELECT `+superShotColumns+` FROM super_shot_requests WHERE video_id=? ORDER BY created_at DESC`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SuperShotRequest
	for rows.Next() {
		ss, err := scanSuperShot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ss)
	}
	return out, rows.Err()
}

// SetSuperShotStatus updates the plan lifecycle state.
func (s *Store) SetSuperShotStatus(ctx context.Context, id, status string) error {
	_, err := s.ExecContext(ctx, `UPDATE super_shot_requests SET status=?, revision=revision+1, updated_at=? WHERE id=?`, status, db.Now(), id)
	return err
}

// ApproveSuperShot freezes the plan version and marks it approved (spec §7).
func (s *Store) ApproveSuperShot(ctx context.Context, id string, revision int) error {
	res, err := s.ExecContext(ctx, `UPDATE super_shot_requests SET status='approved', plan_version=plan_version+1, revision=revision+1, updated_at=? WHERE id=? AND revision=? AND status IN ('draft','awaiting_approval')`,
		db.Now(), id, revision)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	return nil
}

// MarkLegacyInvalidPlan flags a plan as a legacy invalid chain (spec §9.1).
func (s *Store) MarkLegacyInvalidPlan(ctx context.Context, id string) error {
	_, err := s.ExecContext(ctx, `UPDATE super_shot_requests SET legacy_invalid_plan=1, revision=revision+1, updated_at=? WHERE id=?`, db.Now(), id)
	return err
}

// --- Anchors ---

const anchorColumns = `id, super_shot_request_id, idx, time_offset_ms, kind, state_prompt, negative_prompt,
	continuity_json, status, asset_id, source_anchor_id, error, created_at, updated_at, revision`

func insertAnchorsTx(ctx context.Context, tx *sql.Tx, superShotID string, anchors []AnchorInput) ([]VideoAnchor, error) {
	now := db.Now()
	out := make([]VideoAnchor, 0, len(anchors))
	for _, a := range anchors {
		status := defaultString(a.Status, "planned")
		an := VideoAnchor{
			ID: ids.New(), SuperShotRequestID: superShotID, Index: a.Index, TimeOffsetMS: a.TimeOffsetMS,
			Kind: a.Kind, StatePrompt: a.StatePrompt, NegativePrompt: a.NegativePrompt,
			ContinuityJSON: defaultJSONArray(a.ContinuityJSON), Status: status,
			CreatedAt: now, UpdatedAt: now, Revision: 1,
		}
		if a.AssetID != "" {
			an.AssetID = sql.NullString{String: a.AssetID, Valid: true}
		}
		if a.SourceAnchorID != "" {
			an.SourceAnchorID = sql.NullString{String: a.SourceAnchorID, Valid: true}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO video_anchors (`+anchorColumns+`)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			an.ID, an.SuperShotRequestID, an.Index, an.TimeOffsetMS, an.Kind, an.StatePrompt, an.NegativePrompt,
			an.ContinuityJSON, an.Status, nullSQL(an.AssetID), nullSQL(an.SourceAnchorID), "", now, now, 1); err != nil {
			return nil, fmt.Errorf("insert video anchor: %w", err)
		}
		out = append(out, an)
	}
	return out, nil
}

// ListAnchors returns a plan's anchors in index order.
func (s *Store) ListAnchors(ctx context.Context, superShotID string) ([]VideoAnchor, error) {
	rows, err := s.QueryContext(ctx, `SELECT `+anchorColumns+` FROM video_anchors WHERE super_shot_request_id=? ORDER BY idx`, superShotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VideoAnchor
	for rows.Next() {
		a, err := scanAnchor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// GetAnchor loads one anchor.
func (s *Store) GetAnchor(ctx context.Context, id string) (*VideoAnchor, error) {
	row := s.QueryRowContext(ctx, `SELECT `+anchorColumns+` FROM video_anchors WHERE id=?`, id)
	return scanAnchor(row)
}

// UpdateAnchorState edits an intermediate anchor's state and continuity (spec §7.1).
func (s *Store) UpdateAnchorState(ctx context.Context, id, statePrompt, negativePrompt, continuityJSON string, revision int) error {
	res, err := s.ExecContext(ctx, `UPDATE video_anchors SET state_prompt=?, negative_prompt=?, continuity_json=?, revision=revision+1, updated_at=? WHERE id=? AND revision=? AND kind='intermediate'`,
		statePrompt, negativePrompt, defaultJSONArray(continuityJSON), db.Now(), id, revision)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	return nil
}

// SetAnchorStatus updates an anchor's lifecycle status.
func (s *Store) SetAnchorStatus(ctx context.Context, id, status, errMsg string) error {
	_, err := s.ExecContext(ctx, `UPDATE video_anchors SET status=?, error=?, revision=revision+1, updated_at=? WHERE id=?`, status, errMsg, db.Now(), id)
	return err
}

// SetAnchorSucceeded records the generated intermediate image.
func (s *Store) SetAnchorSucceeded(ctx context.Context, id, assetID string) error {
	_, err := s.ExecContext(ctx, `UPDATE video_anchors SET status='succeeded', asset_id=?, error='', revision=revision+1, updated_at=? WHERE id=?`, assetID, db.Now(), id)
	return err
}

// ResetAnchorForRetry returns a failed intermediate anchor to planned.
func (s *Store) ResetAnchorForRetry(ctx context.Context, id string) error {
	res, err := s.ExecContext(ctx, `UPDATE video_anchors SET status='planned', error='', revision=revision+1, updated_at=? WHERE id=? AND kind='intermediate' AND status IN ('failed','planned')`, db.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("anchor is not retryable")
	}
	return nil
}

// --- Motion contexts ---

// InsertMotionContext records a validated motion-context artifact. Only a real
// adapter that exports/consumes the artifact writes these rows (spec §15.3).
func (s *Store) InsertMotionContext(ctx context.Context, mc MotionContext) (string, error) {
	if mc.ID == "" {
		mc.ID = ids.New()
	}
	mc.CreatedAt = db.Now()
	if mc.LayoutJSON == "" {
		mc.LayoutJSON = "{}"
	}
	if mc.FPSDen == 0 {
		mc.FPSDen = 1
	}
	if _, err := s.ExecContext(ctx, `INSERT INTO motion_contexts (id, workspace_id, project_id, super_shot_request_id, producer_segment_id, producer_attempt_id, source_asset_id, source_checksum, mode, artifact_asset_id, artifact_checksum, encoding_version, model_fingerprint, vae_fingerprint, adapter_fingerprint, workflow_version_id, workflow_sha, context_contract_version, width, height, fps_num, fps_den, source_start_frame, source_end_frame, layout_json, validation_status, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		mc.ID, mc.WorkspaceID, mc.ProjectID, mc.SuperShotRequestID, mc.ProducerSegmentID, nullSQL(mc.ProducerAttemptID),
		mc.SourceAssetID, mc.SourceChecksum, mc.Mode, nullSQL(mc.ArtifactAssetID), mc.ArtifactChecksum,
		mc.EncodingVersion, mc.ModelFingerprint, mc.VAEFingerprint, mc.AdapterFingerprint, mc.WorkflowVersionID,
		mc.WorkflowSHA, defaultString(mc.ContextContractVersion, "1"), mc.Width, mc.Height, mc.FPSNum, mc.FPSDen,
		mc.SourceStartFrame, mc.SourceEndFrame, mc.LayoutJSON, defaultString(mc.ValidationStatus, "unvalidated"), mc.CreatedAt); err != nil {
		return "", fmt.Errorf("insert motion context: %w", err)
	}
	return mc.ID, nil
}

// GetMotionContextForProducer returns the validated context a producer segment
// exposes, or ErrNotFound.
func (s *Store) GetMotionContextForProducer(ctx context.Context, producerSegmentID string) (*MotionContext, error) {
	row := s.QueryRowContext(ctx, `SELECT id, workspace_id, project_id, super_shot_request_id, producer_segment_id, producer_attempt_id, source_asset_id, source_checksum, mode, artifact_asset_id, artifact_checksum, encoding_version, model_fingerprint, vae_fingerprint, adapter_fingerprint, workflow_version_id, workflow_sha, context_contract_version, width, height, fps_num, fps_den, source_start_frame, source_end_frame, layout_json, validation_status, created_at
		FROM motion_contexts WHERE producer_segment_id=? ORDER BY created_at DESC LIMIT 1`, producerSegmentID)
	var mc MotionContext
	err := row.Scan(&mc.ID, &mc.WorkspaceID, &mc.ProjectID, &mc.SuperShotRequestID, &mc.ProducerSegmentID, &mc.ProducerAttemptID, &mc.SourceAssetID, &mc.SourceChecksum, &mc.Mode, &mc.ArtifactAssetID, &mc.ArtifactChecksum, &mc.EncodingVersion, &mc.ModelFingerprint, &mc.VAEFingerprint, &mc.AdapterFingerprint, &mc.WorkflowVersionID, &mc.WorkflowSHA, &mc.ContextContractVersion, &mc.Width, &mc.Height, &mc.FPSNum, &mc.FPSDen, &mc.SourceStartFrame, &mc.SourceEndFrame, &mc.LayoutJSON, &mc.ValidationStatus, &mc.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &mc, nil
}

func scanSuperShot(sc scanner) (*SuperShotRequest, error) {
	var x SuperShotRequest
	var legacy int
	err := sc.Scan(&x.ID, &x.GenerationRequestID, &x.WorkspaceID, &x.ProjectID, &x.VideoID, &x.ShotID,
		&x.WorkflowVersionID, &x.ModelProfileID, &x.Strategy, &x.RequestedDurationMS, &x.PlannedDurationMS,
		&x.FPSNum, &x.FPSDen, &x.Width, &x.Height, &x.BasePositive, &x.BaseNegative, &x.ContextMode,
		&x.Status, &x.PlanVersion, &legacy, &x.PlanSummary, &x.ContinuityJSON, &x.CreatedByUserID,
		&x.CreatedAt, &x.UpdatedAt, &x.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	x.LegacyInvalidPlan = legacy != 0
	return &x, nil
}

func scanAnchor(sc scanner) (*VideoAnchor, error) {
	var x VideoAnchor
	err := sc.Scan(&x.ID, &x.SuperShotRequestID, &x.Index, &x.TimeOffsetMS, &x.Kind, &x.StatePrompt,
		&x.NegativePrompt, &x.ContinuityJSON, &x.Status, &x.AssetID, &x.SourceAnchorID, &x.Error,
		&x.CreatedAt, &x.UpdatedAt, &x.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &x, nil
}
