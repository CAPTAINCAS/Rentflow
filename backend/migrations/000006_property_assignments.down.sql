-- Revert property assignments.
--
-- WARNING: This drops all agent-to-property assignments. Export any needed
-- data before rolling back.

DROP TABLE IF EXISTS property_assignments;
