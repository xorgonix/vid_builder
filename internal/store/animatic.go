package store

import (
	"context"
	"database/sql"
	"errors"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

// Animatic is a versioned playback document referencing a video's structure.
type Animatic struct {
	ID             string
	VideoID        string
	Name           string
	SourceRevision int
	SettingsJSON   string
	Status         string
	Revision       int
	CreatedAt      string
	UpdatedAt      string
}

const animaticColumns = `id, video_id, name, source_revision, settings_json, status, revision, created_by_user_id, created_at, updated_at`

// EnsureAnimatic returns the video's animatic, creating a draft if none exists.
func (s *Store) EnsureAnimatic(ctx context.Context, videoID, userID string) (*Animatic, error) {
	if a, err := s.GetAnimaticForVideo(ctx, videoID); err == nil {
		return a, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	video, err := s.GetVideoByID(ctx, videoID)
	if err != nil {
		return nil, err
	}
	now := db.Now()
	a := &Animatic{
		ID: ids.New(), VideoID: videoID, Name: video.Title + " animatic",
		SourceRevision: video.Revision, SettingsJSON: "{}", Status: "draft",
		Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := s.ExecContext(ctx, `INSERT INTO animatics (`+animaticColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.VideoID, a.Name, a.SourceRevision, a.SettingsJSON, a.Status, a.Revision, userID, now, now); err != nil {
		return nil, err
	}
	return a, nil
}

// GetAnimaticForVideo returns the video's animatic, or ErrNotFound.
func (s *Store) GetAnimaticForVideo(ctx context.Context, videoID string) (*Animatic, error) {
	row := s.QueryRowContext(ctx, `SELECT `+animaticColumns+` FROM animatics WHERE video_id=? ORDER BY created_at LIMIT 1`, videoID)
	return scanAnimatic(row)
}

// UpdateAnimaticSettings saves settings with an optimistic revision check.
func (s *Store) UpdateAnimaticSettings(ctx context.Context, videoID, settingsJSON string, expectedRevision int) error {
	res, err := s.ExecContext(ctx, `UPDATE animatics SET settings_json=?, revision=revision+1, updated_at=? WHERE video_id=? AND revision=?`,
		settingsJSON, db.Now(), videoID, expectedRevision)
	return affectedOrConflict(res, err)
}

// SetAnimaticStatus transitions the animatic (draft/review/approved/stale).
func (s *Store) SetAnimaticStatus(ctx context.Context, videoID, status string) error {
	switch status {
	case "draft", "review", "approved", "stale":
	default:
		return errors.New("invalid animatic status")
	}
	_, err := s.ExecContext(ctx, `UPDATE animatics SET status=?, revision=revision+1, updated_at=? WHERE video_id=?`, status, db.Now(), videoID)
	return err
}

// TouchAnimaticRevision records the current video revision on the animatic.
func (s *Store) TouchAnimaticRevision(ctx context.Context, videoID string, revision int) error {
	_, err := s.ExecContext(ctx, `UPDATE animatics SET source_revision=?, updated_at=? WHERE video_id=?`, revision, db.Now(), videoID)
	return err
}

func scanAnimatic(sc scanner) (*Animatic, error) {
	var a Animatic
	var createdBy string
	err := sc.Scan(&a.ID, &a.VideoID, &a.Name, &a.SourceRevision, &a.SettingsJSON, &a.Status, &a.Revision, &createdBy, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}
