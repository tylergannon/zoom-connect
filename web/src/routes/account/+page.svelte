<script lang="ts">
	import { onMount } from 'svelte';
	import LogOut from '@lucide/svelte/icons/log-out';
	import { setMode, userPrefersMode } from 'mode-watcher';
	import { toast } from 'svelte-sonner';
	import * as Avatar from '#lib/components/ui/avatar';
	import * as Card from '#lib/components/ui/card';
	import * as Field from '#lib/components/ui/field';
	import * as NativeSelect from '#lib/components/ui/native-select';
	import * as RadioGroup from '#lib/components/ui/radio-group';
	import { Badge } from '#lib/components/ui/badge';
	import { Button } from '#lib/components/ui/button';
	import { Spinner } from '#lib/components/ui/spinner';
	import { Switch } from '#lib/components/ui/switch';
	import { api, errorMessage, initials, plural } from '#lib/api';
	import { callIndex } from '#lib/calls.svelte';
	import { preferences, transcriptSizes, type TranscriptSize } from '#lib/preferences.svelte';
	import { getUser } from '#lib/session';
	import type { APIKey, User } from '#lib/types';

	const user = getUser();
	let method = $state<'password' | 'demo' | null>(null);
	let people = $state<User[]>([]);
	let persona = $state('');
	let switching = $state(false);
	let switchError = $state('');
	let activeKeys = $state<number | null>(null);

	const themes = [
		{ value: 'system', label: 'Match my device' },
		{ value: 'light', label: 'Light' },
		{ value: 'dark', label: 'Dark' }
	] as const;

	onMount(() => {
		persona = user().email;
		void (async () => {
			try {
				const options = await api<{ password: boolean; demo: boolean }>('/api/auth/options');
				method = options.password ? 'password' : options.demo ? 'demo' : null;
				if (options.demo) people = await api<User[]>('/api/demo/users');
			} catch {
				method = null;
			}
		})();
		void (async () => {
			try {
				activeKeys = (await api<APIKey[]>('/api/keys')).filter((k) => !k.revoked_at).length;
			} catch {
				activeKeys = null;
			}
		})();
	});

	async function switchPerson(event: SubmitEvent) {
		event.preventDefault();
		if (!persona || persona === user().email) return;
		switching = true;
		switchError = '';
		try {
			await api('/api/demo/login', { method: 'POST', body: JSON.stringify({ email: persona }) });
			window.location.assign('/calls');
		} catch (e) {
			switchError = errorMessage(e);
			switching = false;
		}
	}

	async function signOut() {
		try {
			await api('/api/logout', { method: 'POST' });
			window.location.assign('/');
		} catch (e) {
			toast.error('Couldn’t sign out', { description: errorMessage(e) });
		}
	}
</script>

<svelte:head><title>Account · Zoom Connect</title></svelte:head>

<div class="flex max-w-3xl flex-col gap-8">
	<header class="flex flex-col gap-2">
		<h1 class="text-3xl font-semibold tracking-tight">Account</h1>
		<p class="text-base text-muted-foreground">How you’re signed in, and how Zoom Connect looks for you.</p>
	</header>

	<Card.Root>
		<Card.Header><h2 class="text-lg font-semibold">Signed in as</h2></Card.Header>
		<Card.Content class="flex flex-col gap-6">
			<div class="flex flex-wrap items-center gap-4">
				<Avatar.Root class="size-12" aria-hidden="true"><Avatar.Fallback class="text-base">{initials(user().name)}</Avatar.Fallback></Avatar.Root>
				<div class="flex min-w-0 flex-1 flex-col">
					<p class="flex flex-wrap items-center gap-2 text-lg font-semibold">
						{user().name}{#if user().is_admin}<Badge variant="secondary">Admin</Badge>{/if}
					</p>
					<p class="text-muted-foreground [overflow-wrap:anywhere]">{user().email}</p>
				</div>
				<Button variant="outline" onclick={signOut}><LogOut aria-hidden="true" />Sign out</Button>
			</div>
			<dl class="grid gap-4 border-t pt-5 sm:grid-cols-3">
				<div class="flex flex-col gap-1">
					<dt class="text-sm text-muted-foreground">Signed in with</dt>
					<dd>{method === 'password' ? 'Email and password' : method === 'demo' ? 'Demo sign-in' : '—'}</dd>
				</div>
				<div class="flex flex-col gap-1">
					<dt class="text-sm text-muted-foreground">Calls you can see</dt>
					<dd>
						{#if callIndex.status === 'ready'}
							<a href="/calls" class="underline underline-offset-4">{plural(callIndex.calls.length, 'call')}</a>
						{:else}—{/if}
					</dd>
				</div>
				<div class="flex flex-col gap-1">
					<dt class="text-sm text-muted-foreground">Active API keys</dt>
					<dd>
						{#if activeKeys !== null}<a href="/keys" class="underline underline-offset-4">{plural(activeKeys, 'key')}</a>{:else}—{/if}
					</dd>
				</div>
			</dl>
		</Card.Content>
	</Card.Root>

	{#if method === 'demo' && people.length}
		<Card.Root>
			<Card.Header>
				<h2 class="text-lg font-semibold">Try another person</h2>
				<Card.Description>This is a demo. Switch people to check who can see which calls.</Card.Description>
			</Card.Header>
			<Card.Content>
				<form class="flex flex-wrap items-end gap-3" onsubmit={switchPerson}>
					<Field.Field class="min-w-0 flex-[1_1_16rem]">
						<Field.Label for="persona">Person</Field.Label>
						<NativeSelect.Root id="persona" bind:value={persona} class="w-full">
							{#each people as person (person.email)}
								<NativeSelect.Option value={person.email}>
									{person.name}{person.is_admin ? ' (admin)' : ''}{person.email === user().email ? ', you' : ''}
								</NativeSelect.Option>
							{/each}
						</NativeSelect.Root>
					</Field.Field>
					<Button type="submit" variant="outline" disabled={switching || persona === user().email}>
						{#if switching}<Spinner aria-hidden="true" role="presentation" />Switching…{:else}Switch{/if}
					</Button>
				</form>
				{#if switchError}<p role="alert" class="mt-3 text-sm text-destructive">{switchError}</p>{/if}
			</Card.Content>
		</Card.Root>
	{/if}

	<Card.Root>
		<Card.Header>
			<h2 class="text-lg font-semibold">Display</h2>
			<Card.Description>Saved on this device. Changes apply right away.</Card.Description>
		</Card.Header>
		<Card.Content class="flex flex-col gap-6">
			<fieldset class="flex flex-col gap-3">
				<legend class="float-left mb-3 w-full font-medium">Theme</legend>
				<RadioGroup.Root value={userPrefersMode.current} onValueChange={(v) => setMode(v as 'system' | 'light' | 'dark')} class="flex flex-wrap gap-x-6 gap-y-3">
					{#each themes as t (t.value)}
						<div class="flex items-center gap-2">
							<RadioGroup.Item value={t.value} id="theme-{t.value}" />
							<label for="theme-{t.value}">{t.label}</label>
						</div>
					{/each}
				</RadioGroup.Root>
			</fieldset>

			<fieldset class="flex flex-col gap-3 border-t pt-5">
				<legend class="float-left mb-3 w-full font-medium">Transcript text size</legend>
				<RadioGroup.Root
					value={preferences.transcriptSize}
					onValueChange={(v) => preferences.setTranscriptSize(v as TranscriptSize)}
					aria-describedby="size-help"
					class="flex flex-wrap gap-x-6 gap-y-3"
				>
					{#each transcriptSizes as size (size.value)}
						<div class="flex items-center gap-2">
							<RadioGroup.Item value={size.value} id="size-{size.value}" />
							<label for="size-{size.value}">{size.label}</label>
						</div>
					{/each}
				</RadioGroup.Root>
				<p id="size-help" class="text-sm text-muted-foreground">You can also change this while reading a call.</p>
				<p aria-hidden="true" class="rounded-lg bg-muted/50 p-3 text-transcript leading-relaxed">
					<span class="font-semibold">Priya Shah</span> Let’s ship it Monday.
				</p>
			</fieldset>

			<div class="flex items-start justify-between gap-4 border-t pt-5">
				<div class="flex flex-col gap-0.5">
					<label for="read-aloud" class="font-medium">Read new lines aloud on live calls</label>
					<p id="read-aloud-help" class="text-sm text-muted-foreground">For screen reader users. Announces each new line as it’s spoken. Off by default.</p>
				</div>
				<Switch id="read-aloud" checked={preferences.readAloud} onCheckedChange={(on) => preferences.setReadAloud(on)} aria-describedby="read-aloud-help" />
			</div>
		</Card.Content>
	</Card.Root>
</div>
