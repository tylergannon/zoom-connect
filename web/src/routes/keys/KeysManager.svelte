<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { page } from '$app/state';
	import Plus from '@lucide/svelte/icons/plus';
	import KeyRound from '@lucide/svelte/icons/key-round';
	import ChevronRight from '@lucide/svelte/icons/chevron-right';
	import BookOpen from '@lucide/svelte/icons/book-open';
	import { toast } from 'svelte-sonner';
	import * as Alert from '#lib/components/ui/alert';
	import * as AlertDialog from '#lib/components/ui/alert-dialog';
	import * as Collapsible from '#lib/components/ui/collapsible';
	import * as Dialog from '#lib/components/ui/dialog';
	import * as Empty from '#lib/components/ui/empty';
	import * as Field from '#lib/components/ui/field';
	import * as Item from '#lib/components/ui/item';
	import * as Table from '#lib/components/ui/table';
	import { Badge } from '#lib/components/ui/badge';
	import { Button } from '#lib/components/ui/button';
	import { Input } from '#lib/components/ui/input';
	import { Skeleton } from '#lib/components/ui/skeleton';
	import { Spinner } from '#lib/components/ui/spinner';
	import CopyButton from '#lib/components/CopyButton.svelte';
	import { ago, api, errorMessage } from '#lib/api';
	import type { APIKey } from '#lib/types';

	let keys = $state<APIKey[]>([]);
	let loading = $state(true);
	let loadError = $state('');

	let createOpen = $state(false);
	let name = $state('');
	let creating = $state(false);
	let createError = $state('');

	let created = $state<{ key: APIKey; secret: string } | null>(null);
	let revealOpen = $state(false);

	let revoking = $state<APIKey | null>(null);
	let revokeOpen = $state(false);
	let revokeBusy = $state(false);
	let revokeError = $state('');
	let revoked = false;

	const active = $derived(keys.filter((k) => !k.revoked_at));
	const retired = $derived(keys.filter((k) => k.revoked_at));
	const curl = $derived(created ? `curl -H "Authorization: Bearer ${created.secret}" \\\n  ${page.url.origin}/api/v1/calls` : '');
	const shortDate = (value: string) => new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', year: new Date(value).getFullYear() === new Date().getFullYear() ? undefined : 'numeric' }).format(new Date(value));

	onMount(() => {
		void load();
	});

	async function load() {
		loading = true;
		loadError = '';
		try {
			keys = await api<APIKey[]>('/api/keys');
		} catch (e) {
			loadError = errorMessage(e);
		} finally {
			loading = false;
		}
	}

	async function create(event: SubmitEvent) {
		event.preventDefault();
		if (!name.trim()) {
			createError = 'Enter a name for the key.';
			return;
		}
		creating = true;
		createError = '';
		try {
			created = await api<{ key: APIKey; secret: string }>('/api/keys', { method: 'POST', body: JSON.stringify({ name: name.trim() }) });
			keys = [created.key, ...keys];
			createOpen = false;
		} catch (e) {
			createError = errorMessage(e);
		} finally {
			creating = false;
		}
	}

	function createClosed(open: boolean) {
		if (open) return;
		name = '';
		createError = '';
		// Open the one-time reveal only after the create dialog has let go of focus.
		if (created) revealOpen = true;
	}

	function askRevoke(key: APIKey) {
		revoking = key;
		revokeError = '';
		revoked = false;
		revokeOpen = true;
	}

	async function revoke() {
		if (!revoking) return;
		revokeBusy = true;
		revokeError = '';
		try {
			await api(`/api/keys/${encodeURIComponent(revoking.id)}`, { method: 'DELETE' });
			const id = revoking.id;
			keys = keys.map((k) => (k.id === id ? { ...k, revoked_at: new Date().toISOString() } : k));
			toast.success(`Revoked “${revoking.name}”`);
			revoked = true;
			revokeOpen = false;
		} catch (e) {
			revokeError = errorMessage(e);
		} finally {
			revokeBusy = false;
		}
	}

	/** Send focus somewhere that still exists once a dialog closes. */
	async function focusAfter(event: Event, target: () => string) {
		event.preventDefault();
		await tick();
		document.getElementById(target())?.focus();
	}
</script>

<div class="flex flex-col gap-8">
	<header class="flex flex-wrap items-start justify-between gap-4">
		<div class="flex max-w-prose flex-col gap-2">
			<h1 class="text-3xl font-semibold tracking-tight">API keys</h1>
			<p class="text-base text-muted-foreground">
				Give a key to an agent so it can follow your calls live or download finished transcripts. A key sees exactly what you see.
			</p>
		</div>
		<Button id="create-key" onclick={() => (createOpen = true)}><Plus aria-hidden="true" />Create key</Button>
	</header>

	<Alert.Root>
		<KeyRound aria-hidden="true" />
		<Alert.Title>Treat keys like passwords</Alert.Title>
		<Alert.Description>
			Anyone holding a key can read every call you can see, including calls you join later. We show each key once. Revoke any key you no longer use.
		</Alert.Description>
	</Alert.Root>

	{#if loading}
		<div class="flex flex-col gap-3">
			<p class="sr-only" role="status">Loading your keys…</p>
			<Skeleton class="h-6 w-32" />
			{#each [0, 1, 2] as i (i)}<Skeleton class="h-12 w-full" />{/each}
		</div>
	{:else if loadError}
		<Alert.Root variant="destructive">
			<Alert.Title>We couldn’t load your keys</Alert.Title>
			<Alert.Description>
				<p>{loadError}</p>
				<Button variant="outline" size="sm" class="mt-2" onclick={load}>Try again</Button>
			</Alert.Description>
		</Alert.Root>
	{:else}
		<section aria-labelledby="active-keys" class="flex flex-col gap-3">
			<h2 id="active-keys" tabindex="-1" class="flex items-center gap-2 text-lg font-semibold">
				Active keys <Badge variant="secondary">{active.length}</Badge>
			</h2>
			{#if active.length}
				<div class="rounded-xl border">
					<Table.Root>
						<Table.Caption class="sr-only">Active API keys</Table.Caption>
						<Table.Header>
							<Table.Row>
								<Table.Head scope="col" class="px-4">Name</Table.Head>
								<Table.Head scope="col">Key</Table.Head>
								<Table.Head scope="col">Created</Table.Head>
								<Table.Head scope="col">Last used</Table.Head>
								<Table.Head scope="col" class="px-4 text-right"><span class="sr-only">Actions</span></Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#each active as key (key.id)}
								<Table.Row>
									<th scope="row" class="px-4 py-3 text-left font-medium">{key.name}</th>
									<Table.Cell class="font-mono text-xs">{key.prefix}…</Table.Cell>
									<Table.Cell>{shortDate(key.created_at)}</Table.Cell>
									<Table.Cell>{key.last_used_at ? ago(key.last_used_at) : 'Never'}</Table.Cell>
									<Table.Cell class="px-4 text-right">
										<Button id="revoke-{key.id}" variant="outline" size="sm" onclick={() => askRevoke(key)}>
											Revoke<span class="sr-only"> {key.name}</span>
										</Button>
									</Table.Cell>
								</Table.Row>
							{/each}
						</Table.Body>
					</Table.Root>
				</div>
			{:else}
				<Empty.Root class="border border-dashed">
					<Empty.Header>
						<Empty.Media variant="icon"><KeyRound aria-hidden="true" /></Empty.Media>
						<Empty.Title>No active keys</Empty.Title>
						<Empty.Description>Create a key when you’re ready to connect an agent.</Empty.Description>
					</Empty.Header>
					<Empty.Content><Button onclick={() => (createOpen = true)}><Plus aria-hidden="true" />Create key</Button></Empty.Content>
				</Empty.Root>
			{/if}
		</section>

		{#if retired.length}
			<Collapsible.Root class="flex flex-col gap-3">
				<Collapsible.Trigger class="group flex w-fit items-center gap-1.5 rounded-md text-sm font-medium text-muted-foreground hover:text-foreground">
					<ChevronRight aria-hidden="true" class="size-4 transition-transform group-data-[state=open]:rotate-90" />
					Revoked keys ({retired.length})
				</Collapsible.Trigger>
				<Collapsible.Content>
					<div class="rounded-xl border">
						<Table.Root>
							<Table.Caption class="sr-only">Revoked API keys</Table.Caption>
							<Table.Header>
								<Table.Row>
									<Table.Head scope="col" class="px-4">Name</Table.Head>
									<Table.Head scope="col">Key</Table.Head>
									<Table.Head scope="col">Revoked</Table.Head>
								</Table.Row>
							</Table.Header>
							<Table.Body>
								{#each retired as key (key.id)}
									<Table.Row class="text-muted-foreground">
										<th scope="row" class="px-4 py-3 text-left font-medium">{key.name}</th>
										<Table.Cell class="font-mono text-xs">{key.prefix}…</Table.Cell>
										<Table.Cell>{shortDate(key.revoked_at!)}</Table.Cell>
									</Table.Row>
								{/each}
							</Table.Body>
						</Table.Root>
					</div>
				</Collapsible.Content>
			</Collapsible.Root>
		{/if}
	{/if}

	<Item.Root variant="outline">
		<Item.Media variant="icon"><BookOpen aria-hidden="true" /></Item.Media>
		<Item.Content>
			<Item.Title>Connecting an agent?</Item.Title>
			<Item.Description>The API guide has copy-ready examples for live streams and downloads.</Item.Description>
		</Item.Content>
		<Item.Actions><Button href="/agent-api" variant="outline" size="sm">Open the API guide</Button></Item.Actions>
	</Item.Root>
</div>

<Dialog.Root bind:open={createOpen} onOpenChangeComplete={createClosed}>
	<Dialog.Content class="sm:max-w-md">
		<form class="flex flex-col gap-6" onsubmit={create} novalidate>
			<Dialog.Header>
				<Dialog.Title>Create an API key</Dialog.Title>
				<Dialog.Description>The key will read every call you can see, as you.</Dialog.Description>
			</Dialog.Header>
			<Field.Field data-invalid={createError ? true : undefined}>
				<Field.Label for="key-name">Key name</Field.Label>
				<Input
					id="key-name"
					bind:value={name}
					maxlength={80}
					autocomplete="off"
					required
					aria-invalid={createError ? true : undefined}
					aria-describedby="key-name-help{createError ? ' key-name-error' : ''}"
				/>
				<Field.Description id="key-name-help">Name it after the agent or machine that will hold it, so you know what to revoke later.</Field.Description>
				{#if createError}<p id="key-name-error" role="alert" class="text-sm text-destructive">{createError}</p>{/if}
			</Field.Field>
			<Dialog.Footer>
				<Dialog.Close>
					{#snippet child({ props })}<Button {...props} variant="outline">Cancel</Button>{/snippet}
				</Dialog.Close>
				<Button type="submit" disabled={creating}>
					{#if creating}<Spinner aria-hidden="true" role="presentation" />Creating…{:else}Create key{/if}
				</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>

<AlertDialog.Root bind:open={revealOpen} onOpenChangeComplete={(o) => { if (!o) created = null; }}>
	<AlertDialog.Content
		escapeKeydownBehavior="ignore"
		onOpenAutoFocus={(e) => focusAfter(e, () => 'new-key')}
		onCloseAutoFocus={(e) => focusAfter(e, () => 'active-keys')}
	>
		{#if created}
			<AlertDialog.Header>
				<AlertDialog.Title>Copy your new key now</AlertDialog.Title>
				<AlertDialog.Description>
					“{created.key.name}” is ready. This is the only time we show the full key. If you lose it, revoke it and create another.
				</AlertDialog.Description>
			</AlertDialog.Header>
			<div class="flex min-w-0 flex-col gap-2">
				<label for="new-key" class="text-sm font-medium">API key</label>
				<div class="flex gap-2">
					<input
						id="new-key"
						readonly
						value={created.secret}
						class="h-9 min-w-0 flex-1 rounded-md border bg-muted/50 px-3 font-mono text-sm"
						onfocus={(e) => e.currentTarget.select()}
					/>
					<CopyButton text={created.secret} label="Copy key" done="Key copied to the clipboard" />
				</div>
			</div>
			<div class="flex min-w-0 flex-col gap-2">
				<h3 id="try-it" class="text-sm font-medium">Try it</h3>
				<pre aria-labelledby="try-it" class="rounded-md bg-muted p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap [overflow-wrap:anywhere]">{curl}</pre>
				<div class="flex flex-wrap items-center gap-3">
					<CopyButton text={curl} label="Copy curl command" done="Command copied" size="sm" />
					<a href="/agent-api" class="text-sm font-medium underline underline-offset-4">Read the API guide</a>
				</div>
			</div>
			<AlertDialog.Footer>
				<AlertDialog.Action onclick={() => (revealOpen = false)}>I’ve saved the key</AlertDialog.Action>
			</AlertDialog.Footer>
		{/if}
	</AlertDialog.Content>
</AlertDialog.Root>

<AlertDialog.Root bind:open={revokeOpen}>
	<AlertDialog.Content onCloseAutoFocus={(e) => focusAfter(e, () => (revoked || !revoking ? 'active-keys' : `revoke-${revoking.id}`))}>
		{#if revoking}
			<AlertDialog.Header>
				<AlertDialog.Title>Revoke “{revoking.name}”?</AlertDialog.Title>
				<AlertDialog.Description>
					Any agent using {revoking.prefix}… loses access right away, including live streams it has open. You can’t undo this.
				</AlertDialog.Description>
			</AlertDialog.Header>
			{#if revokeError}<p role="alert" class="text-sm text-destructive">{revokeError}</p>{/if}
			<AlertDialog.Footer>
				<AlertDialog.Cancel>Keep key</AlertDialog.Cancel>
				<Button variant="destructive" disabled={revokeBusy} onclick={revoke}>
					{#if revokeBusy}<Spinner aria-hidden="true" role="presentation" />Revoking…{:else}Revoke key{/if}
				</Button>
			</AlertDialog.Footer>
		{/if}
	</AlertDialog.Content>
</AlertDialog.Root>
