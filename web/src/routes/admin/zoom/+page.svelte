<script lang="ts">
	import { onMount } from 'svelte';
	import { RefreshCw, ExternalLink, Radio, Check, Play, Square } from '@lucide/svelte';
	import * as Button from '#lib/components/ui/button';
	import * as Card from '#lib/components/ui/card';
	import * as Badge from '#lib/components/ui/badge';
	import * as Switch from '#lib/components/ui/switch';
	import * as Alert from '#lib/components/ui/alert';
	import { api, APIError, errorMessage, relativeDate } from '#lib/api';
	import type { ZoomSettings, Call } from '#lib/types';
	let settings = $state<ZoomSettings | null>(null); let loading = $state(true); let saving = $state(false); let error = $state(''); let notice = $state('');
	let clientId = $state(''); let clientSecret = $state(''); let transcript = $state(true); let chat = $state(true); let demoCall = $state<Call | null>(null);
	onMount(() => { void load(); });
	async function load() {
		loading=true; error='';
		try { settings=await api<ZoomSettings>('/api/admin/zoom'); clientId=settings.credentials.client_id||''; transcript=settings.capture.transcript; chat=settings.capture.chat; }
		catch(e) { if(e instanceof APIError && e.status===404){error='This page is only available to Zoom Connect admins.';} else error=errorMessage(e); }
		finally { loading=false; }
	}
	async function save() {
		saving=true; error=''; notice='';
		try { settings=await api<ZoomSettings>('/api/admin/zoom',{method:'POST',body:JSON.stringify({client_id:clientId,client_secret:clientSecret||undefined,capture:{transcript,chat}})}); clientSecret=''; notice='Settings saved'; }
		catch(e) { error=errorMessage(e); }
		finally { saving=false; }
	}
	async function startDemo() {
		error='';
		try { demoCall=await api<Call>('/api/demo/calls',{method:'POST',body:JSON.stringify({})}); }
		catch(e){error=errorMessage(e);}
	}
	async function stopDemo() {
		if(!demoCall)return;
		try { demoCall=await api<Call>(`/api/demo/calls/${encodeURIComponent(demoCall.id)}/stop`,{method:'POST'}); notice='Simulated call stopped'; }
		catch(e){error=errorMessage(e);}
	}
	const webhookTime = (value?:string) => value ? relativeDate(value) : 'No webhook received';
</script>

<svelte:head><title>Zoom connection · Zoom Connect</title></svelte:head>
<div class="page-head"><div><h1>Zoom connection</h1><p>Configure Zoom RTMS credentials and review webhook activity.</p></div><Button.Root variant="outline" onclick={load} disabled={loading}><RefreshCw size={15}/> Re-check</Button.Root></div>
{#if loading}<div class="panel empty-state"><span class="spinner"></span><p>Loading Zoom settings…</p></div>
{:else if error && !settings}<div class="panel empty-state"><h2>Zoom settings unavailable</h2><p>{error}</p><a class="sign-in-link" href="/calls">Back to calls</a></div>
{:else if settings}
	<div class="demo-banner"><Badge.Badge variant="outline">Local demo</Badge.Badge><span>This prototype does not have a live Zoom connection. Status comes from the server.</span></div>
	<div class="health-grid"><div class="panel health-cell"><span>Status</span><b><span class="status-dot"></span>{settings.connected?'Connected':'Not connected'}</b></div><div class="panel health-cell"><span>Last webhook</span><b>{webhookTime(settings.last_webhook)}</b></div><div class="panel health-cell"><span>Live streams</span><b>{settings.live_streams}</b></div><div class="panel health-cell"><span>Calls this week</span><b>{settings.calls_this_week}</b></div></div>
	{#if error}<Alert.Root variant="destructive" class="mb-4"><Alert.Title>Request failed</Alert.Title><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}
	<div class="admin-columns"><div class="column">
		<Card.Root class="panel-pad"><h2>Setup checklist</h2><p class="muted small">Configure a Zoom General App and paste the credentials below.</p>
			<div class="step"><span class="step-num">1</span><div><b>Create a General App</b><small>Zoom Marketplace → Develop → Build App → General App</small><a href="https://marketplace.zoom.us/" target="_blank" rel="noreferrer">Open Zoom Marketplace <ExternalLink size={12}/></a></div></div>
			<div class="step"><span class="step-num">2</span><div><b>Set the RTMS webhook URL</b><small class="mono">{settings.webhook_url}</small><small>Subscribe to meeting.rtms_started, meeting.rtms_stopped, and meeting.rtms_interrupted.</small></div></div>
			<div class="step"><span class="step-num">3</span><div><b>Add transcript and chat scopes</b><small>meeting:read:meeting_transcript · meeting:read:meeting_chat</small></div></div>
			<div class="step"><span class="step-num">4</span><div><b>Turn on realtime meeting content</b><small>Zoom Admin → Account Settings → Zoom Apps. This server cannot verify the account-level setting.</small></div></div>
		</Card.Root>
		<Card.Root class="panel-pad"><div class="row-between"><div><h2>Recent webhooks</h2><p class="muted small">Events reported to this server.</p></div><Radio size={17}/></div>
			{#if settings.webhooks?.length}<div class="webhook-list">{#each settings.webhooks ?? [] as hook}<div class="webhook-row"><time>{relativeDate(hook.at)}</time><span><b>{hook.event}</b><small>{hook.topic||'No meeting topic'}</small></span><Badge.Badge variant="outline">{hook.outcome}</Badge.Badge></div>{/each}</div>{:else}<p class="empty-inline">No webhook events have been received.</p>{/if}
		</Card.Root>
	</div><div class="column">
		<Card.Root class="panel-pad"><h2>App credentials</h2><p class="muted small">Secrets are write-only. Saved values are only shown by their last four characters.</p>
			<div class="form-grid"><div class="field"><label for="client-id">Client ID</label><input id="client-id" bind:value={clientId} autocomplete="off" placeholder="Zoom app client ID"/></div><div class="field"><label for="client-secret">Client secret</label><input id="client-secret" type="password" bind:value={clientSecret} autocomplete="new-password" placeholder={settings.credentials.client_secret_suffix ? `Saved ••••${settings.credentials.client_secret_suffix} · enter to replace` : 'Enter client secret'}/></div><div class="field"><span class="field-label">Webhook secret</span><div class="readonly-secret">{settings.credentials.webhook_secret_configured?'Set by server environment':'Not configured on server'}</div><small class="muted">ZOOM_WEBHOOK_SECRET is managed by the server environment.</small></div></div>
			<div class="capture-box"><h3>What to capture</h3><label><Switch.Root bind:checked={transcript}/> <span><b>Transcript</b><small>Zoom’s live transcription</small></span></label><label><Switch.Root bind:checked={chat}/> <span><b>Chat</b><small>Public “Everyone” chat only</small></span></label><div class="deferred-row"><span>Audio and shared media</span><Badge.Badge variant="secondary">Deferred</Badge.Badge></div></div>
			<Button.Root disabled={saving} onclick={save}>{saving?'Saving…':'Save credentials'}</Button.Root>
		</Card.Root>
		<Card.Root class="panel-pad demo-controls"><h2>Demo controls</h2><p class="muted small">These controls create simulated calls for local testing. They do not contact Zoom.</p>
			{#if demoCall}<div class="demo-call"><span><b>{demoCall.topic}</b><small>Simulated {demoCall.status} call</small></span><a href={'/calls/'+demoCall.id}>Open call →</a>{#if demoCall.status==='live'}<Button.Root variant="outline" size="sm" onclick={stopDemo}><Square size={13}/> Stop</Button.Root>{/if}</div>{:else}<Button.Root variant="outline" onclick={startDemo}><Play size={14}/> Start simulated call</Button.Root>{/if}
		</Card.Root>
	</div></div>
	{#if notice}<div class="save-notice"><Check size={14}/>{notice}</div>{/if}
{/if}

<style>
	.demo-banner{display:flex;align-items:center;gap:10px;padding:10px 12px;margin-bottom:14px;background:var(--muted);border-radius:9px;color:var(--muted-foreground);font-size:12px}
	.health-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px;margin-bottom:14px}
	.health-cell{padding:12px 14px}.health-cell span{display:block;color:var(--muted-foreground);font-size:12px}.health-cell b{display:flex;gap:7px;align-items:center;font-weight:600;margin-top:3px}
	.status-dot{width:8px;height:8px;border-radius:50%;background:var(--muted-foreground)}
	.admin-columns{display:grid;grid-template-columns:minmax(0,1.05fr) minmax(0,1fr);gap:14px;align-items:start}.column{display:grid;gap:14px;min-width:0}
	.column h2{margin:0;font-size:15px}.column p.muted{margin:4px 0 14px}
	.step{display:grid;grid-template-columns:24px minmax(0,1fr);gap:10px;padding:10px 0;border-top:1px solid var(--border)}
	.step-num{width:21px;height:21px;display:grid;place-items:center;border-radius:50%;background:var(--muted);font-size:11px;font-weight:600}
	.step div{display:grid;gap:3px}.step b{font-size:13px;font-weight:550}.step small{color:var(--muted-foreground);font-size:11px}.step a{display:flex;gap:4px;align-items:center;font-size:11px;font-weight:550}
	.webhook-list{display:grid}.webhook-row{display:flex;gap:10px;align-items:center;padding:9px 0;border-top:1px solid var(--border);font-size:12px}.webhook-row time{font:10px ui-monospace,monospace;color:var(--muted-foreground);white-space:nowrap}.webhook-row span{flex:1;min-width:0;display:grid}.webhook-row small{color:var(--muted-foreground)}.empty-inline{color:var(--muted-foreground);font-size:12px}
	.readonly-secret{padding:9px 11px;border:1px solid var(--border);border-radius:8px;background:var(--muted);font-size:13px}
	.capture-box{border-top:1px solid var(--border);border-bottom:1px solid var(--border);padding:12px 0;display:grid;gap:10px}.capture-box h3{margin:0;font-size:13px}.capture-box label{display:flex;gap:10px;align-items:center}.capture-box label span{display:grid}.capture-box label b{font-size:12px;font-weight:550}.capture-box label small{font-size:11px;color:var(--muted-foreground)}.deferred-row{display:flex;justify-content:space-between;color:var(--muted-foreground);font-size:12px}
	.demo-call{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.demo-call span{display:grid;flex:1}.demo-call small{color:var(--muted-foreground);font-size:11px}.demo-call a{font-size:12px;font-weight:550}.save-notice{position:fixed;right:20px;bottom:20px;padding:10px 14px;background:var(--foreground);color:var(--background);border-radius:9px;display:flex;gap:8px;align-items:center}
	@media(max-width:1000px){.health-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.admin-columns{grid-template-columns:1fr}}
</style>
