-- 0003_beats: introduce the dramatic beat layer (v0.02 spec §6.1, §5.1).
-- Forward-only and transactional. Existing scenes receive one implicit beat that
-- adopts their existing shots, so simple projects are unchanged.

CREATE TABLE IF NOT EXISTS beats (
    id                       TEXT PRIMARY KEY,
    scene_id                 TEXT NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    title                    TEXT NOT NULL,
    purpose                  TEXT NOT NULL DEFAULT '',
    trigger                  TEXT NOT NULL DEFAULT '',
    action                   TEXT NOT NULL DEFAULT '',
    reaction                 TEXT NOT NULL DEFAULT '',
    resulting_state          TEXT NOT NULL DEFAULT '',
    intended_audience_effect TEXT NOT NULL DEFAULT '',
    duration_budget_ms       INTEGER NOT NULL DEFAULT 0,
    position                 INTEGER NOT NULL DEFAULT 0,
    implicit                 INTEGER NOT NULL DEFAULT 0 CHECK (implicit IN (0,1)),
    created_at               TEXT NOT NULL,
    updated_at               TEXT NOT NULL,
    revision                 INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE INDEX IF NOT EXISTS idx_beats_scene ON beats (scene_id);

ALTER TABLE shots ADD COLUMN beat_id TEXT REFERENCES beats(id);

INSERT INTO beats (id, scene_id, title, purpose, action, resulting_state,
                   duration_budget_ms, position, implicit, created_at, updated_at, revision)
SELECT 'beat_' || s.id, s.id, s.title, s.purpose, s.synopsis, '',
       s.duration_budget_ms, 0, 1, s.created_at, s.updated_at, 1
FROM scenes s
WHERE NOT EXISTS (SELECT 1 FROM beats b WHERE b.scene_id = s.id);

UPDATE shots SET beat_id = 'beat_' || scene_id WHERE beat_id IS NULL;

CREATE TRIGGER IF NOT EXISTS shots_beat_required_insert
BEFORE INSERT ON shots
WHEN NEW.beat_id IS NULL
BEGIN
    SELECT RAISE(ABORT, 'shots.beat_id is required');
END;

CREATE TRIGGER IF NOT EXISTS shots_beat_required_update
BEFORE UPDATE ON shots
WHEN NEW.beat_id IS NULL
BEGIN
    SELECT RAISE(ABORT, 'shots.beat_id is required');
END;
