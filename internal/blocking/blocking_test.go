package blocking

import (
	"strings"
	"testing"
)

func TestRenderDeterministic(t *testing.T) {
	s := Default()
	a := RenderSVG(s)
	b := RenderSVG(s)
	if a != b {
		t.Fatal("RenderSVG is not deterministic")
	}
	if !strings.Contains(a, "<svg") || !strings.Contains(a, "</svg>") {
		t.Fatal("output is not SVG")
	}
}

func TestRenderEscapesText(t *testing.T) {
	s := Default()
	s.Actors[0].Name = `<script>alert("x")</script>`
	out := RenderSVG(s)
	if strings.Contains(out, "<script>") {
		t.Fatal("actor name was not escaped")
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Fatalf("expected escaped markup, got %s", out)
	}
}

func TestAnalysisDetectsChange(t *testing.T) {
	start := Default()
	end := Default()
	end.Actors[0].X = 0.9
	end.Actors[0].Pose = "crouching"
	end.Camera.ShotSize = "close"
	changes := Analyze(start, end)
	joined := strings.Join(changes, " | ")
	for _, want := range []string{"position", "pose", "Shot size"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected change mentioning %q, got %v", want, changes)
		}
	}
}

func TestAnalysisFlagsNoChange(t *testing.T) {
	s := Default()
	changes := Analyze(s, s)
	if len(changes) != 1 || !strings.Contains(changes[0], "No visible change") {
		t.Fatalf("expected no-change warning, got %v", changes)
	}
}

func TestParseFallsBackToDefault(t *testing.T) {
	if got := Parse("not json"); got.Camera.ShotSize != Default().Camera.ShotSize {
		t.Fatalf("expected default camera, got %q", got.Camera.ShotSize)
	}
	if got := Parse(""); len(got.Actors) == 0 {
		t.Fatal("expected default actor")
	}
}

func TestRoundTripMarshal(t *testing.T) {
	s := Default()
	parsed := Parse(s.Marshal())
	if parsed.Camera.ShotSize != s.Camera.ShotSize || len(parsed.Actors) != len(s.Actors) {
		t.Fatal("marshal/parse round trip mismatch")
	}
}
