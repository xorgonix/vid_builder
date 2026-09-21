package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

// NarrativeMetric is a versioned planning metric for a scene, beat or shot
// (v0.02 spec §6.2). Metrics are creative guides, never scientific claims, and
// may be left unset.
type NarrativeMetric struct {
	ID                 string
	VideoID            string
	EntityType         string
	EntityID           string
	DramaticIntensity  sql.NullInt64
	VisualIntensity    sql.NullInt64
	Tension            sql.NullInt64
	InformationDensity sql.NullInt64
	EmotionalValence   sql.NullInt64
	Notes              string
	Version            int
	CreatedAt          string
}

// MetricValues carries optional metric values; nil means "leave unset".
type MetricValues struct {
	DramaticIntensity  *int
	VisualIntensity    *int
	Tension            *int
	InformationDensity *int
	EmotionalValence   *int
}

const metricColumns = `id, video_id, entity_type, entity_id, dramatic_intensity, visual_intensity, tension, information_density, emotional_valence, notes, version, created_by_user_id, created_at`

// SetMetric records a new metric version and supersedes the previous current
// version for that entity (spec §6.2).
func (s *Store) SetMetric(ctx context.Context, videoID, entityType, entityID string, v MetricValues, notes, userID string) (*NarrativeMetric, error) {
	switch entityType {
	case "scene", "beat", "shot":
	default:
		return nil, fmt.Errorf("metric entity_type must be scene, beat or shot")
	}
	if err := validateMetricValue("dramatic_intensity", v.DramaticIntensity, 0, 100); err != nil {
		return nil, err
	}
	if err := validateMetricValue("visual_intensity", v.VisualIntensity, 0, 100); err != nil {
		return nil, err
	}
	if err := validateMetricValue("tension", v.Tension, 0, 100); err != nil {
		return nil, err
	}
	if err := validateMetricValue("information_density", v.InformationDensity, 0, 100); err != nil {
		return nil, err
	}
	if err := validateMetricValue("emotional_valence", v.EmotionalValence, -100, 100); err != nil {
		return nil, err
	}

	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var version int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM narrative_metrics WHERE entity_type=? AND entity_id=?`, entityType, entityID).Scan(&version); err != nil {
		return nil, err
	}
	now := db.Now()
	if _, err := tx.ExecContext(ctx, `UPDATE narrative_metrics SET superseded_at=? WHERE entity_type=? AND entity_id=? AND superseded_at IS NULL`, now, entityType, entityID); err != nil {
		return nil, err
	}
	m := &NarrativeMetric{
		ID: ids.New(), VideoID: videoID, EntityType: entityType, EntityID: entityID,
		DramaticIntensity: nullInt(v.DramaticIntensity), VisualIntensity: nullInt(v.VisualIntensity),
		Tension: nullInt(v.Tension), InformationDensity: nullInt(v.InformationDensity),
		EmotionalValence: nullInt(v.EmotionalValence), Notes: notes, Version: version, CreatedAt: now,
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO narrative_metrics (`+metricColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.VideoID, m.EntityType, m.EntityID, m.DramaticIntensity, m.VisualIntensity,
		m.Tension, m.InformationDensity, m.EmotionalValence, m.Notes, m.Version, userID, now); err != nil {
		return nil, fmt.Errorf("insert metric: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return m, nil
}

// GetCurrentMetric returns the current metric for an entity, or nil.
func (s *Store) GetCurrentMetric(ctx context.Context, entityType, entityID string) (*NarrativeMetric, error) {
	row := s.QueryRowContext(ctx, `SELECT `+metricColumns+` FROM narrative_metrics
		WHERE entity_type=? AND entity_id=? AND superseded_at IS NULL`, entityType, entityID)
	var m NarrativeMetric
	var createdBy string
	err := row.Scan(&m.ID, &m.VideoID, &m.EntityType, &m.EntityID, &m.DramaticIntensity,
		&m.VisualIntensity, &m.Tension, &m.InformationDensity, &m.EmotionalValence, &m.Notes, &m.Version, &createdBy, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ListCurrentMetricsForVideo returns current metrics for a video's entities.
func (s *Store) ListCurrentMetricsForVideo(ctx context.Context, videoID string) ([]NarrativeMetric, error) {
	rows, err := s.QueryContext(ctx, `SELECT `+metricColumns+` FROM narrative_metrics
		WHERE video_id=? AND superseded_at IS NULL`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NarrativeMetric
	for rows.Next() {
		var m NarrativeMetric
		var createdBy string
		if err := rows.Scan(&m.ID, &m.VideoID, &m.EntityType, &m.EntityID, &m.DramaticIntensity,
			&m.VisualIntensity, &m.Tension, &m.InformationDensity, &m.EmotionalValence, &m.Notes, &m.Version, &createdBy, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func nullInt(v *int) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*v), Valid: true}
}

func validateMetricValue(name string, v *int, lo, hi int) error {
	if v == nil {
		return nil
	}
	if *v < lo || *v > hi {
		return fmt.Errorf("%s must be between %d and %d", name, lo, hi)
	}
	return nil
}
