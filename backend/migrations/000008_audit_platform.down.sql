-- Revert audit logs and platform settings.
--
-- WARNING: This drops all audit log entries and platform settings. Export
-- any needed data before rolling back.

DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS platform_settings;
