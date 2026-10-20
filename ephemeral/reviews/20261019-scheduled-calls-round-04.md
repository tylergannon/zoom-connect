# Adversarial review: PR #3, scheduled Zoom calls (round 4)

- Date: 2026-10-19
- Branch: `codex/scheduled-zoom-calls` at `2c605c9` (4 commits over `origin/main`: `8ae0a07`, `f520cfe`, `f37bf47`, `2c605c9`)
- Worktree: `/Users/tyler/.codex/worktrees/f4c2/zoom-connect`
- Prior rounds: round 1 and round 3 completed. Round 2 produced no artifact.
  This round reviews the whole PR, with particular attention to `2c605c9`
  ("preserve waiting clients through Zoom interruptions"), which answers round 3.
- Reviewer: Claude Opus 5.5. The `/adversarial-review` command is not installed in
  this session, so I did the review directly.
- Stance: adversarial. I looked for ways the change loses meeting data, grants
  access it should not, misleads waiting clients, or fails the stated requirement.
- Scope and side effects:
  - This file is the only one I wrote in the repository.
  - `vp run check` regenerated the gitignored `web/.svelte-kit/` directory.
  - Probes ran in a `git archive` copy of HEAD inside my scratchpad, against
    `postgres://zoom:zoom@127.0.0.1:5432/zoom_connect_scheduling_review_test`.
  - I did not touch running services, Fly, Doppler, Zoom configuration, the
    hosted app or production data.
  - The worktree was clean when I started. During the review an untracked
    `ephemeral/scheduled-real-zoom-proof.py` appeared, written by another
    session. It registers meeting `89180298510` on the hosted demo and holds an
    SSE client open. I read it but did not run it or change it.

## 1. Requirement under review

Authoritative: paste an existing Zoom invitation into zoom-connect. Clients or
agents then connect to zoom-connect before the meeting and wait on a stable
stream URL that delivers the call's events when they become available. Clients
never connect directly to Zoom. Shipping is authorized if the change is sound;
otherwise it needs independent review.

Repository rules that bear on it:

- Call IDs are ULIDs, never Zoom UUIDs.
- Access is participant-or-share at every route, including SSE and downloads.
  Admins register calls and receive explicit shares. Owner or host columns
  never grant access on their own.
- The dated nlspec amendments govern. The October 19 amendment is the contract
  for this slice.
- Tests are Go tests against the real handler. `go test ./...` and the frontend
  check must pass.

## 2. Verdict

**Sound to ship for the planned live Zoom test. I found no blocking code
defect.** The core requirement works end to end in simulation:

1. An admin pastes an invitation and gets a ULID.
2. An API-key client holds `/api/calls/{id}/stream` open while the call is `scheduled`.
3. The first RTMS start on that meeting number turns the same call `live`.
4. The client receives the transcript on the same connection, can resume with
   `Last-Event-ID`, and receives `end` on cancellation.

Every round-3 finding (R1–R3) is fixed and has a test. Two remain open:
round-3 R4 and most of R5.

Before calling the requirement met, two things are still owed:

- **A real Zoom proof.** All committed evidence uses simulated Zoom input. The
  hosted proof directory still does not exist. A real-Zoom proof script appeared
  during this review, so the live test looks under way. Its result is not
  evidence yet.
- **A cold-start check of the replacement Doppler token** before any deploy or
  restart (round-3 D1; see §6).

The new findings are follow-ups, not blockers. Two of them (N1, N2) mark where
the "stable URL" promise stops in real meetings. The operator should know them
before the live test:

- **N1. A meeting restart leaves the stable URL.** If the host ends and
  restarts the meeting, Zoom issues a new runtime UUID. That session lands in a
  new call that the people shared on the registration cannot see.
- **N2. The two-minute "recovery window" cannot recover the common case.** Zoom
  expects the app itself to reconnect after `rtms_interrupted`. The recorder
  never reconnects, so a pause is in practice a delayed `end`.

## 3. What I verified

| Check | Result |
|---|---|
| `go vet ./...` | clean |
| `TEST_DATABASE_URL=…review_test go test -count=1 ./...` | pass (`cmd/rtmsrec`, `internal/httpapi` 17.4 s) |
| `go test -race ./cmd/rtmsrec/` | pass |
| Scheduling tests, verbose | `TestScheduledCallWaitStartReplayAndCancel`, `TestRuntimeRestartSupersessionAndShareAccess` and `TestInterruptedStreamKeepsClientAndExpiresDurably` all PASS, none skipped |
| Frontend `vp run check` in the worktree at HEAD | `5088 FILES 0 ERRORS 0 WARNINGS`. This closes round-3's evidence gap, although the committed `ephemeral/scheduled-check.log` still dates from `8ae0a07` |
| `ephemeral/scheduled-proof/` | Timestamps are 00:55:47Z, 24 s before the `2c605c9` commit, so the proof matches HEAD. One API-key connection shows `call.scheduled` → keepalive → `stream.started` → transcript → `stream.stopped` → `end`. Replay and UI cancellation are in `result.json`. Incoming Zoom data is simulated and labelled as such. The proof does not exercise `stream.paused`. |
| Hosted or real-Zoom proof | Not present in the tree. `scheduled-real-zoom-proof.py` is untracked and has produced no output in the tree. |
| Zoom failover docs (developers.zoom.us/docs/rtms/meetings/failover-reconnection) | Re-read for this round. On `meeting.rtms_interrupted`, the **app** closes its sockets and reconnects signaling and media itself, with backoff. Zoom allows about 60 s for signaling and about 30 s for media, and the page describes no new `rtms_started` for this case. A `rtms_started` for an already-active stream means server failover, with the same `meeting_uuid` and `rtms_stream_id`. The page states no handshake deadline after `rtms_started`; its sample says "connect immediately". |
| Reproduction probes | 7 scenarios against the real handler and database (§5). All behaved as described. |

Read in full at HEAD:

- `internal/httpapi/scheduling.go`
- `cmd/rtmsrec/bridge.go`
- the `main.go` webhook, `start`, `runSignaling`, `runMedia` and `closeAll`
- `stream`, `auth`, `parseFrom` and `transcript` in `server.go`
- `hub.go` `persist`, and `share_admin.go` (demo ingestion and announcements)
- migration 003, `queries.sql`, and the Svelte changes (`AddScheduledCall`,
  `ScheduledCallNotice`, `CallWorkspace`, `calls/+page`, `agent-api`, `AppShell`)
- `Dockerfile` and `deploy/run-services.sh`
- the October 19 nlspec amendment and open question 3, the CLAUDE.md and
  README diffs, the worklog, and the round 1 and round 3 reviews

## 4. Round-3 findings: disposition at `2c605c9`

| Round 3 | Status |
|---|---|
| D1 expired Doppler token | **Mitigated, not verified.** The worklog says a replacement read-only token is staged on Fly. Nothing records that `doppler run --no-fallback` has succeeded with it on a cold start. Still a precondition for deploying (§6). |
| R1 interruption was terminal | **Fixed.** `stopRTMSStream` with `interrupted` sets the stream row `interrupted` with a persisted `interrupted_at` and emits non-terminal `stream.paused`. The call stays `live`. A same-ID, same-UUID start resumes an interrupted, non-superseded stream (`scheduling.go:227-233`). An expired deadline becomes final `stopped` with call `interrupted` and `stream.interrupted` → `end`. nlspec open question 3, the README and the agent-api page were updated. Tested by `TestInterruptedStreamKeepsClientAndExpiresDurably`. See N2 for what this can actually recover. |
| R2 phantom streams pinned calls live | **Fixed.** When `runSignaling` exits without a webhook-initiated cancel, `onExit` posts an `interrupted` stop, and the 2-minute expiry then closes the call. A timed-out attachment is retired with a `stopped` post before fallback. Tested by `TestSignedStartCarriesMeetingNumberToApp`, where a failed dial retires the attachment. |
| R3 frame-driven retry, global mutex | **Fixed.** The retry is time-based (30 s, backoff 1→5 s) and uses a per-stream mutex. The webhook flushes its 200 before any attachment work. A redirect signal to waiting clients was not added; the fallback is logged only. See N3 and N4 for the shape this takes now. |
| R4 migration DDL on every boot | **Open.** Migration 003 still runs DROP/ADD CHECK and DROP/CREATE INDEX on each boot. Harmless at current scale. |
| R5 small items | **Mostly open.** Past `scheduled_at` is still accepted. `/my/{vanity}` links are still rejected. Only the registrar can cancel. `addDemoEvent` still checks `live` outside the hub lock. Evidence freshness is now fine (§3). |

## 5. New findings

All probes ran at HEAD in the scratch copy. Each item gives the observed output.

### N1. A restarted meeting leaves the stable URL, and sharees lose access to the rest of it (medium; documented design, a real gap for this requirement)

Probe P1:

1. Register meeting `11111111111`.
2. Attach `u-first` and stop it.
3. Attach `u-second` on the same number, which is what Zoom does when the host
   ends and restarts.

```
P1 registered=…JJ6 first=…JJ6 second=…1W2V sameURL=false
   registered events=[call.scheduled stream.started stream.stopped]
```

Probe P1b, the same sequence without a stop for the first runtime:

```
P1b registered status=interrupted, new session call=…0C1 sameURL=false
    events=[call.scheduled stream.started stream.interrupted]
```

What happens:

- The registration has already been consumed by the first runtime UUID.
- The new UUID marks the old call superseded (`stream.interrupted`, which sends
  `end` to every waiting client).
- It then creates a fresh standalone call. That call's only share goes to the
  recorder's admin account (`grantCaptureOwner` with the bridge key's principal).

So agents that were shared on the registered call get `end` and cannot see the
continuation at all. They are not even told it exists.

This matches the amendment ("registers the next live session", and "a share
applies to one call ULID, never automatically to later sessions"). The README
also says "a new meeting UUID instead starts a new session".

In practice, though, "host's Zoom crashed, rejoined" and "ended by mistake,
restarted" are among the most likely things to happen during a real call. Each
one silently moves the transcript away from the URL the client was told is
stable.

Suggested follow-up, pick one:

- (a) Within a short grace window after a registered call ends (for example
  10 minutes), let a new UUID on the same number re-attach to that call. Record
  `stream.started` with the new UUID and keep the URL and shares.
- (b) Copy the registered call's shares to the successor and emit a
  `call.superseded {successor_id}` event before `end`.

For tonight's live test: do not end and restart the meeting unless you are
deliberately testing this path. If the host's client drops, expect this
behavior.

### N2. The "recovery window" cannot recover a dropped signaling socket, which is the common interruption (medium; not a regression from `main`)

Zoom's failover doc is explicit. On `rtms_interrupted`, the **app** reconnects
signaling and media with backoff, within about 60 s for signaling and about
30 s for media. Zoom does not send a new `rtms_started` for this case.

What this PR does instead:

1. `runSignaling` exits, and `onExit` posts `interrupted`. The bridge sets
   `paused`, the app emits `stream.paused`, and the call stays `live`.
2. When the `rtms_interrupted` webhook arrives, the handler (now also on
   interrupted, `main.go:549-557`) closes all sockets.
3. Nothing ever re-dials `serverURL`.
4. About 60 s later Zoom sends `rtms_stopped` with a reason in 10–19, and the
   call becomes `stopped` → `end`.

So the paused state recovers only in the **server-failover** case, where Zoom
re-sends `rtms_started` with the same IDs. That path is implemented and tested.
For a network blip between Fly and Zoom, a client sees `stream.paused` → about
60 s of keepalives → `stream.stopped` → `end`, and the transcript from that
point on is lost.

`main` lost the same data, so this is not a regression. nlspec's open
question 3 is honest about it ("Automatic socket recovery without a new Zoom
start remains future work"). But the README and agent-api wording ("Temporary
Zoom interruptions emit `stream.paused` and keep clients connected for up to
two minutes") reads as a recovery promise the recorder cannot keep.

Fix (small, local to the recorder): in `onExit`, after posting
`interrupted`, re-run `runSignaling(serverURL)` with backoff for up to about
55 s while the stream is still in `sv.streams`. Media sockets already get fresh
URLs from the new handshake. Clear `paused` on a successful handshake and post
a resume to the app (see N8). Add a recorder test with a fake signaling server
that drops once.

### N3. Attachment retries delay the Zoom handshake by up to ~45 s, and the spec's "opening text is held" claim depends on unverified Zoom buffering (low-medium)

`sv.start` (`main.go:609-614`) runs `bridge.start` → `call()` **before**
`runSignaling`. While the app returns 5xx or hangs:

| Step | Worst case |
|---|---|
| Retry loop, 30 s deadline, last post can start just before the deadline | ~35 s |
| Retire `stop` post (5 s client timeout) | +5 s |
| Fallback `/api/demo/calls` post | +5 s |
| **Total before the recorder dials Zoom** | **~45 s** |

The amendment says "each stream serializes its own opening data behind
attachment so retries do not drop opening text". In fact no text is held by
the recorder. Nothing is received until signaling connects. Whether Zoom
retains transcript (not just the up to 60 s of buffered audio) for a client
that connects 30–45 s late is not documented. Zoom's sample says to connect
immediately, and no handshake deadline is published.

This only matters when the app is unavailable at meeting start. The app and
recorder boot together, so a webhook that arrives during an app restart
(migrations included) hits exactly this window.

Fix: dial Zoom immediately, and let `text()`/`screen()` queue into a bounded
per-stream buffer until `call()` resolves. That makes the spec sentence true.
At minimum, reword the amendment to say signaling is delayed.

### N4. Fallback before the attachment commits leaves the registration armed for a future, unrelated session (low-medium)

When the app fails with 5xx for the whole 30 s and the attachment never
committed:

- The bridge posts the retire `stop`, which creates a `call_id NULL` tombstone
  for that stream ID.
- It then creates a standalone call.

The registered call is untouched. It stays `scheduled`. Waiting clients keep
receiving keepalives while the meeting is captured elsewhere. And the
registration will claim the **next** session on that meeting number, which for
a PMI or recurring link may be a different meeting days later.

The README says fallback "does not deliver into the waiting call". It does not
say the registration stays armed.

Cheap mitigation: make one final attach attempt immediately before the
fallback `/api/demo/calls` post. If the app is reachable enough to create the
fallback call, it is usually reachable enough to attach. Otherwise, on a
successful fallback, post a `stream.redirected`/cancel event to the
registration.

### N5. Superseded stream rows are left `interrupted` with no deadline, so they never expire (low)

Supersession runs `UPDATE rtms_streams SET status='interrupted' WHERE call_id=$1 AND status='live'`
(`scheduling.go:281`), and the late-old-UUID insert runs
`INSERT … status 'interrupted'` (`:250`). Neither sets `interrupted_at`, and the
sweep requires `interrupted_at < now()-2min`.

Probe P2: `interrupted streams with NULL deadline (never expire): 1`.

This is harmless today because those calls are already terminal and
`superseded_by_uuid` blocks resume. It does break the new invariant that
`interrupted` means "inside a 2-minute recovery window", and any future code
that counts active streams will see phantoms. Write these rows as `stopped`,
since they can never resume.

### N6. An expired interruption records `ended` as the sweep time, not the interruption time (low)

The sweep is lazy. It runs on requests and on SSE ticks. Probe P5, with an
interruption three hours old:

```
P5 status=interrupted ended=2026-10-19T19:02:24-06:00 (interruption was 3h ago)
```

With no traffic overnight, a call's duration in the UI and downloads stretches
to the next morning's first request. Use
`ended = interrupted_at + interval '2 minutes'` (or `interrupted_at`) in the
expiry update. Have `RETURNING` carry the timestamp.

### N7. `POST /api/demo/calls/{id}/stop` on a scheduled call ends waiting clients while the call stays scheduled (low, admin-only)

Probe P4:

```
P4 demo stop HTTP 200; waiting client got end=true; status still=scheduled
P4b later attach claims same call=true status=live events=[call.scheduled stream.stopped stream.started]
```

`stopDemoCall` emits `stream.stopped` unconditionally. `persist` then runs
`StopCall`, which only changes `live` rows, and `stream()` sends `end` on that
event. The recorder never takes this branch for an attached call; only the
standalone path does. So only an admin or a stray script can trigger it. If
triggered, every waiting client disconnects from a still-valid call, and the
event history then contains a stop before the start.

Return 409 unless the call is `live`, or route the endpoint through
`changeCall` with a status guard.

### N8. A paused call that regains an active stream emits no resume event (low)

Probe P3: interrupt `x-1`, then attach fresh stream `x-2` on the same runtime,
which is what a host toggling RTMS does.

```
P3 events=[call.scheduled stream.started stream.paused transcript]
```

The call is still `live`, so `attachRTMSStream` returns at
`if status == "live"` without an event:

- The UI's `streamState` stays `reconnecting` ("Reconnecting…") while text flows.
- Agents see `stream.paused` with no counterpart.

The same applies to a multi-stream call where one stream pauses while another
stays live.

Emit `stream.started` (or a new `stream.resumed`) whenever an attach or resume
takes a call from "has interrupted streams" back to "has a live stream". Make
the UI clear `reconnecting` on any transcript as well.

### N9. A sweep failure turns every authenticated request into a 500 (low)

`auth` (`server.go:151-154`) runs `expireRTMSInterruptions` before every
handler and returns 500 if it fails. That couples transcript downloads,
metadata reads and the recorder's own text ingestion to a lifecycle write path.
A persistent failure, such as a read-only failover replica, lock contention or
a poison row, would take the whole API down and drop live text. Log the error
and continue; the SSE tick will retry.

### N10. A stop webhook can hold the recorder's global mutex for up to ~45 s (low)

The `rtms_stopped`/`rtms_interrupted` case takes `sv.mu` and then calls
`bridge.stop`, which needs the stream's `s.mu`. If that stream is still in its
attachment retry (N3), `bridge.start` holds `s.mu` for up to ~45 s. During that
time every other webhook's `sv.start` and every other stream's `onExit` block
on `sv.mu`. Zoom already has its 200, so there is no retry storm. The cost is
delayed handshakes for other meetings while the app is unavailable.

I traced lock order and found no deadlock: nothing takes `s.mu` and then
`sv.mu`. Fix: look up and delete the stream under `sv.mu`, then call
`bridge.stop` after unlocking.

## 6. Deployment path

- **Doppler (D1):** the image entrypoint is
  `doppler run --no-fallback --no-check-version --`. A replacement token is
  reportedly staged. Before deploying this branch, verify that a one-off
  machine or `fly ssh console` can run `doppler run --no-fallback -- true`
  with it. A bad token means neither process starts.
- **Boot ordering:** `run-services.sh` starts the recorder and app together,
  and either one exiting restarts both, so the bridge and app protocol cannot
  drift between versions. The new 30 s retry covers a webhook that arrives
  while the app is still running migrations (at the cost described in N3).
- **Migration 003 on the hosted database:** additive and safe for existing rows.
  Existing statuses are within the new CHECK, `started` loses NOT NULL, and new
  columns and indexes are nullable or partial. It runs as one implicit
  transaction on each boot (R4).
- **Deploying during a meeting** loses that meeting's recorder sockets. This is
  a non-goal. With this PR the call now closes cleanly: Zoom's
  `rtms_interrupted`/`rtms_stopped` reaches the new process, and the bridge
  posts the stop for that stream ID. The call no longer stays `live`.
- **Admin key:** the bridge key must belong to an admin, because
  `/api/rtms/streams` is admin-only. In hosted password mode the single
  account is an admin.

## 7. What is sound

- **Transactions.** `changeCall` commits status changes, stream mappings,
  supersession and every affected event under the hub mutex before fan-out. A
  subscriber never sees an event the database lacks.
- **Identity model.** ULID, meeting number, runtime UUID and stream ID stay
  distinct and are persisted. Stop-before-start tombstones cannot claim a
  registration. `superseded_by_uuid` stops delayed streams from an old UUID
  from touching the current session. Same-ID resume is restricted to
  `interrupted`, non-superseded streams with a matching UUID, and a final
  `stopped` never reopens.
- **Recovery durability.** `interrupted_at` is persisted, so a restart cannot
  reset the window. Expiry is idempotent and emits `stream.interrupted` only
  when no active stream remains.
- **Recorder lifecycle.**
  - `closeOnce` makes teardown idempotent.
  - `onExit` fires only for unexpected exits, and only if the stream is still
    current. A failover `rtms_started` cancels the old stream before its
    `onExit` can misreport it.
  - The ordering of a `TERMINATED` state followed by the `rtms_stopped`
    webhook leaves the final state at `stopped`, with one extra
    `stream.paused`.
- **Access.**
  - Registration and RTMS mutation endpoints are admin-only.
  - Read access is participant-or-share on every route, including SSE.
  - The SSE handler re-checks key validity and access every 3 s.
  - `canShareCall` requires the owner to still have access.
  - Invitation text and passcodes are never persisted.
- **SSE contract.** The connection stays open for `scheduled` and `live`,
  `Last-Event-ID` beats `?from=`, cancellation sends `call.cancelled` + `end`,
  and `stream.paused` does not end the stream. The browser's EventSource
  reconnect, which keeps `?from=` in the URL, now resumes correctly.
- **UI.** Svelte stays UI-only. Scheduled, paused and cancelled states render,
  and the "Add scheduled call" button only appears for admins.

## 8. Before and during the live test

1. Cold-start verify the replacement Doppler token (§6), then deploy.
2. Register the invitation and connect the API-key client **before** starting
   the meeting. Capture its raw SSE and the recorder log line
   `live Zoom call: …/calls/<registered ULID>`. The registered ULID must match.
3. Do not end and restart the meeting except to deliberately observe N1.
4. Optionally toggle the RTMS app off and on once. Expect a fresh stream ID to
   continue at the same URL, with no resume event (N8).
5. Afterwards, in order: N2 (recorder signaling reconnect), N1 (grace-window
   re-attach or share carry-over), N3 and N4 (dial first, buffer text, final
   attach before fallback), then N5–N10, R4 and R5.

## Sources

- Zoom RTMS failover and reconnection: https://developers.zoom.us/docs/rtms/meetings/failover-reconnection/
- Round 3 review: `ephemeral/reviews/20261019-scheduled-calls-round-03.md`
- Probe source (scratch, not committed): `/private/tmp/claude-501/-Users-tyler--codex-worktrees-f4c2-zoom-connect/daca3742-a9a6-4e0c-bbbe-00ec0a8147e3/scratchpad/repo/internal/httpapi/zz_probe_test.go`
