<script lang="ts">
	import { onMount } from 'svelte';
	import { Copy, X, Check } from '@lucide/svelte';
	import * as Button from '#lib/components/ui/button';
	import * as Dialog from '#lib/components/ui/dialog';
	import * as Alert from '#lib/components/ui/alert';
	import { api, errorMessage, initials } from '#lib/api';
	import type { Person, Share } from '#lib/types';
	let { open = $bindable(false), callId, topic, hostId }: { open?: boolean; callId: string; topic: string; hostId: string } = $props();
	let shares = $state<Share[]>([]); let participants = $state<Person[]>([]); let canShare = $state(false);
	let people = $state<Person[]>([]); let search = $state(''); let loading = $state(false); let busy = $state(false);
	let error = $state(''); let toast = $state(''); let searchTimer: ReturnType<typeof setTimeout>;
	let link = $state('');
	const candidateEmail = $derived(/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(search.trim()));
	onMount(() => () => clearTimeout(searchTimer));
	$effect(() => { if (open) { link = window.location.href; void load(); } else { search=''; people=[]; error=''; } });
	async function load() {
		loading=true; error='';
		try { const result=await api<{participants:Person[];shares:Share[];can_share:boolean}>(`/api/calls/${encodeURIComponent(callId)}/shares`); participants=result.participants;shares=result.shares;canShare=result.can_share; }
		catch(e){error=errorMessage(e);}
		finally{loading=false;}
	}
	function lookup(value: string) {
		search=value; clearTimeout(searchTimer);
		if(value.trim().length<2){people=[];return;}
		searchTimer=setTimeout(async()=>{try{people=await api<Person[]>(`/api/people?q=${encodeURIComponent(value.trim())}`);}catch(e){error=errorMessage(e);}},250);
	}
	async function add(email=search) {
		if(!email.trim())return;
		busy=true;error='';
		try { await api(`/api/calls/${encodeURIComponent(callId)}/shares`,{method:'POST',body:JSON.stringify({email:email.trim()})}); await load(); const copied=await copyLink(); search='';people=[];notify(copied?'Access added and link copied':'Access added. Clipboard access failed; copy the link below.'); }
		catch(e){error=errorMessage(e);}
		finally{busy=false;}
	}
	async function remove(grant: Share) {
		error='';try{await api(`/api/calls/${encodeURIComponent(callId)}/shares/${encodeURIComponent(grant.id)}`,{method:'DELETE'});shares=shares.filter(s=>s.id!==grant.id);notify(`Removed ${grant.email}`);}catch(e){error=errorMessage(e);}
	}
	async function copyLink() {
		try{await navigator.clipboard.writeText(link);return true;}catch{return false;}
	}
	function notify(message:string){toast=message;setTimeout(()=>toast='',2500);}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="share-dialog">
		<Dialog.Header><Dialog.Title>Share “{topic}”</Dialog.Title><Dialog.Description>Add people by email to give them access to this call and its transcript.</Dialog.Description></Dialog.Header>
		{#if error}<Alert.Root variant="destructive"><Alert.Title>Sharing failed</Alert.Title><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}
		{#if canShare}<div class="share-search"><input class="search-input" aria-label="Find a person or enter an email" placeholder="Search by name or enter any email address" bind:value={search} oninput={(e)=>lookup(e.currentTarget.value)}/><div class="share-suggestions">{#each people as person}<button onclick={()=>add(person.email||'')}><span class="avatar">{initials(person.name)}</span><span><b>{person.name}</b><small>{person.email}</small></span><span class="add-text">Add</span></button>{/each}{#if candidateEmail&&!people.some(p=>p.email?.toLowerCase()===search.trim().toLowerCase())}<button onclick={()=>add()}><span class="avatar">@</span><span><b>{search}</b><small>Grant access when they sign in</small></span><span class="add-text">Add</span></button>{/if}</div></div>
		{:else}<p class="muted small">You can view this call. Only a participant can add people.</p>{/if}
		<h3>People with access</h3>{#if loading}<div class="loading-line"><span class="spinner"></span> Loading access…</div>{:else}<div class="grant-list">
			{#each participants as person}<div class="grant-row"><span class="avatar">{initials(person.name)}</span><span><b>{person.name}</b><small>{person.id===hostId?'Host · participant':person.email||'Participant'}</small></span><span class="badge-muted">Participant</span></div>{/each}
			{#each shares as grant}<div class="grant-row"><span class="avatar">{initials(grant.name||grant.email)}</span><span><b>{grant.name||grant.email}</b><small>Shared by {grant.granted_by_name}</small></span>{#if grant.can_remove}<Button.Root variant="ghost" size="sm" onclick={()=>remove(grant)}><X size={14}/> Remove</Button.Root>{:else}<span class="badge-muted">Shared</span>{/if}</div>{/each}
		</div>{/if}
		<div class="share-link"><label for="share-url">Link</label><div><input id="share-url" readonly value={link}/><Button.Root variant="outline" onclick={async()=>{const ok=await copyLink();notify(ok?'Link copied':'Clipboard access failed');}}><Copy size={14}/> Copy</Button.Root></div><small>The link only opens for participants and people added above.</small></div>
		<Dialog.Footer><Button.Root variant="outline" onclick={()=>open=false}>Done</Button.Root>{#if canShare}<Button.Root disabled={busy||!search.trim()} onclick={()=>add()}>{busy?'Adding…':'Add & copy link'}</Button.Root>{/if}</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
{#if toast}<div class="toast"><Check size={15}/>{toast}</div>{/if}

<style>
	:global(.share-dialog){max-width:560px}
	.share-search{position:relative}.share-suggestions{position:absolute;top:42px;left:0;right:0;z-index:8;background:var(--popover);border:1px solid var(--border);border-radius:8px;max-height:210px;overflow:auto;box-shadow:0 8px 20px #0002}.share-suggestions:empty{display:none}
	.share-suggestions button{display:flex;width:100%;align-items:center;gap:9px;text-align:left;padding:8px;background:transparent;border:0;cursor:pointer}.share-suggestions button:hover{background:var(--muted)}.share-suggestions button>span:nth-child(2){flex:1;display:grid}.share-suggestions small{color:var(--muted-foreground)}.add-text{color:var(--primary);font-weight:600;font-size:12px}
	h3{margin:14px 0 8px;font-size:13px}.grant-list{display:grid;gap:8px}.grant-row{display:flex;gap:9px;align-items:center;min-width:0}.grant-row>span:nth-child(2){flex:1;min-width:0;display:grid}.grant-row b{font-size:12px;font-weight:550}.grant-row small{color:var(--muted-foreground);font-size:11px;overflow:hidden;text-overflow:ellipsis}.share-link{border-top:1px solid var(--border);margin-top:14px;padding-top:12px;display:grid;gap:7px}.share-link label{font-size:12px;font-weight:550}.share-link>div{display:flex;gap:7px}.share-link input{flex:1;min-width:0;border:1px solid var(--input);border-radius:7px;padding:7px;background:var(--muted);font-size:11px}.share-link small{color:var(--muted-foreground);font-size:11px}.loading-line{display:flex;align-items:center;gap:8px;color:var(--muted-foreground);font-size:12px}.toast{position:fixed;z-index:60;bottom:20px;right:20px;padding:11px 14px;background:var(--foreground);color:var(--background);border-radius:9px;display:flex;align-items:center;gap:8px;box-shadow:0 8px 25px #0002}
</style>
