<script lang="ts">
 import { untrack } from 'svelte';
 import { PenTool, Send } from '@lucide/svelte';
 import * as Card from '#lib/components/ui/card';
 import * as Button from '#lib/components/ui/button';
 import { api, errorMessage } from '#lib/api';
 import type { BoardDraft, EventItem, WhiteboardStatus } from '#lib/types';
 let { callId, events }: { callId: string; events: EventItem[] } = $props();
 let status = $state<WhiteboardStatus | null>(null);
 let error = $state('');
 let busy = $state(false);
 let boardEvents = $derived(events.filter(e => e.kind.startsWith('board.')).length);
 let draft = $derived(status?.latest.draft ? status.latest.draft.data as unknown as BoardDraft : null);
 let published = $derived(status?.latest.published && status.latest.published.data.draft_seq === status.latest.draft?.seq ? status.latest.published : null);
 let failed = $derived(status?.latest.failed && status.latest.failed.data.draft_seq === status.latest.draft?.seq ? status.latest.failed : null);
 let width = $derived(draft ? Math.max(...draft.cards.map(c => c.x + c.w)) + 40 : 0);
 let height = $derived(draft ? Math.max(...draft.cards.map(c => c.y + c.h)) + 40 : 0);
 const laneX = (i: number) => 40 + i * 288 + 12;
 const center = (i: number) => draft ? [draft.cards[i].x + draft.cards[i].w / 2, draft.cards[i].y + draft.cards[i].h / 2] : [0, 0];
 async function load() {
  try { status = await api<WhiteboardStatus>(`/api/calls/${encodeURIComponent(callId)}/whiteboard`); error = ''; }
  catch (e) { error = errorMessage(e); }
 }
 async function publish() {
  if (!status?.latest.draft) return;
  busy = true; error = '';
  try { await api(`/api/calls/${encodeURIComponent(callId)}/whiteboard/publish`, { method: 'POST', body: JSON.stringify({ draft_seq: status.latest.draft.seq }) }); await load(); }
  catch (e) { error = errorMessage(e); await load(); }
  finally { busy = false; }
 }
 $effect(() => { void boardEvents; untrack(() => void load()); });
</script>
<Card.Root class="board-panel">
 <div class="board-head"><h2><PenTool size={17}/> Zoom Whiteboard draft</h2>{#if status}<span class="board-mode" class:native={status.mode === 'native'}>{status.mode === 'native' ? 'Native publishing on' : 'Local preview only'}</span>{/if}</div>
 {#if error}<p class="board-error" role="status">{error}</p>{/if}
 {#if draft && status?.latest.draft}
  <div class="board-meta"><b>{draft.title}</b><span class="muted small">{status.concepts.find(c => c.id === draft.concept)?.name || draft.concept} · {draft.cards.length} cards{draft.agent ? ` · drafted by agent “${draft.agent}”` : ''} · event #{status.latest.draft.seq}</span></div>
  <div class="board-canvas"><svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label={`Board preview: ${draft.title}`}>
   {#each draft.lanes ?? [] as lane, i (lane)}<rect class="lane" x={laneX(i)} y="40" width="264" height={height - 60} rx="8"/><text class="lane-name" x={laneX(i) + 12} y="76">{lane}</text>{/each}
   {#each draft.links ?? [] as link, i (i)}<line class="link" x1={center(link.from)[0]} y1={center(link.from)[1]} x2={center(link.to)[0]} y2={center(link.to)[1]}/>{/each}
   {#each draft.cards as card, i (i)}<rect class="card" x={card.x} y={card.y} width={card.w} height={card.h} rx="6"/><foreignObject x={card.x} y={card.y} width={card.w} height={card.h}><div class="card-text">{card.text}{#if card.source_seq}<small>from event #{card.source_seq}</small>{/if}</div></foreignObject>{/each}
  </svg></div>
  <p class="board-note muted small">Only the rectangles' positions go to Zoom. {status.native.not_sent_reason} Not sent: {status.latest.not_sent?.join(', ')}.</p>
  <details class="board-requests"><summary>Native Zoom requests for this draft</summary><pre>{JSON.stringify(status.latest.native_requests, null, 2)}</pre></details>
  {#if published}<div class="board-result"><b>Saved to Zoom Whiteboard {String(published.data.whiteboard_id)}</b><span>{published.data.rects_read_back} of {published.data.rects_sent} rectangles read back. It is not shown in the meeting until the host opens it: {status.meeting.open}</span></div>
  {:else if failed}<div class="board-result failed"><b>Zoom rejected the {String((failed.data.zoom as {stage?:string})?.stage || 'publish')} step</b><span>Board {String(failed.data.whiteboard_id)} was created and may need removing in Zoom.</span></div>
  {:else if status.can_publish}<div class="board-actions"><Button.Root size="sm" disabled={busy} onclick={() => void publish()}><Send size={13}/> {busy ? 'Publishing…' : 'Publish to Zoom Whiteboard'}</Button.Root><span class="muted small">Creates a new board in your Zoom account.</span></div>
  {:else if status.mode === 'preview'}<p class="board-note muted small">Publishing is off. Missing: {status.native.missing.join('; ')}.</p>{/if}
  {#if status.latest.opened}<div class="board-result"><b>Opened in a Zoom meeting</b><span>Zoom reported board {String(status.latest.opened.data.whiteboard_id)} open in meeting {String(status.latest.opened.data.meeting_id || status.latest.opened.data.meeting_uuid)}.</span></div>{/if}
 {:else if status}<div class="board-empty"><PenTool size={30}/><p>No board yet.</p><span class="muted small">Agents draft one with POST /api/v1/calls/{callId}/whiteboard.{status.can_draft ? '' : ' You can view boards on this call but not draft them.'}</span></div>{/if}
</Card.Root>
<style>
 :global(.board-panel){overflow:hidden;margin-bottom:1rem}
 .board-head{display:flex;align-items:center;justify-content:space-between;gap:1rem;padding:1rem 1.25rem;border-bottom:1px solid var(--border)}
 h2{display:flex;gap:.5rem;align-items:center;font-size:.95rem;font-weight:600;margin:0}
 p{margin:0}.board-mode{font-size:.75rem;padding:.15rem .5rem;border-radius:999px;background:var(--muted);color:var(--muted-foreground)}.board-mode.native{background:var(--primary);color:var(--primary-foreground)}
 .board-meta{display:flex;flex-direction:column;gap:.15rem;padding:.75rem 1.25rem 0}
 .board-canvas{padding:.75rem 1.25rem;overflow-x:auto}.board-canvas svg{display:block;width:100%;min-width:480px;height:auto;background:var(--muted);border-radius:8px}
 .lane{fill:var(--background);opacity:.7}.lane-name{font-size:18px;font-weight:600;fill:currentColor}.link{stroke:var(--muted-foreground);stroke-width:3}
 .card{fill:#fff7c2;stroke:#d8c45a}.card-text{height:100%;box-sizing:border-box;padding:10px;font-size:15px;line-height:1.3;color:#1f1f1f;overflow:hidden}.card-text small{display:block;margin-top:6px;font-size:12px;color:#5d5d5d}
 .board-note{padding:0 1.25rem .5rem}.board-requests{padding:0 1.25rem .75rem;font-size:.8rem}.board-requests pre{max-height:16rem;overflow:auto;background:var(--muted);padding:.5rem;border-radius:6px}
 .board-actions{display:flex;align-items:center;gap:.75rem;padding:0 1.25rem 1rem}
 .board-result{display:flex;flex-direction:column;gap:.2rem;margin:0 1.25rem 1rem;padding:.6rem .75rem;border-radius:6px;background:var(--muted);font-size:.85rem}.board-result.failed{color:var(--destructive)}
 .board-empty{min-height:140px;display:flex;flex-direction:column;align-items:center;justify-content:center;padding:1rem;gap:.5rem;color:var(--muted-foreground);text-align:center}
 .board-error{padding:.5rem 1.25rem;color:var(--destructive);font-size:.85rem}
 @media(max-width:600px){.board-head{align-items:flex-start;flex-direction:column;gap:.3rem}}
</style>
