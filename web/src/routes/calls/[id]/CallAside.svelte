<script lang="ts">
	import { page } from '$app/state';
	import * as Avatar from '#lib/components/ui/avatar';
	import * as Card from '#lib/components/ui/card';
	import { Badge } from '#lib/components/ui/badge';
	import { Button } from '#lib/components/ui/button';
	import CopyButton from '#lib/components/CopyButton.svelte';
	import CancelCall from './CancelCall.svelte';
	import { initials, minutes, names, plural, when } from '#lib/api';
	import { getUser } from '#lib/session';
	import type { Call, EventItem, Share } from '#lib/types';

	let {
		call,
		events,
		shares,
		now,
		onshare,
		oncancelled
	}: { call: Call; events: EventItem[]; shares: Share[] | null; now: number; onshare: () => void; oncancelled: () => void } = $props();

	const user = getUser();
	const id = $props.id();
	const upcoming = $derived(call.status === 'scheduled');
	const streaming = $derived(call.status === 'live' || upcoming);
	const agentURL = $derived(
		`${page.url.origin}/api/v1/calls/${call.id}/${streaming ? 'stream' : 'transcript?format=json'}`
	);
	const lines = $derived(events.filter((e) => e.kind === 'transcript' && e.data.final !== false).length);
	const chats = $derived(events.filter((e) => e.kind === 'chat').length);
	const people = $derived([...call.participants].sort((a, b) => Number(b.id === call.host.id) - Number(a.id === call.host.id)));

	const access = $derived.by(() => {
		if (!shares) return '';
		const others = shares.map((s) => ({ id: s.id, name: s.name || s.email }));
		if (upcoming) {
			return others.length
				? `${call.is_owner ? 'You' : call.host.name}, and ${plural(others.length, 'person', 'people')} it was shared with: ${names(others, 4)}. Everyone who joins the call gets access too.`
				: 'Everyone who joins the call gets access. Share it now so others can follow from the first word.';
		}
		const n = call.participants.length;
		return others.length
			? `Everyone on the call, and ${plural(others.length, 'person', 'people')} it was shared with: ${names(others, 4)}.`
			: `Only the ${n === 1 ? 'person' : `${n} people`} on the call. It hasn’t been shared with anyone else.`;
	});

	function meetingNumber(v: string) {
		const d = v.replace(/\D/g, '');
		if (d.length === 11) return `${d.slice(0, 3)} ${d.slice(3, 7)} ${d.slice(7)}`;
		if (d.length === 10) return `${d.slice(0, 3)} ${d.slice(3, 6)} ${d.slice(6)}`;
		return v;
	}
</script>

<aside aria-label="About this call" class="flex flex-col gap-4">
	{#if !upcoming && people.length}
		<Card.Root size="sm">
			<Card.Header>
				<h2 id="{id}-people" class="flex items-center gap-2 font-semibold">People <Badge variant="secondary">{people.length}</Badge></h2>
			</Card.Header>
			<Card.Content>
				<ul aria-labelledby="{id}-people" class="flex flex-col gap-2.5">
					{#each people as person (person.id)}
						<li class="flex items-center gap-2.5">
							<Avatar.Root class="size-7" aria-hidden="true"><Avatar.Fallback class="text-xs">{initials(person.name)}</Avatar.Fallback></Avatar.Root>
							<span class="min-w-0 flex-1 truncate">{person.name}</span>
							{#if person.id === call.host.id}<Badge variant="outline">Host</Badge>{/if}
							{#if person.id === user().id}<span class="text-sm text-muted-foreground">You</span>{/if}
						</li>
					{/each}
				</ul>
			</Card.Content>
		</Card.Root>
	{/if}

	<Card.Root size="sm">
		<Card.Header>
			<h2 class="font-semibold">Who can see this</h2>
			<Card.Description>{access || 'Loading…'}</Card.Description>
		</Card.Header>
		<Card.Content>
			<Button variant="outline" class="w-full" onclick={onshare}>
				{call.can_share ? (shares?.length ? 'Manage access' : 'Share with someone') : 'See who has access'}
			</Button>
		</Card.Content>
	</Card.Root>

	{#if call.status !== 'cancelled'}
		<Card.Root size="sm">
			<Card.Header>
				<h2 class="font-semibold">Agent access</h2>
				<Card.Description>
					{#if upcoming}Connect now. Events start when the call does.
					{:else if streaming}An agent with one of your API keys can follow this call as it happens.
					{:else}An agent with one of your API keys can fetch the whole transcript.{/if}
				</Card.Description>
			</Card.Header>
			<Card.Content class="flex flex-col gap-2">
				<label for="{id}-agent-url" class="text-sm font-medium">{streaming ? 'Live stream (SSE)' : 'Transcript (JSON)'}</label>
				<div class="flex gap-1.5">
					<input
						id="{id}-agent-url"
						readonly
						value={agentURL}
						class="h-9 min-w-0 flex-1 rounded-md border bg-muted/50 px-2.5 font-mono text-xs"
						onfocus={(e) => e.currentTarget.select()}
					/>
					<CopyButton text={agentURL} iconOnly label="Copy {streaming ? 'stream' : 'transcript'} URL" done="URL copied" size="icon" />
				</div>
				<a href="/agent-api" class="text-sm font-medium underline underline-offset-4">How agents connect</a>
			</Card.Content>
		</Card.Root>
	{/if}

	<Card.Root size="sm">
		<Card.Header><h2 class="font-semibold">Details</h2></Card.Header>
		<Card.Content>
			<dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
				{#if upcoming || call.status === 'cancelled'}
					{#if call.scheduled_at}<dt class="text-muted-foreground">Expected</dt><dd>{when(call.scheduled_at)}</dd>{/if}
				{:else if call.started}
					<dt class="text-muted-foreground">Started</dt><dd>{when(call.started)}</dd>
					<dt class="text-muted-foreground">{call.status === 'live' ? 'Running for' : 'Duration'}</dt>
					<dd>{minutes(call.started, call.status === 'live' ? null : call.ended, now)}</dd>
					<dt class="text-muted-foreground">Lines</dt><dd>{lines}</dd>
					<dt class="text-muted-foreground">Chat</dt><dd>{plural(chats, 'message')}</dd>
				{/if}
				{#if call.zoom_meeting_id}<dt class="text-muted-foreground">Zoom meeting</dt><dd>{meetingNumber(call.zoom_meeting_id)}</dd>{/if}
				<dt class="text-muted-foreground">Call ID</dt><dd class="font-mono text-xs leading-5 break-all">{call.id}</dd>
			</dl>
		</Card.Content>
		{#if upcoming && call.is_owner}
			<Card.Footer><CancelCall {call} {oncancelled} /></Card.Footer>
		{/if}
	</Card.Root>
</aside>
