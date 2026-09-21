package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

// CueTypes are the timed cue kinds (spec §6.3).
var CueTypes = []string{"dialogue", "voiceover", "sound", "music", "caption", "action"}

// ValidCueType reports whether t is a known cue type.
func ValidCueType(t string) bool {
	for _, c := range CueTypes {
		if c == t {
			return true
		}
	}
	return false
}

// ShotCue is one timed cue attached to a shot.
type ShotCue struct {
	ID           string
	ShotID       string
	CueType      string
	CharacterID  sql.NullString
	StartMs      int
	DurationMs   int
	Content      string
	Delivery     string
	Subtext      string
	SettingsJSON string
	Position     int
	Revision     int
	CreatedAt    string
	UpdatedAt    string
}

const cueColumns = `id, shot_id, cue_type, character_id, start_ms, duration_ms, content, delivery, subtext, settings_json, position, revision, created_at, updated_at`

// CreateCue adds a timed cue to a shot.
func (s *Store) CreateCue(ctx context.Context, shotID, cueType, characterID, content, delivery, subtext string, startMs, durationMs int) (*ShotCue, error) {
	if !ValidCueType(cueType) {
		return nil, fmt.Errorf("unknown cue type %q", cueType)
	}
	if startMs < 0 || durationMs < 0 {
		return nil, fmt.Errorf("cue timing cannot be negative")
	}
	now := db.Now()
	var pos int
	if err := s.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),-1)+1 FROM shot_cues WHERE shot_id=?`, shotID).Scan(&pos); err != nil {
		return nil, err
	}
	cue := &ShotCue{
		ID: ids.New(), ShotID: shotID, CueType: cueType, StartMs: startMs, DurationMs: durationMs,
		Content: content, Delivery: delivery, Subtext: subtext, SettingsJSON: "{}",
		Position: pos, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	var char any
	if characterID != "" {
		cue.CharacterID = sql.NullString{String: characterID, Valid: true}
		char = characterID
	}
	if _, err := s.ExecContext(ctx, `INSERT INTO shot_cues (`+cueColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		cue.ID, cue.ShotID, cue.CueType, char, cue.StartMs, cue.DurationMs, cue.Content,
		cue.Delivery, cue.Subtext, cue.SettingsJSON, cue.Position, cue.Revision, now, now); err != nil {
		return nil, fmt.Errorf("insert cue: %w", err)
	}
	return cue, nil
}

// GetCue fetches a cue by id.
func (s *Store) GetCue(ctx context.Context, id string) (*ShotCue, error) {
	row := s.QueryRowContext(ctx, `SELECT `+cueColumns+` FROM shot_cues WHERE id=?`, id)
	return scanCue(row)
}

// ListCues returns a shot's cues in order.
func (s *Store) ListCues(ctx context.Context, shotID string) ([]ShotCue, error) {
	rows, err := s.QueryContext(ctx, `SELECT `+cueColumns+` FROM shot_cues WHERE shot_id=? ORDER BY position, start_ms`, shotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ShotCue
	for rows.Next() {
		c, err := scanCue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// UpdateCue edits a cue's content and timing with an optimistic revision check.
func (s *Store) UpdateCue(ctx context.Context, id, content, delivery, subtext string, startMs, durationMs, expectedRevision int) error {
	if startMs < 0 || durationMs < 0 {
		return fmt.Errorf("cue timing cannot be negative")
	}
	res, err := s.ExecContext(ctx, `UPDATE shot_cues SET content=?, delivery=?, subtext=?, start_ms=?, duration_ms=?, revision=revision+1, updated_at=? WHERE id=? AND revision=?`,
		content, delivery, subtext, startMs, durationMs, db.Now(), id, expectedRevision)
	return affectedOrConflict(res, err)
}

// DeleteCue removes a cue.
func (s *Store) DeleteCue(ctx context.Context, id string) error {
	_, err := s.ExecContext(ctx, `DELETE FROM shot_cues WHERE id=?`, id)
	return err
}

// MoveCue swaps a cue with its neighbour in the given direction.
func (s *Store) MoveCue(ctx context.Context, id string, direction int) error {
	if direction != -1 && direction != 1 {
		return fmt.Errorf("direction must be -1 or 1")
	}
	cue, err := s.GetCue(ctx, id)
	if err != nil {
		return err
	}
	list, err := s.ListCues(ctx, cue.ShotID)
	if err != nil {
		return err
	}
	idx := -1
	for i, c := range list {
		if c.ID == id {
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
	if _, err := tx.ExecContext(ctx, `UPDATE shot_cues SET position=?, updated_at=? WHERE id=?`, other.Position, db.Now(), cue.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE shot_cues SET position=?, updated_at=? WHERE id=?`, cue.Position, db.Now(), other.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func scanCue(sc scanner) (*ShotCue, error) {
	var c ShotCue
	err := sc.Scan(&c.ID, &c.ShotID, &c.CueType, &c.CharacterID, &c.StartMs, &c.DurationMs,
		&c.Content, &c.Delivery, &c.Subtext, &c.SettingsJSON, &c.Position, &c.Revision, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}
