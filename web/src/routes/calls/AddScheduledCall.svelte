<script lang="ts">
 import { onMount } from 'svelte';
 import { goto } from '$app/navigation';
 import { CalendarPlus } from '@lucide/svelte';
 import * as Button from '#lib/components/ui/button';
 import * as Dialog from '#lib/components/ui/dialog';
 import { api, errorMessage } from '#lib/api';
 import type { Call, User } from '#lib/types';
 let open = $state(false);
 let canSchedule = $state(false);
 onMount(() => { void api<User>('/api/me').then(user => canSchedule=user.is_admin).catch(() => {}); });
 let invitation = $state('');
 let topic = $state('');
 let scheduledAt = $state('');
 let busy = $state(false);
 let error = $state('');
 async function add(event: SubmitEvent) {
  event.preventDefault(); busy = true; error = '';
  try {
   const call = await api<Call>('/api/calls', { method:'POST', body:JSON.stringify({ invitation, topic, scheduled_at:scheduledAt ? new Date(scheduledAt).toISOString() : null }) });
   open = false; invitation = ''; topic = ''; scheduledAt = '';
   await goto('/calls/' + encodeURIComponent(call.id));
  } catch (e) { error = errorMessage(e); }
  finally { busy = false; }
 }
</script>
{#if canSchedule}<Button.Root onclick={() => { error = ''; open = true; }}><CalendarPlus size={15}/> Add scheduled call</Button.Root>{/if}
<Dialog.Root bind:open>
 <Dialog.Content>
  <Dialog.Header><Dialog.Title>Add a scheduled Zoom call</Dialog.Title><Dialog.Description>Paste an invitation to prepare a call page and stream URL. Clients can connect now and wait for the next live session on this link.</Dialog.Description></Dialog.Header>
  <form onsubmit={add}>
   <label for="zoom-invitation">Zoom invitation or meeting link</label>
   <textarea id="zoom-invitation" class="search-input" rows="4" required maxlength="32000" placeholder="Paste the invitation, or https://us02web.zoom.us/j/…" bind:value={invitation}></textarea>
   <label for="scheduled-topic">Call title <span>(optional)</span></label>
   <input id="scheduled-topic" class="search-input" maxlength="200" placeholder="Weekly planning" bind:value={topic}/>
   <label for="scheduled-start">Expected start <span>(optional, your local time)</span></label>
   <input id="scheduled-start" class="search-input" type="datetime-local" bind:value={scheduledAt}/>
   <p class="muted small">Your clients connect to Zoom Connect. The stream waits until Zoom Connect receives the meeting data. This adds one session, not a recurring schedule.</p>
   {#if error}<p role="alert" class="form-error">{error}</p>{/if}
   <Dialog.Footer><Button.Root type="button" variant="outline" onclick={() => open=false}>Cancel</Button.Root><Button.Root type="submit" disabled={busy || !invitation.trim()}>{busy ? 'Adding…' : 'Add call'}</Button.Root></Dialog.Footer>
  </form>
 </Dialog.Content>
</Dialog.Root>
<style>
 form { display:grid; gap:10px; }
 label { font-weight:550; font-size:13px; margin-top:6px; }
 label span { color:var(--muted-foreground); font-weight:400; }
 textarea { resize:vertical; min-height:90px; }
 .form-error { color:var(--destructive); font-size:13px; }
</style>
