package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"vidbuild/internal/config"
	"vidbuild/internal/store"
	"vidbuild/internal/testutil"
	"vidbuild/internal/testutil/webapp"
)

const superShotManifest = `{
  "name": "Test Video", "slug": "ss-test-video", "task": "video",
  "capabilities": {
    "video_timing": {
      "minimum_duration_ms": 2000, "maximum_direct_duration_ms": 5000, "preferred_segment_duration_ms": 5000,
      "minimum_frames": 17, "maximum_frames": 121,
      "frame_rule": { "type": "modulo", "modulus": 4, "remainder": 1 },
      "supported_fps": [ { "num": 8, "den": 1 }, { "num": 30, "den": 1 } ]
    },
    "inputs": { "start_image": true, "end_image": true },
    "continuation": { "mode": "anchor_only" }, "verified": false
  }
}`

const noCapManifest = `{ "name": "No Caps", "slug": "ss-no-caps", "task": "video" }`

func seedVideoWorkflow(t *testing.T, st *store.Store, slug, manifest string) string {
	t.Helper()
	bindings := []store.WorkflowBinding{
		{SemanticField: "first_frame_asset", NodeID: "6", InputName: "image", ValueType: "string", Required: true},
		{SemanticField: "last_frame_asset", NodeID: "7", InputName: "image", ValueType: "string", Required: true},
		{SemanticField: "first_frame_positive", NodeID: "4", InputName: "text", ValueType: "string", Required: true},
		{SemanticField: "output_node", NodeID: "12", InputName: "", ValueType: "node", Required: true},
	}
	if err := st.SeedBundledWorkflow(t.Context(), slug, slug, "video", "test", "{}", "sha-"+slug, manifest, "0.35.0", bindings); err != nil {
		t.Fatalf("seed %s: %v", slug, err)
	}
	pkgs, _ := st.ListWorkflowPackages(t.Context())
	for _, p := range pkgs {
		if p.Slug == slug && p.LatestVersion.Valid {
			return p.LatestVersion.String
		}
	}
	t.Fatalf("no version for %s", slug)
	return ""
}

func approveEndpoints(t *testing.T, st *store.Store, shot *store.Shot, wsID, projectID, userID string) {
	t.Helper()
	for _, role := range []string{"start_frame", "end_frame"} {
		a, err := st.CreateAsset(t.Context(), store.Asset{WorkspaceID: wsID, ProjectID: projectID, Kind: "image", Role: role, StorageURI: role + ".png", MimeType: "image/png", Checksum: "c-" + role})
		if err != nil {
			t.Fatalf("asset: %v", err)
		}
		if err := st.CreateSelection(t.Context(), shot.ID, role, a, userID, shot.Revision); err != nil {
			t.Fatalf("selection: %v", err)
		}
		if err := st.CreateApproval(t.Context(), a, "approve", "", "c-"+role, userID, shot.Revision); err != nil {
			t.Fatalf("approval: %v", err)
		}
	}
}

// TestSuperShotWebFlow proves the draft plan renders, creation enqueues nothing,
// review shows states and movement prompts, and approval queues anchors.
func TestSuperShotWebFlow(t *testing.T) {
	st := testutil.NewStore(t)
	h, _ := webapp.NewServer(t, st, config.Default())
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, vid := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Super")
	_, _, shot := testutil.MustShot(t, st, vid.ID, admin.ID, "Shot")
	versionID := seedVideoWorkflow(t, st, "ss-test-video", superShotManifest)
	approveEndpoints(t, st, shot, ws.ID, project.ID, admin.ID)

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + vid.ID

	if body := testutil.MustStatus(t, c.Get(base+"/super-shots/new?shot_id="+shot.ID), http.StatusOK); !strings.Contains(body, "Create plan") {
		t.Fatal("new super shot form should render")
	} else if !strings.Contains(body, "Approve plan") && !strings.Contains(body, "Presets") {
		t.Fatal("form missing expected content")
	}

	// Create the draft plan; it must not enqueue generation.
	resp := c.PostForm(base+"/super-shots/plan", url.Values{
		"shot_id": {shot.ID}, "duration_s": {"15"}, "workflow_id": {versionID},
		"positive": {"person walks to a chair and sits"}, "negative": {"lowres"},
		"start_state": {"standing"}, "end_state": {"seated"},
		"intermediate_states": {"turning toward the seat\nbeginning to lower"},
		"strategy":            {"auto"}, "context_mode": {"anchor_only"},
	})
	testutil.MustStatus(t, resp, http.StatusSeeOther)
	loc := testutil.Location(resp)
	if !strings.Contains(loc, "/super-shots/") {
		t.Fatalf("expected redirect to the plan, got %q", loc)
	}
	if jobs, _ := st.ListActiveJobs(t.Context()); len(jobs) != 0 {
		t.Fatalf("creating a draft plan must not enqueue jobs, got %d", len(jobs))
	}

	// Review shows the chain states and movement prompts.
	body := testutil.MustStatus(t, c.Get(loc), http.StatusOK)
	for _, want := range []string{"Approve plan", "intermediate", "Complete-shot movement", "Transition from", "Segment 1"} {
		if !strings.Contains(body, want) {
			t.Fatalf("plan review missing %q (loc=%s)", want, loc)
		}
	}

	// Approval queues the missing intermediate anchors.
	planID := loc[strings.LastIndex(loc, "/")+1:]
	testutil.MustStatus(t, c.PostForm(base+"/super-shots/"+planID+"/approve", url.Values{}), http.StatusSeeOther)
	jobs, _ := st.ListActiveJobs(t.Context())
	anchors := 0
	for _, j := range jobs {
		if j.JobType == "generate_anchor" {
			anchors++
		}
	}
	if anchors < 1 {
		t.Fatalf("approval should queue intermediate anchors, got %d anchor jobs", anchors)
	}
}

// TestSuperShotRejectsWorkflowWithoutCapability proves server-side rejection.
func TestSuperShotRejectsWorkflowWithoutCapability(t *testing.T) {
	st := testutil.NewStore(t)
	h, _ := webapp.NewServer(t, st, config.Default())
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, vid := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Super")
	_, _, shot := testutil.MustShot(t, st, vid.ID, admin.ID, "Shot")
	versionID := seedVideoWorkflow(t, st, "ss-no-caps", noCapManifest)
	approveEndpoints(t, st, shot, ws.ID, project.ID, admin.ID)

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + vid.ID
	resp := c.PostForm(base+"/super-shots/plan", url.Values{
		"shot_id": {shot.ID}, "duration_s": {"15"}, "workflow_id": {versionID},
		"positive": {"walk"}, "strategy": {"auto"}, "context_mode": {"anchor_only"},
	})
	testutil.MustStatus(t, resp, http.StatusSeeOther)
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=error") {
		t.Fatalf("a workflow without timing capability must be rejected, got %q", loc)
	}
}

// TestSuperShotCrossWorkspaceIs404 proves scope checks on new routes.
func TestSuperShotCrossWorkspaceIs404(t *testing.T) {
	st := testutil.NewStore(t)
	h, _ := webapp.NewServer(t, st, config.Default())
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, vid := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Super")
	_, _, shot := testutil.MustShot(t, st, vid.ID, admin.ID, "Shot")
	versionID := seedVideoWorkflow(t, st, "ss-test-video", superShotManifest)
	approveEndpoints(t, st, shot, ws.ID, project.ID, admin.ID)

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + vid.ID
	resp := c.PostForm(base+"/super-shots/plan", url.Values{
		"shot_id": {shot.ID}, "duration_s": {"15"}, "workflow_id": {versionID},
		"positive": {"walk"}, "strategy": {"auto"}, "context_mode": {"anchor_only"},
	})
	testutil.MustStatus(t, resp, http.StatusSeeOther)
	loc := testutil.Location(resp)
	planID := loc[strings.LastIndex(loc, "/")+1:]

	// A different workspace's project/video must not see this plan.
	otherWs, err := st.CreateWorkspace(t.Context(), admin.ID, "Other", "personal")
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	otherProj, otherVid := testutil.MustProjectVideo(t, st, otherWs.ID, admin.ID, "Other")
	testutil.MustStatus(t, c.Get("/w/"+otherWs.ID+"/projects/"+otherProj.ID+"/videos/"+otherVid.ID+"/super-shots/"+planID), http.StatusNotFound)
}
