package video

import (
	"fmt"
	"strings"
)

// Anchor kinds.
const (
	AnchorSuppliedStart = "supplied_start"
	AnchorIntermediate  = "intermediate"
	AnchorSuppliedEnd   = "supplied_end"
)

// PlanAnchor is one ordered boundary state. The first and last anchors reference
// the approved supplied endpoint assets; intermediates may begin without an
// asset and are generated before dependent segments (spec §5.3).
type PlanAnchor struct {
	Index          int
	TimeOffsetMS   int
	Kind           string
	StatePrompt    string
	NegativePrompt string
	Continuity     []string
	AssetID        string
	SourceAnchorID string
}

// PlanSegment is one adjacent anchor-to-anchor generation.
type PlanSegment struct {
	Index         int
	StartAnchor   int // index into ChainPlan.Anchors
	EndAnchor     int
	ActionPrompt  string // compiled movement prompt sent to the workflow
	RawAction     string // editable interval action (contract C06)
	Continuity    []string
	StartFrame    int
	EndFrame      int
	Frames        int
	StartOffsetMS int
	EndOffsetMS   int
	DurationMS    int
}

// ChainPlan is a fully-described Super Shot plan before persistence.
type ChainPlan struct {
	Strategy            string
	RequestedDurationMS int
	PlannedDurationMS   int
	FPS                 RationalFPS
	ContextMode         string
	Summary             string
	Anchors             []PlanAnchor
	Segments            []PlanSegment
	Warnings            []string
}

// PlanBuildInput supplies the creative material for a plan.
type PlanBuildInput struct {
	Timing             *TimingPlan
	StartAssetID       string
	EndAssetID         string
	StartState         string
	EndState           string
	BasePositive       string
	BaseNegative       string
	ShotIntent         string
	Camera             string
	ContinuityLocks    []string
	IntermediateStates []string // optional; length len(segments)-1
	SegmentActions     []string // optional; length len(segments)
	ContextMode        string
}

// BuildPlan assembles the anchor/segment chain for a timing plan. It never
// repeats the outer endpoints: a chained plan of N segments gets N+1 anchors and
// adjacent pairs (A→B, B→C, …). When the creator does not supply intermediate
// states or actions, it derives a distinct adjacent transition description and
// records a warning — it never fabricates one repeated A→Z segment.
func BuildPlan(in PlanBuildInput) (*ChainPlan, error) {
	if in.Timing == nil {
		return nil, fmt.Errorf("plan: missing timing plan")
	}
	t := in.Timing
	n := len(t.Segments)
	if n == 0 {
		return nil, fmt.Errorf("plan: timing produced no segments")
	}
	if in.StartAssetID == "" || in.EndAssetID == "" {
		return nil, fmt.Errorf("plan: an approved start and end image are required")
	}
	mode := in.ContextMode
	if mode == "" {
		mode = ContextAnchorOnly
	}
	locks := in.ContinuityLocks

	plan := &ChainPlan{
		Strategy: t.Strategy, RequestedDurationMS: t.RequestedDurationMS, PlannedDurationMS: t.PlannedDurationMS,
		FPS: t.FPS, ContextMode: mode, Summary: summarize(in, n),
	}
	plan.Warnings = append(plan.Warnings, t.Note)

	// Anchors: start, intermediates, end.
	startState := in.StartState
	if strings.TrimSpace(startState) == "" {
		startState = "The supplied starting image, unchanged."
	}
	endState := in.EndState
	if strings.TrimSpace(endState) == "" {
		endState = "The supplied ending image, fully reached."
	}
	plan.Anchors = append(plan.Anchors, PlanAnchor{
		Index: 0, TimeOffsetMS: t.Segments[0].StartOffsetMS, Kind: AnchorSuppliedStart,
		StatePrompt: startState, NegativePrompt: in.BaseNegative, Continuity: locks, AssetID: in.StartAssetID,
	})
	for i := 1; i < n; i++ {
		state := ""
		if i-1 < len(in.IntermediateStates) {
			state = strings.TrimSpace(in.IntermediateStates[i-1])
		}
		if state == "" {
			state = fmt.Sprintf("Intermediate state %d: a plausible visible state %s through the shot.", i, fractionPhrase(i, n))
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("anchor %d state is a placeholder; describe it before generating", i))
		}
		plan.Anchors = append(plan.Anchors, PlanAnchor{
			Index: i, TimeOffsetMS: t.Segments[i].StartOffsetMS, Kind: AnchorIntermediate,
			StatePrompt: state, NegativePrompt: in.BaseNegative, Continuity: locks,
		})
	}
	plan.Anchors = append(plan.Anchors, PlanAnchor{
		Index: n, TimeOffsetMS: t.Segments[n-1].EndOffsetMS, Kind: AnchorSuppliedEnd,
		StatePrompt: endState, NegativePrompt: in.BaseNegative, Continuity: locks, AssetID: in.EndAssetID,
	})

	// Segments: adjacent anchor pairs.
	for i, seg := range t.Segments {
		action := ""
		if i < len(in.SegmentActions) {
			action = strings.TrimSpace(in.SegmentActions[i])
		}
		if action == "" {
			action = fmt.Sprintf("Transition from \"%s\" to \"%s\".", short(plan.Anchors[i].StatePrompt), short(plan.Anchors[i+1].StatePrompt))
		}
		plan.Segments = append(plan.Segments, PlanSegment{
			Index: i, StartAnchor: i, EndAnchor: i + 1,
			RawAction:    action,
			ActionPrompt: compileMovementPrompt(in, plan.Anchors[i], plan.Anchors[i+1], action),
			Continuity:   locks,
			StartFrame:   seg.StartFrame, EndFrame: seg.EndFrame, Frames: seg.Frames,
			StartOffsetMS: seg.StartOffsetMS, EndOffsetMS: seg.EndOffsetMS, DurationMS: seg.DurationMS,
		})
	}

	if len(in.IntermediateStates) == 0 && n > 1 {
		plan.Warnings = append(plan.Warnings, "no per-interval actions were supplied; each segment uses an adjacent-state transition description")
	}
	if err := ValidatePlan(plan); err != nil {
		return nil, err
	}
	return plan, nil
}

// compileMovementPrompt builds the movement prompt for one interval (spec §6.7,
// §15.1). It carries the overall intent as context and the bounded change for
// this interval — not the whole action as though the shot restarted.
func compileMovementPrompt(in PlanBuildInput, start, end PlanAnchor, action string) string {
	var b strings.Builder
	// Contract C01: purpose and complete movement are distinct and both retained.
	// Never substitute one for the other, and never present purpose as movement.
	if m := strings.TrimSpace(in.BasePositive); m != "" {
		fmt.Fprintf(&b, "Complete-shot movement: %s\n", m)
	}
	if p := strings.TrimSpace(in.ShotIntent); p != "" {
		fmt.Fprintf(&b, "Shot purpose (why this exists, not the motion): %s\n", p)
	}
	b.WriteString("Preserve the first and last images exactly as conditioning endpoints.\n")
	if start.StatePrompt != "" {
		fmt.Fprintf(&b, "At segment start: %s\n", oneLine(start.StatePrompt))
	}
	fmt.Fprintf(&b, "During this interval: %s\n", oneLine(action))
	if end.StatePrompt != "" {
		fmt.Fprintf(&b, "At segment end: %s\n", oneLine(end.StatePrompt))
	}
	if cam := strings.TrimSpace(in.Camera); cam != "" {
		fmt.Fprintf(&b, "Camera: %s\n", cam)
	}
	if len(in.ContinuityLocks) > 0 {
		fmt.Fprintf(&b, "Keep unchanged: %s\n", strings.Join(in.ContinuityLocks, ", "))
	}
	if neg := strings.TrimSpace(in.BaseNegative); neg != "" {
		fmt.Fprintf(&b, "Avoid: %s\n", neg)
	}
	return strings.TrimSpace(b.String())
}

// ValidatePlan enforces the chain invariants (spec §6.6). Duplicate adjacent
// descriptions are permitted (spec §15.11) and are not errors.
func ValidatePlan(plan *ChainPlan) error {
	if plan == nil || len(plan.Anchors) == 0 {
		return fmt.Errorf("plan: empty plan")
	}
	if len(plan.Anchors) != len(plan.Segments)+1 {
		return fmt.Errorf("plan: %d anchors must equal %d segments + 1", len(plan.Anchors), len(plan.Segments))
	}
	if plan.Anchors[0].TimeOffsetMS != 0 {
		return fmt.Errorf("plan: first anchor must start at 0ms")
	}
	if plan.Anchors[len(plan.Anchors)-1].TimeOffsetMS != plan.PlannedDurationMS {
		return fmt.Errorf("plan: last anchor must end at the planned duration")
	}
	total := 0
	for i := range plan.Anchors {
		if i > 0 && plan.Anchors[i].TimeOffsetMS <= plan.Anchors[i-1].TimeOffsetMS {
			return fmt.Errorf("plan: anchor times must increase strictly (anchor %d)", i)
		}
		if plan.Anchors[i].Kind == AnchorIntermediate && strings.TrimSpace(plan.Anchors[i].StatePrompt) == "" {
			return fmt.Errorf("plan: intermediate anchor %d has no state description", i)
		}
	}
	for _, seg := range plan.Segments {
		if seg.StartAnchor != seg.Index || seg.EndAnchor != seg.Index+1 {
			return fmt.Errorf("plan: segment %d must reference adjacent anchors %d and %d", seg.Index, seg.Index, seg.Index+1)
		}
		if strings.TrimSpace(seg.ActionPrompt) == "" {
			return fmt.Errorf("plan: segment %d has no movement prompt", seg.Index)
		}
		want := plan.Anchors[seg.EndAnchor].TimeOffsetMS - plan.Anchors[seg.StartAnchor].TimeOffsetMS
		if seg.DurationMS != want {
			return fmt.Errorf("plan: segment %d duration %dms does not match its anchors (%dms)", seg.Index, seg.DurationMS, want)
		}
		total += seg.DurationMS
	}
	if total != plan.PlannedDurationMS {
		return fmt.Errorf("plan: segment durations total %dms, expected %dms", total, plan.PlannedDurationMS)
	}
	if plan.Strategy == StrategyChained && len(plan.Segments) < 2 {
		return fmt.Errorf("plan: a chained plan needs at least two segments")
	}
	return nil
}

// LegacyInvalidPlan reports whether a request whose segments all reuse the same
// outer start/end pair is a legacy invalid chain (spec §9.1).
func LegacyInvalidPlan(segmentStarts, segmentEnds []string) bool {
	if len(segmentStarts) < 2 || len(segmentStarts) != len(segmentEnds) {
		return false
	}
	first, last := segmentStarts[0], segmentEnds[0]
	if first == "" || last == "" {
		return false
	}
	for i := range segmentStarts {
		if segmentStarts[i] != first || segmentEnds[i] != last {
			return false
		}
	}
	return true
}

func summarize(in PlanBuildInput, n int) string {
	// The summary describes the movement; purpose is separate context (C01).
	base := strings.TrimSpace(firstNonEmpty(in.BasePositive, in.ShotIntent))
	if base == "" {
		base = "the described movement"
	}
	if n == 1 {
		return fmt.Sprintf("One generation for %q.", base)
	}
	return fmt.Sprintf("A %d-segment chain for %q.", n, base)
}

func fractionPhrase(i, n int) string {
	if n <= 1 {
		return "at the start"
	}
	pct := i * 100 / n
	switch {
	case pct < 34:
		return "early"
	case pct < 67:
		return "around the middle"
	default:
		return "late"
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func short(s string) string {
	s = oneLine(s)
	if len(s) > 60 {
		return s[:57] + "…"
	}
	return s
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
