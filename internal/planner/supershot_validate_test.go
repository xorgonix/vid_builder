package planner

import "testing"

// TestSuperShotValidateIndicesOrder proves R6/A13: the planner must validate the
// returned indices and their order, not only the array lengths.
func TestSuperShotValidateIndicesOrder(t *testing.T) {
	valid := &SuperShotPayload{
		Anchors:  []SuperShotAnchorDraft{{Index: 1, State: "mid"}, {Index: 2, State: "near seat"}},
		Segments: []SuperShotSegmentDraft{{Index: 0, Movement: "turn"}, {Index: 1, Movement: "sit"}, {Index: 2, Movement: "settle"}},
	}
	if err := valid.Validate(3); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}

	outOfOrderAnchors := &SuperShotPayload{
		Anchors:  []SuperShotAnchorDraft{{Index: 2, State: "near"}, {Index: 1, State: "turn"}},
		Segments: []SuperShotSegmentDraft{{Index: 0, Movement: "turn"}, {Index: 1, Movement: "sit"}},
	}
	if err := outOfOrderAnchors.Validate(2); err == nil {
		t.Fatal("out-of-order anchor indices must be rejected")
	}

	outOfOrderSegments := &SuperShotPayload{
		Anchors:  []SuperShotAnchorDraft{{Index: 1, State: "turn"}},
		Segments: []SuperShotSegmentDraft{{Index: 1, Movement: "sit"}, {Index: 0, Movement: "turn"}},
	}
	if err := outOfOrderSegments.Validate(2); err == nil {
		t.Fatal("swapped movement indices must be rejected")
	}

	missingIndexes := &SuperShotPayload{
		Anchors:  []SuperShotAnchorDraft{{State: "near"}, {State: "turn"}},
		Segments: []SuperShotSegmentDraft{{Movement: "sit"}, {Movement: "turn"}},
	}
	if err := missingIndexes.Validate(2); err == nil {
		t.Fatal("missing anchor indices must be rejected, not defaulted")
	}
}
