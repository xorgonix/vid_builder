package worker

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"vidbuild/internal/exporter"
	"vidbuild/internal/store"
	"vidbuild/internal/video"
	"vidbuild/internal/workflows"
)

// videoService wires Super Shot orchestration to the real adapters.
func (w *Worker) videoService() *video.Service {
	return &video.Service{Store: w.store, Generate: w, GenerateAnchor: w, Assemble: w, Log: w.log}
}

// processAnchor generates one intermediate anchor image and, once it has an
// asset, queues the segments that depend on it.
func (w *Worker) processAnchor(ctx context.Context, job *store.Job) error {
	anchor, err := w.store.GetAnchor(ctx, job.AggregateID)
	if err != nil {
		_ = w.store.UpdateJobState(ctx, job.ID, "failed", "anchor", "error", err.Error(), 10000)
		return err
	}
	plan, err := w.store.GetSuperShotRequest(ctx, anchor.SuperShotRequestID)
	if err != nil {
		_ = w.store.UpdateJobState(ctx, job.ID, "failed", "anchor", "error", err.Error(), 10000)
		return err
	}
	// C07: cancellation is a barrier; no anchor is generated for a cancelled plan.
	if plan.Status == "cancelled" {
		_ = w.store.SetAnchorStatus(ctx, anchor.ID, "planned", "plan cancelled")
		return w.store.UpdateJobState(ctx, job.ID, "cancelled", "anchor", "info", "Plan cancelled; anchor not generated", 10000)
	}
	// R2: the frozen queue-time inputs were edited before this job ran.
	if anchor.QueuedInputHash == "" {
		_ = w.store.SetAnchorStatus(ctx, anchor.ID, "planned", "superseded")
		return w.store.UpdateJobState(ctx, job.ID, "cancelled", "anchor", "info", "Superseded; anchor inputs edited after enqueue", 10000)
	}
	_ = w.store.SetAnchorStatus(ctx, anchor.ID, "running", "")
	_ = w.store.UpdateJobState(ctx, job.ID, "running", "generate", "info", "Generating intermediate image", 3000)
	assetID, err := w.GenerateAnchor(ctx, *anchor, *plan)
	if err != nil {
		fresh, _ := w.store.GetAnchor(ctx, anchor.ID)
		if fresh != nil && fresh.ImageAttemptID.Valid {
			_ = w.store.RecordAttemptFailure(ctx, fresh.ImageAttemptID.String, "anchor_failed", err.Error())
		}
		_ = w.store.SetAnchorStatus(ctx, anchor.ID, "failed", err.Error())
		_ = w.store.UpdateJobState(ctx, job.ID, "failed", "anchor", "error", err.Error(), 10000)
		return err
	}
	published, err := w.store.SetAnchorSucceeded(ctx, anchor.ID, assetID)
	if err != nil {
		_ = w.store.UpdateJobState(ctx, job.ID, "failed", "anchor", "error", err.Error(), 10000)
		return err
	}
	if published == 0 {
		// R2: the anchor's frozen inputs were edited while the image generated;
		// the late result is history and the anchor stays invalidated.
		return w.store.UpdateJobState(ctx, job.ID, "cancelled", "anchor", "info", "Superseded; anchor inputs edited during generation", 10000)
	}
	if err := w.videoService().AnchorReady(ctx, anchor.ID); err != nil {
		return err
	}
	return w.store.UpdateJobState(ctx, job.ID, "succeeded", "anchor", "info", "Anchor generated", 10000)
}

// processSegment generates exactly one video segment from its adjacent anchors.
func (w *Worker) processSegment(ctx context.Context, job *store.Job) error {
	svc := w.videoService()
	if err := svc.RunSegment(ctx, job.AggregateID); err != nil {
		if errors.Is(err, video.ErrSuperseded) {
			// R2: immutable superseded outcome, not a provider failure.
			return w.store.UpdateJobState(ctx, job.ID, "cancelled", "segment", "info", "Superseded; segment inputs edited after enqueue", 10000)
		}
		_ = w.store.UpdateJobState(ctx, job.ID, "failed", "segment", "error", err.Error(), 10000)
		return err
	}
	return w.store.UpdateJobState(ctx, job.ID, "succeeded", "segment", "info", "Segment generated", 10000)
}

// processAssembly concatenates a plan's current segments into one video.
func (w *Worker) processAssembly(ctx context.Context, job *store.Job) error {
	svc := w.videoService()
	if err := svc.RunAssembly(ctx, job.AggregateID); err != nil {
		_ = w.store.UpdateJobState(ctx, job.ID, "failed", "assemble", "error", err.Error(), 10000)
		return err
	}
	return w.store.UpdateJobState(ctx, job.ID, "succeeded", "assemble", "info", "Video assembled", 10000)
}

// GenerateSegment implements video.Generator. It conditions on the segment's own
// start and end anchors and never falls back to the shot's outer endpoints
// (spec §7.2).
func (w *Worker) GenerateSegment(ctx context.Context, seg store.VideoSegment) (video.SegmentResult, error) {
	if !seg.SuperShotRequestID.Valid || !seg.StartAnchorID.Valid || !seg.EndAnchorID.Valid {
		return video.SegmentResult{}, fmt.Errorf("segment is not part of a Super Shot chain")
	}
	plan, err := w.store.GetSuperShotRequest(ctx, seg.SuperShotRequestID.String)
	if err != nil {
		return video.SegmentResult{}, fmt.Errorf("load plan: %w", err)
	}
	start, err := w.store.GetAnchor(ctx, seg.StartAnchorID.String)
	if err != nil {
		return video.SegmentResult{}, fmt.Errorf("load start anchor: %w", err)
	}
	end, err := w.store.GetAnchor(ctx, seg.EndAnchorID.String)
	if err != nil {
		return video.SegmentResult{}, fmt.Errorf("load end anchor: %w", err)
	}
	if !start.AssetID.Valid || !end.AssetID.Valid {
		return video.SegmentResult{}, fmt.Errorf("segment anchors do not both have images yet")
	}
	version, err := w.store.GetWorkflowVersion(ctx, plan.WorkflowVersionID)
	if err != nil {
		return video.SegmentResult{}, fmt.Errorf("load workflow: %w", err)
	}
	dbBindings, err := w.store.ListBindings(ctx, version.ID)
	if err != nil {
		return video.SegmentResult{}, err
	}
	bindings := make([]workflows.Binding, 0, len(dbBindings))
	for _, b := range dbBindings {
		bindings = append(bindings, workflows.Binding{SemanticField: b.SemanticField, NodeID: b.NodeID, InputName: b.InputName, ValueType: b.ValueType, Required: b.Required})
	}
	startName, err := w.stageAssetCached(ctx, start.AssetID.String)
	if err != nil {
		return video.SegmentResult{}, fmt.Errorf("stage start anchor: %w", err)
	}
	endName, err := w.stageAssetCached(ctx, end.AssetID.String)
	if err != nil {
		return video.SegmentResult{}, fmt.Errorf("stage end anchor: %w", err)
	}

	seed := rand.Int63()
	positive := seg.CompiledPrompt
	if positive == "" {
		positive = seg.ActionPrompt
	}
	if positive == "" {
		positive = seg.Positive
	}
	values := map[string]any{
		"first_frame_positive": positive,
		"first_frame_negative": seg.Negative,
		"first_frame_asset":    startName,
		"last_frame_asset":     endName,
		"width":                plan.Width,
		"height":               plan.Height,
		"seed":                 seed,
	}
	if seg.FrameCount > 0 {
		values["frame_count"] = seg.FrameCount
	}
	if seg.FPSNum > 0 && seg.FPSDen > 0 {
		values["fps"] = float64(seg.FPSNum) / float64(seg.FPSDen)
	}
	if cp := w.profileCheckpoint(ctx, plan); cp != "" {
		values["model_name"] = cp
	}

	graph, err := workflows.CloneGraph(version.GraphJSON)
	if err != nil {
		return video.SegmentResult{}, err
	}
	graph, err = workflows.ApplyBindings(graph, bindings, values)
	if err != nil {
		return video.SegmentResult{}, err
	}
	boundJSON, _ := json.Marshal(values)

	started := time.Now()
	resp, err := w.comfy.Submit(ctx, graph, w.owner)
	if err != nil {
		return video.SegmentResult{}, fmt.Errorf("submit: %w", err)
	}
	entry, err := w.comfy.Wait(ctx, resp.PromptID, 0, nil)
	if err != nil {
		return video.SegmentResult{}, fmt.Errorf("wait: %w", err)
	}
	if entry.Status.StatusStr != "success" {
		return video.SegmentResult{}, fmt.Errorf("comfyui status %q", entry.Status.StatusStr)
	}
	outputNode := workflows.OutputNodeID(bindings)
	if outputNode == "" {
		for n := range entry.Outputs {
			outputNode = n
		}
	}
	out, ok := entry.Outputs[outputNode]
	if !ok || len(out.Images) == 0 {
		return video.SegmentResult{}, fmt.Errorf("no output on node %q", outputNode)
	}
	img := out.Images[0]
	body, err := w.comfy.Fetch(ctx, img)
	if err != nil {
		return video.SegmentResult{}, fmt.Errorf("fetch: %w", err)
	}
	defer body.Close()
	kind, mime := outputMedia(img.Filename)
	relDir := fmt.Sprintf("%s/%s/videos/%s/super-shots/%s/segments", seg.WorkspaceID, seg.ProjectID, seg.VideoID, seg.SuperShotRequestID.String)
	uri, checksum, size, err := w.media.Put(relDir, fmt.Sprintf("seg-%02d-%s", seg.Index, img.Filename), body)
	if err != nil {
		return video.SegmentResult{}, fmt.Errorf("store segment: %w", err)
	}
	assetID, err := w.store.CreateAsset(ctx, store.Asset{
		WorkspaceID: seg.WorkspaceID, ProjectID: seg.ProjectID,
		GenerationAttemptID: seg.AttemptID, Kind: kind, Role: "motion",
		StorageURI: uri, MimeType: mime, SizeBytes: size, Checksum: checksum,
	})
	if err != nil {
		return video.SegmentResult{}, fmt.Errorf("record segment asset: %w", err)
	}
	w.makeThumbnail(ctx, seg.WorkspaceID, seg.ProjectID, assetID, kind, mime)
	if seg.AttemptID.Valid {
		receipt, _ := json.Marshal(map[string]any{"client_id": w.owner, "prompt_id": resp.PromptID})
		_ = w.store.RecordAttemptSuccess(ctx, seg.AttemptID.String, version.GraphSHA, "{}", string(boundJSON), string(receipt), time.Since(started).Milliseconds())
	}
	return video.SegmentResult{AssetID: assetID, StartAssetID: start.AssetID.String, EndAssetID: end.AssetID.String}, nil
}

// GenerateAnchor implements video.AnchorGenerator using an image workflow.
// Text-only anchors are never auto-generated (R1.5); reference mode requires a
// workflow that actually declares reference bindings (R1.3).
func (w *Worker) GenerateAnchor(ctx context.Context, anchor store.VideoAnchor, plan store.SuperShotRequest) (string, error) {
	if plan.ImageMode == "text_only" {
		return "", fmt.Errorf("this plan uses text-only anchors; attach an image instead of generating one")
	}
	version, err := w.anchorImageWorkflow(ctx, &plan)
	if err != nil {
		return "", err
	}
	dbBindings, err := w.store.ListBindings(ctx, version.ID)
	if err != nil {
		return "", err
	}
	bindings := make([]workflows.Binding, 0, len(dbBindings))
	for _, b := range dbBindings {
		bindings = append(bindings, workflows.Binding{SemanticField: b.SemanticField, NodeID: b.NodeID, InputName: b.InputName, ValueType: b.ValueType, Required: b.Required})
	}
	// R1.3: in reference mode the graph must actually declare a reference input,
	// otherwise the creator is told to choose a capability-matched workflow.
	hasReferenceBinding := false
	for _, b := range bindings {
		if b.SemanticField == "reference_asset" || b.SemanticField == "reference_asset_2" {
			hasReferenceBinding = true
			break
		}
	}
	if plan.ImageMode != "text_only" && !hasReferenceBinding {
		return "", fmt.Errorf("the selected image workflow declares no reference binding; it cannot condition intermediate images on the supplied endpoints (choose another image workflow or switch to text-only mode)")
	}

	// A dedicated image attempt records the anchor's provenance (C03).
	attemptID := ""
	if plan.GenerationRequestID.Valid {
		if id, aerr := w.store.NewAnchorAttempt(ctx, anchor.ID, plan.GenerationRequestID.String, version.ID); aerr == nil {
			attemptID = id
		}
	}
	if attemptID != "" {
		_ = w.store.MarkAttemptRunning(ctx, attemptID, "", "", 0)
	}

	values := map[string]any{
		"first_frame_positive": anchor.StatePrompt,
		"first_frame_negative": anchor.NegativePrompt,
		"width":                plan.Width,
		"height":               plan.Height,
		"seed":                 rand.Int63(),
	}
	// Use the image model profile, never the video checkpoint (C03).
	if cp := w.imageProfileCheckpoint(ctx, &plan); cp != "" {
		values["model_name"] = cp
	}

	// Reference conditioning: the supplied endpoints plus the previously accepted
	// anchor are staged through whatever reference bindings the graph declares.
	refs := w.anchorReferences(ctx, anchor, &plan)
	boundRefs := map[string]string{}
	for _, b := range bindings {
		if assetID, ok := refs[b.SemanticField]; ok && assetID != "" {
			up, uerr := w.stageAssetCached(ctx, assetID)
			if uerr != nil {
				return "", fmt.Errorf("stage reference %s: %w", b.SemanticField, uerr)
			}
			values[b.SemanticField] = up
			boundRefs[b.SemanticField] = up
		}
	}
	// R1.9: when the graph uses the previous generated anchor as its motion
	// reference, that anchor's image must already be selected — we do not vary
	// references according to which parallel job happens to finish first.
	if _, needsPredecessor := refs["reference_asset"]; needsPredecessor {
		if _, bound := boundRefs["reference_asset"]; !bound {
			return "", fmt.Errorf("the previous anchor must have an accepted image before anchor %d can be conditioned on it; generate the predecessor first", anchor.Index)
		}
	}

	graph, err := workflows.CloneGraph(version.GraphJSON)
	if err != nil {
		return "", err
	}
	graph, err = workflows.ApplyBindings(graph, bindings, values)
	if err != nil {
		return "", err
	}
	boundJSON, _ := json.Marshal(values)
	started := time.Now()
	resp, err := w.comfy.Submit(ctx, graph, w.owner)
	if err != nil {
		return "", fmt.Errorf("submit anchor: %w", err)
	}
	entry, err := w.comfy.Wait(ctx, resp.PromptID, 0, nil)
	if err != nil {
		return "", fmt.Errorf("wait anchor: %w", err)
	}
	if entry.Status.StatusStr != "success" {
		return "", fmt.Errorf("comfyui status %q", entry.Status.StatusStr)
	}
	outputNode := workflows.OutputNodeID(bindings)
	if outputNode == "" {
		for n := range entry.Outputs {
			outputNode = n
		}
	}
	out, ok := entry.Outputs[outputNode]
	if !ok || len(out.Images) == 0 {
		return "", fmt.Errorf("no anchor output on node %q", outputNode)
	}
	img := out.Images[0]
	body, err := w.comfy.Fetch(ctx, img)
	if err != nil {
		return "", fmt.Errorf("fetch anchor: %w", err)
	}
	defer body.Close()
	// Validate it is actually an image, not a relabelled video (C03).
	kind, mime := outputMedia(img.Filename)
	if kind != "image" {
		return "", fmt.Errorf("anchor workflow returned %s (%s), expected an image", img.Filename, mime)
	}
	// R1.8: an extension is not proof; verify the actual bytes are a decodable image.
	head := make([]byte, 8)
	if _, err := io.ReadFull(body, head); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return "", fmt.Errorf("anchor output is too short to be a valid image")
		}
		return "", fmt.Errorf("read anchor output: %w", err)
	}
	if !isImageHeader(head) {
		return "", fmt.Errorf("anchor output is not a decodable PNG/JPEG image")
	}
	payload := io.MultiReader(bytes.NewReader(head), body)
	relDir := fmt.Sprintf("%s/%s/videos/%s/super-shots/%s/anchors", plan.WorkspaceID, plan.ProjectID, plan.VideoID, plan.ID)
	uri, checksum, size, err := w.media.Put(relDir, fmt.Sprintf("anchor-%02d-%s", anchor.Index, img.Filename), payload)
	if err != nil {
		return "", fmt.Errorf("store anchor: %w", err)
	}
	asset := store.Asset{
		WorkspaceID: plan.WorkspaceID, ProjectID: plan.ProjectID,
		Kind: kind, Role: "video_anchor", StorageURI: uri, MimeType: mime, SizeBytes: size, Checksum: checksum,
	}
	if attemptID != "" {
		asset.GenerationAttemptID = sql.NullString{String: attemptID, Valid: true}
	}
	assetID, err := w.store.CreateAsset(ctx, asset)
	if err != nil {
		return "", fmt.Errorf("record anchor asset: %w", err)
	}
	w.makeThumbnail(ctx, plan.WorkspaceID, plan.ProjectID, assetID, kind, mime)
	if attemptID != "" {
		receipt, _ := json.Marshal(map[string]any{
			"client_id": w.owner, "prompt_id": resp.PromptID, "image_workflow": version.ID,
			"reference_bound": boundRefs,
		})
		resolved, _ := json.Marshal(map[string]string{"state": anchor.StatePrompt, "negative": anchor.NegativePrompt})
		_ = w.store.RecordAttemptSuccess(ctx, attemptID, version.GraphSHA, string(resolved), string(boundJSON), string(receipt), time.Since(started).Milliseconds())
	}
	return assetID, nil
}

// anchorImageWorkflow resolves the plan's chosen image workflow. When the plan
// explicitly names one, that choice is binding — a missing or inaccessible
// version is an error, never a silent fallback (contract R1.2).
func (w *Worker) anchorImageWorkflow(ctx context.Context, plan *store.SuperShotRequest) (*store.WorkflowVersion, error) {
	if plan.ImageWorkflowVersionID.Valid && plan.ImageWorkflowVersionID.String != "" {
		v, err := w.store.GetWorkflowVersion(ctx, plan.ImageWorkflowVersionID.String)
		if err != nil {
			return nil, fmt.Errorf("the selected image workflow could not be loaded; choose another image workflow: %w", err)
		}
		return v, nil
	}
	return w.pickImageWorkflow(ctx)
}

// imageProfileCheckpoint returns the plan's image model checkpoint, if any. It
// deliberately ignores the video model profile (contract C03).
func (w *Worker) imageProfileCheckpoint(ctx context.Context, plan *store.SuperShotRequest) string {
	if plan == nil || !plan.ImageModelProfileID.Valid {
		return ""
	}
	p, err := w.store.GetModelProfile(ctx, plan.WorkspaceID, plan.ImageModelProfileID.String)
	if err != nil {
		return ""
	}
	return p.ProfileCheckpoint()
}

// anchorReferences maps reference semantic fields to the asset ids that should
// condition an intermediate image: the supplied endpoints and the previous
// accepted anchor.
func (w *Worker) anchorReferences(ctx context.Context, anchor store.VideoAnchor, plan *store.SuperShotRequest) map[string]string {
	out := map[string]string{}
	anchors, err := w.store.ListAnchors(ctx, plan.ID)
	if err != nil || len(anchors) == 0 {
		return out
	}
	if anchors[0].AssetID.Valid {
		out["first_frame_asset"] = anchors[0].AssetID.String
	}
	last := anchors[len(anchors)-1]
	if last.AssetID.Valid {
		out["last_frame_asset"] = last.AssetID.String
		out["reference_asset_2"] = last.AssetID.String
	}
	// The previous accepted anchor is the strongest local reference.
	for i := range anchors {
		if anchors[i].Index == anchor.Index-1 && anchors[i].AssetID.Valid {
			out["reference_asset"] = anchors[i].AssetID.String
		}
	}
	return out
}

// Assemble implements video.Assembler: ordered segment assets become one MP4, and
// the measured duration and joins are returned for verification and review.
func (w *Worker) Assemble(ctx context.Context, asm store.VideoAssembly, segs []store.VideoSegment) (video.AssemblyResult, error) {
	if len(segs) == 0 {
		return video.AssemblyResult{}, fmt.Errorf("no segments to assemble")
	}
	width, height, fpsNum, fpsDen := 0, 0, 0, 1
	if asm.SuperShotRequestID.Valid {
		if plan, err := w.store.GetSuperShotRequest(ctx, asm.SuperShotRequestID.String); err == nil {
			width, height, fpsNum, fpsDen = plan.Width, plan.Height, plan.FPSNum, plan.FPSDen
		}
	}
	if width <= 0 {
		width = 1920
	}
	if height <= 0 {
		height = 1080
	}
	if fpsNum <= 0 {
		fpsNum, fpsDen = segs[0].FPSNum, segs[0].FPSDen
	}
	if fpsNum <= 0 {
		fpsNum, fpsDen = 8, 1
	}
	if fpsDen <= 0 {
		fpsDen = 1
	}
	clips := make([]exporter.Clip, 0, len(segs))
	planned := 0
	for _, seg := range segs {
		asset, err := w.store.GetAsset(ctx, seg.AssetID.String)
		if err != nil {
			return video.AssemblyResult{}, fmt.Errorf("segment %d asset: %w", seg.Index, err)
		}
		path, err := w.media.Resolve(asset.StorageURI)
		if err != nil {
			return video.AssemblyResult{}, fmt.Errorf("segment %d path: %w", seg.Index, err)
		}
		dur := seg.DurationMS
		if dur > 0 {
			planned += dur
		}
		clips = append(clips, exporter.Clip{Path: path, IsImage: false, DurationMs: dur, TransitionOut: "cut"})
	}
	ex := exporter.New(w.cfg.FFmpeg.BinaryPath, width, height, fpsNum, fpsDen, w.cfg.FFmpeg.RequestTimeoutSec)
	tempDir := w.cfg.Media.TempDir
	if !filepath.IsAbs(tempDir) {
		if abs, err := filepath.Abs(tempDir); err == nil {
			tempDir = abs
		}
	}
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return video.AssemblyResult{}, err
	}
	outPath := filepath.Join(tempDir, "super-shot-"+asm.ID+".mp4")
	defer os.Remove(outPath)
	if err := ex.Assemble(ctx, clips, outPath); err != nil {
		return video.AssemblyResult{}, fmt.Errorf("assemble: %w", err)
	}
	// C08: the measured duration must come from the file. A failed probe yields an
	// unverified delivery, never a synthetic "measured" duration.
	measured, perr := exporter.ProbeDurationMS(ctx, w.cfg.FFmpeg.BinaryPath, outPath)
	if perr != nil {
		return video.AssemblyResult{}, fmt.Errorf("duration unverified (ffprobe failed): %w", perr)
	}
	actual := measured
	receipt := fmt.Sprintf("ffmpeg concat, %d segments, %dx%d @%d/%d, planned=%dms, ffprobe duration=%dms", len(segs), width, height, fpsNum, fpsDen, planned, measured)
	f, err := os.Open(outPath)
	if err != nil {
		return video.AssemblyResult{}, err
	}
	defer f.Close()
	relDir := fmt.Sprintf("%s/%s/videos/%s/super-shots/%s/assembly", asm.WorkspaceID, asm.ProjectID, asm.VideoID, nullStrVal(asm.SuperShotRequestID))
	uri, checksum, size, err := w.media.Put(relDir, "super-shot-"+asm.ID+".mp4", f)
	if err != nil {
		return video.AssemblyResult{}, fmt.Errorf("store assembled video: %w", err)
	}
	assetID, err := w.store.CreateAsset(ctx, store.Asset{
		WorkspaceID: asm.WorkspaceID, ProjectID: asm.ProjectID,
		Kind: "video", Role: "video", StorageURI: uri, MimeType: "video/mp4",
		SizeBytes: size, Checksum: checksum,
	})
	if err != nil {
		return video.AssemblyResult{}, fmt.Errorf("record assembled asset: %w", err)
	}
	w.makeThumbnail(ctx, asm.WorkspaceID, asm.ProjectID, assetID, "video", "video/mp4")

	joins := make([]map[string]any, 0, len(segs)-1)
	offset := 0
	for i := 0; i < len(segs)-1; i++ {
		offset += segs[i].DurationMS
		joins = append(joins, map[string]any{
			"index": i, "offset_ms": offset,
			"before_asset": segs[i].AssetID.String, "after_asset": segs[i+1].AssetID.String,
		})
	}
	joinsJSON, _ := json.Marshal(joins)
	return video.AssemblyResult{AssetID: assetID, ActualDurationMS: actual, JoinsJSON: string(joinsJSON), Receipt: receipt}, nil
}

// stageAssetCached uploads an asset to ComfyUI once per checksum.
func (w *Worker) stageAssetCached(ctx context.Context, assetID string) (string, error) {
	asset, err := w.store.GetAsset(ctx, assetID)
	if err != nil {
		return "", err
	}
	if asset.Checksum == "" {
		return "", fmt.Errorf("asset has no verified checksum")
	}
	w.upMu.Lock()
	name, ok := w.uploads[asset.Checksum]
	w.upMu.Unlock()
	if ok {
		return name, nil
	}
	f, err := w.media.Open(asset.StorageURI)
	if err != nil {
		return "", err
	}
	defer f.Close()
	up, err := w.comfy.UploadImage(ctx, filepath.Base(asset.StorageURI), f, "")
	if err != nil {
		return "", err
	}
	w.upMu.Lock()
	if w.uploads == nil {
		w.uploads = map[string]string{}
	}
	w.uploads[asset.Checksum] = up.Name
	w.upMu.Unlock()
	return up.Name, nil
}

// pickImageWorkflow finds a selectable image-task workflow for anchor generation.
func (w *Worker) pickImageWorkflow(ctx context.Context) (*store.WorkflowVersion, error) {
	versions, err := w.store.ListSelectableVersions(ctx)
	if err != nil {
		return nil, err
	}
	for i := range versions {
		pkg, err := w.store.GetWorkflowPackage(ctx, versions[i].WorkflowPackageID)
		if err != nil {
			continue
		}
		if pkg.Task == "image" {
			return &versions[i], nil
		}
	}
	return nil, fmt.Errorf("no image workflow is available to generate intermediate anchors; attach an image to each anchor instead")
}

// profileCheckpoint returns the plan's model checkpoint, if any.
func (w *Worker) profileCheckpoint(ctx context.Context, plan *store.SuperShotRequest) string {
	if plan == nil || !plan.ModelProfileID.Valid {
		return ""
	}
	p, err := w.store.GetModelProfile(ctx, plan.WorkspaceID, plan.ModelProfileID.String)
	if err != nil {
		return ""
	}
	return p.ProfileCheckpoint()
}

func nullStrVal(v sql.NullString) string {
	if v.Valid {
		return v.String
	}
	return ""
}
