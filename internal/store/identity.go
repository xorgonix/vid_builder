package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

// CountUsers returns the number of rows in users.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// GetUserByEmail looks up an active/disabled user by normalized email.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	row := s.QueryRowContext(ctx, `SELECT id, email_normalized, display_name, password_hash, status, is_system_admin, created_at, updated_at
		FROM users WHERE email_normalized = ?`, db.NormalizeEmail(email))
	return scanUser(row)
}

// GetUser fetches a user by id.
func (s *Store) GetUser(ctx context.Context, id string) (*User, error) {
	row := s.QueryRowContext(ctx, `SELECT id, email_normalized, display_name, password_hash, status, is_system_admin, created_at, updated_at
		FROM users WHERE id = ?`, id)
	return scanUser(row)
}

func scanUser(row *sql.Row) (*User, error) {
	var u User
	var admin int
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.Status, &admin, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.IsSystemAdmin = admin == 1
	return &u, nil
}

// CreateUser inserts a user and returns it.
func (s *Store) CreateUser(ctx context.Context, email, displayName, passwordHash string, isAdmin bool) (*User, error) {
	now := db.Now()
	id := ids.New()
	admin := 0
	if isAdmin {
		admin = 1
	}
	_, err := s.ExecContext(ctx, `INSERT INTO users (id, email_normalized, display_name, password_hash, status, is_system_admin, created_at, updated_at)
		VALUES (?,?,?,?,'active',?,?,?)`, id, db.NormalizeEmail(email), displayName, passwordHash, admin, now, now)
	if err != nil {
		return nil, err
	}
	return &User{ID: id, Email: db.NormalizeEmail(email), DisplayName: displayName, PasswordHash: passwordHash, Status: "active", IsSystemAdmin: isAdmin, CreatedAt: now, UpdatedAt: now}, nil
}

// CreateSession stores a session row identified by the token hash.
func (s *Store) CreateSession(ctx context.Context, userID string, tokenHash, csrfSecret []byte, expiresAt, userAgent string) (string, error) {
	id := ids.New()
	now := db.Now()
	_, err := s.ExecContext(ctx, `INSERT INTO sessions (id, user_id, token_hash, csrf_secret, created_at, last_seen_at, expires_at, user_agent)
		VALUES (?,?,?,?,?,?,?,?)`, id, userID, tokenHash, csrfSecret, now, now, expiresAt, userAgent)
	if err != nil {
		return "", err
	}
	return id, nil
}

// GetSessionByTokenHash resolves an active, unexpired session.
func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash []byte) (*Session, error) {
	row := s.QueryRowContext(ctx, `SELECT id, user_id, csrf_secret, created_at, expires_at, revoked_at
		FROM sessions WHERE token_hash = ?`, tokenHash)
	var sess Session
	err := row.Scan(&sess.ID, &sess.UserID, &sess.CSRFSecret, &sess.CreatedAt, &sess.ExpiresAt, &sess.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	// Validate expiry and revocation in Go for portable RFC3339 comparison.
	if sess.RevokedAt.Valid && sess.RevokedAt.String != "" {
		return nil, ErrNotFound
	}
	if sess.ExpiresAt < db.Now() {
		return nil, ErrNotFound
	}
	return &sess, nil
}

// TouchSession updates last_seen_at.
func (s *Store) TouchSession(ctx context.Context, id string) error {
	_, err := s.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ? WHERE id = ?`, db.Now(), id)
	return err
}

// RevokeSession marks a session revoked.
func (s *Store) RevokeSession(ctx context.Context, id string) error {
	_, err := s.ExecContext(ctx, `UPDATE sessions SET revoked_at = ? WHERE id = ?`, db.Now(), id)
	return err
}

// ListWorkspacesForUser returns active workspaces and the user's role.
func (s *Store) ListWorkspacesForUser(ctx context.Context, userID string) ([]Workspace, map[string]string, error) {
	rows, err := s.QueryContext(ctx, `SELECT w.id, w.name, w.slug, w.kind, w.revision, w.created_at, w.updated_at, m.role
		FROM workspaces w JOIN workspace_members m ON m.workspace_id = w.id
		WHERE m.user_id = ? AND m.status = 'active' AND w.archived_at IS NULL
		ORDER BY w.created_at`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []Workspace
	roles := map[string]string{}
	for rows.Next() {
		var w Workspace
		var role string
		if err := rows.Scan(&w.ID, &w.Name, &w.Slug, &w.Kind, &w.Revision, &w.CreatedAt, &w.UpdatedAt, &role); err != nil {
			return nil, nil, err
		}
		out = append(out, w)
		roles[w.ID] = role
	}
	return out, roles, rows.Err()
}

// RoleFor returns the active role for a user in a workspace, or ErrNotFound.
func (s *Store) RoleFor(ctx context.Context, workspaceID, userID string) (string, error) {
	var role string
	err := s.QueryRowContext(ctx, `SELECT role FROM workspace_members
		WHERE workspace_id = ? AND user_id = ? AND status = 'active'`, workspaceID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return role, err
}

// GetWorkspace fetches a workspace by id.
func (s *Store) GetWorkspace(ctx context.Context, id string) (*Workspace, error) {
	row := s.QueryRowContext(ctx, `SELECT id, name, slug, kind, revision, created_at, updated_at FROM workspaces WHERE id = ?`, id)
	var w Workspace
	err := row.Scan(&w.ID, &w.Name, &w.Slug, &w.Kind, &w.Revision, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &w, err
}

// CreateWorkspace creates a workspace and its owner membership transactionally.
func (s *Store) CreateWorkspace(ctx context.Context, userID, name, kind string) (*Workspace, error) {
	slug := slugify(name)
	now := db.Now()
	w := &Workspace{ID: ids.New(), Name: name, Slug: slug, Kind: kind, Revision: 1, CreatedAt: now, UpdatedAt: now}
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO workspaces (id, name, slug, kind, created_by_user_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?)`, w.ID, w.Name, w.Slug, w.Kind, userID, now, now); err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO workspace_members (id, workspace_id, user_id, role, status, joined_at)
		VALUES (?,?,?,'owner','active',?)`, ids.New(), w.ID, userID, now); err != nil {
		return nil, fmt.Errorf("create membership: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return w, nil
}

// SessionInfo is a session row shown to its owner for revocation.
type SessionInfo struct {
	ID        string
	CreatedAt string
	LastSeen  string
	ExpiresAt string
	RevokedAt sql.NullString
	UserAgent sql.NullString
	IPHint    sql.NullString
}

// ListSessionsForUser returns recent sessions for the account screen.
func (s *Store) ListSessionsForUser(ctx context.Context, userID string) ([]SessionInfo, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, created_at, last_seen_at, expires_at, revoked_at, user_agent, ip_hint
		FROM sessions WHERE user_id = ? ORDER BY created_at DESC LIMIT 25`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionInfo
	for rows.Next() {
		var si SessionInfo
		if err := rows.Scan(&si.ID, &si.CreatedAt, &si.LastSeen, &si.ExpiresAt, &si.RevokedAt, &si.UserAgent, &si.IPHint); err != nil {
			return nil, err
		}
		out = append(out, si)
	}
	return out, rows.Err()
}

// Audit records an append-only human/security audit event.
func (s *Store) Audit(ctx context.Context, workspaceID, actorID, action, entityType, entityID, summary string, metadataJSON string) error {
	if metadataJSON == "" {
		metadataJSON = "{}"
	}
	var ws any
	if workspaceID != "" {
		ws = workspaceID
	}
	var actor any
	if actorID != "" {
		actor = actorID
	}
	_, err := s.ExecContext(ctx, `INSERT INTO audit_events (id, workspace_id, actor_user_id, action, entity_type, entity_id, summary, metadata_json, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, ids.New(), ws, actor, action, entityType, entityID, summary, metadataJSON, db.Now())
	return err
}

func slugify(s string) string {
	out := make([]rune, 0, len(s))
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
			prevDash = false
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
			prevDash = false
		default:
			if !prevDash && len(out) > 0 {
				out = append(out, '-')
				prevDash = true
			}
		}
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return "workspace"
	}
	return string(out)
}
