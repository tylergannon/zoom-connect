# Zoom Connect — product requirements

Status: draft, 2026-10-15. Captured from Tyler's brief. `nlspec.md` is still
the authority for the meeting pipeline (RTMS, session, archive); this file adds
the product surface around it and lists where the two disagree (§4).

Zoom Connect is a web application with a Go service behind it (an skgo app).
Its entire purpose is to live as a Zoom bot for a small company: it listens to
the company's Zoom calls, keeps the transcripts, and serves them to the
company's people and to their agents, live and afterward.

**MVP line.** The MVP is reached when we can minimally listen to Zoom calls,
have another connected app or two listening in live, or download the finished
transcript afterward. Everything marked *Deferred* is not MVP.

## 1. User-facing claims

| # | Claim | Scope |
|---|---|---|
| U1 | I sign in with Google. Google is the only sign-in method. | MVP |
| U2 | Only people with an email address at the organization's Google Apps domain can sign in. Anyone else is refused. | MVP |
| U3 | Once in, I can see any call I was in. | MVP |
| U4 | When a new call connects, it immediately gets its own URL. Anyone allowed to see it can open that URL, sign in, and start viewing the call right then; it updates live. | MVP |
| U5 | After a call ends, the same URL shows the finished transcript. | MVP |
| U6 | I can create API keys and revoke them, so I can give a key to my agents. | MVP |
| U7 | An agent holding my API key can watch a call live (SSE) and download the entire structured text data of a finished call. | MVP |
| U8 | Someone who was in a call can share it with anyone in the organization who was not in the call. Sharing produces a link I can copy and send to my coworker; that coworker can then see the call. | MVP (see Q2) |
| U9 | An admin can configure and manage the Zoom connection (whatever Zoom needs to be able to see our calls) from a screen inside the web application. | MVP (see Q1) |
| U10 | When a new call begins, each member of the call who is in the organization gets a Slack DM with a link to it. | Deferred |
| U11 | When a call is done, each such member gets a Slack DM with a link to the finished call. | Deferred |
| U12 | Media shared during the call (screen share, files) is available too. | Deferred |

## 2. Technical requirements

### Identity and access

- T1. Google OpenID Connect only, through skgo. A sign-in is accepted only if
  the ID token's email is verified and its `hd` claim equals the domain in
  the environment variable `ALLOWED_DOMAIN` (the `hd` login hint alone is not
  trusted). (U1, U2)
- T2. A user row is upserted on every accepted sign-in.
- T3. Call visibility: a principal may read a call if they were a participant
  in it **or** hold a share for it. (U3, U8) This replaces nlspec §4's
  "every signed-in user sees every meeting".
- T4. Participant → user matching is by email, case-insensitive, within
  `ALLOWED_DOMAIN`. Participants without a matching email (guests, phone
  dial-ins, external people) are recorded but map to no user. See Q3.
- T5. A share is a row `(call_id, grantee user or email, granted_by, created_at,
  revoked_at)`. Only a participant (or an existing grantee — see Q2) can
  create one. The share link is the call's own URL; the share row is what
  makes it open for the grantee. A grantee who has never signed in is matched
  on first sign-in by email.
- T6. API keys: random ≥32-byte secret, shown exactly once, stored hashed,
  `prefix` kept for display; create, list, revoke per user. A key acts as its
  owner: it sees exactly the calls its owner can see, including calls the
  owner joins later. (U6, U7)
- T7. An admin role exists for the Zoom-connection screen. See Q1.

### Calls

- T8. Every new call (each RTMS stream start) is assigned a ULID `call_id` at
  the moment the webhook arrives. The call URL is `/calls/{call_id}`. The
  Zoom meeting UUID is stored but never appears in URLs. (U4)
- T9. Postgres holds a call index: `calls(id ULID, zoom_meeting_uuid,
  stream_id, topic, host_email, started_at, ended_at, status)` and
  `call_participants(call_id, zoom_participant_id, display_name, email,
  user_id nullable, joined_at, left_at)`. Meeting *content* stays in the
  bucket (nlspec §1); the index exists because access checks need it.
- T10. Live view: the browser follows the call through the same event stream
  agents use (SSE, nlspec §5.6), resuming by `Last-Event-ID`. (U4)
- T11. Finished view: the same URL renders the archived transcript; downloads
  as `txt`, `vtt` and `json` (nlspec `api` cell). (U5)

### Agent API

- T12. All agent routes take `Authorization: Bearer <key>` and enforce T3.
- T13. `GET /api/v1/calls` — calls the key's owner can see, newest first, with
  `?status=live` filter.
- T14. `GET /api/v1/calls/{id}/stream?from=<seq>` — SSE; replays then follows
  live; `event: end` on stop. Any number of concurrent subscribers. (U7)
- T15. `GET /api/v1/calls/{id}/events` (NDJSON, structured) and
  `GET /api/v1/calls/{id}/transcript?format=txt|vtt|json` — the entire
  structured text data of a call, live or finished. (U7)
- T16. `GET /api/v1/stream` — one SSE connection that announces every call
  the key's owner gains access to (`call.started`, `call.shared`,
  `call.ended`), so an agent can broadcast-listen without polling. (U7:
  "broadcast any call live to anyone who has the right API key")

### Zoom connection management

- T17. The Zoom app credentials (`ZOOM_CLIENT_ID`, `ZOOM_CLIENT_SECRET`,
  `ZOOM_WEBHOOK_SECRET`) can be entered and rotated in the admin screen and
  are stored encrypted in Postgres; environment variables, if set, win and
  show as read-only. (U9)
- T18. The admin screen shows: the webhook URL to paste into Zoom, the
  required scopes and event subscriptions as a checklist, the time and result
  of the last webhook received, a URL-validation status, live stream count,
  and the media kinds to capture (transcript, chat; audio off by default).
- T19. Secrets are write-only in the UI: once saved, only their last four
  characters show.

### Slack (Deferred)

- T20. A Slack app installed into the organization's workspace with
  `chat:write` and `users:read.email`. A call member is matched to a Slack
  user by email.
- T21. On `call.started`, DM each org member of the call a link to it; on
  `call.ended`, DM the link to the finished transcript. Members joining late
  get the start DM when they join.
- T22. Users can turn Slack DMs off for themselves.

### Media (Deferred)

- T23. Capture media shared during the call (screen share frames, files) and
  expose it beside the transcript. Not designed yet.

## 3. Open questions

- Q1. **Who is an admin?** Proposal: `ADMIN_EMAILS` environment variable,
  plus admins can promote others in the UI later.
- Q2. **Who can share?** The brief says participants share with
  non-participants. Can a grantee re-share? Proposal: no, participants only.
  Can a share be revoked? Proposal: yes, by whoever granted it.
- Q3. **Do we get participant emails?** RTMS participant events carry a Zoom
  user id and display name, not necessarily an email. Candidates: the
  `meeting.participant_joined` webhook (carries `email` for signed-in Zoom
  users) or the past-meeting participants REST call after the call ends.
  If emails only arrive after the call, U3 for *live* calls depends on it;
  the spike recording (`spike/README.md`) must answer this.
- Q4. **Host access.** Should the host see a call even if they left before
  RTMS started? Proposal: yes, the host is always a participant.

## 4. Required nlspec amendments

| nlspec | Change |
|---|---|
| §4 "every signed-in user and every API key sees every meeting" | Replaced by T3. |
| §4 "a meeting index in the database" excluded | Replaced by T9 (index only; content stays in the bucket). |
| §5.3 meeting `id` in URLs is `path_id` | URLs use the ULID `call_id` (T8); `path_id` stays a bucket-layout detail. |
| §6 `cmd` reads Zoom secrets from env only | T17 adds DB-stored, admin-managed credentials. |
| §6 `api` routes | Move under `/api/v1/…`, add T13 and T16. |
| §6 `store` tables | Add `calls`, `call_participants`, `shares`, `settings`; add admin flag. |
