package web_test

import (
	"net/http"
	"strings"
	"testing"

	"vidbuild/internal/config"
	"vidbuild/internal/testutil"
	"vidbuild/internal/testutil/webapp"
)

func TestPWAAssetsAndShell(t *testing.T) {
	st := testutil.NewStore(t)
	cfg := config.Default()
	cfg.LLM.Enabled = false
	h, _ := webapp.NewServer(t, st, cfg)
	c := testutil.NewClient(t, h)

	manifest := testutil.MustStatus(t, c.Get("/manifest.webmanifest"), http.StatusOK)
	if !strings.Contains(manifest, "vid-build") || !strings.Contains(manifest, "start_url") {
		t.Fatalf("manifest looks wrong: %.200s", manifest)
	}
	sw := testutil.MustStatus(t, c.Get("/sw.js"), http.StatusOK)
	if !strings.Contains(sw, "vidbuild-shell") {
		t.Fatalf("service worker missing cache name")
	}
	offline := testutil.MustStatus(t, c.Get("/offline"), http.StatusOK)
	if !strings.Contains(offline, "offline") {
		t.Fatalf("offline page missing")
	}

	c.LoginAdmin()
	page := testutil.MustStatus(t, c.Get("/account"), http.StatusOK)
	for _, want := range []string{"/manifest.webmanifest", "/static/pwa.js", "/static/drafts.js", "net-indicator"} {
		if !strings.Contains(page, want) {
			t.Fatalf("shell missing %q", want)
		}
	}
}
