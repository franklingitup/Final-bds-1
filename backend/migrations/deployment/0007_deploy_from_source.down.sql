DROP INDEX IF EXISTS deployments_source_repository_idx;
ALTER TABLE deployments
    DROP COLUMN IF EXISTS source_build_id,
    DROP COLUMN IF EXISTS source_repository_id;
