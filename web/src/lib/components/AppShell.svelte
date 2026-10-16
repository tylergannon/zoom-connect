<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { Video, KeyRound, Code2, PlugZap, UserRound, Menu, Sun, Moon, LogOut, ChevronDown } from '@lucide/svelte';
	import * as Button from '#lib/components/ui/button';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu';
	import type { User } from '#lib/types';
	import type { Call } from '#lib/types';
	import { api } from '#lib/api';

	let { user, children }: { user: User; children: import('svelte').Snippet } = $props();
	let mobileOpen = $state(false);
	let dark = $state(false);
	let error = $state('');
	let liveCount = $state(0);
	const links = [
		{ href: '/calls', label: 'Calls', Icon: Video },
		{ href: '/keys', label: 'API keys', Icon: KeyRound },
		{ href: '/agent-api', label: 'Agent API', Icon: Code2 }
	];
	onMount(() => {
		dark = localStorage.getItem('zoom-connect-theme') === 'dark';
		applyTheme();
		void refreshCalls();
		const stream = new EventSource('/api/stream');
		const update = () => { void refreshCalls(); };
		stream.onmessage = update;
		for (const type of ['call.started', 'call.ended', 'call.shared']) stream.addEventListener(type, update);
		return () => stream.close();
	});
	async function refreshCalls() { try { liveCount = (await api<Call[]>('/api/calls')).filter((call) => call.status === 'live').length; } catch { liveCount = 0; } }
	function applyTheme() {
		document.documentElement.classList.toggle('dark', dark);
		localStorage.setItem('zoom-connect-theme', dark ? 'dark' : 'light');
	}
	function toggleTheme() { dark = !dark; applyTheme(); }
	async function logout() {
		try { await api('/api/logout', { method: 'POST' }); window.location.href = '/'; }
		catch (e) { error = e instanceof Error ? e.message : 'Sign out failed'; }
	}
</script>

<div class="app-shell">
	<header class="mobile-header">
		<Button.Root variant="ghost" size="icon" aria-label="Open menu" onclick={() => mobileOpen = !mobileOpen}><Menu size={19} /></Button.Root>
		<a href="/calls" class="brand"><span class="brand-mark"><Video size={17} /></span> Zoom Connect</a>
		<Button.Root variant="ghost" size="icon" aria-label="Toggle color theme" onclick={toggleTheme}>{#if dark}<Sun size={18} />{:else}<Moon size={18} />{/if}</Button.Root>
	</header>
	{#if mobileOpen}<button class="mobile-backdrop" aria-label="Close menu" onclick={() => mobileOpen = false}></button>{/if}
	<aside class:mobile-open={mobileOpen} class="sidebar">
		<a href="/calls" class="brand desktop-brand"><span class="brand-mark"><Video size={17} /></span> Zoom Connect</a>
		<nav aria-label="Main navigation">
			{#each links as link}
				<a class:active={page.url.pathname.startsWith(link.href)} href={link.href} onclick={() => mobileOpen = false}><link.Icon size={17} strokeWidth={1.8} /><span>{link.label}</span>{#if link.href==='/calls' && liveCount}<span class="sidebar-count">{liveCount}</span>{/if}</a>
			{/each}
			{#if user.is_admin}
				<div class="nav-label">Admin</div>
				<a class:active={page.url.pathname.startsWith('/admin/zoom')} href="/admin/zoom" onclick={() => mobileOpen = false}><PlugZap size={17} strokeWidth={1.8} /><span>Zoom connection</span></a>
			{/if}
		</nav>
		<div class="sidebar-bottom">
			{#if error}<p class="error-text">{error}</p>{/if}
			<Button.Root variant="ghost" class="theme-row" onclick={toggleTheme}>{#if dark}<Sun size={16} /> Light theme{:else}<Moon size={16} /> Dark theme{/if}</Button.Root>
			<DropdownMenu.Root>
				<DropdownMenu.Trigger class="user-trigger">
					<span class="avatar">{user.name.split(' ').map((s) => s[0]).join('').slice(0, 2)}</span>
					<span class="user-meta"><b>{user.name}</b><small>{user.email}</small></span><ChevronDown size={15} />
				</DropdownMenu.Trigger>
				<DropdownMenu.Content align="end" class="w-56">
					<DropdownMenu.Item onclick={() => window.location.href = '/account'}><UserRound size={15} /> Account</DropdownMenu.Item>
					<DropdownMenu.Item onclick={logout}><LogOut size={15} /> Sign out</DropdownMenu.Item>
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		</div>
	</aside>
	<main class="main-content">{@render children()}</main>
</div>
