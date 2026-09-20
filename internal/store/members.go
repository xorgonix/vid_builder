package store

import (
	"context"
	"database/sql"
	"errors"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

// Member is a workspace membership joined with its user.
type Member struct {
	UserID      string
	Email       string
	DisplayName string
	Role        string
	Status      string
	JoinedAt    string
}

// ListMembers returns memberships with user identity.
func (s *Store) ListMembers(ctx context.Context, workspaceID string) ([]Member, error) {
	rows, err := s.QueryContext(ctx, `SELECT u.id, u.email_normalized, u.display_name, m.role, m.status, m.joined_at
		FROM workspace_members m JOIN users u ON u.id = m.user_id
		WHERE m.workspace_id=? ORDER BY u.display_name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Email, &m.DisplayName, &m.Role, &m.Status, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddMember adds an existing user to a workspace with a role.
func (s *Store) AddMember(ctx context.Context, workspaceID, userID, role, invitedBy string) error {
	switch role {
	case "owner", "editor", "reviewer", "viewer":
	default:
		role = "viewer"
	}
	now := db.Now()
	_, err := s.ExecContext(ctx, `INSERT INTO workspace_members (id, workspace_id, user_id, role, status, joined_at, invited_by_user_id)
		VALUES (?,?,?,?, 'active', ?, ?)`, ids.New(), workspaceID, userID, role, now, nullStr(invitedBy))
	return err
}

// UpdateMemberRole changes a member's role and status.
func (s *Store) UpdateMemberRole(ctx context.Context, workspaceID, userID, role, status string) error {
	if status != "active" && status != "disabled" {
		status = "active"
	}
	_, err := s.ExecContext(ctx, `UPDATE workspace_members SET role=?, status=? WHERE workspace_id=? AND user_id=?`, role, status, workspaceID, userID)
	return err
}

// RemoveMember removes a membership.
func (s *Store) RemoveMember(ctx context.Context, workspaceID, userID string) error {
	_, err := s.ExecContext(ctx, `DELETE FROM workspace_members WHERE workspace_id=? AND user_id=?`, workspaceID, userID)
	return err
}

// CountOwners returns the number of active owners.
func (s *Store) CountOwners(ctx context.Context, workspaceID string) (int, error) {
	var n int
	err := s.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_members WHERE workspace_id=? AND role='owner' AND status='active'`, workspaceID).Scan(&n)
	return n, err
}

// ListAllUsers returns users for administration.
func (s *Store) ListAllUsers(ctx context.Context) ([]User, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, email_normalized, display_name, password_hash, status, is_system_admin, created_at, updated_at
		FROM users ORDER BY display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var admin int
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.Status, &admin, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		u.IsSystemAdmin = admin == 1
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetUserStatus enables or disables a user.
func (s *Store) SetUserStatus(ctx context.Context, userID, status string) error {
	if status != "active" && status != "disabled" {
		status = "active"
	}
	_, err := s.ExecContext(ctx, `UPDATE users SET status=?, updated_at=? WHERE id=?`, status, db.Now(), userID)
	return err
}

// UserByID returns a user or ErrNotFound.
func (s *Store) UserByID(ctx context.Context, id string) (*User, error) {
	row := s.QueryRowContext(ctx, `SELECT id, email_normalized, display_name, password_hash, status, is_system_admin, created_at, updated_at FROM users WHERE id=?`, id)
	var u User
	var admin int
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.Status, &admin, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	u.IsSystemAdmin = admin == 1
	return &u, err
}
