// Package video implements Super Shot planning: a requested duration is compiled
// into a direct generation or an ordered chain of adjacent segments bounded by
// shared anchors. Timing rules come from the selected workflow's capabilities,
// never from application constants (spec §2.1, §5.5, §15.7).
package video

import (
	"fmt"
	"math"

	"vidbuild/internal/workflows"
)

// Supported request duration range (spec §2.1): 2.0–30.0 seconds.
const (
	MinDurationMS = 2000
	MaxDurationMS = 30000
)

// Strategy values.
const (
	StrategyDirect  = "direct"
	StrategyChained = "chained"
)

// Context modes.
const (
	ContextAnchorOnly   = "anchor_only"
	ContextTailFrames   = "tail_frames"
	ContextNativeLatent = "native_latent"
)

// RationalFPS is an exact frame rate. It is preserved through planning and only
// converted at workflow/FFmpeg boundaries (spec §4.6).
type RationalFPS struct {
	Num int
	Den int
}

// Valid reports whether the rational FPS is well formed.
func (f RationalFPS) Valid() bool { return f.Num > 0 && f.Den > 0 }

// Float converts to a float only for adapters that require one.
func (f RationalFPS) Float() float64 { return float64(f.Num) / float64(f.Den) }

// TimingRequest is the input to the solver.
type TimingRequest struct {
	RequestedDurationMS int
	FPS                 RationalFPS
	// ContextFrames is the conditioning prefix the workflow reserves per
	// segment. Zero for anchor_only. It reduces capacity for new frames
	// (spec §15.7) and is accounted for in the model window, not displayed.
	ContextFrames int
}

// SegmentTiming is one segment's frame range and derived display times.
type SegmentTiming struct {
	Index         int
	StartFrame    int
	EndFrame      int // half-open: [StartFrame, EndFrame)
	Frames        int // new output frames contributed by this segment
	StartOffsetMS int
	EndOffsetMS   int
	DurationMS    int
}

// TimingPlan is the solver result.
type TimingPlan struct {
	Strategy            string
	RequestedDurationMS int
	PlannedDurationMS   int
	TotalFrames         int
	FPS                 RationalFPS
	ContextFrames       int
	Segments            []SegmentTiming
	// Adjusted is true when frame congruence forced the planned duration away
	// from the requested duration. The caller must surface this to the creator.
	Adjusted bool
	Note     string
}

// ValidateRequestedDuration rejects durations outside 2.0–30.0 seconds.
func ValidateRequestedDuration(ms int) error {
	if ms < MinDurationMS || ms > MaxDurationMS {
		return fmt.Errorf("duration must be between %.1fs and %.1fs (got %dms)", float64(MinDurationMS)/1000, float64(MaxDurationMS)/1000, ms)
	}
	return nil
}

// framesForDuration converts a duration to an integer output-frame budget using
// the rational FPS and nearest rounding (spec §15.7).
func framesForDuration(ms int, fps RationalFPS) int {
	num := ms * fps.Num
	den := 1000 * fps.Den
	return int(math.Round(float64(num) / float64(den)))
}

// frameToMS maps an output frame index to a displayed millisecond time.
func frameToMS(frame int, fps RationalFPS) int {
	return int(math.Round(float64(frame) * float64(fps.Den) * 1000 / float64(fps.Num)))
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// adjustFrames returns the frame count nearest to desired that satisfies the
// workflow frame rule and lies within [min,max]. The boolean reports whether any
// feasible value exists.
func adjustFrames(rule workflows.FrameRule, desired, min, max int) (int, bool) {
	if min <= 0 {
		min = 1
	}
	if max <= 0 || max < min {
		max = math.MaxInt32
	}
	if rule.Type != "modulo" || rule.Modulus <= 0 {
		return clampInt(desired, min, max), true
	}
	m := rule.Modulus
	r := ((rule.Remainder % m) + m) % m

	// Smallest value >= min congruent to r (mod m).
	low := min
	if low%m != r {
		low += ((r-low)%m + m) % m
	}
	// Largest value <= max congruent to r (mod m).
	high := max
	if high != math.MaxInt32 {
		high -= ((high-r)%m + m) % m
	}
	if low > high {
		return 0, false
	}
	n := clampInt(desired, low, high)
	// Snap to congruence without crossing the bounds.
	if n%m != r {
		cand := n + ((r-n)%m+m)%m
		if cand > high {
			cand = n - ((n-r)%m+m)%m
		}
		n = cand
	}
	if n < low || n > high {
		return 0, false
	}
	return n, true
}

// SolveTiming compiles a requested duration into a direct or chained timing plan
// using only the workflow's declared capabilities. preferredStrategy is
// "auto", "direct" or "chained"; forcing an unsupported strategy is an error,
// never a silent substitution (spec §6.3).
func SolveTiming(req TimingRequest, cap *workflows.VideoTiming, preferredStrategy string) (*TimingPlan, error) {
	if err := ValidateRequestedDuration(req.RequestedDurationMS); err != nil {
		return nil, err
	}
	if !req.FPS.Valid() {
		return nil, fmt.Errorf("fps denominator must be positive")
	}
	if cap == nil {
		return nil, fmt.Errorf("the selected workflow declares no video timing capability; choose another workflow or confirm a legacy profile")
	}
	if !cap.SupportsFPS(req.FPS.Num, req.FPS.Den) {
		return nil, fmt.Errorf("workflow does not support %d/%d fps", req.FPS.Num, req.FPS.Den)
	}
	if req.ContextFrames < 0 {
		return nil, fmt.Errorf("context frame count cannot be negative")
	}

	fps := req.FPS
	total := framesForDuration(req.RequestedDurationMS, fps)
	if total < 1 {
		total = 1
	}
	// Model capacity available for new frames once context is reserved.
	capMax := cap.MaximumFrames
	if capMax > 0 {
		capMax -= req.ContextFrames
	}
	if capMax > 0 && capMax < cap.MinimumFrames {
		return nil, fmt.Errorf("context of %d frames leaves no room for new frames in this workflow", req.ContextFrames)
	}

	directFeasible := req.RequestedDurationMS <= maximumDirectMS(cap) ||
		(cap.MaximumDirectDurationMS <= 0 && req.RequestedDurationMS <= cap.PreferredSegmentDurationMS)
	single, singleOK := adjustFrames(cap.FrameRule, total, cap.MinimumFrames, capMax)

	switch preferredStrategy {
	case "", "auto":
		if directFeasible && singleOK {
			return &TimingPlan{
				Strategy: StrategyDirect, RequestedDurationMS: req.RequestedDurationMS,
				PlannedDurationMS: frameToMS(single, fps), TotalFrames: single, FPS: fps,
				ContextFrames: req.ContextFrames,
				Segments: []SegmentTiming{{Index: 0, StartFrame: 0, EndFrame: single, Frames: single,
					StartOffsetMS: 0, EndOffsetMS: frameToMS(single, fps), DurationMS: frameToMS(single, fps)}},
				Adjusted: single != total,
			}, nil
		}
	case StrategyDirect:
		if !singleOK {
			return nil, fmt.Errorf("workflow cannot represent the requested duration directly")
		}
		if req.RequestedDurationMS > maximumDirectMS(cap) {
			return nil, fmt.Errorf("requested duration exceeds the workflow's direct maximum of %dms", maximumDirectMS(cap))
		}
		return &TimingPlan{
			Strategy: StrategyDirect, RequestedDurationMS: req.RequestedDurationMS,
			PlannedDurationMS: frameToMS(single, fps), TotalFrames: single, FPS: fps,
			ContextFrames: req.ContextFrames,
			Segments: []SegmentTiming{{Index: 0, StartFrame: 0, EndFrame: single, Frames: single,
				StartOffsetMS: 0, EndOffsetMS: frameToMS(single, fps), DurationMS: frameToMS(single, fps)}},
			Adjusted: single != total,
		}, nil
	case StrategyChained:
		// fall through to chained below
	default:
		return nil, fmt.Errorf("unknown strategy %q", preferredStrategy)
	}

	return solveChained(req, cap, total, capMax, singleOK)
}

func solveChained(req TimingRequest, cap *workflows.VideoTiming, total, capMax int, singleOK bool) (*TimingPlan, error) {
	fps := req.FPS
	prefMS := cap.PreferredSegmentDurationMS
	if prefMS <= 0 {
		prefMS = cap.MaximumDirectDurationMS
	}
	if prefMS <= 0 {
		prefMS = 5000
	}
	minF := cap.MinimumFrames
	if minF <= 0 {
		minF = 1
	}
	if capMax <= 0 {
		capMax = math.MaxInt32
	}
	if total < minF && singleOK {
		// Too short for a chain but representable directly.
		return &TimingPlan{
			Strategy: StrategyDirect, RequestedDurationMS: req.RequestedDurationMS,
			PlannedDurationMS: frameToMS(total, fps), TotalFrames: total, FPS: fps,
			ContextFrames: req.ContextFrames,
			Segments: []SegmentTiming{{Index: 0, StartFrame: 0, EndFrame: total, Frames: total,
				StartOffsetMS: 0, EndOffsetMS: frameToMS(total, fps), DurationMS: frameToMS(total, fps)}},
		}, nil
	}

	// Feasible segment count range from frame capacity.
	nMin := int(math.Ceil(float64(total) / float64(capMax)))
	if nMin < 1 {
		nMin = 1
	}
	nMax := total / minF
	if nMax < 1 {
		return nil, fmt.Errorf("no feasible segmentation: the workflow minimum of %d frames exceeds the %d-frame budget", minF, total)
	}
	nPref := int(math.Ceil(float64(req.RequestedDurationMS) / float64(prefMS)))
	if nPref < 1 {
		nPref = 1
	}
	n := clampInt(nPref, nMin, nMax)
	if n > nMax {
		n = nMax
	}

	segs := make([]SegmentTiming, 0, n)
	frame := 0
	remaining := total
	for i := 0; i < n; i++ {
		left := n - i
		target := remaining / left
		lo := remaining - (left-1)*capMax
		hi := remaining - (left-1)*minF
		if lo < minF {
			lo = minF
		}
		if hi > capMax {
			hi = capMax
		}
		if lo > hi {
			return nil, fmt.Errorf("no feasible segmentation at segment %d", i)
		}
		f, ok := adjustFrames(cap.FrameRule, clampInt(target, lo, hi), lo, hi)
		if !ok {
			// Congruence may make the exact remaining budget unreachable for the
			// last segment; accept the nearest feasible count and let the planned
			// duration adjust (spec §15.7: never silently pad or change speed).
			f, ok = adjustFrames(cap.FrameRule, target, minF, capMax)
			if !ok {
				return nil, fmt.Errorf("no frame count satisfies the workflow rule at segment %d", i)
			}
		}
		segs = append(segs, SegmentTiming{
			Index: i, StartFrame: frame, EndFrame: frame + f, Frames: f,
			StartOffsetMS: frameToMS(frame, fps), EndOffsetMS: frameToMS(frame+f, fps),
		})
		frame += f
		remaining -= f
	}
	for i := range segs {
		segs[i].DurationMS = segs[i].EndOffsetMS - segs[i].StartOffsetMS
	}
	planned := frameToMS(frame, fps)
	return &TimingPlan{
		Strategy: StrategyChained, RequestedDurationMS: req.RequestedDurationMS,
		PlannedDurationMS: planned, TotalFrames: frame, FPS: fps, ContextFrames: req.ContextFrames,
		Segments: segs, Adjusted: frame != total,
		Note: func() string {
			if frame != total {
				return fmt.Sprintf("frame congruence adjusted the planned duration from %dms to %dms", req.RequestedDurationMS, planned)
			}
			return ""
		}(),
	}, nil
}

// maximumDirectMS returns the direct maximum, or the preferred segment duration
// when no explicit direct maximum was declared.
func maximumDirectMS(t *workflows.VideoTiming) int {
	if t == nil {
		return 0
	}
	if t.MaximumDirectDurationMS > 0 {
		return t.MaximumDirectDurationMS
	}
	return t.PreferredSegmentDurationMS
}
