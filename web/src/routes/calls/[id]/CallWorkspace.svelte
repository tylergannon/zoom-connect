<script lang="ts">
	import { Search, Share2, Copy, Download, Users, MessageSquare, RefreshCw } from '@lucide/svelte';
	import * as Button from '#lib/components/ui/button';
	import * as Card from '#lib/components/ui/card';
	import * as Badge from '#lib/components/ui/badge';
	import SharingDialog from './SharingDialog.svelte';
	import ScreenShare from './ScreenShare.svelte';
	import { api, APIError, errorMessage, duration, relativeDate, initials } from '#lib/api';
	import type { Call, EventItem, Person } from '#lib/types';

	let { callId: id }: { callId: string } = $props();
	let call = $state<Call | null>(null);
	let events = $state<EventItem[]>([]);
	let loading = $state(true);
	let error = $state('');
	let noAccess = $state(false);
	let noAccessPeople = $state<Person[]>([]);
	let missing = $state(false);
	let query = $state('');
	let tab = $state<'transcript'|'chat'>('transcript');
	let streamState = $state<'connecting'|'live'|'reconnecting'|'ended'>('connecting');
	let shareOpen = $state(false);
	let downloadsOpen = $state(false);
	let pageOrigin = $state('');
	let tick = $state(0);
	let following = $state(true);
	let transcriptElement: HTMLDivElement | undefined = $state();
	let source: EventSource | null = null;
	let recheckingAccess = false;
	const transcriptKinds = new Set(['transcript', 'chat', 'participant.joined', 'participant.left', 'stream.started', 'stream.stopped', 'stream.interrupted', 'speaker.active']);
	const shownEvents = $derived(events.filter((e) => tab === 'chat' ? e.kind === 'chat' : transcriptKinds.has(e.kind)).filter((e) => !query || `${e.data.text || ''} ${e.kind} ${e.participant?.name || ''}`.toLowerCase().includes(query.toLowerCase())));
	const showDuration = (started: string, ended?: string) => { tick; return duration(started, ended); };
	$effect(() => {
		const callId = id;
		if (!callId) return;
		call = null; events = []; loading = true; error = ''; noAccess = false; missing = false; noAccessPeople = [];
		streamState = 'connecting'; tab = 'transcript'; query = ''; following = true;
		pageOrigin = window.location.origin;
		const timer = setInterval(() => tick++, 1000);
		void load(callId);
		return () => { clearInterval(timer); source?.close(); source = null; };
	});
	$effect(() => {
		events.length;
		if (!following || !transcriptElement) return;
		requestAnimationFrame(() => { if (following && transcriptElement) transcriptElement.scrollTop = transcriptElement.scrollHeight; });
	});
	async function load(callId = id) {
		loading = true; error = ''; noAccess = false; missing = false;
		try {
			const result = await api<Call>(`/api/calls/${encodeURIComponent(callId)}`);
			if (callId !== id) return;
			call = result;
			events = await api<EventItem[]>(`/api/calls/${encodeURIComponent(callId)}/events?from=0`);
			if (callId !== id) return;
			following = true;
			connect(callId, events.reduce((max, e) => Math.max(max, e.seq), 0));
		} catch (e) {
			if (callId !== id) return;
			if (e instanceof APIError && e.status === 403) { noAccess = true; noAccessPeople = e.body?.participants || []; }
			else if (e instanceof APIError && e.status === 404) missing = true;
			else error = errorMessage(e);
		} finally { if (callId === id) loading = false; }
	}
	function connect(callId: string, from: number) {
		if (call?.status !== 'live') { streamState = 'ended'; return; }
		source?.close();
		source = new EventSource(`/api/calls/${encodeURIComponent(callId)}/stream?from=${from}`);
		streamState = 'connecting';
		source.onopen = () => streamState = 'live';
		source.onerror = () => { streamState = 'reconnecting'; void recheckAccess(callId); };
		const finish = async (status?: 'stopped'|'interrupted') => {
			if (callId !== id) return;
			streamState = 'ended';
			source?.close();
			if (status && call) call = { ...call, status, ended: call.ended || new Date().toISOString() };
			try {
				const latest = await api<Call>(`/api/calls/${encodeURIComponent(callId)}`);
				if (callId === id) call = latest;
			} catch { /* keep the terminal status already received */ }
		};
		const accept = (event: Event) => {
			const msg = event as MessageEvent;
			try {
				const item = JSON.parse(msg.data) as EventItem;
				if (callId !== id) return;
				if (!events.some((e) => e.seq === item.seq)) events = [...events, item].sort((a,b) => a.seq-b.seq);
				if (item.kind === 'stream.stopped' || item.kind === 'stream.interrupted') void finish(item.kind === 'stream.stopped' ? 'stopped' : 'interrupted');
			} catch { /* ignore malformed keepalive */ }
		};
		for (const kind of ['transcript','chat','participant.joined','participant.left','stream.started','stream.stopped','stream.interrupted','speaker.active','screen.frame']) source.addEventListener(kind, accept);
		source.addEventListener('end', () => { void finish(); });
		source.onmessage = accept;
	}
	async function recheckAccess(callId: string) {
		if (recheckingAccess) return;
		recheckingAccess = true;
		try {
			await api<Call>(`/api/calls/${encodeURIComponent(callId)}`);
		} catch (e) {
			if (callId !== id) return;
			if (e instanceof APIError && e.status === 403) {
				source?.close(); source = null; streamState = 'ended';
				noAccess = true; noAccessPeople = e.body?.participants || []; events = []; call = null;
			} else if (e instanceof APIError && e.status === 401) {
				source?.close(); source = null;
				window.location.assign('/?next=' + encodeURIComponent(window.location.pathname + window.location.search));
			}
		} finally { recheckingAccess = false; }
	}
	function handleTranscriptScroll() {
		if (!transcriptElement) return;
		following = transcriptElement.scrollHeight - transcriptElement.scrollTop - transcriptElement.clientHeight < 48;
	}
	function jumpToLive() {
		following = true;
		if (transcriptElement) transcriptElement.scrollTop = transcriptElement.scrollHeight;
	}
	async function copyLink() {
		try { await navigator.clipboard.writeText(window.location.href); return true; }
		catch { return false; }
	}
</script>

<svelte:head><title>{call?.topic || 'Call'} · Zoom Connect</title></svelte:head>

{#if loading}<div class="panel empty-state"><span class="spinner"></span><p>Loading call…</p></div>
{:else if noAccess}<div class="panel empty-state"><h2>You weren’t in this call</h2><p>Only participants and coworkers they’ve shared with can see this call. Ask someone who was there to share it with you.</p>{#if noAccessPeople.length}<div class="no-access-people">{#each noAccessPeople as person}<div><span class="avatar">{initials(person.name)}</span><span>{person.name}</span></div>{/each}</div>{/if}<a class="sign-in-link" href="/calls">Back to calls</a></div>
{:else if missing}<div class="panel empty-state"><h2>Call not found</h2><p>Check the link. Call IDs appear in a URL like <span class="mono">/calls/…</span>.</p><a class="sign-in-link" href="/calls">Back to calls</a></div>
{:else if error}<div class="panel empty-state"><h2>Couldn't load this call</h2><p>{error}</p><Button.Root onclick={() => load()}><RefreshCw size={15}/> Try again</Button.Root></div>
{:else if call}
	<div class="page-head"><div><a class="back-link" href="/calls">← Calls</a><h1>{call.topic}</h1><div class="call-sub"><span>{#if call.status==='live'}<span class="badge-live">● LIVE</span>{:else}<Badge.Badge variant="secondary">{call.status === 'stopped' ? 'Ended' : 'Interrupted'}</Badge.Badge>{/if}</span><span>Started {relativeDate(call.started)}</span><span>· {showDuration(call.started, call.status==='live' ? undefined : call.ended)} elapsed</span><span>Host {call.host.name}</span></div></div>
		<div class="detail-actions"><Button.Root variant="outline" onclick={() => void copyLink()}><Copy size={15}/>Copy link</Button.Root><Button.Root variant="outline" onclick={() => shareOpen=true}><Share2 size={15}/> Share</Button.Root><div class="download-menu"><Button.Root variant="outline" onclick={() => downloadsOpen = !downloadsOpen}><Download size={15}/> Download</Button.Root><div id="download-links" class="download-pop" class:show={downloadsOpen}><a href={`/api/calls/${encodeURIComponent(id)}/transcript?format=txt`}>Plain text <span>.txt</span></a><a href={`/api/calls/${encodeURIComponent(id)}/transcript?format=vtt`}>Subtitles (WebVTT) <span>.vtt</span></a><a href={`/api/calls/${encodeURIComponent(id)}/transcript?format=json`}>Structured events <span>.json</span></a></div></div>
		</div>
	</div>
	<div class="call-stats"><div class="panel stat"><span>Duration</span><b>{showDuration(call.started, call.status==='live' ? undefined : call.ended)}</b></div><div class="panel stat"><span>Participants</span><b>{call.participants.length}</b></div><div class="panel stat"><span>Transcript lines</span><b>{events.filter(e=>e.kind==='transcript').length}</b></div><div class="panel stat"><span>Chat messages</span><b>{events.filter(e=>e.kind==='chat').length}</b></div></div>
	{#key id}<ScreenShare callId={id} {events}/>{/key}
	<div class="call-layout">
		<Card.Root class="transcript-panel">
			<div class="transcript-toolbar"><div class="tabs-row"><button class:active={tab==='transcript'} onclick={() => tab='transcript'}>Transcript</button><button class:active={tab==='chat'} onclick={() => tab='chat'}>Chat <b>{events.filter(e=>e.kind==='chat').length}</b></button></div><div class="search-wrap"><Search size={15}/><input class="search-input" aria-label="Search transcript" placeholder="Search transcript" bind:value={query}/></div></div>
			<div class="transcript-body" bind:this={transcriptElement} onscroll={handleTranscriptScroll}>
				{#if shownEvents.length === 0}<div class="empty-state"><h2>{query ? 'No matches' : tab==='chat' ? 'No chat messages' : 'No transcript yet'}</h2><p>{query ? 'Try a different search.' : call.status==='live' ? 'Transcript updates will appear here.' : 'This call has no saved transcript.'}</p></div>
				{:else}{#each shownEvents as event (event.seq)}
					{#if event.kind==='transcript'}<div class="turn"><span class="avatar">{initials(event.participant?.name)}</span><div><div class="turn-head"><b>{event.participant?.name || 'Unknown speaker'}</b><a href={'#t=' + Math.floor(event.ts/1000)} title="Copy timestamp link">{new Date(event.ts).toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'})}</a></div><p class:partial={event.data.final===false}>{event.data.text}{#if event.data.final===false}<span class="caret"></span>{/if}</p></div></div>
					{:else if event.kind==='chat'}<div class="chat-line"><span class="chat-label"><MessageSquare size={12}/> Chat · {event.participant?.name || 'Participant'}</span><span>{event.data.text}</span><small>{new Date(event.ts).toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'})}</small></div>
					{:else}<div class="sys-line">{event.kind.replaceAll('.', ' ')}</div>{/if}
				{/each}{/if}
			</div>
			{#if call.status==='live' || streamState==='ended'}<div class="follow-state"><span class:live-dot={call.status==='live'}></span>{#if call.status!=='live'}{call.status==='interrupted'?'Call interrupted':'Call ended'}{:else if !following}<span>Paused while you read</span><Button.Root variant="ghost" size="sm" onclick={jumpToLive}>Jump to live</Button.Root>{:else}{streamState === 'live' ? 'Following live' : streamState === 'reconnecting' ? 'Reconnecting…' : 'Connecting…'}{/if}</div>{/if}
		</Card.Root>
		<aside class="detail-side">
			<Card.Root class="panel-pad"><h2>In this call · {call.participants.length}</h2><div class="people-list">{#each call.participants as person}<div class="person-row"><span class="avatar">{initials(person.name)}</span><span><b>{person.name}</b><small>{person.email || 'Participant'}</small></span>{#if person.id===call.host.id}<span class="badge-muted">Host</span>{/if}</div>{/each}</div></Card.Root>
			<Card.Root class="panel-pad"><h2>Agent access</h2><p class="muted small">Stream this call with one of your API keys.</p><pre class="code">curl -N -H "Authorization: Bearer $ZC_KEY" {pageOrigin}/api/calls/{call.id}/stream</pre><a class="text-link" href="/keys">Manage keys →</a></Card.Root>
		</aside>
	</div>
{/if}

<SharingDialog bind:open={shareOpen} callId={id} topic={call?.topic || 'Call'} hostId={call?.host.id || ''} />

<style>
	.back-link { display:inline-block; color:var(--muted-foreground); font-size:12px; margin-bottom:8px; }
	.call-sub { display:flex; gap:10px; flex-wrap:wrap; align-items:center; margin-top:8px; color:var(--muted-foreground); font-size:12px; }
	.detail-actions { display:flex; gap:8px; flex-wrap:wrap; }
	.download-menu { position:relative; }
	.download-pop { display:none; position:absolute; right:0; top:42px; z-index:10; width:220px; padding:5px; background:var(--popover); border:1px solid var(--border); border-radius:9px; box-shadow:0 8px 24px #0002; }
	.download-pop.show { display:grid; }
	.download-pop a { padding:8px; border-radius:6px; display:flex; justify-content:space-between; }
	.download-pop a:hover { background:var(--muted); }
	.download-pop span { color:var(--muted-foreground); font:12px ui-monospace,monospace; }
	.call-stats { display:grid; grid-template-columns:repeat(4,minmax(0,1fr)); gap:10px; margin-bottom:14px; }
	.stat { padding:12px 14px; }
	.stat span { color:var(--muted-foreground); font-size:12px; display:block; }
	.stat b { font-size:20px; }
	.call-layout { display:grid; grid-template-columns:minmax(0,1fr) 300px; gap:14px; align-items:start; }
	:global(.transcript-panel) { padding:0; min-width:0; }
	.transcript-toolbar { padding:12px; border-bottom:1px solid var(--border); display:flex; justify-content:space-between; align-items:center; gap:12px; flex-wrap:wrap; }
	.search-wrap { width:min(100%,240px); position:relative; display:flex; align-items:center; }
	.search-wrap :global(svg) { position:absolute; left:10px; color:var(--muted-foreground); }
	.search-wrap input { padding-left:32px; }
	.transcript-body { max-height:70vh; overflow:auto; padding:8px 4px; }
	.turn { display:grid; grid-template-columns:30px minmax(0,1fr); gap:10px; padding:10px 12px; }
	.turn-head { display:flex; justify-content:space-between; gap:10px; align-items:baseline; }
	.turn-head a { font:11px ui-monospace,monospace; color:var(--muted-foreground); }
	.turn p { margin:3px 0 0; }
	.turn p.partial { color:var(--muted-foreground); font-style:italic; }
	.caret { display:inline-block; width:6px; height:14px; background:var(--muted-foreground); margin-left:3px; animation:pulse 1s infinite; vertical-align:middle; }
	@keyframes pulse { 50% { opacity:.2; } }
	.chat-line { margin:8px 12px 8px 52px; padding:9px 11px; border-radius:9px; background:var(--muted); display:grid; gap:4px; }
	.chat-label { color:var(--muted-foreground); font-size:11px; font-weight:600; display:flex; align-items:center; gap:5px; }
	.chat-line small { color:var(--muted-foreground); font:11px ui-monospace,monospace; }
	.sys-line { margin:8px 12px; text-align:center; color:var(--muted-foreground); font-size:11px; }
	.follow-state { border-top:1px solid var(--border); padding:10px 14px; color:var(--muted-foreground); font-size:12px; display:flex; gap:8px; align-items:center; }
	.detail-side { display:grid; gap:12px; }
	.detail-side h2 { margin:0 0 12px; font-size:14px; }
	.people-list { display:grid; gap:8px; }
	.person-row { display:flex; gap:9px; align-items:center; min-width:0; }
	.person-row > span:nth-child(2) { flex:1; min-width:0; display:grid; }
	.person-row b { font-size:12px; font-weight:550; }
	.person-row small { color:var(--muted-foreground); font-size:11px; overflow:hidden; text-overflow:ellipsis; }
	.text-link { display:inline-block; margin-top:10px; font-size:12px; font-weight:550; }
	.no-access-people { display:grid; gap:7px; margin:18px auto; width:max-content; text-align:left; }
	.no-access-people div { display:flex; align-items:center; gap:8px; }
	@media(max-width:1000px){.call-layout{grid-template-columns:1fr}.detail-side{grid-template-columns:repeat(2,minmax(0,1fr))}}
	@media(max-width:640px){.call-stats{grid-template-columns:repeat(2,minmax(0,1fr))}.detail-side{grid-template-columns:1fr}.detail-actions{width:100%}.detail-actions :global(button){flex:1}.transcript-body{max-height:none}}
</style>
