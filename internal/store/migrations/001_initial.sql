CREATE TABLE IF NOT EXISTS users (
  id text PRIMARY KEY,
  email text NOT NULL UNIQUE,
  name text NOT NULL,
  is_admin boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  last_login_at timestamptz
);
CREATE TABLE IF NOT EXISTS sessions (
  id text PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS api_keys (
  id text PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name text NOT NULL,
  key_hash text NOT NULL UNIQUE,
  prefix text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  last_used_at timestamptz,
  revoked_at timestamptz
);
CREATE TABLE IF NOT EXISTS calls (
  id text PRIMARY KEY,
  topic text NOT NULL,
  status text NOT NULL CHECK (status IN ('live','stopped','interrupted')),
  started timestamptz NOT NULL,
  ended timestamptz,
  host_user_id text NOT NULL REFERENCES users(id),
  last_seq bigint NOT NULL DEFAULT 0,
  event_path text NOT NULL
);
CREATE TABLE IF NOT EXISTS call_participants (
  call_id text NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
  participant_id text NOT NULL,
  user_id text REFERENCES users(id),
  name text NOT NULL,
  email text,
  PRIMARY KEY (call_id, participant_id)
);
CREATE TABLE IF NOT EXISTS shares (
  id text PRIMARY KEY,
  call_id text NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
  email text NOT NULL,
  grantee_user_id text REFERENCES users(id),
  granted_by text NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (call_id, email)
);
CREATE TABLE IF NOT EXISTS settings (
  key text PRIMARY KEY,
  value jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);
