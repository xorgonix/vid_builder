package video

import (
	"context"
	"fmt"
)

// RefreshPlanStatus aggregates child state into the parent plan status. It never
// overrides an explicit cancellation and does nothing before approval
// (contract C10).
func (s *Service) RefreshPlanStatus(ctx context.Context, superShotID string) error {
	plan, err := s.Store.GetSuperShotRequest(ctx, superShotID)
	if err != nil {
		return err
	}
	switch plan.Status {
	case "cancelled", "draft", "awaiting_approval":
		return nil
	}
	anchors, err := s.Store.ListAnchors(ctx, superShotID)
	if err != nil {
		return err
	}
	segments, err := s.Store.ListSegmentsForSuperShot(ctx, superShotID)
	if err != nil {
		return err
	}
	missingAnchors := 0
	for _, a := range anchors {
		if a.Kind == AnchorIntermediate && !a.AssetID.Valid {
			missingAnchors++
		}
	}
	status := plan.Status
	if missingAnchors > 0 {
		status = "generating_anchors"
	} else {
		pending, stale, failed := 0, false, false
		for _, seg := range segments {
			switch seg.Status {
			case "succeeded":
				if seg.Stale {
					stale = true
				}
			case "failed":
				failed = true
			default:
				pending++
			}
		}
		switch {
		case pending > 0 || stale:
			status = "generating_segments"
		case failed:
			status = "failed"
		default:
			status = "assembling"
			if plan.GenerationRequestID.Valid {
				if asm, err := s.Store.GetAssembly(ctx, plan.GenerationRequestID.String); err == nil && asm.Status == "succeeded" {
					status = "succeeded"
				}
			}
		}
	}
	if status != plan.Status {
		return s.Store.SetSuperShotStatus(ctx, superShotID, status)
	}
	return nil
}

// Resume queues ready work in dependency order without changing the plan
// revision (contract §15.9).
func (s *Service) Resume(ctx context.Context, superShotID string) error {
	plan, err := s.Store.GetSuperShotRequest(ctx, superShotID)
	if err != nil {
		return err
	}
	if plan.Status == "cancelled" {
		return fmt.Errorf("plan is cancelled")
	}
	anchors, err := s.Store.ListAnchors(ctx, superShotID)
	if err != nil {
		return err
	}
	for _, a := range anchors {
		if a.Kind != AnchorIntermediate || a.AssetID.Valid {
			continue
		}
		key := "supershot:anchor:" + a.ID + ":gen:" + itoa(a.Revision)
		if _, err := s.Store.EnqueueJob(ctx, plan.WorkspaceID, "generate_anchor", "video_anchor", a.ID, key); err != nil {
			return err
		}
		_ = s.Store.SetAnchorStatus(ctx, a.ID, "queued", "")
	}
	return s.enqueueReadySegments(ctx, superShotID)
}

// SelectSegmentAttempt makes one attempt's output the current segment selection,
// invalidates the assembly and (in context mode) descendant segments
// (contract §15.9 select-attempt).
func (s *Service) SelectSegmentAttempt(ctx context.Context, superShotID, segmentID, attemptID string) error {
	plan, err := s.Store.GetSuperShotRequest(ctx, superShotID)
	if err != nil {
		return err
	}
	seg, err := s.Store.GetSegment(ctx, segmentID)
	if err != nil {
		return err
	}
	if !seg.SuperShotRequestID.Valid || seg.SuperShotRequestID.String != superShotID {
		return fmt.Errorf("segment does not belong to this plan")
	}
	assets, err := s.Store.ListAssetsForAttempt(ctx, attemptID)
	if err != nil {
		return err
	}
	assetID := ""
	for _, a := range assets {
		if a.Kind == "video" || a.Role == "motion" {
			assetID = a.ID
			break
		}
	}
	if assetID == "" && len(assets) > 0 {
		assetID = assets[0].ID
	}
	if assetID == "" {
		return fmt.Errorf("selected attempt has no recorded output")
	}
	if err := s.Store.SetSegmentSelectedOutput(ctx, seg.ID, attemptID, assetID); err != nil {
		return err
	}
	if plan.GenerationRequestID.Valid {
		_ = s.Store.InvalidateAssembly(ctx, plan.GenerationRequestID.String)
	}
	if seg.ContextMode != ContextAnchorOnly && plan.GenerationRequestID.Valid {
		_, _ = s.Store.MarkDownstreamStale(ctx, plan.GenerationRequestID.String, seg.ID)
	}
	return s.RefreshPlanStatus(ctx, superShotID)
}

// SetJoinReview records a creator's accept/reject/notes for one boundary. It
// makes no automated quality claim (contract C10).
func (s *Service) SetJoinReview(ctx context.Context, superShotID string, idx int, status, notes string) error {
	plan, err := s.Store.GetSuperShotRequest(ctx, superShotID)
	if err != nil {
		return err
	}
	if !plan.GenerationRequestID.Valid {
		return fmt.Errorf("plan has no assembly")
	}
	asm, err := s.Store.GetAssembly(ctx, plan.GenerationRequestID.String)
	if err != nil {
		return err
	}
	return s.Store.UpsertJoinReview(ctx, asm.ID, idx, status, notes)
}

// SelectAssembledVideo makes the assembled video the shot's motion selection so
// it enters the ordinary Review/Edit flow (contract C10).
func (s *Service) SelectAssembledVideo(ctx context.Context, superShotID, userID string) (string, error) {
	plan, err := s.Store.GetSuperShotRequest(ctx, superShotID)
	if err != nil {
		return "", err
	}
	if !plan.GenerationRequestID.Valid {
		return "", fmt.Errorf("plan has no assembly")
	}
	asm, err := s.Store.GetAssembly(ctx, plan.GenerationRequestID.String)
	if err != nil {
		return "", err
	}
	if asm.Status != "succeeded" || !asm.AssetID.Valid {
		return "", fmt.Errorf("assembly is not a current success")
	}
	shot, err := s.Store.GetShot(ctx, plan.ShotID)
	if err != nil {
		return "", err
	}
	if err := s.Store.CreateSelection(ctx, plan.ShotID, "motion", asm.AssetID.String, userID, shot.Revision); err != nil {
		return "", err
	}
	return asm.AssetID.String, nil
}

// itoa avoids importing strconv in this file.
func itoa(v int) string {
	return fmt.Sprintf("%d", v)
}