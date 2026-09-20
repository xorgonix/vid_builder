package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

// GetRootSection returns the implicit root section of a video.
func (s *Store) GetRootSection(ctx context.Context, videoID string) (*Section, error) {
	row := s.QueryRowContext(ctx, `SELECT id, video_id, parent_section_id, title, purpose, summary, position, duration_budget_ms, revision, created_at, updated_at
		FROM sections WHERE video_id=? ORDER BY position LIMIT 1`, videoID)
	return scanSection(row)
}

// ListSections returns all sections of a video ordered by position.
func (s *Store) ListSections(ctx context.Context, videoID string) ([]Section, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, video_id, parent_section_id, title, purpose, summary, position, duration_budget_ms, revision, created_at, updated_at
		FROM sections WHERE video_id=? ORDER BY position, created_at`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Section
	for rows.Next() {
		sec, err := scanSectionRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sec)
	}
	return out, rows.Err()
}

func scanSection(row *sql.Row) (*Section, error) {
	return scanSectionFields(row)
}

func scanSectionRows(rows *sql.Rows) (*Section, error) {
	return scanSectionFields(rows)
}

func scanSectionFields(sc scanner) (*Section, error) {
	var x Section
	err := sc.Scan(&x.ID, &x.VideoID, &x.ParentSectionID, &x.Title, &x.Purpose, &x.Summary, &x.Position, &x.DurationBudgetMs, &x.Revision, &x.CreatedAt, &x.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &x, err
}

// GetSection fetches a section by id.
func (s *Store) GetSection(ctx context.Context, id string) (*Section, error) {
	row := s.QueryRowContext(ctx, `SELECT id, video_id, parent_section_id, title, purpose, summary, position, duration_budget_ms, revision, created_at, updated_at FROM sections WHERE id=?`, id)
	return scanSection(row)
}

// CreateSection adds a child section under a parent (or root level).
func (s *Store) CreateSection(ctx context.Context, videoID string, parentID *string, title, purpose string, budgetMs int) (*Section, error) {
	now := db.Now()
	var pos int
	if err := s.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),-1)+1 FROM sections WHERE video_id=?`, videoID).Scan(&pos); err != nil {
		return nil, err
	}
	var parent any
	if parentID != nil {
		parent = *parentID
	}
	sec := &Section{ID: ids.New(), VideoID: videoID, Title: title, Purpose: purpose, Position: pos, DurationBudgetMs: budgetMs, Revision: 1, CreatedAt: now, UpdatedAt: now}
	_, err := s.ExecContext(ctx, `INSERT INTO sections (id, video_id, parent_section_id, title, purpose, summary, position, duration_budget_ms, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`, sec.ID, videoID, parent, title, purpose, "", pos, budgetMs, now, now)
	if err != nil {
		return nil, err
	}
	return sec, nil
}

// UpdateSection updates title/purpose/budget with optimistic revision check.
func (s *Store) UpdateSection(ctx context.Context, id, title, purpose string, budgetMs, expectedRevision int) error {
	res, err := s.ExecContext(ctx, `UPDATE sections SET title=?, purpose=?, duration_budget_ms=?, revision=revision+1, updated_at=? WHERE id=? AND revision=?`,
		title, purpose, budgetMs, db.Now(), id, expectedRevision)
	return affectedOrConflict(res, err)
}

// DeleteSection removes an empty section or rehomes its scenes to the target.
func (s *Store) DeleteSection(ctx context.Context, id, rehomeTo string) error {
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if rehomeTo != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE scenes SET section_id=? WHERE section_id=?`, rehomeTo, id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sections SET parent_section_id=? WHERE parent_section_id=?`, rehomeToOrNil(rehomeTo), id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sections WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func rehomeToOrNil(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// ListScenes returns scenes in a section.
func (s *Store) ListScenes(ctx context.Context, sectionID string) ([]Scene, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, section_id, title, synopsis, purpose, pace, audience_reaction, duration_budget_ms, position, revision, created_at, updated_at
		FROM scenes WHERE section_id=? ORDER BY position, created_at`, sectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Scene
	for rows.Next() {
		var x Scene
		if err := rows.Scan(&x.ID, &x.SectionID, &x.Title, &x.Synopsis, &x.Purpose, &x.Pace, &x.AudienceReaction, &x.DurationBudgetMs, &x.Position, &x.Revision, &x.CreatedAt, &x.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// GetScene fetches a scene by id.
func (s *Store) GetScene(ctx context.Context, id string) (*Scene, error) {
	row := s.QueryRowContext(ctx, `SELECT id, section_id, title, synopsis, purpose, pace, audience_reaction, duration_budget_ms, position, revision, created_at, updated_at FROM scenes WHERE id=?`, id)
	var x Scene
	err := row.Scan(&x.ID, &x.SectionID, &x.Title, &x.Synopsis, &x.Purpose, &x.Pace, &x.AudienceReaction, &x.DurationBudgetMs, &x.Position, &x.Revision, &x.CreatedAt, &x.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &x, err
}

// CreateScene adds a scene to a section.
func (s *Store) CreateScene(ctx context.Context, sectionID, title, synopsis, purpose, pace, reaction string, budgetMs int) (*Scene, error) {
	now := db.Now()
	var pos int
	if err := s.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),-1)+1 FROM scenes WHERE section_id=?`, sectionID).Scan(&pos); err != nil {
		return nil, err
	}
	x := &Scene{ID: ids.New(), SectionID: sectionID, Title: title, Synopsis: synopsis, Purpose: purpose, Pace: pace, AudienceReaction: reaction, DurationBudgetMs: budgetMs, Position: pos, Revision: 1, CreatedAt: now, UpdatedAt: now}
	_, err := s.ExecContext(ctx, `INSERT INTO scenes (id, section_id, title, synopsis, purpose, pace, audience_reaction, duration_budget_ms, position, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`, x.ID, sectionID, title, synopsis, purpose, pace, reaction, budgetMs, pos, now, now)
	if err != nil {
		return nil, err
	}
	return x, nil
}

// UpdateScene updates scene fields with optimistic revision check.
func (s *Store) UpdateScene(ctx context.Context, id, title, synopsis, purpose, pace, reaction string, budgetMs, expectedRevision int) error {
	res, err := s.ExecContext(ctx, `UPDATE scenes SET title=?, synopsis=?, purpose=?, pace=?, audience_reaction=?, duration_budget_ms=?, revision=revision+1, updated_at=? WHERE id=? AND revision=?`,
		title, synopsis, purpose, pace, reaction, budgetMs, db.Now(), id, expectedRevision)
	return affectedOrConflict(res, err)
}

// DeleteScene removes a scene and its shots.
func (s *Store) DeleteScene(ctx context.Context, id string) error {
	_, err := s.ExecContext(ctx, `DELETE FROM scenes WHERE id=?`, id)
	return err
}

// ListShots returns shots in a scene.
func (s *Store) ListShots(ctx context.Context, sceneID string) ([]Shot, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, scene_id, title, purpose, treatment, duration_target_ms, position, status, readiness, revision, created_at, updated_at
		FROM shots WHERE scene_id=? ORDER BY position, created_at`, sceneID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Shot
	for rows.Next() {
		x, err := scanShot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}

// GetShot fetches a shot by id.
func (s *Store) GetShot(ctx context.Context, id string) (*Shot, error) {
	row := s.QueryRowContext(ctx, `SELECT id, scene_id, title, purpose, treatment, duration_target_ms, position, status, readiness, revision, created_at, updated_at FROM shots WHERE id=?`, id)
	return scanShot(row)
}

// ShotInVideo reports whether a shot belongs to the given video.
func (s *Store) ShotInVideo(ctx context.Context, shotID, videoID string) (bool, error) {
	var n int
	err := s.QueryRowContext(ctx, `SELECT COUNT(*) FROM shots sh
		JOIN scenes sc ON sc.id = sh.scene_id
		JOIN sections se ON se.id = sc.section_id
		WHERE sh.id=? AND se.video_id=?`, shotID, videoID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ShotContext returns the scene and section titles for a shot.
func (s *Store) ShotContext(ctx context.Context, shotID string) (sceneTitle, sectionTitle string, err error) {
	err = s.QueryRowContext(ctx, `SELECT sc.title, se.title FROM shots sh
		JOIN scenes sc ON sc.id = sh.scene_id
		JOIN sections se ON se.id = sc.section_id WHERE sh.id=?`, shotID).Scan(&sceneTitle, &sectionTitle)
	return
}

func scanShot(sc scanner) (*Shot, error) {
	var x Shot
	err := sc.Scan(&x.ID, &x.SceneID, &x.Title, &x.Purpose, &x.Treatment, &x.DurationTargetMs, &x.Position, &x.Status, &x.Readiness, &x.Revision, &x.CreatedAt, &x.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &x, err
}

// CreateShot adds a shot and its three initial specifications.
func (s *Store) CreateShot(ctx context.Context, sceneID, userID, title, purpose, treatment string, durationTargetMs int) (*Shot, error) {
	now := db.Now()
	var pos int
	if err := s.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),-1)+1 FROM shots WHERE scene_id=?`, sceneID).Scan(&pos); err != nil {
		return nil, err
	}
	shot := &Shot{ID: ids.New(), SceneID: sceneID, Title: title, Purpose: purpose, Treatment: treatment, DurationTargetMs: durationTargetMs, Position: pos, Status: "draft", Readiness: "incomplete", Revision: 1, CreatedAt: now, UpdatedAt: now}
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO shots (id, scene_id, title, purpose, treatment, duration_target_ms, position, status, readiness, created_by_user_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, shot.ID, sceneID, title, purpose, treatment, durationTargetMs, pos, shot.Status, shot.Readiness, userID, now, now); err != nil {
		return nil, fmt.Errorf("insert shot: %w", err)
	}
	for _, kind := range []string{"start_frame", "end_frame", "motion"} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO shot_specs (id, shot_id, kind, positive_prompt, negative_prompt, structured_spec_json, continuity_json, model_preferences_json, version, created_by_user_id, created_at)
			VALUES (?,?,?,'','','{}','{}','{}',1,?,?)`, ids.New(), shot.ID, kind, userID, now); err != nil {
			return nil, fmt.Errorf("insert spec %s: %w", kind, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return shot, nil
}

// UpdateShot updates shot metadata with optimistic revision check.
func (s *Store) UpdateShot(ctx context.Context, id, title, purpose, treatment string, durationTargetMs, expectedRevision int) error {
	res, err := s.ExecContext(ctx, `UPDATE shots SET title=?, purpose=?, treatment=?, duration_target_ms=?, revision=revision+1, updated_at=? WHERE id=? AND revision=?`,
		title, purpose, treatment, durationTargetMs, db.Now(), id, expectedRevision)
	return affectedOrConflict(res, err)
}

// DeleteShot archives a shot.
func (s *Store) DeleteShot(ctx context.Context, id string) error {
	_, err := s.ExecContext(ctx, `UPDATE shots SET status='archived', updated_at=? WHERE id=?`, db.Now(), id)
	return err
}

// GetCurrentSpec returns the current spec of a kind.
func (s *Store) GetCurrentSpec(ctx context.Context, shotID, kind string) (*ShotSpec, error) {
	row := s.QueryRowContext(ctx, `SELECT id, shot_id, kind, positive_prompt, negative_prompt, structured_spec_json, continuity_json, model_preferences_json, version, created_at
		FROM shot_specs WHERE shot_id=? AND kind=? AND superseded_at IS NULL`, shotID, kind)
	var x ShotSpec
	err := row.Scan(&x.ID, &x.ShotID, &x.Kind, &x.PositivePrompt, &x.NegativePrompt, &x.StructuredSpecJSON, &x.ContinuityJSON, &x.ModelPreferencesJSON, &x.Version, &x.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &x, err
}

// ListSpecs returns all spec versions for a shot.
func (s *Store) ListSpecs(ctx context.Context, shotID string) ([]ShotSpec, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, shot_id, kind, positive_prompt, negative_prompt, structured_spec_json, continuity_json, model_preferences_json, version, created_at
		FROM shot_specs WHERE shot_id=? ORDER BY kind, version DESC`, shotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ShotSpec
	for rows.Next() {
		var x ShotSpec
		if err := rows.Scan(&x.ID, &x.ShotID, &x.Kind, &x.PositivePrompt, &x.NegativePrompt, &x.StructuredSpecJSON, &x.ContinuityJSON, &x.ModelPreferencesJSON, &x.Version, &x.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// UpdateSpec supersedes the current spec and writes a new version, returning the
// previous prompt so callers can preview the change.
func (s *Store) UpdateSpec(ctx context.Context, shotID, userID, kind, positive, negative, structuredJSON, continuityJSON string) (*ShotSpec, *ShotSpec, error) {
	prev, _ := s.GetCurrentSpec(ctx, shotID, kind)
	now := db.Now()
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	if prev != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE shot_specs SET superseded_at=? WHERE id=?`, now, prev.ID); err != nil {
			return nil, nil, err
		}
	}
	version := 1
	if prev != nil {
		version = prev.Version + 1
	}
	if structuredJSON == "" {
		structuredJSON = "{}"
	}
	if continuityJSON == "" {
		continuityJSON = "{}"
	}
	next := &ShotSpec{ID: ids.New(), ShotID: shotID, Kind: kind, PositivePrompt: positive, NegativePrompt: negative, StructuredSpecJSON: structuredJSON, ContinuityJSON: continuityJSON, ModelPreferencesJSON: "{}", Version: version, CreatedAt: now}
	if _, err := tx.ExecContext(ctx, `INSERT INTO shot_specs (id, shot_id, kind, positive_prompt, negative_prompt, structured_spec_json, continuity_json, model_preferences_json, version, created_by_user_id, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`, next.ID, shotID, kind, positive, negative, structuredJSON, continuityJSON, "{}", version, userID, now); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return next, prev, nil
}

// GetPromptLayers returns the current spec's prompt layers JSON for a kind.
func (s *Store) GetPromptLayers(ctx context.Context, shotID, kind string) (string, error) {
	var raw string
	err := s.QueryRowContext(ctx, `SELECT prompt_layers_json FROM shot_specs WHERE shot_id=? AND kind=? AND superseded_at IS NULL`, shotID, kind).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "[]", nil
	}
	if err != nil {
		return "[]", err
	}
	if raw == "" {
		return "[]", nil
	}
	return raw, nil
}

// SaveSpecWithLayers supersedes the current spec and writes a new version that
// carries both the compiled prompts and the source layers.
func (s *Store) SaveSpecWithLayers(ctx context.Context, shotID, userID, kind, positive, negative, layersJSON string) (*ShotSpec, error) {
	now := db.Now()
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var prevID string
	var prevVersion int
	err = tx.QueryRowContext(ctx, `SELECT id, version FROM shot_specs WHERE shot_id=? AND kind=? AND superseded_at IS NULL`, shotID, kind).Scan(&prevID, &prevVersion)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		if _, err := tx.ExecContext(ctx, `UPDATE shot_specs SET superseded_at=? WHERE id=?`, now, prevID); err != nil {
			return nil, err
		}
	}
	if layersJSON == "" {
		layersJSON = "[]"
	}
	next := &ShotSpec{ID: ids.New(), ShotID: shotID, Kind: kind, PositivePrompt: positive, NegativePrompt: negative, StructuredSpecJSON: "{}", ContinuityJSON: "{}", ModelPreferencesJSON: "{}", Version: prevVersion + 1, CreatedAt: now}
	if _, err := tx.ExecContext(ctx, `INSERT INTO shot_specs (id, shot_id, kind, positive_prompt, negative_prompt, structured_spec_json, continuity_json, model_preferences_json, prompt_layers_json, version, created_by_user_id, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, next.ID, shotID, kind, positive, negative, "{}", "{}", "{}", layersJSON, next.Version, userID, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return next, nil
}

// RecomputeShotReadiness derives readiness from current specs (basic version;
// blocking/endpoint rules extend this in Phase 3/4).
func (s *Store) RecomputeShotReadiness(ctx context.Context, shotID string) (string, error) {
	start, _ := s.GetCurrentSpec(ctx, shotID, "start_frame")
	end, _ := s.GetCurrentSpec(ctx, shotID, "end_frame")
	motion, _ := s.GetCurrentSpec(ctx, shotID, "motion")

	readiness := "incomplete"
	startOK := start != nil && (start.PositivePrompt != "" || start.StructuredSpecJSON != "{}")
	endOK := end != nil && (end.PositivePrompt != "" || end.StructuredSpecJSON != "{}")
	motionOK := motion != nil && (motion.PositivePrompt != "" || motion.StructuredSpecJSON != "{}")
	switch {
	case startOK && endOK && motionOK:
		readiness = "video_ready"
	case startOK && endOK:
		readiness = "concept_ready"
	case startOK || endOK:
		readiness = "blocking_ready"
	default:
		readiness = "incomplete"
	}
	_, err := s.ExecContext(ctx, `UPDATE shots SET readiness=?, updated_at=? WHERE id=?`, readiness, db.Now(), shotID)
	return readiness, err
}

// MoveShot swaps a shot's position with its neighbour (direction -1 up, +1 down).
func (s *Store) MoveShot(ctx context.Context, shotID string, direction int) error {
	return s.movePosition(ctx, "shots", "scene_id", shotID, direction)
}

// MoveScene swaps a scene's position with its neighbour.
func (s *Store) MoveScene(ctx context.Context, sceneID string, direction int) error {
	return s.movePosition(ctx, "scenes", "section_id", sceneID, direction)
}

// MoveSection swaps a section's position with its neighbour.
func (s *Store) MoveSection(ctx context.Context, sectionID string, direction int) error {
	return s.movePosition(ctx, "sections", "video_id", sectionID, direction)
}

func (s *Store) movePosition(ctx context.Context, table, parentCol, id string, direction int) error {
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var parent string
	var position int
	//nolint:gosec // table/parentCol are fixed internal literals
	q := fmt.Sprintf(`SELECT %s, position FROM %s WHERE id=?`, parentCol, table)
	if err := tx.QueryRowContext(ctx, q, id).Scan(&parent, &position); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	op := "<"
	order := "DESC"
	if direction > 0 {
		op = ">"
		order = "ASC"
	}
	var otherID string
	var otherPos int
	//nolint:gosec // table/parentCol are fixed internal literals
	q2 := fmt.Sprintf(`SELECT id, position FROM %s WHERE %s=? AND position %s ? ORDER BY position %s LIMIT 1`, table, parentCol, op, order)
	if err := tx.QueryRowContext(ctx, q2, parent, position).Scan(&otherID, &otherPos); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	//nolint:gosec // table is a fixed internal literal
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET position=? WHERE id=?`, table), otherPos, id); err != nil {
		return err
	}
	//nolint:gosec // table is a fixed internal literal
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET position=? WHERE id=?`, table), position, otherID); err != nil {
		return err
	}
	return tx.Commit()
}

func affectedOrConflict(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	return nil
}
