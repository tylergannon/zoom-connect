# Zoom RTMS setup record

Status: in progress. The standalone recorder builds and its package tests pass. No real meeting has been captured yet. The web application remains a simulated demo.

## Prerequisites

- Zoom Pro or higher and Developer Pack (user reports both purchased).
- Sign into Zoom Marketplace with the same intended Zoom account.
- A local build of `cmd/rtmsrec` and an HTTPS tunnel to its webhook listener.

## 1. Enter the developer portal

In Safari, open https://marketplace.zoom.us/user/build. The existing session is signed in. On first entry, Zoom presents its Marketplace Terms of Use. The user must approve acceptance before the agent clicks **Agree**.

![Marketplace Terms of Use](screenshots/01-marketplace-terms.png)

## Remaining steps

These are the repository's planned steps; they have not yet been performed or verified in this account:

1. Create a user-managed General App.
2. Configure transcript/chat RTMS scopes and event subscriptions.
3. Save app credentials locally, excluding them from Git and screenshots.
4. Run the recorder and a temporary Cloudflare tunnel; register the HTTPS webhook endpoint.
5. Validate the webhook, install the test app, and configure RTMS startup.
6. Start a test meeting, speak and send a public chat message, then verify actual data reaches the recorder.

Record actual UI labels and results below as setup proceeds. Never treat dummy webhook data or the simulated web UI as live Zoom proof.
