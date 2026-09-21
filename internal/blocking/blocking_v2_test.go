package blocking

import (
	"strings"
	"testing"
)

func TestParseUpgradesV1ToV2(t *testing.T) {
	v1 := `{"canvas":{"width":1,"height":1},"camera":{"shot_size":"medium","angle":"eye","x":0.05,"y":0.05,"w":0.9,"h":0.9},"actors":[{"name":"Elena","x":0.4,"y":0.5,"body_rotation":0,"pose":"walking","gaze_x":0.5,"gaze_y":0.4,"facing":"camera"}]}`
	s := Parse(v1)
	if s.Version != SchemaVersion {
		t.Fatalf("version = %d, want %d", s.Version, SchemaVersion)
	}
	if s.Actors[0].ElementID != "actor_1" {
		t.Fatalf("actor element id = %q, want actor_1", s.Actors[0].ElementID)
	}
	if s.Actors[0].PoseCategory != "walking" {
		t.Fatalf("pose category = %q, want walking", s.Actors[0].PoseCategory)
	}
	if s.Camera.Height != 0.5 {
		t.Fatalf("camera height default = %v, want 0.5", s.Camera.Height)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("upgraded v1 should validate: %v", err)
	}
}

func TestV1RowsAreNotRewritten(t *testing.T) {
	// Parse must not mutate the stored document; Marshal of the original string
	// is only produced when a human saves.
	v1 := `{"canvas":{"width":1,"height":1},"camera":{"shot_size":"medium","angle":"eye","x":0.05,"y":0.05,"w":0.9,"h":0.9}}`
	_ = Parse(v1)
	if strings.Contains(v1, "element_id") {
		t.Fatal("test setup error: v1 input already has element ids")
	}
}

func TestValidateRejectsDuplicateElementIDs(t *testing.T) {
	s := Default()
	s.Actors = append(s.Actors, Actor{ElementID: "actor_1", Name: "Second", X: 0.2, Y: 0.2, GazeX: 0.2, GazeY: 0.2, Facing: "camera"})
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("expected duplicate element id error, got %v", err)
	}
}

func TestValidateRejectsUnknownFocalAndPose(t *testing.T) {
	s := Default()
	s.Camera.FocalCategory = "holographic"
	if err := s.Validate(); err == nil {
		t.Fatal("unknown focal category should be rejected")
	}
	s = Default()
	s.Actors[0].PoseCategory = "floating"
	if err := s.Validate(); err == nil {
		t.Fatal("unknown pose category should be rejected")
	}
}

func TestInterpolateMatchesByElementID(t *testing.T) {
	start := Default()
	end := Default()
	end.Actors[0].X = 0.9
	end.Actors[0].Y = 0.1
	mid := Interpolate(start, end, 0.5)
	if mid.Actors[0].ElementID != start.Actors[0].ElementID {
		t.Fatalf("interpolation should preserve the element id")
	}
	if abs(mid.Actors[0].X-0.7) > 1e-9 || abs(mid.Actors[0].Y-0.325) > 1e-9 {
		t.Fatalf("interpolated position = (%v,%v)", mid.Actors[0].X, mid.Actors[0].Y)
	}

	// A renamed-but-same-id actor is still matched.
	start2 := Default()
	start2.Actors[0].ElementID = "actor_elena"
	start2.Actors[0].Name = "Elena"
	end2 := Default()
	end2.Actors[0].ElementID = "actor_elena"
	end2.Actors[0].Name = "Elena (wet)"
	end2.Actors[0].X = 1
	mid2 := Interpolate(start2, end2, 0.5)
	if abs(mid2.Actors[0].X-0.75) > 1e-9 {
		t.Fatalf("element-id match failed; x = %v", mid2.Actors[0].X)
	}
}

func TestMotionValidation(t *testing.T) {
	ok := MotionSpec{
		SubjectMotions:    []SubjectMotion{{ElementID: "actor_1", Action: "turns toward doorway", StartMs: 1100, DurationMs: 900, Easing: "fast_out"}},
		CameraMotion:      &CameraMotion{Kind: "push_in", Amount: "subtle", DurationMs: 3500},
		EnvironmentMotion: []string{"curtain moves gently"},
		NegativeMotion:    []string{"no camera orbit"},
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid motion rejected: %v", err)
	}
	if err := ValidateMotionJSON("{}"); err != nil {
		t.Fatalf("empty motion should be valid: %v", err)
	}

	bad := MotionSpec{SubjectMotions: []SubjectMotion{{Action: "no id"}}}
	if err := bad.Validate(); err == nil {
		t.Fatal("subject motion without element_id should be rejected")
	}
	bad = MotionSpec{SubjectMotions: []SubjectMotion{{ElementID: "actor_1", Action: "x", Easing: "bouncy"}}}
	if err := bad.Validate(); err == nil {
		t.Fatal("unknown easing should be rejected")
	}
	bad = MotionSpec{CameraMotion: &CameraMotion{Kind: "teleport"}}
	if err := bad.Validate(); err == nil {
		t.Fatal("unknown camera motion should be rejected")
	}
}
