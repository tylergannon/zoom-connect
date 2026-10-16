# Live Zoom setup

decision: The user reports purchasing Zoom Pro and Developer Pack and authorizes getting the application running for testing.
correction: The current web application explicitly simulates RTMS ingestion. Use the existing standalone rtmsrec spike for the first actual Zoom transcript test; a running demo UI does not prove live Zoom integration.
friction: No local .env was found in the root checkout or the previously named app and RTMS test worktrees. Zoom Marketplace in the accessible in-app browser requires sign-in before developer app credentials can be configured.
