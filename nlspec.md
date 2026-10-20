# Zoom Connect — nlspec

Status: draft v0.2, 2026-10-15. Sole authority for intent; the code is the
authority for fact. Amend only through spec-defect escalation or by elevating
demonstrated test cases into claims.

## Scheduled client subscriptions amendment — October 19, 2026

Tyler requested that an existing Zoom invitation can be added to the app so
clients connect to zoom-connect before the call begins. Clients use one stable
ULID URL and an authenticated SSE connection, never a direct Zoom connection.
This amendment governs scheduled subscriptions and supersedes conflicting
lifecycle and access assumptions in the older draft and product requirements.

Registration is restricted to trusted admins, matching the existing recorder
and capture-management trust boundary. Registration atomically creates a
`scheduled` call, an explicit share for the registering account, and the
`call.scheduled` event. It does not fabricate attendance. Visibility remains
participant-or-share at every endpoint; creator/host columns alone never grant
read access. The registrar can share this one call and cancel it while pending.
Other users and their API keys can connect before start through a share.
A share applies to one call ULID, never automatically to later sessions using
the same Zoom meeting number. An invitation is not verified host identity.

The invitation and passcode are discarded after extracting a meeting number.
Each meeting number permits one pending registration; an existing live call
does not prevent registering the next session. The optional expected time is
advisory. SSE stays open while `scheduled` or `live`, including keepalives;
`Last-Event-ID` takes precedence over the initial `from` query cursor.
Cancellation atomically stores `call.cancelled` and status `cancelled`, publishes
the cancellation on both feeds, and closes waiting subscriptions.

The public call ULID, reusable Zoom meeting number, per-session Zoom runtime
UUID, and per-stream RTMS ID remain distinct. Persist runtime and stream
mappings. Missing meeting numbers cannot be matched to a pending registration,
but still capture a standalone call by UUID. A new stream for the same UUID
reopens its stopped/interrupted call, preserving its URL and original start;
a client that already received `end` can reconnect to that URL. An old stream
ID never reopens a call. Stop-before-start records a durable closed-stream
tombstone and never claims a later pending registration.

A new runtime UUID on a meeting number supersedes the previous instance.
Its stale live call becomes interrupted with a durable event; its old stream
rows close. Persist supersession so delayed streams from that older UUID
cannot close or claim the newer session. A call ends when its last active
stream stops. Lifecycle and identity changes, every affected event, and
sequence allocation commit together before publication. All routing assumes
one application instance, as before. Quiet calls are not timed out merely
because no transcript text arrives.

Scheduled attachment is best effort for upstream capture: on an HTTP client
error the recorder captures a standalone call using the existing ingestion
path. Server/network attachment failures receive at most three attempts before
that fallback. Do not report standalone fallback as delivery to a pending call.
Fallback still uses the existing authenticated admin ingestion endpoint.

Behavioral claims: a real handler/database integration demonstrates early
waiting, transition, first transcript delivery on the same connection,
cancellation, replay, authorization revocation, duplicate and out-of-order
notifications, absent meeting numbers, resumed streams, and successive runtime
UUIDs. A running application demonstrates registration and waiting UI and API
clients. A real Zoom meeting is required to claim opening-transcript capture
from Zoom; simulated input is labelled and never substituted for that claim.
This change does not implement unattended joining, recurring scheduling,
participant identity matching, or recovery of recorder sockets across deploys.

## Current prototype amendment — October 16, 2026

Tyler explicitly replaced the draft object-storage requirement for normalized
meeting content: Postgres is the durable record of transcript, chat, and
lifecycle events. Store one row per event, keyed by call ID and sequence;
JSON payloads belong to individual events, never one whole-call object.
Sequence allocation, insertion, and any stop/interruption status change commit
atomically. Publish to live subscribers only after commit. Replay resumes
strictly after the requested sequence, with no gap at the replay/live handoff.
This deployment remains one application instance.

Existing local JSONL archives must import without changing event content or
sequence, atomically per call and idempotently across startup. An incomplete
or conflicting archive must fail visibly rather than silently discard data.
Once imported, replay and downloads must work without the original files.
Raw recorder diagnostics remain separate from the application's durable event
store. Audio/video storage and hosting are later decisions.

For the temporary public demo, Tyler authorized a password login restricted
to tyler@tylergannon.com, with credentials in Doppler zoom-connect/prd. This
mode disables simulated sign-in and seeded fictional data. Real Google
Workspace authentication remains future work. Supabase Free is the hosted
event store; Fly deployment is being prepared separately. Historical local
call data is deliberately not copied, per Tyler.

This amendment supersedes the event-related R2/archive requirements below for
the current prototype. The remaining draft describes future integrations;
Google auth and fully integrated production RTMS are not completed by this
storage change.

## 1. Overview and goals

Zoom Connect is a headless service that receives the live content of Zoom
meetings hosted inside one Zoom account, keeps it, and serves it to people
and agents in the same organization, both while the meeting is happening and
afterward.

It is built on Zoom Realtime Media Streams (RTMS). Zoom pushes transcript,
chat, participant events and (optionally) audio to the service; nothing joins
the meeting as a participant. Zoom does the transcription.

Needs served, in priority order:

1. An agent can subscribe to a meeting's transcript as it happens.
2. Anyone in the organization can read the transcript of any past meeting.
3. The raw audio is retained, per participant, for later use.

Design principles:

- **Zoom is one source among future sources.** The event format and the
  consumer-facing API carry no Zoom-specific concepts. Zoom's protocol is
  confined to two cells.
- **Object storage is the record of meetings.** Every event chunk and audio
  file lives in one R2 bucket under a stable layout; the bucket alone is
  enough to rebuild everything about a meeting.
- **Postgres is the record of people.** Accounts, API keys and anything
  else that is not meeting content live in Postgres, reached through
  sqlc-generated code over pgx/v5. The database never holds meeting content;
  a meeting index, if one is ever added, is rebuildable from the bucket.
- **Auth and pages come from skgo.** Google sign-in, the browser session
  and the server-rendered pages are an skgo app; this service adds the
  domain check, API keys and the meeting routes. (skgo's next release is
  pending; see deferred decision 8.)
- **Least capture.** Only meetings hosted by the organization's own users;
  only the media kinds configured; raw audio off unless switched on.
- **Private app.** The Zoom app registration is Private distribution in the
  organization's own account. Nothing here depends on Marketplace review.
- **One language, one binary.** The service is a single Go program. The
  budgets in §6 assume that.

## 2. Operating environment (assumed, not built)

- A paid Zoom account (Pro or higher) with the Developer Pack active, a
  user-managed General App with Private distribution, the RTMS scopes
  `meeting:read:meeting_transcript`, `meeting:read:meeting_chat` and
  (if audio is on) `meeting:read:meeting_audio`, event subscriptions for
  RTMS started/stopped/interrupted, the account setting "Share realtime
  meeting content with apps" on, and auto-start configured for meetings the
  organization's users host.
- A Google Workspace domain whose users may sign in.
- A Cloudflare R2 bucket reachable over the S3 API.
- A host with a public HTTPS endpoint and connections that stay open for
  the length of a meeting. One instance.
- All outbound TLS at 1.2 or above (Zoom requires this from 2026-11-15).

## 3. Definition of done

Each claim is **cell-local** (provable through one cell's contract, mocks
allowed) or **integration** (demonstrated live against Zoom, Google and R2).
Every seam in §6 is exercised by at least one claim.

| # | Claim | Kind | Seams |
|---|---|---|---|
| C1 | A meeting hosted by an org user, with auto-start on, appears in `GET /live` within 30 s of RTMS starting, and `GET /meetings/{id}/stream` delivers transcript events attributed to the right speaker while people talk. | integration | zoomhook→session, rtms→session, session→api |
| C2 | After that meeting ends, `meta.json` and every event chunk exist in the bucket, and `GET /meetings/{id}/transcript?format=txt` returns what was said, in order, with speaker names. | integration | session→archive, archive→api |
| C3 | The webhook answers Zoom's URL-validation challenge correctly; rejects a request whose signature does not verify (401); rejects a request whose timestamp is more than 5 minutes off (401); and turns each of the three RTMS events into the corresponding `StreamSignal`. | cell-local (zoomhook) | zoomhook→session |
| C4 | Fed a recorded signaling and media exchange, the RTMS client completes both handshakes, answers keep-alives, decodes transcript, chat and participant messages into `Event`s, and on an unexpected media-socket close reconnects and resumes without a duplicate or missing message. | cell-local (rtms) | rtms→session |
| C5 | A subscriber that connects mid-meeting receives every earlier event of that meeting, then live events, with `seq` strictly increasing and no gaps. Two concurrent subscribers see identical sequences. | cell-local (session) | session→api |
| C6 | Events written through the archive are chunked per the flush rule in §5.4; reading a meeting back yields the identical event sequence; the final chunk and `meta.json` land on stop; a meeting id containing `/`, `+` and `=` round-trips through the path encoding. | cell-local (archive) | session→archive |
| C7 | Google sign-in with a verified email in `ALLOWED_DOMAIN` and a matching `hd` claim yields a session and a `users` row; any other Google identity gets 403 and no session; a signed-in user can create and revoke an API key and sees the secret exactly once; a current key is accepted as a bearer token; a revoked or unknown one gets 401; an unauthenticated request to any `/meetings` or `/live` route gets 401. | cell-local (auth) | auth→store, auth→api |
| C12 | Migrations apply cleanly to an empty Postgres and again to one already migrated (no-op); every sqlc query compiles against the migrated schema. | cell-local (store) | store |
| C8 | With `CAPTURE_AUDIO=true`, after a meeting ends `GET /meetings/{id}/audio/{participant}` returns an Ogg/Opus file that plays in a stock player and contains that participant's speech only. | integration | rtms→session, session→archive, archive→api |
| C9 | `?format=vtt` output validates as WebVTT, with one cue per transcript event. | cell-local (api) | archive→api |
| C10 | A server restart during a live meeting does not corrupt the archive: chunks written before the restart read back intact and `meta.json` marks the meeting `interrupted`. | integration | session→archive |
| C11 | A signed-in user opens the web UI, sees the live meeting in the list, opens it, and watches transcript lines appear without reloading. | integration | web→api, auth→api |

## 4. Exclusions

Deliberately not built in this version:

- Transcription of any kind. Zoom's transcript is the only text source.
- A participant bot, voice output, or any channel back into the meeting.
- Search, summaries, an MCP server, calendar linking.
- A meeting index in the database. `GET /meetings` lists from the bucket.
- Video and screen-share capture.
- Meetings hosted outside the organization's account, E2EE meetings,
  private chat (Zoom does not deliver it).
- Starting or stopping RTMS from this service (Zoom auto-start only).
- Per-meeting or per-person access rules: every signed-in user and every
  API key sees every meeting.
- Multiple instances, zero-downtime deploys, resuming a stream after a
  server restart (the rest of that meeting is lost; C10 bounds the damage).
- Any other meeting platform.
- Retention or deletion tooling. Deleting a meeting is deleting its prefix
  in the bucket by hand.

## 5. Central data types and algorithms

### 5.1 Event (public format; shown exactly)

One JSON object per line in event chunks and on the live stream.

```json
{"seq":412,"ts":1760550123456,"recv":1760550123601,"kind":"transcript",
 "participant":{"id":"16778240","name":"Dana Kim"},
 "data":{"text":"let's ship it monday","lang":"en","final":true}}
```

```
RECORD Event:
    seq         : Integer        -- 1-based, strictly increasing per meeting, assigned by session
    ts          : Integer        -- milliseconds since epoch, Zoom's timestamp for the item
    recv        : Integer        -- milliseconds since epoch, when the service received it
    kind        : EventKind
    participant : Participant | None   -- absent for stream.* and for chat from the system
    data        : Map            -- kind-specific, below

RECORD Participant:
    id          : String         -- Zoom's participant id for this meeting, as a string
    name        : String         -- display name at the time of the event

ENUM EventKind:
    stream.started      -- data: {}
    stream.stopped      -- data: {reason: String}
    stream.interrupted  -- data: {reason: String}
    participant.joined  -- data: {}
    participant.left    -- data: {}
    speaker.active      -- data: {}            -- the participant became the active speaker
    transcript          -- data: {text, lang, final: Boolean}
    chat                -- data: {text}        -- public ("Everyone") chat only
```

Audio is not an event; it goes to audio files (§5.3).

### 5.2 MeetingMeta (public format; shown exactly)

```json
{"id":"abc+xyz/123==","path_id":"YWJjK3h5ei8xMjM9PQ","stream_id":"s-9f1",
 "topic":"Weekly sync","host":{"id":"u-1","name":"Dana Kim","email":"dana@example.com"},
 "started":1760550000000,"ended":1760553600000,"status":"stopped",
 "participants":[{"id":"16778240","name":"Dana Kim"}],
 "chunks":3,"audio":["16778240"],"last_seq":1891}
```

`status` is one of `live`, `stopped`, `interrupted`. `meta.json` is written
at start (status `live`, no `ended`) and rewritten at stop and on each
chunk flush, so a crash leaves it at most one flush interval stale.

### 5.3 Bucket layout

```
meetings/<yyyy>/<mm>/<dd>/<path_id>/
    meta.json
    events/000001.jsonl         -- chunks numbered from 1, zero-padded to 6
    events/000002.jsonl
    audio/<participant id>/000001.ogg   -- only when CAPTURE_AUDIO; one file per flush
```

- `<yyyy>/<mm>/<dd>` is the UTC date of `started`.
- `path_id` = base64url(meeting UUID) without padding. Zoom meeting UUIDs
  may contain `/`, `+` and `=`; they never appear in object keys.
- A meeting's `id` in URLs is also `path_id`. `meta.json` holds the raw UUID.

### 5.4 Flush rule

Applies independently to the event log and to each participant's audio.

```
FUNCTION should_flush(buffer, now):
    RETURN buffer.bytes >= 256 KiB
        OR now - buffer.opened_at >= 15 s
        OR stream has stopped or been interrupted
```

A flush writes the buffer as the next numbered chunk, then rewrites
`meta.json`. An empty buffer is never written. Chunk numbers are never
reused; after a server restart the service does not continue an existing
meeting (see Exclusions).

### 5.5 Session state

One session per live meeting, keyed by `(meeting UUID, stream id)`.

```
ENUM SessionState:
    CONNECTING     -- signal received, RTMS client not yet streaming
    STREAMING
    RECONNECTING   -- media or signaling socket lost; client retrying
    STOPPED        -- terminal

CONNECTING   -> STREAMING     -- RTMS client ready
CONNECTING   -> STOPPED       -- cannot connect within 60 s; status interrupted
STREAMING    -> RECONNECTING  -- socket closed unexpectedly
RECONNECTING -> STREAMING     -- resumed
RECONNECTING -> STOPPED       -- not resumed within 2 min; status interrupted
STREAMING    -> STOPPED       -- rtms_stopped signal; status stopped
any          -> STOPPED       -- rtms_interrupted signal; status interrupted
```

Zoom buffers up to 60 s of audio while the client connects; the connect
timeout is chosen to stay inside that.

A session holds the complete event list of its meeting in memory for the
meeting's lifetime; subscribers replay from it (C5). Transcript and chat for
a day-long meeting are a few megabytes, so no eviction. Hard cap 500 000
events per meeting; reaching it is a tripwire (log, stop accepting), not a
design case.

Muted participants send no audio. Audio timestamps are preserved as given;
silence is not synthesized. A consumer wanting a continuous track derives it
from the timestamps.

### 5.6 Subscribe semantics

`GET /meetings/{id}/stream?from=<seq>` (default 0) is Server-Sent Events.
The server sends every buffered event with `seq > from`, then live events,
then a final `event: end` when the session reaches STOPPED. For a meeting
that is not live, the server serves the same sequence from the archive and
ends immediately. Each SSE message's `id` is the event `seq`, so a browser's
automatic `Last-Event-ID` reconnect resumes correctly.

## 6. Seams, cells, contracts, budgets

Budgets count non-test Go lines in the cell's package (plus templates and
static files where noted), excluding generated code. Breach is a tripwire:
stop and report; the spec decides the split.

Dependency direction: `cmd → api, web, auth, store, session, archive, zoomhook, rtms`;
`session → rtms, archive`; `api → session, archive, auth`; `auth → store`;
`web → api, auth`. Nothing else reaches across a cell. `store` is the only
cell that imports pgx or sqlc output.

### zoomhook (budget 250)

Responsibility: Zoom's inbound webhook, and nothing else.

```
INTERFACE ZoomHook:
    FUNCTION handler() -> HTTPHandler           -- mounted at POST /zoom/webhook
    FUNCTION signals() -> Stream<StreamSignal>

RECORD StreamSignal:
    kind         : STARTED | STOPPED | INTERRUPTED
    meeting_uuid : String
    stream_id    : String
    server_url   : String | None       -- signaling server; STARTED only
    topic        : String | None
    host         : Host | None
    reason       : String | None
```

Contract: answers Zoom's URL-validation challenge using the webhook secret;
verifies `x-zm-signature` (HMAC-SHA256 over `v0:<timestamp>:<body>` with
the webhook secret) and rejects timestamps more than 5 minutes from now;
emits exactly one signal per accepted RTMS event; returns 2xx promptly and
never blocks on downstream work. Unknown event types are accepted and
ignored.

### rtms (budget 1200)

Responsibility: Zoom's RTMS wire protocol. The only cell that knows its
message types.

```
INTERFACE RTMSClient:
    FUNCTION connect(signal: StreamSignal, want: Set<MediaKind>) -> Stream<RTMSItem>
    -- the stream ends when Zoom ends it or the client gives up; errors are items

ENUM MediaKind: TRANSCRIPT, CHAT, AUDIO

RTMSItem is one of:
    Event (as §5.1, without seq)
    AudioPacket { participant: Participant, ts: Integer, opus: Bytes }
    ClientState { STREAMING | RECONNECTING | ENDED, reason }
```

Contract: signs the handshake as HMAC-SHA256 of
`client_id,meeting_uuid,stream_id` with the client secret; opens the
signaling socket, obtains the media socket URL, opens it, requests exactly
the kinds in `want` (audio requested as Opus, participant-separated, at
Zoom's default interval); answers keep-alives on both sockets; on an
unexpected close, reconnects with backoff for up to 2 minutes using Zoom's
documented failover sequence, and never re-emits an item already emitted;
emits `ENDED` once and then nothing. Participant join/leave and active
speaker arrive on the signaling socket and are emitted as events like any
other. Requires TLS 1.2+ on both sockets.

### session (budget 450)

Responsibility: one live meeting: numbering, buffering, fan-out, archiving.

```
INTERFACE Sessions:
    FUNCTION handle(signal: StreamSignal)         -- creates, updates or stops a session
    FUNCTION live() -> List<MeetingMeta>
    FUNCTION get(path_id) -> Session | None

INTERFACE Session:
    meta        : MeetingMeta
    FUNCTION subscribe(from_seq) -> Stream<Event>  -- per §5.6
    FUNCTION events(from_seq) -> List<Event>
```

Contract: assigns `seq` in receive order; writes every event to the archive
per §5.4 before or concurrently with fan-out (a slow subscriber never delays
archiving; a slow subscriber is dropped after 30 s of back-pressure); routes
`AudioPacket`s to the archive's audio writer; maintains `meta.json`'s
participant list and status; implements the state machine in §5.5.

### archive (budget 600)

Responsibility: the bucket layout (§5.3) and the flush rule (§5.4), reading
and writing. The only cell that knows the layout.

```
INTERFACE Archive:
    FUNCTION open(meta: MeetingMeta) -> MeetingWriter
    FUNCTION list(date) -> List<MeetingMeta>
    FUNCTION meta(path_id) -> MeetingMeta | None
    FUNCTION events(path_id, from_seq) -> Stream<Event>
    FUNCTION audio(path_id, participant) -> Stream<Bytes> | None   -- concatenated Ogg

INTERFACE MeetingWriter:
    FUNCTION event(e: Event)
    FUNCTION audio(p: AudioPacket)
    FUNCTION meta(m: MeetingMeta)
    FUNCTION close(final: MeetingMeta)   -- flushes everything, rewrites meta.json
```

Contract: writes are idempotent per chunk number; a chunk is complete or
absent, never partial; `events` returns the concatenation of chunks in
number order, filtered by `from_seq`; audio for a participant is written as
Ogg pages carrying the Opus packets, each chunk a self-contained Ogg stream,
and `audio()` concatenates them (chained Ogg plays in stock players);
`list` resolves to a date prefix listing plus one `meta.json` read per
meeting. Implements the path encoding of §5.3.

### store (budget 300 hand-written: schema, migrations, `.sql` queries, wrapper)

Responsibility: the Postgres schema and every query against it. Schema is
owned by SQL migrations in the cell; queries are `.sql` files compiled by
sqlc into Go over `pgx/v5` (v5.11.0). Generated code is excluded from the
budget and is never edited.

```
TABLE users:      id, email (unique), name, created_at, last_login_at
TABLE api_keys:   id, user_id → users, name, key_hash (unique), prefix,
                  created_at, last_used_at, revoked_at (nullable)

INTERFACE Store:
    FUNCTION migrate()                                   -- idempotent
    FUNCTION upsert_user(email, name) -> User
    FUNCTION create_key(user_id, name, key_hash, prefix) -> ApiKey
    FUNCTION find_key(key_hash) -> ApiKey | NONE         -- current keys only
    FUNCTION touch_key(id)                               -- last_used_at
    FUNCTION revoke_key(id, user_id)
    FUNCTION list_keys(user_id) -> List<ApiKey>
```

Contract: `migrate` runs at start-up and is safe to run on every start.
Only hashes of key secrets are stored; `prefix` (first 8 characters) is
the only part shown after creation. `find_key` excludes revoked keys.
No meeting data, ever.

### auth (budget 450)

Responsibility: who is calling. Google sign-in, the session cookie and
the login/logout routes are supplied by skgo; this cell configures them
and adds what skgo does not know about.

```
INTERFACE Auth:
    FUNCTION routes() -> HTTPHandler           -- skgo login routes + /keys (list, create, revoke)
    FUNCTION require(next: HTTPHandler) -> HTTPHandler

RECORD Principal:
    kind    : USER | KEY
    subject : String      -- email, or the key's name
    user_id : Id
```

Contract: Google OpenID Connect through skgo; a sign-in is accepted only
if the ID token's email is verified and `hd` equals `ALLOWED_DOMAIN` (the
`hd` login hint alone is not trusted), and on acceptance `store.upsert_user`
runs. `Authorization: Bearer <key>` is hashed and looked up with
`store.find_key`; the secret is random, ≥32 bytes, shown once at creation.
`require` attaches a `Principal` or returns 401. The subject appears in
access logs.

### api (budget 700)

Responsibility: the consumer-facing HTTP surface. All routes below sit
behind `auth.require`.

```
GET /live                                 -> List<MeetingMeta>              (session)
GET /meetings?date=YYYY-MM-DD             -> List<MeetingMeta>              (archive; default today, UTC)
GET /meetings/{id}                        -> MeetingMeta                    (session if live, else archive)
GET /meetings/{id}/events?from=<seq>      -> application/x-ndjson of Event
GET /meetings/{id}/stream?from=<seq>      -> text/event-stream per §5.6
GET /meetings/{id}/transcript?format=txt|vtt|json
GET /meetings/{id}/audio/{participant}    -> audio/ogg, 404 if none
```

Contract: `txt` is one line per transcript event, `HH:MM:SS Name: text`,
offsets from `started`; `vtt` is valid WebVTT, one cue per transcript event,
cue end = next cue start or +5 s; `json` is the raw transcript events.
Unknown meeting → 404. Live and archived meetings are indistinguishable to
the caller except for `status`.

### web (budget 500, including templates and static files)

Responsibility: the minimum browser UI, as skgo pages: sign-in, today's
and live meetings, a transcript view that follows a live meeting via
`/stream`, and an API-keys page. Everything it shows comes from `api` and
`auth`. Build step, if any, is whatever skgo prescribes.

### cmd (budget 150)

Responsibility: configuration from environment, wiring, listening,
graceful shutdown (on SIGTERM: flush all sessions as `interrupted`, then
exit within 10 s).

Environment (all required unless noted):

```
LISTEN_ADDR            default :8080
PUBLIC_URL             external https base, used for OAuth redirect
ZOOM_CLIENT_ID, ZOOM_CLIENT_SECRET, ZOOM_WEBHOOK_SECRET
GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET
ALLOWED_DOMAIN         e.g. example.com
DATABASE_URL           postgres://… ; migrations run at start-up
SESSION_SECRET         ≥32 bytes, cookie signing (or skgo's equivalent)
R2_ENDPOINT, R2_BUCKET, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY
CAPTURE_AUDIO          true|false, default false
```

## 7. Deferred decisions

Tripwires. An implementer who hits one stops and escalates; the answer
lands here.

1. **RTMS message shapes.** Field names, the exact handshake JSON, the
   keep-alive form and the failover sequence come from Zoom's current docs
   and `zoom/rtms-samples`; the recorded exchange used for C4 is captured
   from a live meeting during the first spike. If the live protocol
   contradicts this spec's assumptions (e.g. Opus unavailable for
   participant-separated audio, no `final` flag on transcripts), escalate.
2. **Partial transcripts.** Whether Zoom sends in-progress lines. If it
   does, `final:false` events are stored and streamed as-is; whether `txt`
   output should drop them is undecided.
3. **Re-sent `rtms_started`.** Resolved for the current prototype by the
   October 19 amendment: fresh streams resume the same runtime UUID, while
   duplicate/closed stream IDs remain idempotent. Superseded UUIDs stay closed.
4. **Billing while idle.** Whether Zoom charges for an auto-started stream
   the service never connects to. Affects whether auto-start should stay the
   default.
5. **Multiple hosts' meetings colliding on `path_id`.** Zoom UUIDs are
   unique per meeting instance; if a recurring meeting reuses one, the
   layout needs the `stream_id`. Verify on the first recurring meeting.
6. **Audio from Zoom's clock vs. receive time** for file naming and
   alignment with transcript events; decide once C8 is attempted.
7. **Host identity → Google identity** mapping for later access rules; not
   needed now, but `meta.json` already carries the host email so it is not
   lost. The `users` table makes the mapping possible later.
8. **What skgo's next release supplies.** The `auth` and `web` cells are
   written against skgo's new version, not yet released. Which of
   Google OIDC, session cookie, CSRF, page layout and the build step skgo
   owns, and the shape of its handler/middleware API, is settled when the
   app is initialized; the budgets for `auth` and `web` are revisited then.
9. **Migration runner.** Whether migrations are applied by the binary at
   start-up (as written) or by a separate command, and which tool (goose,
   tern, hand-rolled); decide at the first `store` slice.
