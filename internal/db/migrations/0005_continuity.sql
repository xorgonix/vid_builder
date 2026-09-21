-- 0005_continuity: semantic shot endpoint states and continuity proposal type
-- (v0.02 spec §6.4, §6.5). Forward-only and transactional.

CREATE TABLE IF NOT EXISTS shot_states (
    id                 TEXT PRIMARY KEY,
    shot_id            TEXT NOT NULL REFERENCES shots(id) ON DELETE CASCADE,
    endpoint           TEXT NOT NULL CHECK (endpoint IN ('start','end')),
    version            INTEGER NOT NULL,
    state_json         TEXT NOT NULL CHECK (json_valid(state_json)),
    source             TEXT NOT NULL CHECK (source IN ('human','model','derived','mixed')),
    created_by_user_id TEXT REFERENCES users(id),
    created_at         TEXT NOT NULL,
    superseded_at      TEXT,
    UNIQUE (shot_id, endpoint, version)
) STRICT;

CREATE INDEX IF NOT EXISTS idx_shot_states_shot ON shot_states (shot_id, endpoint);

-- Extend the proposals type check with 'continuity'. SQLite cannot alter a CHECK
-- in place, so the table is rebuilt; there are no foreign keys pointing at
-- proposals, and the project index is recreated.
DROP INDEX IF EXISTS idx_proposals_project;
ALTER TABLE proposals RENAME TO proposals_old;

CREATE TABLE proposals (
    id               TEXT PRIMARY KEY,
    project_id       TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    video_id         TEXT,
    proposal_type    TEXT NOT NULL CHECK (proposal_type IN ('project_brief','video_outline','scene_plan','shot_plan','blocking','prompt_revision','compute_plan','continuity')),
    source_revision  INTEGER,
    payload_json     TEXT NOT NULL CHECK (json_valid(payload_json)),
    summary          TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed','partially_applied','applied','rejected','superseded')),
    proposed_by_type  TEXT NOT NULL CHECK (proposed_by_type IN ('model','human')),
    proposed_by_user_id TEXT,
    model_id         TEXT,
    created_at       TEXT NOT NULL,
    decided_at       TEXT,
    decided_by_user_id TEXT,
    decision_reason  TEXT NOT NULL DEFAULT '',
    regenerated_from_id TEXT
) STRICT;

INSERT INTO proposals (id, project_id, video_id, proposal_type, source_revision, payload_json, summary,
                       status, proposed_by_type, proposed_by_user_id, model_id, created_at, decided_at,
                       decided_by_user_id, decision_reason, regenerated_from_id)
SELECT id, project_id, video_id, proposal_type, source_revision, payload_json, summary,
       status, proposed_by_type, proposed_by_user_id, model_id, created_at, decided_at,
       decided_by_user_id, decision_reason, regenerated_from_id
FROM proposals_old;

DROP TABLE proposals_old;

CREATE INDEX IF NOT EXISTS idx_proposals_project ON proposals (project_id);
