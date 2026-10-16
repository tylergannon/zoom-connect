<script lang="ts">
	import { onMount } from 'svelte';
	import { Copy, KeyRound, Plus, RefreshCw, Trash2, Check } from '@lucide/svelte';
	import * as Button from '#lib/components/ui/button';
	import * as Card from '#lib/components/ui/card';
	import * as Badge from '#lib/components/ui/badge';
	import * as Dialog from '#lib/components/ui/dialog';
	import * as AlertDialog from '#lib/components/ui/alert-dialog';
	import * as Alert from '#lib/components/ui/alert';
	import { api, APIError, errorMessage, relativeDate } from '#lib/api';
	import type { APIKey } from '#lib/types';

	let keys = $state<APIKey[]>([]);
	let loading = $state(true); let error = $state('');
	let createOpen = $state(false); let name = $state(''); let busy = $state(false);
	let reveal = $state<{ key: APIKey; secret: string } | null>(null);
	let revealOpen = $state(false);
	let revokeTarget = $state<APIKey | null>(null); let revoked = $state(false); let notice = $state('');
	onMount(() => { void load(); });
	async function load() {
		loading = true; error = '';
		try { keys = await api<APIKey[]>('/api/keys'); }
		catch (e) { if (e instanceof APIError && e.status===401) window.location.href='/'; else error=errorMessage(e); }
		finally { loading = false; }
	}
	async function createKey() {
		if (!name.trim()) return;
		busy = true; error = '';
		try {
			const created = await api<{ key: APIKey; secret: string }>('/api/keys', { method:'POST', body:JSON.stringify({name:name.trim()}) });
			keys = [created.key, ...keys]; reveal = created; revealOpen = true; name = ''; createOpen = false;
		} catch (e) { error = errorMessage(e); }
		finally { busy = false; }
	}
	async function revokeKey() {
		if (!revokeTarget) return;
		try { await api(`/api/keys/${encodeURIComponent(revokeTarget.id)}`, { method:'DELETE' }); keys = keys.map(k => k.id===revokeTarget?.id ? {...k, revoked_at:new Date().toISOString()} : k); revoked = false; notice='API key revoked'; setTimeout(()=>notice='',1800); }
		catch (e) { error=errorMessage(e); }
	}
	async function copy(value: string) {
		try { await navigator.clipboard.writeText(value); notice='Copied to clipboard'; setTimeout(()=>notice='',1800); }
		catch { error='Clipboard access failed. Select and copy the key manually.'; }
	}
</script>

<svelte:head><title>API keys · Zoom Connect</title></svelte:head>
<div class="page-head"><div><h1>API keys</h1><p>Keys act as you: agents see exactly the calls you can see.</p></div><Button.Root onclick={() => { error=''; createOpen=true; }}><Plus size={16}/> Create key</Button.Root></div>
<Card.Root class="notice-card"><div class="notice-inner"><span class="key-icon"><KeyRound size={18}/></span><div><b>Treat keys like passwords</b><p>We show a key’s secret once, when you create it. Revoke it when an agent no longer needs access.</p></div><a href="/agent-api">Agent API guide →</a></div></Card.Root>
{#if error}<Alert.Root variant="destructive" class="mt-4"><Alert.Title>Request failed</Alert.Title><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}
{#if loading}<div class="panel empty-state"><span class="spinner"></span><p>Loading keys…</p></div>
{:else if !keys.length}<Card.Root class="panel empty-state"><h2>No API keys yet</h2><p>Create a key to connect an agent to calls you can access.</p><Button.Root onclick={() => createOpen=true}><Plus size={16}/> Create your first key</Button.Root></Card.Root>
{:else}<Card.Root class="keys-card"><div class="table-scroll"><table class="data"><thead><tr><th>Name</th><th>Key</th><th class="hide-mobile">Created</th><th class="hide-mobile">Last used</th><th>Status</th><th></th></tr></thead><tbody>
	{#each keys as key}<tr class:revoked={!!key.revoked_at}><td><b>{key.name}</b></td><td><span class="mono">{key.prefix}…</span></td><td class="hide-mobile">{relativeDate(key.created_at)}</td><td class="hide-mobile">{relativeDate(key.last_used_at)}</td><td>{#if key.revoked_at}<Badge.Badge variant="secondary">Revoked</Badge.Badge>{:else}<Badge.Badge variant="outline">Active</Badge.Badge>{/if}</td><td>{#if !key.revoked_at}<Button.Root variant="ghost" size="sm" onclick={() => { revokeTarget=key; revoked=true; }}><Trash2 size={14}/> Revoke</Button.Root>{/if}</td></tr>{/each}
</tbody></table></div></Card.Root>{/if}

<Dialog.Root bind:open={createOpen}><Dialog.Content><Dialog.Header><Dialog.Title>Create API key</Dialog.Title><Dialog.Description>Give this key a name you’ll recognize when an agent uses it.</Dialog.Description></Dialog.Header>
	<div class="field"><label for="key-name">Name</label><input id="key-name" bind:value={name} maxlength="80" placeholder="notes-agent" onkeydown={(e)=>e.key==='Enter'&&createKey()}/><small class="muted">This key will see every call you can access until revoked.</small></div>
	{#if error}<p class="error-text">{error}</p>{/if}<Dialog.Footer><Button.Root variant="outline" onclick={() => createOpen=false}>Cancel</Button.Root><Button.Root disabled={busy||!name.trim()} onclick={createKey}>{busy?'Creating…':'Create key'}</Button.Root></Dialog.Footer>
</Dialog.Content></Dialog.Root>

<Dialog.Root bind:open={revealOpen}>
	<Dialog.Content onInteractOutside={(e)=>e.preventDefault()} onEscapeKeydown={(e)=>e.preventDefault()}><Dialog.Header><Dialog.Title>Copy your key now</Dialog.Title><Dialog.Description>This secret will not be shown again. Store it in your agent’s secrets.</Dialog.Description></Dialog.Header>
	{#if reveal}<div class="secret-field"><code>{reveal.secret}</code><Button.Root variant="outline" onclick={()=>copy(reveal!.secret)}><Copy size={15}/> Copy key</Button.Root><Button.Root variant="secondary" onclick={()=>copy(`curl -N -H "Authorization: Bearer ${reveal!.secret}" ${window.location.origin}/api/stream`)}>Copy as curl</Button.Root></div>{/if}
	<p class="muted small">You won’t see this again. If you lose it, revoke this key and create another.</p><Dialog.Footer><Button.Root onclick={()=>{revealOpen=false;reveal=null;}}>I’ve copied it</Button.Root></Dialog.Footer>
</Dialog.Content></Dialog.Root>

<AlertDialog.Root bind:open={revoked}><AlertDialog.Content><AlertDialog.Header><AlertDialog.Title>Revoke “{revokeTarget?.name}”?</AlertDialog.Title><AlertDialog.Description>Agents using {revokeTarget?.prefix}… will lose access immediately, including open streams.</AlertDialog.Description></AlertDialog.Header><AlertDialog.Footer><AlertDialog.Cancel onclick={()=>revokeTarget=null}>Cancel</AlertDialog.Cancel><AlertDialog.Action onclick={revokeKey}>Revoke key</AlertDialog.Action></AlertDialog.Footer></AlertDialog.Content></AlertDialog.Root>
{#if notice}<div class="toast"><Check size={15}/>{notice}</div>{/if}
<style>
	:global(.notice-card) { margin-bottom:16px; }
	.notice-inner { display:flex; align-items:center; gap:13px; padding:14px 16px; }
	.key-icon { display:grid; place-items:center; width:36px; height:36px; border-radius:9px; background:var(--muted); }
	.notice-inner div { flex:1; }
	.notice-inner b { font-weight:600; }
	.notice-inner p { margin:2px 0 0; color:var(--muted-foreground); font-size:12px; }
	.notice-inner > a { font-size:12px; font-weight:550; }
	:global(.keys-card) { padding:0; }
	.revoked td { color:var(--muted-foreground); }
	.revoked td b { text-decoration:line-through; }
	.secret-field { display:grid; gap:10px; }
	.secret-field code { padding:12px; border:1px solid var(--border); border-radius:8px; background:var(--muted); word-break:break-all; font:13px ui-monospace,monospace; }
	.secret-field :global(button) { width:100%; }
	.toast { position:fixed; bottom:20px; right:20px; padding:11px 14px; color:var(--background); background:var(--foreground); border-radius:9px; display:flex; gap:8px; align-items:center; z-index:60; }
	@media(max-width:640px){.notice-inner{align-items:flex-start; flex-wrap:wrap}.notice-inner>a{margin-left:49px}}
</style>
