# Adversarial review: PR #3, scheduled Zoom calls (round 1)

- Date: 2026-10-19
- Branch: `codex/scheduled-zoom-calls`, single commit `8ae0a07` over `origin/main`
- Worktree: `/Users/tyler/.codex/worktrees/f4c2/zoom-connect`
- Reviewer stance: adversarial. The goal was to find the ways this change loses
  meeting data, grants access it should not, or fails the stated requirement.

## 1. Requirement under review

Verbatim: "a user pastes an existing Zoom invitation into zoom-connect and
clients or agents connect to zoom-connect before the meeting, waiting on a
stable stream URL that delivers the call's events when available. Clients do
not connect directly to Zoom. The user authorized shipping if sound and
independent review otherwise."

Repository instructions that bear on it (CLAUDE.md):

- Access: a principal sees a call only if they were a participant or hold a
  share. Check at every route including SSE and downloads (`CLAUDE.md:70`).
- Call IDs are ULIDs assigned by us; URLs never use Zoom's meeting UUID.
- Where `docs/requirements.md` and `nlspec.md` change, fold the change into
  nlspec before building on it.
- Tests are Go tests against the real handler; run `go test ./...` and the
  frontend check before claiming a change works.

## 2. Verdict

**Do not ship as-is. Fix findings F1 to F4, then ship.**

The requirement's shape is met: a user registers a call from a pasted
invitation, gets a ULID URL immediately, and an API-key client can hold
`GET /api/calls/{id}/stream` open from before the meeting until the call ends,
with replay via `Last-Event-ID`. Clients never touch Zoom. The UI, the SSE
contract, the hub transaction discipline and the test suite are solid.

What blocks shipping is that the PR changes how the recorder bridge maps
Zoom sessions to calls, and four of those changes can silently drop
transcript text for *every* meeting, not only scheduled ones. These are
regressions relative to `main`, where each RTMS stream unconditionally became
its own call. Each fix is small and local. Findings F5 and F6 are policy and
documentation gaps that need an explicit decision but are not capture bugs.

## 3. What I verified

| Check | Result |
|---|---|
| `go vet ./...` | clean |
| `TEST_DATABASE_URL=postgres://zoom:zoom@127.0.0.1:5432/zoom_connect_scheduling_review_test?sslmode=disable go test ./...` | all packages pass (`internal/httpapi` 10.6s, `cmd/rtmsrec` pass) |
| `vp run check` (svelte-check) | 0 errors, 0 warnings |
| Proof artifacts `ephemeral/scheduled-proof/` | `client-stream.txt`, `result.json`, `waiting.png`, `live.png` inspected; consistent with the worklog's claims |
| Real Zoom webhook payloads (Oct 16 live test, backup at `/Volumes/Desktop Backups/2026-10-19-164047.previous/.../recordings/webhooks.jsonl`) | 5 `meeting.rtms_started` events, all carrying `meeting_id` (int64), `meeting_uuid`, `rtms_stream_id`, `server_urls`; 6 `rtms_stopped` events carrying `meeting_uuid`, `rtms_stream_id`, `stop_reason` |
| Zoom RTMS event reference (developers.zoom.us/docs/api/rtms/events) | `meeting_id` documented as required int64; the quickstart sample shows it as a string; devforum thread 140363 (Jan 2026) shows a live payload with no `meeting_id` |

I read the full diff against `origin/main`, `README.md`, the worklog
`ephemeral/worklog/202610191010-scheduled-calls.md`, `nlspec.md`,
`docs/requirements.md`, the deployment path (`deploy/build.sh`, `Dockerfile`,
`deploy/run-services.sh`, `fly.toml`) and the surrounding recorder and hub
code. I did not deploy, run the recorder against Zoom, or touch any service
or configuration. The only file created by this review is this one.

Important caveat on the proof: it is entirely simulated. The attach was
driven by a direct `POST /api/rtms/streams` and transcript text by
`POST /api/demo/calls/{id}/events`. No real `meeting.rtms_started` has yet
flowed through the new bridge path into a pre-registered call. The PR is
correctly marked draft pending that.

## 4. Findings, ranked

### F1. Recorder refuses to capture any meeting whose webhook lacks `meeting_id` (high)

`cmd/rtmsrec/main.go:526-530`: on `meeting.rtms_started`, if `meeting_id` is
absent the handler logs "webhook: rtms_started missing meeting_id" and
returns without connecting to Zoom. On `main` the recorder connected
regardless.

Why this matters: `meeting_id` exists solely to correlate with a
pre-registered call. It is not needed to capture the stream. All five real
payloads I found do carry it, and Zoom's reference marks it required, but
Zoom's own quickstart shows a different type, and a January 2026 devforum
report shows a payload without it. A single schema wobble on Zoom's side now
means zero capture for the whole company, not merely a failed scheduled
match.

Fix: treat `meeting_id` as optional. Always connect. If present, carry it in
`zoomIdentity`; if absent, let the bridge fall back to creating a new call
by UUID (the existing demo-call path). Add a test for the absent case.

### F2. A new stream for a meeting whose call has already ended is discarded (high)

`internal/httpapi/scheduling.go:173-243` (`attachRTMSStream`): the lookup
finds the call by `(meeting_uuid, meeting_id)`, then at line 208 checks
`status != "scheduled" && status != "live"`. If the call is `stopped` or
`interrupted` it returns the call id with no `rtms_streams` insert and no
event. The bridge (`cmd/rtmsrec/bridge.go:89-92`) sees `stream_status` not
`live`, marks the stream `closed`, and every subsequent text frame for that
stream is dropped.

When this happens in practice: the host stops and restarts RTMS from the
Zoom client mid-meeting; an `interrupted` session is replaced by a fresh
stream with a new `rtms_stream_id`; or the meeting runs past a Zoom-side
reconnect. All of these keep the same `meeting_uuid`. On `main` each stream
became a new call, so the text survived. nlspec open question 3 ("re-sent
`rtms_started`: second session or refuse") is still open and this PR picks
the worst answer, "refuse", without recording the decision.

Fix: when the matched call is terminal, either reopen it (set status back to
`live`, clear `ended`, emit `stream.started`) or create a new call for the
new stream. Reopening is closer to the "one call per meeting" model the PR
is reaching for. Add a test: start, stop, start again with a new stream id,
assert text is persisted.

### F3. A stale `live` call permanently blocks its meeting number (high)

Migration `003_scheduled_calls.sql:8` adds the partial unique index
`calls_zoom_pending (zoom_meeting_id) WHERE status IN ('scheduled','live')`.
If an `rtms_stopped` webhook is missed (recorder restart, Fly deploy during a
meeting, Zoom not sending one after a crash), the call stays `live` forever.
Consequences:

- `scheduleCall` (`scheduling.go:99`) returns 409 for that meeting number
  forever. Personal Meeting IDs are stable, so one missed stop blocks a
  person's recurring room.
- The next real session (new UUID, same `meeting_id`) reaches the insert at
  `scheduling.go:203`, hits the unique violation, and returns 409 "could not
  attach stream to this meeting" (`scheduling.go:222`). The bridge treats
  non-2xx as a transient error with no fallback, retries on every text frame,
  and drops all of them.
- `cancelScheduledCall` (`scheduling.go:143`) only acts on `scheduled`.
  There is no admin override. The only escape is the demo endpoint
  `POST /api/demo/calls/{id}/stop`, which is not documented for this purpose.

Fix: on attach, if a `live` call exists for the same `meeting_id` but a
different `meeting_uuid`, mark the old one `interrupted` (it demonstrably is)
inside the same transaction, then proceed. Add a reaper or an admin cancel
for calls that have been `live` with no stream activity for some bound.
Add a test for the stale-live case.

### F4. Bridge has no fallback when attach fails (medium-high)

`cmd/rtmsrec/bridge.go:89-92`: a non-2xx from `/api/rtms/streams` returns an
error without marking the stream closed and without falling back to
`/api/demo/calls`. The bridge then re-posts on every text event. This is the
amplifier for F2 and F3: any 4xx or 5xx from the app during a meeting means
an unbounded retry loop, log spam, and total loss of that stream's text.
On `main` the bridge created a call and moved on.

Fix: on a 409 or any 4xx, fall back to creating a standalone call for the
stream (the pre-PR behaviour) and log once. Keep retrying only on 5xx or
network errors, with a bound.

### F5. Access model: anyone with a sign-in can claim any meeting (needs a decision)

`scheduling.go:99` lets any signed-in user register any 9 to 11 digit meeting
number. `internal/store/queries.sql:53` and the `ListCalls` change grant
read access to `host_user_id`, a third class of access beyond "participant
or share" in `CLAUDE.md:70` and `docs/requirements.md:42` (T3). The worklog
records this as "creator access is explicit ownership", but neither spec is
updated.

Adversarial reading: Personal Meeting IDs are stable and appear in
signatures, calendars and old emails. A user who registers a colleague's
PMI receives the full transcript of every future meeting in that room
without attending, can share it onward (`canShareCall` = owner or
participant), and blocks the real host from registering it (409). RTMS
participant events are not mapped to users, so there is no check that the
registrant was in the room. Within a small single-domain company this may
be acceptable, but it is a deliberate departure from the written access
rule and should be stated as such, with the mitigations chosen (for
example: owner access ends if the owner never appears as a participant;
or only the Zoom host's email, once known, may own the call).

### F6. nlspec and requirements not amended (process)

The PR adds statuses `scheduled` and `cancelled`, events `call.scheduled`
and `call.cancelled`, table `rtms_streams`, the tombstone semantics for
stop-before-start, the "call ends when the last stream stops" rule, and the
owner access class. `nlspec.md` and `docs/requirements.md` are unchanged.
CLAUDE.md requires folding such changes into nlspec before building on them.
nlspec open question 3 should be closed with whatever F2 decides.

### F7. Minor defects

- `stopRTMSStream` (`scheduling.go:244`) emits `stream.<status>` even when
  the call was never live, so a tombstone stop followed by a real stop can
  append a duplicate `stream.stopped`.
- `announcements` in `share_admin.go` maps any open to closed transition to
  `call.ended`, so a cancellation is announced on the global feed as
  `call.ended`. Clients watching the firehose cannot distinguish.
- `scheduled_at` is accepted unvalidated (any RFC3339, including the past).
  Harmless now but the UI's "Upcoming" section will show stale entries.
- `invitationMeetingID` rejects `https://app.zoom.us/wc/{id}/join`,
  `/s/{id}` and `/my/{name}` links, all of which appear in real invitations.
  The error message is clear, so this is a usability gap, not a bug.
- Migration 003 ends with a redundant
  `ALTER TABLE rtms_streams ALTER COLUMN call_id DROP NOT NULL` (the column
  is already nullable).
- Unregistered live calls created via `attachRTMSStream` line 203 no longer
  add the key owner as a participant, unlike `createDemoCall`. The admin can
  still see them via ownership, so net effect is nil, but the two paths now
  disagree.
- `stream()` in `server.go` reads `c.Status` once before `replaySubscribe`;
  a status change in that window delays the `end` frame by up to the 3s
  tick. Pre-existing, not introduced here.

## 5. What is sound

- `hub.changeCall` commits the status change and its event in one
  transaction under the hub mutex before fan-out, so a subscriber can never
  see an event the database does not have. Good.
- `Last-Event-ID` taking precedence over `?from=` matches the SSE spec and
  the README documents it.
- `callOpen` keeps the SSE connection open across `scheduled` to `live`,
  which is the heart of the requirement, and the restart-durability test
  proves a client reconnecting after an app restart replays correctly.
- Admin-only gating on `/api/rtms/streams*` is enforced and tested (403 for
  a non-admin key).
- Duplicate attach is idempotent (tested). Two concurrent streams on one
  call end the call only after both stop (tested).
- Deployment path is coherent: `deploy/build.sh` builds both binaries,
  the Dockerfile ships both, `run-services.sh` runs them together on one Fly
  machine with the recorder on loopback. App and recorder always deploy
  as a pair, so the new bridge protocol cannot skew against an old app.
- The Svelte changes are UI only. No application I/O in TypeScript.

## 6. Test gaps to close with the fixes

1. `rtms_started` without `meeting_id` still connects and captures (F1).
2. Terminal call, then new stream for the same UUID: text persists (F2).
3. Stale `live` call, then new UUID for the same `meeting_id`: attach
   succeeds and the old call is closed (F3).
4. Bridge receives 409 from attach: falls back and persists text (F4).
5. `app.zoom.us/wc/{id}/join` invitation parses (F7).
6. One real end-to-end run: register a real meeting, start RTMS from Zoom,
   confirm the pre-connected client receives `stream.started` and transcript
   text. This is the proof the draft is waiting on and nothing simulated
   substitutes for it.

## 7. Recommended order of work

1. F1 and F4 together in `cmd/rtmsrec` (make attach best-effort, always
   capture).
2. F2 and F3 together in `attachRTMSStream` (reopen terminal calls; close a
   stale live call on a new UUID).
3. Decide F5, write it down in `nlspec.md` and `docs/requirements.md` along
   with the F6 amendments.
4. Run the real Zoom proof, attach its evidence under `ephemeral/`, and
   un-draft the PR.
