ALTER TABLE calls DROP CONSTRAINT IF EXISTS calls_status_check;
ALTER TABLE calls ADD CONSTRAINT calls_status_check CHECK (status IN ('scheduled','live','stopped','interrupted','cancelled'));
ALTER TABLE calls ALTER COLUMN started DROP NOT NULL;
ALTER TABLE calls ADD COLUMN IF NOT EXISTS scheduled_at timestamptz;
ALTER TABLE calls ADD COLUMN IF NOT EXISTS zoom_meeting_id text;
ALTER TABLE calls ADD COLUMN IF NOT EXISTS zoom_meeting_uuid text;
ALTER TABLE calls ADD COLUMN IF NOT EXISTS superseded_by_uuid text;
CREATE UNIQUE INDEX IF NOT EXISTS calls_zoom_uuid ON calls(zoom_meeting_uuid) WHERE zoom_meeting_uuid IS NOT NULL;
DROP INDEX IF EXISTS calls_zoom_pending;
CREATE UNIQUE INDEX calls_zoom_pending ON calls(zoom_meeting_id) WHERE zoom_meeting_id IS NOT NULL AND status='scheduled';
CREATE TABLE IF NOT EXISTS rtms_streams (
  stream_id text PRIMARY KEY,
  call_id text REFERENCES calls(id) ON DELETE CASCADE,
  status text NOT NULL DEFAULT 'live' CHECK (status IN ('live','stopped','interrupted'))
);
