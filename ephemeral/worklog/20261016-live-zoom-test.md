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


decision: Tyler selected existing Supabase Free organization Diffusion-TPG and created zoom-connect-demo himself. Data API disabled; application access checks chosen instead of automatic RLS. Supabase is healthy in North Virginia.
friction: Supabase CLI login belongs to Guilde organizations, not the Safari GitHub account. Do not create resources in the wrong org. Browser new-password entry was handed to the user.
friction: Supabase session pooler uses the Supabase CA rather than the system trust bundle. Downloaded the official CA from the database settings and verified certificate plus hostname before using the database.
correction: SUPABASE_DATABASE_URL initially held the generated database password. Preserved it as SUPABASE_DATABASE_PASSWORD, built the actual session-pooler URL, and verified DATABASE_URL resolves to it through Doppler.
decision: All deployment secrets belong in existing Doppler zoom-connect/prd. Zoom credentials copied and verified, database CA configured. Future Fly service token must be read-only and scoped to that config.
decision: Tyler requested a real password login restricted to tyler@tylergannon.com for the demo; the prior Google button is simulated. Password mode fails closed on missing config, disables persona bypass, creates only the configured user, and uses secure cookies for public HTTPS. Password stored in Doppler, never in source or logs.
evidence: Real application running locally against Supabase authenticated the configured account and session, listed zero calls, and created a recorder API key owned by that account. Tests covered wrong email/password, disabled demo routes, cookie flags and logout. Frontend check and full application build passed.
correction: Tyler explicitly cancelled moving historical call data to Supabase. Hosted database starts fresh; original local call remains untouched.
requested: Scheduled calls should appear from Zoom scheduling webhooks and offer a stable authorized event stream before start (about 15 minutes early), with keepalives followed by real events on the same connection. Preserve cursor replay on reconnect. Record cancellation/reschedule and distinguish recurring occurrences. Not implemented in this checkpoint.
requested: Tyler asked to start media capture and scheduling as additional tasks alongside deployment. Separate isolated work is appropriate; do not let these change the current live Zoom settings, Doppler config or Fly deployment during packaging.


decision: Deployed zoom-connect-tpg-demo to Fly iad with one shared CPU/512 MB machine, Supabase and scoped read-only Doppler bootstrap token. App and recorder are supervised in one container; public signed webhook proxies to loopback recorder. Normalized content persists in Postgres; raw protocol diagnostics remain ephemeral.
evidence: Fly HTTPS password session verified both over HTTP and actual signed-in browser UI. Anonymous calls, wrong passwords and simulated persona routes rejected. Health checks Postgres plus recorder. Full Go suite and Linux build passed; signed webhook challenge preserved through reverse proxy.
correction: Forked conversations were not the fresh sessions Tyler requested. Created fresh media and scheduling sessions with individual briefs after main was pushed to d357870. User accepted moving them into the Zoom Connect project manually because MCP and native app tools exposed different project registries.
decision: Created separate Zoom Connect Production registration LIt3ti3IQ-KbHXMIhz4x3A, preserving original local registration and .env. New credentials placed only in Doppler prd. Restarted Fly and verified endpoint challenge using the new webhook secret. Browser installation Allow grants sensitive access, so requested action-time approval; awaiting response. Do not claim live Zoom ingestion on Fly yet.
friction: Zoom developer UI cached RTMS capability controls after scope addition; reloading refreshed enabled controls. Accessibility label 'Deleted scope' describes a trash control, not a removed scope warning; screenshot showed saved scopes normally.
