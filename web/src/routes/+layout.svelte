<script lang="ts">
	import '../app.css';
	import favicon from '#lib/assets/favicon.svg';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { ModeWatcher } from 'mode-watcher';
	import AppShell from '#lib/components/AppShell.svelte';
	import { Toaster } from '#lib/components/ui/sonner';
	import { Spinner } from '#lib/components/ui/spinner';
	import { Button } from '#lib/components/ui/button';
	import { api, APIError } from '#lib/api';
	import { preferences } from '#lib/preferences.svelte';
	import { setUser } from '#lib/session';
	import type { User } from '#lib/types';
	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();
	let user = $state<User | null>(null);
	let status = $state<'loading' | 'ready' | 'offline'>('loading');
	const isSignIn = $derived(page.url.pathname === '/');

	setUser(() => user as User);

	onMount(() => {
		preferences.load();
		if (!isSignIn) void loadUser();
	});

	async function loadUser() {
		status = 'loading';
		try {
			user = await api<User>('/api/me');
			status = 'ready';
		} catch (e) {
			if (e instanceof APIError && e.status === 401) {
				window.location.replace('/?next=' + encodeURIComponent(page.url.pathname + page.url.search));
				return;
			}
			status = 'offline';
		}
	}
</script>

<svelte:head>
	<title>Zoom Connect</title>
	<link rel="icon" href={favicon} />
</svelte:head>

<ModeWatcher modeStorageKey="zoom-connect-theme" disableTransitions />
<Toaster position="bottom-right" closeButton />

{#if isSignIn}
	{@render children()}
{:else if status === 'ready' && user}
	<AppShell {user}>{@render children()}</AppShell>
{:else if status === 'offline'}
	<main class="grid min-h-svh place-items-center p-6">
		<div role="alert" class="flex max-w-sm flex-col items-center gap-3 text-center">
			<h1 class="text-xl font-semibold">We couldn’t reach Zoom Connect</h1>
			<p class="text-muted-foreground">Check your connection, then try again.</p>
			<Button onclick={loadUser}>Try again</Button>
		</div>
	</main>
{:else}
	<main class="grid min-h-svh place-items-center p-6 text-muted-foreground">
		<p class="flex items-center gap-2"><Spinner aria-hidden="true" role="presentation" />Loading your calls…</p>
	</main>
{/if}
