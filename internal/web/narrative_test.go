package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"vidbuild/internal/config"
	"vidbuild/internal/testutil"
	"vidbuild/internal/testutil/webapp"
)

func proposalIDFrom(t *testing.T, location string) string {
	t.Helper()
	i := strings.LastIndex(location, "/proposals/")
	if i < 0 {
		t.Fatalf("no proposal id in %q", location)
	}
	id := location[i+len("/proposals/"):]
	if q := strings.IndexByte(id, '?'); q >= 0 {
		id = id[:q]
	}
	return id
}

// TestNarrativeProposalSplit proves beats and shots are proposed separately and
// applied as independent, selectable steps (spec §8.3, §8.4, §8.6).
func TestNarrativeProposalSplit(t *testing.T) {
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
	project, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Narrative")
	_, scene, _ := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")
	beat, err := st.EnsureImplicitBeat(t.Context(), scene.ID)
	if err != nil {
		t.Fatalf("implicit beat: %v", err)
	}

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + video.ID

	fake.SetJSON(map[string]any{
		"summary": "two beats",
		"beats": []map[string]any{
			{"title": "Setup", "action": "she enters", "resulting_state": "she is inside", "intended_audience_effect": "curiosity"},
			{"title": "Turn", "action": "the door locks", "resulting_state": "she is trapped"},
		},
	})
	resp := c.PostForm(base+"/scenes/"+scene.ID+"/propose-beats", url.Values{})
	testutil.MustStatus(t, resp, http.StatusSeeOther)
	beatProposal := proposalIDFrom(t, testutil.Location(resp))

	page := testutil.MustStatus(t, c.Get(base+"/proposals/"+beatProposal), http.StatusOK)
	if !strings.Contains(page, "Setup") || !strings.Contains(page, "Apply selected beats") {
		t.Fatalf("beat proposal page did not render proposed beats")
	}

	fake.SetJSON(map[string]any{
		"summary": "one shot",
		"shots": []map[string]any{
			{"title": "Single shot", "narrative_purpose": "one shot is enough", "framing": "wide",
				"visible_change": "door closes", "generation_method": "blocking_only", "justification": "cheap and clear",
				"duration_target_ms": 2500},
		},
	})
	resp = c.PostForm(base+"/beats/"+beat.ID+"/propose-shots", url.Values{})
	testutil.MustStatus(t, resp, http.StatusSeeOther)
	shotProposal := proposalIDFrom(t, testutil.Location(resp))

	page = testutil.MustStatus(t, c.Get(base+"/proposals/"+shotProposal), http.StatusOK)
	if !strings.Contains(page, "Single shot") || !strings.Contains(page, "Apply selected shots") {
		t.Fatalf("shot proposal page did not render the proposed shot")
	}

	// Apply one beat (edit-before-apply) and one shot.
	resp = c.PostForm(base+"/proposals/"+beatProposal+"/apply", url.Values{
		"c_beat_0":       {"on"},
		"f_beat_title_0": {"Setup (edited)"},
	})
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=success") {
		t.Fatalf("apply beats redirect = %q", loc)
	}
	_ = resp.Body.Close()

	beats, _ := st.ListBeats(t.Context(), scene.ID)
	found := false
	for _, b := range beats {
		if b.Title == "Setup (edited)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("edited beat was not applied: %+v", beats)
	}

	resp = c.PostForm(base+"/proposals/"+shotProposal+"/apply", url.Values{
		"c_shot_0":       {"on"},
		"f_shot_title_0": {"Single shot"},
	})
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=success") {
		t.Fatalf("apply shots redirect = %q", loc)
	}
	_ = resp.Body.Close()

	shots, _ := st.ListShotsInBeat(t.Context(), beat.ID)
	shotFound := false
	for _, s := range shots {
		if s.Title == "Single shot" {
			shotFound = true
		}
	}
	if !shotFound {
		t.Fatalf("shot was not applied to the beat: %+v", shots)
	}
}
