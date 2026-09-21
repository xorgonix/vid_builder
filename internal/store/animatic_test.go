package store_test

import (
	"errors"
	"testing"

	"vidbuild/internal/store"
	"vidbuild/internal/testutil"
)

func TestCueCRUDAndMove(t *testing.T) {
	st := testutil.NewStore(t)
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	_, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Cues")
	_, _, shot := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")

	c1, err := st.CreateCue(t.Context(), shot.ID, "dialogue", "", "We should go.", "whisper", "fear", 500, 1200)
	if err != nil {
		t.Fatalf("create cue: %v", err)
	}
	c2, err := st.CreateCue(t.Context(), shot.ID, "sound", "", "door creaks", "", "", 0, 800)
	if err != nil {
		t.Fatalf("create cue: %v", err)
	}
	if _, err := st.CreateCue(t.Context(), shot.ID, "singing", "", "", "", "", 0, 0); err == nil {
		t.Fatal("unknown cue type should be rejected")
	}
	if _, err := st.CreateCue(t.Context(), shot.ID, "dialogue", "", "", "", "", -1, 0); err == nil {
		t.Fatal("negative start should be rejected")
	}

	if err := st.UpdateCue(t.Context(), c1.ID, "We must go.", "whisper", "fear", 600, 1200, c1.Revision); err != nil {
		t.Fatalf("update cue: %v", err)
	}
	if err := st.UpdateCue(t.Context(), c1.ID, "stale", "", "", 0, 0, c1.Revision); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}

	cues, _ := st.ListCues(t.Context(), shot.ID)
	if len(cues) != 2 {
		t.Fatalf("expected 2 cues, got %d", len(cues))
	}
	firstPos, secondPos := cues[0].Position, cues[1].Position
	if err := st.MoveCue(t.Context(), c2.ID, -1); err != nil {
		t.Fatalf("move cue: %v", err)
	}
	cues, _ = st.ListCues(t.Context(), shot.ID)
	if cues[0].ID != c2.ID || cues[0].Position != firstPos || cues[1].Position != secondPos {
		t.Fatalf("cue move did not reorder: %+v", cues)
	}

	if err := st.DeleteCue(t.Context(), c2.ID); err != nil {
		t.Fatalf("delete cue: %v", err)
	}
	if _, err := st.GetCue(t.Context(), c2.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted cue should be gone, got %v", err)
	}
}

func TestAnimaticLifecycle(t *testing.T) {
	st := testutil.NewStore(t)
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	_, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Animatic")

	a, err := st.EnsureAnimatic(t.Context(), video.ID, admin.ID)
	if err != nil {
		t.Fatalf("ensure animatic: %v", err)
	}
	if a.Status != "draft" {
		t.Fatalf("status = %q, want draft", a.Status)
	}
	again, err := st.EnsureAnimatic(t.Context(), video.ID, admin.ID)
	if err != nil || again.ID != a.ID {
		t.Fatalf("EnsureAnimatic should be idempotent")
	}

	if err := st.UpdateAnimaticSettings(t.Context(), video.ID, `{"speed":1.5}`, a.Revision); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if err := st.UpdateAnimaticSettings(t.Context(), video.ID, `{"speed":2}`, a.Revision); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected revision conflict, got %v", err)
	}
	if err := st.SetAnimaticStatus(t.Context(), video.ID, "approved"); err != nil {
		t.Fatalf("set status: %v", err)
	}
	if err := st.SetAnimaticStatus(t.Context(), video.ID, "nonsense"); err == nil {
		t.Fatal("invalid status should be rejected")
	}
	got, _ := st.GetAnimaticForVideo(t.Context(), video.ID)
	if got.Status != "approved" || got.SettingsJSON != `{"speed":1.5}` {
		t.Fatalf("animatic not updated: %+v", got)
	}
}

func TestNarrativeMetricsVersioning(t *testing.T) {
	st := testutil.NewStore(t)
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	_, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Metrics")
	_, _, shot := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")

	dramatic := 60
	tension := 40
	m, err := st.SetMetric(t.Context(), video.ID, "shot", shot.ID, store.MetricValues{DramaticIntensity: &dramatic, Tension: &tension}, "first pass", admin.ID)
	if err != nil {
		t.Fatalf("set metric: %v", err)
	}
	if m.Version != 1 || !m.DramaticIntensity.Valid || m.DramaticIntensity.Int64 != 60 {
		t.Fatalf("unexpected metric: %+v", m)
	}

	dramatic2 := 80
	m2, err := st.SetMetric(t.Context(), video.ID, "shot", shot.ID, store.MetricValues{DramaticIntensity: &dramatic2}, "", admin.ID)
	if err != nil {
		t.Fatalf("set metric: %v", err)
	}
	if m2.Version != 2 {
		t.Fatalf("version = %d, want 2", m2.Version)
	}
	cur, err := st.GetCurrentMetric(t.Context(), "shot", shot.ID)
	if err != nil {
		t.Fatalf("current metric: %v", err)
	}
	if cur.DramaticIntensity.Int64 != 80 {
		t.Fatalf("current dramatic = %d, want 80", cur.DramaticIntensity.Int64)
	}
	if cur.Tension.Valid {
		t.Fatalf("unset tension should stay unset (empty metrics are valid)")
	}

	bad := 150
	if _, err := st.SetMetric(t.Context(), video.ID, "shot", shot.ID, store.MetricValues{DramaticIntensity: &bad}, "", admin.ID); err == nil {
		t.Fatal("out-of-range metric should be rejected")
	}
	if _, err := st.SetMetric(t.Context(), video.ID, "video", shot.ID, store.MetricValues{}, "", admin.ID); err == nil {
		t.Fatal("invalid entity type should be rejected")
	}

	list, err := st.ListCurrentMetricsForVideo(t.Context(), video.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("current metrics = %d (err=%v), want 1", len(list), err)
	}
}
