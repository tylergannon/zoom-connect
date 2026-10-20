<script lang="ts">
	import { onMount } from 'svelte';
	import { Search, Users, ChevronRight, RefreshCw, Radio } from '@lucide/svelte';
	import * as Card from '#lib/components/ui/card';
	import * as Button from '#lib/components/ui/button';
	import * as Badge from '#lib/components/ui/badge';
	import { api, APIError, errorMessage, duration, relativeDate, initials } from '#lib/api';
	import AddScheduledCall from './AddScheduledCall.svelte';
	import type { Call } from '#lib/types';

	let calls = $state<Call[]>([]);
	let loading = $state(true);
	let error = $state('');
	let query = $state('');
	let filter = $state<'all'|'mine'|'shared'>('all');
	let streamState = $state<'connecting'|'live'|'reconnecting'>('connecting');
	let version = $state(0);
	let tick = $state(0);
	const filtered = $derived.by(() => {
		version;
		const q = query.trim().toLowerCase();
		return calls.filter((call) => {
			if (filter === 'mine' && !call.is_participant && !call.is_owner) return false;
			if (filter === 'shared' && (call.is_participant || call.is_owner)) return false;
			if (!q) return true;
			return call.topic.toLowerCase().includes(q) || [call.host, ...call.participants].some((p) => p.name.toLowerCase().includes(q));
		}).sort((a, b) => new Date(b.started || b.scheduled_at || 0).getTime() - new Date(a.started || a.scheduled_at || 0).getTime());
	});
	const liveCalls = $derived(filtered.filter((c) => c.status === 'live'));
	const scheduledCalls = $derived(filtered.filter((c) => c.status === 'scheduled').sort((a,b) => new Date(a.scheduled_at || 0).getTime() - new Date(b.scheduled_at || 0).getTime()));
	const archivedCalls = $derived(filtered.filter((c) => c.status !== 'live' && c.status !== 'scheduled'));
	const mineCount = $derived(calls.filter((c) => c.is_participant || c.is_owner).length);
	const sharedCount = $derived(calls.filter((c) => !c.is_participant && !c.is_owner).length);
	const showDuration = (call: Call) => { tick; return duration(call.started, call.status === 'live' ? undefined : call.ended); };
	onMount(() => {
		void load();
		const timer = setInterval(() => tick++, 1000);
		const stream = new EventSource('/api/stream');
		stream.onopen = () => streamState = 'live';
		stream.onerror = () => streamState = 'reconnecting';
		const refresh = () => { void load(true); };
		for (const type of ['call.scheduled', 'call.started', 'call.ended', 'call.shared']) stream.addEventListener(type, refresh);
		stream.onmessage = refresh;
		return () => { stream.close(); clearInterval(timer); };
	});
	async function load(quiet = false) {
		if (!quiet) { loading = true; error = ''; }
		try { calls = await api<Call[]>('/api/calls'); version++; }
		catch (e) {
			if (e instanceof APIError && e.status === 401) { window.location.href = '/'; return; }
			if (!quiet) error = errorMessage(e);
		} finally { if (!quiet) loading = false; }
	}
</script>

<svelte:head><title>Calls · Zoom Connect</title></svelte:head>
<div class="page-head"><div><h1>Calls</h1><p>Upcoming calls, live streams, and saved conversations.</p></div><div class="head-actions"><AddScheduledCall/><div class="inline-meta"><Radio size={15} /><span>{streamState === 'live' ? 'Live updates on' : streamState === 'reconnecting' ? 'Reconnecting to live updates' : 'Connecting to live updates'}</span></div></div></div>

{#if liveCalls.length}
	<h2 class="section-label"><span class="live-dot"></span> Live now</h2>
	<div class="live-grid">
		{#each liveCalls as call}
			<a href={'/calls/' + encodeURIComponent(call.id)} class="panel live-card">
				<div class="live-card-top"><h2>{call.topic}</h2><span class="badge-live">● LIVE {showDuration(call)}</span></div>
				<p>{#if call.latest_event?.data.text}<b>{call.latest_event.participant?.name || call.host.name}:</b> {call.latest_event.data.text}{:else}<span class="muted">Live transcript has not started yet.</span>{/if}</p>
				<div class="live-meta"><span class="avatar-stack">{#each call.participants.slice(0,4) as person}<span class="avatar" title={person.name}>{initials(person.name)}</span>{/each}</span><span><Users size={14} /> {call.participants.length} in call</span>{#if call.shared_by}<span>Shared by {call.shared_by}</span>{/if}</div>
			</a>
		{/each}
	</div>
{/if}

<div class="calls-toolbar">
	<div class="tabs-row" role="tablist" aria-label="Call access filter">
		<button class:active={filter==='all'} onclick={() => filter='all'}>All <b>{calls.length}</b></button>
		<button class:active={filter==='mine'} onclick={() => filter='mine'}>Added or attended <b>{mineCount}</b></button>
		<button class:active={filter==='shared'} onclick={() => filter='shared'}>Shared with me <b>{sharedCount}</b></button>
	</div>
	<div class="search-wrap"><Search size={16} /><input class="search-input" aria-label="Search calls" placeholder="Search by title or person" bind:value={query} /></div>
</div>

{#if error}<div class="panel empty-state"><h2>Couldn't load calls</h2><p>{error}</p><Button.Root onclick={() => load()}><RefreshCw size={15} /> Try again</Button.Root></div>
{:else if loading}<div class="panel empty-state"><span class="spinner"></span><p>Loading calls…</p></div>
{:else}
	{#if filtered.length === 0}<div class="panel empty-state"><h2>{calls.length ? 'No calls match' : 'No calls yet'}</h2><p>{calls.length ? 'Try another title or participant name.' : 'Calls will appear here when Zoom Connect hears one you are in or someone shares one with you.'}</p></div>{/if}
	{#if scheduledCalls.length}
  <h2 class="section-label upcoming-title">Upcoming</h2>
  <div class="upcoming-grid">{#each scheduledCalls as call}
   <a class="panel upcoming-card" href={'/calls/'+encodeURIComponent(call.id)}><span class="badge-muted">Scheduled</span><h2>{call.topic}</h2><p>{call.scheduled_at ? relativeDate(call.scheduled_at) : 'Waiting for the next session'}</p><small>Stream ready · clients can connect now →</small></a>
  {/each}</div>
 {/if}
 {#if archivedCalls.length}
	<Card.Root class="calls-panel"><div class="table-scroll"><table class="data"><thead><tr><th>Call</th><th class="hide-mobile">People</th><th>Duration</th><th>Access</th><th></th></tr></thead><tbody>
		{#each archivedCalls as call}
			<tr>
				<td><a href={'/calls/' + encodeURIComponent(call.id)} class="call-title">{call.topic}</a><div class="muted small">{relativeDate(call.started || call.scheduled_at)}</div></td>
				<td class="hide-mobile"><div class="avatar-stack">{#each call.participants.slice(0,3) as person}<span class="avatar" title={person.name}>{initials(person.name)}</span>{/each}{#if call.participants.length>3}<span class="badge-muted">+{call.participants.length-3}</span>{/if}</div></td>
				<td>{showDuration(call)}</td>
				<td>{#if call.status==='live'}<Badge.Badge variant="outline">Live</Badge.Badge>{:else if call.status==='cancelled'}<span class="badge-muted">Cancelled</span>{:else if call.is_owner}<span class="badge-muted">Added by you</span>{:else if call.is_participant}<span class="badge-muted">Participant</span>{:else}<span class="badge-muted">Shared by {call.shared_by || 'coworker'}</span>{/if}</td>
				<td><a class="row-open" href={'/calls/' + encodeURIComponent(call.id)} aria-label={'Open ' + call.topic}><ChevronRight size={17} /></a></td>
			</tr>
		{/each}
	</tbody></table></div></Card.Root>
{:else if filtered.length > 0 && !liveCalls.length && !scheduledCalls.length}<div class="panel empty-state"><h2>No finished calls match</h2><p>Try another title or participant name.</p></div>
{/if}
{/if}

<style>
 .head-actions { display:flex; align-items:center; gap:14px; flex-wrap:wrap; }
 .upcoming-title { margin:4px 0 12px; }
 .upcoming-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(min(280px,100%),1fr)); gap:12px; margin-bottom:18px; }
 .upcoming-card { padding:18px; }
 .upcoming-card h2 { font-size:16px; margin:12px 0 6px; }
 .upcoming-card p { color:var(--muted-foreground); margin:0 0 12px; }
 .upcoming-card small { font-size:12px; }
	.section-label { display:flex; gap:9px; align-items:center; margin:0; font-size:15px; font-weight:600; }
	.search-wrap { width:min(100%,310px); position:relative; display:flex; align-items:center; }
	.search-wrap :global(svg) { position:absolute; left:10px; color:var(--muted-foreground); }
	.search-input { padding-left:34px; }
	:global(.calls-panel) { padding:0; }
	.row-open { display:inline-flex; color:var(--muted-foreground); padding:5px; }
	.row-open:hover { color:var(--foreground); }
</style>
