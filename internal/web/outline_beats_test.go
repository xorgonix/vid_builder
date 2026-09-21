package web_test

import (
	"net/http"
	"strings"
	"testing"

	"vidbuild/internal/config"
	"vidbuild/internal/testutil"
	"vidbuild/internal/testutil/webapp"
)

// TestImplicitBeatIsHidden proves a simple scene looks unchanged: its shots
// render directly without a beat container (spec §5.1).
func TestImplicitBeatIsHidden(t *testing.T) {
	st := testutil.NewStore(t)
	cfg := config.Default()
	cfg.LLM.Enabled = false
	h, _ := webapp.NewServer(t, st, cfg)

	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Beats")
	_, _, _ = testutil.MustShot(t, st, video.ID, admin.ID, "Opening shot")

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + video.ID

	page := testutil.MustStatus(t, c.Get(base+"/outline"), http.StatusOK)
	if !strings.Contains(page, "Opening shot") {
		t.Fatalf("outline does not show the shot")
	}
	if strings.Contains(page, "Beat:") {
		t.Fatalf("implicit beat should not render a beat container")
	}
	if !strings.Contains(page, "Add beat") {
		t.Fatalf("scene should offer to add an explicit beat")
	}
}
