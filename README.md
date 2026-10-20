# zoom-connect

A SvelteKit application served by Go. VitePlus and `sv` created the frontend;
skgo added the Go server and bindings. Node is a build-time and development
dependency, not a production runtime dependency.

## Prerequisites

- Go 1.27.2 or newer
- Node 22.17 or newer
- VitePlus and `just`

The generator installed the frontend dependencies and made an initial frontend
build, and installed Chromium for the Vitest component tests. None of that is
committed. In a fresh clone `just dev` installs the dependencies and makes that
build once, when it finds them missing; the component tests additionally need
`cd web && vp exec playwright install chromium`.

The project includes skgo's Kit 3.0.0 prerender queue correction in its
frontend configuration. `just dev` and the native VitePlus install apply it during a
fresh install. To set it up in an existing application, run `go tool skgo
kit-patch --web web --apply` from the Go app root, then run
`vp install --no-frozen-lockfile` from `web/`, followed by
`go tool skgo kit-patch --web web --check` from the app root. The check confirms
the correction is present in the installed Kit package.

## Development

Start the local database, then start the Go and SvelteKit app:

```sh
docker compose up -d postgres
just dev
```

Open the address printed by `just dev`. On the sign-in screen, choose Dana Kim
(`dana@acme.co`) and click **Continue with Google (demo)**. This POC sign-in is
simulated and needs no Google credentials. The Account page can switch between
the demo users.

Svelte edits update through VitePlus. Compatible Go edits update in the running
development session; changes that require a rebuild are handled by `skgo dev`.

Storybook and Vitest are ready to run:

```sh
just storybook
just test
```

## Production

```sh
just build
./bin/zoom-connect
```

The resulting binary embeds the frontend build and serves it without Node.
For a full local app run, start Postgres first with `docker compose up -d
postgres`. From `web/`, `vp run preview` runs `just serve` and previews that
same Go binary.

## Meeting event storage

Postgres stores one row per transcript, chat, or lifecycle event in
`call_events`, keyed by `(call_id, seq)`. Events commit before live SSE
publication; replay and downloads read directly from Postgres. There is no
whole-call document to rewrite and no event-history cache to reconstruct.
The current live fanout supports one application instance.

On first startup after upgrading, existing `calls.event_path` JSONL archives
are imported transactionally per call. The importer preserves sequence,
speaker, timestamps, and payloads, then clears the legacy path in Postgres.
It leaves the original files intact as backups. An incomplete or conflicting
archive fails startup with a diagnostic; correct it before retrying. Stop the
old application before upgrading so it cannot append to a file during import.
After import, the application no longer needs those files or `ARCHIVE_DIR`.
The standalone RTMS recorder still saves raw protocol diagnostics separately.

After schema/query edits, run `sqlc generate`. `store.Open` applies the
idempotent SQL files under `internal/store/migrations` in filename order.

## Single-account demo login

For a hosted demo, set `AUTH_MODE=password`, `DEMO_LOGIN_EMAIL`,
`DEMO_LOGIN_NAME`, and a generated `DEMO_LOGIN_PASSWORD` of at least 20
characters. Password mode creates that account, disables simulated login and
persona enumeration, and skips fictional demo data. `PUBLIC_URL=https://...`
requires password mode and makes session cookies Secure behind the proxy.
Google Workspace OAuth is still pending. Keep all credentials in the
`zoom-connect` Doppler project's `prd` config.

Hosted Postgres uses a verified TLS URL. Set `DATABASE_CA_CERT` to its PEM
root certificate when the provider uses its own CA. The application adds that
CA to its database TLS trust pool without changing system-wide trust.

## Add a scheduled call

Choose **Add scheduled call** on the Calls page and paste an existing Zoom
invitation or `/j/…` meeting link. Title and expected start time are optional.
This registers the **next live session on that link**, not a recurring schedule.
The invitation and embedded passcode are discarded after extracting the meeting
number. The creator can share or cancel the pending call without being marked
as a meeting participant.

Clients connect only to Zoom Connect. The call page provides a stable URL:

```sh
curl -N -H "Authorization: Bearer $ZC_KEY" \
  https://YOUR_APP/api/calls/CALL_ID/stream
```

The authenticated SSE connection stays open while the call is scheduled, then
receives lifecycle, transcript, chat and screen events as the app receives them.
Expected start time is advisory; a late meeting keeps waiting. Reconnect with
`Last-Event-ID` to resume after the last event received. That header takes
precedence over an initial `?from=` cursor. Cancellation emits `call.cancelled`
and ends the stream. Access and API-key revocations also disconnect clients.

Applications may register calls with `POST /api/calls` (or `/api/v1/calls`):

```json
{
  "invitation": "https://us02web.zoom.us/j/12345678901?pwd=…",
  "topic": "Planning",
  "scheduled_at": "2026-10-20T15:00:00Z"
}
```

The returned `id` works immediately with metadata, sharing and streaming APIs.
A pending call has `status: "scheduled"` and `started: null`. The optional
`scheduled_at` is an RFC3339 timestamp. The creator can cancel it with
`POST /api/calls/{id}/cancel`. Only one pending or live call may use a meeting
number at a time. The recorder durably attaches the runtime meeting UUID and
stream IDs to the registered call; duplicate notifications and recorder restarts
do not create a second call. Concurrent streams remain separate, and the call
ends when its last stream stops.

Saving a link prepares the app's downstream subscription. Upstream capture still
requires Zoom Connect's RTMS app user to start streaming or join with auto-start
enabled, subject to Zoom host settings. This does not schedule a participant bot
or provide unattended joining. Local integration proof uses simulated incoming
Zoom data; live opening-transcript capture for scheduled calls still needs a
real meeting test.
