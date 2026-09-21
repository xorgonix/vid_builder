package store_test

import (
	"testing"

	"vidbuild/internal/testutil"
)

func TestShotStateVersions(t *testing.T) {
	st := testutil.NewStore(t)
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	_, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Continuity")
	_, _, shot := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")

	first, err := st.SaveShotState(t.Context(), shot.ID, "end", `{"setting":"kitchen"}`, "human", admin.ID)
	if err != nil {
		t.Fatalf("save state: %v", err)
	}
	if first.Version != 1 {
		t.Fatalf("first version = %d, want 1", first.Version)
	}
	second, err := st.SaveShotState(t.Context(), shot.ID, "end", `{"setting":"hallway"}`, "human", admin.ID)
	if err != nil {
		t.Fatalf("save state: %v", err)
	}
	if second.Version != 2 {
		t.Fatalf("second version = %d, want 2", second.Version)
	}

	current, err := st.GetCurrentShotState(t.Context(), shot.ID, "end")
	if err != nil {
		t.Fatalf("current state: %v", err)
	}
	if current.ID != second.ID {
		t.Fatalf("current state is not the latest version")
	}

	versions, err := st.ListShotStates(t.Context(), shot.ID, "end")
	if err != nil || len(versions) != 2 {
		t.Fatalf("versions = %d (err=%v), want 2", len(versions), err)
	}
	if versions[0].Version != 2 {
		t.Fatalf("versions should be newest first")
	}

	if _, err := st.GetCurrentShotState(t.Context(), shot.ID, "start"); err == nil {
		t.Fatal("expected no start state")
	}
	if _, err := st.SaveShotState(t.Context(), shot.ID, "middle", "{}", "human", admin.ID); err == nil {
		t.Fatal("invalid endpoint should be rejected")
	}
}

func TestShotLinkUpsertAndNeighbours(t *testing.T) {
	st := testutil.NewStore(t)
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	_, video := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Continuity")
	_, scene, shotA := testutil.MustShot(t, st, video.ID, admin.ID, "Shot A")
	beat, _ := st.EnsureImplicitBeat(t.Context(), scene.ID)
	shotB, err := st.CreateShotInBeat(t.Context(), beat.ID, admin.ID, "Shot B", "p", "t", 1000)
	if err != nil {
		t.Fatalf("create shot B: %v", err)
	}
	shotC, err := st.CreateShotInBeat(t.Context(), beat.ID, admin.ID, "Shot C", "p", "t", 1000)
	if err != nil {
		t.Fatalf("create shot C: %v", err)
	}

	if _, err := st.UpsertShotLink(t.Context(), shotA.ID, shotB.ID, "continues_into", `{"transition_type":"reaction_cut"}`); err != nil {
		t.Fatalf("upsert link: %v", err)
	}
	if _, err := st.UpsertShotLink(t.Context(), shotA.ID, shotB.ID, "continues_into", `{"transition_type":"hard_cut"}`); err != nil {
		t.Fatalf("update link: %v", err)
	}
	link, err := st.GetShotLink(t.Context(), shotA.ID, shotB.ID)
	if err != nil {
		t.Fatalf("get link: %v", err)
	}
	if link.MetadataJSON != `{"transition_type":"hard_cut"}` {
		t.Fatalf("link not updated: %s", link.MetadataJSON)
	}

	ordered, err := st.ListVideoShotsOrdered(t.Context(), video.ID)
	if err != nil {
		t.Fatalf("ordered shots: %v", err)
	}
	if len(ordered) != 3 || ordered[0].ID != shotA.ID || ordered[2].ID != shotC.ID {
		t.Fatalf("unexpected order: %+v", ordered)
	}

	prev, next, err := st.NeighbourShots(t.Context(), shotB.ID, video.ID)
	if err != nil {
		t.Fatalf("neighbours: %v", err)
	}
	if prev == nil || prev.ID != shotA.ID || next == nil || next.ID != shotC.ID {
		t.Fatalf("unexpected neighbours: prev=%v next=%v", prev, next)
	}

	_, next, _ = st.NeighbourShots(t.Context(), shotC.ID, video.ID)
	if next != nil {
		t.Fatalf("last shot should have no next")
	}
}
