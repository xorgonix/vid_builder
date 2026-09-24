package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// SuperShotPromptInput is the creative input to a Super Shot proposal.
type SuperShotPromptInput struct {
	ProjectContext     string
	ShotPurpose        string
	CompleteMovement   string
	StartState         string
	EndState           string
	SegmentDurationsMS []int
	ContinuityLocks    []string
	Constraints        string
}

// SuperShotAnchorDraft is one proposed intermediate boundary state (between the
// supplied first and last images).
type SuperShotAnchorDraft struct {
	Index int    `json:"index"`
	State string `json:"state"`
}

// SuperShotSegmentDraft is one proposed interval movement.
type SuperShotSegmentDraft struct {
	Index    int    `json:"index"`
	Movement string `json:"movement"`
}

// SuperShotPayload is a validated Super Shot plan proposal.
type SuperShotPayload struct {
	Summary         string                  `json:"summary"`
	Anchors         []SuperShotAnchorDraft  `json:"anchors"`
	Segments        []SuperShotSegmentDraft `json:"segments"`
	ContinuityLocks []string                `json:"continuity_locks"`
}

const superShotSchema = `{
  "summary": "one sentence describing the complete progression",
  "anchors": [ { "index": 1, "state": "concrete visible state: pose, position, objects, camera composition" } ],
  "segments": [ { "index": 0, "movement": "what changes during this interval, not the whole action" } ],
  "continuity_locks": ["identity", "wardrobe", "lighting direction"]
}`

// SuperShotProposal asks the model for concrete intermediate states and one
// movement prompt per interval. It must return exactly (segments-1) intermediate
// states and (segments) interval movements. Placeholder prose is rejected by
// Validate so an incomplete plan can never be presented as ready to render
// (contract C02).
func (p *Planner) SuperShotProposal(ctx context.Context, in SuperShotPromptInput) (*SuperShotPayload, error) {
	if !p.Enabled() {
		return nil, fmt.Errorf("planner: LLM disabled")
	}
	n := len(in.SegmentDurationsMS)
	if n < 1 {
		return nil, fmt.Errorf("planner: super shot needs at least one segment")
	}
	system := "You are the vid-build planner. Propose the intermediate visual states and the per-interval movement prompts for ONE shot " +
		"rendered as a chain of adjacent image pairs. " +
		"The supplied first and last images are fixed endpoints; describe only the states between them. " +
		"Each intermediate state must be a concrete description of pose, position, relevant objects and camera composition, " +
		"preserving identity, clothing and setting. Each interval movement describes only what changes between two adjacent states; " +
		"never restate the whole action. A hold or cycle is valid when intended. " +
		fmt.Sprintf("Return exactly %d intermediate states (indexed 1..%d) and exactly %d interval movements (indexed 0..%d). ", n-1, n-1, n, n-1) +
		"Do not claim to have inspected pixels. Output compact JSON only, no prose or markdown."
	if in.ProjectContext != "" {
		system += "\n\nProject context:\n" + in.ProjectContext
	}
	system += "\n\nShot purpose: " + in.ShotPurpose
	system += "\nComplete-shot movement: " + in.CompleteMovement
	if in.StartState != "" {
		system += "\nSupplied first image: " + in.StartState
	}
	if in.EndState != "" {
		system += "\nSupplied last image: " + in.EndState
	}
	if len(in.ContinuityLocks) > 0 {
		system += "\nKeep unchanged: " + strings.Join(in.ContinuityLocks, ", ")
	}
	if in.Constraints != "" {
		system += "\nConstraints: " + in.Constraints
	}
	// Per-interval pacing context (R6): the model must know how much screen time
	// each interval carries so action pacing fits the chain's timing.
	intervals := make([]string, 0, n)
	for i, ms := range in.SegmentDurationsMS {
		intervals = append(intervals, fmt.Sprintf("interval %d: %.1fs", i, float64(ms)/1000.0))
	}
	system += "\nInterval durations: " + strings.Join(intervals, "; ")
	raw, err := p.client.ChatJSON(ctx, system, nil, superShotSchema)
	if err != nil {
		return nil, err
	}
	var payload SuperShotPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if err := payload.Validate(n); err != nil {
		return nil, err
	}
	return &payload, nil
}

// Validate enforces exact counts, strict ordering of returned indices and the
// absence of empty or placeholder prose (R6: validate indices/order, not just
// array lengths).
func (s *SuperShotPayload) Validate(segments int) error {
	if len(s.Anchors) != segments-1 {
		return fmt.Errorf("planner: super shot proposed %d intermediate states, want %d", len(s.Anchors), segments-1)
	}
	if len(s.Segments) != segments {
		return fmt.Errorf("planner: super shot proposed %d movements, want %d", len(s.Segments), segments)
	}
	for i, a := range s.Anchors {
		if a.Index != i+1 {
			return fmt.Errorf("planner: intermediate state %d has index %d, want %d", i, a.Index, i+1)
		}
		st := strings.TrimSpace(a.State)
		if st == "" || strings.HasPrefix(strings.ToUpper(st), "PLACEHOLDER") {
			return fmt.Errorf("planner: intermediate state %d is empty or a placeholder", i+1)
		}
	}
	for i, seg := range s.Segments {
		if seg.Index != i {
			return fmt.Errorf("planner: interval %d has index %d, want %d", i, seg.Index, i)
		}
		if strings.TrimSpace(seg.Movement) == "" {
			return fmt.Errorf("planner: interval %d has no movement", i)
		}
	}
	return nil
}