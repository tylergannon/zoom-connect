<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import UserPlus from '@lucide/svelte/icons/user-plus';
	import Users from '@lucide/svelte/icons/users';
	import Download from '@lucide/svelte/icons/download';
	import ChevronDown from '@lucide/svelte/icons/chevron-down';
	import Lock from '@lucide/svelte/icons/lock';
	import SearchX from '@lucide/svelte/icons/search-x';
	import CalendarX from '@lucide/svelte/icons/calendar-x';
	import * as Alert from '#lib/components/ui/alert';
	import * as Breadcrumb from '#lib/components/ui/breadcrumb';
	import * as Card from '#lib/components/ui/card';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu';
	import * as Empty from '#lib/components/ui/empty';
	import * as Tabs from '#lib/components/ui/tabs';
	import { Badge } from '#lib/components/ui/badge';
	import { Button } from '#lib/components/ui/button';
	import { Skeleton } from '#lib/components/ui/skeleton';
	import { Spinner } from '#lib/components/ui/spinner';
	import CallStatus from '#lib/components/CallStatus.svelte';
	import CopyButton from '#lib/components/CopyButton.svelte';
	import CallAside from './CallAside.svelte';
	import CallPages from './CallPages.svelte';
	import ScreenShare from './ScreenShare.svelte';
	import SharingDialog from './SharingDialog.svelte';
	import Transcript from './Transcript.svelte';
	import { api, APIError, dayLabel, errorMessage, minutes, timeOfDay, when } from '#lib/api';
	import type { Call, EventItem, Person, Share } from '#lib/types';

	// The call layout keys this component on the call id, so each call gets a
	// fresh instance; `artifactId` changes in place when a page is selected.
	let { callId: id, artifactId = '' }: { callId: string; artifactId?: string } = $props();

	let view = $state<'loading' | 'ready' | 'no-access' | 'missing' | 'error'>('loading');
	let call = $state<Call | null>(null);
	let events = $state<EventItem[]>([]);
	let shares = $state<Share[] | null>(null);
	let error = $state('');
	let peopleOnCall = $state<Person[]>([]);
	let connection = $state<'connecting' | 'open' | 'reconnecting' | 'paused' | 'closed'>('closed');
	let announcement = $state('');
	let tab = $state('transcript');
	let shareOpen = $state(false);
	let now = $state(Date.now());
	let source: EventSource | null = null;
	let checkingAccess = false;
	let participantTimer: ReturnType<typeof setTimeout> | undefined;
	// Requests started before the view unmounted must not open a stream after it.
	let closed = false;

	const path = (suffix = '') => `/api/calls/${encodeURIComponent(id)}${suffix}`;
	const chatCount = $derived(events.filter((e) => e.kind === 'chat').length);
	const frameCount = $derived(events.filter((e) => e.kind === 'screen.frame').length);
	const title = $derived(
		view === 'ready' && call ? `${call.topic}${call.status === 'live' ? ' (live)' : ''}`
		: view === 'no-access' ? 'No access'
		: view === 'missing' ? 'Call not found'
		: view === 'error' ? 'Couldn’t load call'
		: 'Loading call'
	);

	onMount(() => {
		void load();
		const timer = setInterval(() => (now = Date.now()), 30_000);
		return () => {
			closed = true;
			clearInterval(timer);
			clearTimeout(participantTimer);
			source?.close();
		};
	});

	async function load() {
		view = 'loading';
		error = '';
		try {
			const loaded = await api<Call>(path());
			const saved = await api<EventItem[]>(path('/events?from=0'));
			if (closed) return;
			call = loaded;
			events = saved;
			view = 'ready';
			void loadShares();
			connect(events.reduce((max, e) => Math.max(max, e.seq), 0));
		} catch (e) {
			if (!closed) fail(e);
		}
	}

	function fail(e: unknown) {
		if (e instanceof APIError && e.status === 403) {
			peopleOnCall = e.body?.participants || [];
			view = 'no-access';
		} else if (e instanceof APIError && e.status === 404) {
			view = 'missing';
		} else if (e instanceof APIError && e.status === 401) {
			window.location.assign('/?next=' + encodeURIComponent(window.location.pathname + window.location.hash));
		} else {
			error = errorMessage(e);
			view = 'error';
		}
	}

	async function loadShares() {
		try {
			shares = (await api<{ shares: Share[] }>(path('/shares'))).shares;
		} catch {
			shares = [];
		}
	}

	async function refreshCall() {
		try {
			call = await api<Call>(path());
		} catch {
			/* keep what we have; the stream reports access changes */
		}
	}

	function connect(from: number) {
		if (closed) return;
		if (call?.status !== 'live' && call?.status !== 'scheduled') return;
		source?.close();
		connection = 'connecting';
		source = new EventSource(`${path('/stream')}?from=${from}`);
		source.onopen = () => {
			if (connection === 'reconnecting') announcement = 'Reconnected.';
			if (connection !== 'paused') connection = 'open';
		};
		source.onerror = () => {
			if (connection === 'closed') return;
			connection = 'reconnecting';
			announcement = 'Connection lost. Reconnecting…';
			void recheckAccess();
		};
		const accept = (message: Event) => {
			let item: EventItem;
			try {
				item = JSON.parse((message as MessageEvent).data) as EventItem;
			} catch {
				return;
			}
			if (!events.some((e) => e.seq === item.seq)) events = [...events, item].sort((a, b) => a.seq - b.seq);
			if (item.kind === 'stream.started' && call) {
				const wasScheduled = call.status === 'scheduled';
				call = { ...call, status: 'live', started: call.started || new Date(item.ts).toISOString() };
				connection = 'open';
				announcement = wasScheduled ? 'The call has started. New lines appear in the transcript.' : 'Transcription resumed.';
				if (wasScheduled) void refreshCall();
			}
			if (item.kind === 'stream.paused') {
				connection = 'paused';
				announcement = 'The connection to Zoom paused. Waiting for it to come back.';
			}
			if ((item.kind === 'transcript' || item.kind === 'chat') && item.participant && call && !call.participants.some((p) => p.id === item.participant!.id)) {
				clearTimeout(participantTimer);
				participantTimer = setTimeout(refreshCall, 2000);
			}
			if (item.kind === 'call.cancelled') void finish('cancelled');
			if (item.kind === 'stream.stopped') void finish('stopped');
			if (item.kind === 'stream.interrupted') void finish('interrupted');
		};
		for (const kind of ['transcript', 'chat', 'screen.frame', 'stream.started', 'stream.paused', 'stream.stopped', 'stream.interrupted', 'call.cancelled']) {
			source.addEventListener(kind, accept);
		}
		source.addEventListener('end', () => void finish());
		source.onmessage = accept;
	}

	async function finish(status?: 'stopped' | 'interrupted' | 'cancelled') {
		connection = 'closed';
		source?.close();
		source = null;
		if (status && call) call = { ...call, status, ended: call.ended || new Date().toISOString() };
		announcement =
			status === 'cancelled' ? 'This call was cancelled.'
			: status === 'interrupted' ? 'Recording was interrupted. The transcript so far is saved.'
			: 'The call has ended. The full transcript is saved.';
		await refreshCall();
	}

	async function recheckAccess() {
		if (checkingAccess) return;
		checkingAccess = true;
		try {
			await api<Call>(path());
		} catch (e) {
			if (e instanceof APIError && (e.status === 401 || e.status === 403)) {
				connection = 'closed';
				source?.close();
				source = null;
				events = [];
				call = null;
				fail(e);
			}
		} finally {
			checkingAccess = false;
		}
	}

	function timeLine(c: Call) {
		if (c.status === 'scheduled') return c.scheduled_at ? when(c.scheduled_at) : 'No start time set';
		if (c.status === 'cancelled') return c.scheduled_at ? `Was planned for ${when(c.scheduled_at)}` : 'Cancelled before it started';
		if (!c.started) return '';
		if (c.status === 'live') return `Started ${timeOfDay(c.started)}`;
		return `${dayLabel(c.started)}, ${timeOfDay(c.started)}${c.ended ? `–${timeOfDay(c.ended)} · ${minutes(c.started, c.ended)}` : ''}`;
	}

	const downloads = [
		{ format: 'txt', label: 'Plain text (.txt)', hint: 'For reading or pasting into notes' },
		{ format: 'vtt', label: 'Captions (.vtt)', hint: 'Timed captions for video players' },
		{ format: 'json', label: 'Structured data (.json)', hint: 'Every event, for agents and scripts' }
	];
</script>

<svelte:head><title>{title} · Zoom Connect</title></svelte:head>

{#if view === 'loading'}
	<div class="flex flex-col gap-6">
		<p class="sr-only" role="status">Loading call…</p>
		<Skeleton class="h-4 w-40" />
		<Skeleton class="h-9 w-2/3" />
		<div class="grid gap-8 lg:grid-cols-[minmax(0,1fr)_20rem]">
			<div class="flex flex-col gap-4">{#each [0, 1, 2, 3] as i (i)}<Skeleton class="h-16 w-full" />{/each}</div>
			<Skeleton class="h-64 w-full" />
		</div>
	</div>
{:else if view === 'no-access'}
	<div class="mx-auto flex max-w-xl flex-col gap-6 py-6">
		<Empty.Root class="p-0">
			<Empty.Header>
				<Empty.Media variant="icon"><Lock aria-hidden="true" /></Empty.Media>
				<h1 class="text-2xl font-semibold tracking-tight">You don’t have access to this call</h1>
				<Empty.Description class="text-base">Only the people on a call, and people they share it with, can see it. Ask someone who was there to share it with you.</Empty.Description>
			</Empty.Header>
		</Empty.Root>
		{#if peopleOnCall.length}
			<Card.Root size="sm">
				<Card.Header><h2 class="font-semibold">People on this call</h2></Card.Header>
				<Card.Content>
					<ul class="flex flex-col gap-1.5">
						{#each peopleOnCall as person (person.id)}<li>{person.name}</li>{/each}
					</ul>
				</Card.Content>
			</Card.Root>
		{/if}
		<Button href="/calls" variant="outline" class="self-center">Back to calls</Button>
	</div>
{:else if view === 'missing'}
	<div class="mx-auto flex max-w-xl flex-col gap-6 py-6">
		<Empty.Root class="p-0">
			<Empty.Header>
				<Empty.Media variant="icon"><SearchX aria-hidden="true" /></Empty.Media>
				<h1 class="text-2xl font-semibold tracking-tight">We can’t find that call</h1>
				<Empty.Description class="text-base">The link may be incomplete, or the call was removed.</Empty.Description>
			</Empty.Header>
		</Empty.Root>
		<Button href="/calls" variant="outline" class="self-center">Back to calls</Button>
	</div>
{:else if view === 'error'}
	<div role="alert" class="mx-auto flex max-w-xl flex-col items-center gap-3 py-10 text-center">
		<h1 class="text-2xl font-semibold tracking-tight">We couldn’t load this call</h1>
		<p class="text-muted-foreground">{error}</p>
		<Button onclick={load}>Try again</Button>
	</div>
{:else if call}
	<div class="flex flex-col gap-6">
		<Breadcrumb.Root>
			<Breadcrumb.List>
				<Breadcrumb.Item><Breadcrumb.Link href="/calls">Calls</Breadcrumb.Link></Breadcrumb.Item>
				<Breadcrumb.Separator />
				<Breadcrumb.Item><Breadcrumb.Page role={undefined} aria-disabled={undefined} class="line-clamp-1">{call.topic}</Breadcrumb.Page></Breadcrumb.Item>
			</Breadcrumb.List>
		</Breadcrumb.Root>

		<header class="flex flex-wrap items-start justify-between gap-4">
			<div class="flex min-w-0 flex-col gap-2">
				<h1 class="text-3xl font-semibold tracking-tight [overflow-wrap:anywhere]">{call.topic}</h1>
				<div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-muted-foreground">
					<CallStatus status={call.status} />
					<span>{timeLine(call)}{#if call.status === 'live' && call.started}<span aria-hidden="true">{' · '}{minutes(call.started, null, now)}</span>{/if}</span>
					<span>{call.status === 'scheduled' || call.status === 'cancelled' ? 'Added by' : 'Hosted by'} {call.host.name}</span>
					<span role="status" class="inline-flex items-center gap-1.5 empty:hidden">
						{#if connection === 'reconnecting'}<Spinner aria-hidden="true" role="presentation" />Reconnecting…{:else if connection === 'paused'}Zoom connection paused{/if}
					</span>
				</div>
			</div>
			<div class="flex flex-wrap gap-2">
				<Button variant="outline" onclick={() => (shareOpen = true)}>
					{#if call.can_share}<UserPlus aria-hidden="true" />Share{:else}<Users aria-hidden="true" />Who has access{/if}
				</Button>
				<CopyButton text={`${page.url.origin}/calls/${call.id}`} label="Copy link" done="Link copied" />
				{#if call.status !== 'scheduled' && call.status !== 'cancelled'}
					<DropdownMenu.Root>
						<DropdownMenu.Trigger>
							{#snippet child({ props })}
								<Button {...props} variant="outline"><Download aria-hidden="true" />Download<ChevronDown aria-hidden="true" /></Button>
							{/snippet}
						</DropdownMenu.Trigger>
						<DropdownMenu.Content align="end" class="w-72">
							<DropdownMenu.Group>
								<DropdownMenu.GroupHeading>Download transcript</DropdownMenu.GroupHeading>
								{#each downloads as d (d.format)}
									<DropdownMenu.Item>
										{#snippet child({ props })}
											<a {...props} href={path(`/transcript?format=${d.format}`)} download class="{props.class} flex-col items-start gap-0.5">
												<span class="font-medium">{d.label}</span>
												<span class="text-xs text-muted-foreground">{d.hint}</span>
											</a>
										{/snippet}
									</DropdownMenu.Item>
								{/each}
							</DropdownMenu.Group>
						</DropdownMenu.Content>
					</DropdownMenu.Root>
				{/if}
			</div>
		</header>
		<p class="sr-only" role="status">{announcement}</p>

		<CallPages {call} selectedId={artifactId} {now} />

		<div class="grid items-start gap-8 lg:grid-cols-[minmax(0,1fr)_20rem]">
			{#if call.status === 'scheduled'}
				<section aria-labelledby="waiting-h" class="flex flex-col gap-4 rounded-xl border p-6">
					<h2 id="waiting-h" class="text-lg font-semibold">This call hasn’t started yet</h2>
					<p class="max-w-prose text-base text-muted-foreground">
						When Zoom starts streaming it, the transcript shows up here as people talk. Anyone you share it with now can follow along from the first word.
					</p>
					<ol class="flex max-w-prose list-decimal flex-col gap-2 pl-5 text-base">
						<li>Share the call with anyone who should follow it.</li>
						<li>Point an agent at the stream URL. It can connect now and will wait for the first event.</li>
						<li>Start the meeting in Zoom as usual. Nothing else to do.</li>
					</ol>
					{#if connection === 'open'}<p class="text-sm text-muted-foreground">This page is connected and will update when the call starts.</p>{/if}
				</section>
			{:else if call.status === 'cancelled'}
				<Alert.Root>
					<CalendarX aria-hidden="true" />
					<Alert.Title>This call was cancelled before it started</Alert.Title>
					<Alert.Description>Nothing was recorded. Add it again from Calls if the meeting is back on.</Alert.Description>
				</Alert.Root>
			{:else}
				<Tabs.Root bind:value={tab} class="min-w-0 gap-4">
					<Tabs.List aria-label="Call content">
						<Tabs.Trigger value="transcript" class="px-3">Transcript</Tabs.Trigger>
						<Tabs.Trigger value="chat" class="px-3">Chat <Badge variant="secondary">{chatCount}</Badge></Tabs.Trigger>
						{#if frameCount}<Tabs.Trigger value="screen" class="px-3">Shared screen <Badge variant="secondary">{frameCount}</Badge></Tabs.Trigger>{/if}
					</Tabs.List>
					<Tabs.Content value="transcript"><Transcript {call} {events} mode="transcript" /></Tabs.Content>
					<Tabs.Content value="chat"><Transcript {call} {events} mode="chat" /></Tabs.Content>
					{#if frameCount}<Tabs.Content value="screen"><ScreenShare callId={call.id} {events} /></Tabs.Content>{/if}
				</Tabs.Root>
			{/if}
			<CallAside {call} {events} {shares} {now} onshare={() => (shareOpen = true)} oncancelled={refreshCall} />
		</div>
	</div>
	<SharingDialog bind:open={shareOpen} callId={call.id} topic={call.topic} hostId={call.host.id} canShare={call.can_share} onchange={loadShares} />
{/if}
