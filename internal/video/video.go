// Package video implements Super Shot planning: a requested duration is compiled
// into a direct generation or an ordered chain of adjacent segments bounded by
// shared anchors. Timing rules come from the selected workflow's capabilities,
// never from application constants (spec §2.1, §5.5, §15.7).
package video

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"

	"vidbuild/internal/store"
	"vidbuild/internal/workflows"
)

// SegmentResult is the outcome of one segment generation, including the exact
// endpoint assets that were resolved from its anchors.
type SegmentResult struct {
	AssetID      string
	StartAssetID string
	EndAssetID   string
}

// Generator produces one segment asset by conditioning on its adjacent anchors.
type Generator interface {
	GenerateSegment(ctx context.Context, seg store.VideoSegment) (SegmentResult, error)
}

// AnchorGenerator produces one intermediate anchor image.
type AnchorGenerator interface {
	GenerateAnchor(ctx context.Context, anchor store.VideoAnchor, plan store.SuperShotRequest) (assetID string, err error)
}

// AssemblyResult is the outcome of assembling a chain.
type AssemblyResult struct {
	AssetID          string
	ActualDurationMS int
	JoinsJSON        string
	Receipt          string
}

// Assembler concatenates ordered segment assets into one video asset and reports
// the measured duration and joins.
type Assembler interface {
	Assemble(ctx context.Context, asm store.VideoAssembly, segments []store.VideoSegment) (AssemblyResult, error)
}

// Service orchestrates Super Shot planning, per-segment generation and final
// assembly. It is driven by the worker but is fully testable with fakes.
type Service struct {
	Store         *store.Store
	Generate      Generator
	GenerateAnchor AnchorGenerator
	Assemble      Assembler
	Log           *slog.Logger
}

// PlanRequest is one Super Shot planning request (spec §6.1).
type PlanRequest struct {
	WorkspaceID         string
	ProjectID           string
	VideoID             string
	ShotID              string
	ShotSpecID          string
	SpecVersion         int
	WorkflowVersionID   string
	ModelProfileID      string
	RequestedDurationMS int
	FPSNum              int
	FPSDen              int
	Width               int
	Height              int
	BasePositive        string
	BaseNegative        string
	ShotIntent          string
	Camera              string
	StartState          string
	EndState            string
	StartAssetID        string
	EndAssetID          string
	IntermediateStates  []string
	SegmentActions      []string
	ContinuityLocks     []string
	PreferredStrategy   string
	ContextMode         string
}

// PlanResult carries the persisted draft plan and the compiled chain.
type PlanResult struct {
	Plan         *store.SuperShotPlan
	Chain        *ChainPlan
	Capabilities *workflows.Capabilities
}

// PlanDraft validates, solves timing, builds the anchor chain and persists a
// draft plan. It never enqueues generation (spec §4.8).
func (s *Service) PlanDraft(ctx context.Context, in PlanRequest, userID string) (*PlanResult, error) {
	if err := ValidateRequestedDuration(in.RequestedDurationMS); err != nil {
		return nil, err
	}
	if in.StartAssetID == "" || in.EndAssetID == "" {
		return nil, fmt.Errorf("an approved start and end image are required")
	}
	version, err := s.Store.GetWorkflowVersion(ctx, in.WorkflowVersionID)
	if err != nil {
		return nil, fmt.Errorf("workflow version: %w", err)
	}
	caps, err := workflows.ParseCapabilities(version.ManifestJSON)
	if err != nil {
		return nil, err
	}
	if caps == nil || caps.VideoTiming == nil {
		return nil, fmt.Errorf("the selected workflow declares no video timing capability; choose another workflow")
	}
	if err := caps.Validate(); err != nil {
		return nil, err
	}
	bindings, err := s.Store.ListBindings(ctx, version.ID)
	if err != nil {
		return nil, err
	}
	if !hasBinding(bindings, "first_frame_asset") || !hasBinding(bindings, "last_frame_asset") {
		return nil, fmt.Errorf("the selected workflow does not declare first and last image bindings")
	}

	mode := in.ContextMode
	if mode == "" {
		mode = ContextAnchorOnly
	}
	if mode != ContextAnchorOnly {
		if !caps.SupportsContext() {
			return nil, fmt.Errorf("this workflow supports continuation but cannot target the supplied last image; use Shared images (anchor-only)")
		}
		// Even with a context-capable capability declaration, no verified graph
		// exists yet that combines context with terminal-image conditioning.
		return nil, fmt.Errorf("%s motion context is not yet available: no verified workflow combines continuation with the supplied last image", mode)
	}

	fps := RationalFPS{Num: in.FPSNum, Den: in.FPSDen}
	if !fps.Valid() {
		return nil, fmt.Errorf("invalid frame rate %d/%d", in.FPSNum, in.FPSDen)
	}
	timing, err := SolveTiming(TimingRequest{RequestedDurationMS: in.RequestedDurationMS, FPS: fps}, caps.VideoTiming, in.PreferredStrategy)
	if err != nil {
		return nil, err
	}
	chain, err := BuildPlan(PlanBuildInput{
		Timing: timing, StartAssetID: in.StartAssetID, EndAssetID: in.EndAssetID,
		StartState: in.StartState, EndState: in.EndState, BasePositive: in.BasePositive, BaseNegative: in.BaseNegative,
		ShotIntent: in.ShotIntent, Camera: in.Camera, ContinuityLocks: in.ContinuityLocks,
		IntermediateStates: in.IntermediateStates, SegmentActions: in.SegmentActions, ContextMode: mode,
	})
	if err != nil {
		return nil, err
	}

	anchors := make([]store.AnchorInput, 0, len(chain.Anchors))
	for _, a := range chain.Anchors {
		anchors = append(anchors, store.AnchorInput{
			Index: a.Index, TimeOffsetMS: a.TimeOffsetMS, Kind: a.Kind,
			StatePrompt: a.StatePrompt, NegativePrompt: a.NegativePrompt,
			ContinuityJSON: mustJSON(a.Continuity), AssetID: a.AssetID, SourceAnchorID: a.SourceAnchorID,
		})
	}
	segments := make([]store.SegmentInput, 0, len(chain.Segments))
	for _, seg := range chain.Segments {
		segments = append(segments, store.SegmentInput{
			ActionPrompt: seg.ActionPrompt, Positive: seg.ActionPrompt, Negative: in.BaseNegative,
			ContinuityJSON: mustJSON(seg.Continuity),
			StartOffsetMS:  seg.StartOffsetMS, EndOffsetMS: seg.EndOffsetMS, DurationMS: seg.DurationMS,
			FrameCount: seg.Frames, FPSNum: fps.Num, FPSDen: fps.Den, ContextMode: mode,
		})
	}
	continuityJSON := mustJSON(in.ContinuityLocks)
	persisted, err := s.Store.CreateSuperShotPlan(ctx, store.SuperShotRequestInput{
		WorkspaceID: in.WorkspaceID, ProjectID: in.ProjectID, VideoID: in.VideoID, ShotID: in.ShotID,
		ShotSpecID: in.ShotSpecID, SpecVersion: in.SpecVersion,
		WorkflowVersionID: in.WorkflowVersionID, ModelProfileID: in.ModelProfileID, Strategy: chain.Strategy,
		RequestedDurationMS: in.RequestedDurationMS, PlannedDurationMS: chain.PlannedDurationMS,
		FPSNum: fps.Num, FPSDen: fps.Den, Width: in.Width, Height: in.Height,
		BasePositive: in.BasePositive, BaseNegative: in.BaseNegative, ContextMode: mode,
		PlanSummary: chain.Summary, ContinuityJSON: continuityJSON,
	}, anchors, segments, userID)
	if err != nil {
		return nil, err
	}
	// Record context dependencies now that segment ids exist.
	if mode != ContextAnchorOnly {
		for i := 1; i < len(persisted.Segments); i++ {
			_ = s.Store.SetSegmentDependsOn(ctx, persisted.Segments[i].ID, persisted.Segments[i-1].ID)
		}
	}
	return &PlanResult{Plan: persisted, Chain: chain, Capabilities: caps}, nil
}

// Approve validates the persisted plan and queues its initial work (spec §7).
func (s *Service) Approve(ctx context.Context, superShotID string, revision int) error {
	plan, err := s.Store.GetSuperShotRequest(ctx, superShotID)
	if err != nil {
		return err
	}
	anchors, err := s.Store.ListAnchors(ctx, superShotID)
	if err != nil {
		return err
	}
	segments, err := s.Store.ListSegmentsForSuperShot(ctx, superShotID)
	if err != nil {
		return err
	}
	if err := ValidatePersistedPlan(plan, anchors, segments); err != nil {
		return err
	}
	if err := s.Store.ApproveSuperShot(ctx, superShotID, revision); err != nil {
		return err
	}
	// Queue missing intermediate anchors; supplied endpoints already have assets.
	for _, a := range anchors {
		if a.Kind != AnchorIntermediate || a.AssetID.Valid {
			continue
		}
		if _, err := s.Store.EnqueueJob(ctx, plan.WorkspaceID, "generate_anchor", "video_anchor", a.ID, "supershot:"+plan.ID+":anchor:"+strconv.Itoa(a.Index)); err != nil {
			return err
		}
		_ = s.Store.SetAnchorStatus(ctx, a.ID, "queued", "")
	}
	_ = s.Store.SetSuperShotStatus(ctx, superShotID, "generating_anchors")
	return s.enqueueReadySegments(ctx, superShotID)
}

// onAnchorReady queues any pending segments whose two anchors now have assets.
func (s *Service) enqueueReadySegments(ctx context.Context, superShotID string) error {
	anchors, err := s.Store.ListAnchors(ctx, superShotID)
	if err != nil {
		return err
	}
	segments, err := s.Store.ListSegmentsForSuperShot(ctx, superShotID)
	if err != nil {
		return err
	}
	assetFor := map[string]bool{}
	for _, a := range anchors {
		assetFor[a.ID] = a.AssetID.Valid
	}
	queued := 0
	for _, seg := range segments {
		if seg.Status != "pending" || !seg.StartAnchorID.Valid || !seg.EndAnchorID.Valid {
			continue
		}
		if !assetFor[seg.StartAnchorID.String] || !assetFor[seg.EndAnchorID.String] {
			continue
		}
		if seg.ContextMode != ContextAnchorOnly {
			// Context mode requires the selected predecessor attempt's validated
			// context artifact; it remains unavailable until a verified adapter.
			continue
		}
		if _, err := s.Store.EnqueueJob(ctx, seg.WorkspaceID, "generate_segment", "video_segment", seg.ID, "supershot:"+seg.RequestID+":segment:"+strconv.Itoa(seg.Index)); err != nil {
			return err
		}
		queued++
	}
	if queued > 0 {
		_ = s.Store.SetSuperShotStatus(ctx, superShotID, "generating_segments")
	}
	return nil
}

// AnchorReady is called after an intermediate anchor succeeds; it queues the
// dependent segments.
func (s *Service) AnchorReady(ctx context.Context, anchorID string) error {
	anchor, err := s.Store.GetAnchor(ctx, anchorID)
	if err != nil {
		return err
	}
	if anchor.Kind != AnchorIntermediate {
		return nil
	}
	return s.enqueueReadySegments(ctx, anchor.SuperShotRequestID)
}

// ChangeAnchor marks an intermediate anchor's asset as changed and invalidates
// the segments that depend on it (spec §7.1, §7.3).
func (s *Service) ChangeAnchor(ctx context.Context, anchorID string) error {
	anchor, err := s.Store.GetAnchor(ctx, anchorID)
	if err != nil {
		return err
	}
	if anchor.Kind != AnchorIntermediate {
		return fmt.Errorf("only intermediate anchors can be changed here")
	}
	plan, err := s.Store.GetSuperShotRequest(ctx, anchor.SuperShotRequestID)
	if err != nil {
		return err
	}
	segments, err := s.Store.ListSegmentsForSuperShot(ctx, anchor.SuperShotRequestID)
	if err != nil {
		return err
	}
	for _, seg := range segments {
		if (seg.StartAnchorID.Valid && seg.StartAnchorID.String == anchorID) ||
			(seg.EndAnchorID.Valid && seg.EndAnchorID.String == anchorID) {
			_ = s.Store.MarkSegmentStale(ctx, seg.ID)
			if seg.ContextMode != ContextAnchorOnly && plan.GenerationRequestID.Valid {
				_, _ = s.Store.MarkDownstreamStale(ctx, plan.GenerationRequestID.String, seg.ID)
			}
		}
	}
	// Any change to a contributing segment or anchor invalidates the assembly.
	if plan.GenerationRequestID.Valid {
		_ = s.Store.InvalidateAssembly(ctx, plan.GenerationRequestID.String)
	}
	return nil
}

// RunSegment generates one segment from its adjacent anchors, then attempts
// assembly when the whole chain is ready.
func (s *Service) RunSegment(ctx context.Context, segmentID string) error {
	seg, err := s.Store.GetSegment(ctx, segmentID)
	if err != nil {
		return err
	}
	if err := s.Store.SetSegmentRunning(ctx, seg.ID, ""); err != nil {
		return err
	}
	result, err := s.Generate.GenerateSegment(ctx, *seg)
	if err != nil {
		_ = s.Store.SetSegmentFailed(ctx, seg.ID, err.Error())
		return err
	}
	if err := s.Store.SetSegmentSucceeded(ctx, seg.ID, result.AssetID, result.StartAssetID, result.EndAssetID); err != nil {
		return err
	}
	_, err = s.TryAssemble(ctx, seg.RequestID)
	return err
}

// RetrySegment resets one failed segment and queues only that segment again.
// A retry creates a new generation attempt; the old one is preserved as history
// (spec §7.3).
func (s *Service) RetrySegment(ctx context.Context, segmentID string) error {
	seg, err := s.Store.GetSegment(ctx, segmentID)
	if err != nil {
		return err
	}
	// The old segment attempt is history; create a new one.
	wfID := ""
	if seg.SuperShotRequestID.Valid {
		if plan, err := s.Store.GetSuperShotRequest(ctx, seg.SuperShotRequestID.String); err == nil {
			wfID = plan.WorkflowVersionID
		}
	}
	if _, err := s.Store.NewSegmentAttempt(ctx, seg.ID, seg.RequestID, wfID); err != nil {
		return err
	}
	if err := s.Store.ResetSegmentForRetry(ctx, seg.ID); err != nil {
		return err
	}
	_ = s.Store.InvalidateAssembly(ctx, seg.RequestID)
	_, err = s.Store.EnqueueJob(ctx, seg.WorkspaceID, "generate_segment", "video_segment", seg.ID, "supershot:"+seg.RequestID+":segment:"+strconv.Itoa(seg.Index))
	return err
}

// RetryAnchor re-queues one failed intermediate anchor.
func (s *Service) RetryAnchor(ctx context.Context, anchorID string) error {
	anchor, err := s.Store.GetAnchor(ctx, anchorID)
	if err != nil {
		return err
	}
	if err := s.Store.ResetAnchorForRetry(ctx, anchor.ID); err != nil {
		return err
	}
	plan, err := s.Store.GetSuperShotRequest(ctx, anchor.SuperShotRequestID)
	if err != nil {
		return err
	}
	if plan.GenerationRequestID.Valid {
		_ = s.Store.InvalidateAssembly(ctx, plan.GenerationRequestID.String)
	}
	_, err = s.Store.EnqueueJob(ctx, plan.WorkspaceID, "generate_anchor", "video_anchor", anchor.ID, "supershot:"+plan.ID+":anchor:"+strconv.Itoa(anchor.Index))
	return err
}

// TryAssemble queues assembly once every current segment is ready. It is
// idempotent for a settled assembly.
func (s *Service) TryAssemble(ctx context.Context, requestID string) (bool, error) {
	ok, err := s.Store.AllSegmentsReady(ctx, requestID)
	if err != nil || !ok {
		return false, err
	}
	segs, err := s.Store.ListSegments(ctx, requestID)
	if err != nil || len(segs) == 0 {
		return false, err
	}
	first := segs[0]
	planned := 0
	if first.SuperShotRequestID.Valid {
		if plan, err := s.Store.GetSuperShotRequest(ctx, first.SuperShotRequestID.String); err == nil {
			planned = plan.PlannedDurationMS
		}
	}
	asm, err := s.Store.UpsertAssembly(ctx, requestID, nullStrVal(first.SuperShotRequestID), first.WorkspaceID, first.ProjectID, first.VideoID, first.ShotID, planned)
	if err != nil {
		return false, err
	}
	if asm.Status == "succeeded" || asm.Status == "running" {
		return true, nil
	}
	if _, err := s.Store.EnqueueJob(ctx, first.WorkspaceID, "assemble_video", "video_assembly", requestID, "supershot:"+requestID+":assemble"); err != nil {
		return false, err
	}
	return true, nil
}

// RunAssembly concatenates a request's current segment assets in order, verifies
// the measured duration and records joins (spec §7.4, §15.7).
func (s *Service) RunAssembly(ctx context.Context, requestID string) error {
	asm, err := s.Store.GetAssembly(ctx, requestID)
	if err != nil {
		return err
	}
	segs, err := s.Store.ListSegments(ctx, requestID)
	if err != nil {
		return err
	}
	current := make([]store.VideoSegment, 0, len(segs))
	assetIDs := make([]string, 0, len(segs))
	for _, seg := range segs {
		if seg.Status != "succeeded" || !seg.AssetID.Valid || seg.Stale {
			return fmt.Errorf("cannot assemble; segment %d is not current", seg.Index)
		}
		current = append(current, seg)
		assetIDs = append(assetIDs, seg.AssetID.String)
	}
	if len(current) == 0 {
		return fmt.Errorf("cannot assemble; no current segments")
	}
	if err := s.Store.SetAssemblyRunning(ctx, asm.ID); err != nil {
		return err
	}
	result, err := s.Assemble.Assemble(ctx, *asm, current)
	if err != nil {
		_ = s.Store.SetAssemblyFailed(ctx, asm.ID, err.Error())
		return err
	}
	if err := VerifyAssemblyDuration(result.ActualDurationMS, asm.PlannedDurationMS, current); err != nil {
		_ = s.Store.SetAssemblyFailed(ctx, asm.ID, err.Error())
		return err
	}
	return s.Store.SetAssemblySucceeded(ctx, asm.ID, result.AssetID, assetIDs, result.ActualDurationMS, result.JoinsJSON, result.Receipt)
}

// ValidatePersistedPlan re-checks the chain invariants on stored rows.
func ValidatePersistedPlan(plan *store.SuperShotRequest, anchors []store.VideoAnchor, segments []store.VideoSegment) error {
	if len(anchors) != len(segments)+1 {
		return fmt.Errorf("plan: %d anchors must equal %d segments + 1", len(anchors), len(segments))
	}
	if len(anchors) == 0 {
		return fmt.Errorf("plan: empty plan")
	}
	if anchors[0].TimeOffsetMS != 0 {
		return fmt.Errorf("plan: first anchor must start at 0ms")
	}
	if anchors[len(anchors)-1].TimeOffsetMS != plan.PlannedDurationMS {
		return fmt.Errorf("plan: last anchor must end at the planned duration")
	}
	for i := range anchors {
		if i > 0 && anchors[i].TimeOffsetMS <= anchors[i-1].TimeOffsetMS {
			return fmt.Errorf("plan: anchor times must increase strictly")
		}
		if anchors[i].Kind == AnchorIntermediate && anchors[i].StatePrompt == "" {
			return fmt.Errorf("plan: intermediate anchor %d has no state", i)
		}
	}
	total := 0
	for _, seg := range segments {
		if seg.ActionPrompt == "" {
			return fmt.Errorf("plan: segment %d has no movement prompt", seg.Index)
		}
		if seg.StartAnchorID.Valid && seg.EndAnchorID.Valid {
			if seg.StartAnchorID.String != anchors[seg.Index].ID || seg.EndAnchorID.String != anchors[seg.Index+1].ID {
				return fmt.Errorf("plan: segment %d does not reference adjacent anchors", seg.Index)
			}
		}
		want := anchors[seg.Index+1].TimeOffsetMS - anchors[seg.Index].TimeOffsetMS
		if seg.DurationMS != want {
			return fmt.Errorf("plan: segment %d duration does not match its anchors", seg.Index)
		}
		total += seg.DurationMS
	}
	if total != plan.PlannedDurationMS {
		return fmt.Errorf("plan: segments total %dms, expected %dms", total, plan.PlannedDurationMS)
	}
	return nil
}

// VerifyAssemblyDuration rejects an assembly whose measured duration differs
// from the plan by more than one output frame plus container rounding.
func VerifyAssemblyDuration(actualMS, plannedMS int, segments []store.VideoSegment) error {
	if plannedMS <= 0 {
		return nil
	}
	fpsNum, fpsDen := 0, 1
	for _, seg := range segments {
		if seg.FPSNum > 0 {
			fpsNum, fpsDen = seg.FPSNum, seg.FPSDen
			break
		}
	}
	tolerance := 250 // container rounding floor
	if fpsNum > 0 && fpsDen > 0 {
		frameMS := 1000 * fpsDen / fpsNum
		if frameMS > tolerance {
			tolerance = frameMS
		}
	}
	diff := actualMS - plannedMS
	if diff < 0 {
		diff = -diff
	}
	if diff > tolerance {
		return fmt.Errorf("assembled duration %dms differs from planned %dms by more than one frame (%dms)", actualMS, plannedMS, tolerance)
	}
	return nil
}

func hasBinding(bindings []store.WorkflowBinding, field string) bool {
	for _, b := range bindings {
		if b.SemanticField == field {
			return true
		}
	}
	return false
}

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil || len(raw) == 0 {
		return "[]"
	}
	return string(raw)
}

func nullStrVal(v sql.NullString) string {
	if v.Valid {
		return v.String
	}
	return ""
}
