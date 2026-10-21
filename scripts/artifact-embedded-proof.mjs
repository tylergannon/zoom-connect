// Headless browser proof for pages embedded in the call view: layout, page
// selection, sorting, version history, Present/Exit, native fullscreen and the
// window fallback, reconnect, history across a long disconnect, deep links,
// moving between calls, calls without pages, the agent guide, and desktop and
// phone layouts in light and dark. It runs one loopback server on a scratch
// database with demo sign-in and a fake agent key.
//
//   ZC_BIN=/tmp/zc-embedded/zoom-connect \
//   DATABASE_URL='postgres://zoom:zoom@127.0.0.1:PORT/scratch_db?sslmode=disable' \
//   node scripts/artifact-embedded-proof.mjs [chromium|firefox|webkit] [screenshot-dir]
//
// Only PATH, HOME and DATABASE_URL reach the server; no real credentials are used.
import { spawn } from 'node:child_process';
import http from 'node:http';
import { mkdirSync } from 'node:fs';
import { chromium, firefox, webkit } from '../web/node_modules/playwright/index.mjs';

const engines = { chromium, firefox, webkit };
const engineName = process.argv[2] || 'chromium';
const shots = process.argv[3] || '/tmp/zc-embedded';
const bin = process.env.ZC_BIN, dsn = process.env.DATABASE_URL;
if (!bin || !dsn || !engines[engineName]) throw new Error('set ZC_BIN and DATABASE_URL; engine must be chromium, firefox or webkit');
mkdirSync(shots, { recursive: true });
const port = 57190, origin = `http://127.0.0.1:${port}`;
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
let failed = 0;
function check(name, ok, detail = '') {
	if (!ok) failed++;
	console.log(`${ok ? 'PASS' : 'FAIL'} ${name}${detail ? ` - ${detail}` : ''}`);
}

let server;
async function startServer() {
	server = spawn(bin, ['-listen', `127.0.0.1:${port}`, '-origin', origin], { env: { PATH: process.env.PATH, HOME: process.env.HOME, DATABASE_URL: dsn, GOMAXPROCS: '2' }, stdio: 'ignore' });
	for (let i = 0; i < 100; i++) {
		try { if ((await fetch(`${origin}/healthz`)).ok) return; } catch { /* still starting */ }
		await sleep(100);
	}
	throw new Error('server did not start');
}
async function stopServer() {
	if (!server || server.exitCode !== null) return;
	const exited = new Promise((resolve) => server.once('exit', resolve));
	server.kill('SIGKILL');
	await exited;
}

let browser;
try {
	await startServer();
	browser = await engines[engineName].launch({ headless: true });
	const context = await browser.newContext({ viewport: { width: 1360, height: 900 } });
	const popups = [];
	context.on('page', (p) => popups.push(p));
	const login = await context.request.post(`${origin}/api/demo/login`, { data: { email: 'dana@acme.co' } });
	if (!login.ok()) throw new Error(`demo login failed: ${login.status()}`);
	const call = await (await context.request.post(`${origin}/api/demo/calls`, { data: { topic: 'Embedded pages proof', source: 'zoom' } })).json();
	const { secret } = await (await context.request.post(`${origin}/api/keys`, { data: { name: 'meeting-agent' } })).json();
	const agent = async (method, path, body) => {
		const r = await fetch(origin + path, { method, headers: { authorization: `Bearer ${secret}`, 'content-type': 'application/json' }, body: JSON.stringify(body) });
		return { status: r.status, body: await r.json().catch(() => null) };
	};
	const artifacts = `/api/calls/${call.id}/artifacts`;
	const page1 = (title, text) => `<h1 id="headline">${text}</h1><p>${title}</p>`;
	const versions = {};
	const create = async (title) => { const r = await agent('POST', artifacts, { title, html: page1(title, `${title} v1`) }); versions[r.body.id] = 1; await sleep(30); return r.body; };
	const update = async (art, text) => {
		const r = await agent('PUT', `${artifacts}/${art.id}`, { base_version: versions[art.id], html: page1(art.title, text ?? `${art.title} v${versions[art.id] + 1}`) });
		if (r.status !== 200) throw new Error(`update ${art.title}: ${r.status} ${JSON.stringify(r.body)}`);
		versions[art.id] = r.body.version;
		return r.body;
	};
	// Created Zeta, Agenda, Metrics; Zeta updated last. The three sort orders differ.
	const zeta = await create('Zeta notes'), agenda = await create('Agenda'), metrics = await create('Metrics');
	await update(zeta);

	const page = await context.newPage();
	const streams = [];
	page.on('request', (r) => { if (r.url().includes('/artifacts/') && r.url().includes('/stream')) streams.push(r.url()); });
	const frame = page.frameLocator('.artifact-frame iframe');
	const headline = frame.locator('#headline');
	const pane = page.getByTestId('artifact-pane');
	const badge = page.getByTestId('artifact-version');
	const text = async (locator) => (await locator.textContent({ timeout: 3000 }).catch(() => null))?.trim();
	const rows = async () => page.locator('.page-row b').allTextContents();
	// App chrome of the redesigned call view: main navigation, the call's
	// heading, and the active call-content tab and its search field.
	const appNav = 'nav[aria-label="Main"]';
	const callHeading = 'main h1';
	const activeTab = '[aria-label="Call content"] [role="tab"][aria-selected="true"]';
	const chatSearch = 'input[type="search"][placeholder="Search chat"]';

	// Embedded layout: app navigation, call context and the page pane together.
	await page.goto(`${origin}/calls/${call.id}`);
	await headline.filter({ hasText: 'Zeta notes v2' }).waitFor({ timeout: 10000 });
	await page.locator('.stream-state[data-state="live"]').waitFor({ timeout: 10000 });
	const visible = async (sel) => page.locator(sel).first().isVisible();
	check('call view shows app navigation, call header, transcript and pages', await visible(appNav) && (await text(page.locator(callHeading))) === 'Embedded pages proof' && await page.getByLabel('Search transcript').isVisible() && await visible('[data-testid="call-artifacts"] .artifact-frame iframe'));
	check('no page in the URL shows the most recently updated page, latest version', (await text(badge)) === 'Version 2' && (await page.locator('.page-row.active b').textContent()) === 'Zeta notes');
	await page.screenshot({ path: `${shots}/embedded-normal-${engineName}.png` });

	// Sorting and filtering the list.
	check('default sort is recently updated', (await rows()).join('|') === 'Zeta notes|Metrics|Agenda', (await rows()).join('|'));
	await page.getByRole('button', { name: 'Title', exact: true }).click();
	check('title sort', (await rows()).join('|') === 'Agenda|Metrics|Zeta notes', (await rows()).join('|'));
	await page.getByRole('button', { name: 'Newest', exact: true }).click();
	check('newest-created sort', (await rows()).join('|') === 'Metrics|Agenda|Zeta notes', (await rows()).join('|'));
	await page.getByLabel('Filter pages').fill('met');
	check('title filter', (await rows()).join('|') === 'Metrics', (await rows()).join('|'));
	await page.getByLabel('Filter pages').fill('');
	await page.getByRole('button', { name: 'Recently updated' }).click();

	// Call context the viewer set up before selecting and presenting.
	await page.getByRole('tab', { name: /^Chat/ }).click();
	await page.getByLabel('Search chat').fill('budget');
	await page.evaluate(() => { window.__sameDocument = 'yes'; });

	// Selecting a page is in-app navigation: no new tab, no reload, no jump.
	const metricsRow = page.locator(`.page-row[data-artifact-id="${metrics.id}"]`);
	// Firefox scrolls the just-filled search field into view a moment later.
	await sleep(500);
	await page.evaluate(() => window.scrollTo(0, 60));
	await sleep(300);
	const scrollBeforeSelect = await page.evaluate(() => window.scrollY);
	await metricsRow.click();
	await headline.filter({ hasText: 'Metrics v1' }).waitFor({ timeout: 10000 });
	await sleep(200);
	const scrollAfterSelect = await page.evaluate(() => window.scrollY);
	check('selecting a page keeps the scroll position', scrollBeforeSelect > 0 && Math.abs(scrollAfterSelect - scrollBeforeSelect) < 2, `${scrollBeforeSelect} -> ${scrollAfterSelect}`);
	const pathAfterSelect = new URL(page.url()).pathname;
	check('selecting a page stays in the same document and tab', popups.length === 1 && await page.evaluate(() => window.__sameDocument) === 'yes' && pathAfterSelect === metrics.viewer_url, `popups ${popups.length - 1}, path ${pathAfterSelect}`);
	const kept = await page.evaluate(([tabSel, searchSel]) => ({ tab: document.querySelector(tabSel)?.textContent?.trim(), search: document.querySelector(searchSel)?.value, focus: document.activeElement?.closest('.page-row')?.getAttribute('data-artifact-id') ?? document.activeElement?.tagName }), [activeTab, chatSearch]);
	check('selection keeps the call tab, search and focus', kept.tab?.startsWith('Chat') && kept.search === 'budget' && kept.focus === metrics.id, JSON.stringify(kept));

	// Two live updates in the embedded pane; the selection survives reordering.
	for (let i = 0; i < 2; i++) {
		const started = Date.now();
		const next = await update(metrics);
		await headline.filter({ hasText: `Metrics v${next.version}` }).waitFor({ timeout: 5000 });
		check(`embedded update to v${next.version} renders without reload`, await page.evaluate(() => window.__sameDocument) === 'yes', `${Date.now() - started} ms after PUT`);
	}
	await page.waitForFunction(() => document.querySelector('.page-row b')?.textContent === 'Metrics', null, { timeout: 15000 }).catch(() => {});
	check('updated page moves up the list and stays selected', (await rows())[0] === 'Metrics' && (await page.locator('.page-row.active b').textContent()) === 'Metrics', (await rows()).join('|'));

	// History: pin an older version; live updates do not replace it.
	await page.getByRole('button', { name: 'History' }).click();
	const history = page.getByTestId('artifact-history');
	await history.locator('.history-row[data-version="1"]').waitFor({ timeout: 5000 });
	check('history lists every version newest first', (await history.locator('.history-row').evaluateAll((els) => els.map((e) => e.dataset.version))).join(',') === '3,2,1');
	await history.locator('.history-row[data-version="1"]').click();
	await headline.filter({ hasText: 'Metrics v1' }).waitFor({ timeout: 5000 });
	check('older version is labeled and shown', (await text(badge)) === 'Version 1' && (await text(page.getByTestId('older-revision')))?.includes('latest is v3'));
	await update(metrics);
	await page.getByTestId('older-revision').filter({ hasText: 'latest is v4' }).waitFor({ timeout: 5000 });
	await history.locator('.history-row[data-version="4"]').waitFor({ timeout: 5000 });
	check('a live update keeps the pinned version on screen', (await text(headline)) === 'Metrics v1' && (await text(badge)) === 'Version 1');
	await page.screenshot({ path: `${shots}/embedded-history-${engineName}.png` });
	await page.getByTestId('return-to-live').click();
	await headline.filter({ hasText: 'Metrics v4' }).waitFor({ timeout: 5000 });
	check('return to live shows the latest version', (await text(badge)) === 'Version 4' && await page.getByTestId('older-revision').count() === 0);
	await page.getByRole('button', { name: 'History' }).click();

	// Present and exit repeatedly. The same frame and stream are kept, and the
	// call view comes back exactly as it was.
	await page.evaluate(() => { document.querySelector('.artifact-frame iframe').__mark = 'same-frame'; window.scrollTo(0, 120); });
	const scrollBefore = await page.evaluate(() => window.scrollY);
	const streamsBefore = streams.length;
	const presentButton = page.locator('[data-present]');
	const modes = [];
	for (const [round, exitBy] of [[1, 'Escape'], [2, 'button'], [3, 'Escape']]) {
		await presentButton.click();
		await page.locator('[data-presenting]').waitFor({ timeout: 5000 });
		await sleep(300);
		const mode = await pane.getAttribute('data-presenting');
		modes.push(mode);
		const box = await pane.boundingBox();
		const view = await page.evaluate(() => ({ w: window.innerWidth, h: window.innerHeight }));
		const exitFocused = await page.evaluate(() => document.activeElement?.hasAttribute('data-exit'));
		// "fullscreen" must mean the browser made the app-owned pane the
		// fullscreen element; "window" means no element is fullscreen.
		const owner = await page.evaluate(() => { const el = document.fullscreenElement; return el ? (el.matches('[data-testid="artifact-pane"]') ? 'pane' : el.tagName) : 'none'; });
		check(`present ${round} fullscreen owner matches the mode`, mode === 'fullscreen' ? owner === 'pane' : mode === 'window' && owner === 'none', `${mode}, fullscreen element ${owner}`);
		check(`present ${round} fills the screen with app-owned controls (${mode})`, box && Math.abs(box.width - view.w) < 2 && Math.abs(box.height - view.h) < 2 && exitFocused && (await text(page.getByTestId('artifact-title'))) === 'Metrics' && await page.locator('[data-presenting] .stream-state').isVisible(), `pane ${box?.width}x${box?.height} viewport ${view.w}x${view.h}, exit focused ${exitFocused}`);
		if (round === 1) {
			for (let i = 0; i < 2; i++) {
				const next = await update(metrics);
				await headline.filter({ hasText: `Metrics v${next.version}` }).waitFor({ timeout: 5000 });
				check(`update to v${next.version} renders while presenting`, (await text(badge)) === `Version ${next.version}`);
			}
			await page.screenshot({ path: `${shots}/embedded-present-${engineName}.png` });
		}
		if (exitBy === 'Escape') await page.keyboard.press('Escape');
		else await page.locator('[data-exit]').click();
		await page.waitForFunction(() => !document.querySelector('[data-presenting]') && !document.fullscreenElement, null, { timeout: 5000 }).catch(() => {});
		await sleep(200);
		const restored = await page.evaluate(([id, tabSel, searchSel]) => ({
			presenting: !!document.querySelector('[data-presenting]'),
			fullscreen: !!document.fullscreenElement,
			bodyLocked: document.body.classList.contains('artifact-presenting'),
			sameFrame: document.querySelector('.artifact-frame iframe')?.__mark === 'same-frame',
			focus: document.activeElement?.hasAttribute('data-present'),
			scroll: window.scrollY,
			selected: document.querySelector('.page-row.active')?.getAttribute('data-artifact-id') === id,
			path: location.pathname,
			search: document.querySelector(searchSel)?.value,
			tab: document.querySelector(tabSel)?.textContent?.trim(),
			sameDocument: window.__sameDocument,
		}), [metrics.id, activeTab, chatSearch]);
		check(`exit ${round} by ${exitBy} restores the call view`, !restored.presenting && !restored.fullscreen && !restored.bodyLocked && restored.sameFrame && restored.focus && Math.abs(restored.scroll - scrollBefore) < 2 && restored.selected && restored.path === metrics.viewer_url && restored.search === 'budget' && restored.tab?.startsWith('Chat') && restored.sameDocument === 'yes', JSON.stringify(restored));
	}
	check('present and exit opened no extra stream', streams.length === streamsBefore, `artifact streams opened during toggles: ${streams.length - streamsBefore}`);
	await page.screenshot({ path: `${shots}/embedded-restored-${engineName}.png` });

	// Reconnect: the server restarts and the pane catches up to the latest.
	await stopServer();
	await page.locator('.stream-state[data-state="reconnecting"]').waitFor({ timeout: 10000 });
	await startServer();
	const afterRestart = await update(metrics);
	await headline.filter({ hasText: `Metrics v${afterRestart.version}` }).waitFor({ timeout: 20000 }).catch(() => {});
	await page.locator('.stream-state[data-state="live"]').waitFor({ timeout: 20000 }).catch(() => {});
	check('pane reconnects after a server restart and shows the latest', (await text(headline)) === `Metrics v${afterRestart.version}` && await page.evaluate(() => window.__sameDocument) === 'yes', `headline ${await text(headline)}`);

	// Fullscreen refused: Present still covers the window and exits cleanly.
	const blocked = await context.newPage();
	await blocked.addInitScript(() => { Element.prototype.requestFullscreen = function () { return Promise.reject(new TypeError('blocked for this test')); }; });
	await blocked.goto(`${origin}${agenda.viewer_url}`);
	await blocked.frameLocator('.artifact-frame iframe').locator('#headline').filter({ hasText: 'Agenda v1' }).waitFor({ timeout: 10000 });
	await blocked.locator('[data-present]').click();
	await blocked.locator('[data-presenting="window"]').waitFor({ timeout: 5000 });
	const fbBox = await blocked.getByTestId('artifact-pane').boundingBox();
	check('refused fullscreen falls back to a window-filling pane', fbBox && Math.abs(fbBox.width - 1360) < 2 && Math.abs(fbBox.height - 900) < 2 && await blocked.locator('[data-exit]').isVisible(), `pane ${fbBox?.width}x${fbBox?.height}`);
	await blocked.screenshot({ path: `${shots}/embedded-present-fallback-${engineName}.png` });
	await blocked.keyboard.press('Escape');
	await blocked.waitForFunction(() => !document.querySelector('[data-presenting]') && document.activeElement?.hasAttribute('data-present'), null, { timeout: 5000 }).catch(() => {});
	check('Escape leaves the fallback and restores focus', await blocked.evaluate(() => !document.querySelector('[data-presenting]') && !document.body.classList.contains('artifact-presenting') && document.activeElement?.hasAttribute('data-present')));

	// A deep link opens the call view with that page selected.
	check('deep link selects the page inside the call view', (await text(blocked.locator(callHeading))) === 'Embedded pages proof' && (await blocked.locator('.page-row.active b').textContent()) === 'Agenda' && await blocked.locator(appNav).isVisible());
	check('mode used while presenting', true, modes.join(', '));
	// Each call view holds a few event streams; close finished pages so the
	// browser's per-host connection limit is not reached.
	await blocked.close();
	await page.close();

	// A fullscreen grant that arrives after Exit is handed back at once.
	const late = await context.newPage();
	await late.addInitScript(() => {
		const original = Element.prototype.requestFullscreen;
		window.__lateGrant = 'not requested';
		Element.prototype.requestFullscreen = function (...args) {
			window.__lateGrant = 'pending';
			return new Promise((resolve, reject) => setTimeout(() => original.apply(this, args).then(() => { window.__lateGrant = 'granted'; resolve(); }, (e) => { window.__lateGrant = 'refused'; reject(e); }), 500));
		};
	});
	await late.goto(`${origin}${agenda.viewer_url}`);
	await late.frameLocator('.artifact-frame iframe').locator('#headline').filter({ hasText: 'Agenda v1' }).waitFor({ timeout: 10000 });
	await late.locator('[data-present]').click();
	await late.locator('[data-exit]').click();
	await late.waitForFunction(() => window.__lateGrant !== 'pending', null, { timeout: 5000 }).catch(() => {});
	await sleep(500);
	const afterLate = await late.evaluate(() => ({ grant: window.__lateGrant, fullscreen: !!document.fullscreenElement, presenting: !!document.querySelector('[data-presenting]'), bodyLocked: document.body.classList.contains('artifact-presenting') }));
	check('a fullscreen grant after Exit does not leave the page fullscreen', !afterLate.fullscreen && !afterLate.presenting && !afterLate.bodyLocked, JSON.stringify(afterLate));
	await late.close();

	// Moving to another call inside the app starts a fresh call view: the old
	// call's streams close and its tab, search and page selection do not carry
	// over. Every EventSource the page opens is recorded to check that.
	const other = await (await context.request.post(`${origin}/api/demo/calls`, { data: { topic: 'Second call', source: 'zoom' } })).json();
	const board = (await agent('POST', `/api/calls/${other.id}/artifacts`, { title: 'Board', html: page1('Board', 'Board v1') })).body;
	const nav = await context.newPage();
	await nav.addInitScript(() => {
		const Original = window.EventSource;
		window.__sources = [];
		window.EventSource = class extends Original { constructor(...args) { super(...args); window.__sources.push(this); } };
	});
	const navHeadline = nav.frameLocator('.artifact-frame iframe').locator('#headline');
	await nav.goto(`${origin}${metrics.viewer_url}`);
	await navHeadline.filter({ hasText: 'Metrics v' }).waitFor({ timeout: 10000 });
	await nav.locator('.stream-state[data-state="live"]').waitFor({ timeout: 10000 });
	await nav.getByRole('tab', { name: /^Chat/ }).click();
	await nav.getByLabel('Search chat').fill('budget');
	await nav.evaluate((href) => { window.__sameDocument = 'yes'; const a = document.createElement('a'); a.href = href; a.id = 'to-other-call'; a.textContent = 'Second call'; document.querySelector('main').append(a); }, `/calls/${other.id}`);
	await nav.locator('#to-other-call').click();
	await navHeadline.filter({ hasText: 'Board v1' }).waitFor({ timeout: 10000 }).catch(() => {});
	await nav.locator('.stream-state[data-state="live"]').waitFor({ timeout: 10000 }).catch(() => {});
	const switched = await nav.evaluate(([firstCall, secondCall, tabSel]) => ({
		sameDocument: window.__sameDocument,
		heading: document.querySelector('main h1')?.textContent?.trim(),
		tab: document.querySelector(tabSel)?.textContent?.trim(),
		search: document.querySelector('input[type="search"][placeholder^="Search"]')?.value,
		rows: [...document.querySelectorAll('.page-row b')].map((b) => b.textContent),
		oldOpen: window.__sources.filter((s) => s.url.includes(firstCall) && s.readyState !== 2).map((s) => new URL(s.url).pathname),
		newOpen: window.__sources.filter((s) => s.url.includes(secondCall) && s.readyState !== 2).length,
	}), [call.id, other.id, activeTab]);
	check('moving to another call in the app resets the call view', switched.sameDocument === 'yes' && switched.heading === 'Second call' && switched.tab === 'Transcript' && switched.search === '' && switched.rows.join('|') === 'Board' && (await text(navHeadline)) === 'Board v1', JSON.stringify({ ...switched, oldOpen: undefined }));
	check('the previous call’s streams are closed', switched.oldOpen.length === 0 && switched.newOpen >= 2, `old still open: ${switched.oldOpen.join(', ') || 'none'}; new open ${switched.newOpen}`);
	// The old call's list refresh (every 10 s) must not keep running either.
	const lateRequests = [];
	nav.on('request', (r) => { if (r.url().includes(call.id)) lateRequests.push(new URL(r.url()).pathname); });
	await update(metrics);
	await sleep(11000);
	check('nothing from the previous call keeps polling or updating', lateRequests.length === 0 && (await text(navHeadline)) === 'Board v1', lateRequests.join(', ') || 'no requests');
	await nav.goBack();
	await navHeadline.filter({ hasText: 'Metrics v' }).waitFor({ timeout: 10000 }).catch(() => {});
	check('going back returns to the first call with its page selected', (await text(nav.locator(callHeading))) === 'Embedded pages proof' && (await nav.locator('.page-row.active b').textContent()) === 'Metrics' && new URL(nav.url()).pathname === metrics.viewer_url);
	await nav.close();

	// A call without pages: an open call says none yet and links the guide,
	// then shows a page once an agent publishes one; an ended call shows nothing.
	const quiet = await (await context.request.post(`${origin}/api/demo/calls`, { data: { topic: 'Call without pages', source: 'zoom' } })).json();
	const qp = await context.newPage();
	await qp.goto(`${origin}/calls/${quiet.id}`);
	await qp.getByTestId('call-artifacts-empty').waitFor({ timeout: 10000 }).catch(() => {});
	const guideLink = qp.getByTestId('call-artifacts-empty').getByRole('link', { name: 'How agents publish pages' });
	check('an open call with no pages says so and links the agent guide', await qp.getByTestId('call-artifacts-empty').isVisible() && (await guideLink.getAttribute('href')) === '/agent-api#pages' && await qp.getByTestId('call-artifacts').count() === 0 && await qp.getByLabel('Search transcript').isVisible() && await qp.locator('.artifact-frame').count() === 0);
	await qp.screenshot({ path: `${shots}/embedded-empty-${engineName}.png` });
	await agent('POST', `/api/calls/${quiet.id}/artifacts`, { title: 'First page', html: page1('First page', 'First page v1') });
	await qp.frameLocator('.artifact-frame iframe').locator('#headline').filter({ hasText: 'First page v1' }).waitFor({ timeout: 15000 }).catch(() => {});
	check('a page published while the call is open appears without a reload', (await text(qp.frameLocator('.artifact-frame iframe').locator('#headline'))) === 'First page v1' && await qp.getByTestId('call-artifacts-empty').count() === 0);
	await qp.goto(`${origin}/agent-api#pages`);
	const guide = qp.locator('section[aria-labelledby="pages"]');
	const guideText = (await guide.textContent({ timeout: 5000 }).catch(() => '')) ?? '';
	check('the agent guide shows create, update and history examples', await qp.getByRole('heading', { name: /Publish a live page/ }).isVisible() && guideText.includes(`-d '{"title":"Live agenda"`) && guideText.includes('"base_version":1') && guideText.includes('/artifacts/PAGE_ID/versions') && guideText.includes('next_before'));
	await qp.close();
	const ended = await (await context.request.post(`${origin}/api/demo/calls`, { data: { topic: 'Ended call without pages', source: 'zoom' } })).json();
	await context.request.post(`${origin}/api/demo/calls/${ended.id}/stop`, { data: {} });
	const ep = await context.newPage();
	await ep.goto(`${origin}/calls/${ended.id}`);
	await ep.locator(callHeading).filter({ hasText: 'Ended call without pages' }).waitFor({ timeout: 10000 });
	await ep.getByLabel('Search transcript').waitFor({ timeout: 5000 }).catch(() => {});
	await sleep(500);
	check('an ended call with no pages shows the transcript and no Pages section', await ep.getByLabel('Search transcript').isVisible() && await ep.getByTestId('call-artifacts').count() === 0 && await ep.getByTestId('call-artifacts-empty').count() === 0);
	await ep.close();

	// Layout at desktop and phone widths, light and dark, with history open.
	for (const [label, viewport] of [['desktop', { width: 1360, height: 900 }], ['mobile', { width: 390, height: 844 }]]) {
		for (const theme of ['light', 'dark']) {
			const lc = await browser.newContext({ viewport, storageState: await context.storageState() });
			await lc.addInitScript((t) => localStorage.setItem('zoom-connect-theme', t), theme);
			const lp = await lc.newPage();
			await lp.goto(`${origin}${metrics.viewer_url}`);
			await lp.frameLocator('.artifact-frame iframe').locator('#headline').filter({ hasText: 'Metrics v' }).waitFor({ timeout: 10000 });
			await lp.getByRole('button', { name: 'History', exact: true }).click();
			await lp.locator('.history-row[data-version="1"]').waitFor({ timeout: 5000 });
			await sleep(300);
			const fit = await lp.evaluate(() => ({ dark: document.documentElement.classList.contains('dark'), overflow: document.documentElement.scrollWidth - window.innerWidth, frame: document.querySelector('.artifact-frame')?.getBoundingClientRect().width ?? 0 }));
			check(`${label} ${theme} layout fits the viewport`, fit.dark === (theme === 'dark') && fit.overflow <= 0 && fit.frame >= (label === 'mobile' ? 300 : 700), JSON.stringify(fit));
			await lp.locator('[data-testid="call-artifacts"]').screenshot({ path: `${shots}/layout-${label}-${theme}-${engineName}.png` });
			await lp.screenshot({ path: `${shots}/layout-${label}-${theme}-page-${engineName}.png`, fullPage: true });
			if (label === 'mobile' && theme === 'light') {
				await lp.locator('[data-present]').click();
				await lp.locator('[data-presenting]').waitFor({ timeout: 5000 });
				await sleep(300);
				// Native fullscreen can resize the window to the screen, so the
				// pane is compared with the window as it is while presenting.
				const box = await lp.getByTestId('artifact-pane').boundingBox();
				const win = await lp.evaluate(() => ({ w: window.innerWidth, h: window.innerHeight, mode: document.querySelector('[data-presenting]')?.dataset.presenting }));
				check('present at phone width covers the screen with Exit visible', box && Math.abs(box.width - win.w) < 2 && Math.abs(box.height - win.h) < 2 && await lp.locator('[data-exit]').isVisible(), `pane ${box?.width}x${box?.height}, window ${win.w}x${win.h}, ${win.mode}`);
				await lp.screenshot({ path: `${shots}/layout-mobile-present-${engineName}.png` });
				await lp.keyboard.press('Escape');
			}
			await lc.close();
		}
	}

	// History across a disconnect longer than one history page. A loopback
	// proxy drops the page's stream and blocks its plain reads while the agent
	// publishes 60 updates; every stored version must stay reachable.
	let offline = false, longPath = '', holdPath = '', held = null;
	const proxied = new Set();
	const proxy = http.createServer((req, res) => {
		const path = req.url?.split('?')[0];
		const stream = longPath && path === `${longPath}/stream`;
		if (offline && longPath && (path === longPath || stream)) { res.destroy(); return; }
		const upstream = http.request(origin + req.url, { method: req.method, headers: { ...req.headers, host: `127.0.0.1:${port}` } }, (response) => {
			if (holdPath && req.url === holdPath) {
				// Hold one complete response until the test releases it.
				holdPath = '';
				let body = '';
				response.on('data', (part) => { body += part; });
				response.on('end', () => { held = { res, status: response.statusCode, headers: response.headers, body }; });
				return;
			}
			res.writeHead(response.statusCode, response.headers);
			if (stream) proxied.add(res);
			response.pipe(res);
		});
		upstream.on('error', () => res.destroy());
		res.on('close', () => { proxied.delete(res); upstream.destroy(); });
		req.pipe(upstream);
	});
	await new Promise((resolve) => proxy.listen(0, '127.0.0.1', resolve));
	try {
		const proxyOrigin = `http://127.0.0.1:${proxy.address().port}`;
		const long = await create('Long history');
		longPath = `/api/calls/${call.id}/artifacts/${long.id}`;
		const lp = await context.newPage();
		const lpHeadline = lp.frameLocator('.artifact-frame iframe').locator('#headline');
		await lp.goto(`${proxyOrigin}${long.viewer_url}`);
		await lpHeadline.filter({ hasText: 'Long history v1' }).waitFor({ timeout: 10000 });
		await update(long);
		await lpHeadline.filter({ hasText: 'Long history v2' }).waitFor({ timeout: 5000 });
		await lp.getByRole('button', { name: 'History', exact: true }).click();
		await lp.locator('.history-row[data-version="1"]').waitFor({ timeout: 5000 });
		await lp.locator('.history-row[data-version="1"]').click();
		await lpHeadline.filter({ hasText: 'Long history v1' }).waitFor({ timeout: 5000 });
		await lp.locator('.stream-state[data-state="live"]').waitFor({ timeout: 5000 });
		offline = true;
		for (const r of proxied) r.destroy();
		await lp.locator('.stream-state[data-state="reconnecting"]').waitFor({ timeout: 10000 });
		while (versions[long.id] < 62) await update(long);
		offline = false;
		await lp.getByTestId('older-revision').filter({ hasText: 'latest is v62' }).waitFor({ timeout: 20000 }).catch(() => {});
		await lp.locator('.history-row[data-version="62"]').waitFor({ timeout: 10000 }).catch(() => {});
		check('after a long disconnect the pinned version stays and the latest is known', (await text(lpHeadline)) === 'Long history v1' && (await text(lp.getByTestId('artifact-version'))) === 'Version 1' && (await text(lp.getByTestId('older-revision')))?.includes('latest is v62'), await text(lp.getByTestId('older-revision')));
		const older = lp.getByRole('button', { name: 'Load older versions', exact: true });
		for (let i = 0; i < 5 && await older.isVisible(); i++) {
			const before = await lp.locator('.history-row').count();
			await older.click();
			await lp.waitForFunction((n) => document.querySelectorAll('.history-row').length !== n, before, { timeout: 5000 }).catch(() => {});
		}
		const listed = await lp.locator('.history-row').evaluateAll((els) => els.map((e) => Number(e.dataset.version)));
		const missing = Array.from({ length: 62 }, (_, i) => i + 1).filter((v) => !listed.includes(v));
		const ordered = listed.every((v, i) => i === 0 || listed[i - 1] > v);
		check('every stored version stays reachable after the reconnect', missing.length === 0 && ordered && listed.length === 62 && await lp.getByTestId('history-gap').count() === 0, missing.length ? `missing ${missing.join(',')}` : `${listed.length} versions listed`);
		await lp.screenshot({ path: `${shots}/embedded-history-reconnect-${engineName}.png`, fullPage: true });
		await lp.locator('.history-row[data-version="11"]').click({ timeout: 5000 }).catch(() => {});
		await lpHeadline.filter({ hasText: 'Long history v11' }).waitFor({ timeout: 5000 }).catch(() => {});
		check('a version from the recovered range opens as a preview', (await text(lpHeadline)) === 'Long history v11' && (await text(lp.getByTestId('artifact-version'))) === 'Version 11');
		await lp.getByTestId('return-to-live').click();
		await lpHeadline.filter({ hasText: 'Long history v62' }).waitFor({ timeout: 5000 }).catch(() => {});
		check('return to live after the reconnect shows the latest', (await text(lpHeadline)) === 'Long history v62');
		await lp.close();

		// A history listing read before an update must not hide that update.
		const inflight = await create('In-flight history');
		const ip = await context.newPage();
		const ipHeadline = ip.frameLocator('.artifact-frame iframe').locator('#headline');
		await ip.goto(`${proxyOrigin}${inflight.viewer_url}`);
		await ipHeadline.filter({ hasText: 'In-flight history v1' }).waitFor({ timeout: 10000 });
		holdPath = `/api/calls/${call.id}/artifacts/${inflight.id}/versions`;
		await ip.getByRole('button', { name: 'History', exact: true }).click();
		for (let i = 0; !held && i < 150; i++) await sleep(20);
		const heldVersion = held && JSON.parse(held.body).current_version;
		await update(inflight);
		await ipHeadline.filter({ hasText: 'In-flight history v2' }).waitFor({ timeout: 5000 });
		if (held) { held.res.writeHead(held.status, held.headers); held.res.end(held.body); held = null; }
		await ip.locator('.history-row[data-version="2"]').waitFor({ timeout: 5000 }).catch(() => {});
		const ipListed = await ip.locator('.history-row').evaluateAll((els) => els.map((e) => Number(e.dataset.version)));
		check('a history reply read before an update still ends up listing the update', heldVersion === 1 && ipListed.join(',') === '2,1' && (await text(ip.locator('.history-row[data-version="2"] b'))) === 'v2 · latest', `held reply current_version ${heldVersion}, listed ${ipListed.join(',')}`);
		await ip.close();
	} finally {
		held?.res.destroy();
		proxy.closeAllConnections();
		await new Promise((resolve) => proxy.close(resolve));
	}
} finally {
	await browser?.close();
	await stopServer();
}
console.log(`${failed === 0 ? 'ALL PASS' : `${failed} FAILED`} (${engineName})`);
process.exitCode = failed === 0 ? 0 : 1;
