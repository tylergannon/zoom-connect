# Zoom Connect

A Zoom bot for one small company. Zoom pushes each call's transcript, chat
and participant events to us over Realtime Media Streams (RTMS); we keep them
and serve them to the company's people (web UI, Google sign-in restricted to
one Google Apps domain) and to their agents (API keys, SSE, downloads).

The current implementation combines the local POC UI (simulated Google
sign-in and seeded demo calls) with a tested live Zoom RTMS recorder bridge.
Postgres stores each normalized meeting event as a separate ordered row,
alongside API keys, sharing, access checks, and the call index. Existing JSONL
archives are imported once at startup; normalized events no longer depend on
local files. The standalone recorder still writes raw protocol diagnostics.
Slack and audio/video capture in the web app remain deferred.

## Read first

- `nlspec.md` — the meeting pipeline: RTMS, session, archive, bucket layout,
  event format, cells and budgets. Load the `spec-writing` skill before
  reading or changing it.
- `docs/requirements.md` — the product surface: user-facing claims, technical
  requirements, MVP line, open questions, and the nlspec amendments the
  product requires. Where they conflict, requirements.md is newer; fold the
  change into nlspec before building on it.
- `spike/README.md` — the RTMS recorder spike and what its recording must
  answer.

## MVP

Listen to Zoom calls; let one or two connected agents listen in live (SSE) or
download the finished transcript. Slack DMs and shared-media capture are
deferred. Do not build deferred items without being asked.

## Stack

- **skgo** (SvelteKit 3 served by a single Go binary). Load the `skgo` skill
  (`.claude/skills/skgo`) before writing app code. All server behavior —
  loads, remote functions, `+server.ts` routes, the webhook, SSE — is Go.
  Svelte is UI only; no application I/O in TypeScript.
- CLI: `skgo` (installed with `go install github.com/tylergannon/skgo/cmd/skgo@latest`;
  inside the app prefer the project's `go tool skgo`). `skgo update` aligns the
  CLI and toolchain.
- Frontend tooling goes through VitePlus (`vp install`, `vp run`, `vp dlx`).
  Never call `pnpm`/`npm` directly.
- UI components: shadcn-svelte (`vp dlx shadcn-svelte@latest add <component>`).
  Reference: https://www.shadcn-svelte.com/llms.txt
- Postgres via sqlc over pgx/v5 for people, keys, shares, settings, the call
  index, and individual text/lifecycle events. Generate query bindings with
  `sqlc generate` after changing schema or queries.
- The October 16 storage amendment in nlspec supersedes the draft R2 design
  for normalized events. Hosting and binary-media storage remain undecided.
- Go 1.27.2 exactly (skgo dev requires it).

## Layout

- `cmd/rtmsrec/` — spike: raw RTMS recorder. Not production code.
- `cmd/main.go`, `server.go` — the skgo application and server composition.
- `internal/httpapi/`, `internal/store/` — Go endpoints and sqlc/pgx storage.
- `web/` — SvelteKit pages and shadcn-svelte components.
- Start Postgres with `docker compose up -d postgres`, then run `just dev`.
  `just build` produces the standalone `bin/zoom-connect` application.

## Rules

- Call IDs are ULIDs assigned when the webhook arrives; URLs use them, never
  Zoom's meeting UUID.
- Access: a principal sees a call only if they were a participant or hold a
  share. An API key acts as its owner. Check this at every route, including
  SSE and downloads.
- Sign-in requires a verified email and `hd == ALLOWED_DOMAIN` from the ID
  token; never trust the `hd` login hint.
- Secrets (API keys, Zoom credentials) are stored hashed or encrypted and
  shown once.
- Tests are Go tests against the real handler. Run `go test ./...` and the
  frontend check before claiming a change works.

## UI design

The proposed UI lives in Diffusion Designer, project "Zoom Connect" (one page
per screen, notes on each page, deferred screens tagged `deferred`).
