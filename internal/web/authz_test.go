package web_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"vidbuild/internal/config"
	"vidbuild/internal/testutil"
	"vidbuild/internal/testutil/webapp"
)

// authzCase is one mutation route family and the roles permitted to use it.
type authzCase struct {
	name   string
	method string
	path   string
	allow  []string
}

func roleAllowed(allow []string, role string) bool {
	for _, a := range allow {
		if a == role {
			return true
		}
	}
	return false
}

// TestAuthorizationMatrix proves every mutation family is role-gated, not just
// CSRF-protected: owner/editor creative work, reviewer review-only work, and
// owner-only administration. Viewers, non-members and anonymous users are
// rejected; hiding buttons is not authorization.
func TestAuthorizationMatrix(t *testing.T) {
	st := testutil.NewStore(t)
	cfg := config.Default()
	cfg.LLM.Enabled = false
	cfg.ComfyUI.Enabled = false
	h, _ := webapp.NewServer(t, st, cfg)

	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Authz")
	root, scene, shot := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")

	wsBase := "/w/" + ws.ID
	videoBase := fmt.Sprintf("%s/projects/%s/videos/%s", wsBase, project.ID, video.ID)

	clients := map[string]*testutil.Client{}
	owner := testutil.NewClient(t, h)
	owner.LoginAdmin()
	clients["owner"] = owner
	for _, role := range []string{"editor", "reviewer", "viewer"} {
		email := role + "@authz.local"
		u := testutil.MustUser(t, st, email, role)
		testutil.MustMember(t, st, ws.ID, u.ID, role, admin.ID)
		c := testutil.NewClient(t, h)
		c.Login(email, testutil.AdminPassword)
		clients[role] = c
	}
	stranger := testutil.MustUser(t, st, "stranger@authz.local", "Stranger")
	nm := testutil.NewClient(t, h)
	nm.Login(stranger.Email, testutil.AdminPassword)
	clients["nonmember"] = nm
	anon := testutil.NewClient(t, h)

	edit := []string{"owner", "editor"}
	review := []string{"owner", "editor", "reviewer"}
	ownerOnly := []string{"owner"}

	v := func(p string) string { return videoBase + p }
	w := func(p string) string { return wsBase + p }

	cases := []authzCase{
		{"create thread", "POST", v("/discovery/threads"), edit},
		{"propose brief", "POST", v("/proposals/brief"), edit},
		{"propose outline", "POST", v("/proposals/outline"), edit},
		{"apply proposal", "POST", v("/proposals/missing/apply"), edit},
		{"reject proposal", "POST", v("/proposals/missing/reject"), edit},
		{"regenerate proposal", "POST", v("/proposals/missing/regenerate"), edit},
		{"revise brief", "POST", v("/brief/revise"), edit},
		{"create section", "POST", v("/sections"), edit},
		{"update section", "PATCH", v("/sections/missing"), edit},
		{"delete section", "DELETE", v("/sections/missing"), edit},
		{"move section", "POST", v("/sections/missing/move"), edit},
		{"create scene", "POST", v("/sections/" + root.ID + "/scenes"), edit},
		{"update scene", "PATCH", v("/scenes/" + scene.ID), edit},
		{"delete scene", "DELETE", v("/scenes/missing"), edit},
		{"move scene", "POST", v("/scenes/" + scene.ID + "/move"), edit},
		{"create shot", "POST", v("/scenes/" + scene.ID + "/shots"), edit},
		{"create beat", "POST", v("/scenes/" + scene.ID + "/beats"), edit},
		{"update beat", "PATCH", v("/beats/missing"), edit},
		{"delete beat", "DELETE", v("/beats/missing"), edit},
		{"move beat", "POST", v("/beats/missing/move"), edit},
		{"propose beats", "POST", v("/scenes/" + scene.ID + "/propose-beats"), edit},
		{"propose shots", "POST", v("/beats/missing/propose-shots"), edit},
		{"update shot", "PATCH", v("/shots/" + shot.ID), edit},
		{"delete shot", "DELETE", v("/shots/missing"), edit},
		{"move shot", "POST", v("/shots/" + shot.ID + "/move"), edit},
		{"update spec", "PATCH", v("/shots/" + shot.ID + "/specs/image"), edit},
		{"suggest spec", "POST", v("/shots/" + shot.ID + "/specs/image/suggest"), edit},
		{"suggest blocking", "POST", v("/shots/" + shot.ID + "/blocking/suggest"), edit},
		{"save blocking", "PATCH", v("/shots/" + shot.ID + "/blocking/start"), edit},
		{"save shot state", "PATCH", v("/shots/" + shot.ID + "/state/end"), edit},
		{"save transition", "PUT", v("/shots/" + shot.ID + "/transition-out"), edit},
		{"derive next", "POST", v("/shots/" + shot.ID + "/transition-out/derive-next"), edit},
		{"copy blocking", "POST", v("/shots/" + shot.ID + "/blocking/start/copy"), edit},
		{"save prompt", "POST", v("/shots/" + shot.ID + "/prompt-workbench"), edit},
		{"prompt preview", "POST", v("/shots/" + shot.ID + "/prompt-preview"), edit},
		{"create generation request", "POST", v("/generation-requests"), edit},
		{"select asset", "POST", v("/shots/" + shot.ID + "/selections"), edit},
		{"add edit item", "POST", v("/edits/missing/items"), edit},
		{"update edit item", "PATCH", v("/edit-items/missing"), edit},
		{"move edit item", "POST", v("/edit-items/missing/move"), edit},
		{"delete edit item", "DELETE", v("/edit-items/missing"), edit},
		{"validate edit", "POST", v("/edits/missing/validate"), edit},
		{"create export", "POST", v("/edits/missing/exports"), edit},
		{"create campaign", "POST", v("/campaigns"), edit},
		{"launch campaign", "POST", v("/campaigns/missing/launch"), edit},
		{"pause campaign", "POST", v("/campaigns/missing/pause"), edit},
		{"create annotation", "POST", v("/annotations"), review},
		{"update annotation", "PATCH", v("/annotations/missing"), review},
		{"approve asset", "POST", v("/assets/missing/approvals"), review},
		{"assess asset", "POST", v("/assets/missing/assessments"), review},
		{"create target", "POST", w("/execution-targets"), ownerOnly},
		{"add member", "POST", w("/members"), ownerOnly},
		{"update member", "PATCH", w("/members/missing"), ownerOnly},
		{"remove member", "DELETE", w("/members/missing"), ownerOnly},
		{"enable lan", "POST", w("/settings/lan/enable"), ownerOnly},
		{"disable lan", "POST", w("/settings/lan/disable"), ownerOnly},
		{"regenerate lan", "POST", w("/settings/lan/regenerate"), ownerOnly},
		{"queue backup", "POST", w("/diagnostics/backup"), ownerOnly},
		{"verify backup", "POST", w("/diagnostics/backups/verify"), ownerOnly},
		{"backfill thumbnails", "POST", w("/diagnostics/thumbnails"), ownerOnly},
		{"import workflow", "POST", w("/workflows/import"), edit},
		{"bind workflow", "PATCH", w("/workflow-versions/missing/bindings"), edit},
		{"validate workflow", "POST", w("/workflow-versions/missing/validate"), edit},
		{"activate workflow", "POST", w("/workflow-versions/missing/activate"), edit},
		{"retire workflow", "POST", w("/workflow-versions/missing/retire"), edit},
		{"create model profile", "POST", w("/model-profiles"), edit},
		{"delete model profile", "DELETE", w("/model-profiles/missing"), edit},
		{"set default profile", "POST", w("/projects/" + project.ID + "/default-profile"), edit},
		{"create character", "POST", w("/projects/" + project.ID + "/characters"), edit},
		{"create location", "POST", w("/projects/" + project.ID + "/locations"), edit},
		{"create style rule", "POST", w("/projects/" + project.ID + "/style-rules"), edit},
		{"create project", "POST", w("/projects"), edit},
		{"create video", "POST", w("/projects/" + project.ID + "/videos"), edit},
		{"queue package", "POST", w("/projects/" + project.ID + "/package"), edit},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			for _, actor := range []string{"owner", "editor", "reviewer", "viewer", "nonmember"} {
				wantAllowed := roleAllowed(tc.allow, actor)
				resp := authzDo(clients[actor], tc.method, tc.path)
				code := resp.StatusCode
				body := testutil.Body(t, resp)
				switch {
				case wantAllowed && code == http.StatusForbidden:
					t.Errorf("%s %s as %s = 403, want allowed (body=%.160s)", tc.method, tc.path, actor, body)
				case !wantAllowed && code != http.StatusForbidden:
					t.Errorf("%s %s as %s = %d, want 403 (body=%.160s)", tc.method, tc.path, actor, code, body)
				}
			}

			resp := authzDo(anon, tc.method, tc.path)
			code := resp.StatusCode
			body := testutil.Body(t, resp)
			if code != http.StatusSeeOther {
				t.Errorf("%s %s anonymous = %d, want 303 to /login (body=%.160s)", tc.method, tc.path, code, body)
			}
		})
	}
}

func authzDo(c *testutil.Client, method, path string) *http.Response {
	switch method {
	case http.MethodPost:
		return c.PostForm(path, url.Values{})
	case http.MethodPatch:
		return c.Patch(path, url.Values{})
	case http.MethodDelete:
		return c.Delete(path)
	case http.MethodPut:
		return c.Request(http.MethodPut, path, url.Values{}, nil)
	default:
		return c.Get(path)
	}
}
