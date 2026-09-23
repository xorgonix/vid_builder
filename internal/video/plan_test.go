package video

import (
	"strings"
	"testing"
)

func timingPlan(t *testing.T, ms int, strategy string) *TimingPlan {
	t.Helper()
	p, err := SolveTiming(TimingRequest{RequestedDurationMS: ms, FPS: RationalFPS{Num: 8, Den: 1}}, videoCap(), strategy)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	return p
}

// TestBuildPlanChain proves an ordered A→B→C→D chain: N+1 anchors, adjacent
// anchor references, and no repeated outer-endpoint segment (spec §10.2).
func TestBuildPlanChain(t *testing.T) {
	timing := timingPlan(t, 15000, "auto")
	if len(timing.Segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(timing.Segments))
	}
	plan, err := BuildPlan(PlanBuildInput{
		Timing: timing, StartAssetID: "A", EndAssetID: "D",
		IntermediateStates: []string{"B", "C"}, ContinuityLocks: []string{"identity"},
		BasePositive: "walk to the chair and sit", ShotIntent: "a person sits down",
	})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if len(plan.Anchors) != 4 || len(plan.Segments) != 3 {
		t.Fatalf("got %d anchors / %d segments, want 4 / 3", len(plan.Anchors), len(plan.Segments))
	}
	kinds := []string{AnchorSuppliedStart, AnchorIntermediate, AnchorIntermediate, AnchorSuppliedEnd}
	for i, a := range plan.Anchors {
		if a.Kind != kinds[i] {
			t.Fatalf("anchor %d kind %s, want %s", i, a.Kind, kinds[i])
		}
	}
	if plan.Anchors[0].AssetID != "A" || plan.Anchors[3].AssetID != "D" {
		t.Fatal("supplied anchors must reference A and D")
	}
	for i, seg := range plan.Segments {
		if seg.StartAnchor != i || seg.EndAnchor != i+1 {
			t.Fatalf("segment %d must reference adjacent anchors %d and %d", i, i, i+1)
		}
		// No intermediate segment may use both outer endpoint assets.
		if plan.Anchors[seg.StartAnchor].AssetID == "A" && plan.Anchors[seg.EndAnchor].AssetID == "D" {
			t.Fatalf("segment %d repeats the outer A→D endpoints", i)
		}
		if !strings.Contains(seg.ActionPrompt, "Overall shot intent") {
			t.Fatalf("segment %d movement prompt should carry the overall intent", i)
		}
	}
	// Adjacent segments share their boundary anchor.
	if plan.Segments[0].EndAnchor != plan.Segments[1].StartAnchor ||
		plan.Segments[1].EndAnchor != plan.Segments[2].StartAnchor {
		t.Fatal("adjacent segments must share their boundary anchor")
	}
}

func TestBuildPlanRequiresEndpoints(t *testing.T) {
	timing := timingPlan(t, 5000, "auto")
	if _, err := BuildPlan(PlanBuildInput{Timing: timing, EndAssetID: "D"}); err == nil {
		t.Fatal("missing start image must be rejected")
	}
	if _, err := BuildPlan(PlanBuildInput{Timing: timing, StartAssetID: "A"}); err == nil {
		t.Fatal("missing end image must be rejected")
	}
}

func TestValidatePlanRejectsInvalid(t *testing.T) {
	plan, err := BuildPlan(PlanBuildInput{
		Timing: timingPlan(t, 15000, "auto"), StartAssetID: "A", EndAssetID: "D",
		IntermediateStates: []string{"B", "C"}, SegmentActions: []string{"a0", "a1", "a2"},
	})
	if err != nil {
		t.Fatalf("base plan: %v", err)
	}
	// Wrong anchor count.
	bad := *plan
	bad.Anchors = plan.Anchors[:2]
	if ValidatePlan(&bad) == nil {
		t.Fatal("wrong anchor count should fail")
	}
	// Non-monotonic times.
	bad = *plan
	bad.Anchors = append([]PlanAnchor(nil), plan.Anchors...)
	bad.Anchors[1].TimeOffsetMS = bad.Anchors[2].TimeOffsetMS
	if ValidatePlan(&bad) == nil {
		t.Fatal("non-monotonic times should fail")
	}
	// Empty movement prompt.
	bad = *plan
	bad.Segments = append([]PlanSegment(nil), plan.Segments...)
	bad.Segments[1].ActionPrompt = ""
	if ValidatePlan(&bad) == nil {
		t.Fatal("empty movement prompt should fail")
	}
	// Duration mismatch.
	bad = *plan
	bad.Segments = append([]PlanSegment(nil), plan.Segments...)
	bad.Segments[0].DurationMS += 1000
	if ValidatePlan(&bad) == nil {
		t.Fatal("duration mismatch should fail")
	}
}

func TestLegacyInvalidPlan(t *testing.T) {
	if !LegacyInvalidPlan([]string{"A", "A", "A"}, []string{"D", "D", "D"}) {
		t.Fatal("all-repeated outer endpoints should be legacy-invalid")
	}
	if LegacyInvalidPlan([]string{"A"}, []string{"D"}) {
		t.Fatal("a single segment is not a legacy-invalid chain")
	}
	if LegacyInvalidPlan([]string{"A", "B"}, []string{"D", "C"}) {
		t.Fatal("distinct pairs are not legacy-invalid")
	}
}

func TestVerifyAssemblyDurationTolerance(t *testing.T) {
	segs := []struct {
		fpsNum, fpsDen, dur int
	}{{8, 1, 5000}}
	_ = segs
	// A 5000ms plan at 8fps tolerates about one frame (125ms) plus rounding.
	if err := VerifyAssemblyDuration(5100, 5000, nil); err != nil {
		t.Fatalf("within tolerance should pass: %v", err)
	}
	if err := VerifyAssemblyDuration(6000, 5000, nil); err == nil {
		t.Fatal("a large duration mismatch must fail")
	}
}
