-- 0009_super_shot_completion: fields the completion contract requires (§6.2).
-- Forward-only. Adds separate purpose/movement, raw vs compiled prompts, planner
-- provenance, an explicit intermediate-image workflow/profile, selected-output
-- attempt, job→attempt links, and measured-vs-planned assembly state.

-- Parent plan: separate the shot purpose from the complete-shot movement, record
-- planner provenance, and name the image workflow/model used for intermediates.
ALTER TABLE super_shot_requests ADD COLUMN shot_purpose TEXT NOT NULL DEFAULT '';
ALTER TABLE super_shot_requests ADD COLUMN complete_movement TEXT NOT NULL DEFAULT '';
ALTER TABLE super_shot_requests ADD COLUMN planner_status TEXT NOT NULL DEFAULT 'incomplete';
ALTER TABLE super_shot_requests ADD COLUMN planner_warnings_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE super_shot_requests ADD COLUMN planner_provenance_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE super_shot_requests ADD COLUMN image_workflow_version_id TEXT;
ALTER TABLE super_shot_requests ADD COLUMN image_model_profile_id TEXT;
ALTER TABLE super_shot_requests ADD COLUMN superseded_by TEXT;

-- Segments: keep the editable interval action separate from the compiled prompt,
-- and record which attempt's output is the current selection.
ALTER TABLE video_segments ADD COLUMN raw_action TEXT NOT NULL DEFAULT '';
ALTER TABLE video_segments ADD COLUMN compiled_prompt TEXT NOT NULL DEFAULT '';
ALTER TABLE video_segments ADD COLUMN selected_attempt_id TEXT;

-- Anchors: image-generation attempt and image workflow receipt (C03).
ALTER TABLE video_anchors ADD COLUMN image_attempt_id TEXT;

-- Jobs: explicit link to the immutable attempt they execute (C04/C05).
ALTER TABLE jobs ADD COLUMN attempt_id TEXT;

-- Assembly: record the exact input revision/hash and whether duration was
-- actually measured (C08).
ALTER TABLE video_assemblies ADD COLUMN input_revision INTEGER NOT NULL DEFAULT 0;
ALTER TABLE video_assemblies ADD COLUMN input_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE video_assemblies ADD COLUMN measured_state TEXT NOT NULL DEFAULT 'unmeasured';

-- Backfill: existing rows keep working; movement defaults to the historic
-- base_positive and the compiled prompt mirrors the historic action prompt.
UPDATE super_shot_requests SET complete_movement = base_positive WHERE complete_movement = '';
UPDATE video_segments SET raw_action = action_prompt WHERE raw_action = '';
UPDATE video_segments SET compiled_prompt = action_prompt WHERE compiled_prompt = '';
UPDATE video_segments SET selected_attempt_id = attempt_id WHERE selected_attempt_id IS NULL;

-- Join review notes: a creator's accept/reject/notes for each boundary of an
-- assembled Super Shot (contract C10 / §15.9). Never an automated quality claim.
CREATE TABLE IF NOT EXISTS assembly_join_reviews (
    id          TEXT PRIMARY KEY,
    assembly_id TEXT NOT NULL REFERENCES video_assemblies(id) ON DELETE CASCADE,
    idx         INTEGER NOT NULL,
    status      TEXT NOT NULL DEFAULT 'unreviewed' CHECK (status IN ('unreviewed','accepted','rejected')),
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE (assembly_id, idx)
) STRICT;
