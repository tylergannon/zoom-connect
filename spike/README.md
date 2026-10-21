# Spike: record a real RTMS exchange

Goal: settle nlspec.md deferred decision 1 (the real RTMS message shapes) by
recording one live meeting, and prove the Zoom account/app configuration
works before any gateway code exists.

One tool: `cmd/rtmsrec`, a raw-protocol client that records every webhook
and every WebSocket message verbatim under `recordings/`. It follows Zoom's
documented flow (`rtms-samples/RTMS_CONNECTION_FLOW.md`) line for line, so a
handshake rejection means provisioning, not our code; the `status_code` and
`reason` in the response say which.

## 1. Zoom side (once)

Prerequisites: paid plan (Pro+), Developer Pack active, and either the account
owner or a role with "Zoom for developers".

1. marketplace.zoom.us → Develop → Build App → **General App** → **User-managed**.
2. Basic Information: name it something honest (participants see it in the
   app indicator). Any OAuth redirect URL is fine for now.
3. Features → Access → **Event Subscriptions** → add a subscription:
   - endpoint URL: `https://<tunnel-host>/zoom/webhook` (see step 2 below;
     the server must be running when you click Validate)
   - events: search "rtms" → `meeting.rtms_started`, `meeting.rtms_stopped`,
     `meeting.rtms_interrupted`
   - copy the **Secret Token** → `ZOOM_WEBHOOK_SECRET`
4. Scopes → search "rtms" → add `meeting:read:meeting_transcript`,
   `meeting:read:meeting_chat`, and `meeting:read:meeting_audio` if you want
   audio. If they're greyed out despite the Developer Pack, post the App ID on
   devforum.zoom.us (RTMS category); that is how everyone gets unblocked.
5. App Credentials → copy Client ID / Client Secret →
   `ZOOM_CLIENT_ID` / `ZOOM_CLIENT_SECRET`.
6. Local Test → **Add** the app to your own user.
7. Admin → Account Settings → Zoom Apps:
   - turn on **Share realtime meeting content with apps**
   - auto-start: select this app for "meetings I host"

## 2. Tunnel

Zoom must reach your laptop. Either:

```bash
cloudflared tunnel --url http://localhost:8080
```

```bash
ngrok http 8080
```

Put the printed https host into step 1.3. The tunnel URL changes each run
with the free tiers; re-validate the endpoint in Zoom after restarting it.

## 3. Run the recorder

```bash
cp .env.example .env   # then fill in the three secrets
```

```bash
set -a; source .env; set +a; go run ./cmd/rtmsrec
```

Then click **Validate** on the Zoom event subscription; the log should say
`answered url_validation`. Start a Zoom meeting from your own account, talk,
type a public chat message, end the meeting.

What you should see, in order:

```text
webhook: meeting.rtms_started {...}
[<stream>] signaling handshake ok; media urls: map[...]
[<stream>] media-transcript handshake ok; sending CLIENT_READY_ACK
[<stream>] Dana Kim: hello hello
...
webhook: meeting.rtms_stopped
[<stream>] closed. message counts:
```

Every distinct `(socket, msg_type)` is printed once in full with the prefix
`FIRST`. That console output alone answers most of deferred decision 1.

## 4. If the handshake is rejected

Signaling `msg_type 2` or media `msg_type 4` with a non-zero `status_code`
is Zoom refusing us. Status 8 on the very first signaling handshake is a
transient startup rejection (Zoom's library retries it). A media socket URL
missing from `media_server.server_urls` means the app lacks that scope.
Anything else: post the App ID and the `reason` on devforum.zoom.us.

## 5. What the recording must answer

Check `recordings/<ts>-<stream>/` and fill these into nlspec.md §7:

- [ ] `webhooks.jsonl`: is `payload.server_urls` a string or an object? Are
      there `topic`/host fields, or do we need the REST API for those?
- [ ] `signaling.jsonl`: shape of `msg_type 6` for event types 2, 3, 4
      (`participants` array vs flat `user_id`/`user_name`), and of
      `msg_type 8`/`9` (`state`, `reason`, `stop_reason`, `rtms_session_id`).
- [ ] `media-transcript.jsonl`: fields in `content` (`data`, `user_id`,
      `user_name`, `timestamp`, `start_time`, `end_time`, `language`,
      `attribute`). Is there any partial/final flag, or only final lines?
- [ ] `media-chat.jsonl`: is `content.data` plain text or a JSON string with
      `sender`/`receiver`/`chat_session`/`message_id`?
- [ ] With `RTMS_MEDIA=audio,transcript,chat` and `RTMS_AUDIO_CODEC=opus`:
      does the audio handshake accept codec 4 + data_opt 2? What is
      `content.data_len` per 20 ms packet? (Opus ≈ 40–80 B; L16 = 640 B.)
      Play `audio-<user>.l16` with
      `ffplay -f s16le -ar 16000 -ac 1 audio-<user>.l16` to confirm.
- [ ] Did audio arrive from before the connect (buffer_data)?
- [ ] End a meeting, restart it from the same link: does `meeting_uuid`
      change? (deferred decision 5)
