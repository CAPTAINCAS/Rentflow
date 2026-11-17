-- Revert password resets and billing notifications.
--
-- WARNING: This drops all password reset tokens and billing notification
-- records. Export any needed data before rolling back.

DROP TABLE IF EXISTS billing_notifications;
DROP TABLE IF EXISTS password_resets;
