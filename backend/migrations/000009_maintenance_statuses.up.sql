-- Update maintenance request statuses and categories to match the required
-- lifecycle: Submitted, Under Review, Assigned, In Progress, Resolved, Rejected.
-- Categories: plumbing, electrical, security, general_repair, other.
-- Also adds an image_upload_path column for storing local file references
-- (image contents are not stored in the database).

-- Add image_upload_path column for local file storage reference.
ALTER TABLE maintenance_requests
  ADD COLUMN IF NOT EXISTS image_upload_path text NOT NULL DEFAULT '';

-- Backfill existing rows: map old statuses to new equivalents BEFORE adding
-- the new CHECK constraint, so legacy values are converted first.
UPDATE maintenance_requests SET status='submitted' WHERE status='open';
UPDATE maintenance_requests SET status='in_progress' WHERE status='in_progress';
UPDATE maintenance_requests SET status='resolved' WHERE status='resolved';
UPDATE maintenance_requests SET status='rejected' WHERE status='closed';

-- Backfill existing rows: map old categories to new equivalents.
UPDATE maintenance_requests SET category='general_repair' WHERE category='general';
UPDATE maintenance_requests SET category='general_repair' WHERE category='structural';
UPDATE maintenance_requests SET category='general_repair' WHERE category='appliance';

-- Update status CHECK constraint (after backfill).
ALTER TABLE maintenance_requests
  DROP CONSTRAINT IF EXISTS maintenance_requests_status_check;

ALTER TABLE maintenance_requests
  ADD CONSTRAINT maintenance_requests_status_check
  CHECK (status IN ('submitted','under_review','assigned','in_progress','resolved','rejected'));

-- Update category CHECK constraint (after backfill).
ALTER TABLE maintenance_requests
  DROP CONSTRAINT IF EXISTS maintenance_requests_category_check;

ALTER TABLE maintenance_requests
  ADD CONSTRAINT maintenance_requests_category_check
  CHECK (category IN ('plumbing','electrical','security','general_repair','other'));
