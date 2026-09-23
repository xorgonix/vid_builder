package video

import (
	"testing"

	"vidbuild/internal/workflows"
)

func videoCap() *workflows.VideoTiming {
	return &workflows.VideoTiming{
		MinimumDurationMS: 2000, MaximumDirectDurationMS: 5000, PreferredSegmentDurationMS: 5000,
		MinimumFrames: 17, MaximumFrames: 121,
		FrameRule:    workflows.FrameRule{Type: "modulo", Modulus: 4, Remainder: 1},
		SupportedFPS: []workflows.FPS{{Num: 8, Den: 1}, {Num: 16, Den: 1}},
	}
}

// TestSolveTimingStrategies covers the strategy/segment table (spec §10.1).
func TestSolveTimingStrategies(t *testing.T) {
	fps := RationalFPS{Num: 8, Den: 1}
	cases := []struct {
		ms       int
		strategy string
		segs     int
	}{
		{2000, StrategyDirect, 1},
		{5000, StrategyDirect, 1},
		{5100, StrategyChained, 2},
		{10000, StrategyChained, 2},
		{14000, StrategyChained, 3},
		{15000, StrategyChained, 3},
		{30000, StrategyChained, 6},
	}
	for _, c := range cases {
		p, err := SolveTiming(TimingRequest{RequestedDurationMS: c.ms, FPS: fps}, videoCap(), "auto")
		if err != nil {
			t.Fatalf("SolveTiming(%d): %v", c.ms, err)
		}
		if p.Strategy != c.strategy {
			t.Fatalf("duration %d: strategy %s, want %s", c.ms, p.Strategy, c.strategy)
		}
		if len(p.Segments) != c.segs {
			t.Fatalf("duration %d: %d segments, want %d", c.ms, len(p.Segments), c.segs)
		}
		if p.PlannedDurationMS <= 0 {
			t.Fatalf("duration %d: planned duration must be positive", c.ms)
		}
		assertSegmentsValid(t, p, 17, 121)
	}
}

func assertSegmentsValid(t *testing.T, p *TimingPlan, min, max int) {
	t.Helper()
	prevEnd := 0
	for _, s := range p.Segments {
		if s.Frames < min || s.Frames > max {
			t.Fatalf("segment %d has %d frames outside [%d,%d]", s.Index, s.Frames, min, max)
		}
		if s.Frames%4 != 1 {
			t.Fatalf("segment %d has %d frames, not 4n+1", s.Index, s.Frames)
		}
		if s.StartFrame != prevEnd {
			t.Fatalf("segment %d start frame %d != previous end %d", s.Index, s.StartFrame, prevEnd)
		}
		if s.EndFrame-s.StartFrame != s.Frames {
			t.Fatalf("segment %d frame range does not match its count", s.Index)
		}
		prevEnd = s.EndFrame
	}
}

func TestSolveTimingRejectsOutOfRange(t *testing.T) {
	fps := RationalFPS{Num: 8, Den: 1}
	for _, bad := range []int{1999, 30001, 0, -1} {
		if _, err := SolveTiming(TimingRequest{RequestedDurationMS: bad, FPS: fps}, videoCap(), "auto"); err == nil {
			t.Fatalf("duration %d should be rejected", bad)
		}
	}
}

func TestSolveTimingRejectsMissingCapability(t *testing.T) {
	fps := RationalFPS{Num: 8, Den: 1}
	if _, err := SolveTiming(TimingRequest{RequestedDurationMS: 5000, FPS: fps}, nil, "auto"); err == nil {
		t.Fatal("missing capability must block planning")
	}
	if _, err := SolveTiming(TimingRequest{RequestedDurationMS: 5000, FPS: RationalFPS{Num: 25, Den: 1}}, videoCap(), "auto"); err == nil {
		t.Fatal("unsupported fps must be rejected")
	}
	// Context that leaves no room for new frames.
	if _, err := SolveTiming(TimingRequest{RequestedDurationMS: 5000, FPS: fps, ContextFrames: 110}, videoCap(), "auto"); err == nil {
		t.Fatal("context exceeding frame capacity must be rejected")
	}
}

func TestSolveTimingForcedStrategyUnsupported(t *testing.T) {
	fps := RationalFPS{Num: 8, Den: 1}
	if _, err := SolveTiming(TimingRequest{RequestedDurationMS: 15000, FPS: fps}, videoCap(), StrategyDirect); err == nil {
		t.Fatal("forcing direct beyond the direct maximum must fail, not silently substitute")
	}
}

func TestFramesForDurationRational(t *testing.T) {
	cases := []struct {
		ms   int
		fps  RationalFPS
		want int
	}{
		{5000, RationalFPS{8, 1}, 40},
		{5000, RationalFPS{16, 1}, 80},
		{2000, RationalFPS{8, 1}, 16},
		{14000, RationalFPS{8, 1}, 112},
		{5000, RationalFPS{30000, 1001}, 150},
	}
	for _, c := range cases {
		if got := framesForDuration(c.ms, c.fps); got != c.want {
			t.Fatalf("framesForDuration(%d, %d/%d) = %d, want %d", c.ms, c.fps.Num, c.fps.Den, got, c.want)
		}
	}
}

func TestAdjustFramesHonoursRuleAndBounds(t *testing.T) {
	rule := workflows.FrameRule{Type: "modulo", Modulus: 4, Remainder: 1}
	cases := []struct {
		desired, min, max, want int
	}{
		{40, 17, 121, 41},
		{16, 17, 121, 17},
		{120, 17, 121, 121},
		{39, 17, 121, 41},
		{1, 17, 121, 17},
	}
	for _, c := range cases {
		got, ok := adjustFrames(rule, c.desired, c.min, c.max)
		if !ok || got != c.want {
			t.Fatalf("adjustFrames(%d,%d,%d) = %d,%v want %d", c.desired, c.min, c.max, got, ok, c.want)
		}
	}
	if _, ok := adjustFrames(rule, 50, 122, 122); ok {
		t.Fatal("no congruent frame count in [122,122] should be infeasible")
	}
	// No rule means any count within bounds.
	if got, ok := adjustFrames(workflows.FrameRule{}, 33, 17, 121); !ok || got != 33 {
		t.Fatalf("rule-free adjust = %d,%v want 33", got, ok)
	}
}
