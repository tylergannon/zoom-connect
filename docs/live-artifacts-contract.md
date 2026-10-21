# Live call artifacts — prototype contract amendment (October 20, 2026)

This is the detailed contract behind the October 20, 2026 live call pages
amendment in `nlspec.md`, which states the intent; this file holds the exact
endpoints and formats. It adds one capability on top of the existing
agent-key, call ACL and live-stream design: an agent can publish an HTML page
for a call and keep replacing it, and a signed-in person can open that page and
share their window in Zoom so other participants watch it change.

## Contract

**Resource.** An artifact belongs to exactly one call. It has an id, a title
and a monotonically increasing `version`. Each create or update stores an
immutable snapshot `{version, title, html, author, key name, time}`. The id
never changes, so one page stays one row however often it is updated. Every
snapshot is kept with the call as its history; nothing is pruned. An earlier
build of this prototype kept only the newest 25 snapshots and deleted older
ones. Those deleted versions cannot be recovered, so a page made by that build
can have a history that starts above version 1.

**Credentials.** The existing ones only: `Authorization: Bearer <API key>` (the
key acts as its owner) or the browser session cookie. No new credential type,
scope, public link or bearer URL exists. Revoked keys and expired sessions get
401, and live streams opened with them close at the next access check.

**Who may do what.**

| Caller | List / read / stream / old versions | Create / update |
| --- | --- | --- |
| Call participant (and their keys) | yes | yes, while the call is scheduled or live |
| Call owner who can already read the call (and their keys) | yes | yes, while the call is scheduled or live |
| Coworker the call was shared with who is neither its owner nor a participant (and their keys) | yes | no (403) |
| Anyone else | 403 | 403 |
| Unknown call, or artifact id under another call | 404 | 404 |

Writing uses the same rule as sharing a call. The caller must first have read
access, which comes only from being a participant or from a share. Then the
caller must be either a participant, or the call's owner (`host_user_id`)
with that read access. An admin who registers a scheduled call gets an
explicit share as the capture owner, so they can read it, and as its owner
they can publish. Being recorded as a call's owner or host never grants read
access by itself: an owner with no participant identity and no share gets 403
like anyone else.

After a call ends (stopped, cancelled or interrupted) its artifacts are
read-only: updates return 409 and reads keep working for everyone with access.

**Endpoints.** Every path is also served under `/api/v1/...`.

| Method and path | Request | Success |
| --- | --- | --- |
| `GET /api/calls/{id}/artifacts` | | 200, array of artifact metadata (no `html`, with `bytes`) |
| `POST /api/calls/{id}/artifacts` | `{"title","html"}` | 201, snapshot, `Location` header |
| `GET /api/calls/{id}/artifacts/{artifactId}` | | 200, latest snapshot |
| `PUT /api/calls/{id}/artifacts/{artifactId}` | `{"base_version","html","title"?}` | 200, new snapshot |
| `GET /api/calls/{id}/artifacts/{artifactId}/versions` | `?before=`, `?limit=` | 200, one page of version metadata, newest first |
| `GET /api/calls/{id}/artifacts/{artifactId}/versions/{n}` | | 200, stored snapshot (404 if never stored or deleted by the earlier build) |
| `GET /api/calls/{id}/artifacts/{artifactId}/stream` | `Last-Event-ID` or `?from=` | `text/event-stream` |

A snapshot is the metadata plus `html`:

```json
{"id":"01M5DE92TF…","call_id":"01M5DE92RM…","title":"Live agenda","version":2,"bytes":55,
 "created_at":"2026-10-20T13:58:09.743799-06:00","updated_at":"2026-10-20T13:58:09.786927-06:00",
 "created_by":{"id":"u-dana","name":"Dana Kim","email":""},
 "updated_by":{"id":"u-dana","name":"Dana Kim","email":""},
 "updated_via":"meeting-agent","viewer_url":"/calls/01M5DE92RM…/artifacts/01M5DE92TF…",
 "html":"<h1>Agenda</h1>…"}
```

`updated_via` is the API key name when a key made the latest version. It is
left out for browser sessions.

**History.** The version list has no `html`:

```json
{"artifact_id":"01M5DE92TF…","call_id":"01M5DE92RM…","current_version":60,
 "versions":[{"version":60,"title":"Board 60","bytes":10,"created_at":"…",
   "author":{"id":"u-dana","name":"Dana Kim"},"via":"meeting-agent"}, …],
 "next_before":11}
```

It lists stored versions below `before` (default: all), newest first, `limit`
per page (1–100, default 50). `next_before` is the `before` value for the next
older page, or `null` when there is none. Bad `before` or `limit` values get
400. The call ACL applies as for reads: 401, 403, and 404 for an artifact id
under another call. There is no restore or delete endpoint.

**Optimistic concurrency.** `PUT` must name the version it replaces. If another
write got there first, nothing is stored and the response is
`409 {"error":"version conflict","current_version":N,"base_version":B}`. The
writer re-reads and retries.

**Bounds.** `html` at most 256 KiB (413). Request body at most 1 MiB (413).
`title` is trimmed of surrounding whitespace. A nonempty title must be at most
120 characters with no control characters (400). On create, a missing, empty
or whitespace-only title is rejected (400 `title is required`). On update,
`title` is optional: leaving it out, or sending it empty or whitespace-only,
keeps the current title. NUL bytes,
unknown JSON fields, a missing `html` and a missing or non-positive
`base_version` are rejected (400). At most 10 artifacts per call (409). The
database repeats the size and title bounds as constraints. The number of
versions per artifact is not capped, so storage for a call grows with every
update (each at most 256 KiB); see the POC notes.

**Stream.** The stream follows the call stream's format. It starts with
`: connected`, then sends `id: <version>` / `event: artifact` /
`data: <snapshot>` with the latest snapshot, unless the client's
`Last-Event-ID` (preferred) or `from` already equals the current version. After
that it sends each newer version, comment keepalives, and finally
`event: end` / `data: {}` when the call is no longer scheduled or live.
Intermediate versions may be skipped: the stream carries the latest state, not
a log. Access (key or session still valid, and still a participant or share
recipient) is rechecked before every snapshot after the first: once access is
gone, the stream closes instead of sending a newer version. An idle stream
also rechecks access every 3 seconds, so it closes within about 3 seconds of
revocation even if nothing changes. A write wakes that server's open streams
in process. The stored version is also rechecked on the same 3-second tick,
so a stream that missed its wake-up still gets the latest version within that
interval.

**Deployment scope.** Only a single app server is supported. The stored-version
recheck recovers missed in-process wake-ups; it does not add support for
running several app servers. Concurrency across instances is out of scope.

**Rendering rule.** The HTML is untrusted. The API only returns it as a JSON
string, with `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`.
It is never served as an HTML document on the app origin. The call view renders it
in `<iframe sandbox="" srcdoc=…>`, which gives it an opaque origin with no
scripts, forms, popups, top navigation or access to the app page. A leading
`Content-Security-Policy` meta tag restricts the page's own subresource loads
to inline styles and `data:` images, fonts and media:
`default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:; media-src data:; form-action 'none'; base-uri 'none'`.
So artifacts are static HTML and CSS, with images and fonts only as `data:` URIs.
The policy does not govern navigation. A link a viewer clicks (default
`target`) navigates the sandboxed frame and requests the link's URL; the app
page's privileges stay blocked.

## Relationship to the spec

- **Reused:** API keys and sessions (`auth`), the participant-or-share call ACL
  for reads, the share rule (owner with read access, or participant) for writes,
  the SSE conventions of `/api/calls/{id}/stream`, and Postgres via
  migrations and sqlc (`store`).
- **Added:** migration `004_call_artifacts.sql` and the endpoints above (`api`),
  plus a Pages section in the call view whose viewer links select a page in
  place (`web`).
- **Unchanged:** no Zoom API, scope, webhook or RTMS behavior changes. Presenting
  is an ordinary Zoom screen or window share done by a person.
- **Deferred:** deleting artifacts or versions, restoring an older version,
  a cap or retention policy for versions, rate limits, interactive (scripted)
  artifacts, partial updates, and publishing artifacts after a call ends.
