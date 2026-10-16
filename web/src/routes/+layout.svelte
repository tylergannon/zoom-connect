<script lang="ts">
	import '../app.css';
	import favicon from '#lib/assets/favicon.svg';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import AppShell from '#lib/components/AppShell.svelte';
	import { api } from '#lib/api';
	import type { User } from '#lib/types';
	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();
	let user = $state<User | null>(null);
	let loading = $state(true);
	onMount(async () => {
		try { user = await api<User>('/api/me'); }
		catch { user = null; }
		finally { loading = false; }
	});
</script>

<svelte:head>
	<title>Zoom Connect</title>
	<meta name="viewport" content="width=device-width, initial-scale=1" />
	<link rel="icon" href={favicon} />
</svelte:head>

{#if page.url.pathname === '/'}
	{@render children()}
{:else if loading}
	<div class="route-loading"><span class="spinner"></span> Loading your workspace…</div>
{:else if user}
	<AppShell {user}>{@render children()}</AppShell>
{:else}
	<div class="route-loading"><p>Sign in to open Zoom Connect.</p><a class="sign-in-link" href="/?next={encodeURIComponent(page.url.pathname + page.url.search)}">Continue to sign in</a></div>
{/if}
