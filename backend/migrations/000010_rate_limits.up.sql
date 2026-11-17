CREATE TABLE IF NOT EXISTS rate_limits (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  key text NOT NULL,
  window_start timestamptz NOT NULL,
  count int NOT NULL DEFAULT 0,
  expires_at timestamptz NOT NULL,
  UNIQUE (key, window_start)
);
CREATE INDEX IF NOT EXISTS rate_limits_key_idx ON rate_limits(key);
CREATE INDEX IF NOT EXISTS rate_limits_expires_idx ON rate_limits(expires_at);