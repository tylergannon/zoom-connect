// Browser regression: a viewer's in-flight access recheck for artifact A must
// not overwrite artifact B after a client-side link navigation from A to B.
// A loopback proxy drops A's stream, holds the recheck read for A, and only
// releases it once B is showing.
//
//   ZC_BIN=/tmp/zc-embedded/zoom-connect \
//   DATABASE_URL='postgres://zoom:zoom@127.0.0.1:56996/zoom_connect_artifacts_demo?sslmode=disable' \
//   node scripts/artifact-route-race-proof.mjs [chromium|firefox|webkit]
//
// Only PATH, HOME and DATABASE_URL reach the server; no real credentials are used.
import { spawn } from 'node:child_process';
import http from 'node:http';
import { chromium, firefox, webkit } from '../web/node_modules/playwright/index.mjs';

const engines = { chromium, firefox, webkit };
const engineName = process.argv[2] || 'chromium';
const bin = process.env.ZC_BIN, dsn = process.env.DATABASE_URL;
if (!bin || !dsn || !engines[engineName]) throw new Error('set ZC_BIN and DATABASE_URL; engine must be chromium, firefox or webkit');
const port = 57190, proxyPort = 57193;
const origin = `http://127.0.0.1:${port}`, proxyOrigin = `http://127.0.0.1:${proxyPort}`;
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
let failed = 0;
function check(name, ok, detail = '') {
	if (!ok) failed++;
	console.log(`${ok ? 'PASS' : 'FAIL'} ${name}${detail ? ` - ${detail}` : ''}`);
}

// holdRead is A's plain read path; failStream is A's stream path prefix.
let holdRead = '', failStream = '';
const held = [], proxied = new Set();
const forward = (req, res) => {
	const upstream = http.request({ host: '127.0.0.1', port, path: req.url, method: req.method, headers: req.headers }, (up) => { res.writeHead(up.statusCode, up.headers); up.pipe(res); });
	upstream.on('error', () => { if (!res.headersSent) res.writeHead(502); res.end(); });
	proxied.add(res);
	res.on('close', () => proxied.delete(res));
	req.pipe(upstream);
};
const proxy = http.createServer((req, res) => {
	if (failStream && req.url.startsWith(failStream)) { res.writeHead(502); res.end('bad gateway'); return; }
	if (holdRead && req.url === holdRead) { held.push(() => forward(req, res)); return; }
	forward(req, res);
});
await new Promise((resolve) => proxy.listen(proxyPort, '127.0.0.1', resolve));

const server = spawn(bin, ['-listen', `127.0.0.1:${port}`, '-origin', origin], { env: { PATH: process.env.PATH, HOME: process.env.HOME, DATABASE_URL: dsn, GOMAXPROCS: '2' }, stdio: 'ignore' });
let browser;
try {
	for (let i = 0; ; i++) {
		try { if ((await fetch(`${origin}/healthz`)).ok) break; } catch { /* still starting */ }
		if (i > 100) throw new Error('server did not start');
		await sleep(100);
	}
	browser = await engines[engineName].launch({ headless: true });
	const context = await browser.newContext();
	const login = await context.request.post(`${origin}/api/demo/login`, { data: { email: 'dana@acme.co' } });
	if (!login.ok()) throw new Error(`demo login failed: ${login.status()}`);
	const call = await (await context.request.post(`${origin}/api/demo/calls`, { data: { topic: 'Artifact route race proof', source: 'zoom' } })).json();
	const { secret } = await (await context.request.post(`${origin}/api/keys`, { data: { name: 'meeting-agent' } })).json();
	const agent = async (method, path, body) => (await fetch(origin + path, { method, headers: { authorization: `Bearer ${secret}`, 'content-type': 'application/json' }, body: JSON.stringify(body) })).json();
	const artifacts = `/api/calls/${call.id}/artifacts`;
	// A is ahead of B, so a stale A snapshot would win a plain version comparison.
	const a = await agent('POST', artifacts, { title: 'Artifact A', html: '<h1 id="headline">Artifact A v1</h1>' });
	for (let v = 1; v < 5; v++) await agent('PUT', `${artifacts}/${a.id}`, { base_version: v, html: `<h1 id="headline">Artifact A v${v + 1}</h1>` });
	const b = await agent('POST', artifacts, { title: 'Artifact B', html: '<h1 id="headline">Artifact B v1</h1>' });

	const page = await context.newPage();
	const headline = page.frameLocator('.artifact-frame iframe').locator('#headline');
	const badge = page.getByTestId('artifact-version');
	await page.goto(proxyOrigin + a.viewer_url);
	await headline.filter({ hasText: 'Artifact A v5' }).waitFor({ timeout: 10000 });
	await page.locator('.stream-state[data-state="live"]').waitFor({ timeout: 10000 });
	await page.evaluate(() => { window.__sameDocument = 'yes'; });

	// Drop A's stream. The viewer's error handler rechecks A with a plain read,
	// which the proxy holds.
	holdRead = `${artifacts}/${a.id}`;
	failStream = `${artifacts}/${a.id}/stream`;
	for (const res of proxied) res.destroy();
	for (let i = 0; i < 100 && held.length === 0; i++) await sleep(100);
	check('recheck read for A is held', held.length === 1, `held reads: ${held.length}`);

	// Select B in the call's page list, which the app's router handles in place.
	await page.locator(`.page-row[data-artifact-id="${b.id}"]`).click();
	await headline.filter({ hasText: 'Artifact B v1' }).waitFor({ timeout: 10000 });
	const sameDocument = await page.evaluate(() => window.__sameDocument);
	check('B shown after client-side navigation', new URL(page.url()).pathname === b.viewer_url && sameDocument === 'yes', `path ${new URL(page.url()).pathname}, same document: ${sameDocument}`);

	// Release the stale read for A and give the viewer time to apply it.
	holdRead = '';
	held.splice(0).forEach((release) => release());
	await sleep(2000);
	const text = await headline.textContent({ timeout: 2000 }).catch(() => null);
	const version = (await badge.textContent({ timeout: 2000 }).catch(() => null))?.trim();
	check('B content kept after stale A read', text === 'Artifact B v1', `headline: ${text}`);
	check('B version kept after stale A read', version === 'Version 1', `badge: ${version}`);
	const title = (await page.getByTestId('artifact-title').textContent({ timeout: 2000 }).catch(() => null))?.trim();
	const active = (await page.locator('.page-row.active b').textContent({ timeout: 2000 }).catch(() => null))?.trim();
	check('pane title and selected row are B', title === 'Artifact B' && active === 'Artifact B', `title: ${title}, selected: ${active}`);
} finally {
	await browser?.close();
	server.kill('SIGKILL');
	proxy.close();
}
console.log(`${failed === 0 ? 'ALL PASS' : `${failed} FAILED`} (${engineName})`);
process.exitCode = failed === 0 ? 0 : 1;
