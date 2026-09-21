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

type syncResponse struct {
	Results []struct {
		DraftID string          `json:"draft_id"`
		Status  string          `json:"status"`
		Reason  string          `json:"reason"`
		Server  json.RawMessage `json:"server"`
	} `json:"results"`
}

func TestDraftsSync(t *testing.T) {
	st := testutil.NewStore(t)
	cfg := config.Default()
	cfg.LLM.Enabled = false
	h, _ := webapp.NewServer(t, st, cfg)

	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Drafts")
	_, _, shot := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")
	threads, _ := st.ListThreads(t.Context(), project.ID, video.ID)
	if len(threads) == 0 {
		t.Fatalf("expected a discovery thread")
	}

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + video.ID

	// The drafts page renders.
	page := testutil.MustStatus(t, c.Get(base+"/drafts"), http.StatusOK)
	if !strings.Contains(page, "Offline drafts") || !strings.Contains(page, "drafts-root") {
		t.Fatalf("drafts page did not render")
	}

	shotRevision := shot.Revision
	body := map[string]any{"drafts": []map[string]any{
		{"id": "d1", "mutation_type": "concept_note", "entity_id": threads[0].ID, "base_revision": 0,
			"payload": map[string]any{"thread_id": threads[0].ID, "content": "queued note"}},
		{"id": "d2", "mutation_type": "annotation", "entity_id": shot.ID, "base_revision": 0,
			"payload": map[string]any{"shot_id": shot.ID, "body": "queued annotation"}},
		{"id": "d3", "mutation_type": "shot_text", "entity_id": shot.ID, "base_revision": shotRevision,
			"payload": map[string]any{"title": "Renamed shot", "purpose": "new purpose", "treatment": "t", "duration_s": 4}},
		{"id": "d4", "mutation_type": "delete_shot", "entity_id": shot.ID, "base_revision": 0, "payload": map[string]any{}},
	}}

	resp := c.PostJSON(base+"/drafts/sync", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sync status = %d", resp.StatusCode)
	}
	var out syncResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode sync: %v", err)
	}
	resp.Body.Close()

	statuses := map[string]string{}
	for _, r := range out.Results {
		statuses[r.DraftID] = r.Status
	}
	for id, want := range map[string]string{"d1": "applied", "d2": "applied", "d3": "applied", "d4": "rejected"} {
		if statuses[id] != want {
			t.Fatalf("draft %s status = %q, want %q (%+v)", id, statuses[id], want, out.Results)
		}
	}

	// The applied shot edit is persisted.
	got, _ := st.GetShot(t.Context(), shot.ID)
	if got.Title != "Renamed shot" || got.DurationTargetMs != 4000 {
		t.Fatalf("shot_text draft not applied: %+v", got)
	}
	anns, _ := st.ListAnnotationsForVideo(t.Context(), video.ID)
	found := false
	for _, a := range anns {
		if a.Body == "queued annotation" {
			found = true
		}
	}
	if !found {
		t.Fatalf("annotation draft not applied")
	}
}

func TestDraftsSyncConflict(t *testing.T) {
	st := testutil.NewStore(t)
	cfg := config.Default()
	cfg.LLM.Enabled = false
	h, _ := webapp.NewServer(t, st, cfg)

	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Drafts")
	_, _, shot := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")

	// Simulate a concurrent server change.
	if err := st.UpdateShot(t.Context(), shot.ID, "Server edit", "", "", 3000, shot.Revision); err != nil {
		t.Fatalf("server edit: %v", err)
	}

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + video.ID

	body := map[string]any{"drafts": []map[string]any{
		{"id": "c1", "mutation_type": "shot_text", "entity_id": shot.ID, "base_revision": shot.Revision,
			"payload": map[string]any{"title": "Mobile edit", "purpose": "", "treatment": "", "duration_s": 5}},
	}}
	resp := c.PostJSON(base+"/drafts/sync", body)
	var out syncResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()

	if len(out.Results) != 1 || out.Results[0].Status != "conflict" {
		t.Fatalf("expected a conflict, got %+v", out.Results)
	}
	if len(out.Results[0].Server) == 0 || !strings.Contains(string(out.Results[0].Server), "Server edit") {
		t.Fatalf("conflict should include the server current value: %s", out.Results[0].Server)
	}
	// The server value must not have been overwritten.
	got, _ := st.GetShot(t.Context(), shot.ID)
	if got.Title != "Server edit" {
		t.Fatalf("conflict must not overwrite the server value: %+v", got)
	}
}
