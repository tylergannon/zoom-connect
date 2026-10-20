<script lang="ts">
	import * as Card from '#lib/components/ui/card';
	import { api, errorMessage, relativeDate } from '#lib/api';
	import type { Artifact, Call } from '#lib/types';

	let { call, origin }: { call: Call; origin: string } = $props();
	let artifacts = $state<Artifact[]>([]);
	let error = $state('');
	const open = $derived(call.status === 'live' || call.status === 'scheduled');

	// Agents publish artifacts while the call is open, so the list refreshes then.
	$effect(() => {
		const callId = call.id;
		void refresh(callId);
		if (!open) return;
		const timer = setInterval(() => void refresh(callId), 10000);
		return () => clearInterval(timer);
	});
	async function refresh(callId: string) {
		try {
			const list = await api<Artifact[]>(`/api/calls/${encodeURIComponent(callId)}/artifacts`);
			if (callId === call.id) { artifacts = list; error = ''; }
		} catch (e) { if (callId === call.id) error = errorMessage(e); }
	}
</script>

<Card.Root class="panel-pad" data-testid="call-artifacts">
	<h2>Artifacts · {artifacts.length}</h2>
	{#if error}<p class="muted small">{error}</p>{/if}
	{#if artifacts.length}
		<div class="artifact-list">{#each artifacts as artifact (artifact.id)}<a class="artifact-row" href={artifact.viewer_url} target="_blank" rel="noopener"><b>{artifact.title}</b><small>v{artifact.version} · updated {relativeDate(artifact.updated_at)} by {artifact.updated_by.name}</small></a>{/each}</div>
		<p class="muted small">Open an artifact and share that window in Zoom; it updates live as agents publish versions.</p>
	{:else if open}
		<p class="muted small">Agents can publish a live HTML page for this call:</p>
		<pre class="code">curl -H "Authorization: Bearer $ZC_KEY" -H "Content-Type: application/json" -d '&lbrace;"title":"Notes","html":"&lt;h1&gt;Hello&lt;/h1&gt;"&rbrace;' {origin}/api/calls/{call.id}/artifacts</pre>
	{:else}
		<p class="muted small">No artifacts were published during this call.</p>
	{/if}
</Card.Root>

<style>
	.artifact-list { display:grid; gap:6px; margin-bottom:10px; }
	.artifact-row { display:grid; gap:2px; padding:8px 10px; border:1px solid var(--border); border-radius:7px; color:inherit; text-decoration:none; }
	.artifact-row:hover { border-color:var(--primary); }
	.artifact-row small { color:var(--muted-foreground); font-size:11px; }
</style>
