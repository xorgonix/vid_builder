package web_test

import (
	"net/http"
	"strings"
	"testing"

	"vidbuild/internal/testutil"
	"vidbuild/internal/testutil/webapp"
)

// TestHarnessLoginFlow proves the shared harness works end to end: an
// unauthenticated page request redirects to login, and the seeded admin can
// sign in and load the workspace list.
func TestHarnessLoginFlow(t *testing.T) {
	st := testutil.NewStore(t)
	h, _ := webapp.NewServer(t, st, nil)
	c := testutil.NewClient(t, h)

	resp := c.Get("/workspaces")
	testutil.MustStatus(t, resp, http.StatusSeeOther)
	if loc := testutil.Location(resp); loc != "/login" {
		t.Fatalf("unauthenticated redirect = %q, want /login", loc)
	}

	c.LoginAdmin()

	body := testutil.MustStatus(t, c.Get("/workspaces"), http.StatusOK)
	if !strings.Contains(body, "Your workspaces") {
		t.Fatalf("workspace list page did not render after login")
	}
}

func TestHarnessHealthEndpoints(t *testing.T) {
	st := testutil.NewStore(t)
	h, _ := webapp.NewServer(t, st, nil)
	c := testutil.NewClient(t, h)

	if body := testutil.MustStatus(t, c.Get("/healthz"), http.StatusOK); !strings.Contains(body, "ok") {
		t.Fatalf("healthz body = %q", body)
	}
	if body := testutil.MustStatus(t, c.Get("/readyz"), http.StatusOK); !strings.Contains(body, "ready") {
		t.Fatalf("readyz body = %q", body)
	}
}

func TestHarnessHTMXUnauthenticatedReturns401(t *testing.T) {
	st := testutil.NewStore(t)
	h, _ := webapp.NewServer(t, st, nil)
	c := testutil.NewClient(t, h)

	testutil.MustStatus(t, c.GetHX("/workspaces"), http.StatusUnauthorized)
}
