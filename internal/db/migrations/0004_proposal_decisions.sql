-- 0004_proposal_decisions: retain proposal rejection reasons and regeneration
-- lineage (v0.02 spec §8.6). Forward-only.

ALTER TABLE proposals ADD COLUMN decision_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE proposals ADD COLUMN regenerated_from_id TEXT;
