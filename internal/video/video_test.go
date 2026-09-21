package video_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"vidbuild/internal/store"
	"vidbuild/internal/testutil"
	"vidbuild/internal/video"
)

type fakeGenerator struct {
	failIndex int
	calls     map[int]int
}

func newFakeGenerator(failIndex int) *fakeGenerator {
	return &fakeGenerator{failIndex: failIndex, calls: map[int]int{}}
}

func (f *fakeGenerator) GenerateSegment(_ context.Context, seg store.VideoSegment) (string, error) {
	f.calls[seg.Index]++
	if seg.Index == f.failIndex {
		return "", fmt.Errorf("simulated segment failure")
	}
	return fmt.Sprintf("asset-%d", seg.Index), nil
}

type fakeAssembler struct {
	got []store.VideoSegment
}

func (f *fakeAssembler) Assemble(_ context.Context, _ store.VideoAssembly, segs []store.VideoSegment) (string, error) {
	f.got = append([]store.VideoSegment(nil), segs...)
	return "assembled-video", nil
}

func TestPlanDurations(t *testing.T) {
	for duration, want := range map[int]int{5: 1, 15: 3, 30: 6} {
		got, err := video.Plan(duration)
		if err != nil {
			t.Fatalf("Plan(%d): %v", duration, err)
		}
		if got != want {
			t.Fatalf("Plan(%d) = %d segments, want %d", duration, got, want)
		}
	}
	for _, bad := range []int{0, 1, 10, 20, 60, -5} {
		if _, err := video.Plan(bad); err == nil {
			t.Fatalf("Plan(%d) should be rejected (arbitrary durations unsupported)", bad)
		}
	}
}

func TestFrames(t *testing.T) {
	for fps, want := range map[float64]int{8: 40, 12: 60, 16: 80, 0: 40, 7: 36} {
		if got := video.Frames(fps); got != want {
			t.Fatalf("Frames(%v) = %d, want %d", fps, got, want)
		}
	}
}

func TestSegmentPromptsAreDistinct(t *testing.T) {
	for _, total := range []int{3, 6} {
		seen := map[string]bool{}
		for i := 0; i < total; i++ {
			p := video.SegmentPrompt("she shapes the clay", i, total)
			if seen[p] {
				t.Fatalf("segment %d/%d prompt is not distinct: %q", i, total, p)
			}
			seen[p] = true
		}
	}
	if got := video.SegmentPrompt("only", 0, 1); got != "only" {
		t.Fatalf("single-segment prompt should be the base prompt, got %q", got)
	}
}

func newService(t *testing.T, gen video.Generator, asm video.Assembler) (*video.Service, *store.Store, *store.Shot, string, string, string) {
	t.Helper()
	st := testutil.NewStore(t)
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, v := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Duration")
	_, _, shot := testutil.MustShot(t, st, v.ID, admin.ID, "Shot")
	return &video.Service{Store: st, Generate: gen, Assemble: asm}, st, shot, v.ID, ws.ID, project.ID
}

func plan(t *testing.T, svc *video.Service, wsID, projectID string, shot *store.Shot, videoID string, duration int) (string, []store.VideoSegment) {
	t.Helper()
	spec, err := svc.Store.GetCurrentSpec(t.Context(), shot.ID, "motion")
	if err != nil {
		t.Fatalf("motion spec: %v", err)
	}
	admin := testutil.MustAdmin(t, svc.Store)
	requestID, segs, err := svc.Plan(t.Context(), video.PlanInput{
		WorkspaceID:     wsID,
		ProjectID:       projectID,
		VideoID:         videoID,
		ShotID:          shot.ID,
		ShotSpecID:      spec.ID,
		SpecVersion:     spec.Version,
		DurationSeconds: duration,
		Positive:        "she shapes the clay",
		Negative:        "lowres",
		FPS:             8,
		Width:           512,
		Height:          320,
		StartAssetID:    "start-asset",
		EndAssetID:      "end-asset",
	}, admin.ID)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return requestID, segs
}

func TestPlanCreatesSegmentsWithEndpointReuse(t *testing.T) {
	for duration, want := range map[int]int{5: 1, 15: 3, 30: 6} {
		svc, _, shot, videoID, wsID, projectID := newService(t, newFakeGenerator(-1), &fakeAssembler{})
		requestID, segs := plan(t, svc, wsID, projectID, shot, videoID, duration)
		if len(segs) != want {
			t.Fatalf("duration %d produced %d segments, want %d", duration, len(segs), want)
		}
		for _, seg := range segs {
			if !seg.StartAssetID.Valid || seg.StartAssetID.String != "start-asset" ||
				!seg.EndAssetID.Valid || seg.EndAssetID.String != "end-asset" {
				t.Fatalf("segment %d did not reuse the approved endpoints: %+v", seg.Index, seg)
			}
			if seg.FrameCount != 40 || seg.FPS != 8 {
				t.Fatalf("segment %d frames/fps = %d/%v, want 40/8", seg.Index, seg.FrameCount, seg.FPS)
			}
		}
		if requestID == "" {
			t.Fatal("plan returned an empty request id")
		}
	}
}

func TestSegmentFailureAndRetryIsolation(t *testing.T) {
	gen := newFakeGenerator(1) // segment index 1 fails
	svc, st, shot, videoID, wsID, projectID := newService(t, gen, &fakeAssembler{})
	requestID, segs := plan(t, svc, wsID, projectID, shot, videoID, 15)
	if len(segs) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(segs))
	}

	for _, seg := range segs {
		_ = svc.RunSegment(t.Context(), seg.ID)
	}

	byIndex := map[int]store.VideoSegment{}
	loaded, _ := st.ListSegments(t.Context(), requestID)
	for _, s := range loaded {
		byIndex[s.Index] = s
	}
	if byIndex[0].Status != "succeeded" || byIndex[2].Status != "succeeded" {
		t.Fatalf("segments 0 and 2 should have succeeded: %+v", byIndex)
	}
	if byIndex[1].Status != "failed" {
		t.Fatalf("segment 1 should have failed: %+v", byIndex[1])
	}
	// A failed segment must not trigger assembly.
	if _, err := st.GetAssembly(t.Context(), requestID); err == nil {
		t.Fatal("assembly should not exist while a segment has failed")
	}

	// Retry only the failed segment.
	if err := svc.RetrySegment(t.Context(), byIndex[1].ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	gen.failIndex = -1
	if err := svc.RunSegment(t.Context(), byIndex[1].ID); err != nil {
		t.Fatalf("retry run: %v", err)
	}

	loaded, _ = st.ListSegments(t.Context(), requestID)
	for _, s := range loaded {
		if s.Status != "succeeded" {
			t.Fatalf("all segments should now succeed: %+v", s)
		}
	}
	if gen.calls[0] != 1 || gen.calls[2] != 1 {
		t.Fatalf("retry must not rerun healthy segments: calls=%v", gen.calls)
	}
	if gen.calls[1] != 2 {
		t.Fatalf("failed segment should have been generated twice: calls=%v", gen.calls)
	}
	// Healthy segment assets are unchanged by the retry.
	if !byIndex[0].AssetID.Valid || byIndex[0].AssetID.String != "asset-0" {
		t.Fatalf("segment 0 asset changed: %+v", byIndex[0])
	}
}

func TestAssemblyLineageAndDuration(t *testing.T) {
	asm := &fakeAssembler{}
	gen := newFakeGenerator(-1)
	svc, st, shot, videoID, wsID, projectID := newService(t, gen, asm)
	requestID, segs := plan(t, svc, wsID, projectID, shot, videoID, 30)
	if len(segs) != 6 {
		t.Fatalf("30s should compile into 6 segments, got %d", len(segs))
	}
	for _, seg := range segs {
		if err := svc.RunSegment(t.Context(), seg.ID); err != nil {
			t.Fatalf("run segment %d: %v", seg.Index, err)
		}
	}

	// Assembly was queued when the last segment succeeded; run it.
	if err := svc.RunAssembly(t.Context(), requestID); err != nil {
		t.Fatalf("assembly: %v", err)
	}
	if len(asm.got) != 6 {
		t.Fatalf("assembler received %d segments, want 6", len(asm.got))
	}
	for i, seg := range asm.got {
		if seg.Index != i {
			t.Fatalf("assembly order wrong at %d: %+v", i, seg)
		}
	}

	got, err := st.GetAssembly(t.Context(), requestID)
	if err != nil {
		t.Fatalf("get assembly: %v", err)
	}
	if got.Status != "succeeded" || !got.AssetID.Valid || got.AssetID.String != "assembled-video" {
		t.Fatalf("assembly not recorded: %+v", got)
	}
	var lineage []string
	if err := json.Unmarshal([]byte(got.SegmentAssetJSON), &lineage); err != nil {
		t.Fatalf("lineage JSON: %v", err)
	}
	want := []string{"asset-0", "asset-1", "asset-2", "asset-3", "asset-4", "asset-5"}
	if len(lineage) != len(want) {
		t.Fatalf("lineage length = %d, want %d", len(lineage), len(want))
	}
	for i := range want {
		if lineage[i] != want[i] {
			t.Fatalf("lineage[%d] = %q, want %q", i, lineage[i], want[i])
		}
	}
}
