package planner_test

import (
	"context"
	"testing"

	"vidbuild/internal/config"
	"vidbuild/internal/llm"
	"vidbuild/internal/planner"
	"vidbuild/internal/testutil"
)

func newPlanner(t *testing.T, fake *testutil.LMStudio) *planner.Planner {
	t.Helper()
	cfg := config.Default().LLM
	cfg.BaseURL = fake.URL
	return planner.New(llm.New(cfg))
}

func TestReplyUsesConfiguredModel(t *testing.T) {
	fake := testutil.NewLMStudio(t)
	fake.SetReply("Focus on the tactile craft.")
	p := newPlanner(t, fake)

	out, err := p.Reply(context.Background(), "Project: pottery", nil)
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if out != "Focus on the tactile craft." {
		t.Fatalf("reply = %q", out)
	}
	if fake.Calls() != 1 {
		t.Fatalf("expected 1 completion call, got %d", fake.Calls())
	}
}

func TestBriefProposalParsesJSON(t *testing.T) {
	fake := testutil.NewLMStudio(t)
	fake.SetJSON(map[string]any{
		"premise": "A calm tactile teaser", "theme": "craft",
		"audience": "locals", "desired_reaction": "warmth",
		"pace": "slow", "excitement": "low", "music_intent": "ambient",
		"sound_intent": "wheel hum", "visual_style": "warm film grain",
		"camera_language": "locked off", "production_notes": "none",
		"summary": "a succinct summary",
	})
	p := newPlanner(t, fake)

	got, err := p.BriefProposal(context.Background(), "", "", nil)
	if err != nil {
		t.Fatalf("brief proposal: %v", err)
	}
	if got.Premise != "A calm tactile teaser" || got.VisualStyle != "warm film grain" {
		t.Fatalf("payload not parsed: %+v", got)
	}
}

func TestBriefProposalToleratesCodeFence(t *testing.T) {
	fake := testutil.NewLMStudio(t)
	fake.SetReply("```json\n{\"premise\":\"fenced\",\"summary\":\"s\"}\n```")
	p := newPlanner(t, fake)

	got, err := p.BriefProposal(context.Background(), "", "", nil)
	if err != nil {
		t.Fatalf("brief proposal: %v", err)
	}
	if got.Premise != "fenced" {
		t.Fatalf("fenced JSON not extracted: %+v", got)
	}
}

func TestBriefProposalRejectsNonJSON(t *testing.T) {
	fake := testutil.NewLMStudio(t)
	fake.SetReply("I cannot help with that.")
	p := newPlanner(t, fake)

	if _, err := p.BriefProposal(context.Background(), "", "", nil); err == nil {
		t.Fatal("expected error for non-JSON planner output")
	}
}

func TestHealthReportsReachability(t *testing.T) {
	fake := testutil.NewLMStudio(t)
	p := newPlanner(t, fake)
	if err := p.Health(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
}
