<script lang="ts">
	import { onMount } from 'svelte';
	import { Copy, Check, ExternalLink, KeyRound } from '@lucide/svelte';
	import * as Button from '#lib/components/ui/button';
	import * as Card from '#lib/components/ui/card';
	import * as Badge from '#lib/components/ui/badge';
	import { api, errorMessage } from '#lib/api';
	import type { APIKey } from '#lib/types';
	let keys = $state<APIKey[]>([]); let error = $state(''); let copied = $state('');
	let origin = $state('');
	onMount(async () => { origin=window.location.origin; try { keys = await api<APIKey[]>('/api/keys'); } catch(e) { error=errorMessage(e); } });
	async function copy(key: string, text: string) { try { await navigator.clipboard.writeText(text); copied=key; setTimeout(()=>copied='',1500); } catch { error='Clipboard access failed.'; } }
	const streamCmd = $derived(`curl -N -H "Authorization: Bearer $ZC_KEY" ${origin}/api/stream`);
	const callCmd = $derived(`curl -N -H "Authorization: Bearer $ZC_KEY" ${origin}/api/calls/CALL_ID/stream`);
	const downloadCmd = $derived(`curl -H "Authorization: Bearer $ZC_KEY" "${origin}/api/calls/CALL_ID/transcript?format=json"`);
</script>

<svelte:head><title>Agent API · Zoom Connect</title></svelte:head>
<div class="page-head"><div><h1>Agent API</h1><p>Give an agent an API key and use these URLs to stream or download call data.</p></div><a href="/keys" class="text-link">Manage keys →</a></div>
<Card.Root class="key-note panel-pad"><KeyRound size={18}/><span>Secrets are shown only once. Set <code>ZC_KEY</code> in your agent’s environment.</span>{#if keys.length}<Badge.Badge variant="outline">{keys.length} key{keys.length===1?'':'s'} available</Badge.Badge>{/if}</Card.Root>
{#if error}<p class="error-text">{error}</p>{/if}
<div class="guide-grid"><article class="guide-main">
	<section id="all-calls"><h2>1. Hear about every call</h2><p>A long-lived Server-Sent Events connection reports calls you can access when they start, are shared with you, or end.</p>{@render CodeBlock('all', streamCmd, copied==='all', () => copy('all',streamCmd))}</section>
	<section id="follow-call"><h2>2. Follow one call</h2><p>Connect before a scheduled call to wait, or replay saved events and continue live. Each message has an event sequence ID; reconnect with <code>Last-Event-ID</code> or <code>?from=</code>. The same URL remains valid as a scheduled call starts. Temporary Zoom interruptions emit <code>stream.paused</code> and keep clients connected for up to two minutes. The stream ends with an <code>end</code> event after the call stops, recovery expires, or it is cancelled. A session may later resume at the same URL; reconnect with <code>Last-Event-ID</code>.</p>{@render CodeBlock('call', callCmd, copied==='call', () => copy('call',callCmd))}<pre class="code">Example event: id 42 · transcript · sequence 42 · text: “Let’s ship it Monday.” · language: en</pre></section>
	<section id="download"><h2>3. Download a call</h2><p>All routes require the same bearer key. A key acts as its owner and only returns calls that owner can see.</p>
		<div class="endpoint-list"><div><Badge.Badge variant="outline">POST</Badge.Badge><code>/api/calls</code><span>Admin: add a scheduled call from a Zoom invitation; returns a streamable call ID</span></div><div><Badge.Badge variant="outline">GET</Badge.Badge><code>/api/calls</code><span>Accessible calls, newest first</span></div><div><Badge.Badge variant="outline">GET</Badge.Badge><code>/api/calls/{'{id}'}</code><span>Call metadata</span></div><div><Badge.Badge variant="outline">GET</Badge.Badge><code>/api/calls/{'{id}'}/events</code><span>Structured event list</span></div><div><Badge.Badge variant="outline">GET</Badge.Badge><code>/api/calls/{'{id}'}/transcript?format=txt|vtt|json</code><span>Transcript download</span></div><div><Badge.Badge variant="outline">GET</Badge.Badge><code>/api/calls/{'{id}'}/stream</code><span>Call events over SSE</span></div></div>
		{@render CodeBlock('download', downloadCmd, copied==='download', () => copy('download',downloadCmd))}
	</section>
	<section><h2>Errors</h2><p><code>401</code> missing or revoked key · <code>403</code> the key owner cannot see the call · <code>404</code> no such call.</p></section>
</article><aside class="guide-toc"><b>On this page</b><a href="#all-calls">Hear about every call</a><a href="#follow-call">Follow one call</a><a href="#download">Download a call</a><a href="/keys">Create an API key <ExternalLink size={13}/></a></aside></div>

{#snippet CodeBlock(id: string, code: string, copied: boolean, oncopy: () => void)}
	<div class="code-wrap"><pre class="code">{code}</pre><Button.Root class="copy-code" variant="outline" size="sm" onclick={oncopy}>{#if copied}<Check size={14}/> Copied{:else}<Copy size={14}/> Copy{/if}</Button.Root></div>
{/snippet}

<style>
	:global(.key-note) { display:flex; gap:12px; align-items:center; margin-bottom:20px; }
	:global(.key-note > span) { flex:1; }
	.guide-grid { display:grid; grid-template-columns:minmax(0,1fr) 190px; gap:28px; }
	.guide-main { min-width:0; }
	.guide-main section { margin-bottom:26px; scroll-margin-top:18px; }
	.guide-main h2 { margin:0 0 6px; font-size:16px; }
	.guide-main p { color:var(--muted-foreground); margin:0 0 12px; }
	.guide-main code, :global(.key-note code) { font-family:ui-monospace,monospace; font-size:.92em; background:var(--muted); padding:2px 4px; border-radius:4px; }
	.code-wrap { position:relative; margin:10px 0; }
	:global(.copy-code) { position:absolute; right:8px; top:8px; }
	.code { padding-right:100px; }
	.endpoint-list { border:1px solid var(--border); border-radius:9px; overflow:auto; margin:10px 0 14px; }
	.endpoint-list div { display:grid; grid-template-columns:52px minmax(0,1.3fr) minmax(120px,1fr); align-items:center; gap:8px; padding:10px; border-bottom:1px solid var(--border); }
	.endpoint-list div:last-child { border:0; }
	.endpoint-list code { overflow-wrap:anywhere; }
	.endpoint-list span { color:var(--muted-foreground); font-size:12px; }
	.guide-toc { display:grid; align-content:start; gap:9px; border-left:1px solid var(--border); padding-left:14px; font-size:12px; color:var(--muted-foreground); height:max-content; }
	.guide-toc b { color:var(--foreground); }
	.guide-toc a { display:flex; gap:4px; align-items:center; }
	@media(max-width:1000px){.guide-grid{grid-template-columns:1fr}.guide-toc{display:none}}
	@media(max-width:640px){.endpoint-list div{grid-template-columns:50px 1fr}.endpoint-list span{grid-column:2}:global(.key-note){flex-wrap:wrap}}
</style>
