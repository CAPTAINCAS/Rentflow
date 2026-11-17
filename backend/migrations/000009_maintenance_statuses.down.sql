-- Revert maintenance status and category changes.
-- Maps new statuses/categories back to valid legacy values before restoring
-- old constraints, so the rollback does not fail on rows using new values.

-- Map new statuses back to legacy equivalents.
UPDATE maintenance_requests SET status='open' WHERE status IN ('submitted','under_review','assigned');
UPDATE maintenance_requests SET status='in_progress' WHERE status='in_progress';
UPDATE maintenance_requests SET status='resolved' WHERE status='resolved';
UPDATE maintenance_requests SET status='closed' WHERE status='rejected';

-- Map new categories back to legacy equivalents.
UPDATE maintenance_requests SET category='general' WHERE category IN ('general_repair','security');
-- plumbing, electrical, other remain valid in legacy set.

-- Revert status CHECK constraint.
ALTER TABLE maintenance_requests
  DROP CONSTRAINT IF EXISTS maintenance_requests_status_check;

ALTER TABLE maintenance_requests
  ADD CONSTRAINT maintenance_requests_status_check
  CHECK (status IN ('open','in_progress','resolved','closed'));

-- Revert category CHECK constraint.
ALTER TABLE maintenance_requests
  DROP CONSTRAINT IF EXISTS maintenance_requests_category_check;

ALTER TABLE maintenance_requests
  ADD CONSTRAINT maintenance_requests_category_check
  CHECK (category IN ('plumbing','electrical','structural','appliance','general','other'));

-- Drop image_upload_path column last (data loss on rollback).
ALTER TABLE maintenance_requests
  DROP COLUMN IF EXISTS image_upload_path;
