DROP TRIGGER IF EXISTS releases_immutable ON releases;
DROP FUNCTION IF EXISTS prevent_release_modification();
DROP TABLE IF EXISTS releases;
DROP TABLE IF EXISTS deployments;
DROP TABLE IF EXISTS applications;
