<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { page } from '$app/state';
	import Search from '@lucide/svelte/icons/search';
	import CircleAlert from '@lucide/svelte/icons/circle-alert';
	import { toast } from 'svelte-sonner';
	import * as Alert from '#lib/components/ui/alert';
	import * as Avatar from '#lib/components/ui/avatar';
	import * as Dialog from '#lib/components/ui/dialog';
	import * as Field from '#lib/components/ui/field';
	import * as InputGroup from '#lib/components/ui/input-group';
	import { Button } from '#lib/components/ui/button';
	import { Spinner } from '#lib/components/ui/spinner';
	import CopyButton from '#lib/components/CopyButton.svelte';
	import { api, errorMessage, initials, plural } from '#lib/api';
	import { domainOf, getUser } from '#lib/session';
	import type { Person, Share } from '#lib/types';

	let {
		callId,
		topic,
		hostId,
		canShare: canShareAtOpen = false,
		onchange
	}: { callId: string; topic: string; hostId: string; canShare?: boolean; onchange?: () => void } = $props();

	const user = getUser();
	let shares = $state<Share[]>([]);
	let participants = $state<Person[]>([]);
	// Known from the call already, so the search field is there when the dialog takes focus.
	let canShare = $state(untrack(() => canShareAtOpen));
	let loading = $state(true);
	let error = $state('');
	let query = $state('');
	let results = $state<Person[]>([]);
	let searched = $state('');
	let active = $state(0);
	let busy = $state(false);
	let input = $state<HTMLInputElement | null>(null);
	let timer: ReturnType<typeof setTimeout> | undefined;

	const link = $derived(`${page.url.origin}/calls/${callId}`);
	const haveAccess = $derived(new Set([...participants.map((p) => p.email?.toLowerCase()), ...shares.map((s) => s.email.toLowerCase())]));
	const typedEmail = $derived(/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(query.trim()) ? query.trim().toLowerCase() : '');
	type Option = { id: string; name: string; email: string };
	const options = $derived.by(() => {
		const list: Option[] = results
			.filter((p) => p.email && !haveAccess.has(p.email.toLowerCase()))
			.map((p) => ({ id: `person-${p.id}`, name: p.name, email: p.email! }));
		if (typedEmail && !haveAccess.has(typedEmail) && !list.some((o) => o.email.toLowerCase() === typedEmail)) {
			list.push({ id: 'person-typed', name: `Add ${typedEmail}`, email: typedEmail });
		}
		return list;
	});
	const listOpen = $derived(query.trim().length >= 2 && searched === query.trim() && options.length > 0);
	const matchStatus = $derived.by(() => {
		if (query.trim().length < 2 || searched !== query.trim()) return '';
		if (options.length) return `${plural(options.length, 'person', 'people')} ${options.length === 1 ? 'matches' : 'match'}. Use the arrow keys to choose.`;
		if (typedEmail && haveAccess.has(typedEmail)) return `${typedEmail} already has access.`;
		return 'No one matches. Type a full email address to add someone new.';
	});

	onMount(() => {
		void load();
		return () => clearTimeout(timer);
	});

	async function load() {
		loading = true;
		error = '';
		try {
			const r = await api<{ participants: Person[]; shares: Share[]; can_share: boolean }>(`/api/calls/${encodeURIComponent(callId)}/shares`);
			participants = r.participants;
			shares = r.shares;
			canShare = r.can_share;
		} catch (e) {
			error = errorMessage(e);
		} finally {
			loading = false;
		}
	}

	function lookup() {
		clearTimeout(timer);
		active = 0;
		const q = query.trim();
		if (q.length < 2) {
			results = [];
			searched = '';
			return;
		}
		timer = setTimeout(async () => {
			try {
				const found = await api<Person[]>(`/api/people?q=${encodeURIComponent(q)}`);
				if (q === query.trim()) {
					results = found;
					searched = q;
				}
			} catch (e) {
				error = errorMessage(e);
			}
		}, 200);
	}

	function onkeydown(event: KeyboardEvent) {
		if (event.key === 'Escape' && listOpen) {
			// Close the suggestions, not the dialog.
			event.stopPropagation();
			query = '';
			results = [];
			searched = '';
			return;
		}
		if (!listOpen) {
			if (event.key === 'Enter' && typedEmail) {
				event.preventDefault();
				void add(typedEmail, typedEmail);
			}
			return;
		}
		if (event.key === 'ArrowDown') {
			event.preventDefault();
			active = (active + 1) % options.length;
		} else if (event.key === 'ArrowUp') {
			event.preventDefault();
			active = (active - 1 + options.length) % options.length;
		} else if (event.key === 'Home') {
			event.preventDefault();
			active = 0;
		} else if (event.key === 'End') {
			event.preventDefault();
			active = options.length - 1;
		} else if (event.key === 'Enter') {
			event.preventDefault();
			const o = options[active];
			if (o) void add(o.email, o.id === 'person-typed' ? o.email : o.name);
		}
	}

	async function add(email: string, name: string) {
		if (busy) return;
		busy = true;
		error = '';
		try {
			await api(`/api/calls/${encodeURIComponent(callId)}/shares`, { method: 'POST', body: JSON.stringify({ email }) });
			query = '';
			results = [];
			searched = '';
			await load();
			toast.success(`${name} can now open this call`);
			onchange?.();
			input?.focus();
		} catch (e) {
			error = errorMessage(e);
		} finally {
			busy = false;
		}
	}

	async function remove(grant: Share) {
		error = '';
		try {
			await api(`/api/calls/${encodeURIComponent(callId)}/shares/${encodeURIComponent(grant.id)}`, { method: 'DELETE' });
			shares = shares.filter((s) => s.id !== grant.id);
			toast.success(`Removed access for ${grant.name || grant.email}`);
			onchange?.();
			input?.focus();
		} catch (e) {
			error = errorMessage(e);
		}
	}

	const shortDate = (v: string) => new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric' }).format(new Date(v));
	const orderedParticipants = $derived([...participants].sort((a, b) => Number(b.id === hostId) - Number(a.id === hostId)));
</script>

	<Dialog.Header>
		<Dialog.Title>Share “{topic}”</Dialog.Title>
		<Dialog.Description>
			{canShare || loading
				? 'People you add can open the call and its transcript, and it shows up in their calls.'
				: 'You can see who has access. Only people who were on the call can share it.'}
		</Dialog.Description>
	</Dialog.Header>

	{#if error}
		<Alert.Root variant="destructive">
			<CircleAlert aria-hidden="true" />
			<Alert.Title>That didn’t work</Alert.Title>
			<Alert.Description>{error}</Alert.Description>
		</Alert.Root>
	{/if}

	{#if canShare}
		<Field.Field>
			<Field.Label for="person-search">Add people</Field.Label>
			<div class="relative">
				<InputGroup.Root>
					<InputGroup.Addon><Search aria-hidden="true" /></InputGroup.Addon>
					<InputGroup.Input
						id="person-search"
						bind:ref={input}
						role="combobox"
						autocomplete="off"
						aria-expanded={listOpen}
						aria-controls="person-options"
						aria-autocomplete="list"
						aria-activedescendant={listOpen ? options[active]?.id : undefined}
						aria-describedby="person-help"
						aria-busy={busy}
						bind:value={query}
						oninput={lookup}
						{onkeydown}
					/>
					{#if busy}<InputGroup.Addon align="inline-end"><Spinner aria-hidden="true" role="presentation" /></InputGroup.Addon>{/if}
				</InputGroup.Root>
				<ul
					id="person-options"
					role="listbox"
					aria-label="People"
					hidden={!listOpen}
					class="mt-1 flex max-h-56 flex-col overflow-y-auto rounded-md border bg-popover p-1 text-popover-foreground shadow-md"
				>
					{#each options as o, i (o.id)}
						<li
							id={o.id}
							role="option"
							aria-selected={i === active}
							class="flex cursor-pointer items-center gap-2.5 rounded-sm px-2 py-1.5 aria-selected:bg-accent aria-selected:text-accent-foreground"
							onpointerenter={() => (active = i)}
							onpointerdown={(e) => { e.preventDefault(); void add(o.email, o.id === 'person-typed' ? o.email : o.name); }}
						>
							<Avatar.Root class="size-7" aria-hidden="true"><Avatar.Fallback class="text-xs">{o.id === 'person-typed' ? '@' : initials(o.name)}</Avatar.Fallback></Avatar.Root>
							<span class="grid min-w-0">
								<span class="truncate font-medium">{o.name}</span>
								{#if o.id !== 'person-typed'}<span class="truncate text-xs text-muted-foreground">{o.email}</span>{/if}
							</span>
						</li>
					{/each}
				</ul>
			</div>
			<Field.Description id="person-help">Search by name or type an {domainOf(user())} email address.</Field.Description>
			<p class="sr-only" role="status">{matchStatus}</p>
		</Field.Field>
	{/if}

	<section aria-labelledby="access-h" class="flex flex-col gap-2">
		<h3 id="access-h" class="text-sm font-medium">People with access</h3>
		{#if loading && !participants.length}
			<p class="flex items-center gap-2 text-sm text-muted-foreground"><Spinner aria-hidden="true" role="presentation" />Loading who has access…</p>
		{:else}
			<ul class="flex flex-col divide-y">
				{#each shares as grant (grant.id)}
					<li class="flex items-center gap-3 py-2">
						<Avatar.Root class="size-8" aria-hidden="true"><Avatar.Fallback class="text-xs">{initials(grant.name || grant.email)}</Avatar.Fallback></Avatar.Root>
						<div class="grid min-w-0 flex-1">
							<span class="truncate font-medium">{grant.name || grant.email}</span>
							<span class="truncate text-sm text-muted-foreground">
								Shared by {grant.granted_by === user().id ? 'you' : grant.granted_by_name} · {shortDate(grant.created_at)}
							</span>
						</div>
						{#if grant.can_remove}
							<Button variant="ghost" size="sm" onclick={() => remove(grant)}>Remove<span class="sr-only"> access for {grant.name || grant.email}</span></Button>
						{/if}
					</li>
				{/each}
				{#each orderedParticipants as person (person.id)}
					<li class="flex items-center gap-3 py-2">
						<Avatar.Root class="size-8" aria-hidden="true"><Avatar.Fallback class="text-xs">{initials(person.name)}</Avatar.Fallback></Avatar.Root>
						<div class="grid min-w-0 flex-1">
							<span class="truncate font-medium">{person.name}{person.id === user().id ? ' (you)' : ''}</span>
							<span class="truncate text-sm text-muted-foreground">On the call{person.id === hostId ? ' · Host' : ''}</span>
						</div>
					</li>
				{/each}
			</ul>
		{/if}
	</section>

	<Field.Field class="border-t pt-4">
		<Field.Label for="call-link">Call link</Field.Label>
		<div class="flex gap-2">
			<input
				id="call-link"
				readonly
				value={link}
				aria-describedby="link-help"
				class="h-9 min-w-0 flex-1 rounded-md border bg-muted/50 px-2.5 font-mono text-sm"
				onfocus={(e) => e.currentTarget.select()}
			/>
			<CopyButton text={link} label="Copy link" done="Link copied" />
		</div>
		<Field.Description id="link-help">The link only opens for people with access. Send it after you add them.</Field.Description>
	</Field.Field>

	<Dialog.Footer>
		<Dialog.Close>
			{#snippet child({ props })}
				<Button {...props}>Done</Button>
			{/snippet}
		</Dialog.Close>
	</Dialog.Footer>
