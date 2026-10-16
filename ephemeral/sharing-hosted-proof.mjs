import { chromium } from '../web/node_modules/playwright/index.mjs';
import { writeFile } from 'node:fs/promises';
import assert from 'node:assert/strict';

const origin = 'https://zoom-connect-tpg-demo.fly.dev';
const email = 'sharing-proof@example.invalid';
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1360, height: 1000 } });
let call;
let shareId;
try {
  const login = await context.request.post(`${origin}/api/login`, { data: { email: process.env.DEMO_LOGIN_EMAIL, password: process.env.DEMO_LOGIN_PASSWORD } });
  assert.equal(login.status(), 200, 'hosted login');
  const calls = await (await context.request.get(`${origin}/api/calls`)).json();
  call = calls.find(c => c.can_share);
  assert.ok(call, 'a shareable call exists');
  const page = await context.newPage();
  await page.goto(`${origin}/calls/${call.id}`);
  await page.getByRole('button', { name: 'Share', exact: true }).click();
  await page.getByRole('textbox', { name: 'Find a person or enter an email' }).fill(email);
  const added = page.waitForResponse(r => r.url().endsWith('/shares') && r.request().method() === 'POST');
  await page.getByRole('button').filter({ hasText: email }).click();
  assert.equal((await added).status(), 201, 'hosted outside-domain share');
  const access = await (await context.request.get(`${origin}/api/calls/${call.id}/shares`)).json();
  shareId = access.shares.find(s => s.email === email)?.id;
  assert.ok(shareId, 'share persisted');
  await page.getByText(email, { exact: true }).waitFor();
  await page.screenshot({ path: 'ephemeral/screenshots/sharing-any-email-hosted.png' });
  console.log('Hosted browser proof passed: outside-domain share added and visible.');
} finally {
  if (call) {
    // Recover the grant even if a UI assertion failed after creation.
    const access = await (await context.request.get(`${origin}/api/calls/${call.id}/shares`)).json();
    shareId ??= access.shares?.find(s => s.email === email)?.id;
    if (shareId) {
      const removed = await context.request.delete(`${origin}/api/calls/${call.id}/shares/${shareId}`);
      assert.equal(removed.status(), 204);
      const remaining = await (await context.request.get(`${origin}/api/calls/${call.id}/shares`)).json();
      assert.ok(!remaining.shares.some(s => s.id === shareId));
      await writeFile('ephemeral/sharing-hosted-proof.json', JSON.stringify({ origin, outside_domain_share_status: 201, browser_screenshot: 'screenshots/sharing-any-email-hosted.png', temporary_grant_removed: true }, null, 2) + '\n');
      console.log('Temporary proof grant removed.');
    }
  }
  await browser.close();
}
