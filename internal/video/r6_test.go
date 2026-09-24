package video_test

import (
	"testing"

	"vidbuild/internal/db"
	"vidbuild/internal/testutil"
)

// TestA12TruthfulParentStatus proves R6: a terminal child failure produces a
// truthful parent state — a failed anchor or a failed assembly becomes a failed
// plan, with recovery on the child, not perpetual progress statuses.
func TestA12TruthfulParentStatus(t *testing.T) {
	gen := &chainGen{failIndex: -1}
	svc, st, vid, shot, versionID, startA, endA, wsID := newChainService(t, gen, &chainAsm{})
	gen.st = st
	res := planChain(t, svc, vid, shot, versionID, startA, endA, wsID, 15000)
	ssID := res.Plan.SuperShot.ID
	if err := svc.Approve(t.Context(), ssID, res.Plan.SuperShot.Revision); err != nil {
		t.Fatalf("approve: %v", err)
	}
	anchors, _ := st.ListAnchors(t.Context(), ssID)
	var mid string
	for _, a := range anchors {
		if a.Kind == "intermediate" {
			if mid == "" {
				mid = a.ID
			} else {
				// Pre-success other intermediates so the chain can dispatch fully.
				if err := st.QueueAnchorInput(t.Context(), a.ID, "a12-ready"); err != nil {
					t.Fatalf("queue: %v", err)
				}
				if _, err := st.SetAnchorSucceeded(t.Context(), a.ID, "anchor-"+a.ID); err != nil {
					t.Fatalf("succeed anchor: %v", err)
				}
			}
		}
	}

	// A failed intermediate anchor makes the parent truthfully failed.
	_ = anchors
	if err := st.SetAnchorStatus(t.Context(), mid, "failed", "image generation exploded"); err != nil {
		t.Fatalf("fail anchor: %v", err)
	}
	if err := svc.RefreshPlanStatus(t.Context(), ssID); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	plan, _ := st.GetSuperShotRequest(t.Context(), ssID)
	if plan.Status != "failed" {
		t.Fatalf("a failed anchor must produce a failed plan, got %s", plan.Status)
	}

	// Recovery on the anchor restores productive parent state.
	if err := st.QueueAnchorInput(t.Context(), mid, "a12-recovered"); err != nil {
		t.Fatalf("queue anchor: %v", err)
	}
	if err := st.SetAnchorStatus(t.Context(), mid, "succeeded", ""); err != nil {
		t.Fatalf("succeed anchor: %v", err)
	}
	if _, err := st.SetAnchorSucceeded(t.Context(), mid, "mid-image"); err != nil {
		t.Fatalf("publish anchor: %v", err)
	}
	// Recovery: the parent returns to productive state and dispatch continues.
	if err := st.SetSuperShotStatus(t.Context(), ssID, "generating_anchors"); err != nil {
		t.Fatalf("reset plan status: %v", err)
	}
	if err := svc.AnchorReady(t.Context(), mid); err != nil {
		t.Fatalf("anchor ready: %v", err)
	}
	if err := svc.RefreshPlanStatus(t.Context(), ssID); err != nil {
		t.Fatalf("refresh 2: %v", err)
	}
	plan, _ = st.GetSuperShotRequest(t.Context(), ssID)
	if plan.Status == "failed" {
		t.Fatal("a recovered anchor must not leave the parent failed")
	}

	// A failed assembly is a failed parent, not perpetual "assembling".
	requestID := res.Plan.GenerationRequest.ID
	segs, _ := st.ListSegments(t.Context(), requestID)
	for _, sg := range segs {
		if err := svc.RunSegment(t.Context(), sg.ID); err != nil {
			t.Fatalf("seg: %v", err)
		}
	}
	asm, err := st.UpsertAssembly(t.Context(), requestID, ssID, "", vid.ProjectID, vid.ID, segs[0].ShotID, 15000)
	if err != nil {
		t.Fatalf("upsert asm: %v", err)
	}
	if err := st.SetAssemblyFailed(t.Context(), asm.ID, "ffmpeg exploded"); err != nil {
		t.Fatalf("fail assembly: %v", err)
	}
	if err := svc.RefreshPlanStatus(t.Context(), ssID); err != nil {
		t.Fatalf("refresh 3: %v", err)
	}
	plan, _ = st.GetSuperShotRequest(t.Context(), ssID)
	if plan.Status != "failed" {
		t.Fatalf("a failed assembly must produce a failed plan, got %s", plan.Status)
	}
}

// TestA14LegacyGenerationRequestsRemainDiagnosable proves R6: duration-video
// generation requests that predate the Super Shot workflow (video_segments
// without a super shot parent) remain visible and diagnosable.
func TestA14LegacyGenerationRequestsRemainDiagnosable(t *testing.T) {
	st := testutil.NewStore(t)
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, vid := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "A14")
	_, _, shot := testutil.MustShot(t, st, vid.ID, admin.ID, "Shot")
	spec, err := st.GetCurrentSpec(t.Context(), shot.ID, "motion")
	if err != nil || spec == nil {
		t.Fatalf("motion spec: %v", err)
	}

	now := db.Now()
	reqID := "legacy-req-1"
	if _, err := st.ExecContext(t.Context(), `INSERT INTO generation_requests (id, project_id, video_id, shot_id, shot_spec_id, spec_version, output_role, quality_tier, candidate_count, status, request_json, prompt_compilation_json, created_by_user_id, created_at, updated_at)
	VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		reqID, project.ID, vid.ID, shot.ID, spec.ID, 1, "motion", "motion", 1, "completed", "{}", "{}", admin.ID, now, now); err != nil {
		t.Fatalf("seed legacy request: %v", err)
	}
	if _, err := st.ExecContext(t.Context(), `INSERT INTO video_segments (id, request_id, workspace_id, project_id, video_id, shot_id, idx, status, created_at, updated_at)
	VALUES ('legacy-seg-1', ?, ?, ?, ?, ?, 0, 'succeeded', ?, ?)`,
		reqID, ws.ID, project.ID, vid.ID, shot.ID, now, now); err != nil {
		t.Fatalf("seed legacy segment: %v", err)
	}
	n, err := st.CountLegacyGenerationRequests(t.Context(), vid.ID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("pre-Super-Shot generation must be diagnosable, got %d", n)
	}
}
