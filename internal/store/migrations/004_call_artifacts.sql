-- Live HTML artifacts belong to exactly one call. The artifact row points at the
-- current version; each create or update writes an immutable snapshot row.
CREATE TABLE IF NOT EXISTS call_artifacts (
  id text PRIMARY KEY,
  call_id text NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
  version bigint NOT NULL CHECK (version > 0),
  created_by text NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS call_artifacts_by_call ON call_artifacts(call_id, created_at);
-- Size limits are enforced by the API and repeated here as a storage bound.
CREATE TABLE IF NOT EXISTS call_artifact_versions (
  artifact_id text NOT NULL REFERENCES call_artifacts(id) ON DELETE CASCADE,
  version bigint NOT NULL CHECK (version > 0),
  title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 120),
  html text NOT NULL CHECK (octet_length(html) <= 262144),
  author_id text NOT NULL REFERENCES users(id),
  key_name text,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (artifact_id, version)
);
