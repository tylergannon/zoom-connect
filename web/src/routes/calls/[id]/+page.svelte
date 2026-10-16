<script lang="ts">
	import { page } from '$app/state';
	import CallWorkspace from './CallWorkspace.svelte';
</script>

<CallWorkspace callId={page.params.id ?? ''} />
