package continuity

import (
	"strings"
	"testing"
)

func TestHandoffValidation(t *testing.T) {
	valid := Handoff{
		TransitionType: TransitionReactionCut,
		Motivation:     "the second cup causes suspicion",
		Inherit:        InheritFlags{CharacterIdentity: true, Costume: true, Location: true},
		Recompose:      Recompose{Allowed: true, ShotSize: "close", Angle: "reverse", CameraSide: "preserve_screen_direction"},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid handoff rejected: %v", err)
	}

	bad := valid
	bad.TransitionType = "teleport"
	if err := bad.Validate(); err == nil {
		t.Fatal("unknown transition type should be rejected")
	}

	bad = valid
	bad.DurationMs = -1
	if err := bad.Validate(); err == nil {
		t.Fatal("negative duration should be rejected")
	}

	bad = valid
	bad.Recompose.CameraSide = "sideways"
	if err := bad.Validate(); err == nil {
		t.Fatal("unknown camera_side should be rejected")
	}

	bad = valid
	bad.TransitionType = ""
	if err := bad.Validate(); err == nil {
		t.Fatal("missing transition type should be rejected")
	}
}

func TestAllRequiredTransitionTypesValid(t *testing.T) {
	if len(TransitionTypes) != 12 {
		t.Fatalf("expected 12 transition types, got %d", len(TransitionTypes))
	}
	for _, tt := range TransitionTypes {
		if !IsValidTransitionType(tt) {
			t.Fatalf("listed type %q is not valid", tt)
		}
	}
}

func TestValidateJSON(t *testing.T) {
	if err := ValidateJSON(`not json`); err == nil {
		t.Fatal("invalid JSON should be rejected")
	}
	if err := ValidateJSON(`{"transition_type":"hard_cut"}`); err != nil {
		t.Fatalf("minimal handoff should validate: %v", err)
	}
}

func TestStateValidation(t *testing.T) {
	ok := State{Characters: []StateEntity{{ID: "char_1", Name: "Elena"}}, Setting: "kitchen"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid state rejected: %v", err)
	}
	bad := State{Characters: []StateEntity{{}}}
	if err := bad.Validate(); err == nil {
		t.Fatal("entity without id or name should be rejected")
	}
	if err := ValidateStateJSON(`{}`); err != nil {
		t.Fatalf("empty state should be valid: %v", err)
	}
}

func TestDeriveNextStart(t *testing.T) {
	end := State{
		Characters:    []StateEntity{{ID: "char_1", Name: "Elena", State: "holding the cup"}},
		Props:         []StateEntity{{ID: "prop_1", Name: "cup", State: "on the table"}},
		Setting:       "kitchen",
		Time:          "evening",
		Emotion:       "uneasy",
		Knowledge:     []string{"she suspects the second cup"},
		OpenQuestions: []string{"who left the cup?"},
	}
	handoff := Handoff{
		TransitionType: TransitionReactionCut,
		Motivation:     "the second cup causes suspicion",
		Inherit: InheritFlags{
			CharacterIdentity: true, EmotionalState: true, KnowledgeState: true,
			Location: true, PropState: true,
		},
		Recompose: Recompose{Allowed: true, ShotSize: "close", Angle: "reverse"},
	}
	d := DeriveNextStart(end, handoff, "show her suspicion", []string{"Elena never removes her ring"})

	if d.Start.Setting != "kitchen" {
		t.Fatalf("setting not inherited: %+v", d.Start)
	}
	if len(d.Start.Characters) != 1 || d.Start.Characters[0].ID != "char_1" {
		t.Fatalf("character not inherited: %+v", d.Start)
	}
	if len(d.Start.Props) != 1 {
		t.Fatalf("prop not inherited: %+v", d.Start)
	}
	if !contains(d.Changed, "recomposed") {
		t.Fatalf("recompose not recorded as changed: %v", d.Changed)
	}
	if !contains(d.Inherited, "knowledge: she suspects the second cup") {
		t.Fatalf("knowledge not inherited: %v", d.Inherited)
	}
	if !contains(d.Unresolved, "who left the cup?") {
		t.Fatalf("open question not surfaced: %v", d.Unresolved)
	}
	if !contains(d.Inherited, "bible invariant: Elena never removes her ring") {
		t.Fatalf("invariant not included: %v", d.Inherited)
	}
}

func TestDeriveNextStartFlagsUninherited(t *testing.T) {
	end := State{Setting: "rooftop", Knowledge: []string{"the code"}}
	handoff := Handoff{TransitionType: TransitionLocationChange, Inherit: InheritFlags{}}
	d := DeriveNextStart(end, handoff, "", nil)
	if !contains(d.Unresolved, "location was not marked inherited: rooftop") {
		t.Fatalf("uninherited location should be unresolved: %v", d.Unresolved)
	}
	if !contains(d.Unresolved, "knowledge state was not marked inherited") {
		t.Fatalf("uninherited knowledge should be unresolved: %v", d.Unresolved)
	}
	if d.Start.Setting != "" {
		t.Fatalf("location should not be carried when not inherited: %+v", d.Start)
	}
}

func contains(list []string, substr string) bool {
	for _, s := range list {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}
