<script lang="ts">
 import * as Button from '#lib/components/ui/button';
 import { api, errorMessage, relativeDate } from '#lib/api';
 import type { Call } from '#lib/types';
 let { call, oncancelled }: { call:Call; oncancelled:()=>void } = $props();
 let busy = $state(false);
 let error = $state('');
 async function cancel() {
  busy = true; error = '';
  try { await api(`/api/calls/${encodeURIComponent(call.id)}/cancel`,{method:'POST',body:'{}'}); oncancelled(); }
  catch (e) { error = errorMessage(e); }
  finally { busy = false; }
 }
</script>
<div class="panel waiting">
 <div><h2>Ready for clients to connect</h2><p>This call’s stream is open now. Clients connected to Zoom Connect will receive events here when the next session on this Zoom link streams.</p>
 {#if call.scheduled_at}<p class="muted small">Expected {relativeDate(call.scheduled_at)}. The stream keeps waiting if the meeting starts late.</p>{/if}
 <p class="muted small">Capture begins when Zoom Connect’s app user starts streaming or joins with auto-start enabled, subject to the host’s settings.</p>
 {#if error}<p role="alert">{error}</p>{/if}</div>
 {#if call.is_owner}<Button.Root variant="outline" disabled={busy} onclick={cancel}>{busy?'Cancelling…':'Cancel scheduled call'}</Button.Root>{/if}
</div>
<style>
 .waiting { display:flex; align-items:start; justify-content:space-between; gap:18px; padding:18px; margin-bottom:14px; }
 h2 { font-size:15px; margin:0 0 6px; }
 p { margin:6px 0 0; max-width:720px; }
 @media(max-width:640px) { .waiting { flex-direction:column; } }
</style>
