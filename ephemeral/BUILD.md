# Zoom Connect POC assignment context

User request 2026-10-15 governs: complete a simple working local POC in <=250 minutes. Luna builds bounded slices; Claude Sonnet reviews. No extra proof machinery or production infrastructure.

Design snapshots in ephemeral/design/*.json and *.html are the current design and requirements authority. Read scope-map.txt and the relevant page .txt/.html. nlspec.md provides pipeline/event semantics, but the latest user instruction authorizes mocked OAuth/Zoom and the newer design's call access/sharing, searchable calls and broader UI. Do not amend nlspec or block on its older budgets/exclusions. No Slack implementation. Audio is deferred. No real cloud credentials needed. Keep meeting transcript content in a simple local filesystem archive; use Postgres for users, sessions, keys, sharing, settings and call access metadata. Do not implement R2 for this POC.

Stack: installed skgo v0.34.0 (check latest release; use latest), Svelte 5, shadcn-svelte real components, Tailwind, pgx v5, sqlc generated queries; Docker Postgres. Single Go app on 127.0.0.1:8080. Keep original cmd/rtmsrec intact. Working tree shared; don't revert others. No commits unless manager asks.

Demo personas Dana Kim dana@acme.co (admin), Marcus Reyes marcus@acme.co (admin), Jordan Lee jordan@acme.co, Priya Shah priya@acme.co, Leo Park leo@acme.co. Host counts as participant. Only participants may grant shares to in-domain email, including not-yet-registered users. Grantees cannot re-share. Only grantor may revoke their share. All API reads, downloads, stream access enforce same owner user permissions. Org member without access gets 403 (participant names okay, never transcript/title). Unknown ID 404. Nonadmin settings 404.

Use API endpoints (JSON snake_case), frontend fetches same origin with cookie. Collections are bare arrays. Dates ISO strings except Event ts/recv millisecond integers. User {id,email,name,is_admin}; Call {id,topic,status:'live'|'stopped'|'interrupted',started,ended?,host:{id,name,email},participants:[{id,name,email}],last_seq,shared_by?,is_participant,can_share}; Event per nlspec {seq,ts,recv,kind,participant?:{id,name},data:{text?,lang?,final?}}.
GET /api/me -> User (401 signed out)
POST /api/demo/login {email} -> User + session cookie (known seeded personas only)
POST /api/logout -> 204
GET /api/demo/users -> User[] (public list for demo switcher)
GET /api/calls -> accessible Call[]; frontend filters live, mine, shared and search
GET /api/calls/{id} -> Call; 403 body {error,participants:[{id,name}]}
GET /api/calls/{id}/events?from=0 -> Event[]
GET /api/calls/{id}/stream?from=0 -> SSE event: transcript/chat/participant.joined/etc, id: seq, data: Event; replay then live; Last-Event-ID; ended emits event:end. Re-check access to close revoked viewers.
GET /api/calls/{id}/transcript?format=txt|vtt|json -> download
GET /api/calls/{id}/shares -> {participants:[{id,name,email}],shares:[{id,email,name?,granted_by,granted_by_name,created_at,can_remove}],can_share}
POST /api/calls/{id}/shares {email} -> share
DELETE /api/calls/{id}/shares/{shareId} -> 204
GET /api/people?q=... -> User[]
GET /api/keys -> [{id,name,prefix,created_at,last_used_at?,revoked_at?}]
POST /api/keys {name} -> {key:{id,name,prefix,created_at},secret} shown once
DELETE /api/keys/{id} ->204
GET /api/admin/zoom -> {mode:'demo',connected:false,webhook_url,last_webhook?,live_streams,calls_this_week,credentials:{client_id,client_secret_suffix,webhook_secret_configured},capture:{transcript,chat},webhooks:[{at,event,topic,outcome}]}
POST /api/admin/zoom {client_id,client_secret?,capture:{transcript,chat}} -> same settings; write-only secrets
POST /api/demo/calls -> new simulated live Call (admin only)
POST /api/demo/calls/{id}/events {kind:'transcript',participant:{id,name},data:{text,lang:'en',final:true}} -> Event (admin only)
POST /api/demo/calls/{id}/stop -> Call (admin only)
POST /zoom/webhook -> signed Zoom validation + started/stopped/interrupted; signature check; do not claim real RTMS validated. Demo ingestion endpoints explicitly simulated.

Offer aliases /live, /meetings, /meetings/{id}/* for agent API if easy; main guide must match real endpoints.

Seed ~10 realistic calls with participants/access contrasts, 2 live, enough transcript/chat to make the screens useful, dates relative to today. Live simulator emits periodically and can be stopped. Keep demo transcript bounded; no unbounded forever-repetition growth.
