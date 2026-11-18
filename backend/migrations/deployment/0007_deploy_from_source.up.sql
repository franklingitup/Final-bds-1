-- Deploy from source: add nullable source tracking columns to deployments.
--
-- source_repository_id: when set, this deployment was created from a Git
--   repository rather than a pre-built image. The build consumer matches
--   build.succeeded / build.failed events by imageRepository and transitions
--   the deployment out of awaiting_build.
-- source_build_id: ID of the most recent build associated with this source
--   deployment. Updated by the build consumer on each event.
--
-- Both columns are nullable and additive — existing image-based deployments
-- are unaffected (NULL values). No data migration required.
--
-- NOTE: No DB-level foreign key on source_repository_id.
-- The repositories table is owned by the build service, which runs its own
-- migrations independently. The deployment service has no visibility into
-- build service tables, so cross-service references are handled by
-- application-level ID only — matching the platform's existing convention
-- (e.g., deployments.cluster_id only carries a FK because the deployment
-- server also runs cluster migrations; a build-service table does not share
-- that same database schema scope).

ALTER TABLE deployments
    ADD COLUMN IF NOT EXISTS source_repository_id UUID,
    ADD COLUMN IF NOT EXISTS source_build_id       TEXT;

CREATE INDEX IF NOT EXISTS deployments_source_repository_idx
    ON deployments (org_id, source_repository_id)
    WHERE source_repository_id IS NOT NULL;
