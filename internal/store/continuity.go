package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

// ShotState is one versioned semantic state document for a shot endpoint.
type ShotState struct {
	ID           string
	ShotID       string
	Endpoint     string
	Version      int
	StateJSON    string
	Source       string
	CreatedBy    sql.NullString
	CreatedAt    string
	SupersededAt sql.NullString
}

// ShotLink is a structured relationship between two shots.
type ShotLink struct {
	ID           string
	FromShotID   string
	ToShotID     string
	Relation     string
	MetadataJSON string
	CreatedAt    string
	UpdatedAt    string
}

const shotStateColumns = `id, shot_id, endpoint, version, state_json, source, created_by_user_id, created_at, superseded_at`

// SaveShotState records a new state version and supersedes the previous current
// version for that endpoint (spec §6.5).
func (s *Store) SaveShotState(ctx context.Context, shotID, endpoint, stateJSON, source, userID string) (*ShotState, error) {
	if endpoint != "start" && endpoint != "end" {
		return nil, fmt.Errorf("state endpoint must be start or end")
	}
	switch source {
	case "human", "model", "derived", "mixed":
	default:
		return nil, fmt.Errorf("state source must be human, model, derived or mixed")
	}
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var version int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM shot_states WHERE shot_id=? AND endpoint=?`, shotID, endpoint).Scan(&version); err != nil {
		return nil, err
	}
	now := db.Now()
	if _, err := tx.ExecContext(ctx, `UPDATE shot_states SET superseded_at=? WHERE shot_id=? AND endpoint=? AND superseded_at IS NULL`, now, shotID, endpoint); err != nil {
		return nil, err
	}
	id := ids.New()
	if _, err := tx.ExecContext(ctx, `INSERT INTO shot_states (id, shot_id, endpoint, version, state_json, source, created_by_user_id, created_at)
		VALUES (?,?,?,?,?,?,?,?)`, id, shotID, endpoint, version, stateJSON, source, userID, now); err != nil {
		return nil, fmt.Errorf("insert shot state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &ShotState{ID: id, ShotID: shotID, Endpoint: endpoint, Version: version, StateJSON: stateJSON, Source: source, CreatedAt: now}, nil
}

// GetCurrentShotState returns the current state for a shot endpoint, or nil.
func (s *Store) GetCurrentShotState(ctx context.Context, shotID, endpoint string) (*ShotState, error) {
	row := s.QueryRowContext(ctx, `SELECT `+shotStateColumns+` FROM shot_states
		WHERE shot_id=? AND endpoint=? AND superseded_at IS NULL`, shotID, endpoint)
	return scanShotState(row)
}

// ListShotStates returns all versions of a shot endpoint, newest first.
func (s *Store) ListShotStates(ctx context.Context, shotID, endpoint string) ([]ShotState, error) {
	rows, err := s.QueryContext(ctx, `SELECT `+shotStateColumns+` FROM shot_states
		WHERE shot_id=? AND endpoint=? ORDER BY version DESC`, shotID, endpoint)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ShotState
	for rows.Next() {
		st, err := scanShotState(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *st)
	}
	return out, rows.Err()
}

func scanShotState(sc scanner) (*ShotState, error) {
	var x ShotState
	err := sc.Scan(&x.ID, &x.ShotID, &x.Endpoint, &x.Version, &x.StateJSON, &x.Source, &x.CreatedBy, &x.CreatedAt, &x.SupersededAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &x, nil
}

// UpsertShotLink creates or updates the structured link between two shots.
func (s *Store) UpsertShotLink(ctx context.Context, fromShotID, toShotID, relation, metadataJSON string) (*ShotLink, error) {
	now := db.Now()
	var id string
	err := s.QueryRowContext(ctx, `SELECT id FROM shot_links WHERE from_shot_id=? AND to_shot_id=? AND relation=?`,
		fromShotID, toShotID, relation).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		id = ids.New()
		if _, err := s.ExecContext(ctx, `INSERT INTO shot_links (id, from_shot_id, to_shot_id, relation, metadata_json, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?)`, id, fromShotID, toShotID, relation, metadataJSON, now, now); err != nil {
			return nil, fmt.Errorf("insert shot link: %w", err)
		}
	case err != nil:
		return nil, err
	default:
		if _, err := s.ExecContext(ctx, `UPDATE shot_links SET metadata_json=?, updated_at=? WHERE id=?`, metadataJSON, now, id); err != nil {
			return nil, fmt.Errorf("update shot link: %w", err)
		}
	}
	return &ShotLink{ID: id, FromShotID: fromShotID, ToShotID: toShotID, Relation: relation, MetadataJSON: metadataJSON, CreatedAt: now, UpdatedAt: now}, nil
}

// GetShotLink returns the link between two shots, or nil.
func (s *Store) GetShotLink(ctx context.Context, fromShotID, toShotID string) (*ShotLink, error) {
	row := s.QueryRowContext(ctx, `SELECT id, from_shot_id, to_shot_id, relation, metadata_json, created_at, updated_at
		FROM shot_links WHERE from_shot_id=? AND to_shot_id=? ORDER BY updated_at DESC LIMIT 1`, fromShotID, toShotID)
	var x ShotLink
	err := row.Scan(&x.ID, &x.FromShotID, &x.ToShotID, &x.Relation, &x.MetadataJSON, &x.CreatedAt, &x.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &x, nil
}

// ListVideoShotsOrdered returns a video's non-archived shots in playback order
// (section, scene, beat, shot).
func (s *Store) ListVideoShotsOrdered(ctx context.Context, videoID string) ([]Shot, error) {
	rows, err := s.QueryContext(ctx, `SELECT sh.id, sh.scene_id, COALESCE(sh.beat_id,''), sh.title, sh.purpose, sh.treatment,
			sh.duration_target_ms, sh.position, sh.status, sh.readiness, sh.revision, sh.created_at, sh.updated_at
		FROM shots sh
		JOIN scenes sc ON sc.id = sh.scene_id
		JOIN sections se ON se.id = sc.section_id
		LEFT JOIN beats b ON b.id = sh.beat_id
		WHERE se.video_id=? AND sh.status <> 'archived'
		ORDER BY se.position, sc.position, COALESCE(b.position,0), sh.position, sh.created_at`, videoID)
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

// NeighbourShots returns the previous and next shots in playback order.
func (s *Store) NeighbourShots(ctx context.Context, shotID, videoID string) (prev, next *Shot, err error) {
	ordered, err := s.ListVideoShotsOrdered(ctx, videoID)
	if err != nil {
		return nil, nil, err
	}
	for i, sh := range ordered {
		if sh.ID != shotID {
			continue
		}
		if i > 0 {
			p := ordered[i-1]
			prev = &p
		}
		if i < len(ordered)-1 {
			n := ordered[i+1]
			next = &n
		}
		return prev, next, nil
	}
	return nil, nil, nil
}
