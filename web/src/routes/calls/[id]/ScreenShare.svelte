<script lang="ts">
	import RefreshCw from '@lucide/svelte/icons/refresh-cw';
	import { Button } from '#lib/components/ui/button';
	import { api, errorMessage, plural, timeOfDay } from '#lib/api';
	import type { EventItem } from '#lib/types';

	let { callId, events }: { callId: string; events: EventItem[] } = $props();

	const frames = $derived(events.filter((e) => e.kind === 'screen.frame'));
	let selected = $state<number | null>(null);
	let renewed = $state<Record<number, string>>({});
	let error = $state('');
	const current = $derived(frames.find((e) => e.seq === selected) ?? frames.at(-1));
	const url = (frame: EventItem) => renewed[frame.seq] || String(frame.data.url || '');
	const describe = (frame: EventItem) => `Shared screen from ${frame.participant?.name || 'the presenter'} at ${timeOfDay(frame.ts, true)}`;

	async function refresh(seq: number) {
		error = '';
		try {
			const data = await api<{ url: string }>(`/api/calls/${encodeURIComponent(callId)}/screenshots/${seq}`);
			renewed = { ...renewed, [seq]: data.url };
		} catch (e) {
			error = errorMessage(e);
		}
	}

	function choose(seq: number | null) {
		selected = seq;
		error = '';
	}
</script>

{#if current}
	<div class="flex flex-col gap-4">
		<p class="text-sm text-muted-foreground">{plural(frames.length, 'image')} captured, about one a second while someone shared.</p>
		<figure class="flex flex-col gap-2">
			<a href={url(current)} target="_blank" rel="noreferrer" class="block overflow-hidden rounded-lg border bg-neutral-950">
				<img
					src={url(current)}
					alt={describe(current)}
					class="max-h-[60vh] w-full object-contain"
					onerror={() => (error = 'This image link has expired. Refresh it to load the image again.')}
				/>
				<span class="sr-only"> (opens full size in a new tab)</span>
			</a>
			<figcaption class="flex flex-wrap items-center gap-2 text-sm">
				<span class="mr-auto">{current.participant?.name || 'Presenter'} · {timeOfDay(current.ts, true)}</span>
				{#if selected !== null}
					<Button variant="ghost" size="sm" onclick={() => choose(null)}>Show latest image</Button>
				{/if}
				<Button variant="ghost" size="sm" onclick={() => refresh(current.seq)}><RefreshCw aria-hidden="true" />Refresh image link</Button>
			</figcaption>
		</figure>
		{#if error}<p role="alert" class="text-sm text-destructive">{error}</p>{/if}
		<ul aria-label="Captured images" class="flex gap-2 overflow-x-auto pb-2">
			{#each frames.slice(-120) as frame (frame.seq)}
				<li class="shrink-0">
					<button
						type="button"
						aria-current={frame.seq === current.seq ? 'true' : undefined}
						class="flex w-32 flex-col overflow-hidden rounded-md border-2 border-transparent bg-muted text-left aria-[current=true]:border-primary"
						onclick={() => choose(frame.seq)}
					>
						<img loading="lazy" src={url(frame)} alt="" class="h-18 w-full bg-neutral-950 object-contain" />
						<span class="px-1.5 py-1 text-xs"><span class="sr-only">Show image from </span>{timeOfDay(frame.ts, true)}</span>
					</button>
				</li>
			{/each}
		</ul>
	</div>
{/if}
