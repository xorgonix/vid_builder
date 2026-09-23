-- 0008_super_shot: the Super Shot correction (spec §5, §15).
--
-- A Super Shot is one perceived shot of 2.0–30.0 seconds. It is generated
-- directly (one invocation) when the selected workflow can represent the whole
-- duration, or as a chain of adjacent segments. A chain of N segments persists
-- N+1 ordered anchors; adjacent segments share their boundary anchor and, once
-- rendered, its exact asset. This migration adds the request/anchor/context
-- records and the segment relations the chain needs. Forward-only; 0007 is a
-- historical reference and is not rewritten.

CREATE TABLE IF NOT EXISTS super_shot_requests (
    id                      TEXT PRIMARY KEY,
    generation_request_id   TEXT REFERENCES generation_requests(id),
    workspace_id            TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id              TEXT NOT NULL,
    video_id                TEXT NOT NULL,
    shot_id                 TEXT NOT NULL,
    workflow_version_id     TEXT NOT NULL,
    model_profile_id        TEXT,
    strategy                TEXT NOT NULL CHECK (strategy IN ('direct','chained')),
    requested_duration_ms   INTEGER NOT NULL,
    planned_duration_ms     INTEGER NOT NULL,
    fps_num                 INTEGER NOT NULL,
    fps_den                 INTEGER NOT NULL,
    width                   INTEGER NOT NULL,
    height                  INTEGER NOT NULL,
    base_positive           TEXT NOT NULL DEFAULT '',
    base_negative           TEXT NOT NULL DEFAULT '',
    context_mode            TEXT NOT NULL DEFAULT 'anchor_only'
                                CHECK (context_mode IN ('anchor_only','tail_frames','native_latent')),
    status                  TEXT NOT NULL DEFAULT 'draft'
                                CHECK (status IN ('draft','awaiting_approval','approved','generating_anchors',
                                                  'generating_segments','assembling','succeeded','failed','cancelled')),
    plan_version            INTEGER NOT NULL DEFAULT 1,
    legacy_invalid_plan     INTEGER NOT NULL DEFAULT 0,
    plan_summary            TEXT NOT NULL DEFAULT '',
    continuity_json         TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(continuity_json)),
    created_by_user_id      TEXT NOT NULL,
    created_at              TEXT NOT NULL,
    updated_at              TEXT NOT NULL,
    revision                INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE INDEX IF NOT EXISTS idx_super_shot_requests_video ON super_shot_requests (video_id);
CREATE INDEX IF NOT EXISTS idx_super_shot_requests_shot ON super_shot_requests (shot_id);
CREATE INDEX IF NOT EXISTS idx_super_shot_requests_genreq ON super_shot_requests (generation_request_id);

CREATE TABLE IF NOT EXISTS video_anchors (
    id                      TEXT PRIMARY KEY,
    super_shot_request_id   TEXT NOT NULL REFERENCES super_shot_requests(id) ON DELETE CASCADE,
    idx                     INTEGER NOT NULL,
    time_offset_ms          INTEGER NOT NULL,
    kind                    TEXT NOT NULL CHECK (kind IN ('supplied_start','intermediate','supplied_end')),
    state_prompt            TEXT NOT NULL DEFAULT '',
    negative_prompt         TEXT NOT NULL DEFAULT '',
    continuity_json         TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(continuity_json)),
    status                  TEXT NOT NULL DEFAULT 'planned'
                                CHECK (status IN ('planned','approved','queued','running','succeeded','failed')),
    asset_id                TEXT,
    source_anchor_id        TEXT,
    error                   TEXT NOT NULL DEFAULT '',
    created_at              TEXT NOT NULL,
    updated_at              TEXT NOT NULL,
    revision                INTEGER NOT NULL DEFAULT 1,
    UNIQUE (super_shot_request_id, idx),
    UNIQUE (super_shot_request_id, time_offset_ms)
) STRICT;

CREATE INDEX IF NOT EXISTS idx_video_anchors_request ON video_anchors (super_shot_request_id);

-- Motion context artifacts (spec §15.5). Rows describe a validated context a
-- later segment may consume. anchor_only requires none; tail_frames and
-- native_latent rows are only written by a workflow adapter that actually
-- exports/consumes the artifact. Bytes live in the managed asset store.
CREATE TABLE IF NOT EXISTS motion_contexts (
    id                      TEXT PRIMARY KEY,
    workspace_id            TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id              TEXT NOT NULL,
    super_shot_request_id   TEXT NOT NULL REFERENCES super_shot_requests(id) ON DELETE CASCADE,
    producer_segment_id     TEXT NOT NULL,
    producer_attempt_id     TEXT,
    source_asset_id         TEXT NOT NULL,
    source_checksum         TEXT NOT NULL DEFAULT '',
    mode                    TEXT NOT NULL CHECK (mode IN ('tail_frames','native_latent')),
    artifact_asset_id       TEXT,
    artifact_checksum       TEXT NOT NULL DEFAULT '',
    encoding_version        TEXT NOT NULL DEFAULT '',
    model_fingerprint       TEXT NOT NULL DEFAULT '',
    vae_fingerprint         TEXT NOT NULL DEFAULT '',
    adapter_fingerprint     TEXT NOT NULL DEFAULT '',
    workflow_version_id     TEXT NOT NULL DEFAULT '',
    workflow_sha            TEXT NOT NULL DEFAULT '',
    context_contract_version TEXT NOT NULL DEFAULT '1',
    width                   INTEGER NOT NULL DEFAULT 0,
    height                  INTEGER NOT NULL DEFAULT 0,
    fps_num                 INTEGER NOT NULL DEFAULT 0,
    fps_den                 INTEGER NOT NULL DEFAULT 1,
    source_start_frame      INTEGER NOT NULL DEFAULT 0,
    source_end_frame        INTEGER NOT NULL DEFAULT 0,
    layout_json             TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(layout_json)),
    validation_status       TEXT NOT NULL DEFAULT 'unvalidated',
    created_at              TEXT NOT NULL
) STRICT;

CREATE INDEX IF NOT EXISTS idx_motion_contexts_producer ON motion_contexts (producer_segment_id);

-- Segment relations the chain needs. Existing rows (0007) keep NULL request and
-- anchor ids and are treated as legacy. Added columns cannot carry CHECK/FK in
-- SQLite ALTER, so the invariants are enforced in the store/service.
ALTER TABLE video_segments ADD COLUMN super_shot_request_id TEXT;
ALTER TABLE video_segments ADD COLUMN start_anchor_id TEXT;
ALTER TABLE video_segments ADD COLUMN end_anchor_id TEXT;
ALTER TABLE video_segments ADD COLUMN start_offset_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE video_segments ADD COLUMN end_offset_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE video_segments ADD COLUMN duration_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE video_segments ADD COLUMN action_prompt TEXT NOT NULL DEFAULT '';
ALTER TABLE video_segments ADD COLUMN continuity_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE video_segments ADD COLUMN fps_num INTEGER NOT NULL DEFAULT 0;
ALTER TABLE video_segments ADD COLUMN fps_den INTEGER NOT NULL DEFAULT 1;
ALTER TABLE video_segments ADD COLUMN context_mode TEXT NOT NULL DEFAULT 'anchor_only';
ALTER TABLE video_segments ADD COLUMN context_input_id TEXT;
ALTER TABLE video_segments ADD COLUMN depends_on_segment_id TEXT;
ALTER TABLE video_segments ADD COLUMN stale INTEGER NOT NULL DEFAULT 0;

-- Assembly receipts: planned vs actual duration, joins and the FFmpeg command.
ALTER TABLE video_assemblies ADD COLUMN super_shot_request_id TEXT;
ALTER TABLE video_assemblies ADD COLUMN planned_duration_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE video_assemblies ADD COLUMN actual_duration_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE video_assemblies ADD COLUMN joins_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE video_assemblies ADD COLUMN ffmpeg_receipt TEXT NOT NULL DEFAULT '';
