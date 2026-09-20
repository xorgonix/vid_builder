package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"vidbuild/internal/db"
	"vidbuild/internal/ids"
)

// WorkflowPackage is a friendly logical workflow.
type WorkflowPackage struct {
	ID            string
	WorkspaceID   sql.NullString
	Name          string
	Slug          string
	Task          string
	Description   string
	Source        string
	Active        bool
	Revision      int
	LatestVersion sql.NullString
	VersionStatus sql.NullString
}

// WorkflowVersion is an immutable imported graph snapshot.
type WorkflowVersion struct {
	ID                   string
	WorkflowPackageID    string
	SemanticVersion      string
	GraphJSON            string
	GraphSHA             string
	ManifestJSON         string
	Status               string
	TestedComfyUIVersion string
	CreatedAt            string
}

// WorkflowBinding maps a semantic field to a node input.
type WorkflowBinding struct {
	ID                string
	WorkflowVersionID string
	SemanticField     string
	NodeID            string
	InputName         string
	ValueType         string
	Required          bool
	DefaultJSON       string
}

// ExecutionTarget is a place generation can run.
type ExecutionTarget struct {
	ID           string
	WorkspaceID  string
	Name         string
	Kind         string
	Capabilities string
	Pricing      string
	Enabled      bool
	Revision     int
}

// Job is a durable unit of work.
type Job struct {
	ID             string
	WorkspaceID    string
	JobType        string
	AggregateType  string
	AggregateID    string
	State          string
	Priority       int
	RequestJSON    string
	LeaseOwner     sql.NullString
	LeaseExpiresAt sql.NullString
	AttemptCount   int
	CreatedAt      string
	StartedAt      sql.NullString
	CompletedAt    sql.NullString
	ErrorCode      sql.NullString
	ErrorMessage   sql.NullString
}

// GenerationRequest freezes desired work.
type GenerationRequest struct {
	ID                string
	ProjectID         string
	VideoID           string
	ShotID            string
	ShotSpecID        string
	SpecVersion       int
	OutputRole        string
	QualityTier       string
	CandidateCount    int
	WorkflowVersionID sql.NullString
	ModelProfileID    sql.NullString
	Status            string
	Revision          int
	RequestJSON       string
	CompilationJSON   string
	CreatedAt         string
}

// GenerationAttempt is one immutable execution.
type GenerationAttempt struct {
	ID                string
	RequestID         string
	AttemptNumber     int
	ExecutionTargetID sql.NullString
	ModelID           sql.NullString
	WorkflowVersionID sql.NullString
	WorkflowGraphSHA  string
	ResolvedPrompts   string
	Seed              sql.NullInt64
	SettingsJSON      string
	Status            string
	QueuedAt          string
	StartedAt         sql.NullString
	CompletedAt       sql.NullString
	ErrorCode         sql.NullString
	ErrorMessage      sql.NullString
	DurationMs        sql.NullInt64
}

// Asset is a produced media file.
type Asset struct {
	ID                  string
	WorkspaceID         string
	ProjectID           string
	GenerationAttemptID sql.NullString
	Kind                string
	Role                string
	StorageURI          string
	MimeType            string
	SizeBytes           int64
	Checksum            string
	Width               sql.NullInt64
	Height              sql.NullInt64
	DurationMs          sql.NullInt64
	CreatedAt           string
}

// Selection is the current asset filling a shot role.
type Selection struct {
	ID           string
	ShotID       string
	Role         string
	AssetID      string
	ShotRevision int
	SelectedAt   string
	SelectedBy   string
}

// Approval is a human production decision.
type Approval struct {
	ID        string
	AssetID   string
	Decision  string
	Reason    string
	Checksum  string
	UserID    string
	CreatedAt string
}

// Assessment is advisory evidence.
type Assessment struct {
	ID           string
	AssetID      string
	Type         string
	AssessorType string
	Status       string
	Score        sql.NullInt64
	Summary      string
	CreatedAt    string
}

// SeedBundledWorkflow inserts a bundled workflow package/version/bindings once.
// If a package exists but has no version (e.g. a previous partial seed), the
// missing version and bindings are filled in.
func (s *Store) SeedBundledWorkflow(ctx context.Context, name, slug, task, description, graphJSON, graphSHA, manifestJSON, tested string, bindings []WorkflowBinding) error {
	now := db.Now()
	var pkgID string
	err := s.QueryRowContext(ctx, `SELECT id FROM workflow_packages WHERE slug=?`, slug).Scan(&pkgID)
	if errors.Is(err, sql.ErrNoRows) {
		pkgID = ids.New()
		if _, err := s.ExecContext(ctx, `INSERT INTO workflow_packages (id, workspace_id, name, slug, task, description, source, active, created_at, updated_at)
			VALUES (?,NULL,?,?,?,?, 'bundled',1,?,?)`, pkgID, name, slug, task, description, now, now); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	var versionCount int
	if err := s.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflow_versions WHERE workflow_package_id=?`, pkgID).Scan(&versionCount); err != nil {
		return err
	}
	if versionCount > 0 {
		return nil
	}
	verID := ids.New()
	if _, err := s.ExecContext(ctx, `INSERT INTO workflow_versions (id, workflow_package_id, semantic_version, graph_json, graph_sha256, manifest_json, status, tested_comfyui_version, created_at)
		VALUES (?,?,'1.0.0',?,?,?, 'validated', ?, ?)`, verID, pkgID, graphJSON, graphSHA, manifestJSON, tested, now); err != nil {
		return err
	}
	for _, b := range bindings {
		req := 0
		if b.Required {
			req = 1
		}
		def := b.DefaultJSON
		if def == "" {
			def = "{}"
		}
		if _, err := s.ExecContext(ctx, `INSERT INTO workflow_bindings (id, workflow_version_id, semantic_field, node_id, input_name, value_type, required, default_json, created_at)
			VALUES (?,?,?,?,?,?,?,?,?)`, ids.New(), verID, b.SemanticField, b.NodeID, b.InputName, b.ValueType, req, def, now); err != nil {
			return err
		}
	}
	return nil
}

// ListWorkflowPackages returns packages with their latest version.
func (s *Store) ListWorkflowPackages(ctx context.Context) ([]WorkflowPackage, error) {
	rows, err := s.QueryContext(ctx, `SELECT p.id, p.workspace_id, p.name, p.slug, p.task, p.description, p.source, p.active, p.revision,
		(SELECT v.id FROM workflow_versions v WHERE v.workflow_package_id=p.id ORDER BY v.created_at DESC LIMIT 1),
		(SELECT v.status FROM workflow_versions v WHERE v.workflow_package_id=p.id ORDER BY v.created_at DESC LIMIT 1)
		FROM workflow_packages p WHERE p.active=1 ORDER BY p.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkflowPackage
	for rows.Next() {
		var p WorkflowPackage
		var active int
		if err := rows.Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.Slug, &p.Task, &p.Description, &p.Source, &active, &p.Revision, &p.LatestVersion, &p.VersionStatus); err != nil {
			return nil, err
		}
		p.Active = active == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetWorkflowVersion fetches a version.
func (s *Store) GetWorkflowVersion(ctx context.Context, id string) (*WorkflowVersion, error) {
	row := s.QueryRowContext(ctx, `SELECT id, workflow_package_id, semantic_version, graph_json, graph_sha256, manifest_json, status, tested_comfyui_version, created_at
		FROM workflow_versions WHERE id=?`, id)
	var v WorkflowVersion
	err := row.Scan(&v.ID, &v.WorkflowPackageID, &v.SemanticVersion, &v.GraphJSON, &v.GraphSHA, &v.ManifestJSON, &v.Status, &v.TestedComfyUIVersion, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &v, err
}

// ListBindings returns bindings for a version.
func (s *Store) ListBindings(ctx context.Context, versionID string) ([]WorkflowBinding, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, workflow_version_id, semantic_field, node_id, input_name, value_type, required, default_json
		FROM workflow_bindings WHERE workflow_version_id=? ORDER BY semantic_field`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkflowBinding
	for rows.Next() {
		var b WorkflowBinding
		var req int
		if err := rows.Scan(&b.ID, &b.WorkflowVersionID, &b.SemanticField, &b.NodeID, &b.InputName, &b.ValueType, &req, &b.DefaultJSON); err != nil {
			return nil, err
		}
		b.Required = req == 1
		out = append(out, b)
	}
	return out, rows.Err()
}

// EnsureLocalComfyTarget creates the default loopback ComfyUI target once.
func (s *Store) EnsureLocalComfyTarget(ctx context.Context, workspaceID, userID string) (*ExecutionTarget, error) {
	row := s.QueryRowContext(ctx, `SELECT id, workspace_id, name, kind, capabilities_json, pricing_json, enabled, revision
		FROM execution_targets WHERE workspace_id=? AND kind='local_comfy' LIMIT 1`, workspaceID)
	var t ExecutionTarget
	var enabled int
	err := row.Scan(&t.ID, &t.WorkspaceID, &t.Name, &t.Kind, &t.Capabilities, &t.Pricing, &enabled, &t.Revision)
	if err == nil {
		t.Enabled = enabled == 1
		return &t, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	now := db.Now()
	t = ExecutionTarget{ID: ids.New(), WorkspaceID: workspaceID, Name: "Local ComfyUI", Kind: "local_comfy", Capabilities: `{"tasks":["image"],"supports_seed":true,"supports_negative_prompt":true,"loopback":true}`, Pricing: `{"kind":"local"}`, Enabled: true, Revision: 1}
	if _, err := s.ExecContext(ctx, `INSERT INTO execution_targets (id, workspace_id, name, kind, capabilities_json, pricing_json, enabled, created_by_user_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,1,?,?,?)`, t.ID, workspaceID, t.Name, t.Kind, t.Capabilities, t.Pricing, userID, now, now); err != nil {
		return nil, err
	}
	return &t, nil
}

// ListExecutionTargets returns workspace targets.
func (s *Store) ListExecutionTargets(ctx context.Context, workspaceID string) ([]ExecutionTarget, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, workspace_id, name, kind, capabilities_json, pricing_json, enabled, revision FROM execution_targets WHERE workspace_id=? ORDER BY name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExecutionTarget
	for rows.Next() {
		var t ExecutionTarget
		var enabled int
		if err := rows.Scan(&t.ID, &t.WorkspaceID, &t.Name, &t.Kind, &t.Capabilities, &t.Pricing, &enabled, &t.Revision); err != nil {
			return nil, err
		}
		t.Enabled = enabled == 1
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetExecutionTarget fetches a target.
func (s *Store) GetExecutionTarget(ctx context.Context, id string) (*ExecutionTarget, error) {
	row := s.QueryRowContext(ctx, `SELECT id, workspace_id, name, kind, capabilities_json, pricing_json, enabled, revision FROM execution_targets WHERE id=?`, id)
	var t ExecutionTarget
	var enabled int
	err := row.Scan(&t.ID, &t.WorkspaceID, &t.Name, &t.Kind, &t.Capabilities, &t.Pricing, &enabled, &t.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	t.Enabled = enabled == 1
	return &t, err
}

// RequestInput carries frozen work for a generation request.
type RequestInput struct {
	WorkspaceID       string
	ProjectID         string
	VideoID           string
	ShotID            string
	ShotSpecID        string
	SpecVersion       int
	OutputRole        string
	QualityTier       string
	CandidateCount    int
	WorkflowVersionID string
	RequestJSON       string
	CompilationJSON   string
	Enqueue           bool
	CampaignID        string
}

// CreateGenerationRequest inserts a request plus candidate attempts and a job.
func (s *Store) CreateGenerationRequest(ctx context.Context, in RequestInput, userID string) (*GenerationRequest, []GenerationAttempt, *Job, error) {
	now := db.Now()
	req := &GenerationRequest{
		ID: ids.New(), ProjectID: in.ProjectID, VideoID: in.VideoID, ShotID: in.ShotID, ShotSpecID: in.ShotSpecID,
		SpecVersion: in.SpecVersion, OutputRole: in.OutputRole, QualityTier: in.QualityTier, CandidateCount: in.CandidateCount,
		Status: "queued", Revision: 1, CreatedAt: now,
	}
	if in.WorkflowVersionID != "" {
		req.WorkflowVersionID = sql.NullString{String: in.WorkflowVersionID, Valid: true}
	}
	requestJSON := in.RequestJSON
	if requestJSON == "" {
		requestJSON = "{}"
	}
	compilationJSON := in.CompilationJSON
	if compilationJSON == "" {
		compilationJSON = "{}"
	}
	status := "draft"
	if in.Enqueue {
		status = "queued"
	}
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `INSERT INTO generation_requests (id, project_id, video_id, shot_id, shot_spec_id, spec_version, workflow_version_id, output_role, quality_tier, candidate_count, status, request_json, prompt_compilation_json, created_by_user_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		req.ID, in.ProjectID, in.VideoID, in.ShotID, in.ShotSpecID, in.SpecVersion, nullStr(in.WorkflowVersionID), in.OutputRole, in.QualityTier, in.CandidateCount, status, requestJSON, compilationJSON, userID, now, now); err != nil {
		return nil, nil, nil, fmt.Errorf("insert request: %w", err)
	}
	attempts := make([]GenerationAttempt, 0, in.CandidateCount)
	for i := 1; i <= in.CandidateCount; i++ {
		a := GenerationAttempt{ID: ids.New(), RequestID: req.ID, AttemptNumber: i, Status: "queued", QueuedAt: now, ResolvedPrompts: "{}", SettingsJSON: "{}"}
		if in.WorkflowVersionID != "" {
			a.WorkflowVersionID = sql.NullString{String: in.WorkflowVersionID, Valid: true}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO generation_attempts (id, generation_request_id, attempt_number, workflow_version_id, resolved_prompts_json, settings_json, status, queued_at)
			VALUES (?,?,?,?,?,?,?,?)`, a.ID, req.ID, i, nullStr(in.WorkflowVersionID), "{}", "{}", "queued", now); err != nil {
			return nil, nil, nil, fmt.Errorf("insert attempt: %w", err)
		}
		attempts = append(attempts, a)
	}
	var job *Job
	if in.Enqueue {
		job = &Job{ID: ids.New(), WorkspaceID: in.WorkspaceID, JobType: "generate", AggregateType: "generation_request", AggregateID: req.ID, State: "queued", Priority: 0, RequestJSON: "{}", CreatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO jobs (id, workspace_id, job_type, aggregate_type, aggregate_id, state, priority, request_json, created_at)
			VALUES (?,?,?,?,?,?,?,?,?)`, job.ID, job.WorkspaceID, job.JobType, job.AggregateType, job.AggregateID, "queued", 0, "{}", now); err != nil {
			return nil, nil, nil, fmt.Errorf("insert job: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO job_events (id, job_id, sequence, phase, level, message, progress_basis_points, created_at)
			VALUES (?,?,0,'queued','info','Generation request queued',0,?)`, ids.New(), job.ID, now); err != nil {
			return nil, nil, nil, fmt.Errorf("insert job event: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, nil, err
	}
	req.Status = status
	return req, attempts, job, nil
}

// EnqueueGenerationJob creates a generate job for an existing request.
func (s *Store) EnqueueGenerationJob(ctx context.Context, workspaceID, requestID, campaignID string) (*Job, error) {
	now := db.Now()
	j := &Job{ID: ids.New(), WorkspaceID: workspaceID, JobType: "generate", AggregateType: "generation_request", AggregateID: requestID, State: "queued", CreatedAt: now}
	var camp any
	if campaignID != "" {
		camp = campaignID
	}
	if _, err := s.ExecContext(ctx, `INSERT INTO jobs (id, workspace_id, campaign_id, job_type, aggregate_type, aggregate_id, state, priority, request_json, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`, j.ID, workspaceID, camp, "generate", "generation_request", requestID, "queued", 0, "{}", now); err != nil {
		return nil, err
	}
	if _, err := s.ExecContext(ctx, `INSERT INTO job_events (id, job_id, sequence, phase, level, message, progress_basis_points, created_at)
		VALUES (?,?,0,'queued','info','Generation request queued by campaign',0,?)`, ids.New(), j.ID, now); err != nil {
		return nil, err
	}
	if err := s.SetRequestStatus(ctx, requestID, "queued"); err != nil {
		return nil, err
	}
	return j, nil
}

func nullStr(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// ListRequestsForVideo returns requests for a video.
func (s *Store) ListRequestsForVideo(ctx context.Context, videoID string) ([]GenerationRequest, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, project_id, video_id, shot_id, shot_spec_id, spec_version, output_role, quality_tier, candidate_count, workflow_version_id, model_profile_id, status, revision, created_at
		FROM generation_requests WHERE video_id=? ORDER BY created_at DESC`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GenerationRequest
	for rows.Next() {
		var r GenerationRequest
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.VideoID, &r.ShotID, &r.ShotSpecID, &r.SpecVersion, &r.OutputRole, &r.QualityTier, &r.CandidateCount, &r.WorkflowVersionID, &r.ModelProfileID, &r.Status, &r.Revision, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRequest fetches a request.
func (s *Store) GetRequest(ctx context.Context, id string) (*GenerationRequest, error) {
	row := s.QueryRowContext(ctx, `SELECT id, project_id, video_id, shot_id, shot_spec_id, spec_version, output_role, quality_tier, candidate_count, workflow_version_id, model_profile_id, status, revision, request_json, prompt_compilation_json, created_at
		FROM generation_requests WHERE id=?`, id)
	var r GenerationRequest
	err := row.Scan(&r.ID, &r.ProjectID, &r.VideoID, &r.ShotID, &r.ShotSpecID, &r.SpecVersion, &r.OutputRole, &r.QualityTier, &r.CandidateCount, &r.WorkflowVersionID, &r.ModelProfileID, &r.Status, &r.Revision, &r.RequestJSON, &r.CompilationJSON, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &r, err
}

// ListAttempts returns attempts for a request.
func (s *Store) ListAttempts(ctx context.Context, requestID string) ([]GenerationAttempt, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, generation_request_id, attempt_number, execution_target_id, model_id, workflow_version_id, workflow_graph_sha256, resolved_prompts_json, seed, settings_json, status, queued_at, started_at, completed_at, error_code, error_message, duration_ms
		FROM generation_attempts WHERE generation_request_id=? ORDER BY attempt_number`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GenerationAttempt
	for rows.Next() {
		a, err := scanAttempt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// GetAttempt fetches an attempt.
func (s *Store) GetAttempt(ctx context.Context, id string) (*GenerationAttempt, error) {
	row := s.QueryRowContext(ctx, `SELECT id, generation_request_id, attempt_number, execution_target_id, model_id, workflow_version_id, workflow_graph_sha256, resolved_prompts_json, seed, settings_json, status, queued_at, started_at, completed_at, error_code, error_message, duration_ms
		FROM generation_attempts WHERE id=?`, id)
	return scanAttempt(row)
}

type rowScanner interface{ Scan(dest ...any) error }

func scanAttempt(sc rowScanner) (*GenerationAttempt, error) {
	var a GenerationAttempt
	err := sc.Scan(&a.ID, &a.RequestID, &a.AttemptNumber, &a.ExecutionTargetID, &a.ModelID, &a.WorkflowVersionID, &a.WorkflowGraphSHA, &a.ResolvedPrompts, &a.Seed, &a.SettingsJSON, &a.Status, &a.QueuedAt, &a.StartedAt, &a.CompletedAt, &a.ErrorCode, &a.ErrorMessage, &a.DurationMs)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &a, err
}

// ClaimJob atomically claims the next queued job.
func (s *Store) ClaimJob(ctx context.Context, leaseOwner string, leaseTTLSeconds int) (*Job, error) {
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM jobs WHERE state='queued' ORDER BY priority DESC, created_at LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	now := db.Now()
	expires := db.Format(nowTime().Add(secondsDuration(leaseTTLSeconds)))
	res, err := tx.ExecContext(ctx, `UPDATE jobs SET state='claimed', lease_owner=?, lease_expires_at=?, started_at=COALESCE(started_at,?), attempt_count=attempt_count+1
		WHERE id=? AND state='queued'`, leaseOwner, expires, now, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrConflict
	}
	j, err := getJobTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return j, nil
}

func getJobTx(ctx context.Context, tx *sql.Tx, id string) (*Job, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, workspace_id, job_type, aggregate_type, aggregate_id, state, priority, request_json, lease_owner, lease_expires_at, attempt_count, created_at, started_at, completed_at, error_code, error_message FROM jobs WHERE id=?`, id)
	return scanJob(row)
}

func scanJob(sc rowScanner) (*Job, error) {
	var j Job
	err := sc.Scan(&j.ID, &j.WorkspaceID, &j.JobType, &j.AggregateType, &j.AggregateID, &j.State, &j.Priority, &j.RequestJSON, &j.LeaseOwner, &j.LeaseExpiresAt, &j.AttemptCount, &j.CreatedAt, &j.StartedAt, &j.CompletedAt, &j.ErrorCode, &j.ErrorMessage)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &j, err
}

// GetJob fetches a job.
func (s *Store) GetJob(ctx context.Context, id string) (*Job, error) {
	row := s.QueryRowContext(ctx, `SELECT id, workspace_id, job_type, aggregate_type, aggregate_id, state, priority, request_json, lease_owner, lease_expires_at, attempt_count, created_at, started_at, completed_at, error_code, error_message FROM jobs WHERE id=?`, id)
	return scanJob(row)
}

// ListActiveJobs returns queued/claimed/running jobs.
func (s *Store) ListActiveJobs(ctx context.Context) ([]Job, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, workspace_id, job_type, aggregate_type, aggregate_id, state, priority, request_json, lease_owner, lease_expires_at, attempt_count, created_at, started_at, completed_at, error_code, error_message
		FROM jobs WHERE state IN ('queued','claimed','running','cancel_requested') ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

// UpdateJobState transitions a job and records an event.
func (s *Store) UpdateJobState(ctx context.Context, id, state, phase, level, message string, progressBP int) error {
	now := db.Now()
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	completed := ""
	if state == "succeeded" || state == "failed" || state == "cancelled" {
		completed = now
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET state=?, completed_at=NULLIF(?,''), error_message=CASE WHEN ?='failed' THEN ? ELSE error_message END WHERE id=?`, state, completed, state, message, id); err != nil {
		return err
	}
	var seq int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),-1)+1 FROM job_events WHERE job_id=?`, id).Scan(&seq); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO job_events (id, job_id, sequence, phase, level, message, progress_basis_points, created_at)
		VALUES (?,?,?,?,?,?,?,?)`, ids.New(), id, seq, phase, level, message, progressBP, now); err != nil {
		return err
	}
	return tx.Commit()
}

// JobEvent is a user-facing/diagnostic job line.
type JobEvent struct {
	Sequence  int
	Phase     string
	Level     string
	Message   string
	Progress  int
	CreatedAt string
}

// ListJobEvents returns events for a job.
func (s *Store) ListJobEvents(ctx context.Context, jobID string) ([]JobEvent, error) {
	rows, err := s.QueryContext(ctx, `SELECT sequence, phase, level, message, COALESCE(progress_basis_points,0), created_at FROM job_events WHERE job_id=? ORDER BY sequence`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobEvent
	for rows.Next() {
		var e JobEvent
		if err := rows.Scan(&e.Sequence, &e.Phase, &e.Level, &e.Message, &e.Progress, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// MarkAttemptRunning sets an attempt to running.
func (s *Store) MarkAttemptRunning(ctx context.Context, id, targetID, modelID string, seed int64) error {
	now := db.Now()
	_, err := s.ExecContext(ctx, `UPDATE generation_attempts SET status='running', started_at=?, execution_target_id=?, model_id=?, seed=? WHERE id=?`,
		now, nullStr(targetID), nullStr(modelID), seed, id)
	return err
}

// RecordAttemptSuccess stores receipt fields and marks the attempt succeeded.
func (s *Store) RecordAttemptSuccess(ctx context.Context, id, graphSHA, resolvedPrompts, boundInputs, receipt string, durationMs int64) error {
	now := db.Now()
	_, err := s.ExecContext(ctx, `UPDATE generation_attempts SET status='succeeded', completed_at=?, workflow_graph_sha256=?, resolved_prompts_json=?, bound_inputs_json=?, provider_request_receipt_json=?, duration_ms=? WHERE id=?`,
		now, graphSHA, resolvedPrompts, boundInputs, receipt, durationMs, id)
	return err
}

// RecordAttemptFailure marks an attempt failed with a diagnostic.
func (s *Store) RecordAttemptFailure(ctx context.Context, id, code, message string) error {
	now := db.Now()
	_, err := s.ExecContext(ctx, `UPDATE generation_attempts SET status='failed', completed_at=?, error_code=?, error_message=? WHERE id=?`, now, code, message, id)
	return err
}

// SetRequestStatus updates a request status.
func (s *Store) SetRequestStatus(ctx context.Context, id, status string) error {
	_, err := s.ExecContext(ctx, `UPDATE generation_requests SET status=?, updated_at=? WHERE id=?`, status, db.Now(), id)
	return err
}

// CreateAsset inserts an asset row.
func (s *Store) CreateAsset(ctx context.Context, a Asset) (string, error) {
	id := ids.New()
	a.ID = id
	_, err := s.ExecContext(ctx, `INSERT INTO assets (id, workspace_id, project_id, generation_attempt_id, kind, role, storage_uri, mime_type, size_bytes, checksum_sha256, width, height, frame_count, duration_ms, metadata_json, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,NULL,?, '{}', ?)`,
		id, a.WorkspaceID, a.ProjectID, a.GenerationAttemptID, a.Kind, a.Role, a.StorageURI, a.MimeType, a.SizeBytes, a.Checksum, a.Width, a.Height, a.DurationMs, db.Now())
	return id, err
}

// GetAsset fetches an asset.
func (s *Store) GetAsset(ctx context.Context, id string) (*Asset, error) {
	row := s.QueryRowContext(ctx, `SELECT id, workspace_id, project_id, generation_attempt_id, kind, role, storage_uri, mime_type, size_bytes, checksum_sha256, width, height, duration_ms, created_at
		FROM assets WHERE id=? AND deleted_at IS NULL`, id)
	var a Asset
	err := row.Scan(&a.ID, &a.WorkspaceID, &a.ProjectID, &a.GenerationAttemptID, &a.Kind, &a.Role, &a.StorageURI, &a.MimeType, &a.SizeBytes, &a.Checksum, &a.Width, &a.Height, &a.DurationMs, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &a, err
}

// ListAssetsForVideo returns assets produced by requests for a video.
func (s *Store) ListAssetsForVideo(ctx context.Context, videoID string) ([]Asset, error) {
	rows, err := s.QueryContext(ctx, `SELECT a.id, a.workspace_id, a.project_id, a.generation_attempt_id, a.kind, a.role, a.storage_uri, a.mime_type, a.size_bytes, a.checksum_sha256, a.width, a.height, a.duration_ms, a.created_at
		FROM assets a JOIN generation_attempts ga ON ga.id=a.generation_attempt_id
		JOIN generation_requests gr ON gr.id=ga.generation_request_id
		WHERE gr.video_id=? AND a.deleted_at IS NULL ORDER BY a.created_at DESC`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Asset
	for rows.Next() {
		var a Asset
		if err := rows.Scan(&a.ID, &a.WorkspaceID, &a.ProjectID, &a.GenerationAttemptID, &a.Kind, &a.Role, &a.StorageURI, &a.MimeType, &a.SizeBytes, &a.Checksum, &a.Width, &a.Height, &a.DurationMs, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CreateSelection supersedes the current selection for a shot role.
func (s *Store) CreateSelection(ctx context.Context, shotID, role, assetID, userID string, shotRevision int) error {
	now := db.Now()
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE asset_selections SET superseded_at=? WHERE shot_id=? AND role=? AND superseded_at IS NULL`, now, shotID, role); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO asset_selections (id, shot_id, role, asset_id, shot_revision, selected_by_user_id, selected_at)
		VALUES (?,?,?,?,?,?,?)`, ids.New(), shotID, role, assetID, shotRevision, userID, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"shot_id": shotID, "role": role, "asset_id": assetID})
	return s.EmitDomainEvent(ctx, "", "shot", shotID, "asset.selected", string(payload))
}

// GetCurrentSelection returns the current selection for a shot role.
func (s *Store) GetCurrentSelection(ctx context.Context, shotID, role string) (*Selection, error) {
	row := s.QueryRowContext(ctx, `SELECT id, shot_id, role, asset_id, shot_revision, selected_at, selected_by_user_id FROM asset_selections WHERE shot_id=? AND role=? AND superseded_at IS NULL`, shotID, role)
	var sel Selection
	err := row.Scan(&sel.ID, &sel.ShotID, &sel.Role, &sel.AssetID, &sel.ShotRevision, &sel.SelectedAt, &sel.SelectedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &sel, err
}

// ListAssetsForShotRole returns assets produced for a shot and output role.
func (s *Store) ListAssetsForShotRole(ctx context.Context, shotID, role string) ([]Asset, error) {
	rows, err := s.QueryContext(ctx, `SELECT a.id, a.workspace_id, a.project_id, a.generation_attempt_id, a.kind, a.role, a.storage_uri, a.mime_type, a.size_bytes, a.checksum_sha256, a.width, a.height, a.duration_ms, a.created_at
		FROM assets a JOIN generation_attempts ga ON ga.id=a.generation_attempt_id
		JOIN generation_requests gr ON gr.id=ga.generation_request_id
		WHERE gr.shot_id=? AND gr.output_role=? AND ga.status='succeeded' AND a.deleted_at IS NULL ORDER BY a.created_at DESC`, shotID, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Asset
	for rows.Next() {
		var a Asset
		if err := rows.Scan(&a.ID, &a.WorkspaceID, &a.ProjectID, &a.GenerationAttemptID, &a.Kind, &a.Role, &a.StorageURI, &a.MimeType, &a.SizeBytes, &a.Checksum, &a.Width, &a.Height, &a.DurationMs, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CreateApproval records a human decision (append-only superseding prior).
func (s *Store) CreateApproval(ctx context.Context, assetID, decision, reason, checksum, userID string, shotRevision int) error {
	_, err := s.ExecContext(ctx, `INSERT INTO approvals (id, asset_id, scope, decision, reason, asset_checksum, shot_revision, user_id, created_at)
		VALUES (?,?, 'production', ?,?,?,?,?,?)`, ids.New(), assetID, decision, reason, checksum, shotRevision, userID, db.Now())
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"asset_id": assetID, "decision": decision})
	return s.EmitDomainEvent(ctx, "", "asset", assetID, "asset.approved", string(payload))
}

// LatestApproval returns the most recent approval for an asset.
func (s *Store) LatestApproval(ctx context.Context, assetID string) (*Approval, error) {
	row := s.QueryRowContext(ctx, `SELECT id, asset_id, decision, reason, asset_checksum, user_id, created_at FROM approvals WHERE asset_id=? ORDER BY created_at DESC LIMIT 1`, assetID)
	var a Approval
	err := row.Scan(&a.ID, &a.AssetID, &a.Decision, &a.Reason, &a.Checksum, &a.UserID, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &a, err
}

// CreateAssessment records advisory evidence.
func (s *Store) CreateAssessment(ctx context.Context, assetID, assessorType, status, summary string, score sql.NullInt64) error {
	_, err := s.ExecContext(ctx, `INSERT INTO assessments (id, asset_id, assessment_type, assessor_type, status, score, criteria_json, summary, created_at)
		VALUES (?,?, 'review', ?,?,?, '{}', ?, ?)`, ids.New(), assetID, assessorType, status, score, summary, db.Now())
	return err
}

// ListAssessments returns assessments for an asset.
func (s *Store) ListAssessments(ctx context.Context, assetID string) ([]Assessment, error) {
	rows, err := s.QueryContext(ctx, `SELECT id, asset_id, assessment_type, assessor_type, status, score, summary, created_at FROM assessments WHERE asset_id=? ORDER BY created_at DESC`, assetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Assessment
	for rows.Next() {
		var a Assessment
		if err := rows.Scan(&a.ID, &a.AssetID, &a.Type, &a.AssessorType, &a.Status, &a.Score, &a.Summary, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RecordObservation stores measured evidence for estimates.
func (s *Store) RecordObservation(ctx context.Context, attemptID, targetID, modelID, workflowVersionID string, elapsedMs, startupMs, peakVRAM, costMicros int64, success bool) error {
	succ := 0
	if success {
		succ = 1
	}
	_, err := s.ExecContext(ctx, `INSERT INTO execution_observations (id, generation_attempt_id, execution_target_id, model_id, workflow_version_id, elapsed_ms, startup_ms, peak_vram_mb, cost_micros, success, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`, ids.New(), attemptID, nullStr(targetID), nullStr(modelID), nullStr(workflowVersionID), elapsedMs, startupMs, peakVRAM, costMicros, succ, db.Now())
	return err
}

// CreateExecutionTarget records a target's metadata (no raw secret is stored).
func (s *Store) CreateExecutionTarget(ctx context.Context, workspaceID, userID, name, kind string) (*ExecutionTarget, error) {
	switch kind {
	case "local_comfy", "remote_agent", "hosted_comfy", "commercial_api", "local_tool":
	default:
		kind = "remote_agent"
	}
	now := db.Now()
	t := &ExecutionTarget{ID: ids.New(), WorkspaceID: workspaceID, Name: name, Kind: kind, Capabilities: "{}", Pricing: "{}", Enabled: true, Revision: 1}
	if _, err := s.ExecContext(ctx, `INSERT INTO execution_targets (id, workspace_id, name, kind, capabilities_json, pricing_json, enabled, created_by_user_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,1,?,?,?)`, t.ID, workspaceID, name, kind, "{}", "{}", userID, now, now); err != nil {
		return nil, err
	}
	return t, nil
}

// GetJobForRequest returns the job that runs a generation request.
func (s *Store) GetJobForRequest(ctx context.Context, requestID string) (*Job, error) {
	row := s.QueryRowContext(ctx, `SELECT id, workspace_id, job_type, aggregate_type, aggregate_id, state, priority, request_json, lease_owner, lease_expires_at, attempt_count, created_at, started_at, completed_at, error_code, error_message
		FROM jobs WHERE aggregate_type='generation_request' AND aggregate_id=? ORDER BY created_at DESC LIMIT 1`, requestID)
	return scanJob(row)
}

// ListJobsForVideo returns recent jobs for a video's requests.
func (s *Store) ListJobsForVideo(ctx context.Context, videoID string) ([]Job, error) {
	rows, err := s.QueryContext(ctx, `SELECT j.id, j.workspace_id, j.job_type, j.aggregate_type, j.aggregate_id, j.state, j.priority, j.request_json, j.lease_owner, j.lease_expires_at, j.attempt_count, j.created_at, j.started_at, j.completed_at, j.error_code, j.error_message
		FROM jobs j JOIN generation_requests r ON r.id=j.aggregate_id
		WHERE j.aggregate_type='generation_request' AND r.video_id=? ORDER BY j.created_at DESC LIMIT 50`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

// ListAssetsForRequest returns assets produced by a request's attempts.
func (s *Store) ListAssetsForRequest(ctx context.Context, requestID string) ([]Asset, error) {
	rows, err := s.QueryContext(ctx, `SELECT a.id, a.workspace_id, a.project_id, a.generation_attempt_id, a.kind, a.role, a.storage_uri, a.mime_type, a.size_bytes, a.checksum_sha256, a.width, a.height, a.duration_ms, a.created_at
		FROM assets a JOIN generation_attempts ga ON ga.id=a.generation_attempt_id
		WHERE ga.generation_request_id=? AND a.deleted_at IS NULL ORDER BY a.created_at`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Asset
	for rows.Next() {
		var a Asset
		if err := rows.Scan(&a.ID, &a.WorkspaceID, &a.ProjectID, &a.GenerationAttemptID, &a.Kind, &a.Role, &a.StorageURI, &a.MimeType, &a.SizeBytes, &a.Checksum, &a.Width, &a.Height, &a.DurationMs, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAttemptByAsset returns the attempt that produced an asset.
func (s *Store) GetAttemptByAsset(ctx context.Context, assetID string) (*GenerationAttempt, error) {
	row := s.QueryRowContext(ctx, `SELECT a.id, a.generation_request_id, a.attempt_number, a.execution_target_id, a.model_id, a.workflow_version_id, a.workflow_graph_sha256, a.resolved_prompts_json, a.seed, a.settings_json, a.status, a.queued_at, a.started_at, a.completed_at, a.error_code, a.error_message, a.duration_ms
		FROM generation_attempts a JOIN assets ast ON ast.generation_attempt_id=a.id WHERE ast.id=?`, assetID)
	return scanAttempt(row)
}

// EmitDomainEvent appends to the transactional outbox.
func (s *Store) EmitDomainEvent(ctx context.Context, workspaceID, aggregateType, aggregateID, eventType, payloadJSON string) error {
	if payloadJSON == "" {
		payloadJSON = "{}"
	}
	_, err := s.ExecContext(ctx, `INSERT INTO domain_events (id, workspace_id, aggregate_type, aggregate_id, event_type, payload_json, created_at)
		VALUES (?,?,?,?,?,?,?)`, ids.New(), nullStr(workspaceID), aggregateType, aggregateID, eventType, payloadJSON, db.Now())
	return err
}
