origin := env("ORIGIN", "http://127.0.0.1:8080")

_default:
    @just --list --unsorted

# Run VitePlus and the Go application together, once. A fresh clone has neither
# frontend dependencies nor the adapter manifest the Go server reads at startup,
# so each is produced once, only while it is missing. `skgo dev` then keeps the
# session current: editing Go source, signatures or routes regenerates the
# bindings, rebuilds the application and replaces it behind the same address,
# and a build error is the response to every request until it is fixed.
dev:
    #!/bin/sh
    set -eu
    go generate ./...
    cd web
    [ -x node_modules/.bin/vp ] || vp install --frozen-lockfile
    [ -f build/skgo.manifest.json ] || ORIGIN="{{origin}}" vp build
    cd ..
    exec go tool skgo dev --listen "127.0.0.1:8080"

# Build the frontend and the standalone Go binary.
build:
    go generate ./...
    cd web && ORIGIN="{{origin}}" vp build
    mkdir -p bin
    go build -ldflags "-X main.origin={{origin}}" -o bin/zoom-connect ./cmd

# Run the standalone binary. Node is not involved.
serve: build
    ./bin/zoom-connect

# Run the upstream-installed Vitest suite.
test:
    cd web && vp run test

# Open the upstream-installed Storybook.
storybook:
    cd web && vp run storybook --host 127.0.0.1
