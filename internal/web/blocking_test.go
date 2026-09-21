package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"vidbuild/internal/blocking"
	"vidbuild/internal/config"
	"vidbuild/internal/testutil"
	"vidbuild/internal/testutil/webapp"
)

func TestBlockingSaveValidation(t *testing.T) {
	st := testutil.NewStore(t)
	cfg := config.Default()
	cfg.LLM.Enabled = false
	h, _ := webapp.NewServer(t, st, cfg)

	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Blocking")
	_, _, shot := testutil.MustShot(t, st, video.ID, admin.ID, "Shot")

	character, err := st.CreateCharacter(t.Context(), project.ID, "Hero", "", "{}")
	if err != nil {
		t.Fatalf("create character: %v", err)
	}

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + video.ID
	save := base + "/shots/" + shot.ID + "/blocking/start"

	valid := blocking.Default()
	valid.Actors[0].CharacterID = character.ID
	resp := c.Patch(save, url.Values{"spec_json": {valid.Marshal()}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("valid save status = %d (%s)", resp.StatusCode, testutil.Body(t, resp))
	}
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=success") {
		t.Fatalf("valid blocking save redirect = %q", loc)
	}
	_ = resp.Body.Close()

	outOfRange := `{"version":1,"canvas":{"width":1,"height":1},"camera":{"shot_size":"medium","angle":"eye","x":5,"y":0,"w":1,"h":1}}`
	resp = c.Patch(save, url.Values{"spec_json": {outOfRange}})
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=error") {
		t.Fatalf("out-of-range blocking should be rejected, redirect = %q", loc)
	}
	_ = resp.Body.Close()

	foreign := blocking.Default()
	foreign.Actors[0].CharacterID = "does-not-belong"
	resp = c.Patch(save, url.Values{"spec_json": {foreign.Marshal()}})
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=error") {
		t.Fatalf("foreign character reference should be rejected, redirect = %q", loc)
	}
	_ = resp.Body.Close()

	// A v1 document is upgraded in memory and persisted as v2 on human save.
	v1 := `{"canvas":{"width":1,"height":1},"camera":{"shot_size":"medium","angle":"eye","x":0.05,"y":0.05,"w":0.9,"h":0.9},"actors":[{"name":"Elena","x":0.4,"y":0.5,"body_rotation":0,"pose":"walking","gaze_x":0.5,"gaze_y":0.4,"facing":"camera"}]}`
	resp = c.Patch(save, url.Values{"spec_json": {v1}})
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=success") {
		t.Fatalf("v1 save redirect = %q", loc)
	}
	_ = resp.Body.Close()

	saved, err := st.GetBlockingForShot(t.Context(), shot.ID, "start_frame", "start")
	if err != nil || saved == nil {
		t.Fatalf("saved blocking: %v", err)
	}
	if !strings.Contains(saved.SpecJSON, "element_id") || !strings.Contains(saved.SpecJSON, `"version":2`) {
		t.Fatalf("v1 blocking was not upgraded to v2 on save: %s", saved.SpecJSON)
	}
}
