<script lang="ts">
	import * as Dialog from '#lib/components/ui/dialog';
	import ShareAccess from './ShareAccess.svelte';

	let {
		open = $bindable(false),
		callId,
		topic,
		hostId,
		canShare = false,
		onchange
	}: { open?: boolean; callId: string; topic: string; hostId: string; canShare?: boolean; onchange?: () => void } = $props();
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-lg">
		<ShareAccess {callId} {topic} {hostId} {canShare} {onchange} />
	</Dialog.Content>
</Dialog.Root>
