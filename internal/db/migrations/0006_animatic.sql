-- 0006_animatic: timed cues, animatic revisions and narrative metrics
-- (v0.02 spec §6.2, §6.3, §6.6). Forward-only and transactional.

CREATE TABLE IF NOT EXISTS shot_cues (
    id                 TEXT PRIMARY KEY,
    shot_id            TEXT NOT NULL REFERENCES shots(id) ON DELETE CASCADE,
    cue_type           TEXT NOT NULL CHECK (cue_type IN ('dialogue','voiceover','sound','music','caption','action')),
    character_id       TEXT REFERENCES characters(id),
    start_ms           INTEGER NOT NULL DEFAULT 0,
    duration_ms        INTEGER NOT NULL DEFAULT 0,
    content            TEXT NOT NULL DEFAULT '',
    delivery           TEXT NOT NULL DEFAULT '',
    subtext            TEXT NOT NULL DEFAULT '',
    settings_json      TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(settings_json)),
    position           INTEGER NOT NULL DEFAULT 0,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    revision           INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE INDEX IF NOT EXISTS idx_shot_cues_shot ON shot_cues (shot_id);

CREATE TABLE IF NOT EXISTS animatics (
    id                 TEXT PRIMARY KEY,
    video_id           TEXT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    name               TEXT NOT NULL,
    source_revision    INTEGER NOT NULL,
    settings_json      TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(settings_json)),
    status             TEXT NOT NULL DEFAULT 'draft'
                         CHECK (status IN ('draft','review','approved','stale')),
    created_by_user_id TEXT NOT NULL REFERENCES users(id),
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    revision           INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE INDEX IF NOT EXISTS idx_animatics_video ON animatics (video_id);

CREATE TABLE IF NOT EXISTS narrative_metrics (
    id                  TEXT PRIMARY KEY,
    video_id            TEXT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    entity_type         TEXT NOT NULL CHECK (entity_type IN ('scene','beat','shot')),
    entity_id           TEXT NOT NULL,
    dramatic_intensity  INTEGER CHECK (dramatic_intensity BETWEEN 0 AND 100),
    visual_intensity    INTEGER CHECK (visual_intensity BETWEEN 0 AND 100),
    tension             INTEGER CHECK (tension BETWEEN 0 AND 100),
    information_density INTEGER CHECK (information_density BETWEEN 0 AND 100),
    emotional_valence   INTEGER CHECK (emotional_valence BETWEEN -100 AND 100),
    notes               TEXT NOT NULL DEFAULT '',
    version             INTEGER NOT NULL,
    created_by_user_id  TEXT NOT NULL REFERENCES users(id),
    created_at          TEXT NOT NULL,
    superseded_at       TEXT
) STRICT;

CREATE INDEX IF NOT EXISTS idx_narrative_metrics_entity ON narrative_metrics (entity_type, entity_id);
