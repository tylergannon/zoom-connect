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
