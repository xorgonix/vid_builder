// Package store provides SQLite-backed repositories for vid-build 0.01.
package store

import (
	"database/sql"
	"errors"
	"time"

	"vidbuild/internal/db"
)

func nowTime() time.Time                  { return time.Now().UTC() }
func secondsDuration(s int) time.Duration { return time.Duration(s) * time.Second }

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("store: not found")

// ErrConflict is returned when an optimistic revision check fails.
var ErrConflict = errors.New("store: revision conflict")

// ErrForbidden is returned by scope checks.
var ErrForbidden = errors.New("store: forbidden")

// Store exposes repository methods over the shared SQLite database.
type Store struct {
	*db.DB
}

// New wraps an open database.
func New(d *db.DB) *Store { return &Store{d} }

// User is an authenticated human identity.
type User struct {
	ID            string
	Email         string
	DisplayName   string
	PasswordHash  string
	Status        string
	IsSystemAdmin bool
	CreatedAt     string
	UpdatedAt     string
}

// Session is a browser session; the raw token never lives here.
type Session struct {
	ID         string
	UserID     string
	CSRFSecret []byte
	CreatedAt  string
	ExpiresAt  string
	RevokedAt  sql.NullString
}

// Workspace is an ownership/permission boundary.
type Workspace struct {
	ID        string
	Name      string
	Slug      string
	Kind      string
	Revision  int
	CreatedAt string
	UpdatedAt string
}

// Membership links a user to a workspace with a role.
type Membership struct {
	WorkspaceID string
	UserID      string
	Role        string
}

// Project is a shared creative world.
type Project struct {
	ID          string
	WorkspaceID string
	Title       string
	Description string
	Status      string
	Revision    int
	CreatedAt   string
	UpdatedAt   string
}

// Video is one ordered deliverable inside a project.
type Video struct {
	ID               string
	ProjectID        string
	Title            string
	Purpose          string
	Audience         string
	DesiredReaction  string
	Format           string
	Width            int
	Height           int
	FPSNum           int
	FPSDen           int
	TargetDurationMs int
	Status           string
	Position         int
	Revision         int
	CreatedAt        string
	UpdatedAt        string
}

// Section is optional rhetorical structure in the outline.
type Section struct {
	ID               string
	VideoID          string
	ParentSectionID  sql.NullString
	Title            string
	Purpose          string
	Summary          string
	Position         int
	DurationBudgetMs int
	Revision         int
	CreatedAt        string
	UpdatedAt        string
}

// Scene is a dramatic/explanatory/location unit.
type Scene struct {
	ID               string
	SectionID        string
	Title            string
	Synopsis         string
	Purpose          string
	Pace             string
	AudienceReaction string
	DurationBudgetMs int
	Position         int
	Revision         int
	CreatedAt        string
	UpdatedAt        string
}

// Shot is the smallest visual production unit.
type Shot struct {
	ID               string
	SceneID          string
	Title            string
	Purpose          string
	Treatment        string
	DurationTargetMs int
	Position         int
	Status           string
	Readiness        string
	Revision         int
	CreatedAt        string
	UpdatedAt        string
}

// ShotSpec is one current specification for a shot (start_frame/end_frame/motion).
type ShotSpec struct {
	ID                   string
	ShotID               string
	Kind                 string
	PositivePrompt       string
	NegativePrompt       string
	StructuredSpecJSON   string
	ContinuityJSON       string
	ModelPreferencesJSON string
	Version              int
	CreatedAt            string
}
