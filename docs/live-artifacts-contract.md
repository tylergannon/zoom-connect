# Live call artifacts — prototype contract amendment (October 20, 2026)

This is a prototype amendment kept separate from `nlspec.md`; it does not change
any durable section there. It adds one capability on top of the existing
agent-key, call ACL and live-stream design: an agent can publish an HTML page
for a call and keep replacing it, and a signed-in person can open that page and
share their window in Zoom so other participants watch it change.

## Contract

**Resource.** An artifact belongs to exactly one call. It has an id, a title
and a monotonically increasing `version`. Each create or update stores an
immutable snapshot `{version, title, html, author, key name, time}`. The
newest 25 snapshots are kept; older ones are deleted.

**Credentials.** The existing ones only: `Authorization: Bearer <API key>` (the
key acts as its owner) or the browser session cookie. No new credential type,
scope, public link or bearer URL exists. Revoked keys and expired sessions get
401, and live streams opened with them close at the next access check.

**Who may do what.**

| Caller | List / read / stream / old versions | Create / update |
| --- | --- | --- |
| Call participant (and their keys) | yes | yes, while the call is scheduled or live |
| Coworker the call was shared with (and their keys) | yes | no (403) |
| Anyone else | 403 | 403 |
| Unknown call, or artifact id under another call | 404 | 404 |

After a call ends (stopped, cancelled or interrupted) its artifacts are
read-only: updates return 409 and reads keep working for everyone with access.

**Endpoints.** Every path is also served under `/api/v1/...`.

| Method and path | Request | Success |
| --- | --- | --- |
| `GET /api/calls/{id}/artifacts` | | 200, array of artifact metadata (no `html`, with `bytes`) |
| `POST /api/calls/{id}/artifacts` | `{"title","html"}` | 201, snapshot, `Location` header |
| `GET /api/calls/{id}/artifacts/{artifactId}` | | 200, latest snapshot |
| `PUT /api/calls/{id}/artifacts/{artifactId}` | `{"base_version","html","title"?}` | 200, new snapshot |
| `GET /api/calls/{id}/artifacts/{artifactId}/versions/{n}` | | 200, retained snapshot |
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

**Optimistic concurrency.** `PUT` must name the version it replaces. If another
write got there first, nothing is stored and the response is
`409 {"error":"version conflict","current_version":N,"base_version":B}`. The
writer re-reads and retries. Leaving out `title` keeps the current title.

**Bounds.** `html` at most 256 KiB (413). Request body at most 1 MiB (413).
`title` is 1–120 characters with no control characters (400). NUL bytes,
unknown JSON fields, a missing `html` and a missing or non-positive
`base_version` are rejected (400). At most 10 artifacts per call (409). The
database repeats the size and title bounds as constraints.

**Stream.** The stream follows the call stream's format. It starts with
`: connected`, then sends `id: <version>` / `event: artifact` /
`data: <snapshot>` with the latest snapshot, unless the client's
`Last-Event-ID` (preferred) or `from` already equals the current version. After
that it sends each newer version, comment keepalives, and finally
`event: end` / `data: {}` when the call is no longer scheduled or live.
Intermediate versions may be skipped: the stream carries the latest state, not
a log. Access (key or session still valid, and still a participant or share
recipient) and the stored version are rechecked every 3 seconds. A server that
missed an in-process wake-up, or runs as a separate instance, still delivers
within that interval.

**Rendering rule.** The HTML is untrusted. The API only returns it as a JSON
string, with `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`.
It is never served as an HTML document on the app origin. The viewer renders it
in `<iframe sandbox="" srcdoc=…>`, which gives it an opaque origin with no
scripts, forms, popups or top navigation. A leading
`Content-Security-Policy` meta tag then blocks every network load:
`default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:; media-src data:; form-action 'none'; base-uri 'none'`.
So artifacts are static HTML and CSS, with images and fonts only as `data:` URIs.

## Relationship to the spec

- **Reused:** API keys and sessions (`auth`), the participant-or-share call ACL,
  the SSE conventions of `/api/calls/{id}/stream`, and Postgres via
  migrations and sqlc (`store`).
- **Added:** migration `004_call_artifacts.sql` and the endpoints above (`api`),
  plus one viewer route and a call-page card (`web`).
- **Unchanged:** no Zoom API, scope, webhook or RTMS behavior changes. Presenting
  is an ordinary Zoom screen or window share done by a person.
- **Deferred:** deleting artifacts, rate limits, interactive (scripted)
  artifacts, partial updates, and publishing artifacts after a call ends.
