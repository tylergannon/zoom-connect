import { chromium } from '../web/node_modules/playwright/index.mjs';
import { mkdir, writeFile } from 'node:fs/promises';
import assert from 'node:assert/strict';

const browser = await chromium.launch({ headless: true });
try {
  const context = await browser.newContext({ viewport: { width: 1360, height: 1000 } });
  const origin = 'http://127.0.0.1:8093';
  const login = await context.request.post(`${origin}/api/demo/login`, { data: { email: 'dana@acme.co' } });
  assert.equal(login.status(), 200);
  const created = await context.request.post(`${origin}/api/demo/calls`, { data: { topic: 'External email sharing proof', source: 'zoom' } });
  assert.equal(created.status(), 201);
  const call = await created.json();
  const page = await context.newPage();
  await page.goto(`${origin}/calls/${call.id}`);
  await page.getByRole('button', { name: 'Share', exact: true }).click();
  await page.getByRole('textbox', { name: 'Find a person or enter an email' }).fill('outside+call@example.net');
  const suggestion = page.getByRole('button').filter({ hasText: 'outside+call@example.net' });
  await suggestion.waitFor();
  await suggestion.click();
  await page.getByText('Shared by Dana Kim', { exact: true }).waitFor();
  const dialog = page.getByRole('dialog');
  assert.equal(await dialog.getByText('Sharing failed', { exact: true }).count(), 0);
  await mkdir('ephemeral/screenshots', { recursive: true });
  await page.screenshot({ path: 'ephemeral/screenshots/sharing-any-email.png' });
  const shares = await context.request.get(`${origin}/api/calls/${call.id}/shares`);
  const access = await shares.json();
  assert.equal(access.shares[0].email, 'outside+call@example.net');
  const anonymous = await browser.newContext();
  assert.equal((await anonymous.request.get(`${origin}/api/calls/${call.id}`)).status(), 401);
  await writeFile('ephemeral/sharing-proof.json', JSON.stringify({ origin, call_id: call.id, external_email: access.shares[0].email, share_created_through_browser: true, anonymous_status: 401, screenshot: 'screenshots/sharing-any-email.png' }, null, 2) + '\n');
  console.log('Browser proof passed: outside email added, share visible, anonymous access denied.');
} finally {
  await browser.close();
}
