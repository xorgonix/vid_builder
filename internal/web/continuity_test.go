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

func TestContinuityStateAndDerivation(t *testing.T) {
	st := testutil.NewStore(t)
	cfg := config.Default()
	cfg.LLM.Enabled = false
	h, _ := webapp.NewServer(t, st, cfg)

	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Continuity")
	_, scene, shotA := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")
	shotB, err := st.CreateShot(t.Context(), scene.ID, admin.ID, "Shot B", "show suspicion", "close on her face", 2000)
	if err != nil {
		t.Fatalf("create shot B: %v", err)
	}

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + video.ID

	// Save an end state for A.
	endState := `{"characters":[{"id":"char_1","name":"Elena","state":"holding the cup"}],"setting":"kitchen","emotion":"uneasy","knowledge":["she suspects the second cup"],"open_questions":["who left the cup?"]}`
	resp := c.Patch(base+"/shots/"+shotA.ID+"/state/end", url.Values{"state_json": {endState}})
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=success") {
		t.Fatalf("save state redirect = %q", loc)
	}
	_ = resp.Body.Close()

	// Invalid state is rejected.
	resp = c.Patch(base+"/shots/"+shotA.ID+"/state/end", url.Values{"state_json": {`{"characters":[{}]}`}})
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=error") {
		t.Fatalf("invalid state should be rejected, redirect = %q", loc)
	}
	_ = resp.Body.Close()

	// Save the transition handoff into B.
	resp = c.Request(http.MethodPut, base+"/shots/"+shotA.ID+"/transition-out", url.Values{
		"transition_type":            {"reaction_cut"},
		"duration_ms":                {"0"},
		"motivation":                 {"the second cup causes suspicion"},
		"inherit_character_identity": {"on"},
		"inherit_emotional_state":    {"on"},
		"inherit_knowledge_state":    {"on"},
		"inherit_location":           {"on"},
		"inherit_prop_state":         {"on"},
		"recompose_allowed":          {"on"},
		"recompose_shot_size":        {"close"},
		"recompose_angle":            {"reverse"},
		"recompose_camera_side":      {"preserve_screen_direction"},
	}, nil)
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=success") {
		t.Fatalf("save transition redirect = %q", loc)
	}
	_ = resp.Body.Close()

	// Unknown transition type is rejected.
	resp = c.Request(http.MethodPut, base+"/shots/"+shotA.ID+"/transition-out", url.Values{"transition_type": {"teleport"}}, nil)
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=error") {
		t.Fatalf("unknown transition should be rejected, redirect = %q", loc)
	}
	_ = resp.Body.Close()

	page := testutil.MustStatus(t, c.Get(base+"/shots/"+shotA.ID+"/state"), http.StatusOK)
	if !strings.Contains(page, "reaction_cut") || !strings.Contains(page, "Shot B") {
		t.Fatalf("continuity page missing saved transition or next shot")
	}

	// Derive the next start state as a proposal.
	resp = c.PostForm(base+"/shots/"+shotA.ID+"/transition-out/derive-next", url.Values{})
	testutil.MustStatus(t, resp, http.StatusSeeOther)
	proposalID := proposalIDFrom(t, testutil.Location(resp))

	page = testutil.MustStatus(t, c.Get(base+"/proposals/"+proposalID), http.StatusOK)
	for _, want := range []string{"Inherited", "Intentionally changed", "Unresolved", "who left the cup?"} {
		if !strings.Contains(page, want) {
			t.Fatalf("continuity proposal missing %q", want)
		}
	}

	// Nothing is written to B until applied.
	if _, err := st.GetCurrentShotState(t.Context(), shotB.ID, "start"); err == nil {
		t.Fatalf("derive-next must not write the neighbour before apply")
	}

	resp = c.PostForm(base+"/proposals/"+proposalID+"/apply", url.Values{})
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=success") {
		t.Fatalf("apply continuity redirect = %q", loc)
	}
	_ = resp.Body.Close()

	state, err := st.GetCurrentShotState(t.Context(), shotB.ID, "start")
	if err != nil {
		t.Fatalf("derived start state missing after apply: %v", err)
	}
	if state.Source != "derived" {
		t.Fatalf("derived state source = %q, want derived", state.Source)
	}
}

func TestBoardContinuityMode(t *testing.T) {
	st := testutil.NewStore(t)
	cfg := config.Default()
	cfg.LLM.Enabled = false
	h, _ := webapp.NewServer(t, st, cfg)

	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Board")
	_, scene, shotA := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")
	shotB, err := st.CreateShot(t.Context(), scene.ID, admin.ID, "Shot B", "p", "t", 2000)
	if err != nil {
		t.Fatalf("create shot B: %v", err)
	}

	if _, err := st.SaveShotState(t.Context(), shotA.ID, "end", `{"setting":"kitchen"}`, "human", admin.ID); err != nil {
		t.Fatalf("save state: %v", err)
	}
	if _, err := st.UpsertShotLink(t.Context(), shotA.ID, shotB.ID, "continues_into",
		`{"transition_type":"hard_cut","motivation":"the door slams","inherit":{"character_identity":true,"location":true}}`); err != nil {
		t.Fatalf("link: %v", err)
	}

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + video.ID

	page := testutil.MustStatus(t, c.Get(base+"/board?mode=continuity"), http.StatusOK)
	if !strings.Contains(page, "hard_cut") {
		t.Fatalf("continuity board missing transition type")
	}
	if !strings.Contains(page, "the door slams") {
		t.Fatalf("continuity board missing motivation")
	}
	if !strings.Contains(page, "Inherits: identity, location") {
		t.Fatalf("continuity board missing inherited summary")
	}
	if !strings.Contains(page, "Final shot in the sequence.") {
		t.Fatalf("continuity board should mark the last shot")
	}
}
