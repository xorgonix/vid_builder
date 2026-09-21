-- vid-build 0.01 — SQLite schema
-- Rules (spec §6): foreign_keys ON, WAL, STRICT tables, TEXT UUID ids,
-- UTC RFC3339 timestamps, INTEGER ms durations, JSON with json_valid checks,
-- immutable history rows, partial unique indexes for "current" rows (spec §9).

CREATE TABLE IF NOT EXISTS users (
    id              TEXT PRIMARY KEY,
    email_normalized TEXT NOT NULL UNIQUE,
    display_name    TEXT NOT NULL,
    password_hash   TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    is_system_admin INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  BLOB NOT NULL UNIQUE,
    csrf_secret BLOB NOT NULL,
    created_at  TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at  TEXT NOT NULL,
    revoked_at  TEXT,
    user_agent  TEXT,
    ip_hint     TEXT
) STRICT;

CREATE TABLE IF NOT EXISTS workspaces (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    slug             TEXT NOT NULL UNIQUE,
    kind             TEXT NOT NULL CHECK (kind IN ('personal','client','team')),
    created_by_user_id TEXT NOT NULL REFERENCES users(id),
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    revision         INTEGER NOT NULL DEFAULT 1,
    archived_at      TEXT
) STRICT;

CREATE TABLE IF NOT EXISTS workspace_members (
    id          TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role        TEXT NOT NULL CHECK (role IN ('owner','editor','reviewer','viewer')),
    status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    joined_at   TEXT NOT NULL,
    invited_by_user_id TEXT,
    UNIQUE (workspace_id, user_id)
) STRICT;

CREATE TABLE IF NOT EXISTS projects (
    id               TEXT PRIMARY KEY,
    workspace_id     TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    title            TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','paused','completed','archived')),
    created_by_user_id TEXT NOT NULL REFERENCES users(id),
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    revision         INTEGER NOT NULL DEFAULT 1,
    archived_at      TEXT
) STRICT;

CREATE TABLE IF NOT EXISTS project_briefs (
    id               TEXT PRIMARY KEY,
    project_id       TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    version          INTEGER NOT NULL,
    premise          TEXT NOT NULL DEFAULT '',
    theme            TEXT NOT NULL DEFAULT '',
    audience         TEXT NOT NULL DEFAULT '',
    desired_reaction TEXT NOT NULL DEFAULT '',
    pace             TEXT NOT NULL DEFAULT '',
    excitement       TEXT NOT NULL DEFAULT '',
    music_intent     TEXT NOT NULL DEFAULT '',
    sound_intent     TEXT NOT NULL DEFAULT '',
    visual_style     TEXT NOT NULL DEFAULT '',
    camera_language  TEXT NOT NULL DEFAULT '',
    production_notes TEXT NOT NULL DEFAULT '',
    source_proposal_id TEXT,
    created_by_user_id TEXT NOT NULL REFERENCES users(id),
    created_at       TEXT NOT NULL,
    superseded_at    TEXT
) STRICT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_briefs_current
    ON project_briefs (project_id) WHERE superseded_at IS NULL;

CREATE TABLE IF NOT EXISTS characters (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    continuity_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(continuity_json)),
    status          TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    revision        INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE TABLE IF NOT EXISTS locations (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    continuity_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(continuity_json)),
    status          TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    revision        INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE TABLE IF NOT EXISTS style_rules (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    category        TEXT NOT NULL DEFAULT 'general',
    rule_text       TEXT NOT NULL,
    priority        INTEGER NOT NULL DEFAULT 0,
    active          INTEGER NOT NULL DEFAULT 1,
    created_by_user_id TEXT NOT NULL REFERENCES users(id),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    revision        INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE TABLE IF NOT EXISTS discovery_threads (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    video_id        TEXT,
    title           TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','closed')),
    created_by_user_id TEXT NOT NULL REFERENCES users(id),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS discovery_messages (
    id          TEXT PRIMARY KEY,
    thread_id   TEXT NOT NULL REFERENCES discovery_threads(id) ON DELETE CASCADE,
    role        TEXT NOT NULL CHECK (role IN ('creator','planner','system')),
    content     TEXT NOT NULL,
    model_id    TEXT,
    created_by_user_id TEXT,
    created_at  TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS proposals (
    id               TEXT PRIMARY KEY,
    project_id       TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    video_id         TEXT,
    proposal_type    TEXT NOT NULL CHECK (proposal_type IN ('project_brief','video_outline','scene_plan','shot_plan','blocking','prompt_revision','compute_plan')),
    source_revision  INTEGER,
    payload_json     TEXT NOT NULL CHECK (json_valid(payload_json)),
    summary          TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed','partially_applied','applied','rejected','superseded')),
    proposed_by_type  TEXT NOT NULL CHECK (proposed_by_type IN ('model','human')),
    proposed_by_user_id TEXT,
    model_id         TEXT,
    created_at       TEXT NOT NULL,
    decided_at       TEXT,
    decided_by_user_id TEXT
) STRICT;

CREATE TABLE IF NOT EXISTS videos (
    id               TEXT PRIMARY KEY,
    project_id       TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title            TEXT NOT NULL,
    purpose          TEXT NOT NULL DEFAULT '',
    audience         TEXT NOT NULL DEFAULT '',
    desired_reaction TEXT NOT NULL DEFAULT '',
    format           TEXT NOT NULL DEFAULT 'landscape' CHECK (format IN ('landscape','vertical','square','custom')),
    width            INTEGER NOT NULL DEFAULT 1920,
    height           INTEGER NOT NULL DEFAULT 1080,
    fps_num          INTEGER NOT NULL DEFAULT 30,
    fps_den          INTEGER NOT NULL DEFAULT 1,
    target_duration_ms INTEGER NOT NULL DEFAULT 0,
    status           TEXT NOT NULL DEFAULT 'developing' CHECK (status IN ('developing','planning','producing','reviewing','editing','complete','archived')),
    position         INTEGER NOT NULL DEFAULT 0,
    created_by_user_id TEXT NOT NULL REFERENCES users(id),
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    revision         INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE TABLE IF NOT EXISTS sections (
    id               TEXT PRIMARY KEY,
    video_id         TEXT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    parent_section_id TEXT,
    title            TEXT NOT NULL,
    purpose          TEXT NOT NULL DEFAULT '',
    summary          TEXT NOT NULL DEFAULT '',
    position         INTEGER NOT NULL DEFAULT 0,
    duration_budget_ms INTEGER NOT NULL DEFAULT 0,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    revision         INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE TABLE IF NOT EXISTS scenes (
    id               TEXT PRIMARY KEY,
    section_id       TEXT NOT NULL REFERENCES sections(id) ON DELETE CASCADE,
    title            TEXT NOT NULL,
    synopsis         TEXT NOT NULL DEFAULT '',
    purpose          TEXT NOT NULL DEFAULT '',
    pace             TEXT NOT NULL DEFAULT '',
    audience_reaction TEXT NOT NULL DEFAULT '',
    duration_budget_ms INTEGER NOT NULL DEFAULT 0,
    position         INTEGER NOT NULL DEFAULT 0,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    revision         INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE TABLE IF NOT EXISTS shots (
    id               TEXT PRIMARY KEY,
    scene_id         TEXT NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    title            TEXT NOT NULL,
    purpose          TEXT NOT NULL DEFAULT '',
    treatment        TEXT NOT NULL DEFAULT '',
    duration_target_ms INTEGER NOT NULL DEFAULT 0,
    position         INTEGER NOT NULL DEFAULT 0,
    status           TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','blocked','ready','generating','review','approved','editing','complete','archived')),
    readiness        TEXT NOT NULL DEFAULT 'incomplete' CHECK (readiness IN ('incomplete','blocking_ready','concept_ready','endpoint_ready','video_ready','complete')),
    created_by_user_id TEXT NOT NULL REFERENCES users(id),
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    revision         INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE TABLE IF NOT EXISTS shot_specs (
    id              TEXT PRIMARY KEY,
    shot_id         TEXT NOT NULL REFERENCES shots(id) ON DELETE CASCADE,
    kind            TEXT NOT NULL CHECK (kind IN ('start_frame','end_frame','motion')),
    positive_prompt TEXT NOT NULL DEFAULT '',
    negative_prompt TEXT NOT NULL DEFAULT '',
    structured_spec_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(structured_spec_json)),
    continuity_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(continuity_json)),
    model_preferences_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(model_preferences_json)),
    version         INTEGER NOT NULL,
    created_by_user_id TEXT NOT NULL REFERENCES users(id),
    created_at      TEXT NOT NULL,
    superseded_at    TEXT
) STRICT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_shot_specs_current
    ON shot_specs (shot_id, kind) WHERE superseded_at IS NULL;

CREATE TABLE IF NOT EXISTS shot_blockings (
    id              TEXT PRIMARY KEY,
    shot_spec_id    TEXT NOT NULL REFERENCES shot_specs(id) ON DELETE CASCADE,
    blocking_kind   TEXT NOT NULL CHECK (blocking_kind IN ('start','end')),
    version         INTEGER NOT NULL,
    spec_json       TEXT NOT NULL CHECK (json_valid(spec_json)),
    svg_cache_uri   TEXT,
    source          TEXT NOT NULL DEFAULT 'human' CHECK (source IN ('human','model','mixed')),
    created_by_user_id TEXT,
    model_id        TEXT,
    created_at      TEXT NOT NULL,
    superseded_at   TEXT
) STRICT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_blockings_current
    ON shot_blockings (shot_spec_id, blocking_kind) WHERE superseded_at IS NULL;

CREATE TABLE IF NOT EXISTS shot_links (
    id          TEXT PRIMARY KEY,
    from_shot_id TEXT NOT NULL REFERENCES shots(id) ON DELETE CASCADE,
    to_shot_id  TEXT NOT NULL REFERENCES shots(id) ON DELETE CASCADE,
    relation    TEXT NOT NULL CHECK (relation IN ('continues_into','matches_action','same_moment','parallel_to','depends_on')),
    metadata_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE (from_shot_id, to_shot_id, relation)
) STRICT;

CREATE TABLE IF NOT EXISTS execution_targets (
    id               TEXT PRIMARY KEY,
    workspace_id     TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    kind             TEXT NOT NULL CHECK (kind IN ('local_comfy','remote_agent','hosted_comfy','commercial_api','local_tool')),
    capabilities_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(capabilities_json)),
    pricing_json     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(pricing_json)),
    credential_ref   TEXT,
    enabled          INTEGER NOT NULL DEFAULT 1,
    created_by_user_id TEXT NOT NULL REFERENCES users(id),
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    revision         INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE TABLE IF NOT EXISTS generation_requests (
    id               TEXT PRIMARY KEY,
    project_id       TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    video_id         TEXT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    shot_id          TEXT NOT NULL REFERENCES shots(id) ON DELETE CASCADE,
    shot_spec_id     TEXT NOT NULL REFERENCES shot_specs(id),
    spec_version     INTEGER NOT NULL,
    output_role      TEXT NOT NULL CHECK (output_role IN ('start_frame','end_frame','motion')),
    quality_tier     TEXT NOT NULL DEFAULT 'concept' CHECK (quality_tier IN ('blocking','concept','final','motion','enhance')),
    candidate_count  INTEGER NOT NULL DEFAULT 1 CHECK (candidate_count BETWEEN 1 AND 16),
    status           TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','queued','running','completed','failed','cancelled')),
    request_json     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(request_json)),
    prompt_compilation_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(prompt_compilation_json)),
    created_by_user_id TEXT NOT NULL REFERENCES users(id),
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    revision         INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE TABLE IF NOT EXISTS generation_attempts (
    id                      TEXT PRIMARY KEY,
    generation_request_id   TEXT NOT NULL REFERENCES generation_requests(id) ON DELETE CASCADE,
    attempt_number          INTEGER NOT NULL,
    execution_target_id     TEXT,
    model_id                TEXT,
    resolved_prompts_json   TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(resolved_prompts_json)),
    seed                    INTEGER,
    settings_json           TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(settings_json)),
    status                  TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','succeeded','failed','cancelled')),
    queued_at               TEXT NOT NULL,
    started_at              TEXT,
    completed_at            TEXT,
    error_code              TEXT,
    error_message           TEXT,
    duration_ms             INTEGER,
    UNIQUE (generation_request_id, attempt_number)
) STRICT;

CREATE TABLE IF NOT EXISTS assets (
    id                      TEXT PRIMARY KEY,
    workspace_id            TEXT NOT NULL REFERENCES workspaces(id),
    project_id              TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    generation_attempt_id   TEXT,
    kind                    TEXT NOT NULL CHECK (kind IN ('image','video','audio','document','mask','thumbnail','export')),
    role                    TEXT NOT NULL DEFAULT '',
    storage_uri             TEXT NOT NULL,
    mime_type               TEXT NOT NULL DEFAULT '',
    size_bytes              INTEGER NOT NULL DEFAULT 0,
    checksum_sha256         TEXT NOT NULL DEFAULT '',
    width                   INTEGER,
    height                  INTEGER,
    frame_count             INTEGER,
    duration_ms             INTEGER,
    metadata_json           TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    created_at              TEXT NOT NULL,
    deleted_at              TEXT
) STRICT;

CREATE TABLE IF NOT EXISTS asset_selections (
    id                TEXT PRIMARY KEY,
    shot_id           TEXT NOT NULL REFERENCES shots(id) ON DELETE CASCADE,
    role              TEXT NOT NULL CHECK (role IN ('start_frame','end_frame','video','thumbnail')),
    asset_id          TEXT NOT NULL REFERENCES assets(id),
    shot_revision     INTEGER NOT NULL,
    selected_by_user_id TEXT NOT NULL REFERENCES users(id),
    selected_at       TEXT NOT NULL,
    superseded_at     TEXT
) STRICT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_selections_current
    ON asset_selections (shot_id, role) WHERE superseded_at IS NULL;

CREATE TABLE IF NOT EXISTS approvals (
    id              TEXT PRIMARY KEY,
    asset_id        TEXT NOT NULL REFERENCES assets(id),
    scope           TEXT NOT NULL DEFAULT 'production',
    decision        TEXT NOT NULL CHECK (decision IN ('approve','reject','approve_with_exception')),
    reason          TEXT NOT NULL DEFAULT '',
    asset_checksum  TEXT NOT NULL DEFAULT '',
    shot_revision   INTEGER,
    user_id         TEXT NOT NULL REFERENCES users(id),
    created_at      TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS assessments (
    id              TEXT PRIMARY KEY,
    asset_id        TEXT NOT NULL REFERENCES assets(id),
    assessment_type TEXT NOT NULL DEFAULT 'model_review',
    assessor_type   TEXT NOT NULL CHECK (assessor_type IN ('model','human')),
    assessor_user_id TEXT,
    model_id        TEXT,
    status          TEXT NOT NULL DEFAULT 'review' CHECK (status IN ('pass','review','fail')),
    score           INTEGER,
    criteria_json    TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(criteria_json)),
    summary          TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS annotations (
    id              TEXT PRIMARY KEY,
    workspace_id    TEXT NOT NULL REFERENCES workspaces(id),
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    video_id        TEXT,
    shot_id         TEXT,
    asset_id        TEXT,
    timecode_ms     INTEGER,
    body            TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved')),
    created_by_user_id TEXT NOT NULL REFERENCES users(id),
    assigned_to_user_id TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    revision        INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE TABLE IF NOT EXISTS edits (
    id              TEXT PRIMARY KEY,
    video_id        TEXT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','review','approved','exported')),
    created_by_user_id TEXT NOT NULL REFERENCES users(id),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    revision        INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE TABLE IF NOT EXISTS edit_items (
    id              TEXT PRIMARY KEY,
    edit_id         TEXT NOT NULL REFERENCES edits(id) ON DELETE CASCADE,
    shot_id         TEXT,
    asset_id        TEXT NOT NULL REFERENCES assets(id),
    position        INTEGER NOT NULL DEFAULT 0,
    source_in_ms    INTEGER NOT NULL DEFAULT 0,
    source_out_ms   INTEGER NOT NULL DEFAULT 0,
    duration_ms     INTEGER NOT NULL DEFAULT 0,
    transition_in   TEXT NOT NULL DEFAULT 'cut' CHECK (transition_in IN ('cut','fade','dissolve')),
    transition_out  TEXT NOT NULL DEFAULT 'cut' CHECK (transition_out IN ('cut','fade','dissolve')),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    revision        INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE TABLE IF NOT EXISTS jobs (
    id              TEXT PRIMARY KEY,
    workspace_id    TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    campaign_id     TEXT,
    job_type        TEXT NOT NULL,
    aggregate_type   TEXT NOT NULL DEFAULT '',
    aggregate_id    TEXT NOT NULL DEFAULT '',
    state           TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','claimed','running','succeeded','failed','cancel_requested','cancelled')),
    priority        INTEGER NOT NULL DEFAULT 0,
    request_json    TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(request_json)),
    lease_owner     TEXT,
    lease_expires_at TEXT,
    attempt_count   INTEGER NOT NULL DEFAULT 0,
    result_json     TEXT,
    created_at      TEXT NOT NULL,
    started_at      TEXT,
    completed_at    TEXT,
    error_code      TEXT,
    error_message   TEXT
) STRICT;

CREATE TABLE IF NOT EXISTS job_events (
    id          TEXT PRIMARY KEY,
    job_id      TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    sequence    INTEGER NOT NULL,
    phase       TEXT NOT NULL DEFAULT '',
    level       TEXT NOT NULL DEFAULT 'info' CHECK (level IN ('debug','info','warn','error')),
    message     TEXT NOT NULL,
    progress_basis_points INTEGER,
    data_json   TEXT,
    created_at  TEXT NOT NULL,
    UNIQUE (job_id, sequence)
) STRICT;

CREATE TABLE IF NOT EXISTS render_campaigns (
    id               TEXT PRIMARY KEY,
    workspace_id     TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id       TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    video_id         TEXT,
    name             TEXT NOT NULL,
    source_revision  INTEGER,
    execution_target_id TEXT,
    status           TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','estimated','ready','running','paused','completed','failed','cancelled')),
    estimated_duration_ms INTEGER NOT NULL DEFAULT 0,
    estimated_cost_micros INTEGER NOT NULL DEFAULT 0,
    currency         TEXT NOT NULL DEFAULT 'USD',
    actual_duration_ms INTEGER NOT NULL DEFAULT 0,
    actual_cost_micros INTEGER NOT NULL DEFAULT 0,
    created_by_user_id TEXT NOT NULL REFERENCES users(id),
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    revision         INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE TABLE IF NOT EXISTS campaign_items (
    id               TEXT PRIMARY KEY,
    campaign_id      TEXT NOT NULL REFERENCES render_campaigns(id) ON DELETE CASCADE,
    generation_request_id TEXT NOT NULL REFERENCES generation_requests(id) ON DELETE CASCADE,
    position         INTEGER NOT NULL DEFAULT 0,
    priority         INTEGER NOT NULL DEFAULT 0,
    status           TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','completed','failed','cancelled')),
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS workflow_packages (
    id              TEXT PRIMARY KEY,
    workspace_id    TEXT,
    name            TEXT NOT NULL,
    slug            TEXT NOT NULL,
    task            TEXT NOT NULL DEFAULT 'image',
    description     TEXT NOT NULL DEFAULT '',
    source          TEXT NOT NULL DEFAULT 'bundled' CHECK (source IN ('bundled','imported')),
    active          INTEGER NOT NULL DEFAULT 1,
    created_by_user_id TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    revision        INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE TABLE IF NOT EXISTS workflow_versions (
    id              TEXT PRIMARY KEY,
    workflow_package_id TEXT NOT NULL REFERENCES workflow_packages(id) ON DELETE CASCADE,
    semantic_version TEXT NOT NULL DEFAULT '1.0.0',
    graph_json      TEXT NOT NULL CHECK (json_valid(graph_json)),
    graph_sha256    TEXT NOT NULL,
    manifest_json   TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(manifest_json)),
    status          TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','validated','retired','invalid')),
    created_by_user_id TEXT,
    created_at      TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS audit_events (
    id              TEXT PRIMARY KEY,
    workspace_id    TEXT,
    actor_user_id   TEXT,
    action          TEXT NOT NULL,
    entity_type     TEXT NOT NULL DEFAULT '',
    entity_id       TEXT NOT NULL DEFAULT '',
    summary         TEXT NOT NULL DEFAULT '',
    metadata_json   TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    created_at      TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS domain_events (
    id              TEXT PRIMARY KEY,
    workspace_id    TEXT,
    aggregate_type  TEXT NOT NULL,
    aggregate_id    TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    payload_json    TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(payload_json)),
    created_at      TEXT NOT NULL,
    processed_at    TEXT,
    processing_error TEXT
) STRICT;

-- Foreign-key lookup indexes (spec §9)
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_members_user ON workspace_members (user_id);
CREATE INDEX IF NOT EXISTS idx_projects_ws ON projects (workspace_id);
CREATE INDEX IF NOT EXISTS idx_videos_project ON videos (project_id);
CREATE INDEX IF NOT EXISTS idx_sections_video ON sections (video_id);
CREATE INDEX IF NOT EXISTS idx_scenes_section ON scenes (section_id);
CREATE INDEX IF NOT EXISTS idx_shots_scene ON shots (scene_id);
CREATE INDEX IF NOT EXISTS idx_shot_specs_shot ON shot_specs (shot_id);
CREATE INDEX IF NOT EXISTS idx_requests_shot ON generation_requests (shot_id);
CREATE INDEX IF NOT EXISTS idx_attempts_request ON generation_attempts (generation_request_id);
CREATE INDEX IF NOT EXISTS idx_assets_project ON assets (project_id);
CREATE INDEX IF NOT EXISTS idx_selections_asset ON asset_selections (asset_id);
CREATE INDEX IF NOT EXISTS idx_approvals_asset ON approvals (asset_id);
CREATE INDEX IF NOT EXISTS idx_edit_items_edit ON edit_items (edit_id);
CREATE INDEX IF NOT EXISTS idx_jobs_ws ON jobs (workspace_id);
CREATE INDEX IF NOT EXISTS idx_job_events_job ON job_events (job_id);
CREATE INDEX IF NOT EXISTS idx_discovery_messages_thread ON discovery_messages (thread_id);
CREATE INDEX IF NOT EXISTS idx_proposals_project ON proposals (project_id);
CREATE INDEX IF NOT EXISTS idx_threads_project ON discovery_threads (project_id);

-- ---------------------------------------------------------------------------
-- vid-build 0.01 production additions (idempotent; applied on every boot)
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS workflow_bindings (
    id                  TEXT PRIMARY KEY,
    workflow_version_id TEXT NOT NULL REFERENCES workflow_versions(id) ON DELETE CASCADE,
    semantic_field      TEXT NOT NULL,
    node_id             TEXT NOT NULL,
    input_name          TEXT NOT NULL DEFAULT '',
    value_type          TEXT NOT NULL DEFAULT 'string',
    required            INTEGER NOT NULL DEFAULT 0,
    transform_json      TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(transform_json)),
    default_json        TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(default_json)),
    created_at          TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS models (
    id                TEXT PRIMARY KEY,
    name              TEXT NOT NULL,
    provider          TEXT NOT NULL DEFAULT '',
    family            TEXT NOT NULL DEFAULT '',
    task              TEXT NOT NULL DEFAULT 'image',
    version           TEXT NOT NULL DEFAULT '',
    quantization      TEXT NOT NULL DEFAULT '',
    requirements_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(requirements_json)),
    capabilities_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(capabilities_json)),
    active            INTEGER NOT NULL DEFAULT 1,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS model_profiles (
    id            TEXT PRIMARY KEY,
    workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    task          TEXT NOT NULL DEFAULT 'image',
    model_id      TEXT,
    settings_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(settings_json)),
    lora_stack_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(lora_stack_json)),
    created_by_user_id TEXT NOT NULL REFERENCES users(id),
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    revision      INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE TABLE IF NOT EXISTS target_capability_snapshots (
    id                  TEXT PRIMARY KEY,
    execution_target_id TEXT NOT NULL REFERENCES execution_targets(id) ON DELETE CASCADE,
    observed_at         TEXT NOT NULL,
    service_version     TEXT NOT NULL DEFAULT '',
    node_types_json     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(node_types_json)),
    models_json         TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(models_json)),
    capabilities_json   TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(capabilities_json)),
    hardware_json       TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(hardware_json)),
    status              TEXT NOT NULL DEFAULT 'ok' CHECK (status IN ('ok','error')),
    error_summary       TEXT NOT NULL DEFAULT ''
) STRICT;

CREATE TABLE IF NOT EXISTS execution_observations (
    id                    TEXT PRIMARY KEY,
    generation_attempt_id TEXT NOT NULL REFERENCES generation_attempts(id) ON DELETE CASCADE,
    execution_target_id   TEXT,
    model_id              TEXT,
    workflow_version_id   TEXT,
    hardware_json         TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(hardware_json)),
    settings_fingerprint  TEXT NOT NULL DEFAULT '',
    elapsed_ms            INTEGER NOT NULL DEFAULT 0,
    startup_ms            INTEGER NOT NULL DEFAULT 0,
    peak_vram_mb          INTEGER,
    cost_micros           INTEGER NOT NULL DEFAULT 0,
    currency              TEXT NOT NULL DEFAULT 'USD',
    success               INTEGER NOT NULL DEFAULT 0,
    created_at            TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS asset_derivations (
    id            TEXT PRIMARY KEY,
    from_asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    to_asset_id   TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    relation      TEXT NOT NULL CHECK (relation IN ('reference','input','mask','upscaled_from','edited_from','thumbnail_of')),
    metadata_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json))
) STRICT;

CREATE TABLE IF NOT EXISTS transitions (
    id            TEXT PRIMARY KEY,
    edit_id       TEXT NOT NULL REFERENCES edits(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL CHECK (kind IN ('cut','fade','dissolve')),
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    settings_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(settings_json))
) STRICT;

CREATE INDEX IF NOT EXISTS idx_bindings_version ON workflow_bindings (workflow_version_id);
CREATE INDEX IF NOT EXISTS idx_wf_versions_pkg ON workflow_versions (workflow_package_id);
CREATE INDEX IF NOT EXISTS idx_snapshots_target ON target_capability_snapshots (execution_target_id);
CREATE INDEX IF NOT EXISTS idx_observations_attempt ON execution_observations (generation_attempt_id);
CREATE INDEX IF NOT EXISTS idx_jobs_state ON jobs (state, priority);
CREATE INDEX IF NOT EXISTS idx_assets_attempt ON assets (generation_attempt_id);

CREATE TABLE IF NOT EXISTS exports (
    id                  TEXT PRIMARY KEY,
    edit_id             TEXT NOT NULL REFERENCES edits(id) ON DELETE CASCADE,
    job_id              TEXT,
    asset_id            TEXT,
    profile_json        TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(profile_json)),
    status              TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','queued','running','complete','failed','cancelled')),
    created_by_user_id  TEXT,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL DEFAULT '',
    error_message       TEXT
) STRICT;

CREATE INDEX IF NOT EXISTS idx_exports_edit ON exports (edit_id);

-- LAN pairing and durable app settings (spec §21.3A)
CREATE TABLE IF NOT EXISTS pairing_tokens (
    id                 TEXT PRIMARY KEY,
    code_hash          TEXT NOT NULL,
    user_id            TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    workspace_id       TEXT,
    device_label       TEXT NOT NULL DEFAULT '',
    created_by_user_id TEXT,
    created_at         TEXT NOT NULL,
    expires_at         TEXT NOT NULL,
    used_at            TEXT
) STRICT;

CREATE TABLE IF NOT EXISTS app_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;
