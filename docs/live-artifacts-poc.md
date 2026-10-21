# Live call artifacts — proof of concept

Built on branch `claude/opus-live-artifacts-poc-20261020` and integrated with
the redesigned app (PR #4) on `claude/live-pages-ui-integration-20261020`. The
contract is in [live-artifacts-contract.md](live-artifacts-contract.md); the
October 20 amendment in `nlspec.md` states its intent.

## Concepts

- **Artifact:** an HTML page that belongs to one call. An agent creates it with
  its existing API key and replaces it as the meeting goes on. Each write is a
  new numbered version of the same page: the id is stable, the row points at
  the current version, every snapshot is kept in Postgres with the call, and
  writes are compare-and-swap on `base_version`.
- **Pages in the call view:** the call page has a full-width **Pages**
  section under the call header: a list of the call's pages above a pane that
  shows the selected one in a sandboxed frame. App navigation, the call
  header, transcript, chat, search and the call's side panel stay on
  screen. It needs a signed-in session that can see the call; there is no
  public or bearer link. `viewer_url` (`/calls/{id}/artifacts/{artifactId}`)
  opens the same call view with that page selected, and choosing a page in the
  list moves to that URL in place, without a reload or a new tab.
- **Present:** **Present** makes the app-owned pane (title, version, live
  status and an **Exit** button) fill the screen, and the presenter shares that
  window in Zoom. Participants see pixels through Zoom's own screen share and
  need no account. See "Design" below.
- **Live delivery:** the viewer loads the latest snapshot, then follows
  `…/stream`, an SSE stream of whole snapshots whose event id is the version.
  The browser resumes with `Last-Event-ID`. A reconnect therefore gets the
  newest version even if it missed several. Writes wake viewers on the same
  instance at once. A stream rechecks access before every snapshot after the
  first, so a revoked key or share never receives a newer version. It also
  rechecks access and the stored version every 3 seconds, which covers other
  instances and closes idle streams whose access was revoked.
- **Isolation:** agent HTML is untrusted. It is only ever returned as a JSON
  string and is shown in `<iframe sandbox="" srcdoc>`, which gives it an opaque
  origin with no scripts, forms, popups or navigation of the app. A CSP meta
  tag inside the frame blocks the page's own subresource loads (images,
  styles, fonts, media, nested frames, objects) except inline styles and
  `data:` URIs. Artifacts are static HTML and CSS; images and fonts must be
  `data:` URIs. Clicked links are the exception; see the limitations.

## Design

**Why the page lives in the call view.** A page is part of a call, so it is
shown with the call's context instead of as a separate whole-page viewer. The
redesigned call view (`CallWorkspace`) is rendered by a SvelteKit layout
(`web/src/routes/calls/[id]/+layout.svelte`), so moving between `/calls/{id}`
and `/calls/{id}/artifacts/{artifactId}` keeps it mounted: its event stream,
transcript tab, search text and scroll position survive selecting a page. The
layout keys the view on the call id. Moving to another call starts a fresh
view, and the old one closes its call and page streams and timers as it
unmounts; a load still in flight at that moment does not open a stream. Page links use
`data-sveltekit-reset="false"` (SvelteKit 3's replacement for `keepfocus` and
`noscroll`), and a clicked row takes focus in every browser.

**Layout.** The Pages section spans the call view's full width above the
transcript and side panel, with the list of pages in up to three columns
above the pane. The page frame therefore gets the whole width (about 1000 px
at a 1360 px window, about 750 px with History open beside it). At phone
widths the list, frame and history stack, and the list scrolls within a short
box. A live or scheduled call with no pages shows a one-line "No pages yet"
note that links to the API guide's **Publish a live page** section
(`/agent-api#pages`, with create, update and history examples). It switches to
the Pages section once an agent publishes a page. An ended call with no pages
shows nothing extra. The section uses the redesign's shadcn-svelte
components and theme tokens, so it follows light and dark mode.

**Sorting the list.** The list shows title, version, when and by whom each page
was last updated.
- **Recently updated** is the default, because during a call the page an agent
  just changed is usually the one people want.
- **Title** (A–Z) helps find a known page.
- **Newest** (by creation) shows pages in the order they appeared.
- Ties fall back to the id, so the order is stable, and a title filter appears
  once there are two or more pages.

The list refreshes every 10 seconds while the call is open, and whenever the
selected page gets a new version. With no page in the URL, the most recently
updated page is chosen once and stays selected even if another page later
moves to the top.

**Present and Exit.**
- **Owner of the fullscreen.** Present calls `requestFullscreen` on the
  app-owned pane, never on the untrusted iframe. The pane's `data-presenting`
  attribute is `fullscreen` when the browser made the pane the fullscreen
  element.
- **Fallback.** If fullscreen is unavailable or refused, the same pane covers
  the window with fixed positioning instead, and `data-presenting` is
  `window`.
- **Same frame and stream.** Both modes keep the same iframe and the same
  stream; no new stream opens.
- **Leaving.** Exit, Escape, or leaving fullscreen through the browser all end
  presenting. The body scroll lock is removed, the scroll position is
  restored, and focus returns to the Present button.
- **Late grant.** A fullscreen grant that arrives after Exit or after leaving
  the view is handed straight back.

**History.**
- **Opening an older version.** **History** lists versions newest first. Choosing one shows a
  read-only preview labelled "Older version from … · latest is vN". Live
  updates keep arriving (the label's latest number moves) but never replace
  the preview, until **Return to live**. There is no restore or delete.
- **One unbroken run.** The list is kept as one unbroken run of stored versions
  with the server's `next_before` cursor for the next older page. A refresh
  that does not overlap the loaded run (more than one page of updates arrived
  during a disconnect) replaces the run, and **Load older versions** walks
  down from there, so no stored version becomes unreachable.
- **Late replies.** A listing that was read before an update arrived is read
  again, so the latest version is always listed.

**Retention.** Versions are no longer pruned: the history of a call's pages is
kept with the call. An earlier build of this branch kept only the newest 25
versions per page and deleted the rest. Those versions are gone and are not
recovered or reconstructed. A page from that build shows "Versions 1–K are no
longer stored" at the end of its history. The 10-pages-per-call and 256 KiB
bounds are unchanged.

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

# Latest, list, version history (metadata only, newest first), an older version
curl -sS -H "Authorization: Bearer $ZC_KEY" "$ZC/api/v1/calls/$CALL/artifacts/$ART"
curl -sS -H "Authorization: Bearer $ZC_KEY" "$ZC/api/v1/calls/$CALL/artifacts"
curl -sS -H "Authorization: Bearer $ZC_KEY" "$ZC/api/v1/calls/$CALL/artifacts/$ART/versions?limit=20"
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
   live or scheduled call; it shows "No pages yet" with a link to the API
   guide. Create a key under **API keys**.
3. Run the create command above (the guide's **Publish a live page** section
   has the same commands with this server's address). The call page's **Pages** section lists it
   within 10 seconds and shows it below the list. The curl output's
   `viewer_url` opens the same call view with that page selected.
4. Run the update commands. The pane shows each new version without a reload,
   and the badge shows the version number. Create a second page and try the
   **Recently updated**, **Title** and **Newest** sorts and the filter.
5. Press **History**, choose an older version, run another update (the preview
   stays and "latest is vN" moves), then press **Return to live**.
6. Press **Present** and share that window in Zoom. **Exit** or Escape returns
   to the call view as it was. When the call ends the pane shows
   "Final version" and updates return 409.

## Integration results (October 20, 2026)

These runs are on the merge of the POC into the redesign (PR #4), using the
build that was committed. All runs were local: Go 1.27.2 with
`GOMAXPROCS=2`, a private throwaway Postgres container on loopback, demo login
and fake keys. No Zoom meeting, recorder or production credential was involved.
Logs are in `/tmp/zc-live-pages-integration/`.

- **Go.** `go vet ./...` is clean. With the private `TEST_DATABASE_URL`,
  `go test -p 1 -count=1 -v ./...` passes 28 top-level tests and 12 subtests,
  none failed or skipped (`internal/httpapi` 19.3 s). The artifact tests pass
  with `-race`. `go generate` and `sqlc generate` leave no diff.
- **Frontend.** `vp run check` reports 0 errors and 0 warnings in 1217 files.
  `vp run test` passes 8 of 8 tests. `vp lint` is clean on the changed
  paths. `vp build` and the binary build succeed.
- **Browser proofs.** These ran in Playwright's headless Chromium, Firefox and
  WebKit:
  - the embedded call view passes 52 of 52 checks in each engine;
  - the security proof passes 24 of 24;
  - the route race passes 5 of 5.

The proofs now use the redesign's markup:
- the `Main` navigation landmark and the call's `h1`;
- the call content tabs, and the Chat tab's own **Search chat** box (inactive
  tabs are unmounted);
- the redesigned "You don’t have access to this call" state;
- the title format `{topic} (live) · Zoom Connect`.

The 39 earlier embedded checks are unchanged in what they require. The 13 new
checks are:

- **Moving to another call.** An in-app link to a second call starts a fresh
  call view in the same document: the second call's heading, the Transcript
  tab, empty search, and its own page list. Every event stream of the first
  call is closed. In the next 11 seconds there is no request for the first
  call (its list refresh runs every 10 s). Going back shows the first call
  with its page selected.
- **Empty states.**
  - An open call with no pages shows "No pages yet" with a link to the
    guide.
  - A page an agent then publishes appears without a reload.
  - `/agent-api#pages` shows the create, update and history examples.
  - An ended call with no pages shows its transcript and no Pages UI.
- **Layout.** These checks ran at 1360×900 and 390×844, each in light and dark
  mode, with History open:
  - the theme class matches the chosen mode;
  - nothing overflows horizontally;
  - the frame is 756 px wide on desktop and 358 px on the phone width.

  Present at phone width covers the window, with Exit visible. Firefox's
  headless fullscreen resizes to its 1366×768 screen, so the check compares
  the pane with the window size measured while presenting.

Integration bugs the proofs caught and that are now fixed:
- **Frame width.** With the page list beside the pane and History open, the
  frame was only 484 px wide at 1360 px. The list now sits above the pane.
- **Focus after Exit in WebKit.** WebKit does not focus a clicked button, so
  Exit put focus back on the previously focused page row, not on Present.
  Exit now always focuses Present, which is the only way into presenting.

Screenshots are in `/tmp/zc-live-pages-integration/shots/`, one per engine:
- `layout-{desktop,mobile}-{light,dark}-<engine>.png` (the Pages section)
  and `…-page-<engine>.png` (the full page);
- `layout-mobile-present-<engine>.png`;
- `embedded-empty-<engine>.png`;
- the earlier `embedded-{normal,present,history,restored,present-fallback,history-reconnect}-<engine>.png`
  and `viewer-<engine>.png`.

The full-page captures show the sticky top bar drawn part-way down the page.
This comes from how Playwright stitches a full-page capture, not from the
layout.

## Verification results on the POC branch (October 20, 2026)

These are the POC branch's results from before the redesign merge. They are
kept as a record. Runs on the merged branch are listed above.

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
  - 30 more updates (to v33) keep one page with the same id; versions 1, 2, 3
    and 33 are all still readable; an 11th artifact gets 409.
  - After the call stops, updates get 409 and reads still return 200.
- `TestArtifactStreamLatestReconnectRevocationAndRestart`
  - Live delivery of v2.
  - `Last-Event-ID: 2` after v3 and v4 first gets v4; `?from=4` gets no
    duplicate and then v5.
  - Removing the share closes the stream; revoking the key closes its stream.
  - A second server instance on the same database delivers a write made
    through the first (by polling).
  - Stopping the call sends `event: end`.
- `TestArtifactHistoryPagesAccessAndRestart`
  - 60 versions list newest first in pages of 50 (`next_before` 11, then
    `null`); `before`/`limit` page exactly; bad values get 400.
  - Entries carry title, bytes, author and key name, and no `html`.
  - 401 anonymous, 403 outsider (200 once shared), 404 under another call.
  - After a server restart a new instance returns the latest v60, all 60
    history entries and version 1's HTML.
- `TestArtifactStreamRechecksAccessBeforeWakeSnapshot`
  - The access tick is an hour, so only the check at wake-up can act. A key
    is revoked, or a share removed, after its stream got v1, and then v2 is
    written. The stream closes without sending v2.
  - Before the fix, both subtests received v2 and failed.

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

  In the viewer none of these reached the beacon host and the script did not run. The
  page title and URL were unchanged, no dialog or popup appeared, and the set
  of API keys was unchanged.
- **Controls show the payload is live and each layer matters.** The same
  payload in a non-sandboxed iframe on the app origin ran the script and beaconed
  every vector. It created an API key using the viewer's cookie and sent it out,
  which is the app-origin privilege the sandbox removes. With `sandbox=""` but
  no policy, scripts were blocked but images, CSS, fonts, media, objects and
  nested frames still loaded from the beacon host. The policy blocks those loads.
  The last control is the viewer's exact rendered `srcdoc` without the
  `sandbox` attribute. The policy still stopped scripts and those loads, but
  the frame's origin was the app's (`http://127.0.0.1:57190`): any script that
  got past the policy would act as the signed-in user. Its meta refresh
  navigated the frame, the `_blank` link opened a popup, and the `_top` link
  navigated the app page away, all to the beacon host. The sandbox is what
  prevents those.

**Route-change regression** (`scripts/artifact-route-race-proof.mjs`) checks
that a reply meant for the previous artifact cannot replace the one on screen:

- Artifact A is at v5 and artifact B at v1, in the same call.
- A loopback proxy drops A's stream and holds the viewer's recheck read for A.
- An ordinary same-origin link to B is clicked, so the app's router navigates
  without a document reload.
- Once B shows, the held read for A is released.

With the earlier viewer, B's URL then showed A's HTML, "Version 5" and A's
title in Chromium, Firefox and WebKit, failing 3 of 5 checks. The viewer now
ignores snapshots whose `call_id` or `id` differs from the route, and replies
or stream events from before the route changed. It passes 5 of 5 checks in all
three engines, and the main proof still passes 24 of 24 in each.

After the pages moved into the call view, both proofs were adapted. The
outsider now sees the call's own "You weren't in this call" state, with no
page list and no frame. The title check compares the call view's title before
and after the hostile clicks. The route race selects B from the real page
list. Each proof closes its finished viewer pages before opening the next
ones: every call view holds a few event streams, and over plain HTTP/1.1 the
browser's six connections per host otherwise run out. On the final build the
security proof passes 24 of 24 and the route race 5 of 5 in each engine.

**Embedded call view** (`scripts/artifact-embedded-proof.mjs`, 39 checks)
passes 39 of 39 in Chromium, Firefox and WebKit on the final build:

- **Layout.** Sidebar navigation, call header, transcript search and the page
  pane are visible together. With no page in the URL the most recently updated
  page shows at its latest version.
- **Sorting.** Each sort order and the title filter.
- **Selection.**
  - Choosing a page opens no tab, doesn't reload, and keeps the call's tab,
    search text and scroll; focus stays on the chosen row.
  - Two live updates render in place, and the page moves up the list while
    staying selected.
- **History.**
  - The list matches the stored versions.
  - A pinned older version stays on screen through a live update while the
    label shows the new latest, and **Return to live** shows the latest.
- **Present.**
  - Three rounds ended by Escape, the Exit button and Escape. Each round fills
    the screen with the app's title, status and focused Exit button.
  - Two updates arrive while presenting.
  - Each exit restores focus to Present, scroll, tab, search, selection, URL,
    the same iframe and the same document, and opens no new stream.
  - All three headless engines granted native fullscreen, and the check
    confirmed the fullscreen element was the pane itself.
  - A separate page whose `requestFullscreen` is made to reject shows the
    window fallback (`data-presenting="window"`), which Escape leaves with
    focus restored.
  - A fullscreen grant delayed past Exit leaves nothing fullscreen; all three
    engines really granted it.
- **Reconnect.** After a server restart the pane reconnects and shows the
  latest version.
- **Deep link.** `viewer_url` opens the call view with that page selected.
- **History across a long disconnect.**
  - A loopback proxy drops the stream and blocks the pane's plain reads while
    the agent publishes 60 updates (v2 to v62), with v1 pinned.
  - The pin survives and the label shows v62.
  - **Load older versions** reaches all 62 versions in order, and a version
    from the formerly missing range opens.
- **History reply read before an update.** The proxy holds the first history
  response, an update arrives, then the response is released; the list ends up
  as v2 (latest), v1.

Run against the build from before each fix, the proof caught the bug:
- 60 updates during the disconnect: versions 3–12 were missing.
- The held history reply: only v1 was listed.
- Selecting a page lost scroll and focus, because the old SvelteKit 2 link
  attributes were ignored.
- A late fullscreen grant left the page fullscreen after Exit.
An independent probe found the first two.

Screenshots from the final runs are in `/tmp/zc-embedded/`:
`embedded-normal-<engine>.png`, `embedded-present-<engine>.png`,
`embedded-history-<engine>.png` (pinned older version),
`embedded-restored-<engine>.png` (after Exit),
`embedded-present-fallback-<engine>.png` and
`embedded-history-reconnect-<engine>.png`.

**Other checks:**

- With the private `TEST_DATABASE_URL`, `go test -p 1 -count=1 ./...` passes
  40 tests with none failed or skipped, with `internal/httpapi` in 19.6 s.
- The five artifact tests pass with `-race -v` with none skipped.
- `go vet ./...` is clean.
- `vp run check` reports 0 errors and 0 warnings.
- `vp build` and the binary build succeed.

Rerun the browser proof with:

```sh
cd web && ORIGIN=http://127.0.0.1:57190 vp build && cd ..
go build -o /tmp/zc/zoom-connect ./cmd
ZC_BIN=/tmp/zc/zoom-connect DATABASE_URL='postgres://…/scratch_db' \
  node scripts/artifact-browser-proof.mjs chromium   # or firefox, webkit
ZC_BIN=/tmp/zc/zoom-connect DATABASE_URL='postgres://…/scratch_db' \
  node scripts/artifact-route-race-proof.mjs chromium   # or firefox, webkit
ZC_BIN=/tmp/zc/zoom-connect DATABASE_URL='postgres://…/scratch_db' \
  node scripts/artifact-embedded-proof.mjs chromium /tmp/zc   # screenshots go to the last argument
```

The proofs create calls, keys and artifacts in that database, so point them
only at a scratch database.

## Limitations and unproven claims

- **No native Zoom integration.** Presenting is an ordinary Zoom window share
  done by a person. Nothing was tested in a real Zoom meeting, and no Zoom
  scope, app or credential is needed or was created.
- **Static artifacts only.** No JavaScript runs, so interactive charts are not
  possible. External images and fonts are blocked by design, so use `data:`
  URIs.
- **Clicked links can navigate the frame.** The policy covers loads the page
  makes by itself, not navigation. An independent review found that clicking
  an ordinary link (default `target`) navigates the sandboxed frame away from
  the artifact and requests the link's URL, which tells that host about the
  click. The parent page was unchanged, and app privileges stay blocked. The
  browser proof covers automatic loads, meta refresh, a `target=_top` form and
  `target=_top`/`_blank` links, not every possible navigation.
- **Route-change guard coverage.** The browser regression covers a delayed
  recheck read. The guard that ignores events from a replaced stream is not
  exercised separately, because browsers stop dispatching events once the
  viewer closes that stream on a route change.
- **Whole-page updates.** Each version replaces the iframe document, so scroll
  position inside the page resets on every update. There is no diff or patch
  format.
- **Escape inside the page.** In the window fallback, Escape is handled by the
  app page. If focus is inside the sandboxed frame (after a click on the
  page), Escape doesn't reach the app, so use **Exit**. This case was not
  tested.
- **List order moves.** With **Recently updated**, an update moves its page
  to the top while the selection stays put. That is intended, but rows can
  shift under the pointer.
- **Connections per tab.** Each open call view holds about three event
  streams: the app's announcements, the call, and the selected page. Over plain
  HTTP/1.1 a browser allows six connections per host, so a second open call
  tab on the same host is the practical limit; with more, requests stall. The
  proofs close finished pages for this reason. HTTP/2 should remove that
  limit; this was not tested, and the app's transport was not changed.
- **Delivery timing.** Wake-ups are in-process. Other instances, and a viewer
  whose wake-up was missed, rely on the 3-second poll. Delivery across instances
  was proven against one shared database, not on Fly. A stream whose access was
  revoked closes at its next update, or within about 3 seconds if idle; the
  viewer then rechecks and clears the page.
- **Missing operations.** There is no delete, restore or rename-only endpoint
  (every update must include `html`) and no per-key rate limit.
- **Storage grows with updates.** All versions are kept, so a call's pages
  use up to 256 KiB per update with no cap while the call is open. Writes are
  refused once a call ends, which bounds a call's total after the fact. A
  retention or cap policy is deferred.
- **Earlier deletions are permanent.** Versions the earlier 25-version build
  deleted cannot be recovered; the history shows the gap.
- **What the browser runs cover.** They prove behavior in the Playwright builds
  of Chromium, Firefox and WebKit on macOS, in headless mode, with demo login.
  Password login mode, Safari proper and real mobile browsers were not
  exercised; the 390 px layout was checked in the same headless engines.
  Fullscreen was exercised only in those headless engines, which granted
  it; a refused request was simulated by overriding `requestFullscreen`.
  Sharing the presented window in a real Zoom meeting was not tested.
- **Scroll inside the call view.** The page list and history scroll inside
  their own boxes. On a phone the history list sits below the frame and can
  be long.
- **Proof side effects.** The browser proof's positive control creates a real
  API key named `stolen-control-unsandboxed` in the scratch database. That is
  intended: it shows what the sandbox prevents.
