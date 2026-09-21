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

// TestDurationPageRenders proves the duration-video screen renders with the
// 5/15/30 form and no eligible shots, and that planning is refused without
// approved endpoints rather than silently degrading.
func TestDurationPageRenders(t *testing.T) {
	st := testutil.NewStore(t)
	h, _ := webapp.NewServer(t, st, config.Default())

	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Duration page")
	_, _, shot := testutil.MustShot(t, st, video.ID, admin.ID, "Shot")

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + video.ID

	body := testutil.MustStatus(t, c.Get(base+"/duration"), http.StatusOK)
	for _, want := range []string{"Duration video", "5 seconds", "15 seconds", "30 seconds", "No duration videos planned yet."} {
		if !strings.Contains(body, want) {
			t.Fatalf("duration page missing %q", want)
		}
	}

	resp := c.PostForm(base+"/duration", url.Values{"shot_id": {shot.ID}, "duration": {"15"}})
	testutil.MustStatus(t, resp, http.StatusSeeOther)
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=error") {
		t.Fatalf("planning without approved endpoints should be refused, got %q", loc)
	}
}
