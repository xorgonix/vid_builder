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

// TestSuperShotPlannerProposal proves C02/T03: when a planner is configured and
// no manual states are given, the app proposes concrete intermediate states and
// interval movements, and approval then queues anchor generation.
func TestSuperShotPlannerProposal(t *testing.T) {
	st := testutil.NewStore(t)
	fake := testutil.NewLMStudio(t)
	cfg := config.Default()
	cfg.LLM.Enabled = true
	cfg.LLM.BaseURL = fake.URL
	cfg.LLM.Model = "test-model"
	cfg.LLM.RequestTimeoutSec = 5
	h, _ := webapp.NewServer(t, st, cfg)

	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, vid := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Planner")
	_, _, shot := testutil.MustShot(t, st, vid.ID, admin.ID, "Shot")
	versionID := seedVideoWorkflow(t, st, "ss-plan", superShotManifest)
	approveEndpoints(t, st, shot, ws.ID, project.ID, admin.ID)

	fake.SetJSON(map[string]any{
		"summary": "walks across the room and sits",
		"anchors": []map[string]any{
			{"index": 1, "state": "beside the chair, beginning to turn"},
			{"index": 2, "state": "turning, knees bending"},
			{"index": 3, "state": "lowering, hands reaching the seat"},
		},
		"segments": []map[string]any{
			{"index": 0, "movement": "walk toward the chair"},
			{"index": 1, "movement": "turn toward the seat"},
			{"index": 2, "movement": "begin lowering"},
			{"index": 3, "movement": "settle into the seat"},
		},
		"continuity_locks": []string{"identity", "wardrobe"},
	})

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + vid.ID
	resp := c.PostForm(base+"/super-shots/plan", url.Values{
		"shot_id": {shot.ID}, "duration_s": {"15"}, "workflow_id": {versionID},
		"positive": {"walk to the chair and sit naturally"}, "negative": {"lowres"},
		"strategy": {"auto"}, "context_mode": {"anchor_only"},
	})
	testutil.MustStatus(t, resp, http.StatusSeeOther)
	loc := testutil.Location(resp)

	plans, _ := st.ListSuperShotRequestsForVideo(t.Context(), vid.ID)
	if len(plans) != 1 {
		t.Fatalf("expected one plan, got %d", len(plans))
	}
	if plans[0].PlannerStatus != "validated" {
		t.Fatalf("planner should have validated the plan, got status %q", plans[0].PlannerStatus)
	}
	body := testutil.MustStatus(t, c.Get(loc), http.StatusOK)
	if !strings.Contains(body, "beside the chair, beginning to turn") {
		t.Fatal("planner-provided state should appear in the plan")
	}
	if strings.Contains(body, "PLACEHOLDER") {
		t.Fatal("a validated plan must not contain placeholders")
	}

	// Approval now queues the intermediate anchors.
	testutil.MustStatus(t, c.PostForm(loc+"/approve", url.Values{"revision": {itoa(plans[0].Revision)}}), http.StatusSeeOther)
	jobs, _ := st.ListActiveJobs(t.Context())
	anchors := 0
	for _, j := range jobs {
		if j.JobType == "generate_anchor" {
			anchors++
		}
	}
	if anchors < 1 {
		t.Fatalf("approval should queue anchors, got %d", anchors)
	}
}

// TestSuperShotNoPlannerBlocksApproval proves C02/T02/T10: with no planner the
// draft is visibly incomplete, no generation is queued, and approval is refused.
func TestSuperShotNoPlannerBlocksApproval(t *testing.T) {
	st := testutil.NewStore(t)
	cfg := config.Default()
	cfg.LLM.Enabled = false
	cfg.LLM.BaseURL = "http://127.0.0.1:1"
	h, _ := webapp.NewServer(t, st, cfg)
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, vid := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "NoPlanner")
	_, _, shot := testutil.MustShot(t, st, vid.ID, admin.ID, "Shot")
	versionID := seedVideoWorkflow(t, st, "ss-noplan", superShotManifest)
	approveEndpoints(t, st, shot, ws.ID, project.ID, admin.ID)

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + vid.ID
	resp := c.PostForm(base+"/super-shots/plan", url.Values{
		"shot_id": {shot.ID}, "duration_s": {"15"}, "workflow_id": {versionID},
		"positive": {"walk and sit"}, "strategy": {"auto"}, "context_mode": {"anchor_only"},
	})
	testutil.MustStatus(t, resp, http.StatusSeeOther)
	loc := testutil.Location(resp)

	// The draft is visibly incomplete and nothing is dispatched (T10).
	if jobs, _ := st.ListActiveJobs(t.Context()); len(jobs) != 0 {
		t.Fatalf("creating an incomplete draft must not dispatch work, got %d jobs", len(jobs))
	}
	plans, _ := st.ListSuperShotRequestsForVideo(t.Context(), vid.ID)
	if len(plans) != 1 {
		t.Fatalf("no plan")
	}
	anchors, _ := st.ListAnchors(t.Context(), plans[0].ID)
	foundPlaceholder := false
	for _, a := range anchors {
		if strings.HasPrefix(a.StatePrompt, "PLACEHOLDER") {
			foundPlaceholder = true
		}
	}
	if !foundPlaceholder {
		t.Fatal("an incomplete draft must visibly contain placeholder states")
	}

	// Approval is refused with the real reason, and still nothing is queued.
	resp = c.PostForm(loc+"/approve", url.Values{"revision": {itoa(plans[0].Revision)}})
	testutil.MustStatus(t, resp, http.StatusSeeOther)
	if !strings.Contains(testutil.Location(resp), "kind=error") {
		t.Fatalf("approval of a placeholder plan must fail, got %q", testutil.Location(resp))
	}
	if jobs, _ := st.ListActiveJobs(t.Context()); len(jobs) != 0 {
		t.Fatalf("a refused approval must not queue work, got %d jobs", len(jobs))
	}
}

// TestSuperShotStaleApprovalConflicts proves C07/T12: approving a revision other
// than the displayed one conflicts and queues nothing.
func TestSuperShotStaleApprovalConflicts(t *testing.T) {
	st := testutil.NewStore(t)
	h, _ := webapp.NewServer(t, st, config.Default())
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, vid := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Stale")
	_, _, shot := testutil.MustShot(t, st, vid.ID, admin.ID, "Shot")
	versionID := seedVideoWorkflow(t, st, "ss-stale", superShotManifest)
	approveEndpoints(t, st, shot, ws.ID, project.ID, admin.ID)

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + vid.ID
	resp := c.PostForm(base+"/super-shots/plan", url.Values{
		"shot_id": {shot.ID}, "duration_s": {"15"}, "workflow_id": {versionID},
		"positive": {"walk"}, "strategy": {"auto"}, "context_mode": {"anchor_only"},
		"intermediate_states": {"a\nb\nc"},
	})
	testutil.MustStatus(t, resp, http.StatusSeeOther)
	loc := testutil.Location(resp)

	resp = c.PostForm(loc+"/approve", url.Values{"revision": {"999999"}})
	testutil.MustStatus(t, resp, http.StatusSeeOther)
	if !strings.Contains(testutil.Location(resp), "kind=error") {
		t.Fatalf("a stale approval must conflict, got %q", testutil.Location(resp))
	}
	if jobs, _ := st.ListActiveJobs(t.Context()); len(jobs) != 0 {
		t.Fatalf("a conflicted approval must not queue work, got %d jobs", len(jobs))
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}