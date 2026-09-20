package store

import (
	"context"
	"database/sql"
	"errors"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

// CreateThumbnail records a derived thumbnail asset linked to its source.
func (s *Store) CreateThumbnail(ctx context.Context, workspaceID, projectID, sourceAssetID, uri, mime string, sizeBytes int64, checksum string, width, height int) (string, error) {
	id, err := s.CreateAsset(ctx, Asset{
		WorkspaceID: workspaceID, ProjectID: projectID,
		Kind: "thumbnail", Role: "thumbnail", StorageURI: uri, MimeType: mime,
		SizeBytes: sizeBytes, Checksum: checksum,
		Width:  sql.NullInt64{Int64: int64(width), Valid: true},
		Height: sql.NullInt64{Int64: int64(height), Valid: true},
	})
	if err != nil {
		return "", err
	}
	if _, err := s.ExecContext(ctx, `INSERT INTO asset_derivations (id, from_asset_id, to_asset_id, relation, metadata_json)
		VALUES (?,?,?, 'thumbnail_of', '{}')`, ids.New(), sourceAssetID, id); err != nil {
		return "", err
	}
	return id, nil
}

// GetThumbnailForAsset returns a derived thumbnail for an asset, if present.
func (s *Store) GetThumbnailForAsset(ctx context.Context, assetID string) (*Asset, error) {
	row := s.QueryRowContext(ctx, `SELECT a.id, a.workspace_id, a.project_id, a.generation_attempt_id, a.kind, a.role, a.storage_uri, a.mime_type, a.size_bytes, a.checksum_sha256, a.width, a.height, a.duration_ms, a.created_at
		FROM asset_derivations d JOIN assets a ON a.id=d.to_asset_id
		WHERE d.from_asset_id=? AND d.relation='thumbnail_of' AND a.deleted_at IS NULL
		ORDER BY a.created_at DESC LIMIT 1`, assetID)
	var a Asset
	err := row.Scan(&a.ID, &a.WorkspaceID, &a.ProjectID, &a.GenerationAttemptID, &a.Kind, &a.Role, &a.StorageURI, &a.MimeType, &a.SizeBytes, &a.Checksum, &a.Width, &a.Height, &a.DurationMs, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// HasThumbnail reports whether an asset already has a derived thumbnail.
func (s *Store) HasThumbnail(ctx context.Context, assetID string) bool {
	var n int
	_ = s.QueryRowContext(ctx, `SELECT COUNT(*) FROM asset_derivations WHERE from_asset_id=? AND relation='thumbnail_of'`, assetID).Scan(&n)
	return n > 0
}

var _ = db.Now
