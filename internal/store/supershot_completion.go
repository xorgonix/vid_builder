package store

import (
	"context"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

// JoinReview is a creator's review of one assembled boundary (contract C10).
type JoinReview struct {
	ID         string
	AssemblyID string
	Index      int
	Status     string
	Notes      string
	CreatedAt  string
	UpdatedAt  string
}

// ListAssetsForAttempt returns the assets recorded against one generation attempt.
func (s *Store) ListAssetsForAttempt(ctx context.Context, attemptID string) ([]Asset, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, workspace_id, project_id, generation_attempt_id, kind, role, storage_uri, mime_type, size_bytes, checksum_sha256, width, height, duration_ms, created_at FROM assets WHERE generation_attempt_id=? ORDER BY created_at DESC`, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Asset
	for rows.Next() {
		var a Asset
		if err := rows.Scan(&a.ID, &a.WorkspaceID, &a.ProjectID, &a.GenerationAttemptID, &a.Kind, &a.Role, &a.StorageURI, &a.MimeType, &a.SizeBytes, &a.Checksum, &a.Width, &a.Height, &a.DurationMs, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetSegmentSelectedOutput makes an attempt's output the current selection for a
// segment (contract §15.9 select-attempt).
func (s *Store) SetSegmentSelectedOutput(ctx context.Context, segmentID, attemptID, assetID string) error {
	_, err := s.ExecContext(ctx, `UPDATE video_segments SET attempt_id=?, selected_attempt_id=?, asset_id=?, status='succeeded', stale=0, error='', revision=revision+1, updated_at=? WHERE id=?`,
		attemptID, attemptID, assetID, db.Now(), segmentID)
	return err
}

// UpsertJoinReview records the creator's decision for one boundary.
func (s *Store) UpsertJoinReview(ctx context.Context, assemblyID string, idx int, status, notes string) error {
	now := db.Now()
	_, err := s.ExecContext(ctx, `INSERT INTO assembly_join_reviews (id, assembly_id, idx, status, notes, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(assembly_id, idx) DO UPDATE SET status=excluded.status, notes=excluded.notes, updated_at=excluded.updated_at`,
		ids.New(), assemblyID, idx, defaultString(status, "unreviewed"), notes, now, now)
	return err
}

// ListJoinReviews returns an assembly's boundary reviews.
func (s *Store) ListJoinReviews(ctx context.Context, assemblyID string) ([]JoinReview, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, assembly_id, idx, status, notes, created_at, updated_at FROM assembly_join_reviews WHERE assembly_id=? ORDER BY idx`, assemblyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JoinReview
	for rows.Next() {
		var j JoinReview
		if err := rows.Scan(&j.ID, &j.AssemblyID, &j.Index, &j.Status, &j.Notes, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ListLegacyInvalidPlans finds plans whose every segment reuses the same outer
// endpoint pair, marks them, and returns them for re-planning (contract C10/§9.1).
// It never deletes their outputs.
func (s *Store) ListLegacyInvalidPlans(ctx context.Context, videoID string) ([]SuperShotRequest, error) {
	plans, err := s.ListSuperShotRequestsForVideo(ctx, videoID)
	if err != nil {
		return nil, err
	}
	var out []SuperShotRequest
	for i := range plans {
		p := plans[i]
		if p.LegacyInvalidPlan {
			out = append(out, p)
			continue
		}
		segs, err := s.ListSegmentsForSuperShot(ctx, p.ID)
		if err != nil || len(segs) < 2 {
			continue
		}
		first, last := "", ""
		if segs[0].StartAssetID.Valid {
			first = segs[0].StartAssetID.String
		}
		if segs[0].EndAssetID.Valid {
			last = segs[0].EndAssetID.String
		}
		if first == "" || last == "" {
			continue
		}
		allSame := true
		for _, sg := range segs {
			if !sg.StartAssetID.Valid || sg.StartAssetID.String != first || !sg.EndAssetID.Valid || sg.EndAssetID.String != last {
				allSame = false
				break
			}
		}
		if allSame {
			_ = s.MarkLegacyInvalidPlan(ctx, p.ID)
			p.LegacyInvalidPlan = true
			out = append(out, p)
		}
	}
	return out, nil
}

// CountLegacyGenerationRequests returns how many pre-Super-Shot duration-video
// generation requests still exist for a video: motion generations whose segments
// were never adopted by a Super Shot plan (video_segments.super_shot_request_id
// IS NULL). Old rows predate the Super Shot workflow and stay visible and
// diagnosable (contract R6/§9.1); iterating super_shot_requests alone cannot
// discover them.
func (s *Store) CountLegacyGenerationRequests(ctx context.Context, videoID string) (int, error) {
	var n int
	err := s.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
		SELECT DISTINCT g.id FROM generation_requests g
		JOIN video_segments vs ON vs.request_id = g.id
		WHERE g.video_id=? AND g.output_role='motion' AND (vs.super_shot_request_id IS NULL OR vs.super_shot_request_id='')
	)`, videoID).Scan(&n)
	return n, err
}