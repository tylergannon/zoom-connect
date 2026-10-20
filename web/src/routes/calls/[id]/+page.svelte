<script lang="ts">
	import { page } from '$app/state';
	import CallWorkspace from './CallWorkspace.svelte';
</script>

{#key page.params.id}
	<CallWorkspace callId={page.params.id ?? ''} />
{/key}
