package prompt

import (
	"strings"
	"testing"
)

func TestCompileProductsRoutesScopes(t *testing.T) {
	layers := []Layer{
		{Scope: "project_style", Mode: ModeAppend, Positive: "warm film grain"},
		{Scope: "character", Mode: ModeAppend, Positive: "Elena, red coat"},
		{Scope: "first_frame", Mode: ModeAppend, Positive: "cup on the table"},
		{Scope: "last_frame", Mode: ModeAppend, Positive: "cup overturned"},
		{Scope: "motion", Mode: ModeAppend, Positive: "she turns toward the door"},
		{Scope: "workflow", Mode: ModeAppend, Positive: "aux guidance"},
	}
	p := CompileProducts("base", "", layers)

	if !strings.Contains(p.FirstPositive, "cup on the table") || strings.Contains(p.FirstPositive, "cup overturned") {
		t.Fatalf("first product wrong: %q", p.FirstPositive)
	}
	if !strings.Contains(p.LastPositive, "cup overturned") || strings.Contains(p.LastPositive, "cup on the table") {
		t.Fatalf("last product wrong: %q", p.LastPositive)
	}
	if !strings.Contains(p.MotionPositive, "she turns") {
		t.Fatalf("motion product wrong: %q", p.MotionPositive)
	}
	for _, shared := range []string{"warm film grain", "Elena, red coat"} {
		if !strings.Contains(p.FirstPositive, shared) || !strings.Contains(p.LastPositive, shared) {
			t.Fatalf("shared layer %q missing from endpoints", shared)
		}
	}
	if p.ReferenceGuidance != "Elena, red coat" {
		t.Fatalf("reference guidance = %q", p.ReferenceGuidance)
	}
	if p.Auxiliary["workflow"] != "aux guidance" {
		t.Fatalf("auxiliary = %+v", p.Auxiliary)
	}
}

func TestMergeProductsReportsLoss(t *testing.T) {
	p := Products{
		FirstPositive:     "start state",
		LastPositive:      "end state",
		MotionPositive:    "she turns",
		FirstNegative:     "lowres",
		ReferenceGuidance: "Elena",
	}
	pos, neg, report := MergeProducts(p)
	if !report.Lossy {
		t.Fatal("merging separate fields into one prompt must be reported as lossy")
	}
	if len(report.MergedFields) < 3 {
		t.Fatalf("expected multiple merged field groups, got %v", report.MergedFields)
	}
	for _, want := range []string{"start state", "end state", "she turns", "Elena"} {
		if !strings.Contains(pos, want) {
			t.Fatalf("merged positive missing %q: %q", want, pos)
		}
	}
	if !strings.Contains(neg, "lowres") {
		t.Fatalf("merged negative missing: %q", neg)
	}
	if !strings.Contains(report.Note, "one prompt input") {
		t.Fatalf("merge note should be explicit: %q", report.Note)
	}
}

func TestMergeProductsNoLossWhenSingle(t *testing.T) {
	p := Products{FirstPositive: "only", FirstNegative: "no"}
	_, _, report := MergeProducts(p)
	if report.Lossy {
		t.Fatalf("a single populated field set should not be lossy: %+v", report)
	}
}

func TestWanOrder(t *testing.T) {
	got := WanOrder(WanFields{
		Subject: "Elena", Setting: "kitchen",
		InitialState: "holding the cup", Action: "the door opens",
		EndState: "she faces the empty hallway", ShotCamera: "medium, eye level",
		LightingColour: "warm evening", StyleMood: "tense", Continuity: "same red coat",
	})
	order := []string{"Elena, kitchen", "holding the cup", "the door opens", "she faces the empty hallway", "medium, eye level", "warm evening", "tense", "same red coat"}
	idx := -1
	for _, part := range order {
		next := strings.Index(got, part)
		if next < 0 {
			t.Fatalf("missing %q in %q", part, got)
		}
		if next < idx {
			t.Fatalf("out of order at %q: %q", part, got)
		}
		idx = next
	}
}

func TestEndPromptFromFacts(t *testing.T) {
	got := EndPromptFromFacts(map[string]string{
		"subject": "Elena", "state": "faces the empty hallway", "continuity": "red coat",
	})
	if !strings.HasPrefix(got, "Elena") || !strings.Contains(got, "faces the empty hallway") {
		t.Fatalf("end prompt = %q", got)
	}
}

func TestLintWarnings(t *testing.T) {
	warnings := Lint(LintInput{
		StartPositive:     "a warm kitchen at dusk",
		EndPositive:       "a warm kitchen at dusk",
		MotionPositive:    "camera push in and pull out. she runs. he falls. they shout.",
		NoVisibleChange:   true,
		InvariantChanged:  true,
		PromptTokens:      900,
		BudgetTokens:      512,
		DroppedStructured: []string{"character_id"},
	})
	codes := map[string]bool{}
	for _, w := range warnings {
		codes[w.Code] = true
	}
	for _, want := range []string{"identical_endpoints", "no_visible_change", "contradictory_camera", "too_many_actions", "invariant_changed", "over_budget", "dropped_fact"} {
		if !codes[want] {
			t.Fatalf("expected lint code %q; got %+v", want, warnings)
		}
	}
}

func TestLintClean(t *testing.T) {
	warnings := Lint(LintInput{
		StartPositive:  "Elena holds a cup in a warm kitchen",
		EndPositive:    "Elena stares at an overturned cup in a cold blue kitchen",
		MotionPositive: "she turns toward the doorway",
	})
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %+v", warnings)
	}
}

func TestLintNearIdentical(t *testing.T) {
	warnings := Lint(LintInput{
		StartPositive: "Elena holds a cup in a warm kitchen at dusk",
		EndPositive:   "Elena holds a cup in a warm kitchen at dusk now",
	})
	found := false
	for _, w := range warnings {
		if w.Code == "near_identical_endpoints" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected near_identical_endpoints, got %+v", warnings)
	}
}
