-- Public release metadata is shared across Worker instances and deployments.
-- A short lease prevents concurrent cache misses from exhausting GitHub's quota.
CREATE TABLE IF NOT EXISTS release_cache (
  id TEXT PRIMARY KEY,
  body TEXT,
  etag TEXT,
  checked_at INTEGER NOT NULL DEFAULT 0,
  refresh_after INTEGER NOT NULL DEFAULT 0,
  lease_until INTEGER NOT NULL DEFAULT 0,
  lease_token TEXT
);
