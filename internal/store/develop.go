package store

import (
	"context"
	"database/sql"
	"errors"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

// DiscoveryThread is a discussion about the project/video direction.
type DiscoveryThread struct {
	ID        string
	ProjectID string
	VideoID   sql.NullString
	Title     string
	Status    string
	CreatedAt string
	UpdatedAt string
}

// DiscoveryMessage is an append-only conversation entry.
type DiscoveryMessage struct {
	ID        string
	ThreadID  string
	Role      string
	Content   string
	ModelID   sql.NullString
	CreatedAt string
}

// Proposal is an inspectable AI/human proposal before application.
type Proposal struct {
	ID                string
	ProjectID         string
	VideoID           sql.NullString
	ProposalType      string
	PayloadJSON       string
	Summary           string
	Status            string
	ModelID           sql.NullString
	DecisionReason    string
	RegeneratedFromID sql.NullString
	CreatedAt         string
}

// Brief is an accepted, versioned creative direction.
type Brief struct {
	ID               string
	ProjectID        string
	Version          int
	Premise          string
	Theme            string
	Audience         string
	DesiredReaction  string
	Pace             string
	Excitement       string
	MusicIntent      string
	SoundIntent      string
	VisualStyle      string
	CameraLanguage   string
	ProductionNotes  string
	SourceProposalID sql.NullString
	CreatedAt        string
	SupersededAt     sql.NullString
}

// AuditEvent is one human/security audit row.
type AuditEvent struct {
	ID         string
	Action     string
	EntityType string
	EntityID   string
	Summary    string
	CreatedAt  string
}

// ListThreads returns discovery threads for a project/video.
func (s *Store) ListThreads(ctx context.Context, projectID, videoID string) ([]DiscoveryThread, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, project_id, video_id, title, status, created_at, updated_at
		FROM discovery_threads WHERE project_id=? AND (video_id=? OR video_id IS NULL) ORDER BY created_at`, projectID, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DiscoveryThread
	for rows.Next() {
		var t DiscoveryThread
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.VideoID, &t.Title, &t.Status, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetThread fetches a thread by id.
func (s *Store) GetThread(ctx context.Context, id string) (*DiscoveryThread, error) {
	row := s.QueryRowContext(ctx, `SELECT id, project_id, video_id, title, status, created_at, updated_at FROM discovery_threads WHERE id=?`, id)
	var t DiscoveryThread
	err := row.Scan(&t.ID, &t.ProjectID, &t.VideoID, &t.Title, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &t, err
}

// CreateThread adds a discovery thread.
func (s *Store) CreateThread(ctx context.Context, projectID, videoID, userID, title string) (*DiscoveryThread, error) {
	now := db.Now()
	t := &DiscoveryThread{ID: ids.New(), ProjectID: projectID, Title: title, Status: "open", CreatedAt: now, UpdatedAt: now}
	if videoID != "" {
		t.VideoID = sql.NullString{String: videoID, Valid: true}
	}
	var vid any
	if videoID != "" {
		vid = videoID
	}
	_, err := s.ExecContext(ctx, `INSERT INTO discovery_threads (id, project_id, video_id, title, status, created_by_user_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, t.ID, projectID, vid, title, "open", userID, now, now)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// ListMessages returns thread messages in order.
func (s *Store) ListMessages(ctx context.Context, threadID string) ([]DiscoveryMessage, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, thread_id, role, content, model_id, created_at FROM discovery_messages WHERE thread_id=? ORDER BY created_at`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DiscoveryMessage
	for rows.Next() {
		var m DiscoveryMessage
		if err := rows.Scan(&m.ID, &m.ThreadID, &m.Role, &m.Content, &m.ModelID, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AppendMessage adds an append-only message.
func (s *Store) AppendMessage(ctx context.Context, threadID, role, content, modelID, userID string) (*DiscoveryMessage, error) {
	now := db.Now()
	m := &DiscoveryMessage{ID: ids.New(), ThreadID: threadID, Role: role, Content: content, CreatedAt: now}
	if modelID != "" {
		m.ModelID = sql.NullString{String: modelID, Valid: true}
	}
	var mid, uid any
	if modelID != "" {
		mid = modelID
	}
	if userID != "" {
		uid = userID
	}
	_, err := s.ExecContext(ctx, `INSERT INTO discovery_messages (id, thread_id, role, content, model_id, created_by_user_id, created_at)
		VALUES (?,?,?,?,?,?,?)`, m.ID, threadID, role, content, mid, uid, now)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// CreateProposal stores an inspectable proposal.
func (s *Store) CreateProposal(ctx context.Context, projectID, videoID, proposalType, payloadJSON, summary, modelID, userID string, sourceRevision int) (*Proposal, error) {
	now := db.Now()
	p := &Proposal{ID: ids.New(), ProjectID: projectID, ProposalType: proposalType, PayloadJSON: payloadJSON, Summary: summary, Status: "proposed", CreatedAt: now}
	if videoID != "" {
		p.VideoID = sql.NullString{String: videoID, Valid: true}
	}
	if modelID != "" {
		p.ModelID = sql.NullString{String: modelID, Valid: true}
	}
	var vid, mid, uid any
	if videoID != "" {
		vid = videoID
	}
	if modelID != "" {
		mid = modelID
	}
	if userID != "" {
		uid = userID
	}
	_, err := s.ExecContext(ctx, `INSERT INTO proposals (id, project_id, video_id, proposal_type, source_revision, payload_json, summary, status, proposed_by_type, proposed_by_user_id, model_id, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, p.ID, projectID, vid, proposalType, sourceRevision, payloadJSON, summary, "proposed", "model", uid, mid, now)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// GetProposal fetches a proposal.
func (s *Store) GetProposal(ctx context.Context, id string) (*Proposal, error) {
	row := s.QueryRowContext(ctx, `SELECT id, project_id, video_id, proposal_type, payload_json, summary, status, model_id, decision_reason, regenerated_from_id, created_at FROM proposals WHERE id=?`, id)
	var p Proposal
	err := row.Scan(&p.ID, &p.ProjectID, &p.VideoID, &p.ProposalType, &p.PayloadJSON, &p.Summary, &p.Status, &p.ModelID, &p.DecisionReason, &p.RegeneratedFromID, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// ListProposals returns proposals for a project/video.
func (s *Store) ListProposals(ctx context.Context, projectID, videoID string) ([]Proposal, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, project_id, video_id, proposal_type, payload_json, summary, status, model_id, created_at
		FROM proposals WHERE project_id=? AND (video_id=? OR video_id IS NULL) ORDER BY created_at DESC`, projectID, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Proposal
	for rows.Next() {
		var p Proposal
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.VideoID, &p.ProposalType, &p.PayloadJSON, &p.Summary, &p.Status, &p.ModelID, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DecideProposal sets the terminal status of a proposal.
func (s *Store) DecideProposal(ctx context.Context, id, status, userID string) error {
	return s.DecideProposalWithReason(ctx, id, status, "", userID)
}

// DecideProposalWithReason records a decision and an optional reason (§8.6).
func (s *Store) DecideProposalWithReason(ctx context.Context, id, status, reason, userID string) error {
	_, err := s.ExecContext(ctx, `UPDATE proposals SET status=?, decision_reason=?, decided_at=?, decided_by_user_id=? WHERE id=?`,
		status, reason, db.Now(), userID, id)
	return err
}

// MarkProposalRegenerated links a regenerated proposal to its predecessor.
func (s *Store) MarkProposalRegenerated(ctx context.Context, id, fromID string) error {
	_, err := s.ExecContext(ctx, `UPDATE proposals SET regenerated_from_id=? WHERE id=?`, fromID, id)
	return err
}

// GetCurrentBrief returns the non-superseded brief, or nil.
func (s *Store) GetCurrentBrief(ctx context.Context, projectID string) (*Brief, error) {
	row := s.QueryRowContext(ctx, `SELECT id, project_id, version, premise, theme, audience, desired_reaction, pace, excitement, music_intent, sound_intent, visual_style, camera_language, production_notes, source_proposal_id, created_at, superseded_at
		FROM project_briefs WHERE project_id=? AND superseded_at IS NULL`, projectID)
	return scanBrief(row)
}

// ListBriefHistory returns brief versions newest first.
func (s *Store) ListBriefHistory(ctx context.Context, projectID string) ([]Brief, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, project_id, version, premise, theme, audience, desired_reaction, pace, excitement, music_intent, sound_intent, visual_style, camera_language, production_notes, source_proposal_id, created_at, superseded_at
		FROM project_briefs WHERE project_id=? ORDER BY version DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Brief
	for rows.Next() {
		b, err := scanBriefRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func scanBrief(row *sql.Row) (*Brief, error) {
	b, err := scanBriefFields(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return b, err
}

func scanBriefRows(rows *sql.Rows) (*Brief, error) {
	return scanBriefFields(rows)
}

func scanBriefFields(sc scanner) (*Brief, error) {
	var b Brief
	err := sc.Scan(&b.ID, &b.ProjectID, &b.Version, &b.Premise, &b.Theme, &b.Audience, &b.DesiredReaction, &b.Pace, &b.Excitement, &b.MusicIntent, &b.SoundIntent, &b.VisualStyle, &b.CameraLanguage, &b.ProductionNotes, &b.SourceProposalID, &b.CreatedAt, &b.SupersededAt)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// InsertBrief supersedes the current brief and writes a new version.
func (s *Store) InsertBrief(ctx context.Context, b Brief, userID string) (*Brief, error) {
	now := db.Now()
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var nextVersion int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM project_briefs WHERE project_id=?`, b.ProjectID).Scan(&nextVersion); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE project_briefs SET superseded_at=? WHERE project_id=? AND superseded_at IS NULL`, now, b.ProjectID); err != nil {
		return nil, err
	}
	var source any
	if b.SourceProposalID.Valid {
		source = b.SourceProposalID.String
	}
	id := ids.New()
	if _, err := tx.ExecContext(ctx, `INSERT INTO project_briefs (id, project_id, version, premise, theme, audience, desired_reaction, pace, excitement, music_intent, sound_intent, visual_style, camera_language, production_notes, source_proposal_id, created_by_user_id, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, b.ProjectID, nextVersion, b.Premise, b.Theme, b.Audience, b.DesiredReaction, b.Pace, b.Excitement, b.MusicIntent, b.SoundIntent, b.VisualStyle, b.CameraLanguage, b.ProductionNotes, source, userID, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	b.ID = id
	b.Version = nextVersion
	b.CreatedAt = now
	return &b, nil
}

// ListAudit returns recent audit events for a workspace.
func (s *Store) ListAudit(ctx context.Context, workspaceID string, limit int) ([]AuditEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.QueryContext(ctx, `SELECT id, action, entity_type, entity_id, summary, created_at
		FROM audit_events WHERE workspace_id=? ORDER BY created_at DESC LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.ID, &e.Action, &e.EntityType, &e.EntityID, &e.Summary, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
