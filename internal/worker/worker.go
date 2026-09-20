// Package worker is the durable local job worker (spec §19). It claims jobs,
// invokes adapters, imports verified outputs and records evidence.
package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"time"

	"vidbuild/internal/comfyui"
	"vidbuild/internal/config"
	"vidbuild/internal/media"
	"vidbuild/internal/store"
	"vidbuild/internal/workflows"
)

// Worker runs generation jobs.
type Worker struct {
	cfg   *config.Config
	store *store.Store
	media *media.Store
	comfy *comfyui.Client
	log   *slog.Logger
	owner string
}

// New constructs a worker.
func New(cfg *config.Config, st *store.Store, ms *media.Store, log *slog.Logger) *Worker {
	return &Worker{
		cfg: cfg, store: st, media: ms, comfy: comfyui.New(cfg.ComfyUI), log: log,
		owner: fmt.Sprintf("local-%d", time.Now().UnixNano()),
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
}

func (w *Worker) process(ctx context.Context, job *store.Job) error {
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
	return w.store.UpdateJobState(ctx, job.ID, "succeeded", "persist", "info", fmt.Sprintf("%d candidate(s) produced", succeeded), 10000)
}

func (w *Worker) runAttempt(ctx context.Context, job *store.Job, req *store.GenerationRequest, version *store.WorkflowVersion, bindings []workflows.Binding, outputNode string, target *store.ExecutionTarget, a store.GenerationAttempt, seed int64, payload requestPayload) error {
	started := time.Now()
	if err := w.store.MarkAttemptRunning(ctx, a.ID, target.ID, payload.Checkpoint, seed); err != nil {
		return err
	}
	_ = w.store.UpdateJobState(ctx, job.ID, "running", "compile", "info", "Compiling bound graph", 2000)

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
	assetID, err := w.store.CreateAsset(ctx, store.Asset{
		WorkspaceID: job.WorkspaceID, ProjectID: req.ProjectID,
		GenerationAttemptID: sql.NullString{String: a.ID, Valid: true}, Kind: "image", Role: req.OutputRole,
		StorageURI: uri, MimeType: "image/png", SizeBytes: size, Checksum: checksum,
	})
	if err != nil {
		return fmt.Errorf("record asset: %w", err)
	}
	_ = assetID

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
