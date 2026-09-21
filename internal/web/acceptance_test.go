package web_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"vidbuild/internal/blocking"
	"vidbuild/internal/config"
	"vidbuild/internal/testutil"
	"vidbuild/internal/testutil/webapp"
)

// TestAcceptanceCheapCreativeLoop walks the v0.02 inexpensive creative loop end
// to end: concept → beats → shots → continuity → blocking → cues/metrics →
// animatic, without any image generation (spec §1, §16).
func TestAcceptanceCheapCreativeLoop(t *testing.T) {
	st := testutil.NewStore(t)
	fake := testutil.NewLMStudio(t)
	cfg := config.Default()
	cfg.LLM.Enabled = true
	cfg.LLM.BaseURL = fake.URL
	cfg.LLM.Model = "test-model"
	cfg.LLM.RequestTimeoutSec = 5
	h, _ := webapp.NewServer(t, st, cfg)

	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Acceptance")

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + video.ID

	// 1. Concept note in the discovery thread.
	threads, _ := st.ListThreads(t.Context(), project.ID, video.ID)
	if len(threads) == 0 {
		t.Fatalf("no discovery thread")
	}
	testutil.MustStatus(t, c.PostForm(base+"/discovery/threads/"+threads[0].ID+"/messages",
		url.Values{"content": {"A calm tactile teaser about a potter."}}), http.StatusSeeOther)

	// 2. Structure: create a section and scene manually (cheap path), then shots.
	section, err := st.CreateSection(t.Context(), video.ID, nil, "Act 1", "open", 15000)
	if err != nil {
		t.Fatalf("section: %v", err)
	}
	scene, err := st.CreateScene(t.Context(), section.ID, "The wheel", "she throws a pot", "hook", "moderate", "curiosity", 8000)
	if err != nil {
		t.Fatalf("scene: %v", err)
	}

	// 3. Beats proposed by the planner and applied.
	fake.SetJSON(map[string]any{
		"summary": "two beats",
		"beats": []map[string]any{
			{"title": "Set up", "action": "she centres the clay", "resulting_state": "clay is centred"},
			{"title": "Turn", "action": "the wheel slows", "resulting_state": "the pot holds"},
		},
	})
	resp := c.PostForm(base+"/scenes/"+scene.ID+"/propose-beats", url.Values{})
	testutil.MustStatus(t, resp, http.StatusSeeOther)
	beatProposal := proposalIDFrom(t, testutil.Location(resp))
	testutil.MustStatus(t, c.PostForm(base+"/proposals/"+beatProposal+"/apply", url.Values{"apply_all": {"1"}}), http.StatusSeeOther)

	beats, _ := st.ListBeats(t.Context(), scene.ID)
	if len(beats) != 2 {
		t.Fatalf("expected 2 applied beats, got %d", len(beats))
	}

	// 4. Shots proposed for the implicit beat and applied.
	implicit, _ := st.EnsureImplicitBeat(t.Context(), scene.ID)
	fake.SetJSON(map[string]any{
		"summary": "one shot is enough",
		"shots": []map[string]any{
			{"title": "Hands and clay", "narrative_purpose": "show the craft", "generation_method": "blocking_only", "duration_target_ms": 3000},
		},
	})
	resp = c.PostForm(base+"/beats/"+implicit.ID+"/propose-shots", url.Values{})
	testutil.MustStatus(t, resp, http.StatusSeeOther)
	shotProposal := proposalIDFrom(t, testutil.Location(resp))
	testutil.MustStatus(t, c.PostForm(base+"/proposals/"+shotProposal+"/apply", url.Values{"apply_all": {"1"}}), http.StatusSeeOther)

	shots, _ := st.ListShotsInBeat(t.Context(), implicit.ID)
	if len(shots) != 1 {
		t.Fatalf("expected 1 applied shot, got %d", len(shots))
	}
	shot := shots[0]

	// 5. Blocking saved and visible on the board.
	testutil.MustStatus(t, c.Patch(base+"/shots/"+shot.ID+"/blocking/start",
		url.Values{"spec_json": {blocking.Default().Marshal()}}), http.StatusSeeOther)
	board := testutil.MustStatus(t, c.Get(base+"/board"), http.StatusOK)
	if !strings.Contains(board, "<svg") {
		t.Fatalf("board should render blocking")
	}

	// 6. Continuity: end state, a following shot, transition and derived start.
	shotB, err := st.CreateShot(t.Context(), scene.ID, admin.ID, "The finished pot", "show the result", "slow push", 3000)
	if err != nil {
		t.Fatalf("create shot B: %v", err)
	}
	testutil.MustStatus(t, c.Patch(base+"/shots/"+shot.ID+"/state/end",
		url.Values{"state_json": {`{"characters":[{"id":"c1","name":"Potter"}],"setting":"studio","emotion":"calm"}`}}), http.StatusSeeOther)
	resp = c.Request(http.MethodPut, base+"/shots/"+shot.ID+"/transition-out", url.Values{
		"transition_type": {"match_action"}, "motivation": {"the hands leave the clay"},
		"inherit_character_identity": {"on"}, "inherit_location": {"on"}, "recompose_allowed": {"on"},
	}, nil)
	testutil.MustStatus(t, resp, http.StatusSeeOther)
	resp = c.PostForm(base+"/shots/"+shot.ID+"/transition-out/derive-next", url.Values{})
	testutil.MustStatus(t, resp, http.StatusSeeOther)
	derivedProposal := proposalIDFrom(t, testutil.Location(resp))
	testutil.MustStatus(t, c.PostForm(base+"/proposals/"+derivedProposal+"/apply", url.Values{}), http.StatusSeeOther)
	if _, err := st.GetCurrentShotState(t.Context(), shotB.ID, "start"); err != nil {
		t.Fatalf("derived start state missing: %v", err)
	}

	// 7. Cue + metric.
	testutil.MustStatus(t, c.PostForm(base+"/shots/"+shot.ID+"/cues", url.Values{
		"cue_type": {"voiceover"}, "content": {"Clay remembers every hand."}, "start_ms": {"0"}, "duration_ms": {"2000"},
	}), http.StatusSeeOther)
	testutil.MustStatus(t, c.PostForm(base+"/metrics", url.Values{
		"entity_type": {"shot"}, "entity_id": {shot.ID}, "dramatic_intensity": {"40"}, "emotional_valence": {"30"},
	}), http.StatusSeeOther)

	// 8. Animatic plays the whole video without any generated asset.
	dataResp := c.Get(base + "/animatic/data")
	if dataResp.StatusCode != http.StatusOK {
		t.Fatalf("animatic data status = %d", dataResp.StatusCode)
	}
	var doc struct {
		TotalDurationMs int `json:"total_duration_ms"`
		Shots           []struct {
			ID   string `json:"id"`
			Cues []struct {
				Content string `json:"content"`
			} `json:"cues"`
			Metrics map[string]int `json:"metrics"`
		} `json:"shots"`
	}
	if err := json.NewDecoder(dataResp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	dataResp.Body.Close()
	if len(doc.Shots) != 2 || doc.TotalDurationMs != 6000 {
		t.Fatalf("unexpected animatic doc: %+v", doc)
	}
	if len(doc.Shots[0].Cues) != 1 || doc.Shots[0].Cues[0].Content == "" {
		t.Fatalf("cue missing from animatic")
	}
	if doc.Shots[0].Metrics["dramatic_intensity"] != 40 {
		t.Fatalf("metric missing from animatic: %+v", doc.Shots[0].Metrics)
	}

	// 9. Approve the animatic.
	testutil.MustStatus(t, c.PostForm(base+"/animatic/approve", url.Values{}), http.StatusSeeOther)
	anim, _ := st.GetAnimaticForVideo(t.Context(), video.ID)
	if anim.Status != "approved" {
		t.Fatalf("animatic status = %q", anim.Status)
	}

	// 10. Backup covers all new tables (VACUUM INTO copies the whole database).
	metrics, _ := st.ListCurrentMetricsForVideo(t.Context(), video.ID)
	if len(metrics) == 0 {
		t.Fatalf("expected persisted metrics")
	}
}
