<script lang="ts">
	import { page } from '$app/state';
	import type { LayoutProps } from './$types';
	import CallWorkspace from './CallWorkspace.svelte';

	let { children }: LayoutProps = $props();
</script>

<!-- The call view lives in this layout so it stays mounted (stream, tab,
     search and scroll) while a child route selects a different page. -->
<CallWorkspace callId={page.params.id ?? ''} artifactId={page.params.artifactId ?? ''} />
{@render children()}
