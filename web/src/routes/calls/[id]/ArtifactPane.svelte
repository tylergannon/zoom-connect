<script lang="ts">
	import { tick } from 'svelte';
	import { History, Maximize, RefreshCw, X } from '@lucide/svelte';
	import * as Button from '#lib/components/ui/button';
	import * as Badge from '#lib/components/ui/badge';
	import { api, APIError, errorMessage, relativeDate } from '#lib/api';
	import { artifactDocument } from '#lib/artifact-frame';
	import type { ArtifactHistory, ArtifactSnapshot, ArtifactVersion, ArtifactVersionInfo } from '#lib/types';

	let { callId, artifactId, onupdate }: { callId: string; artifactId: string; onupdate?: () => void } = $props();
	let artifact = $state<ArtifactSnapshot | null>(null);
	// An older version the viewer chose. Live updates keep advancing `artifact`
	// but never replace a pinned version on screen.
	let pinned = $state<ArtifactVersion | null>(null);
	let loading = $state(true);
	let error = $state('');
	let noAccess = $state(false);
	let missing = $state(false);
	let streamState = $state<'connecting'|'live'|'reconnecting'|'ended'>('connecting');
	let historyOpen = $state(false);
	// Loaded history is one unbroken run of stored versions, newest first.
	// `historyCursor` is the server's `next_before` for the page below it.
	let history = $state<ArtifactVersionInfo[]>([]);
	let historyCursor = $state<number | null>(null);
	let historyRun = 0;
	let historyLoaded = $state(false);
	let historyError = $state('');
	let presenting = $state(false);
	let fullscreen = $state(false);
	let pane: HTMLDivElement | undefined = $state();
	let returnFocus: HTMLElement | null = null;
	let presentedEl: HTMLElement | null = null;
	let presentRun = 0;
	let returnScroll = 0;
	let source: EventSource | null = null;
	let rechecking = '';
	let viewing = 0;
	let retry: ReturnType<typeof setTimeout> | undefined;
	const base = $derived(`/api/calls/${encodeURIComponent(callId)}/artifacts/${encodeURIComponent(artifactId)}`);
	const shown = $derived(pinned ?? artifact);
	const srcdoc = $derived(shown ? artifactDocument(shown.html) : '');
	const historyMore = $derived(historyCursor !== null);
	const oldest = $derived(history.length ? history[history.length - 1].version : 0);

	$effect(() => {
		const key = base;
		artifact = null; pinned = null; loading = true; error = ''; noAccess = false; missing = false; streamState = 'connecting';
		history = []; historyCursor = null; historyLoaded = false; historyError = ''; viewing++; historyRun++;
		void load(key);
		return () => { clearTimeout(retry); source?.close(); source = null; };
	});
	// Leaving the call view while presenting must not leave the page locked.
	$effect(() => () => {
		presentRun++;
		document.body.classList.remove('artifact-presenting');
		if (presentedEl && document.fullscreenElement === presentedEl) void document.exitFullscreen().catch(() => {});
	});
	// Versions only move forward; stale or repeated snapshots are ignored, and
	// so is a snapshot of another artifact (a reply from before a route change).
	function accept(next: ArtifactSnapshot) {
		if (next.call_id !== callId || next.id !== artifactId) return;
		if (artifact && next.version <= artifact.version) return;
		const advanced = artifact !== null;
		artifact = next;
		if (!advanced) return;
		onupdate?.();
		if (historyLoaded) void loadHistory(base);
	}
	async function load(key: string) {
		try {
			const latest = await api<ArtifactSnapshot>(key);
			if (key !== base) return;
			accept(latest);
			connect(key, latest.version);
			if (historyOpen) void loadHistory(key);
		} catch (e) { if (key === base) fail(e); }
		finally { if (key === base) loading = false; }
	}
	function connect(key: string, from: number) {
		source?.close();
		const es = new EventSource(`${key}/stream?from=${from}`);
		source = es;
		streamState = 'connecting';
		// A replaced stream (route change or reconnect) no longer touches state.
		es.onopen = () => { if (source === es) streamState = 'live'; };
		es.addEventListener('artifact', (event) => {
			if (source !== es) return;
			try { accept(JSON.parse((event as MessageEvent).data) as ArtifactSnapshot); } catch { /* ignore malformed frames */ }
		});
		es.addEventListener('end', () => { if (source !== es) return; es.close(); source = null; streamState = 'ended'; });
		es.onerror = () => { if (source === es && streamState !== 'ended') { streamState = 'reconnecting'; void recheck(key); } };
	}
	// The server closes the stream when access ends. A plain read tells that
	// apart from a network or proxy failure, which keeps the last version shown.
	async function recheck(key: string) {
		if (rechecking === key) return;
		rechecking = key;
		try { const latest = await api<ArtifactSnapshot>(key); if (key === base) accept(latest); }
		catch (e) { if (key === base && e instanceof APIError && [401, 403, 404].includes(e.status)) fail(e); }
		finally { if (rechecking === key) rechecking = ''; }
		// EventSource stops retrying after an HTTP error such as a proxy 502.
		if (key === base && source?.readyState === EventSource.CLOSED) {
			clearTimeout(retry);
			retry = setTimeout(() => { if (key === base && artifact && source?.readyState === EventSource.CLOSED) connect(key, artifact.version); }, 3000);
		}
	}
	function fail(e: unknown) {
		const status = e instanceof APIError ? e.status : 0;
		if (status === 401) { source?.close(); source = null; window.location.assign('/?next=' + encodeURIComponent(window.location.pathname)); return; }
		if (status === 403 || status === 404) { exitPresent(); source?.close(); source = null; artifact = null; pinned = null; streamState = 'ended'; noAccess = status === 403; missing = status === 404; return; }
		error = errorMessage(e);
	}

	// History lists versions newest first. Without `before` it refreshes the
	// newest page. That page is kept together with the loaded run only when the
	// two overlap or the page reaches the first version; otherwise versions
	// between them were never listed (many updates during a disconnect), so the
	// run restarts from the fresh page and "Load older" walks down from there.
	// A reply can be older than the live version (it was read before an
	// update arrived); the list is then read again so the latest is listed.
	async function loadHistory(key: string, before?: number) {
		const run = historyRun;
		historyError = '';
		try {
			const page = await api<ArtifactHistory>(`${key}/versions${before ? `?before=${before}` : ''}`);
			if (key !== base || run !== historyRun || page.call_id !== callId || page.artifact_id !== artifactId) return;
			const merge = () => {
				const fresh = new Set(page.versions.map((v) => v.version));
				return [...history.filter((v) => !fresh.has(v.version)), ...page.versions].sort((a, b) => b.version - a.version);
			};
			if (before !== undefined) {
				// An older page counts only if it continues the current run.
				if (before !== historyCursor) return;
				history = merge(); historyCursor = page.next_before;
			} else if (!historyLoaded || !history.length) {
				history = page.versions; historyCursor = page.next_before;
			} else {
				const top = page.versions[0]?.version ?? 0;
				// A late reply entirely below the loaded run adds nothing the
				// cursor will not list, and would leave a hole if merged.
				if (top < history[history.length - 1].version) return;
				if (page.next_before === null) { history = merge(); historyCursor = null; }
				else if (page.next_before <= history[0].version) history = merge();
				else { history = page.versions; historyCursor = page.next_before; }
			}
			historyLoaded = true;
			if (before === undefined && artifact && history[0] && history[0].version < artifact.version) void loadHistory(key);
		} catch (e) { if (key === base && run === historyRun) historyError = errorMessage(e); }
	}
	function toggleHistory() {
		historyOpen = !historyOpen;
		if (historyOpen && !historyLoaded) void loadHistory(base);
	}
	async function view(version: number) {
		const key = base, n = ++viewing;
		if (artifact && version === artifact.version) { pinned = null; return; }
		try {
			const v = await api<ArtifactVersion>(`${key}/versions/${version}`);
			if (n !== viewing || key !== base || v.call_id !== callId || v.artifact_id !== artifactId) return;
			pinned = v;
		} catch (e) { if (n === viewing && key === base) historyError = errorMessage(e); }
	}
	function returnToLive() { viewing++; pinned = null; }

	// Present makes this app-owned pane (header, status and Exit included) fill
	// the screen; the artifact frame itself is never the fullscreen element. If
	// the browser refuses fullscreen, the pane still covers the window.
	async function present() {
		if (presenting || !pane) return;
		const el = pane, run = ++presentRun;
		presentedEl = el;
		// Safari does not focus a clicked button; Exit then returns to Present.
		returnFocus = document.activeElement instanceof HTMLElement && document.activeElement !== document.body ? document.activeElement : null;
		returnScroll = window.scrollY;
		presenting = true;
		document.body.classList.add('artifact-presenting');
		await tick();
		el.querySelector<HTMLElement>('[data-exit]')?.focus();
		try { if (el.requestFullscreen) await el.requestFullscreen(); } catch { /* window-filling fallback */ }
		// Exit, Escape or leaving the view can happen before the browser grants
		// fullscreen; a late grant is then handed straight back.
		if (run !== presentRun || !presenting) {
			if (document.fullscreenElement === el) void document.exitFullscreen().catch(() => {});
			return;
		}
		fullscreen = document.fullscreenElement === el;
	}
	function exitPresent() {
		if (!presenting) return;
		presentRun++;
		presenting = false; fullscreen = false;
		document.body.classList.remove('artifact-presenting');
		if (pane && document.fullscreenElement === pane) void document.exitFullscreen().catch(() => {});
		// The Present button is re-rendered on exit, so focus its new instance.
		void tick().then(() => {
			window.scrollTo({ top: returnScroll });
			const target = returnFocus?.isConnected ? returnFocus : pane?.querySelector<HTMLElement>('[data-present]');
			target?.focus({ preventScroll: true });
		});
	}
	function fullscreenChanged() {
		if (presenting && pane && document.fullscreenElement === pane) fullscreen = true;
		else if (presenting && fullscreen) exitPresent(); // Escape or browser UI left fullscreen
		else if (!presenting && presentedEl && document.fullscreenElement === presentedEl) void document.exitFullscreen().catch(() => {});
	}
	function keydown(event: KeyboardEvent) {
		if (presenting && event.key === 'Escape') { event.preventDefault(); exitPresent(); }
	}
</script>

<svelte:window onkeydown={keydown} />
<svelte:document onfullscreenchange={fullscreenChanged} />

<div class="artifact-pane" class:presenting bind:this={pane} data-testid="artifact-pane" data-presenting={presenting ? (fullscreen ? 'fullscreen' : 'window') : undefined}>
	{#if loading}<div class="empty-state"><span class="spinner"></span><p>Loading page…</p></div>
	{:else if noAccess}<div class="empty-state"><h2>You can’t view this page</h2><p>Only call participants and coworkers they’ve shared the call with can see its pages.</p></div>
	{:else if missing}<div class="empty-state"><h2>Page not found</h2><p>It may belong to a different call.</p></div>
	{:else if error}<div class="empty-state"><h2>Couldn't load this page</h2><p>{error}</p><Button.Root onclick={() => load(base)}><RefreshCw size={15}/> Try again</Button.Root></div>
	{:else if artifact && shown}
		<div class="pane-head">
			<div class="pane-title"><h2 data-testid="artifact-title">{shown.title}</h2>
				<div class="artifact-sub"><Badge.Badge variant={pinned ? 'outline' : 'secondary'} data-testid="artifact-version">Version {shown.version}</Badge.Badge>{#if pinned}<span class="older" data-testid="older-revision">Older version from {relativeDate(pinned.created_at)} · latest is v{artifact.version}</span>{:else}<span>Updated {relativeDate(artifact.updated_at)} by {artifact.updated_by.name}{artifact.updated_via ? ` · agent key “${artifact.updated_via}”` : ''}</span>{/if}<span class="stream-state" data-state={streamState}>{streamState === 'live' ? '● Live' : streamState === 'ended' ? 'Final version' : streamState === 'reconnecting' ? 'Reconnecting…' : 'Connecting…'}</span></div>
			</div>
			<div class="pane-actions">
				{#if pinned}<Button.Root variant="outline" onclick={returnToLive} data-testid="return-to-live">Return to live</Button.Root>{/if}
				{#if presenting}<Button.Root variant="outline" onclick={exitPresent} data-exit><X size={15}/> Exit <kbd>Esc</kbd></Button.Root>
				{:else}<Button.Root variant="outline" aria-pressed={historyOpen} onclick={toggleHistory}><History size={15}/> History</Button.Root><Button.Root onclick={() => void present()} data-present><Maximize size={15}/> Present</Button.Root>{/if}
			</div>
		</div>
		<div class="pane-body" class:with-history={historyOpen && !presenting}>
			<div class="artifact-frame" data-version={shown.version}>
				<!-- sandbox="" gives the page an opaque origin with every capability off. -->
				<iframe title={shown.title} sandbox="" referrerpolicy="no-referrer" {srcdoc}></iframe>
			</div>
			{#if historyOpen && !presenting}
				<div class="history" data-testid="artifact-history">
					<h3>Versions</h3>
					{#if historyError}<p class="muted small">{historyError}</p>{/if}
					{#each history as v (v.version)}
						<button class="history-row" class:active={v.version === shown.version} aria-current={v.version === shown.version ? 'true' : undefined} data-version={v.version} onclick={() => void view(v.version)}><b>v{v.version}{v.version === artifact.version ? ' · latest' : ''}</b><span>{v.title}</span><small>{relativeDate(v.created_at)} · {v.author.name}{v.via ? ` · ${v.via}` : ''}</small></button>
					{/each}
					{#if historyMore}<Button.Root variant="ghost" size="sm" onclick={() => void loadHistory(base, historyCursor ?? undefined)}>Load older versions</Button.Root>
					{:else if historyLoaded && oldest > 1}<p class="muted small" data-testid="history-gap">Versions 1–{oldest - 1} are no longer stored; an earlier build kept only the newest 25.</p>{/if}
				</div>
			{/if}
		</div>
	{/if}
</div>

<style>
	.artifact-pane { min-width:0; display:flex; flex-direction:column; gap:10px; }
	.artifact-pane.presenting { position:fixed; inset:0; z-index:1000; padding:12px 16px 16px; background:var(--background); }
	.pane-head { display:flex; justify-content:space-between; align-items:flex-end; gap:12px; flex-wrap:wrap; }
	.pane-title { min-width:0; }
	.pane-title h2 { margin:0; font-size:16px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
	.pane-actions { display:flex; gap:8px; flex-wrap:wrap; }
	.pane-actions kbd { font:11px ui-monospace,monospace; color:var(--muted-foreground); }
	.artifact-sub { display:flex; gap:10px; flex-wrap:wrap; align-items:center; margin-top:6px; color:var(--muted-foreground); font-size:12px; }
	.older { color:var(--foreground); font-weight:550; }
	.stream-state[data-state='live'] { color:var(--primary); font-weight:600; }
	.pane-body { display:grid; grid-template-columns:minmax(0,1fr); gap:10px; }
	.pane-body.with-history { grid-template-columns:minmax(0,1fr) 220px; }
	.presenting .pane-body { flex:1; min-height:0; }
	.artifact-frame { border:1px solid var(--border); border-radius:9px; overflow:hidden; background:#fff; height:min(70vh, 760px); min-height:380px; }
	.presenting .artifact-frame { height:100%; min-height:0; }
	/* Loading another page keeps the pane's height so the call view does not jump. */
	.artifact-pane > .empty-state { min-height:calc(min(70vh, 760px) + 52px); display:flex; flex-direction:column; align-items:center; justify-content:center; }
	iframe { display:block; width:100%; height:100%; border:0; background:#fff; }
	.history { display:flex; flex-direction:column; gap:6px; max-height:min(70vh, 760px); overflow:auto; }
	.history h3 { margin:0 0 2px; font-size:12px; color:var(--muted-foreground); text-transform:uppercase; letter-spacing:.04em; }
	.history-row { display:grid; gap:1px; text-align:left; padding:7px 9px; border:1px solid var(--border); border-radius:7px; background:none; color:inherit; font:inherit; cursor:pointer; }
	.history-row:hover, .history-row.active { border-color:var(--primary); }
	.history-row b { font-size:12px; }
	.history-row span { font-size:12px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
	.history-row small { color:var(--muted-foreground); font-size:11px; }
	:global(body.artifact-presenting) { overflow:hidden; }
	@media(max-width:800px){ .pane-body.with-history { grid-template-columns:1fr; } }
</style>
