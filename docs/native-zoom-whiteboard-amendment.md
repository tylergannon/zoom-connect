# Proposed nlspec amendment: agent-drafted boards and Zoom Whiteboard publishing

Status: **proposal only, not adopted.** `nlspec.md` is unchanged. Adopting
any part needs HITL approval through the spec-defect channel. The prototype
behind this proposal is reported in
[native-zoom-whiteboard-poc.md](native-zoom-whiteboard-poc.md).

## Why an amendment is needed

The prototype conflicts with durable intent in four places:

1. **§4 Exclusions** bans "a participant bot, voice output, or any channel
   back into the meeting." Publishing writes to the host's Zoom account,
   though not into the meeting itself, since a person still has to open the
   board in the meeting. Even so, it is the first outbound write to Zoom.
2. **§1 design principle:** "The event format and the consumer-facing API
   carry no Zoom-specific concepts. Zoom's protocol is confined to two
   cells."
   - `board.draft` is provider-neutral.
   - The status route (`native_requests`, scopes), `board.published`
     (`whiteboard_id`, `page_id`) and `board.in_meeting` (`meeting_uuid`)
     carry Zoom terms.
   - The prototype also adds a third Zoom-protocol location: the Whiteboard
     REST adapter.
3. **zoomhook cell:** "Zoom's inbound webhook, and nothing else." The
   prototype handles `whiteboard.opened` inside the existing app webhook
   handler. Under the cell model it would become a new zoomhook signal.
4. **§5.1 Event** (shown exactly):
   - it lists a closed `EventKind` enum;
   - it defines `participant` as Zoom's participant ID and `ts` as Zoom's
     timestamp.

   Board events are created by app accounts, so `participant` is the acting
   app user and `ts` is the server time.

## Proposed text (for HITL to accept, edit or reject)

**Exclusion, replacing the §4 bullet:**

> A participant bot, voice output, or any automated channel into a live
> meeting. Exception: a call host may publish a reviewed board draft to Zoom
> Whiteboard in their own Zoom account. Opening it in a meeting remains a
> human action in the Zoom client.

**Principle note (§1):**

> Board drafts are provider-neutral. Publishing is a sink adapter. Events from
> a sink carry `data.provider` and that provider's identifiers, and nothing
> else in the consumer API depends on them.

**Event kinds (§5.1 additions):** for these kinds, `participant` is the acting
account (absent for `board.in_meeting`), and `ts` equals `recv`.

```
    board.draft          -- data: {concept, title, lanes?, cards: [{lane?, text, source_seq?, x, y, w, h}], links?: [{from, to}], agent?}
    board.published      -- data: {draft_seq, provider, whiteboard_id, whiteboard_name, page_id, added, updated, rects_sent, rects_read_back, objects_on_first_page}
    board.publish_failed -- data: {draft_seq, provider, whiteboard_id, zoom: {stage, status?, code?, message?, retry_after?}}
    board.in_meeting     -- data: {provider, whiteboard_id, whiteboard_name, meeting_uuid, meeting_id, opened_by, same_meeting?}
```

**New cell `boards` (proposed budget 650 lines of hand-written Go, tests
excluded; the prototype is 637):**

Responsibility:
- validate and lay out drafts;
- report capabilities;
- run the Zoom Whiteboard REST adapter, disabled unless a feature gate, a
  user OAuth token and the declared scopes are present;
- correlate board-opened signals.

```
INTERFACE Boards:
    FUNCTION status(call, principal) -> {mode, can_draft, can_publish, missing[], latest, native_requests}
    FUNCTION draft(call, principal, concept, title, cards, links) -> Event      -- host or participant; call scheduled|live
    FUNCTION publish(call, principal, draft_seq) -> Event                       -- host only; never sends unless ready
    FUNCTION opened(signal: BoardOpened) -> Event | None                         -- only boards this app published, only in a meeting
```

zoomhook would emit `BoardOpened{whiteboard_id, whiteboard_name, meeting_id,
meeting_uuid, operator_id}` from a verified `whiteboard.opened`.

## Proposed claims

- **B1 (cell-local).** Only the host or a participant (including their API
  keys) can draft, and only while the call is scheduled or live. A
  share-only viewer can read status. Others get `403`.
  Demonstrated by `TestWhiteboardDraftAccessAndPreview`.
- **B2 (cell-local).** Native content objects contain only `type`, `x` and
  `y`, the fields Zoom documents. Card text is never sent.
  Demonstrated by `TestWhiteboardDraftAccessAndPreview` and
  `TestLayoutBoardCoordinates`.
- **B3 (cell-local).** With any precondition missing, publish returns `503`
  naming the missing items and makes no outbound request. With all present,
  publish sends exactly create, content and read-back. The token is not
  stored or returned. A failure after the board was created records a
  failure event that names the board. Concurrent publishes of one draft
  create one board.
  Demonstrated against a local fake by `TestWhiteboardPublishAgainstFakeZoom`,
  `TestWhiteboardConcurrentPublishCreatesOneBoard`,
  `TestWhiteboardPublishFailures` and `TestConfigureWhiteboard`.
- **B4 (integration, NOT demonstrated).** With an approved user OAuth grant,
  publishing a draft creates a board in the host's Zoom account, and
  read-back finds every rectangle. Requires a live Zoom account.
- **B5 (integration, NOT demonstrated).** When the host opens that board in
  the call's meeting, Zoom's `whiteboard.opened` is recorded once as
  `board.in_meeting` with `same_meeting=true`. Requires:
  - a live meeting;
  - the Whiteboard event subscription;
  - routing `whiteboard.opened` past the `rtmsrec` proxy.

  Under the spec standard, the Whiteboard seam is not done until B4 and B5
  are shown live. The fake-server tests prove only the contract.

## Deferred decisions (tripwires; escalate, do not improvise)

- **Token custody.** Options are one operator token (boards owned by that
  user) or per-host OAuth (boards owned by each host). Per-host OAuth changes
  auth and storage.
- **Renderer schema.** Text, sticky notes or connectors need documented
  fields, or an approved sandbox experiment.
- **Edits.** Object identity and "updated" semantics are undocumented. Until
  they are documented, each publish creates a new board.
- **Failed boards.** Deletion or collaborator sharing would need new scopes.
- **Review.** Should an agent ever publish without host review? The
  prototype says no.
