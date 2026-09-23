-- 0010_integration_repairs: immutable execution snapshots, attempt ownership,
-- anchor image mode, and publication guard columns (spec R1–R6).

-- Snapshots: the worker must execute the frozen inputs set at queue time,
-- not mutable live fields (R2). hash is computed at enqueue and checked at
-- publication; input_revision changes every time a segment's creative inputs
-- are edited.
ALTER TABLE video_segments ADD COLUMN queued_input_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE video_segments ADD COLUMN input_revision INTEGER NOT NULL DEFAULT 0;
ALTER TABLE video_anchors ADD COLUMN queued_input_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE video_assemblies ADD COLUMN queued_input_hash TEXT NOT NULL DEFAULT '';

-- Explicit attempt-to-segment/anchor links so R3 can prove membership without
-- inferring from attempt-number arithmetic (which changes across retries).
ALTER TABLE generation_attempts ADD COLUMN segment_id TEXT;
ALTER TABLE generation_attempts ADD COLUMN anchor_id TEXT;
CREATE INDEX IF NOT EXISTS idx_gen_attempts_segment ON generation_attempts(segment_id) WHERE segment_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_gen_attempts_anchor ON generation_attempts(anchor_id) WHERE anchor_id IS NOT NULL;

-- Whether the plan uses reference conditioning or text-only (R1).
ALTER TABLE super_shot_requests ADD COLUMN image_mode TEXT NOT NULL DEFAULT 'references'
    CHECK (image_mode IN ('references','text_only'));

-- Backfill existing attempts so R3 can validate them. Segment attempts link
-- via video_segments.attempt_id / selected_attempt_id; anchor attempts link
-- via video_anchors.image_attempt_id.
UPDATE generation_attempts
SET segment_id = (
    SELECT vs.id FROM video_segments vs
    WHERE vs.attempt_id = generation_attempts.id
       OR vs.selected_attempt_id = generation_attempts.id
    LIMIT 1
)
WHERE segment_id IS NULL
  AND id IN (SELECT attempt_id FROM video_segments WHERE attempt_id IS NOT NULL);

UPDATE generation_attempts
SET anchor_id = (
    SELECT va.id FROM video_anchors va
    WHERE va.image_attempt_id = generation_attempts.id
    LIMIT 1
)
WHERE anchor_id IS NULL
  AND id IN (SELECT image_attempt_id FROM video_anchors WHERE image_attempt_id IS NOT NULL);