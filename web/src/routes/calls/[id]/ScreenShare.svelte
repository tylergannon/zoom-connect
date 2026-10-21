<script lang="ts">
 import { Image, RefreshCw } from '@lucide/svelte';
 import * as Card from '#lib/components/ui/card';
 import * as Button from '#lib/components/ui/button';
 import { api, errorMessage } from '#lib/api';
 import type { EventItem } from '#lib/types';
 let { callId, events }: { callId: string; events: EventItem[] } = $props();
 let frames = $derived(events.filter(e => e.kind === 'screen.frame'));
 let selected = $state<number | null>(null);
 let current = $derived(frames.find(e => e.seq === selected) ?? frames.at(-1));
 let renewed = $state<Record<number,string>>({});
 let error = $state('');
 let currentURL = $derived(current ? renewed[current.seq] || String(current.data.url || '') : '');
 const time = (ts: number) => new Date(ts).toLocaleTimeString([], {hour:'2-digit',minute:'2-digit',second:'2-digit'});
 async function refresh(seq: number) {
  error = '';
  try { const data = await api<{url:string}>(`/api/calls/${encodeURIComponent(callId)}/screenshots/${seq}`); renewed = {...renewed,[seq]:data.url}; }
  catch (e) { error = errorMessage(e); }
 }
</script>
<Card.Root class="screen-panel">
 <div class="screen-head"><h2><Image size={17}/> Shared screen</h2><span class="muted small">{frames.length ? `${frames.length} captured images · 1 per second` : '1 image per second while sharing'}</span></div>
 {#if current}
  <a class="screen-preview" href={currentURL} target="_blank" rel="noreferrer"><img src={currentURL} alt={`Shared screen from ${current.participant?.name || 'presenter'} at ${time(current.ts)}`} onerror={() => error = 'Image link expired or unavailable. Refresh to try again.'}/></a>
  <div class="screen-caption"><span>{current.participant?.name || 'Presenter'} · {time(current.ts)}</span><Button.Root variant="ghost" size="sm" onclick={() => {selected = null; error = '';}}>Latest image</Button.Root><Button.Root variant="ghost" size="sm" onclick={() => void refresh(current!.seq)}><RefreshCw size={13}/> Refresh link</Button.Root></div>
  {#if error}<p class="screen-error" role="status">{error}</p>{/if}
  <div class="screen-thumbnails" aria-label="Captured shared screen images">
   {#each frames.slice(-120) as frame (frame.seq)}
    <button class:chosen={frame.seq === current.seq} onclick={() => {selected = frame.seq; error = '';}} aria-label={`View shared screen at ${time(frame.ts)}`}><img loading="lazy" src={renewed[frame.seq] || String(frame.data.url || '')} alt={`Screen at ${time(frame.ts)}`}/><small>{time(frame.ts)}</small></button>
   {/each}
  </div>
 {:else}<div class="screen-empty"><Image size={30}/><p>Shared screen images will appear here.</p><span class="muted small">Images are saved alongside the meeting transcript.</span></div>{/if}
</Card.Root>
<style>
 :global(.screen-panel){overflow:hidden;margin-bottom:1rem}
 .screen-head{display:flex;align-items:center;justify-content:space-between;gap:1rem;padding:1rem 1.25rem;border-bottom:1px solid var(--border)}
 h2{display:flex;gap:.5rem;align-items:center;font-size:.95rem;font-weight:600;margin:0}
 .screen-empty{min-height:160px;display:flex;flex-direction:column;align-items:center;justify-content:center;padding:1rem;gap:.5rem;color:var(--muted-foreground)}
 p{margin:0}.screen-preview{display:block;background:#15171b;text-align:center;min-height:100px}
 .screen-preview img{display:block;max-height:48vh;width:100%;object-fit:contain}
 .screen-caption{display:flex;align-items:center;gap:.5rem;padding:.5rem 1rem;font-size:.8rem}.screen-caption span{margin-right:auto}
 .screen-thumbnails{display:flex;gap:.5rem;overflow-x:auto;padding:.5rem 1rem 1rem}
 .screen-thumbnails button{flex:0 0 120px;border:2px solid transparent;padding:0;background:var(--muted);border-radius:6px;overflow:hidden;cursor:pointer;color:inherit}
 .screen-thumbnails button.chosen{border-color:var(--primary)}
 .screen-thumbnails img{width:116px;height:65px;object-fit:contain;background:#15171b}.screen-thumbnails small{display:block;padding:3px;font-size:.65rem}
 .screen-error{padding:.5rem 1rem;color:var(--destructive);font-size:.85rem}
 @media(max-width:600px){.screen-head{align-items:flex-start;flex-direction:column;gap:.3rem}.screen-caption{flex-wrap:wrap}}
</style>
