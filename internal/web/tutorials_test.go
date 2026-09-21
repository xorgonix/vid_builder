package web_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"vidbuild/internal/config"
	"vidbuild/internal/testutil"
	"vidbuild/internal/testutil/webapp"
)

// TestGuidedTours proves the guided-tour catalogue and its scripts are served,
// so the in-app driver can fetch and execute them.
func TestGuidedTours(t *testing.T) {
	st := testutil.NewStore(t)
	h, _ := webapp.NewServer(t, st, config.Default())
	testutil.MustAdmin(t, st)

	c := testutil.NewClient(t, h)
	c.LoginAdmin()

	body := testutil.MustStatus(t, c.Get("/tutorials"), http.StatusOK)
	for _, want := range []string{"Guided tours", "Create a workspace and project", "Make a 5/15/30-second video", "/static/tutorial.js"} {
		if !strings.Contains(body, want) {
			t.Fatalf("tutorials page missing %q", want)
		}
	}
	if js := testutil.MustStatus(t, c.Get("/static/tutorial.js"), http.StatusOK); !strings.Contains(js, "tutorial-spotlight") {
		t.Fatal("tutorial driver was not served")
	}

	raw := testutil.MustStatus(t, c.Get("/tutorials/setup/script.json"), http.StatusOK)
	var script struct {
		Slug  string `json:"slug"`
		Steps []struct {
			Action string `json:"action"`
			Target string `json:"target"`
			Value  string `json:"value"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(raw), &script); err != nil {
		t.Fatalf("script json: %v", err)
	}
	if script.Slug != "setup" || len(script.Steps) == 0 {
		t.Fatalf("unexpected script: %+v", script)
	}
	found := false
	for _, s := range script.Steps {
		if s.Value == "Tutorial Studio" {
			found = true
		}
	}
	if !found {
		t.Fatal("setup script should create a Tutorial Studio workspace")
	}

	testutil.MustStatus(t, c.Get("/tutorials/missing/script.json"), http.StatusNotFound)
}
