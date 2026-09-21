// Package testutil provides shared test-only helpers: an in-memory-ish SQLite
// store, an HTTP client with a cookie jar and CSRF handling, and fake LM Studio
// and ComfyUI servers so tests never depend on external services.
package testutil

import (
	"path/filepath"
	"testing"

	"vidbuild/internal/auth"
	"vidbuild/internal/db"
	"vidbuild/internal/store"
)

// AdminEmail and AdminPassword are the seeded bootstrap credentials.
const (
	AdminEmail    = "admin@test.local"
	AdminPassword = "test-password"
)

// NewStore opens a fresh migrated database in a temp directory, seeds the
// bootstrap admin + personal workspace, and returns a store. The database is
// closed automatically at test cleanup.
func NewStore(t *testing.T) *store.Store {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"), 5000, 4)
	if err != nil {
		t.Fatalf("testutil: open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	params := auth.DefaultParams(65536, 2, 2)
	hash, err := auth.HashPassword(AdminPassword, params)
	if err != nil {
		t.Fatalf("testutil: hash password: %v", err)
	}
	if err := d.SeedBootstrap(AdminEmail, hash, "Test Admin"); err != nil {
		t.Fatalf("testutil: seed bootstrap: %v", err)
	}
	return store.New(d)
}

// MustAdmin returns the seeded admin user.
func MustAdmin(t *testing.T, st *store.Store) *store.User {
	t.Helper()
	u, err := st.GetUserByEmail(t.Context(), AdminEmail)
	if err != nil {
		t.Fatalf("testutil: admin lookup: %v", err)
	}
	return u
}

// MustWorkspace returns the admin's personal workspace.
func MustWorkspace(t *testing.T, st *store.Store) *store.Workspace {
	t.Helper()
	admin := MustAdmin(t, st)
	ws, _, err := st.ListWorkspacesForUser(t.Context(), admin.ID)
	if err != nil || len(ws) == 0 {
		t.Fatalf("testutil: workspace lookup: %v", err)
	}
	return &ws[0]
}

// MustUser creates an active non-admin user with the shared test password.
func MustUser(t *testing.T, st *store.Store, email, name string) *store.User {
	t.Helper()
	hash, err := auth.HashPassword(AdminPassword, auth.DefaultParams(65536, 2, 2))
	if err != nil {
		t.Fatalf("testutil: hash password: %v", err)
	}
	u, err := st.CreateUser(t.Context(), email, name, hash, false)
	if err != nil {
		t.Fatalf("testutil: create user: %v", err)
	}
	return u
}

// MustMember adds a user to a workspace with the given role.
func MustMember(t *testing.T, st *store.Store, workspaceID, userID, role, invitedBy string) {
	t.Helper()
	if err := st.AddMember(t.Context(), workspaceID, userID, role, invitedBy); err != nil {
		t.Fatalf("testutil: add member: %v", err)
	}
}

// MustProjectVideo creates a project with its first video and returns both.
func MustProjectVideo(t *testing.T, st *store.Store, workspaceID, userID, title string) (*store.Project, *store.Video) {
	t.Helper()
	p, v, err := st.CreateProjectWithVideo(t.Context(), workspaceID, userID, title, "", title+" video", "test purpose", "landscape")
	if err != nil {
		t.Fatalf("testutil: create project: %v", err)
	}
	return p, v
}

// MustShot creates one scene with one implicit-container-free shot and returns
// the shot id. (Beats arrive in Phase 1; this helper targets Phase 0 tests.)
func MustShot(t *testing.T, st *store.Store, videoID, userID, title string) (*store.Section, *store.Scene, *store.Shot) {
	t.Helper()
	root, err := st.GetRootSection(t.Context(), videoID)
	if err != nil {
		t.Fatalf("testutil: root section: %v", err)
	}
	scene, err := st.CreateScene(t.Context(), root.ID, "Scene", "", "purpose", "moderate", "", 5000)
	if err != nil {
		t.Fatalf("testutil: create scene: %v", err)
	}
	shot, err := st.CreateShot(t.Context(), scene.ID, userID, title, "purpose", "treatment", 3000)
	if err != nil {
		t.Fatalf("testutil: create shot: %v", err)
	}
	return root, scene, shot
}
