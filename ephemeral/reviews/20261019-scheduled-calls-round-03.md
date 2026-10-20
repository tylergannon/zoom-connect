# Adversarial review: PR #3, scheduled Zoom calls (round 3)

- Date: 2026-10-19
- Branch: `codex/scheduled-zoom-calls` at `f37bf47` (3 commits over `origin/main`: `8ae0a07`, `f520cfe`, `f37bf47`)
- Worktree: `/Users/tyler/.codex/worktrees/f4c2/zoom-connect`
- Prior rounds: round 1 (`20261019-scheduled-calls-round-01.md`) completed. Round 2
  (Gimbal `01M5BBDVV8ATY7EDQRMA8FP8VT`) terminated on a usage limit and produced no
  artifact, so this is the first complete review of `f520cfe` + `f37bf47`.
- Stance: adversarial. I looked for ways the change loses meeting data, grants
  access it should not, misleads waiting clients, or fails the stated requirement.
- Scope: the only file this review writes is this one. I made no change to the
  worktree, running services, Fly/Doppler/Zoom configuration, or production data.
  The reproduction probes ran in a scratch copy of the repository, outside the
  worktree, against
  `postgres://zoom:zoom@127.0.0.1:5432/zoom_connect_scheduling_review_test`.

## 1. Requirement under review

Authoritative: paste an existing Zoom invitation into zoom-connect. Clients or
agents then connect to zoom-connect before the meeting and wait on a stable
stream URL that delivers the call's events when they become available. Clients
never connect directly to Zoom. Shipping is authorized if the change is sound;
otherwise it needs independent review.

Repository rules that bear on it (CLAUDE.md, as amended by this PR):

- Call IDs are ULIDs assigned at registration or live ingestion. They are never Zoom UUIDs.
- Access is participant-or-share at every route, including SSE and downloads.
  Admins may register a call and receive an explicit share. Owner or host
  columns alone never grant access.
- The dated nlspec amendments govern. Tests are Go tests against the real
  handler. `go test ./...` and the frontend check must pass before claiming the change works.

## 2. Verdict

**The code is sound enough to ship for the planned live Zoom test, with one
hard precondition and two follow-up fixes. Do not yet claim that the
requirement works with real Zoom.**

- The core path is correct, durable and well tested in simulation:
  1. Registration from a pasted invitation returns a ULID.
  2. An API-key client holds `GET /api/calls/{id}/stream` open while the call is `scheduled`.
  3. The first `rtms_started` on that meeting number flips the same call to
     `live`, and the client receives `stream.started` and transcript text on the
     same connection.
  4. `Last-Event-ID` resumes, and cancellation ends the stream.

  Clients never touch Zoom.
- Every round-1 capture regression (F1–F4) is fixed and has a regression test.
  The access model (F5) and the spec gap (F6) were settled by writing them into
  nlspec and CLAUDE.md. I found no remaining regression against `main` in
  upstream capture.
- **Hard precondition (D1):** per the worklog, Fly's Doppler token has expired.
  The container entrypoint is `doppler run --no-fallback`, so any deploy or
  machine restart will fail to boot and take the app down. Replace the token
  before deploying.
- **Fix next (R1, R2):** the waiting-client contract has two holes. Neither loses
  more upstream data than `main` does, but both break the promise of one stable
  URL. Neither is tested.
  - R1: Zoom's recoverable `rtms_interrupted` ends every waiting client
    permanently.
  - R2: a stream that never receives a stop keeps the call `live` forever.

  R1 also records a resolution of nlspec open question 3 that contradicts
  Zoom's documented failover behavior. Correct that text before it hardens into
  the recorder's future reconnect design.
- **Proof still owed:** all incoming Zoom data in the evidence is simulated. The
  hosted proof script exists but has never been run, and
  `ephemeral/scheduled-hosted-proof/` does not exist. The worklog and README
  state this honestly. The PR must stay labelled accordingly until the
  October 19, 7 PM Costa Rica test meeting shows a pre-connected client
  receiving real opening transcript.

## 3. What I verified

| Check | Result |
|---|---|
| `go vet ./...` | clean |
| `TEST_DATABASE_URL=…_scheduling_review_test go test -count=1 ./...` | all pass. Verbose run confirms nothing skipped: `TestScheduledCallWaitStartReplayAndCancel`, `TestRuntimeRestartSupersessionAndShareAccess`, `TestInvitationMeetingID`, `TestParseFromPrefersReconnectCursor`, `TestBridgeUsesDurableZoomIdentity`, `TestBridgeFallsBackWhenAttachmentUnavailable` (400/409/500), `TestSignedStartCarriesMeetingNumberToApp` (number / string / null), plus all pre-existing tests |
| Frontend check | **Not independently reproduced.** In an isolated copy, `svelte-check` reports one error in the unchanged `src/routes/+layout.svelte` (`./$types`). That is environmental: generated types are bound to the original path. The committed `ephemeral/scheduled-check.log` (0 errors) dates from `8ae0a07`. `f520cfe` later edited four Svelte files (`AppShell`, `agent-api`, `calls/+page`, `AddScheduledCall`), so the claim "frontend check remains clean" has no committed log for HEAD. Re-run `vp run check` in the worktree and commit the log. |
| Proof artifacts `ephemeral/scheduled-proof/` | `client-stream.txt`, `result.json`, `waiting.png`, `live.png` inspected. They are consistent: one API-key connection sees `call.scheduled` → keepalive → `stream.started` → transcript → `stream.stopped` → `end`. Generated 18:29 CST, before `f37bf47`. That commit only changes the global `/api/stream` feed, which this proof does not exercise. Incoming Zoom data is simulated via `POST /api/rtms/streams` and `/api/demo/calls/{id}/events`, and is labelled as such. |
| Hosted proof | `ephemeral/scheduled-hosted-proof.mjs` exists. No output directory exists, so it was never run. Nothing has been deployed. |
| Zoom RTMS failover docs | developers.zoom.us/docs/rtms/meetings/failover-reconnection: `meeting.rtms_interrupted` = signaling dropped, and Zoom allows about 60 s to reconnect. `meeting.rtms_started` for an already-active stream = RTMS server failover, and "the meeting_uuid and rtms_stream_id stay the same". `rtms_stopped` with stop_reason 10–19 or 24 = missed reconnection window. |
| Reproduction probes | §5. Two scenarios run against the real handler and database, both confirmed. |

Read in full: the diff against `origin/main`, `scheduling.go`, the `stream` and
`authorizedCall` code in `server.go`, `share_admin.go` (ingestion and
announcements), `cmd/rtmsrec/bridge.go`, and the webhook and signaling code in
`cmd/rtmsrec/main.go`. Also read: migration 003 (both versions), the store
migration runner, the October 19 nlspec amendment and open question 3,
CLAUDE.md, the README section, the worklog, round 1, `Dockerfile`, `fly.toml`
and `deploy/run-services.sh`.

## 4. Round-1 findings: disposition

| Round 1 | Status at `f37bf47` |
|---|---|
| F1 `meeting_id` absent → no capture | **Fixed.** `main.go:526-535` connects regardless. The app creates a UUID-keyed call. Tested for number, string and null. |
| F2 new stream on an ended call discarded | **Fixed** for fresh stream IDs: `scheduling.go:314` reopens with the same URL and keeps `started`. Tested. See R1 for the same-ID case. |
| F3 stale `live` blocks the number | **Fixed.** The pending index now covers `status='scheduled'` only. A new UUID interrupts live predecessors and records `superseded_by_uuid`, so a delayed old UUID cannot capture or close the new session. Tested. |
| F4 no fallback on attach failure | **Fixed.** A 4xx falls back immediately. A 5xx or network error falls back after 3 attempts. Tested. See R3 for the retry shape. |
| F5 any user can claim any meeting | **Resolved by decision.** Registration is admin-only and access comes from an explicit share. Host-column read bypasses were removed, and a test proves that deleting the share denies access. Recorded in nlspec and CLAUDE.md. Residual risk in §7. |
| F6 spec not amended | **Fixed.** A dated October 19 nlspec amendment was added, CLAUDE.md was updated, and open question 3 was marked resolved (see R1). |
| F7 minor defects | Duplicate `stream.stopped` fixed (RowsAffected guard). Cancellation now announced as `call.cancelled`. `/wc/{id}/join` and `/s/` links now parse. Redundant `ALTER` removed. Still open: past `scheduled_at` accepted; `/my/{vanity}` links unsupported; the `stream()` status snapshot race (pre-existing, ≤3 s delay). |

## 5. Findings

### D1. Deploying now will take the app down: the Doppler token has expired (blocker for deploy, not for merge)

`Dockerfile:11` uses `ENTRYPOINT ["doppler","run","--no-fallback",…]`. The
worklog records that the deployed token is expired ("verified remotely:
Invalid Auth token"). The running machine only works because its processes
loaded configuration before the token expired. Any `fly deploy` or machine
restart, including a crash that triggers `run-services.sh`'s `exit 1` path,
will start a container that cannot fetch secrets. With `--no-fallback`, the
app and recorder will not start, and the live meeting test cannot happen.
`min_machines_running = 1` with one machine means no overlap.

Fix: stage a fresh read-only `zoom-connect/prd` service token on Fly. Then
confirm that `doppler run` succeeds in a one-off machine (or `fly ssh console`)
before deploying this branch. Schedule the deploy before the 7 PM test with
enough slack to roll back.

### R1. A recoverable Zoom interruption permanently ends every waiting client and refuses Zoom's documented failover (high for the client contract; not a capture regression)

What Zoom does, per the failover doc:

- `meeting.rtms_interrupted` means the signaling socket dropped. Zoom waits
  about 60 s for a reconnect before it stops the stream.
- A server failover arrives as `meeting.rtms_started` with the **same**
  `meeting_uuid` and **same** `rtms_stream_id`.

What this PR does:

1. `main.go:536-537` maps `rtms_interrupted` to
   `POST /api/rtms/streams/{id}/stop {"status":"interrupted"}`.
2. `stopRTMSStream` marks the stream row and, if it is the last one, the call
   `interrupted`. It emits `stream.interrupted`, which makes `stream()` send
   `event: end` to every waiting client.
3. The bridge sets `closed[streamID]`, so any later text on that stream is
   dropped in the recorder.
4. If Zoom then fails over (`rtms_started`, same IDs), `attachRTMSStream` finds
   the durable stream row (`scheduling.go:216-226`). It returns the call with
   `stream_status: interrupted`, and the bridge closes the stream.
5. `addDemoEvent` rejects any text with 409 because the call is no longer `live`.

Probe (real handler and database), sequence: register → attach `u1`/`s1` →
stop `s1` `interrupted` → attach `u1`/`s1` again → post text.

```
after interrupt, same-ID re-start: status=interrupted stream_status=interrupted
text after reconnect: HTTP 409
SSE for a fresh client ends immediately: true
```

Why it matters for this requirement:

- The promise is one stable URL that delivers the call's events. On the most
  common Zoom-side failure, a waiting agent receives `end`. Following the
  documented contract (agent-api page: "The stream ends with an `end` event
  after the call stops or is cancelled"), it disconnects.
- If the host restarts RTMS, the PR does resume the call at the same URL with a
  fresh stream ID. The agent has already left, though, and nothing tells it to
  come back. Only nlspec mentions that a client "that already received `end`
  can reconnect". The README and the agent-api page do not.

What is not new:

- On `main` the bridge also marked a stream closed on `rtms_interrupted`.
- The recorder has no signaling reconnect: nlspec lists "recovery of recorder
  sockets" as a non-goal, and `sv.start` ignores a duplicate `rtms_started` for
  a stream it still holds.

So upstream text after an interruption was already lost. What is new is
durable state and a spec resolution that make the loss permanent and push it
to clients. nlspec open question 3 is now marked "resolved: … duplicate/closed
stream IDs remain idempotent", which is the opposite of Zoom's documented
failover semantics. When the recorder gains reconnect support, the app will
refuse it.

Fix (small, local):

- (a) Treat `interrupted` as non-terminal for the call. Record the stream row
  as `interrupted` and emit a non-terminal event (for example
  `stream.paused`, or keep `stream.interrupted` but do not send `end` for it).
  Close the call only on `rtms_stopped`, or when nothing re-attaches within a
  bounded window (about 2 min, matching nlspec's RECONNECTING → STOPPED
  timer).
- (b) Let an `rtms_started` for the same stream ID and UUID re-attach a stream
  whose row is `interrupted`, but not one that is `stopped`. Keep superseded
  UUIDs refused.
- (c) Rewrite nlspec open question 3 and the README paragraph accordingly. Add
  "`end` may be followed by a resumed session at the same URL; reconnect with
  `Last-Event-ID`" to the agent-api page.
- (d) Add a real-handler test: interrupted → same-ID restart → text persists →
  the waiting client never saw `end`.

### R2. A stream that never receives a stop keeps the call `live` indefinitely (medium; plausible, Zoom-side trigger unverified)

The PR's rule is that a call ends when its last active stream stops. A stream
row is created at webhook time (`bridge.start` → `attachRTMSStream`), before
the recorder has even dialed Zoom. Nothing in the recorder reports a stream
that failed afterwards:

- `runSignaling` (`main.go:238-321`) returns on dial failure, handshake
  failure or `streamStateTerminated`. It calls `closeAll()`, but never
  `bridge.stop`.
- If Zoom then sends no `rtms_stopped` for that stream ID, its row stays `live`.

Probe: register → attach `u2`/`good` → attach `u2`/`failed-handshake` →
stop `good`.

```
call after real stream stopped, phantom never stopped: status=live
```

Consequences:

- Waiting clients get keepalives forever instead of `end`. The UI shows LIVE
  indefinitely.
- Only a later session with a new UUID on the same meeting number clears it,
  via supersession. A UUID-only call with no meeting number never clears.
- Before this PR a phantom stream produced an empty standalone call. Now it
  pins the real, scheduled call open.

Realistic triggers:

- Zoom's forum documents two `rtms_started` per meeting (same UUID, different
  stream IDs) when two environments share a subscription; one handshake fails.
- A bad signature or scope makes the handshake fail.
- The bridge's 5 s HTTP timeout fires after the app committed the attach. The
  bridge then falls back to standalone, and the scheduled call keeps a live
  stream row that is never stopped (`bridge.stop` takes the demo-stop branch
  for standalone streams).

I could not confirm whether Zoom always sends `rtms_stopped` for a stream that
never connected.

Fix:

- When `runSignaling` exits for any reason other than a webhook-initiated
  close, post `stop {"status":"interrupted"}` for that stream ID. Interacts
  with R1: under the R1 fix, an `interrupted` stop keeps the call open within
  the reconnect window.
- When the bridge falls back to standalone after a network or timeout error,
  also post a best-effort stop for the scheduled attachment.
- Add a test for each.

### R3. Transient-error fallback is frame-driven, has no backoff, and is silent to waiting clients (medium-low)

`bridge.call` counts attempts per call, and `text()` calls it once per incoming
frame. Three failures therefore take milliseconds, not seconds. Once
`standalone[streamID]` is set, it is permanent for that stream. Sequence on a
5xx: the start attempt fails, the first transcript frame's attempt fails and
that frame is **dropped**, and the second frame's attempt falls back to a new
standalone call.

A one-second database or app hiccup at meeting start (the app runs
`ALTER TABLE … ADD CONSTRAINT` and re-creates an index at every boot, see R4)
therefore permanently diverts the whole meeting away from the URL clients are
waiting on. The README is honest that fallback "does not deliver into the
waiting call". However, the waiting clients get no signal: they keep receiving
keepalives on an empty call.

Each attempt also holds the single bridge mutex for up to the 5 s client
timeout, which stalls every other stream's frames.

Fix:

- Make the retry budget time-based (for example 30 s with backoff) rather than
  frame-based, buffering frames meanwhile.
- When falling back, try once to append a `stream.redirected {call_id}` event,
  or at least log it at error level, so a waiting client or operator can find
  the transcript.

### R4. Migration 003 does exclusive-lock DDL on every boot (low)

`store.Open` re-executes every migration at startup. Migration 003
unconditionally runs:

- `DROP CONSTRAINT calls_status_check` / `ADD CONSTRAINT … CHECK`, which
  re-validates the whole table under ACCESS EXCLUSIVE;
- `DROP INDEX calls_zoom_pending` / `CREATE UNIQUE INDEX calls_zoom_pending`.

At current scale this takes milliseconds. Note two consequences:

- There is a window at boot in which the pending-uniqueness index does not
  exist. This is harmless because the statements run as one implicit
  transaction.
- The runner's "idempotent" premise now means "rewrites on every boot".

The `DROP`/`CREATE` of the index is the right way to replace the round-1
`IN ('scheduled','live')` index on databases that already applied the old 003.
Keep it, but guard it so it does nothing when the index definition already
matches. Leave the CHECK as `ADD … NOT VALID` plus `VALIDATE`, or guard it
with a `pg_constraint` lookup.

### R5. Smaller items (low)

- `scheduled_at` accepts past timestamps. A past time on the Upcoming card
  reads as "Expected Oct 1", which is confusing but harmless.
- `/my/{vanity}` personal links, common in invitations from people with
  vanity URLs, are rejected. The error tells the user to paste a `/j/` link,
  which is clear.
- Only the registrar can cancel (`HostUserID` check). If the registrar is
  unavailable, the pending registration blocks that meeting number (409) and no
  other admin can clear it. Consider letting any admin who holds a share cancel.
- `addDemoEvent` checks `c.Status == "live"` outside the hub lock, so a text
  frame racing a stop can be appended after `stream.stopped`. This is benign
  (the frame is persisted and replayed) but is the inverse of the 409 the
  check intends.
- Evidence freshness: the frontend check log predates `f520cfe`'s Svelte edits
  (§3), and the browser proof predates `f37bf47`. Re-run both on HEAD and
  commit the outputs, so the PR's evidence matches its head.

## 6. What is sound

- **Transaction discipline.** `hub.changeCall` holds the hub mutex, commits the
  lifecycle change, any supersession updates and every affected event in one
  transaction, then fans out. A subscriber never sees an event the database
  lacks, and supersession emits `stream.interrupted` to the old call in the
  same commit.
- **Identity model.** Four identifiers are kept distinct and persisted, as
  nlspec requires: public ULID, reusable meeting number (selects only
  `scheduled` rows), runtime UUID (binds a session) and stream ID (idempotency
  and tombstones). Stop-before-start creates a `call_id NULL` tombstone that
  cannot claim a later registration. Tested.
- **Old sessions cannot claim new registrations.** Re-attaching an old runtime
  after a new registration returns the old call (tested). `superseded_by_uuid`
  stops a delayed stream from an older UUID from interrupting the current
  session (tested).
- **Waiting SSE contract.** `callOpen` keeps the connection open through
  `scheduled` → `live`, sends keepalives, and re-checks key validity and share
  access every 3 s. `Last-Event-ID` takes precedence over `?from=` (tested), and
  cancellation sends `call.cancelled` + `end` (tested). The restart test
  re-creates the handler and proves that a pending registration and its
  subscription survive an app restart.
- **Access.** Registration and the RTMS mutation endpoints are admin-only
  (403 for a non-admin, tested). Read access is participant-or-share
  everywhere. `toCall`'s `CanShare`/`IsOwner` are presentation only, and
  `canShareCall` requires the owner to still hold access. The invitation and
  passcode are never stored: only the regex-extracted meeting number is.
  Host validation rejects look-alike, userinfo and `http` URLs (tested).
- **Global feed.** `f37bf47` moves `: connected` after the initial snapshot,
  which closes the lost-transition window, and adds `call.started` for
  resumed calls (tested).
- **UI.** Svelte is UI only and every I/O call goes to Go endpoints. The
  waiting state, cancellation, and the live transition on `stream.started`
  all render correctly in the screenshots.
- **Deployment pairing.** App and recorder ship in one image and one
  `run-services.sh`, and either one exiting restarts both. The new
  bridge↔app protocol therefore cannot skew between versions.

## 7. Residual risks accepted by design (not defects)

- **A registration claims the next session on that number, whatever it is.**
  If an admin registers a PMI and the PMI's owner first holds a different,
  private meeting there, that meeting's transcript goes to everyone shared on
  the registration. The admin-only gate is the mitigation. The README says
  "next live session on that link", which is accurate. Consider showing the
  matched Zoom topic, if RTMS ever supplies it, so the mismatch is visible.
- **One runtime UUID per meeting number at a time.** Supersession assumes
  this. If Zoom ever emits RTMS for breakout rooms as separate UUIDs under the
  same meeting number, the main session would be interrupted and its later
  text rejected with 409. I found no Zoom documentation either way. Watch for
  it in the live test if breakouts are used.
- **Upstream capture is unattended-hostile by nature.** RTMS needs an eligible
  host present and the app enabled, and nothing here joins a meeting. The UI
  copy and README say so.

## 8. Before the live test, in order

1. **D1:** replace the Doppler token and verify a cold start.
2. Re-run `vp run check` and the browser proof on HEAD, and commit the logs (R5).
3. Optional but cheap: the R1 doc change (tell clients `end` may be followed
   by a resume at the same URL).
4. Deploy. Run `ephemeral/scheduled-hosted-proof.mjs`. Register the real test
   meeting's invitation and connect an API-key client **before** starting the
   meeting. Start RTMS. Capture the client's raw SSE showing `call.scheduled` →
   `stream.started` → real transcript on one connection, plus the recorder log
   line `live Zoom call: …/calls/<the registered ULID>`. If possible, also
   toggle the RTMS app off and on once, so the resume path (fresh stream ID →
   same URL) is observed with real Zoom.
5. Then fix R1 and R2 with their tests, followed by R3 and R4.

## Sources

- Zoom RTMS failover and reconnection: https://developers.zoom.us/docs/rtms/meetings/failover-reconnection/
- Zoom devforum, two `rtms_started` per meeting (same UUID, different stream IDs): https://devforum.zoom.us/t/two-meeting-rtms-started-webhooks-fire-per-meeting/143978
- Zoom devforum, interrupt then stop with reason 19: https://devforum.zoom.us/t/rtms-interrupts-and-disconnects-stop-reason-19/144768
