package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

const beatColumns = `id, scene_id, title, purpose, trigger, action, reaction, resulting_state,
	intended_audience_effect, duration_budget_ms, position, implicit, revision, created_at, updated_at`

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
