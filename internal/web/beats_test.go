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

func TestBeatRoutes(t *testing.T) {
	st := testutil.NewStore(t)
	cfg := config.Default()
	cfg.LLM.Enabled = false
	h, _ := webapp.NewServer(t, st, cfg)

	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Beats")
	_, scene, _ := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + video.ID

	resp := c.PostForm(base+"/scenes/"+scene.ID+"/beats", url.Values{
		"title":           {"The turn"},
		"purpose":         {"raise tension"},
		"trigger":         {"a knock"},
		"action":          {"she opens the door"},
		"reaction":        {"she recoils"},
		"resulting_state": {"the room is empty"},
		"audience_effect": {"unease"},
		"budget_s":        {"4"},
	})
	testutil.MustStatus(t, resp, http.StatusSeeOther)

	created, err := st.ListBeats(t.Context(), scene.ID)
	if err != nil {
		t.Fatalf("list beats: %v", err)
	}
	found := false
	for _, b := range created {
		if b.Title == "The turn" && b.Trigger == "a knock" && b.ResultingState == "the room is empty" {
			found = true
		}
	}
	if !found {
		t.Fatalf("created beat not persisted: %+v", created)
	}

	page := testutil.MustStatus(t, c.Get(base+"/outline"), http.StatusOK)
	if !strings.Contains(page, "Beat: The turn") {
		t.Fatalf("outline does not render the explicit beat")
	}
	if !strings.Contains(page, "Beat: Scene") {
		t.Fatalf("outline should show the promoted beat alongside the new one")
	}

	// Blank title is rejected with a flash, not a 500.
	resp = c.PostForm(base+"/scenes/"+scene.ID+"/beats", url.Values{"title": {"   "}})
	testutil.MustStatus(t, resp, http.StatusSeeOther)

	// Deleting the scene's implicit beat is refused because it owns the shot.
	beats := created
	var implicitID string
	for _, b := range beats {
		shots, _ := st.ListShotsInBeat(t.Context(), b.ID)
		if len(shots) > 0 {
			implicitID = b.ID
		}
	}
	if implicitID == "" {
		t.Fatalf("implicit beat not found in %+v", beats)
	}
	resp = c.Delete(base + "/beats/" + implicitID)
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=error") {
		t.Fatalf("deleting a beat with shots should be refused, redirect = %q", loc)
	}
	_ = resp.Body.Close()

	// Deleting an empty beat succeeds.
	empty, err := st.CreateBeat(t.Context(), scene.ID, "Empty", "", "", "", "", "", "", 0)
	if err != nil {
		t.Fatalf("create empty beat: %v", err)
	}
	resp = c.Delete(base + "/beats/" + empty.ID)
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=info") {
		t.Fatalf("empty beat delete redirect = %q", loc)
	}
	_ = resp.Body.Close()
}

func TestBeatMove(t *testing.T) {
	st := testutil.NewStore(t)
	cfg := config.Default()
	cfg.LLM.Enabled = false
	h, _ := webapp.NewServer(t, st, cfg)

	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Beats")
	_, scene, _ := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")
	second, err := st.CreateBeat(t.Context(), scene.ID, "Second", "", "", "", "", "", "", 0)
	if err != nil {
		t.Fatalf("create beat: %v", err)
	}

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + video.ID
	testutil.MustStatus(t, c.PostForm(base+"/beats/"+second.ID+"/move", url.Values{"direction": {"-1"}}), http.StatusSeeOther)

	beats, _ := st.ListBeats(t.Context(), scene.ID)
	if beats[0].ID != second.ID {
		t.Fatalf("beat did not move to the top: %+v", beats)
	}
}
