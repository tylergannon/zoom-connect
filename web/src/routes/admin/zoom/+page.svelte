<script lang="ts">
	import { onMount } from 'svelte';
	import RefreshCw from '@lucide/svelte/icons/refresh-cw';
	import FlaskConical from '@lucide/svelte/icons/flask-conical';
	import ExternalLink from '@lucide/svelte/icons/external-link';
	import Play from '@lucide/svelte/icons/play';
	import Square from '@lucide/svelte/icons/square';
	import ShieldAlert from '@lucide/svelte/icons/shield-alert';
	import { toast } from 'svelte-sonner';
	import * as Alert from '#lib/components/ui/alert';
	import * as Card from '#lib/components/ui/card';
	import * as Empty from '#lib/components/ui/empty';
	import * as Field from '#lib/components/ui/field';
	import * as Table from '#lib/components/ui/table';
	import { Badge } from '#lib/components/ui/badge';
	import { Button } from '#lib/components/ui/button';
	import { Input } from '#lib/components/ui/input';
	import { Skeleton } from '#lib/components/ui/skeleton';
	import { Spinner } from '#lib/components/ui/spinner';
	import { Switch } from '#lib/components/ui/switch';
	import CallStatus from '#lib/components/CallStatus.svelte';
	import CopyButton from '#lib/components/CopyButton.svelte';
	import { ago, api, APIError, errorMessage } from '#lib/api';
	import { callIndex } from '#lib/calls.svelte';
	import type { Call, ZoomSettings } from '#lib/types';

	let settings = $state<ZoomSettings | null>(null);
	let view = $state<'loading' | 'ready' | 'forbidden' | 'error'>('loading');
	let loadError = $state('');
	let checking = $state(false);
	let checked = $state('');

	let clientId = $state('');
	let clientSecret = $state('');
	let transcript = $state(true);
	let chat = $state(true);
	let saving = $state(false);
	let saveError = $state('');
	let saved = $state('');

	let demoCall = $state<Call | null>(null);
	let demoBusy = $state(false);
	let demoError = $state('');

	const demo = $derived(settings?.mode === 'demo');

	onMount(() => {
		void load();
	});

	function apply(next: ZoomSettings) {
		settings = next;
		clientId = next.credentials.client_id || '';
		transcript = next.capture.transcript;
		chat = next.capture.chat;
	}

	async function load() {
		try {
			apply(await api<ZoomSettings>('/api/admin/zoom'));
			view = 'ready';
		} catch (e) {
			if (e instanceof APIError && (e.status === 404 || e.status === 403)) view = 'forbidden';
			else {
				loadError = errorMessage(e);
				view = 'error';
			}
		}
	}

	async function checkAgain() {
		checking = true;
		checked = '';
		await load();
		checking = false;
		checked = 'Status updated.';
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		saving = true;
		saveError = '';
		saved = '';
		try {
			apply(
				await api<ZoomSettings>('/api/admin/zoom', {
					method: 'POST',
					body: JSON.stringify({ client_id: clientId.trim(), client_secret: clientSecret || undefined, capture: { transcript, chat } })
				})
			);
			clientSecret = '';
			saved = 'Saved';
		} catch (e) {
			saveError = errorMessage(e);
		} finally {
			saving = false;
		}
	}

	async function startDemo() {
		demoBusy = true;
		demoError = '';
		try {
			demoCall = await api<Call>('/api/demo/calls', { method: 'POST', body: '{}' });
			toast.success('Simulated call started', { description: demoCall.topic });
			void callIndex.refresh();
		} catch (e) {
			demoError = errorMessage(e);
		} finally {
			demoBusy = false;
		}
	}

	async function stopDemo() {
		if (!demoCall) return;
		demoBusy = true;
		demoError = '';
		try {
			demoCall = await api<Call>(`/api/demo/calls/${encodeURIComponent(demoCall.id)}/stop`, { method: 'POST', body: '{}' });
			toast.success('Simulated call stopped');
			void callIndex.refresh();
		} catch (e) {
			demoError = errorMessage(e);
		} finally {
			demoBusy = false;
		}
	}

	const messages: Record<string, string> = {
		'meeting.rtms_started': 'Recording started',
		'meeting.rtms_stopped': 'Recording stopped',
		'meeting.rtms_interrupted': 'Recording interrupted',
		'endpoint.url_validation': 'Zoom checked the webhook URL'
	};

	function result(outcome: string): { text: string; variant: 'secondary' | 'outline' | 'destructive' } {
		if (outcome.startsWith('accepted')) return { text: 'Accepted', variant: 'secondary' };
		if (outcome === 'validation answered') return { text: 'Answered', variant: 'secondary' };
		if (outcome === 'rejected') return { text: 'Rejected', variant: 'destructive' };
		if (outcome === 'bad signature') return { text: 'Bad signature', variant: 'destructive' };
		if (outcome === 'ignored') return { text: 'Ignored', variant: 'outline' };
		return { text: outcome[0]?.toUpperCase() + outcome.slice(1), variant: 'outline' };
	}
</script>

<svelte:head><title>Zoom connection · Zoom Connect</title></svelte:head>

{#if view === 'forbidden'}
	<Empty.Root class="mx-auto max-w-xl">
		<Empty.Header>
			<Empty.Media variant="icon"><ShieldAlert aria-hidden="true" /></Empty.Media>
			<h1 class="text-2xl font-semibold tracking-tight">This page is for admins</h1>
			<Empty.Description class="text-base">Ask an admin if something about the Zoom connection needs changing.</Empty.Description>
		</Empty.Header>
		<Empty.Content><Button href="/calls" variant="outline">Back to calls</Button></Empty.Content>
	</Empty.Root>
{:else}
	<div class="flex flex-col gap-8">
		<header class="flex flex-wrap items-start justify-between gap-4">
			<div class="flex max-w-prose flex-col gap-2">
				<h1 class="text-3xl font-semibold tracking-tight">Zoom connection</h1>
				<p class="text-base text-muted-foreground">Check that Zoom is sending calls here, and choose what gets recorded. Only admins see this page.</p>
			</div>
			{#if view === 'ready'}
				<Button variant="outline" onclick={checkAgain} disabled={checking}>
					{#if checking}<Spinner aria-hidden="true" role="presentation" />{:else}<RefreshCw aria-hidden="true" />{/if}Check again
				</Button>
			{/if}
		</header>
		<p class="sr-only" role="status">{checked}</p>

		{#if view === 'loading'}
			<p class="sr-only" role="status">Loading Zoom settings…</p>
			<div class="grid grid-cols-2 gap-3 md:grid-cols-4">{#each [0, 1, 2, 3] as i (i)}<Skeleton class="h-20" />{/each}</div>
			<Skeleton class="h-64" />
		{:else if view === 'error'}
			<Alert.Root variant="destructive">
				<Alert.Title>We couldn’t load the Zoom settings</Alert.Title>
				<Alert.Description>
					<p>{loadError}</p>
					<Button variant="outline" size="sm" class="mt-2" onclick={checkAgain}>Try again</Button>
				</Alert.Description>
			</Alert.Root>
		{:else if settings}
			{#if demo}
				<Alert.Root>
					<FlaskConical aria-hidden="true" />
					<Alert.Title>This is a demo server</Alert.Title>
					<Alert.Description>
						<p>It isn’t connected to Zoom. Use <a href="#demo" class="font-medium text-foreground underline underline-offset-4">Demo controls</a> at the bottom of this page to simulate a call.</p>
					</Alert.Description>
				</Alert.Root>
			{/if}

			<section aria-labelledby="status-h" class="flex flex-col gap-3">
				<h2 id="status-h" class="text-lg font-semibold">Status</h2>
				<dl class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
					<div class="flex flex-col gap-1 rounded-xl border p-4">
						<dt class="text-sm text-muted-foreground">Connection</dt>
						<dd class="flex items-center gap-2 text-lg font-semibold">
							<span aria-hidden="true" class="size-2.5 rounded-full {settings.connected ? 'bg-success' : 'bg-muted-foreground'}"></span>
							{settings.connected ? 'Connected' : 'Not connected'}
						</dd>
					</div>
					<div class="flex flex-col gap-1 rounded-xl border p-4">
						<dt class="text-sm text-muted-foreground">Last message from Zoom</dt>
						<dd class="text-lg font-semibold">{settings.last_webhook ? ago(settings.last_webhook) : 'None yet'}</dd>
					</div>
					<div class="flex flex-col gap-1 rounded-xl border p-4">
						<dt class="text-sm text-muted-foreground">Live calls</dt>
						<dd class="text-lg font-semibold tabular-nums">{settings.live_streams}</dd>
					</div>
					<div class="flex flex-col gap-1 rounded-xl border p-4">
						<dt class="text-sm text-muted-foreground">Calls this week</dt>
						<dd class="text-lg font-semibold tabular-nums">{settings.calls_this_week}</dd>
					</div>
				</dl>
			</section>

			<div class="grid items-start gap-6 lg:grid-cols-2">
				<div class="flex min-w-0 flex-col gap-6">
					<Card.Root>
						<Card.Header>
							<h2 class="text-lg font-semibold">Set up Zoom</h2>
							<Card.Description>Do this once, in Zoom, with a Zoom admin account.</Card.Description>
						</Card.Header>
						<Card.Content>
							<ol class="flex flex-col gap-5">
								<li class="grid grid-cols-[1.75rem_minmax(0,1fr)] gap-3">
									<span aria-hidden="true" class="grid size-7 place-items-center rounded-full bg-muted text-sm font-semibold">1</span>
									<div class="flex flex-col gap-1">
										<h3 class="font-medium">Create a General App</h3>
										<p class="text-sm text-muted-foreground">In the Zoom App Marketplace, go to Develop, then Build App, then General App.</p>
										<a href="https://marketplace.zoom.us/" target="_blank" rel="noreferrer" class="inline-flex w-fit items-center gap-1 text-sm font-medium underline underline-offset-4">
											Open Zoom App Marketplace<ExternalLink aria-hidden="true" class="size-3.5" /><span class="sr-only"> (opens in a new tab)</span>
										</a>
									</div>
								</li>
								<li class="grid grid-cols-[1.75rem_minmax(0,1fr)] gap-3">
									<span aria-hidden="true" class="grid size-7 place-items-center rounded-full bg-muted text-sm font-semibold">2</span>
									<div class="flex min-w-0 flex-col gap-2">
										<h3 class="font-medium">Point its webhook here</h3>
										<label for="webhook-url" class="sr-only">Webhook URL</label>
										<div class="flex gap-2">
											<input
												id="webhook-url"
												readonly
												value={settings.webhook_url}
												class="h-9 min-w-0 flex-1 rounded-md border bg-muted/50 px-2.5 font-mono text-xs"
												onfocus={(e) => e.currentTarget.select()}
											/>
											<CopyButton text={settings.webhook_url} label="Copy webhook URL" done="Webhook URL copied" iconOnly size="icon" />
										</div>
										<p class="text-sm text-muted-foreground">
											Subscribe to <code class="font-mono text-xs">meeting.rtms_started</code>, <code class="font-mono text-xs">meeting.rtms_stopped</code> and <code class="font-mono text-xs">meeting.rtms_interrupted</code>.
										</p>
									</div>
								</li>
								<li class="grid grid-cols-[1.75rem_minmax(0,1fr)] gap-3">
									<span aria-hidden="true" class="grid size-7 place-items-center rounded-full bg-muted text-sm font-semibold">3</span>
									<div class="flex flex-col gap-1">
										<h3 class="font-medium">Add the transcript and chat scopes</h3>
										<p class="text-sm text-muted-foreground">
											<code class="font-mono text-xs [overflow-wrap:anywhere]">meeting:read:meeting_transcript</code> and <code class="font-mono text-xs [overflow-wrap:anywhere]">meeting:read:meeting_chat</code>
										</p>
									</div>
								</li>
								<li class="grid grid-cols-[1.75rem_minmax(0,1fr)] gap-3">
									<span aria-hidden="true" class="grid size-7 place-items-center rounded-full bg-muted text-sm font-semibold">4</span>
									<div class="flex flex-col gap-1">
										<h3 class="font-medium">Turn on realtime meeting content</h3>
										<p class="text-sm text-muted-foreground">In Zoom’s admin settings, under Zoom Apps. We can’t check this setting from here.</p>
									</div>
								</li>
							</ol>
						</Card.Content>
					</Card.Root>

					<section aria-labelledby="webhooks-h" class="flex flex-col gap-3">
						<h2 id="webhooks-h" class="text-lg font-semibold">Recent messages from Zoom</h2>
						{#if settings.webhooks?.length}
							<div class="rounded-xl border">
								<Table.Root>
									<Table.Caption class="sr-only">Recent webhook messages, newest first</Table.Caption>
									<Table.Header>
										<Table.Row>
											<Table.Head scope="col" class="px-4">When</Table.Head>
											<Table.Head scope="col">Message</Table.Head>
											<Table.Head scope="col" class="px-4">Result</Table.Head>
										</Table.Row>
									</Table.Header>
									<Table.Body>
										{#each settings.webhooks as hook, i (i)}
											{@const r = result(hook.outcome)}
											<Table.Row>
												<Table.Cell class="px-4 align-top whitespace-nowrap text-muted-foreground"><time datetime={hook.at}>{ago(hook.at)}</time></Table.Cell>
												<th scope="row" class="py-2 pr-2 text-left align-top font-normal whitespace-normal">
													<span class="block font-medium">{messages[hook.event] || hook.event || 'Unknown message'}</span>
													{#if hook.topic}<span class="block text-muted-foreground">{hook.topic}</span>{/if}
												</th>
												<Table.Cell class="px-4 align-top"><Badge variant={r.variant}>{r.text}</Badge></Table.Cell>
											</Table.Row>
										{/each}
									</Table.Body>
								</Table.Root>
							</div>
						{:else}
							<p class="rounded-xl border border-dashed p-6 text-center text-muted-foreground">Nothing from Zoom yet. Messages appear here once the webhook is set up.</p>
						{/if}
					</section>
				</div>

				<Card.Root>
					<Card.Header>
						<h2 class="text-lg font-semibold">App credentials</h2>
						<Card.Description>From your Zoom app’s App Credentials page.</Card.Description>
					</Card.Header>
					<Card.Content>
						<form class="flex flex-col gap-6" onsubmit={save}>
							<Field.Group>
								<Field.Field>
									<Field.Label for="client-id">Client ID</Field.Label>
									<Input id="client-id" bind:value={clientId} autocomplete="off" spellcheck={false} class="font-mono" />
								</Field.Field>
								<Field.Field>
									<Field.Label for="client-secret">Client secret</Field.Label>
									<Input id="client-secret" type="password" bind:value={clientSecret} autocomplete="off" aria-describedby="client-secret-help" />
									<Field.Description id="client-secret-help">
										{#if settings.credentials.client_secret_suffix}
											Saved secret ends in <span class="font-mono">{settings.credentials.client_secret_suffix}</span>. We never show it again. Leave this blank to keep it.
										{:else}
											No secret saved yet.
										{/if}
									</Field.Description>
								</Field.Field>
								<p class="text-sm text-muted-foreground">
									Webhook secret: {settings.credentials.webhook_secret_configured ? 'set on the server.' : 'not set on the server. Add ZOOM_WEBHOOK_SECRET to its environment.'}
								</p>
							</Field.Group>

							<fieldset class="flex flex-col gap-4 border-t pt-5">
								<legend class="float-left mb-4 w-full font-semibold">What to record</legend>
								<div class="flex items-start justify-between gap-4">
									<div class="flex flex-col gap-0.5">
										<label for="capture-transcript" class="font-medium">Transcript</label>
										<p id="capture-transcript-help" class="text-sm text-muted-foreground">What people say, from Zoom’s live transcription</p>
									</div>
									<Switch id="capture-transcript" bind:checked={transcript} aria-describedby="capture-transcript-help" />
								</div>
								<div class="flex items-start justify-between gap-4">
									<div class="flex flex-col gap-0.5">
										<label for="capture-chat" class="font-medium">Chat</label>
										<p id="capture-chat-help" class="text-sm text-muted-foreground">Messages sent to everyone. Private messages are never recorded.</p>
									</div>
									<Switch id="capture-chat" bind:checked={chat} aria-describedby="capture-chat-help" />
								</div>
								<p class="text-sm text-muted-foreground">Audio and video aren’t recorded.</p>
							</fieldset>

							{#if saveError}<p role="alert" class="text-sm text-destructive">{saveError}</p>{/if}
							<div class="flex items-center gap-3">
								<Button type="submit" disabled={saving}>
									{#if saving}<Spinner aria-hidden="true" role="presentation" />Saving…{:else}Save changes{/if}
								</Button>
								<span role="status" class="text-sm text-muted-foreground">{saved}</span>
							</div>
						</form>
					</Card.Content>
				</Card.Root>
			</div>

			{#if demo}
				<section id="demo" aria-labelledby="demo-h" class="flex scroll-mt-6 flex-col gap-3 rounded-xl border border-dashed p-6">
					<h2 id="demo-h" class="text-lg font-semibold">Demo controls</h2>
					<p class="text-muted-foreground">Start a pretend call that fills in like a real one. Nothing is sent to Zoom.</p>
					{#if demoCall}
						<div class="flex flex-wrap items-center gap-3 rounded-lg bg-muted/50 p-3">
							<div class="flex min-w-0 flex-1 flex-col gap-1">
								<a href="/calls/{demoCall.id}" class="font-medium underline underline-offset-4">{demoCall.topic}</a>
								<CallStatus status={demoCall.status} />
							</div>
							{#if demoCall.status === 'live'}
								<Button variant="outline" onclick={stopDemo} disabled={demoBusy}><Square aria-hidden="true" />Stop call</Button>
							{:else}
								<Button variant="outline" onclick={startDemo} disabled={demoBusy}><Play aria-hidden="true" />Start another</Button>
							{/if}
						</div>
					{:else}
						<Button class="w-fit" onclick={startDemo} disabled={demoBusy}>
							{#if demoBusy}<Spinner aria-hidden="true" role="presentation" />{:else}<Play aria-hidden="true" />{/if}Start a simulated call
						</Button>
					{/if}
					{#if demoError}<p role="alert" class="text-sm text-destructive">{demoError}</p>{/if}
				</section>
			{/if}
		{/if}
	</div>
{/if}
