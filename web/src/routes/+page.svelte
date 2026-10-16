<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { Video, ChevronDown, Sun, Moon } from '@lucide/svelte';
	import * as Card from '#lib/components/ui/card';
	import * as Button from '#lib/components/ui/button';
	import * as Select from '#lib/components/ui/select';
	import { api, errorMessage } from '#lib/api';
	import type { User } from '#lib/types';

	let users = $state<User[]>([]);
	let selected = $state('dana@acme.co');
	let busy = $state(false);
	let error = $state('');
	let dark = $state(false);
	onMount(async () => {
		dark = localStorage.getItem('zoom-connect-theme') === 'dark';
		document.documentElement.classList.toggle('dark', dark);
		try {
			const me = await api<User>('/api/me');
			if (me) await goto(safeNext());
		} catch { /* signed out */ }
		try { users = await api<User[]>('/api/demo/users'); }
		catch (e) { error = errorMessage(e); }
	});
	async function login() {
		busy = true; error = '';
		try {
			await api<User>('/api/demo/login', { method: 'POST', body: JSON.stringify({ email: selected }) });
			window.location.assign(safeNext());
		} catch (e) { error = errorMessage(e); }
		finally { busy = false; }
	}
	function safeNext() {
		const next = page.url.searchParams.get('next') || '/calls';
		return next.startsWith('/') && !next.startsWith('//') ? next : '/calls';
	}
	function theme() {
		dark = !dark;
		document.documentElement.classList.toggle('dark', dark);
		localStorage.setItem('zoom-connect-theme', dark ? 'dark' : 'light');
	}
</script>

<svelte:head><title>Sign in · Zoom Connect</title></svelte:head>
<div class="auth-page">
	<div class="auth-theme"><Button.Root variant="ghost" size="icon" aria-label="Toggle theme" onclick={theme}>{#if dark}<Sun size={18} />{:else}<Moon size={18} />{/if}</Button.Root></div>
	<Card.Root class="auth-card">
		<Card.Header class="auth-header">
			<span class="auth-mark"><Video size={20} /></span>
			<Card.Title>Zoom Connect</Card.Title>
			<Card.Description>Transcripts of the calls you were in — live and afterward.</Card.Description>
		</Card.Header>
		<Card.Content>
			<div class="demo-note"><span class="badge-muted">Local demo</span><span>Google sign-in is simulated for this prototype.</span></div>
			{#if users.length}
				<label class="field"><span>Choose a demo account</span>
					<Select.Root type="single" bind:value={selected}>
						<Select.Trigger><span>{users.find((u) => u.email === selected)?.name ?? 'Select a person'}</span><ChevronDown size={15} /></Select.Trigger>
						<Select.Content>{#each users as person}<Select.Item value={person.email}>{person.name} · {person.email}</Select.Item>{/each}</Select.Content>
					</Select.Root>
				</label>
			{:else}<p class="muted small">Loading demo accounts…</p>{/if}
			{#if error}<p class="error-text" role="alert">{error}</p>{/if}
			<Button.Root class="continue" disabled={busy || !users.length} onclick={login}>{busy ? 'Signing in…' : 'Continue with Google (demo)'}</Button.Root>
			<p class="auth-foot">Demo accounts use the <b>@acme.co</b> organization.</p>
		</Card.Content>
	</Card.Root>
</div>

<style>
	.auth-page { min-height:100vh; padding:60px 20px; display:grid; place-items:center; background:radial-gradient(1000px 400px at 50% -10%,color-mix(in oklch,var(--primary) 8%,var(--sheet)),var(--sheet)); position:relative; }
	:global(.auth-card) { width:min(100%,420px); }
	:global(.auth-card [data-slot="card-header"]) { align-items:center; text-align:center; padding:28px 28px 12px; }
	:global(.auth-card [data-slot="card-content"]) { display:grid; gap:16px; padding:18px 28px 28px; }
	.auth-mark { display:grid; place-items:center; width:44px; height:44px; background:var(--primary); color:var(--primary-foreground); border-radius:12px; margin-bottom:7px; }
	.demo-note { display:flex; gap:10px; align-items:center; color:var(--muted-foreground); font-size:12px; }
	:global(.continue) { width:100%; }
	.auth-foot { color:var(--muted-foreground); text-align:center; margin:0; font-size:12px; }
	.auth-theme { position:absolute; top:18px; right:18px; }
</style>
