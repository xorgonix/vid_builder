package prompt

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLockedLayerStillContributesText(t *testing.T) {
	res := Compile("base", "", []Layer{{Scope: "character", Mode: ModeAppend, Positive: "hero", Locked: true}})
	if !strings.Contains(res.Positive, "hero") {
		t.Fatalf("locked layer text missing from positive: %q", res.Positive)
	}
	if !strings.Contains(strings.Join(res.Applied, ","), "locked") {
		t.Fatalf("applied markers should note the lock: %v", res.Applied)
	}
}

func TestLegacyLockModeMigratesToLockedAppend(t *testing.T) {
	var l Layer
	if err := json.Unmarshal([]byte(`{"scope":"character","mode":"lock","text":"hero"}`), &l); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !l.Locked {
		t.Fatal("legacy lock mode should set Locked")
	}
	if l.Mode != ModeAppend {
		t.Fatalf("legacy lock mode = %q, want append", l.Mode)
	}
	if l.Positive != "hero" {
		t.Fatalf("legacy text = %q, want folded into Positive", l.Positive)
	}
	res := Compile("base", "", []Layer{l})
	if !strings.Contains(res.Positive, "hero") {
		t.Fatalf("migrated locked layer text missing: %q", res.Positive)
	}
}

func TestLegacyTextFoldsIntoPositive(t *testing.T) {
	var l Layer
	if err := json.Unmarshal([]byte(`{"scope":"style","mode":"append","text":"grainy"}`), &l); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if l.Positive != "grainy" {
		t.Fatalf("positive = %q, want grainy", l.Positive)
	}
}

func TestUnknownModeDefaultsToAppend(t *testing.T) {
	res := Compile("", "", []Layer{{Scope: "x", Mode: "banana", Positive: "text"}})
	if res.Positive != "text" {
		t.Fatalf("positive = %q, want text", res.Positive)
	}
}

func TestPriorityOrdersApplication(t *testing.T) {
	layers := []Layer{
		{Scope: "a", Mode: ModeReplace, Positive: "final", Priority: 10},
		{Scope: "b", Mode: ModeAppend, Positive: "base", Priority: 0},
	}
	if got := Compile("", "", layers).Positive; got != "final" {
		t.Fatalf("high priority replace should win, got %q", got)
	}

	reversed := []Layer{
		{Scope: "a", Mode: ModeReplace, Positive: "final", Priority: 0},
		{Scope: "b", Mode: ModeAppend, Positive: "base", Priority: 10},
	}
	if got := Compile("", "", reversed).Positive; got != "final, base" {
		t.Fatalf("low priority replace should be overridden, got %q", got)
	}
}

func TestNegativeAndSuppress(t *testing.T) {
	res := Compile("base", "lowres", []Layer{
		{Scope: "a", Mode: ModeSuppress, Positive: "blurry"},
		{Scope: "b", Mode: ModeAppend, Positive: "sharp", Negative: "distorted"},
	})
	if !strings.Contains(res.Positive, "sharp") || strings.Contains(res.Positive, "blurry") {
		t.Fatalf("suppress should not add to positive: %q", res.Positive)
	}
	for _, want := range []string{"lowres", "blurry", "distorted"} {
		if !strings.Contains(res.Negative, want) {
			t.Fatalf("negative %q missing %q", res.Negative, want)
		}
	}
}

func TestReplaceClearsEarlierPositive(t *testing.T) {
	res := Compile("inherited", "", []Layer{{Scope: "a", Mode: ModeReplace, Positive: "only"}})
	if res.Positive != "only" {
		t.Fatalf("replace = %q, want only", res.Positive)
	}
}

func TestNormalizeDoesNotMutateSupportedModes(t *testing.T) {
	for _, mode := range []string{ModeInherit, ModeAppend, ModeReplace, ModeSuppress} {
		l := Layer{Mode: mode}
		l.Normalize()
		if l.Mode != mode {
			t.Fatalf("Normalize changed supported mode %q to %q", mode, l.Mode)
		}
	}
}
