package video_test

import (
	"testing"

	"vidbuild/internal/testutil"
)

// TestDuplicateJobSubmission proves C05/T07: the same immutable work key returns
// the same job rather than creating a duplicate.
func TestDuplicateJobSubmission(t *testing.T) {
	st := testutil.NewStore(t)
	ws := testutil.MustWorkspace(t, st)
	j1, err := st.EnqueueJob(t.Context(), ws.ID, "generate_segment", "video_segment", "agg-1", "segment:fixed-attempt")
	if err != nil {
		t.Fatalf("enqueue 1: %v", err)
	}
	j2, err := st.EnqueueJob(t.Context(), ws.ID, "generate_segment", "video_segment", "agg-1", "segment:fixed-attempt")
	if err != nil {
		t.Fatalf("enqueue 2: %v", err)
	}
	if j1.ID != j2.ID {
		t.Fatalf("same key must return the same job: %s vs %s", j1.ID, j2.ID)
	}
	jobs, _ := st.ListActiveJobs(t.Context())
	n := 0
	for _, j := range jobs {
		if j.AggregateID == "agg-1" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("identical submissions must produce one job, got %d", n)
	}
}

// TestLateResultCannotOverwrite proves T09/C06: a late success from a superseded
// attempt cannot overwrite the current output.
func TestLateResultCannotOverwrite(t *testing.T) {
	gen := &chainGen{failIndex: -1}
	svc, st, vid, shot, versionID, startA, endA, wsID := newChainService(t, gen, &chainAsm{})
	gen.st = st
	res := planChain(t, svc, vid, shot, versionID, startA, endA, wsID, 15000)
	ssID := res.Plan.SuperShot.ID
	if err := svc.Approve(t.Context(), ssID, res.Plan.SuperShot.Revision); err != nil {
		t.Fatalf("approve: %v", err)
	}
	anchors, _ := st.ListAnchors(t.Context(), ssID)
	for i := range anchors {
		if anchors[i].Kind != "intermediate" {
			continue
		}
		_, _ = st.SetAnchorSucceeded(t.Context(), anchors[i].ID, "asset-"+anchors[i].ID)
		_ = svc.AnchorReady(t.Context(), anchors[i].ID)
	}
	segs, _ := st.ListSegmentsForSuperShot(t.Context(), ssID)
	if err := svc.RunSegment(t.Context(), segs[0].ID); err != nil {
		t.Fatalf("run: %v", err)
	}
	seg, _ := st.GetSegment(t.Context(), segs[0].ID)
	oldAttempt := seg.AttemptID.String
	currentAsset := seg.AssetID.String

	// A retry selects a new attempt; a late result from the old attempt is rejected.
	if _, err := st.NewSegmentAttempt(t.Context(), seg.ID, seg.RequestID, ""); err != nil {
		t.Fatalf("new attempt: %v", err)
	}
	rows, err := st.SetSegmentSucceededIfCurrent(t.Context(), seg.ID, oldAttempt, "late-asset", "", "")
	if err != nil {
		t.Fatalf("late success: %v", err)
	}
	if rows != 0 {
		t.Fatal("a late result from a superseded attempt must not be accepted")
	}
	reloaded, _ := st.GetSegment(t.Context(), seg.ID)
	if reloaded.AssetID.String == "late-asset" {
		t.Fatal("the late asset must not become current")
	}
	if reloaded.AssetID.String != currentAsset {
		t.Fatal("the current output must be preserved")
	}
}

// TestCancelledPlanBlocksExecution proves C07/T11: cancelling a plan prevents
// further segment execution and dispatch.
func TestCancelledPlanBlocksExecution(t *testing.T) {
	gen := &chainGen{failIndex: -1}
	svc, st, vid, shot, versionID, startA, endA, wsID := newChainService(t, gen, &chainAsm{})
	gen.st = st
	res := planChain(t, svc, vid, shot, versionID, startA, endA, wsID, 15000)
	ssID := res.Plan.SuperShot.ID
	if err := svc.Approve(t.Context(), ssID, res.Plan.SuperShot.Revision); err != nil {
		t.Fatalf("approve: %v", err)
	}
	segments, _ := st.ListSegmentsForSuperShot(t.Context(), ssID)
	if err := st.SetSuperShotStatus(t.Context(), ssID, "cancelled"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if err := svc.RunSegment(t.Context(), segments[0].ID); err == nil {
		t.Fatal("a cancelled plan must not run a segment")
	}
}