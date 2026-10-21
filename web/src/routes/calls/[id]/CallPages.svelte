<script lang="ts">
	import Search from '@lucide/svelte/icons/search';
	import FileCode from '@lucide/svelte/icons/file-code';
	import * as ButtonGroup from '#lib/components/ui/button-group';
	import * as InputGroup from '#lib/components/ui/input-group';
	import * as Item from '#lib/components/ui/item';
	import { Badge } from '#lib/components/ui/badge';
	import { Button } from '#lib/components/ui/button';
	import { ago, api, errorMessage } from '#lib/api';
	import type { Artifact, Call } from '#lib/types';
	import ArtifactPane from './ArtifactPane.svelte';

	// Pages are the call's artifacts. The selected one comes from the URL
	// (/calls/{id}/artifacts/{artifactId}) and is shown in the pane below the
	// list, which keeps the page itself as wide as the call view.
	let { call, selectedId, now }: { call: Call; selectedId: string; now: number } = $props();

	const id = $props.id();
	let artifacts = $state<Artifact[]>([]);
	let loaded = $state(false);
	let error = $state('');
	let sort = $state<'updated' | 'title' | 'created'>('updated');
	let filter = $state('');
	// Shown when the URL names no page. It is chosen once, so later updates
	// that reorder the list do not switch the page on screen.
	let firstId = $state('');
	const open = $derived(call.status === 'live' || call.status === 'scheduled');
	const time = (s: string) => new Date(s).getTime();
	const orders = {
		updated: (a: Artifact, b: Artifact) => time(b.updated_at) - time(a.updated_at),
		title: (a: Artifact, b: Artifact) => a.title.localeCompare(b.title, undefined, { sensitivity: 'base' }),
		created: (a: Artifact, b: Artifact) => time(b.created_at) - time(a.created_at)
	};
	const sorts = [
		{ value: 'updated', label: 'Recently updated' },
		{ value: 'title', label: 'Title' },
		{ value: 'created', label: 'Newest' }
	] as const;
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
			artifacts = list;
			error = '';
			if (!firstId && list.length) firstId = [...list].sort(byOrder('updated'))[0].id;
		} catch (e) {
			if (callId === call.id) error = errorMessage(e);
		} finally {
			if (callId === call.id) loaded = true;
		}
	}
</script>

{#if artifacts.length || selectedId}
	<section aria-labelledby="{id}-h" class="call-pages flex flex-col gap-4" data-testid="call-artifacts">
		<div class="flex flex-wrap items-center gap-3">
			<h2 id="{id}-h" class="flex flex-1 items-center gap-2 text-lg font-semibold">Pages <Badge variant="secondary">{artifacts.length}</Badge></h2>
			{#if artifacts.length > 1}
				<ButtonGroup.Root aria-label="Sort pages">
					{#each sorts as s (s.value)}
						<Button size="sm" variant={sort === s.value ? 'secondary' : 'outline'} aria-pressed={sort === s.value} onclick={() => (sort = s.value)}>{s.label}</Button>
					{/each}
				</ButtonGroup.Root>
				<div role="search" class="w-full sm:w-56">
					<label for="{id}-filter" class="sr-only">Filter pages</label>
					<InputGroup.Root>
						<InputGroup.Addon><Search aria-hidden="true" /></InputGroup.Addon>
						<InputGroup.Input id="{id}-filter" type="search" placeholder="Filter by title" bind:value={filter} />
					</InputGroup.Root>
				</div>
			{/if}
		</div>
		{#if error}<p role="alert" class="text-sm text-destructive">{error}</p>{/if}
		<div class="flex flex-col gap-4">
			<nav aria-label="Pages" class="max-h-44 overflow-auto">
				<ul class="grid gap-1.5 sm:grid-cols-2 lg:grid-cols-3">
					{#each shown as artifact (artifact.id)}
						<li>
							<a
								class={['page-row grid gap-0.5 rounded-lg border px-3 py-2 outline-none hover:bg-muted focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50', artifact.id === selected && 'active border-primary bg-muted']}
								aria-current={artifact.id === selected ? 'page' : undefined}
								href={artifact.viewer_url}
								data-sveltekit-reset="false"
								onclick={focusRow}
								data-artifact-id={artifact.id}
							>
								<b class="truncate font-medium">{artifact.title}</b>
								<small class="text-xs text-muted-foreground">v{artifact.version} · updated {ago(artifact.updated_at, now)} by {artifact.updated_by.name}</small>
							</a>
						</li>
					{/each}
				</ul>
				{#if artifacts.length && !shown.length}<p class="text-sm text-muted-foreground">No pages match “{filter}”.</p>{/if}
			</nav>
			{#if selected}<ArtifactPane callId={call.id} artifactId={selected} {now} onupdate={() => void refresh(call.id)} />{/if}
		</div>
		<p class="text-sm text-muted-foreground">
			Agents update pages as the call goes. Present fills the screen with the selected page; share that window in Zoom.
			<a href="/agent-api#pages" class="font-medium text-foreground underline underline-offset-4">How agents publish pages</a>
		</p>
	</section>
{:else if open && loaded}
	<Item.Root variant="outline" size="sm" data-testid="call-artifacts-empty">
		<Item.Media variant="icon"><FileCode aria-hidden="true" /></Item.Media>
		<Item.Content>
			<Item.Title>No pages yet</Item.Title>
			<Item.Description>An agent with one of your API keys can publish live HTML pages for this call. They show up here as it updates them.</Item.Description>
		</Item.Content>
		<Item.Actions><Button href="/agent-api#pages" variant="outline" size="sm">How agents publish pages</Button></Item.Actions>
	</Item.Root>
{/if}
