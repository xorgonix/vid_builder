// Package worker is the durable local job worker (spec §19). It claims jobs,
// invokes adapters, imports verified outputs and records evidence.
package worker

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"vidbuild/internal/backup"
	"vidbuild/internal/comfyui"
	"vidbuild/internal/config"
	"vidbuild/internal/exporter"
	"vidbuild/internal/media"
	"vidbuild/internal/store"
	"vidbuild/internal/thumbs"
	"vidbuild/internal/workflows"
)

// Worker runs generation jobs.
type Worker struct {
	cfg        *config.Config
	store      *store.Store
	media      *media.Store
	comfy      *comfyui.Client
	log        *slog.Logger
	owner      string
	backupsDir string

	upMu    sync.Mutex
	uploads map[string]string
}

// New constructs a worker.
func New(cfg *config.Config, st *store.Store, ms *media.Store, log *slog.Logger) *Worker {
	backups := filepath.Join(filepath.Dir(cfg.Database.Path), "backups")
	if abs, err := filepath.Abs(backups); err == nil {
		backups = abs
	}
	return &Worker{
		cfg: cfg, store: st, media: ms, comfy: comfyui.New(cfg.ComfyUI), log: log,
		owner: fmt.Sprintf("local-%d", time.Now().UnixNano()), backupsDir: backups,
	}
}

// Start runs the worker loop until ctx is cancelled.
func (w *Worker) Start(ctx context.Context) {
	go func() {
		interval := time.Duration(w.cfg.Worker.PollIntervalMs) * time.Millisecond
		if interval <= 0 {
			interval = 1500 * time.Millisecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for {
					if !w.runOnce(ctx) {
						break
					}
				}
			}
		}
	}()
}

// runOnce claims and processes at most one job. It reports whether work ran.
func (w *Worker) runOnce(ctx context.Context) bool {
	job, err := w.store.ClaimJob(ctx, w.owner, w.cfg.Worker.LeaseTTLSeconds)
	if err != nil {
		return false
	}
	if err := w.process(ctx, job); err != nil {
		w.log.Error("job failed", "job", job.ID, "err", err)
		if uerr := w.store.UpdateJobState(ctx, job.ID, "failed", "error", "error", err.Error(), 0); uerr != nil {
			w.log.Error("record job failure", "job", job.ID, "err", uerr)
		}
	}
	return true
}

type requestPayload struct {
	Positive   string         `json:"positive"`
	Negative   string         `json:"negative"`
	Width      int            `json:"width"`
	Height     int            `json:"height"`
	Seed       int64          `json:"seed"`
	SeedPolicy string         `json:"seed_policy"`
	Checkpoint string         `json:"checkpoint"`
	Layers     map[string]any `json:"layers"`

	// Video/motion controls (used by video workflows such as the bundled Wan
	// first/last package). Zero values leave the workflow defaults in place.
	FrameCount     int     `json:"frame_count"`
	FPS            float64 `json:"fps"`
	MotionPositive string  `json:"motion_positive"`
	MotionNegative string  `json:"motion_negative"`
	LastPositive   string  `json:"last_frame_positive"`
	LastNegative   string  `json:"last_frame_negative"`

	RequiresFirstLast bool   `json:"requires_first_last"`
	FirstFrameAssetID string `json:"first_frame_asset_id"`
	LastFrameAssetID  string `json:"last_frame_asset_id"`
}

func (w *Worker) process(ctx context.Context, job *store.Job) error {
	switch job.JobType {
	case "generate":
		return w.processGeneration(ctx, job)
	case "export":
		return w.processExport(ctx, job)
	case "generate_segment":
		return w.processSegment(ctx, job)
	case "generate_anchor":
		return w.processAnchor(ctx, job)
	case "assemble_video":
		return w.processAssembly(ctx, job)
	case "package":
		return w.processPackage(ctx, job)
	case "backup":
		return w.processBackup(ctx, job)
	default:
		return fmt.Errorf("worker: unsupported job type %q", job.JobType)
	}
}

func (w *Worker) processGeneration(ctx context.Context, job *store.Job) error {
	if job.JobType != "generate" {
		return fmt.Errorf("worker: unsupported job type %q", job.JobType)
	}
	req, err := w.store.GetRequest(ctx, job.AggregateID)
	if err != nil {
		return fmt.Errorf("worker: load request: %w", err)
	}
	if err := w.store.SetRequestStatus(ctx, req.ID, "running"); err != nil {
		w.log.Error("set request running", "err", err)
	}
	if err := w.store.UpdateJobState(ctx, job.ID, "running", "validate", "info", "Loading frozen request and workflow", 500); err != nil {
		return err
	}

	if !req.WorkflowVersionID.Valid {
		_ = w.store.SetRequestStatus(ctx, req.ID, "failed")
		return fmt.Errorf("worker: request has no workflow version")
	}
	version, err := w.store.GetWorkflowVersion(ctx, req.WorkflowVersionID.String)
	if err != nil {
		_ = w.store.SetRequestStatus(ctx, req.ID, "failed")
		return fmt.Errorf("worker: load workflow version: %w", err)
	}
	dbBindings, err := w.store.ListBindings(ctx, version.ID)
	if err != nil {
		return fmt.Errorf("worker: load bindings: %w", err)
	}
	bindings := make([]workflows.Binding, 0, len(dbBindings))
	for _, b := range dbBindings {
		bindings = append(bindings, workflows.Binding{SemanticField: b.SemanticField, NodeID: b.NodeID, InputName: b.InputName, ValueType: b.ValueType, Required: b.Required})
	}
	outputNode := workflows.OutputNodeID(bindings)

	var payload requestPayload
	if req.RequestJSON != "" {
		_ = json.Unmarshal([]byte(req.RequestJSON), &payload)
	}
	if payload.Width == 0 {
		payload.Width = 1024
	}
	if payload.Height == 0 {
		payload.Height = 1024
	}
	if payload.SeedPolicy == "" {
		payload.SeedPolicy = "random"
	}

	target, err := w.store.EnsureLocalComfyTarget(ctx, job.WorkspaceID, "")
	if err != nil {
		return fmt.Errorf("worker: ensure target: %w", err)
	}

	// Memory coordination (spec §19.4): free the planner's GPU memory before
	// local GPU work. The action and any failure are recorded on the job.
	if msg, failed := w.freePlannerMemory(ctx); msg != "" {
		level := "info"
		if failed {
			level = "warn"
		}
		_ = w.store.UpdateJobState(ctx, job.ID, "running", "memory", level, msg, 800)
	}

	// Preflight: capability check before submitting anything (spec §18.1).
	if err := w.store.UpdateJobState(ctx, job.ID, "running", "validate", "info", "Probing ComfyUI capabilities", 1000); err != nil {
		return err
	}
	if _, err := w.comfy.Stats(ctx); err != nil {
		_ = w.store.SetRequestStatus(ctx, req.ID, "failed")
		return fmt.Errorf("worker: ComfyUI preflight failed: %w", err)
	}

	attempts, err := w.store.ListAttempts(ctx, req.ID)
	if err != nil {
		return err
	}

	succeeded, failed := 0, 0
	for _, a := range attempts {
		if a.Status != "queued" {
			continue
		}
		seed := payload.Seed
		switch payload.SeedPolicy {
		case "stable":
			seed = payload.Seed + int64(a.AttemptNumber) - 1
		case "explicit":
			// keep payload.Seed
		default:
			seed = rand.Int63n(1 << 62)
		}
		if err := w.runAttempt(ctx, job, req, version, bindings, outputNode, target, a, seed, payload); err != nil {
			w.log.Error("attempt failed", "attempt", a.ID, "err", err)
			_ = w.store.RecordAttemptFailure(ctx, a.ID, "adapter_error", err.Error())
			_ = w.store.RecordObservation(ctx, a.ID, target.ID, "", version.ID, 0, 0, 0, 0, false)
			failed++
			continue
		}
		succeeded++
	}

	if succeeded == 0 {
		_ = w.store.SetRequestStatus(ctx, req.ID, "failed")
		return fmt.Errorf("worker: all %d attempts failed", failed)
	}
	_ = w.store.SetRequestStatus(ctx, req.ID, "completed")
	_ = w.store.EmitDomainEvent(ctx, job.WorkspaceID, "generation_request", req.ID, "generation.completed", "{}")
	if msg, failed := w.reloadPlannerMemory(ctx); msg != "" {
		level := "info"
		if failed {
			level = "warn"
		}
		_ = w.store.UpdateJobState(ctx, job.ID, "running", "memory", level, msg, 9900)
	}
	return w.store.UpdateJobState(ctx, job.ID, "succeeded", "persist", "info", fmt.Sprintf("%d candidate(s) produced", succeeded), 10000)
}

func (w *Worker) runAttempt(ctx context.Context, job *store.Job, req *store.GenerationRequest, version *store.WorkflowVersion, bindings []workflows.Binding, outputNode string, target *store.ExecutionTarget, a store.GenerationAttempt, seed int64, payload requestPayload) error {
	started := time.Now()
	if err := w.store.MarkAttemptRunning(ctx, a.ID, target.ID, payload.Checkpoint, seed); err != nil {
		return err
	}
	_ = w.store.UpdateJobState(ctx, job.ID, "running", "compile", "info", "Compiling bound graph", 2000)

	values := bindingValues(payload, seed)

	// Endpoint-conditioned motion: both endpoints must be staged as inputs and
	// the workflow must declare the bindings. There is no silent fallback
	// (spec §18.3).
	if payload.RequiresFirstLast {
		if !hasBinding(bindings, "first_frame_asset") || !hasBinding(bindings, "last_frame_asset") {
			return fmt.Errorf("workflow does not declare first_frame_asset and last_frame_asset bindings; refusing start-only fallback")
		}
		startA, err := w.store.GetAsset(ctx, payload.FirstFrameAssetID)
		if err != nil {
			return fmt.Errorf("missing first-frame asset: %w", err)
		}
		endA, err := w.store.GetAsset(ctx, payload.LastFrameAssetID)
		if err != nil {
			return fmt.Errorf("missing last-frame asset: %w", err)
		}
		if startA.Checksum == "" || endA.Checksum == "" {
			return fmt.Errorf("endpoint asset is missing a verified checksum")
		}
		sf, err := w.media.Open(startA.StorageURI)
		if err != nil {
			return fmt.Errorf("open first frame: %w", err)
		}
		upStart, err := w.comfy.UploadImage(ctx, filepath.Base(startA.StorageURI), sf, "")
		sf.Close()
		if err != nil {
			return fmt.Errorf("stage first frame: %w", err)
		}
		ef, err := w.media.Open(endA.StorageURI)
		if err != nil {
			return fmt.Errorf("open last frame: %w", err)
		}
		upEnd, err := w.comfy.UploadImage(ctx, filepath.Base(endA.StorageURI), ef, "")
		ef.Close()
		if err != nil {
			return fmt.Errorf("stage last frame: %w", err)
		}
		values["first_frame_asset"] = upStart.Name
		values["last_frame_asset"] = upEnd.Name
		values["endpoint_start_checksum"] = startA.Checksum
		values["endpoint_end_checksum"] = endA.Checksum
	}

	graph, err := workflows.CloneGraph(version.GraphJSON)
	if err != nil {
		return err
	}
	graph, err = workflows.ApplyBindings(graph, bindings, values)
	if err != nil {
		return err
	}
	boundJSON, _ := json.Marshal(values)

	_ = w.store.UpdateJobState(ctx, job.ID, "running", "submit", "info", "Submitting graph to ComfyUI", 3000)
	resp, err := w.comfy.Submit(ctx, graph, w.owner)
	if err != nil {
		return fmt.Errorf("submit: %w", err)
	}
	receipt, _ := json.Marshal(map[string]any{"client_id": w.owner, "prompt_id": resp.PromptID, "number": resp.Number})

	_ = w.store.UpdateJobState(ctx, job.ID, "running", "generate", "info", "Generating", 5000)
	entry, err := w.comfy.Wait(ctx, resp.PromptID, 0, nil)
	if err != nil {
		return fmt.Errorf("wait: %w", err)
	}
	if entry.Status.StatusStr != "success" {
		return fmt.Errorf("comfyui status %q", entry.Status.StatusStr)
	}
	if outputNode == "" {
		for n := range entry.Outputs {
			outputNode = n
		}
	}
	out, ok := entry.Outputs[outputNode]
	if !ok || len(out.Images) == 0 {
		return fmt.Errorf("no images on output node %q", outputNode)
	}
	img := out.Images[0]
	body, err := w.comfy.Fetch(ctx, img)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	defer body.Close()

	relDir := fmt.Sprintf("%s/%s/videos/%s/shots/%s/%s", job.WorkspaceID, req.ProjectID, req.VideoID, req.ShotID, req.OutputRole)
	uri, checksum, size, err := w.media.Put(relDir, img.Filename, io.LimitReader(body, 200<<20))
	if err != nil {
		return fmt.Errorf("store output: %w", err)
	}
	kind, mime := outputMedia(img.Filename)
	assetID, err := w.store.CreateAsset(ctx, store.Asset{
		WorkspaceID: job.WorkspaceID, ProjectID: req.ProjectID,
		GenerationAttemptID: sql.NullString{String: a.ID, Valid: true}, Kind: kind, Role: req.OutputRole,
		StorageURI: uri, MimeType: mime, SizeBytes: size, Checksum: checksum,
	})
	if err != nil {
		return fmt.Errorf("record asset: %w", err)
	}
	// Best-effort derived thumbnail; failure never fails the attempt.
	w.makeThumbnail(ctx, job.WorkspaceID, req.ProjectID, assetID, kind, mime)

	resolved, _ := json.Marshal(map[string]string{"positive": payload.Positive, "negative": payload.Negative})
	elapsed := time.Since(started).Milliseconds()
	if err := w.store.RecordAttemptSuccess(ctx, a.ID, version.GraphSHA, string(resolved), string(boundJSON), string(receipt), elapsed); err != nil {
		return err
	}
	_ = w.store.RecordObservation(ctx, a.ID, target.ID, payload.Checkpoint, version.ID, elapsed, 0, 0, 0, true)
	return nil
}

func nullString(v string) (out *string) {
	if v == "" {
		return nil
	}
	return &v
}

// bindingValues maps a frozen request payload onto the semantic field vocabulary.
// Optional video/motion controls are only included when set, so a workflow's own
// defaults remain authoritative (spec §18.3: no silent fallback).
func bindingValues(payload requestPayload, seed int64) map[string]any {
	values := map[string]any{
		"first_frame_positive": payload.Positive,
		"first_frame_negative": payload.Negative,
		"width":                payload.Width,
		"height":               payload.Height,
		"seed":                 seed,
	}
	if payload.Checkpoint != "" {
		values["model_name"] = payload.Checkpoint
	}
	if payload.FrameCount > 0 {
		values["frame_count"] = payload.FrameCount
	}
	if payload.FPS > 0 {
		values["fps"] = payload.FPS
	}
	if payload.MotionPositive != "" {
		values["motion_positive"] = payload.MotionPositive
	}
	if payload.MotionNegative != "" {
		values["motion_negative"] = payload.MotionNegative
	}
	if payload.LastPositive != "" {
		values["last_frame_positive"] = payload.LastPositive
	}
	if payload.LastNegative != "" {
		values["last_frame_negative"] = payload.LastNegative
	}
	return values
}

// outputMedia classifies a ComfyUI output filename into an asset kind and mime
// type. ComfyUI reports video outputs (webm/mp4) under the same "images" field
// as stills, so the filename extension is authoritative.
func outputMedia(filename string) (kind, mime string) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".webm":
		return "video", "video/webm"
	case ".mp4":
		return "video", "video/mp4"
	case ".webp":
		return "image", "image/webp"
	case ".jpg", ".jpeg":
		return "image", "image/jpeg"
	case ".gif":
		return "image", "image/gif"
	default:
		return "image", "image/png"
	}
}

// processExport assembles an edit into a single video (spec §15.7).
func (w *Worker) processExport(ctx context.Context, job *store.Job) error {
	exp, err := w.store.GetExport(ctx, job.AggregateID)
	if err != nil {
		return fmt.Errorf("worker: load export: %w", err)
	}
	edit, err := w.store.GetEditByID(ctx, exp.EditID)
	if err != nil {
		_ = w.store.FailExport(ctx, exp.ID, "edit not found")
		return fmt.Errorf("worker: load edit: %w", err)
	}
	video, err := w.store.GetVideoByID(ctx, edit.VideoID)
	if err != nil {
		_ = w.store.FailExport(ctx, exp.ID, "video not found")
		return fmt.Errorf("worker: load video: %w", err)
	}
	project, err := w.store.GetProjectByID(ctx, video.ProjectID)
	if err != nil {
		_ = w.store.FailExport(ctx, exp.ID, "project not found")
		return fmt.Errorf("worker: load project: %w", err)
	}
	items, err := w.store.ListEditItems(ctx, edit.ID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		_ = w.store.FailExport(ctx, exp.ID, "edit has no items")
		return fmt.Errorf("worker: edit has no items")
	}
	_ = w.store.UpdateJobState(ctx, job.ID, "running", "validate", "info", "Resolving edit items", 1000)

	clips := make([]exporter.Clip, 0, len(items))
	for _, it := range items {
		asset, err := w.store.GetAsset(ctx, it.AssetID)
		if err != nil {
			_ = w.store.FailExport(ctx, exp.ID, "missing edit asset")
			return fmt.Errorf("worker: missing asset %s: %w", it.AssetID, err)
		}
		path, err := w.media.Resolve(asset.StorageURI)
		if err != nil {
			_ = w.store.FailExport(ctx, exp.ID, "unsafe asset path")
			return fmt.Errorf("worker: resolve asset: %w", err)
		}
		clips = append(clips, exporter.Clip{
			Path: path, IsImage: asset.Kind == "image" || assetsIsImage(asset.MimeType),
			DurationMs: it.DurationMs, TransitionIn: it.TransitionIn, TransitionOut: it.TransitionOut,
		})
	}

	ex := exporter.New(w.cfg.FFmpeg.BinaryPath, video.Width, video.Height, video.FPSNum, w.cfg.FFmpeg.RequestTimeoutSec)
	if err := ex.Available(ctx); err != nil {
		_ = w.store.FailExport(ctx, exp.ID, err.Error())
		return err
	}
	tempDir := w.cfg.Media.TempDir
	if !filepath.IsAbs(tempDir) {
		if abs, err := filepath.Abs(tempDir); err == nil {
			tempDir = abs
		}
	}
	_ = os.MkdirAll(tempDir, 0o755)
	outPath := filepath.Join(tempDir, "export-"+exp.ID+".mp4")

	_ = w.store.UpdateJobState(ctx, job.ID, "running", "render", "info", "Assembling with FFmpeg", 4000)
	if err := ex.Assemble(ctx, clips, outPath); err != nil {
		_ = w.store.FailExport(ctx, exp.ID, err.Error())
		return err
	}

	f, err := os.Open(outPath)
	if err != nil {
		_ = w.store.FailExport(ctx, exp.ID, "open output failed")
		return err
	}
	defer f.Close()
	relDir := fmt.Sprintf("%s/%s/videos/%s/exports", project.WorkspaceID, project.ID, video.ID)
	uri, checksum, size, err := w.media.Put(relDir, "export-"+exp.ID+".mp4", f)
	if err != nil {
		_ = w.store.FailExport(ctx, exp.ID, err.Error())
		return err
	}
	_ = os.Remove(outPath)

	assetID, err := w.store.CreateAsset(ctx, store.Asset{
		WorkspaceID: project.WorkspaceID, ProjectID: project.ID,
		Kind: "export", Role: "edit", StorageURI: uri, MimeType: "video/mp4", SizeBytes: size, Checksum: checksum,
	})
	if err != nil {
		_ = w.store.FailExport(ctx, exp.ID, err.Error())
		return err
	}
	if err := w.store.CompleteExport(ctx, exp.ID, assetID); err != nil {
		return err
	}
	_ = w.store.SetEditStatus(ctx, edit.ID, "exported")
	_ = w.store.EmitDomainEvent(ctx, project.WorkspaceID, "edit", edit.ID, "edit.changed", "{}")
	return w.store.UpdateJobState(ctx, job.ID, "succeeded", "persist", "info", "Export written", 10000)
}

func assetsIsImage(mime string) bool {
	return len(mime) >= 6 && mime[:6] == "image/"
}

func hasBinding(bindings []workflows.Binding, field string) bool {
	for _, b := range bindings {
		if b.SemanticField == field {
			return true
		}
	}
	return false
}

// freePlannerMemory unloads the configured planner instances before local GPU
// work. Returns a job-event message and whether the action failed.
func (w *Worker) freePlannerMemory(ctx context.Context) (string, bool) {
	if !w.cfg.LLM.Enabled || !w.cfg.LLM.UnloadBeforeGPU || strings.TrimSpace(w.cfg.LLM.UnloadCommand) == "" {
		return "", false
	}
	out, err := runConfiguredCommand(ctx, w.cfg.LLM.UnloadCommand)
	if err != nil {
		w.log.Warn("memory coordination: unload failed", "err", err)
		return "Memory coordination: could not unload the planner: " + err.Error(), true
	}
	return "Memory coordination: freed planner GPU memory before local work" + summarize(out), false
}

// reloadPlannerMemory optionally reloads the planner after GPU work. When no
// reload command is configured the planner loads on demand at the next request.
func (w *Worker) reloadPlannerMemory(ctx context.Context) (string, bool) {
	if !w.cfg.LLM.Enabled || strings.TrimSpace(w.cfg.LLM.ReloadCommand) == "" {
		return "", false
	}
	out, err := runConfiguredCommand(ctx, w.cfg.LLM.ReloadCommand)
	if err != nil {
		w.log.Warn("memory coordination: reload failed", "err", err)
		return "Memory coordination: planner reload failed: " + err.Error(), true
	}
	return "Memory coordination: planner reloaded" + summarize(out), false
}

func runConfiguredCommand(ctx context.Context, commandLine string) (string, error) {
	fields := strings.Fields(commandLine)
	if len(fields) == 0 {
		return "", nil
	}
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, fields[0], fields[1:]...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func summarize(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	if len(out) > 200 {
		out = out[:200]
	}
	return ": " + out
}

// makeThumbnail derives and stores a thumbnail asset, logging (not failing) on
// error. Video outputs are reduced to a first frame with FFmpeg when available.
func (w *Worker) makeThumbnail(ctx context.Context, workspaceID, projectID, assetID, kind, mime string) {
	asset, err := w.store.GetAsset(ctx, assetID)
	if err != nil {
		return
	}
	var data []byte
	switch {
	case thumbs.IsImageMime(mime):
		f, err := w.media.Open(asset.StorageURI)
		if err != nil {
			w.log.Warn("thumbnail: open", "err", err)
			return
		}
		data, err = io.ReadAll(io.LimitReader(f, 64<<20))
		f.Close()
		if err != nil {
			w.log.Warn("thumbnail: read", "err", err)
			return
		}
	case kind == "video" && w.cfg.FFmpeg.Enabled:
		frame, err := w.extractVideoFrame(ctx, asset.StorageURI)
		if err != nil {
			w.log.Warn("thumbnail: video frame", "err", err)
			return
		}
		data = frame
	default:
		return
	}
	pngBytes, dw, dh, err := thumbs.GenerateImage(data, 0, 0)
	if err != nil {
		w.log.Warn("thumbnail: generate", "err", err)
		return
	}
	relDir := fmt.Sprintf("%s/%s/thumbs", workspaceID, projectID)
	uri, checksum, size, err := w.media.Put(relDir, assetID+".png", strings.NewReader(string(pngBytes)))
	if err != nil {
		w.log.Warn("thumbnail: store", "err", err)
		return
	}
	if _, err := w.store.CreateThumbnail(ctx, workspaceID, projectID, assetID, uri, "image/png", size, checksum, dw, dh); err != nil {
		w.log.Warn("thumbnail: record", "err", err)
	}
}

// extractVideoFrame renders the first video frame to PNG bytes using FFmpeg.
func (w *Worker) extractVideoFrame(ctx context.Context, storageURI string) ([]byte, error) {
	src, err := w.media.Resolve(storageURI)
	if err != nil {
		return nil, err
	}
	bin := w.cfg.FFmpeg.BinaryPath
	if bin == "" {
		bin = "ffmpeg"
	}
	tempDir := w.cfg.Media.TempDir
	if !filepath.IsAbs(tempDir) {
		if abs, err := filepath.Abs(tempDir); err == nil {
			tempDir = abs
		}
	}
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return nil, err
	}
	out := filepath.Join(tempDir, "frame-"+filepath.Base(storageURI)+".png")
	defer os.Remove(out)
	cmd := exec.CommandContext(ctx, bin, "-y", "-i", src, "-frames:v", "1", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %v: %s", err, strings.TrimSpace(string(b)))
	}
	return os.ReadFile(out)
}

// processPackage builds a portable project package (spec §12.3, §20).
func (w *Worker) processPackage(ctx context.Context, job *store.Job) error {
	project, err := w.store.GetProjectByID(ctx, job.AggregateID)
	if err != nil {
		return fmt.Errorf("worker: load project: %w", err)
	}
	_ = w.store.UpdateJobState(ctx, job.ID, "running", "collect", "info", "Collecting project rows and media", 2000)
	bundle, err := w.store.BuildProjectBundle(ctx, project.ID)
	if err != nil {
		return fmt.Errorf("worker: build bundle: %w", err)
	}

	tempDir := w.cfg.Media.TempDir
	if !filepath.IsAbs(tempDir) {
		if abs, err := filepath.Abs(tempDir); err == nil {
			tempDir = abs
		}
	}
	_ = os.MkdirAll(tempDir, 0o755)
	zipPath := filepath.Join(tempDir, "package-"+job.ID+".zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(zf)

	data, _ := bundle.MarshalJSON()
	if err := writeZipFile(zw, "project.json", data); err != nil {
		zw.Close()
		zf.Close()
		return err
	}
	manifest := map[string]any{
		"schema":      "vid-build-project-package",
		"version":     1,
		"project_id":  project.ID,
		"title":       project.Title,
		"media_count": len(bundle.Assets),
		"media":       bundle.Assets,
	}
	manifestJSON, _ := json.MarshalIndent(manifest, "", "  ")
	if err := writeZipFile(zw, "manifest.json", manifestJSON); err != nil {
		zw.Close()
		zf.Close()
		return err
	}
	_ = w.store.UpdateJobState(ctx, job.ID, "running", "collect", "info", "Packing media", 6000)
	for _, a := range bundle.Assets {
		f, err := w.media.Open(a.StorageURI)
		if err != nil {
			w.log.Warn("package: skip missing media", "uri", a.StorageURI, "err", err)
			continue
		}
		entry, err := zw.Create("media/" + a.StorageURI)
		if err != nil {
			f.Close()
			zw.Close()
			zf.Close()
			return err
		}
		if _, err := io.Copy(entry, f); err != nil {
			f.Close()
			zw.Close()
			zf.Close()
			return err
		}
		f.Close()
	}
	if err := zw.Close(); err != nil {
		zf.Close()
		return err
	}
	if err := zf.Close(); err != nil {
		return err
	}

	zf2, err := os.Open(zipPath)
	if err != nil {
		return err
	}
	defer zf2.Close()
	relDir := fmt.Sprintf("%s/%s", project.WorkspaceID, project.ID)
	uri, checksum, size, err := w.media.Put(relDir, "project-package-"+job.ID+".zip", zf2)
	if err != nil {
		return err
	}
	_ = os.Remove(zipPath)
	if _, err := w.store.CreateAsset(ctx, store.Asset{
		WorkspaceID: project.WorkspaceID, ProjectID: project.ID,
		Kind: "document", Role: "project_package", StorageURI: uri,
		MimeType: "application/zip", SizeBytes: size, Checksum: checksum,
	}); err != nil {
		return err
	}
	_ = w.store.EmitDomainEvent(ctx, project.WorkspaceID, "project", project.ID, "project.packaged", "{}")
	return w.store.UpdateJobState(ctx, job.ID, "succeeded", "persist", "info", fmt.Sprintf("Package written (%d media files)", len(bundle.Assets)), 10000)
}

func writeZipFile(zw *zip.Writer, name string, data []byte) error {
	f, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return err
}

// processBackup writes a coordinated DB + media-manifest backup (spec §20).
func (w *Worker) processBackup(ctx context.Context, job *store.Job) error {
	_ = w.store.UpdateJobState(ctx, job.ID, "running", "collect", "info", "Collecting media manifest", 2000)
	assets, err := w.store.ListAllAssets(ctx)
	if err != nil {
		return fmt.Errorf("worker: list assets: %w", err)
	}
	entries := make([]backup.MediaEntry, 0, len(assets))
	for _, a := range assets {
		entries = append(entries, backup.MediaEntry{URI: a.StorageURI, SHA256: a.Checksum, SizeBytes: a.SizeBytes})
	}
	_ = w.store.UpdateJobState(ctx, job.ID, "running", "copy", "info", "Copying database and writing manifest", 5000)
	dir, err := backup.Create(ctx, w.store.DB.DB, entries, w.backupsDir)
	if err != nil {
		return fmt.Errorf("worker: backup: %w", err)
	}
	_ = w.store.EmitDomainEvent(ctx, job.WorkspaceID, "database", "all", "backup.completed", "{}")
	return w.store.UpdateJobState(ctx, job.ID, "succeeded", "persist", "info", fmt.Sprintf("Backup written to %s (%d media referenced)", filepath.Base(dir), len(entries)), 10000)
}
