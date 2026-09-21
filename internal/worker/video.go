package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"vidbuild/internal/exporter"
	"vidbuild/internal/store"
	"vidbuild/internal/video"
	"vidbuild/internal/workflows"
)

// videoService wires the duration-video orchestration to real adapters.
func (w *Worker) videoService() *video.Service {
	return &video.Service{Store: w.store, Generate: w, Assemble: w, Log: w.log}
}

// processSegment generates exactly one video segment (retry-safe: it only touches
// the segment it was queued for).
func (w *Worker) processSegment(ctx context.Context, job *store.Job) error {
	svc := w.videoService()
	if err := svc.RunSegment(ctx, job.AggregateID); err != nil {
		_ = w.store.UpdateJobState(ctx, job.ID, "failed", "segment", "error", err.Error(), 10000)
		return err
	}
	return w.store.UpdateJobState(ctx, job.ID, "succeeded", "segment", "info", "segment generated", 10000)
}

// processAssembly concatenates a request's segment assets into one video.
func (w *Worker) processAssembly(ctx context.Context, job *store.Job) error {
	svc := w.videoService()
	if err := svc.RunAssembly(ctx, job.AggregateID); err != nil {
		_ = w.store.UpdateJobState(ctx, job.ID, "failed", "assemble", "error", err.Error(), 10000)
		return err
	}
	return w.store.UpdateJobState(ctx, job.ID, "succeeded", "assemble", "info", "video assembled", 10000)
}

// GenerateSegment implements video.Generator using the bound FLF2V workflow.
func (w *Worker) GenerateSegment(ctx context.Context, seg store.VideoSegment) (string, error) {
	req, err := w.store.GetRequest(ctx, seg.RequestID)
	if err != nil {
		return "", fmt.Errorf("load request: %w", err)
	}
	if !req.WorkflowVersionID.Valid {
		return "", fmt.Errorf("segment request has no workflow version")
	}
	version, err := w.store.GetWorkflowVersion(ctx, req.WorkflowVersionID.String)
	if err != nil {
		return "", fmt.Errorf("load workflow version: %w", err)
	}
	bindings, err := w.store.ListBindings(ctx, version.ID)
	if err != nil {
		return "", err
	}
	wfBindings := make([]workflows.Binding, 0, len(bindings))
	for _, b := range bindings {
		wfBindings = append(wfBindings, workflows.Binding{SemanticField: b.SemanticField, NodeID: b.NodeID, InputName: b.InputName, ValueType: b.ValueType, Required: b.Required})
	}
	if !hasBinding(wfBindings, "first_frame_asset") || !hasBinding(wfBindings, "last_frame_asset") {
		return "", fmt.Errorf("workflow does not declare first_frame_asset and last_frame_asset bindings")
	}

	var pl struct {
		Width      int     `json:"width"`
		Height     int     `json:"height"`
		Checkpoint string  `json:"checkpoint"`
		FPS        float64 `json:"fps"`
		Seed       int64   `json:"seed"`
	}
	_ = json.Unmarshal([]byte(req.RequestJSON), &pl)

	startName, err := w.stageAssetCached(ctx, seg.StartAssetID.String)
	if err != nil {
		return "", fmt.Errorf("stage start anchor: %w", err)
	}
	endName, err := w.stageAssetCached(ctx, seg.EndAssetID.String)
	if err != nil {
		return "", fmt.Errorf("stage end anchor: %w", err)
	}

	seed := pl.Seed
	if seed == 0 {
		seed = rand.Int63()
	}
	values := map[string]any{
		"first_frame_positive": seg.Positive,
		"first_frame_negative": seg.Negative,
		"width":                pl.Width,
		"height":               pl.Height,
		"seed":                 seed,
		"first_frame_asset":    startName,
		"last_frame_asset":     endName,
	}
	if seg.FrameCount > 0 {
		values["frame_count"] = seg.FrameCount
	}
	if seg.FPS > 0 {
		values["fps"] = seg.FPS
	} else if pl.FPS > 0 {
		values["fps"] = pl.FPS
	}
	if pl.Checkpoint != "" {
		values["model_name"] = pl.Checkpoint
	}

	graph, err := workflows.CloneGraph(version.GraphJSON)
	if err != nil {
		return "", err
	}
	graph, err = workflows.ApplyBindings(graph, wfBindings, values)
	if err != nil {
		return "", err
	}
	boundJSON, _ := json.Marshal(values)

	started := time.Now()
	resp, err := w.comfy.Submit(ctx, graph, w.owner)
	if err != nil {
		return "", fmt.Errorf("submit: %w", err)
	}
	entry, err := w.comfy.Wait(ctx, resp.PromptID, 0, nil)
	if err != nil {
		return "", fmt.Errorf("wait: %w", err)
	}
	if entry.Status.StatusStr != "success" {
		return "", fmt.Errorf("comfyui status %q", entry.Status.StatusStr)
	}
	outputNode := workflows.OutputNodeID(wfBindings)
	if outputNode == "" {
		for n := range entry.Outputs {
			outputNode = n
		}
	}
	out, ok := entry.Outputs[outputNode]
	if !ok || len(out.Images) == 0 {
		return "", fmt.Errorf("no output on node %q", outputNode)
	}
	img := out.Images[0]
	body, err := w.comfy.Fetch(ctx, img)
	if err != nil {
		return "", fmt.Errorf("fetch: %w", err)
	}
	defer body.Close()

	kind, mime := outputMedia(img.Filename)
	relDir := fmt.Sprintf("%s/%s/videos/%s/shots/%s/segments", seg.WorkspaceID, seg.ProjectID, seg.VideoID, seg.ShotID)
	uri, checksum, size, err := w.media.Put(relDir, fmt.Sprintf("seg-%02d-%s", seg.Index, img.Filename), io.LimitReader(body, 400<<20))
	if err != nil {
		return "", fmt.Errorf("store segment: %w", err)
	}
	assetID, err := w.store.CreateAsset(ctx, store.Asset{
		WorkspaceID: seg.WorkspaceID, ProjectID: seg.ProjectID,
		GenerationAttemptID: seg.AttemptID, Kind: kind, Role: "motion",
		StorageURI: uri, MimeType: mime, SizeBytes: size, Checksum: checksum,
	})
	if err != nil {
		return "", fmt.Errorf("record segment asset: %w", err)
	}
	w.makeThumbnail(ctx, seg.WorkspaceID, seg.ProjectID, assetID, kind, mime)
	if seg.AttemptID.Valid {
		receipt, _ := json.Marshal(map[string]any{"client_id": w.owner, "prompt_id": resp.PromptID})
		_ = w.store.RecordAttemptSuccess(ctx, seg.AttemptID.String, version.GraphSHA, "{}", string(boundJSON), string(receipt), time.Since(started).Milliseconds())
	}
	return assetID, nil
}

// Assemble implements video.Assembler: ordered segments become one MP4 asset.
func (w *Worker) Assemble(ctx context.Context, asm store.VideoAssembly, segs []store.VideoSegment) (string, error) {
	if len(segs) == 0 {
		return "", fmt.Errorf("no segments to assemble")
	}
	vid, err := w.store.GetVideoByID(ctx, asm.VideoID)
	if err != nil {
		return "", err
	}
	clips := make([]exporter.Clip, 0, len(segs))
	for _, seg := range segs {
		asset, err := w.store.GetAsset(ctx, seg.AssetID.String)
		if err != nil {
			return "", fmt.Errorf("segment %d asset: %w", seg.Index, err)
		}
		path, err := w.media.Resolve(asset.StorageURI)
		if err != nil {
			return "", fmt.Errorf("segment %d path: %w", seg.Index, err)
		}
		durMs := 0
		if seg.FrameCount > 0 && seg.FPS > 0 {
			durMs = int(math.Round(float64(seg.FrameCount) / seg.FPS * 1000))
		}
		if durMs <= 0 {
			durMs = video.SegmentSeconds * 1000
		}
		// Minimal cut; advanced transitions are deliberately out of scope.
		clips = append(clips, exporter.Clip{Path: path, IsImage: false, DurationMs: durMs, TransitionOut: "cut"})
	}

	ex := exporter.New(w.cfg.FFmpeg.BinaryPath, vid.Width, vid.Height, vid.FPSNum, w.cfg.FFmpeg.RequestTimeoutSec)
	tempDir := w.cfg.Media.TempDir
	if !filepath.IsAbs(tempDir) {
		if abs, err := filepath.Abs(tempDir); err == nil {
			tempDir = abs
		}
	}
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return "", err
	}
	outPath := filepath.Join(tempDir, "assembly-"+asm.ID+".mp4")
	defer os.Remove(outPath)
	if err := ex.Assemble(ctx, clips, outPath); err != nil {
		return "", fmt.Errorf("assemble: %w", err)
	}
	f, err := os.Open(outPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	relDir := fmt.Sprintf("%s/%s/videos/%s/shot/%s/video", asm.WorkspaceID, asm.ProjectID, asm.VideoID, asm.ShotID)
	uri, checksum, size, err := w.media.Put(relDir, "video-"+asm.ID+".mp4", f)
	if err != nil {
		return "", fmt.Errorf("store assembled video: %w", err)
	}
	assetID, err := w.store.CreateAsset(ctx, store.Asset{
		WorkspaceID: asm.WorkspaceID, ProjectID: asm.ProjectID,
		Kind: "video", Role: "video", StorageURI: uri, MimeType: "video/mp4",
		SizeBytes: size, Checksum: checksum,
	})
	if err != nil {
		return "", fmt.Errorf("record assembled asset: %w", err)
	}
	w.makeThumbnail(ctx, asm.WorkspaceID, asm.ProjectID, assetID, "video", "video/mp4")
	return assetID, nil
}

// stageAssetCached uploads an endpoint asset to ComfyUI once per checksum and
// reuses the uploaded name across segments (endpoint reuse).
func (w *Worker) stageAssetCached(ctx context.Context, assetID string) (string, error) {
	asset, err := w.store.GetAsset(ctx, assetID)
	if err != nil {
		return "", err
	}
	if asset.Checksum == "" {
		return "", fmt.Errorf("endpoint asset has no verified checksum")
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
