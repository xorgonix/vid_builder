package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

// ModelProfile is a named, reusable production settings bundle.
type ModelProfile struct {
	ID           string
	WorkspaceID  string
	Name         string
	Task         string
	ModelID      sql.NullString
	SettingsJSON string
	LoraStack    string
	Revision     int
}

// ListModelProfiles returns a workspace's profiles.
func (s *Store) ListModelProfiles(ctx context.Context, workspaceID string) ([]ModelProfile, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, workspace_id, name, task, model_id, settings_json, lora_stack_json, revision
		FROM model_profiles WHERE workspace_id=? ORDER BY name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModelProfile
	for rows.Next() {
		var p ModelProfile
		if err := rows.Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.Task, &p.ModelID, &p.SettingsJSON, &p.LoraStack, &p.Revision); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetModelProfile fetches a profile in workspace scope.
func (s *Store) GetModelProfile(ctx context.Context, workspaceID, id string) (*ModelProfile, error) {
	row := s.QueryRowContext(ctx, `SELECT id, workspace_id, name, task, model_id, settings_json, lora_stack_json, revision
		FROM model_profiles WHERE id=? AND workspace_id=?`, id, workspaceID)
	var p ModelProfile
	err := row.Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.Task, &p.ModelID, &p.SettingsJSON, &p.LoraStack, &p.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// CreateModelProfile adds a profile.
func (s *Store) CreateModelProfile(ctx context.Context, workspaceID, userID, name, task, checkpoint string) (*ModelProfile, error) {
	settings := map[string]any{}
	if checkpoint != "" {
		settings["checkpoint"] = checkpoint
	}
	settingsJSON, _ := json.Marshal(settings)
	now := db.Now()
	p := &ModelProfile{ID: ids.New(), WorkspaceID: workspaceID, Name: name, Task: task, SettingsJSON: string(settingsJSON), LoraStack: "[]", Revision: 1}
	if _, err := s.ExecContext(ctx, `INSERT INTO model_profiles (id, workspace_id, name, task, settings_json, lora_stack_json, created_by_user_id, created_at, updated_at)
		VALUES (?,?,?,?,?, '[]', ?, ?, ?)`, p.ID, workspaceID, name, task, string(settingsJSON), userID, now, now); err != nil {
		return nil, err
	}
	return p, nil
}

// DeleteModelProfile removes a profile.
func (s *Store) DeleteModelProfile(ctx context.Context, workspaceID, id string) error {
	_, err := s.ExecContext(ctx, `DELETE FROM model_profiles WHERE id=? AND workspace_id=?`, id, workspaceID)
	return err
}

// ProfileCheckpoint extracts the checkpoint hint from a profile's settings.
func (p *ModelProfile) ProfileCheckpoint() string {
	if p == nil {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(p.SettingsJSON), &m) != nil {
		return ""
	}
	if v, ok := m["checkpoint"].(string); ok {
		return v
	}
	return ""
}

// GetProjectDefaultProfile returns the project's default model profile id, or "".
func (s *Store) GetProjectDefaultProfile(ctx context.Context, projectID string) string {
	var v sql.NullString
	_ = s.QueryRowContext(ctx, `SELECT default_model_profile_id FROM projects WHERE id=?`, projectID).Scan(&v)
	if v.Valid {
		return v.String
	}
	return ""
}

// SetProjectDefaultProfile sets (or clears) the project's default model profile.
func (s *Store) SetProjectDefaultProfile(ctx context.Context, projectID, profileID string) error {
	_, err := s.ExecContext(ctx, `UPDATE projects SET default_model_profile_id=?, updated_at=? WHERE id=?`, nullStr(profileID), db.Now(), projectID)
	return err
}
