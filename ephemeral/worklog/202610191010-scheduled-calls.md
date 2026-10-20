# Scheduled calls

decision: User wants to paste an existing Zoom invitation, confirmed in this chat. Implement one next session on a link with a stable pre-start call and SSE subscription.
decision: Registration prepares a subscription, not an unattended Zoom participant. Zoom RTMS still needs an app user in the meeting and host permission; expected time is advisory.
decision: Creator access must be explicit ownership, not a fabricated participant. Persist runtime UUID and stream mappings separately from the reusable meeting number.
decision: Use an isolated local zoom_connect_scheduling_test database; leave active deployment and Zoom account configuration untouched.
correction: Tyler clarified that clients connect only to zoom-connect, never directly to Zoom. Keep explanations and the public API centered on downstream subscription; upstream RTMS permissions belong to the app.
decision: A stop delivered before its start is persisted as a stream tombstone; duplicate old runtime UUIDs cannot claim a later scheduled session. A call with multiple RTMS streams ends only after its last active stream stops.
friction: Screenshots captured resize and dialog transitions mid-animation, obscuring otherwise valid UI -> disable animations when capturing proof and inspect the actual images.
proof: Local production binary on 127.0.0.1:8094, isolated zoom_connect_scheduling_test_browser database. Browser-created invitation registration, desktop/mobile waiting screens, an independent API-key SSE client connected before start, first transcript on that connection, sequence replay, and UI cancellation all demonstrated in ephemeral/scheduled-proof. Incoming Zoom data was simulated; real scheduled Zoom capture remains unproven and nothing was deployed.
