# Live Zoom setup

decision: The user reports purchasing Zoom Pro and Developer Pack and authorizes getting the application running for testing.
correction: The current web application explicitly simulates RTMS ingestion. Use the existing standalone rtmsrec spike for the first actual Zoom transcript test; a running demo UI does not prove live Zoom integration.
friction: No local .env was found in the root checkout or the previously named app and RTMS test worktrees. Zoom Marketplace in the accessible in-app browser requires sign-in before developer app credentials can be configured.

decision: The user explicitly approved both Marketplace and API license agreements. Created the user-managed Zoom Connect Demo app; the user completed its installation. The portal showed Remove App and owned/external auto-start with Always ask.
friction: Existing Cloudflare configuration made a new quick tunnel return HTTP 404 despite healthy local service. Restarting with --config /dev/null isolated the quick tunnel and made public /healthz return ok.
decision: Live transcripts from the user's solo meeting on another laptop reached the recorder and the web view, with real speaker name Tyler Gannon and matching spoken sentences. This proves text ingestion, not real Google identity, host metadata matching, chat delivery or other media capture.
decision: The demo bridge uses the existing authenticated local APIs. Source=zoom calls exclude scripted events, including web-app restart, so real speech cannot be mixed with the simulator's lines.
friction: Sharing another worktree's node_modules through a symlink caused svelte-check to miss generated route types. A local vp install --frozen-lockfile fixed resolution; vp run check then reported zero errors and warnings. All Go packages passed go test ./....
decision: The user accepted the text test, requested wrap-up, and then asked for a quick assessment of moving content into Postgres with one row per event. No database-storage rewrite is authorized by that assessment request. Large-media capture remains follow-up work.


decision: Tyler authorized moving normalized event content to Postgres, one row per event, before selecting hosting. Added an explicit current-prototype nlspec amendment; the old draft R2 premise is superseded for text events.
friction: store.Open only executed 001_initial.sql -> apply all idempotent embedded SQL files in filename order so existing databases receive the new table.
decision: Postgres is event history; the hub holds only live channels. Hold the hub mutex across transaction commit/fanout and replay/subscription registration. Failed persistence returns errors; sequence allocation and stop status share the event transaction.
decision: Legacy JSONL imports are atomic per call, preserve content, reject incomplete/conflicting data, and clear the marker after success. Retain files as backups without further normalized writes.
evidence: 53 original real Zoom events compare equal via the upgraded API; a subsequent real Zoom event yielded 54 contiguous Postgres rows. Downloads retained the actual speaker. Tests cover concurrent listeners/writes, database rejection, restart replay/cursor, and importer rollback/repeat after file removal. Go suite and frontend check passed.
decision: Tyler selected Supabase Free over Fly Managed Postgres after reviewing current published pricing; hosted database setup follows this checkpoint. No hosted database created at this checkpoint.
