-- ── Upgrade commits to support multiple parents (merge commits) ──

ALTER TABLE commits ADD COLUMN parent_ids UUID[] NOT NULL DEFAULT '{}';

-- Backfill: copy existing parent_id into parent_ids array.
UPDATE commits SET parent_ids = ARRAY[parent_id] WHERE parent_id IS NOT NULL;
UPDATE commits SET parent_ids = '{}' WHERE parent_id IS NULL;

-- Keep parent_id for now (backward compat), but parent_ids is the source of truth.

-- ── Full file tree snapshot per commit ──────────────────────────
-- Every commit records its complete file state, not just changed files.
-- This is essential for merge base comparison and checkout.

CREATE TABLE commit_files (
    commit_id       UUID NOT NULL REFERENCES commits(id) ON DELETE CASCADE,
    file_id         UUID NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    file_version_id UUID NOT NULL REFERENCES file_versions(id) ON DELETE CASCADE,
    PRIMARY KEY (commit_id, file_id)
);

CREATE INDEX idx_commit_files_commit ON commit_files(commit_id);
CREATE INDEX idx_commit_files_file ON commit_files(file_id);

-- ── File locks ──────────────────────────────────────────────────

CREATE TABLE locks (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    file_path       TEXT NOT NULL,
    branch          VARCHAR(256) NOT NULL DEFAULT 'main',
    locked_by       UUID NOT NULL REFERENCES users(id),
    locked_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(project_id, file_path)
);

CREATE INDEX idx_locks_project ON locks(project_id);
CREATE INDEX idx_locks_user ON locks(locked_by);
