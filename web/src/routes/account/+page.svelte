<script lang="ts">
	import { onMount } from 'svelte';
	import { goto, invalidateAll } from '$app/navigation';
	import { UserRound, LogOut, RefreshCw, Shield, Users } from '@lucide/svelte';
	import * as Button from '#lib/components/ui/button';
	import * as Card from '#lib/components/ui/card';
	import * as Badge from '#lib/components/ui/badge';
	import * as Select from '#lib/components/ui/select';
	import { api, APIError, errorMessage, initials } from '#lib/api';
	import type { User, Call, APIKey } from '#lib/types';
	let passwordMode=$state(false); let user=$state<User|null>(null); let users=$state<User[]>([]); let calls=$state<Call[]>([]); let keys=$state<APIKey[]>([]); let loading=$state(true); let busy=$state(false); let error=$state(''); let selected=$state('');
	onMount(()=>{void load();});
	async function load(){loading=true;error='';try{[user,calls,keys]=await Promise.all([api<User>('/api/me'),api<Call[]>('/api/calls'),api<APIKey[]>('/api/keys')]);const options=await api<{password:boolean}>('/api/auth/options');passwordMode=options.password;if(!passwordMode)users=await api<User[]>('/api/demo/users');selected=user?.email||'';}catch(e){if(e instanceof APIError&&e.status===401){window.location.href='/';return;}error=errorMessage(e);}finally{loading=false;}}
	async function switchPersona(){if(!selected||selected===user?.email)return;busy=true;error='';try{await api<User>('/api/demo/login',{method:'POST',body:JSON.stringify({email:selected})});await invalidateAll();window.location.reload();}catch(e){error=errorMessage(e);}finally{busy=false;}}
	async function logout(){try{await api('/api/logout',{method:'POST'});window.location.href='/';}catch(e){error=errorMessage(e);}}
</script>

<svelte:head><title>Account · Zoom Connect</title></svelte:head>
<div class="page-head"><div><h1>Account</h1><p>Identity and local demo access.</p></div><Button.Root variant="outline" onclick={load}><RefreshCw size={15}/> Refresh</Button.Root></div>
{#if loading}<div class="panel empty-state"><span class="spinner"></span><p>Loading account…</p></div>
{:else if error}<Card.Root class="panel empty-state"><h2>Couldn't load your account</h2><p>{error}</p></Card.Root>
{:else if user}<div class="account-grid">
	<Card.Root class="panel-pad identity-card"><div class="identity-row"><span class="identity-avatar">{initials(user.name)}</span><div class="identity-name"><h2>{user.name}</h2><p>{user.email} · {passwordMode?'signed in with password':'signed in with Google (demo)'}</p></div>{#if user.is_admin}<Badge.Badge variant="outline"><Shield size={12}/> Admin</Badge.Badge>{/if}</div>
		{#if !passwordMode}<div class="demo-switch"><div><b>Switch demo persona</b><small>Use another seeded account to check call sharing and access.</small></div><Select.Root type="single" bind:value={selected}><Select.Trigger><span>{users.find(u=>u.email===selected)?.name||selected}</span></Select.Trigger><Select.Content>{#each users as person}<Select.Item value={person.email}>{person.name} · {person.email}{person.is_admin?' · admin':''}</Select.Item>{/each}</Select.Content></Select.Root><Button.Root variant="outline" disabled={busy||selected===user.email} onclick={switchPersona}>{busy?'Switching…':'Switch account'}</Button.Root></div>{/if}
		<Button.Root variant="outline" onclick={logout}><LogOut size={15}/> Sign out</Button.Root>
	</Card.Root>
	<div class="stats"><Card.Root class="panel-pad"><span>Calls you were in</span><b>{calls.filter(c=>c.is_participant).length}</b></Card.Root><Card.Root class="panel-pad"><span>Shared with you</span><b>{calls.filter(c=>!c.is_participant).length}</b></Card.Root><Card.Root class="panel-pad"><span>Active API keys</span><b>{keys.filter(k=>!k.revoked_at).length}</b></Card.Root></div>
	<Card.Root class="panel-pad deferred"><div class="deferred-head"><div><h2>Slack notifications</h2><p>DM alerts from the Zoom Connect Slack app.</p></div><Badge.Badge variant="secondary">Deferred</Badge.Badge></div><div class="disabled-line"><Users size={17}/><span><b>Slack is not connected</b><small>Slack integration is outside this local POC.</small></span></div></Card.Root>
</div>{/if}
<style>
	.account-grid{display:grid;gap:14px;max-width:900px}:global(.identity-card){display:grid;gap:20px}.identity-row{display:flex;gap:13px;align-items:center;flex-wrap:wrap}.identity-avatar{width:48px;height:48px;border-radius:50%;display:grid;place-items:center;background:var(--muted);font-weight:650}.identity-name{flex:1;min-width:200px}.identity-name h2{margin:0;font-size:16px}.identity-name p{margin:3px 0 0;color:var(--muted-foreground);font-size:12px}.demo-switch{display:grid;grid-template-columns:minmax(0,1fr) minmax(200px,260px) auto;gap:10px;align-items:center;padding:14px 0;border-top:1px solid var(--border);border-bottom:1px solid var(--border)}.demo-switch div{display:grid}.demo-switch b{font-size:13px}.demo-switch small{color:var(--muted-foreground);font-size:11px}.stats{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px}.stats :global(.panel-pad){display:grid}.stats span{font-size:12px;color:var(--muted-foreground)}.stats b{font-size:21px}:global(.deferred){border-style:dashed}.deferred-head{display:flex;align-items:start;justify-content:space-between}.deferred-head h2{font-size:15px;margin:0}.deferred-head p{font-size:12px;color:var(--muted-foreground);margin:4px 0 15px}.disabled-line{display:flex;align-items:center;gap:10px;color:var(--muted-foreground)}.disabled-line span{display:grid}.disabled-line b{font-size:12px}.disabled-line small{font-size:11px}@media(max-width:700px){.demo-switch{grid-template-columns:1fr}.stats{grid-template-columns:1fr}}
</style>
