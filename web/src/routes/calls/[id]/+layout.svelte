<script lang="ts">
	import { page } from '$app/state';
	import type { LayoutProps } from './$types';
	import CallWorkspace from './CallWorkspace.svelte';

	let { children }: LayoutProps = $props();
</script>

<!-- The call view lives in this layout so it stays mounted (stream, tab,
     search and scroll) while a child route selects a different page. It is
     keyed on the call id: another call starts from a fresh view, and the old
     one closes its streams and timers as it unmounts. -->
{#key page.params.id}
	<CallWorkspace callId={page.params.id ?? ''} artifactId={page.params.artifactId ?? ''} />
{/key}
{@render children()}
