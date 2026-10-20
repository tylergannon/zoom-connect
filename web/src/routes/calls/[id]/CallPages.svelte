<script lang="ts">
	import { Search } from '@lucide/svelte';
	import * as Card from '#lib/components/ui/card';
	import { api, errorMessage, relativeDate } from '#lib/api';
	import type { Artifact, Call } from '#lib/types';
	import ArtifactPane from './ArtifactPane.svelte';

	// Pages are the call's artifacts. The selected one comes from the URL
	// (/calls/{id}/artifacts/{artifactId}) and is shown in the pane beside the list.
	let { call, selectedId, origin }: { call: Call; selectedId: string; origin: string } = $props();
	let artifacts = $state<Artifact[]>([]);
	let error = $state('');
	let sort = $state<'updated'|'title'|'created'>('updated');
	let filter = $state('');
	// Shown when the URL names no page. It is chosen once, so later updates
	// that reorder the list do not switch the page on screen.
	let firstId = $state('');
	const open = $derived(call.status === 'live' || call.status === 'scheduled');
	const time = (s: string) => new Date(s).getTime();
	const orders = {
		updated: (a: Artifact, b: Artifact) => time(b.updated_at) - time(a.updated_at),
		title: (a: Artifact, b: Artifact) => a.title.localeCompare(b.title, undefined, { sensitivity: 'base' }),
		created: (a: Artifact, b: Artifact) => time(b.created_at) - time(a.created_at),
	};
	const byOrder = (order: keyof typeof orders) => (a: Artifact, b: Artifact) => orders[order](a, b) || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0);
	const shown = $derived(artifacts.filter((a) => a.title.toLowerCase().includes(filter.trim().toLowerCase())).sort(byOrder(sort)));
	const selected = $derived(selectedId || firstId);

	// Agents publish while the call is open, so the list refreshes then.
	$effect(() => {
		const callId = call.id;
		void refresh(callId);
		if (!open) return;
		const timer = setInterval(() => void refresh(callId), 10000);
		return () => clearInterval(timer);
	});
	// Firefox and Safari do not focus a clicked link. Focusing the row keeps the
	// keyboard position on the selected page in every browser.
	function focusRow(event: MouseEvent) {
		(event.currentTarget as HTMLElement).focus({ preventScroll: true });
	}
	async function refresh(callId: string) {
		try {
			const list = await api<Artifact[]>(`/api/calls/${encodeURIComponent(callId)}/artifacts`);
			if (callId !== call.id) return;
			artifacts = list; error = '';
			if (!firstId && list.length) firstId = [...list].sort(byOrder('updated'))[0].id;
		} catch (e) { if (callId === call.id) error = errorMessage(e); }
	}
</script>

<Card.Root class="panel-pad call-pages" data-testid="call-artifacts">
	<div class="pages-head">
		<h2>Pages · {artifacts.length}</h2>
		{#if artifacts.length > 1}
			<div class="tabs-row" role="group" aria-label="Sort pages">
				<button class:active={sort==='updated'} aria-pressed={sort==='updated'} onclick={() => sort='updated'}>Recently updated</button>
				<button class:active={sort==='title'} aria-pressed={sort==='title'} onclick={() => sort='title'}>Title</button>
				<button class:active={sort==='created'} aria-pressed={sort==='created'} onclick={() => sort='created'}>Newest</button>
			</div>
			<div class="search-wrap"><Search size={15}/><input class="search-input" aria-label="Filter pages" placeholder="Filter by title" bind:value={filter}/></div>
		{/if}
	</div>
	{#if error}<p class="muted small">{error}</p>{/if}
	{#if artifacts.length || selectedId}
		<div class="pages-layout">
			<nav class="page-list" aria-label="Pages">
				{#each shown as artifact (artifact.id)}<a class="page-row" class:active={artifact.id === selected} aria-current={artifact.id === selected ? 'page' : undefined} href={artifact.viewer_url} data-sveltekit-reset="false" onclick={focusRow} data-artifact-id={artifact.id}><b>{artifact.title}</b><small>v{artifact.version} · updated {relativeDate(artifact.updated_at)} by {artifact.updated_by.name}</small></a>{/each}
				{#if artifacts.length && !shown.length}<p class="muted small">No pages match “{filter}”.</p>{/if}
			</nav>
			{#if selected}<ArtifactPane callId={call.id} artifactId={selected} onupdate={() => void refresh(call.id)} />{/if}
		</div>
		<p class="muted small">Agents update pages live. Present fills the screen with the selected page; share that window in Zoom.</p>
	{:else if open}
		<p class="muted small">Agents can publish live HTML pages for this call:</p>
		<pre class="code">curl -H "Authorization: Bearer $ZC_KEY" -H "Content-Type: application/json" -d '&lbrace;"title":"Notes","html":"&lt;h1&gt;Hello&lt;/h1&gt;"&rbrace;' {origin}/api/calls/{call.id}/artifacts</pre>
	{:else}
		<p class="muted small">No pages were published during this call.</p>
	{/if}
</Card.Root>

<style>
	:global(.call-pages) { margin-bottom:14px; }
	.pages-head { display:flex; align-items:center; gap:12px; flex-wrap:wrap; margin-bottom:12px; }
	.pages-head h2 { margin:0; font-size:14px; flex:1; }
	.search-wrap { width:min(100%,220px); position:relative; display:flex; align-items:center; }
	.search-wrap :global(svg) { position:absolute; left:10px; color:var(--muted-foreground); }
	.search-wrap input { padding-left:32px; }
	.pages-layout { display:grid; grid-template-columns:230px minmax(0,1fr); gap:14px; align-items:start; margin-bottom:10px; }
	.page-list { display:grid; gap:6px; max-height:min(70vh, 760px); overflow:auto; }
	.page-row { display:grid; gap:2px; padding:8px 10px; border:1px solid var(--border); border-radius:7px; color:inherit; text-decoration:none; }
	.page-row:hover { border-color:var(--primary); }
	.page-row.active { border-color:var(--primary); background:var(--muted); }
	.page-row b { font-size:13px; font-weight:550; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
	.page-row small { color:var(--muted-foreground); font-size:11px; }
	@media(max-width:900px){ .pages-layout { grid-template-columns:1fr; } .page-list { max-height:none; } }
</style>
