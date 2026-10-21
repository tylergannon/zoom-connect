<script lang="ts">
	import { toast } from 'svelte-sonner';
	import * as AlertDialog from '#lib/components/ui/alert-dialog';
	import { Button, buttonVariants } from '#lib/components/ui/button';
	import { Spinner } from '#lib/components/ui/spinner';
	import { api, errorMessage } from '#lib/api';
	import type { Call } from '#lib/types';

	let { call, oncancelled }: { call: Call; oncancelled: () => void } = $props();
	let open = $state(false);
	let busy = $state(false);
	let error = $state('');

	async function cancel() {
		busy = true;
		error = '';
		try {
			await api(`/api/calls/${encodeURIComponent(call.id)}/cancel`, { method: 'POST', body: '{}' });
			open = false;
			toast.success('Scheduled call cancelled');
			oncancelled();
		} catch (e) {
			error = errorMessage(e);
		} finally {
			busy = false;
		}
	}
</script>

<AlertDialog.Root bind:open onOpenChangeComplete={(o) => { if (!o) error = ''; }}>
	<AlertDialog.Trigger class={buttonVariants({ variant: 'destructive', class: 'w-full' })}>Cancel scheduled call</AlertDialog.Trigger>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Cancel “{call.topic}”?</AlertDialog.Title>
			<AlertDialog.Description>
				It won’t be recorded, and it leaves everyone’s upcoming calls. Agents waiting on its stream are told it was cancelled. You can’t undo this.
			</AlertDialog.Description>
		</AlertDialog.Header>
		{#if error}<p role="alert" class="text-sm text-destructive">{error}</p>{/if}
		<AlertDialog.Footer>
			<AlertDialog.Cancel>Keep call</AlertDialog.Cancel>
			<Button variant="destructive" disabled={busy} onclick={cancel}>
				{#if busy}<Spinner aria-hidden="true" role="presentation" />Cancelling…{:else}Cancel call{/if}
			</Button>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
