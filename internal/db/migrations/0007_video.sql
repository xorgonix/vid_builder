-- 0007_video: duration-based video segments and their final assembly.
-- A duration request compiles into fixed five-second anchor-to-anchor segments;
-- each segment is generated independently and the ordered segment assets are
-- assembled into one playable video. Forward-only.

CREATE TABLE IF NOT EXISTS video_segments (
    id             TEXT PRIMARY KEY,
    request_id     TEXT NOT NULL REFERENCES generation_requests(id) ON DELETE CASCADE,
    workspace_id   TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id     TEXT NOT NULL,
    video_id       TEXT NOT NULL,
    shot_id        TEXT NOT NULL,
    idx            INTEGER NOT NULL,
    positive       TEXT NOT NULL DEFAULT '',
    negative       TEXT NOT NULL DEFAULT '',
    start_asset_id TEXT,
    end_asset_id   TEXT,
    frame_count    INTEGER NOT NULL DEFAULT 0,
    fps            REAL NOT NULL DEFAULT 0,
    status         TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','succeeded','failed')),
    attempt_id     TEXT,
    asset_id       TEXT,
    error          TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    revision       INTEGER NOT NULL DEFAULT 1,
    UNIQUE (request_id, idx)
) STRICT;

CREATE INDEX IF NOT EXISTS idx_video_segments_request ON video_segments (request_id);

CREATE TABLE IF NOT EXISTS video_assemblies (
    id                      TEXT PRIMARY KEY,
    request_id              TEXT NOT NULL REFERENCES generation_requests(id) ON DELETE CASCADE,
    workspace_id            TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id              TEXT NOT NULL,
    video_id                TEXT NOT NULL,
    shot_id                 TEXT NOT NULL,
    status                  TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','succeeded','failed')),
    asset_id                TEXT,
    segment_asset_ids_json  TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(segment_asset_ids_json)),
    error                   TEXT NOT NULL DEFAULT '',
    created_at              TEXT NOT NULL,
    updated_at              TEXT NOT NULL,
    revision                INTEGER NOT NULL DEFAULT 1,
    UNIQUE (request_id)
) STRICT;
