package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

// GetSetting returns a durable application setting.
func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// SetSetting writes a durable application setting.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.ExecContext(ctx, `INSERT INTO app_settings (key, value, updated_at) VALUES (?,?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, key, value, db.Now())
	return err
}

// PairingToken is a short-lived LAN pairing credential (code hash only).
type PairingToken struct {
	ID          string
	UserID      string
	WorkspaceID sql.NullString
	DeviceLabel string
	ExpiresAt   string
	UsedAt      sql.NullString
}

// CreatePairingToken stores a hashed, short-lived pairing code.
func (s *Store) CreatePairingToken(ctx context.Context, userID, workspaceID, label, codeHash string, ttl time.Duration) (string, error) {
	now := time.Now().UTC()
	id := ids.New()
	var ws any
	if workspaceID != "" {
		ws = workspaceID
	}
	_, err := s.ExecContext(ctx, `INSERT INTO pairing_tokens (id, code_hash, user_id, workspace_id, device_label, created_by_user_id, created_at, expires_at)
		VALUES (?,?,?,?,?,?,?,?)`, id, codeHash, userID, ws, label, userID, db.Format(now), db.Format(now.Add(ttl)))
	if err != nil {
		return "", err
	}
	return id, nil
}

// ConsumePairingToken atomically validates, marks used and returns a token.
func (s *Store) ConsumePairingToken(ctx context.Context, codeHash string) (*PairingToken, error) {
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, `SELECT id, user_id, workspace_id, device_label, expires_at, used_at
		FROM pairing_tokens WHERE code_hash=?`, codeHash)
	var t PairingToken
	if err := row.Scan(&t.ID, &t.UserID, &t.WorkspaceID, &t.DeviceLabel, &t.ExpiresAt, &t.UsedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if t.UsedAt.Valid && t.UsedAt.String != "" {
		return nil, ErrNotFound
	}
	if t.ExpiresAt < db.Now() {
		return nil, ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pairing_tokens SET used_at=? WHERE id=?`, db.Now(), t.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &t, nil
}

// RevokePairingTokens invalidates all outstanding pairing codes.
func (s *Store) RevokePairingTokens(ctx context.Context) error {
	_, err := s.ExecContext(ctx, `UPDATE pairing_tokens SET used_at=? WHERE used_at IS NULL`, db.Now())
	return err
}
