<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import Video from '@lucide/svelte/icons/video';
	import CircleAlert from '@lucide/svelte/icons/circle-alert';
	import * as Alert from '#lib/components/ui/alert';
	import * as Card from '#lib/components/ui/card';
	import * as Field from '#lib/components/ui/field';
	import * as NativeSelect from '#lib/components/ui/native-select';
	import { Button } from '#lib/components/ui/button';
	import { Input } from '#lib/components/ui/input';
	import { Spinner } from '#lib/components/ui/spinner';
	import { api, APIError, errorMessage } from '#lib/api';
	import type { User } from '#lib/types';

	let mode = $state<'loading' | 'password' | 'demo' | 'none'>('loading');
	let users = $state<User[]>([]);
	let persona = $state('');
	let email = $state('');
	let password = $state('');
	let busy = $state(false);
	let error = $state('');
	let wrongPassword = $state(false);

	onMount(async () => {
		try {
			await api<User>('/api/me');
			window.location.replace(safeNext());
			return;
		} catch { /* signed out: show the form */ }
		try {
			const options = await api<{ password: boolean; demo: boolean }>('/api/auth/options');
			if (options.password) mode = 'password';
			else if (options.demo) {
				users = await api<User[]>('/api/demo/users');
				persona = users[0]?.email ?? '';
				mode = 'demo';
			} else mode = 'none';
		} catch (e) {
			error = errorMessage(e);
			mode = 'none';
		}
	});

	function safeNext() {
		const next = page.url.searchParams.get('next') || '/calls';
		return next.startsWith('/') && !next.startsWith('//') ? next : '/calls';
	}

	async function signIn(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		wrongPassword = false;
		try {
			if (mode === 'password') {
				await api('/api/login', { method: 'POST', body: JSON.stringify({ email, password }) });
			} else {
				await api('/api/demo/login', { method: 'POST', body: JSON.stringify({ email: persona }) });
			}
			window.location.assign(safeNext());
		} catch (e) {
			wrongPassword = mode === 'password' && e instanceof APIError && (e.status === 401 || e.status === 403);
			error = wrongPassword ? 'That email and password don’t match. Check both and try again.' : errorMessage(e);
			busy = false;
		}
	}
</script>

<svelte:head><title>Sign in · Zoom Connect</title></svelte:head>

<div class="flex min-h-svh flex-col bg-surface px-4 py-6">
	<div class="flex items-center gap-2 font-semibold">
		<span aria-hidden="true" class="flex size-8 items-center justify-center rounded-md bg-primary text-primary-foreground"><Video class="size-4" /></span>
		Zoom Connect
	</div>

	<main id="main" class="flex flex-1 items-center justify-center py-10">
		<Card.Root class="w-full max-w-sm">
			<Card.Header>
				<h1 class="text-2xl font-semibold tracking-tight">Sign in</h1>
				<Card.Description class="text-base">Transcripts of your team’s Zoom calls, for you and your agents.</Card.Description>
			</Card.Header>
			<Card.Content class="flex flex-col gap-6">
				{#if error}
					<Alert.Root variant="destructive" role="alert">
						<CircleAlert aria-hidden="true" />
						<Alert.Title>We couldn’t sign you in</Alert.Title>
						<Alert.Description>{error}</Alert.Description>
					</Alert.Root>
				{/if}

				{#if mode === 'loading'}
					<p class="flex items-center gap-2 text-muted-foreground"><Spinner aria-hidden="true" role="presentation" />Checking how to sign in…</p>
				{:else if mode === 'password'}
					<form class="flex flex-col gap-6" onsubmit={signIn}>
						<Field.Group>
							<Field.Field>
								<Field.Label for="email">Work email</Field.Label>
								<Input id="email" type="email" name="email" autocomplete="username" required bind:value={email} />
							</Field.Field>
							<Field.Field data-invalid={wrongPassword || undefined}>
								<Field.Label for="password">Password</Field.Label>
								<Input id="password" type="password" name="password" autocomplete="current-password" required bind:value={password} aria-invalid={wrongPassword || undefined} />
							</Field.Field>
						</Field.Group>
						<Button type="submit" size="lg" disabled={busy}>
							{#if busy}<Spinner aria-hidden="true" role="presentation" />Signing in…{:else}Sign in{/if}
						</Button>
					</form>
				{:else if mode === 'demo'}
					<form class="flex flex-col gap-6" onsubmit={signIn}>
						<Field.Field>
							<Field.Label for="persona">Sign in as</Field.Label>
							<NativeSelect.Root id="persona" class="w-full" bind:value={persona} aria-describedby="persona-help">
								{#each users as user (user.id)}
									<NativeSelect.Option value={user.email}>{user.name}{user.is_admin ? ' (admin)' : ''}</NativeSelect.Option>
								{/each}
							</NativeSelect.Root>
							<Field.Description id="persona-help">Local demo. Each person sees only the calls they were in or were given.</Field.Description>
						</Field.Field>
						<Button type="submit" size="lg" disabled={busy || !persona}>
							{#if busy}<Spinner aria-hidden="true" role="presentation" />Signing in…{:else}Continue{/if}
						</Button>
					</form>
				{:else if !error}
					<p class="text-muted-foreground">Sign-in isn’t set up on this server yet. Ask your administrator.</p>
				{/if}
			</Card.Content>
		</Card.Root>
	</main>

	<p class="mx-auto max-w-sm text-center text-sm text-muted-foreground">Only people at your company can sign in. You’ll see calls you were in and calls someone shared with you.</p>
</div>
