package store

import (
	"context"
	"database/sql"
	"errors"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

// Campaign is a planned batch of compatible work for one target.
type Campaign struct {
	ID                string
	WorkspaceID       string
	ProjectID         string
	VideoID           sql.NullString
	Name              string
	ExecutionTargetID sql.NullString
	Status            string
	EstimatedMs       int64
	EstimatedMicros   int64
	ActualMs          int64
	ActualMicros      int64
	Currency          string
	Revision          int
	CreatedAt         string
}

// CampaignItem links a request to a campaign.
type CampaignItem struct {
	ID                  string
	CampaignID          string
	GenerationRequestID string
	Position            int
	Status              string
}

// CreateCampaign inserts a draft campaign.
func (s *Store) CreateCampaign(ctx context.Context, workspaceID, projectID, videoID, name, targetID, userID string) (*Campaign, error) {
	now := db.Now()
	c := &Campaign{ID: ids.New(), WorkspaceID: workspaceID, ProjectID: projectID, Name: name, Status: "draft", Currency: "USD", Revision: 1, CreatedAt: now}
	if videoID != "" {
		c.VideoID = sql.NullString{String: videoID, Valid: true}
	}
	if targetID != "" {
		c.ExecutionTargetID = sql.NullString{String: targetID, Valid: true}
	}
	var vid, tid any
	if videoID != "" {
		vid = videoID
	}
	if targetID != "" {
		tid = targetID
	}
	_, err := s.ExecContext(ctx, `INSERT INTO render_campaigns (id, workspace_id, project_id, video_id, name, execution_target_id, status, currency, created_by_user_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?, 'draft','USD',?,?,?)`, c.ID, workspaceID, projectID, vid, name, tid, nullStr(userID), now, now)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// GetCampaign fetches a campaign scoped to a workspace.
func (s *Store) GetCampaign(ctx context.Context, workspaceID, id string) (*Campaign, error) {
	row := s.QueryRowContext(ctx, `SELECT id, workspace_id, project_id, video_id, name, execution_target_id, status, estimated_duration_ms, estimated_cost_micros, actual_duration_ms, actual_cost_micros, currency, revision, created_at
		FROM render_campaigns WHERE id=? AND workspace_id=?`, id, workspaceID)
	var c Campaign
	err := row.Scan(&c.ID, &c.WorkspaceID, &c.ProjectID, &c.VideoID, &c.Name, &c.ExecutionTargetID, &c.Status, &c.EstimatedMs, &c.EstimatedMicros, &c.ActualMs, &c.ActualMicros, &c.Currency, &c.Revision, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

// ListCampaigns returns campaigns for a video.
func (s *Store) ListCampaigns(ctx context.Context, workspaceID, videoID string) ([]Campaign, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, workspace_id, project_id, video_id, name, execution_target_id, status, estimated_duration_ms, estimated_cost_micros, actual_duration_ms, actual_cost_micros, currency, revision, created_at
		FROM render_campaigns WHERE workspace_id=? AND video_id=? ORDER BY created_at DESC`, workspaceID, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Campaign
	for rows.Next() {
		var c Campaign
		if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.ProjectID, &c.VideoID, &c.Name, &c.ExecutionTargetID, &c.Status, &c.EstimatedMs, &c.EstimatedMicros, &c.ActualMs, &c.ActualMicros, &c.Currency, &c.Revision, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AddCampaignItem appends a request to a campaign.
func (s *Store) AddCampaignItem(ctx context.Context, campaignID, requestID string) error {
	var pos int
	if err := s.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),-1)+1 FROM campaign_items WHERE campaign_id=?`, campaignID).Scan(&pos); err != nil {
		return err
	}
	now := db.Now()
	_, err := s.ExecContext(ctx, `INSERT INTO campaign_items (id, campaign_id, generation_request_id, position, priority, status, created_at, updated_at)
		VALUES (?,?,?,?,0,'pending',?,?)`, ids.New(), campaignID, requestID, pos, now, now)
	return err
}

// ListCampaignItems returns campaign items with their request ids.
func (s *Store) ListCampaignItems(ctx context.Context, campaignID string) ([]CampaignItem, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, campaign_id, generation_request_id, position, status FROM campaign_items WHERE campaign_id=? ORDER BY position`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CampaignItem
	for rows.Next() {
		var it CampaignItem
		if err := rows.Scan(&it.ID, &it.CampaignID, &it.GenerationRequestID, &it.Position, &it.Status); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// SetCampaignStatus updates campaign status and estimates.
func (s *Store) SetCampaignStatus(ctx context.Context, id, status string, estimatedMs, estimatedMicros int64) error {
	_, err := s.ExecContext(ctx, `UPDATE render_campaigns SET status=?, estimated_duration_ms=?, estimated_cost_micros=?, updated_at=? WHERE id=?`,
		status, estimatedMs, estimatedMicros, db.Now(), id)
	return err
}

// ReconcileCampaign records actual provider usage.
func (s *Store) ReconcileCampaign(ctx context.Context, id string, actualMs, actualMicros int64) error {
	_, err := s.ExecContext(ctx, `UPDATE render_campaigns SET actual_duration_ms=?, actual_cost_micros=?, status='completed', updated_at=? WHERE id=?`,
		actualMs, actualMicros, db.Now(), id)
	return err
}

// AvgObservationMs returns the mean elapsed ms and sample count for a target.
func (s *Store) AvgObservationMs(ctx context.Context, targetID string) (int64, int) {
	var avg sql.NullInt64
	var count int
	_ = s.QueryRowContext(ctx, `SELECT AVG(elapsed_ms), COUNT(*) FROM execution_observations WHERE execution_target_id=? AND success=1`, targetID).Scan(&avg, &count)
	if !avg.Valid {
		return 0, 0
	}
	return avg.Int64, count
}

// CountReadyRequests returns requests eligible for a campaign (not yet running).
func (s *Store) CountReadyRequests(ctx context.Context, videoID string) (int, error) {
	var n int
	err := s.QueryRowContext(ctx, `SELECT COUNT(*) FROM generation_requests WHERE video_id=? AND status IN ('draft','queued')`, videoID).Scan(&n)
	return n, err
}

// ListDraftRequests returns requests that have no running/succeeded attempt yet.
func (s *Store) ListDraftRequests(ctx context.Context, videoID string) ([]GenerationRequest, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, project_id, video_id, shot_id, shot_spec_id, spec_version, output_role, quality_tier, candidate_count, workflow_version_id, model_profile_id, status, revision, request_json, prompt_compilation_json, created_at
		FROM generation_requests WHERE video_id=? AND status IN ('draft','queued') ORDER BY created_at`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GenerationRequest
	for rows.Next() {
		var r GenerationRequest
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.VideoID, &r.ShotID, &r.ShotSpecID, &r.SpecVersion, &r.OutputRole, &r.QualityTier, &r.CandidateCount, &r.WorkflowVersionID, &r.ModelProfileID, &r.Status, &r.Revision, &r.RequestJSON, &r.CompilationJSON, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
