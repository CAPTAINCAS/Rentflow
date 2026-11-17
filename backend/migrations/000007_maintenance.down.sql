-- Revert maintenance requests and comments.
--
-- WARNING: This drops all maintenance requests and comments. Export any
-- needed data before rolling back.

DROP TABLE IF EXISTS maintenance_comments;
DROP TRIGGER IF EXISTS maintenance_requests_updated_at ON maintenance_requests;
DROP TABLE IF EXISTS maintenance_requests;
DROP FUNCTION IF EXISTS maintenance_requests_update_updated_at();
DROP SEQUENCE IF EXISTS maintenance_ref_seq;
