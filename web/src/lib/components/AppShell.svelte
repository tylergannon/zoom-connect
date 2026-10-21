<script lang="ts">
	import { onMount, type Snippet } from 'svelte';
	import { afterNavigate } from '$app/navigation';
	import { page } from '$app/state';
	import { setMode, userPrefersMode } from 'mode-watcher';
	import Video from '@lucide/svelte/icons/video';
	import KeyRound from '@lucide/svelte/icons/key-round';
	import BookOpen from '@lucide/svelte/icons/book-open';
	import Plug from '@lucide/svelte/icons/plug';
	import ChevronsUpDown from '@lucide/svelte/icons/chevrons-up-down';
	import UserRound from '@lucide/svelte/icons/user-round';
	import LogOut from '@lucide/svelte/icons/log-out';
	import Menu from '@lucide/svelte/icons/menu';
	import { Button } from '#lib/components/ui/button';
	import * as Avatar from '#lib/components/ui/avatar';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu';
	import * as Sheet from '#lib/components/ui/sheet';
	import { toast } from 'svelte-sonner';
	import { api, errorMessage, initials } from '#lib/api';
	import { callIndex } from '#lib/calls.svelte';
	import { domainOf } from '#lib/session';
	import type { User } from '#lib/types';

	let { user, children }: { user: User; children: Snippet } = $props();
	let menuOpen = $state(false);

	const groups = $derived([
		{ label: '', links: [{ href: '/calls', label: 'Calls', Icon: Video }] },
		{ label: 'For agents', links: [
			{ href: '/keys', label: 'API keys', Icon: KeyRound },
			{ href: '/agent-api', label: 'API guide', Icon: BookOpen }
		] },
		...(user.is_admin ? [{ label: 'Admin', links: [{ href: '/admin/zoom', label: 'Zoom connection', Icon: Plug }] }] : [])
	]);
	const liveCount = $derived(callIndex.live.length);
	const current = (href: string) => page.url.pathname === href || page.url.pathname.startsWith(href + '/');

	onMount(() => callIndex.start());
	afterNavigate(() => { menuOpen = false; });

	async function signOut() {
		try {
			await api('/api/logout', { method: 'POST' });
			window.location.assign('/');
		} catch (e) {
			toast.error('Couldn’t sign out', { description: errorMessage(e) });
		}
	}
</script>

{#snippet brand()}
	<a href="/calls" class="flex items-center gap-2.5 rounded-md p-2 text-sidebar-foreground">
		<span aria-hidden="true" class="flex size-8 items-center justify-center rounded-md bg-sidebar-primary text-sidebar-primary-foreground">
			<Video class="size-4" />
		</span>
		<span class="flex flex-col leading-tight">
			<span class="font-semibold">Zoom Connect</span>
			<span class="text-xs text-muted-foreground">{domainOf(user)}</span>
		</span>
	</a>
{/snippet}

{#snippet sidebar()}
	<div class="flex h-full flex-col gap-1 px-2 py-3">
		{@render brand()}
		<nav aria-label="Main" class="mt-2 flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto">
			{#each groups as group, i (group.label)}
				<div class="flex flex-col gap-1">
					{#if group.label}
						<h2 id="nav-group-{i}" class="px-2 text-xs font-medium text-muted-foreground">{group.label}</h2>
					{/if}
					<ul class="flex flex-col gap-0.5" aria-labelledby={group.label ? `nav-group-${i}` : undefined}>
						{#each group.links as link (link.href)}
							<li>
								<a
									href={link.href}
									aria-current={current(link.href) ? 'page' : undefined}
									class="flex h-9 items-center gap-2 rounded-md px-2 text-sm text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground aria-[current=page]:bg-sidebar-accent aria-[current=page]:font-medium aria-[current=page]:text-sidebar-accent-foreground"
								>
									<link.Icon class="size-4 shrink-0" aria-hidden="true" />
									<span>{link.label}</span>
									{#if link.href === '/calls' && liveCount}
										<span class="ml-auto inline-flex h-5 items-center rounded-full bg-live px-1.5 text-xs font-medium text-live-foreground">
											<span class="sr-only">, </span>{liveCount} live
										</span>
									{/if}
								</a>
							</li>
						{/each}
					</ul>
				</div>
			{/each}
		</nav>
		<div class="border-t border-sidebar-border pt-2">
			<DropdownMenu.Root>
				<DropdownMenu.Trigger>
					{#snippet child({ props })}
						<button
							{...props}
							type="button"
							class="flex w-full items-center gap-2 rounded-md p-2 text-left text-sm hover:bg-sidebar-accent aria-expanded:bg-sidebar-accent"
						>
							<Avatar.Root class="size-8" aria-hidden="true">
								<Avatar.Fallback>{initials(user.name)}</Avatar.Fallback>
							</Avatar.Root>
							<span class="grid min-w-0 flex-1">
								<span class="truncate font-medium">{user.name}</span>
								<span class="truncate text-xs text-muted-foreground">{user.email}</span>
							</span>
							<ChevronsUpDown class="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
							<span class="sr-only">Account menu</span>
						</button>
					{/snippet}
				</DropdownMenu.Trigger>
				<DropdownMenu.Content side="top" align="start" class="w-(--bits-dropdown-menu-anchor-width) min-w-56">
					<DropdownMenu.Item>
						{#snippet child({ props })}
							<a href="/account" {...props}><UserRound aria-hidden="true" />Account</a>
						{/snippet}
					</DropdownMenu.Item>
					<DropdownMenu.Separator />
					<DropdownMenu.Group>
						<DropdownMenu.GroupHeading>Theme</DropdownMenu.GroupHeading>
						<DropdownMenu.RadioGroup value={userPrefersMode.current} onValueChange={(v) => setMode(v as 'light' | 'dark' | 'system')}>
							<DropdownMenu.RadioItem value="system">Match my device</DropdownMenu.RadioItem>
							<DropdownMenu.RadioItem value="light">Light</DropdownMenu.RadioItem>
							<DropdownMenu.RadioItem value="dark">Dark</DropdownMenu.RadioItem>
						</DropdownMenu.RadioGroup>
					</DropdownMenu.Group>
					<DropdownMenu.Separator />
					<DropdownMenu.Item onSelect={signOut}><LogOut aria-hidden="true" />Sign out</DropdownMenu.Item>
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		</div>
	</div>
{/snippet}

<a
	href="#main"
	class="sr-only z-50 rounded-md bg-background px-3 py-2 font-medium shadow-md focus:not-sr-only focus:fixed focus:top-3 focus:left-3"
>Skip to content</a>

<div class="flex min-h-svh">
	<div class="sticky top-0 hidden h-svh w-64 shrink-0 border-r border-sidebar-border bg-sidebar text-sidebar-foreground md:block">
		{@render sidebar()}
	</div>

	<div class="flex min-w-0 flex-1 flex-col">
		<header class="sticky top-0 z-30 flex h-14 items-center gap-2 border-b bg-background px-2 md:hidden">
			<Button variant="ghost" size="icon-lg" aria-expanded={menuOpen} onclick={() => (menuOpen = true)}>
				<Menu aria-hidden="true" /><span class="sr-only">Menu</span>
			</Button>
			<a href="/calls" class="flex items-center gap-2 font-semibold">
				<Video class="size-4" aria-hidden="true" />Zoom Connect
			</a>
			{#if liveCount}
				<a href="/calls" class="ml-auto inline-flex h-6 items-center rounded-full bg-live px-2 text-xs font-medium text-live-foreground">{liveCount} live</a>
			{/if}
		</header>

		<main id="main" tabindex="-1" class="flex-1 px-4 pt-8 pb-20 sm:px-8 lg:px-12 lg:pt-10">
			<div class="mx-auto w-full max-w-6xl">
				{@render children()}
			</div>
		</main>
	</div>
</div>

<Sheet.Root bind:open={menuOpen}>
	<Sheet.Content side="left" class="w-72 gap-0 bg-sidebar p-0">
		<Sheet.Title class="sr-only">Menu</Sheet.Title>
		{@render sidebar()}
	</Sheet.Content>
</Sheet.Root>
