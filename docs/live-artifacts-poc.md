# Live call artifacts — proof of concept

Branch `claude/opus-live-artifacts-poc-20261020`. The contract and how it
relates to `nlspec.md` are in
[live-artifacts-contract.md](live-artifacts-contract.md).

## Concepts

- **Artifact:** an HTML page that belongs to one call. An agent creates it with
  its existing API key and replaces it as the meeting goes on. Each write is a
  new numbered version. The row points at the current version, the newest 25
  snapshots are kept, and writes are compare-and-swap on `base_version`.
- **Viewer:** `/calls/{id}/artifacts/{artifactId}`, opened from the new
  **Artifacts** card on the call page. It needs a signed-in session that can
  see the call. There is no public or bearer link. The presenter opens it,
  presses **Present** (fullscreen), and shares that window in Zoom.
  Participants see pixels through Zoom's own screen share and need no account.
- **Live delivery:** the viewer loads the latest snapshot, then follows
  `…/stream`, an SSE stream of whole snapshots whose event id is the version.
  The browser resumes with `Last-Event-ID`. A reconnect therefore gets the
  newest version even if it missed several. Writes wake viewers on the same
  instance at once. Every stream also rechecks access and the stored version
  every 3 seconds, which covers other instances and revoked access.
- **Isolation:** agent HTML is untrusted. It is only ever returned as a JSON
  string and is shown in `<iframe sandbox="" srcdoc>`, which gives it an opaque
  origin with no scripts, forms, popups or navigation of the app. A CSP meta
  tag inside the frame blocks every network load. Artifacts are static HTML
  and CSS; images and fonts must be `data:` URIs.

## Agent API (curl)

These are the exact commands that were run against the loopback build. Every
path also works without `/v1`.

```sh
export ZC=http://127.0.0.1:8080   # app origin
export ZC_KEY=zc_…                 # an API key from /keys (acts as its owner)
export CALL=01M5…                  # a call the key's owner participates in

# Create (201, Location header, snapshot with version 1 and viewer_url)
curl -sS -H "Authorization: Bearer $ZC_KEY" -H 'Content-Type: application/json' \
  -d '{"title":"Live agenda","html":"<h1>Agenda</h1><ol><li>Intro</li></ol>"}' \
  "$ZC/api/v1/calls/$CALL/artifacts"
export ART=01M5…                   # "id" from the response

# Update: name the version you are replacing (200, version 2)
curl -sS -X PUT -H "Authorization: Bearer $ZC_KEY" -H 'Content-Type: application/json' \
  -d '{"base_version":1,"html":"<h1>Agenda</h1><ol><li>Intro ✓</li><li>Demo</li></ol>"}' \
  "$ZC/api/v1/calls/$CALL/artifacts/$ART"

# A stale writer is refused and told the current version
curl -sS -X PUT -H "Authorization: Bearer $ZC_KEY" -H 'Content-Type: application/json' \
  -d '{"base_version":1,"html":"<p>late</p>"}' "$ZC/api/v1/calls/$CALL/artifacts/$ART"
# 409 {"base_version":1,"current_version":2,"error":"version conflict"}

# Latest, list, a retained older version
curl -sS -H "Authorization: Bearer $ZC_KEY" "$ZC/api/v1/calls/$CALL/artifacts/$ART"
curl -sS -H "Authorization: Bearer $ZC_KEY" "$ZC/api/v1/calls/$CALL/artifacts"
curl -sS -H "Authorization: Bearer $ZC_KEY" "$ZC/api/v1/calls/$CALL/artifacts/$ART/versions/1"

# Follow live; resume after a gap (sends the latest only if newer than 1)
curl -N -H "Authorization: Bearer $ZC_KEY" "$ZC/api/v1/calls/$CALL/artifacts/$ART/stream"
curl -N -H "Authorization: Bearer $ZC_KEY" -H 'Last-Event-ID: 1' \
  "$ZC/api/v1/calls/$CALL/artifacts/$ART/stream"
# : connected
#
# id: 2
# event: artifact
# data: {"id":"01M5…","version":2,…,"html":"<h1>Agenda</h1>…"}
```

Without a key the response is `401 {"error":"unauthorized"}`.

## Demo steps

1. Start a private Postgres and build: `ORIGIN=http://127.0.0.1:8080 just build`.
   Then run `DATABASE_URL=postgres://…/scratch ./bin/zoom-connect`.
2. Sign in as a participant (for example `dana@acme.co` with demo login). Open a
   live or scheduled call, then create a key under **Manage keys**.
3. Run the create command above. The call page's **Artifacts** card lists it
   within 10 seconds; open it in a new window. The curl output's `viewer_url`
   works too.
4. Press **Present** and share that window in Zoom.
5. Run the update commands. The viewer shows each new version without a reload,
   and the badge shows the version number. When the call ends the viewer shows
   "Final version" and updates return 409.

## Verification results (October 20, 2026)

All runs were local: Go 1.27.2 with `GOMAXPROCS=2`, a private throwaway
Postgres container on loopback, demo login and fake keys. No Zoom meeting,
recorder or production credential was involved.

**Go integration tests** (`internal/httpapi/artifacts_integration_test.go`)
call the real HTTP handlers against real Postgres, with `-race`:

- `TestArtifactScopesAndWrongCallIsolation`
  - Anonymous requests and unknown keys get 401; revoked keys get 401.
  - An outsider's key gets 403 on create, list, read, stream and versions.
  - A share recipient can read but gets 403 on create and update.
  - A newly added participant's key can update.
  - The same artifact id under a different call gets 404 on every
    read/stream/version/update path.
  - HTML only appears JSON-escaped, with `nosniff` and `no-store`.
- `TestArtifactVersionConflictsBoundsAndReadOnlyAfterEnd`
  - A stale `base_version` gets 409 with `current_version` and leaves no stray
    snapshot.
  - Five concurrent writers on the same base give exactly 1 success and 4
    conflicts.
  - Validation returns 400 or 413 as documented.
  - Only the newest 25 versions are kept; an 11th artifact gets 409.
  - After the call stops, updates get 409 and reads still return 200.
- `TestArtifactStreamLatestReconnectRevocationAndRestart`
  - Live delivery of v2.
  - `Last-Event-ID: 2` after v3 and v4 first gets v4; `?from=4` gets no
    duplicate and then v5.
  - Removing the share closes the stream; revoking the key closes its stream.
  - A second server instance on the same database delivers a write made
    through the first (by polling).
  - Stopping the call sends `event: end`.

**Headless browser proof** (`scripts/artifact-browser-proof.mjs`) uses
Playwright against the built binary. It runs two instances on one database,
an agent that only holds a Bearer key, a loopback reverse proxy that can fail
stream requests, and a loopback beacon server that records any request it
receives. The final run passed 24 of 24 checks in each of Chromium, Firefox
and WebKit:

- **Two updates render with no manual reload.** v2 and v3 appeared in the
  iframe 10–24 ms after each PUT returned. There were zero main-frame
  navigations, and a `window` marker set after load was still present.
- **Reconnect gets the latest.** The viewer's server was killed and the stream
  showed "Reconnecting…". The agent wrote v4 and v5 through the other
  instance. After the server restarted, v5 appeared 3.3 s later in Chromium and
  WebKit and 5.4 s later in Firefox, set by each browser's EventSource retry
  delay. v6 then arrived live, still with no reload.
- **Proxy errors.** A viewer behind the proxy had its stream dropped. Every
  API request, including the stream, then got 502, which makes EventSource give
  up. The page kept showing v6, and two stream 502s were observed. Once the API
  was served again the viewer reopened the stream itself, and v7 arrived live
  14–27 ms after its PUT.
  - **Mutation check.** A Chromium run with the viewer's earlier reconnect code
    failed both checks. A 502 on the access recheck replaced the presented
    page with an error, and the stream never came back.
  - **Why a real proxy.** An earlier Firefox attempt used Playwright request
    interception (`page.route`) instead of the proxy. In that run the reopened
    stream never delivered events, which looks like an interception artifact
    rather than viewer behavior.
- **Conflicts.** A stale `base_version` got 409 with `current_version: 3`.
- **Outsiders.** A signed-in user without access sees the no-access state and
  no iframe. Their API read gets 403.
- **Sandbox.** A hostile artifact tried to:
  - run an inline script and an `onerror` and `svg onload` handler;
  - set `parent.document.title`;
  - call `fetch` to create an API key with the viewer's cookies;
  - call `alert`;
  - load an image, stylesheet, `@import`, font, background, nested iframe,
    object and video;
  - change the base URL, use meta refresh, submit a `target=_top` form, and
    have `target=_top` and `target=_blank` links clicked.

  In the viewer there were zero beacon requests and the script did not run. The
  page title and URL were unchanged, no dialog or popup appeared, and the set
  of API keys was unchanged.
- **Controls show the payload is live and each layer matters.** The same
  payload in a non-sandboxed iframe on the app origin ran the script and beaconed
  every vector. It created an API key using the viewer's cookie and sent it out,
  which is the app-origin privilege the sandbox removes. With `sandbox=""` but
  no policy, scripts were blocked but images, CSS, fonts, media, objects and
  nested frames still loaded from the beacon host. The policy closes that gap.
  The last control is the viewer's exact rendered `srcdoc` without the
  `sandbox` attribute. The policy still stopped scripts and every load, but
  the frame's origin was the app's (`http://127.0.0.1:57190`): any script that
  got past the policy would act as the signed-in user. Its meta refresh
  navigated the frame, the `_blank` link opened a popup, and the `_top` link
  navigated the app page away, all to the beacon host. The sandbox is what
  prevents those.

**Other checks:**

- With the private `TEST_DATABASE_URL`, `go test -p 1 -count=1 ./...` passes:
  `internal/httpapi` in 18.9 s and `cmd/rtmsrec` in 0.2 s.
- The three artifact tests pass with `-race -v` in 0.22 s, 0.72 s and 0.62 s,
  with none skipped.
- `go vet ./...` is clean.
- `vp run check` reports 0 errors and 0 warnings.
- `vp build` and the binary build succeed.

Rerun the browser proof with:

```sh
cd web && ORIGIN=http://127.0.0.1:57190 vp build && cd ..
go build -o /tmp/zc/zoom-connect ./cmd
ZC_BIN=/tmp/zc/zoom-connect DATABASE_URL='postgres://…/scratch_db' \
  node scripts/artifact-browser-proof.mjs chromium   # or firefox, webkit
```

The proof creates calls, keys and artifacts in that database, so point it only
at a scratch database.

## Limitations and unproven claims

- **No native Zoom integration.** Presenting is an ordinary Zoom window share
  done by a person. Nothing was tested in a real Zoom meeting, and no Zoom
  scope, app or credential is needed or was created.
- **Static artifacts only.** No JavaScript runs, so interactive charts are not
  possible. External images and fonts are blocked by design, so use `data:`
  URIs.
- **Whole-page updates.** Each version replaces the iframe document, so scroll
  position resets on every update. There is no diff or patch format.
- **Delivery timing.** Wake-ups are in-process. Other instances, and a viewer
  whose wake-up was missed, rely on the 3-second poll. Delivery across instances
  was proven against one shared database, not on Fly. Revoked access closes a
  stream within about 3 seconds; the viewer then rechecks and clears the page.
- **Missing operations.** There is no delete or rename-only endpoint (every
  update must include `html`) and no per-key rate limit. The size and count
  bounds cap storage at about 10 × 25 × 256 KiB per call. Writes are refused
  once a call ends.
- **What the browser runs cover.** They prove behavior in the Playwright builds
  of Chromium, Firefox and WebKit on macOS, in headless mode, with demo login.
  Password login mode, Safari proper, mobile browsers and fullscreen
  presentation were not exercised. **Present** relies on the Fullscreen API
  and was not tested headless.
- **Proof side effects.** The browser proof's positive control creates a real
  API key named `stolen-control-unsandboxed` in the scratch database. That is
  intended: it shows what the sandbox prevents.
