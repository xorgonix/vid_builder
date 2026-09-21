package web_test

import (
	"net/url"
	"strings"
	"testing"

	"vidbuild/internal/blocking"
	"vidbuild/internal/config"
	"vidbuild/internal/testutil"
	"vidbuild/internal/testutil/webapp"
)

// TestBoardEndpointPrecedence proves the card falls back to blocking SVG when no
// generated asset exists, and shows an empty endpoint otherwise (spec §5.6).
func TestBoardEndpointPrecedence(t *testing.T) {
	st := testutil.NewStore(t)
	cfg := config.Default()
	cfg.LLM.Enabled = false
	h, _ := webapp.NewServer(t, st, cfg)

	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Board")
	_, _, shot := testutil.MustShot(t, st, video.ID, admin.ID, "Shot")

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + video.ID

	page := testutil.MustStatus(t, c.Get(base+"/board"), 200)
	if !strings.Contains(page, "no endpoint") {
		t.Fatalf("a shot with no asset or blocking should show an empty endpoint")
	}

	resp := c.Patch(base+"/shots/"+shot.ID+"/blocking/start", url.Values{"spec_json": {blocking.Default().Marshal()}})
	_ = resp.Body.Close()

	page = testutil.MustStatus(t, c.Get(base+"/board"), 200)
	if !strings.Contains(page, "<svg") {
		t.Fatalf("board should fall back to the blocking SVG")
	}
}
