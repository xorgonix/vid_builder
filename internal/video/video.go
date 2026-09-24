// Package video implements Super Shot planning: a requested duration is compiled
// into a direct generation or an ordered chain of adjacent segments bounded by
// shared anchors. Timing rules come from the selected workflow's capabilities,
// never from application constants (spec §2.1, §5.5, §15.7).
package video

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"vidbuild/internal/store"
	"vidbuild/internal/workflows"
)

// ErrSuperseded reports queue work whose frozen input snapshot was invalidated
// before it executed (R2): the inputs were edited after enqueue, so the run is
// superseded rather than failed.
var ErrSuperseded = errors.New("superseded: inputs edited after enqueue")

// SegmentInputHash is the canonical hash over the immutable, queue-relevant
// segment inputs (R2): compiled movement, prompts, timing, anchors and context.
// It must not change when only status or progress changes.
func SegmentInputHash(seg store.VideoSegment, workflowVersionID string) string {
	raw := fmt.Sprintf("seg:%s|%s|%s|%s|%s|%d|%d/%d|%d-%d|%s|%s|%s",
		seg.ID, seg.CompiledPrompt, seg.RawAction, seg.Positive, seg.Negative, seg.FrameCount,
		seg.FPSNum, seg.FPSDen, seg.StartOffsetMS, seg.EndOffsetMS,
		seg.StartAnchorID.String, seg.EndAnchorID.String,
		seg.ContextMode+"/"+seg.ContextInputID.String)
	return hashHex(raw + "|" + workflowVersionID)
}

// AnchorInputHash is the canonical hash over the immutable, queue-relevant
// anchor inputs (R2): the selected reference source and image workflow.
func AnchorInputHash(a store.VideoAnchor, imageWorkflowVersionID string) string {
	raw := fmt.Sprintf("anchor:%s|%s|%s|%s|%s|%s|%d/%d",
		a.ID, a.StatePrompt, a.NegativePrompt, a.ContinuityJSON, a.SourceAnchorID.String,
		imageWorkflowVersionID, a.TimeOffsetMS, a.Index)
	return hashHex(raw)
}

func hashHex(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

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

// SuperShotSuggestion is a planner's concrete proposal: intermediate states and
// one movement per interval, plus provenance for the record.
type SuperShotSuggestion struct {
	IntermediateStates []string
	SegmentMovements   []string
	ProvenanceJSON     string
}

// SuperShotPromptRequest is the creative input handed to a planner.
type SuperShotPromptRequest struct {
	ProjectContext     string
	ShotPurpose        string
	CompleteMovement   string
	StartState         string
	EndState           string
	SegmentDurationsMS []int
	ContinuityLocks    []string
}

// Planner proposes concrete Super Shot plans (contract C02). It is implemented
// by an adapter over the LM Studio planner in the web layer.
type Planner interface {
	ProposeSuperShot(ctx context.Context, req SuperShotPromptRequest) (SuperShotSuggestion, error)
}

// Service orchestrates Super Shot planning, per-segment generation and final
// assembly. It is driven by the worker but is fully testable with fakes.
type Service struct {
	Store          *store.Store
	Generate       Generator
	GenerateAnchor AnchorGenerator
	Assemble       Assembler
	Planner        Planner
	Log            *slog.Logger
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
	// Planner provenance and image-workflow selection (contract C02/C03).
	PlannerStatus          string
	PlannerWarningsJSON    string
	PlannerProvenanceJSON  string
	ImageWorkflowVersionID string
	ImageModelProfileID    string
	ImageMode              string
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

	// C02: concrete intermediate states and interval movements. If the creator
	// supplied them, they are authoritative (manual). Otherwise ask the planner;
	// if it is unavailable or fails, keep a visibly incomplete draft with the
	// real error and do not queue placeholder prompts.
	plannerStatus := in.PlannerStatus
	if plannerStatus == "" {
		plannerStatus = "incomplete"
	}
	plannerWarnings := []string{}
	if len(in.IntermediateStates) > 0 {
		plannerStatus = "manual"
	} else if s.Planner != nil {
		durations := make([]int, 0, len(timing.Segments))
		for _, st := range timing.Segments {
			durations = append(durations, st.DurationMS)
		}
		sug, perr := s.Planner.ProposeSuperShot(ctx, SuperShotPromptRequest{
			ShotPurpose: in.ShotIntent, CompleteMovement: in.BasePositive,
			StartState: in.StartState, EndState: in.EndState,
			SegmentDurationsMS: durations, ContinuityLocks: in.ContinuityLocks,
		})
		if perr != nil {
			plannerWarnings = append(plannerWarnings, "planner unavailable: "+perr.Error())
			in.PlannerStatus = "incomplete"
		} else {
			in.IntermediateStates = sug.IntermediateStates
			if len(in.SegmentActions) == 0 {
				in.SegmentActions = sug.SegmentMovements
			}
			plannerStatus = "validated"
			in.PlannerProvenanceJSON = sug.ProvenanceJSON
		}
	} else {
		plannerWarnings = append(plannerWarnings, "no planner is configured; describe the intermediate states manually before approval")
	}
	in.PlannerStatus = plannerStatus

	chain, err := BuildPlan(PlanBuildInput{
		Timing: timing, StartAssetID: in.StartAssetID, EndAssetID: in.EndAssetID,
		StartState: in.StartState, EndState: in.EndState, BasePositive: in.BasePositive, BaseNegative: in.BaseNegative,
		ShotIntent: in.ShotIntent, Camera: in.Camera, ContinuityLocks: in.ContinuityLocks,
		IntermediateStates: in.IntermediateStates, SegmentActions: in.SegmentActions, ContextMode: mode,
	})
	if err != nil {
		return nil, err
	}
	plannerWarnings = append(plannerWarnings, chain.Warnings...)
	in.PlannerWarningsJSON = mustJSON(plannerWarnings)

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
			ActionPrompt: seg.ActionPrompt, RawAction: seg.RawAction, CompiledPrompt: seg.ActionPrompt,
			Positive: seg.ActionPrompt, Negative: in.BaseNegative,
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
		ShotPurpose: in.ShotIntent, CompleteMovement: in.BasePositive,
		PlannerStatus: in.PlannerStatus, PlannerWarningsJSON: in.PlannerWarningsJSON,
		PlannerProvenanceJSON: in.PlannerProvenanceJSON,
		ImageWorkflowVersionID: in.ImageWorkflowVersionID, ImageModelProfileID: in.ImageModelProfileID,
		ImageMode: in.ImageMode,
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
		if _, err := s.Store.EnqueueJob(ctx, plan.WorkspaceID, "generate_anchor", "video_anchor", a.ID, "supershot:anchor:"+a.ID+":gen:"+strconv.Itoa(a.Revision)); err != nil {
			return err
		}
		if err := s.Store.QueueAnchorInput(ctx, a.ID, AnchorInputHash(a, plan.ImageWorkflowVersionID.String)); err != nil {
			return err
		}
		_ = s.Store.SetAnchorStatus(ctx, a.ID, "queued", "")
	}
	_ = s.Store.SetSuperShotStatus(ctx, superShotID, "generating_anchors")
	return s.enqueueReadySegments(ctx, superShotID)
}

// onAnchorReady queues any pending segments whose two anchors now have assets.
func (s *Service) enqueueReadySegments(ctx context.Context, superShotID string) error {
	// C07: dispatch is a barrier. A pre-approval draft or a cancelled plan never
	// dispatches video segments, even when an anchor job just completed.
	plan, err := s.Store.GetSuperShotRequest(ctx, superShotID)
	if err != nil {
		return err
	}
	switch plan.Status {
	case "approved", "generating_anchors", "generating_segments", "assembling":
	default:
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
		attemptRef := "none"
		if seg.AttemptID.Valid {
			attemptRef = seg.AttemptID.String
		}
		key := "supershot:segment:" + seg.ID + ":attempt:" + attemptRef
		if _, err := s.Store.EnqueueJob(ctx, seg.WorkspaceID, "generate_segment", "video_segment", seg.ID, key); err != nil {
			return err
		}
		if err := s.Store.QueueSegmentInput(ctx, seg.ID, SegmentInputHash(seg, plan.WorkflowVersionID)); err != nil {
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
	if err := s.enqueueReadySegments(ctx, anchor.SuperShotRequestID); err != nil {
		return err
	}
	return s.RefreshPlanStatus(ctx, anchor.SuperShotRequestID)
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
			// The frozen queue-time snapshot of a dependent queued run includes
			// this anchor; editing the anchor supersedes those runs (R2).
			_ = s.Store.ClearSegmentQueuedInput(ctx, seg.ID)
			if seg.ContextMode != ContextAnchorOnly && plan.GenerationRequestID.Valid {
				_, _ = s.Store.MarkDownstreamStale(ctx, plan.GenerationRequestID.String, seg.ID)
			}
		}
	}
	// Any change to a contributing segment or anchor invalidates the assembly,
	// and any queued replacement attempt for this anchor is superseded (R2).
	_ = s.Store.ClearAnchorQueuedInput(ctx, anchor.ID)
	// Any change to a contributing segment or anchor invalidates the assembly.
	if plan.GenerationRequestID.Valid {
		_ = s.Store.InvalidateAssembly(ctx, plan.GenerationRequestID.String)
	}
	return nil
}

// RunSegment generates one segment from its adjacent anchors, then attempts
// assembly when the whole chain is ready. It refuses to start when the parent
// plan was cancelled, and never clears the persisted attempt link (C04, C07).
func (s *Service) RunSegment(ctx context.Context, segmentID string) error {
	seg, err := s.Store.GetSegment(ctx, segmentID)
	if err != nil {
		return err
	}
	if seg.QueuedInputHash == "" {
		// R2: the frozen queue-time inputs were edited before this job ran.
		return ErrSuperseded
	}
	if seg.SuperShotRequestID.Valid {
		plan, err := s.Store.GetSuperShotRequest(ctx, seg.SuperShotRequestID.String)
		if err != nil {
			return err
		}
		if plan.Status == "cancelled" {
			return fmt.Errorf("plan cancelled; segment %d not started", seg.Index)
		}
	}
	attemptID := ""
	if seg.AttemptID.Valid {
		attemptID = seg.AttemptID.String
	}
	if err := s.Store.SetSegmentRunning(ctx, seg.ID, attemptID); err != nil {
		return err
	}
	result, err := s.Generate.GenerateSegment(ctx, *seg)
	if err != nil {
		_ = s.Store.SetSegmentFailed(ctx, seg.ID, err.Error())
		return err
	}
	expected := ""
	if seg.AttemptID.Valid {
		expected = seg.AttemptID.String
	}
	n, err := s.Store.SetSegmentSucceededIfCurrent(ctx, seg.ID, expected, result.AssetID, result.StartAssetID, result.EndAssetID)
	if err != nil {
		return err
	}
	if n == 0 {
		// A newer attempt superseded this one; the result is history only (C06).
		return nil
	}
	_, err = s.TryAssemble(ctx, seg.RequestID)
	if seg.SuperShotRequestID.Valid {
		if rerr := s.RefreshPlanStatus(ctx, seg.SuperShotRequestID.String); rerr != nil && err == nil {
			err = rerr
		}
	}
	return err
}

// EditSegmentAction updates a segment's raw interval action and recompiles the
// movement prompt. In a draft plan this is cheap; once production has begun the
// affected output is marked stale and the assembly invalidated, so a stale
// compiled prompt cannot stay current (contract C06).
func (s *Service) EditSegmentAction(ctx context.Context, segmentID, rawAction string, revision int) error {
	seg, err := s.Store.GetSegment(ctx, segmentID)
	if err != nil {
		return err
	}
	if !seg.SuperShotRequestID.Valid {
		return fmt.Errorf("segment is not part of a Super Shot plan")
	}
	plan, err := s.Store.GetSuperShotRequest(ctx, seg.SuperShotRequestID.String)
	if err != nil {
		return err
	}
	anchors, err := s.Store.ListAnchors(ctx, seg.SuperShotRequestID.String)
	if err != nil {
		return err
	}
	startState, endState := "", ""
	if seg.Index < len(anchors) {
		startState = anchors[seg.Index].StatePrompt
	}
	if seg.Index+1 < len(anchors) {
		endState = anchors[seg.Index+1].StatePrompt
	}
	var locks []string
	_ = json.Unmarshal([]byte(plan.ContinuityJSON), &locks)
	compiled := CompileSegmentMovement(plan.ShotPurpose, plan.CompleteMovement, startState, endState, rawAction, "", plan.BaseNegative, locks)
	if err := s.Store.UpdateSegmentActionRaw(ctx, seg.ID, rawAction, compiled, revision); err != nil {
		return err
	}
	// Any creative edit invalidates the frozen queue-time snapshot and bumps the
	// immutable input revision, so a queued run of the old movement can never
	// publish (R2).
	if err := s.Store.ClearSegmentQueuedInput(ctx, seg.ID); err != nil {
		return err
	}
	switch plan.Status {
	case "draft", "awaiting_approval":
		// Cheap draft edit; no outputs exist yet.
	default:
		_ = s.Store.MarkSegmentStale(ctx, seg.ID)
		if plan.GenerationRequestID.Valid {
			_ = s.Store.InvalidateAssembly(ctx, plan.GenerationRequestID.String)
			if seg.ContextMode != ContextAnchorOnly {
				_, _ = s.Store.MarkDownstreamStale(ctx, plan.GenerationRequestID.String, seg.ID)
			}
		}
	}
	return nil
}

// RetrySegment resets one failed segment and queues only that segment again.
// A retry creates a new generation attempt; the old one is preserved as history
// (spec §7.3).
func (s *Service) RetrySegment(ctx context.Context, segmentID string) error {
	seg, err := s.Store.GetSegment(ctx, segmentID)
	if err != nil {
		return err
	}
	// The old segment attempt is history; create a new one and key the job on it
	// (C05) so a retry never reuses a terminal job.
	wfID := ""
	if seg.SuperShotRequestID.Valid {
		if plan, err := s.Store.GetSuperShotRequest(ctx, seg.SuperShotRequestID.String); err == nil {
			wfID = plan.WorkflowVersionID
			if plan.Status == "cancelled" {
				return fmt.Errorf("plan cancelled; segment not retried")
			}
		}
	}
	attemptID, err := s.Store.NewSegmentAttempt(ctx, seg.ID, seg.RequestID, wfID)
	if err != nil {
		return err
	}
	if err := s.Store.ResetSegmentForRetry(ctx, seg.ID); err != nil {
		return err
	}
	_ = s.Store.InvalidateAssembly(ctx, seg.RequestID)
	if _, err = s.Store.EnqueueJob(ctx, seg.WorkspaceID, "generate_segment", "video_segment", seg.ID, "supershot:segment:"+seg.ID+":attempt:"+attemptID); err != nil {
		return err
	}
	// Freeze the queue-time input snapshot for this attempt under its new job
	// identity (C05, R2).
	return s.Store.QueueSegmentInput(ctx, seg.ID, SegmentInputHash(*seg, wfID))
}

// RetryAnchor re-queues one failed intermediate anchor under a new revision key.
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
	if plan.Status == "cancelled" {
		return fmt.Errorf("plan cancelled; anchor not retried")
	}
	if plan.GenerationRequestID.Valid {
		_ = s.Store.InvalidateAssembly(ctx, plan.GenerationRequestID.String)
	}
	// Re-read the anchor: the reset bumped its revision, which is the new job key.
	fresh, err := s.Store.GetAnchor(ctx, anchor.ID)
	if err != nil {
		return err
	}
	_, err = s.Store.EnqueueJob(ctx, plan.WorkspaceID, "generate_anchor", "video_anchor", anchor.ID, "supershot:anchor:"+anchor.ID+":gen:"+strconv.Itoa(fresh.Revision))
	if err != nil {
		return err
	}
	// Freeze the queue-time input snapshot for this anchor attempt (R2).
	return s.Store.QueueAnchorInput(ctx, anchor.ID, AnchorInputHash(*fresh, plan.ImageWorkflowVersionID.String))
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
			if plan.Status == "cancelled" {
				return false, nil
			}
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
	// Key on the assembly revision: an invalidated assembly gets a new revision and
	// therefore a new job, while a duplicate dispatch of the same revision is inert.
	key := "supershot:assemble:" + requestID + ":rev:" + strconv.Itoa(asm.Revision)
	if _, err := s.Store.EnqueueJob(ctx, first.WorkspaceID, "assemble_video", "video_assembly", requestID, key); err != nil {
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
	inputRevision := 0
	hash := sha256.New()
	for _, seg := range segs {
		if seg.Status != "succeeded" || !seg.AssetID.Valid || seg.Stale {
			return fmt.Errorf("cannot assemble; segment %d is not current", seg.Index)
		}
		current = append(current, seg)
		assetIDs = append(assetIDs, seg.AssetID.String)
		if seg.Revision > inputRevision {
			inputRevision = seg.Revision
		}
		fmt.Fprintf(hash, "%s:%d;", seg.AssetID.String, seg.Revision)
	}
	if len(current) == 0 {
		return fmt.Errorf("cannot assemble; no current segments")
	}
	inputHash := hex.EncodeToString(hash.Sum(nil))
	if err := s.Store.SetAssemblyRunning(ctx, asm.ID, inputHash); err != nil {
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
	published, err := s.Store.SetAssemblySucceeded(ctx, asm.ID, result.AssetID, assetIDs, result.ActualDurationMS, inputRevision, inputHash, result.JoinsJSON, result.Receipt, "measured")
	if err == nil && published == 0 {
		// The assembly was invalidated (or its frozen snapshot dropped) while the
		// export ran; the output is history only (R2).
		return nil
	}
	if err == nil && asm.SuperShotRequestID.Valid {
		_ = s.RefreshPlanStatus(ctx, asm.SuperShotRequestID.String)
	}
	return err
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
		if anchors[i].Kind == AnchorIntermediate && IsPlaceholderState(anchors[i].StatePrompt) {
			return fmt.Errorf("plan: intermediate anchor %d is still a placeholder; describe it before approval", i)
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
	// Tolerance is one output frame plus a small documented container allowance
	// (C08), derived from the rational frame rate rather than a fixed floor.
	tolerance := 200
	if fpsNum > 0 && fpsDen > 0 {
		tolerance = (1000*fpsDen)/fpsNum + 40
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
