package web_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"vidbuild/internal/config"
	"vidbuild/internal/store"
	"vidbuild/internal/testutil"
	"vidbuild/internal/testutil/webapp"
)

func TestAnimaticPageAndData(t *testing.T) {
	st := testutil.NewStore(t)
	cfg := config.Default()
	cfg.LLM.Enabled = false
	h, _ := webapp.NewServer(t, st, cfg)

	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Animatic")
	_, scene, shotA := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")
	if _, err := st.CreateShot(t.Context(), scene.ID, admin.ID, "Shot B", "p", "t", 2000); err != nil {
		t.Fatalf("create shot B: %v", err)
	}
	if _, err := st.CreateCue(t.Context(), shotA.ID, "dialogue", "", "We should go.", "whisper", "fear", 500, 1200); err != nil {
		t.Fatalf("create cue: %v", err)
	}
	dramatic := 60
	if _, err := st.SetMetric(t.Context(), video.ID, "shot", shotA.ID, store.MetricValues{DramaticIntensity: &dramatic}, "", admin.ID); err != nil {
		t.Fatalf("set metric: %v", err)
	}

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + video.ID

	page := testutil.MustStatus(t, c.Get(base+"/animatic"), http.StatusOK)
	for _, want := range []string{"Animatic", "Play", "Playback settings", "Add cue", "Narrative metrics"} {
		if !strings.Contains(page, want) {
			t.Fatalf("animatic page missing %q", want)
		}
	}

	resp := c.Get(base + "/animatic/data")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("animatic data status = %d", resp.StatusCode)
	}
	var doc struct {
		VideoID         string `json:"video_id"`
		TotalDurationMs int    `json:"total_duration_ms"`
		Shots           []struct {
			ID            string `json:"id"`
			DurationMs    int    `json:"duration_ms"`
			StartBlocking struct {
				Version int `json:"version"`
			} `json:"start_blocking"`
			Cues []struct {
				CueType string `json:"cue_type"`
				Content string `json:"content"`
			} `json:"cues"`
		} `json:"shots"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode animatic data: %v", err)
	}
	resp.Body.Close()
	if doc.VideoID != video.ID || len(doc.Shots) != 2 {
		t.Fatalf("unexpected animatic document: %+v", doc)
	}
	if doc.TotalDurationMs <= 0 {
		t.Fatalf("total duration should be positive")
	}
	if len(doc.Shots[0].Cues) != 1 || doc.Shots[0].Cues[0].Content != "We should go." {
		t.Fatalf("cue missing from playback document: %+v", doc.Shots[0].Cues)
	}
	if doc.Shots[0].StartBlocking.Version != 2 {
		t.Fatalf("playback blocking should be schema v2")
	}
}

func TestAnimaticSettingsApproveCuesMetrics(t *testing.T) {
	st := testutil.NewStore(t)
	cfg := config.Default()
	cfg.LLM.Enabled = false
	h, _ := webapp.NewServer(t, st, cfg)

	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Animatic")
	_, _, shot := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")

	c := testutil.NewClient(t, h)
	c.LoginAdmin()
	base := "/w/" + ws.ID + "/projects/" + project.ID + "/videos/" + video.ID

	// Cue create/update/delete.
	resp := c.PostForm(base+"/shots/"+shot.ID+"/cues", url.Values{
		"cue_type": {"dialogue"}, "content": {"Hello"}, "start_ms": {"100"}, "duration_ms": {"900"},
	})
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=success") {
		t.Fatalf("create cue redirect = %q", loc)
	}
	_ = resp.Body.Close()
	cues, _ := st.ListCues(t.Context(), shot.ID)
	if len(cues) != 1 {
		t.Fatalf("expected 1 cue, got %d", len(cues))
	}
	resp = c.Patch(base+"/cues/"+cues[0].ID, url.Values{
		"content": {"Hello there"}, "start_ms": {"200"}, "duration_ms": {"800"}, "revision": {"1"},
	})
	testutil.MustStatus(t, resp, http.StatusSeeOther)

	// Settings with optimistic revision.
	resp = c.Request(http.MethodPatch, base+"/animatic/settings", url.Values{
		"speed": {"1.5"}, "captions": {"on"}, "loop_mode": {"scene"}, "revision": {"1"},
	}, nil)
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=success") {
		t.Fatalf("settings redirect = %q", loc)
	}
	_ = resp.Body.Close()
	anim, _ := st.GetAnimaticForVideo(t.Context(), video.ID)
	if !strings.Contains(anim.SettingsJSON, "1.5") {
		t.Fatalf("settings not saved: %s", anim.SettingsJSON)
	}

	// Invalid speed rejected.
	resp = c.Request(http.MethodPatch, base+"/animatic/settings", url.Values{"speed": {"99"}, "revision": {strconv.Itoa(anim.Revision)}}, nil)
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=error") {
		t.Fatalf("invalid speed should be rejected, redirect = %q", loc)
	}
	_ = resp.Body.Close()

	// Metrics.
	resp = c.PostForm(base+"/metrics", url.Values{
		"entity_type": {"shot"}, "entity_id": {shot.ID}, "dramatic_intensity": {"75"}, "emotional_valence": {"-20"},
	})
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=success") {
		t.Fatalf("metrics redirect = %q", loc)
	}
	_ = resp.Body.Close()
	m, err := st.GetCurrentMetric(t.Context(), "shot", shot.ID)
	if err != nil || !m.DramaticIntensity.Valid || m.DramaticIntensity.Int64 != 75 {
		t.Fatalf("metric not saved: %+v (%v)", m, err)
	}

	// Approve.
	resp = c.PostForm(base+"/animatic/approve", url.Values{})
	if loc := testutil.Location(resp); !strings.Contains(loc, "kind=success") {
		t.Fatalf("approve redirect = %q", loc)
	}
	_ = resp.Body.Close()
	anim, _ = st.GetAnimaticForVideo(t.Context(), video.ID)
	if anim.Status != "approved" {
		t.Fatalf("animatic status = %q, want approved", anim.Status)
	}
}
