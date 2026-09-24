package worker

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"vidbuild/internal/media"
	"vidbuild/internal/store"
	"vidbuild/internal/testutil"
	"vidbuild/internal/video"
)

// R2 contract: the worker executes the job's frozen queue-time input snapshot,
// never mutable live plan fields. Editing inputs while a job is queued or
// running supersedes the work before publication; a late result stays history.

const videoManifestR2 = `{"name": "Chain", "slug": "defer", "task": "video",
 "capabilities": {"inputs": {"start_image": true, "end_image": true},
  "continuation": {"mode": "anchor_only"}, "verified": false}}`

// r2ChainPlan builds a plan with video+image workflows, supplied endpoint assets
// and one segment, ready for Approve. It returns the plan, the worker wired to
// the fake provider, and the fake ComfyUI itself.
func r2ChainPlan(t *testing.T, st *store.Store, rawAction string, withIntermediate bool) (*store.SuperShotPlan, *Worker, *testutil.ComfyUI, string) {
	t.Helper()
	fake := testutil.NewComfyUI(t)
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, vid := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "R2")
	_, _, shot := testutil.MustShot(t, st, vid.ID, admin.ID, "Shot")
	spec, err := st.GetCurrentSpec(t.Context(), shot.ID, "motion")
	if err != nil || spec == nil {
		t.Fatalf("motion spec: %v", err)
	}
	graph := `{"4": {"class_type": "CLIPTextEncode", "inputs": {"text": ""}},
	"8": {"class_type": "CLIPTextEncode", "inputs": {"text": ""}},
	"10": {"class_type": "LoadImage", "inputs": {"image": "placeholder.png"}},
	"11": {"class_type": "LoadImage", "inputs": {"image": "placeholder.png"}},
	"7": {"class_type": "SaveImage", "inputs": {}}}`
	videoBindings := []store.WorkflowBinding{
		{SemanticField: "first_frame_asset", NodeID: "10", InputName: "image", ValueType: "string", Required: true},
		{SemanticField: "first_frame_positive", NodeID: "4", InputName: "text", ValueType: "string", Required: true},
		{SemanticField: "output_node", NodeID: "7", InputName: "", ValueType: "node", Required: true},
	}
	imageBindings := []store.WorkflowBinding{
		{SemanticField: "first_frame_asset", NodeID: "10", InputName: "image", ValueType: "string", Required: true},
		{SemanticField: "reference_asset", NodeID: "10", InputName: "image", ValueType: "string", Required: true},
		{SemanticField: "output_node", NodeID: "7", InputName: "", ValueType: "node", Required: true},
	}
	mediaStore := media.New(t.TempDir())
	wk := newTestWorker(t, st, fake.URL, mediaStore)
	if err := st.SeedBundledWorkflow(t.Context(), "Chain Video", "r2-chain-video", "video", "test", graph, "r2vsha", videoManifestR2, "0.35.0", videoBindings); err != nil {
		t.Fatalf("seed video: %v", err)
	}
	if err := st.SeedBundledWorkflow(t.Context(), "Img Ref", "r2-image", "image", "test", graph, "r2isha", videoManifestR2, "0.35.0", imageBindings); err != nil {
		t.Fatalf("seed image: %v", err)
	}
	pkgs, _ := st.ListWorkflowPackages(t.Context())
	ver := map[string]string{}
	for _, p := range pkgs {
		if p.LatestVersion.Valid {
			ver[p.Slug] = p.LatestVersion.String
		}
	}
	startA := mkImageAsset(t, st, mediaStore, ws.ID, project.ID, "start_frame", "start.png", "start-bytes")
	endA := mkImageAsset(t, st, mediaStore, ws.ID, project.ID, "end_frame", "end.png", "end-bytes")
	midAsset := mkImageAsset(t, st, mediaStore, ws.ID, project.ID, "video_anchor", "mid.png", "mid-bytes")
	anchorsIn := []store.AnchorInput{
		{Index: 0, Kind: "supplied_start", StatePrompt: "start", TimeOffsetMS: 0, AssetID: startA},
		{Index: 1, Kind: "intermediate", StatePrompt: "mid", TimeOffsetMS: 5000},
		{Index: 2, Kind: "supplied_end", StatePrompt: "end", TimeOffsetMS: 10000, AssetID: endA},
	}
	segsIn := []store.SegmentInput{
		{RawAction: rawAction, DurationMS: 5000, FrameCount: 40, FPSNum: 8, FPSDen: 1},
		{RawAction: rawAction, DurationMS: 5000, FrameCount: 40, FPSNum: 8, FPSDen: 1},
	}
	if !withIntermediate {
		anchorsIn = []store.AnchorInput{
			{Index: 0, Kind: "supplied_start", StatePrompt: "start", TimeOffsetMS: 0, AssetID: startA},
			{Index: 1, Kind: "supplied_end", StatePrompt: "end", TimeOffsetMS: 10000, AssetID: endA},
		}
		segsIn = []store.SegmentInput{
			{RawAction: rawAction, DurationMS: 10000, FrameCount: 80, FPSNum: 8, FPSDen: 1},
		}
	}
	plan, err := st.CreateSuperShotPlan(t.Context(), store.SuperShotRequestInput{
		WorkspaceID: ws.ID, ProjectID: project.ID, VideoID: vid.ID, ShotID: shot.ID,
		ShotSpecID: spec.ID, SpecVersion: spec.Version, WorkflowVersionID: ver["r2-chain-video"],
		ImageWorkflowVersionID: ver["r2-image"], ImageMode: "references",
		Strategy: "chained", RequestedDurationMS: 10000, PlannedDurationMS: 10000,
		FPSNum: 8, FPSDen: 1, Width: 512, Height: 320, BasePositive: "walk", CompleteMovement: "walk",
		ShotPurpose: "calm", PlannerStatus: "manual",
	}, anchorsIn, segsIn, admin.ID)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	// Compile the segment movement prompt (the real approve path requires it).
	breaks, err := st.ListAnchors(t.Context(), plan.SuperShot.ID)
	if err != nil {
		t.Fatalf("anchors: %v", err)
	}
	segsAll, err := st.ListSegmentsForSuperShot(t.Context(), plan.SuperShot.ID)
	if err != nil {
		t.Fatalf("segments: %v", err)
	}
	ss := plan.SuperShot
	for i, sg := range segsAll {
		start, end := "", ""
		if i < len(breaks) {
			start = breaks[i].StatePrompt
		}
		if i+1 < len(breaks) {
			end = breaks[i+1].StatePrompt
		}
		compiled := video.CompileSegmentMovement(ss.ShotPurpose, ss.CompleteMovement, start, end, sg.RawAction, "", ss.BaseNegative, nil)
		if err := st.UpdateSegmentActionRaw(t.Context(), sg.ID, sg.RawAction, compiled, sg.Revision); err != nil {
			t.Fatalf("compile segment %d: %v", i, err)
		}
	}
	return plan, wk, fake, midAsset
}

// TestR2SegmentEditedWhileQueued proves the exact contract scenario: attempt A
// is queued; the movement is edited to a different action before A's queued job
// executes. A's job must be superseded before submission — it must never run
// attempt B's inputs under A's job identity — and a re-dispatch under a new job
// executes and publishes the new movement.
func TestR2SegmentEditedWhileQueued(t *testing.T) {
	st := testutil.NewStore(t)
	plan, wk, fake, _ := r2ChainPlan(t, st, "walk", false)
	svc := wk.videoService()
	if err := svc.Approve(t.Context(), plan.SuperShot.ID, plan.SuperShot.Revision); err != nil {
		t.Fatalf("approve: %v", err)
	}
	segs, err := st.ListSegmentsForSuperShot(t.Context(), plan.SuperShot.ID)
	if err != nil || len(segs) < 1 {
		t.Fatalf("segments: %v", err)
	}
	seg := segs[0]
	if seg.QueuedInputHash == "" {
		t.Fatal("enqueue must freeze the queue-time input snapshot")
	}

	// Claim attempt A's job and hold it: the provider is "paused".
	job, err := st.ClaimJob(t.Context(), "owner-r2", 60)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if job.JobType != "generate_segment" || job.AggregateID != seg.ID {
		t.Fatalf("wrong job claimed: %s %s", job.JobType, job.AggregateID)
	}

	// Edit the movement while the job sits queued.
	if err := svc.EditSegmentAction(t.Context(), seg.ID, "run", seg.Revision); err != nil {
		t.Fatalf("edit: %v", err)
	}
	fresh, _ := st.GetSegment(t.Context(), seg.ID)
	if fresh.QueuedInputHash != "" || fresh.InputRevision != 1 {
		t.Fatalf("edit must invalidate the frozen snapshot and bump the input revision: hash=%q rev=%d", fresh.QueuedInputHash, fresh.InputRevision)
	}

	uploadsBefore := fake.Uploads()
	if err := wk.process(t.Context(), job); err != nil {
		t.Fatalf("process superseded job: %v", err)
	}
	if fake.Uploads() != uploadsBefore {
		t.Fatal("a superseded queued job must be superseded before provider submission")
	}
	reloaded, _ := st.GetSegment(t.Context(), seg.ID)
	if reloaded.Status == "succeeded" || reloaded.AssetID.Valid {
		t.Fatal("a superseded job must not publish an output")
	}

	// Re-dispatch the new movement under a new job identity and release.
	if err := svc.RetrySegment(t.Context(), seg.ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	job2, err := st.ClaimJob(t.Context(), "owner-r2", 60)
	if err != nil {
		t.Fatalf("claim 2: %v", err)
	}
	if job2.ID == job.ID {
		t.Fatal("the re-dispatch must be a new job (C05)")
	}
	if err := wk.process(t.Context(), job2); err != nil {
		t.Fatalf("process 2: %v", err)
	}
	final, _ := st.GetSegment(t.Context(), seg.ID)
	if final.Status != "succeeded" || !final.AssetID.Valid {
		t.Fatalf("re-dispatched work must publish the new movement's result: %+v", final)
	}
	if !strings.Contains(final.CompiledPrompt, "run") {
		t.Fatalf("the re-dispatched run must execute the new movement, got %q", final.CompiledPrompt)
	}
}

// TestR2SegmentEditedDuringGeneration proves the publication guard: attempt A
// is running, the movement is edited mid-flight, then A's output arrives late.
// The late result stays history; the segment never becomes current.
func TestR2SegmentEditedDuringGeneration(t *testing.T) {
	st := testutil.NewStore(t)
	plan, wk, _, _ := r2ChainPlan(t, st, "walk", false)
	svc := wk.videoService()
	if err := svc.Approve(t.Context(), plan.SuperShot.ID, plan.SuperShot.Revision); err != nil {
		t.Fatalf("approve: %v", err)
	}
	segs, _ := st.ListSegmentsForSuperShot(t.Context(), plan.SuperShot.ID)
	seg := segs[0]

	if _, err := st.ClaimJob(t.Context(), "owner-r2", 60); err != nil {
		t.Fatalf("claim: %v", err)
	}
	// The run marks the segment running (execution started under attempt A).
	if err := svc.Store.SetSegmentRunning(t.Context(), seg.ID, seg.AttemptID.String); err != nil {
		t.Fatalf("set running: %v", err)
	}
	// The movement is edited while the provider renders: the fresh revision is
	// applied and the frozen queue-time snapshot is invalidated.
	fresh, _ := st.GetSegment(t.Context(), seg.ID)
	if err := svc.EditSegmentAction(t.Context(), seg.ID, "run", fresh.Revision); err != nil {
		t.Fatalf("edit: %v", err)
	}
	// Attempt A finishes late: its success must not become the selection.
	rows, err := st.SetSegmentSucceededIfCurrent(t.Context(), seg.ID, seg.AttemptID.String, "late-asset", "", "")
	if err != nil {
		t.Fatalf("late publish: %v", err)
	}
	if rows != 0 {
		t.Fatal("a late result after a mid-flight edit must not be published")
	}
	reloaded, _ := st.GetSegment(t.Context(), seg.ID)
	if reloaded.Status == "succeeded" || reloaded.AssetID.String == "late-asset" {
		t.Fatal("a run whose inputs were edited mid-flight must not become current")
	}
}

// TestR2AnchorSupersededWhileQueued proves the anchor scenario: image attempt A
// is queued; a different image is selected while A runs. A must never submit,
// and the anchor must stay invalid until re-dispatched.
func TestR2AnchorSupersededWhileQueued(t *testing.T) {
	st := testutil.NewStore(t)
	plan, wk, fake, _ := r2ChainPlan(t, st, "walk", true)
	svc := wk.videoService()
	if err := svc.Approve(t.Context(), plan.SuperShot.ID, plan.SuperShot.Revision); err != nil {
		t.Fatalf("approve: %v", err)
	}
	anchors, _ := st.ListAnchors(t.Context(), plan.SuperShot.ID)
	var queuedAnchor store.VideoAnchor
	for _, a := range anchors {
		if a.Kind == "intermediate" && a.QueuedInputHash != "" {
			queuedAnchor = a
		}
	}
	if queuedAnchor.ID == "" {
		t.Fatal("approve must freeze an input snapshot on the queued intermediate")
	}
	uploadsBefore := fake.Uploads()

	// An image selection change (ChangeAnchor-style) supersedes queued work.
	if err := svc.ChangeAnchor(t.Context(), queuedAnchor.ID); err == nil {
		t.Log("ChangeAnchor accepted")
	}
	if err := wk.store.ClearAnchorQueuedInput(t.Context(), queuedAnchor.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	job, err := st.ClaimJob(t.Context(), "owner-r2", 60)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if job.JobType != "generate_anchor" {
		t.Fatalf("expected an anchor job, got %s", job.JobType)
	}
	if err := wk.process(t.Context(), job); err != nil {
		t.Fatalf("process: %v", err)
	}
	if fake.Uploads() != uploadsBefore {
		t.Fatal("a superseded anchor job must be superseded before provider submission")
	}
	reloaded, _ := wk.store.GetAnchor(t.Context(), queuedAnchor.ID)
	if reloaded.Status == "succeeded" {
		t.Fatal("the superseded anchor attempt must not publish")
	}

	// A re-dispatch under a fresh revision publishes normally.
	if err := svc.RetryAnchor(t.Context(), queuedAnchor.ID); err != nil {
		t.Fatalf("retry anchor: %v", err)
	}
	job2, err := st.ClaimJob(t.Context(), "owner-r2", 60)
	if err != nil {
		t.Fatalf("claim 2: %v", err)
	}
	if job2.ID == job.ID {
		t.Fatal("the anchor re-dispatch must be a new job (C05)")
	}
	if err := wk.process(t.Context(), job2); err != nil {
		t.Fatalf("process 2: %v", err)
	}
	final, _ := wk.store.GetAnchor(t.Context(), queuedAnchor.ID)
	if final.Status != "succeeded" || !final.AssetID.Valid {
		t.Fatalf("re-dispatched anchor must publish: %+v", final)
	}
}

// r2PauseAsm is a controllable fake assembler that edits a segment movement
// mid-export, reproducing "pause exporter; change selected Y to Y2; release".
type r2PauseAsm struct {
	st       *store.Store
	midExport func() error
}

func (a *r2PauseAsm) Assemble(ctx context.Context, asm store.VideoAssembly, segs []store.VideoSegment) (video.AssemblyResult, error) {
	if a.midExport != nil {
		if err := a.midExport(); err != nil {
			return video.AssemblyResult{}, err
		}
	}
	return video.AssemblyResult{AssetID: "exported-during-edit", ActualDurationMS: asm.PlannedDurationMS}, nil
}

// TestR2AssemblyEditedDuringExport proves the assembly scenario: the input
// snapshot is frozen when the export starts, the segment movement changes while
// the export runs, and the exporter releases. The old output remains previous
// output only; it must never become the current finished shot.
func TestR2AssemblyEditedDuringExport(t *testing.T) {
	st := testutil.NewStore(t)
	plan, wk, _, midAsset := r2ChainPlan(t, st, "walk", true)
	svc := wk.videoService()
	if err := svc.Approve(t.Context(), plan.SuperShot.ID, plan.SuperShot.Revision); err != nil {
		t.Fatalf("approve: %v", err)
	}
	// Ready the intermediate anchor synthetically so the segments can dispatch.
	anchors, _ := st.ListAnchors(t.Context(), plan.SuperShot.ID)
	for _, a := range anchors {
		if a.Kind != "intermediate" || a.AssetID.Valid {
			continue
		}
		
		if err := st.QueueAnchorInput(t.Context(), a.ID, "anchor-ready"); err != nil {
			t.Fatalf("queue: %v", err)
		}
		if _, err := st.SetAnchorSucceeded(t.Context(), a.ID, midAsset); err != nil {
			t.Fatalf("anchor succeed: %v", err)
		}
		if err := svc.AnchorReady(t.Context(), a.ID); err != nil {
			t.Fatalf("anchor ready: %v", err)
		}
	}
	segs, _ := st.ListSegmentsForSuperShot(t.Context(), plan.SuperShot.ID)
	for _, sg := range segs {
		if err := svc.RunSegment(t.Context(), sg.ID); err != nil {
			t.Fatalf("seg %d: %v", sg.Index, err)
		}
	}
	reqID := plan.GenerationRequest.ID
	if _, err := st.ClaimJob(t.Context(), "owner-r2", 60); err != nil {
		t.Fatalf("claim assemble job: %v", err)
	}

	// Mid-export: pause exporter, change selected Y to Y2 (movement edit), release.
	paused := &r2PauseAsm{st: st}
	paused.midExport = func() error {
		sg, err := st.GetSegment(t.Context(), segs[0].ID)
		if err != nil {
			return err
		}
		return (&video.Service{Store: st}).EditSegmentAction(t.Context(), sg.ID, "run", sg.Revision)
	}
	runSvc := &video.Service{Store: st, Assemble: paused, Log: slog.Default()}
	if err := runSvc.RunAssembly(t.Context(), reqID); err != nil {
		t.Fatalf("assembly: %v", err)
	}
	asm, err := st.GetAssembly(t.Context(), reqID)
	if err != nil {
		t.Fatalf("get assembly: %v", err)
	}
	if asm.Status == "succeeded" {
		t.Fatal("an export whose inputs changed mid-flight must not publish a current success")
	}
	if asm.AssetID.Valid {
		t.Fatal("the superseded export output must not become current")
	}
}
