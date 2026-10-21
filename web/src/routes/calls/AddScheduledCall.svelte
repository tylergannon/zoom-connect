<script lang="ts">
	import { goto } from '$app/navigation';
	import CircleAlert from '@lucide/svelte/icons/circle-alert';
	import * as Alert from '#lib/components/ui/alert';
	import * as Dialog from '#lib/components/ui/dialog';
	import * as Field from '#lib/components/ui/field';
	import { Button } from '#lib/components/ui/button';
	import { Input } from '#lib/components/ui/input';
	import { Textarea } from '#lib/components/ui/textarea';
	import { Spinner } from '#lib/components/ui/spinner';
	import { api, errorMessage } from '#lib/api';
	import { callIndex } from '#lib/calls.svelte';
	import type { Call } from '#lib/types';

	let { open = $bindable(false) }: { open: boolean } = $props();
	let invitation = $state('');
	let topic = $state('');
	let scheduledAt = $state('');
	let busy = $state(false);
	let error = $state('');

	async function add(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		try {
			const call = await api<Call>('/api/calls', {
				method: 'POST',
				body: JSON.stringify({ invitation, topic, scheduled_at: scheduledAt ? new Date(scheduledAt).toISOString() : null })
			});
			open = false;
			invitation = topic = scheduledAt = '';
			void callIndex.refresh();
			await goto('/calls/' + encodeURIComponent(call.id));
		} catch (e) {
			error = errorMessage(e);
		} finally {
			busy = false;
		}
	}
</script>

<Dialog.Root bind:open onOpenChangeComplete={(o) => { if (!o) error = ''; }}>
	<Dialog.Content class="sm:max-w-lg">
		<Dialog.Header>
			<Dialog.Title>Add a scheduled call</Dialog.Title>
			<Dialog.Description>
				Paste a Zoom invitation to make the call’s page now. You can share it and point agents at it before the meeting starts.
			</Dialog.Description>
		</Dialog.Header>
		<form id="schedule-form" class="flex flex-col gap-6" onsubmit={add}>
			{#if error}
				<Alert.Root variant="destructive">
					<CircleAlert aria-hidden="true" />
					<Alert.Title>We couldn’t add the call</Alert.Title>
					<Alert.Description>{error}</Alert.Description>
				</Alert.Root>
			{/if}
			<Field.Group>
				<Field.Field>
					<Field.Label for="zoom-invitation">Zoom invitation or meeting link</Field.Label>
					<Textarea
						id="zoom-invitation"
						rows={4}
						required
						maxlength={32000}
						placeholder="https://zoom.us/j/…"
						aria-describedby="invitation-help"
						bind:value={invitation}
					/>
					<Field.Description id="invitation-help">The whole invitation works. We only need the meeting link or ID.</Field.Description>
				</Field.Field>
				<Field.Field>
					<Field.Label for="scheduled-topic">Title <span class="font-normal text-muted-foreground">(optional)</span></Field.Label>
					<Input id="scheduled-topic" maxlength={200} autocomplete="off" bind:value={topic} />
				</Field.Field>
				<Field.Field>
					<Field.Label for="scheduled-start">Expected start <span class="font-normal text-muted-foreground">(optional)</span></Field.Label>
					<Input id="scheduled-start" type="datetime-local" aria-describedby="start-help" bind:value={scheduledAt} />
					<Field.Description id="start-help">Your local time. This adds one meeting, not a repeating series.</Field.Description>
				</Field.Field>
			</Field.Group>
		</form>
		<Dialog.Footer>
			<Dialog.Close>
				{#snippet child({ props })}
					<Button {...props} variant="outline">Cancel</Button>
				{/snippet}
			</Dialog.Close>
			<Button type="submit" form="schedule-form" disabled={busy || !invitation.trim()}>
				{#if busy}<Spinner aria-hidden="true" role="presentation" />Adding…{:else}Add call{/if}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
