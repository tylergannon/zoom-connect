<script lang="ts">
	import { onMount, tick, untrack } from 'svelte';
	import { page } from '$app/state';
	import Search from '@lucide/svelte/icons/search';
	import ChevronUp from '@lucide/svelte/icons/chevron-up';
	import ChevronDown from '@lucide/svelte/icons/chevron-down';
	import ArrowDown from '@lucide/svelte/icons/arrow-down';
	import ALargeSmall from '@lucide/svelte/icons/a-large-small';
	import { toast } from 'svelte-sonner';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu';
	import * as InputGroup from '#lib/components/ui/input-group';
	import { Badge } from '#lib/components/ui/badge';
	import { Button } from '#lib/components/ui/button';
	import { Label } from '#lib/components/ui/label';
	import { Switch } from '#lib/components/ui/switch';
	import { clock, copyText, plural, timeOfDay } from '#lib/api';
	import { preferences, transcriptSizes, type TranscriptSize } from '#lib/preferences.svelte';
	import { buildTurns, findMatches, lineCount } from './transcript';
	import type { Call, EventItem } from '#lib/types';

	let { call, events, mode }: { call: Call; events: EventItem[]; mode: 'transcript' | 'chat' } = $props();

	const id = $props.id();
	const live = $derived(call.status === 'live');
	const turns = $derived(buildTurns(events, mode));
	const lines = $derived(lineCount(turns));
	const startMs = $derived(call.started ? Date.parse(call.started) : (events[0]?.ts ?? 0));
	const following = $derived(mode === 'transcript' && live);

	let query = $state('');
	let current = $state(0);
	const search = $derived(findMatches(turns, query));

	// Following a live call: the page scrolls with new lines while the end of
	// the transcript is in view, and offers a jump back when it isn't.
	let end: HTMLElement | null = null;
	let atEnd = $state(true);
	let seen = $state(0);
	const unread = $derived(following && !atEnd ? Math.max(0, lines - seen) : 0);

	function watchEnd(node: HTMLElement) {
		end = node;
		const observer = new IntersectionObserver(
			([entry]) => {
				atEnd = entry.isIntersecting;
				if (!atEnd) seen = lines;
			},
			{ rootMargin: '0px 0px 96px 0px' }
		);
		observer.observe(node);
		return () => observer.disconnect();
	}

	function keepUp(node: HTMLElement) {
		void lines;
		if (untrack(() => following && atEnd && !query.trim())) node.scrollIntoView({ block: 'end' });
	}

	// Lines that arrive after reading aloud starts, for the screen reader log.
	const latestSeq = () => events.reduce((m, e) => Math.max(m, e.seq), 0);
	let readFrom = $state(untrack(latestSeq));
	const spoken = $derived(
		turns
			.flatMap((t) => (t.type === 'system' ? [] : t.paras.filter((p) => !p.partial && p.seq > readFrom).map((p) => ({ seq: p.seq, text: `${t.who}: ${p.text}` }))))
			.slice(-20)
	);

	function setReadAloud(on: boolean) {
		readFrom = latestSeq();
		preferences.setReadAloud(on);
	}

	onMount(() => {
		const hash = page.url.hash.slice(1);
		if (hash && mode === 'transcript') document.getElementById(hash)?.scrollIntoView({ block: 'start' });
		else if (following) end?.scrollIntoView({ block: 'end' });
	});

	function jumpToLatest() {
		end?.scrollIntoView({ block: 'end' });
		document.getElementById(`${id}-end`)?.focus({ preventScroll: true });
	}

	async function showMatch(n: number) {
		if (!search.total) return;
		current = (n + search.total) % search.total;
		await tick();
		document.getElementById(`${id}-list`)?.querySelector('mark[data-current]')?.scrollIntoView({ block: 'center' });
	}

	function onSearchKey(event: KeyboardEvent) {
		if (event.key === 'Enter') {
			event.preventDefault();
			void showMatch(current + (event.shiftKey ? -1 : 1));
		}
	}

	async function copyMoment(seq: number) {
		if (await copyText(`${page.url.origin}${page.url.pathname}#t-${seq}`)) toast.success('Link to this moment copied');
	}

	const sizeLabel = $derived(transcriptSizes.find((s) => s.value === preferences.transcriptSize)?.label ?? 'Default');
	const iso = (ts: number) => new Date(ts).toISOString();
</script>

<div class="flex flex-col gap-4">
	<div role="group" aria-label="{mode === 'chat' ? 'Chat' : 'Transcript'} tools" class="flex flex-wrap items-center gap-2">
		<div role="search" class="min-w-0 flex-[1_1_16rem]">
			<label for="{id}-search" class="sr-only">Search {mode === 'chat' ? 'chat' : 'transcript'}</label>
			<InputGroup.Root>
				<InputGroup.Addon><Search aria-hidden="true" /></InputGroup.Addon>
				<InputGroup.Input
					id="{id}-search"
					type="search"
					placeholder="Search {mode === 'chat' ? 'chat' : 'transcript'}"
					aria-describedby={query.trim() ? `${id}-count` : undefined}
					bind:value={query}
					oninput={() => showMatch(0)}
					onkeydown={onSearchKey}
				/>
				{#if query.trim()}
					<InputGroup.Addon align="inline-end">
						<span id="{id}-count" role="status" class="text-sm whitespace-nowrap tabular-nums">
							{search.total ? `${current + 1} of ${search.total}` : 'No matches'}
						</span>
						<InputGroup.Button size="icon-xs" disabled={!search.total} onclick={() => showMatch(current - 1)}>
							<ChevronUp aria-hidden="true" /><span class="sr-only">Previous match</span>
						</InputGroup.Button>
						<InputGroup.Button size="icon-xs" disabled={!search.total} onclick={() => showMatch(current + 1)}>
							<ChevronDown aria-hidden="true" /><span class="sr-only">Next match</span>
						</InputGroup.Button>
					</InputGroup.Addon>
				{/if}
			</InputGroup.Root>
		</div>
		<DropdownMenu.Root>
			<DropdownMenu.Trigger>
				{#snippet child({ props })}
					<Button {...props} variant="outline"><ALargeSmall aria-hidden="true" />Text size: {sizeLabel}</Button>
				{/snippet}
			</DropdownMenu.Trigger>
			<DropdownMenu.Content align="end">
				<DropdownMenu.RadioGroup value={preferences.transcriptSize} onValueChange={(v) => preferences.setTranscriptSize(v as TranscriptSize)}>
					{#each transcriptSizes as size (size.value)}
						<DropdownMenu.RadioItem value={size.value}>{size.label}</DropdownMenu.RadioItem>
					{/each}
				</DropdownMenu.RadioGroup>
			</DropdownMenu.Content>
		</DropdownMenu.Root>
		{#if following}
			<div class="flex items-center gap-2 px-1">
				<Switch id="{id}-read" checked={preferences.readAloud} onCheckedChange={setReadAloud} aria-describedby="{id}-read-help" />
				<Label for="{id}-read">Read new lines aloud</Label>
				<span id="{id}-read-help" class="sr-only">Your screen reader announces each new line as it’s said.</span>
			</div>
		{/if}
	</div>

	{#if turns.length === 0}
		<div class="rounded-lg border border-dashed px-6 py-12 text-center text-muted-foreground">
			{#if mode === 'chat'}
				No chat messages{live ? ' yet' : ''}.
			{:else if live}
				Waiting for the first words. New lines appear here as people talk.
			{:else}
				No one spoke while this call was recorded.
			{/if}
		</div>
	{:else}
		<ol id="{id}-list" aria-label={mode === 'chat' ? 'Chat messages' : 'Transcript'} class="flex flex-col text-transcript leading-relaxed">
			{#each turns as t (t.seq)}
				{#if t.type === 'system'}
					<li class="flex items-center gap-3 py-3 text-sm text-muted-foreground">
						<span aria-hidden="true" class="h-px flex-1 bg-border"></span>
						<span>{t.text} · <time datetime={iso(t.ts)}>{clock(t.ts - startMs)}</time></span>
						<span aria-hidden="true" class="h-px flex-1 bg-border"></span>
					</li>
				{:else}
					<li id={mode === 'transcript' ? `t-${t.seq}` : undefined} class="scroll-mt-20 rounded-md px-2 py-3 -mx-2 target:bg-highlight/20">
						<div class="flex flex-wrap items-baseline gap-x-2 text-sm">
							<span class="font-semibold">{t.who}</span>
							{#if t.type === 'chat' && mode === 'transcript'}<Badge variant="outline">Chat</Badge>{/if}
							<a
								href="#t-{t.seq}"
								class="font-mono text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
								title="{timeOfDay(t.ts, true)}. Copy a link to this moment."
								onclick={() => copyMoment(t.seq)}
							>
								<time datetime={iso(t.ts)}>{clock(t.ts - startMs)}</time><span class="sr-only">, {timeOfDay(t.ts, true)}. Copy a link to this moment</span>
							</a>
						</div>
						{#each t.paras as p (p.seq)}
							{@const parts = search.segments.get(p.seq)}
							<p class="mt-1 max-w-[70ch] [overflow-wrap:anywhere]">
								{#if parts}
									{#each parts as part, i (i)}
										{#if part.match === undefined}{part.text}{:else}<mark data-current={part.match === current || undefined}>{part.text}</mark>{#if part.match === current}<span class="sr-only"> (current match)</span>{/if}{/if}
									{/each}
								{:else}
									{p.text}
								{/if}
								{#if p.partial}<span class="sr-only"> (still transcribing)</span>{/if}
							</p>
							{#if p.partial}
								<span aria-hidden="true" class="mt-1 inline-flex items-center gap-1.5 text-xs text-muted-foreground">
									<span class="size-1.5 animate-pulse rounded-full bg-live"></span>Transcribing
								</span>
							{/if}
						{/each}
					</li>
				{/if}
			{/each}
		</ol>
	{/if}
	<!-- In flow, so it marks the real end of the list; the text inside is for screen readers. -->
	<div {@attach watchEnd} {@attach keepUp} class="relative h-px">
		<p id="{id}-end" tabindex="-1" class="sr-only">End of {mode === 'chat' ? 'chat' : 'transcript'}</p>
	</div>

	{#if unread}
		<div class="pointer-events-none sticky bottom-4 flex justify-center">
			<Button class="pointer-events-auto shadow-lg" onclick={jumpToLatest}>
				<ArrowDown aria-hidden="true" />Jump to latest · {plural(unread, 'new line')}
			</Button>
		</div>
	{/if}
	{#if following}
		<p role="status" class="sr-only">{unread && !preferences.readAloud ? 'New lines below. Use Jump to latest to catch up.' : ''}</p>
		<div role="log" aria-label="New lines" class="sr-only">
			{#if preferences.readAloud}
				{#each spoken as line (line.seq)}<p>{line.text}</p>{/each}
			{/if}
		</div>
	{/if}
</div>
