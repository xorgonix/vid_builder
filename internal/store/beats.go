package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

const beatColumns = `id, scene_id, title, purpose, trigger, action, reaction, resulting_state,
	intended_audience_effect, duration_budget_ms, position, implicit, revision, created_at, updated_at`

// ErrBeatHasShots is returned when deleting a beat that still owns shots.
var ErrBeatHasShots = errors.New("beat still has shots")

// ErrLastBeat is returned when deleting a scene's only beat.
var ErrLastBeat = errors.New("a scene must keep at least one beat")

// CreateBeat adds an explicit beat to a scene. Creating the first explicit beat
// promotes the scene's implicit beat so the hierarchy becomes visible.
func (s *Store) CreateBeat(ctx context.Context, sceneID, title, purpose, trigger, action, reaction, resultingState, audienceEffect string, durationBudgetMs int) (*Beat, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("beat title is required")
	}
	if durationBudgetMs < 0 {
		return nil, fmt.Errorf("beat duration budget cannot be negative")
	}
	now := db.Now()
	var pos int
	if err := s.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),-1)+1 FROM beats WHERE scene_id=?`, sceneID).Scan(&pos); err != nil {
		return nil, err
	}
	beat := &Beat{
		ID: ids.New(), SceneID: sceneID, Title: title, Purpose: purpose, Trigger: trigger,
		Action: action, Reaction: reaction, ResultingState: resultingState,
		IntendedAudienceEffect: audienceEffect, DurationBudgetMs: durationBudgetMs,
		Position: pos, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO beats (`+beatColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		beat.ID, beat.SceneID, beat.Title, beat.Purpose, beat.Trigger, beat.Action,
		beat.Reaction, beat.ResultingState, beat.IntendedAudienceEffect,
		beat.DurationBudgetMs, beat.Position, 0, beat.Revision, now, now); err != nil {
		return nil, fmt.Errorf("insert beat: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE beats SET implicit=0 WHERE scene_id=? AND implicit=1`, sceneID); err != nil {
		return nil, fmt.Errorf("promote implicit beat: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return beat, nil
}

// UpdateBeat edits a beat with an optimistic revision check.
func (s *Store) UpdateBeat(ctx context.Context, id, title, purpose, trigger, action, reaction, resultingState, audienceEffect string, durationBudgetMs, expectedRevision int) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("beat title is required")
	}
	if durationBudgetMs < 0 {
		return fmt.Errorf("beat duration budget cannot be negative")
	}
	res, err := s.ExecContext(ctx, `UPDATE beats SET title=?, purpose=?, trigger=?, action=?, reaction=?,
		resulting_state=?, intended_audience_effect=?, duration_budget_ms=?, revision=revision+1, updated_at=?
		WHERE id=? AND revision=?`,
		title, purpose, trigger, action, reaction, resultingState, audienceEffect,
		durationBudgetMs, db.Now(), id, expectedRevision)
	return affectedOrConflict(res, err)
}

// DeleteBeat removes a beat. A beat that owns shots, or a scene's only beat,
// cannot be deleted; shots are never silently destroyed.
func (s *Store) DeleteBeat(ctx context.Context, id string) error {
	beat, err := s.GetBeat(ctx, id)
	if err != nil {
		return err
	}
	var beats int
	if err := s.QueryRowContext(ctx, `SELECT COUNT(*) FROM beats WHERE scene_id=?`, beat.SceneID).Scan(&beats); err != nil {
		return err
	}
	if beats <= 1 {
		return ErrLastBeat
	}
	var shots int
	if err := s.QueryRowContext(ctx, `SELECT COUNT(*) FROM shots WHERE beat_id=?`, id).Scan(&shots); err != nil {
		return err
	}
	if shots > 0 {
		return ErrBeatHasShots
	}
	_, err = s.ExecContext(ctx, `DELETE FROM beats WHERE id=?`, id)
	return err
}

// MoveBeat swaps a beat with its neighbour in the given direction (-1 up, +1 down).
func (s *Store) MoveBeat(ctx context.Context, id string, direction int) error {
	if direction != -1 && direction != 1 {
		return fmt.Errorf("direction must be -1 or 1")
	}
	beat, err := s.GetBeat(ctx, id)
	if err != nil {
		return err
	}
	list, err := s.ListBeats(ctx, beat.SceneID)
	if err != nil {
		return err
	}
	idx := -1
	for i, b := range list {
		if b.ID == id {
			idx = i
			break
		}
	}
	target := idx + direction
	if idx < 0 || target < 0 || target >= len(list) {
		return nil
	}
	other := list[target]
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE beats SET position=?, updated_at=? WHERE id=?`, other.Position, db.Now(), beat.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE beats SET position=?, updated_at=? WHERE id=?`, beat.Position, db.Now(), other.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// BeatWithShots is a beat and its ordered shots for hierarchy rendering.
type BeatWithShots struct {
	Beat  Beat
	Shots []Shot
}

// SceneBeats returns a scene's beats with their shots (scene → beats → shots).
func (s *Store) SceneBeats(ctx context.Context, sceneID string) ([]BeatWithShots, error) {
	beats, err := s.ListBeats(ctx, sceneID)
	if err != nil {
		return nil, err
	}
	out := make([]BeatWithShots, 0, len(beats))
	for _, b := range beats {
		shots, err := s.ListShotsInBeat(ctx, b.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, BeatWithShots{Beat: b, Shots: shots})
	}
	return out, nil
}

// EnsureImplicitBeat returns the scene's implicit beat, creating one from the
// scene when the scene has no beats yet (v0.02 spec §5.1). It never creates a
// second implicit beat.
func (s *Store) EnsureImplicitBeat(ctx context.Context, sceneID string) (*Beat, error) {
	row := s.QueryRowContext(ctx, `SELECT `+beatColumns+` FROM beats
		WHERE scene_id=? AND implicit=1 ORDER BY position, created_at LIMIT 1`, sceneID)
	if b, err := scanBeat(row); err == nil {
		return b, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	row = s.QueryRowContext(ctx, `SELECT `+beatColumns+` FROM beats
		WHERE scene_id=? ORDER BY position, created_at LIMIT 1`, sceneID)
	if b, err := scanBeat(row); err == nil {
		return b, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	scene, err := s.GetScene(ctx, sceneID)
	if err != nil {
		return nil, err
	}
	now := db.Now()
	beat := &Beat{
		ID: ids.New(), SceneID: sceneID, Title: scene.Title, Purpose: scene.Purpose,
		Action: scene.Synopsis, DurationBudgetMs: scene.DurationBudgetMs,
		Position: 0, Implicit: true, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := s.ExecContext(ctx, `INSERT INTO beats (`+beatColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		beat.ID, beat.SceneID, beat.Title, beat.Purpose, beat.Trigger, beat.Action,
		beat.Reaction, beat.ResultingState, beat.IntendedAudienceEffect,
		beat.DurationBudgetMs, beat.Position, 1, beat.Revision, now, now); err != nil {
		return nil, fmt.Errorf("insert implicit beat: %w", err)
	}
	return beat, nil
}

// GetBeat fetches a beat by id.
func (s *Store) GetBeat(ctx context.Context, id string) (*Beat, error) {
	row := s.QueryRowContext(ctx, `SELECT `+beatColumns+` FROM beats WHERE id=?`, id)
	return scanBeat(row)
}

// ListBeats returns beats in a scene in order.
func (s *Store) ListBeats(ctx context.Context, sceneID string) ([]Beat, error) {
	rows, err := s.QueryContext(ctx, `SELECT `+beatColumns+` FROM beats
		WHERE scene_id=? ORDER BY position, created_at`, sceneID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Beat
	for rows.Next() {
		b, err := scanBeat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

// ListShotsInBeat returns shots attached to a beat in order.
func (s *Store) ListShotsInBeat(ctx context.Context, beatID string) ([]Shot, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, scene_id, COALESCE(beat_id,''), title, purpose, treatment, duration_target_ms, position, status, readiness, revision, created_at, updated_at
		FROM shots WHERE beat_id=? ORDER BY position, created_at`, beatID)
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

// BeatInVideo reports whether a beat belongs to the given video.
func (s *Store) BeatInVideo(ctx context.Context, beatID, videoID string) (bool, error) {
	var n int
	err := s.QueryRowContext(ctx, `SELECT COUNT(*) FROM beats b
		JOIN scenes sc ON sc.id = b.scene_id
		JOIN sections se ON se.id = sc.section_id
		WHERE b.id=? AND se.video_id=?`, beatID, videoID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func scanBeat(sc scanner) (*Beat, error) {
	var x Beat
	var implicit int
	err := sc.Scan(&x.ID, &x.SceneID, &x.Title, &x.Purpose, &x.Trigger, &x.Action,
		&x.Reaction, &x.ResultingState, &x.IntendedAudienceEffect, &x.DurationBudgetMs,
		&x.Position, &implicit, &x.Revision, &x.CreatedAt, &x.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	x.Implicit = implicit == 1
	return &x, nil
}
