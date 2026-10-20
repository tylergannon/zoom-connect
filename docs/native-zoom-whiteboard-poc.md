# POC: agents drafting native Zoom Whiteboards — report (October 20, 2026)

Status: **local contract prototype.** Agents with existing API keys can draft
boards on a call, and the call page previews each draft. The exact Zoom
Whiteboard requests are generated and shown for review. A host-only publish
action uses the documented Zoom REST API, but it is off unless an operator
supplies a user OAuth token and declares the three Whiteboard scopes.

**No request was sent to Zoom.** Native publishing was proven only against a
local fake of `api.zoom.us`. A board appearing inside a live meeting is not
automated and not proven (see Blockers).

Branch `claude/opus-native-whiteboard-poc-20261020`. Proposed spec changes
are in [native-zoom-whiteboard-amendment.md](native-zoom-whiteboard-amendment.md);
`nlspec.md` is unchanged.

## What Zoom officially supports (researched October 20, 2026)

| Need | Official surface | Constraint |
|---|---|---|
| Create a board | `POST https://api.zoom.us/v2/whiteboards` `{"name"}` → `201 {whiteboard_id, whiteboard_name, …}` | Granular scope `whiteboard:write:whiteboard` (classic `whiteboard:write`) |
| Add content | `POST /v2/whiteboards/{whiteboardId}/content` `{"content_objects":[…]}` → `201 {page_id, added, updated}` | `whiteboard:write:content` (classic `whiteboard_content:write`). Up to 10,000 objects. Each object is "a free-form JSON map whose schema is determined by the whiteboard renderer". The only example is `{"type":"rect","x":100,"y":200}`. `403` code `111804` = no edit permission |
| Read content | `GET /v2/whiteboards/{whiteboardId}/content` → `200 {whiteboard_id, page_id, content_objects}` | `whiteboard:read:content`. **First page only** |
| Remove content | `DELETE /v2/whiteboards/{whiteboardId}/content` `{"object_ids":[…]}` → `204` | `whiteboard:delete:content`. POST returns no object IDs (see Limitations) |
| Content API availability | — | Not supported in the Gov cluster. No `:admin` scope variants |
| Show a board in a meeting | **No REST or Apps SDK API.** Apps SDK v0.16.42 has no whiteboard method. The Meeting SDK's `StartNewWhiteboardShare()` only starts a *new* blank board, from a participant client (a bot, which `nlspec` excludes) | A person opens it: desktop client **Whiteboards → Existing Whiteboards → (board) → Open Board** (support article KB0058148). Since November 2024, boards can also be shared into an active meeting from the browser or app |
| Admin controls | Account/group/user Whiteboard setting. July 19, 2026 release: "who can share existing whiteboards during a meeting" (Host only / Only users in organization / All participants, default all) | The host must be allowed to share existing boards |
| Know it was opened in a meeting | Webhook `whiteboard.opened`; `payload.object.meeting_id`/`meeting_uuid` are present only when the board is opened in a meeting | The Zoom app must subscribe to Whiteboard events |

Sources: the Whiteboard API reference and events at
https://developers.zoom.us/docs/api/whiteboard/, the Apps SDK reference at
https://appssdk.zoom.us/classes/ZoomSdk.ZoomSdk.html, Zoom support article
KB0058148, and Zoom Whiteboard release notes (November 2024, January 2026,
July 2026). The January 2026 notes mention an admin API for object properties
that does not appear in the REST reference; it was not used.

## Concepts

All three share one layout engine, and each card becomes one positioned
rectangle in Zoom.

1. **Decision board (default, smallest supportable).** Fixed lanes:
   *Decided / Open questions / Parking lot*. Each card may cite the
   `source_seq` of the transcript event that produced it, and that seq is
   checked against the call's existing events.
2. **Action swimlanes.** One lane per owner, up to 6 lanes and 48 cards. An
   empty lane is shown as *Unassigned*.
3. **Evolving diagram.** Nodes laid out on a 4-column grid, with up to 96
   links shown only in the local preview. "Evolving" means the agent posts a
   new draft. Earlier drafts are kept as events and never edited.

Rejected options:
- Miro, Mural or Visio import endpoints: third-party content.
- Meeting SDK bot: excluded participant channel, and it can only open new
  blank boards.
- Apps SDK: no whiteboard API.
- Native text, connectors or in-place edits: undocumented renderer fields and
  object identity.

## Prototype contract

All routes use the existing session or Bearer API-key auth and the existing
call ACL. `/api/calls/…` and `/api/v1/calls/…` behave the same.

| Route | Who | Result |
|---|---|---|
| `GET /api/v1/calls/{id}/whiteboard` | anyone who can see the call (participant or share) | `mode` (`preview`/`native`), `can_draft`, `can_publish`, `native.missing[]`, the latest draft, published, failed and opened events, and the exact `native_requests` plus what is `not_sent` |
| `POST /api/v1/calls/{id}/whiteboard` | host or participant, call `scheduled`/`live` | `201` and a `board.draft` event. `400` for an invalid board (unknown fields are rejected), `403` for share-only viewers, `409` once the call has ended |
| `POST /api/v1/calls/{id}/whiteboard/publish` `{"draft_seq":N}` | call host only | `503 {missing}` when native publishing is off. Otherwise create, add content, read back: `201` and a `board.published` event. If Zoom fails: `502 {zoom:{stage,status,code,message,retry_after}}`, and if a board was already created, a `board.publish_failed` event naming it. `409` if this draft was already published |
| `POST /zoom/webhook` `whiteboard.opened` (signed) | Zoom | Only when `ZOOM_WHITEBOARD_NATIVE=enabled`, and only for boards this app published that were opened inside a meeting: a `board.in_meeting` event (`meeting_uuid`, `meeting_id`, `same_meeting` when the call has a Zoom meeting ID) |

Native mode needs all of these (names are in `.env.example`):
`ZOOM_WHITEBOARD_NATIVE=enabled`, `ZOOM_WHITEBOARD_ACCESS_TOKEN`, and
`ZOOM_WHITEBOARD_SCOPES` containing all three required scopes.
`ZOOM_WHITEBOARD_API_URL` may point to HTTPS, or to HTTP on loopback for a
fake. The token is only placed in the outbound `Authorization` header. It is
never stored, logged or returned, and redirects are not followed. Scopes are
operator-declared, and Zoom's `401`/`403` remains authoritative.

### Exact example (captured from the local runtime check)

Agent draft:

```sh
curl -H "Authorization: Bearer $ZC_KEY" -H "Content-Type: application/json" \
  http://127.0.0.1:57661/api/v1/calls/$CALL/whiteboard -d '{"concept":"decision_board",
  "title":"Launch decisions","cards":[
   {"lane":"Decided","text":"Ship the beta on Friday","source_seq":1},
   {"lane":"Open questions","text":"Who owns the migration runbook?"},
   {"lane":"Parking lot","text":"Revisit pricing tiers in Q1"}]}'
```

`201` event, delivered to SSE listeners as `event: board.draft`:

```json
{"seq":2,"kind":"board.draft","data":{"agent":"runtime-board-agent","concept":"decision_board",
 "title":"Launch decisions","lanes":["Decided","Open questions","Parking lot"],"cards":[
 {"lane":"Decided","text":"Ship the beta on Friday","source_seq":1,"x":64,"y":104,"w":240,"h":120},
 {"lane":"Open questions","text":"Who owns the migration runbook?","x":352,"y":104,"w":240,"h":120},
 {"lane":"Parking lot","text":"Revisit pricing tiers in Q1","x":640,"y":104,"w":240,"h":120}]}}
```

The Zoom requests that publish would send for this draft. These were
generated and shown, **not sent**:

```http
POST https://api.zoom.us/v2/whiteboards                         (whiteboard:write:whiteboard)
Authorization: Bearer <user OAuth access token>
Content-Type: application/json

{"name":"Launch decisions"}

POST https://api.zoom.us/v2/whiteboards/{whiteboardId}/content  (whiteboard:write:content)
{"content_objects":[{"type":"rect","x":64,"y":104},{"type":"rect","x":352,"y":104},{"type":"rect","x":640,"y":104}]}

GET https://api.zoom.us/v2/whiteboards/{whiteboardId}/content   (whiteboard:read:content)
```

Publish with native publishing off returned `503`:
`{"error":"native Zoom Whiteboard publishing is disabled","missing":["feature gate ZOOM_WHITEBOARD_NATIVE=enabled","user OAuth access token in ZOOM_WHITEBOARD_ACCESS_TOKEN","granted scope whiteboard:write:whiteboard","granted scope whiteboard:write:content","granted scope whiteboard:read:content"]}`

## Demo steps (local only)

1. Start a private Postgres. Then build the app and run it on loopback:
   `go generate ./... && (cd web && ORIGIN=http://127.0.0.1:57661 vp build)`, then
   `DATABASE_URL=postgres://…/zoom_connect_whiteboard_runtime go run ./cmd -listen 127.0.0.1:57661 -origin http://127.0.0.1:57661`.
2. Sign in as a demo user, create an API key on **Keys**, and start a demo
   call. You can also use an existing live call the key owner participates in.
3. Run the curl above with that key. Open `/calls/{id}`: the **Zoom
   Whiteboard draft** panel shows the following:
   - the lanes and cards at the coordinates Zoom would receive;
   - the agent's name;
   - what is not sent;
   - the exact native requests (expandable);
   - the missing grants.
4. Review only: a reviewer can decide from this panel whether the
   rectangle-only native result is worth an OAuth grant.
5. Optional future step, after approval: set the four variables. The host
   then gets **Publish to Zoom Whiteboard**, and opens the new board in the
   meeting by hand (see above).

## Verification results

All checks ran locally on 2026-10-20 with Go 1.27.2, `GOMAXPROCS=2`, and a
private throwaway Postgres 17 container (`127.0.0.1:57660`, tmpfs).

- `go vet ./...` is clean. `go test -p 1 -count=1 -v ./...` with `TEST_DATABASE_URL=…/zoom_connect_whiteboard_test` exits **0**: 39 PASS, 0 SKIP, 0 FAIL (`cmd/rtmsrec`, `internal/httpapi`). New tests:
  - **`TestWhiteboardDraftAccessAndPreview`** (real handlers and Postgres) covers:
    - anonymous `401`;
    - an API-key agent draft on `/api/v1` that records the agent name;
    - `board.draft` in `/events`;
    - lane normalization;
    - native objects carrying only `type/x/y` and no card text;
    - preview status naming all 5 missing items;
    - 11 invalid drafts → `400`;
    - an unshared user `403`;
    - share-only can view but gets `403` on draft and publish;
    - a non-host participant can draft but gets `403` on publish;
    - disabled publish `503` with nothing recorded;
    - the webhook ignored while disabled;
    - an ended call → `409`.
  - **`TestWhiteboardPublishAgainstFakeZoom`** uses an `httptest` fake of `/v2` that returns the documented responses and adds its own object `id` fields. It checks:
    - exactly 3 requests: `POST /v2/whiteboards {"name"}`, `POST …/content` with only `content_objects` of `{type,x,y}`, then `GET …/content`;
    - Bearer auth and a JSON content type;
    - a `board.published` event with `whiteboard_id`, `page_id`, `added 3`, `rects_read_back 3`;
    - the token absent from every app response and from all stored call events;
    - republish `409` without calling Zoom;
    - `400` for a missing `draft_seq` and `404` for a non-draft seq;
    - `whiteboard.opened`: unsigned `401`; outside a meeting or an unknown board are ignored; a signed in-meeting event produces one `board.in_meeting` event with `meeting_id "85012345678"`.
  - **`TestWhiteboardConcurrentPublishCreatesOneBoard`**: 4 concurrent publishes of one draft, with Zoom's create step held open, produce one `201`, three `409`s, 3 Zoom requests and one `board.published` event. A mutation run without the publish lock failed (4 boards, 12 requests), so the test detects the race.
  - **`TestWhiteboardPublishFailures`** checks two failure modes:
    - create `401`/`124` → `502` at stage `create`, with no board and no event;
    - content `403`/`111804` → `502` at stage `content`, with `whiteboard_id`, a `board.publish_failed` event, and no success event.
  - **`TestLayoutBoardCoordinates`**, **`TestConfigureWhiteboard`** cover:
    - exact coordinates;
    - native-object keys;
    - read-back matching;
    - off by default with 5 missing items;
    - rejection of non-HTTPS/non-loopback API URLs;
    - a partial scope grant staying disabled.
- `vp run check`: 0 errors, 0 warnings. `vp build`: succeeded.
- Runtime: the built binary ran on `127.0.0.1:57661` against a separate private database:
  - with curl: demo login `200`, API-key draft `201` (event 2, `agent=runtime-board-agent`), status `200 mode=preview can_publish=false`, publish `503` with the missing list, anonymous `401`;
  - headless Chromium (Playwright, from `web/node_modules`) signed in as the host and opened the call page. It confirmed the panel heading, the *Local preview only* badge, the agent name, 3 SVG cards, the lanes `Decided|Open questions|Parking lot`, card text with its event citation, the missing-grants note and the request JSON, and that no publish button appears in preview mode.

## Limitations

- **Zoom receives positioned rectangles only.** Card text, lane headings,
  card size and links stay in the local preview. Zoom documents no text,
  sticky-note, size or connector fields, so a real board would show unlabeled
  rectangles. This alone may make native publishing not worth approving yet.
- **No in-place edits.** POST does not return object IDs, and how it decides
  between "added" and "updated" is undocumented. Each publish therefore
  creates a new board. Agents "edit" by posting a new draft.
- The read-back check matches `type/x/y` on the first page only. It proves the
  request round-trip, not how the renderer displays the objects.
- Retries, rate limits (`Retry-After` is surfaced but not acted on), board
  deletion and collaborator sharing are not implemented.
- Publishes are serialized by an in-process lock. This is correct for the
  single-instance deployment; several instances would need a database lock.
- Webhook correlation only matches boards whose IDs appear in this app's
  `board.published` events. `same_meeting` needs the call's Zoom meeting ID or
  UUID.

## Blockers to a real integration (nothing below was created or requested)

1. **OAuth grant (pending, not created).** A user-level OAuth app with the
   following scopes:
   - required: `whiteboard:write:whiteboard`, `whiteboard:write:content`,
     `whiteboard:read:content`;
   - optional: `whiteboard:delete:content` (cleanup),
     `whiteboard:write:collaborator` (share to a host other than the token
     user), `whiteboard:read:whiteboard`.

   The existing RTMS app's scopes are not Whiteboard scopes, so no current
   credential covers this.
2. **Token ownership.** A board belongs to the token's user. The host can only
   find it under *Existing Whiteboards* if they own it or it is shared with
   them. That is expected behaviour, not verified.
3. **Account entitlement and admin settings:**
   - Whiteboard must be enabled for the account and the host;
   - the account must not be on the Gov cluster;
   - "who can share existing whiteboards during a meeting" must allow the host.

   Plan limits on board counts were not checked.
4. **In-meeting visibility.** No API exists. A person must open the board in
   the meeting. Proving it needs a live meeting, which was out of scope.
5. **Webhook delivery.** The Zoom app needs the Whiteboard event subscription.
   In the deployed topology `/zoom/webhook` is proxied to `rtmsrec`
   (`RTMS_RECORDER_URL`), which ignores non-RTMS events. `whiteboard.opened`
   would need forwarding to this handler.
6. **Renderer schema.** Text or shape types beyond `rect` need Zoom
   documentation, or a sandbox-account experiment approved separately.
