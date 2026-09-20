package store

import (
	"context"
	"database/sql"
	"errors"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

// Blocking is a versioned structured composition for a shot endpoint.
type Blocking struct {
	ID         string
	ShotSpecID string
	Kind       string
	Version    int
	SpecJSON   string
	SVGCache   sql.NullString
	Source     string
	ModelID    sql.NullString
	CreatedAt  string
}

// GetCurrentBlocking returns the current blocking of a kind for a spec.
func (s *Store) GetCurrentBlocking(ctx context.Context, shotSpecID, kind string) (*Blocking, error) {
	row := s.QueryRowContext(ctx, `SELECT id, shot_spec_id, blocking_kind, version, spec_json, svg_cache_uri, source, model_id, created_at
		FROM shot_blockings WHERE shot_spec_id=? AND blocking_kind=? AND superseded_at IS NULL`, shotSpecID, kind)
	var b Blocking
	err := row.Scan(&b.ID, &b.ShotSpecID, &b.Kind, &b.Version, &b.SpecJSON, &b.SVGCache, &b.Source, &b.ModelID, &b.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &b, err
}

// GetBlockingForShot resolves the spec then its current blocking.
func (s *Store) GetBlockingForShot(ctx context.Context, shotID, specKind, blockingKind string) (*Blocking, error) {
	spec, err := s.GetCurrentSpec(ctx, shotID, specKind)
	if err != nil {
		return nil, err
	}
	return s.GetCurrentBlocking(ctx, spec.ID, blockingKind)
}

// ListBlockings returns all blocking versions for a spec.
func (s *Store) ListBlockings(ctx context.Context, shotSpecID string) ([]Blocking, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, shot_spec_id, blocking_kind, version, spec_json, svg_cache_uri, source, model_id, created_at
		FROM shot_blockings WHERE shot_spec_id=? ORDER BY blocking_kind, version DESC`, shotSpecID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Blocking
	for rows.Next() {
		var b Blocking
		if err := rows.Scan(&b.ID, &b.ShotSpecID, &b.Kind, &b.Version, &b.SpecJSON, &b.SVGCache, &b.Source, &b.ModelID, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// SaveBlocking supersedes the current blocking and writes a new version.
func (s *Store) SaveBlocking(ctx context.Context, shotSpecID, kind, specJSON, source, modelID, userID string) (*Blocking, error) {
	now := db.Now()
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE shot_blockings SET superseded_at=? WHERE shot_spec_id=? AND blocking_kind=? AND superseded_at IS NULL`, now, shotSpecID, kind); err != nil {
		return nil, err
	}
	var version int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM shot_blockings WHERE shot_spec_id=? AND blocking_kind=?`, shotSpecID, kind).Scan(&version); err != nil {
		return nil, err
	}
	var mid, uid any
	if modelID != "" {
		mid = modelID
	}
	if userID != "" {
		uid = userID
	}
	id := ids.New()
	if _, err := tx.ExecContext(ctx, `INSERT INTO shot_blockings (id, shot_spec_id, blocking_kind, version, spec_json, source, created_by_user_id, model_id, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, id, shotSpecID, kind, version, specJSON, source, uid, mid, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Blocking{ID: id, ShotSpecID: shotSpecID, Kind: kind, Version: version, SpecJSON: specJSON, Source: source, CreatedAt: now}, nil
}

// Character is a reusable project entity with continuity rules.
type Character struct {
	ID          string
	ProjectID   string
	Name        string
	Description string
	Continuity  string
	Status      string
	Revision    int
}

// Location is a reusable project place with continuity rules.
type Location struct {
	ID          string
	ProjectID   string
	Name        string
	Description string
	Continuity  string
	Status      string
	Revision    int
}

// StyleRule is a reusable project-wide visual rule.
type StyleRule struct {
	ID       string
	Category string
	Text     string
	Priority int
	Active   bool
}

// ListCharacters returns active characters for a project.
func (s *Store) ListCharacters(ctx context.Context, projectID string) ([]Character, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, project_id, name, description, continuity_json, status, revision FROM characters WHERE project_id=? AND status='active' ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Character
	for rows.Next() {
		var c Character
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Name, &c.Description, &c.Continuity, &c.Status, &c.Revision); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateCharacter adds a character to a project.
func (s *Store) CreateCharacter(ctx context.Context, projectID, name, description, continuityJSON string) (*Character, error) {
	now := db.Now()
	if continuityJSON == "" {
		continuityJSON = "{}"
	}
	c := &Character{ID: ids.New(), ProjectID: projectID, Name: name, Description: description, Continuity: continuityJSON, Status: "active", Revision: 1}
	_, err := s.ExecContext(ctx, `INSERT INTO characters (id, project_id, name, description, continuity_json, status, created_at, updated_at)
		VALUES (?,?,?,?,?,'active',?,?)`, c.ID, projectID, name, description, continuityJSON, now, now)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// ListLocations returns active locations for a project.
func (s *Store) ListLocations(ctx context.Context, projectID string) ([]Location, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, project_id, name, description, continuity_json, status, revision FROM locations WHERE project_id=? AND status='active' ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Location
	for rows.Next() {
		var l Location
		if err := rows.Scan(&l.ID, &l.ProjectID, &l.Name, &l.Description, &l.Continuity, &l.Status, &l.Revision); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// CreateLocation adds a location to a project.
func (s *Store) CreateLocation(ctx context.Context, projectID, name, description, continuityJSON string) (*Location, error) {
	now := db.Now()
	if continuityJSON == "" {
		continuityJSON = "{}"
	}
	l := &Location{ID: ids.New(), ProjectID: projectID, Name: name, Description: description, Continuity: continuityJSON, Status: "active", Revision: 1}
	_, err := s.ExecContext(ctx, `INSERT INTO locations (id, project_id, name, description, continuity_json, status, created_at, updated_at)
		VALUES (?,?,?,?,?,'active',?,?)`, l.ID, projectID, name, description, continuityJSON, now, now)
	if err != nil {
		return nil, err
	}
	return l, nil
}

// ListStyleRules returns active style rules for a project.
func (s *Store) ListStyleRules(ctx context.Context, projectID string) ([]StyleRule, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, category, rule_text, priority, active FROM style_rules WHERE project_id=? AND active=1 ORDER BY priority DESC, category`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StyleRule
	for rows.Next() {
		var r StyleRule
		var active int
		if err := rows.Scan(&r.ID, &r.Category, &r.Text, &r.Priority, &active); err != nil {
			return nil, err
		}
		r.Active = active == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateStyleRule adds a style rule to a project.
func (s *Store) CreateStyleRule(ctx context.Context, projectID, userID, category, text string, priority int) error {
	_, err := s.ExecContext(ctx, `INSERT INTO style_rules (id, project_id, category, rule_text, priority, active, created_by_user_id, created_at, updated_at)
		VALUES (?,?,?,?,?,1,?,?,?)`, ids.New(), projectID, category, text, priority, userID, db.Now(), db.Now())
	return err
}
