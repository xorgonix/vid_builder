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

func TestPromptWorkbenchRoundTrip(t *testing.T) {
	st := testutil.NewStore(t)
	cfg := config.Default()
	cfg.LLM.Enabled = false
	h, _ := webapp.NewServer(t, st, cfg)

	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Prompt")
	_, _, shot := testutil.MustShot(t, st, video.ID, admin.ID, "Shot")

	c := testutil.NewClient(t, h)
	c.LoginAdmin()

	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + video.ID
	workbench := base + "/shots/" + shot.ID + "/prompt-workbench"

	page := testutil.MustStatus(t, c.Get(workbench), http.StatusOK)
	if !strings.Contains(page, "Prompt workbench") {
		t.Fatalf("workbench page did not render")
	}

	form := url.Values{
		"kind":         {"start_frame"},
		"layers_count": {"2"},
		"scope_0":      {"character"},
		"mode_0":       {"append"},
		"positive_0":   {"hero in a workshop"},
		"negative_0":   {""},
		"priority_0":   {"0"},
		"locked_0":     {"on"},
		"scope_1":      {"style"},
		"mode_1":       {"replace"},
		"positive_1":   {"clean warm film"},
		"negative_1":   {"blurry, distorted"},
		"priority_1":   {"5"},
	}
	testutil.MustStatus(t, c.PostForm(workbench, form), http.StatusSeeOther)

	reloaded := testutil.MustStatus(t, c.Get(workbench), http.StatusOK)
	for _, want := range []string{"hero in a workshop", "clean warm film", "blurry, distorted"} {
		if !strings.Contains(reloaded, want) {
			t.Fatalf("reloaded workbench missing %q", want)
		}
	}

	preview := testutil.MustStatus(t, c.PostForm(base+"/shots/"+shot.ID+"/prompt-preview", form), http.StatusOK)
	if !strings.Contains(preview, "clean warm film") {
		t.Fatalf("preview missing compiled positive: %.200s", preview)
	}
	if strings.Contains(preview, "hero in a workshop") {
		t.Fatalf("higher-priority replace should override the append layer: %.200s", preview)
	}
}

func TestPromptLegacyLockLayerStillCompiles(t *testing.T) {
	st := testutil.NewStore(t)
	cfg := config.Default()
	cfg.LLM.Enabled = false
	h, _ := webapp.NewServer(t, st, cfg)

	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Legacy")
	_, _, shot := testutil.MustShot(t, st, video.ID, admin.ID, "Shot")

	if _, err := st.SaveSpecWithLayers(t.Context(), shot.ID, admin.ID, "start_frame", "", "", `[{"scope":"character","mode":"lock","text":"legacy hero"}]`); err != nil {
		t.Fatalf("seed legacy layers: %v", err)
	}

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + video.ID
	page := testutil.MustStatus(t, c.Get(base+"/shots/"+shot.ID+"/prompt-workbench"), http.StatusOK)
	if !strings.Contains(page, "legacy hero") {
		t.Fatalf("legacy locked layer text missing from workbench")
	}
}

// TestContinuityFeedsPromptContext proves endpoint state feeds the inherited
// prompt context (spec 5.7).
func TestContinuityFeedsPromptContext(t *testing.T) {
	st := testutil.NewStore(t)
	cfg := config.Default()
	cfg.LLM.Enabled = false
	h, _ := webapp.NewServer(t, st, cfg)

	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Continuity prompt")
	_, _, shot := testutil.MustShot(t, st, video.ID, admin.ID, "Shot")

	if _, err := st.SaveShotState(t.Context(), shot.ID, "end", `{"characters":[{"id":"c1","name":"Elena"}],"setting":"kitchen","emotion":"uneasy"}`, "human", admin.ID); err != nil {
		t.Fatalf("save state: %v", err)
	}

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + video.ID
	page := testutil.MustStatus(t, c.Get(base+"/shots/"+shot.ID+"/prompt-workbench?kind=end_frame"), http.StatusOK)
	for _, want := range []string{"Elena", "kitchen", "uneasy"} {
		if !strings.Contains(page, want) {
			t.Fatalf("continuity fact %q missing from prompt context", want)
		}
	}
}
