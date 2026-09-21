// Package video compiles a requested duration into fixed-length anchor-to-anchor
// segments, generates each independently, and assembles the ordered results into
// one playable video (v0.02 feature: prompt + 5/15/30s → one video asset).
package video

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"vidbuild/internal/store"
)

// SegmentSeconds is the fixed length of one generated segment.
const SegmentSeconds = 5

// AllowedDurations are the only supported total durations.
var AllowedDurations = []int{5, 15, 30}

// Plan returns how many fixed five-second segments a duration compiles into.
// Arbitrary durations are deliberately not supported.
func Plan(durationSeconds int) (int, error) {
	for _, d := range AllowedDurations {
		if durationSeconds == d {
			return d / SegmentSeconds, nil
		}
	}
	return 0, fmt.Errorf("video: duration must be one of 5, 15 or 30 seconds")
}

// Frames returns the frame count for one segment at the given fps, rounded to a
// multiple of four (Wan's length step) and never below four.
func Frames(fps float64) int {
	if fps <= 0 {
		fps = 8
	}
	n := int(math.Round(fps * SegmentSeconds))
	n = int(math.Round(float64(n)/4)) * 4
	if n < 4 {
		n = 4
	}
	return n
}

// SegmentPrompt makes each segment's motion prompt distinct so adjacent segments
// are not identical. One segment uses the base prompt unchanged.
func SegmentPrompt(base string, index, total int) string {
	base = strings.TrimSpace(base)
	if total <= 1 {
		return base
	}
	phases := []string{"opening", "development", "turning point", "escalation", "climax", "resolution"}
	phase := phases[index*len(phases)/total]
	if base == "" {
		return fmt.Sprintf("segment %d of %d (%s)", index+1, total, phase)
	}
	return fmt.Sprintf("%s — segment %d of %d (%s)", base, index+1, total, phase)
}

// Generator produces one segment asset.
type Generator interface {
	GenerateSegment(ctx context.Context, seg store.VideoSegment) (assetID string, err error)
}

// Assembler concatenates ordered segment assets into one video asset.
type Assembler interface {
	Assemble(ctx context.Context, asm store.VideoAssembly, segments []store.VideoSegment) (assetID string, err error)
}

// Service orchestrates duration-video planning, per-segment generation and
// final assembly. It is driven by the worker but is fully testable with fakes.
type Service struct {
	Store    *store.Store
	Generate Generator
	Assemble Assembler
	Log      *slog.Logger
}

// PlanInput is one duration-video request.
type PlanInput struct {
	WorkspaceID       string
	ProjectID         string
	VideoID           string
	ShotID            string
	ShotSpecID        string
	SpecVersion       int
	DurationSeconds   int
	Positive          string
	Negative          string
	FPS               float64
	Width             int
	Height            int
	WorkflowVersionID string
	ModelProfileID    string
	StartAssetID      string
	EndAssetID        string
}

// Plan creates the generation request and its segments, then queues one
// generation job per segment.
func (s *Service) Plan(ctx context.Context, in PlanInput, userID string) (string, []store.VideoSegment, error) {
	count, err := Plan(in.DurationSeconds)
	if err != nil {
		return "", nil, err
	}
	if in.StartAssetID == "" || in.EndAssetID == "" {
		return "", nil, fmt.Errorf("video: an approved start and end anchor are required")
	}
	fps := in.FPS
	if fps <= 0 {
		fps = 8
	}
	frames := Frames(fps)

	requestJSON, _ := json.Marshal(map[string]any{
		"duration_seconds": in.DurationSeconds,
		"segment_seconds":  SegmentSeconds,
		"segments":         count,
		"fps":              fps,
		"frame_count":      frames,
		"positive":         in.Positive,
		"negative":         in.Negative,
		"width":            in.Width,
		"height":           in.Height,
		"start_asset_id":   in.StartAssetID,
		"end_asset_id":     in.EndAssetID,
	})
	req, attempts, _, err := s.Store.CreateGenerationRequest(ctx, store.RequestInput{
		WorkspaceID: in.WorkspaceID, ProjectID: in.ProjectID, VideoID: in.VideoID, ShotID: in.ShotID,
		ShotSpecID: in.ShotSpecID, SpecVersion: in.SpecVersion, OutputRole: "motion", QualityTier: "motion",
		CandidateCount: count, WorkflowVersionID: in.WorkflowVersionID, ModelProfileID: in.ModelProfileID,
		RequestJSON: string(requestJSON), Enqueue: false,
	}, userID)
	if err != nil {
		return "", nil, err
	}

	inputs := make([]store.SegmentInput, 0, count)
	for i := 0; i < count; i++ {
		si := store.SegmentInput{
			Positive: SegmentPrompt(in.Positive, i, count), Negative: in.Negative,
			StartAssetID: in.StartAssetID, EndAssetID: in.EndAssetID,
			FrameCount: frames, FPS: fps,
		}
		if i < len(attempts) {
			si.AttemptID = attempts[i].ID
		}
		inputs = append(inputs, si)
	}
	segments, err := s.Store.InsertSegments(ctx, req.ID, in.WorkspaceID, in.ProjectID, in.VideoID, in.ShotID, inputs)
	if err != nil {
		return "", nil, err
	}
	for _, seg := range segments {
		if _, err := s.Store.EnqueueJob(ctx, in.WorkspaceID, "generate_segment", "video_segment", seg.ID); err != nil {
			return "", nil, err
		}
	}
	return req.ID, segments, nil
}

// RunSegment generates one segment and, when the whole request is complete,
// queues assembly.
func (s *Service) RunSegment(ctx context.Context, segmentID string) error {
	seg, err := s.Store.GetSegment(ctx, segmentID)
	if err != nil {
		return err
	}
	if err := s.Store.SetSegmentRunning(ctx, seg.ID, ""); err != nil {
		return err
	}
	assetID, err := s.Generate.GenerateSegment(ctx, *seg)
	if err != nil {
		_ = s.Store.SetSegmentFailed(ctx, seg.ID, err.Error())
		return err
	}
	if err := s.Store.SetSegmentSucceeded(ctx, seg.ID, assetID); err != nil {
		return err
	}
	_, err = s.TryAssemble(ctx, seg.RequestID)
	return err
}

// RetrySegment resets one failed segment and queues only that segment again.
func (s *Service) RetrySegment(ctx context.Context, segmentID string) error {
	seg, err := s.Store.GetSegment(ctx, segmentID)
	if err != nil {
		return err
	}
	if err := s.Store.ResetSegmentForRetry(ctx, seg.ID); err != nil {
		return err
	}
	_, err = s.Store.EnqueueJob(ctx, seg.WorkspaceID, "generate_segment", "video_segment", seg.ID)
	return err
}

// TryAssemble queues assembly once every segment has succeeded. It is idempotent.
func (s *Service) TryAssemble(ctx context.Context, requestID string) (bool, error) {
	ok, err := s.Store.AllSegmentsSucceeded(ctx, requestID)
	if err != nil || !ok {
		return false, err
	}
	segs, err := s.Store.ListSegments(ctx, requestID)
	if err != nil || len(segs) == 0 {
		return false, err
	}
	first := segs[0]
	asm, err := s.Store.UpsertAssembly(ctx, requestID, first.WorkspaceID, first.ProjectID, first.VideoID, first.ShotID)
	if err != nil {
		return false, err
	}
	if asm.Status == "succeeded" {
		return true, nil
	}
	if _, err := s.Store.EnqueueJob(ctx, first.WorkspaceID, "assemble_video", "video_assembly", requestID); err != nil {
		return false, err
	}
	return true, nil
}

// RunAssembly concatenates a request's succeeded segments in order and records
// the ordered segment lineage on the assembly.
func (s *Service) RunAssembly(ctx context.Context, requestID string) error {
	asm, err := s.Store.GetAssembly(ctx, requestID)
	if err != nil {
		return err
	}
	segs, err := s.Store.ListSegments(ctx, requestID)
	if err != nil {
		return err
	}
	succeeded := make([]store.VideoSegment, 0, len(segs))
	assetIDs := make([]string, 0, len(segs))
	for _, seg := range segs {
		if seg.Status != "succeeded" || !seg.AssetID.Valid {
			return fmt.Errorf("video: cannot assemble; segment %d is not succeeded", seg.Index)
		}
		succeeded = append(succeeded, seg)
		assetIDs = append(assetIDs, seg.AssetID.String)
	}
	if err := s.Store.SetAssemblyRunning(ctx, asm.ID); err != nil {
		return err
	}
	assetID, err := s.Assemble.Assemble(ctx, *asm, succeeded)
	if err != nil {
		_ = s.Store.SetAssemblyFailed(ctx, asm.ID, err.Error())
		return err
	}
	return s.Store.SetAssemblySucceeded(ctx, asm.ID, assetID, assetIDs)
}
