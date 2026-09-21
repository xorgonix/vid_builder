package blocking

import (
	"math"
	"strings"
	"testing"
)

func TestDefaultValidates(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("default blocking should validate: %v", err)
	}
}

func TestParseSetsSchemaVersion(t *testing.T) {
	s := Parse(`{"canvas":{"width":1,"height":1},"camera":{"shot_size":"medium","angle":"eye","x":0.1,"y":0.1,"w":0.8,"h":0.8}}`)
	if s.Version != SchemaVersion {
		t.Fatalf("version = %d, want %d", s.Version, SchemaVersion)
	}
}

func TestRejectsNaNAndInfinity(t *testing.T) {
	for name, mut := range map[string]func(*Spec){
		"nan actor x":   func(s *Spec) { s.Actors[0].X = math.NaN() },
		"inf actor y":   func(s *Spec) { s.Actors[0].Y = math.Inf(1) },
		"nan camera x":  func(s *Spec) { s.Camera.X = math.NaN() },
		"inf arrow x2":  func(s *Spec) { s.Arrows = []Arrow{{Kind: "movement", X1: 0, Y1: 0, X2: math.Inf(1), Y2: 0}} },
		"nan prop x":    func(s *Spec) { s.Props = []Prop{{Name: "p", X: math.NaN(), Y: 0.5}} },
		"nan gaze x":    func(s *Spec) { s.Actors[0].GazeX = math.NaN() },
		"nan rotation":  func(s *Spec) { s.Actors[0].BodyRotation = math.NaN() },
		"nan zone w":    func(s *Spec) { s.Zones[0].W = math.NaN() },
		"nan label pos": func(s *Spec) { s.Labels = []Label{{Text: "t", X: math.NaN(), Y: 0}} },
	} {
		s := Default()
		mut(&s)
		if err := s.Validate(); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
}

func TestRejectsOutOfRangeCoordinates(t *testing.T) {
	s := Default()
	s.Actors[0].X = 1.5
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected out-of-range error, got %v", err)
	}

	s = Default()
	s.Zones[0].H = 0
	if err := s.Validate(); err == nil {
		t.Fatal("expected zero-size zone to fail")
	}
}

func TestRejectsUnknownEnums(t *testing.T) {
	s := Default()
	s.Camera.ShotSize = "gigantic"
	if err := s.Validate(); err == nil {
		t.Fatal("expected unknown shot_size to fail")
	}

	s = Default()
	s.Camera.Angle = "sideways"
	if err := s.Validate(); err == nil {
		t.Fatal("expected unknown angle to fail")
	}

	s = Default()
	s.Actors[0].Facing = "diagonal"
	if err := s.Validate(); err == nil {
		t.Fatal("expected unknown facing to fail")
	}
}

func TestRejectsUnknownSchemaVersion(t *testing.T) {
	s := Default()
	s.Version = SchemaVersion + 1
	if err := s.Validate(); err == nil {
		t.Fatal("expected future schema version to fail")
	}
}

func TestRejectsMissingNames(t *testing.T) {
	s := Default()
	s.Actors[0].Name = "  "
	if err := s.Validate(); err == nil {
		t.Fatal("expected blank actor name to fail")
	}
}

func TestValidateJSON(t *testing.T) {
	if err := ValidateJSON(`not json`); err == nil {
		t.Fatal("expected invalid JSON to fail")
	}
	if err := ValidateJSON(Default().Marshal()); err != nil {
		t.Fatalf("default JSON should validate: %v", err)
	}
	bad := `{"canvas":{"width":1,"height":1},"camera":{"shot_size":"medium","angle":"eye","x":9,"y":0,"w":1,"h":1}}`
	if err := ValidateJSON(bad); err == nil {
		t.Fatal("expected out-of-range camera to fail")
	}
}
