-- Each text or lifecycle event is independently durable and replayable.
CREATE TABLE IF NOT EXISTS call_events (
  call_id text NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
  seq bigint NOT NULL CHECK (seq > 0),
  ts bigint NOT NULL,
  recv bigint NOT NULL,
  kind text NOT NULL,
  participant jsonb,
  data jsonb NOT NULL,
  PRIMARY KEY (call_id, seq)
);
-- Retained only to locate legacy JSONL archives during the one-time import.
ALTER TABLE calls ALTER COLUMN event_path SET DEFAULT '';
