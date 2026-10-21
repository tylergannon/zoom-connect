# Scheduled-call lifecycle validation (main f0d4d70)

- Isolated Postgres: `docker run --name zc-lifecycle-pg -p 127.0.0.1:55432:5432 postgres:17-alpine`, tests via
  `TEST_DATABASE_URL=postgres://zoom:zoom@127.0.0.1:55432/zoom_connect_test?sslmode=disable`. The helper
  `testDatabase` truncates a shared `_test` DB, so never point it at the long-running dev container's data and
  do not run packages in parallel.
- Baseline: all existing httpapi integration tests passed with none skipped.
- Defect found by code review, reproduced, fixed: `stopRTMSStream` scanned a NULL `call_id` into a string when an
  `interrupted` stop-before-start tombstone later received `stopped` -> HTTP 500 for the recorder. Regression:
  `TestStopBeforeAttachTombstoneAcceptsInterruptedThenStopped`.
- `go test ./...` fails in `cmd` and `web` only because `web/build` (embedded assets) is absent until `vp build`;
  not caused by this change.
- Not changed (judgment): `stream` reads call status before `replaySubscribe`, so a cancel racing the connect is
  only closed on the next 3s tick (self-heals). Late attach of a never-seen older UUID cannot be told from a newer
  one and would supersede the current call; spec does not cover it.
