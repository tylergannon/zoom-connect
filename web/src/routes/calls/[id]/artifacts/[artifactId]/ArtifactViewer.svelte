<script lang="ts">
	import { Maximize, RefreshCw } from '@lucide/svelte';
	import * as Button from '#lib/components/ui/button';
	import * as Badge from '#lib/components/ui/badge';
	import { api, APIError, errorMessage, relativeDate } from '#lib/api';
	import { artifactDocument } from '#lib/artifact-frame';
	import type { ArtifactSnapshot } from '#lib/types';

	let { callId, artifactId }: { callId: string; artifactId: string } = $props();
	let artifact = $state<ArtifactSnapshot | null>(null);
	let loading = $state(true);
	let error = $state('');
	let noAccess = $state(false);
	let missing = $state(false);
	let streamState = $state<'connecting'|'live'|'reconnecting'|'ended'>('connecting');
	let frameWrap: HTMLDivElement | undefined = $state();
	let source: EventSource | null = null;
	let rechecking = '';
	let retry: ReturnType<typeof setTimeout> | undefined;
	const base = $derived(`/api/calls/${encodeURIComponent(callId)}/artifacts/${encodeURIComponent(artifactId)}`);
	const srcdoc = $derived(artifact ? artifactDocument(artifact.html) : '');

	$effect(() => {
		const key = base;
		artifact = null; loading = true; error = ''; noAccess = false; missing = false; streamState = 'connecting';
		void load(key);
		return () => { clearTimeout(retry); source?.close(); source = null; };
	});
	// Versions only move forward; stale or repeated snapshots are ignored, and
	// so is a snapshot of another artifact (a reply from before a route change).
	function accept(next: ArtifactSnapshot) {
		if (next.call_id !== callId || next.id !== artifactId) return;
		if (!artifact || next.version > artifact.version) artifact = next;
	}
	async function load(key: string) {
		try {
			const latest = await api<ArtifactSnapshot>(key);
			if (key !== base) return;
			accept(latest);
			connect(key, latest.version);
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
		if (status === 403 || status === 404) { source?.close(); source = null; artifact = null; streamState = 'ended'; noAccess = status === 403; missing = status === 404; return; }
		error = errorMessage(e);
	}
	function present() { void frameWrap?.requestFullscreen?.(); }
</script>

<svelte:head><title>{artifact?.title || 'Artifact'} · Zoom Connect</title></svelte:head>

{#if loading}<div class="panel empty-state"><span class="spinner"></span><p>Loading artifact…</p></div>
{:else if noAccess}<div class="panel empty-state"><h2>You can’t view this artifact</h2><p>Only call participants and coworkers they’ve shared the call with can see its artifacts.</p><a class="sign-in-link" href="/calls">Back to calls</a></div>
{:else if missing}<div class="panel empty-state"><h2>Artifact not found</h2><p>It may belong to a different call.</p><a class="sign-in-link" href={`/calls/${encodeURIComponent(callId)}`}>Back to call</a></div>
{:else if error}<div class="panel empty-state"><h2>Couldn't load this artifact</h2><p>{error}</p><Button.Root onclick={() => load(base)}><RefreshCw size={15}/> Try again</Button.Root></div>
{:else if artifact}
	<div class="page-head artifact-head">
		<div><a class="back-link" href={`/calls/${encodeURIComponent(callId)}`}>← Call</a><h1>{artifact.title}</h1>
			<div class="artifact-sub"><Badge.Badge variant="secondary" data-testid="artifact-version">Version {artifact.version}</Badge.Badge><span>Updated {relativeDate(artifact.updated_at)} by {artifact.updated_by.name}{artifact.updated_via ? ` · agent key “${artifact.updated_via}”` : ''}</span><span class="stream-state" data-state={streamState}>{streamState === 'live' ? '● Live' : streamState === 'ended' ? 'Final version' : streamState === 'reconnecting' ? 'Reconnecting…' : 'Connecting…'}</span></div>
		</div>
		<Button.Root variant="outline" onclick={present}><Maximize size={15}/> Present</Button.Root>
	</div>
	<div class="artifact-frame" bind:this={frameWrap} data-version={artifact.version}>
		<!-- sandbox="" gives the page an opaque origin with every capability off. -->
		<iframe title={artifact.title} sandbox="" referrerpolicy="no-referrer" {srcdoc}></iframe>
	</div>
{/if}

<style>
	.back-link { display:inline-block; color:var(--muted-foreground); font-size:12px; margin-bottom:8px; }
	.artifact-head { align-items:flex-end; }
	.artifact-sub { display:flex; gap:10px; flex-wrap:wrap; align-items:center; margin-top:8px; color:var(--muted-foreground); font-size:12px; }
	.stream-state[data-state='live'] { color:var(--primary); font-weight:600; }
	.artifact-frame { border:1px solid var(--border); border-radius:9px; overflow:hidden; background:#fff; height:calc(100vh - 190px); min-height:420px; }
	.artifact-frame:fullscreen { border:0; border-radius:0; height:100vh; }
	iframe { display:block; width:100%; height:100%; border:0; background:#fff; }
</style>
