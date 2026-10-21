#!/bin/sh
set -eu
: "${ORIGIN:?Set ORIGIN to the public HTTPS app origin}"
go generate ./...
(cd web && ORIGIN="$ORIGIN" vp build)
mkdir -p ephemeral/bin
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-X main.origin=$ORIGIN" -o ephemeral/bin/zoom-connect-linux ./cmd
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ephemeral/bin/rtmsrec-linux ./cmd/rtmsrec
