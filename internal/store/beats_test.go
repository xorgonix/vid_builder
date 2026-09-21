package store_test

import (
	"errors"
	"testing"

	"vidbuild/internal/store"
	"vidbuild/internal/testutil"
)

func TestImplicitBeatAndCreateShot(t *testing.T) {
	st := testutil.NewStore(t)
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	_, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Beats")
	_, scene, shot := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")

	beats, err := st.ListBeats(t.Context(), scene.ID)
	if err != nil {
		t.Fatalf("list beats: %v", err)
	}
	if len(beats) != 1 || !beats[0].Implicit {
		t.Fatalf("expected one implicit beat, got %+v", beats)
	}
	if shot.BeatID != beats[0].ID {
		t.Fatalf("shot beat = %q, want %q", shot.BeatID, beats[0].ID)
	}

	again, err := st.EnsureImplicitBeat(t.Context(), scene.ID)
	if err != nil {
		t.Fatalf("ensure implicit: %v", err)
	}
	if again.ID != beats[0].ID {
		t.Fatalf("EnsureImplicitBeat created a second beat")
	}
}

func TestCreateBeatPromotesImplicit(t *testing.T) {
	st := testutil.NewStore(t)
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	_, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Beats")
	_, scene, _ := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")

	if _, err := st.CreateBeat(t.Context(), scene.ID, "Beat 2", "p", "trigger", "action", "reaction", "state", "effect", 3000); err != nil {
		t.Fatalf("create beat: %v", err)
	}
	beats, _ := st.ListBeats(t.Context(), scene.ID)
	if len(beats) != 2 {
		t.Fatalf("expected 2 beats, got %d", len(beats))
	}
	for _, b := range beats {
		if b.Implicit {
			t.Fatalf("implicit beat should be promoted once explicit beats exist: %+v", b)
		}
	}
}

func TestCreateBeatValidation(t *testing.T) {
	st := testutil.NewStore(t)
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	_, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Beats")
	_, scene, _ := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")

	if _, err := st.CreateBeat(t.Context(), scene.ID, "  ", "", "", "", "", "", "", 0); err == nil {
		t.Fatal("blank title should be rejected")
	}
	if _, err := st.CreateBeat(t.Context(), scene.ID, "Beat", "", "", "", "", "", "", -1); err == nil {
		t.Fatal("negative duration should be rejected")
	}
}

func TestUpdateAndMoveBeat(t *testing.T) {
	st := testutil.NewStore(t)
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	_, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Beats")
	_, scene, _ := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")

	b1, _ := st.EnsureImplicitBeat(t.Context(), scene.ID)
	b2, err := st.CreateBeat(t.Context(), scene.ID, "Second", "", "", "", "", "", "", 1000)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := st.UpdateBeat(t.Context(), b1.ID, "Renamed", "purpose", "t", "a", "r", "s", "e", 2000, b1.Revision); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := st.UpdateBeat(t.Context(), b1.ID, "Stale", "", "", "", "", "", "", 0, b1.Revision); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected revision conflict, got %v", err)
	}

	before, _ := st.ListBeats(t.Context(), scene.ID)
	if err := st.MoveBeat(t.Context(), b2.ID, -1); err != nil {
		t.Fatalf("move: %v", err)
	}
	after, _ := st.ListBeats(t.Context(), scene.ID)
	if after[0].ID != b2.ID || after[1].ID != before[0].ID {
		t.Fatalf("move did not reorder: %+v", after)
	}
}

func TestDeleteBeatRules(t *testing.T) {
	st := testutil.NewStore(t)
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	_, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Beats")
	_, scene, shot := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")

	beat, _ := st.EnsureImplicitBeat(t.Context(), scene.ID)
	if err := st.DeleteBeat(t.Context(), beat.ID); !errors.Is(err, store.ErrLastBeat) {
		t.Fatalf("expected ErrLastBeat, got %v", err)
	}

	withShot, _ := st.CreateBeat(t.Context(), scene.ID, "Has shot", "", "", "", "", "", "", 0)
	if _, err := st.CreateShotInBeat(t.Context(), withShot.ID, admin.ID, "In beat", "", "", 1000); err != nil {
		t.Fatalf("create shot in beat: %v", err)
	}
	if err := st.DeleteBeat(t.Context(), withShot.ID); !errors.Is(err, store.ErrBeatHasShots) {
		t.Fatalf("expected ErrBeatHasShots, got %v", err)
	}

	empty, _ := st.CreateBeat(t.Context(), scene.ID, "Empty", "", "", "", "", "", "", 0)
	if err := st.DeleteBeat(t.Context(), empty.ID); err != nil {
		t.Fatalf("delete empty beat: %v", err)
	}
	_ = shot
}

func TestSceneBeatsDerivation(t *testing.T) {
	st := testutil.NewStore(t)
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	_, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Beats")
	_, scene, _ := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")

	beat, _ := st.EnsureImplicitBeat(t.Context(), scene.ID)
	if _, err := st.CreateShotInBeat(t.Context(), beat.ID, admin.ID, "Extra", "p", "t", 1000); err != nil {
		t.Fatalf("create shot: %v", err)
	}
	tree, err := st.SceneBeats(t.Context(), scene.ID)
	if err != nil {
		t.Fatalf("scene beats: %v", err)
	}
	if len(tree) != 1 || len(tree[0].Shots) != 2 {
		t.Fatalf("expected 1 beat with 2 shots, got %+v", tree)
	}
}
