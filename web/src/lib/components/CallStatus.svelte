<script lang="ts">
	import Radio from '@lucide/svelte/icons/radio';
	import { Badge } from '#lib/components/ui/badge';
	import type { Call } from '#lib/types';

	let { status }: { status: Call['status'] } = $props();
</script>

{#if status === 'live'}
	<Badge class="bg-live text-live-foreground"><Radio aria-hidden="true" />Live</Badge>
{:else if status === 'scheduled'}
	<Badge variant="outline">Scheduled</Badge>
{:else if status === 'interrupted'}
	<Badge variant="outline">Recording interrupted</Badge>
{:else if status === 'cancelled'}
	<Badge variant="outline">Cancelled</Badge>
{:else}
	<Badge variant="secondary">Ended</Badge>
{/if}
