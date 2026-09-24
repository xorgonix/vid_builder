package video_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"vidbuild/internal/store"
	"vidbuild/internal/testutil"
	"vidbuild/internal/video"
)

type chainGen struct {
	st        *store.Store
	calls     map[int]int
	failIndex int
}

func (g *chainGen) GenerateSegment(ctx context.Context, seg store.VideoSegment) (video.SegmentResult, error) {
	if g.calls == nil {
		g.calls = map[int]int{}
	}
	g.calls[seg.Index]++
	if seg.Index == g.failIndex {
		return video.SegmentResult{}, fmt.Errorf("simulated segment failure")
	}
	start, err := g.st.GetAnchor(ctx, seg.StartAnchorID.String)
	if err != nil {
		return video.SegmentResult{}, err
	}
	end, err := g.st.GetAnchor(ctx, seg.EndAnchorID.String)
	if err != nil {
		return video.SegmentResult{}, err
	}
	return video.SegmentResult{
		AssetID:      fmt.Sprintf("seg-asset-%d", seg.Index),
		StartAssetID: start.AssetID.String,
		EndAssetID:   end.AssetID.String,
	}, nil
}

type chainAsm struct{ got []store.VideoSegment }

func (a *chainAsm) Assemble(_ context.Context, _ store.VideoAssembly, segs []store.VideoSegment) (video.AssemblyResult, error) {
	a.got = segs
	total := 0
	for _, s := range segs {
		total += s.DurationMS
	}
	return video.AssemblyResult{AssetID: "assembled-video", ActualDurationMS: total, JoinsJSON: "[]", Receipt: "test"}, nil
}

const testManifest = `{
  "name": "Test Video",
  "slug": "test-video",
  "task": "video",
  "capabilities": {
    "video_timing": {
      "minimum_duration_ms": 2000,
      "maximum_direct_duration_ms": 5000,
      "preferred_segment_duration_ms": 5000,
      "minimum_frames": 17,
      "maximum_frames": 121,
      "frame_rule": { "type": "modulo", "modulus": 4, "remainder": 1 },
      "supported_fps": [ { "num": 8, "den": 1 } ]
    },
    "inputs": { "start_image": true, "end_image": true },
    "continuation": { "mode": "anchor_only" },
    "verified": false
  }
}`

func newChainService(t *testing.T, gen video.Generator, asm video.Assembler) (*video.Service, *store.Store, *store.Video, *store.Shot, string, string, string, string) {
	t.Helper()
	st := testutil.NewStore(t)
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, vid := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Super")
	_, _, shot := testutil.MustShot(t, st, vid.ID, admin.ID, "Shot")

	bindings := []store.WorkflowBinding{
		{SemanticField: "first_frame_asset", NodeID: "6", InputName: "image", ValueType: "string", Required: true},
		{SemanticField: "last_frame_asset", NodeID: "7", InputName: "image", ValueType: "string", Required: true},
		{SemanticField: "first_frame_positive", NodeID: "4", InputName: "text", ValueType: "string", Required: true},
		{SemanticField: "output_node", NodeID: "12", InputName: "", ValueType: "node", Required: true},
	}
	if err := st.SeedBundledWorkflow(t.Context(), "Test Video", "test-video", "video", "test", "{}", "sha", testManifest, "0.35.0", bindings); err != nil {
		t.Fatalf("seed workflow: %v", err)
	}
	pkgs, _ := st.ListWorkflowPackages(t.Context())
	versionID := ""
	for _, p := range pkgs {
		if p.Slug == "test-video" && p.LatestVersion.Valid {
			versionID = p.LatestVersion.String
		}
	}
	if versionID == "" {
		t.Fatal("no test workflow version")
	}
	startA, _ := st.CreateAsset(t.Context(), store.Asset{WorkspaceID: ws.ID, ProjectID: project.ID, Kind: "image", Role: "start_frame", StorageURI: "start.png", MimeType: "image/png", Checksum: "ca"})
	endA, _ := st.CreateAsset(t.Context(), store.Asset{WorkspaceID: ws.ID, ProjectID: project.ID, Kind: "image", Role: "end_frame", StorageURI: "end.png", MimeType: "image/png", Checksum: "cd"})
	return &video.Service{Store: st, Generate: gen, Assemble: asm}, st, vid, shot, versionID, startA, endA, ws.ID
}

func planChain(t *testing.T, svc *video.Service, vid *store.Video, shot *store.Shot, versionID, startA, endA, wsID string, durationMS int) *video.PlanResult {
	t.Helper()
	admin := testutil.MustAdmin(t, svc.Store)
	spec, err := svc.Store.GetCurrentSpec(t.Context(), shot.ID, "motion")
	if err != nil || spec == nil {
		t.Fatalf("motion spec: %v", err)
	}
	res, err := svc.PlanDraft(t.Context(), video.PlanRequest{
		WorkspaceID: wsID, ProjectID: vid.ProjectID, VideoID: vid.ID, ShotID: shot.ID,
		ShotSpecID: spec.ID, SpecVersion: spec.Version,
		WorkflowVersionID: versionID, RequestedDurationMS: durationMS, FPSNum: 8, FPSDen: 1, Width: 512, Height: 320,
		BasePositive: "person walks to a chair and sits", StartAssetID: startA, EndAssetID: endA,
		IntermediateStates: []string{"turning toward the seat", "beginning to lower"},
	}, admin.ID)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return res
}

// TestPlanChainUsesAdjacentAnchors replaces the old endpoint-reuse test: a
// 15-second request must produce 4 anchors and 3 adjacent segments, never three
// A→D repeats (spec §10.2).
func TestPlanChainUsesAdjacentAnchors(t *testing.T) {
	gen := &chainGen{failIndex: -1}
	svc, st, vid, shot, versionID, startA, endA, wsID := newChainService(t, gen, &chainAsm{})
	gen.st = st
	res := planChain(t, svc, vid, shot, versionID, startA, endA, wsID, 15000)

	if res.Chain.Strategy != video.StrategyChained {
		t.Fatalf("15s should be chained, got %s", res.Chain.Strategy)
	}
	if len(res.Plan.Anchors) != 4 || len(res.Plan.Segments) != 3 {
		t.Fatalf("got %d anchors / %d segments, want 4 / 3", len(res.Plan.Anchors), len(res.Plan.Segments))
	}
	anchors := res.Plan.Anchors
	segments, _ := st.ListSegmentsForSuperShot(t.Context(), res.Plan.SuperShot.ID)
	if len(segments) != 3 {
		t.Fatalf("persisted %d segments, want 3", len(segments))
	}
	for i, seg := range segments {
		if seg.StartAnchorID.String != anchors[i].ID || seg.EndAnchorID.String != anchors[i+1].ID {
			t.Fatalf("segment %d does not reference adjacent anchors", i)
		}
		if seg.StartAssetID.Valid && seg.EndAssetID.Valid && seg.StartAssetID.String == startA && seg.EndAssetID.String == endA {
			t.Fatalf("segment %d repeats the outer endpoints", i)
		}
	}
	// The supplied endpoints carry their assets; intermediates do not yet.
	if !anchors[0].AssetID.Valid || anchors[0].AssetID.String != startA {
		t.Fatal("anchor 0 must reference the supplied start")
	}
	if !anchors[3].AssetID.Valid || anchors[3].AssetID.String != endA {
		t.Fatal("anchor 3 must reference the supplied end")
	}
}

// TestChainGenerateApproveRetryAssembly walks approval, anchor readiness,
// adjacent-anchor generation, isolated retry and assembly.
func TestChainGenerateApproveRetryAssembly(t *testing.T) {
	gen := &chainGen{failIndex: 1}
	asm := &chainAsm{}
	svc, st, vid, shot, versionID, startA, endA, wsID := newChainService(t, gen, asm)
	gen.st = st
	res := planChain(t, svc, vid, shot, versionID, startA, endA, wsID, 15000)
	ssID := res.Plan.SuperShot.ID
	requestID := res.Plan.GenerationRequest.ID

	// Approve queues only the missing intermediate anchors, not the segments.
	if err := svc.Approve(t.Context(), ssID, res.Plan.SuperShot.Revision); err != nil {
		t.Fatalf("approve: %v", err)
	}
	anchors, _ := st.ListAnchors(t.Context(), ssID)
	for _, a := range anchors {
		if a.Kind == "intermediate" && a.Status != "queued" {
			t.Fatalf("intermediate anchor %d should be queued after approval, got %s", a.Index, a.Status)
		}
	}

	// Simulate anchor generation, then readiness enqueues adjacent segments.
	for i := range anchors {
		if anchors[i].Kind != "intermediate" {
			continue
		}
		if _, err := st.SetAnchorSucceeded(t.Context(), anchors[i].ID, fmt.Sprintf("anchor-asset-%d", i)); err != nil {
			t.Fatalf("anchor success: %v", err)
		}
		if err := svc.AnchorReady(t.Context(), anchors[i].ID); err != nil {
			t.Fatalf("anchor ready: %v", err)
		}
	}

	segments, _ := st.ListSegmentsForSuperShot(t.Context(), ssID)
	for _, seg := range segments {
		_ = svc.RunSegment(t.Context(), seg.ID)
	}
	loaded, _ := st.ListSegments(t.Context(), requestID)
	byIndex := map[int]store.VideoSegment{}
	for _, s := range loaded {
		byIndex[s.Index] = s
	}
	if byIndex[0].Status != "succeeded" || byIndex[2].Status != "succeeded" {
		t.Fatalf("segments 0 and 2 should succeed: %+v", byIndex)
	}
	if byIndex[1].Status != "failed" {
		t.Fatalf("segment 1 should have failed: %+v", byIndex[1])
	}
	if ready, _ := st.AllSegmentsReady(t.Context(), requestID); ready {
		t.Fatal("assembly must not be ready while a segment has failed")
	}
	if _, err := st.GetAssembly(t.Context(), requestID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("no assembly should exist yet, got %v", err)
	}

	// Retry only the failed segment.
	gen.failIndex = -1
	if err := svc.RetrySegment(t.Context(), byIndex[1].ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if err := svc.RunSegment(t.Context(), byIndex[1].ID); err != nil {
		t.Fatalf("retry run: %v", err)
	}
	if gen.calls[0] != 1 || gen.calls[2] != 1 {
		t.Fatalf("retry must not rerun healthy segments: calls=%v", gen.calls)
	}

	// Boundary assets are shared between adjacent segments.
	loaded, _ = st.ListSegments(t.Context(), requestID)
	byIndex = map[int]store.VideoSegment{}
	for _, s := range loaded {
		byIndex[s.Index] = s
	}
	if !byIndex[0].EndAssetID.Valid || !byIndex[1].StartAssetID.Valid {
		t.Fatal("adjacent segments must both resolve the boundary asset")
	}
	if byIndex[0].EndAssetID.String != byIndex[1].StartAssetID.String {
		t.Fatalf("segment 0 end asset %q != segment 1 start asset %q", byIndex[0].EndAssetID.String, byIndex[1].StartAssetID.String)
	}

	// Assemble the current chain.
	if err := svc.RunAssembly(t.Context(), requestID); err != nil {
		t.Fatalf("assembly: %v", err)
	}
	if len(asm.got) != 3 {
		t.Fatalf("assembler received %d segments, want 3", len(asm.got))
	}
	got, err := st.GetAssembly(t.Context(), requestID)
	if err != nil {
		t.Fatalf("get assembly: %v", err)
	}
	if got.Status != "succeeded" || !got.AssetID.Valid || got.AssetID.String != "assembled-video" {
		t.Fatalf("assembly not recorded: %+v", got)
	}
}

// TestAttemptRetentionAndRetryNewJob proves C04 (the persisted attempt link
// survives execution) and C05 (a retry creates a new attempt/key rather than
// reusing the terminal job).
func TestAttemptRetentionAndRetryNewJob(t *testing.T) {
	gen := &chainGen{failIndex: -1}
	svc, st, vid, shot, versionID, startA, endA, wsID := newChainService(t, gen, &chainAsm{})
	gen.st = st
	res := planChain(t, svc, vid, shot, versionID, startA, endA, wsID, 15000)
	ssID := res.Plan.SuperShot.ID
	requestID := res.Plan.GenerationRequest.ID
	if err := svc.Approve(t.Context(), ssID, res.Plan.SuperShot.Revision); err != nil {
		t.Fatalf("approve: %v", err)
	}
	anchors, _ := st.ListAnchors(t.Context(), ssID)
	for i := range anchors {
		if anchors[i].Kind != "intermediate" {
			continue
		}
		_, _ = st.SetAnchorSucceeded(t.Context(), anchors[i].ID, fmt.Sprintf("anchor-asset-%d", i))
		_ = svc.AnchorReady(t.Context(), anchors[i].ID)
	}
	segs, _ := st.ListSegmentsForSuperShot(t.Context(), ssID)
	for _, seg := range segs {
		if err := svc.RunSegment(t.Context(), seg.ID); err != nil {
			t.Fatalf("run segment %d: %v", seg.Index, err)
		}
	}

	// C04: the attempt link persists across execution.
	reloaded, _ := st.ListSegments(t.Context(), requestID)
	before := reloaded[0].AttemptID
	if !before.Valid {
		t.Fatal("segment must retain a persisted attempt id after execution (C04)")
	}
	jobsBefore := activeJobsFor(t, st, "generate_segment", reloaded[0].ID)

	// C05: retry creates a new attempt and enqueues a NEW job; a reused terminal
	// job would leave the job count unchanged.
	if err := svc.RetrySegment(t.Context(), reloaded[0].ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	after, _ := st.GetSegment(t.Context(), reloaded[0].ID)
	if !after.AttemptID.Valid || after.AttemptID.String == before.String {
		t.Fatalf("retry must create a new attempt id (was %s, now %s)", before.String, after.AttemptID.String)
	}
	if jobsAfter := activeJobsFor(t, st, "generate_segment", reloaded[0].ID); jobsAfter != jobsBefore+1 {
		t.Fatalf("retry must enqueue a new job (was %d, now %d); reusing the terminal job would not change the count", jobsBefore, jobsAfter)
	}
	// The retried attempt renders and keeps its new attempt link.
	if err := svc.RunSegment(t.Context(), after.ID); err != nil {
		t.Fatalf("retry run: %v", err)
	}
	final, _ := st.GetSegment(t.Context(), after.ID)
	if !final.AttemptID.Valid || final.AttemptID.String != after.AttemptID.String {
		t.Fatal("retried segment must keep its new attempt link through execution (C04)")
	}
}

// TestBundledWorkflowUpgrade proves C09: seeding a changed bundled manifest
// inserts a new immutable version rather than being skipped, and re-seeding the
// same manifest is idempotent.
func TestBundledWorkflowUpgrade(t *testing.T) {
	st := testutil.NewStore(t)
	bindings := []store.WorkflowBinding{{SemanticField: "output_node", NodeID: "1", ValueType: "node", Required: true}}
	m1 := `{"name":"W","slug":"upg","task":"video"}`
	m2 := `{"name":"W","slug":"upg","task":"video","capabilities":{"inputs":{"start_image":true,"end_image":true},"continuation":{"mode":"anchor_only"},"verified":false}}`
	seed := func(manifestJSON, sha string) {
		t.Helper()
		if err := st.SeedBundledWorkflow(t.Context(), "W", "upg", "video", "d", "{}", sha, manifestJSON, "0.35.0", bindings); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	latest := func() string {
		t.Helper()
		pkgs, _ := st.ListWorkflowPackages(t.Context())
		for _, p := range pkgs {
			if p.Slug == "upg" && p.LatestVersion.Valid {
				return p.LatestVersion.String
			}
		}
		t.Fatal("no latest version")
		return ""
	}
	seed(m1, "sha-1")
	v1 := latest()
	seed(m2, "sha-2") // upgraded manifest
	v2 := latest()
	if v2 == v1 {
		t.Fatal("a changed bundled manifest must insert a new version (C09)")
	}
	seed(m2, "sha-2") // idempotent
	if v3 := latest(); v3 != v2 {
		t.Fatal("re-seeding the same manifest must be idempotent (C09)")
	}
}

func activeJobsFor(t *testing.T, st *store.Store, jobType, aggregateID string) int {
	t.Helper()
	jobs, err := st.ListActiveJobs(t.Context())
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	n := 0
	for _, j := range jobs {
		if j.JobType == jobType && j.AggregateID == aggregateID {
			n++
		}
	}
	return n
}

// anchor stale-marks only the segments that touch it (spec §7.1).
func TestChangeAnchorInvalidatesAdjacentSegments(t *testing.T) {
	gen := &chainGen{failIndex: -1}
	svc, st, vid, shot, versionID, startA, endA, wsID := newChainService(t, gen, &chainAsm{})
	gen.st = st
	res := planChain(t, svc, vid, shot, versionID, startA, endA, wsID, 15000)
	ssID := res.Plan.SuperShot.ID
	requestID := res.Plan.GenerationRequest.ID
	if err := svc.Approve(t.Context(), ssID, res.Plan.SuperShot.Revision); err != nil {
		t.Fatalf("approve: %v", err)
	}
	anchors, _ := st.ListAnchors(t.Context(), ssID)
	for i := range anchors {
		if anchors[i].Kind != "intermediate" {
			continue
		}
		_, _ = st.SetAnchorSucceeded(t.Context(), anchors[i].ID, fmt.Sprintf("anchor-asset-%d", i))
		_ = svc.AnchorReady(t.Context(), anchors[i].ID)
	}
	segments, _ := st.ListSegmentsForSuperShot(t.Context(), ssID)
	for _, seg := range segments {
		_ = svc.RunSegment(t.Context(), seg.ID)
	}
	// Change the middle intermediate anchor (index 1), which bounds segments 0 and 1.
	if err := svc.ChangeAnchor(t.Context(), anchors[1].ID); err != nil {
		t.Fatalf("change anchor: %v", err)
	}
	loaded, _ := st.ListSegments(t.Context(), requestID)
	stale := map[int]bool{}
	for _, s := range loaded {
		stale[s.Index] = s.Stale
	}
	if !stale[0] || !stale[1] {
		t.Fatalf("segments 0 and 1 should be stale: %v", stale)
	}
	if stale[2] {
		t.Fatalf("segment 2 does not touch anchor 1 and must remain current: %v", stale)
	}
}
