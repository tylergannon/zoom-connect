<script lang="ts">
	import { onMount } from 'svelte';
	import Plus from '@lucide/svelte/icons/plus';
	import Search from '@lucide/svelte/icons/search';
	import Users from '@lucide/svelte/icons/users';
	import Calendar from '@lucide/svelte/icons/calendar';
	import FileText from '@lucide/svelte/icons/file-text';
	import Ellipsis from '@lucide/svelte/icons/ellipsis';
	import Link from '@lucide/svelte/icons/link';
	import UserPlus from '@lucide/svelte/icons/user-plus';
	import Download from '@lucide/svelte/icons/download';
	import Video from '@lucide/svelte/icons/video';
	import CircleAlert from '@lucide/svelte/icons/circle-alert';
	import { toast } from 'svelte-sonner';
	import * as Alert from '#lib/components/ui/alert';
	import * as Card from '#lib/components/ui/card';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu';
	import * as Empty from '#lib/components/ui/empty';
	import * as InputGroup from '#lib/components/ui/input-group';
	import * as Item from '#lib/components/ui/item';
	import * as ToggleGroup from '#lib/components/ui/toggle-group';
	import { Badge } from '#lib/components/ui/badge';
	import { Button } from '#lib/components/ui/button';
	import { Skeleton } from '#lib/components/ui/skeleton';
	import CallStatus from '#lib/components/CallStatus.svelte';
	import CopyButton from '#lib/components/CopyButton.svelte';
	import AddScheduledCall from './AddScheduledCall.svelte';
	import SharingDialog from './[id]/SharingDialog.svelte';
	import { ago, copyText, dayLabel, minutes, names, plural, timeOfDay, when } from '#lib/api';
	import { callIndex } from '#lib/calls.svelte';
	import { getUser } from '#lib/session';
	import type { Call } from '#lib/types';

	const user = getUser();
	type Filter = 'all' | 'mine' | 'shared';

	let filter = $state<Filter>('all');
	let query = $state('');
	let scheduleOpen = $state(false);
	let shareCall = $state<Call | null>(null);
	let shareOpen = $state(false);
	let origin = $state('');

	onMount(() => {
		origin = window.location.origin;
		void callIndex.refresh();
	});

	const attended = (c: Call) => c.is_participant || c.is_owner;
	const counts = $derived({
		all: callIndex.calls.length,
		mine: callIndex.calls.filter(attended).length,
		shared: callIndex.calls.filter((c) => !attended(c)).length
	});
	const filters: { value: Filter; label: string }[] = [
		{ value: 'all', label: 'All' },
		{ value: 'mine', label: 'I attended' },
		{ value: 'shared', label: 'Shared with me' }
	];

	const startOf = (c: Call) => new Date(c.started || c.scheduled_at || 0).getTime();
	const visible = $derived.by(() => {
		const q = query.trim().toLowerCase();
		return callIndex.calls.filter((c) => {
			if (filter === 'mine' && !attended(c)) return false;
			if (filter === 'shared' && attended(c)) return false;
			if (!q) return true;
			return c.topic.toLowerCase().includes(q) || [c.host, ...c.participants].some((p) => p.name.toLowerCase().includes(q));
		});
	});
	const live = $derived(visible.filter((c) => c.status === 'live').sort((a, b) => startOf(b) - startOf(a)));
	const upcoming = $derived(visible.filter((c) => c.status === 'scheduled').sort((a, b) => startOf(a) - startOf(b)));
	const past = $derived(visible.filter((c) => c.status !== 'live' && c.status !== 'scheduled').sort((a, b) => startOf(b) - startOf(a)));
	const pastByDay = $derived.by(() => {
		const days: { label: string; calls: Call[] }[] = [];
		for (const c of past) {
			const label = dayLabel(c.started || c.scheduled_at || new Date(0).toISOString());
			if (days.at(-1)?.label === label) days.at(-1)!.calls.push(c);
			else days.push({ label, calls: [c] });
		}
		return days;
	});

	function latestLine(c: Call) {
		const e = c.latest_event;
		if (!e || (e.kind !== 'transcript' && e.kind !== 'chat') || !e.data.text) return null;
		return { who: e.participant?.name, text: e.data.text };
	}

	function openShare(c: Call) {
		shareCall = c;
		shareOpen = true;
	}

	async function copyLink(c: Call) {
		if (await copyText(`${origin}/calls/${c.id}`)) toast.success('Link copied');
		else toast.error('Couldn’t copy', { description: 'Your browser blocked the clipboard.' });
	}

	function pastDetails(c: Call) {
		const parts = [c.started ? timeOfDay(c.started) : c.scheduled_at ? `Planned for ${timeOfDay(c.scheduled_at)}` : ''];
		if (c.started && c.ended) parts.push(minutes(c.started, c.ended));
		if (c.participants.length) parts.push(plural(c.participants.length, 'person', 'people'));
		if (c.shared_by && !attended(c)) parts.push(`Shared by ${c.shared_by}`);
		return parts.filter(Boolean).join(' · ');
	}
</script>

<svelte:head><title>Calls · Zoom Connect</title></svelte:head>

<div class="flex flex-col gap-9">
	<header class="flex flex-wrap items-end justify-between gap-4">
		<div class="flex min-w-0 flex-col gap-1.5">
			<h1 class="text-3xl font-semibold tracking-tight">Calls</h1>
			<p class="text-base text-muted-foreground">Calls you were in, and calls someone shared with you.</p>
		</div>
		{#if user().is_admin}
			<Button onclick={() => (scheduleOpen = true)}><Plus aria-hidden="true" data-icon="inline-start" />Add scheduled call</Button>
		{/if}
	</header>

	<div class="flex flex-wrap items-center justify-between gap-3">
		<ToggleGroup.Root
			type="single"
			variant="outline"
			spacing={2}
			aria-label="Show calls"
			class="flex-wrap"
			value={filter}
			onValueChange={(v) => { if (v) filter = v as Filter; }}
		>
			{#each filters as f (f.value)}
				<ToggleGroup.Item value={f.value} class="h-9 px-3 data-[state=on]:bg-accent">
					{f.label} <span class="font-normal text-muted-foreground">{counts[f.value]}</span>
				</ToggleGroup.Item>
			{/each}
		</ToggleGroup.Root>
		<div role="search" class="max-w-sm flex-[1_1_16rem]">
			<label for="call-search" class="sr-only">Search calls</label>
			<InputGroup.Root>
				<InputGroup.Addon><Search aria-hidden="true" /></InputGroup.Addon>
				<InputGroup.Input id="call-search" type="search" placeholder="Search by title or person" bind:value={query} />
			</InputGroup.Root>
		</div>
	</div>

	{#if callIndex.status === 'loading'}
		<div class="flex flex-col gap-3" aria-busy="true">
			<span class="sr-only" role="status">Loading calls…</span>
			{#each [0, 1, 2] as i (i)}<Skeleton class="h-16 w-full" />{/each}
		</div>
	{:else if callIndex.status === 'error'}
		<Alert.Root variant="destructive">
			<CircleAlert aria-hidden="true" />
			<Alert.Title>We couldn’t load your calls</Alert.Title>
			<Alert.Description>
				<p>{callIndex.error}</p>
				<Button variant="outline" size="sm" class="mt-2" onclick={() => callIndex.refresh()}>Try again</Button>
			</Alert.Description>
		</Alert.Root>
	{:else if counts.all === 0}
		<Empty.Root class="border">
			<Empty.Header>
				<Empty.Media variant="icon"><Video aria-hidden="true" /></Empty.Media>
				<Empty.Title>No calls yet</Empty.Title>
				<Empty.Description>When you’re in a Zoom call that Zoom Connect records, or someone shares one with you, it shows up here.</Empty.Description>
			</Empty.Header>
		</Empty.Root>
	{:else if visible.length === 0}
		<Empty.Root class="border">
			<Empty.Header>
				<Empty.Title>No calls match</Empty.Title>
				<Empty.Description>
					{query.trim() ? `Nothing matches “${query.trim()}”` : 'Nothing here'}{filter !== 'all' ? ` in ${filters.find((f) => f.value === filter)?.label.toLowerCase()}` : ''}.
				</Empty.Description>
			</Empty.Header>
			<Empty.Content>
				<Button variant="outline" onclick={() => { query = ''; filter = 'all'; }}>Show all calls</Button>
			</Empty.Content>
		</Empty.Root>
	{:else}
		{#if live.length}
			<section aria-labelledby="live-h" class="flex flex-col gap-3.5">
				<h2 id="live-h" class="flex items-center gap-2 text-lg font-semibold">Live now <Badge variant="secondary">{live.length}</Badge></h2>
				<ul class="grid grid-cols-[repeat(auto-fill,minmax(min(100%,20rem),1fr))] gap-4">
					{#each live as c (c.id)}
						{@const line = latestLine(c)}
						<li>
							<Card.Root class="relative h-full">
								<Card.Header>
									<div class="flex flex-wrap items-center gap-2 text-sm">
										<CallStatus status="live" />
										{#if c.started}<span class="text-muted-foreground">Started {ago(c.started, callIndex.now)}</span>{/if}
									</div>
									<h3 class="mt-1 text-base font-semibold">
										<a href="/calls/{c.id}" class="underline-offset-4 after:absolute after:inset-0 hover:underline">{c.topic}</a>
									</h3>
								</Card.Header>
								<Card.Content>
									{#if line}
										<p class="line-clamp-3 leading-relaxed">{#if line.who}<span class="font-medium">{line.who}:</span>{' '}{/if}<span class="text-muted-foreground">{line.text}</span></p>
									{:else}
										<p class="text-muted-foreground">Waiting for the first words.</p>
									{/if}
								</Card.Content>
								<Card.Footer class="gap-2 text-sm text-muted-foreground">
									<Users class="size-4 shrink-0" aria-hidden="true" />
									<span>{c.participants.length ? names(c.participants) : 'No one has joined yet'}</span>
								</Card.Footer>
							</Card.Root>
						</li>
					{/each}
				</ul>
			</section>
		{/if}

		{#if upcoming.length}
			<section aria-labelledby="upcoming-h" class="flex flex-col gap-3.5">
				<h2 id="upcoming-h" class="text-lg font-semibold">Upcoming</h2>
				<ul class="flex flex-col gap-2">
					{#each upcoming as c (c.id)}
						<li>
							<Item.Root variant="outline">
								<Item.Media variant="icon"><Calendar aria-hidden="true" /></Item.Media>
								<Item.Content class="min-w-56">
									<h3 class="font-medium"><a href="/calls/{c.id}" class="underline-offset-4 hover:underline">{c.topic}</a></h3>
									<Item.Description>
										{#if c.scheduled_at}<time datetime={c.scheduled_at}>{when(c.scheduled_at)}</time>{:else}No start time given{/if}
										· Added by {c.host.name}
									</Item.Description>
								</Item.Content>
								<Item.Actions>
									<CallStatus status="scheduled" />
									<CopyButton text="{origin}/calls/{c.id}" label="Copy link" target="to {c.topic}" done="Link copied" size="sm" />
								</Item.Actions>
							</Item.Root>
						</li>
					{/each}
				</ul>
			</section>
		{/if}

		{#if past.length}
			<section aria-labelledby="past-h" class="flex flex-col gap-3.5">
				<div class="flex flex-wrap items-baseline justify-between gap-2">
					<h2 id="past-h" class="text-lg font-semibold">Past calls</h2>
					<p class="text-sm text-muted-foreground" role="status">{plural(past.length, 'call')}</p>
				</div>
				{#each pastByDay as day (day.label)}
					<h3 class="mt-1 text-sm font-medium text-muted-foreground">{day.label}</h3>
					<ul class="flex flex-col divide-y rounded-lg border">
						{#each day.calls as c (c.id)}
							<li>
								<Item.Root class="rounded-none">
									<Item.Media variant="icon"><FileText aria-hidden="true" /></Item.Media>
									<Item.Content class="min-w-56">
										<h4 class="font-medium"><a href="/calls/{c.id}" class="underline-offset-4 hover:underline">{c.topic}</a></h4>
										<Item.Description>{pastDetails(c)}</Item.Description>
									</Item.Content>
									<Item.Actions>
										{#if c.status !== 'stopped'}<CallStatus status={c.status} />{/if}
										<DropdownMenu.Root>
											<DropdownMenu.Trigger>
												{#snippet child({ props })}
													<Button {...props} variant="ghost" size="icon">
														<Ellipsis aria-hidden="true" /><span class="sr-only">Actions for {c.topic}</span>
													</Button>
												{/snippet}
											</DropdownMenu.Trigger>
											<DropdownMenu.Content align="end" class="w-60">
												<DropdownMenu.Item onSelect={() => copyLink(c)}><Link aria-hidden="true" />Copy link</DropdownMenu.Item>
												{#if c.can_share}
													<DropdownMenu.Item onSelect={() => openShare(c)}><UserPlus aria-hidden="true" />Share…</DropdownMenu.Item>
												{/if}
												{#if c.status !== 'cancelled'}
													<DropdownMenu.Separator />
													<DropdownMenu.Group>
														<DropdownMenu.GroupHeading>Download transcript</DropdownMenu.GroupHeading>
														{#each [['txt', 'Plain text (.txt)'], ['vtt', 'Captions (.vtt)'], ['json', 'Structured data (.json)']] as [format, label] (format)}
															<DropdownMenu.Item>
																{#snippet child({ props })}
																	<a {...props} href="/api/calls/{c.id}/transcript?format={format}" download><Download aria-hidden="true" />{label}</a>
																{/snippet}
															</DropdownMenu.Item>
														{/each}
													</DropdownMenu.Group>
												{/if}
											</DropdownMenu.Content>
										</DropdownMenu.Root>
									</Item.Actions>
								</Item.Root>
							</li>
						{/each}
					</ul>
				{/each}
			</section>
		{/if}
	{/if}
</div>

{#if user().is_admin}
	<AddScheduledCall bind:open={scheduleOpen} />
{/if}
{#if shareCall}
	<SharingDialog bind:open={shareOpen} callId={shareCall.id} topic={shareCall.topic} hostId={shareCall.host.id} canShare={shareCall.can_share} />
{/if}
