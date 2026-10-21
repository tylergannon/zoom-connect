<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import KeyRound from '@lucide/svelte/icons/key-round';
	import * as Item from '#lib/components/ui/item';
	import * as Table from '#lib/components/ui/table';
	import { Button } from '#lib/components/ui/button';
	import CopyButton from '#lib/components/CopyButton.svelte';
	import { api, plural } from '#lib/api';
	import type { APIKey } from '#lib/types';

	let activeKeys = $state<number | null>(null);

	onMount(async () => {
		try {
			activeKeys = (await api<APIKey[]>('/api/keys')).filter((k) => !k.revoked_at).length;
		} catch {
			activeKeys = null;
		}
	});

	const origin = $derived(page.url.origin);
	const shell = $derived(`export ZC_KEY=zc_…\ncurl -H "Authorization: Bearer $ZC_KEY" ${origin}/api/v1/calls`);
	const announcements = $derived(`curl -N -H "Authorization: Bearer $ZC_KEY" \\\n  ${origin}/api/v1/stream`);
	const follow = $derived(`curl -N -H "Authorization: Bearer $ZC_KEY" \\\n  ${origin}/api/v1/calls/CALL_ID/stream`);
	const download = $derived(`curl -H "Authorization: Bearer $ZC_KEY" \\\n  "${origin}/api/v1/calls/CALL_ID/transcript?format=json"`);

	const exampleAnnouncements = `event: call.started
data: {"id":"01JB8Z4M2Q7T","topic":"Weekly product sync","status":"live"}

event: call.shared
data: {"id":"01JB5V1N8C3E","topic":"Q3 pricing","shared_by":"Marcus Reyes"}`;

	const exampleEvent = `id: 42
event: transcript
data: {"seq":42,"participant":{"name":"Priya Shah"},"data":{"text":"Let’s ship it Monday.","lang":"en"}}`;

	const sections = [
		{ id: 'key', title: 'Give your agent a key' },
		{ id: 'every-call', title: 'Hear about every call' },
		{ id: 'follow', title: 'Follow one call live' },
		{ id: 'download', title: 'Download a finished call' },
		{ id: 'reference', title: 'Reference' }
	];

	const endpoints = [
		['GET', '/api/v1/calls', 'Calls you can see, newest first'],
		['GET', '/api/v1/calls/{id}', 'One call: topic, times, host, people'],
		['GET', '/api/v1/calls/{id}/events', 'Every saved event, in order'],
		['GET', '/api/v1/calls/{id}/stream', 'Saved events, then live ones (SSE)'],
		['GET', '/api/v1/calls/{id}/transcript?format=', 'Transcript as txt, vtt or json'],
		['GET', '/api/v1/stream', 'Call announcements (SSE)'],
		['POST', '/api/v1/calls', 'Admins only: add a scheduled call from a Zoom invitation']
	];

	const streamEvents = [
		['transcript', 'A line someone said'],
		['chat', 'A message in the Zoom chat'],
		['screen.frame', 'A new image of a shared screen'],
		['stream.started, stream.paused', 'Recording began, or Zoom paused it for a moment'],
		['stream.stopped, stream.interrupted', 'The call ended, or recording was cut short'],
		['call.cancelled', 'A scheduled call was cancelled before it started'],
		['end', 'Nothing more is coming; close the connection']
	];

	const errors = [
		['401', 'The key is missing, wrong, or revoked'],
		['403', 'The key’s owner can’t see this call'],
		['404', 'There’s no call with that ID']
	];
</script>

<svelte:head><title>API guide · Zoom Connect</title></svelte:head>

{#snippet code(text: string, what: string)}
	<figure class="flex flex-col overflow-hidden rounded-lg border">
		<figcaption class="flex items-center justify-between gap-2 border-b bg-muted/50 py-1.5 pr-1.5 pl-3 text-sm text-muted-foreground">
			Shell
			<CopyButton {text} label="Copy" target={what} done="Copied to the clipboard" variant="ghost" size="sm" />
		</figcaption>
		<pre class="p-3 font-mono text-sm leading-relaxed whitespace-pre-wrap [overflow-wrap:anywhere]">{text}</pre>
	</figure>
{/snippet}

{#snippet example(text: string, caption: string)}
	<figure class="flex flex-col gap-1.5">
		<figcaption class="text-sm font-medium">{caption}</figcaption>
		<pre class="rounded-lg bg-muted p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap [overflow-wrap:anywhere]">{text}</pre>
	</figure>
{/snippet}

{#snippet step(n: number, id: string, title: string)}
	<h2 {id} class="flex scroll-mt-6 items-center gap-3 text-xl font-semibold tracking-tight">
		<span aria-hidden="true" class="grid size-7 shrink-0 place-items-center rounded-full bg-primary text-sm text-primary-foreground">{n}</span>
		<span><span class="sr-only">Step {n}: </span>{title}</span>
	</h2>
{/snippet}

<div class="flex flex-col gap-8">
	<header class="flex max-w-prose flex-col gap-2">
		<h1 class="text-3xl font-semibold tracking-tight">API guide</h1>
		<p class="text-base text-muted-foreground">
			Connect an agent to the calls you can see. It can follow a call as it happens or download the finished transcript. Every request uses an API key, and the key only sees what you see.
		</p>
	</header>

	<div class="grid items-start gap-10 lg:grid-cols-[minmax(0,1fr)_13rem]">
		<nav aria-labelledby="toc-h" class="flex flex-col gap-2 rounded-lg border p-4 lg:sticky lg:top-6 lg:order-last lg:border-0 lg:border-l lg:rounded-none lg:py-0">
			<h2 id="toc-h" class="text-sm font-semibold">On this page</h2>
			<ul class="flex flex-col gap-1.5 text-sm">
				{#each sections as s (s.id)}
					<li><a href="#{s.id}" class="text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">{s.title}</a></li>
				{/each}
			</ul>
		</nav>

		<div class="flex min-w-0 flex-col gap-12 text-base">
			<section aria-labelledby="key" class="flex max-w-3xl flex-col gap-4">
				{@render step(1, 'key', 'Give your agent a key')}
				<p class="max-w-prose">
					Create a key on the API keys page and put it in your agent’s environment as <code class="rounded bg-muted px-1 font-mono text-sm">ZC_KEY</code>. Send it on every request as a bearer token.
				</p>
				<Item.Root variant="outline">
					<Item.Media variant="icon"><KeyRound aria-hidden="true" /></Item.Media>
					<Item.Content>
						<Item.Title>{activeKeys === null ? 'Your API keys' : `You have ${plural(activeKeys, 'active key')}`}</Item.Title>
						<Item.Description>We can’t show a key again after it’s created. Make a new one for each agent.</Item.Description>
					</Item.Content>
					<Item.Actions><Button href="/keys" variant="outline" size="sm">Manage keys</Button></Item.Actions>
				</Item.Root>
				{@render code(shell, 'shell example')}
			</section>

			<section aria-labelledby="every-call" class="flex max-w-3xl flex-col gap-4">
				{@render step(2, 'every-call', 'Hear about every call')}
				<p class="max-w-prose">
					Keep one Server-Sent Events connection open. It tells your agent when a call you can see is scheduled, starts, is shared with you, ends, or is cancelled.
				</p>
				{@render code(announcements, 'announcements command')}
				{@render example(exampleAnnouncements, 'Example messages')}
			</section>

			<section aria-labelledby="follow" class="flex max-w-3xl flex-col gap-4">
				{@render step(3, 'follow', 'Follow one call live')}
				<p class="max-w-prose">
					Open the call’s stream. You get everything said so far, then new lines as people speak. You can connect before a scheduled call starts; the stream waits for it. Replace <code class="rounded bg-muted px-1 font-mono text-sm">CALL_ID</code> with the ID from the call’s page or from the list of calls.
				</p>
				{@render code(follow, 'call stream command')}
				{@render example(exampleEvent, 'Example message')}
				<h3 class="mt-2 text-base font-semibold">If the connection drops</h3>
				<p class="max-w-prose">
					Every message has an <code class="rounded bg-muted px-1 font-mono text-sm">id</code>. Reconnect with the <code class="rounded bg-muted px-1 font-mono text-sm">Last-Event-ID</code> header, or add <code class="rounded bg-muted px-1 font-mono text-sm">?from=42</code>, and the stream picks up after that message. If Zoom pauses the call, you get <code class="rounded bg-muted px-1 font-mono text-sm">stream.paused</code> and the connection stays open for up to two minutes. When the call is over, the stream sends <code class="rounded bg-muted px-1 font-mono text-sm">end</code>.
				</p>
			</section>

			<section aria-labelledby="download" class="flex max-w-3xl flex-col gap-4">
				{@render step(4, 'download', 'Download a finished call')}
				<p class="max-w-prose">
					Pick a format: <code class="rounded bg-muted px-1 font-mono text-sm">txt</code> for reading, <code class="rounded bg-muted px-1 font-mono text-sm">vtt</code> for captions with timings, or <code class="rounded bg-muted px-1 font-mono text-sm">json</code> for every event.
				</p>
				{@render code(download, 'download command')}
			</section>

			<section aria-labelledby="reference" class="flex max-w-3xl flex-col gap-6">
				<h2 id="reference" class="scroll-mt-6 text-xl font-semibold tracking-tight">Reference</h2>
				{@render table('Endpoints', ['Request', 'Returns'], endpoints.map(([m, p, d]) => [`${m} ${p}`, d]))}
				{@render table('Events on a call stream', ['Event', 'Meaning'], streamEvents)}
				{@render table('Errors', ['Status', 'What it means'], errors)}
			</section>
		</div>
	</div>
</div>

{#snippet table(caption: string, columns: string[], rows: string[][])}
	<div class="rounded-lg border">
		<Table.Root class="caption-top">
			<Table.Caption class="mt-0 border-b px-4 py-2.5 text-left text-base font-semibold text-foreground">{caption}</Table.Caption>
			<Table.Header>
				<Table.Row>
					{#each columns as c, i (c)}<Table.Head scope="col" class={i === 0 ? 'px-4' : ''}>{c}</Table.Head>{/each}
				</Table.Row>
			</Table.Header>
			<Table.Body>
				{#each rows as [first, second] (first)}
					<Table.Row>
						<th scope="row" class="px-4 py-2.5 text-left align-top font-mono text-xs font-normal [overflow-wrap:anywhere] whitespace-normal">{first}</th>
						<Table.Cell class="align-top whitespace-normal">{second}</Table.Cell>
					</Table.Row>
				{/each}
			</Table.Body>
		</Table.Root>
	</div>
{/snippet}
