<script lang="ts">
	import { tick } from 'svelte';
	import History from '@lucide/svelte/icons/history';
	import Maximize from '@lucide/svelte/icons/maximize';
	import RefreshCw from '@lucide/svelte/icons/refresh-cw';
	import X from '@lucide/svelte/icons/x';
	import Lock from '@lucide/svelte/icons/lock';
	import SearchX from '@lucide/svelte/icons/search-x';
	import * as Empty from '#lib/components/ui/empty';
	import { Badge } from '#lib/components/ui/badge';
	import { Button } from '#lib/components/ui/button';
	import { Kbd } from '#lib/components/ui/kbd';
	import { Spinner } from '#lib/components/ui/spinner';
	import { ago, api, APIError, errorMessage } from '#lib/api';
	import { artifactDocument } from '#lib/artifact-frame';
	import type { ArtifactHistory, ArtifactSnapshot, ArtifactVersion, ArtifactVersionInfo } from '#lib/types';

	let { callId, artifactId, now, onupdate }: { callId: string; artifactId: string; now: number; onupdate?: () => void } = $props();
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
	let presentedEl: HTMLElement | null = null;
	let presentRun = 0;
	let returnScroll = 0;
	let source: EventSource | null = null;
	let rechecking = '';
	let viewing = 0;
	let retry: ReturnType<typeof setTimeout> | undefined;
	const historyId = $props.id();
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
		// Focus returns to Present, the only way in. It is re-rendered on exit, so
		// focus its new instance; Safari would not have focused the clicked one.
		void tick().then(() => {
			window.scrollTo({ top: returnScroll });
			pane?.querySelector<HTMLElement>('[data-present]')?.focus({ preventScroll: true });
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

<!-- Loading, error and empty states keep the frame's height so the call view does not jump. -->
{#snippet notice(icon: typeof Lock, heading: string, text: string)}
	{@const Icon = icon}
	<Empty.Root class="min-h-[min(70vh,760px)] border">
		<Empty.Header>
			<Empty.Media variant="icon"><Icon aria-hidden="true" /></Empty.Media>
			<h3 class="text-lg font-semibold">{heading}</h3>
			<Empty.Description>{text}</Empty.Description>
		</Empty.Header>
	</Empty.Root>
{/snippet}

<div
	bind:this={pane}
	data-testid="artifact-pane"
	data-presenting={presenting ? (fullscreen ? 'fullscreen' : 'window') : undefined}
	class={['artifact-pane flex min-w-0 flex-col gap-3', presenting && 'fixed inset-0 z-50 bg-background p-3 sm:p-4']}
>
	{#if loading}
		<div class="grid min-h-[min(70vh,760px)] place-items-center rounded-xl border text-muted-foreground">
			<p role="status" class="inline-flex items-center gap-2"><Spinner aria-hidden="true" role="presentation" />Loading page…</p>
		</div>
	{:else if noAccess}
		{@render notice(Lock, 'You can’t view this page', 'Only the people on this call, and people they share it with, can see its pages.')}
	{:else if missing}
		{@render notice(SearchX, 'Page not found', 'It may belong to a different call.')}
	{:else if error}
		<Empty.Root role="alert" class="min-h-[min(70vh,760px)] border">
			<Empty.Header>
				<h3 class="text-lg font-semibold">We couldn’t load this page</h3>
				<Empty.Description>{error}</Empty.Description>
			</Empty.Header>
			<Empty.Content><Button onclick={() => load(base)}><RefreshCw aria-hidden="true" />Try again</Button></Empty.Content>
		</Empty.Root>
	{:else if artifact && shown}
		<div class="flex flex-wrap items-end justify-between gap-3">
			<div class="flex min-w-0 flex-col gap-1">
				<h3 data-testid="artifact-title" class="truncate text-lg font-semibold">{shown.title}</h3>
				<div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
					<Badge variant={pinned ? 'outline' : 'secondary'} data-testid="artifact-version">Version {shown.version}</Badge>
					{#if pinned}
						<span class="font-medium text-foreground" data-testid="older-revision">Older version from {ago(pinned.created_at, now)} · latest is v{artifact.version}</span>
					{:else}
						<span>Updated {ago(artifact.updated_at, now)} by {artifact.updated_by.name}{artifact.updated_via ? ` · agent key “${artifact.updated_via}”` : ''}</span>
					{/if}
					<span class={['stream-state', streamState === 'live' && 'font-medium text-live']} data-state={streamState}>
						{streamState === 'live' ? '● Live' : streamState === 'ended' ? 'Final version' : streamState === 'reconnecting' ? 'Reconnecting…' : 'Connecting…'}
					</span>
				</div>
			</div>
			<div class="flex flex-wrap gap-2">
				{#if pinned}<Button variant="outline" onclick={returnToLive} data-testid="return-to-live">Return to live</Button>{/if}
				{#if presenting}
					<Button variant="outline" onclick={exitPresent} data-exit><X aria-hidden="true" />Exit<Kbd aria-hidden="true">Esc</Kbd></Button>
				{:else}
					<Button variant="outline" aria-pressed={historyOpen} onclick={toggleHistory}><History aria-hidden="true" />History</Button>
					<Button onclick={() => void present()} data-present><Maximize aria-hidden="true" />Present</Button>
				{/if}
			</div>
		</div>
		<div class={presenting ? 'flex min-h-0 flex-1' : ['grid gap-3', historyOpen && 'md:grid-cols-[minmax(0,1fr)_15rem]']}>
			<div
				class={['artifact-frame overflow-hidden rounded-xl border bg-white', presenting ? 'min-h-0 flex-1' : 'h-[min(70vh,760px)] min-h-96']}
				data-version={shown.version}
			>
				<!-- sandbox="" gives the page an opaque origin with every capability off. -->
				<iframe class="block size-full border-0" title={shown.title} sandbox="" referrerpolicy="no-referrer" {srcdoc}></iframe>
			</div>
			{#if historyOpen && !presenting}
				<section aria-labelledby="{historyId}-h" class="flex max-h-[min(70vh,760px)] flex-col gap-2 overflow-auto" data-testid="artifact-history">
					<h4 id="{historyId}-h" class="text-sm font-semibold">Versions</h4>
					{#if historyError}<p role="alert" class="text-sm text-destructive">{historyError}</p>{/if}
					<ul class="flex flex-col gap-1.5">
						{#each history as v (v.version)}
							<li>
								<button
									class={['history-row grid w-full gap-0.5 rounded-lg border px-2.5 py-2 text-left text-sm outline-none hover:bg-muted focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50', v.version === shown.version && 'active border-primary bg-muted']}
									aria-current={v.version === shown.version ? 'true' : undefined}
									data-version={v.version}
									onclick={() => void view(v.version)}
								>
									<b class="font-medium">v{v.version}{v.version === artifact.version ? ' · latest' : ''}</b>
									<span class="truncate">{v.title}</span>
									<small class="text-xs text-muted-foreground">{ago(v.created_at, now)} · {v.author.name}{v.via ? ` · ${v.via}` : ''}</small>
								</button>
							</li>
						{/each}
					</ul>
					{#if historyMore}
						<Button variant="ghost" size="sm" onclick={() => void loadHistory(base, historyCursor ?? undefined)}>Load older versions</Button>
					{:else if historyLoaded && oldest > 1}
						<p class="text-sm text-muted-foreground" data-testid="history-gap">Versions 1–{oldest - 1} are no longer stored; an earlier build kept only the newest 25.</p>
					{/if}
				</section>
			{/if}
		</div>
	{/if}
</div>
